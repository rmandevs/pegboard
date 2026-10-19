// Package stack lee y edita pegboard.yaml (SPEC §3.1).
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

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
	"github.com/rmandevs/agents-casc/internal/skills"
	"github.com/rmandevs/agents-casc/schema"
)

const FileName = "pegboard.yaml"

// Stack es un pegboard.yaml ya validado.
type Stack struct {
	Version int                `yaml:"version"`
	Agents  []string           `yaml:"agents"`
	Inherit string             `yaml:"inherit"`
	Include []string           `yaml:"include"`
	MCPs    map[string]mcp.MCP `yaml:"mcps"`
	Skills  []string           `yaml:"skills"`
	Context struct {
		AgentsMD bool `yaml:"agents_md"`
	} `yaml:"context"`
	Remove struct {
		MCPs   []string `yaml:"mcps"`
		Skills []string `yaml:"skills"`
	} `yaml:"remove"`
	Policy struct {
		AllowMCPs []string `yaml:"allow_mcps"`
	} `yaml:"policy"`

	Path string `yaml:"-"`
}

// Path es la ruta del stack de un alcance.
func Path(env agent.Env, scope agent.Scope) string {
	if scope == agent.Global {
		return filepath.Join(env.ConfigDir(), FileName)
	}
	return filepath.Join(env.ProjectDir, FileName)
}

// Load lee y valida un pegboard.yaml. Si no existe devuelve (nil, nil).
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
	for _, src := range s.Skills {
		if _, err := skills.Parse(src); err != nil {
			return nil, fmt.Errorf("%s: skills: %w", path, err)
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

// MCPNames son los MCPs declarados, ordenados.
func (s *Stack) MCPNames() []string {
	var out []string
	for n := range s.MCPs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Desired son los MCPs que quedan después de remove, validados contra la
// política. Las advertencias cubren lo que esta fase todavía ignora.
func (s *Stack) Desired() (map[string]mcp.MCP, []string, error) {
	var warnings []string
	if len(s.Include) > 0 {
		warnings = append(warnings, s.Path+": include llega en la fase 3; se ignora")
	}
	out := map[string]mcp.MCP{}
	for n, m := range s.MCPs {
		out[n] = m
	}
	for _, n := range s.Remove.MCPs {
		delete(out, n)
	}
	if allow := s.Policy.AllowMCPs; len(allow) > 0 {
		var denied []string
		for n := range out {
			if !contains(allow, n) {
				denied = append(denied, n)
			}
		}
		if len(denied) > 0 {
			sort.Strings(denied)
			return nil, warnings, fmt.Errorf("%s: policy.allow_mcps no permite: %s", s.Path, strings.Join(denied, ", "))
		}
	}
	return out, warnings, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// LockPath es el pegboard.lock junto al stack.
func (s *Stack) LockPath() string { return filepath.Join(filepath.Dir(s.Path), skills.LockFile) }

// Removed informa si una skill (por fuente o por nombre) está en remove.skills.
func (s *Stack) Removed(source, name string) bool {
	for _, r := range s.Remove.Skills {
		if r == source || r == name {
			return true
		}
	}
	return false
}

// SkillMatches informa si la fuente declarada corresponde a arg: la fuente
// exacta, owner/repo@arg o una ruta que termina en arg.
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

// Template es el contenido de `pegboard init`.
func Template(agents []string) string {
	var b strings.Builder
	b.WriteString("# Estado deseado de tus agentes. Ver https://github.com/rmandevs/agents-casc\n")
	b.WriteString("# yaml-language-server: $schema=https://raw.githubusercontent.com/rmandevs/agents-casc/main/schema/pegboard.schema.json\n")
	b.WriteString("version: 1\n")
	if len(agents) > 0 {
		b.WriteString("agents: [" + strings.Join(agents, ", ") + "]\n")
	}
	b.WriteString(`
# Los secretos van como referencia: ${env:NOMBRE} o ${file:~/.secrets/x}.
mcps: {}
#  github:
#    command: npx
#    args: ["-y", "@modelcontextprotocol/server-github"]
#    env:
#      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
#  notion:
#    url: https://mcp.notion.com/mcp

# Skills: owner/repo, owner/repo@skill, github.com/o/r//ruta?ref=v1 o ./local.
# skills:
#   - pegboard

# context:
#   agents_md: true   # AGENTS.md y el puente @AGENTS.md en CLAUDE.md
`)
	return b.String()
}

// Obj es un MCP en el orden en que se escribe en pegboard.yaml.
func Obj(m mcp.MCP) *native.Obj {
	return native.NewObj().
		SetIf("command", m.Command).
		SetIf("args", m.Args).
		SetIf("env", native.StringMap(m.Env)).
		SetIf("url", m.URL).
		SetIf("headers", native.StringMap(m.Headers)).
		SetIf("agents", m.Agents)
}

// Editor agrega o quita MCPs y skills en un pegboard.yaml conservando
// comentarios.
type Editor struct {
	path string
	doc  native.ListDoc
}

// Edit abre el stack para editarlo; si no existe, parte de Template.
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

// Path es el archivo que se edita.
func (e *Editor) Path() string { return e.path }

func (e *Editor) Set(name string, m mcp.MCP) error { return e.doc.Set(name, Obj(m)) }
func (e *Editor) Delete(name string) error         { return e.doc.Delete(name) }

// HasMCP informa si el stack declara el MCP.
func (e *Editor) HasMCP(name string) bool {
	ents, _ := e.doc.Entries()
	_, ok := ents[name]
	return ok
}

// Skills son las fuentes declaradas.
func (e *Editor) Skills() []string { return e.doc.StringList("skills") }

// AddSkill agrega una fuente si no estaba; devuelve si la agregó.
func (e *Editor) AddSkill(source string) (bool, error) {
	l := e.Skills()
	for _, s := range l {
		if s == source {
			return false, nil
		}
	}
	return true, e.doc.SetStringList("skills", append(l, source))
}

// RemoveSkill quita las fuentes que corresponden a arg; devuelve cuáles.
func (e *Editor) RemoveSkill(arg string) ([]string, error) {
	var keep, gone []string
	for _, s := range e.Skills() {
		if SkillMatches(s, arg) {
			gone = append(gone, s)
		} else {
			keep = append(keep, s)
		}
	}
	if len(gone) == 0 {
		return nil, nil
	}
	return gone, e.doc.SetStringList("skills", keep)
}

// Save valida el resultado contra el schema antes de escribirlo.
func (e *Editor) Save() error {
	b, err := e.doc.Bytes()
	if err != nil {
		return err
	}
	if err := schema.ValidateYAML(b); err != nil {
		return fmt.Errorf("el %s resultante no es válido: %w", FileName, err)
	}
	return os.WriteFile(e.path, b, 0o644)
}
