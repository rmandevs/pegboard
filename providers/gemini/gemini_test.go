package gemini

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
	m.Bins["gemini"] = "0.30.0"
	m.File("home/.gemini/settings.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("home/.gemini/settings.json") || got[0].Paths.Skills != m.Path("home/.gemini/skills") {
		t.Fatalf("global: %+v", got)
	}
	m.File("proj/GEMINI.md", "# x")
	proj := (Provider{}).Detect(m.Env(), agent.Project)
	if len(proj) != 1 || proj[0].Paths.MCPs != m.Path("proj/.gemini/settings.json") || proj[0].Paths.Context != m.Path("proj/GEMINI.md") {
		t.Fatalf("project: %+v", proj)
	}
}

func TestDetectHome(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["GEMINI_CLI_HOME"] = m.Mkdir("gh")
	m.Mkdir("gh/.gemini")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("gh/.gemini/settings.json") {
		t.Fatalf("%+v", got)
	}
}
