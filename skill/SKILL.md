---
name: pegboard
description: Install, remove, and sync MCPs, skills, and AGENTS.md in Claude Code, Codex, opencode, Hermes, Pi, OMP, OpenClaw, Cursor, and Gemini CLI with the `pegboard` CLI. Use it whenever the user asks to add, remove, list, or sync an MCP server or a skill across one or more agents ("add the Linear MCP", "install this skill on all my agents", "what MCPs do I have?"), or when the project has a pegboard.yaml.
---

# pegboard

`pegboard` translates a desired state (MCPs, skills, context) into each
agent's native files, with deterministic code, state, and backups. Use it
instead of editing those files yourself.

## Rules

1. **Never edit by hand** `.mcp.json`, `~/.claude.json`, `.codex/config.toml`,
   `opencode.json`, `~/.hermes/config.yaml`, `~/.pi/agent/mcp.json`,
   `.omp/mcp.json`, `~/.openclaw/openclaw.json`, `~/.cursor/mcp.json`,
   `~/.gemini/settings.json`, or the skills directories. Use `pegboard`.
2. **Plan first, then apply.** Run the command with `--dry-run --json` (or
   `pegboard plan --json` for a stack), summarize for the user what will change,
   and only then repeat it with `-y --json`.
3. **Conflicts (`"action": "conflict"`): ask.** It means someone edited by
   hand something pegboard manages, or something with that name already
   exists that pegboard did not create. Explain the `reason` and ask before
   using `--force`.
4. **Never write secrets in plain text.** Use references: `${env:NAME}` or
   `${file:~/.secrets/file}`. If the plan contains "plain text secret"
   warnings, tell the user: that agent does not support references.

## Which command to use

Is there a `pegboard.yaml` in the project (or in `~/.config/pegboard/`)?

- **Yes → declarative.** Edit the stack and apply:
  ```
  pegboard add mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
  pegboard add mcp notion=https://mcp.notion.com/mcp --agent claude-code
  pegboard add skill vercel-labs/agent-skills@web-design-guidelines
  pegboard remove mcp notion
  pegboard plan --json        # show the summary to the user
  pegboard apply -y --json
  ```
- **No → ad-hoc.** Install now, without YAML:
  ```
  pegboard install mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}' --dry-run --json
  pegboard install mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}' --json -y
  pegboard install skill mattpocock/skills@tdd -y
  pegboard uninstall mcp github -y
  ```
  Add `--save` if the user wants it declared.

Without `--agent`, everything goes to all detected agents; repeat `--agent`
to limit it (`claude-code`, `codex`, `opencode`, `hermes`, `hermes:<profile>`,
`pi`, `omp`, `omp:<profile>`, `openclaw`, `cursor`, `gemini`).
`--scope global` is for the user; `--scope project` is for the current
directory. OpenClaw is global-only.

## Copy one agent's setup to others

When the user asks to move or copy their setup ("give Codex what Claude Code
has"), use `replicate`. The first agent can be any of them, and it is not changed:

```
pegboard replicate claude-code codex opencode --dry-run --json   # summarize for the user
pegboard replicate claude-code codex opencode -y --json
```

Tell the user which variables to export: their secrets become `${env:…}`
references.

Context files are left alone unless you pass `--context`. Ask the user where the
source's text should go before using it: `before` or `after` what each target
already has, or `replace` it (the file is backed up). `--dry-run` shows the
result without writing.

## Built-in skill

```
pegboard skill install -y      # install this skill on the detected agents
pegboard skill uninstall -y    # remove it from the detected agents
```

Both default to the global scope. Repeat `--agent` to limit them.

## Skill sources

`owner/repo` (all skills in the repo), `owner/repo@skill` (one),
`github.com/owner/repo//path?ref=v1`, a git URL, a local `./path`, or
`pegboard` (this skill).

## To review

```
pegboard agents --json          # detected agents and their paths
pegboard drift --json           # did anyone edit pegboard-managed files by hand?
pegboard update                 # new skill versions (shows the diff)
```

Exit codes: `0` ok, `1` error, `2` there are changes or drift (with
`--exit-code`).
