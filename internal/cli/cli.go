// Package cli builds the pegboard commands (SPEC §4).
package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/providers"
)

// Version is set at build time with -ldflags "-X …/internal/cli.Version=x.y.z";
// with `go install …@vX` it is taken from the build info.
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

// EnvFunc builds the environment for the --dir directory.
type EnvFunc func(dir string) (agent.Env, error)

type options struct {
	scope  string
	agents []string
	json   bool
	dir    string
}

// exitCode terminates with a code but without an error message (for example,
// 2 = there are changes with --exit-code).
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

// Run executes pegboard with args and returns the exit code: 0 without
// changes, 2 there are changes or drift (with --exit-code), 1 error.
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
		Short:         "Configuration as Code for agents",
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	f := root.PersistentFlags()
	f.StringVar(&opts.scope, "scope", "", "scope: global or project (default: both)")
	f.StringArrayVar(&opts.agents, "agent", nil, "limit to one agent (repeatable)")
	f.BoolVar(&opts.json, "json", false, "stable JSON output")
	f.StringVar(&opts.dir, "dir", ".", "project directory")

	root.AddCommand(
		newAgentsCmd(opts, newEnv),
		newInitCmd(opts, newEnv),
		newPlanCmd(opts, newEnv),
		newApplyCmd(opts, newEnv),
		newDriftCmd(opts, newEnv),
		newImportCmd(opts, newEnv),
		newReplicateCmd(opts, newEnv),
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

// selection resolves --scope and --agent.
func (o *options) selection() ([]agent.Scope, []agent.Provider, error) {
	scopes, ok := agent.ParseScope(o.scope)
	if !ok {
		return nil, nil, fmt.Errorf("--scope %q is invalid: use global or project", o.scope)
	}
	if len(o.agents) == 0 {
		return scopes, providers.All(), nil
	}
	var provs []agent.Provider
	var errs []error
	for _, id := range o.agents {
		p, ok := providers.Get(id)
		if !ok {
			errs = append(errs, fmt.Errorf("unknown agent %q", id))
			continue
		}
		provs = append(provs, p)
	}
	return scopes, provs, errors.Join(errs...)
}
