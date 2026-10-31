package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/rmandevs/agents-casc/internal/state"
)

// Result of Apply: which files were written and where their backups went.
type Result struct {
	Written []Written
}

type Written struct {
	File   string
	Backup string
}

// ErrBlocked: the plan has unresolved conflicts.
var ErrBlocked = errors.New("there are conflicts: resolve them manually or use --force")

// Apply writes the files the plan edited (backing up each one first) and
// then the state for each scope. backupDir is the backup directory.
func Apply(p *Plan, backupDir string, now time.Time) (*Result, error) {
	if p.Blocked() {
		return nil, ErrBlocked
	}
	res := &Result{}
	paths := make([]string, 0, len(p.docs))
	for path := range p.docs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !touched(p, path) {
			continue
		}
		out, err := p.docs[path].doc.Bytes()
		if err != nil {
			return res, err
		}
		if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, out) {
			continue
		}
		bk, err := state.Backup(backupDir, now, path)
		if err != nil {
			return res, err
		}
		if err := state.WriteAtomic(path, out, 0o644); err != nil {
			return res, err
		}
		res.Written = append(res.Written, Written{File: path, Backup: bk})
	}
	for _, c := range p.Changes {
		if c.run == nil {
			continue
		}
		switch c.do {
		case Create, Update, Delete:
			if err := c.run(); err != nil {
				return res, fmt.Errorf("%s: %w", c.File, err)
			}
			res.Written = append(res.Written, Written{File: c.File})
		}
	}
	for _, sc := range p.scopes {
		changed := false
		for _, c := range p.Changes {
			if c.Scope != sc.Scope {
				continue
			}
			if c.commit != nil {
				if c.do != "" {
					c.commit(sc.State)
					changed = true
				}
				continue
			}
			switch c.do {
			case Create, Update, Adopt:
				sc.State.Set(c.Target, c.Name, c.entry)
				changed = true
			case Delete, Forget:
				sc.State.Delete(c.Target, c.Name)
				changed = true
			}
		}
		if changed {
			if err := sc.State.Save(); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// touched reports whether any change writes to path.
func touched(p *Plan, path string) bool {
	for _, c := range p.Changes {
		if c.File == path && (c.do == Create || c.do == Update || c.do == Delete) {
			return true
		}
	}
	return false
}
