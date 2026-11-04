package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/skills"
	"github.com/rmandevs/pegboard/internal/state"
	"github.com/rmandevs/pegboard/providers"
)

func newReplicateCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	f := &applyFlags{}
	var contextMode string
	cmd := &cobra.Command{
		Use:   "replicate <source> <target>…",
		Short: "Give other agents the MCPs and skills one agent already has",
		Long: `Read the MCPs and skills of the source agent and install them on the target
agents, with the same plan, state, and backups as apply. The source agent's
files are not changed, and pegboard.yaml is not needed: what it installs is
ad-hoc, so ` + "`pegboard adopt`" + ` can move it into pegboard.yaml later.

The source can be any detected agent. Plain-text secrets (env and headers) reach
the targets as ${env:…} references: export those variables. Skills that only
exist in the source agent's directory are copied to a directory pegboard owns and
linked from there.

The source's context file (CLAUDE.md, AGENTS.md, GEMINI.md) goes where --context
says: before or after what the target has, or replacing it, with a backup. Without
the flag, replicate asks, unless it runs with -y, --json, or --dry-run.`,
		Example: `  pegboard replicate claude-code codex
  pegboard replicate codex claude-code gemini --scope global
  pegboard replicate cursor claude-code --context after
  pegboard replicate hermes:work claude-code -y`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(opts.agents) > 0 {
				return errors.New("--agent does not apply here: name the agents as arguments")
			}
			source, targets := args[0], args[1:]
			var errs []error
			switch contextMode {
			case "", "skip", "before", "after", "replace":
			default:
				errs = append(errs, fmt.Errorf("--context %q is invalid: use before, after, replace, or skip", contextMode))
			}
			for _, id := range args {
				base, _, _ := strings.Cut(id, ":")
				if _, ok := providers.Get(base); !ok {
					errs = append(errs, fmt.Errorf("unknown agent %q", id))
				}
			}
			if slices.Contains(targets, source) {
				errs = append(errs, fmt.Errorf("%s is both the source and a target", source))
			}
			if err := errors.Join(errs...); err != nil {
				return err
			}
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			scopes, provs, err := opts.selection()
			if err != nil {
				return err
			}
			ctxs, err := opts.load(env, scopes, provs)
			if err != nil {
				return err
			}
			r := &replication{
				env: env, seen: map[string]bool{}, in: cmd.InOrStdin(), out: cmd.OutOrStdout(), contextMode: contextMode,
				ask: contextMode == "" && !f.yes && !f.dryRun && !opts.json,
			}
			for _, c := range ctxs {
				if err := r.scope(c, source, targets); err != nil {
					return err
				}
			}
			for _, id := range args {
				if !r.seen[id] {
					errs = append(errs, fmt.Errorf("%s was not detected; see `pegboard agents`", id))
				}
			}
			if err := errors.Join(errs...); err != nil {
				return err
			}
			if len(r.scopes) == 0 {
				for _, w := range r.warnings {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s has nothing to replicate.\n", source)
				return nil
			}
			if len(r.vars) > 0 {
				sort.Strings(r.vars)
				r.warnings = append(r.warnings, "secrets were replaced with ${env:…}; export: "+strings.Join(compact(r.vars), ", "))
			}
			return execute(cmd, opts, env, r.scopes, r.warnings, f, true, r.prepareAll)
		},
	}
	cmd.Flags().StringVar(&contextMode, "context", "", "copy the context file: before, after, or replace what the target has; skip to leave it")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show the plan and write nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite anything that already exists")
	f.safetyFlags(cmd, false)
	return cmd
}

// replication collects what a replicate run will do across scopes.
type replication struct {
	env      agent.Env
	seen     map[string]bool // agent arguments detected in some scope
	scopes   []engine.Scope
	warnings []string
	vars     []string
	prepares []func() error

	in          io.Reader
	out         io.Writer
	contextMode string
	ask         bool
}

func (r *replication) warnf(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// prepareAll runs after apply: a skill adopted because it was already in place
// runs no step of its own, but its state still points at the owned copy.
func (r *replication) prepareAll() error {
	for _, p := range r.prepares {
		if err := p(); err != nil {
			return err
		}
	}
	return nil
}

// scope adds the scope's part of the replication, if the source is there.
func (r *replication) scope(c *scopeCtx, source string, targets []string) error {
	src, err := pickSource(c.targets, source)
	if err != nil || src == nil {
		return err
	}
	r.seen[source] = true
	var dests []agent.Target
	for _, t := range c.targets {
		for _, id := range targets {
			if (t.ID() == id || t.Agent == id) && t.ID() != src.ID() {
				r.seen[id] = true
				dests = append(dests, t)
				break
			}
		}
	}
	if len(dests) == 0 {
		return nil
	}
	copies, err := r.contextCopies(c, *src, dests)
	if err != nil {
		return err
	}
	mcps, err := r.sourceMCPs(c, *src)
	if err != nil {
		return err
	}
	wanted, err := r.sourceSkills(c, *src, c.targets)
	if err != nil {
		return err
	}

	engScope := engine.Scope{Scope: c.scope, State: c.state, Resolver: c.resolver(), ContextCopies: copies}
	for _, t := range dests {
		p, ok := mcpProvider(t)
		if !ok {
			continue
		}
		wants := map[string]*engine.Want{}
		for name, m := range mcps {
			if e, ok := c.state.Get(t.ID(), name); ok && e.Origin == state.Declared {
				r.warnf("%s · %s: mcp %s is declared in pegboard.yaml; skipping", t.ID(), c.scope, name)
				continue
			}
			wants[name] = &engine.Want{MCP: m, Origin: state.Adhoc}
		}
		if len(wants) > 0 {
			engScope.Targets = append(engScope.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
		}
	}
	// skillDirs reads c.targets; a directory the source shares already has them.
	c.targets = dests
	engScope.Skills = c.skillDirs(func(t agent.Target) map[string]*engine.SkillWant {
		if t.Paths.Skills == src.Paths.Skills {
			return nil
		}
		out := map[string]*engine.SkillWant{}
		for name, w := range wanted {
			if e, ok := c.state.Skill(t.Paths.Skills, name); ok && e.Origin == state.Declared {
				r.warnf("%s · %s: skill %s is declared in pegboard.yaml; skipping", t.ID(), c.scope, name)
				continue
			}
			out[name] = w
		}
		return out
	}, nil)
	if len(engScope.Targets) > 0 || len(engScope.Skills) > 0 || len(engScope.ContextCopies) > 0 {
		r.scopes = append(r.scopes, engScope)
	}
	return nil
}

// contextCopies decides what happens with the source's context file. Hermes is
// left out both ways: SOUL.md is its persona, not instructions for a project.
func (r *replication) contextCopies(c *scopeCtx, src agent.Target, dests []agent.Target) ([]engine.ContextCopy, error) {
	file := src.Paths.Context
	if file == "" || !agent.Exists(file) || r.contextMode == "skip" {
		return nil, nil
	}
	if src.Agent == "hermes" {
		r.warnf("%s · %s: SOUL.md is its persona, so it is not replicated", src.ID(), c.scope)
		return nil, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	content := engine.CleanContext(string(data))
	if content == "" {
		return nil, nil
	}
	sourceReal := realPath(file)
	var order []string
	labels := map[string]string{}
	for _, t := range dests {
		if t.Agent == "hermes" || t.Paths.Context == "" {
			continue
		}
		path := realPath(t.Paths.Context)
		if path == sourceReal {
			continue
		}
		cur, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if engine.ContextPresent(string(cur), src.ID(), content) {
			continue
		}
		if importsFile(string(cur), path, sourceReal) {
			r.warnf("%s · %s: %s already reads %s", t.ID(), c.scope, r.env.Pretty(t.Paths.Context), r.env.Pretty(file))
			continue
		}
		if _, ok := labels[path]; !ok {
			order = append(order, path)
			labels[path] = t.ID()
		} else {
			labels[path] += ", " + t.ID()
		}
	}
	if len(order) == 0 {
		return nil, nil
	}
	mode := r.contextMode
	if mode == "" && r.ask {
		mode = r.askContext(file, order)
	}
	if mode == "" || mode == "skip" {
		r.warnf("%s · %s: %s was not copied; use --context before, after, or replace", src.ID(), c.scope, r.env.Pretty(file))
		return nil, nil
	}
	if strings.Contains("\n"+content, "\n@") {
		r.warnf("%s · %s: %s has @ imports, and not every agent follows them", src.ID(), c.scope, r.env.Pretty(file))
	}
	var out []engine.ContextCopy
	for _, path := range order {
		out = append(out, engine.ContextCopy{
			Path: path, Target: labels[path], Source: src.ID(), Content: content, Mode: engine.ContextMode(mode),
		})
	}
	return out, nil
}

// askContext reads a byte at a time: the plan's confirmation reads the same stdin.
func (r *replication) askContext(file string, paths []string) string {
	pretty := make([]string, len(paths))
	for i, p := range paths {
		pretty[i] = r.env.Pretty(p)
	}
	fmt.Fprintf(r.out, "Copy %s to %s?\n  [s]kip, [b]efore what's there, [a]fter what's there, [r]eplace it: ", r.env.Pretty(file), strings.Join(pretty, ", "))
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := r.in.Read(buf)
		if n > 0 && buf[0] != '\n' {
			line = append(line, buf[0])
		}
		if err != nil || (n > 0 && buf[0] == '\n') {
			break
		}
	}
	switch strings.ToLower(strings.TrimSpace(string(line))) {
	case "b", "before":
		return "before"
	case "a", "after":
		return "after"
	case "r", "replace":
		return "replace"
	}
	return "skip"
}

// realPath follows links, so a CLAUDE.md linked to AGENTS.md counts as one file
// and writes go through the link. A missing file keeps its path.
func realPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// importsFile reports whether a context file has an @ line that reads target.
func importsFile(content, path, target string) bool {
	for _, l := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "@") && realPath(filepath.Join(filepath.Dir(path), t[1:])) == target {
			return true
		}
	}
	return false
}

// pickSource finds the source target: the exact ID, or the agent's only one.
func pickSource(ts []agent.Target, source string) (*agent.Target, error) {
	var byAgent []agent.Target
	for i, t := range ts {
		if t.ID() == source {
			return &ts[i], nil
		}
		if t.Agent == source {
			byAgent = append(byAgent, t)
		}
	}
	switch len(byAgent) {
	case 0:
		return nil, nil
	case 1:
		return &byAgent[0], nil
	}
	return nil, fmt.Errorf("%s has several profiles (%s); name one as the source", source, targetIDs(byAgent))
}

// sourceMCPs reads the source's MCPs with their secrets as references.
func (r *replication) sourceMCPs(c *scopeCtx, src agent.Target) (map[string]mcp.MCP, error) {
	p, ok := mcpProvider(src)
	if !ok {
		return nil, nil
	}
	f := p.MCPFile(src)
	d, err := native.Open(f)
	if err != nil {
		return nil, err
	}
	entries, err := d.Entries()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	out := map[string]mcp.MCP{}
	for name, obs := range entries {
		m, err := p.DecodeMCP(obs)
		if err != nil {
			r.warnf("%s · %s: mcp %s could not be read (%v); skipping", src.ID(), c.scope, name, err)
			continue
		}
		m, _, vars := toReferences(name, m)
		m.Agents = nil
		r.vars = append(r.vars, vars...)
		out[name] = m
	}
	return out, nil
}

// sourceSkills lists what is in the source's skills directory. all is every
// target in the scope, to recognize a link into another agent's directory.
func (r *replication) sourceSkills(c *scopeCtx, src agent.Target, all []agent.Target) (map[string]*engine.SkillWant, error) {
	dir := src.Paths.Skills
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]*engine.SkillWant{}
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		want, err := r.sourceSkill(c, src, name, all)
		if err != nil {
			return nil, err
		}
		if want != nil {
			out[name] = want
		}
	}
	return out, nil
}

func (r *replication) sourceSkill(c *scopeCtx, src agent.Target, name string, all []agent.Target) (*engine.SkillWant, error) {
	path := filepath.Join(src.Paths.Skills, name)
	// Placed by pegboard: the same source and content, without any network.
	if e, ok := c.state.Skill(src.Paths.Skills, name); ok && e.Link != "" && agent.Exists(e.Link) {
		parsed, err := skills.Parse(e.Source)
		if err != nil {
			return nil, err
		}
		sk := skills.Skill{Name: name, Source: e.Source, Dir: e.Link, Hash: e.Hash, Local: parsed.Kind == skills.Local}
		if sk.Local {
			if sk.Hash, err = skills.Hash(e.Link); err != nil {
				return nil, err
			}
		}
		return &engine.SkillWant{Skill: sk, Origin: state.Adhoc}, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		r.warnf("%s · %s: skill %s could not be read (%v); skipping", src.ID(), c.scope, name, err)
		return nil, nil
	}
	if !agent.Exists(filepath.Join(resolved, "SKILL.md")) {
		return nil, nil
	}
	found, err := skills.Discover(resolved)
	if err != nil {
		r.warnf("%s · %s: skill %s could not be read (%v); skipping", src.ID(), c.scope, name, err)
		return nil, nil
	}
	if _, ok := found[name]; !ok {
		r.warnf("%s · %s: the SKILL.md in %s has another name; skipping", src.ID(), c.scope, r.env.Pretty(path))
		return nil, nil
	}
	hash, err := skills.Hash(resolved)
	if err != nil {
		return nil, err
	}
	// A link to the user's own directory stays live: link to the same place.
	if info.Mode()&fs.ModeSymlink != 0 && !insideSkillDir(resolved, all) {
		sk := skills.Skill{Name: name, Source: r.tilde(resolved), Dir: resolved, Hash: hash, Local: true}
		return &engine.SkillWant{Skill: sk, Origin: state.Adhoc}, nil
	}
	return r.ownedCopy(c, src, name, resolved, hash)
}

// ownedCopy keeps a skill out of any agent's directory, so removing it from one
// agent never takes it from the others. Nothing is copied until apply.
func (r *replication) ownedCopy(c *scopeCtx, src agent.Target, name, from, hash string) (*engine.SkillWant, error) {
	dest := filepath.Join(r.env.ConfigDir(), "skills", name)
	source := r.tilde(dest)
	if c.scope == agent.Project {
		dest = filepath.Join(r.env.ProjectDir, "skills", name)
		source = "./skills/" + name
	}
	if agent.Exists(dest) {
		have, err := skills.Hash(dest)
		if err != nil {
			return nil, err
		}
		if have != hash {
			r.warnf("%s · %s: skill %s differs from the one in %s; skipping", src.ID(), c.scope, name, r.env.Pretty(dest))
			return nil, nil
		}
	}
	prepare := func() error {
		if agent.Exists(dest) {
			return nil
		}
		return skills.CopyDir(from, dest)
	}
	r.prepares = append(r.prepares, prepare)
	sk := skills.Skill{Name: name, Source: source, Dir: dest, Hash: hash, Local: true}
	return &engine.SkillWant{Skill: sk, Origin: state.Adhoc, Prepare: prepare}, nil
}

func insideSkillDir(path string, ts []agent.Target) bool {
	for _, t := range ts {
		if t.Paths.Skills == "" {
			continue
		}
		dir := t.Paths.Skills
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		if rel, err := filepath.Rel(dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// tilde writes a path as a source that works from any stack directory.
func (r *replication) tilde(p string) string {
	if rel, err := filepath.Rel(r.env.Home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}
