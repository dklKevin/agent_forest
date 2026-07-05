package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// gitCLI runs one git command in dir for the end-to-end CLI tests.
func gitCLI(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mkCLIRepo creates a repository with one commit stamped at ts.
func mkCLIRepo(t *testing.T, dir string, ts time.Time, file, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCLI(t, dir, nil, "init", "-q", "-b", "main")
	commitCLIAt(t, dir, ts, file, content)
}

// commitCLIAt commits a file change with author and committer time pinned to
// ts, so tests can land two commits inside the same clock second.
func commitCLIAt(t *testing.T, dir string, ts time.Time, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := fmt.Sprintf("%d +0000", ts.Unix())
	env := []string{"GIT_AUTHOR_DATE=" + stamp, "GIT_COMMITTER_DATE=" + stamp}
	gitCLI(t, dir, nil, "add", "-A")
	gitCLI(t, dir, env, "commit", "-q", "-m", "c: "+file)
}

// Git commit timestamps are second-granularity, so a follow-up commit landing
// in the same second as the last scan's newest commit is invisible to a
// timestamp-only cursor. Refresh must still record it.
func TestRefreshCountsQuickCommitsInSameSecond(t *testing.T) {
	t.Setenv("AGENTFOREST_HOME", t.TempDir())
	root := t.TempDir()
	repo := filepath.Join(root, "active-app")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	mkCLIRepo(t, repo, stamp, "main.go", "package main\n")

	if out, code := capture(t, func() int { return runCommand("connect", []string{root}) }); code != 0 {
		t.Fatalf("connect exit = %d\n%s", code, out)
	}

	commitCLIAt(t, repo, stamp, "main.go", "package main\n// progress\n")
	out, code := capture(t, func() int { return runCommand("refresh", nil) })
	if code != 0 {
		t.Fatalf("refresh exit = %d\n%s", code, out)
	}
	if strings.Contains(out, "new: nothing") {
		t.Fatalf("refresh missed the same-second commit:\n%s", out)
	}
	if !regexp.MustCompile(`new: .+ across 1 town\b`).MatchString(out) {
		t.Fatalf("refresh did not attribute the new history to the town:\n%s", out)
	}

	out, code = capture(t, func() int { return runCommand("towns", nil) })
	if code != 0 {
		t.Fatalf("towns exit = %d\n%s", code, out)
	}
	if !regexp.MustCompile(`active-app,[a-z]+,2,`).MatchString(out) {
		t.Fatalf("same-second commit was not counted:\n%s", out)
	}

	// A second refresh finds the log already current: the recorded day
	// count reconciles without re-emitting what it just caught.
	out, code = capture(t, func() int { return runCommand("refresh", nil) })
	if code != 0 {
		t.Fatalf("second refresh exit = %d\n%s", code, out)
	}
	if !strings.Contains(out, "new: nothing (the log is current)") {
		t.Fatalf("second refresh re-emitted recorded history:\n%s", out)
	}
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
