package main

import "unicode"

// Thai has no spaces between words. We do not use a dictionary segmenter
// (those usually need ICU/CGO). Instead each page is split into a rune
// window with overlap:
//
//	window  = 700 runes (~a short paragraph of Thai or mixed text)
//	overlap = 120 runes
//
// If a newline or (for mixed English) a space sits in the last 80 runes of
// the window, we break there so chunks prefer paragraph/word boundaries.
// Mid-word cuts can still happen for long Thai runs — ponytail: upgrade to
// ICU/wordcut if citation quality suffers.
//
// Every chunk keeps the 1-based PDF page number for Phase 3 citations.
const (
	chunkRunes    = 700
	chunkOverlap  = 120
	chunkLookback = 80
)

type Chunk struct {
	Index       int
	Page        int
	SheetName   string
	RowNumber   int
	Text        string
	Question    string
	Answer      string
	Explanation string
	ChoiceA     string
	ChoiceB     string
	ChoiceC     string
	ChoiceD     string
}

func chunkPages(pages []pageText) []Chunk {
	out := make([]Chunk, 0)
	idx := 0
	for _, p := range pages {
		for _, text := range chunkRunesWindow(p.Text) {
			out = append(out, Chunk{Index: idx, Page: p.Number, Text: text})
			idx++
		}
	}
	return out
}

func chunkRunesWindow(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) <= chunkRunes {
		if trimmed := trimChunk(string(runes)); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	}

	out := make([]string, 0, 1+len(runes)/chunkRunes)
	start := 0
	for start < len(runes) {
		end := start + chunkRunes
		if end >= len(runes) {
			piece := string(runes[start:])
			if trimmed := trimChunk(piece); trimmed != "" {
				out = append(out, trimmed)
			}
			break
		}
		cut := end
		window := runes[start:end]
		if br := lastBreak(window); br >= 0 {
			cut = start + br + 1
			if cut <= start {
				cut = end
			}
		}
		piece := string(runes[start:cut])
		if trimmed := trimChunk(piece); trimmed != "" {
			out = append(out, trimmed)
		}
		next := cut - chunkOverlap
		if next <= start {
			next = cut
		}
		start = next
	}
	return out
}

func lastBreak(window []rune) int {
	from := len(window) - 1
	to := len(window) - chunkLookback
	if to < 0 {
		to = 0
	}
	for i := from; i >= to; i-- {
		if window[i] == '\n' || unicode.IsSpace(window[i]) {
			return i
		}
	}
	return -1
}

func trimChunk(s string) string {
	return normalizePage(s)
}
