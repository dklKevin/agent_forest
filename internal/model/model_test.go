package model

import (
	"math"
	"testing"
	"time"
)

func TestDecayCurve(t *testing.T) {
	day := 24 * time.Hour
	if d := DecayAt(12 * time.Hour); d != 0 {
		t.Fatalf("inside grace period, decay = %v, want 0", d)
	}
	// Roughly 1%/day at the start.
	d2 := DecayAt(2 * day)
	if d2 < 0.005 || d2 > 0.02 {
		t.Fatalf("day 2 decay = %v, want ~1%%", d2)
	}
	// Monotonic and capped.
	prev := -1.0
	for days := 0; days < 1200; days += 7 {
		d := DecayAt(time.Duration(days) * 24 * time.Hour)
		if d < prev {
			t.Fatalf("decay not monotonic at day %d", days)
		}
		if d > decayCap {
			t.Fatalf("decay exceeds cap at day %d: %v", days, d)
		}
		prev = d
	}
}

func TestIdleForDecayInverts(t *testing.T) {
	for _, d := range []float64{0.05, 0.15, 0.37, 0.62, 0.85, 0.965} {
		got := DecayAt(IdleForDecay(d))
		if math.Abs(got-d) > 0.002 {
			t.Fatalf("DecayAt(IdleForDecay(%v)) = %v", d, got)
		}
	}
}

func TestStageBoundaries(t *testing.T) {
	cases := map[float64]Stage{
		0.0: Tended, 0.1: FirstQuiet, 0.3: Overgrown,
		0.6: Breaking, 0.8: Skeletal, 0.97: Ruin,
	}
	for d, want := range cases {
		if got := StageOf(d); got != want {
			t.Fatalf("StageOf(%v) = %v, want %v", d, got, want)
		}
	}
}

func TestSpeciesMappingStable(t *testing.T) {
	if SpeciesFor("go") != Oak || SpeciesFor("rust") != Spruce || SpeciesFor("python") != Willow {
		t.Fatal("core language mapping changed")
	}
	// Unknown languages hash deterministically into a real species.
	a, b := SpeciesFor("zig"), SpeciesFor("zig")
	if a != b {
		t.Fatal("unknown-language species not stable")
	}
	if a < 0 || a > Grove {
		t.Fatalf("unknown-language species out of range: %v", a)
	}
}

func TestFinishedNeverDecays(t *testing.T) {
	town := &Town{Finished: true}
	if d := DecayAt(1000 * 24 * time.Hour); d == 0 {
		t.Fatal("sanity: raw decay should be nonzero")
	}
	if d := town.Decay(time.Now()); d != 0 {
		t.Fatalf("finished town decays: %v", d)
	}
}

func TestTendCurve(t *testing.T) {
	day := 24 * time.Hour
	if v := TendAt(0); v != 1 {
		t.Fatalf("tend at zero idle = %v, want 1", v)
	}
	// The three mood grades sit in distinct bands of one continuous curve.
	today := TendAt(6 * time.Hour)
	week := TendAt(4 * day)
	kept := TendAt(12 * day)
	if today <= 0.7 {
		t.Fatalf("worked-today tend = %v, want > 0.7", today)
	}
	if week <= 0.2 || week >= 0.7 {
		t.Fatalf("worked-this-week tend = %v, want in (0.2, 0.7)", week)
	}
	if kept <= 0.02 || kept >= 0.2 {
		t.Fatalf("quiet-but-kept tend = %v, want in (0.02, 0.2)", kept)
	}
	// Out entirely past the cut, so deep decay stages carry no tend at all.
	if v := TendAt(time.Duration(tendCutDays) * 24 * time.Hour); v != 0 {
		t.Fatalf("tend past the cut = %v, want 0", v)
	}
	// Monotonic non-increasing across the whole range.
	prev := 2.0
	for h := 0; h < 24*30; h += 6 {
		v := TendAt(time.Duration(h) * time.Hour)
		if v > prev {
			t.Fatalf("tend not monotonic at hour %d", h)
		}
		prev = v
	}
}

func TestFinishedIsStillNotAlive(t *testing.T) {
	town := &Town{Finished: true}
	if v := town.Tend(time.Now()); v != 0 {
		t.Fatalf("finished town tends: %v", v)
	}
	if v := town.BuildingTend(Building{}, time.Now()); v != 0 {
		t.Fatalf("finished town's building tends: %v", v)
	}
}
