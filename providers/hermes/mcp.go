package hermes

import (
	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
)

func (Provider) MCPFile(t agent.Target) native.File {
	return native.File{Path: t.Paths.MCPs, Format: native.YAML, Container: "mcp_servers"}
}

// Hermes has (as far as we know) no variable references in config.yaml:
// secrets are resolved and the plan warns that they remain in plain text.
func (Provider) EncodeMCP(m mcp.MCP, enc *agent.Encoder) (*native.Obj, error) {
	o := native.NewObj()
	if m.Remote() {
		o.Set("url", enc.S(m.URL, "url")).SetIf("headers", enc.M(m.Headers, "headers"))
	} else {
		o.Set("command", enc.S(m.Command, "command")).
			SetIf("args", enc.L(m.Args, "args")).
			SetIf("env", enc.M(m.Env, "env"))
	}
	return o, enc.Err()
}

func (Provider) DecodeMCP(v any) (mcp.MCP, error) {
	e, _ := v.(map[string]any)
	m := mcp.MCP{
		Command: native.Str(e, "command"),
		Args:    native.Strs(e, "args"),
		Env:     native.StrMap(e, "env"),
		URL:     native.Str(e, "url"),
		Headers: native.StrMap(e, "headers"),
	}
	return m, m.Validate()
}
