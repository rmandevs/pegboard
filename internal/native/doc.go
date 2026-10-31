package native

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Format is the format of a config file.
type Format string

const (
	JSON Format = "json"
	TOML Format = "toml"
	YAML Format = "yaml"
)

// File describes where a target stores its MCPs: the file, its format, and
// the top-level key that contains them (mcpServers, mcp, mcp_servers).
type File struct {
	Path      string
	Format    Format
	Container string
	// Skeleton is top-level keys added only when creating the file (for
	// example, "$schema" in opencode.json).
	Skeleton *Obj
}

// Doc is a file opened for editing its entries. Only the container is
// rewritten; the rest of the file stays as it was (in YAML, as far as the
// encoder allows).
type Doc interface {
	// Entries are the current entries in the container, in Plain shape.
	Entries() (map[string]any, error)
	Set(name string, v *Obj) error
	Delete(name string) error
	// Bytes is the resulting content; equal to the original if nothing
	// changed.
	Bytes() ([]byte, error)
	// Exists reports whether the file existed when it was opened.
	Exists() bool
}

// ErrUnsupported marks entries written in a way pegboard cannot safely edit;
// the plan shows them as conflicts.
var ErrUnsupported = errors.New("unsupported format")

// Open reads the file (if it exists) and prepares it for editing.
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
		return nil, fmt.Errorf("unknown format %q", f.Format)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	return d, nil
}
