// Package skills resolves skills (SPEC §5, "Skills: how they are installed"):
// where they come from, their exact content (hash), and their single copy in
// the cache.
//
// Sources use the same conventions as `npx skills` and skills.sh, so one
// skills repository works with both:
//
//	owner/repo                     all skills in a GitHub repo
//	owner/repo@skill               one skill from the repo
//	github.com/owner/repo//path    host + subdirectory (go-getter style)
//	https://github.com/o/r/tree/<ref>/<path>
//	https://… or git@…             any git repo (supports //path)
//	./path, ../path, /path, ~/path local skill (linked as-is)
//	pegboard                       pegboard's own skill
//
// Git sources accept ?ref=<branch|tag|commit> at the end.
package skills

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind is the source type.
type Kind string

const (
	Git     Kind = "git"
	Local   Kind = "local"
	Builtin Kind = "builtin"
)

// Source is an already parsed skill source.
type Source struct {
	Raw   string
	Kind  Kind
	URL   string // git repo
	Path  string // subdirectory inside the repo
	Ref   string // branch, tag, or commit; "" = default branch
	Skill string // only this skill (owner/repo@skill)
	Dir   string // local source, unresolved against the stack directory
}

var (
	shortRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)/([A-Za-z0-9_.-]+)$`)
	hostRe  = regexp.MustCompile(`^([a-z0-9-]+(?:\.[a-z0-9-]+)+)/([^/]+)/([^/]+?)(?:\.git)?$`)
	treeRe  = regexp.MustCompile(`^https://(github\.com|gitlab\.com)/([^/]+)/([^/]+)/(?:-/)?tree/([^/]+)(?:/(.+))?$`)
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// GitHub is the base for the owner/repo shortcuts; tests replace it.
var GitHub = "https://github.com/"

// Parse interprets a source.
func Parse(raw string) (Source, error) {
	s := Source{Raw: raw}
	rest := strings.TrimSpace(raw)
	if rest == "" {
		return s, fmt.Errorf("empty source")
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
			return s, fmt.Errorf("%s: empty ?ref=", raw)
		}
	}
	if m := treeRe.FindStringSubmatch(rest); m != nil {
		if s.Ref != "" {
			return s, fmt.Errorf("%s: the ref is already in the /tree/ URL", raw)
		}
		s.URL = "https://" + m[1] + "/" + m[2] + "/" + strings.TrimSuffix(m[3], ".git") + ".git"
		s.Ref, s.Path = m[4], m[5]
		return s, nil
	}
	// //subdirectory (after the scheme, if any).
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
				return s, fmt.Errorf("%s: invalid skill name %q", raw, skill)
			}
			s.Skill = skill
		}
		s.URL = GitHub + strings.TrimSuffix(repo, ".git") + ".git"
	case hostRe.MatchString(rest):
		m := hostRe.FindStringSubmatch(rest)
		s.URL = "https://" + m[1] + "/" + m[2] + "/" + m[3] + ".git"
	default:
		return s, fmt.Errorf("unrecognized skill source %q (see `pegboard install --help`)", raw)
	}
	return s, nil
}

// FileURL is the file:// URL of a local directory (file:///C:/… on Windows).
func FileURL(dir string) string {
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
