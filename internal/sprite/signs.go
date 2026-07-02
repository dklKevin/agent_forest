package sprite

import (
	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Sign draws a town's name plaque on a post. The name is the world's focal
// point and carries the single warm accent. Neglect tilts and weathers the
// plaque; the name dims with the wood but never becomes unreadable, because
// a town never loses its name.
type Sign struct {
	Seed     uint64
	X        int // center, dots
	GroundY  int // dots
	Name     string
	Lvl      uint8   // frame brightness
	Acc      uint8   // name warmth
	Decay    float64 // weathers the plaque
	Monument bool    // finished towns get the carved frame
	Focused  bool
}

// DrawSign renders the plaque, post, and monument dressing.
func (p *P) DrawSign(s Sign) {
	w := len(s.Name) + 4
	cx := s.X/2 - w/2
	gy := s.GroundY / 4
	top := gy - 3

	frameLvl := s.Lvl
	nameLvl := uint8(210)
	acc := s.Acc
	if s.Focused {
		frameLvl += 25
		nameLvl = 235
	}
	weather := xnoise.Smoothstep(0.25, 0.95, s.Decay)
	frameLvl = uint8(float64(frameLvl) * (1 - 0.45*weather))
	nameLvl = uint8(float64(nameLvl) * (1 - 0.55*weather))
	if nameLvl < 70 {
		nameLvl = 70 // legible always
	}
	acc = uint8(float64(acc) * (1 - 0.35*weather))

	tl, tr, bl, br := '╭', '╮', '╰', '╯'
	hz, vt, stem := '─', '│', '┬'
	if s.Monument {
		tl, tr, bl, br = '╔', '╗', '╚', '╝'
		hz, vt, stem = '═', '║', '╦'
	}

	// Tilt: neglected plaques slump to one side, row by row.
	tilt := 0
	if !s.Monument && s.Decay > 0.35 {
		tilt = 1
		if xnoise.Hash(s.Seed, 0x716)%2 == 0 {
			tilt = -1
		}
		if s.Decay > 0.7 {
			tilt *= 2
		}
	}
	rowOff := func(row int) int { // 0 top, 2 bottom
		return tilt * (2 - row) / 2
	}

	// The plaque clears its own backdrop so the name is readable against any
	// foliage. Weathered plaques stop clearing where they have rotted open.
	if weather < 0.55 {
		for row := 0; row < 3; row++ {
			p.C.ClearRect(cx+rowOff(row), top+row, w, 1)
		}
	}

	put := func(x, y int, r rune, lvl uint8, a uint8, idx uint64) {
		if weather > 0 && xnoise.Unit(s.Seed, 0x77, idx) < weather*0.55 {
			return // weathered away
		}
		if a > 0 {
			p.C.RuneAcc(x, y, r, lvl, a)
		} else {
			p.C.Rune(x, y, r, lvl)
		}
	}

	// Top row.
	o := rowOff(0)
	put(cx+o, top, tl, frameLvl, 0, 1)
	for i := 1; i < w-1; i++ {
		put(cx+o+i, top, hz, frameLvl, 0, uint64(10+i))
	}
	put(cx+o+w-1, top, tr, frameLvl, 0, 2)
	// Name row: frame may rot, the name persists.
	o = rowOff(1)
	put(cx+o, top+1, vt, frameLvl, 0, 3)
	p.C.Text(cx+o+2, top+1, s.Name, nameLvl, acc)
	put(cx+o+w-1, top+1, vt, frameLvl, 0, 4)
	// Bottom row with post stem.
	o = rowOff(2)
	put(cx+o, top+2, bl, frameLvl, 0, 5)
	for i := 1; i < w-1; i++ {
		g := hz
		if i == w/2 {
			g = stem
		}
		put(cx+o+i, top+2, g, frameLvl, 0, uint64(30+i))
	}
	put(cx+o+w-1, top+2, br, frameLvl, 0, 6)
	// Post to ground.
	postG := vt
	if s.Monument {
		postG = '║'
	}
	for y := top + 3; y <= gy; y++ {
		put(cx+w/2, y, postG, frameLvl-10, 0, uint64(50+y))
	}

	if s.Monument {
		p.monumentDressing(s, cx, w, top, gy)
	}
}

// monumentDressing adds the completion cues: a carved finial and a pair of
// stone cairns flanking the post.
func (p *P) monumentDressing(s Sign, cx, w, top, gy int) {
	p.C.Rune(cx+w/2, top-1, '◆', s.Lvl+30)
	for side := -1; side <= 1; side += 2 {
		bx := s.X + side*(w+3)
		p.cairn(xnoise.Hash(s.Seed, uint64(side+7)), bx, s.GroundY, s.Lvl-15)
	}
}

// cairn stacks a small pyramid of stones in dots.
func (p *P) cairn(seed uint64, x, gy int, lvl uint8) {
	rows := [][2]int{{-2, 2}, {-1, 1}, {0, 0}}
	for i, r := range rows {
		y := gy - i
		for dx := r[0]; dx <= r[1]; dx++ {
			if xnoise.Unit(seed, uint64(i), uint64(dx+8)) < 0.9 {
				p.C.Dot(x+dx, y, lvl-uint8(i*6))
			}
		}
	}
}

// DrawTags plants release stakes along the trail east of the sign: one small
// post per release, up to five, then they read simply as "several".
func (p *P) DrawTags(seed uint64, signX, gy int, count int, lvl uint8, decay float64) {
	n := count
	if n > 5 {
		n = 5
	}
	for i := 0; i < n; i++ {
		x := signX + (i+2)*7
		cx, cy := x/2, gy/4
		if decay > 0.4 && xnoise.Unit(seed, 0x7A, uint64(i)) < decay*0.7 {
			// A stake lost to the undergrowth, or tipped over.
			if xnoise.Hash(seed, 0x7B, uint64(i))%2 == 0 {
				p.C.Rune(cx, cy, '╱', lvl-30)
			}
			continue
		}
		p.C.Rune(cx, cy, '╹', lvl)
		p.C.Dot(x+1, gy-4, lvl-25) // a little banner nub
	}
}
