package engine

import (
	"fmt"
	"strings"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/state"
)

// DriftKind es el resultado de comparar una entrada del estado con su archivo.
type DriftKind string

const (
	InSync  DriftKind = "ok"
	Edited  DriftKind = "edited"
	Missing DriftKind = "missing"
)

// DriftItem es un MCP registrado en el estado y cómo está en su archivo.
type DriftItem struct {
	Scope  agent.Scope  `json:"scope"`
	Target string       `json:"target"`
	Name   string       `json:"name"`
	File   string       `json:"file"`
	Origin state.Origin `json:"origin"`
	Kind   DriftKind    `json:"status"`
}

// Lookup encuentra el provider de un agente por ID.
type Lookup func(agentID string) (agent.MCPProvider, bool)

// Drift compara cada entrada del estado con el archivo real.
func Drift(sc agent.Scope, st *state.State, lookup Lookup) ([]DriftItem, error) {
	var out []DriftItem
	docs := map[string]map[string]any{}
	for _, tid := range st.TargetIDs() {
		agentID, profile, _ := strings.Cut(tid, ":")
		p, ok := lookup(agentID)
		if !ok {
			return nil, fmt.Errorf("el estado menciona un agente desconocido: %s", tid)
		}
		for _, name := range st.Names(tid) {
			e, _ := st.Get(tid, name)
			entries, ok := docs[e.File]
			if !ok {
				t := agent.Target{Agent: agentID, Profile: profile, Scope: sc, Paths: agent.Paths{MCPs: e.File}}
				d, err := native.Open(p.MCPFile(t))
				if err != nil {
					return nil, err
				}
				if entries, err = d.Entries(); err != nil {
					return nil, fmt.Errorf("%s: %w", e.File, err)
				}
				docs[e.File] = entries
			}
			it := DriftItem{Scope: sc, Target: tid, Name: name, File: e.File, Origin: e.Origin, Kind: InSync}
			if obs, ok := entries[name]; !ok {
				it.Kind = Missing
			} else if native.Hash(obs, e.Masked) != e.Hash {
				it.Kind = Edited
			}
			out = append(out, it)
		}
	}
	return out, nil
}
