// Package state records what pegboard wrote to each target (SPEC §3.4): it is
// what lets pegboard avoid overwriting unmanaged content and detect drift.
// It never contains secrets: only hashes computed with secrets masked.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Origin says why a resource exists: declared in pegboard.yaml or installed
// with `pegboard install`.
type Origin string

const (
	Declared Origin = "declared"
	Adhoc    Origin = "adhoc"
)

// Entry is an MCP that pegboard wrote to a file.
type Entry struct {
	File   string   `json:"file"`
	Hash   string   `json:"hash"`
	Masked []string `json:"masked,omitempty"`
	Origin Origin   `json:"origin"`
}

// SkillEntry is a skill that pegboard placed in a skills directory: a
// symlink to the cache (or a copy, where symlinks are unavailable).
type SkillEntry struct {
	Source string `json:"source"`
	Hash   string `json:"hash"`
	// Link is where the symlink points (the cache, or the local directory).
	Link string `json:"link"`
	// Copy: it was copied instead of linked.
	Copy   bool   `json:"copy,omitempty"`
	Origin Origin `json:"origin"`
}

// ContextEntry records what pegboard added to a context file (the
// @AGENTS.md bridge in CLAUDE.md) so it can be removed later.
type ContextEntry struct {
	Line string `json:"line"`
}

// State for a scope. Targets: target ID ("hermes:work") → name → MCP entry.
// Skills: skills directory → name → entry (one directory may serve several
// agents). Context: file → what was added.
type State struct {
	Version int                              `json:"version"`
	Targets map[string]map[string]Entry      `json:"targets"`
	Skills  map[string]map[string]SkillEntry `json:"skills,omitempty"`
	Context map[string]ContextEntry          `json:"context,omitempty"`

	path string
}

// Skill returns the entry for a skill in a directory.
func (s *State) Skill(dir, name string) (SkillEntry, bool) {
	e, ok := s.Skills[dir][name]
	return e, ok
}

func (s *State) SetSkill(dir, name string, e SkillEntry) {
	if s.Skills == nil {
		s.Skills = map[string]map[string]SkillEntry{}
	}
	if s.Skills[dir] == nil {
		s.Skills[dir] = map[string]SkillEntry{}
	}
	s.Skills[dir][name] = e
}

func (s *State) DeleteSkill(dir, name string) {
	delete(s.Skills[dir], name)
	if len(s.Skills[dir]) == 0 {
		delete(s.Skills, dir)
	}
}

// SkillNames lists the skills registered in a directory, sorted.
func (s *State) SkillNames(dir string) []string {
	var out []string
	for n := range s.Skills[dir] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SkillDirs lists the directories with registered skills, sorted.
func (s *State) SkillDirs() []string {
	var out []string
	for d := range s.Skills {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Load reads the state; if the file does not exist it returns an empty one.
func Load(path string) (*State, error) {
	s := &State{Version: 1, Targets: map[string]map[string]Entry{}, path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported state version %d", path, s.Version)
	}
	if s.Targets == nil {
		s.Targets = map[string]map[string]Entry{}
	}
	s.path = path
	return s, nil
}

func (s *State) Path() string { return s.path }

// Get returns the entry for an MCP in a target.
func (s *State) Get(target, name string) (Entry, bool) {
	e, ok := s.Targets[target][name]
	return e, ok
}

func (s *State) Set(target, name string, e Entry) {
	if s.Targets[target] == nil {
		s.Targets[target] = map[string]Entry{}
	}
	s.Targets[target][name] = e
}

func (s *State) Delete(target, name string) {
	delete(s.Targets[target], name)
	if len(s.Targets[target]) == 0 {
		delete(s.Targets, target)
	}
}

// Names lists the MCPs registered in a target, sorted.
func (s *State) Names(target string) []string {
	var out []string
	for n := range s.Targets[target] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TargetIDs lists the targets with something registered, sorted.
func (s *State) TargetIDs() []string {
	var out []string
	for t := range s.Targets {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Save writes the state atomically. In a project, the .pegboard/ directory
// gets its own .gitignore: the state belongs to each machine.
func (s *State) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if filepath.Base(dir) == ".pegboard" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		gi := filepath.Join(dir, ".gitignore")
		if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(gi, []byte("*\n"), 0o644); err != nil {
				return err
			}
		}
	}
	return WriteAtomic(s.path, append(b, '\n'), 0o644)
}
