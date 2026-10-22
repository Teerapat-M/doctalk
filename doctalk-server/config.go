package main

import (
	"fmt"
	"os"
)

// EmbeddingDim is the pgvector column size in migrations/0001_init.sql.
// gemini-embedding-001 supports 768, 1536, and 3072; 768 is the
// storage-friendly Matryoshka size for free-tier Postgres.
const EmbeddingDim = 768

type config struct {
	Port                 string
	DatabaseURL          string
	CORSOrigin           string
	GeminiAPIKey         string
	GeminiEmbeddingModel string
}

func loadConfig() (config, error) {
	cfg := config{
		Port:                 getenv("PORT", "8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		CORSOrigin:           getenv("CORS_ORIGIN", "*"),
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		GeminiEmbeddingModel: getenv("GEMINI_EMBEDDING_MODEL", "gemini-embedding-001"),
	}
	if cfg.DatabaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
