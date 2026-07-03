package forest

import (
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
)

func testTowns(now time.Time) []*model.Town {
	mk := func(name string, commits int, finished bool) *model.Town {
		rs := &events.RepoState{Name: name}
		rs.TotalCommits = commits
		rs.FirstTS = now.Add(-3 * 365 * 24 * time.Hour)
		rs.LastTS = now
		return model.NewTown(rs, finished)
	}
	towns := []*model.Town{
		mk("keepsake", 8, false),
		mk("tidetool", 900, false),
		mk("oldmill", 5000, true),
	}
	// tidetool is a settlement: components of every kind.
	towns[1].Components = map[string]*events.ComponentState{
		"engine":  {Name: "engine", Path: "engine", Bytes: 1 << 20, Files: 40, LastTS: now},
		"server":  {Name: "server", Path: "server", Bytes: 600 << 10, Files: 25, LastTS: now.Add(-40 * 24 * time.Hour)},
		"docs":    {Name: "docs", Path: "docs", Bytes: 200 << 10, Files: 12, LastTS: now.Add(-500 * 24 * time.Hour)},
		"tests":   {Name: "tests", Path: "tests", Bytes: 150 << 10, Files: 30, LastTS: now},
		"scripts": {Name: "scripts", Path: "scripts", Bytes: 30 << 10, Files: 6, LastTS: now.Add(-100 * 24 * time.Hour)},
	}
	return towns
}

// The settlers clear the yards and nothing more: no trunk may stand inside
// any claimed ground, and the hearth stays the focal point.
func TestSettlementCarvesYardsAndHoldsFocus(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	w := Build(5, testTowns(now))
	if len(w.Sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(w.Sites))
	}
	for _, s := range w.Sites {
		if s.SignX != s.Hearth.X {
			t.Errorf("%s: SignX %d is not the hearth %d", s.Town.Name, s.SignX, s.Hearth.X)
		}
		ys := s.yards()
		for _, tm := range s.trees {
			spans := ys.front
			if tm.back {
				spans = ys.back
			}
			for _, sp := range spans {
				if tm.x > sp[0] && tm.x < sp[1] {
					t.Errorf("%s: trunk at %d inside claimed ground (%d..%d, back=%v)",
						s.Town.Name, tm.x, sp[0], sp[1], tm.back)
				}
			}
		}
		for _, b := range s.Buildings {
			if b.X <= s.X0 || b.X >= s.X1 {
				t.Errorf("%s: building %s at %d outside the site (%d..%d)",
					s.Town.Name, b.B.Name, b.X, s.X0, s.X1)
			}
		}
	}
}

// A town with components grows a settlement; the barn stands nearest the
// hearth, and the special kinds take their forms.
func TestSettlementBuildings(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	w := Build(5, testTowns(now))
	s := w.Sites[1]
	if len(s.Buildings) != 5 {
		t.Fatalf("tidetool: %d buildings, want 5", len(s.Buildings))
	}
	forms := map[string]model.BuildingForm{}
	for _, b := range s.Buildings {
		forms[b.B.Name] = b.B.Form
	}
	if forms["engine"] != model.FormBarn {
		t.Errorf("engine should be the barn, got %v", forms["engine"])
	}
	if forms["tests"] != model.FormWatchtower {
		t.Errorf("tests should be the watchtower, got %v", forms["tests"])
	}
	if forms["docs"] != model.FormSchoolhouse {
		t.Errorf("docs should be the schoolhouse, got %v", forms["docs"])
	}
	if s.WellX == 0 {
		t.Errorf("a settlement of 5 should have its well")
	}
	// Buildings are placed biggest first, so on each side of the hearth the
	// distances grow in placement order: the settlement thins outward.
	lastW, lastE := 0, 0
	for _, b := range s.Buildings {
		d := b.X - s.Hearth.X
		if d < 0 {
			if -d <= lastW {
				t.Errorf("%s at %d does not thin outward west", b.B.Name, b.X)
			}
			lastW = -d
		} else {
			if d <= lastE {
				t.Errorf("%s at %d does not thin outward east", b.B.Name, b.X)
			}
			lastE = d
		}
	}
}

// The same seed always grows the same forest, settlements included.
func TestSettlementDeterministic(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	a := Build(5, testTowns(now))
	b := Build(5, testTowns(now))
	for i := range a.Sites {
		if a.Sites[i].Hearth != b.Sites[i].Hearth {
			t.Errorf("site %d: hearth differs across builds", i)
		}
		if len(a.Sites[i].Buildings) != len(b.Sites[i].Buildings) {
			t.Fatalf("site %d: building count differs", i)
		}
		for j := range a.Sites[i].Buildings {
			if a.Sites[i].Buildings[j] != b.Sites[i].Buildings[j] {
				t.Errorf("site %d building %d differs across builds", i, j)
			}
		}
	}
}

// Tiers follow commit volume: hut, cabin, homestead, a decade sooner.
func TestHearthTiers(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	w := Build(5, testTowns(now))
	want := []int{0, 2, 2}
	for i, s := range w.Sites {
		if s.Hearth.Tier != want[i] {
			t.Errorf("%s: tier %d, want %d", s.Town.Name, s.Hearth.Tier, want[i])
		}
	}
}
