# Pegboard

Configuration as Code para agentes: declara en `pegboard.yaml` los MCPs,
skills y contexto que quieres en Claude Code, Codex, opencode, Hermes…, y
`pegboard` los aplica con `plan` / `apply` / `drift`, como Ansible + Terraform.

Como el tablero del taller: cada herramienta tiene su silueta, y de un vistazo
ves qué falta.

Ver [SPEC.md](SPEC.md). Estado: MVP — MCPs, skills y AGENTS.md en Claude Code,
Codex, opencode y Hermes. Stacks remotos y la TUI llegan en las fases 3–4.

## Uso

```
go install github.com/rmandevs/agents-casc/cmd/pegboard@latest
pegboard skill install             # enséñale a tus agentes a usar pegboard

pegboard agents                    # agentes detectados, rutas y versión
pegboard init                      # pegboard.yaml con los agentes de este proyecto
pegboard import                    # o adopta los MCPs que ya tienen tus agentes
pegboard plan                      # qué cambiaría (+ ~ - = !); no escribe
pegboard apply                     # lo aplica, con respaldo de cada archivo
pegboard drift --exit-code         # ¿alguien editó a mano lo que maneja pegboard?
pegboard add skill mattpocock/skills@tdd   # edita pegboard.yaml; luego plan/apply
pegboard update                    # nuevas versiones de skills, con diff

# Modo ad-hoc: sin YAML, de una línea
pegboard install mcp github='npx -y @modelcontextprotocol/server-github' \
                 --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
pegboard install mcp notion=https://mcp.notion.com/mcp --agent claude-code --save
pegboard install skill vercel-labs/agent-skills@web-design-guidelines
pegboard uninstall mcp github
pegboard adopt                     # lo ad-hoc pasa a pegboard.yaml
```

Las skills usan las mismas fuentes que `npx skills` / skills.sh (`owner/repo`,
`owner/repo@skill`, URLs git, `./local`). Se descargan una vez a
`~/.cache/pegboard`, se enlazan en cada agente y quedan fijadas por commit y
hash en `pegboard.lock`.

Banderas comunes: `--scope global|project`, `--agent <id>` (repetible),
`--json`, `--dir`. Los secretos van como `${env:X}` o `${file:ruta}` y nunca
se escriben en el estado; si el agente no admite referencias, el plan avisa
que quedarán en claro.

## Releases

`git tag v0.1.0 && git push origin v0.1.0` corre `.github/workflows/release.yml`,
que publica con goreleaser binarios para Linux, macOS y Windows (amd64/arm64).

## Desarrollo

```
go test ./...
golangci-lint run ./...
```

- `cmd/pegboard/` entrada · `internal/cli/` comandos · `internal/agent/` tipos
  comunes y `Env` · `providers/<id>/` un paquete por agente (detección y
  traducción de MCPs).
- `internal/skills/` fuentes, descubrimiento, caché y `pegboard.lock` ·
  `skill/SKILL.md` la skill propia (embebida).
- `internal/engine/` plan, apply y drift (MCPs, skills y contexto) · `internal/native/` editores de
  JSON/TOML/YAML que solo tocan la llave de MCPs · `internal/state/` estado,
  escritura atómica y backups · `internal/stack/` pegboard.yaml ·
  `internal/secrets/` referencias `${…}`.
- `schema/pegboard.schema.json` es el JSON Schema de `pegboard.yaml`; los casos
  en `schema/testdata/{valid,invalid}/` se validan en los tests.
- Ningún test toca la máquina real: `internal/agent/agenttest` arma un home y
  un proyecto falsos en `t.TempDir()`.
