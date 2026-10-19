// Package cli arma los comandos de pegboard (SPEC §4).
package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/providers"
)

// Version se fija al compilar con -ldflags "-X …/internal/cli.Version=x.y.z";
// con `go install …@vX` se toma de la info de compilación.
var Version = "dev"

func version() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return Version
}

// EnvFunc construye el entorno para la carpeta de --dir.
type EnvFunc func(dir string) (agent.Env, error)

type options struct {
	scope  string
	agents []string
	json   bool
	dir    string
}

// exitCode termina con un código sin mensaje de error (p. ej. 2 = hay
// cambios con --exit-code).
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("código de salida %d", int(e)) }

// Run ejecuta pegboard con args y devuelve el código de salida: 0 sin
// cambios, 2 hay cambios o drift (con --exit-code), 1 error.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, newEnv EnvFunc) int {
	root := newRoot(stdin, stdout, stderr, newEnv)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var code exitCode
		if errors.As(err, &code) {
			return int(code)
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func newRoot(stdin io.Reader, stdout, stderr io.Writer, newEnv EnvFunc) *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "pegboard",
		Short:         "Configuration as Code para agentes",
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	f := root.PersistentFlags()
	f.StringVar(&opts.scope, "scope", "", "alcance: global o project (default: ambos)")
	f.StringArrayVar(&opts.agents, "agent", nil, "limitar a un agente (repetible)")
	f.BoolVar(&opts.json, "json", false, "salida JSON estable")
	f.StringVar(&opts.dir, "dir", ".", "carpeta del proyecto")

	root.AddCommand(
		newAgentsCmd(opts, newEnv),
		newInitCmd(opts, newEnv),
		newPlanCmd(opts, newEnv),
		newApplyCmd(opts, newEnv),
		newDriftCmd(opts, newEnv),
		newImportCmd(opts, newEnv),
		newInstallCmd(opts, newEnv),
		newUninstallCmd(opts, newEnv),
		newAddCmd(opts, newEnv),
		newRemoveCmd(opts, newEnv),
		newAdoptCmd(opts, newEnv),
		newUpdateCmd(opts, newEnv),
		newSkillCmd(opts, newEnv),
	)
	return root
}

// selection resuelve --scope y --agent.
func (o *options) selection() ([]agent.Scope, []agent.Provider, error) {
	scopes, ok := agent.ParseScope(o.scope)
	if !ok {
		return nil, nil, fmt.Errorf("--scope %q inválido: usa global o project", o.scope)
	}
	if len(o.agents) == 0 {
		return scopes, providers.All(), nil
	}
	var provs []agent.Provider
	var errs []error
	for _, id := range o.agents {
		p, ok := providers.Get(id)
		if !ok {
			errs = append(errs, fmt.Errorf("agente desconocido %q", id))
			continue
		}
		provs = append(provs, p)
	}
	return scopes, provs, errors.Join(errs...)
}
