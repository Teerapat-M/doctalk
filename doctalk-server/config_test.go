package main

import "testing"

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
