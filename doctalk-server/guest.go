package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const guestCookieName = "dt_guest"

var (
	errGuestToken = errors.New("invalid guest token")
	guestHeader   = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
)

type guestClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

func guestIDFrom(c *gin.Context) string {
	v, ok := c.Get("guestID")
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func signGuestJWT(secret, sub string, exp time.Time) (string, error) {
	if secret == "" || sub == "" {
		return "", errGuestToken
	}
	payload, err := json.Marshal(guestClaims{Sub: sub, Exp: exp.Unix()})
	if err != nil {
		return "", err
	}
	signing := guestHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseGuestJWT(secret, token string) (string, error) {
	if secret == "" || token == "" {
		return "", errGuestToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errGuestToken
	}
	signing := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, got) {
		return "", errGuestToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errGuestToken
	}
	var claims guestClaims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Sub == "" {
		return "", errGuestToken
	}
	if claims.Exp > 0 && time.Now().Unix() >= claims.Exp {
		return "", errGuestToken
	}
	return claims.Sub, nil
}

func newGuestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hex := "0123456789abcdef"
	out := make([]byte, 36)
	j := 0
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}
		out[j] = hex[v>>4]
		out[j+1] = hex[v&0x0f]
		j += 2
	}
	return string(out)
}

func guestMiddleware(s *server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		secret := s.cfg.JWTSecret
		if secret == "" {
			c.Next()
			return
		}
		ttl := s.cfg.GuestTTL
		if ttl < time.Hour {
			ttl = defaultGuestTTL
		}
		id := ""
		if ck, err := c.Request.Cookie(guestCookieName); err == nil {
			id, _ = parseGuestJWT(secret, ck.Value)
		}
		if id == "" {
			id = newGuestID()
		}
		token, err := signGuestJWT(secret, id, time.Now().Add(ttl))
		if err == nil {
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     guestCookieName,
				Value:    token,
				Path:     "/",
				MaxAge:   int(ttl.Seconds()),
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}
		c.Set("guestID", id)
		c.Next()
	}
}
