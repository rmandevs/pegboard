// Package agent define los tipos comunes a todos los providers: alcances,
// targets, el entorno donde se detectan y la interfaz Provider.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scope es el alcance de un stack: el usuario (global) o una carpeta (project).
type Scope string

const (
	Global  Scope = "global"
	Project Scope = "project"
)

// ParseScope convierte el valor de --scope. Vacío significa "todos".
func ParseScope(s string) ([]Scope, bool) {
	switch s {
	case "":
		return []Scope{Global, Project}, true
	case string(Global):
		return []Scope{Global}, true
	case string(Project):
		return []Scope{Project}, true
	}
	return nil, false
}

// Paths son las rutas nativas donde un target guarda cada tipo de recurso.
// Una ruta vacía significa que el agente no soporta ese recurso en el alcance.
type Paths struct {
	MCPs    string `json:"mcps,omitempty"`
	Skills  string `json:"skills,omitempty"`
	Context string `json:"context,omitempty"`
}

// Target es un agente en un alcance concreto (p. ej. Claude Code × proyecto).
type Target struct {
	Agent    string `json:"agent"`
	Profile  string `json:"profile,omitempty"`
	Scope    Scope  `json:"scope"`
	Provider int    `json:"provider_version"`
	Binary   string `json:"binary,omitempty"`
	Version  string `json:"version,omitempty"`
	// Evidence lista qué se encontró para dar el target por detectado.
	Evidence []string `json:"evidence"`
	Paths    Paths    `json:"paths"`
}

// ID identifica el target dentro de su alcance: "claude-code", "hermes:work".
func (t Target) ID() string {
	if t.Profile != "" {
		return t.Agent + ":" + t.Profile
	}
	return t.Agent
}

// Provider es el adaptador de un agente. En la fase 0 solo detecta; Read,
// Plan y Apply llegan con la fase 1 (SPEC §5).
type Provider interface {
	ID() string
	// Version es la versión del provider, no la del agente: sube cuando
	// cambian las rutas o el formato que entiende.
	Version() int
	// Detect devuelve los targets del agente en el alcance; ninguno si no
	// está instalado o no aplica. Puede haber varios (perfiles de Hermes).
	Detect(env Env, scope Scope) []Target
}

// Env es todo lo que un provider consulta de la máquina. Los tests lo
// construyen sobre un t.TempDir().
type Env struct {
	Home       string
	ProjectDir string
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	// BinVersion devuelve la versión de un binario, o "" si no se sabe.
	BinVersion func(path string) string
	LookupEnv  func(string) (string, bool)
	Now        func() time.Time
}

// DefaultEnv es el entorno real del proceso.
func DefaultEnv(projectDir string) (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return Env{}, err
	}
	return Env{
		Home:       home,
		ProjectDir: abs,
		Getenv:     os.Getenv,
		LookPath:   lookPath,
		BinVersion: memo(binVersion),
		LookupEnv:  os.LookupEnv,
		Now:        time.Now,
	}, nil
}

// NewTarget arma un target detectado; consulta la versión si hay binario.
func (e Env) NewTarget(p Provider, scope Scope, bin string, evidence []string, paths Paths) Target {
	t := Target{
		Agent:    p.ID(),
		Scope:    scope,
		Provider: p.Version(),
		Binary:   bin,
		Evidence: evidence,
		Paths:    paths,
	}
	if t.Evidence == nil {
		t.Evidence = []string{}
	}
	if bin != "" && e.BinVersion != nil {
		t.Version = e.BinVersion(bin)
	}
	return t
}

// EnvDir devuelve $name si está definida, o home/def.
func (e Env) EnvDir(name, def string) string {
	if v := e.Getenv(name); v != "" {
		return v
	}
	return filepath.Join(e.Home, def)
}

// XDGConfig es $XDG_CONFIG_HOME o ~/.config.
func (e Env) XDGConfig() string {
	return e.EnvDir("XDG_CONFIG_HOME", ".config")
}

// Pretty abrevia una ruta para mostrarla: relativa al proyecto o con ~.
// Las abreviadas usan / en todos los sistemas, para que la salida sea la
// misma en Windows.
func (e Env) Pretty(p string) string {
	if p == "" {
		return ""
	}
	if e.ProjectDir != "" {
		if rel, ok := under(e.ProjectDir, p); ok {
			return filepath.ToSlash(rel)
		}
	}
	if rel, ok := under(e.Home, p); ok {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

func under(base, p string) (string, bool) {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// Exists informa si la ruta existe (archivo o carpeta).
func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Installed reúne la evidencia de instalación global de un agente: su
// binario en el PATH y las rutas propias que existan. Devuelve la ruta del
// binario ("" si no está) y la evidencia, vacía si no hay nada.
func (e Env) Installed(bin string, paths ...string) (string, []string) {
	var evidence []string
	binPath, err := e.LookPath(bin)
	if err != nil {
		binPath = ""
	} else {
		evidence = append(evidence, "bin:"+binPath)
	}
	for _, p := range paths {
		if Exists(p) {
			evidence = append(evidence, e.Pretty(p))
		}
	}
	return binPath, evidence
}

// Present filtra las rutas que existen, abreviadas para mostrarlas.
func (e Env) Present(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if Exists(p) {
			out = append(out, e.Pretty(p))
		}
	}
	return out
}

// StateDir es $XDG_STATE_HOME/pegboard (~/.local/state/pegboard): estado
// global y backups.
func (e Env) StateDir() string {
	return filepath.Join(e.EnvDir("XDG_STATE_HOME", filepath.Join(".local", "state")), "pegboard")
}

// ConfigDir es $XDG_CONFIG_HOME/pegboard: donde vive el pegboard.yaml global.
func (e Env) ConfigDir() string {
	return filepath.Join(e.XDGConfig(), "pegboard")
}
