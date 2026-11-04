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

	"github.com/rmandevs/pegboard/internal/state"
)

// Skill is a resolved skill: its exact content and where it lives on disk.
type Skill struct {
	Name   string
	Source string // the source as declared
	Dir    string // directory in the cache (or the local directory)
	Hash   string
	Local  bool
	Commit string
}

// Resolver resolves sources into skills in the cache.
type Resolver struct {
	Cache   string // ~/.cache/pegboard
	Home    string
	BaseDir string // local sources start here (the stack's directory)
	Lock    *Lock  // nil in ad-hoc mode
	// Update ignores what the lock pinned and resolves refs again.
	Update  bool
	Builtin fs.FS
	// Git runs git; nil = the system binary.
	Git func(dir string, args ...string) (string, error)
}

func (r *Resolver) git(dir string, args ...string) (string, error) {
	if r.Git != nil {
		return r.Git(dir, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Never wait for a password on the terminal.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// Resolve returns the skills from a source, sorted by name.
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
	// A skill inside an included remote stack is already a pinned copy:
	// it goes to the cache by hash like a git skill, so a new version of
	// the stack only relinks the skills whose content changed.
	inStacks := strings.HasPrefix(dir, filepath.Join(r.Cache, "stacks")+string(filepath.Separator))
	var out []Skill
	for _, name := range sortedNames(found) {
		if inStacks {
			sk, err := r.store(name, src.Raw, found[name])
			if err != nil {
				return nil, err
			}
			out = append(out, sk)
			continue
		}
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
		return nil, errors.New("pegboard: built-in skill unavailable")
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

// CachedDir is the cache directory for a hash (whether it exists or not).
func (r *Resolver) CachedDir(hash string) string { return r.cached(hash) }

// cached returns the cache directory for a hash.
func (r *Resolver) cached(hash string) string {
	return filepath.Join(r.Cache, "skills", strings.TrimPrefix(hash, "sha256:"))
}

// store copies dir into the cache (if not already there) and returns the
// skill.
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
	// With a lock and everything in the cache, no network is needed.
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
				return nil, fmt.Errorf("%s: could not read %s: it does not exist, or it is private and git has no credentials", src.Raw, src.URL)
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
			return nil, fmt.Errorf("%s: the repository has no skill %q (available: %s)", src.Raw, src.Skill, strings.Join(sortedNames(found), ", "))
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
				return nil, fmt.Errorf("%s: content of %s does not match %s (commit %s); check with `pegboard update`",
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

// Discover finds skills (directories with SKILL.md) the way `npx skills`
// does: the root, then skills/, .agents/skills/, and .claude/skills/ up to
// three levels deep, and if nothing is found, the whole tree. It returns
// name → directory.
func Discover(root string) (map[string]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	found := map[string]string{}
	add := func(dir string) error {
		name, err := skillName(dir)
		if err != nil {
			return err
		}
		if prev, dup := found[name]; dup {
			return fmt.Errorf("two skills are named %q: %s and %s", name, prev, dir)
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
		return nil, fmt.Errorf("no skill (SKILL.md) found in %s", root)
	}
	return found, nil
}

// skillName is the name: field from SKILL.md's frontmatter, or the directory
// name.
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
		return "", fmt.Errorf("%s: invalid skill name %q", dir, name)
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

// LockFile is the lock name, next to each pegboard.yaml (SPEC §3.3).
const LockFile = "pegboard.lock"

// Lock pins the commit and hash of each resolved skill.
type Lock struct {
	Version  int                      `json:"version"`
	Includes map[string]LockedInclude `json:"includes,omitempty"`
	Sources  map[string]LockedSource  `json:"skills"`

	path    string
	changed bool
}

// LockedSource is what was pinned for a source: its commit and name → hash.
type LockedSource struct {
	Commit string            `json:"commit,omitempty"`
	Skills map[string]string `json:"skills"`
}

// LoadLock reads the lock; if it does not exist it returns an empty one.
func LoadLock(path string) (*Lock, error) {
	l := &Lock{Version: 1, Sources: map[string]LockedSource{}, Includes: map[string]LockedInclude{}, path: path}
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
		return nil, fmt.Errorf("%s: unsupported version %d", path, l.Version)
	}
	if l.Sources == nil {
		l.Sources = map[string]LockedSource{}
	}
	if l.Includes == nil {
		l.Includes = map[string]LockedInclude{}
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

func (l *Lock) setInclude(raw string, li LockedInclude) {
	l.Includes[raw] = li
	l.changed = true
}

// Approved returns the fingerprint the lock recorded for an MCP from an
// included stack.
func (l *Lock) Approved(include, name string) (string, bool) {
	if l == nil {
		return "", false
	}
	fp, ok := l.Includes[include].MCPs[name]
	return fp, ok
}

// Approve records the MCPs approved from each included stack (include →
// name → fingerprint), replacing what was there.
func (l *Lock) Approve(byInclude map[string]map[string]string) {
	for raw, li := range l.Includes {
		want := byInclude[raw]
		if len(want) == 0 {
			want = nil
		}
		if equalMaps(li.MCPs, want) {
			continue
		}
		li.MCPs = want
		l.Includes[raw] = li
		l.changed = true
	}
}

// PruneIncludes removes included stacks that are no longer declared.
func (l *Lock) PruneIncludes(keep map[string]bool) {
	for s := range l.Includes {
		if !keep[s] {
			delete(l.Includes, s)
			l.changed = true
		}
	}
}

// Prune removes sources that are no longer declared.
func (l *Lock) Prune(keep map[string]bool) {
	for s := range l.Sources {
		if !keep[s] {
			delete(l.Sources, s)
			l.changed = true
		}
	}
}

// Changed reports whether the lock needs to be written.
func (l *Lock) Changed() bool { return l.changed }

// Path is the lock's path.
func (l *Lock) Path() string { return l.path }

// Save writes the lock if it changed.
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
