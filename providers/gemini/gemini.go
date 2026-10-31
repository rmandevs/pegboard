// Package gemini is the Gemini CLI provider.
package gemini

import (
	"path/filepath"

	"github.com/rmandevs/agents-casc/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "gemini" }
func (Provider) Version() int { return 1 }

// configDir is $GEMINI_CLI_HOME/.gemini or ~/.gemini. GEMINI_CLI_HOME is the
// root directory; Gemini CLI creates its .gemini directory inside it.
func configDir(env agent.Env) string {
	if h := env.Getenv("GEMINI_CLI_HOME"); h != "" {
		return filepath.Join(h, ".gemini")
	}
	return filepath.Join(env.Home, ".gemini")
}

// Detect: installed if `gemini` is on PATH or ~/.gemini exists.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	dir := configDir(env)
	bin, evidence := env.Installed("gemini", dir)
	if len(evidence) == 0 {
		return nil
	}
	switch scope {
	case agent.Global:
		return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
			MCPs:    filepath.Join(dir, "settings.json"),
			Skills:  filepath.Join(dir, "skills"),
			Context: filepath.Join(dir, "GEMINI.md"),
		})}
	case agent.Project:
		pdir := filepath.Join(env.ProjectDir, ".gemini")
		paths := agent.Paths{
			MCPs:    filepath.Join(pdir, "settings.json"),
			Skills:  filepath.Join(pdir, "skills"),
			Context: filepath.Join(env.ProjectDir, "GEMINI.md"),
		}
		return []agent.Target{env.NewTarget(p, scope, bin,
			env.Present(pdir, paths.Context), paths)}
	}
	return nil
}
