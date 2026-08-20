package main

import (
	"strings"
	"testing"
)

func TestToonFieldEscapesControlRunes(t *testing.T) {
	if got := toonField("ok"); got != "ok" {
		t.Fatalf("plain name quoted: %q", got)
	}
	if got := toonField("has space"); got != `"has space"` {
		t.Fatalf("space not quoted: %q", got)
	}
	got := toonField("bad\nname\x1b[31m")
	if !strings.HasPrefix(got, `"`) || strings.Contains(got, "\n") || strings.Contains(got, "\x1b") {
		t.Fatalf("control runes not escaped: %q", got)
	}
}
