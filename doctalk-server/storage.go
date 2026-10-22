package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Storage persists uploaded PDFs.
// DiskStorage is for local dev. A Supabase Storage implementation can
// satisfy this same interface at deploy time (Phase 6) without changing
// the upload pipeline.
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

func sanitizeKey(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid storage key")
	}
	cleaned := path.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
	if cleaned == "/" || strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid storage key")
	}
	return strings.TrimPrefix(cleaned, "/"), nil
}

type diskStorage struct {
	root string
}

func newStorage(cfg config) Storage {
	if cfg.SupabaseURL != "" && cfg.SupabaseServiceKey != "" {
		return newSupabaseStorage(cfg, nil)
	}
	return newDiskStorage(cfg.StorageDir)
}

func newDiskStorage(root string) *diskStorage {
	return &diskStorage{root: root}
}

func (d *diskStorage) resolve(key string) (string, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return "", err
	}
	full := filepath.Join(d.root, filepath.FromSlash(key))
	rel, err := filepath.Rel(d.root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid storage key")
	}
	return full, nil
}

func (d *diskStorage) Put(_ context.Context, key string, r io.Reader) error {
	full, err := d.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.Create(full)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (d *diskStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	full, err := d.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

func (d *diskStorage) Delete(_ context.Context, key string) error {
	full, err := d.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(full)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type memStorage struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStorage() *memStorage {
	return &memStorage{data: make(map[string][]byte)}
}

func (m *memStorage) Put(_ context.Context, key string, r io.Reader) error {
	key, err := sanitizeKey(key)
	if err != nil {
		return err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.data[key] = b
	m.mu.Unlock()
	return nil
}

func (m *memStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	b, ok := m.data[key]
	m.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	key, err := sanitizeKey(key)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()
	return nil
}

type supabaseStorage struct {
	base   string
	bucket string
	key    string
	client *http.Client
}

func newSupabaseStorage(cfg config, client *http.Client) *supabaseStorage {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &supabaseStorage{
		base:   strings.TrimRight(cfg.SupabaseURL, "/"),
		bucket: cfg.SupabaseBucket,
		key:    cfg.SupabaseServiceKey,
		client: client,
	}
}

func (s *supabaseStorage) objectURL(key string) string {
	return s.base + "/storage/v1/object/" + url.PathEscape(s.bucket) + "/" + url.PathEscape(key)
}

func (s *supabaseStorage) do(ctx context.Context, method, key string, body io.Reader, upsert bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.objectURL(key), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("apikey", s.key)
	if upsert {
		req.Header.Set("x-upsert", "true")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return s.client.Do(req)
}

func (s *supabaseStorage) Put(ctx context.Context, key string, r io.Reader) error {
	key, err := sanitizeKey(key)
	if err != nil {
		return err
	}
	res, err := s.do(ctx, http.MethodPost, key, r, true)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("supabase storage put: HTTP %d %s", res.StatusCode, bytes.TrimSpace(b))
	}
	return nil
}

func (s *supabaseStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	key, err := sanitizeKey(key)
	if err != nil {
		return nil, err
	}
	res, err := s.do(ctx, http.MethodGet, key, nil, false)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return nil, os.ErrNotExist
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		res.Body.Close()
		return nil, fmt.Errorf("supabase storage get: HTTP %d %s", res.StatusCode, bytes.TrimSpace(b))
	}
	return res.Body, nil
}

func (s *supabaseStorage) Delete(ctx context.Context, key string) error {
	key, err := sanitizeKey(key)
	if err != nil {
		return err
	}
	res, err := s.do(ctx, http.MethodDelete, key, nil, false)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("supabase storage delete: HTTP %d %s", res.StatusCode, bytes.TrimSpace(b))
	}
	return nil
}
