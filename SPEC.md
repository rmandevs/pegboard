# Pegboard — SPEC

Versión: 0.1 · Estado: fase 0 implementada (esqueleto + `pegboard agents`)
Origen: §4.1–4.2 de `tapioca-core/SPEC.md` (`stack.yaml` + `apply`), separado
como producto propio. Producto y binario: **Pegboard** / `pegboard`
(`peg` ya existe en Homebrew; quien quiera puede usar `alias pb=pegboard`).

---

## 1. Qué es

**Configuration as Code para agentes.** Declaras en YAML qué MCPs, skills y
contexto quieres y en qué agentes (Claude Code, Codex, opencode, Hermes…), y
`pegboard` deja cada agente configurado con sus archivos nativos. Piensa en
Ansible (roles redistribuibles, idempotencia) con la disciplina de Terraform
(`plan`, `apply`, estado, drift, lock).

El problema que resuelve: hoy cada agente guarda MCPs y skills en un lugar y
formato distinto. Sincronizarlos es traducir a mano o pedirle a un agente que
lo haga a ciegas, gastando tokens y con riesgo de que quede mal. `pegboard` hace
esa traducción con código determinista y probado, y le da a los propios
agentes una skill para usarlo bien (§7).

### El nombre

Un *pegboard* es el tablero perforado del taller donde cuelgas las
herramientas. En su variante *shadow board* cada herramienta tiene su silueta
pintada: cada cosa tiene su lugar exacto y de un vistazo ves qué falta o qué
está fuera de lugar. Es el producto entero:

| Taller | Pegboard |
|---|---|
| Las herramientas | MCPs (literalmente, las tools del agente) y skills |
| Las siluetas pintadas | `pegboard.yaml`: el estado deseado |
| Ver qué silueta está vacía | `pegboard drift` |
| Regresar cada herramienta a su silueta | `pegboard apply` |

**Icono:** un tablero con agujeritos, una herramienta colgada y una silueta
vacía al lado. Debe leerse a 16 px.

### Por qué no otra cosa

| Alternativa | Qué le falta |
|---|---|
| rulesync, ruler, vsync, sync-agents-settings | Son generadores/copiadores: traducen una fuente, pero sin `plan`, sin estado de lo que crearon, sin lock y sin stacks redistribuibles |
| add-mcp, `npx skills` | Instaladores puntuales de una sola cosa; no hay estado deseado |
| Ansible / Chef con `template` | Sirven de transporte, pero tendrías que escribir a mano el formato y las rutas de cada agente, sin mezclar con lo existente ni detectar drift |
| Pedírselo a un agente | No determinista, gasta tokens, no deja registro ni se puede repetir |

### Principios

1. **Lo determinista es código, no LLM.** Traducir configs es una función con
   tests por agente. Un agente puede invocar `pegboard`, nunca improvisar su
   trabajo.
2. **Nunca pisar lo ajeno.** `pegboard` solo toca las llaves, archivos y links que
   él creó (registrados en el estado). Lo que el usuario escribió a mano se
   respeta.
3. **Primero mostrar, luego escribir.** Todo cambio pasa por un `plan` legible.
4. **Idempotente.** `apply` dos veces = mismo resultado, cero cambios la
   segunda vez.
5. **Los secretos nunca viven en el YAML.** Solo referencias.
6. **La verdad vive en archivos legibles** y versionables en git.
7. **Pull, no push.** `pegboard` configura la máquina donde corre. Llevarlo a
   otras máquinas es trabajo de git, CI, Ansible o un MDM (§8).

### No-objetivos (por ahora)

- Empujar configs a máquinas remotas (SSH, agentes de flota).
- Instalar los harnesses en sí (solo se configuran los que ya están).
- Gestionar modelos, API keys de proveedores o permisos de los harnesses.
- Ser un registro/marketplace de MCPs o skills.

---

## 2. Conceptos

| Concepto | Qué es | Análogo |
|---|---|---|
| **Stack** | Un `pegboard.yaml`: el estado deseado de un alcance | playbook / módulo raíz |
| **Alcance** (scope) | `global` (tu usuario) o `project` (una carpeta) | inventario |
| **Target** | Un agente instalado en un alcance (p. ej. Claude Code × proyecto) | host |
| **Provider** | Adaptador de un agente: sabe leer, mezclar y escribir sus archivos | módulo / provider |
| **Recurso** | Un MCP, una skill o una regla de contexto | recurso / tarea |
| **Stack remoto** | Un stack publicado (repo git) que otro incluye con `include` | rol de Galaxy / módulo remoto |
| **Estado** | Registro de qué escribió `pegboard` en cada target | `terraform.tfstate` |
| **Lock** | Versiones y hashes exactos de lo resuelto | `.terraform.lock.hcl` |

---

## 3. Formato

### 3.1 `pegboard.yaml`

Uno global (`~/.config/pegboard/pegboard.yaml`) y uno por proyecto (`./pegboard.yaml`).
Validado con JSON Schema publicado (autocompletado en editores).

```yaml
version: 1
agents: [claude-code, codex, opencode, hermes]   # targets; default: los detectados
inherit: global                                  # global | none (solo en proyecto)

include:                                         # stacks remotos (fase 3)
  - github.com/rmandevs/stacks//web?ref=v1

mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
  notion:
    url: https://mcp.notion.com/mcp
    agents: [claude-code]                        # restringir a ciertos agentes

skills:
  - mattpocock/tdd                               # de un repo/registro
  - github.com/anthropics/skills//pdf?ref=main
  - ./skills/mi-skill                            # local

context:
  agents_md: true                                # garantiza AGENTS.md y el puente en CLAUDE.md

remove:                                          # quitar algo heredado o incluido
  mcps: [notion]
```

Reglas de mezcla: `global` → `include` (en orden) → proyecto. Lo posterior
gana por llave; `remove` se aplica al final.

### 3.2 Secretos

Valores con `${…}` se resuelven al aplicar y **nunca** se escriben en el
estado ni en el lock:

- `${env:NOMBRE}` — variable de entorno.
- `${file:~/.secrets/x}` — contenido de un archivo.
- `${op://vault/item/campo}` — 1Password CLI (fase 3).
- `${keychain:servicio}` — llavero del sistema (fase 3).

Si el agente destino admite referencias a env de forma nativa, el provider las
deja como referencia en lugar de escribir el valor en claro. Si no, avisa en el
`plan` que el secreto quedará en texto plano en ese archivo.

### 3.3 `pegboard.lock`

Junto a cada `pegboard.yaml`. Fija la versión exacta (commit, tag, hash del
contenido) de cada skill, de cada stack incluido y, cuando aplica, del paquete
del MCP (`npx …@x.y.z`). Se versiona en git. `pegboard apply` usa el lock;
`pegboard update` lo refresca.

### 3.4 Estado

`~/.local/state/pegboard/state.json` (global) y `.pegboard/state.json` (proyecto, en
`.gitignore`). Por target registra qué llaves, archivos y symlinks creó `pegboard`
y su hash. Es lo que permite el principio 2 y detectar drift.

---

## 4. CLI

```
pegboard init                     # crea pegboard.yaml (detecta agentes instalados)
pegboard import [--agent X]       # adopta la config existente de los agentes al pegboard.yaml
pegboard plan                     # diff de lo que cambiaría; no escribe nada
pegboard apply [-y]               # aplica el plan (pide confirmación sin -y)
pegboard drift [--exit-code]      # compara estado vs. archivos reales (para CI)
pegboard update                   # re-resuelve versiones y actualiza pegboard.lock
pegboard add mcp|skill …          # edita pegboard.yaml de forma segura (para humanos y agentes)
pegboard remove mcp|skill …
pegboard install …                # modo ad-hoc: instala ya, sin pegboard.yaml (§4.1)
pegboard uninstall …              # quita algo instalado ad-hoc
pegboard agents                   # agentes detectados, rutas y versión de provider
pegboard skill install            # instala la skill de pegboard en los agentes (§7)
```

Banderas comunes: `--scope global|project`, `--agent <id>` (repetible),
`--json` (salida estable para agentes y scripts), `--dir <ruta>`.

`plan` muestra, por target, cada recurso como `+` crear, `~` modificar,
`-` quitar o `!` conflicto (alguien editó a mano algo que `pegboard` maneja). Un
conflicto detiene el `apply` salvo `--force` o resolución explícita.

Códigos de salida: `0` sin cambios, `2` hay cambios/drift (con
`--exit-code`), `1` error.

### 4.1 Modo ad-hoc

Como los comandos ad-hoc de Ansible frente a los playbooks: para "necesito
esta skill y estos MCPs ya, en mis agentes", sin escribir YAML. Es también la
puerta de entrada: el primer contacto con Pegboard es una línea, no un
archivo.

```
pegboard install skill mattpocock/tdd \
                 mcp github='npx -y @modelcontextprotocol/server-github' \
                 mcp notion=https://mcp.notion.com/mcp \
                 --env GITHUB_TOKEN='${env:GITHUB_TOKEN}' \
                 --agent claude-code --agent codex      # default: todos los detectados
pegboard install github.com/rmandevs/stacks//web?ref=v1 # un stack remoto, de una vez
pegboard uninstall mcp notion
```

Reglas:

1. **Mismo motor, mismas garantías.** `install` arma un estado deseado
   temporal y pasa por el mismo `plan` → confirmación → `apply` (con `-y`
   para saltar la confirmación). Respeta lo ajeno, hace backup y resuelve
   secretos igual que el modo declarativo.
2. **Queda registrado.** Cada recurso ad-hoc entra al estado con
   `origin: adhoc`. Por eso `uninstall` sabe exactamente qué quitar y de
   dónde.
3. **No toca `pegboard.yaml`**, salvo con `--save`, que además lo agrega al
   stack del alcance. Así un experimento rápido se vuelve declarativo con una
   bandera. `pegboard adopt` promueve después todo lo ad-hoc al YAML.
4. **No pelea con el modo declarativo.** `apply` no borra lo ad-hoc (no es
   suyo); `apply --prune-adhoc` sí. `drift` lo lista aparte como "ad-hoc, no
   declarado", sin marcarlo como error, salvo con `--strict` (útil en CI).
5. **Alcance por defecto:** `project` si hay un `pegboard.yaml` o un repo git
   en la carpeta actual; si no, `global`. Se fuerza con `--scope`.

---

## 5. Providers (agentes)

Cada provider implementa la misma interfaz:

```go
type Provider interface {
    ID() string
    Version() int                               // versión del provider, no del agente
    Detect(env Env, scope Scope) []Target       // ¿está instalado / aplica aquí? (varios: perfiles)
    Read(t Target) (Observed, error)            // config actual, incluida la ajena
    Plan(t Target, desired Desired, st State) (Changes, error)
    Apply(t Target, c Changes) (State, error)   // escritura atómica + backup
}
```

`Env` reúne todo lo que el provider consulta de la máquina (home, carpeta del
proyecto, variables, `PATH`, `--version`), para que los tests corran sobre un
`t.TempDir()`. La fase 0 implementa `ID`, `Version` y `Detect`; `Read`, `Plan`
y `Apply` llegan con la fase 1.

Un agente se da por instalado si su binario está en el `PATH` o existe su
carpeta de configuración de usuario. En alcance `project` aplica siempre que
esté instalado, haya o no archivos en la carpeta todavía.

Requisitos de cada provider: tests de ida y vuelta (leer → escribir → leer da
lo mismo), preservar formato y comentarios cuando el formato lo permita (TOML,
YAML, JSONC) y escribir de forma atómica con respaldo en
`~/.local/state/pegboard/backups/`.

### Día 1

Rutas a verificar al implementar cada provider; esta tabla es punto de
partida, no verdad.

| Agente | Binario · carpeta (variable) | MCPs (global / proyecto) | Skills (global / proyecto) | Contexto |
|---|---|---|---|---|
| Claude Code | `claude` · `~/.claude` (`CLAUDE_CONFIG_DIR`) | `~/.claude.json` / `.mcp.json` | `~/.claude/skills/` / `.claude/skills/` | `CLAUDE.md` con `@AGENTS.md` |
| Codex | `codex` · `~/.codex` (`CODEX_HOME`) | `~/.codex/config.toml` (`[mcp_servers.*]`) / `.codex/config.toml` | `~/.agents/skills/` / `.agents/skills/` | `AGENTS.md` nativo |
| opencode | `opencode` · `~/.config/opencode` (`XDG_CONFIG_HOME`) | `~/.config/opencode/opencode.json` / `opencode.json` (`mcp`) | `~/.config/opencode/skills/` / `.opencode/skills/` | `AGENTS.md` nativo |
| Hermes | `hermes` · `~/.hermes` (`HERMES_HOME`) | `~/.hermes/config.yaml` (`mcp_servers`) | `~/.hermes/skills/`; perfiles en `~/.hermes/profiles/<p>/` | `SOUL.md` |

Por verificar contra cada agente al implementar su `Read`/`Apply` (fase 1):
skills globales de Codex y opencode, `opencode.jsonc` como alternativa a
`opencode.json`, y que `CLAUDE_CONFIG_DIR` mueva también `.claude.json`.

Hermes no tiene alcance de proyecto conocido: solo `global` (y por perfil,
como sub-target `hermes:<perfil>`).

### Después

Cursor, Gemini CLI, Pi, Kiro, Copilot CLI… uno por uno, por demanda. Se
compite en corrección, no en número de agentes.

### Skills: cómo se instalan

Las skills se descargan una vez a un caché (`~/.cache/pegboard/skills/<hash>`) y
se enlazan (symlink; copia en Windows o si el agente no sigue symlinks) en la
carpeta de cada agente. Así una skill existe una sola vez en disco y el lock
garantiza que es la misma en todos lados.

---

## 6. Ejemplo de `plan`

```
$ pegboard plan
claude-code · project
  + mcp github           (.mcp.json)
  + skill mattpocock/tdd (.claude/skills/tdd → cache@a1b2c3)
codex · project
  + mcp github           (.codex/config.toml)
  ! mcp notion           editado a mano desde el último apply
hermes · global
  ~ mcp github           args cambiaron
Plan: 3 crear, 1 modificar, 0 quitar, 1 conflicto.
```

---

## 7. Skill para agentes

`pegboard` trae su propia skill (`SKILL.md`), instalable con `pegboard skill install`
(o declarándola en el stack como cualquier otra). Le enseña al agente a:

1. Nunca editar `.mcp.json`, `config.toml`, etc. a mano. Para pedidos rápidos
   ("instálame X"), `pegboard install`; si el proyecto ya tiene
   `pegboard.yaml`, `pegboard add` / `pegboard remove` o `install --save`.
2. Siempre correr `pegboard plan --json`, mostrar el resumen al usuario y solo
   después `pegboard apply -y`.
3. Ante un conflicto (`!`), preguntar al usuario en lugar de forzar.
4. Nunca escribir secretos en claro: proponer una referencia `${env:…}`.

Con esto, "agrégame el MCP de Linear en todos mis agentes" cuesta un par de
tool calls deterministas en lugar de que el agente lea y reescriba cuatro
formatos distintos.

Opcional (fase 4): `pegboard mcp`, un servidor MCP con tools `plan`, `apply`,
`add`, `drift` para agentes que prefieran tools a shell.

---

## 8. Equipos (pull)

`pegboard` no empuja nada a otras máquinas. En un equipo:

1. **Stack en el repo del proyecto.** `pegboard.yaml` + `pegboard.lock` versionados;
   cada dev clona y corre `pegboard apply`. Como `package.json` + `npm install`.
2. **Stack de la organización como stack remoto.** El global de cada dev hace
   `include: github.com/empresa/agent-stack?ref=v3`. Cambiar de versión es
   decisión de cada quien, viendo el `plan`.
3. **Políticas como validación.** Un stack puede declarar
   `policy: { allow_mcps: [github, linear] }`; `plan`/`apply` fallan si el
   resultado incluye algo fuera de la lista. En CI, `pegboard drift --exit-code`
   y `pegboard plan --exit-code` revisan que el repo cumpla.
4. **Flotas.** Si una empresa quiere forzarlo en todas las laptops, usa
   Ansible, un MDM o los *managed settings* de cada harness para correr
   `pegboard apply`. `pegboard` es el módulo; ellos, el transporte.

---

## 9. Implementación

- **Lenguaje:** Go. Binario único multiplataforma (Linux, macOS, Windows).
- **Librerías candidatas:** `cobra` (CLI), `gopkg.in/yaml.v3` y
  `pelletier/go-toml/v2` (con preservación de formato donde se pueda),
  `santhosh-tekuri/jsonschema`, Bubble Tea / Lip Gloss (TUI, fase 4).
- **Layout:**
  ```
  cmd/pegboard/        # entrada CLI
  internal/stack/      # parseo, schema, mezcla, includes
  internal/resolve/    # skills/stacks remotos, caché, lock
  internal/secrets/    # resolución de ${…}
  internal/state/      # estado y drift
  internal/plan/       # cálculo y render del plan (texto y JSON)
  providers/<id>/      # un paquete por agente, con testdata/ de configs reales
  skill/SKILL.md       # la skill de §7
  schema/pegboard.schema.json
  ```
- **Tests:** golden files por provider con configs reales (incluidas configs
  "sucias" con cosas ajenas que no deben tocarse). Ningún test escribe fuera
  de un `t.TempDir()`.
- **Distribución:** GitHub Releases con goreleaser, Homebrew tap, `go install`.
- **Tapioca** lo consume como binario: sus tools `apply_stack` y
  `check_drift` se vuelven llamadas a `pegboard --json`.

---

## 10. Fases

Cada fase termina cuando la uso a diario sin frustración.

### Fase 0 — Esqueleto ✅
- Repo Go, CI (lint + tests), `pegboard agents` detectando Claude Code, Codex,
  opencode y Hermes.
- JSON Schema de `pegboard.yaml`.
- **Listo cuando:** `pegboard agents` lista bien lo instalado en mi máquina.

### Fase 1 — MCPs local
- `init`, `plan`, `apply`, `drift` para MCPs en los 4 providers, alcances
  global y proyecto. Estado y backups. Secretos `env:` y `file:`.
- `import` desde cada agente.
- Modo ad-hoc para MCPs: `install` / `uninstall`, `--save`, `origin: adhoc`
  en el estado.
- **Listo cuando:** dejo de editar `.mcp.json` / `config.toml` a mano.

### Fase 2 — Skills y skill propia
- Skills locales y de git con caché, symlinks y `pegboard.lock`; `update`.
- `context.agents_md`.
- `add` / `remove`, `install skill …`, `adopt`, `--json` estable,
  `pegboard skill install`.
- **Listo cuando:** le pido a un agente "agrega X a todos" y lo hace con
  `pegboard` sin equivocarse.

### Fase 3 — Stacks remotos y políticas
- `include` de stacks en git con lock; `policy.allow_mcps`.
- Secretos `op://` y `keychain:`.
- `--exit-code` para CI y un ejemplo de GitHub Action.
- **Listo cuando:** comparto un stack en un repo y otra máquina queda igual
  con un `pegboard apply`.

### Fase 4 — TUI y extras
- TUI (Bubble Tea): ver targets, revisar el plan, aplicar.
- `pegboard mcp` (servidor MCP propio).
- Más providers según uso.

### Después (sin compromiso)
- UI ligera (o la de Tapioca como primer cliente).
- Registro de stacks; firmas/verificación de skills.

---

## 11. Riesgos

- **Los agentes cambian rutas y formatos seguido.** Mitigación: providers
  versionados, `pegboard agents` muestra la versión detectada del harness, y
  golden tests con configs reales que se actualizan al detectar cambios.
- **Preservar comentarios y formato** en JSON/TOML/YAML es difícil; si no se
  puede, el `plan` lo avisa antes de escribir.
- **Symlinks en Windows** y agentes que no los siguen: caer a copia y
  registrarla en el estado.
- **Secretos en texto plano** cuando el agente no admite referencias: avisar
  siempre en el `plan`.
- **Skills de terceros = código de terceros.** El lock fija el contenido
  exacto; `update` muestra el diff antes de aceptar cambios.

## 12. Referencias

- Vecinos: rulesync (https://github.com/dyoshikawa/rulesync), ruler, vsync
  (https://github.com/nicepkg/vsync), sync-agents-settings
  (https://github.com/Leoyang183/sync-agents-settings), add-mcp (Neon),
  `npx skills` (Vercel).
- Hermes MCP: https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp
- Inspiración: Ansible (roles, `--check --diff`), Terraform (plan/state/lock),
  Jenkins CasC (nombre original del repo), shadow boards del 5S (el nombre).
