package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadEnv reads KEY=VALUE lines from ~/.config/linkedin-mcp/.env then ./.env.
// Already-set env vars win.
func loadEnv() {
	for _, p := range []string{filepath.Join(filepath.Dir(tokenPath()), ".env"), ".env"} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
			if _, set := os.LookupEnv(k); !set {
				os.Setenv(k, v)
			}
		}
		f.Close()
	}
}
