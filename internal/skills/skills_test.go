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
		{"./skills/mine", Local, "", "", "", ""},
		{"~/skills/mine", Local, "", "", "", ""},
		{"pegboard", Builtin, "", "", "", ""},
		// APM shorthand.
		{"microsoft/apm-sample-package#v1.0.0", Git, GitHub + "microsoft/apm-sample-package.git", "", "v1.0.0", ""},
		{"anthropics/skills/skills/frontend-design", Git, GitHub + "anthropics/skills.git", "skills/frontend-design", "", ""},
		{"ComposioHQ/awesome-claude-skills/brand-guidelines#main", Git, GitHub + "ComposioHQ/awesome-claude-skills.git", "brand-guidelines", "main", ""},
		{"gitlab.com/acme/standards/skills/x#v2", Git, "https://gitlab.com/acme/standards.git", "skills/x", "v2", ""},
		{"bitbucket.org/team/repo#main", Git, "https://bitbucket.org/team/repo.git", "", "main", ""},
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
	for _, bad := range []string{"", "alone", "o/r@bad name", "o/r?ref=", "https://github.com/o/r/tree/main?ref=x", "o/r#", "o/r#a?ref=b", "o/r/../x"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should fail", bad)
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
		"skills/cat/review/sub/SKILL.md": "shadowed by the one above",
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
	write(t, single, map[string]string{"SKILL.md": "# without frontmatter"})
	if f, err := Discover(single); err != nil || f[filepath.Base(single)] == "" {
		t.Fatalf("root: %v %v", f, err)
	}
	deep := t.TempDir()
	write(t, deep, map[string]string{"a/b/c/d/x/SKILL.md": "---\nname: x\n---"})
	if f, err := Discover(deep); err != nil || f["x"] == "" {
		t.Fatalf("full search: %v %v", f, err)
	}
	if _, err := Discover(t.TempDir()); err == nil {
		t.Error("without skills it should fail")
	}
	dup := t.TempDir()
	write(t, dup, map[string]string{"skills/a/SKILL.md": "---\nname: x\n---", "skills/b/SKILL.md": "---\nname: x\n---"})
	if _, err := Discover(dup); err == nil || !strings.Contains(err.Error(), "two skills") {
		t.Errorf("duplicates: %v", err)
	}
}

func TestHashStable(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, map[string]string{"SKILL.md": "x", "ref/a.md": "y"})
	write(t, b, map[string]string{"ref/a.md": "y", "SKILL.md": "x"})
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha != hb {
		t.Error("the hash should not depend on creation order")
	}
	write(t, b, map[string]string{"SKILL.md": "z"})
	if hb2, _ := Hash(b); hb2 == ha {
		t.Error("the hash should change with content")
	}
}

// repo creates a local git repo with the given files and returns its file://
// URL.
func repo(t *testing.T, files map[string]string) (string, func(map[string]string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
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

	// The repo moves on; with a lock the pinned version is still used,
	// without network.
	commit(map[string]string{"skills/tdd/SKILL.md": "---\nname: tdd\n---\nv2"})
	offline := &Resolver{Cache: cache, Lock: lock, Git: func(string, ...string) (string, error) {
		t.Fatal("with a lock and cache it should not use git")
		return "", nil
	}}
	got, err = offline.Resolve(src)
	if err != nil || got[1].Hash != v1 {
		t.Fatalf("pinned: %+v %v", got, err)
	}

	// Without cache, it downloads the pinned commit again (not main's tip).
	r2 := &Resolver{Cache: t.TempDir(), Lock: lock}
	got, err = r2.Resolve(src)
	if err != nil || got[1].Hash != v1 {
		t.Fatalf("pinned commit: %+v %v", got, err)
	}

	// update ignores the lock.
	up := &Resolver{Cache: cache, Lock: lock, Update: true}
	got, err = up.Resolve(src)
	if err != nil || got[1].Hash == v1 || !lock.Changed() {
		t.Fatalf("update: %+v %v", got, err)
	}

	// A single skill with @.
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
	if _, err := (&Resolver{Cache: cache}).Resolve(one); err == nil || !strings.Contains(err.Error(), "available: review, tdd") {
		t.Errorf("missing skill: %v", err)
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
	ls.Skills["x"] = "sha256:other"
	lock.Sources[src.Raw] = ls
	_, err := (&Resolver{Cache: t.TempDir(), Lock: lock}).Resolve(src)
	if err == nil || !strings.Contains(err.Error(), "does not match pegboard.lock") {
		t.Fatalf("it should detect content different from the lock: %v", err)
	}
}

func TestResolveLocalAndBuiltin(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"skills/mine/SKILL.md": "---\nname: mine\n---"})
	src, _ := Parse("./skills/mine")
	got, err := (&Resolver{Cache: t.TempDir(), BaseDir: base}).Resolve(src)
	if err != nil || len(got) != 1 || !got[0].Local || got[0].Dir != filepath.Join(base, "skills", "mine") {
		t.Fatalf("%+v %v", got, err)
	}
	b, _ := Parse("pegboard")
	r := &Resolver{Cache: t.TempDir(), Builtin: fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: pegboard\n---")}}}
	got, err = r.Resolve(b)
	if err != nil || len(got) != 1 || got[0].Name != "pegboard" || got[0].Local {
		t.Fatalf("%+v %v", got, err)
	}
}
