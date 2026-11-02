# Pegboard

Configuration as Code for agents: declare in `pegboard.yaml` the MCPs, skills,
and context you want in Claude Code, Codex, opencode, Hermes, Pi, OMP,
OpenClaw, Cursor, Gemini CLI…, and `pegboard` applies them with `plan` / `apply` / `drift`, like Ansible + Terraform.

Like the workshop pegboard: every tool has its silhouette, and at a glance you
see what is missing.

See [SPEC.md](SPEC.md). Status: MVP — MCPs, skills, and AGENTS.md in Claude
Code, Codex, opencode, Hermes, Pi, OMP, OpenClaw, Cursor, and Gemini CLI.
Remote stacks and the TUI arrive in phases 3–4.

## Supported agents

| Agent | MCPs | Skills | Context |
|---|---|---|---|
| Claude Code | yes | yes | `CLAUDE.md` bridge |
| Codex | yes | yes | `AGENTS.md` |
| opencode | yes | yes | `AGENTS.md` |
| Hermes | yes | yes | `SOUL.md` (display only) |
| Pi | yes | yes | `AGENTS.md` |
| OMP (oh-my-pi) | yes | yes | `.omp/AGENTS.md` (display only) |
| OpenClaw | yes (`mcp.servers`) | yes | workspace `AGENTS.md` (display only) |
| Cursor | yes | yes | project `AGENTS.md` |
| Gemini CLI | yes | yes | `GEMINI.md` (display only) |

## Install

On Linux or macOS:

```
curl -fsSL https://raw.githubusercontent.com/rmandevs/pegboard/main/install.sh | sh
```

This verifies the release checksum and installs the binary to `~/.local/bin`.
Set `PEGBOARD_INSTALL_DIR` to change the location or `PEGBOARD_VERSION=v0.1.0`
to pin a version. To update, run it again.

On Windows, download a binary from
[Releases](https://github.com/rmandevs/pegboard/releases). With Go installed,
you can also use
`go install github.com/rmandevs/pegboard/cmd/pegboard@latest`.

## Usage

```
pegboard skill install             # teach your agents to use pegboard
pegboard skill uninstall           # remove that skill from your agents

pegboard agents                    # detected agents, paths, and version
pegboard init                      # pegboard.yaml with this project's agents
pegboard import                    # or adopt the MCPs your agents already have
pegboard plan                      # what would change (+ ~ - = !); writes nothing
pegboard apply                     # applies it, backing up every file
pegboard drift --exit-code         # did anyone edit pegboard-managed files by hand?
pegboard add skill mattpocock/skills@tdd   # edits pegboard.yaml; then plan/apply
pegboard update                    # new skill versions, with a diff

# Ad-hoc mode: no YAML, one line
pegboard install mcp github='npx -y @modelcontextprotocol/server-github' \
                 --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
pegboard install mcp notion=https://mcp.notion.com/mcp --agent claude-code --save
pegboard install skill vercel-labs/agent-skills@web-design-guidelines
pegboard uninstall mcp github
pegboard adopt                     # move ad-hoc installs into pegboard.yaml
```

Skills use the same sources as `npx skills` / skills.sh (`owner/repo`,
`owner/repo@skill`, git URLs, `./local`). They are downloaded once to
`~/.cache/pegboard`, linked into each agent, and pinned by commit and hash in
`pegboard.lock`.

Common flags: `--scope global|project`, `--agent <id>` (repeatable),
`--json`, `--dir`. Secrets use `${env:X}` or `${file:path}` and are never
written to state; if the agent does not support references, the plan warns
that they will be left in plain text.

## Releases

`git tag v0.1.0 && git push origin v0.1.0` runs
`.github/workflows/release.yml`, which publishes Linux, macOS, and Windows
(amd64/arm64) binaries with goreleaser.

## Development

```
go test ./...
golangci-lint run ./...
```

- `cmd/pegboard/` entry point · `internal/cli/` commands · `internal/agent/`
  shared types and `Env` · `providers/<id>/` one package per agent (detection
  and MCP translation).
- `internal/skills/` sources, discovery, cache, and `pegboard.lock` ·
  `skill/SKILL.md` the built-in skill (embedded).
- `internal/engine/` plan, apply, and drift (MCPs, skills, and context) ·
  `internal/native/` JSON/TOML/YAML editors that only touch the MCP key ·
  `internal/state/` state, atomic writes, and backups · `internal/stack/`
  pegboard.yaml · `internal/secrets/` `${…}` references.
- `schema/pegboard.schema.json` is the JSON Schema for `pegboard.yaml`; the
  cases in `schema/testdata/{valid,invalid}/` are validated in the tests.
- No test touches the real machine: `internal/agent/agenttest` builds a fake
  home and project in `t.TempDir()`.
