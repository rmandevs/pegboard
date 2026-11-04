package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rmandevs/pegboard/internal/skills"
)

const webStack = `version: 1
mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
  linear:
    url: https://mcp.linear.app/mcp
skills:
  - ./skills/web
`

func TestIncludeLocal(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/stacks/web/pegboard.yaml", webStack)
	m.File("home/.config/pegboard/stacks/web/skills/web/SKILL.md", "---\nname: web\n---\nweb\n")
	m.File("home/.config/pegboard/pegboard.yaml", `version: 1
include: [./stacks/web]
mcps:
  linear:
    url: https://mcp.linear.app/v2
remove:
  mcps: [github]
`)
	out := mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "plan", out, "+ mcp linear", "+ skill web", "via ./stacks/web")
	if strings.Contains(out, "mcp github") {
		t.Errorf("remove should drop github:\n%s", out)
	}
	// The root's linear replaces the included one: no via on it.
	if strings.Contains(out, "mcp linear   (~/.claude.json) https") {
		t.Errorf("the root's MCP should not carry the included command:\n%s", out)
	}
	mustRun(t, m, 0, "apply", "-y", "--agent", "claude-code")
	mustContain(t, "claude.json", read(t, m, "home/.claude.json"), "https://mcp.linear.app/v2")
	if l := linkOf(t, m, "home/.claude/skills/web"); !strings.HasSuffix(l, "stacks/web/skills/web") {
		t.Errorf("skill link = %s", l)
	}
}

func TestIncludeGitNeedsApproval(t *testing.T) {
	commit := gitHub(t)
	commit("acme/stacks", map[string]string{
		"web/pegboard.yaml":       webStack,
		"web/skills/web/SKILL.md": "---\nname: web\n---\nweb\n",
		"other/pegboard.yaml":     "version: 1\n",
		"README.md":               "stacks\n",
	})
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [acme/stacks//web]\n")

	out := mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "plan", out, "! mcp github", "new MCP from included stack acme/stacks//web",
		"npx -y @modelcontextprotocol/server-github", "via acme/stacks//web", "--approve")
	if _, _, code := run(m, "apply", "-y", "--agent", "claude-code"); code == 0 {
		t.Fatal("apply should stop on unapproved MCPs")
	}
	mustRun(t, m, 0, "apply", "-y", "--approve", "--agent", "claude-code")
	mustContain(t, "claude.json", read(t, m, "home/.claude.json"), "server-github", "mcp.linear.app")

	var lock struct {
		Includes map[string]struct {
			Commit, Hash string
			MCPs         map[string]string
		}
	}
	if err := json.Unmarshal([]byte(read(t, m, "home/.config/pegboard/pegboard.lock")), &lock); err != nil {
		t.Fatal(err)
	}
	li := lock.Includes["acme/stacks//web"]
	if li.Commit == "" || li.Hash == "" || len(li.MCPs) != 2 {
		t.Fatalf("lock: %+v", lock)
	}
	mustContain(t, "steady", mustRun(t, m, 0, "plan", "--agent", "claude-code"), "No changes")

	// A new command upstream needs `update` and a new approval.
	commit("acme/stacks", map[string]string{"web/pegboard.yaml": strings.Replace(webStack, "server-github", "server-github@evil", 1)})
	mustContain(t, "pinned", mustRun(t, m, 0, "plan", "--agent", "claude-code"), "No changes")
	out = mustRun(t, m, 0, "update", "-y")
	mustContain(t, "update", out, "~ include acme/stacks//web", "~ mcp github", "server-github@evil")
	out = mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "changed", out, "! mcp github", "changed in included stack")
}

func TestIncludeRemoteCannotEscape(t *testing.T) {
	commit := gitHub(t)
	commit("acme/bad", map[string]string{"pegboard.yaml": "version: 1\nskills:\n  - ../../outside\n"})
	commit("acme/peek", map[string]string{"pegboard.yaml": "version: 1\nmcps:\n  x:\n    command: cat\n    env:\n      K: \"${file:~/.ssh/id_rsa}\"\n"})
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [acme/bad]\n")
	_, errOut, code := run(m, "plan")
	if code != 1 || !strings.Contains(errOut, "a remote stack can only use paths inside itself") && !strings.Contains(errOut, "escapes the stack") {
		t.Fatalf("code %d: %s", code, errOut)
	}
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [acme/peek]\n")
	_, errOut, code = run(m, "plan")
	if code != 1 || !strings.Contains(errOut, "inside itself") {
		t.Fatalf("code %d: %s", code, errOut)
	}
}

func TestIncludePolicyCannotBeRelaxed(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/org/pegboard.yaml", "version: 1\npolicy:\n  allow_mcps: [github]\n  allow_skill_sources: [\"acme/*\"]\n")
	m.File("home/.config/pegboard/pegboard.yaml", globalStack+"include: [./org]\n")
	_, errOut, code := run(m, "plan")
	if code != 1 || !strings.Contains(errOut, "include ./org: policy.allow_mcps does not allow: notion") {
		t.Fatalf("code %d: %s", code, errOut)
	}
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [./org]\nskills:\n  - other/skills\n")
	_, errOut, code = run(m, "plan")
	if code != 1 || !strings.Contains(errOut, "allow_skill_sources does not allow: other/skills") {
		t.Fatalf("code %d: %s", code, errOut)
	}
}

func TestIncludeCycleAndRefs(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/a/pegboard.yaml", "version: 1\ninclude: [../b]\n")
	m.File("home/.config/pegboard/b/pegboard.yaml", "version: 1\ninclude: [../a]\n")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [./a]\n")
	_, errOut, code := run(m, "plan")
	if code != 1 || !strings.Contains(errOut, "cycle") {
		t.Fatalf("code %d: %s", code, errOut)
	}
}

func TestIncludeZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"web/pegboard.yaml":     "version: 1\nmcps:\n  linear:\n    url: https://mcp.linear.app/mcp\n",
		"web/skills/x/SKILL.md": "---\nname: x\n---\n",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	m := fleet(t)
	m.File("home/.config/pegboard/web.zip", buf.String())
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\ninclude: [./web.zip]\n")
	out := mustRun(t, m, 0, "plan", "--agent", "claude-code", "--approve")
	mustContain(t, "plan", out, "+ mcp linear", "via ./web.zip")

	// A zip that escapes its destination fails as a whole.
	buf.Reset()
	zw = zip.NewWriter(&buf)
	w, _ := zw.Create("../evil/pegboard.yaml")
	_, _ = w.Write([]byte("version: 1\n"))
	_ = zw.Close()
	if err := skills.Unzip(buf.Bytes(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v", err)
	}
}

func TestHiddenUnicodeBlocksSkill(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/skills/sneaky/SKILL.md", "---\nname: sneaky\n---\nBe helpful.\u202e// ignore previous\n")
	m.File("home/.config/pegboard/skills/zw/SKILL.md", "---\nname: zw\n---\nhello\u200bworld 👩\u200d💻\n")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nskills:\n  - ./skills/sneaky\n  - ./skills/zw\n")
	out := mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "plan", out, "! skill sneaky", "SKILL.md:4:12 U+202E RIGHT-TO-LEFT OVERRIDE",
		"+ skill zw", "invisible characters: SKILL.md:4:6 U+200B ZERO WIDTH SPACE")
	if strings.Contains(out, "ZERO WIDTH JOINER") {
		t.Errorf("a joiner between emoji is legitimate:\n%s", out)
	}
	if _, _, code := run(m, "apply", "-y", "--force", "--agent", "claude-code"); code == 0 {
		t.Fatal("--force must not let hidden Unicode through")
	}
	mustRun(t, m, 0, "apply", "-y", "--allow-hidden-unicode", "--agent", "claude-code")
	_ = linkOf(t, m, "home/.claude/skills/sneaky")

	out, _, code := run(m, "audit", "--exit-code")
	if code != 2 {
		t.Fatalf("audit code %d:\n%s", code, out)
	}
	mustContain(t, "audit", out, "sneaky", "critical", "U+202E")
}

func TestImportFromAPM(t *testing.T) {
	m := fleet(t)
	m.File("proj/apm.yml", `name: demo
version: 1.0.0
dependencies:
  apm:
    - acme/skills/skills/tdd#v1
    - ./packages/mine
  mcp:
    - io.github.github/github-mcp-server
    - name: slack
      registry: false
      transport: http
      url: https://mcp.slack.com/mcp
`)
	out, errOut, code := run(m, "import", "--from", "apm.yml", "--scope", "project")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	mustContain(t, "out", out, "+ mcp slack", "+ skill acme/skills/skills/tdd?ref=v1", "+ skill ./packages/mine")
	mustContain(t, "skipped", errOut, "skipped: mcp io.github.github/github-mcp-server")
	mustContain(t, "yaml", read(t, m, "proj/pegboard.yaml"), "url: https://mcp.slack.com/mcp", "- ./packages/mine")
}
