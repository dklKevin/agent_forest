// The since-last-visit delta: which towns stirred while the forest was
// closed. The append-only log already carries everything that happened, so
// the delta is a pure fold over events after the last-opened instant - no new
// event kinds, no daemon, nothing leaves the machine. A town "changed" only
// when something actually landed for it (commits, a release staked); decay
// deepening on its own is not a change, or a long absence would mark the
// whole forest.
package app

import (
	"sort"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/store"
)

// Stir is one town's change while the forest was closed, ranked so the UI can
// wake a handful of the most notable towns rather than strobe them all.
type Stir struct {
	Repo       string  // event-log key: the canonical path for real repos
	Name       string  // display name
	NewCommits int     // commits that landed after the forest was last open
	NewTags    int     // releases staked after the forest was last open
	DecayThen  float64 // reclamation depth the moment the forest was last open
	DecayNow   float64 // depth now, after everything that landed while away
	// WakeDepth is the deepest the town stood before its first new activity:
	// the depth it actually woke from while nobody was watching. This, not
	// DecayThen, is the depth the waking motion replays - a town tended the
	// day you left and revived five months later still wakes from five
	// months of sleep.
	WakeDepth float64
}

// Woke reports whether the town was visibly asleep and stands lighter now:
// the stirs worth a word, not just a motion.
func (s Stir) Woke() bool {
	return model.StageOf(s.WakeDepth) > model.Tended && s.DecayNow < s.WakeDepth-0.02
}

// SinceLastVisit folds the event log into the towns that changed between
// lastOpened and now, most notable first: woken towns before merely busy
// ones, deeper sleeps before shallower, then by how much landed. A zero
// lastOpened is a first run and a lastOpened at or past now is a clock that
// moved backwards; both return nothing rather than pulse the whole forest.
// Finished towns are skipped - a monument stands, it does not wake.
func SinceLastVisit(evs []events.Event, lastOpened, now time.Time) []Stir {
	if lastOpened.IsZero() || !lastOpened.Before(now) {
		return nil
	}
	type fold struct {
		name       string
		lastThen   time.Time // last activity at or before lastOpened
		lastNow    time.Time // last activity overall
		firstNew   time.Time // first activity or tag after lastOpened
		newCommits int
		newTags    int
		finished   bool
	}
	byRepo := map[string]*fold{}
	var order []string
	get := func(repo string) *fold {
		if f, ok := byRepo[repo]; ok {
			return f
		}
		f := &fold{}
		byRepo[repo] = f
		order = append(order, repo)
		return f
	}
	// Finish and unfinish resolve by their order in time, exactly as Reduce
	// resolves them for the world.
	sorted := make([]events.Event, len(evs))
	copy(sorted, evs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	for _, e := range sorted {
		f := get(e.Repo)
		inVisitWindow := e.TS.After(lastOpened) && !e.TS.After(now)
		switch e.Kind {
		case events.KindRepo:
			if e.Name != "" {
				f.name = e.Name
			}
		case events.KindActivity:
			if e.TS.After(f.lastNow) {
				f.lastNow = e.TS
			}
			if inVisitWindow {
				f.newCommits += e.Commits
				if f.firstNew.IsZero() || e.TS.Before(f.firstNew) {
					f.firstNew = e.TS
				}
			} else if !e.TS.After(lastOpened) && e.TS.After(f.lastThen) {
				f.lastThen = e.TS
			}
		case events.KindTag:
			if inVisitWindow {
				f.newTags++
				if f.firstNew.IsZero() || e.TS.Before(f.firstNew) {
					f.firstNew = e.TS
				}
			}
		case events.KindFinish:
			f.finished = true
		case events.KindUnfinish:
			f.finished = false
		}
	}
	var stirs []Stir
	for _, repo := range order {
		f := byRepo[repo]
		if f.finished || (f.newCommits == 0 && f.newTags == 0) {
			continue
		}
		name := f.name
		if name == "" {
			name = repo
		}
		wakeAt := f.firstNew
		if wakeAt.IsZero() {
			wakeAt = now // unreachable given the guard above; stay safe
		}
		stirs = append(stirs, Stir{
			Repo:       repo,
			Name:       name,
			NewCommits: f.newCommits,
			NewTags:    f.newTags,
			DecayThen:  decayBetween(f.lastThen, lastOpened),
			DecayNow:   decayBetween(f.lastNow, now),
			WakeDepth:  decayBetween(f.lastThen, wakeAt),
		})
	}
	sort.SliceStable(stirs, func(i, j int) bool {
		a, b := stirs[i], stirs[j]
		if a.Woke() != b.Woke() {
			return a.Woke()
		}
		if a.WakeDepth != b.WakeDepth {
			return a.WakeDepth > b.WakeDepth
		}
		if a.NewCommits != b.NewCommits {
			return a.NewCommits > b.NewCommits
		}
		return a.Name < b.Name
	})
	return stirs
}

// decayBetween is the reclamation depth a town idle since lastActivity had
// reached at the instant at. A zero lastActivity means the log holds no
// activity yet, and an instant at or before it means no idle time at all;
// both are depth zero, mirroring Town.Idle, so future-dated commits and
// skewed clocks can never invent decay.
func decayBetween(lastActivity, at time.Time) float64 {
	if lastActivity.IsZero() || !at.After(lastActivity) {
		return 0
	}
	return model.DecayAt(at.Sub(lastActivity))
}

// TouchLastOpened stamps the forest as opened at now and persists it. The
// stamp is decorative state (it only feeds the pulse), so callers may treat
// a failure as non-fatal.
func (a *App) TouchLastOpened(now time.Time) error {
	a.Settings.LastOpened = now
	if latest, found, err := store.LoadSettings(a.Dir); err == nil && found {
		latest.LastOpened = now
		return store.SaveSettings(a.Dir, latest)
	}
	return store.SaveSettings(a.Dir, a.Settings)
}
