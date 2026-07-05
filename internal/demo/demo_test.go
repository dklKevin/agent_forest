package demo

import (
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
)

func TestEventsCarryFinishedCast(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	finished := FinishedNames()
	for _, r := range events.Reduce(Events(5, now)) {
		if r.Finished != finished[r.Name] {
			t.Fatalf("%s finished = %v, want %v", r.Name, r.Finished, finished[r.Name])
		}
		if r.Finished && r.FinishTS.IsZero() {
			t.Fatalf("%s is finished without a finish time", r.Name)
		}
	}
}

// The demo camp is display state on the cast alone: exactly the towns named
// by Occupancies carry it, never a finished one, and the event log stays
// free of it - the same ephemerality rule real occupancy lives by.
func TestTownsAttachCastOccupancy(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	towns, evs := Towns(5, now)
	occ := Occupancies()
	for name := range occ {
		if FinishedNames()[name] {
			t.Fatalf("%s: a finished cast member must not be given a camp", name)
		}
	}
	for _, town := range towns {
		want := occ[town.Name]
		if town.Occupancy != want {
			t.Fatalf("%s occupancy = %+v, want %+v", town.Name, town.Occupancy, want)
		}
	}
	for _, e := range evs {
		switch e.Kind {
		case events.KindRepo, events.KindActivity, events.KindTag,
			events.KindLangs, events.KindComp, events.KindFinish, events.KindUnfinish:
		default:
			t.Fatalf("demo log carries an unexpected event kind %q", e.Kind)
		}
	}
}
