// Package hermes is the Hermes Agent (Nous Research) provider.
package hermes

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rmandevs/agents-casc/internal/agent"
)

type Provider struct{}

func (Provider) ID() string   { return "hermes" }
func (Provider) Version() int { return 1 }

// Detect: installed if `hermes` is on PATH or $HERMES_HOME (~/.hermes)
// exists. Each directory under profiles/ is a hermes:<profile> sub-target
// with the same structure. In a project, Hermes only reads skills
// (.hermes/skills); its MCPs are always global.
func (p Provider) Detect(env agent.Env, scope agent.Scope) []agent.Target {
	home := env.EnvDir("HERMES_HOME", ".hermes")
	bin, evidence := env.Installed("hermes", home)
	if len(evidence) == 0 {
		return nil
	}
	if scope == agent.Project {
		dir := filepath.Join(env.ProjectDir, ".hermes")
		return []agent.Target{env.NewTarget(p, scope, bin, env.Present(dir),
			agent.Paths{Skills: filepath.Join(dir, "skills")})}
	}
	targets := []agent.Target{env.NewTarget(p, scope, bin, evidence, paths(home))}
	for _, prof := range profiles(home) {
		dir := filepath.Join(home, "profiles", prof)
		t := env.NewTarget(p, scope, bin, []string{env.Pretty(dir)}, paths(dir))
		t.Profile = prof
		targets = append(targets, t)
	}
	return targets
}

func paths(dir string) agent.Paths {
	return agent.Paths{
		MCPs:    filepath.Join(dir, "config.yaml"),
		Skills:  filepath.Join(dir, "skills"),
		Context: filepath.Join(dir, "SOUL.md"),
	}
}

func profiles(home string) []string {
	entries, err := os.ReadDir(filepath.Join(home, "profiles"))
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
