package main

import (
	"bytes"
	"fmt"
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
		text, err := p.GetPlainText(nil)
		if err != nil {
			return nil, fmt.Errorf("pdf page %d: %w", i, err)
		}
		pages = append(pages, pageText{Number: i, Text: normalizePage(text)})
	}
	return pages, nil
}

func normalizePage(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}
