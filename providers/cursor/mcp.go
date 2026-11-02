package cursor

import (
	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/secrets"
)

func (Provider) MCPFile(t agent.Target) native.File {
	return native.File{Path: t.Paths.MCPs, Format: native.JSON, Container: "mcpServers"}
}

// Cursor interpolates environment variables with ${env:NAME}.
func nativeRef(r secrets.Ref) (string, bool) {
	if r.Kind == "env" {
		return "${env:" + r.Arg + "}", true
	}
	return "", false
}

func (Provider) EncodeMCP(m mcp.MCP, enc *agent.Encoder) (*native.Obj, error) {
	enc.Native = nativeRef
	o := native.NewObj()
	if m.Remote() {
		o.Set("url", enc.S(m.URL, "url")).
			SetIf("headers", enc.M(m.Headers, "headers"))
	} else {
		o.Set("type", "stdio").
			Set("command", enc.S(m.Command, "command")).
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
