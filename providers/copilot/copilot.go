// Package copilot is the GitHub Copilot CLI provider.
package copilot

import (
	"path/filepath"

	"github.com/rmandevs/pegboard/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "copilot" }
func (Provider) Version() int { return 1 }

func configDir(env agent.Env) string {
	return filepath.Join(env.Home, ".copilot")
}

// Detect: installed if `copilot` is on PATH or ~/.copilot exists. The CLI
// reads its MCP servers from ~/.copilot/mcp-config.json; in a project it
// reads skills from .github/skills and instructions from AGENTS.md, but has
// no project MCP file pegboard writes.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	dir := configDir(env)
	bin, evidence := env.Installed("copilot", dir)
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:   filepath.Join(dir, "mcp-config.json"),
			Skills: filepath.Join(dir, "skills"),
		})}
	case agent.Project:
		skills := filepath.Join(env.ProjectDir, ".github", "skills")
		paths := agent.Paths{
			Skills:  skills,
			Context: filepath.Join(env.ProjectDir, "AGENTS.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin, env.Present(skills, paths.Context), paths)}
	}
	return nil
}
