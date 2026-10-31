package native

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const openclawJSON5 = `{
  // top-level comment
  mcp: {
    servers: {
      old: {
        command: 'uvx',
        args: ['old-server'],
      }, // keep this comment outside the block
    },
  },
  channels: { discord: { enabled: true } },
}
`

func TestJSON5NestedContainer(t *testing.T) {
	d := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, openclawJSON5)
	if got := out(t, d); got != openclawJSON5 {
		t.Fatalf("without changes it should be identical:\n%s", got)
	}
	ents, err := d.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents["old"] == nil {
		t.Fatalf("entries: %v", ents)
	}
	if err := d.Set("new", NewObj().Set("command", "npx").Set("args", []string{"-y", "srv"})); err != nil {
		t.Fatal(err)
	}
	got := out(t, d)
	for _, want := range []string{"// top-level comment", "mcp", "servers", `"old"`, `"new"`, "channels"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	d2 := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, got)
	ents, err = d2.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 || ents["new"] == nil {
		t.Fatalf("entries after set: %v", ents)
	}
	if err := d2.Delete("old"); err != nil {
		t.Fatal(err)
	}
	got = out(t, d2)
	if strings.Contains(got, `"old"`) || !strings.Contains(got, `"new"`) {
		t.Fatalf("delete:\n%s", got)
	}
}

func TestJSON5CreatesBlock(t *testing.T) {
	d := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, "{\n  channels: {},\n}\n")
	_ = d.Set("x", NewObj().Set("url", "https://x"))
	got := out(t, d)
	for _, want := range []string{"mcp", "servers", `"x"`, "channels"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	d2 := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, got)
	if ents, err := d2.Entries(); err != nil || len(ents) != 1 || ents["x"] == nil {
		t.Fatalf("entries: %v %v", ents, err)
	}
}

func TestJSON5NewFile(t *testing.T) {
	d := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, "")
	if d.Exists() {
		t.Fatal("it should not exist")
	}
	_ = d.Set("x", NewObj().Set("command", "npx"))
	got := out(t, d)
	if !strings.Contains(got, `"mcp"`) || !strings.Contains(got, `"servers"`) || !strings.Contains(got, `"x"`) {
		t.Fatalf("new file:\n%s", got)
	}
	d2 := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, got)
	if ents, err := d2.Entries(); err != nil || len(ents) != 1 {
		t.Fatalf("entries: %v %v", ents, err)
	}
}

func TestJSON5Errors(t *testing.T) {
	f := File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}, Path: t.TempDir() + "/cfg"}
	for _, src := range []string{`{"mcp": 1}`, `{"mcp": {"servers": []}}`, `{"mcp": }`, `{`} {
		if err := os.WriteFile(f.Path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Open(f)
		if err == nil {
			t.Errorf("%q should fail", src)
			continue
		}
		if src == `{"mcp": 1}` && !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q should be unsupported, got %v", src, err)
		}
	}
}

func TestJSON5PreservesMCPKeys(t *testing.T) {
	src := "{\n  mcp: { sessionIdleTtlMs: 3600000, servers: {} },\n}\n"
	d := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, src)
	_ = d.Set("x", NewObj().Set("command", "npx"))
	got := out(t, d)
	if !strings.Contains(got, "sessionIdleTtlMs") || !strings.Contains(got, `"servers"`) {
		t.Fatalf("mcp keys:\n%s", got)
	}
	d2 := open(t, File{Format: JSON5, ContainerPath: []string{"mcp", "servers"}}, got)
	if ents, err := d2.Entries(); err != nil || len(ents) != 1 || ents["x"] == nil {
		t.Fatalf("entries: %v %v", ents, err)
	}
}
