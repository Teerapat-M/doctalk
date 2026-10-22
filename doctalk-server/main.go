package main

import (
	"context"
	"log"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
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
	if cfg.GeminiAPIKey != "" {
		embed = newGeminiEmbedder(cfg)
	}

	s := &server{
		ping:    db,
		db:      db,
		cfg:     cfg,
		store:   newDiskStorage(cfg.StorageDir),
		embed:   embed,
		extract: pdfExtractor{},
	}
	r := newRouter(s)
	addr := "0.0.0.0:" + cfg.Port
	log.Printf("listening on %s", addr)
	return r.Run(addr)
}
