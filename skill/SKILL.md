---
name: pegboard
description: Instala, quita y sincroniza MCPs, skills y AGENTS.md en Claude Code, Codex, opencode y Hermes con el CLI `pegboard`. Úsala siempre que el usuario pida agregar, quitar, listar o sincronizar un servidor MCP o una skill en uno o varios agentes ("agrégame el MCP de Linear", "instala esta skill en todos mis agentes", "¿qué MCPs tengo?"), o cuando el proyecto tenga un pegboard.yaml.
---

# pegboard

`pegboard` traduce un estado deseado (MCPs, skills, contexto) a los archivos
nativos de cada agente, con código determinista, estado y backups. Úsalo en
lugar de editar esos archivos tú mismo.

## Reglas

1. **Nunca edites a mano** `.mcp.json`, `~/.claude.json`, `.codex/config.toml`,
   `opencode.json`, `~/.hermes/config.yaml` ni las carpetas de skills
   (`.claude/skills`, `.agents/skills`, …). Hazlo con `pegboard`.
2. **Primero el plan, luego aplicar.** Corre el comando con `--json` sin `-y`
   (o `pegboard plan --json`), resume al usuario qué va a cambiar y solo
   después repite con `-y`.
3. **Conflictos (`"action": "conflict"`): pregunta.** Significa que alguien
   editó a mano algo que pegboard maneja, o que ya existe algo con ese nombre
   que pegboard no creó. Explica el `reason` y pregunta antes de usar
   `--force`.
4. **Nunca escribas secretos en claro.** Usa referencias: `${env:NOMBRE}` o
   `${file:~/.secrets/archivo}`. Si el plan trae `warnings` de "secreto en
   texto plano", díselo al usuario: ese agente no admite referencias.

## Qué comando usar

¿Hay un `pegboard.yaml` en el proyecto (o en `~/.config/pegboard/`)?

- **Sí → declarativo.** Edita el stack y aplica:
  ```
  pegboard add mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
  pegboard add mcp notion=https://mcp.notion.com/mcp --agent claude-code
  pegboard add skill vercel-labs/agent-skills@web-design-guidelines
  pegboard remove mcp notion
  pegboard plan --json        # muestra el resumen al usuario
  pegboard apply -y --json
  ```
- **No → ad-hoc.** Instala ya, sin YAML:
  ```
  pegboard install mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}' --json -y
  pegboard install skill mattpocock/skills@tdd -y
  pegboard uninstall mcp github -y
  ```
  Agrega `--save` si el usuario quiere que quede declarado.

Sin `--agent`, todo va a todos los agentes detectados; repite `--agent` para
limitar (`claude-code`, `codex`, `opencode`, `hermes`, `hermes:<perfil>`).
`--scope global` es para el usuario; `--scope project` para la carpeta actual.

## Fuentes de skills

`owner/repo` (todas las skills del repo), `owner/repo@skill` (una),
`github.com/owner/repo//ruta?ref=v1`, una URL git, `./ruta` local o `pegboard`
(esta skill).

## Para revisar

```
pegboard agents --json          # agentes detectados y sus rutas
pegboard drift --json           # ¿alguien cambió a mano lo que maneja pegboard?
pegboard update                 # nuevas versiones de skills (muestra el diff)
```

Códigos de salida: `0` bien, `1` error, `2` hay cambios o drift (con
`--exit-code`).
