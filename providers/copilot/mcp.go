package copilot

import (
	"regexp"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/secrets"
)

// MCPFile: ~/.copilot/mcp-config.json, under mcpServers.
func (Provider) MCPFile(t agent.Target) native.File {
	return native.File{Path: t.Paths.MCPs, Format: native.JSON, Container: "mcpServers"}
}

// Copilot CLI expands ${VAR} in its MCP config.
func nativeRef(r secrets.Ref) (string, bool) {
	if r.Kind == "env" {
		return "${" + r.Arg + "}", true
	}
	return "", false
}

// EncodeMCP writes "type": "local" or "http" and "tools": ["*"]: Copilot
// CLI only exposes the tools an entry lists.
func (Provider) EncodeMCP(m mcp.MCP, enc *agent.Encoder) (*native.Obj, error) {
	enc.Native = nativeRef
	o := native.NewObj()
	if m.Remote() {
		o.Set("type", "http").
			Set("url", enc.S(m.URL, "url")).
			SetIf("headers", enc.M(m.Headers, "headers"))
	} else {
		o.Set("type", "local").
			Set("command", enc.S(m.Command, "command")).
			SetIf("args", enc.L(m.Args, "args")).
			SetIf("env", enc.M(m.Env, "env"))
	}
	o.Set("tools", []string{"*"})
	return o, enc.Err()
}

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func toRef(s string) string { return varRe.ReplaceAllString(s, "$${env:$1}") }

func (Provider) DecodeMCP(v any) (mcp.MCP, error) {
	e, _ := v.(map[string]any)
	m := mcp.MCP{
		Command: toRef(native.Str(e, "command")),
		Args:    native.Strs(e, "args"),
		Env:     native.StrMap(e, "env"),
		URL:     toRef(native.Str(e, "url")),
		Headers: native.StrMap(e, "headers"),
	}
	for i := range m.Args {
		m.Args[i] = toRef(m.Args[i])
	}
	for k, x := range m.Env {
		m.Env[k] = toRef(x)
	}
	for k, x := range m.Headers {
		m.Headers[k] = toRef(x)
	}
	return m, m.Validate()
}
