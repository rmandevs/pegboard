package native

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Format es el formato de un archivo de config.
type Format string

const (
	JSON Format = "json"
	TOML Format = "toml"
	YAML Format = "yaml"
)

// File describe dónde guarda un target sus MCPs: el archivo, su formato y la
// llave de primer nivel que los contiene (mcpServers, mcp, mcp_servers).
type File struct {
	Path      string
	Format    Format
	Container string
	// Skeleton son llaves de primer nivel que se agregan solo al crear el
	// archivo (p. ej. "$schema" en opencode.json).
	Skeleton *Obj
}

// Doc es un archivo abierto para editar sus entradas. Solo se reescribe la
// parte del contenedor; el resto del archivo queda como estaba (en YAML, lo
// que permita el codificador).
type Doc interface {
	// Entries son las entradas actuales del contenedor, en forma Plain.
	Entries() (map[string]any, error)
	Set(name string, v *Obj) error
	Delete(name string) error
	// Bytes es el contenido resultante; igual al original si no hubo cambios.
	Bytes() ([]byte, error)
	// Exists informa si el archivo existía al abrirlo.
	Exists() bool
}

// ErrUnsupported marca entradas escritas de una forma que pegboard no sabe
// editar sin riesgo; el plan las muestra como conflicto.
var ErrUnsupported = errors.New("formato no soportado")

// Open lee el archivo (si existe) y lo prepara para editar.
func Open(f File) (Doc, error) {
	src, err := os.ReadFile(f.Path)
	exists := true
	if errors.Is(err, fs.ErrNotExist) {
		src, exists, err = nil, false, nil
	}
	if err != nil {
		return nil, err
	}
	var d Doc
	switch f.Format {
	case JSON:
		d, err = openJSON(src, exists, f)
	case TOML:
		d, err = openTOML(src, exists, f)
	case YAML:
		d, err = openYAML(src, exists, f)
	default:
		return nil, fmt.Errorf("formato desconocido %q", f.Format)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	return d, nil
}
