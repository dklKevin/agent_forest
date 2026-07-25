package agentrun

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openLog(repo, run string) string {
	return filepath.Join(repo, ".agentforest", "runs", run, "events.jsonl")
}

func event(at time.Time, phase Phase, fields string) string {
	if fields != "" {
		fields = "," + fields
	}
	return fmt.Sprintf(`{"at":%q,"phase":%q%s}`+"\n", at.Format(time.RFC3339Nano), phase, fields)
}

func TestOpenFormatBuildsBoundedCuratedReplay(t *testing.T) {
	repo := t.TempDir()
	now := time.Date(2026, 7, 25, 18, 0, 0, 0, time.UTC)
	log := strings.Join([]string{
		`not json`,
		event(now.Add(-2*time.Minute), Planning,
			`"objective":"make local evidence visible","summary":"drew the boundary"`),
		event(now.Add(-time.Minute), Testing,
			`"summary":"ran the focused checks","paths":["internal/agentrun/read.go","../secret","/etc/passwd","internal/agentrun/read.go"],"verification":"passed","failure":"one retry","unresolved":["review the shape","review the shape"]`),
	}, "\n")
	put(t, openLog(repo, "open"), log)

	p := Read(repo, now)
	if p.Phase != Testing || !p.Active || p.Objective != "make local evidence visible" {
		t.Fatalf("presence = %+v", p)
	}
	if len(p.Steps) != 2 || p.Steps[1].Summary != "ran the focused checks" {
		t.Fatalf("steps = %#v", p.Steps)
	}
	if got := strings.Join(p.Paths, ","); got != "internal/agentrun/read.go" {
		t.Fatalf("unsafe or duplicate path survived: %q", got)
	}
	if p.Verification != "passed" || len(p.Failures) != 1 || len(p.Unresolved) != 1 {
		t.Fatalf("plaque fields = %+v", p)
	}
}

func TestOpenFormatReplaysAllFieldsInTimestampOrder(t *testing.T) {
	repo := t.TempDir()
	now := time.Date(2026, 7, 25, 18, 0, 0, 0, time.UTC)
	put(t, openLog(repo, "delayed"), strings.Join([]string{
		event(now.Add(-time.Minute), Testing,
			`"objective":"current","summary":"tested","paths":["new.go"],"verification":"passed","failure":"new failure","unresolved":["new item"]`),
		event(now.Add(-2*time.Minute), Building,
			`"objective":"old","summary":"built","paths":["old.go"],"verification":"failed","failure":"old failure","unresolved":["old item"]`),
	}, ""))

	p := Read(repo, now)
	if p.Phase != Testing || p.Objective != "current" || p.Verification != "passed" {
		t.Fatalf("newer fields were overwritten by delayed event: %+v", p)
	}
	if len(p.Steps) != 2 || p.Steps[0].Summary != "built" || p.Steps[1].Summary != "tested" {
		t.Fatalf("steps are not chronological: %#v", p.Steps)
	}
	if strings.Join(p.Paths, ",") != "old.go,new.go" ||
		strings.Join(p.Failures, ",") != "old failure,new failure" ||
		strings.Join(p.Unresolved, ",") != "old item,new item" {
		t.Fatalf("replay lists are not chronological: %+v", p)
	}
}

func TestIncompleteMalformedFutureAndStaleEvidence(t *testing.T) {
	now := time.Date(2026, 7, 25, 18, 0, 0, 0, time.UTC)
	t.Run("partial final line is not evidence", func(t *testing.T) {
		repo := t.TempDir()
		put(t, openLog(repo, "partial"), event(now.Add(-time.Minute), Building, "")+
			strings.TrimSuffix(event(now, Completed, ""), "\n"))
		p := Read(repo, now)
		if p.Phase != Building {
			t.Fatalf("partial line became a phase: %+v", p)
		}
	})
	t.Run("future evidence is ignored", func(t *testing.T) {
		repo := t.TempDir()
		put(t, openLog(repo, "future"), event(now.Add(3*time.Minute), Building, ""))
		if p := Read(repo, now); p.Available() {
			t.Fatalf("future evidence became presence: %+v", p)
		}
	})
	t.Run("stale plaque is retained but not active", func(t *testing.T) {
		repo := t.TempDir()
		put(t, openLog(repo, "stale"), event(now.Add(-16*time.Minute), Reviewing,
			`"summary":"left a review note"`))
		p := Read(repo, now)
		if !p.Available() || p.Active || p.Phase != Reviewing {
			t.Fatalf("stale evidence = %+v", p)
		}
	})
	t.Run("malformed only is absent", func(t *testing.T) {
		repo := t.TempDir()
		put(t, openLog(repo, "bad"), "{}\n{\"at\":")
		if p := Read(repo, now); p.Available() {
			t.Fatalf("malformed evidence became presence: %+v", p)
		}
	})
}

func TestGNHFCompatibilityReadsKindsAndSummariesOnly(t *testing.T) {
	repo := t.TempDir()
	dst := filepath.Join(repo, ".gnhf", "runs", "fixture")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyTree("testdata/gnhf/run", dst); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, name := range []string{"iteration-1.jsonl", "iteration-2.jsonl"} {
		if err := os.Chtimes(filepath.Join(dst, name), now, now); err != nil {
			t.Fatal(err)
		}
	}
	p := Read(repo, now)
	if p.Phase != Building || !p.Active || p.Objective != "" {
		t.Fatalf("presence = %+v", p)
	}
	got := fmt.Sprintf("%+v", p)
	for _, secret := range []string{
		"Preserve local work without surveillance", "must never appear",
		"private-provider-id", "99999",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("private raw field %q leaked into %+v", secret, p)
		}
	}
	if len(p.Steps) != 2 || p.Steps[0].Summary != "Established the filesystem boundary." {
		t.Fatalf("summaries = %#v", p.Steps)
	}
}

func TestGNHFThreadStartAloneIsPlanningNotFabricatedProgress(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".gnhf", "runs", "live", "iteration-12.jsonl")
	put(t, path, `{"type":"thread.started","thread_id":"private"}`+"\n")
	now := time.Now().UTC()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	p := Read(repo, now)
	if p.Phase != Planning || !p.Active {
		t.Fatalf("thread start = %+v", p)
	}
	if p.Phase == Building || p.Phase == Completed {
		t.Fatal("thread start fabricated progress")
	}
}

func TestSymlinkOversizeUnreadableAndMissingAreAbsent(t *testing.T) {
	now := time.Now().UTC()
	t.Run("missing", func(t *testing.T) {
		if p := Read(t.TempDir(), now); p.Available() {
			t.Fatalf("missing evidence became presence: %+v", p)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		repo := t.TempDir()
		target := filepath.Join(t.TempDir(), "events.jsonl")
		put(t, target, event(now, Blocked, `"`+"summary"+`":"outside"`))
		run := filepath.Dir(openLog(repo, "linked"))
		if err := os.MkdirAll(run, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(run, "events.jsonl")); err != nil {
			t.Fatal(err)
		}
		if p := Read(repo, now); p.Available() {
			t.Fatalf("symlink evidence became presence: %+v", p)
		}
	})
	t.Run("symlinked evidence root", func(t *testing.T) {
		repo := t.TempDir()
		outside := t.TempDir()
		put(t, openLog(outside, "linked"), event(now, Building, `"summary":"outside"`))
		if err := os.Symlink(filepath.Join(outside, ".agentforest"),
			filepath.Join(repo, ".agentforest")); err != nil {
			t.Fatal(err)
		}
		if p := Read(repo, now); p.Available() {
			t.Fatalf("symlinked evidence root became presence: %+v", p)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		repo := t.TempDir()
		put(t, openLog(repo, "huge"), strings.Repeat("x", maxFileBytes+1))
		if p := Read(repo, now); p.Available() {
			t.Fatalf("oversize evidence became presence: %+v", p)
		}
	})
	t.Run("permission denied", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix permissions")
		}
		repo := t.TempDir()
		path := openLog(repo, "closed")
		put(t, path, event(now, Blocked, ""))
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		if f, err := os.Open(path); err == nil {
			f.Close()
			t.Skip("test process can bypass file permissions")
		}
		if p := Read(repo, now); p.Available() {
			t.Fatalf("unreadable evidence became presence: %+v", p)
		}
	})
}

func TestNewestValidRunWins(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	put(t, openLog(repo, "older"), event(now.Add(-2*time.Minute), Building, `"objective":"old"`))
	put(t, openLog(repo, "newer"), event(now.Add(-time.Minute), Reviewing, `"objective":"new"`))
	p := Read(repo, now)
	if p.Phase != Reviewing || p.Objective != "new" {
		t.Fatalf("newest = %+v", p)
	}
}

func TestTouchedStaleRunsBeyondCausalLimitFailClosed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	for i := 0; i < maxRuns; i++ {
		path := openLog(repo, fmt.Sprintf("%02d-old", i))
		put(t, path, event(now.Add(-time.Hour-time.Duration(i)*time.Second), Planning,
			fmt.Sprintf(`"objective":"stale-%02d"`, i)))
		if err := os.Chtimes(path, now.Add(time.Duration(i)*time.Second), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	fresh := openLog(repo, "causally-newest")
	put(t, fresh, event(now.Add(-time.Minute), Testing, `"objective":"fresh-should-win"`))
	if err := os.Chtimes(fresh, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	p := Read(repo, now)
	if p.Available() {
		t.Fatalf("overflow selected touched stale evidence instead of failing closed: %+v", p)
	}
}

func TestCausalTimestampBeatsTouchedMtimeWithinRunLimit(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	for i := 0; i < maxRuns-1; i++ {
		path := openLog(repo, fmt.Sprintf("%02d-stale", i))
		put(t, path, event(now.Add(-time.Hour-time.Duration(i)*time.Second), Planning,
			fmt.Sprintf(`"objective":"stale-%02d"`, i)))
		if err := os.Chtimes(path, now.Add(time.Duration(i)*time.Second), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	fresh := openLog(repo, "causally-newest")
	put(t, fresh, event(now.Add(-time.Minute), Testing, `"objective":"fresh-should-win"`))
	if err := os.Chtimes(fresh, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	p := Read(repo, now)
	if p.Phase != Testing || p.Objective != "fresh-should-win" {
		t.Fatalf("file-touch order beat causal time: %+v", p)
	}
}

func TestCausalNewestRunWinsAcrossProviders(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	openPath := openLog(repo, "fresh")
	put(t, openPath, event(now.Add(-time.Minute), Testing, `"objective":"causally-fresh"`))
	if err := os.Chtimes(openPath, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	gnhfPath := filepath.Join(repo, ".gnhf", "runs", "touched-stale", "iteration-1.jsonl")
	put(t, gnhfPath, `{"type":"item.started"}`+"\n")
	if err := os.Chtimes(gnhfPath, now, now); err != nil {
		t.Fatal(err)
	}

	p := Read(repo, now)
	if p.Phase != Testing || p.Objective != "causally-fresh" {
		t.Fatalf("touched compatibility evidence starved causally newer run: %+v", p)
	}
}

func TestSafeReadDirKeepsHardTraversalLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		put(t, filepath.Join(dir, fmt.Sprintf("%02d", i)), "x")
	}
	if entries, err := safeReadDir(dir, 7); err == nil || entries != nil {
		t.Fatalf("overflow returned %d entries without failing closed", len(entries))
	}
}

func TestOverflowingGNHFRunFailsClosed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	put(t, openLog(repo, "otherwise-visible"), event(now.Add(-time.Hour), Planning, `"objective":"must-not-leak"`))
	run := filepath.Join(repo, ".gnhf", "runs", "overflow")
	for i := 1; i <= maxRunEntries+1; i++ {
		put(t, filepath.Join(run, fmt.Sprintf("iteration-%d.jsonl", i)),
			`{"type":"thread.started"}`+"\n")
	}
	if p := Read(repo, now); p.Available() {
		t.Fatalf("truncated run published a stale phase: %+v", p)
	}
}

func TestOversizedEvidenceFileFailsRepositoryClosed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	put(t, openLog(repo, "otherwise-visible"),
		event(now.Add(-time.Minute), Testing, `"objective":"must-not-leak"`))
	put(t, filepath.Join(repo, ".gnhf", "runs", "oversized", "iteration-1.jsonl"),
		strings.Repeat("x", maxFileBytes+1))

	if p := Read(repo, now); p.Available() {
		t.Fatalf("oversized provider candidate allowed stale fallback: %+v", p)
	}
}

func TestReadEnforcesAggregateByteBudgetAcrossRuns(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	first := openLog(repo, "newer")
	second := openLog(repo, "older")
	put(t, first, strings.Repeat("x", 64)+"\n")
	put(t, second, event(now.Add(-time.Minute), Planning, ""))
	if err := os.Chtimes(first, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(second, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	p := readWithBudgets(repo, now, 65, 0)
	if p.Available() {
		t.Fatalf("later run escaped aggregate scan budget: %+v", p)
	}
}

func TestReadBudgetCannotBeExhaustedByAnotherProvider(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	gnhfLog := `{"type":"item.started"}` + "\n"
	openData := strings.Repeat("x", len(gnhfLog)-1) + "\n"
	openPath := openLog(repo, "touched-stale")
	gnhfPath := filepath.Join(repo, ".gnhf", "runs", "active", "iteration-1.jsonl")
	put(t, openPath, openData)
	put(t, gnhfPath, gnhfLog)
	if err := os.Chtimes(openPath, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(gnhfPath, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	p := readWithBudgets(repo, now, int64(len(openData)), int64(len(gnhfLog)))
	if p.Phase != Building || !p.Active {
		t.Fatalf("one provider exhausted another provider's budget: %+v", p)
	}
}

func TestFreshnessAndTerminalPhasesDoNotInventOngoingWork(t *testing.T) {
	now := time.Now().UTC()
	p := Presence{Phase: Building, Active: true, UpdatedAt: now.Add(-freshFor)}
	if !p.ActiveAt(now) || p.ActiveAt(now.Add(time.Nanosecond)) {
		t.Fatal("freshness boundary is not exact")
	}
	for _, phase := range []Phase{Planning, Building, Testing, Reviewing, Blocked} {
		if !phase.InProgress() {
			t.Fatalf("%s should be an in-progress mark", phase)
		}
	}
	for _, phase := range []Phase{HandedOff, Completed} {
		if phase.InProgress() {
			t.Fatalf("%s must not imply ongoing work", phase)
		}
	}
}

func TestFingerprintChangesWithoutFollowingSymlinks(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	path := openLog(repo, "one")
	put(t, path, event(now, Planning, ""))
	first := Fingerprint(repo)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(event(now.Add(time.Second), Building, ""))
	_ = f.Close()
	second := Fingerprint(repo)
	if first == second {
		t.Fatal("append did not change fingerprint")
	}

	outside := filepath.Join(t.TempDir(), "outside")
	put(t, outside, "one")
	if err := os.Symlink(outside, filepath.Join(repo, ".agentforest", "runs", "one", "outside")); err != nil {
		t.Fatal(err)
	}
	before := Fingerprint(repo)
	put(t, outside, "a much larger outside value")
	if after := Fingerprint(repo); before != after {
		t.Fatal("fingerprint followed a symlink target")
	}
}

func TestConcurrentAppendAndReadKeepsLastCompleteEvidence(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	path := openLog(repo, "live")
	put(t, path, event(now.Add(-time.Minute), Planning, `"summary":"start"`))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 80; i++ {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return
			}
			_, _ = fmt.Fprintf(f, `{"at":%q,"phase":"building","summary":"turn %d"}`,
				now.Add(time.Duration(i)*time.Millisecond).Format(time.RFC3339Nano), i)
			if i%2 == 0 {
				_, _ = f.WriteString("\n")
			}
			_ = f.Close()
		}
	}()
	for i := 0; i < 80; i++ {
		p := Read(repo, now.Add(time.Minute))
		if p.Available() && p.Phase != Planning && p.Phase != Building {
			t.Fatalf("concurrent read fabricated evidence: %+v", p)
		}
	}
	wg.Wait()
	if p := Read(repo, now.Add(time.Minute)); !p.Available() || (p.Phase != Planning && p.Phase != Building) {
		t.Fatalf("settled append-only evidence was not readable: %+v", p)
	}
}

func TestConcurrentGrowthCannotExceedChargedBudgetOrPublishRecord(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC()
	path := openLog(repo, "growing")
	initial := event(now.Add(-time.Minute), Planning, `"objective":"within-budget"`)
	appended := event(now, Reviewing, `"objective":"over-budget-must-not-appear"`)
	put(t, path, initial)

	budget := &readBudget{remaining: int64(len(initial))}
	var once sync.Once
	budget.beforeRead = func(got string) {
		if got != path {
			return
		}
		once.Do(func() {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(appended); err != nil {
				f.Close()
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}

	p, ok := readOpenRun(filepath.Dir(path), now, budget)
	if ok || p.Available() || p.Phase == Reviewing ||
		strings.Contains(p.Objective, "over-budget") {
		t.Fatalf("concurrent growth affected visible presence: %+v", p)
	}
	if !budget.uncertain {
		t.Fatal("concurrent descriptor growth was not detected")
	}
	if budget.consumed > int64(len(initial)) || budget.remaining < 0 {
		t.Fatalf("read exceeded charged limit: consumed=%d remaining=%d limit=%d",
			budget.consumed, budget.remaining, len(initial))
	}
	if budget.consumed != int64(len(initial)) {
		t.Fatalf("actual bytes were not reconciled: consumed=%d want=%d", budget.consumed, len(initial))
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(to, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, info.Mode())
	})
}
