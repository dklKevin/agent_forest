// Package render turns a laid-out world into a single printable frame. It is
// the one seam the CLI snapshot path and the golden tests share, so the whole
// composed scene is exercised through one function instead of being welded to
// os.Stdout inside main.
//
// The frame is a pure function of the world and the options, and crucially of
// Now: the caller owns the clock. Passing a fixed instant makes the output
// deterministic (and keeps the future day/night atmosphere testable), which is
// what the CLI's hidden --now flag threads through.
package render

import (
	"fmt"
	"time"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/forest"
)

// SnapshotOpts configures a single-frame render.
type SnapshotOpts struct {
	Width, Height int            // canvas size in cells
	At            string         // center on this town; empty centers on the spot
	T             float64        // wind phase seconds
	Now           time.Time      // reference instant: tend, decay, age, and the lantern key off it
	Profile       canvas.Profile // color emission (NoColor for shape-only goldens)
}

// RenderSnapshot paints one frame of the world and returns it as a string.
//
// When At names a town not present in the world it returns an error whose
// message is the plain reason; the caller formats any help line from
// w.Sites. When At is empty the frame centers on the most recently tended
// town, or the west edge when the forest is empty.
func RenderSnapshot(w *forest.World, opts SnapshotOpts) (string, error) {
	c := canvas.New(opts.Width, opts.Height, opts.Profile)
	cam := 0.0
	var focus *forest.Site
	if opts.At != "" {
		for _, s := range w.Sites {
			if s.Town.Name == opts.At {
				cam = float64(s.SignX) - float64(opts.Width)
				focus = s
			}
		}
		if focus == nil {
			return "", fmt.Errorf("no town named %s", opts.At)
		}
	} else if s := w.SpotSite(opts.Now); s != nil {
		cam = float64(s.SignX) - float64(opts.Width)
		focus = s
	}
	if cam < 0 {
		cam = 0
	}
	w.Render(c, forest.Frame{Cam: cam, T: opts.T, Now: opts.Now, Focus: focus, Spot: w.SpotSite(opts.Now)})
	return c.Render(), nil
}
