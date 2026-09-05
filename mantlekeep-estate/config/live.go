package config

import (
	"sync/atomic"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// Live is the config a running server reads, replaceable without a restart.
//
// A limit that can only change by cutting a release is a limit a deployment will eventually be
// given a way around instead — the same reason the floor is a file rather than a constant, one
// step further out. Operationally the cases are ordinary and urgent: a cluster goes offline, a
// quota is wrong at 3am, a shared DEV env turns out to need a gate.
//
// Three properties make that safe, and all three are the point of this type:
//
//  1. VALIDATE THEN SWAP. A reload parses and validates a COMPLETE new config and replaces the
//     pointer, or changes nothing at all. It never merges into the live one, so there is no
//     state in which half a floor is in force.
//  2. A BAD FILE KEEPS THE GOOD CONFIG. Reload returns the error and leaves the previous
//     config serving. A typo must not be able to un-govern a running footprint, and refusing to
//     serve at all would make an operator's slip an outage.
//  3. READERS GET A SNAPSHOT. [Live.Current] returns a value, not a reference into shared
//     state, so a request that reads the floor twice reads the same floor twice — a decision
//     made half under one revision and half under another is a decision nobody can reproduce.
type Live struct {
	path    string
	current atomic.Pointer[Config]
}

// OpenLive loads the config once and holds it for reloading.
//
// A failure here is fatal to the caller by design: starting with no floor would govern under
// limits nobody chose, which is precisely what [Load] refuses to do.
func OpenLive(path string) (*Live, error) {
	loaded, err := Load(path)
	if err != nil {
		return nil, err
	}
	live := &Live{path: path}
	live.current.Store(&loaded)
	return live, nil
}

// NewLive holds an already-parsed config with no file behind it, for a test or an embedded
// deployment that builds its floor in code. Reload on one of these is a no-op error rather
// than a panic — there is nothing to re-read.
func NewLive(loaded Config) *Live {
	live := &Live{}
	live.current.Store(&loaded)
	return live
}

// Current returns the config in force right now.
func (l *Live) Current() Config { return *l.current.Load() }

// Floor returns just the floor, which is what most callers want.
func (l *Live) Floor() estate.Floor { return l.current.Load().Floor }

// Path is the file this reloads from, empty for an in-code config.
func (l *Live) Path() string { return l.path }

// Reload re-reads and validates the file, swapping it in only if the whole document is sound.
//
// Returns the config now in force either way: on success the new one, on failure the one that
// was already serving — so a caller can log what is actually governing rather than what it
// hoped would be.
func (l *Live) Reload() (Config, error) {
	if l.path == "" {
		return l.Current(), errNoPath
	}
	loaded, err := Load(l.path)
	if err != nil {
		return l.Current(), err
	}
	l.current.Store(&loaded)
	return loaded, nil
}
