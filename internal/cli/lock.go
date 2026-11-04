package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rmandevs/pegboard/internal/engine"
	"github.com/rmandevs/pegboard/internal/sbom"
	"github.com/rmandevs/pegboard/internal/skills"
)

func newLockCmd(opts *options, newEnv EnvFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Work with pegboard.lock",
	}
	var format string
	export := &cobra.Command{
		Use:   "export",
		Short: "Export pegboard.lock as an SBOM (CycloneDX or SPDX JSON)",
		Long: `export writes what pegboard.lock pins (git skills and included stacks, with
their commit and content hash) as a CycloneDX 1.5 or SPDX 2.3 JSON document.
It is an inventory, not a compliance attestation: local skills are not in
the lock, and the packages MCP servers run are not resolved by pegboard.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newEnv(opts.dir)
			if err != nil {
				return err
			}
			ctxs, err := opts.declarativeScopes(env)
			if err != nil {
				return err
			}
			var locks []*skills.Lock
			name := filepath.Base(env.ProjectDir)
			for _, c := range ctxs {
				if c.lock != nil {
					locks = append(locks, c.lock)
				}
			}
			if len(ctxs) == 1 && ctxs[0].scope == "global" {
				name = "pegboard-global"
			}
			d := sbom.Doc{Name: name, Tool: version(), Created: env.Now()}
			items := sbom.Items(locks...)
			var doc map[string]any
			switch format {
			case "cyclonedx":
				doc = sbom.CycloneDX(d, items)
			case "spdx":
				doc = sbom.SPDX(d, items)
			default:
				return fmt.Errorf("--format %q: use cyclonedx or spdx", format)
			}
			return engine.WriteJSON(cmd.OutOrStdout(), doc)
		},
	}
	export.Flags().StringVar(&format, "format", "cyclonedx", "cyclonedx or spdx")
	cmd.AddCommand(export)
	return cmd
}
