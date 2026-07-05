// Package demo grows a sample forest: twelve invented repositories with
// plausible lives, spanning every species, age, size, tended mood, and decay
// stage. It emits the same event stream shape a real git adapter will emit.
package demo

import (
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
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
	comps    []comp
}

// comp is one demo component: share is its weight relative to the largest,
// idleDays is its own quiet, independent of the town's.
type comp struct {
	name     string
	share    float64
	files    int
	idleDays float64
}

// The demo cast. West to east this reads as a life: an ancient finished
// village, a thriving old-growth town with one quarter gone quiet, the ruins
// of something once large, and at the far east a lone sapling hut before
// open dark. The idle spread deliberately spans the tended moods too:
// worked-today (winterwell, mossjar), worked-this-week (lanternfish,
// driftnet), and quiet-but-kept (foxglove).
var cast = []spec{
	{"mothgate", 9.2, 7800, 400, 12, map[string]float64{"c": 0.86, "shell": 0.14}, true, []comp{
		{"core", 1.0, 120, 400}, {"gate", 0.55, 60, 400}, {"tools", 0.2, 30, 420},
		{"docs", 0.12, 20, 430}, {"tests", 0.18, 44, 400}, {"scripts", 0.06, 9, 500},
	}},
	{"winterwell", 8.5, 6200, 0.2, 14, map[string]float64{"go": 0.81, "shell": 0.12, "html": 0.07}, false, []comp{
		{"engine", 1.0, 140, 0.2}, {"server", 0.62, 80, 1.5}, {"cli", 0.34, 40, 6},
		{"store", 0.2, 26, 12}, {"docs", 0.1, 18, 320}, {"tests", 0.22, 60, 0.7},
		{"proto", 0.08, 9, 90}, {"scripts", 0.05, 7, 45},
	}},
	{"hollowlamp", 7.1, 4100, 780, 7, map[string]float64{"go": 0.74, "javascript": 0.26}, false, []comp{
		{"lamp", 1.0, 90, 780}, {"web", 0.5, 55, 800}, {"docs", 0.1, 14, 900},
		{"tests", 0.15, 30, 780}, {"assets", 0.24, 40, 860},
	}},
	{"tidepool", 6.0, 3100, 45, 9, map[string]float64{"python": 0.88, "shell": 0.12}, false, []comp{
		{"pool", 1.0, 70, 45}, {"agents", 0.4, 34, 60}, {"docs", 0.09, 12, 200}, {"tests", 0.2, 40, 45},
	}},
	{"embermill", 5.2, 380, 320, 2, map[string]float64{"shell": 0.93, "make": 0.07}, false, []comp{
		{"mill", 1.0, 30, 320}, {"hooks", 0.2, 12, 400},
	}},
	{"lanternfish", 4.1, 2400, 2, 11, map[string]float64{"rust": 0.9, "toml": 0.1}, false, []comp{
		{"fish", 1.0, 80, 2}, {"lure", 0.5, 30, 3}, {"tests", 0.17, 26, 2}, {"docs", 0.08, 10, 30},
	}},
	{"foxglove", 3.2, 900, 12, 4, map[string]float64{"swift": 0.95, "shell": 0.05}, false, []comp{
		{"glove", 1.0, 44, 12}, {"kit", 0.3, 18, 20}, {"tests", 0.12, 14, 12},
	}},
	{"paperboat", 2.3, 640, 130, 3, map[string]float64{"typescript": 0.83, "css": 0.17}, false, []comp{
		{"boat", 1.0, 38, 130}, {"site", 0.35, 22, 150}, {"tests", 0.1, 12, 170},
	}},
	{"thornbook", 1.6, 300, 95, 5, map[string]float64{"rust": 0.97, "shell": 0.03}, true, []comp{
		{"book", 1.0, 40, 95}, {"tests", 0.2, 20, 95}, {"docs", 0.1, 9, 110},
	}},
	{"driftnet", 1.1, 480, 5, 2, map[string]float64{"zig": 0.9, "c": 0.1}, false, []comp{
		{"net", 1.0, 30, 5}, {"knots", 0.25, 12, 8},
	}},
	{"quietmail", 0.6, 210, 1.2, 1, map[string]float64{"typescript": 0.78, "sql": 0.22}, false, []comp{
		{"mail", 1.0, 26, 1.2}, {"docs", 0.1, 6, 40},
	}},
	{"mossjar", 0.17, 60, 0.1, 0, map[string]float64{"lua": 1.0}, false, nil},
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
		for _, c := range s.comps {
			touched := now.Add(-time.Duration(c.idleDays * 24 * float64(time.Hour)))
			if touched.Before(first) {
				touched = first
			}
			evs = append(evs, events.Event{
				Kind: events.KindComp, Repo: s.name, TS: touched,
				Name: c.name, Path: c.name,
				Bytes: int64(c.share * 800 * 1024), Files: c.files,
			})
		}
		for k := 0; k < s.tags; k++ {
			frac := 0.15 + 0.85*float64(k)/float64(maxi(s.tags-1, 1))
			ts := lerpTime(first, last, frac)
			evs = append(evs, events.Event{
				Kind: events.KindTag, Repo: s.name, TS: ts,
				Name: "v" + itoa(1+k/3) + "." + itoa(k%3) + ".0",
			})
		}
		if s.finished {
			evs = append(evs, events.Event{Kind: events.KindFinish, Repo: s.name, TS: last})
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

// Occupancies is the demo cast's current working states: winterwell, the
// town worked today, keeps a camp by the path so the mark is discoverable in
// the demo forest. Display state only - the demo event log stays exactly as
// it was, the same rule real occupancy lives by.
func Occupancies() map[string]model.Occupancy {
	return map[string]model.Occupancy{
		"winterwell": {Dirty: true, Branch: "thaw"},
	}
}

// Towns builds the demo cast as towns, the way the CLI opens the demo
// forest: the event log reduced, finish state applied, and the cast's
// display-only occupancy attached.
func Towns(seed uint64, now time.Time) ([]*model.Town, []events.Event) {
	evs := Events(seed, now)
	occ := Occupancies()
	var towns []*model.Town
	for _, r := range events.Reduce(evs) {
		t := model.NewTown(r, r.Finished)
		if o, ok := occ[r.Name]; ok && !r.Finished {
			t.Occupancy = o
		}
		towns = append(towns, t)
	}
	return towns, evs
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
