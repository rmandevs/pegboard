package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/engine"
	"github.com/rmandevs/agents-casc/internal/mcp"
	"github.com/rmandevs/agents-casc/internal/secrets"
	"github.com/rmandevs/agents-casc/internal/skills"
	"github.com/rmandevs/agents-casc/internal/stack"
	"github.com/rmandevs/agents-casc/internal/state"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// adhocScope aplica la regla 5 de SPEC §4.1: project si hay pegboard.yaml o
// un repo git en la carpeta; si no, global.
func (o *options) adhocScope(env agent.Env) (agent.Scope, error) {
	if o.scope != "" {
		s, ok := agent.ParseScope(o.scope)
		if !ok {
			return "", fmt.Errorf("--scope %q inválido: usa global o project", o.scope)
		}
		return s[0], nil
	}
	if agent.Exists(stack.Path(env, agent.Project)) || agent.Exists(filepath.Join(env.ProjectDir, ".git")) {
		return agent.Project, nil
	}
	return agent.Global, nil
}

func newInstallCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	f := &applyFlags{}
	var envs, headers []string
	var save bool
	cmd := &cobra.Command{
		Use:   "install [mcp <nombre>=<comando|url>…] [skill <fuente>…]",
		Short: "Instala ya, sin pegboard.yaml (modo ad-hoc)",
		Long: `Instala MCPs y skills en los agentes detectados, con el mismo plan, estado y
respaldos que apply, sin tocar pegboard.yaml (salvo con --save).

Fuentes de skills (las mismas que npx skills): owner/repo, owner/repo@skill,
github.com/owner/repo//ruta?ref=v1, una URL git, ./ruta local o pegboard.`,
		Example: `  pegboard install mcp github='npx -y @modelcontextprotocol/server-github' \
                   --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
  pegboard install mcp notion=https://mcp.notion.com/mcp --agent claude-code
  pegboard install skill vercel-labs/agent-skills@web-design-guidelines`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mcps, order, sources, err := parseInstall(args, envs, headers)
			if err != nil {
				return err
			}
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			scope, err := opts.adhocScope(env)
			if err != nil {
				return err
			}
			_, provs, err := opts.selection()
			if err != nil {
				return err
			}
			ctxs, err := opts.load(env, []agent.Scope{scope}, provs)
			if err != nil {
				return err
			}
			c := ctxs[0]
			if len(c.targets) == 0 {
				return fmt.Errorf("no hay agentes detectados en alcance %s", scope)
			}
			origin := state.Adhoc
			if save {
				origin = state.Declared
			}
			es := engine.Scope{Scope: scope, State: c.state, Resolver: c.resolver()}
			// Con --save las skills quedan fijadas en el lock del stack.
			var lock *skills.Lock
			if save && len(sources) > 0 {
				if lock, err = skills.LoadLock(filepath.Join(filepath.Dir(stack.Path(env, scope)), skills.LockFile)); err != nil {
					return err
				}
			}
			resolved, err := resolveSkills(c.skillResolver(lock), sources)
			if err != nil {
				return err
			}
			skillWants := map[string]*engine.SkillWant{}
			for _, sk := range resolved {
				for _, d := range c.state.SkillDirs() {
					if e, ok := c.state.Skill(d, sk.Name); ok && e.Origin == state.Declared && !save {
						return fmt.Errorf("la skill %s está declarada en %s; cámbiala ahí o usa --save", sk.Name, stack.FileName)
					}
				}
				skillWants[sk.Name] = &engine.SkillWant{Skill: sk, Origin: origin}
			}
			if len(skillWants) > 0 {
				es.Skills = c.skillDirs(func(agent.Target) map[string]*engine.SkillWant { return skillWants }, nil)
			}
			for _, t := range c.targets {
				if len(order) == 0 {
					break
				}
				p, ok := mcpProvider(t)
				if !ok {
					continue
				}
				wants := map[string]*engine.Want{}
				for _, name := range order {
					if e, ok := c.state.Get(t.ID(), name); ok && e.Origin == state.Declared && !save {
						return fmt.Errorf("mcp %s está declarado en %s; cámbialo ahí o usa --save", name, stack.FileName)
					}
					wants[name] = &engine.Want{MCP: mcps[name], Origin: origin}
				}
				es.Targets = append(es.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
			}
			var after func() error
			if save {
				after = func() error {
					var agents []string
					for _, t := range providersDetectAll(env, scope) {
						agents = appendUnique(agents, t.Agent)
					}
					ed, err := stack.Edit(stack.Path(env, scope), agents)
					if err != nil {
						return err
					}
					for _, name := range order {
						m := mcps[name]
						m.Agents = opts.agents
						if err := ed.Set(name, m); err != nil {
							return err
						}
					}
					for _, src := range sources {
						if _, err := ed.AddSkill(src); err != nil {
							return err
						}
					}
					if err := ed.Save(); err != nil {
						return err
					}
					if lock != nil {
						if err := lock.Save(); err != nil {
							return err
						}
					}
					if !opts.json {
						fmt.Fprintf(cmd.OutOrStdout(), "Guardado en %s.\n", env.Pretty(stack.Path(env, scope)))
					}
					return nil
				}
			}
			return execute(cmd, opts, env, []engine.Scope{es}, nil, f, true, after)
		},
	}
	cmd.Flags().StringArrayVar(&envs, "env", nil, "variable para los MCPs stdio: K=V (repetible; V puede ser ${env:X})")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "header para los MCPs http: K=V (repetible)")
	cmd.Flags().BoolVar(&save, "save", false, "agregarlo también a pegboard.yaml")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "no pedir confirmación")
	cmd.Flags().BoolVar(&f.force, "force", false, "sobrescribir lo que ya exista")
	return cmd
}

func appendUnique(l []string, s string) []string {
	for _, x := range l {
		if x == s {
			return l
		}
	}
	return append(l, s)
}

// parseInstall lee `mcp a=… b=… skill x y mcp c=…`. Los stacks remotos
// llegan en la fase 3.
func parseInstall(args, envs, headers []string) (map[string]mcp.MCP, []string, []string, error) {
	mcps, order, sources, err := parseInstallArgs(args, envs, headers)
	if err == nil && len(order) == 0 && len(sources) == 0 {
		err = errors.New("nada que instalar")
	}
	return mcps, order, sources, err
}

func parseInstallArgs(args, envs, headers []string) (map[string]mcp.MCP, []string, []string, error) {
	var sources []string
	env, err := kvs("--env", envs)
	if err != nil {
		return nil, nil, nil, err
	}
	hdr, err := kvs("--header", headers)
	if err != nil {
		return nil, nil, nil, err
	}
	out := map[string]mcp.MCP{}
	var order []string
	usedEnv, usedHdr := false, false
	for i := 0; i < len(args); {
		switch kw := args[i]; {
		case kw == "mcp":
			i++
			n := 0
			for ; i < len(args) && strings.Contains(args[i], "="); i++ {
				name, val, _ := strings.Cut(args[i], "=")
				if !nameRe.MatchString(name) {
					return nil, nil, nil, fmt.Errorf("nombre de mcp inválido %q", name)
				}
				if _, dup := out[name]; dup {
					return nil, nil, nil, fmt.Errorf("mcp %s repetido", name)
				}
				var m mcp.MCP
				if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
					m = mcp.MCP{URL: val, Headers: hdr}
					usedHdr = usedHdr || hdr != nil
				} else {
					words, err := splitWords(val)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("mcp %s: %w", name, err)
					}
					if len(words) == 0 {
						return nil, nil, nil, fmt.Errorf("mcp %s: falta el comando", name)
					}
					m = mcp.MCP{Command: words[0], Args: words[1:], Env: env}
					if len(m.Args) == 0 {
						m.Args = nil
					}
					usedEnv = usedEnv || env != nil
				}
				for _, v := range append(append([]string{m.Command, m.URL}, m.Args...), values(m.Env, m.Headers)...) {
					if err := secrets.Check(v); err != nil {
						return nil, nil, nil, fmt.Errorf("mcp %s: %w", name, err)
					}
				}
				out[name] = m
				order = append(order, name)
				n++
			}
			if n == 0 {
				return nil, nil, nil, errors.New("mcp necesita nombre=comando o nombre=url")
			}
		case kw == "skill":
			i++
			n := 0
			for ; i < len(args) && args[i] != "mcp" && args[i] != "skill"; i++ {
				if _, err := skills.Parse(args[i]); err != nil {
					return nil, nil, nil, err
				}
				sources = append(sources, args[i])
				n++
			}
			if n == 0 {
				return nil, nil, nil, errors.New("skill necesita una fuente (owner/repo, owner/repo@skill, ./ruta…)")
			}
		case strings.Contains(kw, "/"):
			return nil, nil, nil, errors.New("instalar stacks remotos llega en la fase 3")
		default:
			return nil, nil, nil, fmt.Errorf("esperaba `mcp nombre=…`, no %q", kw)
		}
	}
	if env != nil && !usedEnv {
		return nil, nil, nil, errors.New("--env solo aplica a MCPs stdio (nombre=comando)")
	}
	if hdr != nil && !usedHdr {
		return nil, nil, nil, errors.New("--header solo aplica a MCPs http (nombre=url)")
	}
	return out, order, sources, nil
}

func values(ms ...map[string]string) []string {
	var out []string
	for _, m := range ms {
		for _, v := range m {
			out = append(out, v)
		}
	}
	return out
}

func kvs(flag string, l []string) (map[string]string, error) {
	if len(l) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, s := range l {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("%s espera K=V, no %q", flag, s)
		}
		out[k] = v
	}
	return out, nil
}

// splitWords separa un comando como lo haría un shell simple: espacios,
// comillas simples y dobles, y \ para escapar.
func splitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || esc {
		return nil, errors.New("comillas sin cerrar")
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

func newUninstallCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "uninstall [mcp <nombre>…] [skill <nombre>…]",
		Short: "Quita algo instalado ad-hoc",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			byKind := map[string][]string{}
			kind := ""
			for _, a := range args {
				if a == "mcp" || a == "skill" {
					kind = a
					continue
				}
				if kind == "" {
					return fmt.Errorf("esperaba `uninstall mcp|skill nombre…`, no %q", a)
				}
				byKind[kind] = append(byKind[kind], a)
			}
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			scope, err := opts.adhocScope(env)
			if err != nil {
				return err
			}
			_, provs, err := opts.selection()
			if err != nil {
				return err
			}
			ctxs, err := opts.load(env, []agent.Scope{scope}, provs)
			if err != nil {
				return err
			}
			c := ctxs[0]
			es := engine.Scope{Scope: scope, State: c.state, Resolver: c.resolver()}
			found := map[string]bool{}
			for _, t := range c.targets {
				p, ok := mcpProvider(t)
				if !ok {
					continue
				}
				wants := map[string]*engine.Want{}
				for _, name := range byKind["mcp"] {
					e, ok := c.state.Get(t.ID(), name)
					if !ok {
						continue
					}
					if e.Origin == state.Declared {
						return fmt.Errorf("mcp %s está declarado en %s: quítalo con `pegboard remove mcp %s` y corre apply", name, stack.FileName, name)
					}
					wants[name] = nil
					found["mcp "+name] = true
				}
				if len(wants) > 0 {
					es.Targets = append(es.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
				}
			}
			var declaredErr error
			es.Skills = c.skillDirs(func(t agent.Target) map[string]*engine.SkillWant {
				out := map[string]*engine.SkillWant{}
				for _, name := range byKind["skill"] {
					e, ok := c.state.Skill(t.Paths.Skills, name)
					if !ok {
						continue
					}
					if e.Origin == state.Declared {
						declaredErr = fmt.Errorf("la skill %s está declarada en %s: quítala con `pegboard remove skill %s` y corre apply", name, stack.FileName, name)
					}
					out[name] = nil
					found["skill "+name] = true
				}
				return out
			}, nil)
			if declaredErr != nil {
				return declaredErr
			}
			var missing []string
			for k, names := range byKind {
				for _, n := range names {
					if !found[k+" "+n] {
						missing = append(missing, k+" "+n)
					}
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				return fmt.Errorf("pegboard no instaló en alcance %s: %s", scope, strings.Join(missing, ", "))
			}
			return execute(cmd, opts, env, []engine.Scope{es}, nil, f, true, nil)
		},
	}
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "no pedir confirmación")
	cmd.Flags().BoolVar(&f.force, "force", false, "quitar aunque se haya editado a mano")
	return cmd
}
