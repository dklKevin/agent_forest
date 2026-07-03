package forest

import (
	"math"
	"time"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/sprite"
	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Frame carries everything volatile about one rendered moment.
type Frame struct {
	Cam   float64 // left edge of the main plane, reference dots
	T     float64 // seconds since launch, drives wind
	Now   time.Time
	Focus *Site // town under focus (sign brightens); may be nil
	Spot  *Site // most recently tended town (lantern glow); may be nil
}

// Render paints the whole world for one frame.
func (w *World) Render(c *canvas.Canvas, f Frame) {
	c.Clear()
	dw, dh := c.DotW(), c.DotH()
	vs := xnoise.Clamp(float64(dh)/refDotH, 0.5, 1.05)

	ground := func(wx float64) int {
		g := float64(dh)*0.78 + (xnoise.FBM1(w.Seed^0x9E0, wx*0.0052, 3)-0.5)*2*14*vs
		return int(g)
	}
	wind := func(x, y float64) float64 {
		base := (xnoise.FBM1(w.Seed^0x3141, x*0.012+f.T*0.32, 2) - 0.5) * 2.1
		env := xnoise.Smoothstep(0.56, 0.78, xnoise.FBM1(w.Seed^0x1592, f.T*0.055, 2))
		gust := 0.0
		if env > 0 {
			s := math.Sin(x*0.005 - f.T*1.05)
			if s > 0 {
				gust = env * s * s * s * 2.6
			}
		}
		return base + gust
	}
	p := &sprite.P{C: c, T: f.T, Wind: wind}

	w.sky(c, f, dw, dh)
	w.ridges(c, f, dw, dh, vs)
	w.groundPass(c, f, dw, dh, vs, ground)

	// Trees, west to east; back rows within a town go first so the grove has
	// depth. Wild filler is interleaved purely by position.
	w.wildPass(p, f, dw, dh, vs, ground, true)
	for _, s := range w.Sites {
		w.sitePass(p, f, s, dw, vs, ground, true)
	}
	w.wildPass(p, f, dw, dh, vs, ground, false)
	for _, s := range w.Sites {
		w.sitePass(p, f, s, dw, vs, ground, false)
	}
	for _, s := range w.Sites {
		w.signPass(p, f, s, dw, vs, ground)
	}
	w.foreground(c, f, dw, dh)

	// Hearthlight: a soft warm breath on the most recently tended homestead.
	if f.Spot != nil {
		sx := float64(f.Spot.SignX) - f.Cam
		if sx > -60 && sx < float64(dw)+60 {
			gy := ground(float64(f.Spot.SignX))
			_, wallH, roofRise := sprite.CabinDims(f.Spot.Hearth.Tier)
			ch := float64((wallH + roofRise) * 4)
			cy := (float64(gy) - ch*0.75) / 4
			breath := 0.86 + 0.14*math.Sin(f.T*0.6)
			c.Warm(int(sx/2), int(cy), 16, 7.5, uint8(105*breath))
		}
	}
}

// sky scatters a very few stars and hangs a small moon, far away.
func (w *World) sky(c *canvas.Canvas, f Frame, dw, dh int) {
	horizon := int(float64(dh) * 0.34)
	off := int(f.Cam * 0.04)
	for y := 0; y < horizon; y++ {
		for x := 0; x < dw; x++ {
			u := xnoise.Unit(w.Seed^0x57A2, uint64(x+off), uint64(y))
			if u < 0.0016 {
				lvl := uint8(58 + 40*xnoise.Unit(w.Seed^0x57A3, uint64(x+off), uint64(y)))
				c.Dot(x, y, lvl)
			}
		}
	}
	// The moon: world-fixed, drifting almost imperceptibly with parallax.
	mx := float64(w.Width)*0.18 - f.Cam*0.08
	my := float64(dh) * 0.13
	r := 5.5
	for y := int(my - r - 1); y <= int(my+r+1); y++ {
		for x := int(mx - r - 1); x <= int(mx+r+1); x++ {
			dx, dy := float64(x)-mx, float64(y)-my
			if dx*dx+dy*dy > r*r {
				continue
			}
			// Waxing crescent: the shadow disc eats the lower left.
			sx, sy := dx+r*0.55, dy+r*0.28
			if sx*sx+sy*sy < r*r*0.92 {
				continue
			}
			c.Dot(x, y, 132)
		}
	}
}

// ridges draws two distant treelines with parallax: solid dark masses with a
// jagged canopy edge, dissolving into dither at their base, and a breath of
// valley mist between them and the near ground.
func (w *World) ridges(c *canvas.Canvas, f Frame, dw, dh int, vs float64) {
	layer := func(par float64, base float64, amp float64, lvl uint8, seedK uint64, depth float64) {
		off := f.Cam * par
		for x := 0; x < dw; x++ {
			wx := float64(x) + off
			top := base + (xnoise.FBM1(w.Seed^seedK, wx*0.010, 3)-0.5)*2*amp
			// The crest is an uneven treeline, not a clean curve.
			top -= xnoise.Value1(w.Seed^seedK^0xF, wx*0.16) * 5 * vs
			for y := int(top); y < int(top+depth); y++ {
				if y < 0 || y >= dh {
					continue
				}
				fy := (float64(y) - top) / depth
				// Dense at the crest, clump-dithered dissolve below.
				fill := 1.02 * (1 - fy*fy)
				clump := xnoise.Value2(w.Seed^seedK, wx*0.20, float64(y)*0.28)
				if clump < fill {
					// Faint variation keeps the mass from reading flat.
					l := int(lvl) + int((clump-0.5)*8)
					c.Dot(x, y, uint8(l))
				}
			}
		}
	}
	layer(0.20, float64(dh)*0.27, 8*vs, 32, 0xA11, 15*vs)
	layer(0.44, float64(dh)*0.47, 10*vs, 46, 0xA22, 17*vs)
	// Valley mist: the faint breath between the far woods and here.
	m0, m1 := int(float64(dh)*0.64), int(float64(dh)*0.76)
	off := int(f.Cam * 0.7)
	for y := m0; y < m1; y++ {
		for x := 0; x < dw; x++ {
			if xnoise.Unit(w.Seed^0x1157, uint64(x+off), uint64(y)) < 0.03 {
				c.Dot(x, y, 24)
			}
		}
	}
}

// groundPass draws the earth line, the soil beneath, grass, and the trail.
func (w *World) groundPass(c *canvas.Canvas, f Frame, dw, dh int, vs float64, ground func(float64) int) {
	for x := 0; x < dw; x++ {
		wx := f.Cam + float64(x)
		gy := ground(wx)
		infl := w.decayInfluence(wx, f.Now)

		// The earth line itself, softly broken.
		if xnoise.Unit(w.Seed^0xEA, uint64(int(wx))) < 0.62 {
			c.Dot(x, gy, 60)
		}
		// Soil fading downward.
		for dy := 1; dy < 18; dy++ {
			y := gy + dy
			if y >= dh-2 {
				break
			}
			keep := 0.26 / (1 + float64(dy)*0.45)
			if xnoise.Unit(w.Seed^0xEB, uint64(int(wx)), uint64(dy)) < keep {
				c.Dot(x, y, 38)
			}
		}
		// Grass: everywhere a little, riotous where the forest is winning.
		gdens := 0.16 + 0.5*infl + 0.12*xnoise.Value1(w.Seed^0xEC, wx*0.03)
		if xnoise.Unit(w.Seed^0xED, uint64(int(wx))) < gdens {
			bh := 1 + int(xnoise.Unit(w.Seed^0xEE, uint64(int(wx)))*(2+3*infl))
			for dy := 1; dy <= bh; dy++ {
				c.Dot(x, gy-dy, uint8(76+int(18*xnoise.Unit(w.Seed^0xEF, uint64(int(wx))))))
			}
		}
		// The trail: dashes wandering along the ground, fading to grass where
		// a town is being reclaimed. The path to a ruin disappears first.
		tw := int(wx)
		if (tw/4)%2 == 0 {
			toff := int((xnoise.FBM1(w.Seed^0x7E, wx*0.016, 2) - 0.5) * 5)
			tl := 82 * (1 - infl*0.95)
			if tl > 30 {
				c.Dot(x, gy+2+toff, uint8(tl))
			}
		}
	}
}

// decayInfluence is how strongly the wild is winning at a world x: the max of
// nearby towns' decay with soft falloff. Grass, trail, and undergrowth key off it.
func (w *World) decayInfluence(wx float64, now time.Time) float64 {
	best := 0.0
	for _, s := range w.Sites {
		d := s.Town.Decay(now)
		if d <= 0 {
			continue
		}
		half := float64(s.X1-s.X0)/2 + 26
		dist := abs64(wx-float64(s.Center())) / half
		v := d * math.Exp(-dist*dist*1.6)
		if v > best {
			best = v
		}
	}
	return best
}

// sitePass draws one town's grove (back rows or front rows) and its
// settlement, plus the forest's own regrowth once decay is deep. Mid-set
// buildings go down with the back rows so the front canopy closes over
// them; the hearth and front buildings go down under the front canopy.
func (w *World) sitePass(p *sprite.P, f Frame, s *Site, dw int, vs float64, ground func(float64) int, backRow bool) {
	d := s.Town.Decay(f.Now)
	stature := s.Town.Stature(f.Now)
	if backRow {
		defer w.settlementPass(p, f, s, dw, vs, ground, true)
	} else {
		w.settlementPass(p, f, s, dw, vs, ground, false)
	}
	for _, tm := range s.trees {
		if tm.back != backRow {
			continue
		}
		sx := float64(tm.x) - f.Cam
		if sx < -90 || sx > float64(dw)+90 {
			continue
		}
		gy := ground(float64(tm.x))
		lvl := uint8(150)
		if tm.back {
			gy -= int(6 * vs)
			lvl = 106
		}
		lvl += uint8(xnoise.Hash(tm.seed, 0x11) % 14)
		p.Draw(sprite.Tree{
			Seed: tm.seed, Species: tm.sp,
			X: int(sx), GroundY: gy,
			H:   int(stature * tm.hMul * vs),
			Lvl: lvl, Decay: d,
		})
	}
	// New life among the ruins: the forest planting itself back.
	if !backRow && d > 0.5 {
		n := int((d - 0.5) * 16)
		for i := 0; i < n; i++ {
			wx := float64(s.X0) + xnoise.Unit(w.Seed, 0x5A0, uint64(i), uint64(s.X0))*float64(s.X1-s.X0)
			sx := wx - f.Cam
			if sx < 0 || sx >= float64(dw) {
				continue
			}
			p.Sapling(xnoise.Hash(w.Seed, 0x5A9, uint64(i), uint64(s.X0)), int(sx), ground(wx), 95)
		}
	}
}

// settlementPass draws the buildings of one depth plane, and with the front
// plane the settlement's connective tissue: the hearth, the well, worn
// footpaths, and the split-rail fragments between the yards.
func (w *World) settlementPass(p *sprite.P, f Frame, s *Site, dw int, vs float64, ground func(float64) int, midPlane bool) {
	for _, b := range s.Buildings {
		if b.Mid != midPlane {
			continue
		}
		sx := float64(b.X) - f.Cam
		if sx < -90 || sx > float64(dw)+90 {
			continue
		}
		gy := ground(float64(b.X))
		lvl := uint8(126)
		if midPlane {
			gy -= int(6 * vs) // set back with the rear rank, half-hidden
			lvl = 110
		}
		p.DrawBuilding(sprite.Building{
			Seed: b.Seed, X: int(sx), GroundY: gy,
			Form: b.B.Form, Share: b.B.Share,
			Lvl: lvl, Decay: s.Town.BuildingDecay(b.B, f.Now),
			Finished: s.Town.Finished,
			Focused:  f.Focus == s,
		})
		if !midPlane {
			// The worn path from this door back toward the hearth.
			p.DrawFootpath(xnoise.Hash(b.Seed, 0xFA7), int(sx), s.Hearth.X-int(f.Cam), gy,
				s.Town.BuildingDecay(b.B, f.Now))
		}
	}
	if midPlane {
		return
	}
	hx := float64(s.Hearth.X) - f.Cam
	if hx > -90 && hx < float64(dw)+90 {
		p.DrawCabin(sprite.Cabin{
			Seed: s.Hearth.Seed, X: int(hx),
			GroundY: ground(float64(s.Hearth.X)),
			Tier:    s.Hearth.Tier,
			Lvl:     128, Decay: s.Town.Decay(f.Now),
			Finished: s.Town.Finished,
			Focused:  f.Focus == s,
		})
	}
	if s.WellX != 0 {
		wx := float64(s.WellX) - f.Cam
		if wx > -20 && wx < float64(dw)+20 {
			p.DrawWell(xnoise.Hash(w.Seed, 0x11E1, uint64(s.WellX)), int(wx),
				ground(float64(s.WellX)), 122, s.Town.Decay(f.Now))
		}
	}
	for _, fc := range s.Fences {
		x0, x1 := float64(fc.X0)-f.Cam, float64(fc.X1)-f.Cam
		if x1 < -20 || x0 > float64(dw)+20 {
			continue
		}
		dA, dB := s.Town.Decay(f.Now), s.Town.Decay(f.Now)
		if fc.A >= 0 {
			dA = s.Town.BuildingDecay(s.Buildings[fc.A].B, f.Now)
		}
		if fc.B >= 0 {
			dB = s.Town.BuildingDecay(s.Buildings[fc.B].B, f.Now)
		}
		if dB < dA {
			dA = dB // a fence stands while either neighbor still tends it
		}
		gy := ground((float64(fc.X0) + float64(fc.X1)) / 2)
		p.DrawFence(fc.Seed, int(x0), int(x1), gy, 118, dA)
	}
}

// signPass mounts the town's name board against its homestead, and plants
// the release stakes along the trail east of the dooryard.
func (w *World) signPass(p *sprite.P, f Frame, s *Site, dw int, vs float64, ground func(float64) int) {
	sx := float64(s.SignX) - f.Cam
	if sx < -110 || sx > float64(dw)+110 {
		return
	}
	d := s.Town.Decay(f.Now)
	gy := ground(float64(s.SignX))
	stakes := s.StakesX - int(f.Cam)
	p.DrawTags(xnoise.Hash(w.Seed, 0x7A6, uint64(s.SignX)), stakes, ground(float64(s.StakesX)), len(s.Town.Tags), 100, d)
	signX, signGY, hang, armC := sprite.CabinSignMount(
		s.Hearth.Tier, s.Hearth.Seed, int(sx), gy, len(s.Town.Name)+4, d)
	if !hang {
		signGY = ground(f.Cam + float64(signX)) // the post plants on its own ground
	}
	p.DrawSign(sprite.Sign{
		Seed: xnoise.Hash(w.Seed, 0x516, uint64(s.SignX)),
		X:    signX, GroundY: signGY,
		Name: s.Town.Name,
		Lvl:  135, Acc: 235,
		Decay:    d,
		Monument: s.Town.Finished,
		Hang:     hang,
		ArmC:     armC,
		Focused:  f.Focus == s,
	})
}

// wildPass draws the untamed growth. Old west woods and snags behind the
// towns (backLayer true), scrub and rocks in front.
func (w *World) wildPass(p *sprite.P, f Frame, dw, dh int, vs float64, ground func(float64) int, backLayer bool) {
	for _, it := range w.wild {
		isBack := it.kind == wildOldTree || it.kind == wildSnag
		if isBack != backLayer {
			continue
		}
		sx := float64(it.x) - f.Cam
		if sx < -90 || sx > float64(dw)+90 {
			continue
		}
		gy := ground(float64(it.x))
		switch it.kind {
		case wildOldTree:
			p.Draw(sprite.Tree{
				Seed: it.seed, Species: it.sp,
				X: int(sx), GroundY: gy, H: int(float64(it.h) * vs),
				Lvl: 96, Decay: 0,
			})
		case wildScrub:
			p.Draw(sprite.Tree{
				Seed: it.seed, Species: model.Wild,
				X: int(sx), GroundY: gy, H: int(float64(it.h) * 0.8 * vs),
				Lvl: 104, Decay: 0,
			})
		case wildRock:
			w.rock(p, it.seed, int(sx), gy)
		case wildSnag:
			w.snag(p, it.seed, int(sx), gy, int(float64(it.h)*1.4*vs))
		case wildSapling:
			p.Sapling(it.seed, int(sx), gy, 92)
		}
	}
}

// rock is a small half-buried boulder.
func (w *World) rock(p *sprite.P, seed uint64, x, gy int) {
	wd := 2 + int(xnoise.Hash(seed, 1)%3)
	for dy := 0; dy <= wd/2; dy++ {
		for dx := -wd + dy; dx <= wd-dy; dx++ {
			if xnoise.Unit(seed, uint64(dx+9), uint64(dy)) < 0.85 {
				p.C.Dot(x+dx, gy-dy, uint8(66+int(14*xnoise.Unit(seed, uint64(dx), 3))))
			}
		}
	}
}

// snag is a standing dead spar: part of any real forest, a quiet echo of the
// decay language.
func (w *World) snag(p *sprite.P, seed uint64, x, gy, h int) {
	for cy := gy / 4; cy >= (gy-h)/4; cy-- {
		p.C.Rune(x/2, cy, '│', 84)
	}
	top := (gy - h) / 4
	if xnoise.Hash(seed, 2)%2 == 0 {
		p.C.Rune(x/2+1, top, '╱', 78)
	} else {
		p.C.Rune(x/2-1, top, '╲', 78)
	}
}

// foreground is the near underbrush strip: darker, faster, framing the scene.
func (w *World) foreground(c *canvas.Canvas, f Frame, dw, dh int) {
	off := f.Cam * 1.3
	for x := 0; x < dw; x++ {
		wx := float64(x) + off
		h := 3 + xnoise.FBM1(w.Seed^0xF6, wx*0.05, 2)*9
		if xnoise.Unit(w.Seed^0xF7, uint64(int(wx))) < 0.65 {
			bh := int(h * xnoise.Unit(w.Seed^0xF8, uint64(int(wx))) * 1.4)
			for dy := 0; dy <= bh; dy++ {
				c.Dot(x, dh-1-dy, 34)
			}
			// Occasional seed head standing above the grass.
			if xnoise.Unit(w.Seed^0xF9, uint64(int(wx))) < 0.05 {
				c.Dot(x, dh-2-bh, 46)
			}
		}
	}
}
