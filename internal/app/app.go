// Package app assembles the forest from persisted state. It loads settings
// and the append-only event log, folds them into towns, and reconciles the
// log against the real repositories on disk. Both the TUI and the command
// line go through this one seam, and the renderer sits entirely behind it.
package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/gitscan"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/store"
)

// App is the loaded persistent state of one forest.
type App struct {
	Dir         string
	Settings    *store.Settings
	HasSettings bool // settings.json existed; false means first run
	Events      []events.Event
	Skipped     int // unreadable event-log lines skipped while loading

	// occupancy is each repo's working state as of the latest scan: in
	// memory only, by design. It never touches the event log or settings, so
	// a camp can only exist while a scan just saw the work standing.
	occupancy map[string]gitscan.Occupancy
}

// Load reads settings and the event log from the storage directory.
func Load() (*App, error) {
	dir, err := store.Dir()
	if err != nil {
		return nil, err
	}
	s, found, err := store.LoadSettings(dir)
	if err != nil {
		return nil, err
	}
	evs, skipped, err := store.LoadEvents(dir)
	if err != nil {
		return nil, err
	}
	evs = synthesizeLegacyFinishes(evs, s.Finished)
	return &App{Dir: dir, Settings: s, HasSettings: found, Events: evs, Skipped: skipped}, nil
}

// synthesizeLegacyFinishes folds settings.json's old finished list into the
// event stream. Finish state once lived in settings; it now lives in the log,
// so any legacy path the log holds no finish or unfinish record for gets a
// synthesized finish event stamped at that repo's last activity. In-memory
// only: the log is never rewritten, and the settings list is left in place so
// older builds keep reading it.
func synthesizeLegacyFinishes(evs []events.Event, finished []string) []events.Event {
	if len(finished) == 0 {
		return evs
	}
	known := map[string]bool{}
	settled := map[string]bool{} // repos the log already decides
	last := map[string]time.Time{}
	for _, e := range evs {
		known[e.Repo] = true
		if e.Kind == events.KindFinish || e.Kind == events.KindUnfinish {
			settled[e.Repo] = true
		}
		if e.TS.After(last[e.Repo]) {
			last[e.Repo] = e.TS
		}
	}
	for _, path := range finished {
		if !known[path] || settled[path] {
			continue
		}
		evs = append(evs, events.Event{Kind: events.KindFinish, Repo: path, TS: last[path]})
	}
	return evs
}

// Connected reports whether any real forest exists yet: a root to scan or
// history already in the log. When false, the world falls back to the demo.
func (a *App) Connected() bool {
	return len(a.Settings.Roots) > 0 || len(a.Events) > 0
}

// Towns folds the event log into towns, oldest first. Excluded repos are
// filtered here at build time; their history stays in the log so restoring
// them later costs nothing. Finish state and epitaphs are derived state,
// folded from the log's finish/unfinish events.
func (a *App) Towns() []*model.Town {
	repos := events.Reduce(a.Events)
	towns := make([]*model.Town, 0, len(repos))
	for _, r := range repos {
		if r.Path != "" && a.Settings.IsExcluded(r.Path) {
			continue
		}
		t := model.NewTown(r, r.Finished)
		if occ, ok := a.occupancy[r.Path]; ok {
			t.Occupancy = model.Occupancy{Dirty: occ.Dirty, Branch: occ.Branch, Worktrees: occ.Worktrees}
		}
		towns = append(towns, t)
	}
	return towns
}

// EpitaphMaxRunes is the carving limit. An epitaph is carved, not written:
// one short line, never prose.
const EpitaphMaxRunes = 40

// ValidateEpitaph reports why a string cannot be carved: too long, or not a
// single plain line. Empty is fine; a monument may stand unmarked.
func ValidateEpitaph(s string) error {
	if utf8.RuneCountInString(s) > EpitaphMaxRunes {
		return fmt.Errorf("an epitaph is carved, not written: at most %d characters", EpitaphMaxRunes)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("an epitaph is one plain line")
		}
	}
	return nil
}

// Finish lays a repo to rest as a monument, carving the epitaph (possibly
// empty). It appends a finish event to the log - the log is the truth; the
// legacy settings list is not touched. Re-finishing appends again: the log
// keeps every laying-to-rest, and the newest non-empty words win for display.
func (a *App) Finish(path, epitaph string, now time.Time) error {
	epitaph = strings.TrimSpace(epitaph)
	if err := ValidateEpitaph(epitaph); err != nil {
		return err
	}
	ev := events.Event{Kind: events.KindFinish, Repo: path, TS: now, Epitaph: epitaph}
	if err := store.AppendEvents(a.Dir, []events.Event{ev}); err != nil {
		return err
	}
	a.Events = append(a.Events, ev)
	return nil
}

// Unfinish lights the hearth again: the quiet reverse of Finish. The carved
// words stay in the log untouched. Any legacy settings entry for the path is
// dropped in memory and saved best-effort; once the reverse event is appended,
// the log remains the source of truth.
func (a *App) Unfinish(path string, now time.Time) error {
	ev := events.Event{Kind: events.KindUnfinish, Repo: path, TS: now}
	if err := store.AppendEvents(a.Dir, []events.Event{ev}); err != nil {
		return err
	}
	a.Events = append(a.Events, ev)
	if a.Settings.SetFinished(path, false) {
		_ = store.SaveSettings(a.Dir, a.Settings)
	}
	return nil
}

// FindTown resolves a name or path to the repo key used in settings and the
// log. Exact path wins; otherwise a unique town name matches. Excluded repos
// are still findable, so they can be restored.
func (a *App) FindTown(nameOrPath string) (string, error) {
	if c, err := gitscan.Canonical(nameOrPath); err == nil {
		for _, r := range events.Reduce(a.Events) {
			if r.Path == c {
				return c, nil
			}
		}
	}
	var matches []string
	var names []string
	for _, r := range events.Reduce(a.Events) {
		names = append(names, r.Name)
		if r.Name == nameOrPath {
			matches = append(matches, r.Path)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no town named %q (towns: %s)", nameOrPath, joinOr(names, "none yet"))
	default:
		return "", fmt.Errorf("%d towns are named %q; use the full path (%s)",
			len(matches), nameOrPath, joinOr(matches, ""))
	}
}

func joinOr(list []string, empty string) string {
	if len(list) == 0 {
		return empty
	}
	sort.Strings(list)
	out := ""
	for i, s := range list {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// ScanReport summarizes one reconcile pass.
type ScanReport struct {
	Repos     int      // repositories scanned after excludes are applied
	Changed   int      // repositories that produced new events
	NewEvents int      // events appended to the log
	Errors    []string // per-repo scan failures, "path: reason"
	// OccupancyShift reports that some repo's working-state read changed in
	// this pass, so camps need a world rebuild even when no events landed.
	OccupancyShift bool
}

// ConnectRoot records a new root directory and scans it. The root must
// exist; it is stored in canonical form.
func (a *App) ConnectRoot(root string, now time.Time) (ScanReport, error) {
	c, err := gitscan.Canonical(root)
	if err != nil {
		return ScanReport{}, fmt.Errorf("resolve %s: %w", root, err)
	}
	info, err := os.Stat(c)
	if err != nil || !info.IsDir() {
		return ScanReport{}, fmt.Errorf("%s is not a directory", root)
	}
	if a.Settings.AddRoot(c) {
		if err := store.SaveSettings(a.Dir, a.Settings); err != nil {
			return ScanReport{}, err
		}
	}
	return a.Reconcile(now)
}

// Reconcile discovers repositories under every connected root, skips excluded
// repos, scans the ones the log is behind on, and appends the missing events.
// It works from what the log already knows, so it is safe to run any number
// of times.
func (a *App) Reconcile(now time.Time) (ScanReport, error) {
	repos := gitscan.Discover(a.Settings.Roots)
	kept := repos[:0]
	for _, r := range repos {
		if !a.Settings.IsExcluded(r) {
			kept = append(kept, r)
		}
	}
	rep, err := a.scan(kept, now)
	// Presence must never outlive its repo: a camp for a repository that is
	// no longer discoverable (deleted, excluded, root removed) breaks here.
	keptSet := map[string]bool{}
	for _, r := range kept {
		keptSet[r] = true
	}
	for path := range a.occupancy {
		if !keptSet[path] {
			delete(a.occupancy, path)
			rep.OccupancyShift = true
		}
	}
	return rep, err
}

// RescanRepo reconciles a single repository: the live-update path while the
// app is open.
func (a *App) RescanRepo(path string, now time.Time) (ScanReport, error) {
	return a.scan([]string{path}, now)
}

// scan runs the git adapter over repos in parallel and appends whatever the
// log is missing, in deterministic repo order. Each repo's working state is
// read in the same pass and held in memory only: occupancy rides the scan
// cadence, never the log.
func (a *App) scan(repos []string, now time.Time) (ScanReport, error) {
	rep := ScanReport{Repos: len(repos)}
	known := KnownByRepo(a.Events)

	type result struct {
		repo string
		evs  []events.Event
		err  error
		occ  gitscan.Occupancy
	}
	results := make([]result, len(repos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			evs, err := gitscan.Scan(repo, known[repo], now)
			results[i] = result{repo, evs, err, gitscan.ReadOccupancy(repo)}
		}(i, repo)
	}
	wg.Wait()

	if a.occupancy == nil {
		a.occupancy = map[string]gitscan.Occupancy{}
	}
	var fresh []events.Event
	for _, r := range results {
		if a.occupancy[r.repo] != r.occ {
			rep.OccupancyShift = true
		}
		a.occupancy[r.repo] = r.occ
		if r.err != nil {
			rep.Errors = append(rep.Errors, r.repo+": "+r.err.Error())
			continue
		}
		if len(r.evs) > 0 {
			rep.Changed++
			fresh = append(fresh, r.evs...)
		}
	}
	if len(fresh) > 0 {
		if err := store.AppendEvents(a.Dir, fresh); err != nil {
			return rep, err
		}
		a.Events = append(a.Events, fresh...)
		rep.NewEvents = len(fresh)
	}
	return rep, nil
}

// KnownByRepo derives, per repository, what the event log already recorded:
// the log is its own scan cursor, so no separate cache file can drift.
func KnownByRepo(evs []events.Event) map[string]gitscan.Known {
	known := map[string]gitscan.Known{}
	langTS := map[string]time.Time{}
	for _, e := range evs {
		k := known[e.Repo]
		switch e.Kind {
		case events.KindRepo:
			k.Announced = true
		case events.KindActivity:
			if e.TS.After(k.LastTS) {
				k.LastTS = e.TS
			}
			if k.DayCounts == nil {
				k.DayCounts = map[string]int{}
			}
			k.DayCounts[gitscan.ActivityDay(e.TS)] += e.Commits
		case events.KindTag:
			if k.Tags == nil {
				k.Tags = map[string]bool{}
			}
			k.Tags[e.Name] = true
		case events.KindLangs:
			if !e.TS.Before(langTS[e.Repo]) {
				langTS[e.Repo] = e.TS
				k.Mix = e.Mix
			}
		case events.KindComp:
			if k.Comps == nil {
				k.Comps = map[string]gitscan.KnownComp{}
			}
			c := k.Comps[e.Path]
			c.Bytes = e.Bytes
			if e.TS.After(c.LastTS) {
				c.LastTS = e.TS
			}
			k.Comps[e.Path] = c
		}
		known[e.Repo] = k
	}
	return known
}

// SaveSettings persists the current settings.
func (a *App) SaveSettings() error {
	return store.SaveSettings(a.Dir, a.Settings)
}
