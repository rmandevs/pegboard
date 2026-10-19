package claudecode

import (
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
)

func TestDetectNothing(t *testing.T) {
	m := agenttest.New(t)
	for _, s := range []agent.Scope{agent.Global, agent.Project} {
		if got := (Provider{}).Detect(m.Env(), s); len(got) != 0 {
			t.Errorf("%s: no debía detectar nada: %+v", s, got)
		}
	}
}

func TestDetectGlobal(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["claude"] = "2.1.3"
	m.File("home/.claude.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 target, hay %d", len(got))
	}
	tg := got[0]
	if tg.Version != "2.1.3" || tg.Binary != m.BinPath("claude") {
		t.Errorf("binario/versión: %+v", tg)
	}
	if tg.Paths.MCPs != m.Path("home/.claude.json") || tg.Paths.Skills != m.Path("home/.claude/skills") {
		t.Errorf("rutas: %+v", tg.Paths)
	}
	if len(tg.Evidence) != 2 {
		t.Errorf("evidencia: %v", tg.Evidence)
	}
}

func TestDetectConfigDirOnly(t *testing.T) {
	m := agenttest.New(t)
	m.Mkdir("home/.claude")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Binary != "" || got[0].Version != "" {
		t.Fatalf("debía detectar sin binario: %+v", got)
	}
}

func TestDetectClaudeConfigDir(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["CLAUDE_CONFIG_DIR"] = m.Mkdir("cfg")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 {
		t.Fatal("no detectó con CLAUDE_CONFIG_DIR")
	}
	if got[0].Paths.MCPs != m.Path("cfg/.claude.json") || got[0].Paths.Skills != m.Path("cfg/skills") {
		t.Errorf("rutas: %+v", got[0].Paths)
	}
}

func TestDetectProject(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["claude"] = ""
	m.File("proj/.mcp.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Project)
	if len(got) != 1 {
		t.Fatal("no detectó proyecto")
	}
	tg := got[0]
	if tg.Paths.MCPs != m.Path("proj/.mcp.json") || tg.Paths.Skills != m.Path("proj/.claude/skills") {
		t.Errorf("rutas: %+v", tg.Paths)
	}
	if len(tg.Evidence) != 1 || tg.Evidence[0] != ".mcp.json" {
		t.Errorf("evidencia: %v", tg.Evidence)
	}
}
