package gitscan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadOccupancyCleanRepoIsEmpty(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "clean")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	if o := ReadOccupancy(repo); o.Occupied() {
		t.Fatalf("clean repo on main reads occupied: %+v", o)
	}
}

func TestReadOccupancyDirtyTrackedFile(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "dirty")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := ReadOccupancy(repo)
	if !o.Dirty || !o.Occupied() {
		t.Fatalf("edited tracked file not read as dirty: %+v", o)
	}
	if o.Branch != "" {
		t.Fatalf("default branch must carry no branch signal: %+v", o)
	}
}

func TestReadOccupancyStagedChange(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "staged")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // staged"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, nil, "add", "-A")
	if o := ReadOccupancy(repo); !o.Dirty {
		t.Fatalf("staged change not read as dirty: %+v", o)
	}
}

func TestReadOccupancyUntrackedFile(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "scratch")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := ReadOccupancy(repo); !o.Dirty {
		t.Fatalf("untracked file not read as dirty: %+v", o)
	}
	// An ignored file is put-away work, not presence.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("notes.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, nil, "add", ".gitignore")
	gitIn(t, repo, nil, "commit", "-q", "-m", "ignore")
	if o := ReadOccupancy(repo); o.Dirty {
		t.Fatalf("ignored file read as dirty: %+v", o)
	}
}

func TestReadOccupancyNonDefaultBranch(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "branchy")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	gitIn(t, repo, nil, "checkout", "-q", "-b", "feature/x")
	o := ReadOccupancy(repo)
	if o.Branch != "feature/x" || !o.Occupied() {
		t.Fatalf("non-default branch not read: %+v", o)
	}
	gitIn(t, repo, nil, "checkout", "-q", "main")
	if o := ReadOccupancy(repo); o.Occupied() {
		t.Fatalf("back on main should read empty: %+v", o)
	}
}

// A repo whose default branch cannot be known locally (no origin HEAD, no
// main or master) must carry no branch signal: the check never guesses.
func TestReadOccupancyUnknownDefaultStaysQuiet(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "trunkish")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, nil, "init", "-q", "-b", "trunk")
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	if o := ReadOccupancy(repo); o.Branch != "" {
		t.Fatalf("unknowable default guessed a branch: %+v", o)
	}
}

// The origin HEAD pointer, when it exists, names the default even when it is
// neither main nor master.
func TestReadOccupancyOriginHeadNamesDefault(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "pointed")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, nil, "init", "-q", "-b", "trunk")
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	gitIn(t, repo, nil, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	gitIn(t, repo, nil, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	if o := ReadOccupancy(repo); o.Branch != "" {
		t.Fatalf("checkout of the origin default read as a branch signal: %+v", o)
	}
	gitIn(t, repo, nil, "checkout", "-q", "-b", "side")
	if o := ReadOccupancy(repo); o.Branch != "side" {
		t.Fatalf("branch off an origin-named default not read: %+v", o)
	}
}

func TestReadOccupancyDetachedHeadStaysQuiet(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "detached")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	gitIn(t, repo, nil, "checkout", "-q", "--detach")
	if o := ReadOccupancy(repo); o.Branch != "" {
		t.Fatalf("detached HEAD guessed a branch: %+v", o)
	}
}

func TestReadOccupancyExtraWorktrees(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "trunked")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	if o := ReadOccupancy(repo); o.Worktrees != 0 {
		t.Fatalf("lone worktree counted as extra: %+v", o)
	}
	wt := filepath.Join(t.TempDir(), "side")
	gitIn(t, repo, nil, "worktree", "add", "-q", "-b", "side", wt)
	o := ReadOccupancy(repo)
	if o.Worktrees != 1 || !o.Occupied() {
		t.Fatalf("linked worktree not counted: %+v", o)
	}
}

// A repo that errors shows no camp: empty repos and plain directories read
// as unoccupied rather than failing.
func TestReadOccupancyErrorsReadEmpty(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "unborn")
	initRepo(t, empty)
	if o := ReadOccupancy(empty); o.Dirty || o.Worktrees != 0 {
		t.Fatalf("empty repo reads occupied: %+v", o)
	}
	if o := ReadOccupancy(t.TempDir()); o.Occupied() {
		t.Fatalf("non-repo directory reads occupied: %+v", o)
	}
}

func TestFingerprintChangesOnStageAndWorktree(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "living")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	fp := Fingerprint(repo)
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, nil, "add", "-A")
	fp2 := Fingerprint(repo)
	if fp2 == fp {
		t.Fatal("fingerprint did not change after staging")
	}
	wt := filepath.Join(t.TempDir(), "side")
	gitIn(t, repo, nil, "worktree", "add", "-q", "-b", "side", wt)
	if Fingerprint(repo) == fp2 {
		t.Fatal("fingerprint did not change after a worktree was added")
	}
}
