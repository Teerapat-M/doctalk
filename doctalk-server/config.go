package main

import (
	"fmt"
	"net"
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
	defaultAPIPort              = "8181"
	defaultMaxUpload            = 10 << 20 // 10 MiB
	defaultStorageDir           = "data/uploads"
	defaultEmbedBatch           = 16
	defaultEmbedModel           = "gemini-embedding-001"
	defaultChatModel            = "gemini-3.8-flash"
	defaultGeminiFallbackModel  = "gemini-2.0-flash"
	defaultGeminiBaseURL        = "https://generativelanguage.googleapis.com"
	defaultAskTopK              = 5
	defaultAskTopKMax           = 20
	defaultAskTimeoutSec        = 60
	defaultGeminiAttempts       = 3
	maxGeminiAttempts           = 8
	defaultGeminiRetryBudget    = 8 * time.Second
	defaultGeminiRetryBase      = 500 * time.Millisecond
	defaultGeminiRetryMaxDelay  = 4 * time.Second
	defaultGeminiRequestTimeout = 25 * time.Second
	defaultGeminiMaxInFlight    = 2
	maxQuestionRunes            = 4000
	maxAskDocumentIDs           = 32
	citationSnippetRunes        = 240
	defaultQuizN                = 5
	defaultQuizNMax             = 20
	defaultVitePort             = "3917"
	defaultCORSOrigin           = "http://localhost:" + defaultVitePort + ",http://127.0.0.1:" + defaultVitePort
	defaultQuizPerDay           = 5
	defaultAskPerMinute         = 20
	defaultIngestPerMinute      = 10
	defaultQuizIPPerDay         = 20
	defaultQuizResetHour        = 7
	defaultGuestTTL             = 7 * 24 * time.Hour
	defaultMaxPages             = 50
	defaultMaxRows              = 2000
	defaultSupabaseBucket       = "doctalk-uploads"
	minJWTSecretBytes           = 32
)

type config struct {
	Port                 string
	DatabaseURL          string
	CORSOrigin           string
	GeminiAPIKey         string
	GeminiBaseURL        string
	GeminiEmbeddingModel string
	GeminiChatModel      string
	GeminiFallbackModel  string
	GeminiQuizModel      string
	GeminiEmbedBatch     int
	GeminiMaxInFlight    int
	GeminiRetryAttempts  int
	GeminiRetryBudget    time.Duration
	GeminiRetryBaseDelay time.Duration
	GeminiRetryMaxDelay  time.Duration
	GeminiRequestTimeout time.Duration
	QuizNMax             int
	AskTopK              int
	AskTopKMax           int
	AskTimeout           time.Duration
	MaxUploadBytes       int64
	MaxPages             int
	MaxRows              int
	StorageDir           string
	TrustedProxies       []string
	ClientIPHeader       string
	JWTSecret            string
	QuizPerDay           int
	AskPerMinute         int
	IngestPerMinute      int
	QuizIPPerDay         int
	QuizResetHour        int
	GuestTTL             time.Duration
	CookieSecure         bool
	CookieSameSite       http.SameSite
	SupabaseURL          string
	SupabaseServiceKey   string
	SupabaseBucket       string
}

func loadConfig() (config, error) {
	cfg := config{
		Port:                 listenPort(),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		CORSOrigin:           getenv("CORS_ORIGIN", defaultCORSOrigin),
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		GeminiBaseURL:        strings.TrimRight(getenv("GEMINI_BASE_URL", defaultGeminiBaseURL), "/"),
		GeminiEmbeddingModel: getenv("GEMINI_EMBEDDING_MODEL", defaultEmbedModel),
		GeminiChatModel:      getenv("GEMINI_CHAT_MODEL", defaultChatModel),
		GeminiFallbackModel:  loadGeminiFallbackModel(),
		GeminiQuizModel:      os.Getenv("GEMINI_QUIZ_MODEL"),
		GeminiEmbedBatch:     getenvInt("GEMINI_EMBED_BATCH", defaultEmbedBatch),
		GeminiMaxInFlight:    getenvInt("GEMINI_MAX_IN_FLIGHT", defaultGeminiMaxInFlight),
		GeminiRetryAttempts:  getenvInt("GEMINI_RETRY_ATTEMPTS", defaultGeminiAttempts),
		GeminiRetryBudget:    time.Duration(getenvInt("GEMINI_RETRY_BUDGET_SECONDS", 8)) * time.Second,
		GeminiRetryBaseDelay: time.Duration(getenvInt("GEMINI_RETRY_BASE_DELAY_MS", 500)) * time.Millisecond,
		GeminiRetryMaxDelay:  time.Duration(getenvInt("GEMINI_RETRY_MAX_DELAY_MS", 4000)) * time.Millisecond,
		GeminiRequestTimeout: time.Duration(getenvInt("GEMINI_REQUEST_TIMEOUT_SECONDS", 25)) * time.Second,
		QuizNMax:             getenvInt("QUIZ_N_MAX", defaultQuizNMax),
		AskTopK:              getenvInt("ASK_TOP_K", defaultAskTopK),
		AskTopKMax:           getenvInt("ASK_TOP_K_MAX", defaultAskTopKMax),
		AskTimeout:           time.Duration(getenvInt("ASK_TIMEOUT_SECONDS", defaultAskTimeoutSec)) * time.Second,
		MaxUploadBytes:       getenvInt64("MAX_UPLOAD_BYTES", defaultMaxUpload),
		MaxPages:             getenvInt("MAX_PAGES", defaultMaxPages),
		MaxRows:              getenvInt("MAX_ROWS", defaultMaxRows),
		StorageDir:           getenv("STORAGE_DIR", defaultStorageDir),
		TrustedProxies:       splitCSV(os.Getenv("TRUSTED_PROXIES")),
		JWTSecret:            os.Getenv("JWT_SECRET"),
		QuizPerDay:           getenvInt("QUIZ_PER_DAY", defaultQuizPerDay),
		AskPerMinute:         getenvInt("ASK_PER_MINUTE", defaultAskPerMinute),
		IngestPerMinute:      getenvInt("INGEST_PER_MINUTE", defaultIngestPerMinute),
		QuizIPPerDay:         getenvInt("QUIZ_IP_PER_DAY", defaultQuizIPPerDay),
		GuestTTL:             time.Duration(getenvInt("GUEST_TTL_HOURS", int(defaultGuestTTL/time.Hour))) * time.Hour,
		SupabaseURL:          strings.TrimRight(os.Getenv("SUPABASE_URL"), "/"),
		SupabaseServiceKey:   os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),
		SupabaseBucket:       getenv("SUPABASE_BUCKET", defaultSupabaseBucket),
	}
	sameSite, err := parseCookieSameSite(os.Getenv("COOKIE_SAMESITE"))
	if err != nil {
		return config{}, err
	}
	cfg.CookieSameSite = sameSite
	secure, err := parseCookieSecure(os.Getenv("COOKIE_SECURE"))
	if err != nil {
		return config{}, err
	}
	cfg.CookieSecure = secure
	resetHour, err := parseQuizResetHour(os.Getenv("QUIZ_RESET_HOUR"))
	if err != nil {
		return config{}, err
	}
	cfg.QuizResetHour = resetHour
	header, err := parseClientIPHeader(os.Getenv("CLIENT_IP_HEADER"))
	if err != nil {
		return config{}, err
	}
	cfg.ClientIPHeader = header
	if err := validateTrustedProxies(cfg.TrustedProxies); err != nil {
		return config{}, err
	}
	if cfg.DatabaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretBytes {
		return config{}, fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretBytes)
	}
	if corsHasWildcard(cfg.CORSOrigin) {
		return config{}, fmt.Errorf("CORS_ORIGIN must not be a wildcard when credentials are used")
	}
	if cfg.CookieSameSite == http.SameSiteNoneMode && !cfg.CookieSecure {
		return config{}, fmt.Errorf("COOKIE_SAMESITE=None requires COOKIE_SECURE=true")
	}
	if cfg.CookieSameSite == http.SameSiteNoneMode && cfg.CookieSecure && corsHasHTTP(cfg.CORSOrigin) {
		return config{}, fmt.Errorf("CORS_ORIGIN must use https when COOKIE_SAMESITE=None and COOKIE_SECURE=true")
	}
	if (cfg.SupabaseURL == "") != (cfg.SupabaseServiceKey == "") {
		return config{}, fmt.Errorf("SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY must both be set, or both unset")
	}
	if cfg.CookieSameSite == http.SameSiteNoneMode && cfg.CookieSecure && cfg.SupabaseURL == "" {
		return config{}, fmt.Errorf("SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY are required in production (Render disk is ephemeral)")
	}
	if cfg.SupabaseBucket == "" {
		cfg.SupabaseBucket = defaultSupabaseBucket
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
	if cfg.GuestTTL < time.Hour {
		cfg.GuestTTL = defaultGuestTTL
	}
	if cfg.GeminiRetryAttempts < 1 {
		cfg.GeminiRetryAttempts = defaultGeminiAttempts
	}
	if cfg.GeminiRetryAttempts > maxGeminiAttempts {
		cfg.GeminiRetryAttempts = maxGeminiAttempts
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
	if cfg.GeminiRetryBaseDelay < 50*time.Millisecond {
		cfg.GeminiRetryBaseDelay = defaultGeminiRetryBase
	}
	if cfg.GeminiRetryBaseDelay > 5*time.Second {
		cfg.GeminiRetryBaseDelay = 5 * time.Second
	}
	if cfg.GeminiRetryMaxDelay < cfg.GeminiRetryBaseDelay {
		cfg.GeminiRetryMaxDelay = cfg.GeminiRetryBaseDelay
	}
	if cfg.GeminiRetryMaxDelay > 30*time.Second {
		cfg.GeminiRetryMaxDelay = 30 * time.Second
	}
	if cfg.GeminiRequestTimeout < 5*time.Second {
		cfg.GeminiRequestTimeout = defaultGeminiRequestTimeout
	}
	if cfg.GeminiRequestTimeout > cfg.AskTimeout {
		cfg.GeminiRequestTimeout = cfg.AskTimeout
	}
	return cfg, nil
}

func loadGeminiFallbackModel() string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_FALLBACK_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GEMINI_CHAT_FALLBACK_MODEL")); v != "" {
		return v
	}
	return defaultGeminiFallbackModel
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

func corsHasWildcard(csv string) bool {
	for _, o := range splitCSV(csv) {
		if o == "*" {
			return true
		}
	}
	return false
}

func corsHasHTTP(csv string) bool {
	for _, o := range splitCSV(csv) {
		if strings.HasPrefix(strings.ToLower(o), "http://") {
			return true
		}
	}
	return false
}

func parseCookieSameSite(v string) (http.SameSite, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "lax":
		return http.SameSiteLaxMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	default:
		return 0, fmt.Errorf("COOKIE_SAMESITE must be Lax, None, or Strict")
	}
}

func parseCookieSecure(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("COOKIE_SECURE must be true or false")
	}
}

func parseQuizResetHour(v string) (int, error) {
	if strings.TrimSpace(v) == "" {
		return defaultQuizResetHour, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 || n > 23 {
		return 0, fmt.Errorf("QUIZ_RESET_HOUR must be an integer 0-23")
	}
	return n, nil
}

func parseClientIPHeader(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	canon := http.CanonicalHeaderKey(v)
	if strings.EqualFold(canon, "X-Forwarded-For") {
		return "", fmt.Errorf("CLIENT_IP_HEADER must not be X-Forwarded-For; use True-Client-IP on Render")
	}
	return canon, nil
}

func validateTrustedProxies(proxies []string) error {
	for _, p := range proxies {
		pl := strings.ToLower(strings.TrimSpace(p))
		if pl == "0.0.0.0/0" || pl == "::/0" {
			return fmt.Errorf("TRUSTED_PROXIES must not trust all addresses (%s); leave it unset and set CLIENT_IP_HEADER=True-Client-IP on Render", p)
		}
		if !strings.Contains(p, "/") {
			if net.ParseIP(p) == nil {
				return fmt.Errorf("TRUSTED_PROXIES: invalid IP address %q", p)
			}
			continue
		}
		if _, _, err := net.ParseCIDR(p); err != nil {
			return fmt.Errorf("TRUSTED_PROXIES: %w", err)
		}
	}
	return nil
}

func ephemeralStorageWarning(cfg config) string {
	if cfg.SupabaseURL != "" {
		return ""
	}
	return "warning: SUPABASE_URL is unset; uploads use disk " + cfg.StorageDir + " which is ephemeral on Render"
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
