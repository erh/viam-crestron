package crestron

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadEnvFile reads simple KEY=value lines from ~/.crestron.env (or the path in
// the CRESTRON_ENV environment variable) and sets any variable not already
// present in the environment, so an explicit export or command-line flag still
// takes precedence. A missing file is not an error. Lines may be blank, start
// with '#', or begin with an optional "export "; surrounding quotes and
// whitespace around the value are trimmed.
//
// All the crestron-* commands call this at startup so they share one file for
// CRESTRON_HOST, CRESTRON_USER, CRESTRON_PASS, and CRESTRON_TOKEN.
func LoadEnvFile() {
	path := os.Getenv("CRESTRON_ENV")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		path = filepath.Join(home, ".crestron.env")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k == "" {
			continue
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}
