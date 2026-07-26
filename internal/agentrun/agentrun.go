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
	pathpkg "path"
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

type presenceResult struct {
	p      Presence
	causal bool
}

type tierResult struct {
	found     []presenceResult
	uncertain bool
}

type runDir struct {
	name string
	info os.FileInfo
	at   time.Time
}

type dirSnapshot struct {
	info      os.FileInfo
	entries   []os.DirEntry
	namespace map[string]os.FileInfo
}

// FingerprintScope identifies the evidence tier whose metadata can affect a
// result returned by ReadWithScope.
type FingerprintScope uint8

const (
	FingerprintAuthoritative FingerprintScope = iota
	FingerprintCompatibility
)

// Fingerprints holds metadata-only polling digests for both possible evidence
// scopes from one bounded metadata pass.
type Fingerprints struct {
	authoritative       string
	compatibility       string
	authoritativeStable bool
	compatibilityStable bool
}

// For returns the polling digest for scope, prefixed so the next poll can
// preserve the selected authority tier.
func (f Fingerprints) For(scope FingerprintScope) string {
	if scope == FingerprintAuthoritative {
		if !f.authoritativeStable {
			return ""
		}
		return "a:" + f.authoritative
	}
	if !f.compatibilityStable {
		return ""
	}
	return "c:" + f.compatibility
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
		if a.Steps[i].Phase != b.Steps[i].Phase ||
			a.Steps[i].Summary != b.Steps[i].Summary ||
			!a.Steps[i].At.Equal(b.Steps[i].At) {
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
// run. Malformed records and incomplete final lines are ignored; symlinks are
// never followed. Unreadable, changing, unsafe, or over-budget evidence
// encountered during bounded selection makes it fail closed.
func Read(repo string, now time.Time) Presence {
	return readWithBudgets(repo, now, maxOpenBytes, maxGNHFBytes)
}

func readWithBudgets(repo string, now time.Time, openLimit, gnhfLimit int64) Presence {
	p, _, _ := readWithScope(repo, now, openLimit, gnhfLimit)
	return p
}

// ReadWithScope returns the selected presence and the evidence scope that may
// invalidate it during foreground polling. Stable is false when the selected
// tier changed or could not be validated during the read.
func ReadWithScope(repo string, now time.Time) (Presence, FingerprintScope, bool) {
	return readWithScope(repo, now, maxOpenBytes, maxGNHFBytes)
}

func readWithScope(repo string, now time.Time, openLimit, gnhfLimit int64) (Presence, FingerprintScope, bool) {
	return readWithScopeBudgets(repo, now,
		&readBudget{remaining: openLimit},
		&readBudget{remaining: gnhfLimit},
	)
}

func readWithScopeBudgets(repo string, now time.Time, openBudget, gnhfBudget *readBudget) (Presence, FingerprintScope, bool) {
	authoritative := scanTier(repo, ".agentforest", now, openBudget)
	if authoritative.uncertain {
		return Presence{}, FingerprintAuthoritative, false
	}
	if len(authoritative.found) > 0 {
		return selectPresence(authoritative.found), FingerprintAuthoritative, true
	}

	compatibility := scanTier(repo, ".gnhf", now, gnhfBudget)
	if compatibility.uncertain {
		return Presence{}, FingerprintCompatibility, false
	}
	return selectPresence(compatibility.found), FingerprintCompatibility, true
}

func scanTier(repo, provider string, now time.Time, budget *readBudget) tierResult {
	type candidate struct {
		dir  string
		info os.FileInfo
		at   time.Time
	}
	root, present, uncertain := evidenceRoot(repo, provider)
	if uncertain || !present {
		return tierResult{uncertain: uncertain}
	}

	before, err := safeReadDirSnapshot(root, maxDirEntries)
	if err != nil {
		return tierResult{uncertain: true}
	}
	entries, err := recentRunDirsFromSnapshot(root, before)
	if err != nil {
		return tierResult{uncertain: true}
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		candidates = append(candidates, candidate{
			dir:  filepath.Join(root, entry.name),
			info: entry.info,
			at:   entry.at,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at.Equal(candidates[j].at) {
			return candidates[i].dir < candidates[j].dir
		}
		return candidates[i].at.After(candidates[j].at)
	})
	var found []presenceResult
	for _, item := range candidates {
		info, err := os.Lstat(item.dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
			!sameFileState(item.info, info) {
			return tierResult{uncertain: true}
		}
		var observations []Presence
		switch provider {
		case ".agentforest":
			observations = readOpenRunObservations(item.dir, now, budget)
		case ".gnhf":
			observations = readGNHFRunObservations(item.dir, now, budget)
		}
		if budget.uncertain {
			return tierResult{uncertain: true}
		}
		for _, p := range observations {
			found = append(found, presenceResult{p: p, causal: provider == ".agentforest"})
		}
	}
	if !tierSnapshotStable(root, before, entries, nil) {
		return tierResult{uncertain: true}
	}
	return tierResult{found: found}
}

func tierSnapshotStable(root string, before dirSnapshot, runs []runDir, afterRunAudit func()) bool {
	after, err := safeReadDirSnapshot(root, maxDirEntries)
	if err != nil || !sameDirSnapshots(before, after) {
		return false
	}
	auditedRuns, err := recentRunDirsFromSnapshot(root, after)
	if err != nil || !sameRunDirs(runs, auditedRuns) {
		return false
	}
	if afterRunAudit != nil {
		afterRunAudit()
	}
	final, err := safeReadDirSnapshot(root, maxDirEntries)
	if err != nil || !sameDirSnapshots(before, final) {
		return false
	}
	if !sameDirSnapshots(after, final) {
		return false
	}
	return true
}

// selectPresence admits a winning run only when every candidate at the same
// causal rank and maximum event time carries identical privacy-filtered truth.
// Exact duplicates are safe to collapse: returning any of them produces the
// same Presence, so filesystem metadata and enumeration order cannot affect
// the visible phase, plaque, or activity state.
func selectPresence(found []presenceResult) Presence {
	if len(found) == 0 {
		return Presence{}
	}
	causalRank := false
	for _, item := range found {
		if item.causal {
			causalRank = true
			break
		}
	}
	var maximum time.Time
	for _, item := range found {
		if item.causal == causalRank && item.p.UpdatedAt.After(maximum) {
			maximum = item.p.UpdatedAt
		}
	}
	var selected Presence
	haveSelected := false
	for _, item := range found {
		if item.causal != causalRank || !item.p.UpdatedAt.Equal(maximum) {
			continue
		}
		if !haveSelected {
			selected = item.p
			haveSelected = true
			continue
		}
		if !Equal(selected, item.p) {
			return Presence{}
		}
	}
	return selected
}

func evidenceRoot(repo, name string) (root string, present, uncertain bool) {
	base := filepath.Join(repo, name)
	info, err := os.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, false
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false, true
	}
	root = filepath.Join(base, "runs")
	info, err = os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, false
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false, true
	}
	return root, true, false
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
	observations := readOpenRunObservations(dir, now, budget)
	found := make([]presenceResult, 0, len(observations))
	for _, p := range observations {
		found = append(found, presenceResult{p: p, causal: true})
	}
	p := selectPresence(found)
	return p, p.Available()
}

func readOpenRunObservations(dir string, now time.Time, budget *readBudget) []Presence {
	data, err := safeReadFile(filepath.Join(dir, "events.jsonl"), budget)
	if err != nil {
		return nil
	}

	type timedOpenEvent struct {
		event openEvent
		at    time.Time
	}
	var events []timedOpenEvent
	forEachCompleteLine(data, func(line []byte) {
		var ev openEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return
		}
		at, err := time.Parse(time.RFC3339Nano, ev.At)
		if err != nil || !validPhase(ev.Phase) || at.After(now.Add(futureLeeway)) {
			return
		}
		events = append(events, timedOpenEvent{event: ev, at: at})
	})
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].at.Before(events[j].at)
	})
	if len(events) == 0 {
		return nil
	}

	maximum := events[len(events)-1].at
	var base Presence
	for _, item := range events {
		if !item.at.Before(maximum) {
			break
		}
		applyOpenEvent(&base, item.event, item.at)
	}
	observations := make([]Presence, 0, len(events))
	for _, item := range events {
		if !item.at.Equal(maximum) {
			continue
		}
		p := clonePresence(base)
		applyOpenEvent(&p, item.event, item.at)
		p.Active = now.Sub(p.UpdatedAt) <= freshFor && now.Sub(p.UpdatedAt) >= -futureLeeway
		observations = append(observations, p)
	}
	return observations
}

func applyOpenEvent(p *Presence, ev openEvent, at time.Time) {
	p.Phase = ev.Phase
	p.UpdatedAt = at
	if v := cleanText(ev.Objective); v != "" {
		p.Objective = v
	}
	if v := cleanText(ev.Summary); v != "" {
		appendStep(p, Step{At: at, Phase: ev.Phase, Summary: v})
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

func clonePresence(p Presence) Presence {
	p.Steps = append([]Step(nil), p.Steps...)
	p.Paths = append([]string(nil), p.Paths...)
	p.Failures = append([]string(nil), p.Failures...)
	p.Unresolved = append([]string(nil), p.Unresolved...)
	return p
}

type gnhfEvent struct {
	Type string `json:"type"`
	Item struct {
		Type string `json:"type"`
	} `json:"item"`
}

func readGNHFRunObservations(dir string, now time.Time, budget *readBudget) []Presence {
	entries, err := safeReadDir(dir, maxRunEntries)
	if err != nil {
		budget.uncertain = true
		return nil
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
		return nil
	}
	sort.Slice(logs, func(i, j int) bool {
		return iterationNumber(logs[i]) < iterationNumber(logs[j])
	})

	// prompt.md and event item payloads are intentionally not read into
	// the plaque. Only the writer's curated iteration summaries cross the
	// compatibility boundary.
	summaries := map[int]string{}
	notes := filepath.Join(dir, "notes.md")
	if _, ok := regularEvidenceInfo(notes); ok {
		iterations := make(map[int]struct{}, len(logs))
		for _, path := range logs {
			iterations[iterationNumber(path)] = struct{}{}
		}
		summaries = noteSummaries(notes, budget, iterations)
	}
	type observation struct {
		phase     Phase
		at        time.Time
		iteration int
		summary   string
	}
	var parsed []observation
	for _, path := range logs {
		phase, at, ok := readGNHFLog(path, now, budget)
		if !ok {
			continue
		}
		n := iterationNumber(path)
		parsed = append(parsed, observation{
			phase: phase, at: at, iteration: n, summary: summaries[n],
		})
	}
	if len(parsed) == 0 {
		return nil
	}
	sort.SliceStable(parsed, func(i, j int) bool {
		if parsed[i].at.Equal(parsed[j].at) {
			return parsed[i].iteration < parsed[j].iteration
		}
		return parsed[i].at.Before(parsed[j].at)
	})
	maximum := parsed[len(parsed)-1].at
	var base Presence
	for _, item := range parsed {
		if !item.at.Before(maximum) {
			break
		}
		applyGNHFObservation(&base, item.phase, item.at, item.summary)
	}
	observations := make([]Presence, 0, len(parsed))
	for _, item := range parsed {
		if !item.at.Equal(maximum) {
			continue
		}
		p := clonePresence(base)
		applyGNHFObservation(&p, item.phase, item.at, item.summary)
		p.Active = now.Sub(p.UpdatedAt) <= freshFor && now.Sub(p.UpdatedAt) >= -futureLeeway
		observations = append(observations, p)
	}
	return observations
}

func applyGNHFObservation(p *Presence, phase Phase, at time.Time, summary string) {
	p.Phase, p.UpdatedAt = phase, at
	if summary != "" {
		appendStep(p, Step{At: at, Phase: phase, Summary: summary})
	}
}

func recentRunDirsFromSnapshot(root string, snapshot dirSnapshot) ([]runDir, error) {
	candidates := make([]runDir, 0, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("local evidence run changed during traversal")
		}
		enumerated, ok := snapshot.namespace[entry.Name()]
		if !ok || !sameFileState(enumerated, info) {
			return nil, errors.New("local evidence run changed after enumeration")
		}
		at, err := runEvidenceModTime(root, dir)
		if err != nil {
			return nil, err
		}
		if at.IsZero() {
			continue
		}
		candidates = append(candidates, runDir{name: entry.Name(), info: info, at: at})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at.Equal(candidates[j].at) {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].at.After(candidates[j].at)
	})
	if len(candidates) > maxRuns {
		return nil, errors.New("local evidence run count exceeds causal selection limit")
	}
	return candidates, nil
}

func sameRunDirs(a, b []runDir) bool {
	if len(a) != len(b) {
		return false
	}
	byName := make(map[string]os.FileInfo, len(a))
	for _, run := range a {
		byName[run.name] = run.info
	}
	for _, run := range b {
		info, ok := byName[run.name]
		if !ok || !sameFileState(info, run.info) {
			return false
		}
	}
	byTime := make(map[string]time.Time, len(a))
	for _, run := range a {
		byTime[run.name] = run.at
	}
	for _, run := range b {
		if !byTime[run.name].Equal(run.at) {
			return false
		}
	}
	return true
}

func runEvidenceModTime(root, dir string) (time.Time, error) {
	if filepath.Base(filepath.Dir(root)) == ".agentforest" {
		path := filepath.Join(dir, "events.jsonl")
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, nil
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return time.Time{}, errors.New("unsafe provider-neutral evidence file")
		}
		return info.ModTime(), nil
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
	pathAfter, pathStatErr := os.Lstat(path)
	if readErr != nil || statErr != nil || int64(len(data)) != opened.Size() ||
		!os.SameFile(opened, after) || after.Size() != opened.Size() ||
		!after.ModTime().Equal(opened.ModTime()) || pathStatErr != nil ||
		!pathAfter.Mode().IsRegular() || pathAfter.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(opened, pathAfter) {
		budget.uncertain = true
		return nil, nil, errors.New("local evidence changed while reading")
	}
	return data, opened, nil
}

func safeReadDir(path string, limit int) ([]os.DirEntry, error) {
	return safeReadDirAfter(path, limit, nil)
}

func safeReadDirAfter(path string, limit int, afterRead func()) ([]os.DirEntry, error) {
	snapshot, err := safeReadDirSnapshotAfter(path, limit, afterRead)
	if err != nil {
		return nil, err
	}
	return snapshot.entries, nil
}

func safeReadDirSnapshot(path string, limit int) (dirSnapshot, error) {
	return safeReadDirSnapshotAfter(path, limit, nil)
}

func safeReadDirSnapshotAfter(path string, limit int, afterRead func()) (dirSnapshot, error) {
	if limit <= 0 {
		return dirSnapshot{}, errors.New("local evidence directory limit must be positive")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return dirSnapshot{}, errors.New("unsafe local evidence directory")
	}
	f, err := os.Open(path)
	if err != nil {
		return dirSnapshot{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameFileState(info, opened) || !opened.IsDir() {
		return dirSnapshot{}, errors.New("local evidence directory changed while opening")
	}
	entries, err := f.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return dirSnapshot{}, err
	}
	if len(entries) > limit {
		return dirSnapshot{}, errors.New("local evidence directory exceeds traversal limit")
	}
	first, err := dirEntryInfos(entries)
	if err != nil {
		return dirSnapshot{}, err
	}
	if afterRead != nil {
		afterRead()
	}
	current, err := os.Open(path)
	if err != nil {
		return dirSnapshot{}, err
	}
	defer current.Close()
	currentInfo, err := current.Stat()
	if err != nil || !sameFileState(opened, currentInfo) || !currentInfo.IsDir() {
		return dirSnapshot{}, errors.New("local evidence directory changed during traversal")
	}
	currentEntries, err := current.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return dirSnapshot{}, err
	}
	if len(currentEntries) > limit {
		return dirSnapshot{}, errors.New("local evidence directory exceeds traversal limit")
	}
	second, err := dirEntryInfos(currentEntries)
	if err != nil || !sameDirEntryInfos(first, second) {
		return dirSnapshot{}, errors.New("local evidence directory namespace changed during traversal")
	}
	sort.Slice(currentEntries, func(i, j int) bool {
		return currentEntries[i].Name() < currentEntries[j].Name()
	})
	return dirSnapshot{info: currentInfo, entries: currentEntries, namespace: second}, nil
}

func dirEntryInfos(entries []os.DirEntry) (map[string]os.FileInfo, error) {
	infos := make(map[string]os.FileInfo, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		infos[entry.Name()] = info
	}
	return infos, nil
}

func sameDirEntryInfos(a, b map[string]os.FileInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for name, first := range a {
		second, ok := b[name]
		if !ok || !sameFileState(first, second) {
			return false
		}
	}
	return true
}

func sameDirSnapshots(a, b dirSnapshot) bool {
	return sameFileState(a.info, b.info) && sameDirEntryInfos(a.namespace, b.namespace)
}

func sameFileState(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) &&
		a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
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

func noteSummaries(path string, budget *readBudget, iterations map[int]struct{}) map[int]string {
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
		if _, wanted := iterations[current]; current > 0 && wanted &&
			strings.HasPrefix(line, "**Summary:**") {
			if v := cleanText(strings.TrimSpace(strings.TrimPrefix(line, "**Summary:**"))); v != "" {
				out[current] = v
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
	if !utf8.ValidString(path) {
		return "", false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	for _, r := range path {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", false
		}
	}
	clean := pathpkg.Clean(strings.ReplaceAll(path, "\\", "/"))
	clean = pathpkg.Clean(strings.TrimSpace(clean))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "/") ||
		clean == ".." || strings.HasPrefix(clean, "../") ||
		(len(clean) >= 2 &&
			((clean[0] >= 'a' && clean[0] <= 'z') ||
				(clean[0] >= 'A' && clean[0] <= 'Z')) &&
			clean[1] == ':') {
		return "", false
	}
	clean = cleanText(clean)
	return clean, clean != ""
}

// Fingerprint returns cheap metadata-only digests for both authority scopes.
// It follows no symlinks, reads no evidence content, and does not traverse the
// compatibility tier when authoritative metadata is unsafe.
func Fingerprint(repo string) Fingerprints {
	var authoritative bytes.Buffer
	stable := fingerprintTier(&authoritative, repo, ".agentforest")
	authDigest := fingerprintDigest(authoritative.Bytes())
	if !stable {
		return Fingerprints{authoritative: authDigest, compatibility: authDigest}
	}
	var compatibility bytes.Buffer
	_, _ = compatibility.Write(authoritative.Bytes())
	compatibilityStable := fingerprintTier(&compatibility, repo, ".gnhf")
	return Fingerprints{
		authoritative:       authDigest,
		compatibility:       fingerprintDigest(compatibility.Bytes()),
		authoritativeStable: true,
		compatibilityStable: compatibilityStable,
	}
}

// FingerprintFor recomputes the metadata-only polling digest for an explicit
// authority scope.
func FingerprintFor(repo string, scope FingerprintScope) string {
	var data bytes.Buffer
	stable := fingerprintTier(&data, repo, ".agentforest")
	if stable && scope == FingerprintCompatibility {
		stable = fingerprintTier(&data, repo, ".gnhf")
	}
	if !stable {
		return ""
	}
	digest := fingerprintDigest(data.Bytes())
	if scope == FingerprintAuthoritative {
		return "a:" + digest
	}
	return "c:" + digest
}

func fingerprintDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fingerprintTier(h io.Writer, repo, name string) bool {
	_, _ = fmt.Fprintf(h, "%s\x00", name)
	root, present, uncertain := evidenceRoot(repo, name)
	if uncertain {
		_, _ = io.WriteString(h, "unsafe\x00")
		return false
	}
	if !present {
		_, _ = io.WriteString(h, "absent\x00")
		return true
	}
	before, err := safeReadDirSnapshot(root, maxDirEntries)
	if err != nil {
		_, _ = io.WriteString(h, "unstable-root\x00")
		return false
	}
	rootInfo, rootID, ok := stableMetadataIdentity(root)
	if !ok || !sameFileState(before.info, rootInfo) {
		_, _ = io.WriteString(h, "unknown-root-identity\x00")
		return false
	}
	_, _ = fmt.Fprintf(h, "root\x00%s\x00%d\x00%d\x00%d\x00",
		rootID, rootInfo.Size(), rootInfo.ModTime().UnixNano(), rootInfo.Mode())
	for _, entry := range before.entries {
		info := before.namespace[entry.Name()]
		_, _ = fmt.Fprintf(h, "entry\x00%s\x00%d\x00%d\x00%d\x00",
			entry.Name(), info.Size(), info.ModTime().UnixNano(), info.Mode())
	}

	runs, err := recentRunDirsFromSnapshot(root, before)
	if err != nil {
		_, _ = io.WriteString(h, "unstable\x00")
		return false
	}
	for _, run := range runs {
		dir := filepath.Join(root, run.name)
		for _, path := range fingerprintFiles(root, dir) {
			info, identity, ok := stableMetadataIdentity(path)
			if !ok || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				_, _ = io.WriteString(h, "unsafe-file\x00")
				return false
			}
			rel, _ := filepath.Rel(repo, path)
			_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%d\x00",
				rel, identity, info.Size(), info.ModTime().UnixNano(), info.Mode())
		}
	}
	after, err := safeReadDirSnapshot(root, maxDirEntries)
	if err != nil || !sameDirSnapshots(before, after) {
		_, _ = io.WriteString(h, "changed-root\x00")
		return false
	}
	return true
}

func stableMetadataIdentity(path string) (os.FileInfo, string, bool) {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 ||
		(!before.IsDir() && !before.Mode().IsRegular()) {
		return nil, "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameFileState(before, opened) {
		return nil, "", false
	}
	identity, ok := platformFileIdentity(f, opened)
	if !ok || identity == "" {
		return nil, "", false
	}
	after, err := os.Lstat(path)
	if err != nil || !sameFileState(opened, after) {
		return nil, "", false
	}
	return opened, identity, true
}
