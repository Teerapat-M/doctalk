package main

import (
	"testing"
	"time"
)

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
	t.Setenv("GEMINI_CHAT_FALLBACK_MODEL", "")
	t.Setenv("GEMINI_MAX_IN_FLIGHT", "")
	t.Setenv("GEMINI_RETRY_BUDGET_SECONDS", "")
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != defaultAPIPort {
		t.Fatalf("Port = %q, want %s", cfg.Port, defaultAPIPort)
	}
	if cfg.CORSOrigin != "*" {
		t.Fatalf("CORSOrigin = %q, want *", cfg.CORSOrigin)
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
	if cfg.GeminiChatFallbackModel != "" {
		t.Fatalf("fallback should be unset, got %q", cfg.GeminiChatFallbackModel)
	}
	if cfg.GeminiMaxInFlight != defaultGeminiMaxInFlight {
		t.Fatalf("in-flight=%d", cfg.GeminiMaxInFlight)
	}
	if cfg.Port != "8181" {
		t.Fatalf("default port=%q", cfg.Port)
	}
}

func TestLoadConfigGeminiBaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5435/doctalk?sslmode=disable")
	t.Setenv("GEMINI_BASE_URL", "https://example.invalid/gemini/")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GeminiBaseURL != "https://example.invalid/gemini" {
		t.Fatalf("GeminiBaseURL = %q", cfg.GeminiBaseURL)
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
