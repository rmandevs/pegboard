package pi

import (
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
)

func TestDetectNothing(t *testing.T) {
	m := agenttest.New(t)
	if got := (Provider{}).Detect(m.Env(), agent.Global); len(got) != 0 {
		t.Errorf("it should not detect anything: %+v", got)
	}
}

func TestDetectGlobalAndProject(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["pi"] = "1.4.0"
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Version != "1.4.0" {
		t.Fatalf("%+v", got)
	}
	if got[0].Paths.MCPs != m.Path("home/.pi/agent/mcp.json") || got[0].Paths.Skills != m.Path("home/.pi/agent/skills") {
		t.Errorf("paths: %+v", got[0].Paths)
	}
	proj := (Provider{}).Detect(m.Env(), agent.Project)
	if len(proj) != 1 || proj[0].Paths.MCPs != m.Path("proj/.pi/mcp.json") || proj[0].Paths.Skills != m.Path("proj/.pi/skills") {
		t.Fatalf("project: %+v", proj)
	}
}

func TestDetectAgentDir(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["PI_CODING_AGENT_DIR"] = m.Mkdir("pi-agent")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("pi-agent/mcp.json") {
		t.Fatalf("%+v", got)
	}
}
