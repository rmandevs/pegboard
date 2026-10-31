// Package native works with the agents' native config: value trees (what is
// under mcpServers, mcp_servers…) and the JSON, TOML, and YAML files that
// contain them, edited without touching anything else.
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

// Obj is an object with key order, so entries are written the way a person
// would write them (command before args).
type Obj struct {
	Keys []string
	Vals map[string]any
}

func NewObj() *Obj { return &Obj{Vals: map[string]any{}} }

// Set adds or replaces a key, keeping its position if it was already there.
func (o *Obj) Set(k string, v any) *Obj {
	if _, ok := o.Vals[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.Vals[k] = v
	return o
}

// SetIf calls Set only if v is not empty ("", empty list, or empty map).
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

// StringMap converts a map into an Obj with sorted keys.
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

// marshal is json.Marshal without escaping <, >, and & (the agents do not
// escape them either).
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Plain converts any tree (Obj, TOML or YAML maps, integers…) to the
// encoding/json shape, so they can be compared regardless of origin.
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

// Canonical is the stable serialization of a tree (sorted keys).
func Canonical(v any) string {
	b, _ := marshal(Plain(v))
	return string(b)
}

// Equal compares two trees by their canonical form.
func Equal(a, b any) bool { return Canonical(a) == Canonical(b) }

// Masked is the text that replaces a secret before computing a hash.
const Masked = "<secret>"

// Hash is the sha256 of the canonical form, with the values at masked paths
// (JSON pointer style: /env/TOKEN) replaced: this way the state never
// depends on a secret in plain text.
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

// Pointer builds a JSON pointer path (RFC 6901).
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

// Helpers for reading Plain trees when decoding native entries.

// Str returns m[k] if it is a string.
func Str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// Strs returns m[k] if it is a list of strings.
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

// StrMap returns m[k] if it is an object of strings.
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
