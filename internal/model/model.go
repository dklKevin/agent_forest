// Package model turns derived repo state into towns: species, stature, grove
// density, decay. All the meaning-bearing mappings live here, and none of
// them involve color.
package model

import (
	"math"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/xnoise"
)

// Species is a tree form, distinguishable by silhouette alone.
type Species int

const (
	Oak     Species = iota // round billowing crown, stout trunk
	Spruce                 // tiered triangular conifer
	Willow                 // weeping curtain of strands
	Poplar                 // tall narrow column
	Flattop                // massive trunk, broad flat crown
	Scrub                  // low ground-hugging bushes
	Birch                  // slender bare trunk, small high crown
	Grove                  // irregular mixed-blob hardwood (fallback)
	Wild                   // filler scrub between towns; not a language
)

var speciesNames = map[Species]string{
	Oak: "oak", Spruce: "spruce", Willow: "willow", Poplar: "poplar",
	Flattop: "flattop", Scrub: "juniper scrub", Birch: "birch", Grove: "mixed grove",
	Wild: "wild scrub",
}

func (s Species) String() string { return speciesNames[s] }

// speciesByLang maps well-known primary languages to species. Anything else
// hashes deterministically into one of the eight forms, so unknown languages
// still get a stable, distinct-enough tree.
var speciesByLang = map[string]Species{
	"go":         Oak,
	"rust":       Spruce,
	"python":     Willow,
	"typescript": Poplar,
	"javascript": Poplar,
	"c":          Flattop,
	"c++":        Flattop,
	"shell":      Scrub,
	"swift":      Birch,
}

// SpeciesFor resolves a primary language to a tree species.
func SpeciesFor(lang string) Species {
	if s, ok := speciesByLang[lang]; ok {
		return s
	}
	h := xnoise.Hash(0x5eed, hashString(lang))
	return Species(h % 8)
}

func hashString(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// Stage names the visible depth of reclamation.
type Stage int

const (
	Tended Stage = iota
	FirstQuiet
	Overgrown
	Breaking
	Skeletal
	Ruin
)

var stageNames = map[Stage]string{
	Tended:     "tended",
	FirstQuiet: "the first quiet",
	Overgrown:  "overgrown",
	Breaking:   "breaking",
	Skeletal:   "skeletal",
	Ruin:       "ruins",
}

func (s Stage) String() string { return stageNames[s] }

// StageLine is the poetic description shown in inspect.
func StageLine(s Stage, finished bool) string {
	if finished {
		return "finished · stands as a monument"
	}
	switch s {
	case Tended:
		return "tended · the grove is bright"
	case FirstQuiet:
		return "the first quiet · undergrowth stirs"
	case Overgrown:
		return "overgrown · vines take the trunks"
	case Breaking:
		return "breaking · the canopy opens"
	case Skeletal:
		return "skeletal · bare boughs stand"
	default:
		return "ruins · the forest has taken it back"
	}
}

// Decay tuning. Reclamation waits out a one-day grace, then deepens along an
// exponential approach: roughly 1% per day at first, overgrown within a
// season, ruins after about two years untouched.
const (
	graceDays = 1.0
	tauDays   = 110.0
	decayCap  = 0.98
)

// DecayAt maps idle time to reclamation depth in [0, decayCap].
func DecayAt(idle time.Duration) float64 {
	days := idle.Hours() / 24
	if days <= graceDays {
		return 0
	}
	d := 1 - math.Exp(-(days-graceDays)/tauDays)
	return xnoise.Clamp(d, 0, decayCap)
}

// StageOf quantizes decay depth into a named stage.
func StageOf(d float64) Stage {
	switch {
	case d < 0.05:
		return Tended
	case d < 0.25:
		return FirstQuiet
	case d < 0.50:
		return Overgrown
	case d < 0.75:
		return Breaking
	case d < 0.93:
		return Skeletal
	default:
		return Ruin
	}
}

// IdleForDecay inverts DecayAt: the idle duration that produces depth d.
// Used by the almanac's stage jumps.
func IdleForDecay(d float64) time.Duration {
	d = xnoise.Clamp(d, 0, decayCap)
	if d <= 0 {
		return 0
	}
	days := graceDays - tauDays*math.Log(1-d)
	return time.Duration(days * 24 * float64(time.Hour))
}

// Town is one repository as a place in the forest.
type Town struct {
	*events.RepoState
	Species  Species
	Finished bool
	// IdleOverride, when set, replaces the real idle time. It exists for the
	// almanac so stages can be previewed without waiting real days.
	IdleOverride *time.Duration
}

// NewTown derives a town from repo state.
func NewTown(r *events.RepoState, finished bool) *Town {
	return &Town{RepoState: r, Species: SpeciesFor(r.PrimaryLang()), Finished: finished}
}

// Idle returns the effective idle duration at now.
func (t *Town) Idle(now time.Time) time.Duration {
	if t.IdleOverride != nil {
		return *t.IdleOverride
	}
	if t.LastTS.IsZero() {
		return 0
	}
	return now.Sub(t.LastTS)
}

// Decay returns reclamation depth at now. Finished towns never decay.
func (t *Town) Decay(now time.Time) float64 {
	if t.Finished {
		return 0
	}
	return DecayAt(t.Idle(now))
}

// AgeYears is the town's age in years at now.
func (t *Town) AgeYears(now time.Time) float64 {
	if t.FirstTS.IsZero() {
		return 0
	}
	return now.Sub(t.FirstTS).Hours() / (24 * 365.25)
}

// Stature maps age to canopy height in dots (designed for a 140-dot-tall
// reference viewport; the renderer scales). Growth is fast when young and
// slow in age, like trees.
func (t *Town) Stature(now time.Time) float64 {
	years := t.AgeYears(now)
	g := math.Log1p(years*1.6) / math.Log1p(9*1.6)
	return 22 + 74*xnoise.Clamp(g, 0, 1)
}

// HearthTier maps commit volume to homestead size: a hut, a cabin, or a
// full homestead. Like everything meaning-bearing, it is shape, not color.
func (t *Town) HearthTier() int {
	switch {
	case t.TotalCommits < 200:
		return 0
	case t.TotalCommits < 2500:
		return 1
	default:
		return 2
	}
}

// TreeCount maps commit volume to grove size.
func (t *Town) TreeCount() int {
	n := 2 + 2.6*math.Log10(float64(t.TotalCommits)+1)
	if n < 2 {
		n = 2
	}
	if n > 13 {
		n = 13
	}
	return int(n)
}
