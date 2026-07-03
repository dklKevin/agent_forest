// Package almanac folds the append-only event log into a town's memoir: a
// handful of terse, place-flavored lines telling the life of one repository.
// Planted, grown into a settlement, releases staked, long quiets and wakings,
// and - for a finished town - the carved words it was laid to rest with. It
// is the emotional counterweight to decay, read entirely from events the log
// already holds; no event kind exists for its sake.
//
// The almanac is a deliberate-inspection surface. Numbers may appear here,
// sparingly and in prose, never in columns; the map itself stays numberless.
package almanac

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
	"github.com/dklKevin/agentforest/internal/model"
)

// Memoir is one town's life story, in reading order.
type Memoir struct {
	Name     string
	Finished bool
	Epitaph  string   // the carved words; set only while the monument stands
	Brief    string   // finished towns only: "planted 2018, kept 2026"
	Chapters []string // the life, oldest first, ending on the present
}

// silenceMin is the shortest quiet stretch worth telling: a season. Anything
// shorter is a project's ordinary rhythm, not a chapter of its life.
const silenceMin = 90 * 24 * time.Hour

// chapter is one dated line; chapters sort by time so the memoir reads in
// the order the life happened.
type chapter struct {
	ts   time.Time
	text string
}

// Fold builds the memoir of the town keyed by repoKey - the log's Repo
// field, a canonical path for real repos, a bare name for demo towns. The
// reduced summary alone cannot see gaps, so the fold walks the event
// sequence a second time for silences, stakes, and the settlement date. It
// returns nil when the log holds nothing under that key.
func Fold(evs []events.Event, repoKey string, now time.Time) *Memoir {
	var mine []events.Event
	for _, e := range evs {
		if e.Repo == repoKey {
			mine = append(mine, e)
		}
	}
	if len(mine) == 0 {
		return nil
	}
	r := events.Reduce(mine)[0] // one key in, exactly one state out

	sort.SliceStable(mine, func(i, j int) bool { return mine[i].TS.Before(mine[j].TS) })
	var acts []time.Time
	var tags []events.Event
	var settled time.Time // earliest moment the log shows structure standing
	for _, e := range mine {
		switch e.Kind {
		case events.KindActivity:
			acts = append(acts, e.TS)
		case events.KindTag:
			tags = append(tags, e)
		case events.KindComp:
			if settled.IsZero() || e.TS.Before(settled) {
				settled = e.TS
			}
		}
	}

	var chs []chapter
	if !r.FirstTS.IsZero() {
		chs = append(chs, chapter{r.FirstTS, "planted " + MonthYear(r.FirstTS)})
	}
	if !settled.IsZero() {
		chs = append(chs, chapter{settled, "a settlement by " + MonthYear(settled)})
	}
	chs = append(chs, tagChapters(tags)...)
	if c, ok := silenceChapter(acts); ok {
		chs = append(chs, c)
	}
	sort.SliceStable(chs, func(i, j int) bool { return chs[i].ts.Before(chs[j].ts) })

	m := &Memoir{Name: r.Name, Finished: r.Finished}
	if r.Finished {
		// The words show only while the monument stands, exactly as in
		// inspect; an unfinished town keeps them in the log, unread.
		m.Epitaph = r.Epitaph
		m.Brief = brief(r)
	}
	for _, c := range chs {
		m.Chapters = append(m.Chapters, c.text)
	}
	m.Chapters = append(m.Chapters, closing(r, now))
	return m
}

// brief is a finished town's life in one line: "planted 2018, kept 2026".
func brief(r *events.RepoState) string {
	if r.FirstTS.IsZero() {
		return ""
	}
	b := "planted " + r.FirstTS.Format("2006")
	if !r.FinishTS.IsZero() {
		b += ", kept " + r.FinishTS.Format("2006")
	}
	return b
}

// tagChapters tells the releases as survey stakes: the first is a chapter of
// its own, the rest fold into a single line, so a busy release history never
// becomes a changelog.
func tagChapters(tags []events.Event) []chapter {
	if len(tags) == 0 {
		return nil
	}
	first := tags[0]
	chs := []chapter{{first.TS, first.Name + " staked " + MonthYear(first.TS)}}
	last := tags[len(tags)-1]
	switch {
	case len(tags) == 2:
		chs = append(chs, chapter{last.TS, last.Name + " followed, " + MonthYear(last.TS)})
	case len(tags) > 2:
		chs = append(chs, chapter{last.TS, fmt.Sprintf("%s more stakes followed; the last, %s, %s",
			word(len(tags)-1), last.Name, MonthYear(last.TS))})
	}
	return chs
}

// silenceChapter finds the quiet stretches: gaps in the activity sequence of
// at least a season that ended with the town waking. The longest one is the
// story; several become a single line, never a list.
func silenceChapter(acts []time.Time) (chapter, bool) {
	var count int
	var longest time.Duration
	var longestWoke time.Time
	var latestWoke time.Time
	for i := 1; i < len(acts); i++ {
		gap := acts[i].Sub(acts[i-1])
		if gap < silenceMin {
			continue
		}
		count++
		latestWoke = acts[i]
		if gap > longest {
			longest, longestWoke = gap, acts[i]
		}
	}
	switch count {
	case 0:
		return chapter{}, false
	case 1:
		return chapter{latestWoke, fmt.Sprintf("quiet for %s, then woke, %s", span(longest), MonthYear(longestWoke))}, true
	default:
		times := word(count) + " times"
		if count == 2 {
			times = "twice"
		}
		return chapter{latestWoke, fmt.Sprintf("slept and woke %s · the longest quiet ran %s, ending %s",
			times, span(longest), MonthYear(longestWoke))}, true
	}
}

// closing grounds the memoir in the present: the monument standing, the
// grove bright, or the reclamation as it stands today.
func closing(r *events.RepoState, now time.Time) string {
	if r.Finished {
		if r.FinishTS.IsZero() {
			return "laid to rest · the monument stands"
		}
		return "laid to rest " + MonthYear(r.FinishTS) + " · the monument stands"
	}
	var idle time.Duration
	if !r.LastTS.IsZero() {
		idle = now.Sub(r.LastTS)
	}
	stage := model.StageOf(model.DecayAt(idle))
	if stage == model.Tended {
		return "tended still · the grove is bright"
	}
	return fmt.Sprintf("quiet for %s now · %s", span(idle), stageTail(stage))
}

// stageTail matches model.StageLine's imagery, so the almanac and inspect
// always describe the same forest.
func stageTail(s model.Stage) string {
	switch s {
	case model.FirstQuiet:
		return "undergrowth stirs"
	case model.Overgrown:
		return "vines take the trunks"
	case model.Breaking:
		return "the canopy opens"
	case model.Skeletal:
		return "bare boughs stand"
	default:
		return "the forest has taken it back"
	}
}

// MonthYear names a moment the way the forest speaks: "march 2018". Go's
// layout is case-sensitive, so the lowering happens on the output.
func MonthYear(t time.Time) string {
	return strings.ToLower(t.Format("January 2006"))
}

// span tells a duration the way a person would: days, then weeks, then
// months, then years. Digits, but always in prose.
func span(d time.Duration) string {
	days := d.Hours() / 24
	switch {
	case days < 14:
		return counted(int(days), "day")
	case days < 60:
		return counted(int(days/7), "week")
	case days < 700:
		return counted(int(days/30.44+0.5), "month")
	default:
		return counted(int(days/365.25+0.5), "year")
	}
}

// counted is "a week", "3 weeks": the singular reads as prose, not arithmetic.
func counted(n int, unit string) string {
	if n <= 1 {
		return "a " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// word spells a small count; larger ones stay digits.
func word(n int) string {
	small := []string{"", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "eleven", "twelve"}
	if n >= 1 && n < len(small) {
		return small[n]
	}
	return fmt.Sprintf("%d", n)
}
