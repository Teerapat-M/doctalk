package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dslipak/pdf"
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

func TestJoinTextsBreaksOnYNotOnSameLine(t *testing.T) {
	got := joinTexts([]pdf.Text{
		{S: "ใต้", Y: 800},
		{S: "กรุง", Y: 778},
	})
	if got != "ใต้\nกรุง" {
		t.Fatalf("got %q", got)
	}
	same := joinTexts([]pdf.Text{
		{S: "doc", Y: 800},
		{S: "uments.", Y: 800.2},
		{S: "Answers", Y: 778},
	})
	if same != "documents.\nAnswers" {
		t.Fatalf("got %q", same)
	}
}

func TestFixturePDFsExtract(t *testing.T) {
	want := map[string][]string{
		"testdata/english.pdf": {
			"Page one: DocTalk lets users ask questions about PDF documents.\nAnswers include citations with page numbers.",
			"Page two: Embeddings are stored in Postgres using pgvector.\nThe Gemini embedding model returns 768 dimensional vectors.",
		},
		"testdata/thai.pdf": {
			"หน้าที่หนึ่ง: ประเทศไทยตั้งอยู่ในเอเชียตะวันออกเฉียงใต้\nกรุงเทพมหานครเป็นเมืองหลวงของประเทศไทย",
			"หน้าที่สอง: อาหารไทยมีรสชาติเผ็ดเปรี้ยวหวานเค็ม\nต้มยำกุ้งเป็นอาหารที่มีชื่อเสียงไปทั่วโลก",
		},
	}
	for path, pagesWant := range want {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !isPDF(b) {
			t.Fatalf("%s not sniffed as PDF", path)
		}
		pages, err := pdfExtractor{}.ExtractPages(b)
		if err != nil {
			t.Fatalf("%s extract: %v", path, err)
		}
		if len(pages) != len(pagesWant) {
			t.Fatalf("%s pages=%d want %d", path, len(pages), len(pagesWant))
		}
		for i, p := range pages {
			if p.Number != i+1 {
				t.Fatalf("%s page %d number=%d", path, i, p.Number)
			}
			if strings.TrimSpace(p.Text) == "" {
				t.Fatalf("%s page %d extracted empty text", path, p.Number)
			}
			if p.Text != pagesWant[i] {
				t.Fatalf("%s page %d text=%q want %q", path, p.Number, p.Text, pagesWant[i])
			}
		}
	}
}

func TestThaiFixtureNoStrayPoPlaAtLineJoin(t *testing.T) {
	b, err := os.ReadFile("testdata/thai.pdf")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := pdfExtractor{}.ExtractPages(b)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(pageTexts(pages), "\n")
	for _, bad := range []string{"ใต้ปกรุง", "เค็มปต้ม"} {
		if strings.Contains(all, bad) {
			t.Fatalf("stray ป at line join: found %q in %q", bad, all)
		}
	}
	if strings.Contains(all, "ไทยป") {
		t.Fatalf("trailing T*-encoded ป in %q", all)
	}
}

func TestEnglishFixtureKeepsLineBreaks(t *testing.T) {
	b, err := os.ReadFile("testdata/english.pdf")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := pdfExtractor{}.ExtractPages(b)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(pageTexts(pages), "\n")
	for _, bad := range []string{"documents.Answers", "pgvector.The"} {
		if strings.Contains(all, bad) {
			t.Fatalf("lines concatenated without break: found %q in %q", bad, all)
		}
	}
}

func pageTexts(pages []pageText) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.Text
	}
	return out
}
