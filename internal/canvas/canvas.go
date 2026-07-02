// Package canvas is the drawing surface for the forest: a braille dot layer
// for organic masses composited with a rune layer for structural strokes,
// shaded in greyscale with a single warm accent channel.
//
// Coordinates: cells are terminal character positions (W x H). Dots are the
// braille sub-grid, 2 wide and 4 tall per cell, so the dot grid is 2W x 4H.
// All organic drawing happens in dot space; runes and text land in cell space.
package canvas

import (
	"os"
	"strconv"
	"strings"
)

// Profile selects how color is emitted.
type Profile int

const (
	TrueColor Profile = iota
	ANSI256
	NoColor
)

// DetectProfile inspects the environment. Terminal.app has no truecolor, so
// the 256-grey ramp is a first-class citizen, not an afterthought.
func DetectProfile() Profile {
	ct := os.Getenv("COLORTERM")
	if strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return TrueColor
	}
	term := os.Getenv("TERM")
	if term == "dumb" || term == "" {
		return NoColor
	}
	return ANSI256
}

// Background is near-black with a whisper of blue: a moonlit dark, not void.
const (
	bgR, bgG, bgB = 9, 9, 14
	// Warm accent target: soft coral-amber. The only color in the world.
	accR, accG, accB = 235, 158, 106
)

type cell struct {
	dots uint8 // braille bits
	dlvl uint8 // dot brightness 0..255
	r    rune  // structural rune; overrides dots when set
	rlvl uint8
	acc  uint8 // warm blend 0..255, applies to whatever renders
}

// Canvas is a single frame's drawing surface. Reused across frames.
type Canvas struct {
	W, H    int
	cells   []cell
	profile Profile
	fgCache map[uint16]string
	buf     strings.Builder
}

// New allocates a canvas of w x h cells.
func New(w, h int, p Profile) *Canvas {
	return &Canvas{
		W: w, H: h,
		cells:   make([]cell, w*h),
		profile: p,
		fgCache: make(map[uint16]string, 128),
	}
}

// Resize adjusts the cell grid, dropping contents.
func (c *Canvas) Resize(w, h int) {
	c.W, c.H = w, h
	c.cells = make([]cell, w*h)
}

// Clear wipes the frame.
func (c *Canvas) Clear() {
	for i := range c.cells {
		c.cells[i] = cell{}
	}
}

// DotW and DotH are the dot-space dimensions.
func (c *Canvas) DotW() int { return c.W * 2 }
func (c *Canvas) DotH() int { return c.H * 4 }

// Braille bit for a dot at sub-position (x%2, y%4).
var brailleBit = [4][2]uint8{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

func (c *Canvas) at(cx, cy int) *cell {
	return &c.cells[cy*c.W+cx]
}

// Dot plants a braille dot at dot coordinates with a brightness level.
// Overlapping dots keep the brighter level.
func (c *Canvas) Dot(x, y int, lvl uint8) {
	if x < 0 || y < 0 || x >= c.W*2 || y >= c.H*4 {
		return
	}
	cl := c.at(x/2, y/4)
	cl.dots |= brailleBit[y%4][x%2]
	if lvl > cl.dlvl {
		cl.dlvl = lvl
	}
}

// DotAcc is Dot with a warm accent contribution.
func (c *Canvas) DotAcc(x, y int, lvl, acc uint8) {
	if x < 0 || y < 0 || x >= c.W*2 || y >= c.H*4 {
		return
	}
	cl := c.at(x/2, y/4)
	cl.dots |= brailleBit[y%4][x%2]
	if lvl > cl.dlvl {
		cl.dlvl = lvl
	}
	if acc > cl.acc {
		cl.acc = acc
	}
}

// Rune places a structural glyph at cell coordinates.
func (c *Canvas) Rune(cx, cy int, r rune, lvl uint8) {
	if cx < 0 || cy < 0 || cx >= c.W || cy >= c.H {
		return
	}
	cl := c.at(cx, cy)
	cl.r = r
	cl.rlvl = lvl
}

// RuneAcc places a glyph with warm accent.
func (c *Canvas) RuneAcc(cx, cy int, r rune, lvl, acc uint8) {
	if cx < 0 || cy < 0 || cx >= c.W || cy >= c.H {
		return
	}
	cl := c.at(cx, cy)
	cl.r = r
	cl.rlvl = lvl
	if acc > cl.acc {
		cl.acc = acc
	}
}

// Text writes a string of glyphs starting at cell coordinates.
func (c *Canvas) Text(cx, cy int, s string, lvl, acc uint8) {
	i := 0
	for _, r := range s {
		if acc > 0 {
			c.RuneAcc(cx+i, cy, r, lvl, acc)
		} else {
			c.Rune(cx+i, cy, r, lvl)
		}
		i++
	}
}

// ClearRect empties a cell-space rectangle (used as panel backdrop).
func (c *Canvas) ClearRect(cx, cy, w, h int) {
	for y := cy; y < cy+h; y++ {
		for x := cx; x < cx+w; x++ {
			if x < 0 || y < 0 || x >= c.W || y >= c.H {
				continue
			}
			*c.at(x, y) = cell{}
		}
	}
}

// Warm blends a soft radial accent over an elliptical cell-space region:
// the lantern light on the most recently tended town.
func (c *Canvas) Warm(cx, cy int, rx, ry float64, strength uint8) {
	x0, x1 := cx-int(rx)-1, cx+int(rx)+1
	y0, y1 := cy-int(ry)-1, cy+int(ry)+1
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if x < 0 || y < 0 || x >= c.W || y >= c.H {
				continue
			}
			dx := float64(x-cx) / rx
			dy := float64(y-cy) / ry
			q := dx*dx + dy*dy
			if q >= 1 {
				continue
			}
			fall := (1 - q) * (1 - q)
			a := uint8(float64(strength) * fall)
			cl := c.at(x, y)
			if a > cl.acc {
				cl.acc = a
			}
		}
	}
}

func lerp8(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}

// rgbFor computes the on-screen color for a brightness level and accent blend.
func rgbFor(lvl, acc uint8) (r, g, b uint8) {
	// Greys carry a faint cool tint so the amber accent reads warm against them.
	v := lvl
	r, g, b = v, v, v+v/9
	if int(v)+int(v/9) > 255 {
		b = 255
	}
	if acc > 0 {
		t := float64(acc) / 255
		r = lerp8(r, accR, t)
		g = lerp8(g, accG, t)
		b = lerp8(b, accB, t)
	}
	return
}

func to256(r, g, b uint8) int {
	// Near-grey colors use the fine 24-step grey ramp; the rest snap to the cube.
	maxc, minc := r, r
	for _, v := range []uint8{g, b} {
		if v > maxc {
			maxc = v
		}
		if v < minc {
			minc = v
		}
	}
	if maxc-minc < 24 {
		v := (int(r) + int(g) + int(b)) / 3
		if v < 5 {
			return 16
		}
		idx := (v - 8) / 10
		if idx < 0 {
			idx = 0
		}
		if idx > 23 {
			idx = 23
		}
		return 232 + idx
	}
	q := func(v uint8) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (int(v) - 35) / 40
	}
	return 16 + 36*q(r) + 6*q(g) + q(b)
}

func (c *Canvas) fgSeq(lvl, acc uint8) string {
	if c.profile == NoColor {
		return ""
	}
	key := uint16(lvl&0xF8)<<5 | uint16(acc>>3)
	if s, ok := c.fgCache[key]; ok {
		return s
	}
	r, g, b := rgbFor(lvl&0xF8, acc&0xF8)
	var s string
	if c.profile == TrueColor {
		s = "\x1b[38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)) + "m"
	} else {
		s = "\x1b[38;5;" + strconv.Itoa(to256(r, g, b)) + "m"
	}
	c.fgCache[key] = s
	return s
}

func (c *Canvas) bgSeq() string {
	switch c.profile {
	case TrueColor:
		return "\x1b[48;2;" + strconv.Itoa(bgR) + ";" + strconv.Itoa(bgG) + ";" + strconv.Itoa(bgB) + "m"
	case ANSI256:
		return "\x1b[48;5;232m"
	default:
		return ""
	}
}

// Render emits the frame as a string of H lines joined by newlines,
// batching escape codes across runs of same-styled cells.
func (c *Canvas) Render() string {
	c.buf.Reset()
	c.buf.Grow(c.W*c.H*3 + c.H*16)
	bg := c.bgSeq()
	for y := 0; y < c.H; y++ {
		if y > 0 {
			c.buf.WriteByte('\n')
		}
		c.buf.WriteString(bg)
		lastFG := ""
		for x := 0; x < c.W; x++ {
			cl := c.at(x, y)
			var ch rune
			var lvl, acc uint8
			switch {
			case cl.r != 0:
				ch, lvl, acc = cl.r, cl.rlvl, cl.acc
			case cl.dots != 0:
				ch, lvl, acc = rune(0x2800+int(cl.dots)), cl.dlvl, cl.acc
			default:
				c.buf.WriteByte(' ')
				continue
			}
			if fg := c.fgSeq(lvl, acc); fg != lastFG {
				c.buf.WriteString(fg)
				lastFG = fg
			}
			c.buf.WriteRune(ch)
		}
		if c.profile != NoColor {
			c.buf.WriteString("\x1b[0m")
		}
	}
	return c.buf.String()
}
