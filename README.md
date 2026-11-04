<p align="center">
  <a href="https://usepegboard.dev"><img src="assets/logo.svg" alt="Pegboard" width="112"></a>
</p>

<h1 align="center">Pegboard</h1>

<p align="center">
  <strong>Every tool in its place. On every agent.</strong><br>
  Declare your MCPs, skills, and context in one file, and Pegboard writes each agent's native config.
</p>

<p align="center">
  <a href="https://github.com/rmandevs/pegboard/actions/workflows/ci.yml"><img src="https://github.com/rmandevs/pegboard/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/rmandevs/pegboard/releases/latest"><img src="https://img.shields.io/github/v/release/rmandevs/pegboard?color=444" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/rmandevs/pegboard?color=444" alt="MIT license"></a>
</p>

<p align="center">
  <a href="https://usepegboard.dev">Website</a> ·
  <a href="https://usepegboard.dev/docs/">Docs</a> ·
  <a href="https://usepegboard.dev/docs/quick-start/">Quick start</a> ·
  <a href="https://github.com/rmandevs/pegboard/releases">Releases</a>
</p>

---

Every coding agent stores its MCPs and skills in a different place and format, so keeping them in sync means translating by hand. Pegboard does that translation with `plan`, `apply`, and `drift`, like Ansible and Terraform do for servers.

Like a workshop pegboard, every tool has its silhouette, and at a glance you see what's missing.

## Install

On Linux or macOS:

```sh
curl -fsSL https://usepegboard.dev/install.sh | sh
```

The script verifies the release checksum and installs the binary to `~/.local/bin`. To update, run it again. You can [read the script](install.sh) first.

On Windows, download a binary from [Releases](https://github.com/rmandevs/pegboard/releases). With Go, run `go install github.com/rmandevs/pegboard/cmd/pegboard@latest`.

## Quick start

Create a stack from a project directory:

```sh
pegboard init      # pegboard.yaml with the agents found on this machine
pegboard import    # or adopt the MCPs your agents already have
```

Declare what you want in `pegboard.yaml`:

```yaml
version: 1
agents: [claude-code, codex, opencode]

mcps:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${env:GITHUB_TOKEN}"
  notion:
    url: https://mcp.notion.com/mcp
    agents: [claude-code]

context:
  agents_md: true
```

See what would change. `plan` writes nothing:

```console
$ pegboard plan
claude-code · project
  + mcp github                       (.mcp.json)
  + mcp notion                       (.mcp.json)
codex · project
  + mcp github                       (.codex/config.toml)
opencode · project
  + mcp github                       (opencode.json)
agents.md · project
  + context AGENTS.md                (AGENTS.md)
claude-code · project
  + context @AGENTS.md in CLAUDE.md  (CLAUDE.md)
Plan: 6 create, 0 update, 0 remove, 0 conflicts.
```

Then apply it, and check later that nobody edited those files by hand:

```sh
pegboard apply               # backs up every file it changes
pegboard drift --exit-code   # also useful in CI
```

Already have one agent set up the way you like? Copy it to the others, with no YAML:

```sh
pegboard replicate claude-code codex opencode
```

Continue with the [quick start](https://usepegboard.dev/docs/quick-start/) or the [CLI reference](https://usepegboard.dev/docs/cli/).

## Supported agents

| Agent | MCPs | Skills | Context |
|---|---|---|---|
| Claude Code | yes | yes | `CLAUDE.md` bridge |
| Codex | yes | yes | `AGENTS.md` |
| opencode | yes | yes | `AGENTS.md` |
| Hermes | yes | yes | `SOUL.md` (display only) |
| Pi | yes | yes | `AGENTS.md` |
| OMP (oh-my-pi) | yes | yes | `.omp/AGENTS.md` (`replicate --context`) |
| OpenClaw | yes (`mcp.servers`) | yes | workspace `AGENTS.md` (`replicate --context`) |
| Cursor | yes | yes | project `AGENTS.md` |
| Gemini CLI | yes | yes | `GEMINI.md` (`replicate --context`) |
| GitHub Copilot CLI | yes (global) | yes | project `AGENTS.md` |

## What it does

- **Shows the plan first.** `plan` writes nothing, and `apply` backs up every file before changing it.
- **Keeps secrets out of state.** Use `${env:X}` or `${file:path}`. Secrets are never written to state, and if an agent doesn't support references, the plan warns that they will be left in plain text.
- **Pins skills.** Skills use the same sources as `npx skills` (`owner/repo`, `owner/repo@skill`, git URLs, `./local`). They're cached once, linked into each agent, and pinned by commit and hash in `pegboard.lock`.
- **Works without a YAML file too.** Try something with one line, then keep it with `pegboard adopt`:

  ```sh
  pegboard install mcp notion=https://mcp.notion.com/mcp --agent claude-code
  pegboard install skill vercel-labs/agent-skills@web-design-guidelines
  ```

- **Shares stacks.** `include` pulls other stacks from a folder, a git repo, or a zip, pinned in `pegboard.lock`. An MCP that a remote stack brings is shown with its full command and source, and only goes in with `--approve`. Approval is recorded in the lock, so a team reviews it once, in a pull request.

  ```yaml
  include:
    - acme/agent-stack//web?ref=v3
  policy:
    allow_mcps: [github, linear]   # an included stack can tighten this, never relax it
  ```

- **Blocks hidden instructions.** Skills with invisible Unicode (bidirectional overrides, Unicode tags) are stopped in the plan, and `pegboard audit` scans every skill and context file your agents read.
- **Fits CI.** `drift`, `plan`, and `audit` take `--exit-code`, and the repo is a GitHub Action:

  ```yaml
  - uses: rmandevs/pegboard@v0.2.0
    with:
      command: drift   # or plan, or audit
  ```

- **Exports an SBOM.** `pegboard lock export --format cyclonedx` (or `spdx`) lists every pinned skill and stack with its commit and hash.
- **Reads APM manifests.** `pegboard import --from apm.yml` brings the skills and self-defined MCPs of a [Microsoft APM](https://github.com/microsoft/apm) manifest into `pegboard.yaml`, and sources like `owner/repo/path#v1` work as written.
- **Teaches your agents to use it.** `pegboard skill install` gives them a skill for working with Pegboard.

## Pegboard and APM

[APM](https://github.com/microsoft/apm) is a package manager: it resolves agent packages (instructions, prompts, agents, hooks, skills) for a project and compiles them. Pegboard reconciles: it reads what each agent already has, shows a plan against those live files, writes only what it owns, and detects drift. They work together. Keep `apm.yml` for packages, and use `pegboard import --from apm.yml` when you want plan, state, and drift across agents.

## Status

Pegboard is young. It handles MCPs, skills, context, and shared stacks on the agents above. `op://` and keychain secrets and a TUI are planned; see [SPEC.md](SPEC.md) for the design and phases, and [CHANGELOG.md](CHANGELOG.md) for what changed.

## Contributing

Bug reports and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) to get set up, and [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## License

[MIT](LICENSE)
