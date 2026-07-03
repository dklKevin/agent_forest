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
	"github.com/dklKevin/agentforest/internal/gallery"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/render"
	"github.com/dklKevin/agentforest/internal/ui"
)

var version = "0.3.0"

func usage() {
	fmt.Print(`agentforest · a forest grown from your repositories

usage:
  agentforest                 open the forest (first run walks you through connecting repos)
  agentforest connect <dir>   connect a root directory and scan it for repositories
  agentforest towns           list every town
  agentforest refresh         rescan all connected roots
  agentforest exclude <name>  hide a town (history kept); include restores it
  agentforest finish <name> ["word"]   lay a town to rest as a monument; unfinish reverses it
  agentforest --snapshot      print one frame and exit (for scripts and screenshots)
  agentforest --gallery x     print a reference sheet: species | decay | homestead | settlement

flags:
  --seed n        world seed (default 5)
  --demo          open the demo forest even when repos are connected
  --snapshot      render a single frame to stdout (reads the log; refresh first for fresh data)
  --width n       snapshot width in cells (default 160)
  --height n      snapshot height in cells (default 42)
  --at name       center the snapshot on a town
  --t sec         wind phase for the snapshot (default 2.5)
  --plain         no color escapes (shape-only output)
  --gallery kind  print a reference sheet: species | decay | homestead | settlement
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
		sheet    = fs.String("gallery", "", "reference sheet: species | decay | homestead | settlement")
		ver      = fs.Bool("version", false, "print version")
		// --now pins the reference instant (unix seconds) for the snapshot and
		// demo clock; 0 means the wall clock. Hidden from usage(): it exists so
		// goldens and screenshots own the clock, not for everyday use.
		nowUnix = fs.Int64("now", 0, "reference instant in unix seconds (0 = wall clock)")
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

	if *sheet != "" {
		out, err := gallery.RenderGallery(*sheet, *width, *height, prof)
		if err != nil {
			fmt.Println("error: " + err.Error())
			fmt.Println("help: --gallery species | decay | homestead | settlement")
			os.Exit(2)
		}
		fmt.Println(out)
		return
	}

	a, err := app.Load()
	if err != nil {
		fmt.Println("error: " + err.Error())
		os.Exit(1)
	}

	// The world comes from the persisted event log once anything is
	// connected; otherwise from the demo generator, through the same reducer.
	// One reference instant is read here and threaded into both the demo clock
	// and the snapshot, so a frame never straddles two wall-clock reads.
	now := time.Now()
	if *nowUnix != 0 {
		now = time.Unix(*nowUnix, 0).UTC()
	}
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
		printSnapshot(world, *width, *height, *at, *tphase, now, prof)
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

func printSnapshot(w *forest.World, cw, ch int, at string, t float64, now time.Time, prof canvas.Profile) {
	out, err := render.RenderSnapshot(w, render.SnapshotOpts{
		Width: cw, Height: ch, At: at, T: t, Now: now, Profile: prof,
	})
	if err != nil {
		fmt.Println("error: " + err.Error())
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
	fmt.Println(out)
}
