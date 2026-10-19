package hermes

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

func TestProjectOnlySkills(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["hermes"] = "0.9"
	got := (Provider{}).Detect(m.Env(), agent.Project)
	if len(got) != 1 || got[0].Paths.MCPs != "" || got[0].Paths.Skills != m.Path("proj/.hermes/skills") {
		t.Errorf("en proyecto Hermes solo tiene skills: %+v", got)
	}
}

func TestDetectProfiles(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["hermes"] = "0.9"
	m.File("home/.hermes/config.yaml", "")
	m.Mkdir("home/.hermes/profiles/work")
	m.Mkdir("home/.hermes/profiles/casa")
	m.File("home/.hermes/profiles/notas.txt", "no es perfil")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	var ids []string
	for _, tg := range got {
		ids = append(ids, tg.ID())
	}
	want := []string{"hermes", "hermes:casa", "hermes:work"}
	if len(ids) != len(want) {
		t.Fatalf("targets %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("targets %v, want %v", ids, want)
		}
	}
	if got[2].Paths.MCPs != m.Path("home/.hermes/profiles/work/config.yaml") || got[2].Version != "0.9" {
		t.Errorf("perfil: %+v", got[2])
	}
}

func TestDetectHermesHome(t *testing.T) {
	m := agenttest.New(t)
	m.Vars["HERMES_HOME"] = m.Mkdir("hh")
	got := (Provider{}).Detect(m.Env(), agent.Global)
	if len(got) != 1 || got[0].Paths.Skills != m.Path("hh/skills") {
		t.Fatalf("%+v", got)
	}
}
