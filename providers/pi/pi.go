// Package pi is the Pi coding agent provider.
package pi

import (
	"path/filepath"

	"github.com/rmandevs/pegboard/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "pi" }
func (Provider) Version() int { return 1 }

// agentDir is Pi's user agent directory ($PI_CODING_AGENT_DIR or
// ~/.pi/agent).
func agentDir(env agent.Env) string {
	if d := env.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	return filepath.Join(env.Home, ".pi", "agent")
}

// Detect: installed if `pi` is on PATH or its agent directory exists.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	dir := agentDir(env)
	bin, evidence := env.Installed("pi", dir)
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:    filepath.Join(dir, "mcp.json"),
			Skills:  filepath.Join(dir, "skills"),
			Context: filepath.Join(dir, "AGENTS.md"),
		})}
	case agent.Project:
		pdir := filepath.Join(env.ProjectDir, ".pi")
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
