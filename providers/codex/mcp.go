package codex

import (
	"sort"
	"strings"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
)

func (Provider) MCPFile(t agent.Target) native.File {
	return native.File{Path: t.Paths.MCPs, Format: native.TOML, Container: "mcp_servers"}
}

// Codex no interpola variables en config.toml, pero tiene llaves para
// tomarlas del entorno:
//   - env_vars = ["K"]: reenvía K al servidor stdio (para K: ${env:K}).
//   - bearer_token_env_var = "K": Authorization: Bearer ${env:K}.
//   - env_http_headers = { H = "K" }: H: ${env:K}.
//
// Cualquier otra referencia se resuelve y queda en claro.
func (Provider) EncodeMCP(m mcp.MCP, enc *agent.Encoder) (*native.Obj, error) {
	o := native.NewObj()
	if m.Remote() {
		o.Set("url", enc.S(m.URL, "url"))
		literal := map[string]string{}
		fromEnv := map[string]string{}
		for h, v := range m.Headers {
			if ref, ok := secrets.Whole(strings.TrimPrefix(v, "Bearer ")); ok && ref.Kind == "env" &&
				strings.EqualFold(h, "Authorization") && strings.HasPrefix(v, "Bearer ") {
				o.Set("bearer_token_env_var", ref.Arg)
			} else if ref, ok := secrets.Whole(v); ok && ref.Kind == "env" {
				fromEnv[h] = ref.Arg
			} else {
				literal[h] = v
			}
		}
		o.SetIf("http_headers", enc.M(literal, "http_headers"))
		o.SetIf("env_http_headers", native.StringMap(fromEnv))
		return o, enc.Err()
	}
	o.Set("command", enc.S(m.Command, "command")).SetIf("args", enc.L(m.Args, "args"))
	literal := map[string]string{}
	var passthrough []string
	for k, v := range m.Env {
		if ref, ok := secrets.Whole(v); ok && ref.Kind == "env" && ref.Arg == k {
			passthrough = append(passthrough, k)
		} else {
			literal[k] = v
		}
	}
	sort.Strings(passthrough)
	o.SetIf("env", enc.M(literal, "env")).SetIf("env_vars", passthrough)
	return o, enc.Err()
}

func (Provider) DecodeMCP(v any) (mcp.MCP, error) {
	e, _ := v.(map[string]any)
	m := mcp.MCP{
		Command: native.Str(e, "command"),
		Args:    native.Strs(e, "args"),
		Env:     native.StrMap(e, "env"),
		URL:     native.Str(e, "url"),
		Headers: native.StrMap(e, "http_headers"),
	}
	for _, k := range native.Strs(e, "env_vars") {
		if m.Env == nil {
			m.Env = map[string]string{}
		}
		m.Env[k] = "${env:" + k + "}"
	}
	if k := native.Str(e, "bearer_token_env_var"); k != "" {
		if m.Headers == nil {
			m.Headers = map[string]string{}
		}
		m.Headers["Authorization"] = "Bearer ${env:" + k + "}"
	}
	for h, k := range native.StrMap(e, "env_http_headers") {
		if m.Headers == nil {
			m.Headers = map[string]string{}
		}
		m.Headers[h] = "${env:" + k + "}"
	}
	return m, m.Validate()
}
