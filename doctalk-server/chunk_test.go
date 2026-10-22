package main

import (
	"strings"
	"testing"
)

func TestChunkPagesKeepsPageNumber(t *testing.T) {
	pages := []pageText{
		{Number: 1, Text: "hello from page one"},
		{Number: 2, Text: "สวัสดีจากหน้าที่สอง"},
	}
	chunks := chunkPages(pages)
	if len(chunks) != 2 {
		t.Fatalf("len=%d want 2", len(chunks))
	}
	if chunks[0].Page != 1 || chunks[1].Page != 2 {
		t.Fatalf("pages = %d, %d", chunks[0].Page, chunks[1].Page)
	}
	if chunks[0].Index != 0 || chunks[1].Index != 1 {
		t.Fatalf("indexes = %d, %d", chunks[0].Index, chunks[1].Index)
	}
}

func TestThaiRuneWindowSplitsLongUnspacedText(t *testing.T) {
	// Thai has no spaces; a single page longer than the window must still split.
	text := strings.Repeat("ก", chunkRunes+50)
	chunks := chunkPages([]pageText{{Number: 4, Text: text}})
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.Page != 4 {
			t.Fatalf("page = %d, want 4", c.Page)
		}
		if c.Text == "" {
			t.Fatal("empty chunk")
		}
	}
	// Overlap: the start of chunk 1 should appear in chunk 0.
	first := []rune(chunks[0].Text)
	second := []rune(chunks[1].Text)
	if len(first) < chunkOverlap || len(second) == 0 {
		return
	}
	overlap := string(first[len(first)-chunkOverlap:])
	if !strings.Contains(chunks[1].Text, overlap[:20]) {
		t.Fatalf("expected overlap between consecutive chunks")
	}
}

func TestChunkSkipsEmptyPages(t *testing.T) {
	chunks := chunkPages([]pageText{
		{Number: 1, Text: "   "},
		{Number: 2, Text: "ok"},
	})
	if len(chunks) != 1 || chunks[0].Page != 2 {
		t.Fatalf("got %+v", chunks)
	}
}
