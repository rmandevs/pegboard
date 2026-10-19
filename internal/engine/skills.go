package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rmandevs/agents-casc/internal/skills"
	"github.com/rmandevs/agents-casc/internal/state"
)

// SkillWant es una skill que debe estar en una carpeta. nil = debe no estar.
type SkillWant struct {
	Skill  skills.Skill
	Origin state.Origin
}

// SkillDir es una carpeta de skills y los targets que la leen: Codex y
// opencode comparten .agents/skills en un proyecto, así que la skill se
// enlaza una sola vez.
type SkillDir struct {
	Dir     string
	Targets []string
	Wants   map[string]*SkillWant
}

// observed es lo que hay en <dir>/<name>.
type observed struct {
	exists bool
	link   string // destino si es symlink
	hash   string // contenido si es una carpeta copiada
	err    error
}

func observe(path string) observed {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return observed{}
	}
	if err != nil {
		return observed{err: err}
	}
	o := observed{exists: true}
	if info.Mode()&fs.ModeSymlink != 0 {
		o.link, o.err = os.Readlink(path)
		return o
	}
	if info.IsDir() {
		o.hash, o.err = skills.Hash(path)
	}
	return o
}

// matches informa si lo observado es lo que dice la entrada.
func (o observed) matches(link, hash string, copied bool) bool {
	if copied {
		return o.link == "" && o.hash == hash
	}
	return o.link != "" && filepath.Clean(o.link) == filepath.Clean(link)
}

func (p *Plan) skillDir(sc Scope, sd SkillDir, opts Options) error {
	names := make([]string, 0, len(sd.Wants))
	for n := range sd.Wants {
		names = append(names, n)
	}
	sort.Strings(names)
	label := strings.Join(sd.Targets, ", ")
	for _, name := range names {
		want := sd.Wants[name]
		dest := filepath.Join(sd.Dir, name)
		st, managed := sc.State.Skill(sd.Dir, name)
		obs := observe(dest)
		if obs.err != nil {
			return fmt.Errorf("%s: %w", dest, obs.err)
		}
		c := Change{Scope: sc.Scope, Target: label, Kind: "skill", Name: name, File: dest}
		edited := managed && obs.exists && !obs.matches(st.Link, st.Hash, st.Copy)
		if want != nil {
			sk := want.Skill
			c.Origin = want.Origin
			c.Detail = "→ cache@" + Short(sk.Hash)
			if sk.Local {
				c.Detail, c.Link = "", sk.Dir
			}
			entry := state.SkillEntry{Source: sk.Source, Hash: sk.Hash, Link: sk.Dir, Origin: want.Origin}
			same := obs.exists && (obs.matches(sk.Dir, "", false) || obs.matches("", sk.Hash, true))
			switch {
			case managed && !obs.exists:
				c.Action, c.Reason = Create, "borrada a mano; se vuelve a crear"
			case same && (edited || !managed):
				c.Action, c.Reason = Adopt, "ya estaba así; pegboard la registra"
				entry.Copy = obs.link == ""
			case same:
				entry.Copy = st.Copy
				if st == entry {
					continue
				}
				c.Action, c.silent = Adopt, true
			case edited:
				c.Action, c.Forced, c.Reason = Conflict, Update, "cambiada a mano desde el último apply"
			case !managed && obs.exists:
				c.Action, c.Forced, c.Reason = Conflict, Update, "ya existe y no la puso pegboard"
			case managed:
				c.Action = Update
				c.Detail = Short(st.Hash) + " → " + Short(sk.Hash)
			default:
				c.Action = Create
			}
			c.do = resolve(c.Action, c.Forced, opts.Force)
			dir, ent := sd.Dir, entry
			c.run = func() error {
				copied, err := placeSkill(dest, sk.Dir, obs.exists)
				ent.Copy = copied
				return err
			}
			c.commit = func(s *state.State) { s.SetSkill(dir, name, ent) }
		} else {
			if !managed {
				continue
			}
			c.Origin = st.Origin
			switch {
			case !obs.exists:
				c.Action, c.Reason = Forget, "ya no estaba"
			case edited:
				c.Action, c.Forced, c.Reason = Conflict, Delete, "cambiada a mano; no se quita"
			default:
				c.Action = Delete
			}
			c.do = resolve(c.Action, c.Forced, opts.Force)
			dir := sd.Dir
			c.run = func() error { return removeEntry(dest) }
			c.commit = func(s *state.State) { s.DeleteSkill(dir, name) }
		}
		p.Changes = append(p.Changes, c)
	}
	return nil
}

func resolve(a, forced Action, force bool) Action {
	if a != Conflict {
		return a
	}
	if force {
		return forced
	}
	return ""
}

// Short abrevia un hash.
func Short(h string) string { return skills.Short(h) }

// placeSkill pone dest apuntando a src: un symlink atómico (se crea al lado
// y se renombra encima); si el sistema no permite symlinks, una copia.
// Devuelve si quedó como copia.
func placeSkill(dest, src string, replace bool) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	tmp := dest + ".pegboard-tmp"
	_ = os.RemoveAll(tmp)
	if err := os.Symlink(src, tmp); err == nil {
		if info, err := os.Lstat(dest); err == nil && info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
			// Una carpeta real no se puede reemplazar con rename.
			if err := os.RemoveAll(dest); err != nil {
				return false, err
			}
		}
		if err := os.Rename(tmp, dest); err != nil {
			// Windows no deja renombrar encima de un symlink a carpeta:
			// se quita el anterior y se reintenta (ahí deja de ser atómico).
			if rmErr := removeEntry(dest); rmErr != nil {
				_ = os.Remove(tmp)
				return false, err
			}
			if err := os.Rename(tmp, dest); err != nil {
				_ = os.Remove(tmp)
				return false, err
			}
		}
		return false, nil
	}
	if replace {
		if err := os.RemoveAll(dest); err != nil {
			return true, err
		}
	}
	if err := skills.CopyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return true, err
	}
	return true, os.Rename(tmp, dest)
}

// removeEntry quita un symlink (sin seguirlo) o una carpeta copiada.
func removeEntry(p string) error {
	info, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return os.Remove(p)
	}
	return os.RemoveAll(p)
}
