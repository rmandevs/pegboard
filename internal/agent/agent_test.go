package agent

import (
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"2.1.3 (Claude Code)\n":     "2.1.3",
		"codex-cli 0.46.0":          "0.46.0",
		"opencode v1.0.12-beta.1\n": "1.0.12-beta.1",
		"Hermes Agent 0.9":          "0.9",
		"sin versión":               "",
	}
	for in, want := range cases {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseScope(t *testing.T) {
	if s, ok := ParseScope(""); !ok || len(s) != 2 {
		t.Errorf("vacío debía dar ambos alcances: %v", s)
	}
	if s, ok := ParseScope("project"); !ok || len(s) != 1 || s[0] != Project {
		t.Errorf("project: %v", s)
	}
	if _, ok := ParseScope("todo"); ok {
		t.Error("todo debía ser inválido")
	}
}

func TestPretty(t *testing.T) {
	env := Env{Home: "/h", ProjectDir: "/h/proj"}
	cases := map[string]string{
		"/h/proj/.mcp.json": ".mcp.json",
		"/h/.claude.json":   filepath.Join("~", ".claude.json"),
		"/h/projx/a":        filepath.Join("~", "projx/a"),
		"/etc/x":            "/etc/x",
		"":                  "",
	}
	for in, want := range cases {
		if got := env.Pretty(in); got != want {
			t.Errorf("Pretty(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTargetID(t *testing.T) {
	if id := (Target{Agent: "hermes", Profile: "work"}).ID(); id != "hermes:work" {
		t.Error(id)
	}
	if id := (Target{Agent: "codex"}).ID(); id != "codex" {
		t.Error(id)
	}
}
