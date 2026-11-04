// Package engine computes and applies plans (SPEC §4): the same engine
// serves declarative mode (pegboard.yaml) and ad-hoc mode (pegboard install).
package engine

import (
	"errors"
	"fmt"
	"sort"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/secrets"
	"github.com/rmandevs/pegboard/internal/state"
)

// Action is what the plan will do with a resource.
type Action string

const (
	Create   Action = "create"
	Update   Action = "update"
	Delete   Action = "delete"
	Conflict Action = "conflict"
	// Adopt: the entry is already as desired; it is only recorded in state.
	Adopt Action = "adopt"
	// Forget: it was already absent from the file; it is only removed from
	// state.
	Forget Action = "forget"
)

// Want is the desired state of an MCP in a target. nil = it must not exist.
type Want struct {
	MCP    mcp.MCP
	Origin state.Origin
	// Via is the included stack the MCP comes from ("" = the stack itself).
	Via string
	// Review, if set, explains why the MCP needs --approve: it comes from
	// a remote included stack and the lock has not recorded it as is.
	Review string
}

// Target is what the plan knows about a target: its provider, its scope, and
// what is wanted for each name the operation touches.
type Target struct {
	Target   agent.Target
	Provider agent.MCPProvider
	Wants    map[string]*Want
}

// Scope is a scope to plan: its targets, its state, and how to resolve
// secrets.
type Scope struct {
	Scope    agent.Scope
	State    *state.State
	Resolver secrets.Resolver
	Targets  []Target
	Skills   []SkillDir
	Context  []ContextFile
	// ContextCopies are context files replicated from another agent.
	ContextCopies []ContextCopy
}

// Change is one line of the plan.
type Change struct {
	Scope  agent.Scope
	Target string
	// Kind: mcp, skill, or context.
	Kind     string
	Name     string
	Action   Action
	Origin   state.Origin
	File     string
	Reason   string
	Warnings []string
	// Forced: a conflict that --force turns into this action.
	Forced Action
	// Unicode: blocked because of hidden characters; --force does not
	// override it, only --allow-hidden-unicode does.
	Unicode bool
	// Review: an MCP from an included stack, new or changed since the lock
	// recorded it; only --approve lets it through.
	Review bool
	// Via is the included stack a resource comes from; Command, what an
	// included MCP runs (third-party code).
	Via     string
	Command string

	// Detail completes the line when there is no Reason (for example,
	// where a skill points). Link is the local directory it points to, if
	// local.
	Detail string
	Link   string

	entry  state.Entry
	do     Action // what Apply will do; "" in an unresolved conflict
	silent bool   // only changes state (for example, origin); not shown
	backup bool   // run overwrites a file of the user's: back it up first
	// run writes to disk (skills, context); commit updates state.
	run    func() error
	commit func(*state.State)
}

// Blocked reports whether this is a conflict Apply will not resolve.
func (c Change) Blocked() bool { return c.Action == Conflict && c.do == "" }

// Visible reports whether the change is shown in the plan.
func (c Change) Visible() bool { return !c.silent }

// Plan is the result of Compute: the changes and the files already edited in
// memory, ready for Apply.
type Plan struct {
	Changes []Change
	docs    map[string]*openDoc
	scopes  []Scope
}

type openDoc struct {
	file native.File
	doc  native.Doc
}

// Options for Compute.
type Options struct {
	// Force turns conflicts into their underlying action.
	Force bool
	// AllowHiddenUnicode installs skills even if they carry critical hidden
	// characters (bidirectional overrides, Unicode tags).
	AllowHiddenUnicode bool
	// Approve accepts MCPs from included stacks that are new or changed
	// since the lock recorded them.
	Approve bool
}

// Compute builds the plan. It writes nothing.
func Compute(scopes []Scope, opts Options) (*Plan, error) {
	p := &Plan{docs: map[string]*openDoc{}, scopes: scopes}
	for _, sc := range scopes {
		for _, t := range sc.Targets {
			if err := p.target(sc, t, opts); err != nil {
				return nil, err
			}
		}
		for _, sd := range sc.Skills {
			if err := p.skillDir(sc, sd, opts); err != nil {
				return nil, err
			}
		}
		for _, cf := range sc.Context {
			if err := p.context(sc, cf); err != nil {
				return nil, err
			}
		}
		for _, cc := range sc.ContextCopies {
			if err := p.contextCopy(sc, cc); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}

func (p *Plan) open(f native.File) (*openDoc, error) {
	if d, ok := p.docs[f.Path]; ok {
		return d, nil
	}
	doc, err := native.Open(f)
	if err != nil {
		return nil, err
	}
	d := &openDoc{file: f, doc: doc}
	p.docs[f.Path] = d
	return d, nil
}

func (p *Plan) target(sc Scope, t Target, opts Options) error {
	f := t.Provider.MCPFile(t.Target)
	d, err := p.open(f)
	if err != nil {
		return err
	}
	observed, err := d.doc.Entries()
	if err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	tid := t.Target.ID()
	names := make([]string, 0, len(t.Wants))
	for n := range t.Wants {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		want := t.Wants[name]
		st, managed := sc.State.Get(tid, name)
		obs, present := observed[name]
		c := Change{Scope: sc.Scope, Target: tid, Kind: "mcp", Name: name, File: f.Path}
		var obj *native.Obj
		if want != nil {
			c.Origin = want.Origin
			c.Via = want.Via
			if want.Via != "" {
				c.Command = want.MCP.Summary()
			}
			enc := &agent.Encoder{Resolver: sc.Resolver}
			obj, err = t.Provider.EncodeMCP(want.MCP, enc)
			if err != nil {
				return fmt.Errorf("%s · %s: mcp %s: %w", tid, sc.Scope, name, err)
			}
			for _, m := range enc.Masked {
				c.Warnings = append(c.Warnings, "secret in plain text at "+m)
			}
			c.entry = state.Entry{File: f.Path, Hash: native.Hash(obj, enc.Masked), Masked: enc.Masked, Origin: want.Origin}
			same := present && (native.Equal(obs, obj) || decodesTo(t.Provider, obs, want.MCP))
			edited := managed && present && native.Hash(obs, st.Masked) != st.Hash
			switch {
			case managed && !present:
				c.Action, c.Reason = Create, "deleted manually; recreating"
			case same && (edited || !managed):
				c.Action = Adopt
				c.entry.Hash = native.Hash(obs, enc.Masked)
				c.Reason = "already in place; pegboard records it"
				if edited {
					c.Reason = "edited manually, but it already matches"
				}
			case same:
				if st.Origin == want.Origin && st.Hash == native.Hash(obs, enc.Masked) {
					continue
				}
				c.Action, c.silent = Adopt, true
				c.entry.Hash = native.Hash(obs, enc.Masked)
			case edited:
				c.Action, c.Forced, c.Reason = Conflict, Update, "edited manually since the last apply"
			case !managed && present:
				c.Action, c.Forced, c.Reason = Conflict, Update, "already exists and was not created by pegboard"
			case managed:
				c.Action = Update
			default:
				c.Action = Create
			}
		} else {
			if !managed {
				continue
			}
			c.Origin = st.Origin
			switch {
			case !present:
				c.Action, c.Reason = Forget, "no longer in the file"
			case native.Hash(obs, st.Masked) != st.Hash:
				c.Action, c.Forced, c.Reason = Conflict, Delete, "edited manually; not removing"
			default:
				c.Action = Delete
			}
		}
		c.do = c.Action
		if c.Action == Conflict {
			c.do = ""
			if opts.Force {
				c.do = c.Forced
			}
		}
		if want != nil && want.Review != "" && (c.Action == Create || c.Action == Update) {
			if opts.Approve {
				c.Warnings = append(c.Warnings, want.Review+" (approved with --approve)")
			} else {
				c.Forced, c.Action, c.do, c.Review = c.Action, Conflict, "", true
				c.Reason = want.Review + "; review the command, then run with --approve"
			}
		}
		var editErr error
		switch c.do {
		case Create, Update:
			editErr = d.doc.Set(name, obj)
		case Delete:
			editErr = d.doc.Delete(name)
		}
		if editErr != nil {
			if !errors.Is(editErr, native.ErrUnsupported) {
				return fmt.Errorf("%s: %w", f.Path, editErr)
			}
			c.Action, c.Forced, c.do, c.Reason = Conflict, "", "", editErr.Error()
		}
		p.Changes = append(p.Changes, c)
	}
	return nil
}

// decodesTo reports whether the observed entry means the same as the desired
// MCP, even if written differently (for example, an extra "type": "stdio").
func decodesTo(p agent.MCPProvider, obs any, want mcp.MCP) bool {
	got, err := p.DecodeMCP(obs)
	return err == nil && got.Equal(want)
}

// Summary counts visible changes by action.
func (p *Plan) Summary() map[Action]int {
	out := map[Action]int{}
	for _, c := range p.Changes {
		if c.Visible() {
			out[c.Action]++
		}
	}
	return out
}

// Blocked reports whether there are unresolved conflicts: they stop apply.
func (p *Plan) Blocked() bool {
	for _, c := range p.Changes {
		if c.Blocked() {
			return true
		}
	}
	return false
}

// Empty reports whether there is nothing to show.
func (p *Plan) Empty() bool {
	for _, c := range p.Changes {
		if c.Visible() {
			return false
		}
	}
	return true
}

// Pending reports whether Apply has anything to do (including state-only
// changes).
func (p *Plan) Pending() bool { return len(p.Changes) > 0 }
