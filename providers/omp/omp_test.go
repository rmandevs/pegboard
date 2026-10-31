package omp

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

func TestDetectGlobalProfilesAndProject(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["omp"] = "18.14.1"
	m.File("home/.omp/agent/config.yml", "")
	m.Mkdir("home/.omp/profiles/work/agent")
	m.Mkdir("home/.omp/profiles/home/agent")
	m.File("home/.omp/profiles/notes.txt", "not a profile")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	var ids []string
	for _, t := range got {
		ids = append(ids, t.ID())
	}
	want := []string{"omp", "omp:home", "omp:work"}
	if len(ids) != len(want) {
		t.Fatalf("targets %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("targets %v, want %v", ids, want)
		}
	}
	if got[0].Paths.MCPs != m.Path("home/.omp/agent/mcp.json") || got[2].Paths.MCPs != m.Path("home/.omp/profiles/work/agent/mcp.json") {
		t.Errorf("paths: %+v", got)
	}

	proj := (Provider{}).Detect(m.Env(), agent.Project)
	if len(proj) != 1 || proj[0].Paths.MCPs != m.Path("proj/.omp/mcp.json") || proj[0].Paths.Skills != m.Path("proj/.omp/skills") {
		t.Fatalf("project: %+v", proj)
	}
}

func TestDetectConfigDir(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["PI_CONFIG_DIR"] = m.Mkdir("cfg")
	m.Mkdir("cfg/agent")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.MCPs != m.Path("cfg/agent/mcp.json") {
		t.Fatalf("%+v", got)
	}
}
