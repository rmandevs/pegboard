package gemini

import (
	"regexp"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
)

func (Provider) MCPFile(t agent.Target) native.File {
	return native.File{Path: t.Paths.MCPs, Format: native.JSON, Container: "mcpServers"}
}

// Gemini CLI expands ${VAR} in env and header values.
func nativeRef(r secrets.Ref) (string, bool) {
	if r.Kind == "env" {
		return "${" + r.Arg + "}", true
	}
	return "", false
}

func (Provider) EncodeMCP(m mcp.MCP, enc *agent.Encoder) (*native.Obj, error) {
	enc.Native = nativeRef
	o := native.NewObj()
	if m.Remote() {
		o.Set("httpUrl", enc.S(m.URL, "url")).
			SetIf("headers", enc.M(m.Headers, "headers"))
	} else {
		o.Set("command", enc.S(m.Command, "command")).
			SetIf("args", enc.L(m.Args, "args")).
			SetIf("env", enc.M(m.Env, "env"))
	}
	return o, enc.Err()
}

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func toRef(s string) string { return varRe.ReplaceAllString(s, "$${env:$1}") }

func toRefs(l []string) []string {
	for i := range l {
		l[i] = toRef(l[i])
	}
	return l
}

func toRefMap(m map[string]string) map[string]string {
	for k, v := range m {
		m[k] = toRef(v)
	}
	return m
}

func (Provider) DecodeMCP(v any) (mcp.MCP, error) {
	e, _ := v.(map[string]any)
	url := native.Str(e, "httpUrl")
	if url == "" {
		url = native.Str(e, "url")
	}
	m := mcp.MCP{
		Command: toRef(native.Str(e, "command")),
		Args:    toRefs(native.Strs(e, "args")),
		Env:     toRefMap(native.StrMap(e, "env")),
		URL:     toRef(url),
		Headers: toRefMap(native.StrMap(e, "headers")),
	}
	return m, m.Validate()
}
