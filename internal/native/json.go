package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// jsonDoc edita un archivo JSON reemplazando solo los bytes del valor del
// contenedor. ~/.claude.json, por ejemplo, es enorme y lo reescribe Claude
// Code: el resto se conserva byte a byte.
type jsonDoc struct {
	src      []byte
	exists   bool
	file     File
	indent   string
	found    bool // el contenedor ya existía
	valStart int  // span del valor del contenedor en src
	valEnd   int
	entries  []jsonEntry
	changed  bool
}

type jsonEntry struct {
	key string
	raw []byte // bytes originales, o nuevos ya indentados
}

var indentRe = regexp.MustCompile(`\n([ \t]+)"`)

func openJSON(src []byte, exists bool, f File) (Doc, error) {
	d := &jsonDoc{src: src, exists: exists, file: f, indent: "  "}
	if m := indentRe.FindSubmatch(src); m != nil {
		d.indent = string(m[1])
	}
	if !exists || len(bytes.TrimSpace(src)) == 0 {
		return d, nil
	}
	err := eachMember(src, func(key string, start, end int) error {
		if key != f.Container {
			return nil
		}
		d.found, d.valStart, d.valEnd = true, start, end
		raw := src[start:end]
		if bytes.Equal(raw, []byte("null")) {
			return nil
		}
		if raw[0] != '{' {
			return fmt.Errorf("%q no es un objeto: %w", key, ErrUnsupported)
		}
		return eachMember(raw, func(k string, s, e int) error {
			d.entries = append(d.entries, jsonEntry{key: k, raw: raw[s:e]})
			return nil
		})
	})
	return d, err
}

// eachMember recorre las llaves de primer nivel de un objeto JSON y da el
// span [start, end) de cada valor dentro de src.
func eachMember(src []byte, fn func(key string, start, end int) error) error {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("el archivo no es un objeto JSON")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		end := int(dec.InputOffset())
		if err := fn(key, end-len(raw), end); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("contenido extra después del objeto JSON")
	}
	return nil
}

func (d *jsonDoc) Exists() bool { return d.exists }

func (d *jsonDoc) Entries() (map[string]any, error) {
	out := map[string]any{}
	for _, e := range d.entries {
		var v any
		if err := json.Unmarshal(e.raw, &v); err != nil {
			return nil, err
		}
		out[e.key] = v
	}
	return out, nil
}

func (d *jsonDoc) Set(name string, v *Obj) error {
	raw, err := d.render(v, 2)
	if err != nil {
		return err
	}
	d.changed = true
	for i := range d.entries {
		if d.entries[i].key == name {
			d.entries[i].raw = raw
			return nil
		}
	}
	d.entries = append(d.entries, jsonEntry{key: name, raw: raw})
	return nil
}

func (d *jsonDoc) Delete(name string) error {
	for i := range d.entries {
		if d.entries[i].key == name {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			d.changed = true
			return nil
		}
	}
	return nil
}

// render serializa v indentado para quedar a la profundidad depth.
func (d *jsonDoc) render(v any, depth int) ([]byte, error) {
	b, err := marshal(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	prefix := ""
	for i := 0; i < depth; i++ {
		prefix += d.indent
	}
	if err := json.Indent(&buf, b, prefix, d.indent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (d *jsonDoc) container() []byte {
	if len(d.entries) == 0 {
		return []byte("{}")
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, e := range d.entries {
		k, _ := marshal(e.key)
		b.WriteString(d.indent + d.indent)
		b.Write(k)
		b.WriteString(": ")
		b.Write(e.raw)
		if i < len(d.entries)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(d.indent + "}")
	return b.Bytes()
}

func (d *jsonDoc) Bytes() ([]byte, error) {
	if !d.changed {
		return d.src, nil
	}
	key, _ := marshal(d.file.Container)
	member := append(append(append([]byte(d.indent), key...), ": "...), d.container()...)
	switch {
	case d.found:
		out := append([]byte{}, d.src[:d.valStart]...)
		out = append(out, d.container()...)
		return append(out, d.src[d.valEnd:]...), nil
	case !d.exists || len(bytes.TrimSpace(d.src)) == 0:
		var b bytes.Buffer
		b.WriteString("{\n")
		if sk := d.file.Skeleton; sk != nil {
			for _, k := range sk.Keys {
				kb, _ := marshal(k)
				vb, err := d.render(sk.Vals[k], 1)
				if err != nil {
					return nil, err
				}
				fmt.Fprintf(&b, "%s%s: %s,\n", d.indent, kb, vb)
			}
		}
		b.Write(member)
		b.WriteString("\n}\n")
		return b.Bytes(), nil
	}
	// Agregar el contenedor como última llave del objeto raíz.
	end := bytes.LastIndexByte(d.src, '}')
	head := bytes.TrimRight(d.src[:end], " \t\r\n")
	out := append([]byte{}, head...)
	if head[len(head)-1] != '{' {
		out = append(out, ',')
	}
	out = append(out, '\n')
	out = append(out, member...)
	out = append(out, '\n')
	return append(out, d.src[end:]...), nil
}
