package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemStorageRoundTrip(t *testing.T) {
	s := newMemStorage()
	ctx := context.Background()
	if err := s.Put(ctx, "abc.pdf", bytes.NewReader([]byte("%PDF-1.4"))); err != nil {
		t.Fatal(err)
	}
	rc, err := s.Get(ctx, "abc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "%PDF-1.4" {
		t.Fatalf("got %q", got)
	}
	if err := s.Delete(ctx, "abc.pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "abc.pdf"); err == nil {
		t.Fatal("expected missing after delete")
	}
}

func TestDiskStorageRejectsTraversal(t *testing.T) {
	d := newDiskStorage(t.TempDir())
	err := d.Put(context.Background(), "../etc/passwd", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Fatal("expected traversal to fail")
	}
}

func TestDiskStorageRoundTrip(t *testing.T) {
	d := newDiskStorage(t.TempDir())
	ctx := context.Background()
	if err := d.Put(ctx, "doc/id.pdf", bytes.NewReader([]byte("hi"))); err != nil {
		t.Fatal(err)
	}
	rc, err := d.Get(ctx, "doc/id.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestNewStorageUsesDiskWithoutSupabase(t *testing.T) {
	got := newStorage(config{StorageDir: t.TempDir()})
	if _, ok := got.(*diskStorage); !ok {
		t.Fatalf("got %T, want *diskStorage", got)
	}
}

func TestSupabaseStorageRoundTrip(t *testing.T) {
	objects := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-service-role" {
			t.Errorf("authorization=%q", got)
		}
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/doctalk-uploads/"):
			b, _ := io.ReadAll(r.Body)
			objects[r.URL.Path] = b
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet:
			b, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(b)
		case r.Method == http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	s := newSupabaseStorage(config{
		SupabaseURL:        srv.URL,
		SupabaseServiceKey: "test-service-role",
		SupabaseBucket:     defaultSupabaseBucket,
	}, srv.Client())
	ctx := context.Background()
	if err := s.Put(ctx, "abc.pdf", bytes.NewReader([]byte("%PDF-1.4"))); err != nil {
		t.Fatal(err)
	}
	rc, err := s.Get(ctx, "abc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "%PDF-1.4" {
		t.Fatalf("got %q", got)
	}
	if err := s.Delete(ctx, "abc.pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "abc.pdf"); err == nil {
		t.Fatal("expected missing after delete")
	}
}
