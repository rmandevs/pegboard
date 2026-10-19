package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in                    string
		kind                  Kind
		url, path, ref, skill string
	}{
		{"mattpocock/skills", Git, GitHub + "mattpocock/skills.git", "", "", ""},
		{"vercel-labs/agent-skills@web-design", Git, GitHub + "vercel-labs/agent-skills.git", "", "", "web-design"},
		{"o/r@s?ref=v1", Git, GitHub + "o/r.git", "", "v1", "s"},
		{"github.com/anthropics/skills//pdf?ref=main", Git, "https://github.com/anthropics/skills.git", "pdf", "main", ""},
		{"gitlab.com/org/repo", Git, "https://gitlab.com/org/repo.git", "", "", ""},
		{"https://github.com/o/r/tree/main/skills/x", Git, "https://github.com/o/r.git", "skills/x", "main", ""},
		{"https://git.example.com/a/b.git//skills?ref=abc", Git, "https://git.example.com/a/b.git", "skills", "abc", ""},
		{"git@github.com:o/r.git", Git, "git@github.com:o/r.git", "", "", ""},
		{"./skills/mia", Local, "", "", "", ""},
		{"~/skills/mia", Local, "", "", "", ""},
		{"pegboard", Builtin, "", "", "", ""},
	}
	for _, c := range cases {
		s, err := Parse(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if s.Kind != c.kind || s.URL != c.url || s.Path != c.path || s.Ref != c.ref || s.Skill != c.skill {
			t.Errorf("%s: %+v", c.in, s)
		}
	}
	for _, bad := range []string{"", "solo", "o/r@mal nombre", "o/r?ref=", "https://github.com/o/r/tree/main?ref=x"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q debía fallar", bad)
		}
	}
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"skills/tdd/SKILL.md":            "---\nname: tdd\ndescription: x\n---\n# TDD",
		"skills/cat/review/SKILL.md":     "---\nname: code-review\n---\n",
		"skills/cat/review/sub/SKILL.md": "sombreada por la de arriba",
		"README.md":                      "x",
	})
	found, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found["tdd"] == "" || found["code-review"] == "" {
		t.Fatalf("%v", found)
	}
	single := t.TempDir()
	write(t, single, map[string]string{"SKILL.md": "# sin frontmatter"})
	if f, err := Discover(single); err != nil || f[filepath.Base(single)] == "" {
		t.Fatalf("raíz: %v %v", f, err)
	}
	deep := t.TempDir()
	write(t, deep, map[string]string{"a/b/c/d/x/SKILL.md": "---\nname: x\n---"})
	if f, err := Discover(deep); err != nil || f["x"] == "" {
		t.Fatalf("búsqueda completa: %v %v", f, err)
	}
	if _, err := Discover(t.TempDir()); err == nil {
		t.Error("sin skills debía fallar")
	}
	dup := t.TempDir()
	write(t, dup, map[string]string{"skills/a/SKILL.md": "---\nname: x\n---", "skills/b/SKILL.md": "---\nname: x\n---"})
	if _, err := Discover(dup); err == nil || !strings.Contains(err.Error(), "dos skills") {
		t.Errorf("duplicadas: %v", err)
	}
}

func TestHashStable(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, map[string]string{"SKILL.md": "x", "ref/a.md": "y"})
	write(t, b, map[string]string{"ref/a.md": "y", "SKILL.md": "x"})
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha != hb {
		t.Error("el hash no debía depender del orden de creación")
	}
	write(t, b, map[string]string{"SKILL.md": "z"})
	if hb2, _ := Hash(b); hb2 == ha {
		t.Error("el hash debía cambiar con el contenido")
	}
}

// repo crea un repo git local con los archivos dados y devuelve su URL file://.
func repo(t *testing.T, files map[string]string) (string, func(map[string]string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está instalado")
	}
	dir := t.TempDir()
	gitRun := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitRun("init", "-q", "-b", "main")
	gitRun("config", "uploadpack.allowAnySHA1InWant", "true")
	commit := func(fs map[string]string) string {
		write(t, dir, fs)
		gitRun("add", "-A")
		gitRun("commit", "-q", "-m", "c")
		return gitRun("rev-parse", "HEAD")
	}
	commit(files)
	return FileURL(dir), commit
}

func TestResolveGitAndLock(t *testing.T) {
	url, commit := repo(t, map[string]string{
		"skills/tdd/SKILL.md":    "---\nname: tdd\n---\nv1",
		"skills/review/SKILL.md": "---\nname: review\n---\n",
	})
	cache := t.TempDir()
	lock, _ := LoadLock(filepath.Join(t.TempDir(), LockFile))
	r := &Resolver{Cache: cache, Lock: lock}
	src, err := Parse(url + "?ref=main")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Resolve(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Name != "tdd" || !strings.HasPrefix(got[1].Dir, cache) || got[1].Commit == "" {
		t.Fatalf("%+v", got)
	}
	if !lock.Changed() || lock.Sources[src.Raw].Skills["tdd"] != got[1].Hash {
		t.Fatalf("lock: %+v", lock.Sources)
	}
	if err := lock.Save(); err != nil {
		t.Fatal(err)
	}
	v1 := got[1].Hash

	// El repo avanza; con lock se sigue usando lo fijado, sin red.
	commit(map[string]string{"skills/tdd/SKILL.md": "---\nname: tdd\n---\nv2"})
	offline := &Resolver{Cache: cache, Lock: lock, Git: func(string, ...string) (string, error) {
		t.Fatal("con lock y caché no debía usar git")
		return "", nil
	}}
	got, err = offline.Resolve(src)
	if err != nil || got[1].Hash != v1 {
		t.Fatalf("fijado: %+v %v", got, err)
	}

	// Sin caché, vuelve a bajar el commit fijado (no la punta de main).
	r2 := &Resolver{Cache: t.TempDir(), Lock: lock}
	got, err = r2.Resolve(src)
	if err != nil || got[1].Hash != v1 {
		t.Fatalf("commit fijado: %+v %v", got, err)
	}

	// update ignora el lock.
	up := &Resolver{Cache: cache, Lock: lock, Update: true}
	got, err = up.Resolve(src)
	if err != nil || got[1].Hash == v1 || !lock.Changed() {
		t.Fatalf("update: %+v %v", got, err)
	}

	// Una sola skill con @.
	one, err := Parse(strings.Replace(url, "file://", "file://", 1))
	if err != nil {
		t.Fatal(err)
	}
	one.Skill = "review"
	got, err = (&Resolver{Cache: cache}).Resolve(one)
	if err != nil || len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("@skill: %+v %v", got, err)
	}
	one.Skill = "nope"
	if _, err := (&Resolver{Cache: cache}).Resolve(one); err == nil || !strings.Contains(err.Error(), "tiene: review, tdd") {
		t.Errorf("skill inexistente: %v", err)
	}
}

func TestResolveIntegrity(t *testing.T) {
	url, _ := repo(t, map[string]string{"SKILL.md": "---\nname: x\n---"})
	src, _ := Parse(url)
	lock, _ := LoadLock(filepath.Join(t.TempDir(), LockFile))
	if _, err := (&Resolver{Cache: t.TempDir(), Lock: lock}).Resolve(src); err != nil {
		t.Fatal(err)
	}
	ls := lock.Sources[src.Raw]
	ls.Skills["x"] = "sha256:otro"
	lock.Sources[src.Raw] = ls
	_, err := (&Resolver{Cache: t.TempDir(), Lock: lock}).Resolve(src)
	if err == nil || !strings.Contains(err.Error(), "no coincide con pegboard.lock") {
		t.Fatalf("debía detectar contenido distinto al lock: %v", err)
	}
}

func TestResolveLocalAndBuiltin(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"skills/mia/SKILL.md": "---\nname: mia\n---"})
	src, _ := Parse("./skills/mia")
	got, err := (&Resolver{Cache: t.TempDir(), BaseDir: base}).Resolve(src)
	if err != nil || len(got) != 1 || !got[0].Local || got[0].Dir != filepath.Join(base, "skills", "mia") {
		t.Fatalf("%+v %v", got, err)
	}
	b, _ := Parse("pegboard")
	r := &Resolver{Cache: t.TempDir(), Builtin: fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: pegboard\n---")}}}
	got, err = r.Resolve(b)
	if err != nil || len(got) != 1 || got[0].Name != "pegboard" || got[0].Local {
		t.Fatalf("%+v %v", got, err)
	}
}
