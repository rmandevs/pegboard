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
		"no version":                "",
	}
	for in, want := range cases {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseScope(t *testing.T) {
	if s, ok := ParseScope(""); !ok || len(s) != 2 {
		t.Errorf("empty should return both scopes: %v", s)
	}
	if s, ok := ParseScope("project"); !ok || len(s) != 1 || s[0] != Project {
		t.Errorf("project: %v", s)
	}
	if _, ok := ParseScope("all"); ok {
		t.Error("all should be invalid")
	}
}

func TestPretty(t *testing.T) {
	env := Env{Home: filepath.FromSlash("/h"), ProjectDir: filepath.FromSlash("/h/proj")}
	cases := map[string]string{
		"/h/proj/.mcp.json": ".mcp.json",
		"/h/.claude.json":   "~/.claude.json",
		"/h/projx/a":        "~/projx/a",
		"/etc/x":            filepath.FromSlash("/etc/x"),
		"":                  "",
	}
	for in, want := range cases {
		if got := env.Pretty(filepath.FromSlash(in)); got != want {
			t.Errorf("Pretty(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrettyFromHome(t *testing.T) {
	env := Env{Home: filepath.FromSlash("/h"), ProjectDir: filepath.FromSlash("/h")}
	if got := env.Pretty(filepath.FromSlash("/h/.claude/skills")); got != "~/.claude/skills" {
		t.Error(got)
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
