// Package sprite draws the living things: trees of eight species told apart
// by silhouette alone, and the ways they lean, thin, and break as the forest
// reclaims them. Everything is drawn in dot space onto the canvas; runes are
// used only for structural strokes (trunks, boughs, logs).
package sprite

import (
	"math"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/xnoise"
)

// P is the paint context for one frame.
type P struct {
	C    *canvas.Canvas
	T    float64                    // seconds, for wind
	Wind func(x, y float64) float64 // horizontal sway in dots at a world point
}

func (p *P) wind(x, y float64) float64 {
	if p.Wind == nil {
		return 0
	}
	return p.Wind(x, y)
}

// Tree is one plant instance, fully described so drawing is deterministic.
type Tree struct {
	Seed    uint64
	Species model.Species
	X       int     // trunk center, dots
	GroundY int     // baseline, dots
	H       int     // stature, dots
	Lvl     uint8   // base brightness
	Decay   float64 // 0 tended .. ~1 ruin
}

// Draw renders a tree with its current decay applied.
func (p *P) Draw(t Tree) {
	d := xnoise.Clamp(t.Decay, 0, 1)
	// A seeded minority of well-decayed trees fall or reduce to stumps.
	fate := xnoise.Hash(t.Seed, 0xFA7E) % 10
	if d > 0.62 && fate < 2 {
		p.fallenLog(t)
		return
	}
	if d > 0.85 && fate < 4 {
		p.stump(t)
		return
	}
	switch t.Species {
	case model.Oak:
		p.oak(t, d)
	case model.Spruce:
		p.spruce(t, d)
	case model.Willow:
		p.willow(t, d)
	case model.Poplar:
		p.poplar(t, d)
	case model.Flattop:
		p.flattop(t, d)
	case model.Scrub, model.Wild:
		p.scrub(t, d)
	case model.Birch:
		p.birch(t, d)
	default:
		p.grove(t, d)
	}
}

// lean returns the horizontal shear (dots per dot of height) decay gives a tree.
func lean(seed uint64, d float64) float64 {
	if xnoise.Unit(seed, 0x1EA4) < 0.45 {
		return 0
	}
	dir := 1.0
	if xnoise.Hash(seed, 0x1EA5)%2 == 0 {
		dir = -1
	}
	return dir * 0.15 * xnoise.Smoothstep(0.30, 0.78, d)
}

// canopyGone reports how erased the canopy is at depth d (0 full, 1 gone).
func canopyGone(d float64) float64 { return xnoise.Smoothstep(0.60, 0.88, d) }

type blob struct{ cx, cy, rx, ry float64 }

// canopy fills a union of soft ellipses with clumped, feather-edged dots.
// This is the halftone that makes foliage read organic: leaf-clump noise
// gates each dot, decay eats low-frequency patches (never salt noise), and
// wind erodes the rim in slow waves.
func (p *P) canopy(seed uint64, blobs []blob, density float64, lvl uint8, d float64, sway float64, cut func(nx, ny float64) bool) {
	gone := canopyGone(d)
	if gone >= 1 {
		return
	}
	density *= (1 - 0.4*xnoise.Smoothstep(0.05, 0.9, d)) * (1 - gone)
	x0, x1 := math.Inf(1), math.Inf(-1)
	y0, y1 := math.Inf(1), math.Inf(-1)
	for _, b := range blobs {
		x0 = math.Min(x0, b.cx-b.rx-2)
		x1 = math.Max(x1, b.cx+b.rx+2)
		y0 = math.Min(y0, b.cy-b.ry-2)
		y1 = math.Max(y1, b.cy+b.ry+2)
	}
	for y := int(y0); y <= int(y1); y++ {
		for x := int(x0); x <= int(x1); x++ {
			cov := -1.0
			var bn *blob
			for i := range blobs {
				b := &blobs[i]
				nx := (float64(x) - b.cx) / b.rx
				ny := (float64(y) - b.cy) / b.ry
				c := 1 - (nx*nx + ny*ny)
				if c > cov {
					cov, bn = c, b
				}
			}
			if cov < 0 || bn == nil {
				continue
			}
			nx := (float64(x) - bn.cx) / bn.rx
			ny := (float64(y) - bn.cy) / bn.ry
			if cut != nil && cut(nx, ny) {
				continue
			}
			edge := xnoise.Smoothstep(0.0, 0.42, cov)
			w := p.wind(float64(x), float64(y))
			// Leaf clumping at two scales. The fine scale gates individual
			// dots (texture); the coarse scale groups them into leaf masses
			// with soft hollows between, which is what survives at cell size.
			// Sampled with a slight wind shift so gusts ripple through the
			// texture instead of teleporting it.
			fine := xnoise.Value2(seed, (float64(x)+w*sway)*0.55, float64(y)*0.55)
			coarse := xnoise.Value2(seed^0xC0A, float64(x)*0.085, float64(y)*0.13)
			thr := density * (0.24 + 0.76*edge) * (0.62 + 0.72*coarse)
			if edge < 0.5 {
				thr -= math.Abs(w) * 0.08 * (1 - edge*2)
			}
			if fine >= thr {
				continue
			}
			if d > 0.02 {
				patch := xnoise.FBM2(seed^0xDECA, float64(x)*0.045, float64(y)*0.06, 2)
				if patch < d*0.74 {
					continue
				}
			}
			// Coarse shading gives the crown its lumpy volume; moonlight
			// from above brightens the tops.
			l := int(lvl) + int((coarse-0.5)*44) - int((1-edge)*24) - int(ny*12)
			if l < 18 {
				l = 18
			}
			if l > 250 {
				l = 250
			}
			p.C.Dot(x, y, uint8(l))
		}
	}
}

// trunk draws a rune-stroke trunk from top (dot y) to the ground, with lean.
func (p *P) trunk(seed uint64, x, top, gy int, thick int, lvl uint8, shear float64) {
	glyph := '│'
	if thick == 2 {
		glyph = '┃'
	} else if thick >= 3 {
		glyph = '║'
	}
	prevCX := -1 << 30
	for cy := gy / 4; cy >= top/4; cy-- {
		yMid := cy*4 + 2
		off := shear * float64(gy-yMid)
		cx := (x + int(math.Round(off))) / 2
		g := glyph
		if prevCX != -1<<30 && cx != prevCX {
			if cx > prevCX {
				g = '╲'
			} else {
				g = '╱'
			}
		}
		p.C.Rune(cx, cy, g, lvl)
		prevCX = cx
	}
	// Root flare for stout trunks.
	if thick >= 2 {
		p.C.Dot(x-2, gy-1, lvl-20)
		p.C.Dot(x+2, gy-1, lvl-20)
		p.C.Dot(x-3, gy, lvl-30)
		p.C.Dot(x+3, gy, lvl-30)
	}
}

// boughs draws a bare branch skeleton from the trunk's upper reach. This is
// what remains visible as the canopy thins: deliberate structure, not loss.
func (p *P) boughs(seed uint64, x, top, gy int, h int, lvl uint8, shear float64, d float64) {
	if d < 0.48 {
		return
	}
	fade := xnoise.Smoothstep(0.48, 0.72, d)
	l := uint8(float64(lvl) * (0.55 + 0.45*fade))
	n := 3 + int(xnoise.Hash(seed, 0xB0)%3)
	// Deep ruin weathers the smaller boughs away: spars, not lattices.
	n -= int(xnoise.Smoothstep(0.88, 1.0, d) * 3)
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		frac := 0.05 + 0.42*xnoise.Unit(seed, 0xB1, uint64(i))
		startY := top + int(float64(h)*frac*0.6)
		dir := 1
		if xnoise.Hash(seed, 0xB2, uint64(i))%2 == 0 {
			dir = -1
		}
		length := 2 + int(xnoise.Hash(seed, 0xB3, uint64(i))%3)
		cx := (x + int(shear*float64(gy-startY))) / 2
		cy := startY / 4
		for s := 1; s <= length; s++ {
			cx += dir
			cy--
			g := '╱'
			if dir < 0 {
				g = '╲'
			}
			if s == length && xnoise.Hash(seed, 0xB4, uint64(i))%2 == 0 {
				g = '─'
				cy++
			}
			p.C.Rune(cx, cy, g, l)
			// A secondary twig partway along the larger boughs.
			if s == length-1 && length >= 3 {
				tg := '╲'
				if dir < 0 {
					tg = '╱'
				}
				p.C.Rune(cx, cy-1, tg, l-20)
			}
		}
		// Twig dots at the tip.
		p.C.Dot(cx*2+dir, cy*4, l-25)
		p.C.Dot(cx*2+dir*2, cy*4-2, l-35)
	}
}

// vines climb the trunk once neglect sets in.
func (p *P) vines(seed uint64, x, gy, h int, lvl uint8, d float64) {
	if d < 0.22 {
		return
	}
	climb := int(float64(h) * 0.8 * xnoise.Smoothstep(0.2, 0.62, d))
	phase := xnoise.Range(seed, 0, 6.28, 0x71)
	for dy := 0; dy < climb; dy++ {
		y := gy - dy
		xx := x + int(math.Round(math.Sin(float64(y)*0.42+phase)*2.3))
		if xnoise.Unit(seed, 0x72, uint64(y)) < 0.75 {
			p.C.Dot(xx, y, lvl)
		}
		// Occasional leaf pair off the vine.
		if xnoise.Unit(seed, 0x73, uint64(y)) < 0.18 {
			p.C.Dot(xx+1, y, lvl-15)
		}
	}
}

// ---- species ------------------------------------------------------------

// oak: broad billowing dome on a stout trunk. (go)
func (p *P) oak(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d)
	cx, gy := float64(t.X), float64(t.GroundY)
	cy := gy - h*0.62
	ccx := cx - sh*(gy-cy)
	rx, ry := h*0.31, h*0.29
	thick := 1
	if h > 55 {
		thick = 2
	}
	if h > 82 {
		thick = 3
	}
	top := int(gy - h*0.5)
	p.trunk(t.Seed, t.X, top, t.GroundY, thick, t.Lvl-28, sh)
	p.boughs(t.Seed, t.X, int(gy-h*0.72), t.GroundY, t.H, t.Lvl-18, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	p.canopy(t.Seed, []blob{
		{ccx, cy, rx, ry},
		{ccx - rx*0.62, cy + ry*0.3, rx * 0.55, ry * 0.6},
		{ccx + rx*0.62, cy + ry*0.28, rx * 0.58, ry * 0.62},
		{ccx + rx*0.1, cy - ry*0.5, rx * 0.5, ry * 0.5},
	}, 0.88, t.Lvl, d, 0.9, nil)
}

// spruce: tall tiered triangle, pointed crown, drooping shelves. (rust)
func (p *P) spruce(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d)
	gy := float64(t.GroundY)
	top := gy - h
	gone := canopyGone(d)
	p.trunk(t.Seed, t.X, int(gy-h*0.35), t.GroundY, 1, t.Lvl-28, sh)
	p.boughs(t.Seed, t.X, int(top)+6, t.GroundY, t.H, t.Lvl-18, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	if gone >= 1 {
		return
	}
	dmul := (1 - 0.4*xnoise.Smoothstep(0.05, 0.9, d)) * (1 - gone)
	span := h * 0.80
	tiers := 4 + int(h/30)
	for k := 0; k < tiers; k++ {
		fk := float64(k) / float64(tiers-1) // 0 tip .. 1 skirt
		y0 := top + span*(float64(k)/float64(tiers))*0.94
		y1 := top + span*((float64(k)+1.18)/float64(tiers)) // overlap the next shelf
		w := h * 0.30 * (0.18 + 0.82*fk) * xnoise.Range(t.Seed, 0.9, 1.1, uint64(k))
		for y := int(y0); y <= int(y1); y++ {
			fy := (float64(y) - y0) / math.Max(y1-y0, 1)
			half := w * (0.10 + 0.90*fy*fy) // shelves flare toward their drooping hems
			cx := float64(t.X) - sh*(gy-float64(y))
			for x := int(cx - half - 1); x <= int(cx+half+1); x++ {
				nx := math.Abs(float64(x)-cx) / math.Max(half, 0.7)
				if nx > 1.12 {
					continue
				}
				wnd := p.wind(float64(x), float64(y))
				wob := 0.6*xnoise.Value2(t.Seed, (float64(x)+wnd*0.5)*0.5, float64(y)*0.5) +
					0.4*xnoise.Value2(t.Seed^0xC0A, float64(x)*0.14, float64(y)*0.2)
				edge := 1 - xnoise.Smoothstep(0.72, 1.08, nx)
				thr := 0.92 * (0.25 + 0.75*edge) * dmul
				if nx > 0.8 {
					thr -= math.Abs(wnd) * 0.06
				}
				if wob >= thr {
					continue
				}
				if d > 0.02 && xnoise.FBM2(t.Seed^0xDECA, float64(x)*0.05, float64(y)*0.07, 2) < d*0.74 {
					continue
				}
				l := int(t.Lvl) + int((wob-0.5)*24) - int(nx*24) - int(fy*8)
				if l < 18 {
					l = 18
				}
				p.C.Dot(x, y, uint8(l))
			}
		}
	}
	// Pointed tip above the first shelf.
	p.C.Dot(t.X, int(top)-2, t.Lvl-16)
	p.C.Dot(t.X, int(top), t.Lvl-6)
	p.C.Dot(t.X+1, int(top)+1, t.Lvl-10)
}

// willow: soft crown with hanging strands that sway. (python)
func (p *P) willow(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d)
	cx, gy := float64(t.X), float64(t.GroundY)
	cy := gy - h*0.70
	ccx := cx - sh*(gy-cy)
	rx, ry := h*0.30, h*0.22
	top := int(gy - h*0.55)
	p.trunk(t.Seed, t.X, top, t.GroundY, 1, t.Lvl-28, sh)
	p.boughs(t.Seed, t.X, int(gy-h*0.78), t.GroundY, t.H, t.Lvl-18, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	p.canopy(t.Seed, []blob{
		{ccx, cy, rx, ry},
		{ccx - rx*0.5, cy + ry*0.2, rx * 0.5, ry * 0.6},
		{ccx + rx*0.5, cy + ry*0.2, rx * 0.5, ry * 0.6},
	}, 0.82, t.Lvl, d, 1.1, nil)
	// Strands: the weeping curtain.
	gone := canopyGone(d)
	if gone >= 1 {
		return
	}
	n := int(rx * 1.5)
	for i := 0; i < n; i++ {
		fi := float64(i)/float64(n-1)*2 - 1 // -1..1 across the crown
		px := ccx + fi*rx*1.02
		py := cy + ry*math.Sqrt(math.Max(0, 1-fi*fi))*0.5
		ln := h * 0.52 * xnoise.Range(t.Seed, 0.55, 1.05, 0x51, uint64(i))
		if xnoise.Unit(t.Seed, 0x52, uint64(i)) < d*1.1 {
			continue // strands are lost to neglect first
		}
		phase := xnoise.Range(t.Seed, 0, 6.28, 0x53, uint64(i))
		for s := 0.0; s < ln; s += 1.0 {
			y := py + s
			if y >= gy-1 {
				break
			}
			w := p.wind(px, y)
			dx := math.Sin(y*0.16+phase)*1.1 + w*1.9
			dx *= 0.25 + 0.75*(s/ln) // pinned at crown, free at tip
			if xnoise.Unit(t.Seed, 0x54, uint64(i), uint64(int(s))) < 0.82*(1-gone) {
				l := int(t.Lvl) - 25 - int(s/ln*20)
				if l < 20 {
					l = 20
				}
				p.C.Dot(int(px+dx), int(y), uint8(l))
			}
		}
	}
}

// poplar: a narrow candle-flame column. (typescript, javascript)
func (p *P) poplar(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d)
	gy := float64(t.GroundY)
	cy := gy - h*0.55
	ccx := float64(t.X) - sh*(gy-cy)
	top := int(gy - h*0.28)
	p.trunk(t.Seed, t.X, top, t.GroundY, 1, t.Lvl-28, sh)
	p.boughs(t.Seed, t.X, int(gy-h*0.85), t.GroundY, t.H, t.Lvl-18, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	p.canopy(t.Seed, []blob{{ccx, cy, h * 0.14, h * 0.42}}, 0.90, t.Lvl, d, 0.8,
		func(nx, ny float64) bool {
			// Taper the top to a flame tip.
			if ny < -0.45 {
				return math.Abs(nx) > 1.0-(-ny-0.45)*1.4
			}
			return false
		})
}

// flattop: massive trunk, broad crown sheared flat by age. (c, c++)
func (p *P) flattop(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d) * 0.5 // old giants barely lean
	gy := float64(t.GroundY)
	cy := gy - h*0.74
	ccx := float64(t.X) - sh*(gy-cy)
	top := int(gy - h*0.62)
	p.trunk(t.Seed, t.X, top, t.GroundY, 3, t.Lvl-26, sh)
	p.boughs(t.Seed, t.X, int(gy-h*0.8), t.GroundY, t.H, t.Lvl-16, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	p.canopy(t.Seed, []blob{
		{ccx, cy, h * 0.34, h * 0.18},
	}, 0.88, t.Lvl, d, 0.7,
		func(nx, ny float64) bool { return ny < -0.55 }) // the flat top
}

// scrub: low ground-hugging domes, no visible trunk. (shell; also wild filler)
func (p *P) scrub(t Tree, d float64) {
	h := float64(t.H)
	gy := float64(t.GroundY)
	cx := float64(t.X)
	n := 1 + int(xnoise.Hash(t.Seed, 0x5C)%3)
	blobs := make([]blob, 0, n)
	for i := 0; i < n; i++ {
		off := xnoise.Range(t.Seed, -h*0.8, h*0.8, 0x5D, uint64(i))
		r := h * xnoise.Range(t.Seed, 0.45, 0.75, 0x5E, uint64(i))
		blobs = append(blobs, blob{cx + off, gy - r*0.52, r, r * 0.62})
	}
	p.canopy(t.Seed, blobs, 0.86, t.Lvl, d, 0.8,
		func(nx, ny float64) bool { return ny > 0.75 }) // sits on the ground
	if d > 0.5 {
		// Dead scrub leaves a few woody fingers.
		for i := 0; i < 3; i++ {
			x := t.X + int(xnoise.Range(t.Seed, -h*0.6, h*0.6, 0x5F, uint64(i)))
			p.C.Dot(x, t.GroundY-2-i%2, t.Lvl-30)
			p.C.Dot(x, t.GroundY-1, t.Lvl-35)
		}
	}
}

// birch: slender pale trunk with bark ticks, small airy crown held high. (swift)
func (p *P) birch(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d)
	gy := float64(t.GroundY)
	cy := gy - h*0.84
	ccx := float64(t.X) - sh*(gy-cy)
	p.trunk(t.Seed, t.X, int(gy-h*0.92), t.GroundY, 1, t.Lvl-8, sh) // pale trunk is the tell
	// Bark ticks.
	for cyc := t.GroundY/4 - 1; cyc > int(gy-h*0.8)/4; cyc -= 2 {
		if xnoise.Unit(t.Seed, 0xBB, uint64(cyc)) < 0.5 {
			side := 1
			if xnoise.Hash(t.Seed, 0xBC, uint64(cyc))%2 == 0 {
				side = -1
			}
			p.C.Dot(t.X+side, cyc*4+2, t.Lvl-38)
		}
	}
	p.boughs(t.Seed, t.X, int(gy-h*0.9), t.GroundY, t.H, t.Lvl-14, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	p.canopy(t.Seed, []blob{
		{ccx, cy, h * 0.13, h * 0.10},
		{ccx + h*0.06, cy - h*0.07, h * 0.09, h * 0.07},
	}, 0.72, t.Lvl, d, 1.0, nil)
}

// grove: irregular mixed hardwood, the fallback form.
func (p *P) grove(t Tree, d float64) {
	h := float64(t.H)
	sh := lean(t.Seed, d)
	gy := float64(t.GroundY)
	cy := gy - h*0.6
	ccx := float64(t.X) - sh*(gy-cy)
	top := int(gy - h*0.45)
	p.trunk(t.Seed, t.X, top, t.GroundY, 1, t.Lvl-28, sh)
	p.boughs(t.Seed, t.X, int(gy-h*0.72), t.GroundY, t.H, t.Lvl-18, sh, d)
	p.vines(t.Seed, t.X, t.GroundY, t.H, t.Lvl-45, d)
	o1 := xnoise.Range(t.Seed, -h*0.2, h*0.2, 0x91)
	o2 := xnoise.Range(t.Seed, -h*0.25, h*0.25, 0x92)
	p.canopy(t.Seed, []blob{
		{ccx + o1, cy, h * 0.26, h * 0.22},
		{ccx + o2, cy - h*0.16, h * 0.2, h * 0.17},
		{ccx - o1*0.7, cy + h*0.08, h * 0.22, h * 0.16},
	}, 0.86, t.Lvl, d, 0.9, nil)
}

// fallenLog: a tree that has come down whole, mossing over where it lies.
func (p *P) fallenLog(t Tree) {
	ln := int(float64(t.H) * 0.34)
	dir := 1
	if xnoise.Hash(t.Seed, 0xF0)%2 == 0 {
		dir = -1
	}
	cy := t.GroundY / 4
	x0 := t.X / 2
	for i := 0; i < ln/2; i++ {
		g := '─'
		if t.H > 55 {
			g = '━'
		}
		p.C.Rune(x0+dir*i, cy, g, t.Lvl-35)
	}
	// Root plate at the base end.
	p.C.Rune(x0-dir, cy, '╾', t.Lvl-30)
	p.C.Dot(t.X-dir*2, t.GroundY-3, t.Lvl-30)
	p.C.Dot(t.X-dir*1, t.GroundY-5, t.Lvl-35)
	p.C.Dot(t.X-dir*3, t.GroundY-2, t.Lvl-40)
	// Moss and regrowth along the top.
	for i := 1; i < ln/2; i++ {
		if xnoise.Unit(t.Seed, 0xF1, uint64(i)) < 0.4 {
			p.C.Dot(t.X+dir*i*2, t.GroundY-4, t.Lvl-45)
		}
	}
}

// stump: what remains after the fall is complete.
func (p *P) stump(t Tree) {
	cy := t.GroundY / 4
	p.C.Rune(t.X/2, cy, '▂', t.Lvl-30)
	p.C.Dot(t.X-1, t.GroundY-4, t.Lvl-40)
	p.C.Dot(t.X+1, t.GroundY-5, t.Lvl-42)
}

// Sapling is the forest's own regrowth among ruins: a few dots of new life.
func (p *P) Sapling(seed uint64, x, gy int, lvl uint8) {
	h := 3 + int(xnoise.Hash(seed, 0x5A)%4)
	for dy := 0; dy < h; dy++ {
		p.C.Dot(x, gy-dy, lvl-uint8(dy*4))
	}
	p.C.Dot(x-1, gy-h, lvl)
	p.C.Dot(x+1, gy-h+1, lvl-8)
}
