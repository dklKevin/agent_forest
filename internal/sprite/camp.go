// The occupancy camp: unfinished local work gives a town a visible body. A
// small canvas tent pitched at the edge of the kept ground, a fire breathing
// beside it, a thin wisp on the wind - presence read fresh from the working
// tree each scan, gone the moment the work lands or is put away. Like
// everything meaning-bearing it is shape, not color: the fire flickers by
// brightness alone, and the warm accent stays the lantern's.
package sprite

import (
	"math"

	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Camp is one town's occupancy mark, fully described so drawing is
// deterministic frame to frame.
type Camp struct {
	Seed    uint64
	X       int // tent center, dots
	GroundY int // baseline, dots
	Away    int // +1 east or -1 west: the side away from the homestead
	Lvl     uint8
	Second  bool // parallel work in another clearing: a second, smaller tent
}

// DrawCamp renders the tent, the fire, and its wisp.
func (p *P) DrawCamp(c Camp) {
	away := c.Away
	if away == 0 {
		away = 1
	}
	cx := c.X / 2
	gyr := c.GroundY / 4

	// The tent: a low A of canvas over a dark mouth, in its own small
	// clearing so it reads against any foliage.
	p.C.ClearRect(cx-2, gyr-1, 4, 2)
	p.C.Rune(cx-1, gyr-1, '╱', c.Lvl+8)
	p.C.Rune(cx, gyr-1, '╲', c.Lvl+8)
	p.C.Rune(cx-2, gyr, '╱', c.Lvl-2)
	p.C.Rune(cx+1, gyr, '╲', c.Lvl-2)
	// Guy pegs at the skirt.
	p.C.Dot(c.X-6, c.GroundY, c.Lvl-26)
	p.C.Dot(c.X+5, c.GroundY, c.Lvl-26)

	// The second tent, smaller and set a little apart, when work runs in
	// parallel clearings.
	if c.Second {
		sx := cx - away*4
		p.C.ClearRect(sx-1, gyr, 2, 1)
		p.C.Rune(sx-1, gyr, '╱', c.Lvl-8)
		p.C.Rune(sx, gyr, '╲', c.Lvl-8)
	}

	// The fire, on the trail side away from the homestead: a stone ring, one
	// bright knot breathing at the heart, and a short wisp that wanders with
	// the same wind that moves the trees.
	fx := c.X + away*9
	p.C.ClearRect(fx/2, gyr-1, 1, 1)
	phase := xnoise.Range(c.Seed, 0, 6.28, 0x91)
	breath := 0.5 + 0.5*math.Sin(p.T*2.6+phase)
	p.C.Dot(fx-1, c.GroundY, c.Lvl-14)
	p.C.Dot(fx+1, c.GroundY, c.Lvl-14)
	p.C.Dot(fx, c.GroundY-1, uint8(150+40*breath))
	p.C.Dot(fx, c.GroundY-2, uint8(110+34*breath))
	for s := 0; s < 6; s++ {
		y := c.GroundY - 3 - s
		fs := float64(s) / 6
		k := xnoise.Value2(c.Seed^0xF13E, float64(s)*0.5, p.T*0.9)
		if k > 0.75-0.35*fs*fs {
			continue
		}
		drift := math.Sin(p.T*1.1+float64(s)*0.7+phase)*(0.4+fs*1.6) +
			p.wind(float64(fx), float64(y))*fs
		p.C.Dot(fx+int(math.Round(drift)), y, uint8(52+50*(1-fs)))
	}
}
