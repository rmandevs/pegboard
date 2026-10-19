// Package secrets resuelve las referencias ${…} de pegboard.yaml (SPEC §3.2).
// Los valores resueltos solo existen en memoria mientras se escribe el
// archivo del agente: nunca van al estado ni al lock.
package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Ref es una referencia: ${env:NOMBRE} → {env, NOMBRE}.
type Ref struct {
	Kind string
	Arg  string
}

func (r Ref) String() string { return "${" + r.Kind + ":" + r.Arg + "}" }

var refRe = regexp.MustCompile(`\$\{([a-z]+):([^}]*)\}`)

// Refs lista las referencias que contiene s.
func Refs(s string) []Ref {
	var out []Ref
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		out = append(out, Ref{Kind: m[1], Arg: m[2]})
	}
	return out
}

// Whole informa si s es exactamente una referencia, sin texto alrededor.
func Whole(s string) (Ref, bool) {
	m := refRe.FindStringSubmatch(s)
	if m == nil || m[0] != s {
		return Ref{}, false
	}
	return Ref{Kind: m[1], Arg: m[2]}, true
}

// Native traduce una referencia a la sintaxis propia del agente (p. ej.
// ${env:X} → ${X} en Claude Code). ok=false: el agente no la entiende y hay
// que escribir el valor.
type Native func(Ref) (string, bool)

// Resolver lee los valores de las referencias.
type Resolver struct {
	LookupEnv func(string) (string, bool)
	Home      string
	// BaseDir resuelve las rutas relativas de ${file:…}: la carpeta del stack.
	BaseDir string
}

// Value devuelve el valor de una referencia.
func (r Resolver) Value(ref Ref) (string, error) {
	switch ref.Kind {
	case "env":
		v, ok := r.LookupEnv(ref.Arg)
		if !ok {
			return "", fmt.Errorf("%s: la variable %s no está definida", ref, ref.Arg)
		}
		return v, nil
	case "file":
		b, err := os.ReadFile(r.FilePath(ref.Arg))
		if err != nil {
			return "", fmt.Errorf("%s: %w", ref, err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	case "op", "keychain":
		return "", fmt.Errorf("%s: los secretos %s: llegan en la fase 3", ref, ref.Kind)
	}
	return "", fmt.Errorf("%s: tipo de secreto desconocido %q", ref, ref.Kind)
}

// FilePath resuelve la ruta de ${file:…}: ~ es el home y las relativas
// parten de BaseDir.
func (r Resolver) FilePath(p string) string {
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		return filepath.Join(r.Home, p[1:])
	case filepath.IsAbs(p):
		return p
	}
	return filepath.Join(r.BaseDir, p)
}

// Expand reemplaza cada referencia de s: por su forma nativa si el agente la
// entiende, si no por su valor. plain=true si quedó algún valor en claro.
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

// Check valida que todas las referencias de s tengan un tipo conocido.
func Check(s string) error {
	for _, ref := range Refs(s) {
		switch ref.Kind {
		case "env", "file":
		case "op", "keychain":
			return fmt.Errorf("%s: los secretos %s: llegan en la fase 3", ref, ref.Kind)
		default:
			return fmt.Errorf("%s: tipo de secreto desconocido %q", ref, ref.Kind)
		}
	}
	return nil
}
