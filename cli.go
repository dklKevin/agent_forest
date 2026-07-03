// Command-line surface: everything here is non-interactive and structured,
// so both humans and agents can drive the forest from scripts. Errors print
// to stdout with a help line and a meaningful exit code; no-ops exit 0.
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/dklKevin/agentforest/internal/app"
	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
)

// runCommand dispatches a subcommand and returns the process exit code:
// 0 success or no-op, 1 the intent cannot be satisfied, 2 usage error.
func runCommand(cmd string, args []string) int {
	if len(args) > 0 && args[0] == "--help" {
		return commandHelp(cmd)
	}
	switch cmd {
	case "connect":
		return cmdConnect(args)
	case "towns":
		return cmdTowns()
	case "refresh":
		return cmdRefresh()
	case "exclude":
		return cmdSetExcluded(args, true)
	case "include":
		return cmdSetExcluded(args, false)
	case "finish":
		return cmdFinish(args)
	case "unfinish":
		return cmdUnfinish(args)
	case "help":
		usage()
		return 0
	default:
		fmt.Printf("error: unknown command %q\n", cmd)
		fmt.Println("help: commands are connect, towns, refresh, exclude, include, finish, unfinish")
		return 2
	}
}

func commandHelp(cmd string) int {
	switch cmd {
	case "connect":
		fmt.Print(`connect: add a root directory and scan it recursively for git repositories

usage:
  agentforest connect <dir>

Repos found under the root become towns. Connecting an already-connected
root simply rescans it. Roots persist in settings.json.

examples:
  agentforest connect ~/code
  agentforest connect /Volumes/work/src
`)
	case "towns":
		fmt.Print(`towns: list every town in the forest

usage:
  agentforest towns

Fields: name, stage (tended..ruins or monument), commits, last activity.

examples:
  agentforest towns
`)
	case "refresh":
		fmt.Print(`refresh: rescan every connected root and append new history to the log

usage:
  agentforest refresh

examples:
  agentforest refresh
`)
	case "exclude":
		fmt.Print(`exclude: hide a town from the forest (its history is kept)

usage:
  agentforest exclude <name|path>

examples:
  agentforest exclude old-experiment
  agentforest include old-experiment
`)
	case "include":
		fmt.Print(`include: restore a town hidden with exclude

usage:
  agentforest include <name|path>

examples:
  agentforest include old-experiment
`)
	case "finish":
		fmt.Print(`finish: lay a town to rest as a monument, with an optional carved epitaph

usage:
  agentforest finish <name|path> ["a word to carve"]

The epitaph is one short line (40 characters at most), shown only when the
town is inspected; the map stays numberless and the monument just stands.
Finishing an already-finished town with a new epitaph re-carves it; the log
keeps every word ever carved.

examples:
  agentforest finish sidecar
  agentforest finish sidecar "shipped the thing. slept better."
`)
	case "unfinish":
		fmt.Print(`unfinish: light the hearth again (the quiet reverse of finish)

usage:
  agentforest unfinish <name|path>

The town returns to ordinary life and decay. Its carved words are kept, and
they return with it if it is ever finished again.

examples:
  agentforest unfinish sidecar
`)
	default:
		usage()
	}
	return 0
}

func cmdConnect(args []string) int {
	if len(args) != 1 {
		fmt.Println("error: connect needs exactly one directory")
		fmt.Println("help: agentforest connect <dir>")
		return 2
	}
	a, err := app.Load()
	if err != nil {
		return internalError(err)
	}
	rep, err := a.ConnectRoot(args[0], time.Now())
	if err != nil {
		fmt.Println("error: " + err.Error())
		fmt.Println("help: agentforest connect <dir> (the directory must exist)")
		return 1
	}
	fmt.Println("connected: " + args[0])
	fmt.Printf("repos: %d found · %d with new history · %d events appended\n",
		rep.Repos, rep.Changed, rep.NewEvents)
	fmt.Printf("towns: %d in the forest\n", len(a.Towns()))
	printScanErrors(rep.Errors)
	fmt.Println("help[2]:")
	fmt.Println("  Run `agentforest` to walk the forest")
	fmt.Println("  Run `agentforest towns` to list every town")
	return 0
}

func cmdTowns() int {
	a, err := app.Load()
	if err != nil {
		return internalError(err)
	}
	towns := a.Towns()
	if len(towns) == 0 {
		fmt.Println("towns: 0 connected (the forest is empty)")
		fmt.Println("help[1]:")
		fmt.Println("  Run `agentforest connect <dir>` to grow it from your repositories")
		return 0
	}
	now := time.Now()
	fmt.Printf("towns[%d]{name,stage,commits,last}:\n", len(towns))
	for _, t := range towns {
		stage := model.StageOf(t.Decay(now)).String()
		if t.Finished {
			stage = "monument"
		}
		fmt.Printf("  %s,%s,%d,%s\n", toonField(t.Name), stage, t.TotalCommits, shortAgo(t.Idle(now)))
	}
	fmt.Println("help[2]:")
	fmt.Println("  Run `agentforest` to walk the forest")
	fmt.Println("  Run `agentforest exclude <name>` to hide a town")
	return 0
}

func cmdRefresh() int {
	a, err := app.Load()
	if err != nil {
		return internalError(err)
	}
	if len(a.Settings.Roots) == 0 {
		fmt.Println("scanned: 0 repos (no roots connected)")
		fmt.Println("help[1]:")
		fmt.Println("  Run `agentforest connect <dir>` to connect one")
		return 0
	}
	rep, err := a.Reconcile(time.Now())
	if err != nil {
		return internalError(err)
	}
	fmt.Printf("scanned: %d repos\n", rep.Repos)
	if rep.NewEvents == 0 {
		fmt.Println("new: nothing (the log is current)")
	} else {
		fmt.Printf("new: %s across %s\n",
			pluralize(rep.NewEvents, "event"), pluralize(rep.Changed, "town"))
	}
	printScanErrors(rep.Errors)
	return 0
}

func cmdSetExcluded(args []string, excluded bool) int {
	verb := "exclude"
	if !excluded {
		verb = "include"
	}
	if len(args) != 1 {
		fmt.Printf("error: %s needs exactly one town name or path\n", verb)
		fmt.Printf("help: agentforest %s <name|path>\n", verb)
		return 2
	}
	a, err := app.Load()
	if err != nil {
		return internalError(err)
	}
	key, err := a.FindTown(args[0])
	if err != nil {
		fmt.Println("error: " + err.Error())
		fmt.Println("help: Run `agentforest towns` to see every town")
		return 1
	}
	changed := a.Settings.SetExcluded(key, excluded)
	if !changed {
		if excluded {
			fmt.Printf("excluded: %s was already hidden (no-op)\n", key)
		} else {
			fmt.Printf("included: %s was not hidden (no-op)\n", key)
		}
		return 0
	}
	if err := a.SaveSettings(); err != nil {
		return internalError(err)
	}
	if excluded {
		fmt.Println("excluded: " + key + " (history kept)")
		fmt.Println("help[1]:")
		fmt.Printf("  Run `agentforest include %s` to restore it\n", args[0])
	} else {
		fmt.Println("included: " + key)
	}
	return 0
}

// findFinishState resolves a town and reports whether it currently stands
// finished, from the same derived state the forest renders.
func findFinishState(a *app.App, nameOrPath string) (key string, finished bool, err error) {
	key, err = a.FindTown(nameOrPath)
	if err != nil {
		return "", false, err
	}
	for _, r := range events.Reduce(a.Events) {
		if r.Path == key {
			return key, r.Finished, nil
		}
	}
	return key, false, nil
}

func cmdFinish(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Println("error: finish needs a town and, at most, one carved line")
		fmt.Println(`help: agentforest finish <name|path> ["a word to carve"]`)
		return 2
	}
	epitaph := ""
	if len(args) == 2 {
		epitaph = strings.TrimSpace(args[1])
	}
	if err := app.ValidateEpitaph(epitaph); err != nil {
		fmt.Println("error: " + err.Error())
		fmt.Printf("help: an epitaph is one plain line of at most %d characters\n", app.EpitaphMaxRunes)
		return 2
	}
	a, err := app.Load()
	if err != nil {
		return internalError(err)
	}
	key, finished, err := findFinishState(a, args[0])
	if err != nil {
		fmt.Println("error: " + err.Error())
		fmt.Println("help: Run `agentforest towns` to see every town")
		return 1
	}
	if finished && epitaph == "" {
		fmt.Printf("finished: %s already stands as a monument (no-op)\n", key)
		fmt.Println("help[1]:")
		fmt.Printf("  Run `agentforest finish %s \"a word to carve\"` to re-carve its epitaph\n", args[0])
		return 0
	}
	if err := a.Finish(key, epitaph, time.Now()); err != nil {
		return internalError(err)
	}
	if finished {
		fmt.Printf("carved: %q · %s stands as a monument\n", epitaph, key)
	} else {
		fmt.Println("finished: " + key + " stands as a monument")
		if epitaph != "" {
			fmt.Printf("epitaph: %q (shown when the town is inspected)\n", epitaph)
		}
	}
	fmt.Println("help[1]:")
	fmt.Printf("  Run `agentforest unfinish %s` to light the hearth again\n", args[0])
	return 0
}

func cmdUnfinish(args []string) int {
	if len(args) != 1 {
		fmt.Println("error: unfinish needs exactly one town name or path")
		fmt.Println("help: agentforest unfinish <name|path>")
		return 2
	}
	a, err := app.Load()
	if err != nil {
		return internalError(err)
	}
	key, finished, err := findFinishState(a, args[0])
	if err != nil {
		fmt.Println("error: " + err.Error())
		fmt.Println("help: Run `agentforest towns` to see every town")
		return 1
	}
	if !finished {
		fmt.Printf("unfinished: %s was not a monument (no-op)\n", key)
		return 0
	}
	if err := a.Unfinish(key, time.Now()); err != nil {
		return internalError(err)
	}
	fmt.Println("unfinished: " + key + " · the hearth is lit again (its carved words are kept)")
	return 0
}

func printScanErrors(errs []string) {
	if len(errs) == 0 {
		return
	}
	fmt.Printf("errors[%d]:\n", len(errs))
	for _, e := range errs {
		fmt.Println("  " + e)
	}
}

func internalError(err error) int {
	fmt.Println("error: " + err.Error())
	return 1
}

func pluralize(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// toonField quotes a value only when it would break the row shape.
func toonField(s string) string {
	if strings.ContainsAny(s, ", \"\t") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// shortAgo compresses idle time for list output.
func shortAgo(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "now"
	case d < 100*time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 36*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 21*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 11*30*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30.4)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365.25)))
	}
}
