package claudecode

import (
	"testing"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/agent/agenttest"
)

func TestDetectNothing(t *testing.T) {
	m := agenttest.New(t)
	for _, s := range []agent.Scope{agent.Global, agent.Project} {
		if got := (Provider{}).Detect(m.Env(), s); len(got) != 0 {
			t.Errorf("%s: it should not detect anything: %+v", s, got)
		}
	}
}

func TestDetectGlobal(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["claude"] = "2.1.3"
	m.File("home/.claude.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 {
		t.Fatalf("expected 1 target, got %d", len(got))
	}
	tg := got[0]
	if tg.Version != "2.1.3" || tg.Binary != m.BinPath("claude") {
		t.Errorf("binary/version: %+v", tg)
	}
	if tg.Paths.MCPs != m.Path("home/.claude.json") || tg.Paths.Skills != m.Path("home/.claude/skills") {
		t.Errorf("paths: %+v", tg.Paths)
	}
	if len(tg.Evidence) != 2 {
		t.Errorf("evidence: %v", tg.Evidence)
	}
}

func TestDetectConfigDirOnly(t *testing.T) {
	m := agenttest.New(t)
	m.Mkdir("home/.claude")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Binary != "" || got[0].Version != "" {
		t.Fatalf("it should detect without a binary: %+v", got)
	}
}

func TestDetectClaudeConfigDir(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["CLAUDE_CONFIG_DIR"] = m.Mkdir("cfg")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 {
		t.Fatal("it did not detect with CLAUDE_CONFIG_DIR")
	}
	if got[0].Paths.MCPs != m.Path("cfg/.claude.json") || got[0].Paths.Skills != m.Path("cfg/skills") {
		t.Errorf("paths: %+v", got[0].Paths)
	}
}

func TestDetectProject(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["claude"] = ""
	m.File("proj/.mcp.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Project)
	if len(got) != 1 {
		t.Fatal("it did not detect the project")
	}
	tg := got[0]
	if tg.Paths.MCPs != m.Path("proj/.mcp.json") || tg.Paths.Skills != m.Path("proj/.claude/skills") {
		t.Errorf("paths: %+v", tg.Paths)
	}
	if len(tg.Evidence) != 1 || tg.Evidence[0] != ".mcp.json" {
		t.Errorf("evidence: %v", tg.Evidence)
	}
}
