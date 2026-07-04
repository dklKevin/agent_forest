// Package guidebook reads a town's own papers - files already inside a
// connected repository - into a compact, deterministic self-introduction:
// the README's first words and which notable pages sit on the shelf. It only
// ever reads the local filesystem; no process is spawned, nothing is fetched,
// and nothing leaves the machine. A town without readable papers still gets
// a quiet page.
package guidebook

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dklKevin/agentforest/internal/gitscan"
)

// introMaxRunes caps the README excerpt: a guidebook entry is a few quiet
// lines, never the whole preface.
const introMaxRunes = 220

// readmeBytes bounds how much of a README is read for the excerpt; the first
// words always live near the top.
const readmeBytes = 64 * 1024

// Pages is what the guidebook found in one repository's papers.
type Pages struct {
	Intro   string   // the README's first meaningful words; "" when none
	Notable []string // notable pages that exist, in shelf order: readme, license, docs
	Branch  string   // the checked-out branch when it is not the default; "" otherwise
}

var readmeNames = []string{"README.md", "README.markdown", "README.rst", "README.txt", "README"}
var licenseNames = []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"}
var docsNames = []string{"docs", "doc"}

// Read gathers the guidebook pages for the repository at dir. Every field is
// best-effort: an empty or unreadable repository simply yields empty pages.
func Read(dir string) Pages {
	var p Pages
	if dir == "" {
		return p
	}
	for _, name := range readmeNames {
		b, ok := readHead(filepath.Join(dir, name))
		if !ok {
			continue
		}
		p.Intro = intro(string(b))
		p.Notable = append(p.Notable, "readme")
		break
	}
	for _, name := range licenseNames {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			p.Notable = append(p.Notable, "license")
			break
		}
	}
	for _, name := range docsNames {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
			p.Notable = append(p.Notable, "docs")
			break
		}
	}
	if cur := gitscan.HeadBranch(dir); cur != "" {
		if def := gitscan.DefaultBranch(dir); def != "" && cur != def {
			p.Branch = cur
		}
	}
	return p
}

// readHead reads at most readmeBytes of a regular file.
func readHead(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, readmeBytes))
	if err != nil {
		return nil, false
	}
	return b, true
}

var (
	imageRe   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	linkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	refLinkRe = regexp.MustCompile(`\[([^\]]*)\]\[[^\]]*\]`)
	htmlRe    = regexp.MustCompile(`<[^>]*>`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// intro pulls the first meaningful prose paragraph out of a README. Headings,
// badges, images, raw HTML, fenced code, and rules are noise, not story; the
// paragraph ends at the first blank or noisy line and is capped so a wordy
// preface stays a guidebook entry.
func intro(text string) string {
	var para []string
	inFence := false
	for _, ln := range strings.Split(text, "\n") {
		s := strings.TrimSpace(ln)
		if strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") {
			inFence = !inFence
			if len(para) > 0 {
				break
			}
			continue
		}
		if inFence {
			continue
		}
		if s == "" || noise(s) {
			if len(para) > 0 {
				break
			}
			continue
		}
		clean := stripInline(s)
		if clean == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, clean)
	}
	return capRunes(strings.Join(para, " "))
}

// noise reports lines that carry no story: headings, rules, tables, and
// lines that exist only to hold markup.
func noise(s string) bool {
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "|") {
		return true
	}
	if strings.HasPrefix(s, "<!--") {
		return true
	}
	// Horizontal rules and setext underlines: nothing but rule characters.
	if len(s) >= 3 && strings.Trim(s, "-=*_ ") == "" {
		return true
	}
	return false
}

// stripInline reduces one markdown line to its plain words: images and badges
// vanish, links keep their text, code ticks and emphasis stars fall away, and
// raw HTML tags are dropped. A line that was all markup reduces to "".
func stripInline(s string) string {
	s = strings.TrimLeft(s, "> ")
	s = imageRe.ReplaceAllString(s, "")
	s = linkRe.ReplaceAllString(s, "$1")
	s = refLinkRe.ReplaceAllString(s, "$1")
	s = htmlRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("`", "", "**", "", "*", "").Replace(s)
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if strings.Trim(s, " .,:;!?-–—·|[]()") == "" {
		return ""
	}
	return s
}

// capRunes trims the excerpt to introMaxRunes on a word boundary, marking the
// cut with an ellipsis.
func capRunes(s string) string {
	r := []rune(s)
	if len(r) <= introMaxRunes {
		return s
	}
	cut := string(r[:introMaxRunes])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " .,;:") + " …"
}
