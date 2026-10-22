package main

import (
	"strings"
	"unicode/utf8"
)

const (
	foundMarker    = "FOUND"
	notFoundMarker = "NOT_FOUND"
)

// answerGate strips the first-line FOUND / NOT_FOUND signal from a Gemini
// stream so the client only sees the user-facing answer. Missing markers are
// treated as found (keep citations).
type answerGate struct {
	buf      strings.Builder
	decided  bool
	notFound bool
}

func (g *answerGate) push(delta string, emit func(string) error) error {
	if g.decided {
		if delta == "" {
			return nil
		}
		return emit(delta)
	}
	g.buf.WriteString(delta)
	s := g.buf.String()
	line, rest, ok := strings.Cut(s, "\n")
	if !ok {
		if utf8.RuneCountInString(s) > 32 {
			g.decided = true
			return emit(s)
		}
		return nil
	}
	return g.finish(strings.TrimSpace(line), rest, emit)
}

func (g *answerGate) flush(emit func(string) error) error {
	if g.decided {
		return nil
	}
	s := g.buf.String()
	line, rest, ok := strings.Cut(s, "\n")
	if ok {
		return g.finish(strings.TrimSpace(line), rest, emit)
	}
	head := strings.TrimSpace(s)
	if head == notFoundMarker {
		return g.finish(notFoundMarker, "", emit)
	}
	g.decided = true
	if s == "" {
		return nil
	}
	return emit(s)
}

func (g *answerGate) finish(head, rest string, emit func(string) error) error {
	g.decided = true
	switch head {
	case notFoundMarker:
		g.notFound = true
	case foundMarker:
		g.notFound = false
	default:
		g.notFound = false
		whole := head
		if rest != "" {
			whole = head + "\n" + rest
		}
		if whole == "" {
			return nil
		}
		return emit(whole)
	}
	if rest == "" {
		return nil
	}
	return emit(rest)
}
