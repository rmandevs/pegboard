package agent

import (
	"strconv"

	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
)

// MCPProvider es la parte de Provider que maneja MCPs: dónde viven y cómo se
// traducen. Leer, mezclar y escribir es común a todos (internal/engine).
type MCPProvider interface {
	Provider
	// MCPFile es el archivo de MCPs del target.
	MCPFile(t Target) native.File
	// EncodeMCP traduce un MCP al formato nativo, resolviendo secretos con r
	// cuando el agente no admite referencias. Las rutas de enc.Masked
	// quedaron con un secreto en claro.
	EncodeMCP(m mcp.MCP, enc *Encoder) (*native.Obj, error)
	// DecodeMCP lee una entrada nativa (forma Plain) y lleva las referencias
	// nativas a ${env:…} / ${file:…}.
	DecodeMCP(v any) (mcp.MCP, error)
}

// Encoder expande strings con secretos y anota dónde quedaron en claro.
type Encoder struct {
	Resolver secrets.Resolver
	// Native traduce referencias a la sintaxis del agente; nil = ninguna.
	Native secrets.Native
	Masked []string
	err    error
}

// Err es el primer error de resolución.
func (e *Encoder) Err() error { return e.err }

// S expande s, que va en la ruta path de la entrada nativa.
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

// L expande cada elemento de una lista.
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

// M expande los valores de un mapa a un Obj con llaves ordenadas.
func (e *Encoder) M(m map[string]string, path ...string) *native.Obj {
	o := native.StringMap(m)
	for _, k := range o.Keys {
		o.Vals[k] = e.S(m[k], append(path, k)...)
	}
	return o
}

func itoa(i int) string { return strconv.Itoa(i) }
