package render_test

import (
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/goldentest"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/render"
)

// Layer C: in-process goldens over hand-built fixtures. Unlike the demo
// snapshots these pin specific, deliberately-chosen states the demo cannot hit
// on demand - one frame per decay stage, one frame per tended mood, a mixed
// settlement mood, a settlement with every building form, a finished monument,
// a lone first town, and the empty forest. Each fixture pins the composed town
// (hearth cabin, name board, grove, and the ground / grass / trail that key
// off tend and decay) at a moderate width so the frame stays purpose-built
// rather than giant.
const (
	lcW    = 120
	lcH    = 40
	lcSeed = 5
)

// plainTown is a single repository with no components: just a hearth cabin, a
// name board, and a grove. commits pick the hearth tier, ageYears the stature,
// and d the reclamation depth (via the exact inverse of the decay curve). The
// go mix pins the species to an oak so the silhouette never drifts.
func plainTown(name string, commits int, ageYears, d float64, now time.Time) *model.Town {
	rs := &events.RepoState{Name: name, Mix: map[string]float64{"go": 1.0}}
	rs.TotalCommits = commits
	rs.FirstTS = now.Add(-time.Duration(ageYears * 365 * 24 * float64(time.Hour)))
	rs.LastTS = now.Add(-model.IdleForDecay(d))
	return model.NewTown(rs, false)
}

// settlementTown is a town whose components span every building form: a barn
// (the first code component), a homeplace, a workshop, a couple of minor
// sheds/cribs, a watchtower (tests), and a schoolhouse (docs).
func settlementTown(name string, finished bool, now time.Time) *model.Town {
	rs := &events.RepoState{Name: name, Mix: map[string]float64{"go": 1.0}}
	rs.TotalCommits = 3000
	rs.FirstTS = now.Add(-6 * 365 * 24 * time.Hour)
	rs.LastTS = now
	comp := func(name string, bytes int64, files int) *events.ComponentState {
		return &events.ComponentState{Name: name, Path: name, Bytes: bytes, Files: files, LastTS: now}
	}
	rs.Components = map[string]*events.ComponentState{
		"engine": comp("engine", 1<<20, 60),   // barn: the dominant code component
		"server": comp("server", 700<<10, 40), // homeplace: share >= 0.45
		"cli":    comp("cli", 300<<10, 20),    // workshop: share >= 0.15
		"assets": comp("assets", 60<<10, 10),  // shed or crib: minor
		"proto":  comp("proto", 40<<10, 8),    // shed or crib: minor
		"tests":  comp("tests", 200<<10, 30),  // watchtower
		"docs":   comp("docs", 120<<10, 15),   // schoolhouse
	}
	return model.NewTown(rs, finished)
}

func renderFixture(t *testing.T, name string, w *forest.World, at string) {
	t.Helper()
	got, err := render.RenderSnapshot(w, render.SnapshotOpts{
		Width: lcW, Height: lcH, At: at, T: snapT,
		Now: fixedNow, Profile: canvas.NoColor,
	})
	if err != nil {
		t.Fatal(err)
	}
	goldentest.Assert(t, name, got)
}

// TestDecayStagesGolden pins one composed-town frame per named decay stage, so
// a change to cabin weathering, sign tilt, grove regrowth, or the ground's
// decay influence shows up as a diff at exactly the stage it touched.
func TestDecayStagesGolden(t *testing.T) {
	stages := []struct {
		name string
		d    float64
	}{
		{"town_tended", 0.0},
		{"town_first_quiet", 0.15},
		{"town_overgrown", 0.37},
		{"town_breaking", 0.62},
		{"town_skeletal", 0.85},
		{"town_ruins", 0.965},
	}
	for _, st := range stages {
		t.Run(st.name, func(t *testing.T) {
			town := plainTown("cedarhold", 120, 4, st.d, fixedNow)
			w := forest.Build(lcSeed, []*model.Town{town})
			renderFixture(t, st.name, w, "cedarhold")
		})
	}
}

// TestSettlementFormsGolden pins a settlement carrying every building form.
func TestSettlementFormsGolden(t *testing.T) {
	w := forest.Build(lcSeed, []*model.Town{settlementTown("harborfold", false, fixedNow)})
	renderFixture(t, "settlement_forms", w, "harborfold")
}

// moodTown is a single repository pinned at a fixed idle offset from now, so
// each golden captures one grade of the tended-side mood curve directly.
func moodTown(name string, idleDays float64, now time.Time) *model.Town {
	rs := &events.RepoState{Name: name, Mix: map[string]float64{"go": 1.0}}
	rs.TotalCommits = 120
	rs.FirstTS = now.Add(-4 * 365 * 24 * time.Hour)
	rs.LastTS = now.Add(-time.Duration(idleDays * 24 * float64(time.Hour)))
	return model.NewTown(rs, false)
}

// TestTendedMoodsGolden pins one frame per tended-side mood grade, the alive
// counterpart of TestDecayStagesGolden: worked-today (fullest life - the tall
// lively plume, chips at the block, lamplight pooling under the windows, a
// nearly unbroken trail), worked-this-week (a steady wisp, the axe still
// standing), and quiet-but-kept (the last thread of a banked fire, the axe
// set down, the trail back to sparse dashes).
func TestTendedMoodsGolden(t *testing.T) {
	moods := []struct {
		name     string
		idleDays float64
	}{
		{"mood_worked_today", 0.25},
		{"mood_worked_this_week", 4},
		{"mood_quiet_but_kept", 12},
	}
	for _, m := range moods {
		t.Run(m.name, func(t *testing.T) {
			town := moodTown("emberhold", m.idleDays, fixedNow)
			w := forest.Build(lcSeed, []*model.Town{town})
			renderFixture(t, m.name, w, "emberhold")
		})
	}
}

// TestSettlementMoodsGolden pins a settlement whose components sit at
// staggered idles, so every building carries its own mood: the barn bustles
// while the schoolhouse dozes. That differential is the feature at
// settlement scale, and this frame locks it.
func TestSettlementMoodsGolden(t *testing.T) {
	rs := &events.RepoState{Name: "emberline", Mix: map[string]float64{"go": 1.0}}
	rs.TotalCommits = 3000
	rs.FirstTS = fixedNow.Add(-6 * 365 * 24 * time.Hour)
	rs.LastTS = fixedNow.Add(-6 * time.Hour)
	comp := func(name string, bytes int64, files int, idleDays float64) *events.ComponentState {
		return &events.ComponentState{Name: name, Path: name, Bytes: bytes, Files: files,
			LastTS: fixedNow.Add(-time.Duration(idleDays * 24 * float64(time.Hour)))}
	}
	rs.Components = map[string]*events.ComponentState{
		"engine": comp("engine", 1<<20, 60, 0.25), // barn: worked today
		"server": comp("server", 700<<10, 40, 3),  // homeplace: this week
		"cli":    comp("cli", 300<<10, 20, 5),     // workshop: late in the week
		"assets": comp("assets", 60<<10, 10, 10),  // minor: quiet but kept
		"proto":  comp("proto", 40<<10, 8, 60),    // minor: gone quiet
		"tests":  comp("tests", 200<<10, 30, 1),   // watchtower: the watch kept
		"docs":   comp("docs", 120<<10, 15, 30),   // schoolhouse: dozing
	}
	town := model.NewTown(rs, false)
	w := forest.Build(lcSeed, []*model.Town{town})
	renderFixture(t, "settlement_moods", w, "emberline")
}

// TestFinishedMonumentGolden pins a finished settlement: the carved name board,
// the kept buildings that never decay, and the monument dressing.
func TestFinishedMonumentGolden(t *testing.T) {
	w := forest.Build(lcSeed, []*model.Town{settlementTown("harborfold", true, fixedNow)})
	renderFixture(t, "finished_monument", w, "harborfold")
}

// TestFinishCeremonyMidGolden pins one frame from the middle of the
// laying-to-rest passage: the board half stone, the last plume thinning, the
// finial risen, the grove partway into its monument symmetry. It locks the
// carve choreography so the ceremony cannot silently drift into a snap.
func TestFinishCeremonyMidGolden(t *testing.T) {
	town := settlementTown("harborfold", false, fixedNow)
	w := forest.Build(lcSeed, []*model.Town{town})
	carve := 0.55
	town.CarveOverride = &carve
	w.Sites[0].CarveGrove(carve)
	renderFixture(t, "finish_ceremony_mid", w, "harborfold")
}

// TestSingleTownGolden pins the lone first town - a young tended hut in an
// otherwise empty wood. The frame worth locking before that first-forest
// moment is ever redesigned.
func TestSingleTownGolden(t *testing.T) {
	w := forest.Build(lcSeed, []*model.Town{plainTown("mossjar", 12, 0.2, 0, fixedNow)})
	renderFixture(t, "single_town", w, "mossjar")
}

// TestEmptyForestGolden pins the world with no towns at all: wild woods,
// ground, sky, and foreground only. It centers on the west edge (no spot).
func TestEmptyForestGolden(t *testing.T) {
	w := forest.Build(lcSeed, nil)
	renderFixture(t, "empty_forest", w, "")
}
