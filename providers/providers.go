// Package providers registra los providers disponibles.
package providers

import (
	"sync"

	"github.com/rmandevs/agents-casc/internal/agent"
	"github.com/rmandevs/agents-casc/providers/claudecode"
	"github.com/rmandevs/agents-casc/providers/codex"
	"github.com/rmandevs/agents-casc/providers/hermes"
	"github.com/rmandevs/agents-casc/providers/opencode"
)

// All son los providers en el orden en que se muestran.
func All() []agent.Provider {
	return []agent.Provider{
		claudecode.Provider{},
		codex.Provider{},
		opencode.Provider{},
		hermes.Provider{},
	}
}

// Get busca un provider por ID.
func Get(id string) (agent.Provider, bool) {
	for _, p := range All() {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// Detect corre la detección de los providers dados en paralelo (cada uno
// puede invocar `--version`) y devuelve los targets en orden estable:
// por alcance y luego por provider.
func Detect(env agent.Env, provs []agent.Provider, scopes []agent.Scope) []agent.Target {
	results := make([][]agent.Target, len(provs)*len(scopes))
	var wg sync.WaitGroup
	for si, s := range scopes {
		for pi, p := range provs {
			wg.Add(1)
			go func(i int, p agent.Provider, s agent.Scope) {
				defer wg.Done()
				results[i] = p.Detect(env, s)
			}(si*len(provs)+pi, p, s)
		}
	}
	wg.Wait()
	var out []agent.Target
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}
