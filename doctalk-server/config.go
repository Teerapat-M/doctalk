package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EmbeddingDim is the pgvector column size (migrations/0001_init.sql).
// gemini-embedding-001 supports 768, 1536, and 3072; 768 is the
// storage-friendly Matryoshka size for free-tier Postgres. Values at
// 768 must be L2-normalized (required by Gemini for non-3072 dims).
const EmbeddingDim = 768

const (
	defaultAPIPort       = "8081"
	defaultMaxUpload     = 10 << 20 // 10 MiB
	defaultStorageDir    = "data/uploads"
	defaultEmbedBatch    = 16
	defaultEmbedModel    = "gemini-embedding-001"
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com"
)

type config struct {
	Port                 string
	DatabaseURL          string
	CORSOrigin           string
	GeminiAPIKey         string
	GeminiBaseURL        string
	GeminiEmbeddingModel string
	GeminiEmbedBatch     int
	MaxUploadBytes       int64
	StorageDir           string
}

func loadConfig() (config, error) {
	cfg := config{
		Port:                 listenPort(),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		CORSOrigin:           getenv("CORS_ORIGIN", "*"),
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		GeminiBaseURL:        strings.TrimRight(getenv("GEMINI_BASE_URL", defaultGeminiBaseURL), "/"),
		GeminiEmbeddingModel: getenv("GEMINI_EMBEDDING_MODEL", defaultEmbedModel),
		GeminiEmbedBatch:     getenvInt("GEMINI_EMBED_BATCH", defaultEmbedBatch),
		MaxUploadBytes:       getenvInt64("MAX_UPLOAD_BYTES", defaultMaxUpload),
		StorageDir:           getenv("STORAGE_DIR", defaultStorageDir),
	}
	if cfg.DatabaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.GeminiEmbedBatch < 1 {
		cfg.GeminiEmbedBatch = defaultEmbedBatch
	}
	if cfg.MaxUploadBytes < 1 {
		cfg.MaxUploadBytes = defaultMaxUpload
	}
	if cfg.GeminiBaseURL == "" {
		cfg.GeminiBaseURL = defaultGeminiBaseURL
	}
	return cfg, nil
}

// listenPort prefers API_PORT (local compose / host) then PORT (Render).
func listenPort() string {
	if v := os.Getenv("API_PORT"); v != "" {
		return v
	}
	if v := os.Getenv("PORT"); v != "" {
		return v
	}
	return defaultAPIPort
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getenvInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
