package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitIn(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitAt(t *testing.T, dir string, ts time.Time, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := fmt.Sprintf("%d +0000", ts.Unix())
	gitIn(t, dir, nil, "add", "-A")
	gitIn(t, dir, []string{"GIT_AUTHOR_DATE=" + stamp, "GIT_COMMITTER_DATE=" + stamp},
		"commit", "-q", "-m", "c")
}

func mkRepo(t *testing.T, dir string, ts time.Time, file, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, nil, "init", "-q", "-b", "main")
	commitAt(t, dir, ts, file, content)
}

func TestConnectPersistExcludeRelaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTFOREST_HOME", home)
	root := t.TempDir()
	old := time.Now().Add(-90 * 24 * time.Hour)
	mkRepo(t, filepath.Join(root, "keep"), old, "main.go", strings.Repeat("g", 200))
	mkRepo(t, filepath.Join(root, "drift"), time.Now().Add(-time.Hour), "lib.rs", strings.Repeat("r", 200))

	a, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if a.HasSettings || a.Connected() {
		t.Fatal("fresh home should be a first run")
	}
	rep, err := a.ConnectRoot(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Repos != 2 || rep.Changed != 2 || len(rep.Errors) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	towns := a.Towns()
	if len(towns) != 2 {
		t.Fatalf("towns = %d", len(towns))
	}
	// Oldest first is the west-to-east order.
	if towns[0].Name != "keep" || towns[1].Name != "drift" {
		t.Fatalf("order = %s, %s", towns[0].Name, towns[1].Name)
	}
	if towns[0].TotalCommits != 1 || towns[0].PrimaryLang() != "go" {
		t.Fatalf("keep state wrong: %+v", towns[0].RepoState)
	}

	// Relaunch: the world must come back from the log alone.
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !b.HasSettings || !b.Connected() {
		t.Fatal("second run lost persistence")
	}
	towns = b.Towns()
	if len(towns) != 2 || towns[0].Name != "keep" {
		t.Fatalf("relaunch towns = %d", len(towns))
	}
	idle := towns[0].Idle(time.Now())
	if idle < 89*24*time.Hour || idle > 91*24*time.Hour {
		t.Fatalf("real elapsed idle wrong: %v", idle)
	}

	// A reconcile with nothing new stays silent.
	rep, err = b.Reconcile(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 0 || rep.NewEvents != 0 {
		t.Fatalf("silent reconcile emitted: %+v", rep)
	}

	// Exclude hides the town but keeps its history in the log.
	key, err := b.FindTown("drift")
	if err != nil {
		t.Fatal(err)
	}
	b.Settings.SetExcluded(key, true)
	if err := b.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	if len(b.Towns()) != 1 {
		t.Fatal("exclude did not hide the town")
	}
	c, _ := Load()
	if len(c.Towns()) != 1 {
		t.Fatal("exclude did not persist")
	}
	c.Settings.SetExcluded(key, false)
	if len(c.Towns()) != 2 {
		t.Fatal("restore after exclude lost history")
	}

	// Finished persists by repo path.
	c.Settings.SetFinished(key, true)
	c.SaveSettings()
	d, _ := Load()
	for _, tn := range d.Towns() {
		if tn.Path == key && !tn.Finished {
			t.Fatal("finished did not persist")
		}
	}
}

func TestRescanRepoPicksUpNewCommits(t *testing.T) {
	t.Setenv("AGENTFOREST_HOME", t.TempDir())
	root := t.TempDir()
	repo := filepath.Join(root, "live")
	mkRepo(t, repo, time.Now().Add(-48*time.Hour), "main.go", "package main")

	a, _ := Load()
	if _, err := a.ConnectRoot(root, time.Now()); err != nil {
		t.Fatal(err)
	}
	key, _ := a.FindTown("live")
	before := a.Towns()[0].TotalCommits

	commitAt(t, repo, time.Now(), "main.go", "package main // more")
	rep, err := a.RescanRepo(key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 1 || rep.NewEvents == 0 {
		t.Fatalf("rescan report = %+v", rep)
	}
	town := a.Towns()[0]
	if town.TotalCommits != before+1 {
		t.Fatalf("commits = %d, want %d", town.TotalCommits, before+1)
	}
	if town.Idle(time.Now()) > time.Hour {
		t.Fatalf("new commit did not revive: idle %v", town.Idle(time.Now()))
	}
}

func TestFindTownErrors(t *testing.T) {
	t.Setenv("AGENTFOREST_HOME", t.TempDir())
	a, _ := Load()
	if _, err := a.FindTown("nowhere"); err == nil {
		t.Fatal("unknown town should error")
	}
}
