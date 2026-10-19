// Package agenttest arma entornos falsos para probar providers sin tocar
// la máquina real: todo vive en un t.TempDir().
package agenttest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rmandevs/agents-casc/internal/agent"
)

// Machine es una máquina falsa: un home, una carpeta de proyecto, binarios
// en el PATH con su versión y variables de entorno.
type Machine struct {
	T    *testing.T
	Root string
	Bins map[string]string // nombre → versión ("" = no responde)
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

// Path es una ruta dentro de la máquina falsa.
func (m *Machine) Path(rel string) string { return filepath.Join(m.Root, rel) }

// Mkdir crea una carpeta relativa a Root.
func (m *Machine) Mkdir(rel string) string {
	m.T.Helper()
	p := m.Path(rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		m.T.Fatal(err)
	}
	return p
}

// File crea un archivo relativo a Root, con sus carpetas.
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

// BinPath es donde la máquina falsa dice que vive un binario.
func (m *Machine) BinPath(name string) string { return "/fake/bin/" + name }

// Env construye el agent.Env de la máquina.
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

// EnvFunc es Env con la firma que espera la CLI; ignora --dir salvo que
// apunte a otra carpeta.
func (m *Machine) EnvFunc() func(string) (agent.Env, error) {
	return func(dir string) (agent.Env, error) {
		env := m.Env()
		if dir != "" && dir != "." {
			env.ProjectDir = dir
		}
		return env, nil
	}
}
