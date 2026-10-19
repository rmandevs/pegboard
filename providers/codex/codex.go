// Package codex es el provider de OpenAI Codex CLI.
package codex

import (
	"path/filepath"

	"github.com/rmandevs/agents-casc/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "codex" }
func (Provider) Version() int { return 1 }

// Detect: instalado si `codex` está en el PATH o existe $CODEX_HOME
// (~/.codex). Las skills viven en .agents/skills, compartido con otros
// agentes que siguen esa convención.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	home := env.EnvDir("CODEX_HOME", ".codex")
	bin, evidence := env.Installed("codex", home)
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:    filepath.Join(home, "config.toml"),
			Skills:  filepath.Join(env.Home, ".agents", "skills"),
			Context: filepath.Join(home, "AGENTS.md"),
		})}
	case agent.Project:
		dir := env.ProjectDir
		paths := agent.Paths{
			MCPs:    filepath.Join(dir, ".codex", "config.toml"),
			Skills:  filepath.Join(dir, ".agents", "skills"),
			Context: filepath.Join(dir, "AGENTS.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin,
			env.Present(filepath.Join(dir, ".codex"), paths.Skills, paths.Context), paths)}
	}
	return nil
}
