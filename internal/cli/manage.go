package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/skills"
	"github.com/rmandevs/pegboard/internal/stack"
)

// editResult is the JSON output of add and remove.
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
	fmt.Fprintln(out, "Review with `pegboard plan` and apply with `pegboard apply`.")
	return nil
}

func newAddCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var envs, headers []string
	cmd := &cobra.Command{
		Use:   "add [mcp <name>=<command|url>…] [skill <source>…]",
		Short: "Add MCPs or skills to pegboard.yaml (does not apply)",
		Example: `  pegboard add mcp github='npx -y @modelcontextprotocol/server-github' --env GITHUB_TOKEN='${env:GITHUB_TOKEN}'
  pegboard add skill mattpocock/skills@tdd ./skills/mine`,
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
				added, err := ed.AddSkill(src, opts.agents)
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
	cmd.Flags().StringArrayVar(&envs, "env", nil, "variable for stdio MCPs: K=V (repeatable)")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "header for http MCPs: K=V (repeatable)")
	return cmd
}

func newRemoveCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "remove [mcp <name>…] [skill <source|name>…]",
		Short: "Remove MCPs or skills from pegboard.yaml (does not apply)",
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
					return fmt.Errorf("expected `remove mcp|skill name…`, not %q", a)
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
				return fmt.Errorf("%s does not declare: %s", env.Pretty(ed.Path()), strings.Join(missing, ", "))
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
		Short: "Move everything installed ad-hoc into pegboard.yaml",
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
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing is installed ad-hoc.")
			}
			return nil
		},
	}
}

func newUpdateCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "update [source|name…]",
		Short: "Re-resolve skills and update pegboard.lock (shows the diff)",
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
				if c.stack == nil {
					continue
				}
				r := c.skillResolver(c.lock)
				r.Update = true
				sources := c.stack.SkillSources()
				if len(c.stack.Include) > 0 && len(args) == 0 {
					n, merged, err := updateIncludes(out, c, r)
					if err != nil {
						return err
					}
					changes += n
					sources = merged.SkillSources()
				}
				for _, src := range sources {
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
				fmt.Fprintln(out, "Skills and includes are already up to date.")
				return nil
			}
			if !yes && !confirm(cmd.InOrStdin(), out) {
				fmt.Fprintln(out, "Canceled; pegboard.lock unchanged.")
				return nil
			}
			for _, l := range pending {
				if err := l.Save(); err != nil {
					return err
				}
				fmt.Fprintf(out, "Wrote %s. Run `pegboard apply` to install it.\n", env.Pretty(l.Path()))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

// updateIncludes re-resolves the remote includes and prints what changed:
// their content and, above all, the MCPs they bring (third-party code).
func updateIncludes(out io.Writer, c *scopeCtx, r *skills.Resolver) (int, *stack.Resolved, error) {
	before := map[string]skills.LockedInclude{}
	for k, v := range c.lock.Includes {
		before[k] = v
	}
	old, err := c.resolve()
	if err != nil {
		return 0, nil, err
	}
	fresh, err := c.stack.Resolve(func(in skills.Include, base string) (skills.Fetched, error) {
		return r.FetchInclude(in, base)
	})
	if err != nil {
		return 0, nil, err
	}
	changes := 0
	for _, in := range fresh.Includes {
		if !in.Remote || in.Hash == "" {
			continue
		}
		prev, had := before[in.Raw]
		switch {
		case !had || prev.Hash == "":
			fmt.Fprintf(out, "+ include %s %s\n", in.Raw, engine.Short(in.Hash))
		case prev.Hash != in.Hash:
			fmt.Fprintf(out, "~ include %s %s → %s\n", in.Raw, engine.Short(prev.Hash), engine.Short(in.Hash))
		default:
			continue
		}
		changes++
	}
	for _, name := range sortedKeys(fresh.MCPs) {
		o := fresh.MCPOrigin[name]
		if !o.Remote {
			continue
		}
		m := fresh.MCPs[name]
		prev, had := old.MCPs[name]
		switch {
		case !had:
			fmt.Fprintf(out, "+ mcp %s %s (via %s)\n", name, m.Summary(), o.Via)
		case stack.Fingerprint(prev) != stack.Fingerprint(m):
			fmt.Fprintf(out, "~ mcp %s %s → %s (via %s)\n", name, prev.Summary(), m.Summary(), o.Via)
		default:
			continue
		}
		changes++
	}
	for _, name := range sortedKeys(old.MCPs) {
		if _, ok := fresh.MCPs[name]; !ok && old.MCPOrigin[name].Remote {
			fmt.Fprintf(out, "- mcp %s (via %s)\n", name, old.MCPOrigin[name].Via)
			changes++
		}
	}
	c.merged = fresh
	return changes, fresh, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// fileDiff summarizes which files changed between the cached version and the
// new one.
func fileDiff(r *skills.Resolver, prevHash string, sk skills.Skill) string {
	before, err1 := skills.Files(r.CachedDir(prevHash))
	after, err2 := skills.Files(sk.Dir)
	if err1 != nil || err2 != nil {
		return " (the previous version is not in the cache)"
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
		Short: "Pegboard's built-in skill",
	}
	var yesInstall bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the pegboard skill on agents (default: global scope)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.scope == "" {
				opts.scope = string(agent.Global)
			}
			inst := newInstallCmd(opts, newEnv)
			if yesInstall {
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
	install.Flags().BoolVarP(&yesInstall, "yes", "y", false, "do not ask for confirmation")

	var yesUninstall bool
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the pegboard skill from agents (default: global scope)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.scope == "" {
				opts.scope = string(agent.Global)
			}
			un := newUninstallCmd(opts, newEnv)
			if yesUninstall {
				if err := un.Flags().Set("yes", "true"); err != nil {
					return err
				}
			}
			un.SetIn(cmd.InOrStdin())
			un.SetOut(cmd.OutOrStdout())
			un.SetErr(cmd.ErrOrStderr())
			return un.RunE(un, []string{"skill", "pegboard"})
		},
	}
	uninstall.Flags().BoolVarP(&yesUninstall, "yes", "y", false, "do not ask for confirmation")
	cmd.AddCommand(install, uninstall)
	return cmd
}
