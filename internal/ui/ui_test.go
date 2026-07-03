package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklKevin/agentforest/internal/app"
	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/store"
)

// uiTown is a single in-memory town: no repo path, so the finishing paths
// exercise their demo branch and never touch disk.
func uiTown(name string, finished bool, epitaph string, now time.Time) *model.Town {
	rs := &events.RepoState{Name: name, Mix: map[string]float64{"go": 1}}
	rs.TotalCommits = 120
	rs.FirstTS = now.Add(-2 * 365 * 24 * time.Hour)
	rs.LastTS = now
	rs.Epitaph = epitaph
	return model.NewTown(rs, finished)
}

func uiRepoTown(name, path string, finished bool, epitaph string, now time.Time) *model.Town {
	t := uiTown(name, finished, epitaph, now)
	t.Path = path
	return t
}

func uiModel(t *testing.T, town *model.Town) Model {
	t.Helper()
	m := New(Config{World: forest.Build(5, []*model.Town{town}), Demo: true})
	m.w, m.h = 120, 40
	m.canv = canvas.New(m.w, m.h, canvas.NoColor)
	m.ready = true
	m.now = time.Now()
	m.focus = m.world.Sites[0]
	return m
}

func persistedUIModel(t *testing.T, town *model.Town, a *app.App) Model {
	t.Helper()
	m := New(Config{World: forest.Build(5, []*model.Town{town}), App: a})
	m.w, m.h = 120, 40
	m.canv = canvas.New(m.w, m.h, canvas.NoColor)
	m.ready = true
	m.now = time.Now()
	m.focus = m.world.Sites[0]
	return m
}

func appWithUnwritableDir(t *testing.T) *app.App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &app.App{Dir: path, Settings: &store.Settings{}}
}

func press(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	mm, _ := m.Update(msg)
	return mm.(Model)
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// The carved words live in inspect and only in inspect: quoted under the
// stage line while the town stands finished, absent the moment it does not.
func TestInspectShowsEpitaphOnlyWhenFinished(t *testing.T) {
	now := time.Now()
	m := uiModel(t, uiTown("keepsake", true, "shipped the thing", now))
	m.mode = inspect
	if out := m.View(); !strings.Contains(out, `"shipped the thing"`) {
		t.Fatalf("inspect does not carry the carved words:\n%s", out)
	}
	m = uiModel(t, uiTown("keepsake", false, "shipped the thing", now))
	m.mode = inspect
	if strings.Contains(m.View(), `"shipped the thing"`) {
		t.Fatal("an unfinished town must not display an epitaph")
	}
}

// f is a threshold, never a toggle: it opens the panel, the line editor caps
// the carving, enter begins the passage, and only the passage's end leaves
// the town standing finished.
func TestFinishThresholdAndCeremony(t *testing.T) {
	m := uiModel(t, uiTown("keepsake", false, "", time.Now()))
	m = press(t, m, runes("f"))
	if m.mode != confirmFinish {
		t.Fatalf("f should open the threshold, mode=%v", m.mode)
	}
	if m.focus.Town.Finished {
		t.Fatal("f must not toggle finished directly")
	}

	m = press(t, m, runes(strings.Repeat("x", 60)))
	if n := len([]rune(m.epitaph)); n != app.EpitaphMaxRunes {
		t.Fatalf("the carving was not capped: %d runes", n)
	}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != ceremony || m.ceremonyAnim == nil {
		t.Fatal("enter should begin the ceremony")
	}
	town := m.focus.Town
	if town.Finished {
		t.Fatal("the town must not stand finished before the passage ends")
	}
	if town.Epitaph != strings.Repeat("x", app.EpitaphMaxRunes) {
		t.Fatalf("the carving was lost: %q", town.Epitaph)
	}

	// Midway the carve is partial: a passage, not a snap.
	m.ceremonyAnim.start = time.Now().Add(-finishDur / 2)
	m.stepCeremony()
	if c := town.Carve(); c <= 0 || c >= 1 {
		t.Fatalf("mid-ceremony carve = %v, want strictly between 0 and 1", c)
	}

	// Past the end the monument stands, every override cleared.
	m.ceremonyAnim.start = time.Now().Add(-2 * finishDur)
	m.stepCeremony()
	if m.ceremonyAnim != nil || m.mode != roam {
		t.Fatal("the ceremony did not complete")
	}
	if !town.Finished || town.CarveOverride != nil || town.IdleOverride != nil {
		t.Fatalf("the monument does not stand clean: finished=%v", town.Finished)
	}
	if town.Carve() != 1 {
		t.Fatalf("a finished town must carve 1, got %v", town.Carve())
	}
}

// esc at the threshold keeps tending: nothing changes, nothing is carved.
func TestFinishThresholdEscKeepsTending(t *testing.T) {
	m := uiModel(t, uiTown("keepsake", false, "", time.Now()))
	m = press(t, m, runes("f"))
	m = press(t, m, runes("oops"))
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != roam || m.ceremonyAnim != nil {
		t.Fatal("esc should close the threshold quietly")
	}
	if m.focus.Town.Finished || m.focus.Town.Epitaph != "" {
		t.Fatal("esc must leave the town untouched")
	}
}

func TestFinishSaveErrorLeavesTownUntouched(t *testing.T) {
	town := uiRepoTown("keepsake", "/repos/keepsake", false, "old words", time.Now())
	idle := 42 * time.Hour
	town.IdleOverride = &idle
	m := persistedUIModel(t, town, appWithUnwritableDir(t))
	m.mode = confirmFinish
	m.epitaph = "new words"

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != confirmFinish || m.ceremonyAnim != nil {
		t.Fatal("failed finish must not start the ceremony")
	}
	if !strings.Contains(m.status, "could not save") {
		t.Fatalf("missing save failure toast, got %q", m.status)
	}
	if town.Finished || town.Epitaph != "old words" || !town.FinishTS.IsZero() {
		t.Fatalf("failed finish changed town state: finished=%v epitaph=%q finishTS=%v", town.Finished, town.Epitaph, town.FinishTS)
	}
	if town.IdleOverride != &idle {
		t.Fatal("failed finish cleared idle state")
	}
	if m.epitaph != "new words" {
		t.Fatalf("failed finish lost the in-progress epitaph: %q", m.epitaph)
	}
}

// Un-finishing is the quiet reverse: no panel, no ceremony, the carved words
// kept, and the hearth lit again.
func TestUnfinishIsQuiet(t *testing.T) {
	m := uiModel(t, uiTown("keepsake", true, "words to keep", time.Now()))
	m = press(t, m, runes("f"))
	if m.mode != roam || m.ceremonyAnim != nil {
		t.Fatal("unfinish must be a quiet reverse, not a ceremony")
	}
	town := m.focus.Town
	if town.Finished {
		t.Fatal("unfinish did not reverse")
	}
	if town.Epitaph != "words to keep" {
		t.Fatal("unfinish erased the carving")
	}
	if !strings.Contains(m.status, "the hearth is lit again") {
		t.Fatalf("missing the quiet line, got %q", m.status)
	}
}

func TestUnfinishSaveErrorLeavesTownFinished(t *testing.T) {
	town := uiRepoTown("keepsake", "/repos/keepsake", true, "words to keep", time.Now())
	m := persistedUIModel(t, town, appWithUnwritableDir(t))

	m = press(t, m, runes("f"))

	if m.mode != roam || m.ceremonyAnim != nil {
		t.Fatal("failed unfinish must not start another mode")
	}
	if !strings.Contains(m.status, "could not save") {
		t.Fatalf("missing save failure toast, got %q", m.status)
	}
	if !town.Finished {
		t.Fatal("failed unfinish changed the town")
	}
	if town.Epitaph != "words to keep" {
		t.Fatal("failed unfinish erased the carving")
	}
}
