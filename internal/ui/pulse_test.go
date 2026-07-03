package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/app"
	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/store"
)

const day = 24 * time.Hour

// pulseTown emits one town's life: planted at the first instant, active at
// each, keyed by path the way a real forest keys its log.
func pulseTown(path, name string, commits ...time.Time) []events.Event {
	evs := []events.Event{{Kind: events.KindRepo, Repo: path, TS: commits[0], Path: path, Name: name}}
	for _, ts := range commits {
		evs = append(evs, events.Event{Kind: events.KindActivity, Repo: path, TS: ts, Commits: 3})
	}
	return evs
}

// pulseModel is a real-forest UI over an in-memory log, ready to receive the
// startup scan's completion.
func pulseModel(t *testing.T, evs []events.Event, lastOpened, now time.Time) Model {
	t.Helper()
	a := &app.App{Dir: t.TempDir(), Settings: &store.Settings{}, Events: evs}
	repos := events.Reduce(evs)
	towns := make([]*model.Town, 0, len(repos))
	for _, r := range repos {
		towns = append(towns, model.NewTown(r, r.Finished))
	}
	m := New(Config{World: forest.Build(5, towns), App: a, LastOpened: lastOpened})
	m.w, m.h = 120, 40
	m.canv = canvas.New(m.w, m.h, canvas.NoColor)
	m.ready = true
	m.now = now
	return m
}

func startupDone(t *testing.T, m Model) Model {
	t.Helper()
	mm, _ := m.Update(scanDoneMsg{kind: scanStartup})
	return mm.(Model)
}

// On launch, a town that stirred while the forest was closed wakes from the
// depth it actually woke from, eases to its true present depth, and - once
// the motion fades - leaves the forest exactly as it ordinarily stands. An
// unchanged neighbor never moves.
func TestStartupPulseWakesStirredTowns(t *testing.T) {
	now := time.Now()
	evs := pulseTown("/r/winterwell", "winterwell", now.Add(-400*day), now.Add(-2*day))
	evs = append(evs, pulseTown("/r/mossjar", "mossjar", now.Add(-300*day), now.Add(-40*day))...)
	m := pulseModel(t, evs, now.Add(-30*day), now)

	m = startupDone(t, m)

	anim := m.revives["/r/winterwell"]
	if anim == nil {
		t.Fatal("the stirred town did not pulse")
	}
	if _, ok := m.revives["/r/mossjar"]; ok {
		t.Fatal("an unchanged town must not pulse")
	}
	if anim.from < 0.9 {
		t.Fatalf("a 400-day sleep should wake from deep reclamation, from=%v", anim.from)
	}
	if anim.to > 0.05 {
		t.Fatalf("the pulse should ease into the town's true, tended depth, to=%v", anim.to)
	}
	if m.status != "winterwell stirred while you were away" {
		t.Fatalf("the most notable waking gets its one soft line, got %q", m.status)
	}

	// After the motion fades the forest is exactly the normal forest.
	anim.start = time.Now().Add(-2 * reviveDur)
	m.stepRevives()
	if len(m.revives) != 0 {
		t.Fatal("the pulse did not fade on its own")
	}
	if s := m.siteByPath("/r/winterwell"); s.Town.IdleOverride != nil {
		t.Fatal("a faded pulse must leave no override behind")
	}
}

// A first run - and an upgrade from a build without the stamp - shows no
// pulse at all.
func TestStartupPulseQuietOnFirstRun(t *testing.T) {
	now := time.Now()
	evs := pulseTown("/r/winterwell", "winterwell", now.Add(-400*day), now.Add(-2*day))
	m := pulseModel(t, evs, time.Time{}, now)

	m = startupDone(t, m)

	if len(m.revives) != 0 || m.status != "" {
		t.Fatalf("a first run must be quiet: %d pulses, status %q", len(m.revives), m.status)
	}
}

// A long absence wakes a handful of the most notable towns, never the whole
// forest: the deepest sleeps pulse, the shallowest wait for their day.
func TestStartupPulseCapsSimultaneousWakes(t *testing.T) {
	now := time.Now()
	var evs []events.Event
	for i := 1; i <= 8; i++ {
		path := fmt.Sprintf("/r/town%d", i)
		name := fmt.Sprintf("town%d", i)
		evs = append(evs, pulseTown(path, name, now.Add(-time.Duration(i)*50*day), now.Add(-2*day))...)
	}
	m := pulseModel(t, evs, now.Add(-30*day), now)

	m = startupDone(t, m)

	if len(m.revives) != maxPulses {
		t.Fatalf("pulses = %d, want the cap of %d", len(m.revives), maxPulses)
	}
	for i := 4; i <= 8; i++ {
		if _, ok := m.revives[fmt.Sprintf("/r/town%d", i)]; !ok {
			t.Fatalf("town%d slept deep and should be among the pulses", i)
		}
	}
}

// A town that was bright the whole time still visibly stirs - from the floor
// depth, so the smoke swells back - but it earns no toast: motion for every
// change, words only for a waking.
func TestStartupPulseFloorsBrightTownsWithoutAToast(t *testing.T) {
	now := time.Now()
	evs := pulseTown("/r/steady", "steady", now.Add(-100*day), now.Add(-31*day), now.Add(-29*day), now.Add(-2*day))
	m := pulseModel(t, evs, now.Add(-30*day), now)

	m = startupDone(t, m)

	anim := m.revives["/r/steady"]
	if anim == nil {
		t.Fatal("a bright town that gained commits still stirs")
	}
	if anim.from != pulseFloor {
		t.Fatalf("a bright town wakes from the floor, from=%v", anim.from)
	}
	if m.status != "" {
		t.Fatalf("a town that never slept earns no toast, got %q", m.status)
	}
}

func TestStartupPulseSkipsTagOnlyStirs(t *testing.T) {
	now := time.Now()
	evs := pulseTown("/r/staked", "staked", now.Add(-25*time.Hour))
	evs = append(evs, events.Event{Kind: events.KindTag, Repo: "/r/staked", TS: now.Add(-time.Hour), Name: "v1.0"})
	m := pulseModel(t, evs, now.Add(-24*time.Hour), now)

	m = startupDone(t, m)

	if len(m.revives) != 0 || m.status != "" {
		t.Fatalf("tag-only stirs must not pulse or toast: %d pulses, status %q", len(m.revives), m.status)
	}
}

// Staggered pulses hold their depth until their moment arrives, so the towns
// wake one after another instead of blinking together.
func TestStartupPulseHoldsUntilItsMoment(t *testing.T) {
	now := time.Now()
	evs := pulseTown("/r/winterwell", "winterwell", now.Add(-400*day), now.Add(-2*day))
	m := pulseModel(t, evs, now.Add(-30*day), now)
	m = startupDone(t, m)

	anim := m.revives["/r/winterwell"]
	if anim == nil {
		t.Fatal("the stirred town did not pulse")
	}
	anim.start = time.Now().Add(time.Hour)
	m.stepRevives()
	town := m.siteByPath("/r/winterwell").Town
	if town.IdleOverride == nil {
		t.Fatal("a waiting pulse must hold the town at its sleeping depth")
	}
	if want := model.IdleForDecay(anim.from); *town.IdleOverride != want {
		t.Fatalf("held depth = %v, want %v", *town.IdleOverride, want)
	}
}

// The launch toast can land while the wander hint still owns the bottom
// line; the toast takes the line whole so the two never interleave.
func TestToastTakesTheBottomLineFromTheHint(t *testing.T) {
	m := uiModel(t, uiTown("keepsake", false, "", time.Now()))
	if !m.hint {
		t.Fatal("a fresh model shows the hint")
	}
	m.toast("keepsake stirred while you were away")
	out := m.View()
	if !strings.Contains(out, "stirred while you were away") {
		t.Fatalf("the toast is missing:\n%s", out)
	}
	if strings.Contains(out, "wander") {
		t.Fatalf("the hint bled through the toast:\n%s", out)
	}
}
