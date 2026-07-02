// Command-line surface: everything here is non-interactive and structured,
// so both humans and agents can drive the forest from scripts. Errors print
// to stdout with a help line and a meaningful exit code; no-ops exit 0.
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/dklKevin/agentforest/internal/app"
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
	case "help":
		usage()
		return 0
	default:
		fmt.Printf("error: unknown command %q\n", cmd)
		fmt.Println("help: commands are connect, towns, refresh, exclude, include")
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
