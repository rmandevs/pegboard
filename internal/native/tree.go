// Package native trabaja con la config nativa de los agentes: árboles de
// valores (lo que hay bajo mcpServers, mcp_servers…) y los archivos JSON,
// TOML y YAML que los contienen, editados sin tocar lo ajeno.
package native

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Obj es un objeto con orden de llaves, para escribir las entradas como las
// escribiría una persona (command antes que args).
type Obj struct {
	Keys []string
	Vals map[string]any
}

func NewObj() *Obj { return &Obj{Vals: map[string]any{}} }

// Set agrega o reemplaza una llave, conservando su posición si ya estaba.
func (o *Obj) Set(k string, v any) *Obj {
	if _, ok := o.Vals[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.Vals[k] = v
	return o
}

// SetIf hace Set solo si v no está vacío ("" , lista o mapa vacíos).
func (o *Obj) SetIf(k string, v any) *Obj {
	switch x := v.(type) {
	case string:
		if x == "" {
			return o
		}
	case []string:
		if len(x) == 0 {
			return o
		}
	case map[string]string:
		if len(x) == 0 {
			return o
		}
	case *Obj:
		if x == nil || len(x.Keys) == 0 {
			return o
		}
	}
	return o.Set(k, v)
}

// StringMap convierte un mapa en Obj con llaves ordenadas.
func StringMap(m map[string]string) *Obj {
	o := NewObj()
	for _, k := range sortedKeys(m) {
		o.Set(k, m[k])
	}
	return o
}

func (o *Obj) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := marshal(o.Vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshal es json.Marshal sin escapar <, > y & (los agentes no lo hacen).
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Plain lleva cualquier árbol (Obj, mapas de TOML o YAML, enteros…) a la
// forma de encoding/json, para compararlos sin importar de dónde vienen.
func Plain(v any) any {
	b, err := marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

// Canonical es la serialización estable de un árbol (llaves ordenadas).
func Canonical(v any) string {
	b, _ := marshal(Plain(v))
	return string(b)
}

// Equal compara dos árboles por su forma canónica.
func Equal(a, b any) bool { return Canonical(a) == Canonical(b) }

// Masked es el texto que reemplaza a un secreto antes de calcular un hash.
const Masked = "<secreto>"

// Hash es el sha256 de la forma canónica, con los valores en masked (rutas
// tipo JSON pointer: /env/TOKEN) reemplazados: así el estado nunca depende
// de un secreto en claro.
func Hash(v any, masked []string) string {
	p := Plain(v)
	for _, path := range masked {
		p = mask(p, splitPointer(path))
	}
	sum := sha256.Sum256([]byte(Canonical(p)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mask(v any, path []string) any {
	if len(path) == 0 {
		return Masked
	}
	switch x := v.(type) {
	case map[string]any:
		if c, ok := x[path[0]]; ok {
			x[path[0]] = mask(c, path[1:])
		}
	case []any:
		if i, err := strconv.Atoi(path[0]); err == nil && i >= 0 && i < len(x) {
			x[i] = mask(x[i], path[1:])
		}
	}
	return v
}

// Pointer arma una ruta tipo JSON pointer (RFC 6901).
func Pointer(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteByte('/')
		b.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(p))
	}
	return b.String()
}

func splitPointer(p string) []string {
	if p == "" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(s)
	}
	return parts
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Helpers para leer árboles Plain al decodificar entradas nativas.

// Str devuelve m[k] si es string.
func Str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// Strs devuelve m[k] si es una lista de strings.
func Strs(m map[string]any, k string) []string {
	l, _ := m[k].([]any)
	var out []string
	for _, v := range l {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// StrMap devuelve m[k] si es un objeto de strings.
func StrMap(m map[string]any, k string) map[string]string {
	o, _ := m[k].(map[string]any)
	if len(o) == 0 {
		return nil
	}
	out := map[string]string{}
	for kk, v := range o {
		if s, ok := v.(string); ok {
			out[kk] = s
		}
	}
	return out
}
