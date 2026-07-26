// Package agentrun reads short-lived, local evidence of work happening in a
// repository. It is deliberately a filesystem adapter: no process discovery,
// provider API, network request, background watcher, or persistent AgentForest
// state is involved.
package agentrun

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Phase is an explicitly evidenced stage of a local run.
type Phase string

const (
	Planning  Phase = "planning"
	Building  Phase = "building"
	Testing   Phase = "testing"
	Reviewing Phase = "reviewing"
	Blocked   Phase = "blocked"
	HandedOff Phase = "handed_off"
	Completed Phase = "completed"
)

const (
	freshFor      = 15 * time.Minute
	futureLeeway  = 2 * time.Minute
	maxRuns       = 32
	maxDirEntries = 256
	maxRunEntries = 64
	maxFileBytes  = 1 << 20
	maxOpenBytes  = maxRuns * maxFileBytes
	maxGNHFBytes  = 16 << 20
	maxScanBytes  = maxOpenBytes + maxGNHFBytes
	maxLineBytes  = 64 << 10
	maxTextRunes  = 240
	maxSteps      = 12
	maxList       = 24
)

var phaseNames = map[Phase]string{
	Planning: "planning", Building: "building", Testing: "testing",
	Reviewing: "reviewing", Blocked: "blocked", HandedOff: "handed off",
	Completed: "completed",
}

// String is the quiet, human-facing spelling used by inspect and replay.
func (p Phase) String() string { return phaseNames[p] }

// InProgress distinguishes a present working state from terminal evidence.
// Handed-off and completed marks may linger briefly without pitching a camp.
func (p Phase) InProgress() bool {
	switch p {
	case Planning, Building, Testing, Reviewing, Blocked:
		return true
	default:
		return false
	}
}

func validPhase(p Phase) bool {
	_, ok := phaseNames[p]
	return ok
}

// Step is one curated replay entry. It contains no prompt, command, output,
// provider identity, or machine identifier.
type Step struct {
	At      time.Time
	Phase   Phase
	Summary string
}

// Presence is the selected usable local run in a repository. Phase remains
// the last evidenced phase for replay; Active controls whether a live mark may
// be drawn. Stale evidence can therefore leave a plaque without pretending
// work is still happening.
type Presence struct {
	Phase        Phase
	Active       bool
	UpdatedAt    time.Time
	Objective    string
	Steps        []Step
	Paths        []string
	Verification string
	Failures     []string
	Unresolved   []string
}

// Available reports whether there is enough curated evidence to offer a work
// plaque. A lone phase is useful even when the writer has not supplied prose.
func (p Presence) Available() bool { return validPhase(p.Phase) }

// ActiveAt ages a scanned mark out even when no file changes trigger another
// scan. The retained Presence can still back a replay plaque.
func (p Presence) ActiveAt(now time.Time) bool {
	age := now.Sub(p.UpdatedAt)
	return p.Active && age >= -futureLeeway && age <= freshFor
}

// Equal compares display-bearing evidence without relying on provider-specific
// file identity.
func Equal(a, b Presence) bool {
	if a.Phase != b.Phase || a.Active != b.Active || !a.UpdatedAt.Equal(b.UpdatedAt) ||
		a.Objective != b.Objective || a.Verification != b.Verification ||
		len(a.Steps) != len(b.Steps) || len(a.Paths) != len(b.Paths) ||
		len(a.Failures) != len(b.Failures) || len(a.Unresolved) != len(b.Unresolved) {
		return false
	}
	for i := range a.Steps {
		if a.Steps[i] != b.Steps[i] {
			return false
		}
	}
	return sameStrings(a.Paths, b.Paths) && sameStrings(a.Failures, b.Failures) &&
		sameStrings(a.Unresolved, b.Unresolved)
}

func sameStrings(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Read inspects the two documented local evidence roots and selects a valid
// run. Malformed records, incomplete final lines, and symlinks are ignored;
// unreadable, changing, or over-budget evidence encountered during bounded
// selection makes it fail closed.
func Read(repo string, now time.Time) Presence {
	return readWithBudgets(repo, now, maxOpenBytes, maxGNHFBytes)
}

func readWithBudgets(repo string, now time.Time, openLimit, gnhfLimit int64) Presence {
	budgets := map[string]*readBudget{
		".agentforest": {remaining: openLimit},
		".gnhf":        {remaining: gnhfLimit},
	}
	type candidate struct {
		dir      string
		provider string
		at       time.Time
	}
	var candidates []candidate
	if root, ok := evidenceRoot(repo, ".agentforest"); ok {
		entries, err := recentRunDirs(root)
		if err != nil {
			return Presence{}
		}
		for _, entry := range entries {
			dir := filepath.Join(root, entry.Name())
			at, err := runEvidenceModTime(root, dir)
			if err != nil {
				return Presence{}
			}
			candidates = append(candidates, candidate{
				dir: dir, provider: ".agentforest", at: at,
			})
		}
	}
	if root, ok := evidenceRoot(repo, ".gnhf"); ok {
		entries, err := recentRunDirs(root)
		if err != nil {
			return Presence{}
		}
		for _, entry := range entries {
			dir := filepath.Join(root, entry.Name())
			at, err := runEvidenceModTime(root, dir)
			if err != nil {
				return Presence{}
			}
			candidates = append(candidates, candidate{
				dir: dir, provider: ".gnhf", at: at,
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at.Equal(candidates[j].at) {
			if candidates[i].provider == candidates[j].provider {
				return candidates[i].dir < candidates[j].dir
			}
			return candidates[i].provider < candidates[j].provider
		}
		return candidates[i].at.After(candidates[j].at)
	})
	type result struct {
		p      Presence
		causal bool
	}
	var found []result
	for _, item := range candidates {
		info, err := os.Lstat(item.dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Presence{}
		}
		var p Presence
		var ok bool
		switch item.provider {
		case ".agentforest":
			p, ok = readOpenRun(item.dir, now, budgets[item.provider])
		case ".gnhf":
			p, ok = readGNHFRun(item.dir, now, budgets[item.provider])
		}
		if budgets[item.provider].uncertain {
			return Presence{}
		}
		if ok {
			found = append(found, result{p: p, causal: item.provider == ".agentforest"})
		}
	}
	if len(found) == 0 {
		return Presence{}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].causal != found[j].causal {
			return found[i].causal
		}
		return found[i].p.UpdatedAt.After(found[j].p.UpdatedAt)
	})
	return found[0].p
}

func evidenceRoot(repo, name string) (string, bool) {
	base := filepath.Join(repo, name)
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	root := filepath.Join(base, "runs")
	info, err = os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	return root, true
}

type openEvent struct {
	At           string   `json:"at"`
	Phase        Phase    `json:"phase"`
	Objective    string   `json:"objective"`
	Summary      string   `json:"summary"`
	Paths        []string `json:"paths"`
	Verification string   `json:"verification"`
	Failure      string   `json:"failure"`
	Unresolved   []string `json:"unresolved"`
}

func readOpenRun(dir string, now time.Time, budget *readBudget) (Presence, bool) {
	data, err := safeReadFile(filepath.Join(dir, "events.jsonl"), budget)
	if err != nil {
		return Presence{}, false
	}

	var events []struct {
		event openEvent
		at    time.Time
	}
	forEachCompleteLine(data, func(line []byte) {
		var ev openEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return
		}
		at, err := time.Parse(time.RFC3339Nano, ev.At)
		if err != nil || !validPhase(ev.Phase) || at.After(now.Add(futureLeeway)) {
			return
		}
		events = append(events, struct {
			event openEvent
			at    time.Time
		}{event: ev, at: at})
	})
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].at.Before(events[j].at)
	})

	var p Presence
	for _, item := range events {
		ev, at := item.event, item.at
		p.Phase = ev.Phase
		p.UpdatedAt = at
		if v := cleanText(ev.Objective); v != "" {
			p.Objective = v
		}
		if v := cleanText(ev.Summary); v != "" {
			appendStep(&p, Step{At: at, Phase: ev.Phase, Summary: v})
		}
		for _, path := range ev.Paths {
			if v, ok := cleanRelativePath(path); ok {
				appendUnique(&p.Paths, v)
			}
		}
		switch ev.Verification {
		case "passed", "failed":
			p.Verification = ev.Verification
		}
		if v := cleanText(ev.Failure); v != "" {
			appendUnique(&p.Failures, v)
		}
		for _, item := range ev.Unresolved {
			if v := cleanText(item); v != "" {
				appendUnique(&p.Unresolved, v)
			}
		}
	}
	if !p.Available() {
		return Presence{}, false
	}
	p.Active = now.Sub(p.UpdatedAt) <= freshFor && now.Sub(p.UpdatedAt) >= -futureLeeway
	return p, true
}

type gnhfEvent struct {
	Type string `json:"type"`
	Item struct {
		Type string `json:"type"`
	} `json:"item"`
}

func readGNHFRun(dir string, now time.Time, budget *readBudget) (Presence, bool) {
	entries, err := safeReadDir(dir, maxRunEntries)
	if err != nil {
		budget.uncertain = true
		return Presence{}, false
	}
	var logs []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 ||
			!strings.HasPrefix(name, "iteration-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		logs = append(logs, filepath.Join(dir, name))
	}
	if len(logs) == 0 {
		return Presence{}, false
	}
	sort.Slice(logs, func(i, j int) bool {
		return iterationNumber(logs[i]) < iterationNumber(logs[j])
	})
	if len(logs) > maxSteps {
		logs = logs[len(logs)-maxSteps:]
	}

	// prompt.md and event item payloads are intentionally not read into
	// the plaque. Only the writer's curated iteration summaries cross the
	// compatibility boundary.
	p := Presence{}
	summaries := map[int]string{}
	notes := filepath.Join(dir, "notes.md")
	if _, ok := regularEvidenceInfo(notes); ok {
		summaries = noteSummaries(notes, budget)
	}
	for _, path := range logs {
		phase, at, ok := readGNHFLog(path, now, budget)
		if !ok {
			continue
		}
		p.Phase, p.UpdatedAt = phase, at
		n := iterationNumber(path)
		if summary := summaries[n]; summary != "" {
			appendStep(&p, Step{At: at, Phase: phase, Summary: summary})
		}
	}
	if !p.Available() {
		return Presence{}, false
	}
	p.Active = now.Sub(p.UpdatedAt) <= freshFor && now.Sub(p.UpdatedAt) >= -futureLeeway
	return p, true
}

func recentRunDirs(root string) ([]os.DirEntry, error) {
	entries, err := safeReadDir(root, maxDirEntries)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		entry os.DirEntry
		at    time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		at, err := runEvidenceModTime(root, dir)
		if err != nil {
			return nil, err
		}
		if at.IsZero() {
			continue
		}
		candidates = append(candidates, candidate{entry: entry, at: at})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at.Equal(candidates[j].at) {
			return candidates[i].entry.Name() < candidates[j].entry.Name()
		}
		return candidates[i].at.After(candidates[j].at)
	})
	if len(candidates) > maxRuns {
		return nil, errors.New("local evidence run count exceeds causal selection limit")
	}
	out := make([]os.DirEntry, len(candidates))
	for i, item := range candidates {
		out[i] = item.entry
	}
	return out, nil
}

func runEvidenceModTime(root, dir string) (time.Time, error) {
	if filepath.Base(filepath.Dir(root)) == ".agentforest" {
		if info, ok := regularEvidenceInfo(filepath.Join(dir, "events.jsonl")); ok {
			return info.ModTime(), nil
		}
		return time.Time{}, nil
	}
	entries, err := safeReadDir(dir, maxRunEntries)
	if err != nil {
		return time.Time{}, err
	}
	var newest time.Time
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 ||
			!strings.HasPrefix(name, "iteration-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest, nil
}

func regularEvidenceInfo(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false
	}
	return info, true
}

func fingerprintFiles(root, dir string) []string {
	if filepath.Base(filepath.Dir(root)) == ".agentforest" {
		path := filepath.Join(dir, "events.jsonl")
		if _, ok := regularEvidenceInfo(path); ok {
			return []string{path}
		}
		return nil
	}
	var paths []string
	notes := filepath.Join(dir, "notes.md")
	if _, ok := regularEvidenceInfo(notes); ok {
		paths = append(paths, notes)
	}
	entries, err := safeReadDir(dir, maxRunEntries)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 ||
			!strings.HasPrefix(name, "iteration-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

func readGNHFLog(path string, now time.Time, budget *readBudget) (Phase, time.Time, bool) {
	data, info, err := safeReadFileInfo(path, budget)
	if err != nil {
		return "", time.Time{}, false
	}
	if info.ModTime().After(now.Add(futureLeeway)) {
		return "", time.Time{}, false
	}
	var sawTurn, sawItem, sawComplete bool
	forEachCompleteLine(data, func(line []byte) {
		var ev gnhfEvent
		if json.Unmarshal(line, &ev) != nil {
			return
		}
		switch ev.Type {
		case "thread.started", "turn.started":
			sawTurn = true
		case "item.started", "item.updated", "item.completed":
			sawItem = true
		case "turn.completed":
			sawComplete = true
		}
	})
	switch {
	case sawComplete:
		return HandedOff, info.ModTime(), true
	case sawItem:
		return Building, info.ModTime(), true
	case sawTurn:
		return Planning, info.ModTime(), true
	default:
		return "", time.Time{}, false
	}
}

type readBudget struct {
	remaining  int64
	consumed   int64
	uncertain  bool
	beforeRead func(string)
}

func (b *readBudget) canRead(size int64) bool {
	if b == nil || size < 0 || size > b.remaining {
		return false
	}
	return true
}

func (b *readBudget) charge(size int64) bool {
	if !b.canRead(size) {
		if b != nil {
			b.uncertain = true
		}
		return false
	}
	b.remaining -= size
	b.consumed += size
	return true
}

func safeReadFile(path string, budget *readBudget) ([]byte, error) {
	data, _, err := safeReadFileInfo(path, budget)
	return data, err
}

// safeReadFileInfo consumes at most the descriptor size established before the
// read and charges the bytes actually returned by the kernel. A writer may
// append while the descriptor is open, but those bytes are neither consumed
// nor parsed; observed growth makes this candidate uncertain and the enclosing
// repository scan fails closed.
func safeReadFileInfo(path string, budget *readBudget) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		budget.uncertain = true
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxFileBytes {
		budget.uncertain = true
		return nil, nil, errors.New("unsafe local evidence file")
	}
	f, err := os.Open(path)
	if err != nil {
		budget.uncertain = true
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() ||
		opened.Size() > maxFileBytes {
		budget.uncertain = true
		return nil, nil, errors.New("local evidence changed while opening")
	}
	if !budget.canRead(opened.Size()) {
		budget.uncertain = true
		return nil, nil, errors.New("local evidence exceeds scan byte budget")
	}
	if budget.beforeRead != nil {
		budget.beforeRead(path)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, opened.Size()))
	if !budget.charge(int64(len(data))) {
		return nil, nil, errors.New("local evidence exceeds scan byte budget")
	}
	after, statErr := f.Stat()
	if readErr != nil || statErr != nil || int64(len(data)) != opened.Size() ||
		!os.SameFile(opened, after) || after.Size() != opened.Size() ||
		!after.ModTime().Equal(opened.ModTime()) {
		budget.uncertain = true
		return nil, nil, errors.New("local evidence changed while reading")
	}
	return data, opened, nil
}

func safeReadDir(path string, limit int) ([]os.DirEntry, error) {
	if limit <= 0 {
		return nil, errors.New("local evidence directory limit must be positive")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe local evidence directory")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.IsDir() {
		return nil, errors.New("local evidence directory changed while opening")
	}
	entries, err := f.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > limit {
		return nil, errors.New("local evidence directory exceeds traversal limit")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// forEachCompleteLine consumes newline-terminated records only. Append-only
// writers frequently leave the newest JSON object half-written; even a
// momentarily valid object is not committed evidence until its newline lands.
func forEachCompleteLine(data []byte, fn func([]byte)) {
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return
		}
		if i <= maxLineBytes {
			fn(data[:i])
		}
		data = data[i+1:]
	}
}

func noteSummaries(path string, budget *readBudget) map[int]string {
	out := map[int]string{}
	data, err := safeReadFile(path, budget)
	if err != nil {
		return out
	}
	var current int
	forEachCompleteLine(data, func(raw []byte) {
		line := strings.TrimSpace(string(raw))
		if strings.HasPrefix(line, "### Iteration ") {
			current, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "### Iteration ")))
			return
		}
		if current > 0 && strings.HasPrefix(line, "**Summary:**") {
			if v := cleanText(strings.TrimSpace(strings.TrimPrefix(line, "**Summary:**"))); v != "" {
				out[current] = v
				if len(out) > maxSteps {
					oldest := current
					for n := range out {
						if n < oldest {
							oldest = n
						}
					}
					delete(out, oldest)
				}
			}
		}
	})
	return out
}

func iterationNumber(path string) int {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	n, _ := strconv.Atoi(strings.TrimPrefix(base, "iteration-"))
	return n
}

func appendStep(p *Presence, step Step) {
	if len(p.Steps) == maxSteps {
		copy(p.Steps, p.Steps[1:])
		p.Steps = p.Steps[:maxSteps-1]
	}
	p.Steps = append(p.Steps, step)
}

func appendUnique(dst *[]string, v string) {
	for _, have := range *dst {
		if have == v {
			return
		}
	}
	if len(*dst) < maxList {
		*dst = append(*dst, v)
	}
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ""
		}
	}
	rs := []rune(s)
	if len(rs) > maxTextRunes {
		rs = rs[:maxTextRunes]
	}
	return string(rs)
}

func cleanRelativePath(path string) (string, bool) {
	if path == "" || filepath.IsAbs(path) || !utf8.ValidString(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	for _, r := range clean {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return cleanText(filepath.ToSlash(clean)), true
}

// Fingerprint returns a cheap metadata-only digest of the supported local
// evidence roots. It follows no symlinks and reads no evidence content.
func Fingerprint(repo string) string {
	h := sha256.New()
	for _, name := range []string{".agentforest", ".gnhf"} {
		root, ok := evidenceRoot(repo, name)
		if !ok {
			continue
		}
		runs, err := recentRunDirs(root)
		if err != nil {
			continue
		}
		for _, run := range runs {
			if !run.IsDir() || run.Type()&os.ModeSymlink != 0 {
				continue
			}
			dir := filepath.Join(root, run.Name())
			for _, path := range fingerprintFiles(root, dir) {
				info, ok := regularEvidenceInfo(path)
				if !ok {
					continue
				}
				rel, _ := filepath.Rel(repo, path)
				_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00",
					rel, info.Size(), info.ModTime().UnixNano(), info.Mode())
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
