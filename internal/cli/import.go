package cli

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/native"
	"github.com/rmandevs/agents-casc/internal/secrets"
	"github.com/rmandevs/agents-casc/internal/stack"
	"github.com/rmandevs/agents-casc/internal/state"
)

// found es un MCP ajeno encontrado en un target al importar.
type found struct {
	target agent.Target
	file   string
	obs    any
	mcp    mcp.MCP
	masked []string
}

func newImportCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "Adopta los MCPs que ya tienen los agentes en pegboard.yaml",
		Long: `Lee la config de cada agente, agrega a pegboard.yaml los MCPs que pegboard
no gestiona todavía y los registra en el estado. Los secretos en claro
(env y headers) se reemplazan por referencias ${env:…}: exporta esas
variables antes de apply.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			scopes, provs, err := opts.selection()
			if err != nil {
				return err
			}
			ctxs, err := opts.load(env, scopes, provs)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			total := 0
			for _, c := range ctxs {
				n, err := importScope(c, out, env)
				if err != nil {
					return err
				}
				total += n
			}
			if total == 0 {
				fmt.Fprintln(out, "No hay MCPs nuevos que importar.")
			} else {
				fmt.Fprintln(out, "Revisa con `pegboard plan`.")
			}
			return nil
		},
	}
}

func importScope(c *scopeCtx, out interface{ Write([]byte) (int, error) }, env agent.Env) (int, error) {
	byName := map[string][]found{}
	var vars []string
	for _, t := range c.targets {
		p, ok := mcpProvider(t.Agent)
		if !ok {
			continue
		}
		f := p.MCPFile(t)
		d, err := native.Open(f)
		if err != nil {
			return 0, err
		}
		entries, err := d.Entries()
		if err != nil {
			return 0, fmt.Errorf("%s: %w", f.Path, err)
		}
		for name, obs := range entries {
			if _, managed := c.state.Get(t.ID(), name); managed {
				continue
			}
			m, err := p.DecodeMCP(obs)
			if err != nil {
				fmt.Fprintf(out, "aviso: %s · %s: mcp %s no se pudo leer (%v); se omite\n", t.ID(), c.scope, name, err)
				continue
			}
			m, literals, vs := toReferences(name, m)
			vars = append(vars, vs...)
			byName[name] = append(byName[name], found{
				target: t, file: f.Path, obs: obs, mcp: m, masked: maskLiterals(obs, literals),
			})
		}
	}
	if len(byName) == 0 {
		return 0, nil
	}
	var names []string
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	var agents []string
	for _, t := range c.targets {
		agents = appendUnique(agents, t.Agent)
	}
	path := stack.Path(env, c.scope)
	ed, err := stack.Edit(path, agents)
	if err != nil {
		return 0, err
	}
	declared := map[string]mcp.MCP{}
	if c.stack != nil {
		declared = c.stack.MCPs
	}
	var imported []string
	for _, name := range names {
		all := byName[name]
		first := all[0]
		var same []found
		for _, fd := range all {
			if fd.mcp.Equal(first.mcp) {
				same = append(same, fd)
			} else {
				fmt.Fprintf(out, "aviso: mcp %s en %s difiere del de %s; se importa el de %s\n",
					name, fd.target.ID(), first.target.ID(), first.target.ID())
			}
		}
		m := first.mcp
		if d, ok := declared[name]; ok {
			if !d.Equal(m) {
				fmt.Fprintf(out, "aviso: mcp %s ya está declarado distinto en %s; se omite\n", name, stack.FileName)
				continue
			}
		} else {
			if len(same) < len(c.targets) {
				for _, fd := range same {
					m.Agents = append(m.Agents, fd.target.ID())
				}
			}
			if err := ed.Set(name, m); err != nil {
				return 0, err
			}
		}
		for _, fd := range same {
			c.state.Set(fd.target.ID(), name, state.Entry{
				File: fd.file, Hash: native.Hash(fd.obs, fd.masked), Masked: fd.masked, Origin: state.Declared,
			})
		}
		imported = append(imported, fmt.Sprintf("%s (%s)", name, targetIDs(targetsOf(same))))
	}
	if len(imported) == 0 {
		return 0, nil
	}
	if err := ed.Save(); err != nil {
		return 0, err
	}
	if err := c.state.Save(); err != nil {
		return 0, err
	}
	fmt.Fprintf(out, "Importado a %s:\n", env.Pretty(path))
	for _, s := range imported {
		fmt.Fprintf(out, "  + mcp %s\n", s)
	}
	if len(vars) > 0 {
		sort.Strings(vars)
		vars = compact(vars)
		fmt.Fprintf(out, "Los secretos quedaron como ${env:…}; exporta: %s\n", strings.Join(vars, ", "))
	}
	return len(imported), nil
}

func targetsOf(fs []found) []agent.Target {
	out := make([]agent.Target, len(fs))
	for i, f := range fs {
		out[i] = f.target
	}
	return out
}

func compact(l []string) []string {
	var out []string
	for i, s := range l {
		if i == 0 || s != l[i-1] {
			out = append(out, s)
		}
	}
	return out
}

var unsafeVar = regexp.MustCompile(`[^A-Z0-9_]+`)

// toReferences cambia los valores en claro de env y headers por referencias
// ${env:…}. Devuelve los valores originales (para enmascararlos en el
// estado) y las variables que el usuario debe exportar.
func toReferences(name string, m mcp.MCP) (mcp.MCP, []string, []string) {
	var literals, vars []string
	if len(m.Env) > 0 {
		env := map[string]string{}
		for k, v := range m.Env {
			if len(secrets.Refs(v)) > 0 || v == "" {
				env[k] = v
				continue
			}
			literals = append(literals, v)
			vars = append(vars, k)
			env[k] = "${env:" + k + "}"
		}
		m.Env = env
	}
	if len(m.Headers) > 0 {
		hdr := map[string]string{}
		for h, v := range m.Headers {
			if len(secrets.Refs(v)) > 0 || v == "" {
				hdr[h] = v
				continue
			}
			prefix := ""
			if strings.HasPrefix(v, "Bearer ") {
				prefix = "Bearer "
			}
			literals = append(literals, strings.TrimPrefix(v, prefix))
			vr := strings.Trim(unsafeVar.ReplaceAllString(strings.ToUpper(name+"_"+h), "_"), "_")
			if prefix != "" {
				vr = strings.Trim(unsafeVar.ReplaceAllString(strings.ToUpper(name), "_"), "_") + "_TOKEN"
			}
			vars = append(vars, vr)
			hdr[h] = prefix + "${env:" + vr + "}"
		}
		m.Headers = hdr
	}
	return m, literals, vars
}

// maskLiterals devuelve las rutas de obs cuyos strings contienen alguno de
// los secretos: así el hash del estado no depende de ellos.
func maskLiterals(obs any, literals []string) []string {
	if len(literals) == 0 {
		return nil
	}
	var out []string
	var walk func(v any, path []string)
	walk = func(v any, path []string) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				walk(c, append(append([]string{}, path...), k))
			}
		case []any:
			for i, c := range x {
				walk(c, append(append([]string{}, path...), fmt.Sprint(i)))
			}
		case string:
			for _, l := range literals {
				if l != "" && strings.Contains(x, l) {
					out = append(out, native.Pointer(path...))
					return
				}
			}
		}
	}
	walk(native.Plain(obs), nil)
	sort.Strings(out)
	return out
}
