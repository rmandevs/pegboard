package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/secrets"
	"github.com/rmandevs/pegboard/internal/skills"
	"github.com/rmandevs/pegboard/internal/stack"
	"github.com/rmandevs/pegboard/internal/state"
	"github.com/rmandevs/pegboard/providers"
	"github.com/rmandevs/pegboard/skill"
)

// scopeCtx is everything loaded for a scope.
type scopeCtx struct {
	scope   agent.Scope
	stack   *stack.Stack // nil if there is no pegboard.yaml
	state   *state.State
	targets []agent.Target // detected and filtered by --agent
	env     agent.Env
	lock    *skills.Lock // nil if there is no stack
}

func statePath(env agent.Env, scope agent.Scope) string {
	if scope == agent.Global {
		return filepath.Join(env.StateDir(), "state.json")
	}
	return filepath.Join(env.ProjectDir, ".pegboard", "state.json")
}

// load loads the stack, state, and targets for each scope.
func (o *options) load(env agent.Env, scopes []agent.Scope, provs []agent.Provider) ([]*scopeCtx, error) {
	var out []*scopeCtx
	for _, sc := range scopes {
		st, err := stack.Load(stack.Path(env, sc))
		if err != nil {
			return nil, err
		}
		state, err := state.Load(statePath(env, sc))
		if err != nil {
			return nil, err
		}
		c := &scopeCtx{
			scope:   sc,
			stack:   st,
			state:   state,
			targets: providers.Detect(env, provs, []agent.Scope{sc}),
			env:     env,
		}
		if st != nil {
			if c.lock, err = skills.LoadLock(st.LockPath()); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (c *scopeCtx) resolver() secrets.Resolver {
	base := c.env.ProjectDir
	if c.stack != nil {
		base = filepath.Dir(c.stack.Path)
	}
	return secrets.Resolver{LookupEnv: c.env.LookupEnv, Home: c.env.Home, BaseDir: base}
}

func (c *scopeCtx) hasDeclared() bool {
	for _, tid := range c.state.TargetIDs() {
		for _, n := range c.state.Names(tid) {
			if e, _ := c.state.Get(tid, n); e.Origin == state.Declared {
				return true
			}
		}
	}
	for _, d := range c.state.SkillDirs() {
		for _, n := range c.state.SkillNames(d) {
			if e, _ := c.state.Skill(d, n); e.Origin == state.Declared {
				return true
			}
		}
	}
	return len(c.state.Context) > 0
}

// skillResolver resolves skill sources for this scope.
func (c *scopeCtx) skillResolver(lock *skills.Lock) *skills.Resolver {
	return &skills.Resolver{
		Cache:   c.env.CacheDir(),
		Home:    c.env.Home,
		BaseDir: c.resolver().BaseDir,
		Lock:    lock,
		Builtin: skill.FS(),
	}
}

// resolveSkills resolves a list of sources; two skills with the same name
// are an error.
func resolveSkills(r *skills.Resolver, sources []string) ([]skills.Skill, error) {
	var out []skills.Skill
	from := map[string]string{}
	for _, raw := range sources {
		src, err := skills.Parse(raw)
		if err != nil {
			return nil, err
		}
		got, err := r.Resolve(src)
		if err != nil {
			return nil, err
		}
		for _, sk := range got {
			if prev, dup := from[sk.Name]; dup {
				return nil, fmt.Errorf("skill %q comes from two sources: %s and %s", sk.Name, prev, raw)
			}
			from[sk.Name] = raw
			out = append(out, sk)
		}
	}
	return out, nil
}

// skillDirs groups targets by skills directory (Codex and opencode share
// .agents/skills in a project). want gives each target's skills; drop
// decides whether an unwanted state entry is removed.
func (c *scopeCtx) skillDirs(want func(agent.Target) map[string]*engine.SkillWant, drop func(state.SkillEntry) bool) []engine.SkillDir {
	var order []string
	byDir := map[string]*engine.SkillDir{}
	for _, t := range c.targets {
		dir := t.Paths.Skills
		if dir == "" {
			continue
		}
		sd, ok := byDir[dir]
		if !ok {
			sd = &engine.SkillDir{Dir: dir, Wants: map[string]*engine.SkillWant{}}
			byDir[dir] = sd
			order = append(order, dir)
		}
		sd.Targets = append(sd.Targets, t.ID())
		for name, w := range want(t) {
			sd.Wants[name] = w
		}
	}
	var out []engine.SkillDir
	for _, dir := range order {
		sd := byDir[dir]
		for _, name := range c.state.SkillNames(dir) {
			if _, ok := sd.Wants[name]; ok {
				continue
			}
			if e, _ := c.state.Skill(dir, name); drop != nil && drop(e) {
				sd.Wants[name] = nil
			}
		}
		if len(sd.Wants) > 0 {
			out = append(out, *sd)
		}
	}
	return out
}

const agentsMDTemplate = `# AGENTS.md

Instructions for the agents that work in this project (Codex, opencode, and
Claude Code via @AGENTS.md in CLAUDE.md).
`

// declarativeScopes chooses the scopes for plan/apply: the one from --scope,
// or those that have pegboard.yaml (or declared state to clean up). A
// project with inherit: none excludes the global scope.
func (o *options) declarativeScopes(env agent.Env) ([]*scopeCtx, error) {
	scopes, provs, err := o.selection()
	if err != nil {
		return nil, err
	}
	all, err := o.load(env, scopes, provs)
	if err != nil {
		return nil, err
	}
	if o.scope != "" {
		return all, nil
	}
	var out []*scopeCtx
	none := false
	for _, c := range all {
		if c.scope == agent.Project && c.stack != nil && c.stack.Inherit == "none" {
			none = true
		}
	}
	for _, c := range all {
		if c.scope == agent.Global && none {
			continue
		}
		if c.stack != nil || c.hasDeclared() {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no %s here or globally; run `pegboard init` or use `pegboard install`", stack.FileName)
	}
	return out, nil
}

// mcpProvider returns the target's MCP provider, if the target has an MCP
// file (Hermes in a project, for example, does not).
func mcpProvider(t agent.Target) (agent.MCPProvider, bool) {
	if t.Paths.MCPs == "" {
		return nil, false
	}
	return mcpProviderByID(t.Agent)
}

func mcpProviderByID(id string) (agent.MCPProvider, bool) {
	p, ok := providers.Get(id)
	if !ok {
		return nil, false
	}
	mp, ok := p.(agent.MCPProvider)
	return mp, ok
}

// declarative builds the engine.Scope for a scope from its stack. It also
// returns warnings (listed agents not detected, future phases) and what to
// do after applying (save the lock).
func (c *scopeCtx) declarative(pruneAdhoc bool) (engine.Scope, []string, func() error, error) {
	engScope, warnings, err := c.declarativeMCPs(pruneAdhoc)
	if err != nil {
		return engScope, warnings, nil, err
	}
	var listed []string
	var declared []string
	agentsMD := false
	if c.stack != nil {
		listed = c.stack.Agents
		declared = c.stack.Skills
		agentsMD = c.stack.Context.AgentsMD
	}
	selected := func(t agent.Target) bool { return mcp.Matches(listed, t.Agent, t.ID()) }
	lock := c.lock
	resolved, err := resolveSkills(c.skillResolver(lock), declared)
	if err != nil {
		return engScope, warnings, nil, err
	}
	wants := map[string]*engine.SkillWant{}
	for _, sk := range resolved {
		if !c.stack.Removed(sk.Source, sk.Name) {
			wants[sk.Name] = &engine.SkillWant{Skill: sk, Origin: state.Declared}
		}
	}
	engScope.Skills = c.skillDirs(func(t agent.Target) map[string]*engine.SkillWant {
		if !selected(t) {
			return nil
		}
		return wants
	}, func(e state.SkillEntry) bool { return e.Origin == state.Declared || pruneAdhoc })

	if c.scope == agent.Global && agentsMD {
		warnings = append(warnings, c.stack.Path+": context.agents_md only applies in a project; ignoring it")
	}
	if c.scope == agent.Project && (agentsMD || len(c.state.Context) > 0) {
		engScope.Context = append(engScope.Context, engine.ContextFile{
			Path: filepath.Join(c.env.ProjectDir, "AGENTS.md"), Target: "agents.md",
			Template: agentsMDTemplate, Want: agentsMD,
		})
		for _, t := range c.targets {
			if t.Agent == "claude-code" && t.Paths.Context != "" {
				engScope.Context = append(engScope.Context, engine.ContextFile{
					Path: t.Paths.Context, Target: t.ID(), Line: "@AGENTS.md", Want: agentsMD && selected(t),
				})
			}
		}
	}
	after := func() error {
		if lock == nil {
			return nil
		}
		keep := map[string]bool{}
		for _, s := range declared {
			keep[s] = true
		}
		lock.Prune(keep)
		return lock.Save()
	}
	return engScope, warnings, after, nil
}

// declarativeMCPs builds the MCP part of the scope.
func (c *scopeCtx) declarativeMCPs(pruneAdhoc bool) (engine.Scope, []string, error) {
	engScope := engine.Scope{Scope: c.scope, State: c.state, Resolver: c.resolver()}
	var warnings []string
	desired := map[string]mcp.MCP{}
	var listed []string
	if c.stack != nil {
		var err error
		desired, warnings, err = c.stack.Desired()
		if err != nil {
			return engScope, warnings, err
		}
		listed = c.stack.Agents
		for _, a := range listed {
			found := false
			for _, t := range c.targets {
				if t.Agent == a || t.ID() == a {
					found = true
				}
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("%s · %s: not detected; skipping", a, c.scope))
			}
		}
	}
	for _, t := range c.targets {
		p, ok := mcpProvider(t)
		if !ok {
			continue
		}
		selected := mcp.Matches(listed, t.Agent, t.ID())
		wants := map[string]*engine.Want{}
		if selected {
			for name, m := range desired {
				if m.AppliesTo(t.Agent, t.ID()) {
					wants[name] = &engine.Want{MCP: m, Origin: state.Declared}
				}
			}
		}
		for _, name := range c.state.Names(t.ID()) {
			if _, ok := wants[name]; ok {
				continue
			}
			e, _ := c.state.Get(t.ID(), name)
			if e.Origin == state.Declared || pruneAdhoc {
				wants[name] = nil
			}
		}
		if len(wants) > 0 {
			engScope.Targets = append(engScope.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
		}
	}
	return engScope, warnings, nil
}

func targetIDs(ts []agent.Target) string {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID()
	}
	slices.Sort(ids)
	return strings.Join(ids, ", ")
}

// providersDetectAll detects all agents in the scope, without --agent.
func providersDetectAll(env agent.Env, scope agent.Scope) []agent.Target {
	return providers.Detect(env, providers.All(), []agent.Scope{scope})
}
