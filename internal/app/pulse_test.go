package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/store"
)

const day = 24 * time.Hour

// pulseRepo builds the minimal life of one town: planted, active at each
// given instant, keyed by path the way real forests key their logs.
func pulseRepo(path, name string, commits []time.Time, perCommit int) []events.Event {
	evs := []events.Event{{Kind: events.KindRepo, Repo: path, TS: commits[0], Path: path, Name: name}}
	for _, ts := range commits {
		evs = append(evs, events.Event{Kind: events.KindActivity, Repo: path, TS: ts, Commits: perCommit})
	}
	return evs
}

func TestSinceLastVisitFirstRunIsQuiet(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	evs := pulseRepo("/r/winterwell", "winterwell", []time.Time{now.Add(-400 * day), now.Add(-2 * day)}, 3)
	if got := SinceLastVisit(evs, time.Time{}, now); got != nil {
		t.Fatalf("a first run (zero lastOpened) must pulse nothing, got %d stirs", len(got))
	}
}

func TestSinceLastVisitNoChangesIsQuiet(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	evs := pulseRepo("/r/mossjar", "mossjar", []time.Time{now.Add(-300 * day), now.Add(-40 * day)}, 2)
	if got := SinceLastVisit(evs, now.Add(-30*day), now); got != nil {
		t.Fatalf("nothing landed since the last visit, got %d stirs", len(got))
	}
}

func TestSinceLastVisitOneTownStirred(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	lastOpened := now.Add(-10 * day)
	evs := pulseRepo("/r/winterwell", "winterwell", []time.Time{now.Add(-200 * day), now.Add(-2 * day)}, 5)
	evs = append(evs, pulseRepo("/r/mossjar", "mossjar", []time.Time{now.Add(-300 * day), now.Add(-40 * day)}, 2)...)

	stirs := SinceLastVisit(evs, lastOpened, now)
	if len(stirs) != 1 {
		t.Fatalf("stirs = %d, want exactly the one changed town", len(stirs))
	}
	st := stirs[0]
	if st.Repo != "/r/winterwell" || st.Name != "winterwell" || st.NewCommits != 5 {
		t.Fatalf("wrong stir: %+v", st)
	}
	// It slept ~198 days before waking: the wake depth is that sleep, the
	// stage visibly changed, and it stands tended now.
	if model.StageOf(st.WakeDepth) <= model.Tended {
		t.Fatalf("wake depth should be a visible sleep, got %v", st.WakeDepth)
	}
	if model.StageOf(st.DecayThen) == model.StageOf(st.DecayNow) {
		t.Fatalf("stage should have changed since last visit: then %v, now %v", st.DecayThen, st.DecayNow)
	}
	if model.StageOf(st.DecayNow) != model.Tended {
		t.Fatalf("a town that just woke should stand tended, got %v", st.DecayNow)
	}
	if !st.Woke() {
		t.Fatal("a deep sleep broken by commits is the definition of woke")
	}
}

func TestSinceLastVisitRanksManyByDepthOfSleep(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	lastOpened := now.Add(-30 * day)
	var evs []events.Event
	// All three gained commits while away; deeper sleeps are more notable,
	// and a town that never visibly slept ranks last however busy it was.
	evs = append(evs, pulseRepo("/r/deep", "deep", []time.Time{now.Add(-400 * day), now.Add(-5 * day)}, 1)...)
	evs = append(evs, pulseRepo("/r/mid", "mid", []time.Time{now.Add(-60 * day), now.Add(-5 * day)}, 3)...)
	evs = append(evs, pulseRepo("/r/busy", "busy", []time.Time{now.Add(-31 * day), now.Add(-29 * day), now.Add(-1 * day)}, 50)...)

	stirs := SinceLastVisit(evs, lastOpened, now)
	if len(stirs) != 3 {
		t.Fatalf("stirs = %d, want 3", len(stirs))
	}
	if stirs[0].Name != "deep" || stirs[1].Name != "mid" || stirs[2].Name != "busy" {
		t.Fatalf("ranking wrong: %s, %s, %s", stirs[0].Name, stirs[1].Name, stirs[2].Name)
	}
	if !stirs[0].Woke() || !stirs[1].Woke() {
		t.Fatal("deep and mid visibly woke")
	}
	if stirs[2].Woke() {
		t.Fatal("busy never visibly slept, so it did not wake")
	}
}

func TestSinceLastVisitClockSkewIsSafe(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	evs := pulseRepo("/r/winterwell", "winterwell", []time.Time{now.Add(-200 * day), now.Add(-2 * day)}, 3)

	// A lastOpened at or past now means the clock moved backwards: pulse
	// nothing rather than everything.
	if got := SinceLastVisit(evs, now, now); got != nil {
		t.Fatalf("lastOpened == now must be quiet, got %d stirs", len(got))
	}
	if got := SinceLastVisit(evs, now.Add(time.Hour), now); got != nil {
		t.Fatalf("a future lastOpened must be quiet, got %d stirs", len(got))
	}

	skewed := pulseRepo("/r/tomorrow", "tomorrow", []time.Time{now.Add(30 * day)}, 1)
	stirs := SinceLastVisit(skewed, now.Add(-10*day), now)
	if len(stirs) != 0 {
		t.Fatalf("a future-dated commit must wait for the wall clock: %+v", stirs)
	}
}

func TestSinceLastVisitSkipsMonuments(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	lastOpened := now.Add(-30 * day)
	evs := pulseRepo("/r/kept", "kept", []time.Time{now.Add(-200 * day), now.Add(-2 * day)}, 3)
	evs = append(evs, events.Event{Kind: events.KindFinish, Repo: "/r/kept", TS: now.Add(-100 * day)})
	if got := SinceLastVisit(evs, lastOpened, now); len(got) != 0 {
		t.Fatalf("a monument stands, it does not wake: %d stirs", len(got))
	}
	// The quiet reverse restores it to ordinary waking life.
	evs = append(evs, events.Event{Kind: events.KindUnfinish, Repo: "/r/kept", TS: now.Add(-50 * day)})
	if got := SinceLastVisit(evs, lastOpened, now); len(got) != 1 {
		t.Fatalf("an unfinished town wakes again: %d stirs", len(got))
	}
}

func TestSinceLastVisitCountsTagsAndNewTowns(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	lastOpened := now.Add(-30 * day)

	// A release staked while away is a change even with no new commits.
	tagged := pulseRepo("/r/staked", "staked", []time.Time{now.Add(-40 * day)}, 2)
	tagged = append(tagged, events.Event{Kind: events.KindTag, Repo: "/r/staked", TS: now.Add(-5 * day), Name: "v1.0"})
	stirs := SinceLastVisit(tagged, lastOpened, now)
	if len(stirs) != 1 || stirs[0].NewTags != 1 || stirs[0].NewCommits != 0 {
		t.Fatalf("a fresh stake should stir: %+v", stirs)
	}

	// A town planted while away is news too; having never slept, its depths
	// stay zero.
	planted := pulseRepo("/r/sapling", "sapling", []time.Time{now.Add(-3 * day)}, 4)
	stirs = SinceLastVisit(planted, lastOpened, now)
	if len(stirs) != 1 || stirs[0].NewCommits != 4 {
		t.Fatalf("a new town should stir: %+v", stirs)
	}
	if st := stirs[0]; st.WakeDepth != 0 || st.Woke() {
		t.Fatalf("a town planted while away never slept: %+v", st)
	}
}

func TestTouchLastOpenedPersists(t *testing.T) {
	t.Setenv("AGENTFOREST_HOME", t.TempDir())
	a, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !a.Settings.LastOpened.IsZero() {
		t.Fatal("a fresh home has never been opened")
	}
	ts := time.Date(2026, 7, 3, 8, 30, 0, 0, time.UTC)
	if err := a.TouchLastOpened(ts); err != nil {
		t.Fatal(err)
	}
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !b.Settings.LastOpened.Equal(ts) {
		t.Fatalf("the stamp did not round-trip: %v", b.Settings.LastOpened)
	}
}

func TestTouchLastOpenedPreservesDiskSettings(t *testing.T) {
	dir := t.TempDir()
	a := &App{
		Dir: dir,
		Settings: &store.Settings{
			Roots:    []string{"/old-root"},
			Excludes: []string{"/old-hidden"},
			Finished: []string{"/old-finished"},
		},
	}
	if err := store.SaveSettings(dir, a.Settings); err != nil {
		t.Fatal(err)
	}

	fresh := &store.Settings{
		Roots:    []string{"/fresh-root"},
		Excludes: []string{"/fresh-hidden"},
		Finished: []string{"/fresh-finished"},
	}
	if err := store.SaveSettings(dir, fresh); err != nil {
		t.Fatal(err)
	}

	original := a.Settings
	ts := time.Date(2026, 7, 3, 9, 45, 0, 0, time.UTC)
	if err := a.TouchLastOpened(ts); err != nil {
		t.Fatal(err)
	}
	if a.Settings != original {
		t.Fatal("TouchLastOpened replaced the shared settings pointer")
	}
	if !a.Settings.LastOpened.Equal(ts) {
		t.Fatalf("in-memory stamp was not updated: %v", a.Settings.LastOpened)
	}

	got, found, err := store.LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("settings file disappeared")
	}
	if !reflect.DeepEqual(got.Roots, fresh.Roots) {
		t.Fatalf("roots were overwritten: got %v want %v", got.Roots, fresh.Roots)
	}
	if !reflect.DeepEqual(got.Excludes, fresh.Excludes) {
		t.Fatalf("excludes were overwritten: got %v want %v", got.Excludes, fresh.Excludes)
	}
	if !reflect.DeepEqual(got.Finished, fresh.Finished) {
		t.Fatalf("finished settings were overwritten: got %v want %v", got.Finished, fresh.Finished)
	}
	if !got.LastOpened.Equal(ts) {
		t.Fatalf("disk stamp was not updated: %v", got.LastOpened)
	}
}
