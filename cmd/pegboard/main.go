// pegboard: Configuration as Code for agents. See SPEC.md.
package main

import (
	"os"

	"github.com/rmandevs/pegboard/internal/agent"
	"github.com/rmandevs/pegboard/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, agent.DefaultEnv))
}
