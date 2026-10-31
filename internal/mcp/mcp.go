// Package mcp is the normalized model of an MCP server: what is declared in
// pegboard.yaml, before it is translated into each agent's format.
package mcp

import (
	"fmt"
	"slices"
	"strings"
)

// MCP is a stdio server (Command) or an http server (URL). Values may carry
// unresolved secret references (${env:X}, ${file:path}).
type MCP struct {
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Agents restricts the MCP to certain agents; empty = all.
	Agents []string `yaml:"agents,omitempty" json:"agents,omitempty"`
}

// Remote reports whether this is an http MCP.
func (m MCP) Remote() bool { return m.URL != "" }

// Validate checks what the schema cannot: that it is either stdio or http,
// not both.
func (m MCP) Validate() error {
	switch {
	case m.Command == "" && m.URL == "":
		return fmt.Errorf("missing command or url")
	case m.Command != "" && m.URL != "":
		return fmt.Errorf("command and url are mutually exclusive")
	case m.Remote() && (len(m.Args) > 0 || len(m.Env) > 0):
		return fmt.Errorf("an http MCP cannot have args or env")
	case !m.Remote() && len(m.Headers) > 0:
		return fmt.Errorf("a stdio MCP cannot have headers")
	}
	return nil
}

// AppliesTo reports whether the MCP belongs in the target. "hermes" covers
// all Hermes profiles; "hermes:work" only that one.
func (m MCP) AppliesTo(agentID, targetID string) bool {
	return Matches(m.Agents, agentID, targetID)
}

// Matches applies the same rule to any list of agents.
func Matches(list []string, agentID, targetID string) bool {
	if len(list) == 0 {
		return true
	}
	return slices.Contains(list, agentID) || slices.Contains(list, targetID)
}

// Equal compares two MCPs without counting the agent restriction.
func (m MCP) Equal(o MCP) bool {
	return m.Command == o.Command && m.URL == o.URL &&
		slices.Equal(m.Args, o.Args) && mapsEqual(m.Env, o.Env) && mapsEqual(m.Headers, o.Headers)
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// Summary is a readable one-liner: the command or the URL.
func (m MCP) Summary() string {
	if m.Remote() {
		return m.URL
	}
	return strings.Join(append([]string{m.Command}, m.Args...), " ")
}
