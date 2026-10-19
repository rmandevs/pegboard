package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/engine"
	"github.com/rmandevs/agents-casc/internal/skills"
	"github.com/rmandevs/agents-casc/internal/stack"
)

// editResult es la salida JSON de add y remove.
type editResult struct {
	File    string   `json:"file"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

func (o *options) editor(env agent.Env) (*stack.Editor, error) {
	scope, err := o.adhocScope(env)
	if err != nil {
		return nil, err
	}
	var agents []string
	for _, t := range providersDetectAll(env, scope) {
		agents = appendUnique(agents, t.Agent)
	}
	return stack.Edit(stack.Path(env, scope), agents)
}

func writeEdit(cmd *cobra.Command, opts *options, env agent.Env, r editResult) error {
	if r.Added == nil {
		r.Added = []string{}
	}
	if r.Removed == nil {
		r.Removed = []string{}
	}
	if opts.json {
		return engine.WriteJSON(cmd.OutOrStdout(), r)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s:\n", env.Pretty(r.File))
	for _, a := range r.Added {
		fmt.Fprintf(out, "  + %s\n", a)
	}
	for _, d := range r.Removed {
		fmt.Fprintf(out, "  - %s\n", d)
	}
	fmt.Fprintln(out, "Revisa con `pegboard plan` y aplica con `pegboard apply`.")
	return nil
}

func newAddCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var envs, headers []string
	cmd := &cobra.Command{
		Use:   "add [mcp <nombre>=<comando|url>…] [skill <fuente>…]",
		Short: "Agrega MCPs o skills a pegboard.yaml (no aplica)",
		Example: `  pegboard add mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
  pegboard add skill mattpocock/skills@tdd ./skills/mia`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mcps, order, sources, err := parseInstall(args, envs, headers)
			if err != nil {
				return err
			}
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			if _, _, err := opts.selection(); err != nil {
				return err
			}
			ed, err := opts.editor(env)
			if err != nil {
				return err
			}
			r := editResult{File: ed.Path()}
			for _, name := range order {
				m := mcps[name]
				m.Agents = opts.agents
				if err := ed.Set(name, m); err != nil {
					return err
				}
				r.Added = append(r.Added, "mcp "+name)
			}
			for _, src := range sources {
				added, err := ed.AddSkill(src)
				if err != nil {
					return err
				}
				if added {
					r.Added = append(r.Added, "skill "+src)
				}
			}
			if err := ed.Save(); err != nil {
				return err
			}
			return writeEdit(cmd, opts, env, r)
		},
	}
	cmd.Flags().StringArrayVar(&envs, "env", nil, "variable para los MCPs stdio: K=V (repetible)")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "header para los MCPs http: K=V (repetible)")
	return cmd
}

func newRemoveCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "remove [mcp <nombre>…] [skill <fuente|nombre>…]",
		Short: "Quita MCPs o skills de pegboard.yaml (no aplica)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			ed, err := opts.editor(env)
			if err != nil {
				return err
			}
			r := editResult{File: ed.Path()}
			kind := ""
			var missing []string
			for _, a := range args {
				switch {
				case a == "mcp" || a == "skill":
					kind = a
				case kind == "":
					return fmt.Errorf("esperaba `remove mcp|skill nombre…`, no %q", a)
				case kind == "mcp":
					if !ed.HasMCP(a) {
						missing = append(missing, "mcp "+a)
						continue
					}
					if err := ed.Delete(a); err != nil {
						return err
					}
					r.Removed = append(r.Removed, "mcp "+a)
				default:
					gone, err := ed.RemoveSkill(a)
					if err != nil {
						return err
					}
					if len(gone) == 0 {
						missing = append(missing, "skill "+a)
					}
					for _, g := range gone {
						r.Removed = append(r.Removed, "skill "+g)
					}
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("%s no declara: %s", env.Pretty(ed.Path()), strings.Join(missing, ", "))
			}
			if err := ed.Save(); err != nil {
				return err
			}
			return writeEdit(cmd, opts, env, r)
		},
	}
}

func newAdoptCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "adopt",
		Short: "Pasa a pegboard.yaml todo lo instalado ad-hoc",
		Args:  cobra.NoArgs,
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
			total := 0
			for _, c := range ctxs {
				n, err := importScope(c, cmd.OutOrStdout(), env, true)
				if err != nil {
					return err
				}
				total += n
			}
			if total == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No hay nada instalado ad-hoc.")
			}
			return nil
		},
	}
}

func newUpdateCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "update [fuente|nombre…]",
		Short: "Re-resuelve las skills y actualiza pegboard.lock (muestra el diff)",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			ctxs, err := opts.declarativeScopes(env)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			var pending []*skills.Lock
			changes := 0
			for _, c := range ctxs {
				if c.stack == nil || len(c.stack.Skills) == 0 {
					continue
				}
				r := c.skillResolver(c.lock)
				r.Update = true
				for _, src := range c.stack.Skills {
					if !matchesAny(src, args) {
						continue
					}
					old := c.lock.Sources[src]
					parsed, err := skills.Parse(src)
					if err != nil {
						return err
					}
					if parsed.Kind != skills.Git {
						continue
					}
					got, err := r.Resolve(parsed)
					if err != nil {
						return err
					}
					for _, sk := range got {
						prev, had := old.Skills[sk.Name]
						switch {
						case !had:
							fmt.Fprintf(out, "+ skill %s (%s) %s\n", sk.Name, src, engine.Short(sk.Hash))
						case prev != sk.Hash:
							fmt.Fprintf(out, "~ skill %s (%s) %s → %s%s\n", sk.Name, src, engine.Short(prev), engine.Short(sk.Hash),
								fileDiff(r, prev, sk))
						default:
							continue
						}
						changes++
					}
					for name := range old.Skills {
						if !hasSkill(got, name) {
							fmt.Fprintf(out, "- skill %s (%s)\n", name, src)
							changes++
						}
					}
				}
				if c.lock.Changed() {
					pending = append(pending, c.lock)
				}
			}
			if changes == 0 {
				for _, l := range pending {
					if err := l.Save(); err != nil {
						return err
					}
				}
				fmt.Fprintln(out, "Las skills ya están al día.")
				return nil
			}
			if !yes && !confirm(cmd.InOrStdin(), out) {
				fmt.Fprintln(out, "Cancelado; pegboard.lock no cambió.")
				return nil
			}
			for _, l := range pending {
				if err := l.Save(); err != nil {
					return err
				}
				fmt.Fprintf(out, "Escrito %s. Corre `pegboard apply` para instalarlo.\n", env.Pretty(l.Path()))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "no pedir confirmación")
	return cmd
}

func matchesAny(source string, args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, a := range args {
		if stack.SkillMatches(source, a) {
			return true
		}
	}
	return false
}

func hasSkill(l []skills.Skill, name string) bool {
	for _, s := range l {
		if s.Name == name {
			return true
		}
	}
	return false
}

// fileDiff resume qué archivos cambiaron entre la versión del caché y la nueva.
func fileDiff(r *skills.Resolver, prevHash string, sk skills.Skill) string {
	before, err1 := skills.Files(r.CachedDir(prevHash))
	after, err2 := skills.Files(sk.Dir)
	if err1 != nil || err2 != nil {
		return " (la versión anterior no está en el caché)"
	}
	var parts []string
	for f, h := range after {
		if old, ok := before[f]; !ok {
			parts = append(parts, "+"+f)
		} else if old != h {
			parts = append(parts, "~"+f)
		}
	}
	for f := range before {
		if _, ok := after[f]; !ok {
			parts = append(parts, "-"+f)
		}
	}
	sort.Strings(parts)
	return ": " + strings.Join(parts, " ")
}

func newSkillCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "La skill propia de pegboard",
	}
	var yes bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Instala la skill de pegboard en los agentes (default: alcance global)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.scope == "" {
				opts.scope = string(agent.Global)
			}
			inst := newInstallCmd(opts, newEnv)
			if yes {
				if err := inst.Flags().Set("yes", "true"); err != nil {
					return err
				}
			}
			inst.SetIn(cmd.InOrStdin())
			inst.SetOut(cmd.OutOrStdout())
			inst.SetErr(cmd.ErrOrStderr())
			return inst.RunE(inst, []string{"skill", "pegboard"})
		},
	}
	install.Flags().BoolVarP(&yes, "yes", "y", false, "no pedir confirmación")
	cmd.AddCommand(install)
	return cmd
}
