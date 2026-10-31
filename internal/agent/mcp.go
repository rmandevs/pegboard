package agent

import (
	"strconv"

	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
)

// MCPProvider is the part of Provider that handles MCPs: where they live and
// how they are translated. Reading, merging, and writing are shared by all
// providers (internal/engine).
type MCPProvider interface {
	Provider
	// MCPFile is the target's MCP file.
	MCPFile(t Target) native.File
	// EncodeMCP translates an MCP into the native format, resolving secrets
	// with r when the agent does not support references. The paths in
	// enc.Masked ended up with a secret in plain text.
	EncodeMCP(m mcp.MCP, enc *Encoder) (*native.Obj, error)
	// DecodeMCP reads a native entry (Plain shape) and converts native
	// references to ${env:…} / ${file:…}.
	DecodeMCP(v any) (mcp.MCP, error)
}

// Encoder expands strings with secrets and records where they ended up in
// plain text.
type Encoder struct {
	Resolver secrets.Resolver
	// Native translates references to the agent's syntax; nil = none.
	Native secrets.Native
	Masked []string
	err    error
}

// Err is the first resolution error.
func (e *Encoder) Err() error { return e.err }

// S expands s, which lives at path in the native entry.
func (e *Encoder) S(s string, path ...string) string {
	out, plain, err := e.Resolver.Expand(s, e.Native)
	if err != nil && e.err == nil {
		e.err = err
	}
	if plain {
		e.Masked = append(e.Masked, native.Pointer(path...))
	}
	return out
}

// L expands each element of a list.
func (e *Encoder) L(l []string, path ...string) []string {
	if len(l) == 0 {
		return nil
	}
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = e.S(s, append(path, itoa(i))...)
	}
	return out
}

// M expands the values of a map into an Obj with sorted keys.
func (e *Encoder) M(m map[string]string, path ...string) *native.Obj {
	o := native.StringMap(m)
	for _, k := range o.Keys {
		o.Vals[k] = e.S(m[k], append(path, k)...)
	}
	return o
}

func itoa(i int) string { return strconv.Itoa(i) }
