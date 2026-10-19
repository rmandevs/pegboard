package providers

import (
	"path/filepath"
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
	"github.com/rmandevs/agents-casc/internal/state"
)

var cases = map[string]mcp.MCP{
	"stdio": {
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-github"},
		Env:     map[string]string{"GITHUB_TOKEN": "${env:GITHUB_TOKEN}", "LOG": "debug"},
	},
	"http": {
		URL:     "https://mcp.linear.app/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${env:LINEAR}", "X-Org": "${env:ORG}", "X-Plain": "a"},
	},
	"bare": {Command: "uvx"},
}

var values = map[string]string{"GITHUB_TOKEN": "ghp", "LINEAR": "lin", "ORG": "org"}

// resolved es el MCP con los secretos resueltos: lo que se lee de vuelta en
// un agente que no admite referencias.
func resolved(m mcp.MCP) mcp.MCP {
	r := secrets.Resolver{LookupEnv: func(k string) (string, bool) { v, ok := values[k]; return v, ok }}
	exp := func(s string) string { v, _, _ := r.Expand(s, nil); return v }
	out := mcp.MCP{Command: m.Command, URL: m.URL}
	for _, a := range m.Args {
		out.Args = append(out.Args, exp(a))
	}
	if m.Env != nil {
		out.Env = map[string]string{}
		for k, v := range m.Env {
			out.Env[k] = exp(v)
		}
	}
	if m.Headers != nil {
		out.Headers = map[string]string{}
		for k, v := range m.Headers {
			out.Headers[k] = exp(v)
		}
	}
	return out
}

// Ida y vuelta: codificar → escribir → leer → decodificar da el mismo MCP,
// y lo leído coincide con el hash que guardaría el estado.
func TestRoundTrip(t *testing.T) {
	// Qué agentes conservan las referencias (y cuáles quedan en claro).
	keepsRefs := map[string]map[string]bool{
		"claude-code": {"stdio": true, "http": true},
		"opencode":    {"stdio": true, "http": true},
		// Codex reenvía GITHUB_TOKEN (misma llave) y los headers de env.
		"codex":  {"stdio": true, "http": true},
		"hermes": {},
	}
	for _, p := range All() {
		mp := p.(agent.MCPProvider)
		for name, m := range cases {
			t.Run(p.ID()+"/"+name, func(t *testing.T) {
				target := agent.Target{Agent: p.ID(), Paths: agent.Paths{MCPs: filepath.Join(t.TempDir(), "cfg")}}
				enc := &agent.Encoder{Resolver: secrets.Resolver{
					LookupEnv: func(k string) (string, bool) { v, ok := values[k]; return v, ok },
				}}
				obj, err := mp.EncodeMCP(m, enc)
				if err != nil {
					t.Fatal(err)
				}
				f := mp.MCPFile(target)
				d, err := native.Open(f)
				if err != nil {
					t.Fatal(err)
				}
				if err := d.Set(name, obj); err != nil {
					t.Fatal(err)
				}
				b, err := d.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				if err := state.WriteAtomic(f.Path, b, 0o600); err != nil {
					t.Fatal(err)
				}
				d2, err := native.Open(f)
				if err != nil {
					t.Fatal(err)
				}
				ents, err := d2.Entries()
				if err != nil {
					t.Fatal(err)
				}
				if native.Hash(ents[name], enc.Masked) != native.Hash(obj, enc.Masked) {
					t.Fatalf("el hash de lo leído no coincide con lo escrito:\n%s\n%s", native.Canonical(ents[name]), native.Canonical(obj))
				}
				got, err := mp.DecodeMCP(ents[name])
				if err != nil {
					t.Fatal(err)
				}
				want := m
				if !keepsRefs[p.ID()][name] {
					want = resolved(m)
				}
				if !got.Equal(want) {
					t.Fatalf("ida y vuelta:\n got %+v\nwant %+v\n%s", got, want, b)
				}
				if keepsRefs[p.ID()][name] && len(enc.Masked) > 0 && p.ID() != "codex" {
					t.Errorf("no debía quedar nada en claro: %v", enc.Masked)
				}
			})
		}
	}
}
