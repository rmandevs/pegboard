package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/providers"
)

func newAgentsCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Detected agents, their paths, and provider version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, provs, err := opts.selection()
			if err != nil {
				return err
			}
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			targets := providers.Detect(env, provs, scopes)
			if opts.json {
				return writeAgentsJSON(cmd.OutOrStdout(), env, targets)
			}
			writeAgentsText(cmd.OutOrStdout(), env, targets)
			return nil
		},
	}
}

// agentsJSON is the stable format of `pegboard agents --json`. Paths are
// absolute; changing an existing field breaks consumers.
type agentsJSON struct {
	Project string         `json:"project"`
	Targets []agent.Target `json:"targets"`
}

func writeAgentsJSON(w io.Writer, env agent.Env, targets []agent.Target) error {
	if targets == nil {
		targets = []agent.Target{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(agentsJSON{Project: env.ProjectDir, Targets: targets})
}

func writeAgentsText(w io.Writer, env agent.Env, targets []agent.Target) {
	if len(targets) == 0 {
		fmt.Fprintln(w, "No agents detected.")
		return
	}
	for i, t := range targets {
		if i > 0 {
			fmt.Fprintln(w)
		}
		version := "unknown version"
		if t.Version != "" {
			version = "v" + t.Version
		}
		fmt.Fprintf(w, "%s · %s  (%s, provider v%d)\n", t.ID(), t.Scope, version, t.Provider)
		evidence := "—"
		if len(t.Evidence) > 0 {
			evidence = strings.Join(t.Evidence, ", ")
		}
		fmt.Fprintf(w, "  found: %s\n", evidence)
		row(w, env, "mcps", t.Paths.MCPs)
		row(w, env, "skills", t.Paths.Skills)
		row(w, env, "context", t.Paths.Context)
	}
}

func row(w io.Writer, env agent.Env, label, path string) {
	if path == "" {
		return
	}
	mark := ""
	if !agent.Exists(path) {
		mark = "  (does not exist)"
	}
	fmt.Fprintf(w, "  %-11s %s%s\n", label+":", env.Pretty(path), mark)
}
