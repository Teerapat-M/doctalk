package main

import (
	"fmt"
	"net/http"
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
	defaultQuizN             = 5
	defaultQuizNMax          = 20
	defaultVitePort          = "3917"
	defaultCORSOrigin        = "http://localhost:" + defaultVitePort + ",http://127.0.0.1:" + defaultVitePort
	defaultQuizPerDay        = 5
	defaultAskPerMinute      = 20
	defaultIngestPerMinute   = 10
	defaultQuizIPPerDay      = 20
	defaultQuizResetHour     = 7
	defaultGuestTTL          = 7 * 24 * time.Hour
	defaultMaxPages          = 50
	defaultMaxRows           = 2000
	minJWTSecretBytes        = 32
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
	GeminiQuizModel         string
	GeminiEmbedBatch        int
	GeminiMaxInFlight       int
	GeminiRetryBudget       time.Duration
	QuizNMax                int
	AskTopK                 int
	AskTopKMax              int
	AskTimeout              time.Duration
	MaxUploadBytes          int64
	MaxPages                int
	MaxRows                 int
	StorageDir              string
	TrustedProxies          []string
	JWTSecret               string
	QuizPerDay              int
	AskPerMinute            int
	IngestPerMinute         int
	QuizIPPerDay            int
	QuizResetHour           int
	GuestTTL                time.Duration
	CookieSecure            bool
	CookieSameSite          http.SameSite
}

func loadConfig() (config, error) {
	cfg := config{
		Port:                    listenPort(),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		CORSOrigin:              getenv("CORS_ORIGIN", defaultCORSOrigin),
		GeminiAPIKey:            os.Getenv("GEMINI_API_KEY"),
		GeminiBaseURL:           strings.TrimRight(getenv("GEMINI_BASE_URL", defaultGeminiBaseURL), "/"),
		GeminiEmbeddingModel:    getenv("GEMINI_EMBEDDING_MODEL", defaultEmbedModel),
		GeminiChatModel:         getenv("GEMINI_CHAT_MODEL", defaultChatModel),
		GeminiChatFallbackModel: os.Getenv("GEMINI_CHAT_FALLBACK_MODEL"),
		GeminiQuizModel:         os.Getenv("GEMINI_QUIZ_MODEL"),
		GeminiEmbedBatch:        getenvInt("GEMINI_EMBED_BATCH", defaultEmbedBatch),
		GeminiMaxInFlight:       getenvInt("GEMINI_MAX_IN_FLIGHT", defaultGeminiMaxInFlight),
		GeminiRetryBudget:       time.Duration(getenvInt("GEMINI_RETRY_BUDGET_SECONDS", 8)) * time.Second,
		QuizNMax:                getenvInt("QUIZ_N_MAX", defaultQuizNMax),
		AskTopK:                 getenvInt("ASK_TOP_K", defaultAskTopK),
		AskTopKMax:              getenvInt("ASK_TOP_K_MAX", defaultAskTopKMax),
		AskTimeout:              time.Duration(getenvInt("ASK_TIMEOUT_SECONDS", defaultAskTimeoutSec)) * time.Second,
		MaxUploadBytes:          getenvInt64("MAX_UPLOAD_BYTES", defaultMaxUpload),
		MaxPages:                getenvInt("MAX_PAGES", defaultMaxPages),
		MaxRows:                 getenvInt("MAX_ROWS", defaultMaxRows),
		StorageDir:              getenv("STORAGE_DIR", defaultStorageDir),
		TrustedProxies:          splitCSV(os.Getenv("TRUSTED_PROXIES")),
		JWTSecret:               os.Getenv("JWT_SECRET"),
		QuizPerDay:              getenvInt("QUIZ_PER_DAY", defaultQuizPerDay),
		AskPerMinute:            getenvInt("ASK_PER_MINUTE", defaultAskPerMinute),
		IngestPerMinute:         getenvInt("INGEST_PER_MINUTE", defaultIngestPerMinute),
		QuizIPPerDay:            getenvInt("QUIZ_IP_PER_DAY", defaultQuizIPPerDay),
		QuizResetHour:           getenvInt("QUIZ_RESET_HOUR", defaultQuizResetHour),
		GuestTTL:                time.Duration(getenvInt("GUEST_TTL_HOURS", int(defaultGuestTTL/time.Hour))) * time.Hour,
		CookieSecure:            getenvBool("COOKIE_SECURE", false),
		CookieSameSite:          parseCookieSameSite(os.Getenv("COOKIE_SAMESITE")),
	}
	if cfg.DatabaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretBytes {
		return config{}, fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretBytes)
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
	if cfg.GeminiQuizModel == "" {
		cfg.GeminiQuizModel = cfg.GeminiChatModel
	}
	if cfg.QuizNMax < 1 {
		cfg.QuizNMax = defaultQuizNMax
	}
	if cfg.QuizNMax > 50 {
		cfg.QuizNMax = 50
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
	if cfg.MaxPages < 1 {
		cfg.MaxPages = defaultMaxPages
	}
	if cfg.MaxRows < 1 {
		cfg.MaxRows = defaultMaxRows
	}
	if cfg.QuizPerDay < 1 {
		cfg.QuizPerDay = defaultQuizPerDay
	}
	if cfg.AskPerMinute < 1 {
		cfg.AskPerMinute = defaultAskPerMinute
	}
	if cfg.IngestPerMinute < 1 {
		cfg.IngestPerMinute = defaultIngestPerMinute
	}
	if cfg.QuizIPPerDay < 1 {
		cfg.QuizIPPerDay = defaultQuizIPPerDay
	}
	if cfg.CookieSameSite == http.SameSiteNoneMode {
		cfg.CookieSecure = true
	}
	if cfg.QuizResetHour < 0 || cfg.QuizResetHour > 23 {
		cfg.QuizResetHour = defaultQuizResetHour
	}
	if cfg.GuestTTL < time.Hour {
		cfg.GuestTTL = defaultGuestTTL
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

func getenvBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseCookieSameSite(v string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
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
