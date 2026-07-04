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
	"github.com/dklKevin/agentforest/internal/demo"
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

// The planted line must show the town's real first-commit month. A Go time
// layout of "january 2006" treats the month as literal text, which once froze
// every town's planted date to january; this locks the fixed rendering.
func TestInspectShowsActualPlantedMonth(t *testing.T) {
	town := uiTown("forge", false, "", time.Now())
	town.FirstTS = time.Date(2026, 4, 9, 9, 0, 0, 0, time.UTC)
	m := uiModel(t, town)
	m.mode = inspect

	view := m.View()
	if !strings.Contains(view, "planted april 2026") {
		t.Fatalf("inspect did not show the real planted month:\n%s", view)
	}
	if strings.Contains(view, "planted january") {
		t.Fatalf("inspect showed the literal layout month:\n%s", view)
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

// almanacModel builds the UI over a world folded from a real event set, the
// way main.go does for the demo, so the almanac page has a log to read.
func almanacModel(t *testing.T, evs []events.Event, now time.Time) Model {
	t.Helper()
	repos := events.Reduce(evs)
	towns := make([]*model.Town, 0, len(repos))
	for _, r := range repos {
		towns = append(towns, model.NewTown(r, r.Finished))
	}
	m := New(Config{World: forest.Build(5, towns), Demo: true, Events: evs})
	m.w, m.h = 120, 40
	m.canv = canvas.New(m.w, m.h, canvas.NoColor)
	m.ready = true
	m.now = now
	m.focus = m.world.Sites[0]
	return m
}

// The almanac is one deliberate keypress past inspect: a does nothing on the
// map, opens the memoir from inspect, and steps back to inspect from there.
func TestAlmanacIsASecondKeypressFromInspect(t *testing.T) {
	m := uiModel(t, uiTown("keepsake", false, "", time.Now()))
	m = press(t, m, runes("a"))
	if m.mode != roam {
		t.Fatalf("a on the map must do nothing, mode=%v", m.mode)
	}
	m = press(t, m, runes("i"))
	m = press(t, m, runes("a"))
	if m.mode != almanacView {
		t.Fatalf("a from inspect should open the almanac, mode=%v", m.mode)
	}
	m = press(t, m, runes("a"))
	if m.mode != inspect {
		t.Fatalf("a from the almanac should step back to inspect, mode=%v", m.mode)
	}
	m = press(t, m, runes("a"))
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != roam {
		t.Fatalf("esc should leave the almanac for the forest, mode=%v", m.mode)
	}
}

// The memoir tells the life from the log: planted, the settlement, the
// stake, the long quiet - terse lines, not a stats table.
func TestAlmanacTellsTheLife(t *testing.T) {
	planted := time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)
	sleep := planted.AddDate(0, 2, 0)
	wake := sleep.Add(426 * 24 * time.Hour)
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "keepsake", TS: planted, Path: "~/demo/keepsake"},
		{Kind: events.KindActivity, Repo: "keepsake", TS: planted, Commits: 3},
		{Kind: events.KindActivity, Repo: "keepsake", TS: sleep, Commits: 2},
		{Kind: events.KindActivity, Repo: "keepsake", TS: wake, Commits: 1},
		{Kind: events.KindTag, Repo: "keepsake", TS: wake, Name: "v1.0"},
		{Kind: events.KindComp, Repo: "keepsake", TS: wake, Path: "engine", Name: "engine", Bytes: 100, Files: 5},
	}
	m := almanacModel(t, evs, wake.Add(time.Hour))
	m.mode = almanacView
	out := m.View()
	for _, want := range []string{
		"almanac · keepsake",
		"planted march 2018",
		"a settlement by",
		"v1.0 staked",
		"quiet for 14 months, then woke",
		"tended still · the grove is bright",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("almanac page missing %q:\n%s", want, out)
		}
	}
}

// For a finished town the carved words lead, with the life-in-brief under
// them, before any chapter.
func TestAlmanacEpitaphLeads(t *testing.T) {
	planted := time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)
	kept := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "keepsake", TS: planted, Path: "~/demo/keepsake"},
		{Kind: events.KindActivity, Repo: "keepsake", TS: planted, Commits: 3},
		{Kind: events.KindFinish, Repo: "keepsake", TS: kept, Epitaph: "shipped the thing"},
	}
	m := almanacModel(t, evs, kept.AddDate(0, 1, 0))
	m.mode = almanacView
	out := m.View()
	epitaph := strings.Index(out, `"shipped the thing"`)
	brief := strings.Index(out, "planted 2018, kept 2026")
	chapter := strings.Index(out, "planted march 2018")
	rest := strings.Index(out, "laid to rest june 2026 · the monument stands")
	if epitaph < 0 || brief < 0 || chapter < 0 || rest < 0 {
		t.Fatalf("almanac page misses the monument story (%d %d %d %d):\n%s", epitaph, brief, chapter, rest, out)
	}
	if !(epitaph < brief && brief < chapter && chapter < rest) {
		t.Fatal("the carved words and the life-in-brief must lead the memoir")
	}
}

func TestDemoAlmanacReadsFinishedCastState(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	evs := demo.Events(5, now)
	repos := events.Reduce(evs)
	towns := make([]*model.Town, 0, len(repos))
	for _, r := range repos {
		towns = append(towns, model.NewTown(r, r.Finished))
	}
	m := New(Config{World: forest.Build(5, towns), Demo: true, Events: evs})
	m.w, m.h = 120, 40
	m.canv = canvas.New(m.w, m.h, canvas.NoColor)
	m.ready = true
	m.now = now
	for _, s := range m.world.Sites {
		if s.Town.Name == "mothgate" {
			m.focus = s
			break
		}
	}
	if m.focus == nil {
		t.Fatal("mothgate not found")
	}
	if !m.focus.Town.Finished {
		t.Fatal("mothgate should inspect as finished")
	}
	m.mode = almanacView
	if out := m.View(); !strings.Contains(out, "the monument stands") {
		t.Fatalf("demo almanac did not read monument state:\n%s", out)
	}
}

func TestDemoAlmanacReflectsInSessionFinish(t *testing.T) {
	now := time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "keepsake", TS: now, Path: "~/demo/keepsake"},
		{Kind: events.KindActivity, Repo: "keepsake", TS: now, Commits: 3},
	}
	m := almanacModel(t, evs, now.Add(time.Hour))
	m.mode = confirmFinish
	m.epitaph = "kept words"
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.ceremonyAnim == nil {
		t.Fatal("demo finish did not start the ceremony")
	}
	m.ceremonyAnim.start = time.Now().Add(-2 * finishDur)
	m.stepCeremony()
	m.mode = almanacView
	out := m.View()
	if !strings.Contains(out, `"kept words"`) || !strings.Contains(out, "the monument stands") {
		t.Fatalf("demo almanac did not read in-session finish:\n%s", out)
	}
}

func TestDemoAlmanacReflectsInSessionUnfinish(t *testing.T) {
	planted := time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)
	kept := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "keepsake", TS: planted, Path: "~/demo/keepsake"},
		{Kind: events.KindActivity, Repo: "keepsake", TS: planted, Commits: 3},
		{Kind: events.KindFinish, Repo: "keepsake", TS: kept, Epitaph: "kept words"},
	}
	m := almanacModel(t, evs, kept.Add(time.Hour))
	if !m.focus.Town.Finished {
		t.Fatal("test town should start finished")
	}
	m = press(t, m, runes("f"))
	if m.focus.Town.Finished {
		t.Fatal("demo unfinish did not change inspect state")
	}
	m.mode = almanacView
	out := m.View()
	if strings.Contains(out, `"kept words"`) || strings.Contains(out, "the monument stands") {
		t.Fatalf("demo almanac did not read in-session unfinish:\n%s", out)
	}
}

// A world with no log behind it (never the shipped wiring) still opens a
// quiet page instead of panicking.
func TestAlmanacWithoutALogStaysQuiet(t *testing.T) {
	m := uiModel(t, uiTown("keepsake", false, "", time.Now()))
	m.mode = almanacView
	if out := m.View(); !strings.Contains(out, "the log holds no story yet") {
		t.Fatalf("logless almanac page should say so:\n%s", out)
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
