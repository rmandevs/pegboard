package copilot

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
	m.Bins["copilot"] = "0.0.400"
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("home/.copilot/mcp-config.json") || got[0].Paths.Skills != m.Path("home/.copilot/skills") {
		t.Fatalf("global: %+v", got)
	}
	proj := (Provider{}).Detect(m.Env(), agent.Project)
	if len(proj) != 1 || proj[0].Paths.MCPs != "" || proj[0].Paths.Skills != m.Path("proj/.github/skills") || proj[0].Paths.Context != m.Path("proj/AGENTS.md") {
		t.Fatalf("project: %+v", proj)
	}
}
