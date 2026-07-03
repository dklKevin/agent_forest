package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
)

func settlementTown(now time.Time) *Town {
	rs := &events.RepoState{Name: "village", Path: "/v"}
	rs.TotalCommits = 3000
	rs.FirstTS = now.Add(-4 * 365 * 24 * time.Hour)
	rs.LastTS = now
	rs.Components = map[string]*events.ComponentState{
		"engine": {Name: "engine", Path: "engine", Bytes: 1000, Files: 50, LastTS: now},
		"server": {Name: "server", Path: "server", Bytes: 600, Files: 30, LastTS: now.Add(-2 * 24 * time.Hour)},
		"cli":    {Name: "cli", Path: "cli", Bytes: 200, Files: 12, LastTS: now.Add(-30 * 24 * time.Hour)},
		"tools":  {Name: "tools", Path: "tools", Bytes: 40, Files: 5, LastTS: now.Add(-90 * 24 * time.Hour)},
		"docs":   {Name: "docs", Path: "docs", Bytes: 900, Files: 40, LastTS: now.Add(-400 * 24 * time.Hour)},
		"tests":  {Name: "tests", Path: "tests", Bytes: 300, Files: 60, LastTS: now},
	}
	return NewTown(rs, false)
}

// Forms: the confident kinds take their buildings regardless of size, and
// the rest rank against the largest code component.
func TestBuildingsForms(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	town := settlementTown(now)
	got := map[string]BuildingForm{}
	for _, b := range town.Buildings() {
		got[b.Name] = b.Form
	}
	want := map[string]BuildingForm{
		"engine": FormBarn,        // largest code component
		"server": FormHomeplace,   // 60% of the barn
		"cli":    FormWorkshop,    // 20%
		"docs":   FormSchoolhouse, // kind wins over its size
		"tests":  FormWatchtower,  // kind wins
	}
	for name, form := range want {
		if got[name] != form {
			t.Errorf("%s: form %v, want %v", name, got[name], form)
		}
	}
	if got["tools"] != FormShed && got["tools"] != FormCrib {
		t.Errorf("tools: form %v, want a shed or crib", got["tools"])
	}
}

// The village ceiling holds: never more than 12 buildings.
func TestBuildingsCeiling(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	town := settlementTown(now)
	for i := 0; i < 20; i++ {
		p := fmt.Sprintf("extra%02d", i)
		town.Components[p] = &events.ComponentState{
			Name: p, Path: p, Bytes: int64(100 + i), Files: 9, LastTS: now,
		}
	}
	if n := len(town.Buildings()); n != 12 {
		t.Fatalf("buildings = %d, want the ceiling of 12", n)
	}
}

// Each building decays on its own component's clock; the almanac's town
// override slides everything forward preserving each building's offset.
func TestBuildingDecayClocks(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	town := settlementTown(now)
	var engine, docs Building
	for _, b := range town.Buildings() {
		switch b.Name {
		case "engine":
			engine = b
		case "docs":
			docs = b
		}
	}
	if d := town.BuildingDecay(engine, now); d != 0 {
		t.Errorf("fresh engine decay = %v, want 0", d)
	}
	dd := town.BuildingDecay(docs, now)
	if dd < 0.9 {
		t.Errorf("400-day docs decay = %v, want deep", dd)
	}
	// Almanac preview: slide the town two years out.
	ov := 2 * 365 * 24 * time.Hour
	town.IdleOverride = &ov
	de := town.BuildingIdle(engine, now)
	dc := town.BuildingIdle(docs, now)
	if de != ov {
		t.Errorf("engine idle under preview = %v, want %v", de, ov)
	}
	if dc != ov+400*24*time.Hour {
		t.Errorf("docs idle under preview = %v, want offset preserved", dc)
	}
	// A revive override wins over everything.
	town.IdleOverride = nil
	town.CompIdleOverride = map[string]time.Duration{"docs": time.Hour}
	if d := town.BuildingDecay(docs, now); d != 0 {
		t.Errorf("reviving docs decay = %v, want 0 (within grace)", d)
	}
	// Finished settlements are kept whole.
	town.CompIdleOverride = nil
	town.Finished = true
	if d := town.BuildingDecay(docs, now); d != 0 {
		t.Errorf("kept settlement decay = %v, want 0", d)
	}
}
