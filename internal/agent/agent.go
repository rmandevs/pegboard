// Package agent defines the types shared by all providers: scopes, targets,
// the environment where they are detected, and the Provider interface.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scope is the scope of a stack: the user (global) or a directory (project).
type Scope string

const (
	Global  Scope = "global"
	Project Scope = "project"
)

// ParseScope converts the --scope value. Empty means "all".
func ParseScope(s string) ([]Scope, bool) {
	switch s {
	case "":
		return []Scope{Global, Project}, true
	case string(Global):
		return []Scope{Global}, true
	case string(Project):
		return []Scope{Project}, true
	}
	return nil, false
}

// Paths are the native paths where a target stores each kind of resource.
// An empty path means the agent does not support that resource in the scope.
type Paths struct {
	MCPs    string `json:"mcps,omitempty"`
	Skills  string `json:"skills,omitempty"`
	Context string `json:"context,omitempty"`
}

// Target is an agent in a specific scope (for example, Claude Code × project).
type Target struct {
	Agent    string `json:"agent"`
	Profile  string `json:"profile,omitempty"`
	Scope    Scope  `json:"scope"`
	Provider int    `json:"provider_version"`
	Binary   string `json:"binary,omitempty"`
	Version  string `json:"version,omitempty"`
	// Evidence lists what was found to consider the target detected.
	Evidence []string `json:"evidence"`
	Paths    Paths    `json:"paths"`
}

// ID identifies the target within its scope: "claude-code", "hermes:work".
func (t Target) ID() string {
	if t.Profile != "" {
		return t.Agent + ":" + t.Profile
	}
	return t.Agent
}

// Provider is an agent adapter. It knows how to detect its targets; the
// engine handles reading, planning, and applying changes.
type Provider interface {
	ID() string
	// Version is the provider version, not the agent version: it increases
	// when the paths or formats it understands change.
	Version() int
	// Detect returns the agent's targets in the scope; none if the agent is
	// not installed or does not apply. There may be several (Hermes
	// profiles).
	Detect(env Env, scope Scope) []Target
}

// Env is everything a provider queries from the machine. Tests build it on
// top of a t.TempDir().
type Env struct {
	Home       string
	ProjectDir string
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	// BinVersion returns the version of a binary, or "" if unknown.
	BinVersion func(path string) string
	LookupEnv  func(string) (string, bool)
	Now        func() time.Time
}

// DefaultEnv is the process's real environment.
func DefaultEnv(projectDir string) (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return Env{}, err
	}
	return Env{
		Home:       home,
		ProjectDir: abs,
		Getenv:     os.Getenv,
		LookPath:   lookPath,
		BinVersion: memo(binVersion),
		LookupEnv:  os.LookupEnv,
		Now:        time.Now,
	}, nil
}

// NewTarget builds a detected target and queries the version if a binary is
// available.
func (e Env) NewTarget(p Provider, scope Scope, bin string, evidence []string, paths Paths) Target {
	t := Target{
		Agent:    p.ID(),
		Scope:    scope,
		Provider: p.Version(),
		Binary:   bin,
		Evidence: evidence,
		Paths:    paths,
	}
	if t.Evidence == nil {
		t.Evidence = []string{}
	}
	if bin != "" && e.BinVersion != nil {
		t.Version = e.BinVersion(bin)
	}
	return t
}

// EnvDir returns $name if set, or home/def.
func (e Env) EnvDir(name, def string) string {
	if v := e.Getenv(name); v != "" {
		return v
	}
	return filepath.Join(e.Home, def)
}

// XDGConfig is $XDG_CONFIG_HOME or ~/.config.
func (e Env) XDGConfig() string {
	return e.EnvDir("XDG_CONFIG_HOME", ".config")
}

// Pretty shortens a path for display: relative to the project or with ~.
// Shortened paths use / on every platform so the output is the same on
// Windows.
func (e Env) Pretty(p string) string {
	if p == "" {
		return ""
	}
	// From home, ~/… is clearer than a relative path.
	if e.ProjectDir != "" && filepath.Clean(e.ProjectDir) != filepath.Clean(e.Home) {
		if rel, ok := under(e.ProjectDir, p); ok {
			return filepath.ToSlash(rel)
		}
	}
	if rel, ok := under(e.Home, p); ok {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

func under(base, p string) (string, bool) {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// Exists reports whether the path exists (file or directory).
func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Installed gathers the evidence of a global agent installation: its binary
// on PATH and its own paths that exist. It returns the binary path ("" if
// absent) and the evidence, empty if nothing was found.
func (e Env) Installed(bin string, paths ...string) (string, []string) {
	var evidence []string
	binPath, err := e.LookPath(bin)
	if err != nil {
		binPath = ""
	} else {
		evidence = append(evidence, "bin:"+binPath)
	}
	for _, p := range paths {
		if Exists(p) {
			evidence = append(evidence, e.Pretty(p))
		}
	}
	return binPath, evidence
}

// Present filters the paths that exist, shortened for display.
func (e Env) Present(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if Exists(p) {
			out = append(out, e.Pretty(p))
		}
	}
	return out
}

// StateDir is $XDG_STATE_HOME/pegboard (~/.local/state/pegboard): global
// state and backups.
func (e Env) StateDir() string {
	return filepath.Join(e.EnvDir("XDG_STATE_HOME", filepath.Join(".local", "state")), "pegboard")
}

// ConfigDir is $XDG_CONFIG_HOME/pegboard: where the global pegboard.yaml
// lives.
func (e Env) ConfigDir() string {
	return filepath.Join(e.XDGConfig(), "pegboard")
}

// CacheDir is $XDG_CACHE_HOME/pegboard (~/.cache/pegboard): downloaded
// skills, stored once per content hash.
func (e Env) CacheDir() string {
	return filepath.Join(e.EnvDir("XDG_CACHE_HOME", ".cache"), "pegboard")
}
