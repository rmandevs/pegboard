# Pegboard

Configuration as Code para agentes: declara en `pegboard.yaml` los MCPs,
skills y contexto que quieres en Claude Code, Codex, opencode, Hermes…, y
`pegboard` los aplica con `plan` / `apply` / `drift`, como Ansible + Terraform.

Como el tablero del taller: cada herramienta tiene su silueta, y de un vistazo
ves qué falta.

Ver [SPEC.md](SPEC.md). Estado: fase 0 — esqueleto y `pegboard agents`.

## Uso

```
go install github.com/rmandevs/agents-casc/cmd/pegboard@latest

pegboard agents                    # agentes detectados, rutas y versión
pegboard agents --scope project --agent claude-code
pegboard agents --json             # salida estable para scripts y agentes
```

## Desarrollo

```
go test ./...
golangci-lint run ./...
```

- `cmd/pegboard/` entrada · `internal/cli/` comandos · `internal/agent/` tipos
  comunes y `Env` · `providers/<id>/` un paquete por agente.
- `schema/pegboard.schema.json` es el JSON Schema de `pegboard.yaml`; los casos
  en `schema/testdata/{valid,invalid}/` se validan en los tests.
- Ningún test toca la máquina real: `internal/agent/agenttest` arma un home y
  un proyecto falsos en `t.TempDir()`.
