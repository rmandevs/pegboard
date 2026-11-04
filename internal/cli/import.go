package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/apm"
	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/mcp"
	"github.com/rmandevs/pegboard/internal/native"
	"github.com/rmandevs/pegboard/internal/secrets"
	"github.com/rmandevs/pegboard/internal/stack"
	"github.com/rmandevs/pegboard/internal/state"
)

// found is an unmanaged MCP found in a target during import.
type found struct {
	target agent.Target
	file   string
	obs    any
	mcp    mcp.MCP
	masked []string
}

func newImportCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Adopt the MCPs already configured by agents (or an apm.yml) into pegboard.yaml",
		Long: `Read each agent's config, add the MCPs pegboard does not manage yet to
pegboard.yaml, and record them in state. Plain-text secrets (env and headers)
are replaced with ${env:…} references: export those variables before apply.

With --from apm.yml, read a Microsoft APM manifest instead: its skill
packages and self-defined MCP servers (registry: false) go into
pegboard.yaml. Registry and marketplace entries, prompts, instructions, and
hooks stay with apm; the output lists what was skipped and why.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			if from != "" {
				return importAPM(cmd, opts, env, from)
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
				n, err := importScope(c, out, env, false)
				if err != nil {
					return err
				}
				total += n
			}
			if total == 0 {
				fmt.Fprintln(out, "No new MCPs to import.")
			} else {
				fmt.Fprintln(out, "Review with `pegboard plan`.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "read an APM manifest (apm.yml) instead of the agents")
	return cmd
}

// importAPM translates an apm.yml into the scope's pegboard.yaml.
func importAPM(cmd *cobra.Command, opts *options, env agent.Env, from string) error {
	path := from
	if !filepath.IsAbs(path) {
		path = filepath.Join(env.ProjectDir, path)
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, apm.FileName)
	}
	ed, err := opts.editor(env)
	if err != nil {
		return err
	}
	base := filepath.Dir(ed.Path())
	res, err := apm.Read(path, func(abs string) string {
		rel, err := filepath.Rel(base, abs)
		if err != nil {
			return abs
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "../") {
			rel = "./" + rel
		}
		return rel
	})
	if err != nil {
		return err
	}
	r := editResult{File: ed.Path()}
	var skipped []string
	for _, name := range sortedKeys(res.MCPs) {
		if ed.HasMCP(name) {
			skipped = append(skipped, "mcp "+name+": already in "+stack.FileName)
			continue
		}
		if err := ed.Set(name, res.MCPs[name]); err != nil {
			return err
		}
		r.Added = append(r.Added, "mcp "+name)
	}
	for _, sk := range res.Skills {
		added, err := ed.AddSkill(sk.Source, nil)
		if err != nil {
			return err
		}
		if added {
			r.Added = append(r.Added, "skill "+sk.Source)
		}
	}
	if len(r.Added) > 0 {
		if err := ed.Save(); err != nil {
			return err
		}
	}
	skipped = append(skipped, res.Skipped...)
	if opts.json {
		return engine.WriteJSON(cmd.OutOrStdout(), struct {
			editResult
			Skipped  []string `json:"skipped"`
			Warnings []string `json:"warnings"`
		}{r, nonNil(skipped), nonNil(res.Warnings)})
	}
	errOut := cmd.ErrOrStderr()
	for _, w := range res.Warnings {
		fmt.Fprintln(errOut, "warning:", w)
	}
	for _, s := range skipped {
		fmt.Fprintln(errOut, "skipped:", s)
	}
	if len(r.Added) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Nothing to import from %s.\n", env.Pretty(path))
		return nil
	}
	return writeEdit(cmd, opts, env, r)
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// importScope moves the scope's targets' MCPs into pegboard.yaml. With
// adopt=false it takes what pegboard does not manage (import); with
// adopt=true, the ad-hoc installed entries, plus ad-hoc skills (adopt).
func importScope(c *scopeCtx, out interface{ Write([]byte) (int, error) }, env agent.Env, adopt bool) (int, error) {
	byName := map[string][]found{}
	var vars []string
	for _, t := range c.targets {
		p, ok := mcpProvider(t)
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
			e, managed := c.state.Get(t.ID(), name)
			if managed != adopt || (adopt && e.Origin != state.Adhoc) {
				continue
			}
			m, err := p.DecodeMCP(obs)
			if err != nil {
				fmt.Fprintf(out, "warning: %s · %s: mcp %s could not be read (%v); skipping\n", t.ID(), c.scope, name, err)
				continue
			}
			m, literals, vs := toReferences(name, m)
			vars = append(vars, vs...)
			byName[name] = append(byName[name], found{
				target: t, file: f.Path, obs: obs, mcp: m, masked: maskLiterals(obs, literals),
			})
		}
	}
	var adhocSkills []string
	if adopt {
		for _, d := range c.state.SkillDirs() {
			for _, n := range c.state.SkillNames(d) {
				if e, _ := c.state.Skill(d, n); e.Origin == state.Adhoc {
					adhocSkills = appendUnique(adhocSkills, e.Source)
				}
			}
		}
	}
	if len(byName) == 0 && len(adhocSkills) == 0 {
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
				fmt.Fprintf(out, "warning: mcp %s in %s differs from the one in %s; importing the one from %s\n",
					name, fd.target.ID(), first.target.ID(), first.target.ID())
			}
		}
		m := first.mcp
		if d, ok := declared[name]; ok {
			if !d.Equal(m) {
				fmt.Fprintf(out, "warning: mcp %s is already declared differently in %s; skipping\n", name, stack.FileName)
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
			e := state.Entry{File: fd.file, Hash: native.Hash(fd.obs, fd.masked), Masked: fd.masked, Origin: state.Declared}
			if adopt {
				e, _ = c.state.Get(fd.target.ID(), name)
				e.Origin = state.Declared
			}
			c.state.Set(fd.target.ID(), name, e)
		}
		imported = append(imported, fmt.Sprintf("mcp %s (%s)", name, targetIDs(targetsOf(same))))
	}
	for _, src := range adhocSkills {
		if _, err := ed.AddSkill(src, c.agentsWithSkill(src)); err != nil {
			return 0, err
		}
		for _, d := range c.state.SkillDirs() {
			for _, n := range c.state.SkillNames(d) {
				if e, _ := c.state.Skill(d, n); e.Source == src {
					e.Origin = state.Declared
					c.state.SetSkill(d, n, e)
				}
			}
		}
		imported = append(imported, "skill "+src)
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
	verb := "Imported into"
	if adopt {
		verb = "Adopted into"
	}
	fmt.Fprintf(out, "%s %s:\n", verb, env.Pretty(path))
	for _, s := range imported {
		fmt.Fprintf(out, "  + %s\n", s)
	}
	if len(vars) > 0 {
		sort.Strings(vars)
		vars = compact(vars)
		fmt.Fprintf(out, "Secrets were replaced with ${env:…}; export: %s\n", strings.Join(vars, ", "))
	}
	return len(imported), nil
}

// agentsWithSkill lists the targets that have the source's ad-hoc skill, or
// nil if all of them do: adopting it must not spread it to other agents.
func (c *scopeCtx) agentsWithSkill(source string) []string {
	var out []string
	for _, t := range c.targets {
		for _, n := range c.state.SkillNames(t.Paths.Skills) {
			if e, _ := c.state.Skill(t.Paths.Skills, n); e.Source == source {
				out = append(out, t.ID())
				break
			}
		}
	}
	if len(out) == len(c.targets) {
		return nil
	}
	return out
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

// toReferences replaces plain-text env and header values with ${env:…}
// references. It returns the original values (to mask them in state) and
// the variables the user must export.
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

// maskLiterals returns the paths in obs whose strings contain any of the
// secrets, so the state hash does not depend on them.
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
