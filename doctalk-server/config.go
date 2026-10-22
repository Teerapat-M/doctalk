package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// EmbeddingDim is the pgvector column size (migrations/0001_init.sql).
// gemini-embedding-001 supports 768, 1536, and 3072; 768 is the
// storage-friendly Matryoshka size for free-tier Postgres. Values at
// 768 must be L2-normalized (required by Gemini for non-3072 dims).
const EmbeddingDim = 768

const (
	defaultAPIPort           = "8181"
	defaultMaxUpload         = 10 << 20 // 10 MiB
	defaultStorageDir        = "data/uploads"
	defaultEmbedBatch        = 16
	defaultEmbedModel        = "gemini-embedding-001"
	defaultChatModel         = "gemini-3.8-flash"
	defaultGeminiBaseURL     = "https://generativelanguage.googleapis.com"
	defaultAskTopK           = 5
	defaultAskTopKMax        = 20
	defaultAskTimeoutSec     = 60
	defaultGeminiAttempts    = 3
	defaultGeminiRetryBudget = 8 * time.Second
	defaultGeminiMaxInFlight = 2
	maxQuestionRunes         = 4000
	maxAskDocumentIDs        = 32
	citationSnippetRunes     = 240
)

type config struct {
	Port                    string
	DatabaseURL             string
	CORSOrigin              string
	GeminiAPIKey            string
	GeminiBaseURL           string
	GeminiEmbeddingModel    string
	GeminiChatModel         string
	GeminiChatFallbackModel string
	GeminiEmbedBatch        int
	GeminiMaxInFlight       int
	GeminiRetryBudget       time.Duration
	AskTopK                 int
	AskTopKMax              int
	AskTimeout              time.Duration
	MaxUploadBytes          int64
	StorageDir              string
	TrustedProxies          []string
}

func loadConfig() (config, error) {
	cfg := config{
		Port:                    listenPort(),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		CORSOrigin:              getenv("CORS_ORIGIN", "*"),
		GeminiAPIKey:            os.Getenv("GEMINI_API_KEY"),
		GeminiBaseURL:           strings.TrimRight(getenv("GEMINI_BASE_URL", defaultGeminiBaseURL), "/"),
		GeminiEmbeddingModel:    getenv("GEMINI_EMBEDDING_MODEL", defaultEmbedModel),
		GeminiChatModel:         getenv("GEMINI_CHAT_MODEL", defaultChatModel),
		GeminiChatFallbackModel: os.Getenv("GEMINI_CHAT_FALLBACK_MODEL"),
		GeminiEmbedBatch:        getenvInt("GEMINI_EMBED_BATCH", defaultEmbedBatch),
		GeminiMaxInFlight:       getenvInt("GEMINI_MAX_IN_FLIGHT", defaultGeminiMaxInFlight),
		GeminiRetryBudget:       time.Duration(getenvInt("GEMINI_RETRY_BUDGET_SECONDS", 8)) * time.Second,
		AskTopK:                 getenvInt("ASK_TOP_K", defaultAskTopK),
		TrustedProxies:          splitCSV(os.Getenv("TRUSTED_PROXIES")),
		AskTopKMax:              getenvInt("ASK_TOP_K_MAX", defaultAskTopKMax),
		AskTimeout:              time.Duration(getenvInt("ASK_TIMEOUT_SECONDS", defaultAskTimeoutSec)) * time.Second,
		MaxUploadBytes:          getenvInt64("MAX_UPLOAD_BYTES", defaultMaxUpload),
		StorageDir:              getenv("STORAGE_DIR", defaultStorageDir),
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
	if cfg.GeminiChatModel == "" {
		cfg.GeminiChatModel = defaultChatModel
	}
	if cfg.AskTopKMax < 1 {
		cfg.AskTopKMax = defaultAskTopKMax
	}
	if cfg.AskTopK < 1 {
		cfg.AskTopK = defaultAskTopK
	}
	if cfg.AskTopK > cfg.AskTopKMax {
		cfg.AskTopK = cfg.AskTopKMax
	}
	if cfg.AskTimeout < time.Second {
		cfg.AskTimeout = time.Duration(defaultAskTimeoutSec) * time.Second
	}
	if cfg.GeminiMaxInFlight < 1 {
		cfg.GeminiMaxInFlight = defaultGeminiMaxInFlight
	}
	if cfg.GeminiRetryBudget < time.Second {
		cfg.GeminiRetryBudget = defaultGeminiRetryBudget
	}
	if cfg.GeminiRetryBudget > cfg.AskTimeout/2 {
		cfg.GeminiRetryBudget = cfg.AskTimeout / 2
		if cfg.GeminiRetryBudget < time.Second {
			cfg.GeminiRetryBudget = time.Second
		}
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
