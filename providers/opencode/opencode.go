// Package opencode es el provider de opencode.
package opencode

import (
	"path/filepath"

	"github.com/rmandevs/agents-casc/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "opencode" }
func (Provider) Version() int { return 1 }

// Detect: instalado si `opencode` está en el PATH o existe su carpeta en
// $XDG_CONFIG_HOME (~/.config/opencode).
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	cfgDir := filepath.Join(env.XDGConfig(), "opencode")
	bin, evidence := env.Installed("opencode", cfgDir)
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:    filepath.Join(cfgDir, "opencode.json"),
			Skills:  filepath.Join(cfgDir, "skills"),
			Context: filepath.Join(cfgDir, "AGENTS.md"),
		})}
	case agent.Project:
		dir := env.ProjectDir
		paths := agent.Paths{
			MCPs: filepath.Join(dir, "opencode.json"),
			// .agents/skills: la carpeta "universal" que comparte con Codex.
			Skills:  filepath.Join(dir, ".agents", "skills"),
			Context: filepath.Join(dir, "AGENTS.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin,
			env.Present(paths.MCPs, filepath.Join(dir, "opencode.jsonc"), filepath.Join(dir, ".opencode")), paths)}
	}
	return nil
}
