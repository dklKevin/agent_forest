// Occupancy is the one read that is deliberately not an event: the current
// working state of a repository, taken fresh at scan time and never written
// anywhere. A dirty tree, a checked-out non-default branch, or extra
// worktrees mean someone is there right now; the moment the work lands or is
// put away, the next read comes back empty. Presence, not history.
package gitscan

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Occupancy is the current working state of one repository.
type Occupancy struct {
	Dirty     bool   // uncommitted changes: tracked edits or untracked files
	Branch    string // checked-out non-default branch; empty otherwise
	Worktrees int    // linked worktrees beyond the main one
}

// Occupied reports whether any signal shows work under way.
func (o Occupancy) Occupied() bool {
	return o.Dirty || o.Branch != "" || o.Worktrees > 0
}

// ReadOccupancy takes the working-state read of one repository. Every check
// is bounded, and any git failure simply reports that signal absent: a repo
// that errors shows no camp, and never breaks the scan.
func ReadOccupancy(path string) Occupancy {
	var o Occupancy
	o.Dirty = dirtyTree(path) || hasUntracked(path)
	o.Branch = nonDefaultBranch(path)
	o.Worktrees = extraWorktrees(path)
	return o
}

// dirtyTree reports whether tracked files differ from HEAD, staged or not.
// diff --quiet prints nothing and stops at the first difference, so the
// check stays cheap even on huge repos; an unborn HEAD or any other error
// reads as clean.
func dirtyTree(path string) bool {
	return gitExitCode(path, "diff", "--quiet", "HEAD", "--") == 1
}

// hasUntracked reports whether any untracked file exists. ls-files streams
// its findings, so the process is killed at the first byte of output: the
// cost is one entry, never a listing of a huge untracked tree. Untracked
// directories are folded to one entry so git's own walk stays short too.
func hasUntracked(path string) bool {
	cmd := exec.Command("git", "-C", path, "--no-optional-locks",
		"ls-files", "--others", "--exclude-standard", "--directory", "--no-empty-directory", "-z")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	timer := time.AfterFunc(gitTimeout, func() { cmd.Process.Kill() })
	b, readErr := bufio.NewReader(out).ReadByte()
	timer.Stop()
	cmd.Process.Kill()
	cmd.Wait()
	return readErr == nil && b != 0
}

// nonDefaultBranch returns the checked-out branch when it is confidently not
// the repo's default. A detached HEAD, an unknowable default, or any error
// return "": the signal must never guess.
func nonDefaultBranch(path string) string {
	out, err := runGit(path, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "" // detached HEAD, or not a repo
	}
	branch := strings.TrimSpace(out)
	if branch == "" {
		return ""
	}
	def := defaultBranch(path)
	if def == "" || branch == def {
		return ""
	}
	return branch
}

// defaultBranch resolves the repo's default branch from local state alone:
// the origin HEAD pointer when one exists, else a local main or master.
// Anything else returns "" (unknown), which reads as no branch signal.
func defaultBranch(path string) string {
	if out, err := runGit(path, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil {
		if _, name, ok := strings.Cut(strings.TrimSpace(out), "/"); ok && name != "" {
			return name
		}
	}
	for _, cand := range []string{"main", "master"} {
		if gitExitCode(path, "show-ref", "--verify", "--quiet", "refs/heads/"+cand) == 0 {
			return cand
		}
	}
	return ""
}

// extraWorktrees counts linked worktrees beyond the main one: parallel work
// pitched in other clearings.
func extraWorktrees(path string) int {
	out, err := runGit(path, "worktree", "list", "--porcelain")
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			n++
		}
	}
	if n <= 1 {
		return 0
	}
	return n - 1
}

// gitExitCode runs git for its exit code alone: 0, the documented non-zero
// codes of --quiet-style checks, or -1 for anything that failed to run.
func gitExitCode(dir string, args ...string) int {
	full := append([]string{"-C", dir, "--no-optional-locks"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	timer := time.AfterFunc(gitTimeout, func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	err := cmd.Run()
	timer.Stop()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() > 0 {
		return ee.ExitCode()
	}
	return -1
}
