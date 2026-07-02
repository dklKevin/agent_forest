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
