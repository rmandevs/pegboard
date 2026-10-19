package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rmandevs/agents-casc/internal/state"
)

// Skill es una skill resuelta: su contenido exacto y dónde está en disco.
type Skill struct {
	Name   string
	Source string // la fuente tal como se declaró
	Dir    string // carpeta en el caché (o la carpeta local)
	Hash   string
	Local  bool
	Commit string
}

// Resolver resuelve fuentes a skills en el caché.
type Resolver struct {
	Cache   string // ~/.cache/pegboard
	Home    string
	BaseDir string // las fuentes locales parten de aquí (la carpeta del stack)
	Lock    *Lock  // nil en modo ad-hoc
	// Update ignora lo fijado en el lock y vuelve a resolver los refs.
	Update  bool
	Builtin fs.FS
	// Git corre git; nil = el binario del sistema.
	Git func(dir string, args ...string) (string, error)
}

func (r *Resolver) git(dir string, args ...string) (string, error) {
	if r.Git != nil {
		return r.Git(dir, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Nunca esperar una contraseña en la terminal.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// Resolve devuelve las skills de una fuente, ordenadas por nombre.
func (r *Resolver) Resolve(src Source) ([]Skill, error) {
	switch src.Kind {
	case Local:
		return r.local(src)
	case Builtin:
		return r.builtin(src)
	}
	return r.fromGit(src)
}

func (r *Resolver) local(src Source) ([]Skill, error) {
	dir := src.Dir
	switch {
	case strings.HasPrefix(dir, "~/"):
		dir = filepath.Join(r.Home, dir[2:])
	case !filepath.IsAbs(dir):
		dir = filepath.Join(r.BaseDir, dir)
	}
	found, err := Discover(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src.Raw, err)
	}
	var out []Skill
	for _, name := range sortedNames(found) {
		h, err := Hash(found[name])
		if err != nil {
			return nil, err
		}
		out = append(out, Skill{Name: name, Source: src.Raw, Dir: found[name], Hash: h, Local: true})
	}
	return out, nil
}

func (r *Resolver) builtin(src Source) ([]Skill, error) {
	if r.Builtin == nil {
		return nil, errors.New("pegboard: skill propia no disponible")
	}
	tmp, err := r.tmp()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	err = fs.WalkDir(r.Builtin, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(tmp, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(r.Builtin, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return nil, err
	}
	sk, err := r.store("pegboard", src.Raw, tmp)
	if err != nil {
		return nil, err
	}
	return []Skill{sk}, nil
}

func (r *Resolver) tmp() (string, error) {
	if err := os.MkdirAll(r.Cache, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(r.Cache, "tmp-")
}

// CachedDir es la carpeta del caché para un hash (exista o no).
func (r *Resolver) CachedDir(hash string) string { return r.cached(hash) }

// cached devuelve la carpeta del caché para un hash.
func (r *Resolver) cached(hash string) string {
	return filepath.Join(r.Cache, "skills", strings.TrimPrefix(hash, "sha256:"))
}

// store copia dir al caché (si no estaba ya) y devuelve la skill.
func (r *Resolver) store(name, source, dir string) (Skill, error) {
	h, err := Hash(dir)
	if err != nil {
		return Skill{}, err
	}
	dst := r.cached(h)
	if _, err := os.Stat(dst); err != nil {
		tmp, err := r.tmp()
		if err != nil {
			return Skill{}, err
		}
		staged := filepath.Join(tmp, "skill")
		if err := CopyDir(dir, staged); err != nil {
			_ = os.RemoveAll(tmp)
			return Skill{}, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return Skill{}, err
		}
		if err := os.Rename(staged, dst); err != nil && !exists(dst) {
			_ = os.RemoveAll(tmp)
			return Skill{}, err
		}
		_ = os.RemoveAll(tmp)
	}
	return Skill{Name: name, Source: source, Dir: dst, Hash: h}, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func (r *Resolver) fromGit(src Source) ([]Skill, error) {
	var pinned *LockedSource
	if r.Lock != nil && !r.Update {
		if l, ok := r.Lock.Sources[src.Raw]; ok {
			pinned = &l
		}
	}
	// Con lock y todo en el caché, no hace falta red.
	if pinned != nil && len(pinned.Skills) > 0 {
		var out []Skill
		for _, name := range sortedNames(pinned.Skills) {
			h := pinned.Skills[name]
			if !exists(r.cached(h)) {
				out = nil
				break
			}
			out = append(out, Skill{Name: name, Source: src.Raw, Dir: r.cached(h), Hash: h, Commit: pinned.Commit})
		}
		if out != nil {
			return out, nil
		}
	}
	ref := src.Ref
	if pinned != nil && pinned.Commit != "" {
		ref = pinned.Commit
	}
	tmp, err := r.tmp()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if ref == "" {
		ref = "HEAD"
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", src.URL, ref},
		{"checkout", "-q", "FETCH_HEAD"},
	} {
		if _, err := r.git(tmp, args...); err != nil {
			if msg := err.Error(); strings.Contains(msg, "could not read Username") || strings.Contains(msg, "not found") {
				return nil, fmt.Errorf("%s: no se pudo leer %s: no existe, o es privado y git no tiene credenciales", src.Raw, src.URL)
			}
			return nil, fmt.Errorf("%s: %w", src.Raw, err)
		}
	}
	commit, err := r.git(tmp, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	root := filepath.Join(tmp, filepath.FromSlash(src.Path))
	found, err := Discover(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src.Raw, err)
	}
	if src.Skill != "" {
		dir, ok := found[src.Skill]
		if !ok {
			return nil, fmt.Errorf("%s: el repo no tiene la skill %q (tiene: %s)", src.Raw, src.Skill, strings.Join(sortedNames(found), ", "))
		}
		found = map[string]string{src.Skill: dir}
	}
	var out []Skill
	locked := LockedSource{Commit: commit, Skills: map[string]string{}}
	for _, name := range sortedNames(found) {
		sk, err := r.store(name, src.Raw, found[name])
		if err != nil {
			return nil, err
		}
		sk.Commit = commit
		if pinned != nil && pinned.Commit == commit {
			if want, ok := pinned.Skills[name]; ok && want != sk.Hash {
				return nil, fmt.Errorf("%s: el contenido de %s no coincide con %s (commit %s); revisa con `pegboard update`",
					src.Raw, name, LockFile, Short(commit))
			}
		}
		locked.Skills[name] = sk.Hash
		out = append(out, sk)
	}
	if r.Lock != nil {
		r.Lock.set(src.Raw, locked)
	}
	return out, nil
}

// Discover busca skills (carpetas con SKILL.md) como lo hace `npx skills`:
// la raíz, luego skills/, .agents/skills/ y .claude/skills/ hasta tres
// niveles, y si no hay nada, en todo el árbol. Devuelve nombre → carpeta.
func Discover(root string) (map[string]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s no es una carpeta", root)
	}
	found := map[string]string{}
	add := func(dir string) error {
		name, err := skillName(dir)
		if err != nil {
			return err
		}
		if prev, dup := found[name]; dup {
			return fmt.Errorf("dos skills se llaman %q: %s y %s", name, prev, dir)
		}
		found[name] = dir
		return nil
	}
	if exists(filepath.Join(root, "SKILL.md")) {
		return found, add(root)
	}
	walk := func(base string, maxDepth int) error {
		return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if n := d.Name(); p != base && (n == ".git" || n == "node_modules") {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(base, p)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if rel == "." {
				depth = 0
			}
			if maxDepth > 0 && depth > maxDepth {
				return filepath.SkipDir
			}
			if p != base && exists(filepath.Join(p, "SKILL.md")) {
				if err := add(p); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return nil
		})
	}
	for _, c := range []string{"skills", ".agents/skills", ".claude/skills"} {
		if dir := filepath.Join(root, filepath.FromSlash(c)); exists(dir) {
			if err := walk(dir, 3); err != nil {
				return nil, err
			}
		}
	}
	if len(found) == 0 {
		if err := walk(root, 0); err != nil {
			return nil, err
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no hay ninguna skill (SKILL.md) en %s", root)
	}
	return found, nil
}

// skillName es el name: del frontmatter de SKILL.md, o el nombre de la carpeta.
func skillName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", err
	}
	name := filepath.Base(dir)
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[4:], "\n---"); end >= 0 {
			var fm struct {
				Name string `yaml:"name"`
			}
			if err := yaml.Unmarshal([]byte(text[4:4+end]), &fm); err == nil && fm.Name != "" {
				name = fm.Name
			}
		}
	}
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("%s: nombre de skill inválido %q", dir, name)
	}
	return name, nil
}

func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LockFile es el nombre del lock, junto a cada pegboard.yaml (SPEC §3.3).
const LockFile = "pegboard.lock"

// Lock fija el commit y el hash de cada skill resuelta.
type Lock struct {
	Version int                     `json:"version"`
	Sources map[string]LockedSource `json:"skills"`

	path    string
	changed bool
}

// LockedSource es lo fijado para una fuente: su commit y nombre → hash.
type LockedSource struct {
	Commit string            `json:"commit,omitempty"`
	Skills map[string]string `json:"skills"`
}

// LoadLock lee el lock; si no existe devuelve uno vacío.
func LoadLock(path string) (*Lock, error) {
	l := &Lock{Version: 1, Sources: map[string]LockedSource{}, path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.Version != 1 {
		return nil, fmt.Errorf("%s: versión %d no soportada", path, l.Version)
	}
	if l.Sources == nil {
		l.Sources = map[string]LockedSource{}
	}
	l.path = path
	return l, nil
}

func (l *Lock) set(source string, ls LockedSource) {
	old, ok := l.Sources[source]
	if ok && old.Commit == ls.Commit && equalMaps(old.Skills, ls.Skills) {
		return
	}
	l.Sources[source] = ls
	l.changed = true
}

// Prune quita las fuentes que ya no se declaran.
func (l *Lock) Prune(keep map[string]bool) {
	for s := range l.Sources {
		if !keep[s] {
			delete(l.Sources, s)
			l.changed = true
		}
	}
}

// Changed informa si hay que escribir el lock.
func (l *Lock) Changed() bool { return l.changed }

// Path es la ruta del lock.
func (l *Lock) Path() string { return l.path }

// Save escribe el lock si cambió.
func (l *Lock) Save() error {
	if !l.changed {
		return nil
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := state.WriteAtomic(l.path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	l.changed = false
	return nil
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
