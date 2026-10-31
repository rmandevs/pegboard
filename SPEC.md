# Pegboard — SPEC

Version: 0.1 · Status: MVP (phases 0–2): MCPs, skills, and AGENTS.md in Claude Code, Codex, opencode, Hermes, Pi, OMP, OpenClaw, Cursor, and Gemini CLI
Origin: §4.1–4.2 of `tapioca-core/SPEC.md` (`stack.yaml` + `apply`), split out
as its own product. Product and binary: **Pegboard** / `pegboard`
(`peg` already exists in Homebrew; anyone who wants can use `alias pb=pegboard`).

---

## 1. What it is

**Configuration as Code for agents.** You declare in YAML which MCPs, skills,
and context you want and in which agents (Claude Code, Codex, opencode,
Hermes, Pi, OMP, OpenClaw, Cursor, Gemini CLI…), and `pegboard` leaves each
agent configured with its native files.
Think Ansible (redistributable roles, idempotency) with Terraform's discipline
(`plan`, `apply`, state, drift, lock).

The problem it solves: today each agent stores MCPs and skills in a different
place and format. Syncing them means translating by hand or asking an agent to
do it blind, burning tokens and risking mistakes. `pegboard` does that
translation with deterministic, tested code, and gives the agents themselves a
skill to use it well (§7).

### The name

A *pegboard* is the perforated workshop board where you hang your tools. In
its *shadow board* variant, every tool has a painted silhouette: everything
has its exact place, and at a glance you see what is missing or out of place.
That is the whole product:

| Workshop | Pegboard |
|---|---|
| The tools | MCPs (literally, the agent's tools) and skills |
| The painted silhouettes | `pegboard.yaml`: the desired state |
| Seeing which silhouette is empty | `pegboard drift` |
| Returning each tool to its silhouette | `pegboard apply` |

**Icon:** a board with little holes, one hanging tool, and an empty
silhouette next to it. It must read at 16 px.

### Why not something else

| Alternative | What it lacks |
|---|---|
| rulesync, ruler, vsync, sync-agents-settings | They are generators/copiers: they translate a source, but without `plan`, without state of what they created, without a lock, and without redistributable stacks |
| add-mcp, `npx skills` | One-off installers for a single thing; there is no desired state |
| Ansible / Chef with `template` | They work as transport, but you would have to write each agent's format and paths by hand, without merging with existing content or detecting drift |
| Asking an agent to do it | Non-deterministic, burns tokens, leaves no record, and cannot be repeated |

### Principles

1. **Deterministic work is code, not an LLM.** Translating configs is a
   function with tests per agent. An agent may invoke `pegboard`, never
   improvise its work.
2. **Never overwrite what is not yours.** `pegboard` only touches the keys,
   files, and links it created (recorded in state). What the user wrote by
   hand is respected.
3. **Show first, write later.** Every change goes through a readable `plan`.
4. **Idempotent.** `apply` twice = same result, zero changes the second time.
5. **Secrets never live in the YAML.** References only.
6. **The truth lives in readable, git-versionable files.**
7. **Pull, not push.** `pegboard` configures the machine where it runs. Moving
   it to other machines is the job of git, CI, Ansible, or an MDM (§8).

### Non-goals (for now)

- Pushing configs to remote machines (SSH, fleet agents).
- Installing the harnesses themselves (only those already installed are
  configured).
- Managing models, provider API keys, or harness permissions.
- Being a registry/marketplace of MCPs or skills.

---

## 2. Concepts

| Concept | What it is | Analogue |
|---|---|---|
| **Stack** | A `pegboard.yaml`: the desired state of a scope | playbook / root module |
| **Scope** | `global` (your user) or `project` (a directory) | inventory |
| **Target** | An agent installed in a scope (for example, Claude Code × project) | host |
| **Provider** | An agent adapter: it knows how to read, merge, and write its files | module / provider |
| **Resource** | An MCP, a skill, or a context rule | resource / task |
| **Remote stack** | A published stack (git repo) that another stack includes with `include` | Galaxy role / remote module |
| **State** | Record of what `pegboard` wrote to each target | `terraform.tfstate` |
| **Lock** | Exact versions and hashes of what was resolved | `.terraform.lock.hcl` |

---

## 3. Format

### 3.1 `pegboard.yaml`

One global (`~/.config/pegboard/pegboard.yaml`) and one per project
(`./pegboard.yaml`). Validated with a published JSON Schema (editor
autocompletion).

```yaml
version: 1
agents: [claude-code, codex, opencode, hermes]   # targets; default: detected ones
inherit: global                                  # global | none (project only)

include:                                         # remote stacks (phase 3)
  - github.com/rmandevs/stacks//web?ref=v1

mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
  notion:
    url: https://mcp.notion.com/mcp
    agents: [claude-code]                        # restrict to certain agents

skills:
  - mattpocock/skills@tdd                        # one skill from a GitHub repo
  - vercel-labs/agent-skills                     # all skills in the repo
  - github.com/anthropics/skills//pdf?ref=main
  - ./skills/my-skill                            # local
  - pegboard                                     # the built-in skill (§7)

context:
  agents_md: true                                # guarantees AGENTS.md and the CLAUDE.md bridge

remove:                                          # remove something inherited or included
  mcps: [notion]
```

Merge rules: `global` → `include` (in order) → project. Later entries win per
key; `remove` is applied at the end.

In phase 1 each scope applies its own stack to its own targets: the global
one to `~/.claude.json`, `~/.codex/config.toml`…; the project one to
`.mcp.json`, `.codex/config.toml`… Agents already read the global config in
addition to the project one, so copying the global one into every project
would duplicate it. Without `--scope`, `plan`/`apply` work on the scopes that
have a `pegboard.yaml`; `inherit: none` in the project excludes the global
scope.

### 3.2 Secrets

Values with `${…}` are resolved at apply time and are **never** written to
state or the lock:

- `${env:NAME}` — environment variable.
- `${file:~/.secrets/x}` — file contents.
- `${op://vault/item/field}` — 1Password CLI (phase 3).
- `${keychain:service}` — system keychain (phase 3).

If the target agent supports env references natively, the provider leaves
them as references instead of writing the plain-text value. If not, it warns
in the `plan` that the secret will remain in plain text in that file.

| Agent | `${env:X}` | `${file:…}` |
|---|---|---|
| Claude Code | `${X}` (Claude Code expands it) | plain text |
| Codex | `env_vars = ["X"]` if the key is `X`; `Authorization: Bearer ${env:X}` → `bearer_token_env_var`; header `${env:X}` → `env_http_headers`; everything else plain text | plain text |
| opencode | `{env:X}` | `{file:/absolute/path}` |
| Hermes | plain text | plain text |
| Pi | `${X}` | plain text |
| OMP | `${X}` | plain text |
| OpenClaw | `${X}` | plain text |
| Cursor | `${env:X}` | plain text |
| Gemini CLI | `${X}` | plain text |

State stores a hash of each entry with plain-text values masked: it detects
manual edits without depending on the secret. Because `plan` compares what is
in the file with what was just resolved, rotating a plain-text secret shows
up as `~ update`.

### 3.3 `pegboard.lock`

Next to each `pegboard.yaml`. It pins the exact version (commit, tag, content
hash) of each skill, each included stack, and, when applicable, the MCP
package (`npx …@x.y.z`). It is versioned in git. `pegboard apply` uses the
lock; `pegboard update` refreshes it.

Today it pins git skills (JSON, per source):

```json
{
  "version": 1,
  "skills": {
    "mattpocock/skills@tdd": {
      "commit": "3f2a…",
      "skills": { "tdd": "sha256:91c4…" }
    }
  }
}
```

`apply` writes new sources into the lock (like `npm install`) and forgets
those no longer declared. If content downloaded from a pinned commit does not
match its hash, it fails. With a complete lock and cache, `apply` does not
need the network. Local skills are not added to the lock.

### 3.4 State

`~/.local/state/pegboard/state.json` (global) and `.pegboard/state.json`
(project, in `.gitignore`). Per target it records which keys, files, and
symlinks `pegboard` created and their hash. This is what enables principle 2
and drift detection.

---

## 4. CLI

```
pegboard init                     # creates pegboard.yaml (detects installed agents)
pegboard import [--agent X]       # adopts the agents' existing config into pegboard.yaml
pegboard plan                     # diff of what would change; writes nothing
pegboard apply [-y]               # applies the plan (asks for confirmation without -y)
pegboard drift [--exit-code]      # compares state vs. real files (for CI)
pegboard update                   # re-resolves versions and updates pegboard.lock
pegboard add mcp|skill …          # edits pegboard.yaml safely (for humans and agents)
pegboard remove mcp|skill …
pegboard adopt                    # moves everything installed ad-hoc into pegboard.yaml
pegboard install …                # ad-hoc mode: install now, without pegboard.yaml (§4.1)
pegboard uninstall …              # removes something installed ad-hoc
pegboard agents                   # detected agents, paths, and provider version
pegboard skill install            # installs pegboard's skill on agents (§7)
pegboard skill uninstall          # uninstalls pegboard's skill from agents (§7)
```

Common flags: `--scope global|project`, `--agent <id>` (repeatable), `--json`
(stable output for agents and scripts), `--dir <path>`.

`plan` shows, per target, each resource as `+` create, `~` update, `-` remove,
`=` adopt (it was already exactly like that; it is only recorded), or `!`
conflict. A conflict exists when someone edited by hand something `pegboard`
manages, when an entry with that name already exists that `pegboard` did not
create, or when it is written in a way pegboard cannot safely edit (for
example, an inline table in TOML). A conflict stops `apply` unless `--force`
is used.

Exit codes: `0` no changes, `2` there are changes/drift (with `--exit-code`),
`1` error.

### 4.1 Ad-hoc mode

Like Ansible's ad-hoc commands versus playbooks: for "I need this skill and
these MCPs now, on my agents", without writing YAML. It is also the entry
point: the first contact with Pegboard is one line, not a file.

```
pegboard install skill mattpocock/tdd \
                 mcp github='npx -y @modelcontextprotocol/server-github' \
                 mcp notion=https://mcp.notion.com/mcp \
                 --env GITHUB_TOKEN='${env:GITHUB_TOKEN}' \
                 --agent claude-code --agent codex      # default: all detected
pegboard install github.com/rmandevs/stacks//web?ref=v1 # a remote stack, at once
pegboard uninstall mcp notion
```

Rules:

1. **Same engine, same guarantees.** `install` builds a temporary desired
   state and goes through the same `plan` → confirmation → `apply` (with `-y`
   to skip confirmation). It respects what is not yours, makes backups, and
   resolves secrets exactly like declarative mode.
2. **It is recorded.** Every ad-hoc resource enters state with
   `origin: adhoc`. That is why `uninstall` knows exactly what to remove and
   from where.
3. **It does not touch `pegboard.yaml`**, unless `--save` is used, which also
   adds it to the scope's stack. This way a quick experiment becomes
   declarative with one flag. `pegboard adopt` later promotes all ad-hoc
   entries to YAML.
4. **It does not fight declarative mode.** `apply` does not delete ad-hoc
   entries (they are not its own); `apply --prune-adhoc` does. `drift` lists
   them separately as "ad-hoc, not declared", without marking them as errors,
   except with `--strict` (useful in CI).
5. **Default scope:** `project` if there is a `pegboard.yaml` or a git repo in
   the current directory; otherwise `global`. It can be forced with `--scope`.

---

## 5. Providers (agents)

Each provider implements the same interface:

```go
type Provider interface {
    ID() string
    Version() int                               // provider version, not the agent's
    Detect(env Env, scope Scope) []Target       // is it installed / does it apply here? (several: profiles)
}

type MCPProvider interface {
    Provider
    MCPFile(t Target) native.File               // file, format, and container key
    EncodeMCP(m mcp.MCP, enc *Encoder) (*native.Obj, error) // to native format
    DecodeMCP(v any) (mcp.MCP, error)           // from native format (for import)
}
```

`Env` gathers everything the provider queries from the machine (home, project
directory, variables, `PATH`, `--version`), so tests run on a `t.TempDir()`.

Reading, merging, planning, and writing are not repeated per agent:
`internal/engine` does that on top of the `internal/native` editors. A
provider only says where its MCPs live and how they translate. The editors
only rewrite the container key (`mcpServers`, `mcp`, `mcp_servers`):

- **JSON:** it replaces the bytes of the container value; the rest of the file
  (for example, everything else in `~/.claude.json`) stays byte for byte.
- **TOML:** each MCP is a `[mcp_servers.<name>]` section; the section is
  replaced in place and comments remain. An MCP written as an inline table is
  a conflict.
- **YAML:** `yaml.v3` node tree; it preserves comments but normalizes
  indentation to 2 spaces.

An agent is considered installed if its binary is on `PATH` or its user
config directory exists. In `project` scope it applies whenever it is
installed, whether or not files exist in the directory yet.

Requirements for each provider: round-trip tests (read → write → read gives
the same result), preserving format and comments when the format allows it
(TOML, YAML, JSONC), and writing atomically with a backup in
`~/.local/state/pegboard/backups/`.

### Day one

Paths to verify when implementing each provider; this table is a starting
point, not truth.

| Agent | Binary · directory (variable) | MCPs (global / project) | Skills (global / project) | Context |
|---|---|---|---|---|
| Claude Code | `claude` · `~/.claude` (`CLAUDE_CONFIG_DIR`) | `~/.claude.json` / `.mcp.json` | `~/.claude/skills/` / `.claude/skills/` | `CLAUDE.md` with `@AGENTS.md` |
| Codex | `codex` · `~/.codex` (`CODEX_HOME`) | `~/.codex/config.toml` (`[mcp_servers.*]`) / `.codex/config.toml` | `~/.agents/skills/` / `.agents/skills/` | native `AGENTS.md` |
| opencode | `opencode` · `~/.config/opencode` (`XDG_CONFIG_HOME`) | `~/.config/opencode/opencode.json` / `opencode.json` (`mcp`) | `~/.config/opencode/skills/` / `.agents/skills/` | native `AGENTS.md` |
| Hermes | `hermes` · `~/.hermes` (`HERMES_HOME`) | `~/.hermes/config.yaml` (`mcp_servers`) | `~/.hermes/skills/` / `.hermes/skills/`; profiles in `~/.hermes/profiles/<p>/` | `SOUL.md` |
| Pi | `pi` · `~/.pi/agent` (`PI_CODING_AGENT_DIR`) | `~/.pi/agent/mcp.json` / `.pi/mcp.json` (`mcpServers`) | `~/.pi/agent/skills/` / `.pi/skills/` | `AGENTS.md` |
| OMP | `omp` · `~/.omp/agent` (`PI_CONFIG_DIR`, `PI_CODING_AGENT_DIR`) | `~/.omp/agent/mcp.json` / `.omp/mcp.json` (`mcpServers`); profiles in `~/.omp/profiles/<p>/agent/` | `~/.omp/agent/skills/` / `.omp/skills/`; profiles in `~/.omp/profiles/<p>/agent/skills/` | `~/.omp/agent/AGENTS.md` / `.omp/AGENTS.md` |
| OpenClaw | `openclaw` · `~/.openclaw` (`OPENCLAW_STATE_DIR`, `OPENCLAW_CONFIG_PATH`) | `~/.openclaw/openclaw.json` (`mcp.servers`, JSON5); no project scope | `<state-dir>/skills/` | workspace `AGENTS.md` |
| Cursor | `cursor` or `cursor-agent` · `~/.cursor` | `~/.cursor/mcp.json` / `.cursor/mcp.json` (`mcpServers`) | `~/.cursor/skills/` / `.cursor/skills/` | project `AGENTS.md` |
| Gemini CLI | `gemini` · `~/.gemini` (`GEMINI_CLI_HOME`) | `~/.gemini/settings.json` / `.gemini/settings.json` (`mcpServers`) | `~/.gemini/skills/` / `.gemini/skills/` | `GEMINI.md` |

Skills directories follow the `npx skills` agent table (`docs/agents.md` from
vercel-labs/skills 1.7.3). In a project, Codex and opencode share
`.agents/skills/` (the "universal" directory): the skill is linked only once
and the plan shows it as `codex, opencode · project`.

To verify against each real agent: `opencode.jsonc` (today only
`opencode.json` is written); that `CLAUDE_CONFIG_DIR` also moves
`.claude.json`; Codex's `env_vars`, `bearer_token_env_var`, and
`env_http_headers` keys; whether Hermes interpolates variables in
`config.yaml` (today it is assumed not to); OMP's profile layout; OpenClaw's
JSON5 config (pegboard rewrites only the `mcp` block); Cursor's required
`type: "stdio"`; and Gemini's `httpUrl` vs `url` transports.

Hermes and OpenClaw have no known project scope: only `global` (Hermes also
per profile, as the `hermes:<profile>` sub-target).

### Later

Kiro, GitHub Copilot CLI, Windsurf, Cline, Zed, Amp… one at a time, on demand. The
competition is on correctness, not on the number of agents.

### Skills: how they are installed

Skills are downloaded once into a cache (`~/.cache/pegboard/skills/<hash>`)
and linked (symlink; copy on Windows or if the agent does not follow
symlinks) into each agent's directory. This way a skill exists only once on
disk and the lock guarantees it is the same everywhere.

**Sources** (the same conventions as `npx skills` and skills.sh, so a skills
repo works the same for both):

| Source | What it is |
|---|---|
| `owner/repo` | all skills in a GitHub repo |
| `owner/repo@skill` | a single skill from the repo |
| `github.com/owner/repo//path` | host + subdirectory (go-getter style); any git host |
| `https://github.com/o/r/tree/<ref>/<path>` | GitHub or GitLab URL |
| `https://…`, `git@…` | any git repo (supports `//path`) |
| `./path`, `~/path`, `/path` | local skill: linked as-is, without cache or lock |
| `pegboard` | the built-in skill (§7), embedded in the binary |

Git sources accept `?ref=<branch|tag|commit>`. They are downloaded with the
system `git` (`fetch --depth 1`), so whatever credentials you already have
for private repos work. Discovery inside the repo is the same as `npx
skills`: `SKILL.md` at the root; otherwise `skills/`, `.agents/skills/`, and
`.claude/skills/` up to three levels deep; otherwise the whole tree. The name
comes from the frontmatter's `name:`.

**Relationship with `npx skills`.** It is not invoked: pegboard speaks the
same format (sources, discovery, per-agent directories) but performs the
installation itself, because delegating it would break three principles.
`npx skills` does not expose which files it wrote, so there would be no state
and no drift (principle 2); it brings its own lock (`skills-lock.json`),
which would be a second source of truth; and it adds a Node ≥ 22 dependency
to a binary that is currently self-contained. What is gained from the
ecosystem is the whole catalog: any skills.sh repo installs the same way with
`pegboard install skill owner/repo@skill`.

---

## 6. `plan` example

```
$ pegboard plan
claude-code · project
  + mcp github           (.mcp.json)
  + skill mattpocock/tdd (.claude/skills/tdd → cache@a1b2c3)
codex · project
  + mcp github           (.codex/config.toml)
  ! mcp notion           edited manually since the last apply
hermes · global
  ~ mcp github           args changed
Plan: 3 create, 1 update, 0 remove, 1 conflict.
```

---

## 7. Skill for agents

`pegboard` ships its own skill (`SKILL.md`), installable with
`pegboard skill install` (or by declaring it in the stack like any other). It
teaches the agent to:

1. Never edit `.mcp.json`, `config.toml`, etc. by hand. For quick requests
   ("install X for me"), use `pegboard install`; if the project already has a
   `pegboard.yaml`, use `pegboard add` / `pegboard remove` or `install
   --save`.
2. Always run `pegboard plan --json`, show the summary to the user, and only
   then run `pegboard apply -y`.
3. On a conflict (`!`), ask the user instead of forcing it.
4. Never write secrets in plain text: propose a `${env:…}` reference.

With this, "add the Linear MCP to all my agents" costs a couple of
deterministic tool calls instead of having the agent read and rewrite four
different formats.

The skill lives in `skill/SKILL.md` and is embedded in the binary:
`pegboard skill install` installs it in the global scope of all detected
agents, `pegboard skill uninstall` removes it from them, and it can also be
declared in a stack as the `pegboard` source.

### `context.agents_md`

Project scope only. It creates `AGENTS.md` if it does not exist (from then on
it belongs to the user: pegboard never deletes it) and adds the line
`@AGENTS.md` to `CLAUDE.md` so Claude Code reads it. If `agents_md` is later
removed, pegboard removes that line only if it added it.

Optional (phase 4): `pegboard mcp`, an MCP server with `plan`, `apply`, `add`,
and `drift` tools for agents that prefer tools over a shell.

---

## 8. Teams (pull)

`pegboard` does not push anything to other machines. On a team:

1. **Stack in the project repo.** `pegboard.yaml` + `pegboard.lock` are
   versioned; each developer clones and runs `pegboard apply`. Like
   `package.json` + `npm install`.
2. **Organization stack as a remote stack.** Each developer's global stack
   does `include: github.com/company/agent-stack?ref=v3`. Changing versions
   is each person's decision, after seeing the `plan`.
3. **Policies as validation.** A stack can declare
   `policy: { allow_mcps: [github, linear] }`; `plan`/`apply` fail if the
   result includes anything outside the list. In CI, `pegboard drift
   --exit-code` and `pegboard plan --exit-code` check that the repo complies.
4. **Fleets.** If a company wants to enforce it on every laptop, it uses
   Ansible, an MDM, or each harness's *managed settings* to run `pegboard
   apply`. `pegboard` is the module; they are the transport.

---

## 9. Implementation

- **Language:** Go. Single cross-platform binary (Linux, macOS, Windows).
- **Candidate libraries:** `cobra` (CLI), `gopkg.in/yaml.v3` and
  `pelletier/go-toml/v2` (preserving format where possible),
  `santhosh-tekuri/jsonschema`, Bubble Tea / Lip Gloss (TUI, phase 4).
- **Layout:**
  ```
  cmd/pegboard/        # CLI entry point
  internal/stack/      # parsing, schema, merging, includes
  internal/resolve/    # remote skills/stacks, cache, lock
  internal/secrets/    # ${…} resolution
  internal/state/      # state and drift
  internal/plan/       # plan computation and rendering (text and JSON)
  providers/<id>/      # one package per agent, with testdata/ of real configs
  skill/SKILL.md       # the skill from §7
  schema/pegboard.schema.json
  ```
- **Tests:** golden files per provider with real configs (including "dirty"
  configs with unmanaged content that must not be touched). No test writes
  outside a `t.TempDir()`.
- **Distribution:** GitHub Releases with goreleaser, Homebrew tap, `go
  install`.
- **Tapioca** consumes it as a binary: its `apply_stack` and `check_drift`
  tools become calls to `pegboard --json`.

---

## 10. Phases

Each phase ends when I use it daily without frustration.

### Phase 0 — Skeleton ✅
- Go repo, CI (lint + tests), `pegboard agents` detecting the supported agents.
- JSON Schema for `pegboard.yaml`.
- **Done when:** `pegboard agents` lists what is installed on my machine
  correctly.

### Phase 1 — Local MCPs ✅
- `init`, `plan`, `apply`, `drift` for MCPs across the providers, global and
  project scopes. State and backups. `env:` and `file:` secrets.
- `import` from each agent.
- Ad-hoc mode for MCPs: `install` / `uninstall`, `--save`, `origin: adhoc` in
  state.
- **Done when:** I stop editing `.mcp.json` / `config.toml` by hand.

**MVP = phases 0–2**, plus goreleaser releases (`git tag vX.Y.Z` publishes
Linux, macOS, and Windows binaries). Phases 3 and 4 come later.

### Phase 2 — Skills and the built-in skill ✅
- Local and git skills with cache, symlinks, and `pegboard.lock`; `update`.
- `context.agents_md`.
- `add` / `remove`, `install skill …`, `adopt`, stable `--json`,
  `pegboard skill install` / `pegboard skill uninstall`.
- **Done when:** I ask an agent "add X to all of them" and it does it with
  `pegboard` without mistakes.

### Phase 3 — Remote stacks and policies
- `include` of git stacks with a lock; `policy.allow_mcps`.
- `op://` and `keychain:` secrets.
- `--exit-code` for CI and a GitHub Action example.
- **Done when:** I share a stack in a repo and another machine ends up the
  same with one `pegboard apply`.

### Phase 4 — TUI and extras
- TUI (Bubble Tea): view targets, review the plan, apply.
- `pegboard mcp` (pegboard's own MCP server).
- More providers based on use (Kiro, GitHub Copilot CLI, Windsurf, Cline, Zed, Amp…).

### Later (no commitment)
- Light UI (or Tapioca's as the first client).
- Stack registry; skill signing/verification.

---

## 11. Risks

- **Agents change paths and formats often.** Mitigation: versioned providers,
  `pegboard agents` shows the detected harness version, and golden tests with
  real configs are updated when changes are detected.
- **Preserving comments and format** in JSON/TOML/YAML is hard; if it cannot
  be done, the `plan` warns before writing.
- **Symlinks on Windows** and agents that do not follow them: fall back to a
  copy and record it in state.
- **Plain-text secrets** when the agent does not support references: always
  warn in the `plan`.
- **Third-party skills = third-party code.** The lock pins the exact content;
  `update` shows the diff before accepting changes.

## 12. References

- Neighbors: rulesync (https://github.com/dyoshikawa/rulesync), ruler, vsync
  (https://github.com/nicepkg/vsync), sync-agents-settings
  (https://github.com/Leoyang183/sync-agents-settings), add-mcp (Neon),
  `npx skills` (Vercel).
- Hermes MCP: https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp
- Inspiration: Ansible (roles, `--check --diff`), Terraform
  (plan/state/lock), Jenkins CasC (the repo's original name), 5S shadow
  boards (the name).
