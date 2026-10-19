package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/engine"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/secrets"
	"github.com/rmandevs/agents-casc/internal/stack"
	"github.com/rmandevs/agents-casc/internal/state"
	"github.com/rmandevs/agents-casc/providers"
)

// scopeCtx es todo lo cargado para un alcance.
type scopeCtx struct {
	scope   agent.Scope
	stack   *stack.Stack // nil si no hay pegboard.yaml
	state   *state.State
	targets []agent.Target // detectados y filtrados por --agent
	env     agent.Env
}

func statePath(env agent.Env, scope agent.Scope) string {
	if scope == agent.Global {
		return filepath.Join(env.StateDir(), "state.json")
	}
	return filepath.Join(env.ProjectDir, ".pegboard", "state.json")
}

// load carga stack, estado y targets de cada alcance.
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
		out = append(out, &scopeCtx{
			scope:   sc,
			stack:   st,
			state:   state,
			targets: providers.Detect(env, provs, []agent.Scope{sc}),
			env:     env,
		})
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
	return false
}

// declarativeScopes elige los alcances de plan/apply: el de --scope, o los
// que tienen pegboard.yaml (o estado declarado que limpiar). Un proyecto con
// inherit: none excluye el global.
func (o *options) declarativeScopes(env agent.Env) ([]*scopeCtx, []agent.Provider, error) {
	scopes, provs, err := o.selection()
	if err != nil {
		return nil, nil, err
	}
	all, err := o.load(env, scopes, provs)
	if err != nil {
		return nil, nil, err
	}
	if o.scope != "" {
		return all, provs, nil
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
		return nil, nil, fmt.Errorf("no hay %s aquí ni global; corre `pegboard init` o usa `pegboard install`", stack.FileName)
	}
	return out, provs, nil
}

func mcpProvider(id string) (agent.MCPProvider, bool) {
	p, ok := providers.Get(id)
	if !ok {
		return nil, false
	}
	mp, ok := p.(agent.MCPProvider)
	return mp, ok
}

// declarative arma el engine.Scope de un alcance a partir de su stack.
// Devuelve también los avisos (agentes listados no detectados, fases
// futuras).
func (c *scopeCtx) declarative(pruneAdhoc bool) (engine.Scope, []string, error) {
	es := engine.Scope{Scope: c.scope, State: c.state, Resolver: c.resolver()}
	var warnings []string
	desired := map[string]mcp.MCP{}
	var listed []string
	if c.stack != nil {
		var err error
		desired, warnings, err = c.stack.Desired()
		if err != nil {
			return es, warnings, err
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
				warnings = append(warnings, fmt.Sprintf("%s · %s: no detectado; se omite", a, c.scope))
			}
		}
	}
	for _, t := range c.targets {
		p, ok := mcpProvider(t.Agent)
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
			es.Targets = append(es.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
		}
	}
	return es, warnings, nil
}

func targetIDs(ts []agent.Target) string {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID()
	}
	slices.Sort(ids)
	return strings.Join(ids, ", ")
}

// providersDetectAll detecta todos los agentes del alcance, sin --agent.
func providersDetectAll(env agent.Env, scope agent.Scope) []agent.Target {
	return providers.Detect(env, providers.All(), []agent.Scope{scope})
}
