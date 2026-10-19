// Package state guarda qué escribió pegboard en cada target (SPEC §3.4): es
// lo que permite no pisar lo ajeno y detectar drift. Nunca contiene
// secretos: solo hashes calculados con los secretos enmascarados.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Origin dice por qué existe un recurso: declarado en pegboard.yaml o
// instalado con `pegboard install`.
type Origin string

const (
	Declared Origin = "declared"
	Adhoc    Origin = "adhoc"
)

// Entry es un MCP que pegboard escribió en un archivo.
type Entry struct {
	File   string   `json:"file"`
	Hash   string   `json:"hash"`
	Masked []string `json:"masked,omitempty"`
	Origin Origin   `json:"origin"`
}

// SkillEntry es una skill que pegboard puso en una carpeta de skills: un
// symlink al caché (o una copia, donde no hay symlinks).
type SkillEntry struct {
	Source string `json:"source"`
	Hash   string `json:"hash"`
	// Link es a dónde apunta el symlink (el caché, o la carpeta local).
	Link string `json:"link"`
	// Copy: se copió en vez de enlazar.
	Copy   bool   `json:"copy,omitempty"`
	Origin Origin `json:"origin"`
}

// ContextEntry registra lo que pegboard agregó a un archivo de contexto (el
// puente @AGENTS.md en CLAUDE.md) para poder quitarlo después.
type ContextEntry struct {
	Line string `json:"line"`
}

// State de un alcance. Targets: ID del target ("hermes:work") → nombre →
// entrada de MCP. Skills: carpeta de skills → nombre → entrada (una carpeta
// puede servir a varios agentes). Context: archivo → lo agregado.
type State struct {
	Version int                              `json:"version"`
	Targets map[string]map[string]Entry      `json:"targets"`
	Skills  map[string]map[string]SkillEntry `json:"skills,omitempty"`
	Context map[string]ContextEntry          `json:"context,omitempty"`

	path string
}

// Skill devuelve la entrada de una skill en una carpeta.
func (s *State) Skill(dir, name string) (SkillEntry, bool) {
	e, ok := s.Skills[dir][name]
	return e, ok
}

func (s *State) SetSkill(dir, name string, e SkillEntry) {
	if s.Skills == nil {
		s.Skills = map[string]map[string]SkillEntry{}
	}
	if s.Skills[dir] == nil {
		s.Skills[dir] = map[string]SkillEntry{}
	}
	s.Skills[dir][name] = e
}

func (s *State) DeleteSkill(dir, name string) {
	delete(s.Skills[dir], name)
	if len(s.Skills[dir]) == 0 {
		delete(s.Skills, dir)
	}
}

// SkillNames son las skills registradas en una carpeta, ordenadas.
func (s *State) SkillNames(dir string) []string {
	var out []string
	for n := range s.Skills[dir] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SkillDirs son las carpetas con skills registradas, ordenadas.
func (s *State) SkillDirs() []string {
	var out []string
	for d := range s.Skills {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Load lee el estado; si el archivo no existe devuelve uno vacío.
func Load(path string) (*State, error) {
	s := &State{Version: 1, Targets: map[string]map[string]Entry{}, path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("%s: versión de estado %d no soportada", path, s.Version)
	}
	if s.Targets == nil {
		s.Targets = map[string]map[string]Entry{}
	}
	s.path = path
	return s, nil
}

func (s *State) Path() string { return s.path }

// Get devuelve la entrada de un MCP en un target.
func (s *State) Get(target, name string) (Entry, bool) {
	e, ok := s.Targets[target][name]
	return e, ok
}

func (s *State) Set(target, name string, e Entry) {
	if s.Targets[target] == nil {
		s.Targets[target] = map[string]Entry{}
	}
	s.Targets[target][name] = e
}

func (s *State) Delete(target, name string) {
	delete(s.Targets[target], name)
	if len(s.Targets[target]) == 0 {
		delete(s.Targets, target)
	}
}

// Names son los MCPs registrados en un target, ordenados.
func (s *State) Names(target string) []string {
	var out []string
	for n := range s.Targets[target] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TargetIDs son los targets con algo registrado, ordenados.
func (s *State) TargetIDs() []string {
	var out []string
	for t := range s.Targets {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Save escribe el estado de forma atómica. En un proyecto, la carpeta
// .pegboard/ lleva su propio .gitignore: el estado es de cada máquina.
func (s *State) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if filepath.Base(dir) == ".pegboard" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		gi := filepath.Join(dir, ".gitignore")
		if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(gi, []byte("*\n"), 0o644); err != nil {
				return err
			}
		}
	}
	return WriteAtomic(s.path, append(b, '\n'), 0o644)
}
