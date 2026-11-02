// Package claudecode is the Claude Code provider.
package claudecode

import (
	"path/filepath"

	"github.com/rmandevs/pegboard/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "claude-code" }
func (Provider) Version() int { return 1 }

// Detect: installed if `claude` is on PATH or its user config exists.
// $CLAUDE_CONFIG_DIR moves ~/.claude and, with it, .claude.json.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	cfgDir := filepath.Join(env.Home, ".claude")
	claudeJSON := filepath.Join(env.Home, ".claude.json")
	if d := env.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		cfgDir = d
		claudeJSON = filepath.Join(d, ".claude.json")
	}
	bin, evidence := env.Installed("claude", claudeJSON, cfgDir)
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:    claudeJSON,
			Skills:  filepath.Join(cfgDir, "skills"),
			Context: filepath.Join(cfgDir, "CLAUDE.md"),
		})}
	case agent.Project:
		dir := env.ProjectDir
		paths := agent.Paths{
			MCPs:    filepath.Join(dir, ".mcp.json"),
			Skills:  filepath.Join(dir, ".claude", "skills"),
			Context: filepath.Join(dir, "CLAUDE.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin,
			env.Present(paths.MCPs, filepath.Join(dir, ".claude"), paths.Context), paths)}
	}
	return nil
}
