// agentforest: your repositories, growing as a forest in your terminal.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/demo"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/sprite"
	"github.com/dklKevin/agentforest/internal/ui"
)

var version = "0.1.0-gate1"

func usage() {
	fmt.Fprint(os.Stderr, `agentforest · a forest grown from your repositories

usage:
  agentforest                 open the forest (demo forest until repos are connected)
  agentforest --snapshot      print one frame and exit (for scripts and screenshots)
  agentforest --gallery x     print a reference sheet: x is "species" or "decay"

flags:
  --seed n        world seed (default 5)
  --snapshot      render a single frame to stdout
  --width n       snapshot width in cells (default 160)
  --height n      snapshot height in cells (default 42)
  --at name       center the snapshot on a town
  --t sec         wind phase for the snapshot (default 2.5)
  --plain         no color escapes (shape-only output)
  --gallery kind  print a reference sheet ("species" or "decay")
  --version       print version
`)
}

func main() {
	fs := flag.NewFlagSet("agentforest", flag.ExitOnError)
	fs.Usage = usage
	var (
		seed     = fs.Uint64("seed", 5, "world seed")
		snapshot = fs.Bool("snapshot", false, "render one frame and exit")
		width    = fs.Int("width", 160, "snapshot width")
		height   = fs.Int("height", 42, "snapshot height")
		at       = fs.String("at", "", "center snapshot on town")
		tphase   = fs.Float64("t", 2.5, "snapshot wind phase seconds")
		plain    = fs.Bool("plain", false, "no color escapes")
		gallery  = fs.String("gallery", "", "reference sheet: species | decay")
		ver      = fs.Bool("version", false, "print version")
	)
	fs.Parse(os.Args[1:])

	if *ver {
		fmt.Println("agentforest " + version)
		return
	}

	prof := canvas.DetectProfile()
	if *plain {
		prof = canvas.NoColor
	}

	if *gallery != "" {
		if err := printGallery(*gallery, *width, *height, prof); err != nil {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
			fmt.Fprintln(os.Stderr, "help: --gallery species | --gallery decay")
			os.Exit(2)
		}
		return
	}

	// The current build always starts with the demo forest. Real repositories
	// will connect through the same event stream.
	now := time.Now()
	evs := demo.Events(*seed, now)
	repos := events.Reduce(evs)
	finished := demo.FinishedNames()
	towns := make([]*model.Town, 0, len(repos))
	for _, r := range repos {
		towns = append(towns, model.NewTown(r, finished[r.Name]))
	}
	world := forest.Build(*seed, towns)

	if *snapshot {
		printSnapshot(world, towns, *width, *height, *at, *tphase, prof)
		return
	}

	p := tea.NewProgram(ui.New(world), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func printSnapshot(w *forest.World, towns []*model.Town, cw, ch int, at string, t float64, prof canvas.Profile) {
	c := canvas.New(cw, ch, prof)
	now := time.Now()
	cam := 0.0
	var focus *forest.Site
	if at != "" {
		for _, s := range w.Sites {
			if s.Town.Name == at {
				cam = float64(s.SignX) - float64(cw)
				focus = s
			}
		}
		if focus == nil {
			fmt.Fprintln(os.Stderr, "error: no town named "+at)
			names := ""
			for i, s := range w.Sites {
				if i > 0 {
					names += ", "
				}
				names += s.Town.Name
			}
			fmt.Fprintln(os.Stderr, "help: towns are "+names)
			os.Exit(2)
		}
	} else if s := w.SpotSite(now); s != nil {
		cam = float64(s.SignX) - float64(cw)
		focus = s
	}
	if cam < 0 {
		cam = 0
	}
	w.Render(c, forest.Frame{Cam: cam, T: t, Now: now, Focus: focus, Spot: w.SpotSite(now)})
	fmt.Println(c.Render())
}

// printGallery renders reference sheets: every species side by side, or one
// species walked through every decay stage plus a finished monument reference.
// These exist so silhouettes and stages can be judged at a glance.
func printGallery(kind string, cw, ch int, prof canvas.Profile) error {
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
	default:
		return fmt.Errorf("unknown gallery %q", kind)
	}
	fmt.Println(c.Render())
	return nil
}
