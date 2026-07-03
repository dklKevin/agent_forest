// Package goldentest is the shared golden-file plumbing for the render/art
// tests. One -update flag rewrites the goldens (so an intentional art change
// lands as a reviewable diff), and a failing comparison reports the first
// differing line so a reviewer sees exactly which piece of the ASCII moved.
//
// Goldens are the exact --plain (shape-only) frame, compared as raw bytes:
// the braille glyphs are multi-byte UTF-8, so nothing is normalized.
package goldentest

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Update, set by `go test -update`, rewrites golden files instead of asserting
// against them. It is defined here once so every render/art test package shares
// a single flag.
var Update = flag.Bool("update", false, "rewrite golden files")

// Assert compares got against testdata/<name>.golden under the calling
// package's directory. With -update it (re)writes the file and returns; without
// it, a mismatch fails the test with the first differing line.
func Assert(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *Update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("goldentest: mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("goldentest: write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("goldentest: missing golden %s (run: go test ./internal/gallery ./internal/render -update): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s changed:\n%s", name, FirstDiff(string(want), got))
	}
}

// FirstDiff returns a short description of the first line at which want and got
// diverge, one-indexed to match an editor's gutter. Identical strings return
// "identical".
func FirstDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := len(wl)
	if len(gl) < n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			return fmt.Sprintf("line %d:\n  want: %q\n  got:  %q", i+1, wl[i], gl[i])
		}
	}
	if len(wl) != len(gl) {
		return fmt.Sprintf("line count differs: want %d lines, got %d", len(wl), len(gl))
	}
	return "identical"
}
