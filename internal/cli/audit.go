package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/skills"
)

// auditItem is a skill or context file with hidden characters.
type auditItem struct {
	Scope    string           `json:"scope"`
	Kind     string           `json:"kind"` // skill or context
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Targets  []string         `json:"targets"`
	Findings []skills.Finding `json:"findings"`
}

type auditJSON struct {
	Items    []auditItem `json:"items"`
	Scanned  int         `json:"scanned"`
	Critical int         `json:"critical"`
	Warning  int         `json:"warning"`
	Warnings []string    `json:"warnings"`
}

func newAuditCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var exit, strict bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Scan skills and context files for hidden Unicode that can hijack an agent",
		Long: `audit scans every skill the agents can read (installed by pegboard or not),
the skills declared in pegboard.yaml, and the context files (AGENTS.md,
CLAUDE.md…) for invisible characters: bidirectional overrides, Unicode tags,
zero-width characters. Critical findings are the ones pegboard refuses to
install; with --exit-code they exit with 2 (with --strict, warnings too).`,
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
			res := auditJSON{Items: []auditItem{}, Warnings: []string{}}
			seen := map[string]*auditItem{}
			add := func(scope agent.Scope, kind, name, path, target string) error {
				real := path
				if r, err := filepath.EvalSymlinks(path); err == nil {
					real = r
				}
				key := kind + "\x00" + real
				if it, ok := seen[key]; ok {
					if target != "" && !contains(it.Targets, target) {
						it.Targets = append(it.Targets, target)
					}
					return nil
				}
				var found []skills.Finding
				if kind == "skill" {
					found, err = skills.Scan(real)
				} else {
					var data []byte
					data, err = os.ReadFile(real)
					found = skills.ScanText(filepath.Base(path), data)
				}
				if err != nil {
					return err
				}
				res.Scanned++
				it := &auditItem{Scope: string(scope), Kind: kind, Name: name, Path: path, Targets: []string{}, Findings: found}
				if target != "" {
					it.Targets = append(it.Targets, target)
				}
				seen[key] = it
				return nil
			}
			for _, c := range ctxs {
				for _, t := range c.targets {
					if dir := t.Paths.Skills; dir != "" {
						ents, _ := os.ReadDir(dir)
						for _, e := range ents {
							p := filepath.Join(dir, e.Name())
							if info, err := os.Stat(p); err != nil || !info.IsDir() || !agent.Exists(filepath.Join(p, "SKILL.md")) {
								continue
							}
							if err := add(c.scope, "skill", e.Name(), p, t.ID()); err != nil {
								return err
							}
						}
					}
					if f := t.Paths.Context; f != "" && agent.Exists(f) {
						if err := add(c.scope, "context", filepath.Base(f), f, t.ID()); err != nil {
							return err
						}
					}
				}
				if c.stack == nil {
					continue
				}
				merged, err := c.resolve()
				if err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", env.Pretty(c.stack.Path), err))
					continue
				}
				got, err := resolveSkills(c.skillResolver(c.lock), merged.SkillSources())
				if err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", env.Pretty(c.stack.Path), err))
					continue
				}
				for _, sk := range got {
					if err := add(c.scope, "skill", sk.Name, sk.Dir, ""); err != nil {
						return err
					}
				}
			}
			for _, it := range seen {
				if len(it.Findings) == 0 {
					continue
				}
				sort.Strings(it.Targets)
				for _, f := range it.Findings {
					if f.Severity == skills.Critical {
						res.Critical++
					} else {
						res.Warning++
					}
				}
				res.Items = append(res.Items, *it)
			}
			sort.Slice(res.Items, func(i, j int) bool {
				a, b := res.Items[i], res.Items[j]
				if a.Scope != b.Scope {
					return a.Scope < b.Scope
				}
				return a.Path < b.Path
			})
			if opts.json {
				if err := engine.WriteJSON(cmd.OutOrStdout(), res); err != nil {
					return err
				}
			} else {
				writeAudit(cmd, env, res)
			}
			if exit && (res.Critical > 0 || strict && res.Warning > 0) {
				return exitCode(2)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&exit, "exit-code", false, "exit with 2 if there are critical findings")
	cmd.Flags().BoolVar(&strict, "strict", false, "with --exit-code, warnings also count")
	return cmd
}

func writeAudit(cmd *cobra.Command, env agent.Env, res auditJSON) {
	out := cmd.OutOrStdout()
	for _, w := range res.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}
	for _, it := range res.Items {
		head := fmt.Sprintf("%s %s · %s (%s)", it.Kind, it.Name, it.Scope, env.Pretty(it.Path))
		if len(it.Targets) > 0 {
			head += " — " + strings.Join(it.Targets, ", ")
		}
		fmt.Fprintln(out, head)
		for _, f := range it.Findings {
			fmt.Fprintf(out, "  %-8s %s\n", f.Severity, f.String())
		}
	}
	if len(res.Items) == 0 {
		fmt.Fprintf(out, "Scanned %d %s: no hidden Unicode.\n", res.Scanned, plural(res.Scanned, "item", "items"))
		return
	}
	fmt.Fprintf(out, "Scanned %d %s: %d critical, %d %s.\n", res.Scanned, plural(res.Scanned, "item", "items"),
		res.Critical, res.Warning, plural(res.Warning, "warning", "warnings"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
