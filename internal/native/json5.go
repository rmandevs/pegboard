package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/titanous/json5"
)

// json5Doc edits a JSON5 file. It parses and rewrites only the top-level
// object that contains the configured container path, leaving the rest of
// the file byte for byte. This is enough for OpenClaw's openclaw.json, whose
// MCP servers live under mcp.servers and which itself rewrites config as
// strict JSON (stripping JSON5 comments).
type json5Doc struct {
	src     []byte
	exists  bool
	file    File
	path    []string
	root    j5Object
	block   j5Object
	found   bool
	blockOn map[string]any
	changed bool
}

func openJSON5(src []byte, exists bool, f File) (Doc, error) {
	path := f.containerPath()
	if len(path) == 0 {
		return nil, errors.New("json5: empty container path")
	}
	d := &json5Doc{src: src, exists: exists, file: f, path: path, blockOn: map[string]any{}}
	if !exists || len(bytes.TrimSpace(src)) == 0 {
		return d, nil
	}
	var whole any
	if err := json5.Unmarshal(src, &whole); err != nil {
		return nil, err
	}
	start := skipJ5(src, 0)
	root, err := scanJ5Object(src, start)
	if err != nil {
		return nil, err
	}
	d.root = root
	if i := findJ5Member(root, path[0]); i >= 0 {
		m := root.members[i]
		var block any
		if err := json5.Unmarshal(src[m.valueStart:m.valueEnd], &block); err != nil {
			return nil, err
		}
		bm, ok := block.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q is not an object: %w", path[0], ErrUnsupported)
		}
		d.block = j5Object{start: m.valueStart, end: m.valueEnd}
		d.found = true
		d.blockOn = bm
		if _, err := d.container(false); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (f File) containerPath() []string {
	if len(f.ContainerPath) > 0 {
		return f.ContainerPath
	}
	if f.Container != "" {
		return []string{f.Container}
	}
	return nil
}

func (d *json5Doc) Exists() bool { return d.exists }

// container returns the map that directly holds the entries.
func (d *json5Doc) container(create bool) (map[string]any, error) {
	if len(d.path) == 1 {
		return d.blockOn, nil
	}
	parent := d.blockOn
	for _, k := range d.path[1 : len(d.path)-1] {
		child, ok := parent[k]
		if !ok {
			if !create {
				return nil, nil
			}
			child = map[string]any{}
			parent[k] = child
		}
		m, ok := child.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q is not an object: %w", k, ErrUnsupported)
		}
		parent = m
	}
	key := d.path[len(d.path)-1]
	child, ok := parent[key]
	if !ok {
		if !create {
			return nil, nil
		}
		m := map[string]any{}
		parent[key] = m
		return m, nil
	}
	m, ok := child.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q is not an object: %w", key, ErrUnsupported)
	}
	return m, nil
}

func (d *json5Doc) Entries() (map[string]any, error) {
	m, err := d.container(false)
	if err != nil || m == nil {
		return map[string]any{}, err
	}
	out := map[string]any{}
	for k, v := range m {
		out[k] = Plain(v)
	}
	return out, nil
}

func (d *json5Doc) Set(name string, v *Obj) error {
	m, err := d.container(true)
	if err != nil {
		return err
	}
	m[name] = Plain(v)
	d.changed = true
	return nil
}

func (d *json5Doc) Delete(name string) error {
	m, err := d.container(false)
	if err != nil || m == nil {
		return err
	}
	if _, ok := m[name]; ok {
		delete(m, name)
		d.changed = true
	}
	return nil
}

func (d *json5Doc) Bytes() ([]byte, error) {
	if !d.changed {
		return d.src, nil
	}
	out, err := json.MarshalIndent(d.blockOn, "  ", "  ")
	if err != nil {
		return nil, err
	}
	if !d.found {
		return d.insertBlock(out)
	}
	res := make([]byte, 0, len(d.src)-(d.block.end-d.block.start)+len(out))
	res = append(res, d.src[:d.block.start]...)
	res = append(res, out...)
	res = append(res, d.src[d.block.end:]...)
	return res, nil
}

func (d *json5Doc) insertBlock(block []byte) ([]byte, error) {
	key, err := marshal(d.path[0])
	if err != nil {
		return nil, err
	}
	if !d.exists || len(bytes.TrimSpace(d.src)) == 0 {
		var b bytes.Buffer
		b.WriteString("{\n  ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(block)
		b.WriteString("\n}\n")
		return b.Bytes(), nil
	}
	close := d.root.end - 1
	sep := ""
	if len(d.root.members) > 0 && !d.root.trailingComma {
		sep = ","
	}
	member := append([]byte(sep+"\n  "), key...)
	member = append(member, ": "...)
	member = append(member, block...)
	member = append(member, '\n')
	res := make([]byte, 0, len(d.src)+len(member))
	res = append(res, d.src[:close]...)
	res = append(res, member...)
	res = append(res, d.src[close:]...)
	return res, nil
}

// j5Object is a JSON5 object span with its top-level members.
type j5Object struct {
	start         int
	end           int
	members       []j5Member
	trailingComma bool
}

type j5Member struct {
	key                  string
	valueStart, valueEnd int
}

func findJ5Member(o j5Object, key string) int {
	for i, m := range o.members {
		if m.key == key {
			return i
		}
	}
	return -1
}

func scanJ5Object(src []byte, i int) (j5Object, error) {
	obj := j5Object{start: i}
	if i >= len(src) || src[i] != '{' {
		return obj, errors.New("expected a JSON5 object")
	}
	i++
	for {
		i = skipJ5(src, i)
		if i >= len(src) {
			return obj, errors.New("unterminated JSON5 object")
		}
		if src[i] == '}' {
			obj.end = i + 1
			return obj, nil
		}
		key, next, err := scanJ5Key(src, i)
		if err != nil {
			return obj, err
		}
		i = skipJ5(src, next)
		if i >= len(src) || src[i] != ':' {
			return obj, fmt.Errorf("expected ':' after %q", key)
		}
		i = skipJ5(src, i+1)
		valueStart := i
		valueEnd, err := scanJ5Value(src, i)
		if err != nil {
			return obj, err
		}
		obj.members = append(obj.members, j5Member{key: key, valueStart: valueStart, valueEnd: valueEnd})
		obj.trailingComma = false
		i = skipJ5(src, valueEnd)
		if i >= len(src) {
			return obj, errors.New("unterminated JSON5 object")
		}
		if src[i] == ',' {
			obj.trailingComma = true
			i++
			continue
		}
		if src[i] == '}' {
			obj.end = i + 1
			return obj, nil
		}
		return obj, errors.New("expected ',' or '}' in JSON5 object")
	}
}

func scanJ5Array(src []byte, i int) (int, error) {
	i++ // [
	for {
		i = skipJ5(src, i)
		if i >= len(src) {
			return 0, errors.New("unterminated JSON5 array")
		}
		if src[i] == ']' {
			return i + 1, nil
		}
		end, err := scanJ5Value(src, i)
		if err != nil {
			return 0, err
		}
		i = skipJ5(src, end)
		if i >= len(src) {
			return 0, errors.New("unterminated JSON5 array")
		}
		if src[i] == ',' {
			i++
			continue
		}
		if src[i] == ']' {
			return i + 1, nil
		}
		return 0, errors.New("expected ',' or ']' in JSON5 array")
	}
}

func scanJ5Value(src []byte, i int) (int, error) {
	if i >= len(src) {
		return 0, errors.New("missing JSON5 value")
	}
	switch src[i] {
	case '{':
		o, err := scanJ5Object(src, i)
		return o.end, err
	case '[':
		return scanJ5Array(src, i)
	case '"', '\'':
		return scanJ5String(src, i)
	}
	start := i
	for i < len(src) {
		c := src[i]
		if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			break
		}
		if c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
			break
		}
		i++
	}
	if i == start {
		return 0, errors.New("empty JSON5 value")
	}
	return i, nil
}

func scanJ5Key(src []byte, i int) (string, int, error) {
	if src[i] == '"' || src[i] == '\'' {
		end, err := scanJ5String(src, i)
		if err != nil {
			return "", i, err
		}
		var key string
		if err := json5.Unmarshal(src[i:end], &key); err != nil {
			return "", i, err
		}
		return key, end, nil
	}
	start := i
	for i < len(src) {
		c := src[i]
		if c == ':' || c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '/' {
			break
		}
		i++
	}
	key := strings.TrimSpace(string(src[start:i]))
	if key == "" {
		return "", i, errors.New("empty JSON5 key")
	}
	return key, i, nil
}

func scanJ5String(src []byte, i int) (int, error) {
	quote := src[i]
	i++
	for i < len(src) {
		switch src[i] {
		case '\\':
			i += 2
		case quote:
			return i + 1, nil
		case '\n', '\r':
			return 0, errors.New("newline in JSON5 string")
		default:
			i++
		}
	}
	return 0, errors.New("unterminated JSON5 string")
}

func skipJ5(src []byte, i int) int {
	if i == 0 && len(src) >= 3 && bytes.Equal(src[:3], []byte{0xEF, 0xBB, 0xBF}) {
		i = 3
	}
	for i < len(src) {
		switch {
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r':
			i++
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
		default:
			return i
		}
	}
	return i
}
