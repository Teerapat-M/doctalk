package main

import (
	"strings"
	"testing"
)

func TestAnswerGateStripsNotFoundMarker(t *testing.T) {
	var g answerGate
	var got strings.Builder
	emit := func(s string) error { got.WriteString(s); return nil }
	if err := g.push("NOT_FOUND\nThe answer is not found in the document.", emit); err != nil {
		t.Fatal(err)
	}
	if err := g.flush(emit); err != nil {
		t.Fatal(err)
	}
	if !g.notFound {
		t.Fatal("expected notFound")
	}
	if strings.Contains(got.String(), "NOT_FOUND") {
		t.Fatalf("marker leaked: %q", got.String())
	}
	if !strings.Contains(got.String(), "not found") {
		t.Fatalf("got %q", got.String())
	}
}

func TestAnswerGateMissingMarkerIsFound(t *testing.T) {
	var g answerGate
	var got strings.Builder
	emit := func(s string) error { got.WriteString(s); return nil }
	_ = g.push("The vectors are 768-d.", emit)
	_ = g.flush(emit)
	if g.notFound {
		t.Fatal("missing marker should be treated as found")
	}
	if got.String() != "The vectors are 768-d." {
		t.Fatalf("got %q", got.String())
	}
}
