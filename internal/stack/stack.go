// Package stack reads and edits pegboard.yaml (SPEC §3.1).
package stack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/secrets"
	"github.com/rmandevs/pegboard/internal/skills"
	"github.com/rmandevs/pegboard/schema"
)

const FileName = "pegboard.yaml"

// Stack is an already validated pegboard.yaml.
type Stack struct {
	Version int                `yaml:"version"`
	Agents  []string           `yaml:"agents"`
	Inherit string             `yaml:"inherit"`
	Include []string           `yaml:"include"`
	MCPs    map[string]mcp.MCP `yaml:"mcps"`
	Skills  []SkillRef         `yaml:"skills"`
	Context struct {
		AgentsMD bool `yaml:"agents_md"`
	} `yaml:"context"`
	Remove struct {
		MCPs   []string `yaml:"mcps"`
		Skills []string `yaml:"skills"`
	} `yaml:"remove"`
	Policy struct {
		AllowMCPs         []string `yaml:"allow_mcps"`
		AllowSkillSources []string `yaml:"allow_skill_sources"`
	} `yaml:"policy"`

	Path string `yaml:"-"`
}

// SkillRef is one entry of skills:, a source that may be limited to some agents.
type SkillRef struct {
	Source string   `yaml:"source"`
	Agents []string `yaml:"agents,omitempty"`
}

// UnmarshalYAML reads the short form (a string) or the long one.
func (r *SkillRef) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&r.Source)
	}
	type long SkillRef
	return n.Decode((*long)(r))
}

// MarshalYAML writes the short form when no agents limit the skill.
func (r SkillRef) MarshalYAML() (any, error) {
	if len(r.Agents) == 0 {
		return r.Source, nil
	}
	type long SkillRef
	return long(r), nil
}

// SkillSources lists the declared sources.
func (s *Stack) SkillSources() []string {
	out := make([]string, len(s.Skills))
	for i, r := range s.Skills {
		out[i] = r.Source
	}
	return out
}

// Path is the stack path for a scope.
func Path(env agent.Env, scope agent.Scope) string {
	if scope == agent.Global {
		return filepath.Join(env.ConfigDir(), FileName)
	}
	return filepath.Join(env.ProjectDir, FileName)
}

// Load reads and validates a pegboard.yaml. If it does not exist it returns
// (nil, nil).
func Load(path string) (*Stack, error) {
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := schema.ValidateYAML(src); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s := &Stack{Path: path}
	if err := yaml.Unmarshal(src, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, src := range s.SkillSources() {
		if _, err := skills.Parse(src); err != nil {
			return nil, fmt.Errorf("%s: skills: %w", path, err)
		}
	}
	for _, in := range s.Include {
		if _, err := skills.ParseInclude(in); err != nil {
			return nil, fmt.Errorf("%s: include: %w", path, err)
		}
	}
	for _, name := range s.MCPNames() {
		m := s.MCPs[name]
		if err := m.Validate(); err != nil {
			return nil, fmt.Errorf("%s: mcps.%s: %w", path, name, err)
		}
		for _, v := range strs(m) {
			if err := secrets.Check(v); err != nil {
				return nil, fmt.Errorf("%s: mcps.%s: %w", path, name, err)
			}
		}
	}
	return s, nil
}

func strs(m mcp.MCP) []string {
	out := append([]string{m.Command, m.URL}, m.Args...)
	for _, v := range m.Env {
		out = append(out, v)
	}
	for _, v := range m.Headers {
		out = append(out, v)
	}
	return out
}

// MCPNames lists the declared MCPs, sorted.
func (s *Stack) MCPNames() []string {
	var out []string
	for n := range s.MCPs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// LockPath is the pegboard.lock next to the stack.
func (s *Stack) LockPath() string { return filepath.Join(filepath.Dir(s.Path), skills.LockFile) }

// Removed reports whether a skill (by source or by name) is in
// remove.skills.
func (s *Stack) Removed(source, name string) bool {
	for _, r := range s.Remove.Skills {
		if r == source || r == name {
			return true
		}
	}
	return false
}

// SkillMatches reports whether the declared source corresponds to arg: the
// exact source, owner/repo@arg, or a path that ends in arg.
func SkillMatches(source, arg string) bool {
	if source == arg {
		return true
	}
	base := strings.SplitN(source, "?", 2)[0]
	if _, skill, ok := strings.Cut(base, "@"); ok && skill == arg {
		return true
	}
	return filepath.Base(filepath.FromSlash(base)) == arg
}

// Template is the content of `pegboard init`.
func Template(agents []string) string {
	var b strings.Builder
	b.WriteString("# Desired state for your agents. See https://github.com/rmandevs/pegboard\n")
	b.WriteString("# yaml-language-server: $schema=https://raw.githubusercontent.com/rmandevs/pegboard/main/schema/pegboard.schema.json\n")
	b.WriteString("version: 1\n")
	if len(agents) > 0 {
		b.WriteString("agents: [" + strings.Join(agents, ", ") + "]\n")
	}
	b.WriteString(`
# Secrets use references: ${env:NAME} or ${file:~/.secrets/x}.
mcps: {}
#  github:
#    command: npx
#    args: ["-y", "@modelcontextprotocol/server-github"]
#    env:
#      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
#  notion:
#    url: https://mcp.notion.com/mcp

# Skills: owner/repo, owner/repo@skill, github.com/o/r//path?ref=v1 or ./local.
# skills:
#   - pegboard

# context:
#   agents_md: true   # AGENTS.md and the @AGENTS.md bridge in CLAUDE.md
`)
	return b.String()
}

// Obj is an MCP in the order it is written to pegboard.yaml.
func Obj(m mcp.MCP) *native.Obj {
	return native.NewObj().
		SetIf("command", m.Command).
		SetIf("args", m.Args).
		SetIf("env", native.StringMap(m.Env)).
		SetIf("url", m.URL).
		SetIf("headers", native.StringMap(m.Headers)).
		SetIf("agents", m.Agents)
}

// Editor adds or removes MCPs and skills in a pegboard.yaml while preserving
// comments.
type Editor struct {
	path string
	doc  native.ListDoc
}

// Edit opens the stack for editing; if it does not exist, it starts from
// Template.
func Edit(path string, agents []string) (*Editor, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(Template(agents)), 0o644); err != nil {
			return nil, err
		}
	}
	doc, err := native.Open(native.File{Path: path, Format: native.YAML, Container: "mcps"})
	if err != nil {
		return nil, err
	}
	return &Editor{path: path, doc: doc.(native.ListDoc)}, nil
}

// Path is the file being edited.
func (e *Editor) Path() string { return e.path }

func (e *Editor) Set(name string, m mcp.MCP) error { return e.doc.Set(name, Obj(m)) }
func (e *Editor) Delete(name string) error         { return e.doc.Delete(name) }

// HasMCP reports whether the stack declares the MCP.
func (e *Editor) HasMCP(name string) bool {
	ents, _ := e.doc.Entries()
	_, ok := ents[name]
	return ok
}

// Skills lists the declared skills.
func (e *Editor) Skills() ([]SkillRef, error) {
	var l []SkillRef
	return l, e.doc.List("skills", &l)
}

// AddSkill adds a source if it was not already there, limited to agents if any;
// it reports whether it was added.
func (e *Editor) AddSkill(source string, agents []string) (bool, error) {
	l, err := e.Skills()
	if err != nil {
		return false, err
	}
	for _, r := range l {
		if r.Source == source {
			return false, nil
		}
	}
	return true, e.doc.SetList("skills", append(l, SkillRef{Source: source, Agents: agents}), false)
}

// RemoveSkill removes the sources that correspond to arg; it returns which
// ones were removed.
func (e *Editor) RemoveSkill(arg string) ([]string, error) {
	l, err := e.Skills()
	if err != nil {
		return nil, err
	}
	var keep []SkillRef
	var gone []string
	for _, r := range l {
		if SkillMatches(r.Source, arg) {
			gone = append(gone, r.Source)
		} else {
			keep = append(keep, r)
		}
	}
	if len(gone) == 0 {
		return nil, nil
	}
	return gone, e.doc.SetList("skills", keep, len(keep) == 0)
}

// Save validates the result against the schema before writing it.
func (e *Editor) Save() error {
	b, err := e.doc.Bytes()
	if err != nil {
		return err
	}
	if err := schema.ValidateYAML(b); err != nil {
		return fmt.Errorf("the resulting %s is not valid: %w", FileName, err)
	}
	return os.WriteFile(e.path, b, 0o644)
}
