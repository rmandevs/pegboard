package native

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// yamlDoc edita un YAML con el árbol de nodos de yaml.v3, que conserva los
// comentarios. El codificador sí normaliza la indentación (2 espacios).
type yamlDoc struct {
	src     []byte
	exists  bool
	file    File
	root    *yaml.Node // mapping raíz
	doc     *yaml.Node
	changed bool
}

func openYAML(src []byte, exists bool, f File) (Doc, error) {
	d := &yamlDoc{src: src, exists: exists, file: f}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("la raíz no es un mapa: %w", ErrUnsupported)
	}
	d.doc, d.root = &doc, doc.Content[0]
	return d, nil
}

func (d *yamlDoc) Exists() bool { return d.exists }

// lookup busca la llave en un mapping y devuelve el índice del nodo llave.
func lookup(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func (d *yamlDoc) container(create bool) (*yaml.Node, error) {
	i := lookup(d.root, d.file.Container)
	if i >= 0 {
		c := d.root.Content[i+1]
		if c.Kind == yaml.ScalarNode && c.Tag == "!!null" {
			if !create {
				return nil, nil
			}
			*c = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		if c.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s no es un mapa: %w", d.file.Container, ErrUnsupported)
		}
		if create {
			// `mcps: {}` pasa a estilo bloque al agregarle entradas.
			c.Style &^= yaml.FlowStyle
		}
		return c, nil
	}
	if !create {
		return nil, nil
	}
	c := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	d.root.Content = append(d.root.Content, strNode(d.file.Container), c)
	return c, nil
}

func (d *yamlDoc) Entries() (map[string]any, error) {
	out := map[string]any{}
	c, err := d.container(false)
	if err != nil || c == nil {
		return out, err
	}
	for i := 0; i+1 < len(c.Content); i += 2 {
		var v any
		if err := c.Content[i+1].Decode(&v); err != nil {
			return nil, err
		}
		out[c.Content[i].Value] = Plain(v)
	}
	return out, nil
}

func (d *yamlDoc) Set(name string, v *Obj) error {
	c, err := d.container(true)
	if err != nil {
		return err
	}
	n, err := toNode(v)
	if err != nil {
		return err
	}
	d.changed = true
	if i := lookup(c, name); i >= 0 {
		c.Content[i+1] = n
		return nil
	}
	c.Content = append(c.Content, strNode(name), n)
	return nil
}

func (d *yamlDoc) Delete(name string) error {
	c, err := d.container(false)
	if err != nil || c == nil {
		return err
	}
	if i := lookup(c, name); i >= 0 {
		c.Content = append(c.Content[:i], c.Content[i+2:]...)
		d.changed = true
	}
	return nil
}

func (d *yamlDoc) Bytes() ([]byte, error) {
	if !d.changed {
		return d.src, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d.doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func strNode(s string) *yaml.Node {
	n := &yaml.Node{}
	_ = n.Encode(s)
	return n
}

// toNode convierte un árbol (Obj, strings, listas) a nodos YAML, conservando
// el orden de llaves del Obj.
func toNode(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case *Obj:
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range x.Keys {
			c, err := toNode(x.Vals[k])
			if err != nil {
				return nil, err
			}
			m.Content = append(m.Content, strNode(k), c)
		}
		return m, nil
	case []string:
		s := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range x {
			s.Content = append(s.Content, strNode(e))
		}
		return s, nil
	}
	n := &yaml.Node{}
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	return n, nil
}

// ListDoc es un Doc YAML que además edita listas de strings de primer nivel
// (skills: en pegboard.yaml).
type ListDoc interface {
	Doc
	StringList(key string) []string
	SetStringList(key string, l []string) error
}

func (d *yamlDoc) StringList(key string) []string {
	i := lookup(d.root, key)
	if i < 0 {
		return nil
	}
	var out []string
	_ = d.root.Content[i+1].Decode(&out)
	return out
}

func (d *yamlDoc) SetStringList(key string, l []string) error {
	n, err := toNode(l)
	if err != nil {
		return err
	}
	d.changed = true
	i := lookup(d.root, key)
	if len(l) == 0 {
		if i >= 0 {
			d.root.Content = append(d.root.Content[:i], d.root.Content[i+2:]...)
		}
		return nil
	}
	if i >= 0 {
		// Conservar el estilo (en línea o bloque) que ya tenía.
		n.Style = d.root.Content[i+1].Style
		d.root.Content[i+1] = n
		return nil
	}
	d.root.Content = append(d.root.Content, strNode(key), n)
	return nil
}
