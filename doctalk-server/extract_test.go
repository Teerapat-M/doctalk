package main

import "testing"

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
