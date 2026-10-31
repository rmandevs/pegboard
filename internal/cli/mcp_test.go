package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
)

const foreignClaude = `{
  "numStartups": 7,
  "mcpServers": {
    "mine": {
      "command": "uvx",
      "args": ["mine"]
    }
  },
  "projects": {}
}
`

const foreignCodex = `# Codex config
model = "o4"

[mcp_servers.mine]
command = "uvx"
`

// fleet is a machine with all four agents, unmanaged configs, and a Hermes
// profile.
func fleet(t *testing.T) *agenttest.Machine {
	m := agenttest.New(t)
	for _, b := range []string{"claude", "codex", "opencode", "hermes"} {
		m.Bins[b] = "1.0.0"
	}
	m.File("home/.claude.json", foreignClaude)
	m.File("home/.codex/config.toml", foreignCodex)
	m.Mkdir("home/.config/opencode")
	m.File("home/.hermes/config.yaml", "# hermes\nmodel: x\n")
	m.Mkdir("home/.hermes/profiles/work")
	m.Vars["GITHUB_TOKEN"] = "ghp_supersecret"
	return m
}

const globalStack = `version: 1
mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
  notion:
    url: https://mcp.notion.com/mcp
    agents: [claude-code, hermes:work]
`

func read(t *testing.T, m *agenttest.Machine, rel string) string {
	t.Helper()
	b, err := os.ReadFile(m.Path(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", what, w, got)
		}
	}
}

func mustRun(t *testing.T, m *agenttest.Machine, want int, args ...string) string {
	t.Helper()
	out, errOut, code := run(m, args...)
	if code != want {
		t.Fatalf("%v: code %d (expected %d)\nstdout:\n%s\nstderr:\n%s", args, code, want, out, errOut)
	}
	return out + errOut
}

func TestDeclarativeCycle(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)

	out := mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "plan", out,
		"claude-code · global\n  + mcp github  (~/.claude.json)\n  + mcp notion  (~/.claude.json)",
		"codex · global\n  + mcp github  (~/.codex/config.toml)",
		"hermes:work · global",
		"⚠ secret in plain text at /env/GITHUB_TOKEN",
		"Plan: 7 create, 0 update, 0 remove, 0 conflicts.")
	if strings.Contains(out, "codex · global\n  + mcp notion") {
		t.Error("notion should not go to codex")
	}
	if strings.Contains(out, "ghp_supersecret") {
		t.Error("the plan must not show secrets")
	}

	// Without -y it asks; "n" cancels without writing.
	stdin = "n\n"
	out = mustRun(t, m, 0, "apply")
	stdin = ""
	mustContain(t, "cancel", out, "Apply? [y/N]", "Canceled")
	if read(t, m, "home/.claude.json") != foreignClaude {
		t.Fatal("canceling should not write")
	}

	out = mustRun(t, m, 0, "apply", "-y")
	mustContain(t, "apply", out, "Wrote ~/.claude.json (backup: ~/.local/state/pegboard/backups/20261019T120000Z/")

	claude := read(t, m, "home/.claude.json")
	mustContain(t, ".claude.json", claude, `"numStartups": 7`, `"mine": {`, `"GITHUB_TOKEN": "${GITHUB_TOKEN}"`,
		`"notion": {`, `"type": "http"`)
	codex := read(t, m, "home/.codex/config.toml")
	mustContain(t, "config.toml", codex, "# Codex config", "[mcp_servers.mine]",
		"[mcp_servers.github]\ncommand = \"npx\"\nargs = [\"-y\", \"@modelcontextprotocol/server-github\"]\nenv_vars = [\"GITHUB_TOKEN\"]\n")
	mustContain(t, "opencode.json", read(t, m, "home/.config/opencode/opencode.json"),
		`"$schema": "https://opencode.ai/config.json"`, `"type": "local"`, `"GITHUB_TOKEN": "{env:GITHUB_TOKEN}"`)
	mustContain(t, "hermes", read(t, m, "home/.hermes/config.yaml"), "# hermes", "GITHUB_TOKEN: ghp_supersecret")
	mustContain(t, "hermes:work", read(t, m, "home/.hermes/profiles/work/config.yaml"), "notion:", "url: https://mcp.notion.com/mcp")

	st := read(t, m, "home/.local/state/pegboard/state.json")
	if strings.Contains(st, "ghp_supersecret") {
		t.Fatal("state must not contain secrets")
	}
	mustContain(t, "state", st, `"origin": "declared"`, `"/env/GITHUB_TOKEN"`)
	if b := read(t, m, "home/.local/state/pegboard/backups/20261019T120000Z/"+
		strings.TrimLeft(strings.NewReplacer(string(filepath.Separator), "%", ":", "%").Replace(m.Path("home/.claude.json")), "%")); b != foreignClaude {
		t.Errorf("the backup should be the original:\n%s", b)
	}

	// Idempotent.
	out = mustRun(t, m, 0, "plan", "--exit-code")
	mustContain(t, "second plan", out, "No changes")
	mustRun(t, m, 0, "drift", "--exit-code")

	// Rotating the secret rewrites only where it is in plain text.
	m.Vars["GITHUB_TOKEN"] = "ghp_new"
	out = mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "rotation", out, "hermes · global\n  ~ mcp github", "hermes:work · global\n  ~ mcp github", "Plan: 0 create, 2 update")
	m.Vars["GITHUB_TOKEN"] = "ghp_supersecret"

	// Changing the YAML updates and removes.
	m.File("home/.config/pegboard/pegboard.yaml", `version: 1
mcps:
  github:
    command: npx
    args: ["@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
`)
	out = mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "change", out, "~ mcp github", "- mcp notion", "Plan: 0 create, 5 update, 2 remove")
	mustRun(t, m, 0, "apply", "-y")
	if c := read(t, m, "home/.claude.json"); strings.Contains(c, "notion") || !strings.Contains(c, `"mine"`) {
		t.Fatalf("removing notion should preserve unmanaged content:\n%s", c)
	}
}

func TestConflictsAndDrift(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	mustRun(t, m, 0, "apply", "-y", "--agent", "claude-code")

	// Manual edit: conflict in plan, apply stops, drift sees it.
	c := read(t, m, "home/.claude.json")
	m.File("home/.claude.json", strings.Replace(c, "server-github", "server-gitlab", 1))
	out := mustRun(t, m, 2, "plan", "--exit-code", "--agent", "claude-code")
	mustContain(t, "conflict", out, "! mcp github  edited manually since the last apply", "1 conflict")
	out = mustRun(t, m, 1, "apply", "-y", "--agent", "claude-code")
	mustContain(t, "blocked apply", out, "there are conflicts")
	out = mustRun(t, m, 2, "drift", "--exit-code")
	mustContain(t, "drift", out, "claude-code · global  mcp github  edited manually", "Drift: 1 entry.")

	out = mustRun(t, m, 0, "apply", "-y", "--force", "--agent", "claude-code")
	mustContain(t, "force", out, "(--force: overwrite)")
	mustRun(t, m, 0, "drift", "--exit-code")

	// Manual deletion: it is recreated.
	m.File("home/.claude.json", foreignClaude)
	out = mustRun(t, m, 2, "drift", "--exit-code")
	mustContain(t, "deleted", out, "deleted manually")
	out = mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "recreate", out, "+ mcp github  deleted manually; recreating")
}

func TestForeignSameName(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nmcps:\n  mine:\n    command: npx\n")
	out := mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "foreign", out, "! mcp mine  already exists and was not created by pegboard")

	// If it is already identical, it is adopted without writing.
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nmcps:\n  mine:\n    command: uvx\n    args: [mine]\n")
	out = mustRun(t, m, 0, "apply", "-y", "--agent", "claude-code")
	mustContain(t, "adopt", out, "= mcp mine  already in place; pegboard records it", "1 adopt")
	if read(t, m, "home/.claude.json") != foreignClaude {
		t.Fatal("adopting should not rewrite the file")
	}
	mustContain(t, "second plan", mustRun(t, m, 0, "plan", "--agent", "claude-code"), "No changes")
}

func TestCodexInlineIsConflict(t *testing.T) {
	m := fleet(t)
	m.File("home/.codex/config.toml", "[mcp_servers]\ngithub = { command = \"x\" }\n")
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	out := mustRun(t, m, 0, "plan", "--agent", "codex", "--force")
	mustContain(t, "inline", out, "! mcp github", "is not written as a")
}

func TestMissingSecret(t *testing.T) {
	m := fleet(t)
	delete(m.Vars, "GITHUB_TOKEN")
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	// Claude Code, Codex, and opencode keep it as a reference: no value
	// needed.
	mustRun(t, m, 0, "plan", "--agent", "claude-code", "--agent", "codex", "--agent", "opencode")
	out := mustRun(t, m, 1, "plan", "--agent", "hermes")
	mustContain(t, "missing", out, "variable GITHUB_TOKEN is not defined")
}

func TestPolicyAndPhases(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack+"policy:\n  allow_mcps: [github]\n")
	out := mustRun(t, m, 1, "plan")
	mustContain(t, "policy", out, "policy.allow_mcps does not allow: notion")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [github.com/x/y]\n")
	out = mustRun(t, m, 0, "plan")
	mustContain(t, "phase 3", out, "warning:", "include arrives in phase 3")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nmcps:\n  x:\n    command: \"${op://v/i}\"\n")
	mustContain(t, "phase 3", mustRun(t, m, 1, "plan"), "phase 3")
}

func TestPlanJSON(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	out := mustRun(t, m, 0, "plan", "--json", "--agent", "hermes")
	var p struct {
		Changes []struct {
			Target, Name, Action, File string
			Warnings                   []string
		}
		Summary map[string]int
		Blocked bool
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if p.Summary["create"] != 3 || p.Blocked || len(p.Changes) != 3 {
		t.Fatalf("%+v", p)
	}
	if p.Changes[0].Target != "hermes" || p.Changes[0].Action != "create" || len(p.Changes[0].Warnings) != 1 {
		t.Errorf("%+v", p.Changes[0])
	}
	if _, _, code := run(m, "apply", "--json"); code != 1 {
		t.Error("apply --json without -y should fail")
	}
	out = mustRun(t, m, 0, "apply", "--json", "-y", "--agent", "hermes")
	mustContain(t, "apply json", out, `"applied": true`, `"written": [`)
}

func TestNoStack(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 1, "plan")
	mustContain(t, "without stack", out, "pegboard init")
}

func TestInit(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 0, "init")
	mustContain(t, "init", out, "Created pegboard.yaml (agents: claude-code, codex, opencode, hermes)")
	mustContain(t, "content", read(t, m, "proj/pegboard.yaml"), "version: 1\nagents: [claude-code, codex, opencode, hermes]\n", "mcps: {}")
	mustRun(t, m, 1, "init")
	out = mustRun(t, m, 0, "plan")
	mustContain(t, "empty plan", out, "No changes")
	mustContain(t, "global init", mustRun(t, m, 0, "init", "--scope", "global"), "hermes")
}

func TestAdhoc(t *testing.T) {
	m := fleet(t)
	m.Mkdir("proj/.git") // rule 5: there is a repo → project scope
	out := mustRun(t, m, 0, "install", "mcp", "fs=npx -y '@mcp/fs' /tmp", "mcp", "docs=https://docs.example/mcp",
		"--header", "X-Key=${env:DOCS_KEY}", "--agent", "claude-code", "--agent", "opencode", "-y")
	mustContain(t, "install", out, "claude-code · project\n  + mcp docs", "[ad-hoc]", "Wrote .mcp.json", "Wrote opencode.json")
	mustContain(t, ".mcp.json", read(t, m, "proj/.mcp.json"), `"@mcp/fs"`, `"X-Key": "${DOCS_KEY}"`)
	mustContain(t, "opencode", read(t, m, "proj/opencode.json"), `"command": [`, `"X-Key": "{env:DOCS_KEY}"`)
	mustContain(t, "gitignore", read(t, m, "proj/.pegboard/.gitignore"), "*")
	mustContain(t, "state", read(t, m, "proj/.pegboard/state.json"), `"origin": "adhoc"`)
	if agentExists(m.Path("proj/pegboard.yaml")) {
		t.Fatal("install without --save must not create pegboard.yaml")
	}

	out = mustRun(t, m, 0, "drift", "--scope", "project")
	mustContain(t, "ad-hoc drift", out, "Ad-hoc, not declared:", "No drift.")
	mustRun(t, m, 2, "drift", "--scope", "project", "--strict", "--exit-code")

	// A project pegboard.yaml does not remove ad-hoc entries, except with
	// --prune-adhoc.
	m.File("proj/pegboard.yaml", "version: 1\n")
	mustContain(t, "apply does not prune", mustRun(t, m, 0, "plan", "--scope", "project"), "No changes")
	mustContain(t, "prune", mustRun(t, m, 0, "plan", "--scope", "project", "--prune-adhoc"), "- mcp fs", "Plan: 0 create, 0 update, 4 remove")

	out = mustRun(t, m, 0, "uninstall", "mcp", "fs", "-y")
	mustContain(t, "uninstall", out, "- mcp fs")
	if strings.Contains(read(t, m, "proj/.mcp.json"), "@mcp/fs") {
		t.Fatal("uninstall did not remove fs")
	}
	mustContain(t, "uninstall again", mustRun(t, m, 1, "uninstall", "mcp", "fs", "-y"), "pegboard did not install")
}

func TestInstallSave(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 0, "install", "mcp", "gh=npx srv", "--env", "TOKEN=${env:GITHUB_TOKEN}",
		"--scope", "global", "--agent", "codex", "--save", "-y")
	mustContain(t, "save", out, "Saved to ~/.config/pegboard/pegboard.yaml")
	mustContain(t, "yaml", read(t, m, "home/.config/pegboard/pegboard.yaml"),
		"version: 1", "gh:\n    command: npx\n    args:\n      - srv\n    env:\n      TOKEN: ${env:GITHUB_TOKEN}\n    agents:\n      - codex")
	// TOKEN≠GITHUB_TOKEN: Codex cannot forward it, so it stays in plain text.
	mustContain(t, "codex", read(t, m, "home/.codex/config.toml"), `env = { TOKEN = "ghp_supersecret" }`)
	mustContain(t, "declared", read(t, m, "home/.local/state/pegboard/state.json"), `"origin": "declared"`)
	mustContain(t, "plan after save", mustRun(t, m, 0, "plan", "--scope", "global"), "No changes")
	mustContain(t, "uninstall declared", mustRun(t, m, 1, "uninstall", "mcp", "gh", "--scope", "global"), "is declared")
}

func TestInstallParse(t *testing.T) {
	m := fleet(t)
	for args, want := range map[string]string{
		"install skill":                     "requires a source",
		"install skill invalid":             "unrecognized",
		"install github.com/x/stacks//web":  "phase 3",
		"install mcp":                       "name=command",
		"install mcp a=x --header H=1":      "--header only applies",
		"install mcp a=https://x --env K=1": "--env only applies",
		"install mcp a=\"x":                 "quotes",
		"install mcp a@b=x":                 "invalid",
		"install mcp a=x mcp a=y":           "repeated",
	} {
		_, errOut, code := run(m, strings.Fields(args)...)
		if code != 1 || !strings.Contains(errOut, want) {
			t.Errorf("%s: code=%d err=%q (expected %q)", args, code, errOut, want)
		}
	}
	words, err := splitWords(`npx -y "a b" 'c d' e\ f`)
	if err != nil || strings.Join(words, "|") != "npx|-y|a b|c d|e f" {
		t.Errorf("splitWords: %q %v", words, err)
	}
}

func TestImport(t *testing.T) {
	m := fleet(t)
	m.File("home/.claude.json", `{
  "mcpServers": {
    "linear": {"type": "http", "url": "https://mcp.linear.app/mcp", "headers": {"Authorization": "Bearer lin_secret"}},
    "fs": {"type": "stdio", "command": "npx", "args": ["fs"], "env": {"API_KEY": "k_secret"}}
  }
}
`)
	m.File("home/.codex/config.toml", "[mcp_servers.fs]\ncommand = \"npx\"\nargs = [\"fs\"]\nenv = { API_KEY = \"k_secret\" }\n")
	out := mustRun(t, m, 0, "import", "--scope", "global")
	mustContain(t, "import", out, "Imported into ~/.config/pegboard/pegboard.yaml:",
		"+ mcp fs (claude-code, codex)", "+ mcp linear (claude-code)", "export: API_KEY, LINEAR_TOKEN")
	y := read(t, m, "home/.config/pegboard/pegboard.yaml")
	mustContain(t, "yaml", y, "API_KEY: ${env:API_KEY}", "Authorization: Bearer ${env:LINEAR_TOKEN}", "agents:\n      - claude-code")
	if strings.Contains(y, "k_secret") || strings.Contains(y, "lin_secret") {
		t.Fatalf("import must not copy secrets into the YAML:\n%s", y)
	}
	if st := read(t, m, "home/.local/state/pegboard/state.json"); strings.Contains(st, "k_secret") || strings.Contains(st, "lin_secret") {
		t.Fatal("nor into state")
	}
	// What was imported is now pegboard's: the plan converts it to native
	// references.
	m.Vars["API_KEY"] = "k_secret"
	out = mustRun(t, m, 0, "plan", "--scope", "global", "--agent", "claude-code", "--agent", "codex")
	mustContain(t, "plan after import", out, "claude-code · global\n  ~ mcp fs", "~ mcp linear", "codex · global\n  ~ mcp fs")
	if strings.Contains(out, "!") {
		t.Errorf("what was imported should not conflict:\n%s", out)
	}
	mustContain(t, "import again", mustRun(t, m, 0, "import", "--scope", "global"), "No new MCPs")
}

func agentExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestOpenClawJSON5(t *testing.T) {
	m := agenttest.New(t)
	m.Bins["openclaw"] = "1.0.0"
	m.File("home/.openclaw/openclaw.json", `{
  // keep this comment
  channels: { discord: { enabled: true } },
}
`)
	m.File("home/.config/pegboard/pegboard.yaml", `version: 1
agents: [openclaw]
mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
`)
	m.Vars["GITHUB_TOKEN"] = "ghp_secret"

	out := mustRun(t, m, 0, "apply", "-y")
	mustContain(t, "openclaw apply", out, "openclaw · global\n  + mcp github  (~/.openclaw/openclaw.json)")
	cfg := read(t, m, "home/.openclaw/openclaw.json")
	mustContain(t, "openclaw config", cfg, "// keep this comment", "channels", `"mcp"`, `"servers"`, `"github"`, `"GITHUB_TOKEN": "${GITHUB_TOKEN}"`)
	if strings.Contains(cfg, "ghp_secret") {
		t.Fatal("the config must not contain the secret value")
	}
	mustContain(t, "idempotent", mustRun(t, m, 0, "plan", "--exit-code"), "No changes")
	mustRun(t, m, 0, "drift", "--exit-code")
}
