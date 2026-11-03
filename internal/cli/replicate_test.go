package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmandevs/pegboard/internal/agent/agenttest"
)

const claudeWithTools = `{
  "theme": "dark",
  "mcpServers": {
    "linear": {"type": "http", "url": "https://mcp.linear.app/mcp", "headers": {"Authorization": "Bearer lin_secret"}},
    "fs": {"type": "stdio", "command": "npx", "args": ["fs"], "env": {"API_KEY": "k_secret"}}
  }
}
`

const mineSkill = "---\nname: mine\ndescription: local\n---\nbody\n"

func replicaFleet(t *testing.T) *agenttest.Machine {
	t.Helper()
	m := fleet(t)
	m.File("home/.claude.json", claudeWithTools)
	m.File("home/.claude/skills/mine/SKILL.md", mineSkill)
	m.File("home/.claude/CLAUDE.md", "Be concise.\n")
	return m
}

func TestReplicate(t *testing.T) {
	m := replicaFleet(t)
	args := []string{"replicate", "claude-code", "codex", "opencode", "--scope", "global"}

	// Declining writes nothing, not even the owned copy of the skill.
	stdin = "s\nn\n"
	out := mustRun(t, m, 0, args...)
	stdin = ""
	mustContain(t, "plan", out,
		"codex · global\n  + mcp fs", "+ mcp linear", "+ skill mine",
		"export: API_KEY, LINEAR_TOKEN", "~/.claude/CLAUDE.md was not copied", "Canceled")
	if strings.Contains(out, "claude-code · global\n") {
		t.Errorf("the source must not be in the plan:\n%s", out)
	}
	if agentExists(m.Path("home/.config/pegboard/skills/mine")) {
		t.Fatal("declining must not copy the skill")
	}
	codexBefore := read(t, m, "home/.codex/config.toml")

	// --dry-run shows the plan as JSON, for agents, and writes nothing either.
	out = mustRun(t, m, 0, append(args, "--dry-run", "--json")...)
	mustContain(t, "dry run", out, `"action": "create"`, `"name": "mine"`)
	if read(t, m, "home/.codex/config.toml") != codexBefore || agentExists(m.Path("home/.config/pegboard/skills/mine")) {
		t.Fatal("--dry-run must not write")
	}

	out = mustRun(t, m, 0, append(args, "-y")...)
	if got := read(t, m, "home/.claude.json"); got != claudeWithTools {
		t.Errorf("the source must not change:\n%s", got)
	}
	if info, err := os.Lstat(m.Path("home/.claude/skills/mine")); err != nil || !info.IsDir() {
		t.Errorf("the source's skill must stay a real directory: %v", err)
	}
	codex := read(t, m, "home/.codex/config.toml")
	mustContain(t, "codex", codex, "[mcp_servers.fs]", `env_vars = ["API_KEY"]`, `bearer_token_env_var = "LINEAR_TOKEN"`)
	if !strings.Contains(codex, strings.TrimSpace(codexBefore)) {
		t.Errorf("codex's own config must stay:\n%s", codex)
	}
	opencode := read(t, m, "home/.config/opencode/opencode.json")
	mustContain(t, "opencode", opencode, "{env:API_KEY}", "Bearer {env:LINEAR_TOKEN}")
	for _, f := range []string{codex, opencode, read(t, m, "home/.local/state/pegboard/state.json"), out} {
		if strings.Contains(f, "k_secret") || strings.Contains(f, "lin_secret") {
			t.Fatalf("secrets must not be copied:\n%s", f)
		}
	}
	owned := m.Path("home/.config/pegboard/skills/mine")
	if linkOf(t, m, "home/.agents/skills/mine") != owned || linkOf(t, m, "home/.config/opencode/skills/mine") != owned {
		t.Error("the targets must link to the owned copy")
	}
	if agentExists(m.Path("home/.hermes/skills/mine")) {
		t.Error("an agent that was not named must not change")
	}

	// A second run has nothing to do.
	out = mustRun(t, m, 0, append(args, "-y")...)
	if strings.Contains(out, "+ ") || strings.Contains(out, "~ ") {
		t.Errorf("replicate must be idempotent:\n%s", out)
	}

	// What it installed is ad-hoc, so adopt can move it into pegboard.yaml.
	out = mustRun(t, m, 0, "adopt", "--scope", "global")
	mustContain(t, "adopt", out, "mcp fs (codex, opencode)", "skill ~/.config/pegboard/skills/mine")
	y := read(t, m, "home/.config/pegboard/pegboard.yaml")
	mustContain(t, "adopted skill", y, "source: ~/.config/pegboard/skills/mine", "- codex", "- opencode")
	// Adopting must not spread the skill to agents that did not have it.
	out = mustRun(t, m, 0, "plan", "--scope", "global")
	if strings.Contains(out, "skill mine") {
		t.Errorf("adopt spread the skill:\n%s", out)
	}
}

func TestStackSkillAgents(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/skills/mine/SKILL.md", mineSkill)
	m.File("home/.config/pegboard/skills/all/SKILL.md", "---\nname: all\n---\nbody\n")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nskills:\n  - ./skills/all\n  - source: ./skills/mine\n    agents: [codex]\n")
	out := mustRun(t, m, 0, "plan", "--scope", "global")
	mustContain(t, "plan", out, "claude-code · global\n  + skill all", "(~/.agents/skills/mine")
	if strings.Contains(out, "~/.claude/skills/mine") {
		t.Errorf("the skill is limited to codex:\n%s", out)
	}
	// Editing the list keeps the long form.
	mustRun(t, m, 0, "add", "skill", "./skills/other", "--scope", "global", "--agent", "opencode")
	y := read(t, m, "home/.config/pegboard/pegboard.yaml")
	mustContain(t, "edit", y, "source: ./skills/mine", "source: ./skills/other", "- ./skills/all")
	mustRun(t, m, 0, "remove", "skill", "other", "--scope", "global")
	y = read(t, m, "home/.config/pegboard/pegboard.yaml")
	mustContain(t, "remove", y, "source: ./skills/mine")
	if strings.Contains(y, "other") {
		t.Errorf("not removed:\n%s", y)
	}
}

func TestDryRunOnAdhocCommands(t *testing.T) {
	m := fleet(t)
	before := read(t, m, "home/.claude.json")
	args := []string{"install", "mcp", "notion=https://mcp.notion.com/mcp", "--scope", "global"}
	out := mustRun(t, m, 0, append(args, "--dry-run", "--json")...)
	mustContain(t, "install", out, `"action": "create"`, `"name": "notion"`)
	if read(t, m, "home/.claude.json") != before {
		t.Fatal("install --dry-run must not write")
	}
	mustRun(t, m, 0, append(args, "-y")...)
	after := read(t, m, "home/.claude.json")
	out = mustRun(t, m, 0, "uninstall", "mcp", "notion", "--scope", "global", "--dry-run", "--json")
	mustContain(t, "uninstall", out, `"action": "delete"`)
	if read(t, m, "home/.claude.json") != after {
		t.Fatal("uninstall --dry-run must not write")
	}
}

// Any detected agent can be the source, not only Claude Code.
func TestReplicateFromAnyAgent(t *testing.T) {
	m := fleet(t)
	m.File("home/.codex/config.toml", "[mcp_servers.fs]\ncommand = \"npx\"\nargs = [\"fs\"]\nenv = { API_KEY = \"k_secret\" }\n")
	m.File("home/.agents/skills/mine/SKILL.md", mineSkill)
	m.File("home/.codex/AGENTS.md", "From codex.\n")
	mustRun(t, m, 0, "replicate", "codex", "claude-code", "--scope", "global", "-y", "--context", "after")
	claude := read(t, m, "home/.claude.json")
	mustContain(t, "claude", claude, `"fs"`, "${API_KEY}")
	if strings.Contains(claude, "k_secret") {
		t.Fatal("the secret must not be copied")
	}
	if linkOf(t, m, "home/.claude/skills/mine") != m.Path("home/.config/pegboard/skills/mine") {
		t.Error("the skill must reach claude-code")
	}
	mustContain(t, "context", read(t, m, "home/.claude/CLAUDE.md"), "pegboard:replicated from codex", "From codex.")
}

func TestReplicateContext(t *testing.T) {
	const mine = "Mine.\n"
	setup := func(t *testing.T) *agenttest.Machine {
		m := replicaFleet(t)
		m.File("home/.codex/AGENTS.md", mine)
		return m
	}
	args := []string{"replicate", "claude-code", "codex", "--scope", "global", "-y", "--context"}
	file := "home/.codex/AGENTS.md"
	block := "<!-- pegboard:replicated from claude-code -->\n\nBe concise.\n\n<!-- /pegboard:replicated from claude-code -->"

	t.Run("after", func(t *testing.T) {
		m := setup(t)
		out := mustRun(t, m, 0, append(args, "after")...)
		mustContain(t, "plan", out, "~ context AGENTS.md", "← claude-code, after", "backup:")
		want := "Mine.\n\n" + block + "\n"
		if got := read(t, m, file); got != want {
			t.Fatalf("got:\n%s", got)
		}
		// Running it again changes nothing, and the source's edits update the block.
		mustRun(t, m, 0, append(args, "after")...)
		if got := read(t, m, file); got != want {
			t.Fatalf("not idempotent:\n%s", got)
		}
		m.File("home/.claude/CLAUDE.md", "Be brief.\n")
		mustRun(t, m, 0, append(args, "after")...)
		if got := read(t, m, file); got != strings.ReplaceAll(want, "Be concise.", "Be brief.") {
			t.Fatalf("not updated:\n%s", got)
		}
	})
	t.Run("before", func(t *testing.T) {
		m := setup(t)
		mustRun(t, m, 0, append(args, "before")...)
		if got := read(t, m, file); got != block+"\n\nMine.\n" {
			t.Fatalf("got:\n%s", got)
		}
	})
	t.Run("replace keeps a backup", func(t *testing.T) {
		m := setup(t)
		out := mustRun(t, m, 0, append(args, "replace")...)
		if got := read(t, m, file); got != "Be concise.\n" {
			t.Fatalf("got:\n%s", got)
		}
		_, after, ok := strings.Cut(out, "AGENTS.md (backup: ~/")
		if !ok {
			t.Fatalf("no backup:\n%s", out)
		}
		rel, _, _ := strings.Cut(after, ")")
		if got := read(t, m, "home/"+rel); got != mine {
			t.Fatalf("backup has %q", got)
		}
	})
	t.Run("asks, and shares stdin with the confirmation", func(t *testing.T) {
		m := setup(t)
		stdin = "a\ny\n"
		out := mustRun(t, m, 0, "replicate", "claude-code", "codex", "--scope", "global")
		stdin = ""
		mustContain(t, "prompt", out, "[s]kip, [b]efore", "Apply? [y/N]")
		mustContain(t, "file", read(t, m, file), "Mine.", "Be concise.")
	})
	t.Run("-y does not touch it without the flag", func(t *testing.T) {
		m := setup(t)
		out := mustRun(t, m, 0, "replicate", "claude-code", "codex", "--scope", "global", "-y")
		mustContain(t, "warn", out, "was not copied; use --context")
		if read(t, m, file) != mine {
			t.Fatal("the file must not change")
		}
	})
	t.Run("the bridge line stays out", func(t *testing.T) {
		m := setup(t)
		m.File("home/.claude/CLAUDE.md", "@AGENTS.md\n\nBe concise.\n")
		mustRun(t, m, 0, append(args, "replace")...)
		if got := read(t, m, file); got != "Be concise.\n" {
			t.Fatalf("got:\n%s", got)
		}
	})
	t.Run("a file linked to the source is left alone", func(t *testing.T) {
		m := replicaFleet(t)
		m.Mkdir("home/.codex")
		if err := os.Symlink(m.Path("home/.claude/CLAUDE.md"), m.Path("home/.codex/AGENTS.md")); err != nil {
			t.Skip(err)
		}
		mustRun(t, m, 0, append(args, "replace")...)
		if got := read(t, m, "home/.claude/CLAUDE.md"); got != "Be concise.\n" {
			t.Fatalf("got:\n%s", got)
		}
		linkOf(t, m, file)
	})
	t.Run("hermes is left out", func(t *testing.T) {
		m := replicaFleet(t)
		m.File("home/.hermes/SOUL.md", "Persona.\n")
		m.Vars["API_KEY"], m.Vars["LINEAR_TOKEN"] = "k", "l"
		mustRun(t, m, 0, "replicate", "claude-code", "hermes", "--scope", "global", "-y", "--context", "replace")
		if read(t, m, "home/.hermes/SOUL.md") != "Persona.\n" {
			t.Fatal("SOUL.md must not change")
		}
	})
}

func TestReplicateConflict(t *testing.T) {
	m := replicaFleet(t)
	m.File("home/.gemini/settings.json", `{"mcpServers": {"fs": {"command": "other"}}}`)
	m.Bins["gemini"] = "1.0.0"
	before := read(t, m, "home/.gemini/settings.json")
	mustRun(t, m, 1, "replicate", "claude-code", "gemini", "--scope", "global", "-y")
	if read(t, m, "home/.gemini/settings.json") != before {
		t.Error("a conflict must stop replicate")
	}
	mustRun(t, m, 0, "replicate", "claude-code", "gemini", "--scope", "global", "-y", "--force")
	mustContain(t, "forced", read(t, m, "home/.gemini/settings.json"), "API_KEY")
}

func TestReplicateLinkedSkill(t *testing.T) {
	m := replicaFleet(t)
	m.File("home/dev/live/SKILL.md", "---\nname: live\n---\nbody\n")
	if err := os.Symlink(m.Path("home/dev/live"), m.Path("home/.claude/skills/live")); err != nil {
		t.Skip(err)
	}
	mustRun(t, m, 0, "replicate", "claude-code", "codex", "--scope", "global", "-y")
	if got := linkOf(t, m, "home/.agents/skills/live"); got != filepath.Clean(m.Path("home/dev/live")) {
		t.Errorf("a link to the user's own directory must stay live, got %s", got)
	}
}

func TestReplicateArguments(t *testing.T) {
	m := replicaFleet(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"replicate", "nope", "codex"}, `unknown agent "nope"`},
		{[]string{"replicate", "codex", "codex"}, "both the source and a target"},
		{[]string{"replicate", "claude-code", "cursor", "--scope", "global"}, "cursor was not detected"},
		{[]string{"replicate", "claude-code", "codex", "--agent", "cursor"}, "--agent does not apply"},
	} {
		mustContain(t, tc.args[1], mustRun(t, m, 1, tc.args...), tc.want)
	}
}
