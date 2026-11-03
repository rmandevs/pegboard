# Contributing

Bug reports and pull requests are welcome. For a larger change, open an issue first so we can agree on the approach.

## Set up

You need the Go version in [`go.mod`](go.mod) and [golangci-lint](https://golangci-lint.run/).

```sh
go test ./...
golangci-lint run ./...
```

No test touches your real machine: `internal/agent/agenttest` builds a fake home and project in `t.TempDir()`.

## Where things live

- `cmd/pegboard/` is the entry point, and `internal/cli/` holds the commands.
- `internal/agent/` has the shared types and `Env`. `providers/<id>/` has one package per agent, with its detection and MCP translation.
- `internal/skills/` handles sources, discovery, the cache, and `pegboard.lock`. `skill/SKILL.md` is the built-in skill, embedded in the binary.
- `internal/engine/` runs plan, apply, and drift for MCPs, skills, and context.
- `internal/native/` has the JSON, TOML, and YAML editors that only touch the MCP key.
- `internal/state/` has state, atomic writes, and backups. `internal/stack/` reads `pegboard.yaml`, and `internal/secrets/` resolves `${…}` references.
- `schema/pegboard.schema.json` is the JSON Schema for `pegboard.yaml`. The tests validate the cases in `schema/testdata/{valid,invalid}/`.

[SPEC.md](SPEC.md) explains the design.

## Releases

Pushing a version tag runs `.github/workflows/release.yml`, which publishes Linux, macOS, and Windows binaries (amd64 and arm64) with goreleaser:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

Tags matching `v*` can't be moved or deleted, so use a new version to fix a release.
