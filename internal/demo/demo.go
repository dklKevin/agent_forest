// Package demo grows a sample forest: twelve invented repositories with
// plausible lives, spanning every species, age, size, and decay stage. It
// emits the same event stream shape a real git adapter will emit.
package demo

import (
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/xnoise"
)

type spec struct {
	name     string
	ageYears float64
	commits  int
	idleDays float64 // days since last activity, relative to now
	tags     int
	mix      map[string]float64
	finished bool
}

// The demo cast. West to east this reads as a life: an ancient finished
// monument, a thriving old-growth town, the ruins of something once large,
// and at the far east a two-month sapling clearing before open dark.
var cast = []spec{
	{"mothgate", 9.2, 7800, 400, 12, map[string]float64{"c": 0.86, "shell": 0.14}, true},
	{"winterwell", 8.5, 6200, 0.2, 14, map[string]float64{"go": 0.81, "shell": 0.12, "html": 0.07}, false},
	{"hollowlamp", 7.1, 4100, 780, 7, map[string]float64{"go": 0.74, "javascript": 0.26}, false},
	{"tidepool", 6.0, 3100, 45, 9, map[string]float64{"python": 0.88, "shell": 0.12}, false},
	{"embermill", 5.2, 380, 320, 2, map[string]float64{"shell": 0.93, "make": 0.07}, false},
	{"lanternfish", 4.1, 2400, 2, 11, map[string]float64{"rust": 0.9, "toml": 0.1}, false},
	{"foxglove", 3.2, 900, 9, 4, map[string]float64{"swift": 0.95, "shell": 0.05}, false},
	{"paperboat", 2.3, 640, 130, 3, map[string]float64{"typescript": 0.83, "css": 0.17}, false},
	{"thornbook", 1.6, 300, 95, 5, map[string]float64{"rust": 0.97, "shell": 0.03}, true},
	{"driftnet", 1.1, 480, 5, 2, map[string]float64{"zig": 0.9, "c": 0.1}, false},
	{"quietmail", 0.6, 210, 1.2, 1, map[string]float64{"typescript": 0.78, "sql": 0.22}, false},
	{"mossjar", 0.17, 60, 0.1, 0, map[string]float64{"lua": 1.0}, false},
}

// Events generates the demo event log at now with a fixed seed.
func Events(seed uint64, now time.Time) []events.Event {
	var evs []events.Event
	for i, s := range cast {
		rs := xnoise.Hash(seed, uint64(i))
		first := now.Add(-time.Duration(s.ageYears * 365.25 * 24 * float64(time.Hour)))
		last := now.Add(-time.Duration(s.idleDays * 24 * float64(time.Hour)))
		if last.Before(first) {
			last = first
		}
		evs = append(evs,
			events.Event{Kind: events.KindRepo, Repo: s.name, TS: first, Path: "~/demo/" + s.name},
			events.Event{Kind: events.KindLangs, Repo: s.name, TS: last, Mix: s.mix},
		)
		evs = append(evs, activity(rs, s.name, first, last, s.commits)...)
		for k := 0; k < s.tags; k++ {
			frac := 0.15 + 0.85*float64(k)/float64(maxi(s.tags-1, 1))
			ts := lerpTime(first, last, frac)
			evs = append(evs, events.Event{
				Kind: events.KindTag, Repo: s.name, TS: ts,
				Name: "v" + itoa(1+k/3) + "." + itoa(k%3) + ".0",
			})
		}
	}
	return evs
}

// activity spreads commits over the repo's life in bursty, human clumps:
// stretches of work, stretches of silence.
func activity(seed uint64, repo string, first, last time.Time, total int) []events.Event {
	span := last.Sub(first)
	days := int(span.Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	// Weight each day by low-frequency noise so effort arrives in waves.
	weights := make([]float64, 0, days)
	sum := 0.0
	for d := 0; d < days; d++ {
		w := xnoise.FBM1(seed, float64(d)*0.09, 3)
		w = w * w * w // sharpen: most days quiet, some days hot
		weights = append(weights, w)
		sum += w
	}
	var evs []events.Event
	remaining := total
	for d := 0; d < days && remaining > 0; d++ {
		share := int(float64(total) * weights[d] / sum)
		if share <= 0 {
			continue
		}
		if share > remaining {
			share = remaining
		}
		remaining -= share
		evs = append(evs, events.Event{
			Kind: events.KindActivity, Repo: repo,
			TS: first.Add(time.Duration(d) * 24 * time.Hour), Commits: share,
		})
	}
	// Anchor endpoints so first/last timestamps are exact.
	evs = append(evs,
		events.Event{Kind: events.KindActivity, Repo: repo, TS: first, Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: repo, TS: last, Commits: maxi(remaining, 1)},
	)
	return evs
}

// FinishedNames reports which demo towns start life marked finished.
func FinishedNames() map[string]bool {
	m := map[string]bool{}
	for _, s := range cast {
		if s.finished {
			m[s.name] = true
		}
	}
	return m
}

func lerpTime(a, b time.Time, t float64) time.Time {
	return a.Add(time.Duration(float64(b.Sub(a)) * t))
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
