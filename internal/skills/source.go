// Package skills resuelve skills (SPEC §5, "Skills: cómo se instalan"): de
// dónde vienen, su contenido exacto (hash) y su copia única en el caché.
//
// Las fuentes usan las mismas convenciones que `npx skills` y skills.sh, para
// que un repo de skills sirva igual a los dos:
//
//	owner/repo                     todas las skills de un repo de GitHub
//	owner/repo@skill               una sola skill del repo
//	github.com/owner/repo//ruta    host + subcarpeta (estilo go-getter)
//	https://github.com/o/r/tree/<ref>/<ruta>
//	https://… o git@…              cualquier repo git (admite //ruta)
//	./ruta, ../ruta, /ruta, ~/ruta skill local (se enlaza tal cual)
//	pegboard                       la skill propia de pegboard
//
// Las fuentes git aceptan ?ref=<rama|tag|commit> al final.
package skills

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind es el tipo de fuente.
type Kind string

const (
	Git     Kind = "git"
	Local   Kind = "local"
	Builtin Kind = "builtin"
)

// Source es una fuente de skills ya interpretada.
type Source struct {
	Raw   string
	Kind  Kind
	URL   string // repo git
	Path  string // subcarpeta dentro del repo
	Ref   string // rama, tag o commit; "" = la rama por defecto
	Skill string // solo esta skill (owner/repo@skill)
	Dir   string // fuente local, sin resolver contra la carpeta del stack
}

var (
	shortRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)/([A-Za-z0-9_.-]+)$`)
	hostRe  = regexp.MustCompile(`^([a-z0-9-]+(?:\.[a-z0-9-]+)+)/([^/]+)/([^/]+?)(?:\.git)?$`)
	treeRe  = regexp.MustCompile(`^https://(github\.com|gitlab\.com)/([^/]+)/([^/]+)/(?:-/)?tree/([^/]+)(?:/(.+))?$`)
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// GitHub es la base de los atajos owner/repo; los tests la cambian.
var GitHub = "https://github.com/"

// Parse interpreta una fuente.
func Parse(raw string) (Source, error) {
	s := Source{Raw: raw}
	rest := strings.TrimSpace(raw)
	if rest == "" {
		return s, fmt.Errorf("fuente vacía")
	}
	if rest == "pegboard" {
		s.Kind = Builtin
		return s, nil
	}
	if strings.HasPrefix(rest, "./") || strings.HasPrefix(rest, "../") || rest == "." ||
		strings.HasPrefix(rest, "~/") || filepath.IsAbs(rest) || strings.HasPrefix(rest, "/") {
		s.Kind, s.Dir = Local, rest
		return s, nil
	}
	s.Kind = Git
	if i := strings.LastIndex(rest, "?ref="); i >= 0 {
		s.Ref, rest = rest[i+len("?ref="):], rest[:i]
		if s.Ref == "" {
			return s, fmt.Errorf("%s: ?ref= vacío", raw)
		}
	}
	if m := treeRe.FindStringSubmatch(rest); m != nil {
		if s.Ref != "" {
			return s, fmt.Errorf("%s: el ref ya va en la URL /tree/", raw)
		}
		s.URL = "https://" + m[1] + "/" + m[2] + "/" + strings.TrimSuffix(m[3], ".git") + ".git"
		s.Ref, s.Path = m[4], m[5]
		return s, nil
	}
	// //subcarpeta (después del esquema, si lo hay).
	scheme := ""
	if i := strings.Index(rest, "://"); i >= 0 {
		scheme, rest = rest[:i+3], rest[i+3:]
	}
	if i := strings.Index(rest, "//"); i >= 0 {
		s.Path, rest = strings.Trim(rest[i+2:], "/"), rest[:i]
	}
	switch {
	case scheme != "" || strings.HasPrefix(rest, "git@"):
		s.URL = scheme + rest
	case shortRe.MatchString(strings.SplitN(rest, "@", 2)[0]):
		repo, skill, has := strings.Cut(rest, "@")
		if has {
			if !nameRe.MatchString(skill) {
				return s, fmt.Errorf("%s: nombre de skill inválido %q", raw, skill)
			}
			s.Skill = skill
		}
		s.URL = GitHub + strings.TrimSuffix(repo, ".git") + ".git"
	case hostRe.MatchString(rest):
		m := hostRe.FindStringSubmatch(rest)
		s.URL = "https://" + m[1] + "/" + m[2] + "/" + m[3] + ".git"
	default:
		return s, fmt.Errorf("fuente de skill no reconocida %q (ver `pegboard install --help`)", raw)
	}
	return s, nil
}

// FileURL es la URL file:// de una carpeta local (file:///C:/… en Windows).
func FileURL(dir string) string {
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
