package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/store"
)

// capture runs fn with stdout redirected, returning what it printed and the
// exit code it returned.
func capture(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := fn()
	os.Stdout = old
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

// almanacHome seeds a storage home with one town's life and points the app
// at it.
func almanacHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AGENTFOREST_HOME", home)
	planted := time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)
	sleep := planted.AddDate(0, 2, 0)
	wake := sleep.Add(426 * 24 * time.Hour)
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "/x/mothgate", TS: planted, Path: "/x/mothgate", Name: "mothgate"},
		{Kind: events.KindActivity, Repo: "/x/mothgate", TS: planted, Commits: 3},
		{Kind: events.KindActivity, Repo: "/x/mothgate", TS: sleep, Commits: 2},
		{Kind: events.KindActivity, Repo: "/x/mothgate", TS: wake, Commits: 1},
		{Kind: events.KindTag, Repo: "/x/mothgate", TS: wake, Name: "v1.0"},
		{Kind: events.KindFinish, Repo: "/x/mothgate", TS: wake.AddDate(1, 0, 0), Epitaph: "shipped the thing"},
	}
	if err := store.AppendEvents(home, evs); err != nil {
		t.Fatal(err)
	}
}

// The scriptable almanac: a structured header, the memoir with the carved
// words leading, and a help footer.
func TestCmdAlmanacTellsTheMemoir(t *testing.T) {
	almanacHome(t)
	out, code := capture(t, func() int { return cmdAlmanac([]string{"mothgate"}) })
	if code != 0 {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	for _, want := range []string{
		"almanac[", ": mothgate",
		`"shipped the thing"`,
		"planted 2018, kept 2020",
		"planted march 2018",
		"v1.0 staked",
		"quiet for 14 months, then woke",
		"laid to rest", "the monument stands",
		"help[2]:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("almanac output missing %q:\n%s", want, out)
		}
	}
	epitaph := strings.Index(out, `"shipped the thing"`)
	chapter := strings.Index(out, "planted march 2018")
	if epitaph < 0 || chapter < 0 || epitaph > chapter {
		t.Fatalf("the carved words must lead:\n%s", out)
	}
}

// A wrong name fails helpfully, listing the towns that do exist.
func TestCmdAlmanacUnknownTown(t *testing.T) {
	almanacHome(t)
	out, code := capture(t, func() int { return cmdAlmanac([]string{"nowhere"}) })
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, `no town named "nowhere"`) || !strings.Contains(out, "mothgate") {
		t.Fatalf("unknown-town error must list the valid towns:\n%s", out)
	}
}

func TestCmdAlmanacUsage(t *testing.T) {
	almanacHome(t)
	if out, code := capture(t, func() int { return cmdAlmanac(nil) }); code != 2 ||
		!strings.Contains(out, "agentforest almanac <name|path>") {
		t.Fatalf("missing usage error, exit %d:\n%s", code, out)
	}
	if out, code := capture(t, func() int { return runCommand("almanac", []string{"--help"}) }); code != 0 ||
		!strings.Contains(out, "almanac: read a town's memoir") {
		t.Fatalf("--help must answer, exit %d:\n%s", code, out)
	}
}
