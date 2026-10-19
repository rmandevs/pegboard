package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rmandevs/agents-casc/internal/agent/agenttest"
)

const foreignClaude = `{
  "numStartups": 7,
  "mcpServers": {
    "mio": {
      "command": "uvx",
      "args": ["mio"]
    }
  },
  "projects": {}
}
`

const foreignCodex = `# config de Codex
model = "o4"

[mcp_servers.mio]
command = "uvx"
`

// fleet es una máquina con los cuatro agentes, configs ajenas y un perfil
// de Hermes.
func fleet(t *testing.T) *agenttest.Machine {
	m := agenttest.New(t)
	for _, b := range []string{"claude", "codex", "opencode", "hermes"} {
		m.Bins[b] = "1.0.0"
	}
	m.File("home/.claude.json", foreignClaude)
	m.File("home/.codex/config.toml", foreignCodex)
	m.Mkdir("home/.config/opencode")
	m.File("home/.hermes/config.yaml", "# hermes\nmodel: x\n")
	m.Mkdir("home/.hermes/profiles/work")
	m.Vars["GITHUB_TOKEN"] = "ghp_supersecreto"
	return m
}

const globalStack = `version: 1
mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
  notion:
    url: https://mcp.notion.com/mcp
    agents: [claude-code, hermes:work]
`

func read(t *testing.T, m *agenttest.Machine, rel string) string {
	t.Helper()
	b, err := os.ReadFile(m.Path(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: falta %q en:\n%s", what, w, got)
		}
	}
}

func mustRun(t *testing.T, m *agenttest.Machine, want int, args ...string) string {
	t.Helper()
	out, errOut, code := run(m, args...)
	if code != want {
		t.Fatalf("%v: código %d (esperaba %d)\nstdout:\n%s\nstderr:\n%s", args, code, want, out, errOut)
	}
	return out + errOut
}

func TestDeclarativeCycle(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)

	out := mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "plan", out,
		"claude-code · global\n  + mcp github  (~/.claude.json)\n  + mcp notion  (~/.claude.json)",
		"codex · global\n  + mcp github  (~/.codex/config.toml)",
		"hermes:work · global",
		"⚠ secreto en texto plano en /env/GITHUB_TOKEN",
		"Plan: 7 crear, 0 modificar, 0 quitar, 0 conflictos.")
	if strings.Contains(out, "codex · global\n  + mcp notion") {
		t.Error("notion no debía ir a codex")
	}
	if strings.Contains(out, "ghp_supersecreto") {
		t.Error("el plan no debe mostrar secretos")
	}

	// Sin -y pregunta; "n" cancela sin escribir.
	stdin = "n\n"
	out = mustRun(t, m, 0, "apply")
	stdin = ""
	mustContain(t, "cancelar", out, "¿Aplicar? [s/N]", "Cancelado")
	if read(t, m, "home/.claude.json") != foreignClaude {
		t.Fatal("cancelar no debía escribir")
	}

	out = mustRun(t, m, 0, "apply", "-y")
	mustContain(t, "apply", out, "Escrito ~/.claude.json (respaldo: ~/.local/state/pegboard/backups/20261019T120000Z/")

	claude := read(t, m, "home/.claude.json")
	mustContain(t, ".claude.json", claude, `"numStartups": 7`, `"mio": {`, `"GITHUB_TOKEN": "${GITHUB_TOKEN}"`,
		`"notion": {`, `"type": "http"`)
	codex := read(t, m, "home/.codex/config.toml")
	mustContain(t, "config.toml", codex, "# config de Codex", "[mcp_servers.mio]",
		"[mcp_servers.github]\ncommand = \"npx\"\nargs = [\"-y\", \"@modelcontextprotocol/server-github\"]\nenv_vars = [\"GITHUB_TOKEN\"]\n")
	mustContain(t, "opencode.json", read(t, m, "home/.config/opencode/opencode.json"),
		`"$schema": "https://opencode.ai/config.json"`, `"type": "local"`, `"GITHUB_TOKEN": "{env:GITHUB_TOKEN}"`)
	mustContain(t, "hermes", read(t, m, "home/.hermes/config.yaml"), "# hermes", "GITHUB_TOKEN: ghp_supersecreto")
	mustContain(t, "hermes:work", read(t, m, "home/.hermes/profiles/work/config.yaml"), "notion:", "url: https://mcp.notion.com/mcp")

	st := read(t, m, "home/.local/state/pegboard/state.json")
	if strings.Contains(st, "ghp_supersecreto") {
		t.Fatal("el estado no debe contener secretos")
	}
	mustContain(t, "estado", st, `"origin": "declared"`, `"/env/GITHUB_TOKEN"`)
	if b := read(t, m, "home/.local/state/pegboard/backups/20261019T120000Z/"+
		strings.TrimLeft(strings.NewReplacer(string(filepath.Separator), "%", ":", "%").Replace(m.Path("home/.claude.json")), "%")); b != foreignClaude {
		t.Errorf("el respaldo debía ser el original:\n%s", b)
	}

	// Idempotente.
	out = mustRun(t, m, 0, "plan", "--exit-code")
	mustContain(t, "segundo plan", out, "Sin cambios")
	mustRun(t, m, 0, "drift", "--exit-code")

	// Rotar el secreto reescribe solo donde está en claro.
	m.Vars["GITHUB_TOKEN"] = "ghp_nuevo"
	out = mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "rotación", out, "hermes · global\n  ~ mcp github", "hermes:work · global\n  ~ mcp github", "Plan: 0 crear, 2 modificar")
	m.Vars["GITHUB_TOKEN"] = "ghp_supersecreto"

	// Cambiar el YAML modifica y quita.
	m.File("home/.config/pegboard/pegboard.yaml", `version: 1
mcps:
  github:
    command: npx
    args: ["@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
`)
	out = mustRun(t, m, 2, "plan", "--exit-code")
	mustContain(t, "cambio", out, "~ mcp github", "- mcp notion", "Plan: 0 crear, 5 modificar, 2 quitar")
	mustRun(t, m, 0, "apply", "-y")
	if c := read(t, m, "home/.claude.json"); strings.Contains(c, "notion") || !strings.Contains(c, `"mio"`) {
		t.Fatalf("quitar notion debía respetar lo ajeno:\n%s", c)
	}
}

func TestConflictsAndDrift(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	mustRun(t, m, 0, "apply", "-y", "--agent", "claude-code")

	// Edición a mano: conflicto en plan, apply se detiene, drift lo ve.
	c := read(t, m, "home/.claude.json")
	m.File("home/.claude.json", strings.Replace(c, "server-github", "server-gitlab", 1))
	out := mustRun(t, m, 2, "plan", "--exit-code", "--agent", "claude-code")
	mustContain(t, "conflicto", out, "! mcp github  editado a mano desde el último apply", "1 conflicto")
	out = mustRun(t, m, 1, "apply", "-y", "--agent", "claude-code")
	mustContain(t, "apply bloqueado", out, "hay conflictos")
	out = mustRun(t, m, 2, "drift", "--exit-code")
	mustContain(t, "drift", out, "claude-code · global  mcp github  editado a mano", "Drift: 1 entrada.")

	out = mustRun(t, m, 0, "apply", "-y", "--force", "--agent", "claude-code")
	mustContain(t, "force", out, "(--force: se sobrescribe)")
	mustRun(t, m, 0, "drift", "--exit-code")

	// Borrado a mano: se vuelve a crear.
	m.File("home/.claude.json", foreignClaude)
	out = mustRun(t, m, 2, "drift", "--exit-code")
	mustContain(t, "borrado", out, "borrado a mano")
	out = mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "recrear", out, "+ mcp github  borrado a mano; se vuelve a crear")
}

func TestForeignSameName(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nmcps:\n  mio:\n    command: npx\n")
	out := mustRun(t, m, 0, "plan", "--agent", "claude-code")
	mustContain(t, "ajeno", out, "! mcp mio  ya existe y no lo creó pegboard")

	// Si ya está igual, se adopta sin escribir.
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nmcps:\n  mio:\n    command: uvx\n    args: [mio]\n")
	out = mustRun(t, m, 0, "apply", "-y", "--agent", "claude-code")
	mustContain(t, "adoptar", out, "= mcp mio  ya estaba así; pegboard lo registra", "1 adoptar")
	if read(t, m, "home/.claude.json") != foreignClaude {
		t.Fatal("adoptar no debía reescribir el archivo")
	}
	mustContain(t, "segundo plan", mustRun(t, m, 0, "plan", "--agent", "claude-code"), "Sin cambios")
}

func TestCodexInlineIsConflict(t *testing.T) {
	m := fleet(t)
	m.File("home/.codex/config.toml", "[mcp_servers]\ngithub = { command = \"x\" }\n")
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	out := mustRun(t, m, 0, "plan", "--agent", "codex", "--force")
	mustContain(t, "inline", out, "! mcp github", "no está escrito como tabla")
}

func TestMissingSecret(t *testing.T) {
	m := fleet(t)
	delete(m.Vars, "GITHUB_TOKEN")
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	// Claude Code, Codex y opencode lo dejan como referencia: no hace falta.
	mustRun(t, m, 0, "plan", "--agent", "claude-code", "--agent", "codex", "--agent", "opencode")
	out := mustRun(t, m, 1, "plan", "--agent", "hermes")
	mustContain(t, "falta", out, "la variable GITHUB_TOKEN no está definida")
}

func TestPolicyAndPhases(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack+"policy:\n  allow_mcps: [github]\nskills: [a/b]\n")
	out := mustRun(t, m, 1, "plan")
	mustContain(t, "policy", out, "policy.allow_mcps no permite: notion")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nskills: [a/b]\n")
	out = mustRun(t, m, 0, "plan")
	mustContain(t, "fase 2", out, "aviso:", "skills llegan en la fase 2")
	m.File("home/.config/pegboard/pegboard.yaml", "version: 1\nmcps:\n  x:\n    command: \"${op://v/i}\"\n")
	mustContain(t, "fase 3", mustRun(t, m, 1, "plan"), "fase 3")
}

func TestPlanJSON(t *testing.T) {
	m := fleet(t)
	m.File("home/.config/pegboard/pegboard.yaml", globalStack)
	out := mustRun(t, m, 0, "plan", "--json", "--agent", "hermes")
	var p struct {
		Changes []struct {
			Target, Name, Action, File string
			Warnings                   []string
		}
		Summary map[string]int
		Blocked bool
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if p.Summary["create"] != 3 || p.Blocked || len(p.Changes) != 3 {
		t.Fatalf("%+v", p)
	}
	if p.Changes[0].Target != "hermes" || p.Changes[0].Action != "create" || len(p.Changes[0].Warnings) != 1 {
		t.Errorf("%+v", p.Changes[0])
	}
	if _, _, code := run(m, "apply", "--json"); code != 1 {
		t.Error("apply --json sin -y debía fallar")
	}
	out = mustRun(t, m, 0, "apply", "--json", "-y", "--agent", "hermes")
	mustContain(t, "apply json", out, `"applied": true`, `"written": [`)
}

func TestNoStack(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 1, "plan")
	mustContain(t, "sin stack", out, "pegboard init")
}

func TestInit(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 0, "init")
	mustContain(t, "init", out, "Creado pegboard.yaml (agentes: claude-code, codex, opencode)")
	mustContain(t, "contenido", read(t, m, "proj/pegboard.yaml"), "version: 1\nagents: [claude-code, codex, opencode]\n", "mcps: {}")
	mustRun(t, m, 1, "init")
	out = mustRun(t, m, 0, "plan")
	mustContain(t, "plan vacío", out, "Sin cambios")
	mustContain(t, "init global", mustRun(t, m, 0, "init", "--scope", "global"), "hermes")
}

func TestAdhoc(t *testing.T) {
	m := fleet(t)
	m.Mkdir("proj/.git") // regla 5: hay repo → alcance project
	out := mustRun(t, m, 0, "install", "mcp", "fs=npx -y '@mcp/fs' /tmp", "mcp", "docs=https://docs.example/mcp",
		"--header", "X-Key=${env:DOCS_KEY}", "--agent", "claude-code", "--agent", "opencode", "-y")
	mustContain(t, "install", out, "claude-code · project\n  + mcp docs", "[ad-hoc]", "Escrito .mcp.json", "Escrito opencode.json")
	mustContain(t, ".mcp.json", read(t, m, "proj/.mcp.json"), `"@mcp/fs"`, `"X-Key": "${DOCS_KEY}"`)
	mustContain(t, "opencode", read(t, m, "proj/opencode.json"), `"command": [`, `"X-Key": "{env:DOCS_KEY}"`)
	mustContain(t, "gitignore", read(t, m, "proj/.pegboard/.gitignore"), "*")
	mustContain(t, "estado", read(t, m, "proj/.pegboard/state.json"), `"origin": "adhoc"`)
	if agentExists(m.Path("proj/pegboard.yaml")) {
		t.Fatal("install sin --save no debe crear pegboard.yaml")
	}

	out = mustRun(t, m, 0, "drift", "--scope", "project")
	mustContain(t, "drift ad-hoc", out, "Ad-hoc, no declarado:", "Sin drift.")
	mustRun(t, m, 2, "drift", "--scope", "project", "--strict", "--exit-code")

	// Un pegboard.yaml de proyecto no borra lo ad-hoc, salvo --prune-adhoc.
	m.File("proj/pegboard.yaml", "version: 1\n")
	mustContain(t, "apply no poda", mustRun(t, m, 0, "plan", "--scope", "project"), "Sin cambios")
	mustContain(t, "prune", mustRun(t, m, 0, "plan", "--scope", "project", "--prune-adhoc"), "- mcp fs", "Plan: 0 crear, 0 modificar, 4 quitar")

	out = mustRun(t, m, 0, "uninstall", "mcp", "fs", "-y")
	mustContain(t, "uninstall", out, "- mcp fs")
	if strings.Contains(read(t, m, "proj/.mcp.json"), "@mcp/fs") {
		t.Fatal("uninstall no quitó fs")
	}
	mustContain(t, "uninstall otra vez", mustRun(t, m, 1, "uninstall", "mcp", "fs", "-y"), "pegboard no instaló")
}

func TestInstallSave(t *testing.T) {
	m := fleet(t)
	out := mustRun(t, m, 0, "install", "mcp", "gh=npx srv", "--env", "TOKEN=${env:GITHUB_TOKEN}",
		"--scope", "global", "--agent", "codex", "--save", "-y")
	mustContain(t, "save", out, "Guardado en ~/.config/pegboard/pegboard.yaml")
	mustContain(t, "yaml", read(t, m, "home/.config/pegboard/pegboard.yaml"),
		"version: 1", "gh:\n    command: npx\n    args:\n      - srv\n    env:\n      TOKEN: ${env:GITHUB_TOKEN}\n    agents:\n      - codex")
	// TOKEN≠GITHUB_TOKEN: Codex no puede reenviarla, queda en claro.
	mustContain(t, "codex", read(t, m, "home/.codex/config.toml"), `env = { TOKEN = "ghp_supersecreto" }`)
	mustContain(t, "declarado", read(t, m, "home/.local/state/pegboard/state.json"), `"origin": "declared"`)
	mustContain(t, "plan tras save", mustRun(t, m, 0, "plan", "--scope", "global"), "Sin cambios")
	mustContain(t, "uninstall declarado", mustRun(t, m, 1, "uninstall", "mcp", "gh", "--scope", "global"), "está declarado")
}

func TestInstallParse(t *testing.T) {
	m := fleet(t)
	for args, want := range map[string]string{
		"install skill mattpocock/tdd":      "fase 2",
		"install github.com/x/stacks//web":  "fase 3",
		"install mcp":                       "nombre=comando",
		"install mcp a=x --header H=1":      "--header solo aplica",
		"install mcp a=https://x --env K=1": "--env solo aplica",
		"install mcp a=\"x":                 "comillas",
		"install mcp a@b=x":                 "inválido",
		"install mcp a=x mcp a=y":           "repetido",
	} {
		_, errOut, code := run(m, strings.Fields(args)...)
		if code != 1 || !strings.Contains(errOut, want) {
			t.Errorf("%s: code=%d err=%q (esperaba %q)", args, code, errOut, want)
		}
	}
	words, err := splitWords(`npx -y "a b" 'c d' e\ f`)
	if err != nil || strings.Join(words, "|") != "npx|-y|a b|c d|e f" {
		t.Errorf("splitWords: %q %v", words, err)
	}
}

func TestImport(t *testing.T) {
	m := fleet(t)
	m.File("home/.claude.json", `{
  "mcpServers": {
    "linear": {"type": "http", "url": "https://mcp.linear.app/mcp", "headers": {"Authorization": "Bearer lin_secreto"}},
    "fs": {"type": "stdio", "command": "npx", "args": ["fs"], "env": {"API_KEY": "k_secreto"}}
  }
}
`)
	m.File("home/.codex/config.toml", "[mcp_servers.fs]\ncommand = \"npx\"\nargs = [\"fs\"]\nenv = { API_KEY = \"k_secreto\" }\n")
	out := mustRun(t, m, 0, "import", "--scope", "global")
	mustContain(t, "import", out, "Importado a ~/.config/pegboard/pegboard.yaml:",
		"+ mcp fs (claude-code, codex)", "+ mcp linear (claude-code)", "exporta: API_KEY, LINEAR_TOKEN")
	y := read(t, m, "home/.config/pegboard/pegboard.yaml")
	mustContain(t, "yaml", y, "API_KEY: ${env:API_KEY}", "Authorization: Bearer ${env:LINEAR_TOKEN}", "agents:\n      - claude-code")
	if strings.Contains(y, "k_secreto") || strings.Contains(y, "lin_secreto") {
		t.Fatalf("import no debe copiar secretos al YAML:\n%s", y)
	}
	if st := read(t, m, "home/.local/state/pegboard/state.json"); strings.Contains(st, "k_secreto") || strings.Contains(st, "lin_secreto") {
		t.Fatal("ni al estado")
	}
	// Lo importado ya es de pegboard: el plan lo pasa a referencias nativas.
	m.Vars["API_KEY"] = "k_secreto"
	out = mustRun(t, m, 0, "plan", "--scope", "global", "--agent", "claude-code", "--agent", "codex")
	mustContain(t, "plan tras import", out, "claude-code · global\n  ~ mcp fs", "~ mcp linear", "codex · global\n  ~ mcp fs")
	if strings.Contains(out, "!") {
		t.Errorf("lo importado no debía dar conflicto:\n%s", out)
	}
	mustContain(t, "import otra vez", mustRun(t, m, 0, "import", "--scope", "global"), "No hay MCPs nuevos")
}

func agentExists(p string) bool { _, err := os.Stat(p); return err == nil }
