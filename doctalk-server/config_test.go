package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const testJWTSecret = "ci-dummy-jwt-secret-not-for-prod"

func TestLoadConfigRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PORT", "9090")
	t.Setenv("API_PORT", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("PORT", "")
	t.Setenv("API_PORT", "")
	t.Setenv("CORS_ORIGIN", "")
	t.Setenv("GEMINI_EMBEDDING_MODEL", "")
	t.Setenv("MAX_UPLOAD_BYTES", "")
	t.Setenv("STORAGE_DIR", "")
	t.Setenv("GEMINI_BASE_URL", "")
	t.Setenv("GEMINI_CHAT_MODEL", "")
	t.Setenv("ASK_TOP_K", "")
	t.Setenv("ASK_TOP_K_MAX", "")
	t.Setenv("ASK_TIMEOUT_SECONDS", "")
	t.Setenv("GEMINI_QUIZ_MODEL", "")
	t.Setenv("QUIZ_N_MAX", "")
	t.Setenv("GEMINI_CHAT_FALLBACK_MODEL", "")
	t.Setenv("GEMINI_FALLBACK_MODEL", "")
	t.Setenv("GEMINI_MAX_IN_FLIGHT", "")
	t.Setenv("GEMINI_RETRY_BUDGET_SECONDS", "")
	t.Setenv("GEMINI_RETRY_ATTEMPTS", "")
	t.Setenv("GEMINI_RETRY_BASE_DELAY_MS", "")
	t.Setenv("GEMINI_RETRY_MAX_DELAY_MS", "")
	t.Setenv("GEMINI_REQUEST_TIMEOUT_SECONDS", "")
	t.Setenv("GEMINI_TOTAL_TIMEOUT_SECONDS", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("CLIENT_IP_HEADER", "")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("COOKIE_SAMESITE", "")
	t.Setenv("QUIZ_PER_DAY", "")
	t.Setenv("ASK_PER_MINUTE", "")
	t.Setenv("INGEST_PER_MINUTE", "")
	t.Setenv("QUIZ_RESET_HOUR", "")
	t.Setenv("GUEST_TTL_HOURS", "")
	t.Setenv("MAX_PAGES", "")
	t.Setenv("MAX_ROWS", "")
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "")
	t.Setenv("SUPABASE_BUCKET", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != defaultAPIPort {
		t.Fatalf("Port = %q, want %s", cfg.Port, defaultAPIPort)
	}
	if cfg.CORSOrigin != defaultCORSOrigin {
		t.Fatalf("CORSOrigin = %q, want %s", cfg.CORSOrigin, defaultCORSOrigin)
	}
	if cfg.GeminiEmbeddingModel != defaultEmbedModel {
		t.Fatalf("GeminiEmbeddingModel = %q", cfg.GeminiEmbeddingModel)
	}
	if cfg.MaxUploadBytes != defaultMaxUpload {
		t.Fatalf("MaxUploadBytes = %d, want %d", cfg.MaxUploadBytes, defaultMaxUpload)
	}
	if cfg.StorageDir != defaultStorageDir {
		t.Fatalf("StorageDir = %q", cfg.StorageDir)
	}
	if cfg.GeminiBaseURL != defaultGeminiBaseURL {
		t.Fatalf("GeminiBaseURL = %q", cfg.GeminiBaseURL)
	}
	if cfg.GeminiChatModel != defaultChatModel {
		t.Fatalf("GeminiChatModel = %q", cfg.GeminiChatModel)
	}
	if cfg.AskTopK != defaultAskTopK || cfg.AskTopKMax != defaultAskTopKMax {
		t.Fatalf("k=%d max=%d", cfg.AskTopK, cfg.AskTopKMax)
	}
	if cfg.AskTimeout != time.Duration(defaultAskTimeoutSec)*time.Second {
		t.Fatalf("AskTimeout=%s", cfg.AskTimeout)
	}
	if cfg.GeminiQuizModel != cfg.GeminiChatModel {
		t.Fatalf("GeminiQuizModel=%q want chat default", cfg.GeminiQuizModel)
	}
	if cfg.QuizNMax != defaultQuizNMax {
		t.Fatalf("QuizNMax=%d", cfg.QuizNMax)
	}
	if cfg.GeminiFallbackModel != defaultGeminiFallbackModel {
		t.Fatalf("fallback=%q want %q", cfg.GeminiFallbackModel, defaultGeminiFallbackModel)
	}
	if cfg.GeminiRetryAttempts != defaultGeminiAttempts {
		t.Fatalf("retry attempts=%d", cfg.GeminiRetryAttempts)
	}
	if cfg.GeminiRetryBaseDelay != defaultGeminiRetryBase || cfg.GeminiRetryMaxDelay != defaultGeminiRetryMaxDelay {
		t.Fatalf("retry delays base=%s max=%s", cfg.GeminiRetryBaseDelay, cfg.GeminiRetryMaxDelay)
	}
	if cfg.GeminiRequestTimeout != defaultGeminiRequestTimeout {
		t.Fatalf("request timeout=%s", cfg.GeminiRequestTimeout)
	}
	if cfg.GeminiTotalTimeout != defaultGeminiTotalTimeout {
		t.Fatalf("total timeout=%s", cfg.GeminiTotalTimeout)
	}
	if cfg.GeminiMaxInFlight != defaultGeminiMaxInFlight {
		t.Fatalf("in-flight=%d", cfg.GeminiMaxInFlight)
	}
	if cfg.Port != "8181" {
		t.Fatalf("default port=%q", cfg.Port)
	}
	if cfg.JWTSecret != testJWTSecret {
		t.Fatalf("JWTSecret = %q", cfg.JWTSecret)
	}
	if cfg.CookieSecure {
		t.Fatal("CookieSecure should default false")
	}
	if cfg.CookieSameSite != http.SameSiteLaxMode {
		t.Fatalf("CookieSameSite=%v want Lax", cfg.CookieSameSite)
	}
	if cfg.QuizPerDay != defaultQuizPerDay || cfg.AskPerMinute != defaultAskPerMinute || cfg.IngestPerMinute != defaultIngestPerMinute {
		t.Fatalf("limits quiz=%d ask=%d ingest=%d", cfg.QuizPerDay, cfg.AskPerMinute, cfg.IngestPerMinute)
	}
	if cfg.QuizResetHour != defaultQuizResetHour {
		t.Fatalf("QuizResetHour=%d", cfg.QuizResetHour)
	}
	if cfg.GuestTTL != defaultGuestTTL {
		t.Fatalf("GuestTTL=%s", cfg.GuestTTL)
	}
	if cfg.MaxPages != defaultMaxPages || cfg.MaxRows != defaultMaxRows {
		t.Fatalf("caps pages=%d rows=%d", cfg.MaxPages, cfg.MaxRows)
	}
	if !strings.Contains(cfg.CORSOrigin, "3917") {
		t.Fatalf("CORSOrigin=%q", cfg.CORSOrigin)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("TrustedProxies=%v want empty (do not trust X-Forwarded-For)", cfg.TrustedProxies)
	}
	if cfg.ClientIPHeader != "" {
		t.Fatalf("ClientIPHeader=%q", cfg.ClientIPHeader)
	}
}

func TestLoadConfigRequiresJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when JWT_SECRET is empty")
	}
	t.Setenv("JWT_SECRET", "too-short")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when JWT_SECRET is shorter than 32 bytes")
	}
}

func TestLoadConfigCookieNoneRequiresSecure(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("COOKIE_SAMESITE", "None")
	t.Setenv("COOKIE_SECURE", "false")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when SameSite=None without Secure")
	}
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("CORS_ORIGIN", "https://doctalk.vercel.app")
	t.Setenv("SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "service-role-not-a-real-key")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CookieSameSite != http.SameSiteNoneMode || !cfg.CookieSecure {
		t.Fatalf("SameSite=%v secure=%v", cfg.CookieSameSite, cfg.CookieSecure)
	}
}

func TestLoadConfigRejectsWildcardCORS(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("CORS_ORIGIN", "*")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for wildcard CORS with credentials")
	}
	t.Setenv("CORS_ORIGIN", "https://doctalk.vercel.app,*")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when CORS list includes wildcard")
	}
}

func TestLoadConfigSupabaseKeysTogether(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when only SUPABASE_URL is set")
	}
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "service-role-not-a-real-key")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SupabaseURL != "https://example.supabase.co" {
		t.Fatalf("SupabaseURL=%q", cfg.SupabaseURL)
	}
	if cfg.SupabaseBucket != defaultSupabaseBucket {
		t.Fatalf("bucket=%q", cfg.SupabaseBucket)
	}
}

func TestLoadConfigRejectsBadCookieSameSite(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("COOKIE_SAMESITE", "bogus")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for invalid COOKIE_SAMESITE")
	}
}

func TestLoadConfigRejectsUnparseableCookieSecure(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("COOKIE_SECURE", "not-a-bool")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for unparseable COOKIE_SECURE")
	}
}

func TestLoadConfigRejectsBadQuizResetHour(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("QUIZ_RESET_HOUR", "24")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for QUIZ_RESET_HOUR=24")
	}
	t.Setenv("QUIZ_RESET_HOUR", "-1")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for QUIZ_RESET_HOUR=-1")
	}
	t.Setenv("QUIZ_RESET_HOUR", "noon")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for unparseable QUIZ_RESET_HOUR")
	}
}

func TestLoadConfigRejectsMalformedTrustedProxies(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("TRUSTED_PROXIES", "not-a-cidr")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for malformed TRUSTED_PROXIES")
	}
	t.Setenv("TRUSTED_PROXIES", "0.0.0.0/0")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for TRUSTED_PROXIES=0.0.0.0/0")
	}
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("CLIENT_IP_HEADER", "X-Forwarded-For")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for CLIENT_IP_HEADER=X-Forwarded-For")
	}
}

func TestLoadConfigRejectsHTTPOriginWithNoneSecure(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("COOKIE_SAMESITE", "None")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("CORS_ORIGIN", "http://doctalk.vercel.app")
	t.Setenv("SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "service-role-not-a-real-key")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for http CORS origin with SameSite=None")
	}
}

func TestLoadConfigProdRequiresSupabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("COOKIE_SAMESITE", "None")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("CORS_ORIGIN", "https://doctalk.vercel.app")
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when production cookies lack Supabase")
	}
}

func TestEphemeralStorageWarning(t *testing.T) {
	if got := ephemeralStorageWarning(config{StorageDir: "/data/uploads"}); got == "" {
		t.Fatal("expected warning when Supabase is unset")
	}
	if got := ephemeralStorageWarning(config{SupabaseURL: "https://example.supabase.co", StorageDir: "/data/uploads"}); got != "" {
		t.Fatalf("unexpected warning %q", got)
	}
}

func TestLoadConfigQuizModelAndCap(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("GEMINI_CHAT_MODEL", "gemini-3.8-flash")
	t.Setenv("GEMINI_QUIZ_MODEL", "gemini-quiz")
	t.Setenv("QUIZ_N_MAX", "99")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GeminiQuizModel != "gemini-quiz" {
		t.Fatalf("GeminiQuizModel=%q", cfg.GeminiQuizModel)
	}
	if cfg.QuizNMax != 50 {
		t.Fatalf("QuizNMax cap=%d", cfg.QuizNMax)
	}
}

func TestLoadConfigGeminiBaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("GEMINI_BASE_URL", "https://example.invalid/gemini/")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GeminiBaseURL != "https://example.invalid/gemini" {
		t.Fatalf("GeminiBaseURL = %q", cfg.GeminiBaseURL)
	}
}

func TestLoadConfigFallbackAliasAndBounds(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("GEMINI_FALLBACK_MODEL", "")
	t.Setenv("GEMINI_CHAT_FALLBACK_MODEL", "alias-flash")
	t.Setenv("GEMINI_RETRY_ATTEMPTS", "99")
	t.Setenv("GEMINI_RETRY_BASE_DELAY_MS", "10")
	t.Setenv("GEMINI_RETRY_MAX_DELAY_MS", "100")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GeminiFallbackModel != "alias-flash" {
		t.Fatalf("fallback alias=%q", cfg.GeminiFallbackModel)
	}
	if cfg.GeminiRetryAttempts != maxGeminiAttempts {
		t.Fatalf("attempts cap=%d", cfg.GeminiRetryAttempts)
	}
	if cfg.GeminiRetryBaseDelay != defaultGeminiRetryBase {
		t.Fatalf("base delay=%s", cfg.GeminiRetryBaseDelay)
	}

	t.Setenv("GEMINI_FALLBACK_MODEL", "gemini-2.5-flash-lite")
	t.Setenv("GEMINI_CHAT_FALLBACK_MODEL", "ignored-alias")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GeminiFallbackModel != "gemini-2.5-flash-lite" {
		t.Fatalf("GEMINI_FALLBACK_MODEL should win, got %q", cfg.GeminiFallbackModel)
	}
}

func TestLoadConfigRequestTimeoutBelow5FailsFast(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("GEMINI_REQUEST_TIMEOUT_SECONDS", "1")
	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for GEMINI_REQUEST_TIMEOUT_SECONDS=1")
	}
	if !strings.Contains(err.Error(), "GEMINI_REQUEST_TIMEOUT_SECONDS") || !strings.Contains(err.Error(), "at least 5") {
		t.Fatalf("got %v", err)
	}
}

func TestListenPortPrefersAPIPortOverPORT(t *testing.T) {
	t.Setenv("API_PORT", "8081")
	t.Setenv("PORT", "10000")
	if got := listenPort(); got != "8081" {
		t.Fatalf("listenPort = %q, want 8081", got)
	}
}

func TestListenPortFallsBackToPORT(t *testing.T) {
	t.Setenv("API_PORT", "")
	t.Setenv("PORT", "10000")
	if got := listenPort(); got != "10000" {
		t.Fatalf("listenPort = %q, want 10000", got)
	}
}
