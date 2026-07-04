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

func TestDetectComponents(t *testing.T) {
	kb := func(n int64) int64 { return n * 1024 }
	files := []trackedFile{
		{"README.md", kb(2)}, // root files belong to the hearth
		{".github/ci.yml", kb(40)},
		{"engine/a.go", kb(300)}, {"engine/b.go", kb(200)}, {"engine/c.go", kb(100)},
		{"docs/a.md", kb(30)}, {"docs/b.md", kb(30)}, {"docs/c.md", kb(30)},
		{"tiny/x.go", kb(1)}, // below the floor
	}
	comps := detectComponents(files)
	if len(comps) != 2 {
		t.Fatalf("components = %d (%+v), want 2", len(comps), comps)
	}
	if comps[0].Path != "engine" || comps[1].Path != "docs" {
		t.Fatalf("wrong components or order: %+v", comps)
	}

	// A dominant wrapper is descended one level; siblings stay.
	files = []trackedFile{
		{"src/core/a.go", kb(500)}, {"src/core/b.go", kb(300)}, {"src/core/c.go", kb(100)},
		{"src/util/a.go", kb(80)}, {"src/util/b.go", kb(40)}, {"src/util/c.go", kb(30)},
		{"src/loose.go", kb(10)}, // directly in the wrapper: hearth's
		{"docs/a.md", kb(20)}, {"docs/b.md", kb(20)}, {"docs/c.md", kb(20)},
	}
	comps = detectComponents(files)
	want := map[string]string{"src/core": "core", "src/util": "util", "docs": "docs"}
	if len(comps) != len(want) {
		t.Fatalf("components = %+v, want %d", comps, len(want))
	}
	for _, c := range comps {
		if want[c.Path] != c.Name {
			t.Errorf("component %q name %q, want %q", c.Path, c.Name, want[c.Path])
		}
	}
}

// TestDetectComponentsDominantFileKeepsSiblings pins the component-detection
// floor bug: a single large generated/vendored/lock file in one directory must
// not suppress sibling directories of real code. Because the byte floor is
// relative to the repo total, engine/'s 20 KB blob would otherwise drag every
// sibling under 1% of repo bytes and collapse a genuinely structured repo into
// a lone building. Flooring on file share as well as byte share keeps a
// directory with a real file count standing even when another dominates bytes.
func TestDetectComponentsDominantFileKeepsSiblings(t *testing.T) {
	kb := func(n int64) int64 { return n * 1024 }
	files := []trackedFile{
		// engine/: two small real files plus one 20 KB generated blob that
		// dominates the repo's byte total.
		{"engine/engine.go", 40}, {"engine/lifecycle.go", 40}, {"engine/generated.lock", kb(20)},
		// Four sibling directories of genuine, small source code (>=3 files each).
		{"server/a.go", 30}, {"server/b.go", 30}, {"server/c.go", 30},
		{"cli/a.go", 30}, {"cli/b.go", 30}, {"cli/c.go", 30},
		{"parser/a.go", 30}, {"parser/b.go", 30}, {"parser/c.go", 30},
		{"store/a.go", 30}, {"store/b.go", 30}, {"store/c.go", 30},
	}
	got := map[string]bool{}
	for _, c := range detectComponents(files) {
		got[c.Name] = true
	}
	for _, want := range []string{"engine", "server", "cli", "parser", "store"} {
		if !got[want] {
			t.Errorf("component %q vanished: a dominant file suppressed a real sibling (got %v)", want, got)
		}
	}
	if len(got) != 5 {
		t.Fatalf("components = %v, want exactly the 5 real directories", got)
	}
}

func TestScanEmitsComponentsWithOwnClocks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "village")
	initRepo(t, dir)
	old := time.Now().Add(-400 * 24 * time.Hour).Truncate(time.Second)
	mid := time.Now().Add(-40 * 24 * time.Hour).Truncate(time.Second)
	fresh := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	for _, d := range []string{"engine", "docs", "tests"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commitAt(t, dir, old, "docs/a.md", strings.Repeat("d", 9000))
	commitAt(t, dir, old, "docs/b.md", strings.Repeat("d", 9000))
	commitAt(t, dir, old, "docs/c.md", strings.Repeat("d", 9000))
	commitAt(t, dir, mid, "tests/a_test.go", strings.Repeat("t", 9000))
	commitAt(t, dir, mid, "tests/b_test.go", strings.Repeat("t", 9000))
	commitAt(t, dir, mid, "tests/c_test.go", strings.Repeat("t", 9000))
	commitAt(t, dir, fresh, "engine/a.go", strings.Repeat("e", 40000))
	commitAt(t, dir, fresh, "engine/b.go", strings.Repeat("e", 40000))
	commitAt(t, dir, fresh, "engine/c.go", strings.Repeat("e", 40000))

	evs, err := Scan(dir, Known{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	comp := map[string]events.Event{}
	for _, e := range evs {
		if e.Kind == events.KindComp {
			comp[e.Path] = e
		}
	}
	if len(comp) != 3 {
		t.Fatalf("comp events = %d (%v), want 3", len(comp), comp)
	}
	if !comp["docs"].TS.Equal(old) {
		t.Errorf("docs last touch %v, want %v", comp["docs"].TS, old)
	}
	if !comp["tests"].TS.Equal(mid) {
		t.Errorf("tests last touch %v, want %v", comp["tests"].TS, mid)
	}
	if !comp["engine"].TS.Equal(fresh) {
		t.Errorf("engine last touch %v, want %v", comp["engine"].TS, fresh)
	}

	// A rescan with the log's knowledge is silent.
	known := Known{Announced: true, LastTS: fresh, Comps: map[string]KnownComp{}}
	for p, e := range comp {
		known.Comps[p] = KnownComp{Bytes: e.Bytes, LastTS: e.TS}
	}
	known.Mix = map[string]float64{}
	evs, err = Scan(dir, known, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == events.KindComp {
			t.Errorf("silent rescan emitted comp event: %+v", e)
		}
	}

	// A commit touching one directory advances only that component.
	later := time.Now().Truncate(time.Second)
	commitAt(t, dir, later, "docs/new.md", strings.Repeat("n", 9000))
	evs, err = Scan(dir, known, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var compEvs []events.Event
	for _, e := range evs {
		if e.Kind == events.KindComp {
			compEvs = append(compEvs, e)
		}
	}
	if len(compEvs) != 1 || compEvs[0].Path != "docs" || !compEvs[0].TS.Equal(later) {
		t.Fatalf("docs commit produced %+v, want one docs event at %v", compEvs, later)
	}
}

// HeadBranch and DefaultBranch read the git dir as plain files: the branch
// comes back with no process spawned, detached heads stay quiet, and the
// default falls back to a local main when origin/HEAD is absent.
func TestHeadAndDefaultBranch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "branchy")
	initRepo(t, dir)
	commitAt(t, dir, time.Now().Add(-time.Hour), "a.go", "package a")

	if got := HeadBranch(dir); got != "main" {
		t.Fatalf("HeadBranch = %q, want main", got)
	}
	if got := DefaultBranch(dir); got != "main" {
		t.Fatalf("DefaultBranch = %q, want main (local fallback)", got)
	}

	gitIn(t, dir, nil, "checkout", "-q", "-b", "feature/x")
	if got := HeadBranch(dir); got != "feature/x" {
		t.Fatalf("HeadBranch = %q, want feature/x", got)
	}
	if got := DefaultBranch(dir); got != "main" {
		t.Fatalf("DefaultBranch = %q, want main", got)
	}

	// origin/HEAD, when present, names the default outright.
	if err := os.MkdirAll(filepath.Join(dir, ".git", "refs", "remotes", "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "refs", "remotes", "origin", "HEAD"),
		[]byte("ref: refs/remotes/origin/trunk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DefaultBranch(dir); got != "trunk" {
		t.Fatalf("DefaultBranch = %q, want trunk (origin/HEAD)", got)
	}

	// A detached HEAD names no branch at all.
	gitIn(t, dir, nil, "checkout", "-q", "--detach")
	if got := HeadBranch(dir); got != "" {
		t.Fatalf("detached HeadBranch = %q, want empty", got)
	}

	if got := HeadBranch(filepath.Join(dir, "nope")); got != "" {
		t.Fatalf("missing repo HeadBranch = %q, want empty", got)
	}
	if got := DefaultBranch(filepath.Join(dir, "nope")); got != "" {
		t.Fatalf("missing repo DefaultBranch = %q, want empty", got)
	}
}

// Packed refs still reveal the local default when loose ref files are gone.
func TestDefaultBranchReadsPackedRefs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "packed")
	initRepo(t, dir)
	commitAt(t, dir, time.Now().Add(-time.Hour), "a.go", "package a")
	gitIn(t, dir, nil, "pack-refs", "--all")
	if _, err := os.Stat(filepath.Join(dir, ".git", "refs", "heads", "main")); err == nil {
		t.Skip("loose ref survived pack-refs; nothing to exercise")
	}
	if got := DefaultBranch(dir); got != "main" {
		t.Fatalf("DefaultBranch = %q, want main from packed-refs", got)
	}
}
