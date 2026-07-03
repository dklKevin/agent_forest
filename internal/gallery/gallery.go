// Package gallery renders the reference sheets: every species side by side,
// one form walked through decay, the homestead tiers, and the settlement's
// building set. These sheets exist so silhouettes and stages can be judged at
// a glance.
//
// RenderGallery reads no clock, so its output is a pure function of
// (kind, width, height, profile). Both the CLI (`--gallery`) and the golden
// tests draw through this one seam, which is why the art layer is testable.
package gallery

import (
	"fmt"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/sprite"
)

// Kinds are the reference sheets RenderGallery understands.
var Kinds = []string{"species", "decay", "homestead", "settlement"}

// RenderGallery draws one reference sheet and returns it as a string. It never
// touches the clock; the result depends only on (kind, cw, ch, prof).
func RenderGallery(kind string, cw, ch int, prof canvas.Profile) (string, error) {
	c := canvas.New(cw, ch, prof)
	p := &sprite.P{C: c, T: 2.5}
	gy := int(float64(ch*4) * 0.78)

	switch kind {
	case "species":
		specs := []struct {
			sp   model.Species
			lang string
			h    int
		}{
			{model.Oak, "go", 58}, {model.Spruce, "rust", 84}, {model.Willow, "python", 64},
			{model.Poplar, "typescript", 78}, {model.Flattop, "c", 58}, {model.Scrub, "shell", 20},
			{model.Birch, "swift", 72}, {model.Grove, "other", 60},
		}
		step := cw * 2 / len(specs)
		for i, s := range specs {
			x := step/2 + i*step
			p.Draw(sprite.Tree{Seed: uint64(40 + i), Species: s.sp, X: x, GroundY: gy, H: s.h, Lvl: 150})
			label := s.sp.String() + " · " + s.lang
			c.Text(x/2-len(label)/2, gy/4+2, label, 120, 0)
		}
	case "decay":
		depths := []struct {
			d     float64
			label string
		}{
			{0, "tended"}, {0.15, "first quiet"}, {0.37, "overgrown"},
			{0.62, "breaking"}, {0.85, "skeletal"}, {0.965, "ruins"},
		}
		step := cw * 2 / (len(depths) + 1)
		for i, s := range depths {
			x := step/2 + i*step
			p.Draw(sprite.Tree{Seed: 7, Species: model.Oak, X: x, GroundY: gy, H: 68, Lvl: 150, Decay: s.d})
			p.Draw(sprite.Tree{Seed: 21, Species: model.Oak, X: x + 18, GroundY: gy, H: 52, Lvl: 130, Decay: s.d})
			c.Text(x/2-len(s.label)/2, gy/4+2, s.label, 120, 0)
		}
		// The seventh slot: what finished looks like instead.
		x := step/2 + len(depths)*step
		p.Draw(sprite.Tree{Seed: 7, Species: model.Oak, X: x, GroundY: gy, H: 68, Lvl: 150})
		p.DrawSign(sprite.Sign{Seed: 3, X: x, GroundY: gy, Name: "done", Lvl: 135, Acc: 235, Monument: true})
		c.Text(x/2-4, gy/4+2, "monument", 120, 0)
	case "homestead":
		type slot struct {
			tier  int
			d     float64
			fin   bool
			name  string
			label string
		}
		row := func(slots []slot, rgy int) {
			// The name board hangs east of each cabin (doors face east on this
			// sheet) and a finished town's carved board, with its flanking
			// cairns, is wider still. Reserve room on the right for the widest
			// board so the last slot never clips the sheet edge, and a small
			// margin on the left for the cabins.
			const westMargin = 4
			eastReserve := 0
			for _, s := range slots {
				wallW, _, _ := sprite.CabinDims(s.tier)
				ext := wallW/2 + (len(s.name) + 4) + 6 // hang + board width + cairn slack
				if ext > eastReserve {
					eastReserve = ext
				}
			}
			step := (cw*2 - (westMargin+eastReserve)*2) / len(slots)
			inset := westMargin * 2
			if step < 8 { // too narrow to reserve; fall back to the even split
				step, inset = cw*2/len(slots), 0
			}
			for i, s := range slots {
				x := inset + step/2 + i*step
				// Doors face east here so neighboring sheet slots stay clear;
				// in the forest each settler picks their own side.
				seed := uint64(90 + i)
				for sprite.CabinDoorSide(seed) < 0 {
					seed++
				}
				p.DrawCabin(sprite.Cabin{
					Seed: seed, X: x, GroundY: rgy, Tier: s.tier,
					Lvl: 128, Decay: s.d, Finished: s.fin,
				})
				signX, signGY, hang, armC := sprite.CabinSignMount(s.tier, seed, x, rgy, len(s.name)+4, s.d)
				p.DrawSign(sprite.Sign{
					Seed: uint64(9 + i), X: signX, GroundY: signGY,
					Name: s.name, Lvl: 135, Acc: 235,
					Decay: s.d, Monument: s.fin, Hang: hang, ArmC: armC,
				})
				c.Text(x/2-len(s.label)/2, rgy/4+2, s.label, 120, 0)
			}
		}
		// Upper row: the three sizes and a kept homestead. Lower row: one
		// homestead walked into the ground.
		row([]slot{
			{0, 0, false, "mossjar", "hut · tended"},
			{1, 0, false, "foxglove", "cabin · tended"},
			{2, 0, false, "winterwell", "homestead · tended"},
			{1, 0, true, "thornbook", "kept · finished"},
		}, int(float64(ch*4)*0.42))
		row([]slot{
			{2, 0.37, false, "winterwell", "overgrown"},
			{2, 0.62, false, "winterwell", "breaking"},
			{2, 0.85, false, "winterwell", "skeletal"},
			{2, 0.965, false, "winterwell", "ruins"},
		}, int(float64(ch*4)*0.9))
	case "settlement":
		forms := []struct {
			f     model.BuildingForm
			share float64
			label string
		}{
			{model.FormBarn, 1.0, "barn"},
			{model.FormHomeplace, 0.8, "cabin"},
			{model.FormWorkshop, 0.3, "workshop"},
			{model.FormShed, 0.1, "shed"},
			{model.FormCrib, 0.08, "crib"},
			{model.FormWatchtower, 0.2, "watchtower"},
			{model.FormSchoolhouse, 0.15, "schoolhouse"},
		}
		// The row caption sits at the far left. Inset the building row so the
		// first slot's label never collides with the widest caption.
		const capReserve = len("what remains") + 3 // cells
		row := func(d float64, rgy int, caption string) {
			inset := capReserve * 2
			step := (cw*2 - inset) / (len(forms) + 1)
			if step < 6 { // too narrow to inset; fall back to the even split
				step, inset = cw*2/(len(forms)+1), 0
			}
			for i, s := range forms {
				x := inset + step/2 + i*step
				p.DrawBuilding(sprite.Building{
					Seed: uint64(70 + i), X: x, GroundY: rgy,
					Form: s.f, Share: s.share, Lvl: 126, Decay: d,
				})
				c.Text(x/2-len(s.label)/2, rgy/4+2, s.label, 120, 0)
			}
			x := inset + step/2 + len(forms)*step
			p.DrawWell(uint64(88), x, rgy, 122, d)
			c.Text(x/2-2, rgy/4+2, "well", 120, 0)
			c.Text(2, rgy/4+2, caption, 100, 0)
		}
		row(0, int(float64(ch*4)*0.42), "tended")
		row(0.93, int(float64(ch*4)*0.9), "what remains")
	default:
		return "", fmt.Errorf("unknown gallery %q", kind)
	}
	return c.Render(), nil
}
