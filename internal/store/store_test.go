package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dklKevin/agentforest/internal/events"
)

func TestDirResolution(t *testing.T) {
	t.Setenv("AGENTFOREST_HOME", "/tmp/af-home")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if d, _ := Dir(); d != "/tmp/af-home" {
		t.Fatalf("AGENTFOREST_HOME not honored: %s", d)
	}
	t.Setenv("AGENTFOREST_HOME", "")
	if d, _ := Dir(); d != "/tmp/xdg/agentforest" {
		t.Fatalf("XDG_CONFIG_HOME not honored: %s", d)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if d != filepath.Join(home, ".config", "agentforest") {
		t.Fatalf("default dir wrong: %s", d)
	}
}

func TestSettingsRoundtripAndFirstRun(t *testing.T) {
	dir := t.TempDir()
	s, found, err := LoadSettings(dir)
	if err != nil || found {
		t.Fatalf("missing settings should be a clean first run, got found=%v err=%v", found, err)
	}
	if !s.AddRoot("/x/code") || s.AddRoot("/x/code") {
		t.Fatal("AddRoot dedupe broken")
	}
	s.SetExcluded("/x/code/junk", true)
	s.SetFinished("/x/code/done", true)
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	got, found, err := LoadSettings(dir)
	if err != nil || !found {
		t.Fatalf("reload failed: found=%v err=%v", found, err)
	}
	if len(got.Roots) != 1 || got.Roots[0] != "/x/code" {
		t.Fatalf("roots lost: %v", got.Roots)
	}
	if !got.IsExcluded("/x/code/junk") || got.IsExcluded("/x/code") {
		t.Fatal("excludes lost")
	}
	if !got.IsFinished("/x/code/done") {
		t.Fatal("finished lost")
	}
	if !got.SetFinished("/x/code/done", false) || got.IsFinished("/x/code/done") {
		t.Fatal("unfinish broken")
	}
}

func TestLastOpenedRoundtripAndBackCompat(t *testing.T) {
	dir := t.TempDir()
	// A settings.json from before the stamp existed parses to a zero stamp:
	// the first-run signal, so upgrades never pulse spuriously.
	old := []byte("{\n  \"roots\": [\"/x/code\"]\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	s, found, err := LoadSettings(dir)
	if err != nil || !found {
		t.Fatalf("old settings should load: found=%v err=%v", found, err)
	}
	if !s.LastOpened.IsZero() {
		t.Fatalf("an absent stamp must read as never: %v", s.LastOpened)
	}

	// The stamp round-trips exactly.
	ts := time.Date(2026, 7, 3, 8, 30, 0, 0, time.UTC)
	s.LastOpened = ts
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastOpened.Equal(ts) {
		t.Fatalf("stamp drifted: %v", got.LastOpened)
	}

	// A zero stamp stays out of the file, keeping the schema addition
	// invisible until it is used.
	got.LastOpened = time.Time{}
	if err := SaveSettings(dir, got); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "lastOpened") {
		t.Fatalf("a zero stamp leaked into settings.json:\n%s", b)
	}
}

func TestEventLogAppendLoadAndCorruption(t *testing.T) {
	dir := t.TempDir()
	evs, skipped, err := LoadEvents(dir)
	if err != nil || skipped != 0 || len(evs) != 0 {
		t.Fatalf("missing log should be empty, got %d events err=%v", len(evs), err)
	}
	ts := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	batch1 := []events.Event{
		{Kind: events.KindRepo, Repo: "/x/a", TS: ts, Path: "/x/a", Name: "a"},
		{Kind: events.KindActivity, Repo: "/x/a", TS: ts, Commits: 3},
	}
	if err := AppendEvents(dir, batch1); err != nil {
		t.Fatal(err)
	}
	// A crash mid-append or stray edit must not poison the log.
	f, _ := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_WRONLY|os.O_APPEND, 0o644)
	f.WriteString("{broken json\n\n")
	f.Close()
	if err := AppendEvents(dir, []events.Event{
		{Kind: events.KindActivity, Repo: "/x/a", TS: ts.AddDate(0, 0, 1), Commits: 1},
	}); err != nil {
		t.Fatal(err)
	}
	evs, skipped, err = LoadEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(evs) != 3 {
		t.Fatalf("events = %d, want 3", len(evs))
	}
	if evs[0].Name != "a" || evs[2].Commits != 1 {
		t.Fatalf("event content lost: %+v", evs)
	}
	if !evs[0].TS.Equal(ts) {
		t.Fatalf("timestamp drifted: %v", evs[0].TS)
	}
}
