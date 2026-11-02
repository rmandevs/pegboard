package opencode

import (
	"testing"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/agent/agenttest"
)

func TestDetectNothing(t *testing.T) {
	m := agenttest.New(t)
	if got := (Provider{}).Detect(m.Env(), agent.Project); len(got) != 0 {
		t.Errorf("it should not detect anything: %+v", got)
	}
}

func TestDetectGlobal(t *testing.T) {
	m := agenttest.New(t)
	m.File("home/.config/opencode/opencode.json", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Binary != "" {
		t.Fatalf("%+v", got)
	}
	if got[0].Paths.MCPs != m.Path("home/.config/opencode/opencode.json") {
		t.Errorf("paths: %+v", got[0].Paths)
	}
}

func TestDetectXDG(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["XDG_CONFIG_HOME"] = m.Path("xdg")
	m.Mkdir("xdg/opencode")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.Skills != m.Path("xdg/opencode/skills") {
		t.Fatalf("%+v", got)
	}
}

func TestDetectProject(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["opencode"] = "1.0.0"
	m.File("proj/opencode.jsonc", "{}")
	got := (Provider{}).Detect(m.Env(), agent.Project)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("proj/opencode.json") || got[0].Paths.Skills != m.Path("proj/.agents/skills") {
		t.Fatalf("%+v", got)
	}
	if len(got[0].Evidence) != 1 || got[0].Evidence[0] != "opencode.jsonc" {
		t.Errorf("evidence: %v", got[0].Evidence)
	}
}
