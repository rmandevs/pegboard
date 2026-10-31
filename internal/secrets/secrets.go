// Package secrets resolves the ${…} references in pegboard.yaml (SPEC §3.2).
// Resolved values only exist in memory while the agent's file is being
// written: they never go into the state or the lock.
package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Ref is a reference: ${env:NAME} → {env, NAME}.
type Ref struct {
	Kind string
	Arg  string
}

func (r Ref) String() string { return "${" + r.Kind + ":" + r.Arg + "}" }

var refRe = regexp.MustCompile(`\$\{([a-z]+):([^}]*)\}`)

// Refs lists the references contained in s.
func Refs(s string) []Ref {
	var out []Ref
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		out = append(out, Ref{Kind: m[1], Arg: m[2]})
	}
	return out
}

// Whole reports whether s is exactly one reference, with no surrounding
// text.
func Whole(s string) (Ref, bool) {
	m := refRe.FindStringSubmatch(s)
	if m == nil || m[0] != s {
		return Ref{}, false
	}
	return Ref{Kind: m[1], Arg: m[2]}, true
}

// Native translates a reference to the agent's own syntax (for example,
// ${env:X} → ${X} in Claude Code). ok=false means the agent does not
// understand it and the value must be written.
type Native func(Ref) (string, bool)

// Resolver reads the values of references.
type Resolver struct {
	LookupEnv func(string) (string, bool)
	Home      string
	// BaseDir resolves relative paths in ${file:…}: the stack's directory.
	BaseDir string
}

// Value returns the value of a reference.
func (r Resolver) Value(ref Ref) (string, error) {
	switch ref.Kind {
	case "env":
		v, ok := r.LookupEnv(ref.Arg)
		if !ok {
			return "", fmt.Errorf("%s: variable %s is not defined", ref, ref.Arg)
		}
		return v, nil
	case "file":
		b, err := os.ReadFile(r.FilePath(ref.Arg))
		if err != nil {
			return "", fmt.Errorf("%s: %w", ref, err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	case "op", "keychain":
		return "", fmt.Errorf("%s: %s secrets arrive in phase 3", ref, ref.Kind)
	}
	return "", fmt.Errorf("%s: unknown secret kind %q", ref, ref.Kind)
}

// FilePath resolves the path of ${file:…}: ~ is the home directory and
// relative paths start from BaseDir.
func (r Resolver) FilePath(p string) string {
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		return filepath.Join(r.Home, p[1:])
	case filepath.IsAbs(p):
		return p
	}
	return filepath.Join(r.BaseDir, p)
}

// Expand replaces each reference in s: with its native form if the agent
// understands it, otherwise with its value. plain=true if any value was
// left in plain text.
func (r Resolver) Expand(s string, native Native) (out string, plain bool, err error) {
	out = refRe.ReplaceAllStringFunc(s, func(m string) string {
		if err != nil {
			return m
		}
		sub := refRe.FindStringSubmatch(m)
		ref := Ref{Kind: sub[1], Arg: sub[2]}
		if native != nil {
			if n, ok := native(ref); ok {
				return n
			}
		}
		v, e := r.Value(ref)
		if e != nil {
			err = e
			return m
		}
		plain = true
		return v
	})
	return out, plain, err
}

// Check validates that every reference in s has a known kind.
func Check(s string) error {
	for _, ref := range Refs(s) {
		switch ref.Kind {
		case "env", "file":
		case "op", "keychain":
			return fmt.Errorf("%s: %s secrets arrive in phase 3", ref, ref.Kind)
		default:
			return fmt.Errorf("%s: unknown secret kind %q", ref, ref.Kind)
		}
	}
	return nil
}
