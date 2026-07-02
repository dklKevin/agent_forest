package forest

import (
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
	"github.com/dklKevin/agentforest/internal/sprite"
)

func testTowns(now time.Time) []*model.Town {
	mk := func(name string, commits int, finished bool) *model.Town {
		rs := &events.RepoState{Name: name}
		rs.TotalCommits = commits
		rs.FirstTS = now.Add(-3 * 365 * 24 * time.Hour)
		rs.LastTS = now
		return model.NewTown(rs, finished)
	}
	return []*model.Town{
		mk("keepsake", 60, false),
		mk("tidetool", 900, false),
		mk("oldmill", 5000, true),
	}
}

// The settler clears the footprint and dooryard, nothing more: no trunk may
// stand inside the hearth's yard, and the hearth is the site's focal point.
func TestHearthCarvesYardAndHoldsFocus(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	w := Build(5, testTowns(now))
	if len(w.Sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(w.Sites))
	}
	for _, s := range w.Sites {
		if s.SignX != s.Hearth.X {
			t.Errorf("%s: SignX %d is not the hearth %d", s.Town.Name, s.SignX, s.Hearth.X)
		}
		if s.Town.Finished && s.Hearth.X != s.X0+(s.X1-s.X0)/2 {
			t.Errorf("%s: a finished hearth should hold the center", s.Town.Name)
		}
		gw, ge, gb := sprite.CabinYardGaps(s.Hearth.Tier, s.Hearth.Seed)
		for _, tm := range s.trees {
			lo, hi := s.Hearth.X-gw, s.Hearth.X+ge
			if tm.back {
				lo, hi = s.Hearth.X-gb, s.Hearth.X+gb
			}
			if tm.x > lo && tm.x < hi {
				t.Errorf("%s: trunk at %d inside the yard (%d..%d, back=%v)",
					s.Town.Name, tm.x, lo, hi, tm.back)
			}
		}
	}
}

// The same seed always grows the same forest, hearths included.
func TestHearthDeterministic(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	a := Build(5, testTowns(now))
	b := Build(5, testTowns(now))
	for i := range a.Sites {
		ha, hb := a.Sites[i].Hearth, b.Sites[i].Hearth
		if ha != hb {
			t.Errorf("site %d: hearth differs across builds: %+v vs %+v", i, ha, hb)
		}
	}
}

// Tiers follow commit volume: hut, cabin, homestead.
func TestHearthTiers(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	w := Build(5, testTowns(now))
	want := []int{0, 1, 2}
	for i, s := range w.Sites {
		if s.Hearth.Tier != want[i] {
			t.Errorf("%s: tier %d, want %d", s.Town.Name, s.Hearth.Tier, want[i])
		}
	}
}
