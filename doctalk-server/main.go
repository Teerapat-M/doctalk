package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrate(ctx, db); err != nil {
		return err
	}

	var embed Embedder
	var chat ChatStreamer
	var quiz Quizzer
	if cfg.GeminiAPIKey != "" {
		lim := newSlotLimiter(cfg.GeminiMaxInFlight)
		embed = newGeminiEmbedder(cfg, lim)
		chat = newGeminiChat(cfg, lim)
		quiz = newGeminiQuiz(cfg, lim)
	}

	s := &server{
		ping:    db,
		db:      db,
		cfg:     cfg,
		store:   newStorage(cfg),
		embed:   embed,
		chat:    chat,
		quiz:    quiz,
		extract: pdfExtractor{},
		limits:  newRateLimiter(cfg),
	}
	s.startGuestCleanup()
	r := newRouter(s)
	addr := "0.0.0.0:" + cfg.Port
	log.Printf("listening on %s", addr)
	return r.Run(addr)
}
