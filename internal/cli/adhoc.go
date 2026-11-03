package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/secrets"
	"github.com/rmandevs/pegboard/internal/skills"
	"github.com/rmandevs/pegboard/internal/stack"
	"github.com/rmandevs/pegboard/internal/state"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// adhocScope applies rule 5 of SPEC §4.1: project if there is a
// pegboard.yaml or a git repo in the directory; otherwise global.
func (o *options) adhocScope(env agent.Env) (agent.Scope, error) {
	if o.scope != "" {
		s, ok := agent.ParseScope(o.scope)
		if !ok {
			return "", fmt.Errorf("--scope %q is invalid: use global or project", o.scope)
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
		Use:   "install [mcp <name>=<command|url>…] [skill <source>…]",
		Short: "Install now, without pegboard.yaml (ad-hoc mode)",
		Long: `Install MCPs and skills on the detected agents with the same plan, state, and
backups as apply, without touching pegboard.yaml (unless --save is used).

Skill sources (the same as npx skills): owner/repo, owner/repo@skill,
github.com/owner/repo//path?ref=v1, a git URL, a local ./path, or pegboard.`,
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
				return fmt.Errorf("no agents detected in %s scope", scope)
			}
			origin := state.Adhoc
			if save {
				origin = state.Declared
			}
			engScope := engine.Scope{Scope: scope, State: c.state, Resolver: c.resolver()}
			// With --save, skills are pinned in the stack's lock.
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
						return fmt.Errorf("skill %s is declared in %s; change it there or use --save", sk.Name, stack.FileName)
					}
				}
				skillWants[sk.Name] = &engine.SkillWant{Skill: sk, Origin: origin}
			}
			if len(skillWants) > 0 {
				engScope.Skills = c.skillDirs(func(agent.Target) map[string]*engine.SkillWant { return skillWants }, nil)
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
						return fmt.Errorf("mcp %s is declared in %s; change it there or use --save", name, stack.FileName)
					}
					wants[name] = &engine.Want{MCP: mcps[name], Origin: origin}
				}
				engScope.Targets = append(engScope.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
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
						if _, err := ed.AddSkill(src, opts.agents); err != nil {
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
						fmt.Fprintf(cmd.OutOrStdout(), "Saved to %s.\n", env.Pretty(stack.Path(env, scope)))
					}
					return nil
				}
			}
			return execute(cmd, opts, env, []engine.Scope{engScope}, nil, f, true, after)
		},
	}
	cmd.Flags().StringArrayVar(&envs, "env", nil, "variable for stdio MCPs: K=V (repeatable; V may be ${env:X})")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "header for http MCPs: K=V (repeatable)")
	cmd.Flags().BoolVar(&save, "save", false, "also add it to pegboard.yaml")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show the plan and write nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite anything that already exists")
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

// parseInstall reads `mcp a=… b=… skill x y mcp c=…`. Remote stacks arrive
// in phase 3.
func parseInstall(args, envs, headers []string) (map[string]mcp.MCP, []string, []string, error) {
	mcps, order, sources, err := parseInstallArgs(args, envs, headers)
	if err == nil && len(order) == 0 && len(sources) == 0 {
		err = errors.New("nothing to install")
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
					return nil, nil, nil, fmt.Errorf("invalid mcp name %q", name)
				}
				if _, dup := out[name]; dup {
					return nil, nil, nil, fmt.Errorf("mcp %s is repeated", name)
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
						return nil, nil, nil, fmt.Errorf("mcp %s: missing command", name)
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
				return nil, nil, nil, errors.New("mcp requires name=command or name=url")
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
				return nil, nil, nil, errors.New("skill requires a source (owner/repo, owner/repo@skill, ./path…)")
			}
		case strings.Contains(kw, "/"):
			return nil, nil, nil, errors.New("installing remote stacks arrives in phase 3")
		default:
			return nil, nil, nil, fmt.Errorf("expected `mcp name=…`, not %q", kw)
		}
	}
	if env != nil && !usedEnv {
		return nil, nil, nil, errors.New("--env only applies to stdio MCPs (name=command)")
	}
	if hdr != nil && !usedHdr {
		return nil, nil, nil, errors.New("--header only applies to http MCPs (name=url)")
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
			return nil, fmt.Errorf("%s expects K=V, not %q", flag, s)
		}
		out[k] = v
	}
	return out, nil
}

// splitWords splits a command the way a simple shell would: spaces, single
// and double quotes, and \ for escaping.
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
		return nil, errors.New("unterminated quotes")
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

func newUninstallCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "uninstall [mcp <name>…] [skill <name>…]",
		Short: "Remove something installed ad-hoc",
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
					return fmt.Errorf("expected `uninstall mcp|skill name…`, not %q", a)
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
			engScope := engine.Scope{Scope: scope, State: c.state, Resolver: c.resolver()}
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
						return fmt.Errorf("mcp %s is declared in %s: remove it with `pegboard remove mcp %s` and run apply", name, stack.FileName, name)
					}
					wants[name] = nil
					found["mcp "+name] = true
				}
				if len(wants) > 0 {
					engScope.Targets = append(engScope.Targets, engine.Target{Target: t, Provider: p, Wants: wants})
				}
			}
			var declaredErr error
			engScope.Skills = c.skillDirs(func(t agent.Target) map[string]*engine.SkillWant {
				out := map[string]*engine.SkillWant{}
				for _, name := range byKind["skill"] {
					e, ok := c.state.Skill(t.Paths.Skills, name)
					if !ok {
						continue
					}
					if e.Origin == state.Declared {
						declaredErr = fmt.Errorf("skill %s is declared in %s: remove it with `pegboard remove skill %s` and run apply", name, stack.FileName, name)
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
				return fmt.Errorf("pegboard did not install in scope %s: %s", scope, strings.Join(missing, ", "))
			}
			return execute(cmd, opts, env, []engine.Scope{engScope}, nil, f, true, nil)
		},
	}
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "show the plan and write nothing")
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&f.force, "force", false, "remove even if it was edited by hand")
	return cmd
}
