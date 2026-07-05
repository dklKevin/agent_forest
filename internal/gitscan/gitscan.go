// Package gitscan reads real repositories and emits the same event stream
// the demo generator produces. It is the only package that touches git; the
// renderer never sees anything but reduced state.
//
// Scans are incremental against what the event log already knows, so the log
// stays append-only: a rescan emits only the missing activity, folded per
// local day, along with new tags and a language snapshot when the mix has
// actually shifted. Because commit timestamps are second-granular, that delta
// includes a follow-up commit sharing the last scan's second, which the
// timestamp cursor alone would drop.
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
	Announced bool      // a repo event exists
	LastTS    time.Time // newest activity timestamp seen
	// DayCounts stores recorded activity commits by local day. Scans reconcile
	// only LastTS's day because timezone changes can re-key older aggregate
	// activity events and double-count history.
	DayCounts map[string]int
	Tags      map[string]bool    // tag names already recorded
	Mix       map[string]float64 // last language snapshot
	Comps     map[string]KnownComp
}

// KnownComp is the last recorded observation of one component.
type KnownComp struct {
	Bytes  int64
	LastTS time.Time
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
	evs = append(evs, bucketByDay(path, stamps, known.LastTS, known.DayCounts)...)

	tags, err := tagEvents(path, known.Tags)
	if err == nil {
		evs = append(evs, tags...)
	}
	files := trackedFiles(path)
	if mix := langMix(files); mix != nil && mixChanged(known.Mix, mix) {
		evs = append(evs, events.Event{Kind: events.KindLangs, Repo: path, TS: now, Mix: mix})
	}
	evs = append(evs, compEvents(path, files, known.Comps)...)
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

// bucketByDay folds missing commits into one activity event per local calendar
// day. Git commit timestamps are second-granularity, so the timestamp cursor
// alone drops a follow-up commit that shares the last scan's second; the
// recorded day count catches those only on the cursor's local day. Reconciling
// every historical day would let a timezone change re-key old aggregate
// activity events and double-count history. Commits on every other day follow
// the plain timestamp cursor; commits strictly after the cursor are always
// emitted, so stale or over-attributed day counts cannot swallow new work.
// Each event carries the newest commit time in its day, so "last tended" stays
// truthful down to the minute.
func bucketByDay(repo string, stamps []time.Time, after time.Time, knownCounts map[string]int) []events.Event {
	type bucket struct {
		count int
		after int
		last  time.Time
	}
	cursorDay := ""
	if !after.IsZero() {
		cursorDay = ActivityDay(after)
	}
	byDay := map[string]*bucket{}
	for _, ts := range stamps {
		afterStamp := ts.After(after)
		day := ActivityDay(ts)
		reconcileDay := knownCounts != nil && day == cursorDay
		if !afterStamp && !reconcileDay {
			continue
		}
		b := byDay[day]
		if b == nil {
			b = &bucket{}
			byDay[day] = b
		}
		b.count++
		if afterStamp {
			b.after++
		}
		if ts.After(b.last) {
			b.last = ts
		}
	}
	var evs []events.Event
	for day, b := range byDay {
		commits := b.count
		if knownCounts != nil && day == cursorDay {
			commits -= knownCounts[day]
			if b.after > commits {
				commits = b.after
			}
		}
		if commits <= 0 {
			continue
		}
		evs = append(evs, events.Event{
			Kind: events.KindActivity, Repo: repo, TS: b.last, Commits: commits,
		})
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].TS.Before(evs[j].TS) })
	return evs
}

// ActivityDay is the reconciliation key for one local calendar day. The scanner
// and log-derived cursor (app.KnownByRepo) use it consistently, but only the
// cursor day is reconciled because aggregate activity events cannot safely
// re-key every historical commit after timezone changes.
func ActivityDay(ts time.Time) string {
	return ts.Local().Format("2006-01-02")
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

// trackedFile is one git-tracked regular file with its size on disk.
type trackedFile struct {
	rel  string
	size int64
}

// trackedFiles lists the repository's tracked regular files once; the
// language mix and the component map are both derived from this single pass.
func trackedFiles(path string) []trackedFile {
	out, err := runGit(path, "ls-files", "-z")
	if err != nil {
		return nil
	}
	var files []trackedFile
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(path, rel))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, trackedFile{rel: rel, size: info.Size()})
	}
	return files
}

// langMix weighs tracked files by size and returns fractions per language,
// or nil when nothing recognizable is tracked.
func langMix(files []trackedFile) map[string]float64 {
	sizes := map[string]int64{}
	var total int64
	for _, f := range files {
		lang, ok := langByExt[strings.ToLower(filepath.Ext(f.rel))]
		if !ok {
			continue
		}
		sizes[lang] += f.size
		total += f.size
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

// Component is a major directory of a repository: a building in the making.
type Component struct {
	Name  string // base name, the display name
	Path  string // repo-relative path, the stable identity
	Bytes int64
	Files int
}

// Directories that usually exist only to hold the real components. When one
// of these dominates the repo, the components live a level deeper.
var wrapperDirs = map[string]bool{
	"src": true, "lib": true, "libs": true, "source": true, "sources": true,
	"packages": true, "pkg": true, "internal": true, "app": true, "apps": true,
	"modules": true, "crates": true,
}

// The settlement ceiling: no town keeps more than this many buildings.
const maxComponents = 12

// detectComponents maps tracked files to the repo's major directories.
// Top-level directories are the components, except that a single dominant
// wrapper (src, packages, internal...) is descended one level so the true
// structure shows. Files at the root belong to the hearth, dot-directories
// are ignored, and directories must clear the file floor plus either the
// byte-share or file-share materiality floor.
func detectComponents(files []trackedFile) []Component {
	type agg struct {
		bytes int64
		files int
	}
	var total int64
	top := map[string]*agg{}
	for _, f := range files {
		total += f.size
		dir, _, ok := strings.Cut(f.rel, "/")
		if !ok || strings.HasPrefix(dir, ".") {
			continue
		}
		a := top[dir]
		if a == nil {
			a = &agg{}
			top[dir] = a
		}
		a.bytes += f.size
		a.files++
	}
	if total == 0 {
		return nil
	}

	// A wrapper that holds most of the repo is descended one level.
	wrapper := ""
	for dir, a := range top {
		if wrapperDirs[dir] && float64(a.bytes) >= 0.7*float64(total) {
			wrapper = dir
			break
		}
	}
	group := map[string]*agg{}
	name := map[string]string{}
	for dir, a := range top {
		if dir == wrapper {
			continue
		}
		group[dir] = a
		name[dir] = dir
	}
	if wrapper != "" {
		prefix := wrapper + "/"
		for _, f := range files {
			if !strings.HasPrefix(f.rel, prefix) {
				continue
			}
			rest := f.rel[len(prefix):]
			dir, _, ok := strings.Cut(rest, "/")
			if !ok || strings.HasPrefix(dir, ".") {
				continue // files directly inside the wrapper stay with the hearth
			}
			key := prefix + dir
			a := group[key]
			if a == nil {
				a = &agg{}
				group[key] = a
			}
			a.bytes += f.size
			a.files++
			name[key] = dir
		}
	}

	totalFiles := len(files)
	var comps []Component
	for key, a := range group {
		// A directory earns a building when it clears the absolute file floor
		// and is material by bytes OR by file count. Flooring on file share as
		// well as byte share keeps a directory of real code from vanishing when
		// a single large generated/vendored/lock file elsewhere dominates the
		// repo's byte total and drags every sibling under the byte floor.
		byteShare := float64(a.bytes) / float64(total)
		fileShare := float64(a.files) / float64(totalFiles)
		if a.files < 3 || (byteShare < 0.01 && fileShare < 0.01) {
			continue // below the floor: no building earned
		}
		comps = append(comps, Component{Name: name[key], Path: key, Bytes: a.bytes, Files: a.files})
	}
	sort.Slice(comps, func(i, j int) bool {
		if comps[i].Bytes != comps[j].Bytes {
			return comps[i].Bytes > comps[j].Bytes
		}
		return comps[i].Path < comps[j].Path
	})
	if len(comps) > maxComponents {
		comps = comps[:maxComponents]
	}
	return comps
}

// compLastTouch asks git when a component's path last appeared in a commit
// on any ref.
func compLastTouch(path, comp string) (time.Time, error) {
	out, err := runGit(path, "log", "--all", "-1", "--format=%ct", "--", comp)
	if err != nil {
		return time.Time{}, err
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("component %s has no commit history", comp)
	}
	return time.Unix(sec, 0), nil
}

// compEvents emits an observation for every component that is new, freshly
// touched, or materially resized since the log last looked. One quick git
// call per component; a component that vanished emits nothing and its last
// observation stands forever.
func compEvents(path string, files []trackedFile, known map[string]KnownComp) []events.Event {
	comps := detectComponents(files)
	var total int64
	for _, f := range files {
		total += f.size
	}
	var evs []events.Event
	for _, c := range comps {
		last, err := compLastTouch(path, c.Path)
		if err != nil {
			continue
		}
		prev, had := known[c.Path]
		eps := prev.Bytes / 8
		if eps < 16*1024 {
			eps = 16 * 1024
		}
		grown := c.Bytes-prev.Bytes > eps || prev.Bytes-c.Bytes > eps
		if had && !last.After(prev.LastTS) && !grown {
			continue
		}
		evs = append(evs, events.Event{
			Kind: events.KindComp, Repo: path, TS: last,
			Name: c.Name, Path: c.Path, Bytes: c.Bytes, Files: c.Files,
		})
	}
	return evs
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

// HeadBranch returns the branch checked out in a repository, read straight
// from the git dir like Fingerprint (no process spawned). A detached HEAD or
// an unreadable repository returns "".
func HeadBranch(path string) string {
	gitDir := resolveGitDir(path)
	if gitDir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	const prefix = "ref: refs/heads/"
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, prefix) {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// DefaultBranch returns the repository's default branch, best-effort and
// exec-free: the branch origin/HEAD points at when a remote is set up, else
// main or master when one exists locally, else "".
func DefaultBranch(path string) string {
	gitDir := resolveGitDir(path)
	if gitDir == "" {
		return ""
	}
	// Shared refs live in the common dir when the path is a linked worktree.
	common := gitDir
	if b, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			if !filepath.IsAbs(t) {
				t = filepath.Join(gitDir, t)
			}
			common = t
		}
	}
	if b, err := os.ReadFile(filepath.Join(common, "refs", "remotes", "origin", "HEAD")); err == nil {
		const prefix = "ref: refs/remotes/origin/"
		if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	packed, _ := os.ReadFile(filepath.Join(common, "packed-refs"))
	for _, name := range []string{"main", "master"} {
		if _, err := os.Stat(filepath.Join(common, "refs", "heads", name)); err == nil {
			return name
		}
		if ref := " refs/heads/" + name; strings.Contains(string(packed), ref+"\n") ||
			strings.HasSuffix(strings.TrimRight(string(packed), "\n"), ref) {
			return name
		}
	}
	return ""
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
