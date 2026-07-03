package render_test

import (
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/demo"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/goldentest"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/render"
)

// fixedNow owns the clock for every snapshot golden. The demo builds its whole
// event log relative to now, so decay and age depend only on this instant - the
// same discipline the CLI's --now flag threads through. Pinning it here keeps
// the goldens deterministic today and survivable when day/night atmosphere
// later keys off absolute time.
var fixedNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const (
	snapSeed = 5
	snapW    = 160
	snapH    = 42
	snapT    = 2.5
)

// demoWorld builds the demo forest exactly as the CLI does, but with a pinned
// reference instant.
func demoWorld(seed uint64, now time.Time) *forest.World {
	repos := events.Reduce(demo.Events(seed, now))
	finished := demo.FinishedNames()
	var towns []*model.Town
	for _, r := range repos {
		towns = append(towns, model.NewTown(r, finished[r.Name]))
	}
	return forest.Build(seed, towns)
}

// TestDemoSnapshotGolden pins the composed world: the layout, depth-plane
// compositing, camera culling, ground/sky/ridge passes, and decay influence
// that the gallery sheets never exercise. A handful of representative frames,
// each centered on a town that shows a different life stage.
func TestDemoSnapshotGolden(t *testing.T) {
	w := demoWorld(snapSeed, fixedNow)
	cases := []struct {
		name string
		at   string // empty centers on the most recently tended town
	}{
		{"demo_winterwell", "winterwell"}, // the thriving settlement
		{"demo_mothgate", "mothgate"},     // the ancient finished village
		{"demo_mossjar", "mossjar"},       // the lone sapling hut
		{"demo_spot", ""},                 // default camera: the lantern's town
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := render.RenderSnapshot(w, render.SnapshotOpts{
				Width: snapW, Height: snapH, At: tc.at, T: snapT,
				Now: fixedNow, Profile: canvas.NoColor,
			})
			if err != nil {
				t.Fatal(err)
			}
			goldentest.Assert(t, tc.name, got)
		})
	}
}

// TestSnapshotUnknownTown confirms the seam reports a missing town rather than
// rendering a stray frame.
func TestSnapshotUnknownTown(t *testing.T) {
	w := demoWorld(snapSeed, fixedNow)
	if _, err := render.RenderSnapshot(w, render.SnapshotOpts{
		Width: snapW, Height: snapH, At: "nowheretown", T: snapT,
		Now: fixedNow, Profile: canvas.NoColor,
	}); err == nil {
		t.Fatal("expected an error for a town that does not exist")
	}
}
