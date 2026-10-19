package native

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T, f File, content string) Doc {
	t.Helper()
	f.Path = filepath.Join(t.TempDir(), "cfg")
	if content != "" {
		if err := os.WriteFile(f.Path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func out(t *testing.T, d Doc) string {
	t.Helper()
	b, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func github() *Obj {
	return NewObj().Set("command", "npx").Set("args", []string{"-y", "srv"}).
		Set("env", StringMap(map[string]string{"TOKEN": "${TOKEN}"}))
}

const claudeJSON = `{
  "numStartups": 42,
  "tipsHistory": {"a": 1},
  "mcpServers": {
    "mine": {
      "command": "a  b",
      "args": []
    }
  },
  "projects": {"/x": {"mcpServers": {"local": {}}}},
  "html": "<b>&</b>"
}
`

func TestJSONPreservesOutside(t *testing.T) {
	d := open(t, File{Format: JSON, Container: "mcpServers"}, claudeJSON)
	if got := out(t, d); got != claudeJSON {
		t.Fatalf("sin cambios debía ser idéntico:\n%s", got)
	}
	ents, _ := d.Entries()
	if len(ents) != 1 || ents["mine"] == nil {
		t.Fatalf("entradas: %v", ents)
	}
	if err := d.Set("github", github()); err != nil {
		t.Fatal(err)
	}
	want := `{
  "numStartups": 42,
  "tipsHistory": {"a": 1},
  "mcpServers": {
    "mine": {
      "command": "a  b",
      "args": []
    },
    "github": {
      "command": "npx",
      "args": [
        "-y",
        "srv"
      ],
      "env": {
        "TOKEN": "${TOKEN}"
      }
    }
  },
  "projects": {"/x": {"mcpServers": {"local": {}}}},
  "html": "<b>&</b>"
}
`
	if got := out(t, d); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if err := d.Delete("github"); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if got := out(t, d); !strings.Contains(got, `"mcpServers": {},`) || !strings.Contains(got, `"projects": {"/x"`) {
		t.Fatalf("delete:\n%s", got)
	}
}

func TestJSONAddContainer(t *testing.T) {
	d := open(t, File{Format: JSON, Container: "mcpServers"}, "{\n    \"a\": 1\n}\n")
	_ = d.Set("x", NewObj().Set("url", "https://x"))
	want := "{\n    \"a\": 1,\n    \"mcpServers\": {\n        \"x\": {\n            \"url\": \"https://x\"\n        }\n    }\n}\n"
	if got := out(t, d); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
	d = open(t, File{Format: JSON, Container: "mcpServers"}, "{}")
	_ = d.Set("x", NewObj().Set("url", "https://x"))
	if got := out(t, d); !strings.HasPrefix(got, "{\n  \"mcpServers\": {") {
		t.Fatalf("objeto vacío:\n%s", got)
	}
}

func TestJSONNewFileWithSkeleton(t *testing.T) {
	d := open(t, File{Format: JSON, Container: "mcp", Skeleton: NewObj().Set("$schema", "https://opencode.ai/config.json")}, "")
	if d.Exists() {
		t.Fatal("no debía existir")
	}
	_ = d.Set("x", NewObj().Set("type", "remote"))
	want := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"mcp\": {\n    \"x\": {\n      \"type\": \"remote\"\n    }\n  }\n}\n"
	if got := out(t, d); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestJSONErrors(t *testing.T) {
	f := File{Format: JSON, Container: "mcpServers", Path: filepath.Join(t.TempDir(), "x.json")}
	for _, c := range []string{"[1]", "{\"mcpServers\": [1]}", "{} {}", "{"} {
		_ = os.WriteFile(f.Path, []byte(c), 0o600)
		if _, err := Open(f); err == nil {
			t.Errorf("%q debía fallar", c)
		}
	}
}

const codexTOML = `# Mi config de Codex
model = "o4"

[mcp_servers.github]
command = "old"
# comentario dentro

[mcp_servers.github.env]
A = "1"

[mcp_servers.otro] # ajeno
command = "x"

[profiles.dev]
model = "o3"
`

func TestTOMLReplaceInPlace(t *testing.T) {
	d := open(t, File{Format: TOML, Container: "mcp_servers"}, codexTOML)
	ents, _ := d.Entries()
	if Str(ents["github"].(map[string]any), "command") != "old" || len(ents) != 2 {
		t.Fatalf("entradas: %v", ents)
	}
	if err := d.Set("github", github()); err != nil {
		t.Fatal(err)
	}
	want := `# Mi config de Codex
model = "o4"

[mcp_servers.github]
command = "npx"
args = ["-y", "srv"]
env = { TOKEN = "${TOKEN}" }

[mcp_servers.otro] # ajeno
command = "x"

[profiles.dev]
model = "o3"
`
	if got := out(t, d); got != want {
		t.Fatalf("got:\n%s", got)
	}
	_ = d.Set("nuevo.srv", NewObj().Set("url", "https://x"))
	if got := out(t, d); !strings.HasSuffix(got, "model = \"o3\"\n\n[mcp_servers.\"nuevo.srv\"]\nurl = \"https://x\"\n") {
		t.Fatalf("agregar:\n%s", got)
	}
	_ = d.Delete("otro")
	_ = d.Delete("nuevo.srv")
	got := out(t, d)
	if strings.Contains(got, "otro") || strings.Contains(got, "nuevo") || !strings.Contains(got, "[profiles.dev]") {
		t.Fatalf("quitar:\n%s", got)
	}
}

func TestTOMLUnsupported(t *testing.T) {
	d := open(t, File{Format: TOML, Container: "mcp_servers"}, "[mcp_servers]\ngithub = { command = \"x\" }\n")
	if err := d.Set("github", github()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("esperaba ErrUnsupported, fue %v", err)
	}
	if err := d.Set("otro", github()); err != nil {
		t.Fatalf("otro MCP sí debía poder agregarse: %v", err)
	}
}

func TestTOMLNewFile(t *testing.T) {
	d := open(t, File{Format: TOML, Container: "mcp_servers"}, "")
	_ = d.Set("a", NewObj().Set("command", "x\"y"))
	if got := out(t, d); got != "[mcp_servers.a]\ncommand = \"x\\\"y\"\n" {
		t.Fatalf("got %q", got)
	}
}

const hermesYAML = `# Config de Hermes
model: nous/hermes-4
mcp_servers:
  # el mío
  local:
    command: uvx
    args: [srv]
toolsets:
  - web
`

func TestYAMLKeepsComments(t *testing.T) {
	d := open(t, File{Format: YAML, Container: "mcp_servers"}, hermesYAML)
	if got := out(t, d); got != hermesYAML {
		t.Fatal("sin cambios debía ser idéntico")
	}
	_ = d.Set("github", github())
	got := out(t, d)
	for _, want := range []string{"# Config de Hermes", "# el mío", "toolsets:", "  github:\n    command: npx\n    args:\n      - -y\n      - srv\n    env:\n      TOKEN: ${TOKEN}\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en:\n%s", want, got)
		}
	}
	_ = d.Delete("local")
	if got := out(t, d); strings.Contains(got, "uvx") {
		t.Errorf("no quitó local:\n%s", got)
	}
}

func TestYAMLNewAndNull(t *testing.T) {
	d := open(t, File{Format: YAML, Container: "mcp_servers"}, "")
	_ = d.Set("a", NewObj().Set("url", "https://x"))
	if got := out(t, d); got != "mcp_servers:\n  a:\n    url: https://x\n" {
		t.Fatalf("got %q", got)
	}
	d = open(t, File{Format: YAML, Container: "mcp_servers"}, "mcp_servers:\n")
	if e, _ := d.Entries(); len(e) != 0 {
		t.Fatal(e)
	}
	_ = d.Set("a", NewObj().Set("url", "https://x"))
	if got := out(t, d); got != "mcp_servers:\n  a:\n    url: https://x\n" {
		t.Fatalf("got %q", got)
	}
}

func TestHashMasks(t *testing.T) {
	a := NewObj().Set("env", StringMap(map[string]string{"T": "secreto1"}))
	b := NewObj().Set("env", StringMap(map[string]string{"T": "secreto2"}))
	m := []string{Pointer("env", "T")}
	if Hash(a, m) != Hash(b, m) {
		t.Error("el hash no debía depender del secreto")
	}
	if Hash(a, nil) == Hash(b, nil) {
		t.Error("sin máscara los hashes debían diferir")
	}
	if Pointer("a/b", "c~d") != "/a~1b/c~0d" || len(splitPointer("/a~1b/c~0d")) != 2 {
		t.Error("pointer")
	}
}
