// The homestead: every town keeps one hand-hewn cabin, the repo's hearth,
// built in a gap between the trees barely wider than its walls. Walls are
// rune log courses with crossed corner notches; the gable roof is dot-shaded
// shake; the stone chimney is drawn from the ground up in every frame, so
// deep decay does not add a ruin, it reveals the one thing that never falls.
package sprite

import (
	"math"

	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Cabin is one town's homestead, fully described so drawing is deterministic.
type Cabin struct {
	Seed    uint64
	X       int // center, dots
	GroundY int // baseline, dots
	Tier    int // 0 hut, 1 cabin, 2 homestead
	Lvl     uint8
	Decay   float64 // 0 lived-in .. ~1 chimney and sills
	Carve   float64 // 0 lived-in .. 1 a kept homestead: shuttered, stocked, still
	Bare    bool    // an outbuilding dwelling: cold chimney, no dooryard
	Focused bool
}

// CabinDims returns wall width in cells (odd), wall height in cell rows, and
// roof rise in cell rows for a size tier.
func CabinDims(tier int) (wallW, wallH, roofRise int) {
	switch {
	case tier <= 0:
		return 9, 2, 3
	case tier == 1:
		return 11, 3, 3
	default:
		return 15, 3, 4
	}
}

// CabinYardGaps returns the half-widths in dots of the ground a settler
// actually clears, west and east of the hearth for front trees and behind it
// for back rows. The shed side needs room for the lean-to and the cord, the
// door side for the chopping block; the back only clears the roofline, so
// the grove still rises behind the cabin. A footprint and a dooryard, never
// a plaza.
func CabinYardGaps(tier int, seed uint64) (west, east, back int) {
	w, _, _ := CabinDims(tier)
	door, shed := w+12, w+15
	if CabinDoorSide(seed) > 0 {
		return shed, door, w + 5
	}
	return door, shed, w + 5
}

// CabinDoorSide reports which gable end holds the door (+1 east, -1 west);
// the shingle sign and the chopping block keep to this side, the chimney and
// woodstore take the other.
func CabinDoorSide(seed uint64) int {
	if xnoise.Hash(seed, 0xD0)%2 == 0 {
		return -1
	}
	return 1
}

// CabinSignMount places the town's name board against its homestead. Cabins
// and homesteads swing a shingle from a bracket at the eave corner beside
// the door; a hut is too low for one, so it plants the classic post sign in
// its dooryard instead. Once the homestead is ruins the board lies propped
// near the ground, because a town never loses its name.
// x and groundY are dots; nameW is the board width in cells.
// It returns the board center x, the y DrawSign anchors to, whether the
// board hangs, and the eave-corner cell a hanging bracket reaches back to.
func CabinSignMount(tier int, seed uint64, x, groundY, nameW int, d float64) (signX, signGY int, hang bool, armC int) {
	wallW, wallH, _ := CabinDims(tier)
	side := CabinDoorSide(seed)
	if tier == 0 {
		// Past the chopping block, where the trail meets the dooryard.
		return x + side*(wallW+16+nameW), groundY, false, 0
	}
	cornerC := x/2 + side*(wallW/2+1)
	signX = (cornerC + side*(2+nameW/2)) * 2
	if d >= 0.9 {
		return signX, groundY, true, 0
	}
	return signX, (groundY/4 - wallH + 3) * 4, true, cornerC
}

// DrawCabin renders the homestead with its current decay applied.
func (p *P) DrawCabin(cb Cabin) {
	d := xnoise.Clamp(cb.Decay, 0, 1)
	wallW, wallH, roofRise := CabinDims(cb.Tier)
	lvl := cb.Lvl
	if cb.Focused {
		lvl += 10
	}
	// Structure fails a little behind the canopy, and almost nothing of the
	// walls outlives the trees.
	weather := xnoise.Smoothstep(0.32, 0.97, d)
	ruin := xnoise.Smoothstep(0.86, 0.94, d)

	cx := cb.X / 2
	gyr := cb.GroundY / 4
	hw := wallW / 2
	eaveRow := gyr - wallH
	ridgeRow := eaveRow - roofRise + 1
	roofY0 := ridgeRow * 4
	roofY1 := eaveRow*4 + 3
	rise := float64(roofY1 - roofY0)
	span := float64(wallW + 2) // eaves overhang the walls by a cell each side

	side := CabinDoorSide(cb.Seed) // the chimney takes the other gable end

	p.cabinRoof(cb, lvl, d, ruin, cx, roofY0, roofY1, rise, span)
	p.cabinWalls(cb, lvl, weather, cx, gyr, hw, wallH)
	p.cabinDoor(cb, lvl, d, cx, gyr, hw, side)
	p.cabinWindows(cb, lvl, d, weather, cx, gyr, hw, side)
	p.cabinChimney(cb, lvl, cx, roofY0, rise, span, wallW, side)
	if !cb.Bare {
		p.cabinWoodstore(cb, lvl, d, wallW, side)
		p.cabinStump(cb, lvl, d, gyr, wallW, side)
	}

	// The forest climbs the corners the way it climbs trunks.
	p.vines(xnoise.Hash(cb.Seed, 0xC1), cb.X-wallW-2, cb.GroundY, wallH*4+8, lvl-40, d)
	p.vines(xnoise.Hash(cb.Seed, 0xC2), cb.X+wallW+2, cb.GroundY, wallH*4+8, lvl-40, d)

	if cb.Carve > 0 {
		// A kept homestead: the carved finial rises at the peak, catching the
		// warm light; the cairns stack by the path, course by course.
		if rise := xnoise.Smoothstep(carveFinial, carveNameRow, cb.Carve); rise > 0 {
			p.C.Rune(cx, ridgeRow-1, '◆', uint8(int(lvl)-15+int(40*rise)))
		}
		rows := cairnRows(cb.Carve)
		p.cairn(xnoise.Hash(cb.Seed, 0xC3), cb.X-wallW-8, cb.GroundY, lvl-12, rows)
		p.cairn(xnoise.Hash(cb.Seed, 0xC4), cb.X+wallW+8, cb.GroundY, lvl-12, rows)
	}
}

// cabinRoof draws the gable: a dot-shaded triangle of shake courses with a
// bright rim, holed by the same patch noise that eats canopies, kept as bare
// A-frame bones once the shakes are gone, and dissolved entirely by ruin.
func (p *P) cabinRoof(cb Cabin, lvl uint8, d, ruin float64, cx, roofY0, roofY1 int, rise, span float64) {
	if ruin >= 1 {
		return
	}
	// Occlude the grove behind the roof body, but only cells fully inside the
	// triangle: edge cells stay open so foliage feathers over the silhouette.
	for rr := roofY0 / 4; rr <= roofY1/4; rr++ {
		fyTop := (float64(rr*4) - float64(roofY0)) / rise
		inner := int(span*fyTop)/2 - 1
		if inner > 0 {
			p.C.ClearRect(cx-inner, rr, inner*2+1, 1)
		}
	}
	for y := roofY0; y <= roofY1; y++ {
		fy := (float64(y) - float64(roofY0)) / rise
		half := span * fy
		for x := cb.X - int(half); x <= cb.X+int(half); x++ {
			nx := 0.0
			if half > 0.5 {
				nx = math.Abs(float64(x-cb.X)) / half
			}
			// Shake courses: a combed seam every few dot rows. The peak stays
			// solid; a gable is built from its ridge down.
			if y > roofY0+3 && (y-roofY0)%4 == 3 && xnoise.Value2(cb.Seed, float64(x)*0.3, float64(y)) < 0.55 {
				continue
			}
			wob := xnoise.Value2(cb.Seed^0x400F, float64(x)*0.33, float64(y)*0.6)
			if fy > 0.2 && fy < 0.93 && wob > 0.86 {
				continue
			}
			if d > 0.02 && xnoise.FBM2(cb.Seed^0xDECA, float64(x)*0.05, float64(y)*0.07, 2) < d*0.80 {
				continue
			}
			if ruin > 0 && xnoise.Unit(cb.Seed, 0xB01, uint64(x+512), uint64(y)) < ruin {
				continue
			}
			l := int(lvl) - 16 + int((wob-0.5)*20)
			if nx > 0.86 {
				l += 16 // moonlit rim
			}
			if y <= roofY0+1 {
				l += 12 // the ridge cap catches the light
			}
			if fy > 0.93 {
				l += 8 // dense eave line, a crisp junction over the logs
			}
			if l < 20 {
				l = 20
			}
			p.C.Dot(x, y, uint8(l))
		}
	}
	// Once the shakes are mostly gone the A-frame stands as bones, the same
	// grammar as bare boughs: deliberate structure, not loss.
	if d > 0.62 && ruin < 0.6 {
		steps := int(rise) + 1
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			y := roofY0 + int(t*rise)
			dx := int(t * span)
			if xnoise.Unit(cb.Seed, 0xAF1, uint64(i)) < 0.78 {
				p.C.Dot(cb.X-dx, y, lvl-8)
			}
			if xnoise.Unit(cb.Seed, 0xAF2, uint64(i)) < 0.78 {
				p.C.Dot(cb.X+dx, y, lvl-8)
			}
		}
	}
}

// cabinWalls lays the log courses: rune strokes with crossed ends, weathering
// away cell by cell, the sill logs holding out longest.
func (p *P) cabinWalls(cb Cabin, lvl uint8, weather float64, cx, gyr, hw, wallH int) {
	put := func(x, y int, g rune, l uint8, idx uint64, fall float64) {
		if weather > 0 && xnoise.Unit(cb.Seed, 0xCAB, idx) < weather*fall {
			return
		}
		p.C.Rune(x, y, g, l)
	}
	for row := 0; row < wallH; row++ {
		cy := gyr - row
		fall := 1.25 // upper courses tumble first
		if row == 0 {
			fall = 0.92 // sills sink into the ground and stay
		}
		for i := -hw; i <= hw; i++ {
			g := '═'
			l := lvl - 20
			if i == -hw || i == hw {
				g, l = '╪', lvl-12 // crossed log ends at the corners
			}
			put(cx+i, cy, g, l, uint64(row*64+i+32), fall)
		}
	}
	// Hand-hewn sill ends poke past the corners.
	put(cx-hw-1, gyr, '═', lvl-32, 900, 0.6)
	put(cx+hw+1, gyr, '═', lvl-32, 901, 0.6)
}

// cabinDoor hangs the plank door near one corner, and lets it fall open into
// a dark doorway as neglect deepens. The worn threshold outlives everything.
func (p *P) cabinDoor(cb Cabin, lvl uint8, d float64, cx, gyr, hw, side int) {
	doorC := cx + side*(hw-2)
	cells := []int{doorC}
	if cb.Tier > 0 {
		cells = append(cells, doorC-side)
	}
	for _, dc := range cells {
		for row := 0; row < 2; row++ {
			if d > 0.5 {
				// The outer plank hangs off its hinge, the rest is dark.
				if dc == doorC && row == 1 && d < 0.86 {
					p.C.Rune(dc, gyr-row, '╱', lvl-26)
				}
				continue
			}
			p.C.Rune(dc, gyr-row, '║', lvl-8)
		}
	}
	// Threshold stones, kept swept while tended, a doorstep to nowhere after.
	for i := -1; i <= 2; i++ {
		if xnoise.Unit(cb.Seed, 0xD5, uint64(i+4)) < 0.8 {
			p.C.Dot(doorC*2+i, cb.GroundY+1, 52)
		}
	}
}

// cabinWindows sets moonlit glass opposite the door: bright while someone
// lives here, dimming with the quiet, a dark hole once broken. A finished
// homestead is shuttered instead.
func (p *P) cabinWindows(cb Cabin, lvl uint8, d, weather float64, cx, gyr, hw, side int) {
	draw := func(wc int, idx uint64) {
		if cb.Carve >= carveShutter {
			p.C.Rune(wc, gyr-1, '▤', lvl-2)
			return
		}
		if d > 0.55 {
			return // broken out; the wall gap says the rest
		}
		glass := uint8(186 - 118*xnoise.Smoothstep(0.03, 0.30, d))
		if weather > 0 && xnoise.Unit(cb.Seed, 0xF7, idx) < weather*0.8 {
			return
		}
		p.C.Rune(wc, gyr-1, '□', glass)
	}
	draw(cx-side*(hw-2), 1)
	if cb.Tier >= 2 {
		draw(cx+side*1, 2)
	}
}

// cabinChimney stacks the stone flue from the ground through the roof every
// frame. While the cabin stands only the stack above the slope shows; as the
// walls fall the whole chimney is simply revealed. Ruins never disappear.
// Smoke is the first trace of life and the first to go.
func (p *P) cabinChimney(cb Cabin, lvl uint8, cx, roofY0 int, rise, span float64, wallW, side int) {
	chimX := cb.X - side*(wallW-4)
	fyc := math.Abs(float64(chimX-cb.X)) / span
	slopeY := roofY0 + int(fyc*rise)
	top := slopeY - 7
	for y := top; y <= cb.GroundY; y++ {
		x0, x1 := chimX-1, chimX+1
		if y <= top+1 {
			x0, x1 = chimX-2, chimX+2 // a wider capstone course
		}
		for x := x0; x <= x1; x++ {
			if xnoise.Unit(cb.Seed, 0x57, uint64(x+256), uint64(y)) < 0.86 {
				l := int(lvl) - 18 + int(xnoise.Hash(cb.Seed, 0x58, uint64(x+256), uint64(y))%12)
				p.C.Dot(x, y, uint8(l))
			}
		}
	}
	if cb.Carve >= 1 || cb.Bare {
		return // a kept or outbuilding hearth is cold
	}
	fresh := 1 - xnoise.Smoothstep(0.008, 0.085, cb.Decay)
	if cb.Carve > 0 {
		// The ceremony: one last full plume, however quiet the hearth had
		// grown, thinning to nothing as the stone spreads down the board.
		fresh = 1 - xnoise.Smoothstep(carvePlume0, carvePlume1, cb.Carve)
	}
	if fresh <= 0.02 {
		return
	}
	// Woodsmoke: firelit at the mouth so it reads even against foliage,
	// widening and thinning as it climbs, wandering with the same wind that
	// moves the trees.
	length := 9 + int(21*fresh)
	phase := xnoise.Range(cb.Seed, 0, 6.28, 0x5B)
	for s := 0; s < length; s++ {
		y := top - 1 - s
		fs := float64(s) / float64(length)
		den := fresh * (1 - fs*fs)
		k := xnoise.Value2(cb.Seed^0x50F0, float64(s)*0.42, p.T*0.85)
		if k > 0.28+0.60*den {
			continue
		}
		drift := math.Sin(p.T*0.85+float64(s)*0.34+phase)*(0.5+fs*2.8) +
			p.wind(float64(chimX), float64(y))*fs*1.6
		x := chimX + int(math.Round(drift))
		l := uint8(58 + int(88*(1-fs*fs)*(0.4+0.6*fresh)))
		p.C.Dot(x, y, l)
		if s < 3 {
			p.C.Dot(x+1, y, l-10) // the dense lit column at the mouth
		} else if fs > 0.4 && k < den*0.55 {
			p.C.Dot(x+1, y, l-14)
		}
	}
}

// cabinWoodstore keeps the cord: a lean-to off the chimney end sheltering
// stacked log ends. The pile dwindles as the town idles, strays scatter into
// the grass, and the grass swallows those too. A finished homestead keeps a
// full cord, squared away.
func (p *P) cabinWoodstore(cb Cabin, lvl uint8, d float64, wallW, side int) {
	wallEdge := cb.X - side*wallW
	outX := wallEdge - side*11
	_, wallH, _ := CabinDims(cb.Tier)
	eaveY := (cb.GroundY/4-wallH+1)*4 + 2

	if cb.Tier >= 1 && (d < 0.5 || cb.Carve >= carveCord) {
		// The lean-to roof, one plank thick, and its corner post.
		steps := 11
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			x := wallEdge - side*int(t*11)
			y := eaveY + int(t*7)
			if xnoise.Unit(cb.Seed, 0x61, uint64(i)) < 0.88 {
				p.C.Dot(x, y, lvl-20)
				p.C.Dot(x, y+1, lvl-26)
			}
		}
		for y := eaveY + 9; y <= cb.GroundY; y++ {
			if xnoise.Unit(cb.Seed, 0x62, uint64(y)) < 0.9 {
				p.C.Dot(outX, y, lvl-24)
			}
		}
	}

	rows := [][2]int{} // {logs in course}
	switch {
	case cb.Tier >= 2:
		rows = [][2]int{{4, 0}, {3, 0}, {2, 0}}
	case cb.Tier == 1:
		rows = [][2]int{{3, 0}, {2, 0}, {1, 0}}
	default:
		rows = [][2]int{{2, 0}, {1, 0}}
	}
	total := 0
	for _, r := range rows {
		total += r[0]
	}
	remaining := total
	if cb.Carve < carveCord {
		remaining = int(math.Ceil(float64(total) * (1 - xnoise.Smoothstep(0.04, 0.52, d))))
	}
	drawn := 0
	for r, row := range rows {
		for j := 0; j < row[0]; j++ {
			if drawn >= remaining {
				break
			}
			drawn++
			x := wallEdge - side*(3+j*3+r)
			y := cb.GroundY - r*2
			p.C.Dot(x, y, lvl-24)
			p.C.Dot(x-1, y, lvl-30)
			p.C.Dot(x, y-1, lvl-14) // end grain catching light
			p.C.Dot(x-1, y-1, lvl-28)
		}
	}
	// What was taken and never restacked lies where it fell, until the grass
	// closes over it.
	scattered := total - remaining
	if scattered > 0 && d < 0.72 {
		if scattered > 4 {
			scattered = 4
		}
		for i := 0; i < scattered; i++ {
			if xnoise.Unit(cb.Seed, 0x66, uint64(i)) < xnoise.Smoothstep(0.5, 0.72, d) {
				continue
			}
			x := wallEdge - side*(2+int(xnoise.Unit(cb.Seed, 0x67, uint64(i))*17))
			p.C.Dot(x, cb.GroundY, lvl-34)
			p.C.Dot(x+1, cb.GroundY, lvl-38)
		}
	}
}

// cabinStump is the chopping block in the dooryard. The axe stands in it
// while the work is fresh, lies beside it a while, then is simply gone. The
// stump, like all stumps, stays.
func (p *P) cabinStump(cb Cabin, lvl uint8, d float64, gyr, wallW, side int) {
	sx := cb.X + side*(wallW+9)
	p.C.Rune(sx/2, gyr, '▂', lvl-26)
	p.C.Dot(sx-1, cb.GroundY-4, lvl-38)
	p.C.Dot(sx+1, cb.GroundY-5, lvl-40)
	if cb.Carve >= carveCord {
		return // the axe is hung up; the work is done
	}
	switch {
	case d < 0.15:
		p.C.Rune(sx/2, gyr-1, '╱', lvl-4)
		hx := (sx / 2) * 2
		p.C.Dot(hx-1, (gyr-1)*4, lvl+18) // the bit, a glint of steel
		p.C.Dot(hx-1, (gyr-1)*4+1, lvl+8)
		p.C.Dot(hx-2, (gyr-1)*4+1, lvl-2)
	case d < 0.42:
		p.C.Dot(sx+side*4, cb.GroundY-1, lvl+6) // fallen in the grass
		p.C.Dot(sx+side*5, cb.GroundY-1, lvl-6)
	}
}
