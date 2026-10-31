package openclaw

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
	if got := (Provider{}).Detect(m.Env(), agent.Project); len(got) != 0 {
		t.Errorf("it should not have a project target: %+v", got)
	}
}

func TestDetectGlobal(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["openclaw"] = "1.0.0"
	m.File("home/.openclaw/openclaw.json", "{\n  mcp: { servers: {} },\n}\n")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if got[0].Paths.MCPs != m.Path("home/.openclaw/openclaw.json") || got[0].Paths.Skills != m.Path("home/.openclaw/skills") {
		t.Errorf("paths: %+v", got[0].Paths)
	}
	if got[0].Paths.Context != m.Path("home/.openclaw/workspace/AGENTS.md") {
		t.Errorf("context: %s", got[0].Paths.Context)
	}
}

func TestDetectEnvPaths(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["OPENCLAW_STATE_DIR"] = m.Mkdir("state")
	m.Vars["OPENCLAW_CONFIG_PATH"] = m.File("custom.json5", "{}\n")
	m.Vars["OPENCLAW_WORKSPACE_DIR"] = m.Mkdir("ws")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("custom.json5") || got[0].Paths.Skills != m.Path("state/skills") {
		t.Fatalf("%+v", got)
	}
	if got[0].Paths.Context != m.Path("ws/AGENTS.md") {
		t.Errorf("context: %s", got[0].Paths.Context)
	}
}
