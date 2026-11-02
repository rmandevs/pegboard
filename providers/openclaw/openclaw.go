// Package openclaw is the OpenClaw provider.
package openclaw

import (
	"path/filepath"

	"github.com/rmandevs/pegboard/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "openclaw" }
func (Provider) Version() int { return 1 }

func stateDir(env agent.Env) string {
	if d := env.Getenv("OPENCLAW_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(env.Home, ".openclaw")
}

func configPath(env agent.Env) string {
	if p := env.Getenv("OPENCLAW_CONFIG_PATH"); p != "" {
		return p
	}
	return filepath.Join(stateDir(env), "openclaw.json")
}

func workspaceDir(env agent.Env) string {
	if d := env.Getenv("OPENCLAW_WORKSPACE_DIR"); d != "" {
		return d
	}
	if prof := env.Getenv("OPENCLAW_PROFILE"); prof != "" && prof != "default" {
		return filepath.Join(env.Home, ".openclaw-"+prof, "workspace")
	}
	return filepath.Join(stateDir(env), "workspace")
}

// Detect: installed if `openclaw` is on PATH or its state/config exists.
// OpenClaw is a gateway, so it has no project scope.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	if scope != agent.Global {
		return nil
	}
	sd, cfg := stateDir(env), configPath(env)
	bin, evidence := env.Installed("openclaw", sd, cfg)
	if len(evidence) == 0 {
		return nil
	}
	return []agent.Target{env.NewTarget(p, scope, bin, evidence, agent.Paths{
		MCPs:    cfg,
		Skills:  filepath.Join(sd, "skills"),
		Context: filepath.Join(workspaceDir(env), "AGENTS.md"),
	})}
}
