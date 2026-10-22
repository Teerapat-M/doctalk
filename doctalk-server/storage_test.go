package main

import (
	"bytes"
	"context"
	"io"
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
