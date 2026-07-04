package guidebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The extraction skips the usual README preamble noise - title, badges,
// images, raw HTML, rules - and lands on the first real sentence.
func TestIntroSkipsBadgeNoise(t *testing.T) {
	md := strings.Join([]string{
		"# keepsake",
		"",
		"[![Build](https://img.shields.io/badge/build-passing-green)](https://ci.example)",
		"![logo](assets/logo.png)",
		"<p align=\"center\"><img src=\"banner.png\"></p>",
		"<!-- a comment -->",
		"---",
		"",
		"A tiny keepsake forest for your terminal.",
		"It grows from your own repositories.",
		"",
		"## Install",
	}, "\n")
	got := intro(md)
	want := "A tiny keepsake forest for your terminal. It grows from your own repositories."
	if got != want {
		t.Fatalf("intro = %q, want %q", got, want)
	}
}

// Inline markdown falls away but the words stay: links keep their text,
// emphasis and code ticks vanish.
func TestIntroStripsInlineMarkup(t *testing.T) {
	got := intro("A **bold** little `tool`, see [the docs](https://x) or [refs][1].")
	want := "A bold little tool, see the docs or refs."
	if got != want {
		t.Fatalf("intro = %q, want %q", got, want)
	}
}

// An empty README, or one that is all noise, yields no intro at all.
func TestIntroEmptyAndAllNoise(t *testing.T) {
	if got := intro(""); got != "" {
		t.Fatalf("empty README gave %q", got)
	}
	allNoise := "# title\n\n[![b](u)](v)\n![i](u)\n\n## section\n"
	if got := intro(allNoise); got != "" {
		t.Fatalf("all-noise README gave %q", got)
	}
}

// The paragraph ends at the first blank line; the section that follows never
// leaks into the excerpt.
func TestIntroStopsAtParagraphEnd(t *testing.T) {
	got := intro("First words.\nStill first words.\n\nSecond paragraph.")
	if got != "First words. Still first words." {
		t.Fatalf("intro = %q", got)
	}
}

// Fenced code before the prose is skipped whole, never quoted.
func TestIntroSkipsFencedCode(t *testing.T) {
	got := intro("```\ngo install example\n```\n\nThe real story.")
	if got != "The real story." {
		t.Fatalf("intro = %q", got)
	}
}

// A wordy preface is capped on a word boundary with an ellipsis.
func TestIntroCapsLongParagraphs(t *testing.T) {
	long := strings.Repeat("many words flow here ", 30)
	got := intro(long)
	if n := len([]rune(got)); n > introMaxRunes+2 {
		t.Fatalf("intro not capped: %d runes", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("capped intro missing ellipsis: %q", got)
	}
}

// Read on a repository with the full shelf finds every page in shelf order
// and pulls the intro from the README.
func TestReadFindsPages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# hi\n\nA quiet little place.\n")
	writeFile(t, dir, "LICENSE", "MIT")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Read(dir)
	if p.Intro != "A quiet little place." {
		t.Fatalf("intro = %q", p.Intro)
	}
	if got := strings.Join(p.Notable, " · "); got != "readme · license · docs" {
		t.Fatalf("notable = %q", got)
	}
}

// No README at all: no intro, and readme stays off the shelf while the other
// pages still show.
func TestReadWithoutReadme(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "LICENSE.md", "MIT")
	p := Read(dir)
	if p.Intro != "" {
		t.Fatalf("intro without a README: %q", p.Intro)
	}
	if got := strings.Join(p.Notable, " · "); got != "license" {
		t.Fatalf("notable = %q", got)
	}
}

// An empty README earns its shelf spot but tells nothing; an empty or absent
// repository path yields wholly empty pages.
func TestReadEmptyReadmeAndEmptyDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "")
	p := Read(dir)
	if p.Intro != "" || strings.Join(p.Notable, ",") != "readme" {
		t.Fatalf("empty README pages = %+v", p)
	}
	if p := Read(""); p.Intro != "" || p.Notable != nil || p.Branch != "" {
		t.Fatalf("empty path pages = %+v", p)
	}
	if p := Read(filepath.Join(dir, "nope")); p.Intro != "" || p.Notable != nil {
		t.Fatalf("missing dir pages = %+v", p)
	}
}

// Read reports the checked-out branch only when it strays from the default:
// the git dir is read as plain files, no process spawned.
func TestReadBranchOffDefault(t *testing.T) {
	dir := t.TempDir()
	gd := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gd, "refs", "remotes", "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, gd, "HEAD", "ref: refs/heads/feature/guidebook\n")
	writeFile(t, filepath.Join(gd, "refs", "remotes", "origin"), "HEAD", "ref: refs/remotes/origin/main\n")
	if p := Read(dir); p.Branch != "feature/guidebook" {
		t.Fatalf("branch = %q, want feature/guidebook", p.Branch)
	}
	writeFile(t, gd, "HEAD", "ref: refs/heads/main\n")
	if p := Read(dir); p.Branch != "" {
		t.Fatalf("on the default branch, branch = %q, want none", p.Branch)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
