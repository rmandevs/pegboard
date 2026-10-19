// pegboard: Configuration as Code para agentes. Ver SPEC.md.
package main

import (
	"os"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, agent.DefaultEnv))
}
