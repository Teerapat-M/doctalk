package main

import "testing"

func TestLoadConfigRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PORT", "9090")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://doctalk:doctalk@localhost:5432/doctalk?sslmode=disable")
	t.Setenv("PORT", "")
	t.Setenv("CORS_ORIGIN", "")
	t.Setenv("GEMINI_EMBEDDING_MODEL", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.CORSOrigin != "*" {
		t.Fatalf("CORSOrigin = %q, want *", cfg.CORSOrigin)
	}
	if cfg.GeminiEmbeddingModel != "gemini-embedding-001" {
		t.Fatalf("GeminiEmbeddingModel = %q", cfg.GeminiEmbeddingModel)
	}
}
