package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/store"
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

	// A legacy finished list in settings.json still stands: load synthesizes
	// finish events from it, so old forests keep their monuments.
	c.Settings.SetFinished(key, true)
	c.SaveSettings()
	d, _ := Load()
	for _, tn := range d.Towns() {
		if tn.Path == key && !tn.Finished {
			t.Fatal("legacy finished did not persist")
		}
	}

	// Unfinishing a legacy monument sticks: the log records the reverse, the
	// legacy entry is retired, and the next load does not resurrect it.
	if err := d.Unfinish(key, time.Now()); err != nil {
		t.Fatal(err)
	}
	if d.Settings.IsFinished(key) {
		t.Fatal("unfinish left the legacy settings entry")
	}
	e, _ := Load()
	for _, tn := range e.Towns() {
		if tn.Path == key && tn.Finished {
			t.Fatal("legacy finish resurrected after unfinish")
		}
	}
}

func TestFinishCeremonyPersistsAndReverses(t *testing.T) {
	t.Setenv("AGENTFOREST_HOME", t.TempDir())
	root := t.TempDir()
	repo := filepath.Join(root, "keepsake")
	mkRepo(t, repo, time.Now().Add(-30*24*time.Hour), "main.go", strings.Repeat("g", 200))

	a, _ := Load()
	if _, err := a.ConnectRoot(root, time.Now()); err != nil {
		t.Fatal(err)
	}
	key, err := a.FindTown("keepsake")
	if err != nil {
		t.Fatal(err)
	}

	// Finish with an epitaph; both survive a relaunch from the log alone.
	if err := a.Finish(key, "shipped the thing", time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _ := Load()
	town := b.Towns()[0]
	if !town.Finished || town.Epitaph != "shipped the thing" || town.FinishTS.IsZero() {
		t.Fatalf("finish did not persist: %+v", town.RepoState)
	}
	if b.Settings.IsFinished(key) {
		t.Fatal("finish leaked into settings; it belongs to the log")
	}

	// The quiet reverse: unfinished, but the carved words are kept.
	if err := b.Unfinish(key, time.Now()); err != nil {
		t.Fatal(err)
	}
	c, _ := Load()
	town = c.Towns()[0]
	if town.Finished {
		t.Fatal("unfinish did not persist")
	}
	if town.Epitaph != "shipped the thing" {
		t.Fatalf("unfinish erased the epitaph: %q", town.Epitaph)
	}

	// Re-finishing unmarked brings the old words back with the monument.
	if err := c.Finish(key, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	d, _ := Load()
	town = d.Towns()[0]
	if !town.Finished || town.Epitaph != "shipped the thing" {
		t.Fatalf("re-finish lost the prior words: %+v", town.RepoState)
	}

	// Re-carving: the last epitaph wins.
	if err := d.Finish(key, "slept better", time.Now()); err != nil {
		t.Fatal(err)
	}
	e, _ := Load()
	if got := e.Towns()[0].Epitaph; got != "slept better" {
		t.Fatalf("last epitaph did not win: %q", got)
	}
}

func TestUnfinishIgnoresLegacySettingsCleanupFailure(t *testing.T) {
	dir := t.TempDir()
	key := "/repos/keepsake"
	if err := os.Mkdir(filepath.Join(dir, "settings.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{Dir: dir, Settings: &store.Settings{Finished: []string{key}}}
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)

	if err := a.Unfinish(key, now); err != nil {
		t.Fatalf("unfinish failed after appending event: %v", err)
	}
	if a.Settings.IsFinished(key) {
		t.Fatal("legacy settings entry was not retired in memory")
	}
	if len(a.Events) != 1 || a.Events[0].Kind != events.KindUnfinish || !a.Events[0].TS.Equal(now) {
		t.Fatalf("unfinish event was not kept in memory: %+v", a.Events)
	}
	evs, skipped, err := store.LoadEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(evs) != 1 || evs[0].Kind != events.KindUnfinish || evs[0].Repo != key {
		t.Fatalf("unfinish event was not persisted: skipped=%d events=%+v", skipped, evs)
	}
}

func TestValidateEpitaph(t *testing.T) {
	if err := ValidateEpitaph(""); err != nil {
		t.Fatal("an unmarked monument must be allowed")
	}
	if err := ValidateEpitaph(strings.Repeat("x", EpitaphMaxRunes)); err != nil {
		t.Fatalf("a full line must fit: %v", err)
	}
	if err := ValidateEpitaph(strings.Repeat("x", EpitaphMaxRunes+1)); err == nil {
		t.Fatal("an epitaph beyond the cap must be refused")
	}
	if err := ValidateEpitaph("two\nlines"); err == nil {
		t.Fatal("an epitaph must be one plain line")
	}
	if err := ValidateEpitaph("bad\u009bline"); err == nil {
		t.Fatal("an epitaph must reject C1 controls")
	}
}

func TestLegacySynthesisSkipsUnknownAndSettled(t *testing.T) {
	ts := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "/x/a", TS: ts, Path: "/x/a", Name: "a"},
		{Kind: events.KindActivity, Repo: "/x/a", TS: ts, Commits: 1},
		{Kind: events.KindRepo, Repo: "/x/b", TS: ts, Path: "/x/b", Name: "b"},
		{Kind: events.KindUnfinish, Repo: "/x/b", TS: ts.AddDate(0, 1, 0)},
	}
	got := synthesizeLegacyFinishes(evs, []string{"/x/a", "/x/b", "/x/gone"})
	repos := events.Reduce(got)
	for _, r := range repos {
		switch r.Path {
		case "/x/a":
			if !r.Finished {
				t.Fatal("legacy /x/a not synthesized")
			}
			if !r.FinishTS.Equal(ts) {
				t.Fatalf("synthesized finish should stamp the last activity, got %v", r.FinishTS)
			}
		case "/x/b":
			if r.Finished {
				t.Fatal("the log already decided /x/b; legacy must not override it")
			}
		}
	}
	if len(repos) != 2 {
		t.Fatalf("a stale legacy path grew a ghost town: %d repos", len(repos))
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
