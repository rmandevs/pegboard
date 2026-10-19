// Package engine calcula y aplica planes (SPEC §4): el mismo motor sirve al
// modo declarativo (pegboard.yaml) y al ad-hoc (pegboard install).
package engine

import (
	"errors"
	"fmt"
	"sort"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
	"github.com/rmandevs/agents-casc/internal/state"
)

// Action es lo que el plan hará con un recurso.
type Action string

const (
	Create   Action = "create"
	Update   Action = "update"
	Delete   Action = "delete"
	Conflict Action = "conflict"
	// Adopt: la entrada ya está como se quiere; solo se registra en el estado.
	Adopt Action = "adopt"
	// Forget: ya no estaba en el archivo; solo se quita del estado.
	Forget Action = "forget"
)

// Want es el estado deseado de un MCP en un target. nil = debe no estar.
type Want struct {
	MCP    mcp.MCP
	Origin state.Origin
}

// Target es lo que el plan sabe de un target: su provider, su alcance, y
// qué se quiere de cada nombre que la operación toca.
type Target struct {
	Target   agent.Target
	Provider agent.MCPProvider
	Wants    map[string]*Want
}

// Scope es un alcance a planear: sus targets, su estado y cómo resolver
// secretos.
type Scope struct {
	Scope    agent.Scope
	State    *state.State
	Resolver secrets.Resolver
	Targets  []Target
}

// Change es una línea del plan.
type Change struct {
	Scope    agent.Scope
	Target   string
	Name     string
	Action   Action
	Origin   state.Origin
	File     string
	Reason   string
	Warnings []string
	// Forced: un conflicto que --force convierte en esta acción.
	Forced Action

	entry  state.Entry
	do     Action // lo que hará Apply; "" en un conflicto sin resolver
	silent bool   // solo cambia el estado (p. ej. el origen); no se muestra
}

// Blocked informa si es un conflicto que Apply no va a resolver.
func (c Change) Blocked() bool { return c.Action == Conflict && c.do == "" }

// Visible informa si el cambio se muestra en el plan.
func (c Change) Visible() bool { return !c.silent }

// Plan es el resultado de Compute: los cambios y los archivos ya editados en
// memoria, listos para Apply.
type Plan struct {
	Changes []Change
	docs    map[string]*openDoc
	scopes  []Scope
}

type openDoc struct {
	file native.File
	doc  native.Doc
}

// Options de Compute.
type Options struct {
	// Force convierte los conflictos en su acción subyacente.
	Force bool
}

// Compute arma el plan. No escribe nada.
func Compute(scopes []Scope, opts Options) (*Plan, error) {
	p := &Plan{docs: map[string]*openDoc{}, scopes: scopes}
	for _, sc := range scopes {
		for _, t := range sc.Targets {
			if err := p.target(sc, t, opts); err != nil {
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
		c := Change{Scope: sc.Scope, Target: tid, Name: name, File: f.Path}
		var obj *native.Obj
		if want != nil {
			c.Origin = want.Origin
			enc := &agent.Encoder{Resolver: sc.Resolver}
			obj, err = t.Provider.EncodeMCP(want.MCP, enc)
			if err != nil {
				return fmt.Errorf("%s · %s: mcp %s: %w", tid, sc.Scope, name, err)
			}
			for _, m := range enc.Masked {
				c.Warnings = append(c.Warnings, "secreto en texto plano en "+m)
			}
			c.entry = state.Entry{File: f.Path, Hash: native.Hash(obj, enc.Masked), Masked: enc.Masked, Origin: want.Origin}
			same := present && (native.Equal(obs, obj) || decodesTo(t.Provider, obs, want.MCP))
			edited := managed && present && native.Hash(obs, st.Masked) != st.Hash
			switch {
			case managed && !present:
				c.Action, c.Reason = Create, "borrado a mano; se vuelve a crear"
			case same && (edited || !managed):
				c.Action = Adopt
				c.entry.Hash = native.Hash(obs, enc.Masked)
				c.Reason = "ya estaba así; pegboard lo registra"
				if edited {
					c.Reason = "editado a mano, pero ya coincide"
				}
			case same:
				if st.Origin == want.Origin && st.Hash == native.Hash(obs, enc.Masked) {
					continue
				}
				c.Action, c.silent = Adopt, true
				c.entry.Hash = native.Hash(obs, enc.Masked)
			case edited:
				c.Action, c.Forced, c.Reason = Conflict, Update, "editado a mano desde el último apply"
			case !managed && present:
				c.Action, c.Forced, c.Reason = Conflict, Update, "ya existe y no lo creó pegboard"
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
				c.Action, c.Reason = Forget, "ya no estaba en el archivo"
			case native.Hash(obs, st.Masked) != st.Hash:
				c.Action, c.Forced, c.Reason = Conflict, Delete, "editado a mano; no se quita"
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

// decodesTo informa si la entrada observada significa lo mismo que el MCP
// deseado, aunque esté escrita distinto (p. ej. "type": "stdio" de más).
func decodesTo(p agent.MCPProvider, obs any, want mcp.MCP) bool {
	got, err := p.DecodeMCP(obs)
	return err == nil && got.Equal(want)
}

// Summary cuenta los cambios visibles por acción.
func (p *Plan) Summary() map[Action]int {
	out := map[Action]int{}
	for _, c := range p.Changes {
		if c.Visible() {
			out[c.Action]++
		}
	}
	return out
}

// Blocked informa si hay conflictos sin resolver: detienen el apply.
func (p *Plan) Blocked() bool {
	for _, c := range p.Changes {
		if c.Blocked() {
			return true
		}
	}
	return false
}

// Empty informa si no hay nada que mostrar.
func (p *Plan) Empty() bool {
	for _, c := range p.Changes {
		if c.Visible() {
			return false
		}
	}
	return true
}

// Pending informa si Apply tiene algo que hacer (incluido solo el estado).
func (p *Plan) Pending() bool { return len(p.Changes) > 0 }
