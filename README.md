# Pegboard

Configuration as Code para agentes: declara en `pegboard.yaml` los MCPs,
skills y contexto que quieres en Claude Code, Codex, opencode, Hermes…, y
`pegboard` los aplica con `plan` / `apply` / `drift`, como Ansible + Terraform.

Como el tablero del taller: cada herramienta tiene su silueta, y de un vistazo
ves qué falta.

Ver [SPEC.md](SPEC.md). Estado: fase 1 — MCPs en Claude Code, Codex, opencode y
Hermes. Skills, stacks remotos y la TUI llegan en las fases 2–4.

## Uso

```
go install github.com/rmandevs/agents-casc/cmd/pegboard@latest

pegboard agents                    # agentes detectados, rutas y versión
pegboard init                      # pegboard.yaml con los agentes de este proyecto
pegboard import                    # o adopta los MCPs que ya tienen tus agentes
pegboard plan                      # qué cambiaría (+ ~ - = !); no escribe
pegboard apply                     # lo aplica, con respaldo de cada archivo
pegboard drift --exit-code         # ¿alguien editó a mano lo que maneja pegboard?

# Modo ad-hoc: sin YAML, de una línea
pegboard install mcp github='npx -y @modelcontextprotocol/server-github' \
                 --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
pegboard install mcp notion=https://mcp.notion.com/mcp --agent claude-code --save
pegboard uninstall mcp github
```

Banderas comunes: `--scope global|project`, `--agent <id>` (repetible),
`--json`, `--dir`. Los secretos van como `${env:X}` o `${file:ruta}` y nunca
se escriben en el estado; si el agente no admite referencias, el plan avisa
que quedarán en claro.

## Desarrollo

```
go test ./...
golangci-lint run ./...
```

- `cmd/pegboard/` entrada · `internal/cli/` comandos · `internal/agent/` tipos
  comunes y `Env` · `providers/<id>/` un paquete por agente (detección y
  traducción de MCPs).
- `internal/engine/` plan, apply y drift · `internal/native/` editores de
  JSON/TOML/YAML que solo tocan la llave de MCPs · `internal/state/` estado,
  escritura atómica y backups · `internal/stack/` pegboard.yaml ·
  `internal/secrets/` referencias `${…}`.
- `schema/pegboard.schema.json` es el JSON Schema de `pegboard.yaml`; los casos
  en `schema/testdata/{valid,invalid}/` se validan en los tests.
- Ningún test toca la máquina real: `internal/agent/agenttest` arma un home y
  un proyecto falsos en `t.TempDir()`.
