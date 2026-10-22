package main

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	"github.com/dslipak/pdf"
)

var pdfMagic = []byte("%PDF-")

type pageText struct {
	Number int
	Text   string
}

type Extractor interface {
	ExtractPages(data []byte) ([]pageText, error)
}

type pdfExtractor struct{}

func isPDF(data []byte) bool {
	if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}
	return bytes.HasPrefix(data, pdfMagic)
}

func (pdfExtractor) ExtractPages(data []byte) ([]pageText, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	n := r.NumPage()
	pages := make([]pageText, 0, n)
	for i := 1; i <= n; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := extractPage(p)
		if err != nil {
			return nil, fmt.Errorf("pdf page %d: %w", i, err)
		}
		pages = append(pages, pageText{Number: i, Text: normalizePage(text)})
	}
	return pages, nil
}

func extractPage(p pdf.Page) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("%v", r)
		}
	}()
	// GetPlainText feeds T* through ToUnicode, so byte 0x0A becomes ป
	// (U+0E1B) in this Sarabun subset. Content() keeps the line break as Y.
	return joinTexts(p.Content().Text), nil
}

// Same-line glyphs can jitter by a fraction of a point; reportlab lines here are 22pt apart.
const lineYTol = 1.0

func joinTexts(texts []pdf.Text) string {
	var b strings.Builder
	var lastY float64
	started := false
	for _, t := range texts {
		if t.S == "" {
			continue
		}
		if started && math.Abs(t.Y-lastY) > lineYTol {
			b.WriteByte('\n')
		}
		b.WriteString(t.S)
		lastY = t.Y
		started = true
	}
	return b.String()
}

func normalizePage(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}
