// Package events defines the append-only event stream that feeds the world.
//
// This is the seam between data sources and everything else: the demo
// generator emits the same shape of events that the git adapter will emit, the
// reducer folds events into derived repo state, and the renderer only ever
// sees the derived state. Events are shaped for replay (a future time-machine
// walks the same log with an earlier cutoff), and carry JSON tags so the app
// can persist them as JSONL without reshaping.
package events

import (
	"sort"
	"time"
)

// Kind discriminates event payloads.
type Kind string

const (
	// KindRepo announces a repository: name, path, and first-seen time.
	KindRepo Kind = "repo"
	// KindActivity is a day bucket of commits. One event per active day keeps
	// the log compact at thousands of commits while staying replayable.
	KindActivity Kind = "activity"
	// KindTag is a tag or release.
	KindTag Kind = "tag"
	// KindLangs is a language-mix snapshot (fractions summing to ~1).
	KindLangs Kind = "langs"
)

// Event is one record in the append-only log.
type Event struct {
	Kind    Kind               `json:"kind"`
	Repo    string             `json:"repo"`
	TS      time.Time          `json:"ts"`
	Path    string             `json:"path,omitempty"`
	Commits int                `json:"commits,omitempty"`
	Name    string             `json:"name,omitempty"`
	Mix     map[string]float64 `json:"mix,omitempty"`
}

// RepoState is the derived, render-facing summary of one repository.
type RepoState struct {
	Name         string
	Path         string
	FirstTS      time.Time
	LastTS       time.Time
	TotalCommits int
	Tags         []string
	Mix          map[string]float64
}

// PrimaryLang returns the largest share of the language mix.
func (r *RepoState) PrimaryLang() string {
	best, share := "", -1.0
	for l, s := range r.Mix {
		if s > share || (s == share && l < best) {
			best, share = l, s
		}
	}
	return best
}

// Reduce folds an event log into per-repo derived state, ordered by FirstTS
// (oldest first), which is also the world's west-to-east town order.
func Reduce(evs []Event) []*RepoState {
	byRepo := map[string]*RepoState{}
	ordered := []*RepoState{}
	get := func(name string) *RepoState {
		if r, ok := byRepo[name]; ok {
			return r
		}
		r := &RepoState{Name: name, Mix: map[string]float64{}}
		byRepo[name] = r
		ordered = append(ordered, r)
		return r
	}
	sorted := make([]Event, len(evs))
	copy(sorted, evs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	for _, e := range sorted {
		r := get(e.Repo)
		switch e.Kind {
		case KindRepo:
			r.Path = e.Path
			if r.FirstTS.IsZero() || e.TS.Before(r.FirstTS) {
				r.FirstTS = e.TS
			}
		case KindActivity:
			r.TotalCommits += e.Commits
			if r.FirstTS.IsZero() || e.TS.Before(r.FirstTS) {
				r.FirstTS = e.TS
			}
			if e.TS.After(r.LastTS) {
				r.LastTS = e.TS
			}
		case KindTag:
			r.Tags = append(r.Tags, e.Name)
		case KindLangs:
			r.Mix = e.Mix
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].FirstTS.Before(ordered[j].FirstTS) })
	return ordered
}
