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
