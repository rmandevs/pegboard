package native

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// tomlDoc edita config.toml de Codex por secciones de texto: cada MCP es una
// tabla [mcp_servers.<nombre>] (más sus subtablas). Se quitan esas
// secciones y se escribe la nueva en su lugar; comentarios y el resto del
// archivo quedan intactos. Un MCP escrito de otra forma (tabla en línea,
// llaves con puntos) es ErrUnsupported.
type tomlDoc struct {
	lines   []string
	exists  bool
	file    File
	parsed  map[string]any
	changed bool
}

var headerRe = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)

func openTOML(src []byte, exists bool, f File) (Doc, error) {
	d := &tomlDoc{exists: exists, file: f, parsed: map[string]any{}}
	if len(src) > 0 {
		if err := toml.Unmarshal(src, &d.parsed); err != nil {
			return nil, err
		}
		d.lines = strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	}
	return d, nil
}

func (d *tomlDoc) Exists() bool { return d.exists }

func (d *tomlDoc) servers() map[string]any {
	m, _ := d.parsed[d.file.Container].(map[string]any)
	return m
}

func (d *tomlDoc) Entries() (map[string]any, error) {
	out := map[string]any{}
	for k, v := range d.servers() {
		out[k] = Plain(v)
	}
	return out, nil
}

// header es una línea [a.b."c"] del archivo, ya separada en partes.
func header(line string) ([]string, bool) {
	if strings.HasPrefix(strings.TrimSpace(line), "[[") {
		return []string{"[["}, true
	}
	m := headerRe.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	parts, err := splitDotted(m[1])
	if err != nil {
		return nil, false
	}
	return parts, true
}

// splitDotted separa una llave TOML con puntos: a."b.c".d → [a b.c d].
func splitDotted(s string) ([]string, error) {
	var parts []string
	s = strings.TrimSpace(s)
	for s != "" {
		var part string
		switch s[0] {
		case '"':
			end := 1
			for end < len(s) && (s[end] != '"' || s[end-1] == '\\') {
				end++
			}
			if end >= len(s) {
				return nil, fmt.Errorf("comillas sin cerrar")
			}
			uq, err := strconv.Unquote(s[:end+1])
			if err != nil {
				return nil, err
			}
			part, s = uq, s[end+1:]
		case '\'':
			end := strings.IndexByte(s[1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("comillas sin cerrar")
			}
			part, s = s[1:end+1], s[end+2:]
		default:
			end := strings.IndexByte(s, '.')
			if end < 0 {
				end = len(s)
			}
			part, s = strings.TrimSpace(s[:end]), s[end:]
		}
		parts = append(parts, part)
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, ".") {
			s = strings.TrimSpace(s[1:])
		}
	}
	return parts, nil
}

// sections devuelve los rangos [inicio, fin) de líneas que pertenecen al MCP.
func (d *tomlDoc) sections(name string) [][2]int {
	var out [][2]int
	start := -1
	for i, l := range d.lines {
		parts, ok := header(l)
		if !ok {
			continue
		}
		if start >= 0 {
			out = append(out, [2]int{start, i})
			start = -1
		}
		if len(parts) >= 2 && parts[0] == d.file.Container && parts[1] == name {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(d.lines)})
	}
	return out
}

func (d *tomlDoc) remove(name string) (int, error) {
	secs := d.sections(name)
	if _, ok := d.servers()[name]; ok && len(secs) == 0 {
		return 0, fmt.Errorf("%s.%s no está escrito como tabla [%s.%s]: %w",
			d.file.Container, name, d.file.Container, name, ErrUnsupported)
	}
	if len(secs) == 0 {
		return -1, nil
	}
	at := secs[0][0]
	for i := len(secs) - 1; i >= 0; i-- {
		s := secs[i]
		// La última sección conserva la línea en blanco que la separa de la
		// siguiente; las anteriores se quitan completas.
		end := s[1]
		for i == len(secs)-1 && end > s[0] && strings.TrimSpace(d.lines[end-1]) == "" {
			end--
		}
		d.lines = append(d.lines[:s[0]], d.lines[end:]...)
	}
	return at, nil
}

func (d *tomlDoc) Set(name string, v *Obj) error {
	at, err := d.remove(name)
	if err != nil {
		return err
	}
	sec, err := tomlSection(d.file.Container, name, v)
	if err != nil {
		return err
	}
	if at < 0 {
		if n := len(d.lines); n > 0 && strings.TrimSpace(d.lines[n-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, sec...)
	} else {
		d.lines = append(d.lines[:at], append(sec, d.lines[at:]...)...)
	}
	d.markChanged(name, v)
	return nil
}

func (d *tomlDoc) Delete(name string) error {
	at, err := d.remove(name)
	if err != nil || at < 0 {
		return err
	}
	// Quitar una línea en blanco sobrante donde estaba la sección.
	if at < len(d.lines) && at > 0 && strings.TrimSpace(d.lines[at]) == "" && strings.TrimSpace(d.lines[at-1]) == "" {
		d.lines = append(d.lines[:at], d.lines[at+1:]...)
	}
	d.markChanged(name, nil)
	return nil
}

func (d *tomlDoc) markChanged(name string, v *Obj) {
	d.changed = true
	srv := d.servers()
	if srv == nil {
		srv = map[string]any{}
		d.parsed[d.file.Container] = srv
	}
	if v == nil {
		delete(srv, name)
	} else {
		srv[name] = Plain(v)
	}
}

func (d *tomlDoc) Bytes() ([]byte, error) {
	out := strings.Join(d.lines, "\n")
	if len(d.lines) > 0 {
		out += "\n"
	}
	if d.changed {
		// Verificación: lo escrito debe volver a leerse igual.
		var check map[string]any
		if err := toml.Unmarshal([]byte(out), &check); err != nil {
			return nil, fmt.Errorf("el TOML resultante no es válido: %w", err)
		}
	}
	return []byte(out), nil
}

func tomlSection(container, name string, v *Obj) ([]string, error) {
	lines := []string{"[" + tomlKey(container) + "." + tomlKey(name) + "]"}
	for _, k := range v.Keys {
		val, err := tomlValue(v.Vals[k])
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", name, k, err)
		}
		lines = append(lines, tomlKey(k)+" = "+val)
	}
	return lines, nil
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return tomlString(k)
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return tomlString(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case []string:
		q := make([]string, len(x))
		for i, s := range x {
			q[i] = tomlString(s)
		}
		return "[" + strings.Join(q, ", ") + "]", nil
	case *Obj:
		var kv []string
		for _, k := range x.Keys {
			val, err := tomlValue(x.Vals[k])
			if err != nil {
				return "", err
			}
			kv = append(kv, tomlKey(k)+" = "+val)
		}
		if len(kv) == 0 {
			return "{}", nil
		}
		return "{ " + strings.Join(kv, ", ") + " }", nil
	}
	return "", fmt.Errorf("tipo %T no soportado en TOML", v)
}
