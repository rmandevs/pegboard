// Package skill trae la skill propia de pegboard (SPEC §7), embebida en el
// binario para `pegboard skill install` y la fuente `pegboard`.
package skill

import (
	"embed"
	"io/fs"
)

//go:embed SKILL.md
var files embed.FS

// FS es el contenido de la skill.
func FS() fs.FS { return files }
