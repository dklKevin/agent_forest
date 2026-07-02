package gitscan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
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

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, nil, "init", "-q", "-b", "main")
}

func commitAt(t *testing.T, dir string, ts time.Time, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := fmt.Sprintf("%d +0000", ts.Unix())
	env := []string{"GIT_AUTHOR_DATE=" + stamp, "GIT_COMMITTER_DATE=" + stamp}
	gitIn(t, dir, nil, "add", "-A")
	gitIn(t, dir, env, "commit", "-q", "-m", "c: "+file)
}

func TestDiscoverFindsReposAndSkipsNoise(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "a"))
	initRepo(t, filepath.Join(root, "x", "deep", "b"))
	initRepo(t, filepath.Join(root, ".hidden", "c"))
	initRepo(t, filepath.Join(root, "node_modules", "d"))
	initRepo(t, filepath.Join(root, "a", "inner")) // inside repo a: not scanned

	repos := Discover([]string{root, root}) // duplicate root must dedupe
	var names []string
	for _, r := range repos {
		names = append(names, filepath.Base(r))
	}
	if strings.Join(names, ",") != "a,b" {
		t.Fatalf("discovered %v, want [a b]", names)
	}
}

func TestBucketByDay(t *testing.T) {
	day1a := time.Date(2024, 3, 1, 10, 0, 0, 0, time.Local)
	day1b := time.Date(2024, 3, 1, 15, 30, 0, 0, time.Local)
	day2 := time.Date(2024, 3, 2, 9, 0, 0, 0, time.Local)
	evs := bucketByDay("r", []time.Time{day1a, day1b, day2}, time.Time{})
	if len(evs) != 2 {
		t.Fatalf("buckets = %d, want 2", len(evs))
	}
	if evs[0].Commits != 2 || !evs[0].TS.Equal(day1b) {
		t.Fatalf("day1 bucket wrong: %+v", evs[0])
	}
	if evs[1].Commits != 1 || !evs[1].TS.Equal(day2) {
		t.Fatalf("day2 bucket wrong: %+v", evs[1])
	}
	// Only commits strictly after the cutoff appear.
	evs = bucketByDay("r", []time.Time{day1a, day1b, day2}, day1b)
	if len(evs) != 1 || evs[0].Commits != 1 {
		t.Fatalf("cutoff not honored: %+v", evs)
	}
}

func TestScanInitialAndIncremental(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "keep")
	initRepo(t, repo)
	first := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	commitAt(t, repo, first, "main.go", strings.Repeat("g", 300))
	commitAt(t, repo, first.Add(time.Hour), "main.go", strings.Repeat("g", 400))
	commitAt(t, repo, first.Add(26*time.Hour), "main.go", strings.Repeat("g", 500))
	gitIn(t, repo, nil, "tag", "v1")

	now := time.Now()
	evs, err := Scan(repo, Known{}, now)
	if err != nil {
		t.Fatal(err)
	}
	repos := events.Reduce(evs)
	if len(repos) != 1 {
		t.Fatalf("repos = %d", len(repos))
	}
	r := repos[0]
	if r.Name != "keep" || r.TotalCommits != 3 {
		t.Fatalf("state = %q %d commits, want keep 3", r.Name, r.TotalCommits)
	}
	if !r.FirstTS.Equal(first) {
		t.Fatalf("first = %v, want %v", r.FirstTS, first)
	}
	if !r.LastTS.Equal(first.Add(26 * time.Hour)) {
		t.Fatalf("last = %v", r.LastTS)
	}
	if len(r.Tags) != 1 || r.Tags[0] != "v1" {
		t.Fatalf("tags = %v", r.Tags)
	}
	if r.PrimaryLang() != "go" {
		t.Fatalf("primary = %q", r.PrimaryLang())
	}

	// A rescan with the log's knowledge must be silent.
	known := Known{Announced: true, LastTS: r.LastTS, Tags: map[string]bool{"v1": true}, Mix: r.Mix}
	evs, err = Scan(repo, known, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("silent rescan emitted %d events: %+v", len(evs), evs)
	}

	// New work: a commit that tips the language mix, and a new tag.
	commitAt(t, repo, first.Add(28*time.Hour), "core.rs", strings.Repeat("r", 4000))
	gitIn(t, repo, nil, "tag", "v2")
	evs, err = Scan(repo, known, now)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	total := 0
	for _, e := range evs {
		kinds = append(kinds, string(e.Kind))
		if e.Kind == events.KindActivity {
			total += e.Commits
		}
		if e.Kind == events.KindRepo {
			t.Fatal("repo re-announced on incremental scan")
		}
	}
	if total != 1 {
		t.Fatalf("incremental commits = %d, want 1", total)
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "tag") || !strings.Contains(joined, "langs") {
		t.Fatalf("expected new tag and langs snapshot, got %v", joined)
	}
	for _, e := range evs {
		if e.Kind == events.KindLangs && e.Mix["rust"] <= e.Mix["go"] {
			t.Fatalf("mix should now lean rust: %v", e.Mix)
		}
		if e.Kind == events.KindTag && e.Name != "v2" {
			t.Fatalf("new tag = %q, want v2", e.Name)
		}
	}
}

func TestScanEmptyRepo(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "bare")
	initRepo(t, repo)
	evs, err := Scan(repo, Known{}, time.Now())
	if err != nil || len(evs) != 0 {
		t.Fatalf("empty repo should be silent, got %d events err=%v", len(evs), err)
	}
}

func TestFingerprintChangesOnCommit(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "fp")
	initRepo(t, repo)
	commitAt(t, repo, time.Now().Add(-time.Hour), "a.go", "package a")
	fp1 := Fingerprint(repo)
	if fp1 == "" {
		t.Fatal("fingerprint empty for a healthy repo")
	}
	if Fingerprint(repo) != fp1 {
		t.Fatal("fingerprint unstable without changes")
	}
	commitAt(t, repo, time.Now(), "a.go", "package a // v2")
	if Fingerprint(repo) == fp1 {
		t.Fatal("fingerprint did not change after a commit")
	}
}
