// Package omp is the OMP (oh-my-pi) coding agent provider.
package omp

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rmandevs/agents-casc/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "omp" }
func (Provider) Version() int { return 1 }

// configRoot is OMP's user config root ($PI_CONFIG_DIR or ~/.omp).
func configRoot(env agent.Env) string {
	d := env.Getenv("PI_CONFIG_DIR")
	if d == "" {
		return filepath.Join(env.Home, ".omp")
	}
	if filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(env.Home, d)
}

// agentDir is the default profile's agent directory.
func agentDir(env agent.Env) string {
	if d := env.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	return filepath.Join(configRoot(env), "agent")
}

func profileAgentDir(env agent.Env, name string) string {
	return filepath.Join(configRoot(env), "profiles", name, "agent")
}

func paths(dir string) agent.Paths {
	return agent.Paths{
		MCPs:    filepath.Join(dir, "mcp.json"),
		Skills:  filepath.Join(dir, "skills"),
		Context: filepath.Join(dir, "AGENTS.md"),
	}
}

// Detect: installed if `omp` is on PATH or its agent directory exists. Each
// directory under profiles/ is an omp:<profile> sub-target. In a project,
// OMP reads .omp/mcp.json and .omp/skills.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	dir := agentDir(env)
	bin, evidence := env.Installed("omp", dir)
	if len(evidence) == 0 {
		return nil
	}
	if scope == agent.Project {
		pdir := filepath.Join(env.ProjectDir, ".omp")
		paths := agent.Paths{
			MCPs:    filepath.Join(pdir, "mcp.json"),
			Skills:  filepath.Join(pdir, "skills"),
			Context: filepath.Join(pdir, "AGENTS.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin,
			env.Present(pdir, paths.Skills, paths.Context), paths)}
	}
	targets := []agent.Target{env.NewTarget(p, scope, bin, evidence, paths(dir))}
	for _, prof := range profiles(env) {
		pdir := profileAgentDir(env, prof)
		if !agent.Exists(pdir) {
			continue
		}
		t := env.NewTarget(p, scope, bin, []string{env.Pretty(pdir)}, paths(pdir))
		t.Profile = prof
		targets = append(targets, t)
	}
	return targets
}

func profiles(env agent.Env) []string {
	entries, err := os.ReadDir(filepath.Join(configRoot(env), "profiles"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
