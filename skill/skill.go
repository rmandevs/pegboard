// Package skill embeds pegboard's own skill (SPEC §7) in the binary for
// `pegboard skill install` and the `pegboard` source.
package skill

import (
	"embed"
	"io/fs"
)

//go:embed SKILL.md
var files embed.FS

// FS is the skill content.
func FS() fs.FS { return files }
