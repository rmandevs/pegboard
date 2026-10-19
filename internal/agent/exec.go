package agent

import (
	"context"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

var versionRe = regexp.MustCompile(`v?(\d+\.\d+(?:\.\d+)?(?:[-+][0-9A-Za-z.-]+)?)`)

func lookPath(bin string) (string, error) { return exec.LookPath(bin) }

// binVersion corre `bin --version` con un timeout corto y extrae la primera
// versión con forma x.y[.z]. Si el binario tarda o falla, devuelve "".
func binVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	return ParseVersion(string(out))
}

// ParseVersion extrae la primera versión de la salida de `--version`.
func ParseVersion(out string) string {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return m[1]
}

// memo cachea f por argumento: global y proyecto preguntan por el mismo
// binario y `--version` puede tardar.
func memo(f func(string) string) func(string) string {
	var mu sync.Mutex
	cache := map[string]string{}
	return func(k string) string {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := cache[k]; ok {
			return v
		}
		v := f(k)
		cache[k] = v
		return v
	}
}
