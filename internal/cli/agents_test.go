package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
)

// stdin es lo que lee la confirmación de apply; los tests lo cambian.
var stdin = ""

func run(m *agenttest.Machine, args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &out, &errb, m.EnvFunc())
	return out.String(), errb.String(), code
}

func TestAgentsNone(t *testing.T) {
	m := agenttest.New(t)
	out, _, code := run(m, "agents")
	if code != 0 || !strings.Contains(out, "No se detectó") {
		t.Errorf("code=%d out=%q", code, out)
	}
	out, _, _ = run(m, "agents", "--json")
	var got agentsJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Targets == nil || len(got.Targets) != 0 {
		t.Errorf("targets debía ser [] y no null: %s", out)
	}
}

func machine(t *testing.T) *agenttest.Machine {
	m := agenttest.New(t)
	m.Bins["claude"] = "2.1.3"
	m.Bins["hermes"] = ""
	m.Mkdir("home/.hermes/profiles/work")
	m.File("proj/.mcp.json", "{}")
	return m
}

func TestAgentsText(t *testing.T) {
	out, _, code := run(machine(t), "agents")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{
		"claude-code · global  (v2.1.3, provider v1)",
		"mcps:       ~/.claude.json  (no existe)",
		"hermes:work · global  (versión desconocida, provider v1)",
		"claude-code · project",
		"mcps:       .mcp.json\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hermes:work · project") || strings.Contains(out, "codex") {
		t.Errorf("targets de más:\n%s", out)
	}
}

func TestAgentsJSONFilters(t *testing.T) {
	out, _, code := run(machine(t), "agents", "--json", "--scope", "global", "--agent", "hermes")
	if code != 0 {
		t.Fatal(code)
	}
	var got agentsJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 2 || got.Targets[0].ID() != "hermes" || got.Targets[1].ID() != "hermes:work" {
		t.Errorf("%+v", got.Targets)
	}
	if !strings.Contains(out, `"provider_version": 1`) || !strings.Contains(out, `"scope": "global"`) {
		t.Errorf("formato JSON:\n%s", out)
	}
}

func TestAgentsErrors(t *testing.T) {
	m := agenttest.New(t)
	if _, errOut, code := run(m, "agents", "--scope", "todo"); code != 1 || !strings.Contains(errOut, "--scope") {
		t.Errorf("code=%d err=%q", code, errOut)
	}
	if _, errOut, code := run(m, "agents", "--agent", "cursor"); code != 1 || !strings.Contains(errOut, `"cursor"`) {
		t.Errorf("code=%d err=%q", code, errOut)
	}
}
