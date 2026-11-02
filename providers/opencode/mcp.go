package opencode

import (
	"regexp"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/secrets"
)

func (Provider) MCPFile(t agent.Target) native.File {
	return native.File{
		Path:      t.Paths.MCPs,
		Format:    native.JSON,
		Container: "mcp",
		Skeleton:  native.NewObj().Set("$schema", "https://opencode.ai/config.json"),
	}
}

// opencode substitutes {env:X} and {file:path} in any string in its config.
// Paths are written absolute: opencode would resolve them from its own file.
func (Provider) EncodeMCP(m mcp.MCP, enc *agent.Encoder) (*native.Obj, error) {
	enc.Native = func(r secrets.Ref) (string, bool) {
		switch r.Kind {
		case "env":
			return "{env:" + r.Arg + "}", true
		case "file":
			return "{file:" + enc.Resolver.FilePath(r.Arg) + "}", true
		}
		return "", false
	}
	o := native.NewObj()
	if m.Remote() {
		o.Set("type", "remote").
			Set("url", enc.S(m.URL, "url")).
			SetIf("headers", enc.M(m.Headers, "headers"))
	} else {
		cmd := append([]string{m.Command}, m.Args...)
		o.Set("type", "local").
			Set("command", enc.L(cmd, "command")).
			SetIf("environment", enc.M(m.Env, "environment"))
	}
	return o, enc.Err()
}

var refRe = regexp.MustCompile(`\{(env|file):([^}]*)\}`)

func toRef(s string) string { return refRe.ReplaceAllString(s, "$${$1:$2}") }

func toRefMap(m map[string]string) map[string]string {
	for k, v := range m {
		m[k] = toRef(v)
	}
	return m
}

func (Provider) DecodeMCP(v any) (mcp.MCP, error) {
	e, _ := v.(map[string]any)
	m := mcp.MCP{URL: toRef(native.Str(e, "url")), Headers: toRefMap(native.StrMap(e, "headers"))}
	if cmd := native.Strs(e, "command"); len(cmd) > 0 {
		for i := range cmd {
			cmd[i] = toRef(cmd[i])
		}
		m.Command, m.Args = cmd[0], cmd[1:]
		if len(m.Args) == 0 {
			m.Args = nil
		}
	}
	m.Env = toRefMap(native.StrMap(e, "environment"))
	return m, m.Validate()
}
