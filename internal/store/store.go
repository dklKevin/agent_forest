// Package store persists the forest between runs: connected roots, per-town
// choices, and the append-only event log. Everything is a plain file a human
// can open, read, and hand-edit.
//
//	settings.json  - connected roots, excluded repos, finished repos
//	events.jsonl   - append-only event log, one JSON event per line
//
// The event log is the source of truth for world history: repos that vanish
// from disk keep their towns (ruins never disappear), and a future
// time-machine replays the same log with an earlier cutoff.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dklKevin/agentforest/internal/events"
)

const (
	settingsFile = "settings.json"
	eventsFile   = "events.jsonl"
)

// Dir resolves the storage directory without creating it:
// $AGENTFOREST_HOME, else $XDG_CONFIG_HOME/agentforest, else
// ~/.config/agentforest.
func Dir() (string, error) {
	if d := os.Getenv("AGENTFOREST_HOME"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "agentforest"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "agentforest"), nil
}

// Settings is everything the user has chosen: which roots to scan, which
// repos to hide, which towns stand finished. Repos are keyed by their
// canonical absolute path, so renames read as new towns but moves of the
// whole root do not silently duplicate anything a human could not fix by
// editing this file.
type Settings struct {
	Roots    []string `json:"roots"`
	Excludes []string `json:"excludes,omitempty"`
	Finished []string `json:"finished,omitempty"`
}

// LoadSettings reads settings.json. A missing file returns empty settings
// and found=false: the signal for first-run onboarding.
func LoadSettings(dir string) (s *Settings, found bool, err error) {
	s = &Settings{}
	b, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return s, false, nil
	}
	if err != nil {
		return s, false, fmt.Errorf("read settings: %w", err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return s, false, fmt.Errorf("parse settings: %w", err)
	}
	return s, true, nil
}

// SaveSettings writes settings.json atomically (temp file + rename).
func SaveSettings(dir string, s *Settings) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return atomicWrite(filepath.Join(dir, settingsFile), append(b, '\n'))
}

// AddRoot records a root directory, deduped and sorted. Reports whether the
// root was new.
func (s *Settings) AddRoot(root string) bool { return addTo(&s.Roots, root) }

// IsExcluded reports whether a repo path is hidden from the forest.
func (s *Settings) IsExcluded(path string) bool { return contains(s.Excludes, path) }

// SetExcluded hides or restores a repo. Reports whether anything changed.
func (s *Settings) SetExcluded(path string, excluded bool) bool {
	if excluded {
		return addTo(&s.Excludes, path)
	}
	return removeFrom(&s.Excludes, path)
}

// IsFinished reports whether a repo stands as a monument.
func (s *Settings) IsFinished(path string) bool { return contains(s.Finished, path) }

// SetFinished freezes or unfreezes a repo. Reports whether anything changed.
func (s *Settings) SetFinished(path string, finished bool) bool {
	if finished {
		return addTo(&s.Finished, path)
	}
	return removeFrom(&s.Finished, path)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func addTo(list *[]string, v string) bool {
	if contains(*list, v) {
		return false
	}
	*list = append(*list, v)
	sort.Strings(*list)
	return true
}

func removeFrom(list *[]string, v string) bool {
	for i, x := range *list {
		if x == v {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return true
		}
	}
	return false
}

// AppendEvents appends events to events.jsonl, one JSON object per line.
// The log is append-only: nothing here ever rewrites or truncates it.
func AppendEvents(dir string, evs []events.Event) error {
	if len(evs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, eventsFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, e := range evs {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("append event: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush event log: %w", err)
	}
	return f.Close()
}

// LoadEvents reads the whole event log. A missing file is an empty log.
// Unparseable lines (a crash mid-append, a stray edit) are skipped and
// counted rather than poisoning the world.
func LoadEvents(dir string) (evs []events.Event, skipped int, err error) {
	f, err := os.Open(filepath.Join(dir, eventsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e events.Event
		if json.Unmarshal(line, &e) != nil || e.Kind == "" {
			skipped++
			continue
		}
		evs = append(evs, e)
	}
	if err := sc.Err(); err != nil {
		return evs, skipped, fmt.Errorf("read event log: %w", err)
	}
	return evs, skipped, nil
}

func atomicWrite(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("stage write: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("stage write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("stage write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("commit write: %w", err)
	}
	return nil
}
