package main

import (
	"os"
	"testing"
)

func TestIsPDFSniffsMagicNotExtension(t *testing.T) {
	if !isPDF([]byte("%PDF-1.4\n%rest")) {
		t.Fatal("PDF magic should pass")
	}
	if !isPDF(append([]byte{0xEF, 0xBB, 0xBF}, []byte("%PDF-1.7")...)) {
		t.Fatal("PDF with UTF-8 BOM should pass")
	}
	if isPDF([]byte{0xFF, 0xD8, 0xFF}) {
		t.Fatal("JPEG magic should fail")
	}
	if isPDF([]byte("this is named.pdf but is text")) {
		t.Fatal("extension is not a sniff")
	}
	if isPDF([]byte("%PD")) {
		t.Fatal("truncated header should fail")
	}
}

func TestFixturePDFsExtract(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"testdata/english.pdf", "Hello from DocTalk."},
		{"testdata/thai.pdf", "สวัสดี DocTalk"},
	} {
		b, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if !isPDF(b) {
			t.Fatalf("%s not sniffed as PDF", tc.path)
		}
		pages, err := pdfExtractor{}.ExtractPages(b)
		if err != nil {
			t.Fatalf("%s extract: %v", tc.path, err)
		}
		if len(pages) != 1 || pages[0].Number != 1 {
			t.Fatalf("%s pages=%+v", tc.path, pages)
		}
		if pages[0].Text != tc.want {
			t.Fatalf("%s text=%q want %q", tc.path, pages[0].Text, tc.want)
		}
	}
}
