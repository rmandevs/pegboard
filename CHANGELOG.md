# Changelog

All notable changes to Pegboard. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/) (before 1.0, a minor version can
change behavior).

## [0.2.0] - 2026-11-04

Shared stacks, safer skills, and a CI story.

### Added

- **`include`** of other stacks: a local folder, a git repo
  (`owner/repo//path?ref=v1`), or a zip (local or https). Stacks merge in
  order; `remove` applies to the stack's own subtree; cycles, more than 10
  levels, and the same repo with two refs are errors. Git and zip stacks are
  pinned in `pegboard.lock` under `includes` and cached by content hash.
- **Approval of MCPs from remote stacks.** An MCP that a git or zip stack
  brings is a conflict until you run `apply --approve`; the plan shows its full
  command and `via` source. The lock records what was approved, so teammates
  applying the same lock are not asked again, and a changed command asks
  again.
- **Trust boundaries for remote stacks.** Their skills, includes, and
  `${file:…}` references cannot use `~/`, absolute paths, or `..` that leaves
  the stack.
- **`policy.allow_skill_sources`**, with `*` patterns, next to `allow_mcps`.
  Every policy in the include tree is checked against the final result, so an
  included stack's policy cannot be relaxed.
- **Hidden-Unicode scanning.** Skills with bidirectional overrides, Unicode
  tags, or the variation selector supplement are blocked in the plan
  (`--allow-hidden-unicode` lets them through; `--force` does not).
  Zero-width characters are warnings.
- **`pegboard audit`** scans every skill your agents can read, the skills the
  stack declares, and the context files. `--exit-code` and `--strict` for CI.
- **`pegboard lock export --format cyclonedx|spdx`** prints the lock as a
  CycloneDX 1.5 or SPDX 2.3 SBOM.
- **`pegboard import --from apm.yml`** reads a Microsoft APM manifest: its
  skill packages and self-defined MCP servers go into `pegboard.yaml`, and
  everything skipped is listed with a reason.
- **APM-style sources:** `owner/repo#ref`, `owner/repo/sub/path`, and
  `host/owner/repo/sub/path#ref` work as skill sources.
- **GitHub Copilot CLI provider** (`copilot`): MCPs in
  `~/.copilot/mcp-config.json`, skills in `~/.copilot/skills` and
  `.github/skills`, and project `AGENTS.md`.
- **GitHub Action** at the repository root: runs `drift`, `plan`, or `audit`
  with `--exit-code`.
- `pegboard update` refreshes included stacks and lists new or changed MCP
  commands before writing the lock.

### Changed

- `include` is no longer ignored with a warning.
- A skill that `remove.skills` drops no longer counts in the duplicate-name
  check, so `remove` can settle a clash between two sources.
- The built-in skill tells agents never to pass `--approve` or
  `--allow-hidden-unicode` without asking.

## [0.1.0] - 2026-11-03

First release.

### Added

- `init`, `plan`, `apply`, `drift`, and `import` for MCPs on Claude Code,
  Codex, opencode, Hermes, Pi, OMP, OpenClaw, Cursor, and Gemini CLI, in
  global and project scopes, with state and backups.
- `${env:…}` and `${file:…}` secrets, kept as references where the agent
  supports them.
- Skills from git and local sources, cached once, linked into each agent, and
  pinned in `pegboard.lock`; `update`.
- `context.agents_md` and the `@AGENTS.md` bridge in `CLAUDE.md`.
- Ad-hoc mode: `install`, `uninstall`, `--save`, and `adopt`.
- `replicate` to copy one agent's setup to others.
- The built-in skill: `pegboard skill install` / `uninstall`.
- `install.sh` with checksum verification.

[0.2.0]: https://github.com/rmandevs/pegboard/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/rmandevs/pegboard/releases/tag/v0.1.0
