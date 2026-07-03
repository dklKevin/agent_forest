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
// on demand - one frame per decay stage, a settlement with every building form,
// a finished monument, a lone first town, and the empty forest. Each fixture
// pins the composed town (hearth cabin, name board, grove, and the ground /
// grass / trail that key off decay) at a moderate width so the frame stays
// purpose-built rather than giant.
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

// TestFinishedMonumentGolden pins a finished settlement: the carved name board,
// the kept buildings that never decay, and the monument dressing.
func TestFinishedMonumentGolden(t *testing.T) {
	w := forest.Build(lcSeed, []*model.Town{settlementTown("harborfold", true, fixedNow)})
	renderFixture(t, "finished_monument", w, "harborfold")
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
