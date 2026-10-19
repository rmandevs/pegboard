package codex

import (
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
)

func TestDetectNothing(t *testing.T) {
	m := agenttest.New(t)
	if got := (Provider{}).Detect(m.Env(), agent.Global); len(got) != 0 {
		t.Errorf("no debía detectar nada: %+v", got)
	}
}

func TestDetectGlobal(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["codex"] = "0.46.0"
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Version != "0.46.0" {
		t.Fatalf("%+v", got)
	}
	if got[0].Paths.MCPs != m.Path("home/.codex/config.toml") || got[0].Paths.Skills != m.Path("home/.agents/skills") {
		t.Errorf("rutas: %+v", got[0].Paths)
	}
}

func TestDetectCodexHome(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["CODEX_HOME"] = m.Mkdir("ch")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("ch/config.toml") {
		t.Fatalf("%+v", got)
	}
}

func TestDetectProject(t *testing.T) {
	m := agenttest.New(t)
	m.Mkdir("home/.codex")
	m.File("proj/AGENTS.md", "# x")
	got := (Provider{}).Detect(m.Env(), agent.Project)
	if len(got) != 1 {
		t.Fatal("no detectó proyecto")
	}
	if got[0].Paths.MCPs != m.Path("proj/.codex/config.toml") || got[0].Paths.Skills != m.Path("proj/.agents/skills") {
		t.Errorf("rutas: %+v", got[0].Paths)
	}
	if len(got[0].Evidence) != 1 || got[0].Evidence[0] != "AGENTS.md" {
		t.Errorf("evidencia: %v", got[0].Evidence)
	}
}
