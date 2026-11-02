package engine

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/rmandevs/pegboard/internal/state"
)

// ContextFile is a context file the stack guarantees.
//   - Line == "": the file must exist; if missing, it is created with
//     Template. It is never deleted: once created it belongs to the user.
//   - Line != "": the file must contain that line (the @AGENTS.md bridge in
//     CLAUDE.md). If pegboard added it and it is no longer wanted, it
//     removes it.
type ContextFile struct {
	Path     string
	Target   string
	Line     string
	Template string
	Want     bool
}

func hasLine(content, line string) bool {
	for _, l := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

func (p *Plan) context(sc Scope, cf ContextFile) error {
	data, err := os.ReadFile(cf.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	content := string(data)
	c := Change{Scope: sc.Scope, Target: cf.Target, Kind: "context", File: cf.Path}
	path := cf.Path
	if cf.Line == "" {
		if !cf.Want || exists {
			return nil
		}
		c.Name, c.Action, c.do = baseName(cf.Path), Create, Create
		tpl := cf.Template
		c.run = func() error { return state.WriteAtomic(path, []byte(tpl), 0o644) }
		c.commit = func(*state.State) {}
		p.Changes = append(p.Changes, c)
		return nil
	}
	c.Name = cf.Line + " in " + baseName(cf.Path)
	_, managed := sc.State.Context[cf.Path]
	present := hasLine(content, cf.Line)
	line := cf.Line
	switch {
	case cf.Want && !present:
		c.Action = Create
		c.run = func() error {
			cur, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			s := string(cur)
			if s != "" && !strings.HasSuffix(s, "\n") {
				s += "\n"
			}
			if s != "" {
				s += "\n"
			}
			return state.WriteAtomic(path, []byte(s+line+"\n"), 0o644)
		}
		c.commit = func(s *state.State) { setContext(s, path, line) }
	case !cf.Want && managed && present:
		c.Action = Delete
		c.run = func() error {
			cur, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return state.WriteAtomic(path, []byte(removeLine(string(cur), line)), 0o644)
		}
		c.commit = func(s *state.State) { delete(s.Context, path) }
	case !cf.Want && managed:
		c.Action, c.Reason = Forget, "no longer present"
		c.commit = func(s *state.State) { delete(s.Context, path) }
	default:
		return nil
	}
	c.do = c.Action
	p.Changes = append(p.Changes, c)
	return nil
}

func setContext(s *state.State, path, line string) {
	if s.Context == nil {
		s.Context = map[string]state.ContextEntry{}
	}
	s.Context[path] = state.ContextEntry{Line: line}
}

// removeLine removes the line (and one blank line before it).
func removeLine(content, line string) string {
	lines := strings.Split(content, "\n")
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == line {
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
