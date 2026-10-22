package main

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type server struct {
	ping    pinger
	db      *sql.DB
	cfg     config
	store   Storage
	embed   Embedder
	chat    ChatStreamer
	quiz    Quizzer
	extract Extractor
	limits  *rateLimiter
	wg      sync.WaitGroup
}

func newRouter(s *server) *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(s.cfg.TrustedProxies); err != nil {
		panic(fmt.Sprintf("TRUSTED_PROXIES: %v", err))
	}
	r.Use(gin.Logger(), gin.Recovery(), cors(s.cfg.CORSOrigin), guestMiddleware(s))
	r.MaxMultipartMemory = 8 << 20
	r.GET("/health", healthHandler(s.ping))
	r.GET("/quota", s.getQuota)
	r.POST("/documents", s.uploadDocument)
	r.GET("/documents", s.listDocuments)
	r.GET("/documents/:id", s.getDocument)
	r.POST("/documents/:id/ingest", s.ingestDocument)
	r.POST("/ask", s.ask)
	r.POST("/quiz", s.generateQuiz)
	return r
}

func cors(originCSV string) gin.HandlerFunc {
	origins := splitCSV(originCSV)
	wildcard := false
	allow := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		if o == "*" {
			wildcard = true
		}
		allow[o] = struct{}{}
	}
	return func(c *gin.Context) {
		reqOrigin := c.GetHeader("Origin")
		switch {
		case wildcard:
			c.Header("Access-Control-Allow-Origin", "*")
		case reqOrigin != "":
			if _, ok := allow[reqOrigin]; ok {
				c.Header("Access-Control-Allow-Origin", reqOrigin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Expose-Headers", "Retry-After")
				c.Header("Vary", "Origin")
			}
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (s *server) clientIP(c *gin.Context) string {
	if name := s.cfg.ClientIPHeader; name != "" {
		if ip := headerClientIP(c.GetHeader(name)); ip != "" {
			return ip
		}
	}
	return c.ClientIP()
}

func headerClientIP(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if net.ParseIP(v) == nil {
		return ""
	}
	return v
}
