// Package cursor is the Cursor provider.
package cursor

import (
	"path/filepath"

	"github.com/rmandevs/pegboard/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "cursor" }
func (Provider) Version() int { return 1 }

func configDir(env agent.Env) string {
	return filepath.Join(env.Home, ".cursor")
}

// Detect: installed if `cursor` or `cursor-agent` is on PATH, or ~/.cursor
// exists.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	dir := configDir(env)
	bin, evidence := env.Installed("cursor", dir)
	if len(evidence) == 0 {
		bin, evidence = env.Installed("cursor-agent", dir)
	}
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:   filepath.Join(dir, "mcp.json"),
			Skills: filepath.Join(dir, "skills"),
		})}
	case agent.Project:
		pdir := filepath.Join(env.ProjectDir, ".cursor")
		paths := agent.Paths{
			MCPs:    filepath.Join(pdir, "mcp.json"),
			Skills:  filepath.Join(pdir, "skills"),
			Context: filepath.Join(env.ProjectDir, "AGENTS.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin,
			env.Present(pdir, paths.Context), paths)}
	}
	return nil
}
