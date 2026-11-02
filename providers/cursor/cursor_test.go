package cursor

import (
	"testing"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/agent/agenttest"
)

func TestDetectNothing(t *testing.T) {
	m := agenttest.New(t)
	if got := (Provider{}).Detect(m.Env(), agent.Global); len(got) != 0 {
		t.Errorf("it should not detect anything: %+v", got)
	}
}

func TestDetectGlobalAndProject(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["cursor"] = "2.0.0"
	m.File("home/.cursor/mcp.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("home/.cursor/mcp.json") || got[0].Paths.Skills != m.Path("home/.cursor/skills") {
		t.Fatalf("global: %+v", got)
	}
	m.File("proj/AGENTS.md", "# x")
	proj := (Provider{}).Detect(m.Env(), agent.Project)
	if len(proj) != 1 || proj[0].Paths.MCPs != m.Path("proj/.cursor/mcp.json") || proj[0].Paths.Context != m.Path("proj/AGENTS.md") {
		t.Fatalf("project: %+v", proj)
	}
}

func TestDetectCursorAgent(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["cursor-agent"] = "2026.1"
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Version != "2026.1" {
		t.Fatalf("%+v", got)
	}
}
