package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
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
