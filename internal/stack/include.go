package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/skills"
)

// maxDepth is how deep includes can nest (SPEC §3.5).
const maxDepth = 10

// Fetcher brings a remote include (git or zip) to disk; base is the
// directory of the stack that declares it.
type Fetcher func(in skills.Include, base string) (skills.Fetched, error)

// Origin says where a resource of the merged stack was declared.
type Origin struct {
	// Via is the include that declared it, as written there; "" = the root.
	Via string
	// Remote: it comes from a git or zip stack (or from a stack those
	// include), so it is third-party code.
	Remote bool
}

// ResolvedSkill is a skill source of the merged stack.
type ResolvedSkill struct {
	Ref SkillRef // local sources are absolute when they come from an include
	Origin
	// Removes are the remove.skills lists that apply to it: those of the
	// stack that declares it and of every stack above.
	Removes []string
}

// Removed reports whether the skill (by source or name) was removed.
func (r ResolvedSkill) Removed(name string) bool {
	for _, x := range r.Removes {
		if x == r.Ref.Source || x == name {
			return true
		}
	}
	return false
}

// Policy is the policy of one stack of the tree; all of them are checked
// against the final result, so an included stack cannot be relaxed.
type Policy struct {
	From              string // path or include of the stack that declared it
	AllowMCPs         []string
	AllowSkillSources []string
}

// IncludeInfo is an include that was loaded.
type IncludeInfo struct {
	Raw    string
	Dir    string
	Remote bool
	Commit string
	Hash   string
}

// Resolved is the root stack merged with its includes.
type Resolved struct {
	MCPs      map[string]mcp.MCP
	MCPOrigin map[string]Origin
	Skills    []ResolvedSkill
	Policies  []Policy
	Includes  []IncludeInfo
	Warnings  []string
}

// node is a stack of the tree while merging.
type node struct {
	st     *Stack
	label  string // "" for the root; the include as written otherwise
	dir    string
	remote bool
	root   string // remote stacks cannot reach outside this directory
}

type merger struct {
	fetch    Fetcher
	seen     map[string]bool   // includes already loaded (first position wins)
	refs     map[string]string // repo//path → ref
	res      *Resolved
	visiting map[string]bool
}

// Resolve merges the stack with its includes (SPEC §3.5). fetch may be nil
// if the stack has only local includes.
func (s *Stack) Resolve(fetch Fetcher) (*Resolved, error) {
	m := &merger{
		fetch: fetch, seen: map[string]bool{}, refs: map[string]string{}, visiting: map[string]bool{},
		res: &Resolved{MCPs: map[string]mcp.MCP{}, MCPOrigin: map[string]Origin{}},
	}
	root := &node{st: s, dir: filepath.Dir(s.Path)}
	mcps, origins, sk, err := m.merge(root, 0)
	if err != nil {
		return nil, err
	}
	m.res.MCPs, m.res.MCPOrigin, m.res.Skills = mcps, origins, sk
	if err := m.res.check(); err != nil {
		return nil, err
	}
	return m.res, nil
}

// merge returns what a stack contributes: its includes in order, then its
// own entries, minus its remove.
func (m *merger) merge(n *node, depth int) (map[string]mcp.MCP, map[string]Origin, []ResolvedSkill, error) {
	if depth > maxDepth {
		return nil, nil, nil, fmt.Errorf("%s: includes nest more than %d levels", n.where(), maxDepth)
	}
	key := n.dir
	if m.visiting[key] {
		return nil, nil, nil, fmt.Errorf("%s: include cycle", n.where())
	}
	m.visiting[key] = true
	defer delete(m.visiting, key)

	mcps := map[string]mcp.MCP{}
	origins := map[string]Origin{}
	var sks []ResolvedSkill
	for _, raw := range n.st.Include {
		child, err := m.load(n, raw)
		if err != nil {
			return nil, nil, nil, err
		}
		if child == nil {
			continue // already included earlier
		}
		cm, co, cs, err := m.merge(child, depth+1)
		if err != nil {
			return nil, nil, nil, err
		}
		for name, v := range cm {
			mcps[name], origins[name] = v, co[name]
		}
		sks = appendSkills(sks, cs)
	}
	own := Origin{Via: n.label, Remote: n.remote}
	for name, v := range n.st.MCPs {
		if n.label != "" {
			var err error
			if v, err = m.rebaseMCP(n, name, v); err != nil {
				return nil, nil, nil, err
			}
		}
		mcps[name], origins[name] = v, own
	}
	var mine []ResolvedSkill
	for _, ref := range n.st.Skills {
		if n.label != "" {
			var err error
			if ref, err = rebaseSkill(n, ref); err != nil {
				return nil, nil, nil, err
			}
		}
		mine = append(mine, ResolvedSkill{Ref: ref, Origin: own})
	}
	sks = appendSkills(sks, mine)
	for _, name := range n.st.Remove.MCPs {
		delete(mcps, name)
		delete(origins, name)
	}
	if len(n.st.Remove.Skills) > 0 {
		for i := range sks {
			sks[i].Removes = append(sks[i].Removes, n.st.Remove.Skills...)
		}
	}
	if p := n.st.Policy; len(p.AllowMCPs) > 0 || len(p.AllowSkillSources) > 0 {
		m.res.Policies = append(m.res.Policies, Policy{From: n.where(), AllowMCPs: p.AllowMCPs, AllowSkillSources: p.AllowSkillSources})
	}
	return mcps, origins, sks, nil
}

func appendSkills(dst, src []ResolvedSkill) []ResolvedSkill {
	for _, s := range src {
		dup := false
		for i := range dst {
			if dst[i].Ref.Source == s.Ref.Source {
				dup = true
				dst[i].Removes = append(dst[i].Removes, s.Removes...)
			}
		}
		if !dup {
			dst = append(dst, s)
		}
	}
	return dst
}

func (n *node) where() string {
	if n.label == "" {
		return n.st.Path
	}
	return "include " + n.label
}

// load reads an include of n; nil if it was already loaded.
func (m *merger) load(n *node, raw string) (*node, error) {
	in, err := skills.ParseInclude(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", n.where(), err)
	}
	child := &node{label: raw, remote: n.remote, root: n.root}
	var info IncludeInfo
	switch in.Kind {
	case skills.IncludeLocal:
		dir, err := m.localPath(n, in.Dir, "include")
		if err != nil {
			return nil, err
		}
		child.dir = dir
		if !n.remote {
			child.root = ""
		}
		info = IncludeInfo{Raw: raw, Dir: dir, Remote: n.remote}
	default:
		if in.Kind == skills.IncludeZip && in.Dir != "" {
			if _, err := m.localPath(n, in.Dir, "include"); err != nil {
				return nil, err
			}
		}
		if in.Kind == skills.IncludeGit {
			if prev, ok := m.refs[in.Key()]; ok && prev != in.Ref() {
				return nil, fmt.Errorf("%s: %s is included with two refs (%q and %q)", n.where(), in.Key(), prev, in.Ref())
			}
			m.refs[in.Key()] = in.Ref()
		}
		if m.fetch == nil {
			return nil, fmt.Errorf("%s: cannot fetch %s here", n.where(), raw)
		}
		f, err := m.fetch(in, n.dir)
		if err != nil {
			return nil, err
		}
		child.dir, child.remote, child.root = f.Dir, true, f.Dir
		info = IncludeInfo{Raw: raw, Dir: f.Dir, Remote: true, Commit: f.Commit, Hash: f.Hash}
	}
	seenKey := child.dir
	if m.visiting[seenKey] {
		return nil, fmt.Errorf("%s: include cycle through %s", n.where(), raw)
	}
	if m.seen[seenKey] {
		return nil, nil
	}
	m.seen[seenKey] = true
	st, err := Load(filepath.Join(child.dir, FileName))
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("%s: include %s has no %s", n.where(), raw, FileName)
	}
	child.st = st
	if len(st.Agents) > 0 || st.Inherit != "" || st.Context.AgentsMD {
		m.res.Warnings = append(m.res.Warnings,
			fmt.Sprintf("include %s: agents, inherit, and context are decided by the root stack; ignoring them", raw))
	}
	m.res.Includes = append(m.res.Includes, info)
	return child, nil
}

// localPath resolves a path written in stack n. A remote stack can only
// point inside its own root.
func (m *merger) localPath(n *node, p, what string) (string, error) {
	if n.remote {
		if strings.HasPrefix(p, "~") || filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
			return "", fmt.Errorf("%s: %s %s: a remote stack can only use paths inside itself", n.where(), what, p)
		}
		abs := filepath.Join(n.dir, filepath.FromSlash(p))
		if rel, err := filepath.Rel(n.root, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%s: %s %s escapes the stack", n.where(), what, p)
		}
		return abs, nil
	}
	if strings.HasPrefix(p, "~/") || filepath.IsAbs(p) {
		return p, nil // ~ is expanded by whoever reads it
	}
	return filepath.Join(n.dir, filepath.FromSlash(p)), nil
}

// rebaseSkill makes a local source of an included stack absolute.
func rebaseSkill(n *node, ref SkillRef) (SkillRef, error) {
	src, err := skills.Parse(ref.Source)
	if err != nil || src.Kind != skills.Local {
		return ref, err
	}
	if n.remote {
		if strings.HasPrefix(src.Dir, "~") || filepath.IsAbs(src.Dir) || strings.HasPrefix(src.Dir, "/") {
			return ref, fmt.Errorf("%s: skill %s: a remote stack can only use paths inside itself", n.where(), ref.Source)
		}
	}
	m := &merger{}
	abs, err := m.localPath(n, src.Dir, "skill")
	if err != nil {
		return ref, err
	}
	if !strings.HasPrefix(abs, "~/") && !filepath.IsAbs(abs) {
		abs, _ = filepath.Abs(abs)
	}
	ref.Source = abs
	return ref, nil
}

var fileRefRe = regexp.MustCompile(`\$\{file:([^}]*)\}`)

// rebaseMCP makes the ${file:…} of an included stack point to its own
// directory, and forbids a remote stack from reading outside itself.
func (m *merger) rebaseMCP(n *node, name string, v mcp.MCP) (mcp.MCP, error) {
	var err error
	fix := func(s string) string {
		return fileRefRe.ReplaceAllStringFunc(s, func(match string) string {
			p := fileRefRe.FindStringSubmatch(match)[1]
			abs, e := m.localPath(n, p, "mcps."+name+" ${file:…}")
			if e != nil && err == nil {
				err = e
			}
			return "${file:" + abs + "}"
		})
	}
	out := v
	out.Command, out.URL = fix(v.Command), fix(v.URL)
	out.Args = nil
	for _, a := range v.Args {
		out.Args = append(out.Args, fix(a))
	}
	out.Env = remap(v.Env, fix)
	out.Headers = remap(v.Headers, fix)
	return out, err
}

func remap(in map[string]string, f func(string) string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = f(v)
	}
	return out
}

// check runs every policy of the tree against the result.
func (r *Resolved) check() error {
	for _, p := range r.Policies {
		if len(p.AllowMCPs) > 0 {
			var denied []string
			for n := range r.MCPs {
				if !contains(p.AllowMCPs, n) {
					denied = append(denied, n)
				}
			}
			if len(denied) > 0 {
				sort.Strings(denied)
				return fmt.Errorf("%s: policy.allow_mcps does not allow: %s", p.From, strings.Join(denied, ", "))
			}
		}
		if len(p.AllowSkillSources) > 0 {
			var denied []string
			for _, s := range r.Skills {
				if !s.Removed("") && !MatchesAny(p.AllowSkillSources, s.Ref.Source) {
					denied = append(denied, s.Ref.Source)
				}
			}
			if len(denied) > 0 {
				return fmt.Errorf("%s: policy.allow_skill_sources does not allow: %s", p.From, strings.Join(denied, ", "))
			}
		}
	}
	return nil
}

// MatchesAny reports whether source matches one of the patterns, where *
// matches any text (including /): "acme/*", "github.com/acme/*", "./skills/*".
func MatchesAny(patterns []string, source string) bool {
	for _, p := range patterns {
		re := "^" + strings.ReplaceAll(regexp.QuoteMeta(p), `\*`, ".*") + "$"
		if ok, _ := regexp.MatchString(re, source); ok {
			return true
		}
	}
	return false
}

// SkillSources lists the merged sources.
func (r *Resolved) SkillSources() []string {
	out := make([]string, len(r.Skills))
	for i, s := range r.Skills {
		out[i] = s.Ref.Source
	}
	return out
}

// Fingerprint identifies what an MCP runs, without secret values: the
// command or URL, the args, and the names of its variables and headers.
func Fingerprint(m mcp.MCP) string {
	keys := func(mm map[string]string) []string {
		var out []string
		for k := range mm {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	b, _ := json.Marshal(struct {
		Command string   `json:"command,omitempty"`
		Args    []string `json:"args,omitempty"`
		URL     string   `json:"url,omitempty"`
		Env     []string `json:"env,omitempty"`
		Headers []string `json:"headers,omitempty"`
	}{m.Command, m.Args, m.URL, keys(m.Env), keys(m.Headers)})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Review explains why an MCP from a remote include needs --approve, or ""
// if the lock already approved it as is.
func Review(lock *skills.Lock, via, name string, m mcp.MCP) string {
	fp, ok := lock.Approved(via, name)
	switch {
	case !ok:
		return "new MCP from included stack " + via
	case fp != Fingerprint(m):
		return "changed in included stack " + via
	}
	return ""
}
