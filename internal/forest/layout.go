// Package forest lays out and renders the world: one continuous stretch of
// wild woodland, oldest towns to the west, deep old-growth beyond the first
// town and open young dark beyond the last. Nothing here is a tile or a grid;
// every position is jittered, every baseline follows the terrain.
package forest

import (
	"time"

	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Reference viewport height in dots; stature and terrain are designed against
// this and scaled to the real terminal at render time.
const refDotH = 140

// Margins of wild land beyond the first and last towns, in dots.
const (
	westWild = 300
	eastWild = 280
)

// treeMeta is a planted tree: stable identity, live-computed size and decay.
type treeMeta struct {
	seed uint64
	x    int // absolute reference dots
	hMul float64
	back bool
	sp   model.Species
}

// wildKind enumerates the filler that grows between towns.
type wildKind int

const (
	wildScrub wildKind = iota
	wildRock
	wildSnag
	wildOldTree // the deep woods at the western edge
	wildSapling // the young east
)

type wildItem struct {
	seed uint64
	x    int
	h    int
	kind wildKind
	sp   model.Species
}

// Site is a town's place in the world.
type Site struct {
	Town   *model.Town
	X0, X1 int // extent in reference dots
	SignX  int
	trees  []treeMeta
}

// Center is the site's focal x in reference dots.
func (s *Site) Center() int { return s.SignX }

// World is the fully laid out forest.
type World struct {
	Seed  uint64
	Sites []*Site
	wild  []wildItem
	Width int // total reference dots including margins
}

// Build lays out towns west to east with organic spacing, plants their
// groves, and fills the land between with wild growth.
func Build(seed uint64, towns []*model.Town) *World {
	w := &World{Seed: seed}
	cursor := westWild

	for i, t := range towns {
		ts := xnoise.Hash(seed, 0x70, uint64(i))
		n := t.TreeCount()
		front := (n*3 + 4) / 5
		if front < 1 {
			front = 1
		}
		back := n - front

		perTree := 13.0 + xnoise.Unit(ts, 1)*7
		extent := int(float64(front)*perTree) + 30
		site := &Site{Town: t, X0: cursor, X1: cursor + extent}
		site.SignX = cursor + extent/2

		if t.Finished {
			site.trees = plantMonumentGrove(ts, t, cursor, extent, front, back)
		} else {
			site.trees = plantWildGrove(ts, t, cursor, extent, front, back)
		}
		w.Sites = append(w.Sites, site)

		gap := 80 + int(xnoise.Unit(ts, 2)*170)
		if xnoise.Unit(ts, 3) < 0.18 {
			gap += 190 // an occasional long wilderness walk
		}
		cursor += extent + gap
	}
	w.Width = cursor + eastWild
	w.plantWilderness()
	return w
}

// plantWildGrove scatters a town's trees with jittered spacing and varied
// heights: a real grove, denser and taller near its heart.
func plantWildGrove(ts uint64, t *model.Town, x0, extent, front, back int) []treeMeta {
	var trees []treeMeta
	place := func(i, n int, isBack bool) {
		fi := 0.0
		if n > 1 {
			fi = float64(i)/float64(n-1)*2 - 1 // -1..1 across extent
		}
		x := x0 + 15 + int((fi*0.5+0.5)*float64(extent-30))
		x += int(xnoise.Range(ts, -7, 7, 0x11, uint64(i), boolKey(isBack)))
		center := 1 - 0.35*fi*fi // taller near the middle
		mul := center * xnoise.Range(ts, 0.68, 1.04, 0x12, uint64(i), boolKey(isBack))
		if isBack {
			mul *= 0.72
		}
		trees = append(trees, treeMeta{
			seed: xnoise.Hash(ts, 0x13, uint64(i), boolKey(isBack)),
			x:    x, hMul: mul, back: isBack, sp: t.Species,
		})
	}
	for i := 0; i < back; i++ {
		place(i, back, true)
	}
	for i := 0; i < front; i++ {
		place(i, front, false)
	}
	return trees
}

// plantMonumentGrove is the one place order is allowed: a finished town's
// trees stand in calm symmetry, tallest at the center. Human intent, held.
func plantMonumentGrove(ts uint64, t *model.Town, x0, extent, front, back int) []treeMeta {
	var trees []treeMeta
	place := func(i, n int, isBack bool) {
		fi := 0.0
		if n > 1 {
			fi = float64(i)/float64(n-1)*2 - 1
		}
		x := x0 + 15 + int((fi*0.5+0.5)*float64(extent-30))
		mul := 1 - 0.42*fi*fi // clean symmetric fall-off
		if isBack {
			mul *= 0.72
		}
		trees = append(trees, treeMeta{
			seed: xnoise.Hash(ts, 0x13, uint64(i), boolKey(isBack)),
			x:    x, hMul: mul, back: isBack, sp: t.Species,
		})
	}
	for i := 0; i < back; i++ {
		place(i, back, true)
	}
	for i := 0; i < front; i++ {
		place(i, front, false)
	}
	return trees
}

// plantWilderness fills margins and the gaps between towns with wild growth,
// letting a little of it bleed into town edges so nothing has a border.
func (w *World) plantWilderness() {
	// The deep old woods west of everything: tall, close, fading as they
	// approach the first town.
	x := 16
	i := uint64(0)
	for x < westWild-30 {
		f := float64(x) / float64(westWild) // 0 deep west .. 1 near town
		h := int(xnoise.Range(w.Seed, 46, 82, 0x21, i) * (1 - 0.45*f))
		sp := []model.Species{model.Oak, model.Grove, model.Spruce}[xnoise.Hash(w.Seed, 0x22, i)%3]
		w.wild = append(w.wild, wildItem{seed: xnoise.Hash(w.Seed, 0x23, i), x: x, h: h, kind: wildOldTree, sp: sp})
		x += 13 + int(xnoise.Unit(w.Seed, 0x24, i)*16*(0.5+f))
		i++
	}
	// Filler through the gaps (and just past town edges: edges must blur).
	spans := w.gapSpans()
	for _, sp := range spans {
		x := sp[0]
		for x < sp[1] {
			u := xnoise.Unit(w.Seed, 0x25, uint64(x))
			var k wildKind
			switch {
			case u < 0.58:
				k = wildScrub
			case u < 0.83:
				k = wildRock
			default:
				k = wildSnag
			}
			h := 8 + int(xnoise.Unit(w.Seed, 0x26, uint64(x))*11)
			w.wild = append(w.wild, wildItem{seed: xnoise.Hash(w.Seed, 0x27, uint64(x)), x: x, h: h, kind: k, sp: model.Wild})
			x += 30 + int(xnoise.Unit(w.Seed, 0x28, uint64(x))*46)
		}
	}
	// The young east: a few saplings walking out into the dark.
	last := w.Width - eastWild
	for j := 0; j < 4; j++ {
		x := last + 50 + j*55 + int(xnoise.Unit(w.Seed, 0x29, uint64(j))*30)
		w.wild = append(w.wild, wildItem{seed: xnoise.Hash(w.Seed, 0x2A, uint64(j)), x: x, h: 6 + j%3, kind: wildSapling, sp: model.Wild})
	}
}

// gapSpans returns the wild stretches between and slightly overlapping towns.
func (w *World) gapSpans() [][2]int {
	var spans [][2]int
	prev := westWild - 40
	for _, s := range w.Sites {
		if s.X0+14 > prev {
			spans = append(spans, [2]int{prev, s.X0 + 14})
		}
		prev = s.X1 - 14
	}
	spans = append(spans, [2]int{prev, w.Width - eastWild + 40})
	return spans
}

// NearestSite returns the site whose sign is closest to worldX.
func (w *World) NearestSite(worldX float64) *Site {
	var best *Site
	bd := 1e18
	for _, s := range w.Sites {
		d := abs64(worldX - float64(s.SignX))
		if d < bd {
			bd, best = d, s
		}
	}
	return best
}

// SpotSite returns the most recently tended town: the one under the lantern.
func (w *World) SpotSite(now time.Time) *Site {
	var best *Site
	var bt time.Time
	for _, s := range w.Sites {
		if s.Town.IdleOverride != nil {
			continue // a town under the almanac's hand is not "just tended"
		}
		if s.Town.LastTS.After(bt) {
			bt, best = s.Town.LastTS, s
		}
	}
	return best
}

func abs64(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func boolKey(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}
