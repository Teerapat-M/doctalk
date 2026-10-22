package main

import (
	"database/sql"
	"net/http"
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
	wg      sync.WaitGroup
}

func newRouter(s *server) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(s.cfg.TrustedProxies)
	r.Use(gin.Logger(), gin.Recovery(), cors(s.cfg.CORSOrigin))
	r.MaxMultipartMemory = 8 << 20
	r.GET("/health", healthHandler(s.ping))
	r.POST("/documents", s.uploadDocument)
	r.GET("/documents", s.listDocuments)
	r.GET("/documents/:id", s.getDocument)
	r.POST("/documents/:id/ingest", s.ingestDocument)
	r.POST("/ask", s.ask)
	r.POST("/quiz", s.generateQuiz)
	return r
}

func cors(origin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
