package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/engine"
	"github.com/rmandevs/agents-casc/internal/stack"
	"github.com/rmandevs/agents-casc/internal/state"
	"github.com/rmandevs/agents-casc/providers"
)

func newInitCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Crea pegboard.yaml con los agentes detectados",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			scope := agent.Project
			if opts.scope != "" {
				scopes, ok := agent.ParseScope(opts.scope)
				if !ok {
					return fmt.Errorf("--scope %q inválido: usa global o project", opts.scope)
				}
				scope = scopes[0]
			}
			_, provs, err := opts.selection()
			if err != nil {
				return err
			}
			path := stack.Path(env, scope)
			if agent.Exists(path) {
				return fmt.Errorf("%s ya existe", env.Pretty(path))
			}
			var agents []string
			seen := map[string]bool{}
			for _, t := range providers.Detect(env, provs, []agent.Scope{scope}) {
				if !seen[t.Agent] {
					seen[t.Agent] = true
					agents = append(agents, t.Agent)
				}
			}
			if _, err := stack.Edit(path, agents); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Creado %s (agentes: %s).\n", env.Pretty(path), orNone(agents))
			return nil
		},
	}
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "ninguno detectado"
	}
	return strings.Join(l, ", ")
}

// applyFlags son las banderas de los comandos que escriben.
type applyFlags struct {
	yes        bool
	force      bool
	exitCode   bool
	pruneAdhoc bool
}

func newPlanCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Muestra lo que cambiaría; no escribe nada",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeclarative(cmd, opts, newEnv, f, false)
		},
	}
	cmd.Flags().BoolVar(&f.exitCode, "exit-code", false, "salir con 2 si hay cambios")
	cmd.Flags().BoolVar(&f.force, "force", false, "mostrar el plan resolviendo conflictos a favor de pegboard.yaml")
	cmd.Flags().BoolVar(&f.pruneAdhoc, "prune-adhoc", false, "incluir quitar lo instalado ad-hoc")
	return cmd
}

func newApplyCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Aplica el plan (pide confirmación sin -y)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeclarative(cmd, opts, newEnv, f, true)
		},
	}
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "no pedir confirmación")
	cmd.Flags().BoolVar(&f.force, "force", false, "sobrescribir lo editado a mano")
	cmd.Flags().BoolVar(&f.pruneAdhoc, "prune-adhoc", false, "quitar también lo instalado ad-hoc")
	return cmd
}

func runDeclarative(cmd *cobra.Command, opts *options, newEnv EnvFunc, f *applyFlags, apply bool) error {
	env, err := newEnv(opts.dir)
	if err != nil {
		return err
	}
	ctxs, _, err := opts.declarativeScopes(env)
	if err != nil {
		return err
	}
	var scopes []engine.Scope
	var warnings []string
	for _, c := range ctxs {
		es, w, err := c.declarative(f.pruneAdhoc)
		warnings = append(warnings, w...)
		if err != nil {
			return err
		}
		scopes = append(scopes, es)
	}
	return execute(cmd, opts, env, scopes, warnings, f, apply, nil)
}

// execute calcula el plan, lo muestra y, si apply, lo aplica tras confirmar.
// after corre solo si se aplicó.
func execute(cmd *cobra.Command, opts *options, env agent.Env, scopes []engine.Scope, warnings []string,
	f *applyFlags, apply bool, after func() error) error {
	out := cmd.OutOrStdout()
	if apply && opts.json && !f.yes {
		return errors.New("--json necesita -y: no hay a quién preguntar")
	}
	plan, err := engine.Compute(scopes, engine.Options{Force: f.force})
	if err != nil {
		return err
	}
	if opts.json {
		pj := plan.JSON(warnings)
		if !apply {
			if err := engine.WriteJSON(out, pj); err != nil {
				return err
			}
			if f.exitCode && !plan.Empty() {
				return exitCode(2)
			}
			return nil
		}
		res := struct {
			engine.PlanJSON
			Applied bool             `json:"applied"`
			Written []engine.Written `json:"written"`
		}{PlanJSON: pj, Written: []engine.Written{}}
		if plan.Blocked() {
			_ = engine.WriteJSON(out, res)
			return exitCode(1)
		}
		r, err := engine.Apply(plan, backupDir(env), env.Now())
		if err == nil && after != nil {
			err = after()
		}
		if r != nil && r.Written != nil {
			res.Written = r.Written
		}
		res.Applied = err == nil
		if werr := engine.WriteJSON(out, res); werr != nil {
			return werr
		}
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "aviso:", w)
	}
	plan.WriteText(out, env.Pretty)
	if !apply {
		if f.exitCode && !plan.Empty() {
			return exitCode(2)
		}
		return nil
	}
	if !plan.Pending() {
		if after != nil {
			return after()
		}
		return nil
	}
	if plan.Blocked() {
		return engine.ErrBlocked
	}
	if !plan.Empty() && !f.yes && !confirm(cmd.InOrStdin(), out) {
		fmt.Fprintln(out, "Cancelado; no se escribió nada.")
		return nil
	}
	r, err := engine.Apply(plan, backupDir(env), env.Now())
	if err != nil {
		return err
	}
	for _, w := range r.Written {
		line := "Escrito " + env.Pretty(w.File)
		if w.Backup != "" {
			line += " (respaldo: " + env.Pretty(w.Backup) + ")"
		}
		fmt.Fprintln(out, line)
	}
	if after != nil {
		return after()
	}
	return nil
}

func backupDir(env agent.Env) string { return env.StateDir() + string(os.PathSeparator) + "backups" }

func confirm(in io.Reader, out io.Writer) bool {
	fmt.Fprint(out, "¿Aplicar? [s/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "si", "sí", "y", "yes":
		return true
	}
	return false
}

func newDriftCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	var exit, strict bool
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Compara el estado con los archivos reales",
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
			items := []engine.DriftItem{}
			for _, c := range ctxs {
				its, err := engine.Drift(c.scope, c.state, mcpProvider)
				if err != nil {
					return err
				}
				for _, it := range its {
					agentID, _, _ := strings.Cut(it.Target, ":")
					if len(opts.agents) == 0 || contains(opts.agents, agentID) || contains(opts.agents, it.Target) {
						items = append(items, it)
					}
				}
			}
			drift := 0
			for _, it := range items {
				if it.Kind != engine.InSync || (strict && it.Origin == state.Adhoc) {
					drift++
				}
			}
			if opts.json {
				if err := engine.WriteJSON(cmd.OutOrStdout(), struct {
					Items []engine.DriftItem `json:"items"`
					Drift int                `json:"drift"`
				}{items, drift}); err != nil {
					return err
				}
			} else {
				engine.WriteDriftText(cmd.OutOrStdout(), items, env.Pretty)
				if strict && drift > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "--strict: lo ad-hoc cuenta como drift.")
				}
			}
			if exit && drift > 0 {
				return exitCode(2)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&exit, "exit-code", false, "salir con 2 si hay drift")
	cmd.Flags().BoolVar(&strict, "strict", false, "contar lo ad-hoc como drift")
	return cmd
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
