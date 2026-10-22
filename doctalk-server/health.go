package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type pinger interface {
	PingContext(ctx context.Context) error
}

func healthHandler(db pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		payload := gin.H{
			"status": "ok",
			"checks": gin.H{"database": "ok"},
		}
		status := http.StatusOK

		if db == nil || db.PingContext(ctx) != nil {
			status = http.StatusServiceUnavailable
			payload["status"] = "unhealthy"
			payload["checks"] = gin.H{"database": "unreachable"}
		}

		c.JSON(status, payload)
	}
}
