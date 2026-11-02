// Package agenttest builds fake environments to test providers without
// touching the real machine: everything lives in a t.TempDir().
package agenttest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rmandevs/pegboard/internal/agent"
)

// Machine is a fake machine: a home directory, a project directory, binaries
// on PATH with their versions, and environment variables.
type Machine struct {
	T    *testing.T
	Root string
	Bins map[string]string // name → version ("" = does not respond)
	Vars map[string]string
}

func New(t *testing.T) *Machine {
	t.Helper()
	root := t.TempDir()
	m := &Machine{T: t, Root: root, Bins: map[string]string{}, Vars: map[string]string{}}
	m.Mkdir("home")
	m.Mkdir("proj")
	return m
}

func (m *Machine) Home() string    { return filepath.Join(m.Root, "home") }
func (m *Machine) Project() string { return filepath.Join(m.Root, "proj") }

// Path is a path inside the fake machine.
func (m *Machine) Path(rel string) string { return filepath.Join(m.Root, rel) }

// Mkdir creates a directory relative to Root.
func (m *Machine) Mkdir(rel string) string {
	m.T.Helper()
	p := m.Path(rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		m.T.Fatal(err)
	}
	return p
}

// File creates a file relative to Root, including its directories.
func (m *Machine) File(rel, content string) string {
	m.T.Helper()
	p := m.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		m.T.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		m.T.Fatal(err)
	}
	return p
}

// BinPath is where the fake machine says a binary lives.
func (m *Machine) BinPath(name string) string { return "/fake/bin/" + name }

// Env builds the agent.Env for the machine.
func (m *Machine) Env() agent.Env {
	return agent.Env{
		Home:       m.Home(),
		ProjectDir: m.Project(),
		Getenv:     func(k string) string { return m.Vars[k] },
		LookPath: func(name string) (string, error) {
			if _, ok := m.Bins[name]; ok {
				return m.BinPath(name), nil
			}
			return "", errors.New("not found")
		},
		BinVersion: func(path string) string {
			return m.Bins[filepath.Base(path)]
		},
		LookupEnv: func(k string) (string, bool) {
			v, ok := m.Vars[k]
			return v, ok
		},
		Now: func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) },
	}
}

// EnvFunc is Env with the signature the CLI expects; it ignores --dir unless
// it points to another directory.
func (m *Machine) EnvFunc() func(string) (agent.Env, error) {
	return func(dir string) (agent.Env, error) {
		env := m.Env()
		if dir != "" && dir != "." {
			env.ProjectDir = dir
		}
		return env, nil
	}
}
