package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validUUID(s string) bool {
	return uuidRE.MatchString(s)
}

func isJSONContentType(h string) bool {
	mt, _, err := mime.ParseMediaType(h)
	return err == nil && strings.EqualFold(mt, "application/json")
}

func requireJSONBody(c *gin.Context, dst any) bool {
	if !isJSONContentType(c.GetHeader("Content-Type")) {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Content-Type must be application/json"})
		return false
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}
	return true
}

func invalidDocumentIDs(ids []string) bool {
	for _, id := range ids {
		if !validUUID(id) {
			return true
		}
	}
	return false
}

func parseOptionalK(k *int, def, max int) (int, string) {
	return parseOptionalBounded(k, def, max, "k")
}

func parseOptionalBounded(v *int, def, max int, name string) (int, string) {
	if max < 1 {
		max = defaultAskTopKMax
	}
	if def < 1 {
		def = 1
	}
	if def > max {
		def = max
	}
	if v == nil {
		return def, ""
	}
	if *v < 1 || *v > max {
		return 0, name + " is out of range"
	}
	return *v, ""
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "none" {
			out = append(out, p)
		}
	}
	return out
}
