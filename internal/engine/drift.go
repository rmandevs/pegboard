package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/state"
)

// DriftKind is the result of comparing a state entry with its file.
type DriftKind string

const (
	InSync  DriftKind = "ok"
	Edited  DriftKind = "edited"
	Missing DriftKind = "missing"
)

// DriftItem is an MCP registered in state and how it looks in its file.
type DriftItem struct {
	Scope agent.Scope `json:"scope"`
	// Resource: mcp or skill.
	Resource string       `json:"kind"`
	Target   string       `json:"target"`
	Name     string       `json:"name"`
	File     string       `json:"file"`
	Origin   state.Origin `json:"origin"`
	Kind     DriftKind    `json:"status"`
}

// Lookup finds an agent's provider by ID.
type Lookup func(agentID string) (agent.MCPProvider, bool)

// Drift compares each state entry with the real file.
func Drift(sc agent.Scope, st *state.State, lookup Lookup) ([]DriftItem, error) {
	var out []DriftItem
	docs := map[string]map[string]any{}
	for _, tid := range st.TargetIDs() {
		agentID, profile, _ := strings.Cut(tid, ":")
		p, ok := lookup(agentID)
		if !ok {
			return nil, fmt.Errorf("state references an unknown agent: %s", tid)
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
			it := DriftItem{Scope: sc, Resource: "mcp", Target: tid, Name: name, File: e.File, Origin: e.Origin, Kind: InSync}
			if obs, ok := entries[name]; !ok {
				it.Kind = Missing
			} else if native.Hash(obs, e.Masked) != e.Hash {
				it.Kind = Edited
			}
			out = append(out, it)
		}
	}
	for _, dir := range st.SkillDirs() {
		for _, name := range st.SkillNames(dir) {
			e, _ := st.Skill(dir, name)
			dest := filepath.Join(dir, name)
			it := DriftItem{Scope: sc, Resource: "skill", Target: "skills", Name: name, File: dest, Origin: e.Origin, Kind: InSync}
			obs := observe(dest)
			switch {
			case obs.err != nil:
				return nil, obs.err
			case !obs.exists:
				it.Kind = Missing
			case !obs.matches(e.Link, e.Hash, e.Copy):
				it.Kind = Edited
			}
			out = append(out, it)
		}
	}
	return out, nil
}
