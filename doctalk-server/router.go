package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func newRouter(db pinger, cfg config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), cors(cfg.CORSOrigin))
	r.GET("/health", healthHandler(db))
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
