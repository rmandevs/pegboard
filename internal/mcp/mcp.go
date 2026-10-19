// Package mcp es el modelo normalizado de un servidor MCP: lo que se
// declara en pegboard.yaml, antes de traducirlo al formato de cada agente.
package mcp

import (
	"fmt"
	"slices"
	"strings"
)

// MCP es un servidor stdio (Command) o http (URL). Los valores pueden llevar
// referencias a secretos (${env:X}, ${file:ruta}) sin resolver.
type MCP struct {
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Agents restringe el MCP a ciertos agentes; vacío = todos.
	Agents []string `yaml:"agents,omitempty" json:"agents,omitempty"`
}

// Remote informa si es un MCP http.
func (m MCP) Remote() bool { return m.URL != "" }

// Validate revisa lo que el schema no puede: que sea stdio o http, no ambos.
func (m MCP) Validate() error {
	switch {
	case m.Command == "" && m.URL == "":
		return fmt.Errorf("falta command o url")
	case m.Command != "" && m.URL != "":
		return fmt.Errorf("command y url son excluyentes")
	case m.Remote() && (len(m.Args) > 0 || len(m.Env) > 0):
		return fmt.Errorf("un MCP http no lleva args ni env")
	case !m.Remote() && len(m.Headers) > 0:
		return fmt.Errorf("un MCP stdio no lleva headers")
	}
	return nil
}

// AppliesTo informa si el MCP va en el target. "hermes" cubre todos los
// perfiles de Hermes; "hermes:work" solo ese.
func (m MCP) AppliesTo(agentID, targetID string) bool {
	return Matches(m.Agents, agentID, targetID)
}

// Matches aplica la misma regla a cualquier lista de agentes.
func Matches(list []string, agentID, targetID string) bool {
	if len(list) == 0 {
		return true
	}
	return slices.Contains(list, agentID) || slices.Contains(list, targetID)
}

// Equal compara dos MCPs sin contar la restricción de agentes.
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

// Summary es una línea legible: el comando o la URL.
func (m MCP) Summary() string {
	if m.Remote() {
		return m.URL
	}
	return strings.Join(append([]string{m.Command}, m.Args...), " ")
}
