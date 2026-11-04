package apm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmandevs/pegboard/internal/skills"
)

const manifest = `name: demo
version: 1.0.0
targets: [copilot, claude]
dependencies:
  apm:
    - microsoft/apm-sample-package#v1.0.0
    - anthropics/skills/skills/frontend-design
    - gitlab.com/acme/coding-standards#main
    - ./packages/my-skills
    - contoso/prompts/review.prompt.md
    - git: https://gitlab.com/acme/repo.git
      path: instructions/security
      ref: v2.0
    - git: vercel-labs/agent-skills
      skills: [deploy-to-vercel, web-design]
    - git: acme/toolkit
      ref: ^1.2.0
    - name: sec-check
      marketplace: acme-plugins
  mcp:
    - io.github.github/github-mcp-server
    - name: my-private-server
      registry: false
      transport: stdio
      command: ./bin/my-server
      args: ["--port", "3000"]
      env:
        API_KEY: ${{ secrets.KEY }}
        OTHER: ${OTHER_TOKEN}
    - name: io.example/slack
      registry: false
      transport: http
      url: https://mcp.slack.com/mcp
      headers:
        Authorization: "Bearer ${MY_SECRET_TOKEN}"
        X-Project: "${input:proj}"
`

func TestRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Read(p, func(abs string) string {
		rel, _ := filepath.Rel(dir, abs)
		return "./" + filepath.ToSlash(rel)
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range r.Skills {
		got = append(got, s.Source)
		if _, err := skills.Parse(s.Source); err != nil {
			t.Errorf("%s does not parse: %v", s.Source, err)
		}
	}
	want := []string{
		"microsoft/apm-sample-package?ref=v1.0.0",
		"anthropics/skills/skills/frontend-design",
		"gitlab.com/acme/coding-standards?ref=main",
		"./packages/my-skills",
		"https://gitlab.com/acme/repo.git//instructions/security?ref=v2.0",
		"vercel-labs/agent-skills@deploy-to-vercel",
		"vercel-labs/agent-skills@web-design",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("skills:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(r.MCPs) != 2 {
		t.Fatalf("mcps: %+v\n%v", r.MCPs, r.Skipped)
	}
	s := r.MCPs["my-private-server"]
	if s.Command != "./bin/my-server" || s.Env["API_KEY"] != "${env:KEY}" || s.Env["OTHER"] != "${env:OTHER_TOKEN}" || len(s.Args) != 2 {
		t.Errorf("stdio: %+v", s)
	}
	h := r.MCPs["slack"]
	if h.URL != "https://mcp.slack.com/mcp" || h.Headers["Authorization"] != "Bearer ${env:MY_SECRET_TOKEN}" {
		t.Errorf("http: %+v", h)
	}
	skipped := strings.Join(r.Skipped, "\n")
	for _, w := range []string{"review.prompt.md", "semver range", "marketplace", "io.github.github/github-mcp-server"} {
		if !strings.Contains(skipped, w) {
			t.Errorf("skipped should mention %q:\n%s", w, skipped)
		}
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "${input:") {
		t.Errorf("warnings: %v", r.Warnings)
	}
}
