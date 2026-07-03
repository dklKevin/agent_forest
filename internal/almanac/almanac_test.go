package almanac

import (
	"strings"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
)

var planted = time.Date(2018, 3, 10, 12, 0, 0, 0, time.UTC)

func day(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC) }

// town builds a minimal real-repo event set: announced and planted, with
// extra life appended by each test.
func town(extra ...events.Event) []events.Event {
	evs := []events.Event{
		{Kind: events.KindRepo, Repo: "/x/mothgate", TS: planted, Path: "/x/mothgate", Name: "mothgate"},
		{Kind: events.KindActivity, Repo: "/x/mothgate", TS: planted, Commits: 3},
	}
	return append(evs, extra...)
}

func foldOne(t *testing.T, evs []events.Event, now time.Time) *Memoir {
	t.Helper()
	m := Fold(evs, "/x/mothgate", now)
	if m == nil {
		t.Fatal("fold returned nil for a town the log knows")
	}
	return m
}

func wantChapters(t *testing.T, m *Memoir, want []string) {
	t.Helper()
	if len(m.Chapters) != len(want) {
		t.Fatalf("chapters = %q, want %q", m.Chapters, want)
	}
	for i := range want {
		if m.Chapters[i] != want[i] {
			t.Fatalf("chapter %d = %q, want %q", i, m.Chapters[i], want[i])
		}
	}
}

func TestFoldUnknownKeyReturnsNil(t *testing.T) {
	if m := Fold(town(), "/x/elsewhere", planted); m != nil {
		t.Fatalf("unknown key should fold to nil, got %+v", m)
	}
}

// The barest life: planted, still tended. No tags, no settlement, no
// silences - the memoir is two honest lines, not an empty stats table.
func TestFoldPlantedAndTended(t *testing.T) {
	m := foldOne(t, town(), planted.Add(24*time.Hour))
	if m.Finished || m.Epitaph != "" || m.Brief != "" {
		t.Fatalf("living town carries finish state: %+v", m)
	}
	if m.Name != "mothgate" {
		t.Fatalf("name = %q", m.Name)
	}
	wantChapters(t, m, []string{
		"planted march 2018",
		"tended still · the grove is bright",
	})
}

// An idle town closes on the present reclamation, told in prose.
func TestFoldClosingTellsTheQuiet(t *testing.T) {
	m := foldOne(t, town(), planted.Add(200*24*time.Hour))
	want := "quiet for 7 months now · bare boughs stand"
	if got := m.Chapters[len(m.Chapters)-1]; got != want {
		t.Fatalf("closing = %q, want %q", got, want)
	}
}

// One long sleep is the story: the exact "quiet for 14 months, then woke"
// line, dated by the waking.
func TestFoldLongestSilence(t *testing.T) {
	sleep := day(2018, 5, 1)                // 52 days after planting: rhythm
	wake := sleep.Add(426 * 24 * time.Hour) // july 2019
	m := foldOne(t, town(
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: sleep, Commits: 2},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: wake, Commits: 1},
	), wake.Add(time.Hour))
	wantChapters(t, m, []string{
		"planted march 2018",
		"quiet for 14 months, then woke, july 2019",
		"tended still · the grove is bright",
	})
}

// Several sleeps fold into one line, never a list; ordinary weekend-sized
// gaps are rhythm, not chapters.
func TestFoldRevivalsFoldIntoOneLine(t *testing.T) {
	a := day(2018, 5, 1)             // 52 days after planting: rhythm
	b := a.Add(426 * 24 * time.Hour) // the long sleep, waking july 2019
	c := b.Add(5 * 24 * time.Hour)   // rhythm, not a silence
	d := c.Add(120 * 24 * time.Hour) // a second, shorter sleep
	m := foldOne(t, town(
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: a, Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: b, Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: c, Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: d, Commits: 1},
	), d.Add(time.Hour))
	want := "slept and woke twice · the longest quiet ran 14 months, ending july 2019"
	if m.Chapters[1] != want {
		t.Fatalf("silence line = %q, want %q", m.Chapters[1], want)
	}
}

func TestFoldRevivalsSortAtLatestWake(t *testing.T) {
	a := day(2018, 5, 1)
	b := a.Add(426 * 24 * time.Hour)
	c := b.Add(5 * 24 * time.Hour)
	d := c.Add(120 * 24 * time.Hour)
	m := foldOne(t, town(
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: a, Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: b, Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: c, Commits: 1},
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2019, 9, 1), Name: "v1.0"},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: d, Commits: 1},
	), d.Add(time.Hour))
	wantChapters(t, m, []string{
		"planted march 2018",
		"v1.0 staked september 2019",
		"slept and woke twice · the longest quiet ran 14 months, ending july 2019",
		"tended still · the grove is bright",
	})
}

// Stakes: one tag is a chapter; a crowd of tags folds to first and last.
func TestFoldTagChapters(t *testing.T) {
	one := foldOne(t, town(
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2019, 10, 2), Name: "v1.0"},
	), day(2019, 10, 3))
	if one.Chapters[1] != "v1.0 staked october 2019" {
		t.Fatalf("single stake = %q", one.Chapters[1])
	}

	two := foldOne(t, town(
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2019, 10, 2), Name: "v1.0"},
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2020, 6, 20), Name: "v1.1"},
	), day(2020, 6, 21))
	if two.Chapters[2] != "v1.1 followed, june 2020" {
		t.Fatalf("second stake = %q", two.Chapters[2])
	}

	many := foldOne(t, town(
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2019, 10, 2), Name: "v1.0"},
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2020, 2, 1), Name: "v1.1"},
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2020, 9, 9), Name: "v1.2"},
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2021, 5, 30), Name: "v2.0"},
	), day(2021, 6, 1))
	want := "three more stakes followed; the last, v2.0, may 2021"
	if many.Chapters[2] != want {
		t.Fatalf("folded stakes = %q, want %q", many.Chapters[2], want)
	}
}

// The settlement chapter dates from the earliest structure the log ever saw:
// an honest "by then", never an invented founding day.
func TestFoldSettlementByEarliestComponent(t *testing.T) {
	m := foldOne(t, town(
		events.Event{Kind: events.KindComp, Repo: "/x/mothgate", TS: day(2020, 1, 15), Path: "engine", Name: "engine", Bytes: 100, Files: 5},
		events.Event{Kind: events.KindComp, Repo: "/x/mothgate", TS: day(2019, 6, 20), Path: "docs", Name: "docs", Bytes: 50, Files: 4},
	), day(2020, 1, 16))
	if m.Chapters[1] != "a settlement by june 2019" {
		t.Fatalf("settlement = %q", m.Chapters[1])
	}
}

// A finished town: the carved words and the life-in-brief lead, and the
// memoir closes on the monument.
func TestFoldFinishedLeadsWithEpitaph(t *testing.T) {
	m := foldOne(t, town(
		events.Event{Kind: events.KindFinish, Repo: "/x/mothgate", TS: day(2026, 6, 15), Epitaph: "shipped the thing. slept better."},
	), day(2026, 7, 1))
	if !m.Finished {
		t.Fatal("finish event did not fold")
	}
	if m.Epitaph != "shipped the thing. slept better." {
		t.Fatalf("epitaph = %q", m.Epitaph)
	}
	if m.Brief != "planted 2018, kept 2026" {
		t.Fatalf("brief = %q", m.Brief)
	}
	if got := m.Chapters[len(m.Chapters)-1]; got != "laid to rest june 2026 · the monument stands" {
		t.Fatalf("closing = %q", got)
	}
}

// Unfinishing keeps the words in the log but the memoir, like inspect, reads
// them only while the monument stands.
func TestFoldUnfinishedHidesEpitaph(t *testing.T) {
	m := foldOne(t, town(
		events.Event{Kind: events.KindFinish, Repo: "/x/mothgate", TS: day(2024, 6, 15), Epitaph: "kept words"},
		events.Event{Kind: events.KindUnfinish, Repo: "/x/mothgate", TS: day(2025, 1, 1)},
	), day(2025, 1, 2))
	if m.Finished || m.Epitaph != "" || m.Brief != "" {
		t.Fatalf("unfinished town shows monument state: %+v", m)
	}
	if got := m.Chapters[len(m.Chapters)-1]; strings.Contains(got, "laid to rest") {
		t.Fatalf("unfinished town closes on the monument: %q", got)
	}
}

// The memoir reads in the order the life happened, whatever order the log
// arrived in: here the long sleep predates the settlement and the stake.
func TestFoldChaptersAreChronological(t *testing.T) {
	m := foldOne(t, town(
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: day(2019, 10, 5), Commits: 1},
		events.Event{Kind: events.KindTag, Repo: "/x/mothgate", TS: day(2019, 10, 2), Name: "v1.0"},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: day(2018, 6, 1), Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: day(2019, 1, 1), Commits: 1}, // wakes a 7-month quiet
		events.Event{Kind: events.KindComp, Repo: "/x/mothgate", TS: day(2019, 6, 20), Path: "engine", Name: "engine", Bytes: 100, Files: 5},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: day(2019, 3, 1), Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: day(2019, 5, 15), Commits: 1},
		events.Event{Kind: events.KindActivity, Repo: "/x/mothgate", TS: day(2019, 7, 20), Commits: 1},
	), day(2019, 10, 5).Add(time.Hour))
	wantChapters(t, m, []string{
		"planted march 2018",
		"quiet for 7 months, then woke, january 2019",
		"a settlement by june 2019",
		"v1.0 staked october 2019",
		"tended still · the grove is bright",
	})
}

func TestSpanSpeaksLikeAPerson(t *testing.T) {
	dayD := 24 * time.Hour
	cases := []struct {
		d    time.Duration
		want string
	}{
		{20 * time.Hour, "a day"},
		{8 * dayD, "8 days"},
		{21 * dayD, "3 weeks"},
		{65 * dayD, "2 months"},
		{426 * dayD, "14 months"},
		{800 * dayD, "2 years"},
	}
	for _, c := range cases {
		if got := span(c.d); got != c.want {
			t.Errorf("span(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
