// Package config loads the server-side settings an estate deployment chooses: the FLOOR and
// field OWNERSHIP.
//
// The floor lives here rather than in the binary because it is the sealed floor's DATA. A
// hardcoded floor would put every limit behind a recompile, and a deployment that cannot
// change a limit without a release will eventually be given a way around the limit instead.
// [estate.DefaultFloor] stays what its name says — a starting point for authoring this file,
// never a fallback: [Load] refuses an absent path rather than governing under limits nobody
// chose.
//
// What config may NOT do is remove a guarantee. The floor is validated (every tier present,
// every limit positive) and ownership is MERGED onto the default rather than replacing it, so
// a config can tighten and extend but never un-govern. Config chooses the policy; it can never
// reach the guarantee.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// Config is everything a deployment chooses about how grants are floored and judged.
type Config struct {
	// Floor is what a team may consume, per tier. Validated on load.
	Floor estate.Floor
	// Ownership is which fields MantleKeep corrects and which it merely watches, with the
	// document's declarations merged onto the defaults.
	Ownership estate.Ownership
}

// document is the file's shape. Durations are written as strings ("168h") because this is the
// one file an operator hand-edits: Go marshals a time.Duration as nanoseconds, and a floor
// authored as 604800000000000 is a floor where a dropped zero is a silently shortened
// retention that nobody reviewing the diff would catch.
type document struct {
	Floor     floorDocument     `json:"floor"`
	Ownership ownershipDocument `json:"ownership"`
}

// Load reads and validates the deployment's config.
//
// Unknown fields are REFUSED, exactly as [estate.ParseManifest] refuses them and for the same
// reason: a misspelled limit that is silently ignored is worse than one that is missing, because
// the operator believes the limit is in force.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, fmt.Errorf(
			"config: no path given — the floor is the sealed floor's data and must be chosen by " +
				"this deployment; set MANTLEKEEP_ESTATE_CONFIG (estate.DefaultFloor is a starting point " +
				"for authoring that file, not a fallback to govern under)")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return Parse(content)
}

// Parse decodes and validates a config document. Separated from [Load] so a test drives the
// bytes without a temp file.
func Parse(content []byte) (Config, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()

	var parsed document
	if err := decoder.Decode(&parsed); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	floor := parsed.Floor.toFloor()
	if err := validateFloor(floor); err != nil {
		return Config{}, err
	}
	if err := validateGates(floor); err != nil {
		return Config{}, err
	}
	if err := validateEnvTiers(floor); err != nil {
		return Config{}, err
	}
	// Derived from the bytes, not declared in them. Two deployments running the same file
	// compute the same revision, and an edit of any kind produces a different one — including
	// an edit an operator forgot to mention.
	floor.Revision = revisionOf(content)
	return Config{Floor: floor, Ownership: parsed.Ownership.mergeOntoDefault()}, nil
}

// revisionOf identifies a floor by its content.
//
// Short because it is read by humans in an audit record next to a decision, and twelve hex
// characters distinguish every floor a deployment will ever have while still fitting in a
// column. The full hash is not a secret; it is simply not useful at a glance.
func revisionOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:12]
}

// errNoPath is returned by a reload of a config that was built in code rather than read from a
// file. Named rather than inline so a caller can tell "nothing to reload" apart from "the
// reload failed", which are opposite situations for an operator.
var errNoPath = errors.New("config: this config was built in code, not read from a file — " +
	"there is nothing to reload")
