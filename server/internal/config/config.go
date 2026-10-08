// Package config reads settings from the environment and an optional .env file.
package config

import (
	"bufio"
	"os"
	"strings"
)

const DefaultDatabaseURL = "postgres://qoimeks:qoimeks@127.0.0.1:5433/qoimeks"

// LoadDotEnv sets variables from the first of the given files that exists.
// Lines are KEY=VALUE; variables already present in the environment win.
func LoadDotEnv(paths ...string) {
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			key, value, ok := strings.Cut(line, "=")
			if !ok || strings.HasPrefix(line, "#") {
				continue
			}
			key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if _, set := os.LookupEnv(key); !set {
				os.Setenv(key, value)
			}
		}
		return
	}
}

// Get returns the first non-empty variable among keys, or fallback.
func Get(fallback string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fallback
}
