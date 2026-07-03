// Package model turns derived repo state into towns: species, stature, grove
// density, decay. All the meaning-bearing mappings live here, and none of
// them involve color.
package model

import (
	"math"
	"sort"
	"strings"
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
	Species Species
	// Finished is the town's display state, seeded from the derived repo
	// state (or the demo cast) at build. The UI flips it in place when a
	// ceremony completes so the world need not be rebuilt mid-moment.
	Finished bool
	// IdleOverride, when set, replaces the real idle time. It exists for the
	// almanac so stages can be previewed without waiting real days.
	IdleOverride *time.Duration
	// CompIdleOverride overrides one building's idle time by component path.
	// It exists for the revive animation, so a freshly touched building can
	// ease back to life instead of snapping.
	CompIdleOverride map[string]time.Duration
	// CarveOverride, when set, drives the laying-to-rest ceremony: the
	// monument dressing shown at a partial depth while the transition plays.
	CarveOverride *float64
}

// NewTown derives a town from repo state.
func NewTown(r *events.RepoState, finished bool) *Town {
	return &Town{RepoState: r, Species: SpeciesFor(r.PrimaryLang()), Finished: finished}
}

// Carve is how far along the wood-to-stone transition the town stands:
// 0 living, 1 the full monument, between only while the ceremony plays.
func (t *Town) Carve() float64 {
	if t.CarveOverride != nil {
		return xnoise.Clamp(*t.CarveOverride, 0, 1)
	}
	if t.Finished {
		return 1
	}
	return 0
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
	case t.TotalCommits < 20:
		return 0
	case t.TotalCommits < 250:
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

// BuildingForm is a settlement structure, told apart by silhouette alone.
type BuildingForm int

const (
	FormBarn        BuildingForm = iota // broad gambrel mass: the dominant component
	FormHomeplace                       // a dwelling cabin: large components
	FormWorkshop                        // single-slope working building: middle components
	FormShed                            // small lean-to: minor components
	FormCrib                            // slatted box on stilts: minor components
	FormWatchtower                      // tall braced tower: the tests
	FormSchoolhouse                     // gable with a bell cupola: the docs
)

var formNames = map[BuildingForm]string{
	FormBarn: "barn", FormHomeplace: "cabin", FormWorkshop: "workshop",
	FormShed: "shed", FormCrib: "crib", FormWatchtower: "watchtower",
	FormSchoolhouse: "schoolhouse",
}

func (f BuildingForm) String() string { return formNames[f] }

// Building is one component of the repo as a structure in the settlement.
type Building struct {
	Name   string
	Path   string // component path: the stable identity
	Form   BuildingForm
	Share  float64 // of the largest component's bytes, 0..1
	LastTS time.Time
}

// kindForName maps only the near-certain component names to special forms.
// Wrong guesses are lies, so this set is deliberately small.
func kindForName(name string) (BuildingForm, bool) {
	switch strings.ToLower(name) {
	case "test", "tests", "spec", "specs", "e2e", "__tests__", "testing", "testdata":
		return FormWatchtower, true
	case "doc", "docs", "documentation", "wiki":
		return FormSchoolhouse, true
	}
	return 0, false
}

// Buildings derives the settlement: components sorted by weight, capped at
// the village ceiling, each given a form. Confident kinds take their own
// forms; everything else ranks by size against the largest code component.
func (t *Town) Buildings() []Building {
	if len(t.Components) == 0 {
		return nil
	}
	comps := make([]*events.ComponentState, 0, len(t.Components))
	for _, c := range t.Components {
		comps = append(comps, c)
	}
	sort.Slice(comps, func(i, j int) bool {
		if comps[i].Bytes != comps[j].Bytes {
			return comps[i].Bytes > comps[j].Bytes
		}
		return comps[i].Path < comps[j].Path
	})
	if len(comps) > 12 {
		comps = comps[:12]
	}
	var maxBytes int64
	for _, c := range comps {
		if _, kinded := kindForName(c.Name); !kinded {
			maxBytes = c.Bytes
			break
		}
	}
	bs := make([]Building, 0, len(comps))
	firstCode := true
	for _, c := range comps {
		b := Building{Name: c.Name, Path: c.Path, LastTS: c.LastTS}
		if maxBytes > 0 {
			b.Share = xnoise.Clamp(float64(c.Bytes)/float64(maxBytes), 0, 1)
		}
		if form, ok := kindForName(c.Name); ok {
			b.Form = form
		} else {
			switch {
			case firstCode:
				b.Form, firstCode = FormBarn, false
			case b.Share >= 0.45:
				b.Form = FormHomeplace
			case b.Share >= 0.15:
				b.Form = FormWorkshop
			default:
				b.Form = FormShed
				if xnoise.Hash(hashString(c.Path), 0xC71B)%2 == 0 {
					b.Form = FormCrib
				}
			}
		}
		bs = append(bs, b)
	}
	return bs
}

// BuildingIdle is one building's effective idle time. The almanac's town
// override slides every building forward while preserving each one's own
// offset, so a preview of neglect keeps the village's internal structure.
func (t *Town) BuildingIdle(b Building, now time.Time) time.Duration {
	if d, ok := t.CompIdleOverride[b.Path]; ok {
		return d
	}
	if t.IdleOverride != nil {
		off := t.LastTS.Sub(b.LastTS)
		if off < 0 {
			off = 0
		}
		return *t.IdleOverride + off
	}
	if b.LastTS.IsZero() {
		return 0
	}
	return now.Sub(b.LastTS)
}

// BuildingDecay is reclamation depth for one building at now. Buildings of a
// finished town never decay: the whole settlement is kept.
func (t *Town) BuildingDecay(b Building, now time.Time) float64 {
	if t.Finished {
		return 0
	}
	return DecayAt(t.BuildingIdle(b, now))
}
