// Package forest lays out and renders the world: one continuous stretch of
// wild woodland, oldest towns to the west, deep old-growth beyond the first
// town and open young dark beyond the last. Nothing here is a tile or a grid;
// every position is jittered, every baseline follows the terrain.
package forest

import (
	"math"
	"sort"
	"time"

	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/sprite"
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

// Hearth is a town's homestead: one hand-hewn cabin in the grove. The name
// board hangs on it, so it is also the site's focal point.
type Hearth struct {
	Seed uint64
	X    int // center, reference dots
	Tier int
}

// BuildingSite is one component's structure placed in the settlement.
type BuildingSite struct {
	B    model.Building
	X    int  // center, reference dots
	Mid  bool // set between the tree rows, half-hidden
	Seed uint64
}

// Fence is a fragment of split-rail between two neighboring yards.
// A and B index Site.Buildings; -1 is the hearth.
type Fence struct {
	X0, X1 int
	A, B   int
	Seed   uint64
}

// Site is a town's place in the world.
type Site struct {
	Town      *model.Town
	X0, X1    int // extent in reference dots
	SignX     int
	Hearth    Hearth
	Buildings []BuildingSite
	Fences    []Fence
	WellX     int // communal well; 0 means none
	StakesX   int // release stakes: where the trail leaves the settlement
	BeltW     int // settlement belt edges: understory grows inside,
	BeltE     int // old growth stays outside and behind
	trees     []treeMeta
	// The grove's planting inputs, kept so CarveGrove can replant it at any
	// depth between its wild and monument layouts.
	ts          uint64
	front, back int
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

		site := placeSettlement(ts, t, cursor, int(float64(front)*perTree*0.7)+30)
		site.ts, site.front, site.back = ts, front, back

		carve := site.yards()
		if t.Finished {
			site.trees = plantMonumentGrove(ts, t, site.X0, site.X1-site.X0, front, back, carve)
		} else {
			site.trees = plantWildGrove(ts, t, site.X0, site.X1-site.X0, front, back, carve)
		}
		w.Sites = append(w.Sites, site)

		gap := 80 + int(xnoise.Unit(ts, 2)*170)
		if xnoise.Unit(ts, 3) < 0.18 {
			gap += 190 // an occasional long wilderness walk
		}
		cursor = site.X1 + gap
	}
	w.Width = cursor + eastWild
	w.plantWilderness()
	return w
}

// placeSettlement lays a town's buildings around its hearth: biggest
// nearest the home, alternating sides, a stand of trees' worth of gap
// between every pair, the small forms sometimes set back between the tree
// rows. The whole cluster then decides the site's extent.
func placeSettlement(ts uint64, t *model.Town, cursor, treeRoom int) *Site {
	tier := t.HearthTier()
	hearthSeed := xnoise.Hash(ts, 0xCAB1)
	gapW, gapE, _ := sprite.CabinYardGaps(tier, hearthSeed)

	// Edges of claimed ground, in dots relative to the hearth center.
	westEdge, eastEdge := -gapW, gapE
	side := 1
	if xnoise.Hash(ts, 0x5E7)%2 == 0 {
		side = -1
	}
	var placed []BuildingSite
	for bi, b := range t.Buildings() {
		bseed := xnoise.Hash(ts, 0xB17D, uint64(bi))
		mid := false
		switch b.Form {
		case model.FormShed, model.FormCrib, model.FormWorkshop:
			mid = xnoise.Unit(bseed, 1) < 0.4
		}
		yw, ye, _ := sprite.BuildingYard(b.Form, b.Share)
		if mid {
			w, _ := sprite.BuildingDims(b.Form, b.Share)
			yw, ye = w+2, w+2 // half-hidden: the trees keep only off its face
		}
		gap := 12 + int(xnoise.Unit(bseed, 2)*20)
		var bx int
		if side > 0 {
			bx = eastEdge + gap + yw
			eastEdge = bx + ye
		} else {
			bx = westEdge - gap - ye
			westEdge = bx - yw
		}
		placed = append(placed, BuildingSite{B: b, X: bx, Mid: mid, Seed: bseed})
		side = -side
	}

	extent := (eastEdge - westEdge) + treeRoom
	x0 := cursor
	hx := x0 - westEdge + treeRoom/2
	site := &Site{
		Town: t, X0: x0, X1: x0 + extent,
		SignX:   hx,
		Hearth:  Hearth{Seed: hearthSeed, X: hx, Tier: tier},
		StakesX: hx + eastEdge + 4,
		BeltW:   hx + westEdge,
		BeltE:   hx + eastEdge,
	}
	for i := range placed {
		placed[i].X += hx
	}
	site.Buildings = placed

	// The well, once the town is more than a homestead: in the first gap
	// east of the hearth's yard, where the paths cross.
	if len(placed) >= 3 {
		site.WellX = hx + gapE + 7
	}
	site.placeFences(ts)
	return site
}

// placeFences runs split-rail fragments through the wider gaps between
// neighboring front-plane yards. Broken by seed, never an enclosure.
func (s *Site) placeFences(ts uint64) {
	type edge struct {
		idx      int // -1 hearth
		lo, hi   int
		frontRow bool
	}
	var edges []edge
	gw, ge, _ := sprite.CabinYardGaps(s.Hearth.Tier, s.Hearth.Seed)
	edges = append(edges, edge{-1, s.Hearth.X - gw, s.Hearth.X + ge, true})
	for i, b := range s.Buildings {
		if b.Mid {
			continue
		}
		yw, ye, _ := sprite.BuildingYard(b.B.Form, b.B.Share)
		edges = append(edges, edge{i, b.X - yw, b.X + ye, true})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].lo < edges[j].lo })
	for i := 1; i < len(edges); i++ {
		l, r := edges[i-1], edges[i]
		gap := r.lo - l.hi
		if gap < 16 || xnoise.Unit(ts, 0xFE9, uint64(i)) < 0.35 {
			continue // some neighbors never fenced anything
		}
		s.Fences = append(s.Fences, Fence{
			X0: l.hi + 4, X1: r.lo - 4, A: l.idx, B: r.idx,
			Seed: xnoise.Hash(ts, 0xFE5, uint64(i)),
		})
	}
}

// yardSpans is the settlement's claimed ground: merged intervals that trees
// must stand clear of, one set for the front rows and one for the back.
// The settlers felled what stood where the buildings went, and nothing more.
type yardSpans struct {
	front [][2]int
	back  [][2]int
	// The settlement belt: front trees inside it grow as understory, kept
	// low by the settlers' axes. The old growth stands outside and behind.
	beltLo, beltHi int
}

func (s *Site) yards() yardSpans {
	ys := yardSpans{beltLo: s.BeltW, beltHi: s.BeltE}
	gw, ge, gb := sprite.CabinYardGaps(s.Hearth.Tier, s.Hearth.Seed)
	ys.front = append(ys.front, [2]int{s.Hearth.X - gw, s.Hearth.X + ge})
	ys.back = append(ys.back, [2]int{s.Hearth.X - gb, s.Hearth.X + gb})
	for _, b := range s.Buildings {
		yw, ye, yb := sprite.BuildingYard(b.B.Form, b.B.Share)
		if b.Mid {
			w, _ := sprite.BuildingDims(b.B.Form, b.B.Share)
			ys.front = append(ys.front, [2]int{b.X - w - 2, b.X + w + 2})
			ys.back = append(ys.back, [2]int{b.X - yb, b.X + yb})
			continue
		}
		ys.front = append(ys.front, [2]int{b.X - yw, b.X + ye})
		ys.back = append(ys.back, [2]int{b.X - yb, b.X + yb})
	}
	if s.WellX != 0 {
		ys.front = append(ys.front, [2]int{s.WellX - 6, s.WellX + 6})
	}
	ys.front = mergeSpans(ys.front)
	ys.back = mergeSpans(ys.back)
	return ys
}

func mergeSpans(in [][2]int) [][2]int {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i][0] < in[j][0] })
	out := [][2]int{in[0]}
	for _, sp := range in[1:] {
		last := &out[len(out)-1]
		if sp[0] <= last[1] {
			if sp[1] > last[1] {
				last[1] = sp[1]
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

// apply pushes a trunk out of any claimed yard to the nearest edge, with a
// settler's pad of jitter. Once a direction is picked the trunk slides past
// every further span in that direction, so it never lands inside a neighbor.
func (ys yardSpans) apply(ts uint64, x, i int, isBack bool) int {
	spans := ys.front
	if isBack {
		spans = ys.back
	}
	pad := int(xnoise.Unit(ts, 0x16, uint64(i), boolKey(isBack)) * 5)
	for _, sp := range spans {
		if x <= sp[0] || x >= sp[1] {
			continue
		}
		if x-sp[0] < sp[1]-x { // west is nearer: slide west past everything
			out := sp[0] - pad
			for j := len(spans) - 1; j >= 0; j-- {
				if out > spans[j][0] && out < spans[j][1] {
					out = spans[j][0] - pad
				}
			}
			return out
		}
		out := sp[1] + pad
		for _, sj := range spans {
			if out > sj[0] && out < sj[1] {
				out = sj[1] + pad
			}
		}
		return out
	}
	return x
}

// plantWildGrove scatters a town's trees with jittered spacing and varied
// heights: a real grove, denser and taller near its heart.
func plantWildGrove(ts uint64, t *model.Town, x0, extent, front, back int, carve yardSpans) []treeMeta {
	var trees []treeMeta
	place := func(i, n int, isBack bool) {
		fi := 0.0
		if n > 1 {
			fi = float64(i)/float64(n-1)*2 - 1 // -1..1 across extent
		}
		x := x0 + 15 + int((fi*0.5+0.5)*float64(extent-30))
		x += int(xnoise.Range(ts, -7, 7, 0x11, uint64(i), boolKey(isBack)))
		x = carve.apply(ts, x, i, isBack)
		center := 1 - 0.35*fi*fi // taller near the middle
		mul := center * xnoise.Range(ts, 0.68, 1.04, 0x12, uint64(i), boolKey(isBack))
		if isBack {
			mul *= 0.72
		} else if carve.beltHi > carve.beltLo && x > carve.beltLo && x < carve.beltHi {
			// Understory between the yards: no settler lets old growth
			// overhang a roof. The giants stand outside and behind.
			mul *= xnoise.Range(ts, 0.38, 0.55, 0x17, uint64(i))
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
func plantMonumentGrove(ts uint64, t *model.Town, x0, extent, front, back int, carve yardSpans) []treeMeta {
	var trees []treeMeta
	place := func(i, n int, isBack bool) {
		fi := 0.0
		if n > 1 {
			fi = float64(i)/float64(n-1)*2 - 1
		}
		x := x0 + 15 + int((fi*0.5+0.5)*float64(extent-30))
		x = carve.apply(ts, x, i, isBack)
		mul := 1 - 0.42*fi*fi // clean symmetric fall-off
		if isBack {
			mul *= 0.72
		} else if carve.beltHi > carve.beltLo && x > carve.beltLo && x < carve.beltHi {
			mul *= 0.5 // a kept settlement's understory, trimmed even
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

// CarveGrove replants the site's grove partway between its wild layout (0)
// and the calm monument layout (1) it keeps once finished. Both layouts plant
// the same trees - same seeds, same rows, same species - so the grove stills
// into symmetry rather than being replaced: each trunk eases toward the place
// and height the monument grove holds for it. At 1 the trees land exactly
// where Build would put them for a finished town, so a completed ceremony
// needs no rebuild; at 0 they stand exactly as the wild grove grew.
func (s *Site) CarveGrove(p float64) {
	carve := s.yards()
	extent := s.X1 - s.X0
	if p <= 0 {
		s.trees = plantWildGrove(s.ts, s.Town, s.X0, extent, s.front, s.back, carve)
		return
	}
	mon := plantMonumentGrove(s.ts, s.Town, s.X0, extent, s.front, s.back, carve)
	if p >= 1 {
		s.trees = mon
		return
	}
	wild := plantWildGrove(s.ts, s.Town, s.X0, extent, s.front, s.back, carve)
	for i := range wild {
		wild[i].x += int(math.Round(float64(mon[i].x-wild[i].x) * p))
		wild[i].hMul += (mon[i].hMul - wild[i].hMul) * p
	}
	s.trees = wild
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
