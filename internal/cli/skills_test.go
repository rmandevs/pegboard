package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
	"github.com/rmandevs/agents-casc/internal/skills"
)

// gitHub crea repos git locales que hacen de GitHub: acme/skills se resuelve
// a file://<dir>/acme/skills.git. Devuelve cómo agregar commits a un repo.
func gitHub(t *testing.T) func(repo string, files map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está instalado")
	}
	base := t.TempDir()
	prev := skills.GitHub
	skills.GitHub = skills.FileURL(base) + "/"
	t.Cleanup(func() { skills.GitHub = prev })
	return func(repo string, files map[string]string) {
		dir := filepath.Join(base, filepath.FromSlash(repo)+".git")
		g := func(args ...string) {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		if _, err := os.Stat(dir); err != nil {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			g("init", "-q", "-b", "main")
			g("config", "uploadpack.allowAnySHA1InWant", "true")
		}
		for rel, content := range files {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		g("add", "-A")
		g("commit", "-q", "-m", "c")
	}
}

var acmeV1 = map[string]string{
	"skills/tdd/SKILL.md":    "---\nname: tdd\ndescription: tests primero\n---\nv1\n",
	"skills/review/SKILL.md": "---\nname: review\ndescription: revisar\n---\n",
}

func linkOf(t *testing.T, m *agenttest.Machine, rel string) string {
	t.Helper()
	l, err := os.Readlink(m.Path(rel))
	if err != nil {
		t.Fatalf("%s no es un symlink: %v", rel, err)
	}
	return l
}

func TestSkillsDeclarative(t *testing.T) {
	commit := gitHub(t)
	commit("acme/skills", acmeV1)
	m := fleet(t)
	m.File("home/.config/pegboard/skills/mia/SKILL.md", "---\nname: mia\n---\nlocal\n")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nskills:\n  - acme/skills@tdd\n  - ./skills/mia\n")

	out := mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "plan", out,
		"claude-code · global\n  + skill mia  (~/.claude/skills/mia → ~/.config/pegboard/skills/mia)\n  + skill tdd  (~/.claude/skills/tdd → cache@",
		"codex · global\n  + skill mia", "(~/.agents/skills/tdd",
		"opencode · global\n  + skill mia", "(~/.config/opencode/skills/tdd",
		"hermes · global", "hermes:work · global", "(~/.hermes/profiles/work/skills/tdd",
		"Plan: 10 crear")
	if agentExists(m.Path("home/.config/pegboard/pegboard.lock")) {
		t.Fatal("plan no debe escribir el lock")
	}
	mustRun(t, m, 0, "apply", "-y")
	tdd := linkOf(t, m, "home/.claude/skills/tdd")
	if !strings.HasPrefix(tdd, m.Path("home/.cache/pegboard/skills/")) {
		t.Fatalf("tdd debía apuntar al caché: %s", tdd)
	}
	if linkOf(t, m, "home/.agents/skills/tdd") != tdd || linkOf(t, m, "home/.hermes/skills/tdd") != tdd {
		t.Error("todos los agentes deben compartir la misma copia")
	}
	if b, _ := os.ReadFile(filepath.Join(tdd, "SKILL.md")); !strings.Contains(string(b), "v1") {
		t.Errorf("contenido: %s", b)
	}
	if linkOf(t, m, "home/.claude/skills/mia") != m.Path("home/.config/pegboard/skills/mia") {
		t.Error("una skill local se enlaza tal cual")
	}
	lock := read(t, m, "home/.config/pegboard/pegboard.lock")
	mustContain(t, "lock", lock, `"acme/skills@tdd"`, `"commit": "`, `"tdd": "sha256:`)
	if strings.Contains(lock, "mia") {
		t.Error("las skills locales no van al lock")
	}
	mustContain(t, "idempotente", mustRun(t, m, 0, "plan", "--exit-code"), "Sin cambios")
	mustContain(t, "drift", mustRun(t, m, 0, "drift", "--exit-code"), "skills · global  skill tdd  ok")

	// El repo avanza: el lock lo fija hasta `update`.
	commit("acme/skills", map[string]string{"skills/tdd/SKILL.md": "---\nname: tdd\n---\nv2\n", "skills/tdd/extra.md": "x"})
	mustContain(t, "fijado", mustRun(t, m, 0, "plan", "--exit-code"), "Sin cambios")
	stdin = "n\n"
	out = mustRun(t, m, 0, "update")
	stdin = ""
	mustContain(t, "update cancelado", out, "~ skill tdd (acme/skills@tdd)", ": +extra.md ~SKILL.md", "Cancelado")
	if read(t, m, "home/.config/pegboard/pegboard.lock") != lock {
		t.Fatal("cancelar no debía tocar el lock")
	}
	out = mustRun(t, m, 0, "update", "tdd", "-y")
	mustContain(t, "update", out, "Escrito ~/.config/pegboard/pegboard.lock")
	out = mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "plan tras update", out, "~ skill tdd  (~/.claude/skills/tdd ", " → ", "Plan: 0 crear, 5 modificar")
	mustRun(t, m, 0, "apply", "-y")
	if b, _ := os.ReadFile(filepath.Join(linkOf(t, m, "home/.claude/skills/tdd"), "SKILL.md")); !strings.Contains(string(b), "v2") {
		t.Errorf("tras update+apply debía quedar v2: %s", b)
	}
	mustContain(t, "al día", mustRun(t, m, 0, "update", "-y"), "ya están al día")

	// Quitarla del stack la quita de los agentes y del lock.
	mustContain(t, "remove", mustRun(t, m, 0, "remove", "skill", "tdd"), "- skill acme/skills@tdd")
	out = mustRun(t, m, 0, "apply", "-y")
	mustContain(t, "apply remove", out, "- skill tdd")
	if agentExists(m.Path("home/.claude/skills/tdd")) {
		t.Fatal("no quitó el symlink")
	}
	if strings.Contains(read(t, m, "home/.config/pegboard/pegboard.lock"), "acme") {
		t.Error("el lock debía olvidar la fuente")
	}
}

func TestSkillsSharedDirAndConflicts(t *testing.T) {
	commit := gitHub(t)
	commit("acme/skills", acmeV1)
	m := fleet(t)
	m.File("proj/pegboard.yaml", "version: 1\nskills: [acme/skills]\n")
	m.File("proj/.claude/skills/review/SKILL.md", "la mía, hecha a mano")
	out := mustRun(t, m, 0, "plan", "--scope", "project")
	mustContain(t, "plan", out,
		"claude-code · project\n  ! skill review  ya existe y no la puso pegboard\n  + skill tdd",
		"codex, opencode · project\n  + skill review  (.agents/skills/review → cache@",
		"hermes · project\n  + skill review  (.hermes/skills/review",
		"1 conflicto")
	mustRun(t, m, 1, "apply", "-y", "--scope", "project")
	mustRun(t, m, 0, "apply", "-y", "--scope", "project", "--agent", "codex", "--agent", "opencode")
	if !strings.HasPrefix(linkOf(t, m, "proj/.agents/skills/tdd"), m.Path("home/.cache")) {
		t.Error(".agents/skills debía tener tdd")
	}
	if read(t, m, "proj/.claude/skills/review/SKILL.md") != "la mía, hecha a mano" {
		t.Fatal("no debía tocar la skill ajena")
	}

	// Cambiar a mano un symlink gestionado es drift y conflicto.
	_ = os.Remove(m.Path("proj/.agents/skills/tdd"))
	_ = os.Symlink(m.Path("proj"), m.Path("proj/.agents/skills/tdd"))
	mustContain(t, "drift", mustRun(t, m, 2, "drift", "--scope", "project", "--exit-code"), "skill tdd  editado a mano")
	mustContain(t, "conflicto", mustRun(t, m, 0, "plan", "--scope", "project", "--agent", "codex"),
		"! skill tdd  cambiada a mano desde el último apply")
	mustRun(t, m, 0, "apply", "-y", "--force", "--scope", "project", "--agent", "codex")
	mustRun(t, m, 0, "drift", "--scope", "project", "--exit-code")
}

func TestAgentsMD(t *testing.T) {
	m := fleet(t)
	m.File("proj/CLAUDE.md", "# Mi proyecto\nUsa tabs.")
	m.File("proj/pegboard.yaml", "version: 1\ncontext:\n  agents_md: true\n")
	out := mustRun(t, m, 0, "apply", "-y", "--scope", "project")
	mustContain(t, "apply", out, "proyecto · project\n  + context AGENTS.md", "claude-code · project\n  + context @AGENTS.md en CLAUDE.md")
	if c := read(t, m, "proj/CLAUDE.md"); c != "# Mi proyecto\nUsa tabs.\n\n@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md: %q", c)
	}
	mustContain(t, "AGENTS.md", read(t, m, "proj/AGENTS.md"), "# AGENTS.md")
	mustContain(t, "idempotente", mustRun(t, m, 0, "plan", "--scope", "project"), "Sin cambios")

	m.File("proj/AGENTS.md", "# lo escribí yo")
	m.File("proj/pegboard.yaml", "version: 1\n")
	out = mustRun(t, m, 0, "apply", "-y", "--scope", "project")
	mustContain(t, "quitar puente", out, "- context @AGENTS.md en CLAUDE.md")
	if c := read(t, m, "proj/CLAUDE.md"); c != "# Mi proyecto\nUsa tabs.\n" {
		t.Fatalf("CLAUDE.md tras quitar: %q", c)
	}
	if read(t, m, "proj/AGENTS.md") != "# lo escribí yo" {
		t.Fatal("AGENTS.md nunca se borra")
	}

	// Un puente escrito por el usuario no es de pegboard: no se quita.
	m.File("proj/CLAUDE.md", "@AGENTS.md\n")
	m.File("proj/pegboard.yaml", "version: 1\ncontext:\n  agents_md: true\n")
	mustContain(t, "ya estaba", mustRun(t, m, 0, "apply", "-y", "--scope", "project"), "Sin cambios")
	m.File("proj/pegboard.yaml", "version: 1\n")
	mustContain(t, "del usuario", mustRun(t, m, 0, "plan", "--scope", "project"), "Sin cambios")
}

func TestSkillsAdhocAndAdopt(t *testing.T) {
	commit := gitHub(t)
	commit("acme/skills", acmeV1)
	m := fleet(t)
	m.Mkdir("proj/.git")
	out := mustRun(t, m, 0, "install", "skill", "acme/skills", "mcp", "fs=npx fs", "--agent", "claude-code", "-y")
	mustContain(t, "install", out, "+ skill review", "+ skill tdd", "[ad-hoc]", "+ mcp fs")
	linkOf(t, m, "proj/.claude/skills/tdd")
	if agentExists(m.Path("proj/pegboard.lock")) {
		t.Error("lo ad-hoc no escribe lock")
	}
	mustContain(t, "uninstall", mustRun(t, m, 0, "uninstall", "skill", "review", "-y"), "- skill review")
	if agentExists(m.Path("proj/.claude/skills/review")) {
		t.Fatal("uninstall no quitó review")
	}
	out = mustRun(t, m, 0, "adopt")
	mustContain(t, "adopt", out, "Adoptado a pegboard.yaml:", "+ mcp fs (claude-code)", "+ skill acme/skills")
	mustContain(t, "yaml", read(t, m, "proj/pegboard.yaml"), "skills:\n  - acme/skills\n", "fs:\n    command: npx")
	st := read(t, m, "proj/.pegboard/state.json")
	if strings.Contains(st, `"adhoc"`) {
		t.Errorf("tras adopt nada queda ad-hoc:\n%s", st)
	}
	// Ya declarada, la skill review vuelve en el siguiente apply.
	mustContain(t, "plan tras adopt", mustRun(t, m, 0, "plan", "--scope", "project", "--agent", "claude-code"), "+ skill review")
	mustContain(t, "uninstall declarado", mustRun(t, m, 1, "uninstall", "skill", "tdd"), "pegboard remove skill tdd")
}

func TestSkillInstallBuiltin(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 0, "skill", "install", "-y", "--agent", "claude-code", "--agent", "codex")
	mustContain(t, "skill install", out, "claude-code · global\n  + skill pegboard", "codex · global\n  + skill pegboard")
	mustContain(t, "SKILL.md", read(t, m, "home/.claude/skills/pegboard/SKILL.md"), "name: pegboard", "pegboard plan --json")
	if agentExists(m.Path("proj/.claude/skills/pegboard")) {
		t.Error("skill install va al alcance global por defecto")
	}
}

func TestAddRemove(t *testing.T) {
	m := fleet(t)
	m.File("proj/pegboard.yaml", "version: 1\n# mis mcps\nmcps: {}\n")
	out := mustRun(t, m, 0, "add", "mcp", "gh=npx srv", "--env", "T=${env:T}", "skill", "acme/skills@tdd", "--json")
	var r editResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(r.Added) != 2 || r.File != m.Path("proj/pegboard.yaml") {
		t.Errorf("%+v", r)
	}
	y := read(t, m, "proj/pegboard.yaml")
	mustContain(t, "yaml", y, "# mis mcps", "gh:\n    command: npx", "T: ${env:T}", "skills:\n  - acme/skills@tdd")
	mustContain(t, "add repetido", mustRun(t, m, 0, "add", "skill", "acme/skills@tdd"), "Revisa con `pegboard plan`")
	if strings.Count(read(t, m, "proj/pegboard.yaml"), "acme/skills@tdd") != 1 {
		t.Error("no debía duplicar la skill")
	}
	mustContain(t, "add inválido", mustRun(t, m, 1, "add", "skill", "nada"), "no reconocida")
	out = mustRun(t, m, 0, "remove", "mcp", "gh", "skill", "tdd")
	mustContain(t, "remove", out, "- mcp gh", "- skill acme/skills@tdd")
	if y := read(t, m, "proj/pegboard.yaml"); strings.Contains(y, "gh:") || strings.Contains(y, "acme") {
		t.Errorf("remove:\n%s", y)
	}
	mustContain(t, "remove inexistente", mustRun(t, m, 1, "remove", "mcp", "nope"), "no declara: mcp nope")
}
