// Package gitscan reads real repositories and emits the same event stream
// the demo generator produces. It is the only package that touches git; the
// renderer never sees anything but reduced state.
//
// Scans are incremental against what the event log already knows, so the log
// stays append-only: a rescan emits only new activity days, new tags, and a
// language snapshot when the mix has actually shifted.
package gitscan

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
)

// gitTimeout bounds every git invocation so one wedged repo cannot hang the
// whole forest.
const gitTimeout = 30 * time.Second

// Discover walks root directories recursively and returns canonical paths of
// every git repository found, deduped and sorted. It does not descend into
// repositories (submodules and vendored checkouts belong to their parent),
// hidden directories, or node_modules, and it does not follow symlinks.
func Discover(roots []string) []string {
	seen := map[string]bool{}
	var repos []string
	for _, root := range roots {
		root, err := Canonical(root)
		if err != nil {
			continue
		}
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil // unreadable subtrees are skipped, not fatal
			}
			if p != root {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			if gitDirExists(p) {
				if c, err := Canonical(p); err == nil && !seen[c] {
					seen[c] = true
					repos = append(repos, c)
				}
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Strings(repos)
	return repos
}

// Canonical resolves a path to its absolute, symlink-free form: the identity
// key for repositories across settings and the event log.
func Canonical(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r, nil
	}
	return abs, nil
}

func gitDirExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Known is what the event log already recorded about one repository, so a
// scan can emit only the delta.
type Known struct {
	Announced bool               // a repo event exists
	LastTS    time.Time          // newest activity timestamp seen
	Tags      map[string]bool    // tag names already recorded
	Mix       map[string]float64 // last language snapshot
}

// Scan reads one repository and returns the events the log is missing. A
// repository with no commits yet returns nothing. Every commit on any ref
// counts as life; there is no author filtering.
func Scan(path string, known Known, now time.Time) ([]events.Event, error) {
	stamps, err := commitStamps(path)
	if err != nil {
		return nil, err
	}
	if len(stamps) == 0 {
		return nil, nil
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i].Before(stamps[j]) })

	var evs []events.Event
	if !known.Announced {
		evs = append(evs, events.Event{
			Kind: events.KindRepo, Repo: path, TS: stamps[0],
			Path: path, Name: filepath.Base(path),
		})
	}
	evs = append(evs, bucketByDay(path, stamps, known.LastTS)...)

	tags, err := tagEvents(path, known.Tags)
	if err == nil {
		evs = append(evs, tags...)
	}
	if mix := langMix(path); mix != nil && mixChanged(known.Mix, mix) {
		evs = append(evs, events.Event{Kind: events.KindLangs, Repo: path, TS: now, Mix: mix})
	}
	return evs, nil
}

// commitStamps returns the committer timestamp of every commit reachable
// from any ref. rev-list is used instead of log because it exits cleanly on
// a repository that has refs but no commits yet.
func commitStamps(path string) ([]time.Time, error) {
	out, err := runGit(path, "rev-list", "--all", "--timestamp")
	if err != nil {
		return nil, err
	}
	var stamps []time.Time
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		sec, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		stamps = append(stamps, time.Unix(sec, 0))
	}
	return stamps, nil
}

// bucketByDay folds commits newer than after into one activity event per
// local calendar day. Each event carries the newest commit time in its day,
// so "last tended" stays truthful down to the minute.
func bucketByDay(repo string, stamps []time.Time, after time.Time) []events.Event {
	type bucket struct {
		count int
		last  time.Time
	}
	byDay := map[string]*bucket{}
	for _, ts := range stamps {
		if !ts.After(after) {
			continue
		}
		day := ts.Local().Format("2006-01-02")
		b := byDay[day]
		if b == nil {
			b = &bucket{}
			byDay[day] = b
		}
		b.count++
		if ts.After(b.last) {
			b.last = ts
		}
	}
	var evs []events.Event
	for _, b := range byDay {
		evs = append(evs, events.Event{
			Kind: events.KindActivity, Repo: repo, TS: b.last, Commits: b.count,
		})
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].TS.Before(evs[j].TS) })
	return evs
}

func tagEvents(path string, knownTags map[string]bool) ([]events.Event, error) {
	out, err := runGit(path, "for-each-ref", "refs/tags",
		"--format=%(creatordate:unix)%09%(refname:short)")
	if err != nil {
		return nil, err
	}
	var evs []events.Event
	for _, line := range strings.Split(out, "\n") {
		ts, name, ok := strings.Cut(line, "\t")
		if !ok || name == "" || knownTags[name] {
			continue
		}
		sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
		if err != nil {
			continue
		}
		evs = append(evs, events.Event{
			Kind: events.KindTag, Repo: path, TS: time.Unix(sec, 0), Name: name,
		})
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].TS.Before(evs[j].TS) })
	return evs, nil
}

// langMix weighs tracked files by size on disk and returns fractions per
// language, or nil when nothing recognizable is tracked.
func langMix(path string) map[string]float64 {
	out, err := runGit(path, "ls-files", "-z")
	if err != nil {
		return nil
	}
	sizes := map[string]int64{}
	var total int64
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		lang, ok := langByExt[strings.ToLower(filepath.Ext(rel))]
		if !ok {
			continue
		}
		info, err := os.Lstat(filepath.Join(path, rel))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		sizes[lang] += info.Size()
		total += info.Size()
	}
	if total == 0 {
		return nil
	}
	mix := make(map[string]float64, len(sizes))
	for lang, n := range sizes {
		mix[lang] = float64(n) / float64(total)
	}
	return mix
}

// langByExt maps file extensions to the languages the forest knows how to
// name. Unlisted extensions simply do not weigh into the mix.
var langByExt = map[string]string{
	".go":    "go",
	".rs":    "rust",
	".py":    "python",
	".pyi":   "python",
	".ts":    "typescript",
	".tsx":   "typescript",
	".mts":   "typescript",
	".cts":   "typescript",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".cjs":   "javascript",
	".c":     "c",
	".h":     "c",
	".cc":    "c++",
	".cpp":   "c++",
	".cxx":   "c++",
	".hpp":   "c++",
	".hh":    "c++",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".swift": "swift",
	".java":  "java",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".rb":    "ruby",
	".php":   "php",
	".cs":    "c#",
	".lua":   "lua",
	".zig":   "zig",
	".hs":    "haskell",
	".ex":    "elixir",
	".exs":   "elixir",
	".erl":   "erlang",
	".scala": "scala",
	".dart":  "dart",
	".jl":    "julia",
	".ml":    "ocaml",
	".mli":   "ocaml",
	".pl":    "perl",
	".pm":    "perl",
	".r":     "r",
	".m":     "objective-c",
	".html":  "html",
	".css":   "css",
	".scss":  "css",
	".sass":  "css",
	".less":  "css",
	".vue":   "javascript",
	".sql":   "sql",
}

// mixChanged reports whether two language snapshots differ enough to be
// worth a new log entry (half a percent on any language).
func mixChanged(a, b map[string]float64) bool {
	const eps = 0.005
	for k, v := range b {
		if av, ok := a[k]; !ok || v-av > eps || av-v > eps {
			return true
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return true
		}
	}
	return false
}

// Fingerprint returns a cheap, exec-free change signal for a repository:
// stat data of the ref stores plus HEAD content. Any commit, tag, branch
// move, or checkout changes it. An unreadable repository returns "".
func Fingerprint(path string) string {
	gitDir := resolveGitDir(path)
	if gitDir == "" {
		return ""
	}
	h := fnv.New64a()
	if b, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
		h.Write(b)
	}
	for _, name := range []string{"logs/HEAD", "packed-refs"} {
		if info, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			fmt.Fprintf(h, "%s:%d:%d;", name, info.Size(), info.ModTime().UnixNano())
		}
	}
	filepath.WalkDir(filepath.Join(gitDir, "refs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			fmt.Fprintf(h, "%s:%d:%d;", p, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return strconv.FormatUint(h.Sum64(), 16)
}

// resolveGitDir maps a repository path to its actual git directory,
// following the "gitdir: ..." indirection of worktrees and submodules.
func resolveGitDir(path string) string {
	p := filepath.Join(path, ".git")
	info, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return p
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
	if target == "" {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(path, target)
	}
	return target
}

func runGit(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir, "--no-optional-locks"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	timer := time.AfterFunc(gitTimeout, func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	err := cmd.Run()
	timer.Stop()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}
