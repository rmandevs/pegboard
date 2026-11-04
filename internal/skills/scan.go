package skills

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Severity of a hidden character found in a skill.
type Severity string

const (
	// Critical characters have no legitimate use in instructions for an
	// agent and are the ones used to smuggle text past a human reviewer:
	// bidirectional overrides (Trojan Source), Unicode tags and the
	// variation-selector supplement (invisible ASCII payloads).
	Critical Severity = "critical"
	// Warning characters are invisible but have legitimate uses (zero-width
	// joiners in emoji, a byte order mark in the middle of a file pasted
	// from Windows).
	Warning Severity = "warning"
)

// Finding is a hidden character inside a skill file.
type Finding struct {
	File     string   `json:"file"` // relative to the skill, with /
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Rune     string   `json:"codepoint"` // U+202E
	Name     string   `json:"name"`
	Severity Severity `json:"severity"`
	// Count is how many characters of the same kind follow on that line
	// (an invisible payload is usually a run of tags).
	Count int `json:"count"`
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s:%d:%d %s %s", f.File, f.Line, f.Col, f.Rune, f.Name)
	if f.Count > 1 {
		s += fmt.Sprintf(" (×%d)", f.Count)
	}
	return s
}

// hidden classifies a rune; "" means it is not a hidden character.
func hidden(r rune) (string, Severity) {
	switch {
	case r >= 0x202A && r <= 0x202E:
		return [...]string{"LEFT-TO-RIGHT EMBEDDING", "RIGHT-TO-LEFT EMBEDDING", "POP DIRECTIONAL FORMATTING",
			"LEFT-TO-RIGHT OVERRIDE", "RIGHT-TO-LEFT OVERRIDE"}[r-0x202A], Critical
	case r >= 0x2066 && r <= 0x2069:
		return [...]string{"LEFT-TO-RIGHT ISOLATE", "RIGHT-TO-LEFT ISOLATE", "FIRST STRONG ISOLATE",
			"POP DIRECTIONAL ISOLATE"}[r-0x2066], Critical
	case r >= 0xE0000 && r <= 0xE007F:
		return "TAG CHARACTER", Critical
	case r >= 0xE0100 && r <= 0xE01EF:
		return "VARIATION SELECTOR SUPPLEMENT", Critical
	case r == 0x200B:
		return "ZERO WIDTH SPACE", Warning
	case r == 0x200C:
		return "ZERO WIDTH NON-JOINER", Warning
	case r == 0x200D:
		return "ZERO WIDTH JOINER", Warning
	case r == 0x200E:
		return "LEFT-TO-RIGHT MARK", Warning
	case r == 0x200F:
		return "RIGHT-TO-LEFT MARK", Warning
	case r == 0x061C:
		return "ARABIC LETTER MARK", Warning
	case r >= 0x2060 && r <= 0x2064:
		return [...]string{"WORD JOINER", "FUNCTION APPLICATION", "INVISIBLE TIMES", "INVISIBLE SEPARATOR",
			"INVISIBLE PLUS"}[r-0x2060], Warning
	case r == 0xFEFF:
		return "ZERO WIDTH NO-BREAK SPACE", Warning
	case r == 0x00AD:
		return "SOFT HYPHEN", Warning
	case r == 0x180E:
		return "MONGOLIAN VOWEL SEPARATOR", Warning
	case r >= 0xFFF9 && r <= 0xFFFB:
		return "INTERLINEAR ANNOTATION", Warning
	}
	return "", ""
}

// emojiJoin reports whether a zero width joiner at this point joins two
// emoji (👩‍💻), its legitimate use.
func emojiJoin(prev, next rune) bool {
	return isEmoji(prev) && isEmoji(next)
}

func isEmoji(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F || (r >= 0x1F3FB && r <= 0x1F3FF)
}

// maxScan is the largest file scanned; bigger files are rarely instructions.
const maxScan = 4 << 20

// ScanText looks for hidden characters in one file's content.
func ScanText(file string, data []byte) []Finding {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil // binary
	}
	var out []Finding
	text := string(data)
	line, col := 1, 0
	var prev rune
	var last *Finding
	for i, r := range text {
		col++
		if r == '\n' {
			line, col, prev, last = line+1, 0, r, nil
			continue
		}
		name, sev := hidden(r)
		switch {
		case name == "":
		case r == 0xFEFF && i == 0:
			name = "" // a byte order mark at the start of a file is harmless
		case r == 0x200D:
			next, _ := utf8.DecodeRuneInString(text[i+utf8.RuneLen(r):])
			if emojiJoin(prev, next) {
				name = ""
			}
		}
		if name == "" {
			prev, last = r, nil
			continue
		}
		if last != nil && last.Name == name {
			last.Count++
		} else {
			out = append(out, Finding{File: file, Line: line, Col: col, Rune: fmt.Sprintf("U+%04X", r),
				Name: name, Severity: sev, Count: 1})
			last = &out[len(out)-1]
		}
		prev = r
	}
	return out
}

// Scan looks for hidden characters in every text file of a skill directory.
func Scan(dir string) ([]Finding, error) {
	var out []Finding
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxScan {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, ScanText(filepath.ToSlash(rel), data)...)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, err
}

// HasCritical reports whether any finding is critical.
func HasCritical(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == Critical {
			return true
		}
	}
	return false
}

// Describe summarizes findings in one line: the first ones and how many more.
func Describe(fs []Finding) string {
	const show = 2
	var parts []string
	for i, f := range fs {
		if i == show {
			parts = append(parts, fmt.Sprintf("and %d more", len(fs)-show))
			break
		}
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "; ")
}
