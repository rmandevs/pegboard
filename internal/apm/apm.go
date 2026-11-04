// Package apm reads the manifest of Microsoft's Agent Package Manager
// (apm.yml) and translates what pegboard can manage: skills and self-defined
// MCP servers. APM is a package manager; pegboard reconciles agent configs.
// Reading apm.yml lets a team keep one manifest and use pegboard for plan,
// state, and drift.
package apm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/skills"
)

// FileName is the APM manifest.
const FileName = "apm.yml"

// Manifest is the part of apm.yml pegboard reads.
type Manifest struct {
	Name         string `yaml:"name"`
	Dependencies struct {
		APM []yaml.Node `yaml:"apm"`
		MCP []yaml.Node `yaml:"mcp"`
	} `yaml:"dependencies"`
}

// Skill is a skill source translated to pegboard's syntax.
type Skill struct {
	Source string
	From   string // the dependency as written in apm.yml
}

// Result is what could be translated, plus what could not and why.
type Result struct {
	Skills   []Skill
	MCPs     map[string]mcp.MCP
	Skipped  []string
	Warnings []string
}

type apmDep struct {
	Git    string   `yaml:"git"`
	Path   string   `yaml:"path"`
	Ref    string   `yaml:"ref"`
	Skills []string `yaml:"skills"`
	// Entries pegboard cannot resolve.
	ID          string `yaml:"id"`
	Marketplace string `yaml:"marketplace"`
	Registry    string `yaml:"registry"`
	Name        string `yaml:"name"`
}

type mcpDep struct {
	Name      string            `yaml:"name"`
	Registry  any               `yaml:"registry"`
	Transport string            `yaml:"transport"`
	Command   string            `yaml:"command"`
	Args      any               `yaml:"args"`
	Env       map[string]string `yaml:"env"`
	URL       string            `yaml:"url"`
	Headers   map[string]string `yaml:"headers"`
}

// Read parses apm.yml. rebase turns a local path relative to apm.yml into
// the form pegboard.yaml should hold.
func Read(path string, rebase func(abs string) string) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	dir := filepath.Dir(path)
	r := &Result{MCPs: map[string]mcp.MCP{}}
	for _, n := range m.Dependencies.APM {
		r.apm(n, dir, rebase)
	}
	for _, n := range m.Dependencies.MCP {
		r.mcp(n)
	}
	if len(r.Skills) > 0 {
		r.Warnings = append(r.Warnings,
			"pegboard installs the skills (SKILL.md) inside APM packages; instructions, prompts, agents, and hooks stay with apm")
	}
	return r, nil
}

func (r *Result) apm(n yaml.Node, dir string, rebase func(string) string) {
	var raw string
	var d apmDep
	if n.Kind == yaml.ScalarNode {
		raw = n.Value
		if skills.IsLocalPath(raw) {
			d.Path = raw
		} else {
			d.Git = raw
		}
	} else if err := n.Decode(&d); err != nil {
		r.Skipped = append(r.Skipped, fmt.Sprintf("dependencies.apm line %d: %v", n.Line, err))
		return
	} else {
		raw = describe(d)
	}
	switch {
	case d.Marketplace != "" || d.ID != "" || d.Registry != "":
		r.Skipped = append(r.Skipped, raw+": marketplace and registry packages are resolved by apm, not pegboard")
		return
	case d.Git == "parent":
		r.Skipped = append(r.Skipped, raw+": monorepo siblings (git: parent) are not supported")
		return
	case d.Git == "" && d.Path != "":
		p := d.Path
		if strings.HasPrefix(p, "~/") || filepath.IsAbs(p) {
			r.Skills = append(r.Skills, Skill{Source: p, From: raw})
			return
		}
		r.Skills = append(r.Skills, Skill{Source: rebase(filepath.Join(dir, filepath.FromSlash(p))), From: raw})
		return
	case d.Git == "":
		r.Skipped = append(r.Skipped, raw+": no git or path")
		return
	}
	if strings.HasSuffix(d.Path, ".md") || strings.HasSuffix(strings.SplitN(d.Git, "#", 2)[0], ".md") {
		r.Skipped = append(r.Skipped, raw+": a single prompt or agent file is not a skill")
		return
	}
	src, err := source(d)
	if err != nil {
		r.Skipped = append(r.Skipped, raw+": "+err.Error())
		return
	}
	if len(d.Skills) == 0 || len(d.Skills) == 1 && d.Skills[0] == "*" {
		r.Skills = append(r.Skills, Skill{Source: src, From: raw})
		return
	}
	// owner/repo@skill selects one skill; it only exists for the GitHub
	// shorthand without a subdirectory.
	parsed, _ := skills.Parse(src)
	if parsed.Path != "" || !strings.HasPrefix(parsed.URL, skills.GitHub) {
		r.Warnings = append(r.Warnings, raw+": installing every skill in it (pegboard selects single skills only with owner/repo@skill)")
		r.Skills = append(r.Skills, Skill{Source: src, From: raw})
		return
	}
	repo := strings.TrimSuffix(strings.TrimPrefix(parsed.URL, skills.GitHub), ".git")
	for _, name := range d.Skills {
		s := repo + "@" + name
		if parsed.Ref != "" {
			s += "?ref=" + parsed.Ref
		}
		r.Skills = append(r.Skills, Skill{Source: s, From: raw})
	}
}

// source builds a pegboard source from an APM git dependency.
func source(d apmDep) (string, error) {
	g := d.Git
	ref := d.Ref
	if i := strings.LastIndex(g, "#"); i >= 0 && !strings.Contains(g[i:], "/") {
		if ref == "" {
			ref = g[i+1:]
		}
		g = g[:i]
	}
	if isSemverRange(ref) {
		return "", fmt.Errorf("semver range %q: pin a tag or commit", ref)
	}
	if d.Path != "" {
		if strings.Contains(g, "://") || strings.HasPrefix(g, "git@") || strings.Contains(g, ".") && strings.Count(g, "/") >= 2 {
			g += "//" + strings.Trim(d.Path, "/")
		} else {
			g = strings.TrimSuffix(g, "/") + "/" + strings.Trim(d.Path, "/")
		}
	}
	if ref != "" {
		g += "?ref=" + ref
	}
	if _, err := skills.Parse(g); err != nil {
		return "", err
	}
	return g, nil
}

var semverRe = regexp.MustCompile(`^[\^~><=]`)

func isSemverRange(ref string) bool { return semverRe.MatchString(ref) || strings.Contains(ref, " ") }

func describe(d apmDep) string {
	var parts []string
	for _, kv := range [][2]string{{"git", d.Git}, {"path", d.Path}, {"ref", d.Ref}, {"id", d.ID},
		{"name", d.Name}, {"marketplace", d.Marketplace}, {"registry", d.Registry}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+": "+kv[1])
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

var nameRe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func (r *Result) mcp(n yaml.Node) {
	if n.Kind == yaml.ScalarNode {
		r.Skipped = append(r.Skipped, "mcp "+n.Value+": registry servers are resolved by apm; declare it with registry: false, or add it with `pegboard add mcp`")
		return
	}
	var d mcpDep
	if err := n.Decode(&d); err != nil {
		r.Skipped = append(r.Skipped, fmt.Sprintf("dependencies.mcp line %d: %v", n.Line, err))
		return
	}
	selfDefined := d.Registry == false
	if !selfDefined {
		r.Skipped = append(r.Skipped, "mcp "+d.Name+": registry servers are resolved by apm; declare it with registry: false, or add it with `pegboard add mcp`")
		return
	}
	// The registry name can carry dots and slashes; pegboard names cannot.
	name := strings.Trim(nameRe.ReplaceAllString(lastSegment(d.Name), "-"), "-.")
	if name == "" {
		r.Skipped = append(r.Skipped, "mcp "+d.Name+": no usable name")
		return
	}
	var m mcp.MCP
	switch d.Transport {
	case "stdio":
		var args []string
		if l, ok := d.Args.([]any); ok {
			for _, a := range l {
				args = append(args, fmt.Sprint(a))
			}
		}
		m = mcp.MCP{Command: d.Command, Args: args}
		m.Env = r.refs(name, d.Env)
	case "http", "sse", "streamable-http":
		m = mcp.MCP{URL: d.URL}
		m.Headers = r.refs(name, d.Headers)
	default:
		r.Skipped = append(r.Skipped, fmt.Sprintf("mcp %s: unknown transport %q", d.Name, d.Transport))
		return
	}
	for i, a := range m.Args {
		m.Args[i] = r.ref(name, a)
	}
	if err := m.Validate(); err != nil {
		r.Skipped = append(r.Skipped, "mcp "+d.Name+": "+err.Error())
		return
	}
	if _, dup := r.MCPs[name]; dup {
		r.Skipped = append(r.Skipped, "mcp "+d.Name+": the name "+name+" is already taken")
		return
	}
	r.MCPs[name] = m
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

var (
	ghSecretRe = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	bareRe     = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	legacyRe   = regexp.MustCompile(`^<([A-Za-z_][A-Za-z0-9_]*)>$`)
	inputRe    = regexp.MustCompile(`\$\{input:[^}]*\}`)
)

// ref translates APM's variable syntaxes to ${env:X}.
func (r *Result) ref(name, v string) string {
	v = ghSecretRe.ReplaceAllString(v, "$${env:$1}")
	v = bareRe.ReplaceAllString(v, "$${env:$1}")
	v = legacyRe.ReplaceAllString(v, "$${env:$1}")
	if inputRe.MatchString(v) {
		r.Warnings = append(r.Warnings, "mcp "+name+": ${input:…} prompts have no pegboard equivalent; replace them with ${env:…}")
	}
	return v
}

func (r *Result) refs(name string, in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = r.ref(name, in[k])
	}
	return out
}
