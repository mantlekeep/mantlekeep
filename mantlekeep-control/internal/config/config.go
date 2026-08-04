// Package config is MantleKeep's RUNTIME plugin selection — the "Helm values" of the
// framework, read from environment (or .mantlekeep.yaml via Koanf in production) and
// applied at startup. Changing a value switches the active plugin WITHOUT a
// rebuild.
//
// It is GENERIC: it knows nothing about any specific capability. Every capability
// — forge, policy, store, audit, transport — is selected and bound the same way:
//
//	MANTLEKEEP_SELECT_<CAP>=<provider>          pick the active provider for a capability
//	                                        e.g. MANTLEKEEP_SELECT_FORGE=github
//	MANTLEKEEP_BIND_<CAP>_<ROLE>=<provider>     bind a role within a capability
//	                                        e.g. MANTLEKEEP_BIND_STORE_AUDIT=postgres
//	MANTLEKEEP_DRIVER_<NAME>=<binary-path>      attach an out-of-process driver
//	                                        e.g. MANTLEKEEP_DRIVER_POSTGRES=./bin/…
//
// This is distinct from build tags (which decide whether a heavy library is
// compiled in at all). Availability = compile-time; selection = runtime.
package config

import (
	"os"
	"strings"

	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/safeio"
)

// Config is the resolved runtime selection — all capabilities, uniformly.
type Config struct {
	Selections map[string]string            // capability → provider
	Bindings   map[string]map[string]string // capability → role → provider
	Drivers    map[string]string            // driver name → external binary path
}

// Load reads the config: an optional file first (the "Helm values" file), then
// environment variables ON TOP so env overrides the file. The file path is
// $MANTLEKEEP_CONFIG, else `.mantlekeep.conf` in the working directory if present.
//
// The file is dotenv-style `KEY=VALUE` (stdlib parse, zero dependency) using the
// same keys as the env. A YAML/`.mantlekeep.yaml` form via Koanf is a drop-in swap
// that adds a dependency — kept out of the core by default (config parsing can
// itself be a plugin).
func Load() Config {
	var pairs []string
	if fv, err := readFile(configPath()); err == nil {
		pairs = append(pairs, fv...) // file first (base)
	}
	pairs = append(pairs, os.Environ()...) // env last → env wins
	return parse(pairs)
}

func configPath() string {
	if p := os.Getenv("MANTLEKEEP_CONFIG"); p != "" {
		return p
	}
	return ".mantlekeep.conf"
}

// readFile returns the non-comment KEY=VALUE lines of a dotenv-style file. The path is the
// operator-set MANTLEKEEP_CONFIG (or the default .mantlekeep.conf), read through the validated
// config door.
func readFile(path string) ([]string, error) {
	data, err := safeio.ReadConfigFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

// parse builds a Config from KEY=VALUE pairs; later pairs override earlier ones.
func parse(pairs []string) Config {
	c := Config{
		Selections: map[string]string{},
		Bindings:   map[string]map[string]string{},
		Drivers:    map[string]string{},
	}
	for _, e := range pairs {
		k, v, ok := strings.Cut(e, "=")
		if !ok || v == "" {
			continue
		}
		switch {
		case strings.HasPrefix(k, "MANTLEKEEP_SELECT_"):
			c.Selections[strings.ToLower(strings.TrimPrefix(k, "MANTLEKEEP_SELECT_"))] = v
		case strings.HasPrefix(k, "MANTLEKEEP_BIND_"):
			// <CAP>_<ROLE>: capability is the first segment, role the remainder.
			cap, role, ok := strings.Cut(strings.TrimPrefix(k, "MANTLEKEEP_BIND_"), "_")
			if !ok {
				continue
			}
			cap, role = strings.ToLower(cap), strings.ToLower(role)
			if c.Bindings[cap] == nil {
				c.Bindings[cap] = map[string]string{}
			}
			c.Bindings[cap][role] = v
		case strings.HasPrefix(k, "MANTLEKEEP_DRIVER_"):
			c.Drivers[strings.ToLower(strings.TrimPrefix(k, "MANTLEKEEP_DRIVER_"))] = v
		}
	}
	return c
}

// Select returns the chosen provider for a capability (empty = let the code auto-pick).
func (c Config) Select(capability string) string { return c.Selections[capability] }

// Routes returns a capability's role→provider map: config bindings overlay the
// supplied defaults, so an unset role keeps its default and a set one switches.
func (c Config) Routes(capability string, defaults map[string]string) map[string]string {
	out := map[string]string{}
	for role, prov := range defaults {
		out[role] = prov
	}
	for role, prov := range c.Bindings[capability] {
		out[role] = prov
	}
	return out
}
