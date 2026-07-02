// agentforest: your repositories, growing as a forest in your terminal.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklKevin/agentforest/internal/app"
	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/demo"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/forest"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/sprite"
	"github.com/dklKevin/agentforest/internal/ui"
)

var version = "0.2.0"

func usage() {
	fmt.Print(`agentforest · a forest grown from your repositories

usage:
  agentforest                 open the forest (first run walks you through connecting repos)
  agentforest connect <dir>   connect a root directory and scan it for repositories
  agentforest towns           list every town
  agentforest refresh         rescan all connected roots
  agentforest exclude <name>  hide a town (history kept); include restores it
  agentforest --snapshot      print one frame and exit (for scripts and screenshots)
  agentforest --gallery x     print a reference sheet: "species", "decay", or "homestead"

flags:
  --seed n        world seed (default 5)
  --demo          open the demo forest even when repos are connected
  --snapshot      render a single frame to stdout (reads the log; refresh first for fresh data)
  --width n       snapshot width in cells (default 160)
  --height n      snapshot height in cells (default 42)
  --at name       center the snapshot on a town
  --t sec         wind phase for the snapshot (default 2.5)
  --plain         no color escapes (shape-only output)
  --gallery kind  print a reference sheet ("species", "decay", or "homestead")
  --version       print version

Every subcommand answers --help.
`)
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		os.Exit(runCommand(args[0], args[1:]))
	}

	fs := flag.NewFlagSet("agentforest", flag.ExitOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = usage
	var (
		seed     = fs.Uint64("seed", 5, "world seed")
		demoFlag = fs.Bool("demo", false, "open the demo forest")
		snapshot = fs.Bool("snapshot", false, "render one frame and exit")
		width    = fs.Int("width", 160, "snapshot width")
		height   = fs.Int("height", 42, "snapshot height")
		at       = fs.String("at", "", "center snapshot on town")
		tphase   = fs.Float64("t", 2.5, "snapshot wind phase seconds")
		plain    = fs.Bool("plain", false, "no color escapes")
		gallery  = fs.String("gallery", "", "reference sheet: species | decay")
		ver      = fs.Bool("version", false, "print version")
	)
	fs.Parse(args)

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
			fmt.Println("error: " + err.Error())
			fmt.Println("help: --gallery species | decay | homestead")
			os.Exit(2)
		}
		return
	}

	a, err := app.Load()
	if err != nil {
		fmt.Println("error: " + err.Error())
		os.Exit(1)
	}

	// The world comes from the persisted event log once anything is
	// connected; otherwise from the demo generator, through the same reducer.
	now := time.Now()
	demoMode := *demoFlag || !a.Connected()
	var towns []*model.Town
	if demoMode {
		repos := events.Reduce(demo.Events(*seed, now))
		finished := demo.FinishedNames()
		for _, r := range repos {
			towns = append(towns, model.NewTown(r, finished[r.Name]))
		}
	} else {
		towns = a.Towns()
	}
	world := forest.Build(*seed, towns)

	if *snapshot {
		printSnapshot(world, *width, *height, *at, *tphase, prof)
		return
	}

	m := ui.New(ui.Config{
		App:     a,
		World:   world,
		Seed:    *seed,
		Demo:    demoMode,
		Onboard: !*demoFlag && !a.Connected(),
	})
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error: " + err.Error())
		os.Exit(1)
	}
}

func printSnapshot(w *forest.World, cw, ch int, at string, t float64, prof canvas.Profile) {
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
			fmt.Println("error: no town named " + at)
			names := ""
			for i, s := range w.Sites {
				if i > 0 {
					names += ", "
				}
				names += s.Town.Name
			}
			if names == "" {
				names = "(none yet; run `agentforest connect <dir>`)"
			}
			fmt.Println("help: towns are " + names)
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
	case "homestead":
		type slot struct {
			tier  int
			d     float64
			fin   bool
			name  string
			label string
		}
		row := func(slots []slot, rgy int) {
			step := cw * 2 / len(slots)
			for i, s := range slots {
				x := step/2 + i*step
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
	default:
		return fmt.Errorf("unknown gallery %q", kind)
	}
	fmt.Println(c.Render())
	return nil
}
