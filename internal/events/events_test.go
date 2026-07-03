package events

import (
	"testing"
	"time"
)

func TestReduceDerivesRepoState(t *testing.T) {
	base := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	evs := []Event{
		{Kind: KindActivity, Repo: "a", TS: base.AddDate(0, 6, 0), Commits: 5},
		{Kind: KindRepo, Repo: "a", TS: base, Path: "/x/a"},
		{Kind: KindActivity, Repo: "a", TS: base, Commits: 2},
		{Kind: KindTag, Repo: "a", TS: base.AddDate(0, 3, 0), Name: "v1.0.0"},
		{Kind: KindLangs, Repo: "a", TS: base, Mix: map[string]float64{"go": 0.9, "shell": 0.1}},
		{Kind: KindRepo, Repo: "b", TS: base.AddDate(1, 0, 0), Path: "/x/b"},
		{Kind: KindActivity, Repo: "b", TS: base.AddDate(1, 0, 0), Commits: 1},
	}
	repos := Reduce(evs)
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	// Oldest first: that ordering is also the world's west-to-east layout.
	a, b := repos[0], repos[1]
	if a.Name != "a" || b.Name != "b" {
		t.Fatalf("order wrong: %s, %s", a.Name, b.Name)
	}
	if a.TotalCommits != 7 {
		t.Fatalf("commits = %d, want 7", a.TotalCommits)
	}
	if !a.FirstTS.Equal(base) {
		t.Fatalf("first = %v, want %v", a.FirstTS, base)
	}
	if !a.LastTS.Equal(base.AddDate(0, 6, 0)) {
		t.Fatalf("last = %v", a.LastTS)
	}
	if a.PrimaryLang() != "go" {
		t.Fatalf("primary lang = %q", a.PrimaryLang())
	}
	if len(a.Tags) != 1 || a.Tags[0] != "v1.0.0" {
		t.Fatalf("tags = %v", a.Tags)
	}
}

func TestReduceFinishFold(t *testing.T) {
	base := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(days int) time.Time { return base.AddDate(0, 0, days) }
	repo := []Event{
		{Kind: KindRepo, Repo: "/x/a", TS: base, Path: "/x/a", Name: "a"},
		{Kind: KindActivity, Repo: "/x/a", TS: base, Commits: 3},
	}
	reduceOne := func(extra ...Event) *RepoState {
		t.Helper()
		repos := Reduce(append(append([]Event{}, repo...), extra...))
		if len(repos) != 1 {
			t.Fatalf("got %d repos, want 1", len(repos))
		}
		return repos[0]
	}

	// Untouched: not finished, nothing carved.
	r := reduceOne()
	if r.Finished || r.Epitaph != "" || !r.FinishTS.IsZero() {
		t.Fatalf("clean repo carries finish state: %+v", r)
	}

	// A finish carries its epitaph and its moment.
	r = reduceOne(Event{Kind: KindFinish, Repo: "/x/a", TS: at(10), Epitaph: "done well"})
	if !r.Finished || r.Epitaph != "done well" || !r.FinishTS.Equal(at(10)) {
		t.Fatalf("finish fold wrong: %+v", r)
	}

	// Unfinish reverses the standing but never erases the words.
	r = reduceOne(
		Event{Kind: KindFinish, Repo: "/x/a", TS: at(10), Epitaph: "done well"},
		Event{Kind: KindUnfinish, Repo: "/x/a", TS: at(20)},
	)
	if r.Finished {
		t.Fatal("unfinish did not reverse")
	}
	if r.Epitaph != "done well" {
		t.Fatalf("unfinish erased the epitaph: %q", r.Epitaph)
	}

	// Re-finishing unmarked keeps the old words; the log lost nothing.
	r = reduceOne(
		Event{Kind: KindFinish, Repo: "/x/a", TS: at(10), Epitaph: "done well"},
		Event{Kind: KindUnfinish, Repo: "/x/a", TS: at(20)},
		Event{Kind: KindFinish, Repo: "/x/a", TS: at(30)},
	)
	if !r.Finished || r.Epitaph != "done well" || !r.FinishTS.Equal(at(30)) {
		t.Fatalf("unmarked re-finish fold wrong: %+v", r)
	}

	// Re-carving: the last epitaph wins for display.
	r = reduceOne(
		Event{Kind: KindFinish, Repo: "/x/a", TS: at(10), Epitaph: "done well"},
		Event{Kind: KindUnfinish, Repo: "/x/a", TS: at(20)},
		Event{Kind: KindFinish, Repo: "/x/a", TS: at(30), Epitaph: "done better"},
	)
	if !r.Finished || r.Epitaph != "done better" {
		t.Fatalf("last epitaph did not win: %+v", r)
	}
}

func TestReducePrefersExplicitName(t *testing.T) {
	ts := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	repos := Reduce([]Event{
		{Kind: KindRepo, Repo: "/home/x/keep", TS: ts, Path: "/home/x/keep", Name: "keep"},
		{Kind: KindActivity, Repo: "/home/x/keep", TS: ts, Commits: 1},
	})
	if len(repos) != 1 || repos[0].Name != "keep" || repos[0].Path != "/home/x/keep" {
		t.Fatalf("name/path wrong: %+v", repos[0])
	}
}
