// The settlement: a town's components stand as outbuildings around the
// hearth. Every form is told apart by silhouette alone, follows the cabin's
// grammar (runes for structure, dots for mass), rots under the same patch
// noise as the canopy, shows its own tended traces by density and shape, and
// leaves its own bones when it falls. Nothing here touches the accent channel.
package sprite

import (
	"math"

	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Building is one settlement structure, fully described so drawing is
// deterministic.
type Building struct {
	Seed     uint64
	X        int // center, dots
	GroundY  int // baseline, dots
	Form     model.BuildingForm
	Share    float64 // of the largest component, scales the form
	Lvl      uint8
	Decay    float64
	Tend     float64 // 1 worked today .. 0 quiet: this component's own clock
	Finished bool
	Focused  bool
}

// BuildingDims returns wall width in cells (odd) and total height in cell
// rows for layout purposes.
func BuildingDims(f model.BuildingForm, share float64) (wallW, rows int) {
	s := xnoise.Clamp(share, 0, 1)
	switch f {
	case model.FormBarn:
		w := 13 + int(s*6)
		return w | 1, 7
	case model.FormHomeplace:
		w, h, r := CabinDims(homeplaceTier(s))
		return w, h + r
	case model.FormWorkshop:
		return 9, 4
	case model.FormWatchtower:
		return 5, 7 + int(s*2)
	case model.FormSchoolhouse:
		return 9 + 2*int(s*1.5), 7
	case model.FormCrib:
		return 5, 3
	default: // shed
		return 7, 3
	}
}

// BuildingYard returns the half-widths in dots of ground the settlers keep
// clear around a form: front west, front east, and behind. Small forms keep
// small yards, so the woods stay close.
func BuildingYard(f model.BuildingForm, share float64) (west, east, back int) {
	w, _ := BuildingDims(f, share)
	switch f {
	case model.FormBarn:
		return w + 9, w + 9, w + 4
	case model.FormHomeplace:
		return w + 7, w + 7, w + 4
	case model.FormWatchtower:
		return w + 3, w + 3, w + 2
	default:
		return w + 5, w + 5, w + 3
	}
}

func homeplaceTier(share float64) int {
	if share >= 0.7 {
		return 1
	}
	return 0
}

// DrawBuilding renders one settlement structure with its current tend and decay.
func (p *P) DrawBuilding(b Building) {
	b.Decay = xnoise.Clamp(b.Decay, 0, 1)
	b.Tend = xnoise.Clamp(b.Tend, 0, 1)
	if b.Finished {
		// A kept building is stilled by its Finished gates alone (no tools
		// out, no pennant, no grain); the stances that read "kept as built" -
		// the workshop's open doorway - stay exactly as the monument holds them.
		b.Tend = 1
	}
	if b.Focused {
		b.Lvl += 8
	}
	switch b.Form {
	case model.FormBarn:
		p.barn(b)
	case model.FormHomeplace:
		p.DrawCabin(Cabin{
			Seed: b.Seed, X: b.X, GroundY: b.GroundY,
			Tier: homeplaceTier(b.Share), Lvl: b.Lvl,
			Decay: b.Decay, Tend: b.Tend, Carve: carve01(b.Finished), Bare: true,
		})
	case model.FormWorkshop:
		p.workshop(b)
	case model.FormWatchtower:
		p.watchtower(b)
	case model.FormSchoolhouse:
		p.schoolhouse(b)
	case model.FormCrib:
		p.crib(b)
	default:
		p.shedB(b)
	}
}

// bWeather is the structural failure gate shared by every form.
func bWeather(d float64) float64 { return xnoise.Smoothstep(0.32, 0.97, d) }

// bRuin dissolves what weather leaves.
func bRuin(d float64) float64 { return xnoise.Smoothstep(0.86, 0.94, d) }

// bput places a structural rune unless weather has taken that cell.
func (p *P) bput(seed uint64, weather float64, x, y int, g rune, l uint8, idx uint64, fall float64) {
	if weather > 0 && xnoise.Unit(seed, 0xB1D, idx) < weather*fall {
		return
	}
	p.C.Rune(x, y, g, l)
}

// dotRoof fills a roof between topY and botY where half(fy) gives the
// half-span in dots at each depth. It clears the grove behind interior
// cells, combs shake courses, eats decay patches with the canopy's noise,
// and brightens the rim, exactly like the hearth's roof.
func (p *P) dotRoof(seed uint64, cx, topY, botY int, half func(float64) float64, lvl uint8, d, ruin float64) {
	if ruin >= 1 {
		return
	}
	rise := float64(botY - topY)
	for rr := topY / 4; rr <= botY/4; rr++ {
		fyTop := (float64(rr*4) - float64(topY)) / rise
		if fyTop < 0 {
			fyTop = 0
		}
		inner := int(half(fyTop))/2 - 1
		if inner > 0 {
			p.C.ClearRect(cx/2-inner, rr, inner*2+1, 1)
		}
	}
	for y := topY; y <= botY; y++ {
		fy := (float64(y) - float64(topY)) / rise
		h := half(fy)
		for x := cx - int(h); x <= cx+int(h); x++ {
			nx := 0.0
			if h > 0.5 {
				nx = math.Abs(float64(x-cx)) / h
			}
			if y > topY+3 && (y-topY)%4 == 3 && xnoise.Value2(seed, float64(x)*0.3, float64(y)) < 0.55 {
				continue
			}
			wob := xnoise.Value2(seed^0x400F, float64(x)*0.33, float64(y)*0.6)
			if fy > 0.2 && fy < 0.93 && wob > 0.86 {
				continue
			}
			if d > 0.02 && xnoise.FBM2(seed^0xDECA, float64(x)*0.05, float64(y)*0.07, 2) < d*0.80 {
				continue
			}
			if ruin > 0 && xnoise.Unit(seed, 0xB01, uint64(x+512), uint64(y)) < ruin {
				continue
			}
			l := int(lvl) - 16 + int((wob-0.5)*20)
			if nx > 0.86 {
				l += 16
			}
			if y <= topY+1 {
				l += 12
			}
			if fy > 0.93 {
				l += 8
			}
			if l < 20 {
				l = 20
			}
			p.C.Dot(x, y, uint8(l))
		}
	}
}

// plankWalls lays board rows: thinner strokes than the hearth's log courses,
// which is how frame buildings differ from the hand-hewn home.
func (p *P) plankWalls(seed uint64, weather float64, cx, gyr, hw, wallH int, lvl uint8) {
	for row := 0; row < wallH; row++ {
		fall := 1.25
		if row == 0 {
			fall = 0.92
		}
		for i := -hw; i <= hw; i++ {
			g, l := '─', lvl-22
			if i == -hw || i == hw {
				g, l = '│', lvl-12
			}
			p.bput(seed, weather, cx+i, gyr-row, g, l, uint64(row*64+i+32), fall)
		}
	}
}

// foundation is the stone line every fallen frame building leaves.
func (p *P) foundation(seed uint64, x0, x1, gy int, lvl uint8, vis float64) {
	for x := x0; x <= x1; x += 2 {
		if xnoise.Unit(seed, 0xF0D, uint64(x)) < 0.7*vis {
			p.C.Dot(x, gy, lvl-26)
		}
	}
}

// ---- barn ----------------------------------------------------------------

// barn: the dominant component. A broad gambrel roof over board walls, big
// cross-braced doors that stand open with hay spilling while the work is
// fresh. Its bones are the foundation line and a leaning ridge pole.
func (p *P) barn(b Building) {
	wallW, _ := BuildingDims(b.Form, b.Share)
	wallH := 3
	roofRise := 4
	weather := bWeather(b.Decay)
	ruin := bRuin(b.Decay)
	fresh := b.Tend
	cx := b.X / 2
	gyr := b.GroundY / 4
	hw := wallW / 2
	span := float64(wallW + 2)
	topY := (gyr - wallH - roofRise + 1) * 4
	botY := (gyr-wallH)*4 + 3

	// The gambrel: shallow to the break, steep to the eaves.
	p.dotRoof(b.Seed, b.X, topY, botY, func(fy float64) float64 {
		if fy < 0.45 {
			return span * 0.55 * (fy / 0.45)
		}
		return span * (0.55 + 0.45*(fy-0.45)/0.55)
	}, b.Lvl, b.Decay, ruin)

	// Loft door in the upper gable.
	if weather < 0.7 {
		p.C.Rune(cx, topY/4+1, '▯', b.Lvl-30)
	}
	p.plankWalls(b.Seed, weather, cx, gyr, hw, wallH, b.Lvl)

	// The doors: two leaves, cross-braced. Fresh work leaves them open with
	// hay scattered at the threshold; neglect leaves one hanging, then a gap.
	open := fresh > 0.35 && !b.Finished
	if open {
		p.C.ClearRect(cx-1, gyr-1, 2, 2) // the dark opening
	}
	for row := 0; row < 2; row++ {
		for i := -2; i <= 1; i++ {
			x := cx + i
			if open && (i == -1 || i == 0) {
				continue
			}
			g := '║'
			if row == 1 && (i == -2 || i == 1) {
				g = '╳'
			}
			if b.Decay > 0.5 && !open {
				if i == -1 && row == 1 && b.Decay < 0.86 {
					p.C.Rune(x, gyr-row, '╱', b.Lvl-26)
				}
				continue
			}
			p.bput(b.Seed, weather, x, gyr-row, g, b.Lvl-10, uint64(200+row*8+i+4), 1.1)
		}
	}
	if open {
		p.C.Rune(cx-3, gyr, '╱', b.Lvl-16) // leaves swung wide
		p.C.Rune(cx+2, gyr, '╲', b.Lvl-16)
		hay := 3 + int(4*fresh) // the threshold scatter thins as the work cools
		for i := 0; i < hay; i++ {
			dx := int(xnoise.Range(b.Seed, -6, 7, 0x8A1, uint64(i)))
			p.C.Dot(b.X+dx, b.GroundY+1-int(xnoise.Hash(b.Seed, 0x8A2, uint64(i))%2), b.Lvl-24)
		}
	}
	// Bones.
	if ruin > 0.3 {
		p.foundation(b.Seed, b.X-wallW, b.X+wallW, b.GroundY, b.Lvl, ruin)
		rx := cx - 2
		for i := 0; i < 3; i++ { // the leaning ridge pole
			p.C.Rune(rx+i, gyr-1-i, '╱', b.Lvl-20)
		}
	}
	p.vines(xnoise.Hash(b.Seed, 0xC1), b.X-wallW-2, b.GroundY, wallH*4+8, b.Lvl-40, b.Decay)
	p.vines(xnoise.Hash(b.Seed, 0xC2), b.X+wallW+2, b.GroundY, wallH*4+8, b.Lvl-40, b.Decay)
}

// ---- watchtower ----------------------------------------------------------

// watchtower: the tests stand guard. Braced legs, a ladder, a small cab with
// a pyramid roof, and a pennant streaming on the wind while the tests are
// fresh. Its bones are two leaning posts.
func (p *P) watchtower(b Building) {
	weather := bWeather(b.Decay)
	ruin := bRuin(b.Decay)
	fresh := b.Tend
	cx := b.X / 2
	gyr := b.GroundY / 4
	_, rows := BuildingDims(b.Form, b.Share)
	legRows := rows - 3
	platRow := gyr - legRows
	cabRow := platRow - 1
	roofRow := cabRow - 1

	if ruin > 0.55 {
		// Two leaning posts and the ladder's first rung.
		p.C.Rune(cx-2, gyr, '╱', b.Lvl-18)
		p.C.Rune(cx-1, gyr-1, '╱', b.Lvl-26)
		p.C.Rune(cx+2, gyr, '╲', b.Lvl-22)
		p.C.Dot(b.X, b.GroundY-2, b.Lvl-30)
		return
	}
	for r := 0; r < legRows; r++ {
		p.bput(b.Seed, weather, cx-2, gyr-r, '│', b.Lvl-14, uint64(300+r), 0.8)
		p.bput(b.Seed, weather, cx+2, gyr-r, '│', b.Lvl-14, uint64(340+r), 0.8)
		if r%2 == 1 {
			p.bput(b.Seed, weather, cx, gyr-r, '╳', b.Lvl-24, uint64(380+r), 1.3)
		}
	}
	// Ladder up the middle.
	for y := b.GroundY - 2; y > platRow*4+3; y -= 3 {
		if xnoise.Unit(b.Seed, 0x1AD, uint64(y)) < 0.8*(1-weather) {
			p.C.Dot(b.X, y, b.Lvl-30)
		}
	}
	// Platform, cab, and roof.
	for i := -2; i <= 2; i++ {
		p.bput(b.Seed, weather, cx+i, platRow, '═', b.Lvl-12, uint64(420+i+4), 1.0)
	}
	p.bput(b.Seed, weather, cx-1, cabRow, '╪', b.Lvl-14, 430, 1.1)
	glass := uint8(180 - 110*xnoise.Smoothstep(0.03, 0.30, b.Decay))
	if b.Decay <= 0.55 {
		p.C.Rune(cx, cabRow, '▫', glass)
	}
	p.bput(b.Seed, weather, cx+1, cabRow, '╪', b.Lvl-14, 431, 1.1)
	for y := roofRow * 4; y < roofRow*4+4; y++ {
		h := 5 - (y - roofRow*4)
		for x := b.X - h; x <= b.X+h; x++ {
			if xnoise.Unit(b.Seed, 0x700, uint64(x+128), uint64(y)) < 0.8*(1-weather*0.8) {
				p.C.Dot(x, y, b.Lvl-10)
			}
		}
	}
	// The pennant: a streamer off the roof peak while the watch is kept.
	if fresh > 0.05 && !b.Finished {
		tipY := roofRow*4 - 1
		dir := 1.0
		if p.wind(float64(b.X), float64(tipY)) < 0 {
			dir = -1
		}
		p.C.Dot(b.X, tipY, b.Lvl-4)
		n := 2 + int(3*fresh)
		for i := 1; i <= n; i++ {
			wob := math.Sin(p.T*2.1+float64(i)*0.9) * 0.9
			p.C.Dot(b.X+int(dir*float64(i)), tipY+int(wob)-i/3, uint8(float64(b.Lvl)-8-float64(i)*6))
		}
	}
}

// ---- schoolhouse ----------------------------------------------------------

// schoolhouse: the docs. A trim gable with tall windows, front steps, and a
// bell cupola at the ridge. When it falls, the bell lies in the grass by the
// step, a bright dot in a dark mound.
func (p *P) schoolhouse(b Building) {
	wallW, _ := BuildingDims(b.Form, b.Share)
	wallH := 3
	roofRise := 3
	weather := bWeather(b.Decay)
	ruin := bRuin(b.Decay)
	cx := b.X / 2
	gyr := b.GroundY / 4
	hw := wallW / 2
	span := float64(wallW + 2)
	topY := (gyr - wallH - roofRise + 1) * 4
	botY := (gyr-wallH)*4 + 3

	p.dotRoof(b.Seed, b.X, topY, botY, func(fy float64) float64 { return span * fy }, b.Lvl, b.Decay, ruin)
	p.plankWalls(b.Seed, weather, cx, gyr, hw, wallH, b.Lvl)

	// Center door with front steps; tall windows flanking.
	for row := 0; row < 2; row++ {
		if b.Decay > 0.5 {
			if row == 1 && b.Decay < 0.86 {
				p.C.Rune(cx, gyr-row, '╱', b.Lvl-26)
			}
			continue
		}
		p.bput(b.Seed, weather, cx, gyr-row, '║', b.Lvl-8, uint64(210+row), 1.0)
	}
	for x := b.X - 3; x <= b.X+3; x++ {
		if xnoise.Unit(b.Seed, 0xD5, uint64(x+64)) < 0.75 {
			p.C.Dot(x, b.GroundY+1, 52)
		}
	}
	glass := uint8(186 - 118*xnoise.Smoothstep(0.03, 0.30, b.Decay))
	for _, wx := range []int{cx - hw + 2, cx + hw - 2} {
		for row := 1; row < 3; row++ { // tall windows, two rows
			if b.Decay > 0.55 {
				continue
			}
			g := '▯'
			l := glass
			if b.Finished {
				g, l = '▤', b.Lvl-2
			}
			if weather > 0 && xnoise.Unit(b.Seed, 0xF7, uint64(wx*4+row)) < weather*0.8 {
				continue
			}
			p.C.Rune(wx, gyr-row, g, l)
		}
	}
	// The bell cupola, and the fallen bell once the cupola goes.
	bellX, bellTop := b.X, topY-4
	if ruin < 0.4 && b.Decay < 0.72 {
		for dy := 0; dy < 3; dy++ {
			p.C.Dot(bellX-2, bellTop+dy, b.Lvl-14)
			p.C.Dot(bellX+2, bellTop+dy, b.Lvl-14)
		}
		p.C.Dot(bellX-1, bellTop-1, b.Lvl-8)
		p.C.Dot(bellX, bellTop-2, b.Lvl-6)
		p.C.Dot(bellX+1, bellTop-1, b.Lvl-8)
		p.C.Dot(bellX, bellTop+1, b.Lvl+22) // the bell, catching moonlight
	} else {
		p.C.Dot(b.X+4, b.GroundY-1, b.Lvl+18) // the bell in the grass
		p.C.Dot(b.X+3, b.GroundY, b.Lvl-24)
		p.C.Dot(b.X+5, b.GroundY, b.Lvl-28)
	}
	if ruin > 0.3 {
		p.foundation(b.Seed, b.X-wallW, b.X+wallW, b.GroundY, b.Lvl, ruin)
	}
	p.vines(xnoise.Hash(b.Seed, 0xC1), b.X-wallW-2, b.GroundY, wallH*4+8, b.Lvl-40, b.Decay)
}

// ---- workshop --------------------------------------------------------------

// workshop: a middle component. Single-slope roof, a wide doorway, and a
// bench outside where the tools sit while the work is fresh.
func (p *P) workshop(b Building) {
	wallW, _ := BuildingDims(b.Form, b.Share)
	wallH := 2
	weather := bWeather(b.Decay)
	ruin := bRuin(b.Decay)
	fresh := b.Tend
	cx := b.X / 2
	gyr := b.GroundY / 4
	hw := wallW / 2
	side := 1
	if xnoise.Hash(b.Seed, 0xD0)%2 == 0 {
		side = -1
	}
	// Skillion roof: high at the door end, low at the far end.
	topY := (gyr - wallH - 1) * 4
	if ruin < 1 {
		for y := topY; y < topY+8; y++ {
			fy := float64(y-topY) / 8
			for i := -hw - 1; i <= hw+1; i++ {
				x := b.X + i*2
				lift := float64(side*i+hw) / float64(wallW) // 0 far .. 1 door end
				if fy < (1-lift)*0.75 {
					continue
				}
				if b.Decay > 0.02 && xnoise.FBM2(b.Seed^0xDECA, float64(x)*0.05, float64(y)*0.07, 2) < b.Decay*0.8 {
					continue
				}
				if ruin > 0 && xnoise.Unit(b.Seed, 0xB01, uint64(x+512), uint64(y)) < ruin {
					continue
				}
				l := int(b.Lvl) - 14 + int(6*lift)
				p.C.Dot(x, y, uint8(l))
				p.C.Dot(x+1, y, uint8(l-6))
			}
		}
	}
	p.plankWalls(b.Seed, weather, cx, gyr, hw, wallH, b.Lvl)
	// The wide doorway, dark when open.
	doorC := cx + side*(hw-2)
	for row := 0; row < 2; row++ {
		if fresh > 0.35 || b.Decay > 0.5 {
			continue // open while working; fallen open when left
		}
		p.bput(b.Seed, weather, doorC, gyr-row, '║', b.Lvl-10, uint64(220+row), 1.0)
		p.bput(b.Seed, weather, doorC-side, gyr-row, '║', b.Lvl-10, uint64(224+row), 1.0)
	}
	// The bench, and tools out on it while the component is worked.
	bx := b.X - side*(wallW+4)
	if b.Decay < 0.72 {
		for i := -2; i <= 2; i++ {
			p.C.Dot(bx+i, b.GroundY-3, b.Lvl-18)
		}
		p.C.Dot(bx-2, b.GroundY-1, b.Lvl-30)
		p.C.Dot(bx+2, b.GroundY-1, b.Lvl-30)
		if fresh > 0.2 && !b.Finished {
			p.C.Rune(bx/2, gyr-1, '╱', b.Lvl-6) // a saw against the bench
			p.C.Dot(bx+3, b.GroundY-4, b.Lvl+14)
		}
	}
	if ruin > 0.3 {
		p.foundation(b.Seed, b.X-wallW, b.X+wallW, b.GroundY, b.Lvl, ruin)
	}
}

// ---- shed and crib ---------------------------------------------------------

// shedB: a minor component, one room of boards under a lean-to roof, its
// doorway showing stacked stores while the component is worked.
func (p *P) shedB(b Building) {
	wallW, _ := BuildingDims(b.Form, b.Share)
	wallH := 2
	weather := bWeather(b.Decay)
	ruin := bRuin(b.Decay)
	fresh := b.Tend
	cx := b.X / 2
	gyr := b.GroundY / 4
	hw := wallW / 2
	topY := (gyr - wallH) * 4
	if ruin < 1 {
		for y := topY - 4; y <= topY+2; y++ {
			fy := float64(y-topY+4) / 6
			h := float64(wallW) * (0.4 + 0.6*fy)
			for x := b.X - int(h); x <= b.X+int(h); x++ {
				if xnoise.Value2(b.Seed^0x400F, float64(x)*0.33, float64(y)*0.6) > 0.82 {
					continue
				}
				if b.Decay > 0.02 && xnoise.FBM2(b.Seed^0xDECA, float64(x)*0.05, float64(y)*0.07, 2) < b.Decay*0.8 {
					continue
				}
				if ruin > 0 && xnoise.Unit(b.Seed, 0xB01, uint64(x+512), uint64(y)) < ruin {
					continue
				}
				p.C.Dot(x, y, b.Lvl-14)
			}
		}
	}
	p.plankWalls(b.Seed, weather, cx, gyr, hw, wallH, b.Lvl)
	// The doorway, with the stores visible while stocked.
	if b.Decay < 0.5 {
		p.C.Rune(cx+hw-1, gyr, '║', b.Lvl-12)
	}
	if fresh > 0.25 && !b.Finished {
		p.C.Dot(b.X+(hw-1)*2-3, b.GroundY-1, b.Lvl-6)
		p.C.Dot(b.X+(hw-1)*2-4, b.GroundY-2, b.Lvl-12)
	}
	if ruin > 0.3 {
		p.foundation(b.Seed, b.X-wallW+2, b.X+wallW-2, b.GroundY, b.Lvl, ruin)
	}
}

// crib: the smallest store, a slatted box up on stilts to keep the damp out.
// Its bones are the stilts, still standing.
func (p *P) crib(b Building) {
	weather := bWeather(b.Decay)
	ruin := bRuin(b.Decay)
	fresh := b.Tend
	cx := b.X / 2
	gyr := b.GroundY / 4
	// Stilts survive nearly everything.
	p.bput(b.Seed, weather*0.4, cx-1, gyr, '╹', b.Lvl-16, 500, 1.0)
	p.bput(b.Seed, weather*0.4, cx+1, gyr, '╹', b.Lvl-16, 501, 1.0)
	if ruin > 0.55 {
		return
	}
	for i := -2; i <= 2; i++ {
		p.bput(b.Seed, weather, cx+i, gyr-1, '─', b.Lvl-20, uint64(510+i+2), 1.1)
	}
	// Grain between the slats while the crib is kept filled.
	if fresh > 0.25 && !b.Finished {
		for i := -3; i <= 3; i += 2 {
			p.C.Dot(b.X+i, b.GroundY-6, b.Lvl-8)
		}
	}
	for y := (gyr - 2) * 4; y < (gyr-2)*4+4; y++ {
		h := 5 - (y - (gyr-2)*4)
		for x := b.X - h; x <= b.X+h; x++ {
			if xnoise.Unit(b.Seed, 0x701, uint64(x+128), uint64(y)) < 0.75*(1-weather) {
				p.C.Dot(x, y, b.Lvl-12)
			}
		}
	}
}

// ---- communal dressing ------------------------------------------------------

// DrawWell is the settlement's shared water: a stone ring, two posts, a
// windlass beam, and the bucket. It appears once a town has grown past its
// first outbuildings, and it outlasts most of them.
func (p *P) DrawWell(seed uint64, x, gy int, lvl uint8, d float64) {
	weather := xnoise.Smoothstep(0.5, 1.0, d)
	for i := -4; i <= 4; i++ {
		if xnoise.Unit(seed, 0x11E, uint64(i+8)) < 0.85 {
			p.C.Dot(x+i, gy-1, lvl-18)
			if i > -4 && i < 4 {
				p.C.Dot(x+i, gy-2, lvl-24)
			}
		}
	}
	if weather > 0.6 {
		return // the ring alone, filling with leaves
	}
	for dy := 3; dy <= 8; dy++ {
		p.C.Dot(x-3, gy-dy, lvl-22)
		p.C.Dot(x+3, gy-dy, lvl-22)
	}
	for i := -3; i <= 3; i++ {
		p.C.Dot(x+i, gy-9, lvl-16)
	}
	p.C.Dot(x, gy-7, lvl-10)
	p.C.Dot(x, gy-5, lvl+6) // the bucket
}

// DrawFence runs a fragment of split-rail fence between two yards: posts,
// two rails, seeded gaps. It is the first thing the forest swallows.
func (p *P) DrawFence(seed uint64, x0, x1, gy int, lvl uint8, d float64) {
	gone := xnoise.Smoothstep(0.12, 0.4, d)
	if gone >= 1 || x1-x0 < 8 {
		return
	}
	for x := x0; x <= x1; x += 7 + int(xnoise.Unit(seed, 0xFE0, uint64(x))*4) {
		if xnoise.Unit(seed, 0xFE1, uint64(x)) < gone {
			continue
		}
		for dy := 1; dy <= 4; dy++ {
			p.C.Dot(x, gy-dy, lvl-18)
		}
	}
	for x := x0; x <= x1; x++ {
		if xnoise.Unit(seed, 0xFE2, uint64(x)) < 0.75*(1-gone) {
			if x%3 != 0 {
				p.C.Dot(x, gy-3, lvl-26)
			}
			if x%4 != 1 {
				p.C.Dot(x, gy-2+((x/9)%2), lvl-30)
			}
		}
	}
}

// DrawFootpath wears a faint branch trail between a building and the hearth:
// trodden nearly unbroken while the building's component is worked, back to
// sparse dashes as it rests, fading out as it goes quiet.
func (p *P) DrawFootpath(seed uint64, x0, x1, gy int, d, tend float64) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	tl := 74 * (1 - d*1.2)
	if tl <= 28 {
		return
	}
	for x := x0; x <= x1; x++ {
		on := (x/3)%2 == 0
		if !on && tend > 0 {
			// Fresh feet close the gaps in the worn line.
			on = xnoise.Unit(seed, 0xF00, uint64(x)) < tend*0.55
		}
		if !on {
			continue
		}
		off := int((xnoise.FBM1(seed, float64(x)*0.02, 2) - 0.5) * 4)
		p.C.Dot(x, gy+2+off, uint8(tl))
	}
}
