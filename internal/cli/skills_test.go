package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmandevs/pegboard/internal/agent/agenttest"
	"github.com/rmandevs/pegboard/internal/skills"
)

// gitHub creates local git repos that stand in for GitHub: acme/skills
// resolves to file://<dir>/acme/skills.git. It returns a function that adds
// commits to a repo.
func gitHub(t *testing.T) func(repo string, files map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	base := t.TempDir()
	prev := skills.GitHub
	skills.GitHub = skills.FileURL(base) + "/"
	t.Cleanup(func() { skills.GitHub = prev })
	return func(repo string, files map[string]string) {
		dir := filepath.Join(base, filepath.FromSlash(repo)+".git")
		g := func(args ...string) {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		if _, err := os.Stat(dir); err != nil {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			g("init", "-q", "-b", "main")
			g("config", "uploadpack.allowAnySHA1InWant", "true")
		}
		for rel, content := range files {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		g("add", "-A")
		g("commit", "-q", "-m", "c")
	}
}

var acmeV1 = map[string]string{
	"skills/tdd/SKILL.md":    "---\nname: tdd\ndescription: tests first\n---\nv1\n",
	"skills/review/SKILL.md": "---\nname: review\ndescription: review\n---\n",
}

func linkOf(t *testing.T, m *agenttest.Machine, rel string) string {
	t.Helper()
	l, err := os.Readlink(m.Path(rel))
	if err != nil {
		t.Fatalf("%s is not a symlink: %v", rel, err)
	}
	return l
}

func TestSkillsDeclarative(t *testing.T) {
	commit := gitHub(t)
	commit("acme/skills", acmeV1)
	m := fleet(t)
	m.File("home/.config/pegboard/skills/mine/SKILL.md", "---\nname: mine\n---\nlocal\n")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nskills:\n  - acme/skills@tdd\n  - ./skills/mine\n")

	out := mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "plan", out,
		"claude-code · global\n  + skill mine  (~/.claude/skills/mine → ~/.config/pegboard/skills/mine)\n  + skill tdd   (~/.claude/skills/tdd → cache@",
		"codex · global\n  + skill mine", "(~/.agents/skills/tdd",
		"opencode · global\n  + skill mine", "(~/.config/opencode/skills/tdd",
		"hermes · global", "hermes:work · global", "(~/.hermes/profiles/work/skills/tdd",
		"Plan: 10 create")
	if agentExists(m.Path("home/.config/pegboard/pegboard.lock")) {
		t.Fatal("plan must not write the lock")
	}
	mustRun(t, m, 0, "apply", "-y")
	tdd := linkOf(t, m, "home/.claude/skills/tdd")
	if !strings.HasPrefix(tdd, m.Path("home/.cache/pegboard/skills/")) {
		t.Fatalf("tdd should point to the cache: %s", tdd)
	}
	if linkOf(t, m, "home/.agents/skills/tdd") != tdd || linkOf(t, m, "home/.hermes/skills/tdd") != tdd {
		t.Error("all agents should share the same copy")
	}
	if b, _ := os.ReadFile(filepath.Join(tdd, "SKILL.md")); !strings.Contains(string(b), "v1") {
		t.Errorf("content: %s", b)
	}
	if linkOf(t, m, "home/.claude/skills/mine") != m.Path("home/.config/pegboard/skills/mine") {
		t.Error("a local skill is linked as-is")
	}
	lock := read(t, m, "home/.config/pegboard/pegboard.lock")
	mustContain(t, "lock", lock, `"acme/skills@tdd"`, `"commit": "`, `"tdd": "sha256:`)
	if strings.Contains(lock, "mine") {
		t.Error("local skills do not go into the lock")
	}
	mustContain(t, "idempotent", mustRun(t, m, 0, "plan", "--exit-code"), "No changes")
	mustContain(t, "drift", mustRun(t, m, 0, "drift", "--exit-code"), "skills · global  skill tdd  ok")

	// The repo moves on: the lock pins it until `update`.
	commit("acme/skills", map[string]string{"skills/tdd/SKILL.md": "---\nname: tdd\n---\nv2\n", "skills/tdd/extra.md": "x"})
	mustContain(t, "pinned", mustRun(t, m, 0, "plan", "--exit-code"), "No changes")
	stdin = "n\n"
	out = mustRun(t, m, 0, "update")
	stdin = ""
	mustContain(t, "update canceled", out, "~ skill tdd (acme/skills@tdd)", ": +extra.md ~SKILL.md", "Canceled")
	if read(t, m, "home/.config/pegboard/pegboard.lock") != lock {
		t.Fatal("canceling should not touch the lock")
	}
	out = mustRun(t, m, 0, "update", "tdd", "-y")
	mustContain(t, "update", out, "Wrote ~/.config/pegboard/pegboard.lock")
	out = mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "plan after update", out, "~ skill tdd  (~/.claude/skills/tdd ", " → ", "Plan: 0 create, 5 update")
	mustRun(t, m, 0, "apply", "-y")
	if b, _ := os.ReadFile(filepath.Join(linkOf(t, m, "home/.claude/skills/tdd"), "SKILL.md")); !strings.Contains(string(b), "v2") {
		t.Errorf("after update+apply it should be v2: %s", b)
	}
	mustContain(t, "up to date", mustRun(t, m, 0, "update", "-y"), "already up to date")

	// Removing it from the stack removes it from agents and the lock.
	mustContain(t, "remove", mustRun(t, m, 0, "remove", "skill", "tdd"), "- skill acme/skills@tdd")
	out = mustRun(t, m, 0, "apply", "-y")
	mustContain(t, "apply remove", out, "- skill tdd")
	if agentExists(m.Path("home/.claude/skills/tdd")) {
		t.Fatal("it did not remove the symlink")
	}
	if strings.Contains(read(t, m, "home/.config/pegboard/pegboard.lock"), "acme") {
		t.Error("the lock should forget the source")
	}
}

func TestSkillsSharedDirAndConflicts(t *testing.T) {
	commit := gitHub(t)
	commit("acme/skills", acmeV1)
	m := fleet(t)
	m.File("proj/pegboard.yaml", "version: 1\nskills: [acme/skills]\n")
	m.File("proj/.claude/skills/review/SKILL.md", "mine, written by hand")
	out := mustRun(t, m, 0, "plan", "--scope", "project")
	mustContain(t, "plan", out,
		"claude-code · project\n  ! skill review  already exists and was not placed by pegboard\n  + skill tdd",
		"codex, opencode · project\n  + skill review  (.agents/skills/review → cache@",
		"hermes · project\n  + skill review  (.hermes/skills/review",
		"1 conflict")
	mustRun(t, m, 1, "apply", "-y", "--scope", "project")
	mustRun(t, m, 0, "apply", "-y", "--scope", "project", "--agent", "codex", "--agent", "opencode")
	if !strings.HasPrefix(linkOf(t, m, "proj/.agents/skills/tdd"), m.Path("home/.cache")) {
		t.Error(".agents/skills should have tdd")
	}
	if read(t, m, "proj/.claude/skills/review/SKILL.md") != "mine, written by hand" {
		t.Fatal("it should not touch the unmanaged skill")
	}

	// Manually changing a managed symlink is drift and a conflict.
	_ = os.Remove(m.Path("proj/.agents/skills/tdd"))
	_ = os.Symlink(m.Path("proj"), m.Path("proj/.agents/skills/tdd"))
	mustContain(t, "drift", mustRun(t, m, 2, "drift", "--scope", "project", "--exit-code"), "skill tdd  edited manually")
	mustContain(t, "conflict", mustRun(t, m, 0, "plan", "--scope", "project", "--agent", "codex"),
		"! skill tdd  changed manually since the last apply")
	mustRun(t, m, 0, "apply", "-y", "--force", "--scope", "project", "--agent", "codex")
	mustRun(t, m, 0, "drift", "--scope", "project", "--exit-code")
}

func TestAgentsMD(t *testing.T) {
	m := fleet(t)
	m.File("proj/CLAUDE.md", "# My project\nUses tabs.")
	m.File("proj/pegboard.yaml", "version: 1\ncontext:\n  agents_md: true\n")
	out := mustRun(t, m, 0, "apply", "-y", "--scope", "project")
	mustContain(t, "apply", out, "agents.md · project\n  + context AGENTS.md", "claude-code · project\n  + context @AGENTS.md in CLAUDE.md")
	if c := read(t, m, "proj/CLAUDE.md"); c != "# My project\nUses tabs.\n\n@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md: %q", c)
	}
	mustContain(t, "AGENTS.md", read(t, m, "proj/AGENTS.md"), "# AGENTS.md")
	mustContain(t, "idempotent", mustRun(t, m, 0, "plan", "--scope", "project"), "No changes")

	m.File("proj/AGENTS.md", "# I wrote this")
	m.File("proj/pegboard.yaml", "version: 1\n")
	out = mustRun(t, m, 0, "apply", "-y", "--scope", "project")
	mustContain(t, "remove bridge", out, "- context @AGENTS.md in CLAUDE.md")
	if c := read(t, m, "proj/CLAUDE.md"); c != "# My project\nUses tabs.\n" {
		t.Fatalf("CLAUDE.md after removal: %q", c)
	}
	if read(t, m, "proj/AGENTS.md") != "# I wrote this" {
		t.Fatal("AGENTS.md is never deleted")
	}

	// A bridge written by the user is not pegboard's: it is not removed.
	m.File("proj/CLAUDE.md", "@AGENTS.md\n")
	m.File("proj/pegboard.yaml", "version: 1\ncontext:\n  agents_md: true\n")
	mustContain(t, "already there", mustRun(t, m, 0, "apply", "-y", "--scope", "project"), "No changes")
	m.File("proj/pegboard.yaml", "version: 1\n")
	mustContain(t, "user's", mustRun(t, m, 0, "plan", "--scope", "project"), "No changes")
}

func TestSkillsAdhocAndAdopt(t *testing.T) {
	commit := gitHub(t)
	commit("acme/skills", acmeV1)
	m := fleet(t)
	m.Mkdir("proj/.git")
	out := mustRun(t, m, 0, "install", "skill", "acme/skills", "mcp", "fs=npx fs", "--agent", "claude-code", "-y")
	mustContain(t, "install", out, "+ skill review", "+ skill tdd", "[ad-hoc]", "+ mcp fs")
	linkOf(t, m, "proj/.claude/skills/tdd")
	if agentExists(m.Path("proj/pegboard.lock")) {
		t.Error("ad-hoc mode does not write the lock")
	}
	mustContain(t, "uninstall", mustRun(t, m, 0, "uninstall", "skill", "review", "-y"), "- skill review")
	if agentExists(m.Path("proj/.claude/skills/review")) {
		t.Fatal("uninstall did not remove review")
	}
	out = mustRun(t, m, 0, "adopt")
	mustContain(t, "adopt", out, "Adopted into pegboard.yaml:", "+ mcp fs (claude-code)", "+ skill acme/skills")
	mustContain(t, "yaml", read(t, m, "proj/pegboard.yaml"), "skills:\n  - source: acme/skills\n    agents:\n      - claude-code\n", "fs:\n    command: npx")
	st := read(t, m, "proj/.pegboard/state.json")
	if strings.Contains(st, `"adhoc"`) {
		t.Errorf("after adopt nothing remains ad-hoc:\n%s", st)
	}
	// Now declared, skill review comes back on the next apply.
	mustContain(t, "plan after adopt", mustRun(t, m, 0, "plan", "--scope", "project", "--agent", "claude-code"), "+ skill review")
	mustContain(t, "uninstall declared", mustRun(t, m, 1, "uninstall", "skill", "tdd"), "pegboard remove skill tdd")
}

func TestSkillInstallBuiltin(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 0, "skill", "install", "-y", "--agent", "claude-code", "--agent", "codex")
	mustContain(t, "skill install", out, "claude-code · global\n  + skill pegboard", "codex · global\n  + skill pegboard")
	mustContain(t, "SKILL.md", read(t, m, "home/.claude/skills/pegboard/SKILL.md"), "name: pegboard", "pegboard plan --json")
	if agentExists(m.Path("proj/.claude/skills/pegboard")) {
		t.Error("skill install goes to the global scope by default")
	}
}

func TestSkillUninstallBuiltin(t *testing.T) {
	m := fleet(t)
	mustRun(t, m, 0, "skill", "install", "-y", "--agent", "claude-code", "--agent", "codex")

	out := mustRun(t, m, 0, "skill", "uninstall", "-y", "--agent", "claude-code", "--agent", "codex")
	mustContain(t, "skill uninstall", out, "claude-code · global\n  - skill pegboard", "codex · global\n  - skill pegboard")
	if agentExists(m.Path("home/.claude/skills/pegboard")) {
		t.Error("skill uninstall did not remove the Claude Code copy")
	}
	if agentExists(m.Path("home/.agents/skills/pegboard")) {
		t.Error("skill uninstall did not remove the Codex copy")
	}

	// The command is idempotent from the user's point of view: after the
	// skill is gone, it reports that pegboard did not install it.
	mustContain(t, "second uninstall", mustRun(t, m, 1, "skill", "uninstall", "-y", "--agent", "claude-code"),
		"pegboard did not install")
}

func TestAddRemove(t *testing.T) {
	m := fleet(t)
	m.File("proj/pegboard.yaml", "version: 1\n# my mcps\nmcps: {}\n")
	out := mustRun(t, m, 0, "add", "mcp", "gh=npx srv", "--env", "T=${env:T}", "skill", "acme/skills@tdd", "--json")
	var r editResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(r.Added) != 2 || r.File != m.Path("proj/pegboard.yaml") {
		t.Errorf("%+v", r)
	}
	y := read(t, m, "proj/pegboard.yaml")
	mustContain(t, "yaml", y, "# my mcps", "gh:\n    command: npx", "T: ${env:T}", "skills:\n  - acme/skills@tdd")
	mustContain(t, "duplicate add", mustRun(t, m, 0, "add", "skill", "acme/skills@tdd"), "Review with `pegboard plan`")
	if strings.Count(read(t, m, "proj/pegboard.yaml"), "acme/skills@tdd") != 1 {
		t.Error("it should not duplicate the skill")
	}
	mustContain(t, "invalid add", mustRun(t, m, 1, "add", "skill", "invalid"), "unrecognized")
	out = mustRun(t, m, 0, "remove", "mcp", "gh", "skill", "tdd")
	mustContain(t, "remove", out, "- mcp gh", "- skill acme/skills@tdd")
	if y := read(t, m, "proj/pegboard.yaml"); strings.Contains(y, "gh:") || strings.Contains(y, "acme") {
		t.Errorf("remove:\n%s", y)
	}
	mustContain(t, "missing remove", mustRun(t, m, 1, "remove", "mcp", "nope"), "does not declare: mcp nope")
}
