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

// binVersion runs `bin --version` with a short timeout and extracts the first
// version shaped like x.y[.z]. If the binary is slow or fails, it returns "".
func binVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	return ParseVersion(string(out))
}

// ParseVersion extracts the first version from the output of `--version`.
func ParseVersion(out string) string {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return m[1]
}

// memo caches f per argument: the global and project scopes ask about the
// same binary, and `--version` can be slow.
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
