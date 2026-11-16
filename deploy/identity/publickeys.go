/*
===========================================================================

publickeys.go - the GameWorld's copy of the Agent session public keys

Nomad handed the GameWorld only the public projection of the Agent's
session key ring. Here a one-shot run writes it into the cluster volume
with the upstream's own projection (auth.PublicAgentSessionKeyRing).

===========================================================================
*/
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"opensro.online/server/internal/security/auth"
)

/*
================
derivePublic

Writes the public ring for the private ring at in to out, atomically.
Returns false, and touches nothing, when out already holds it.
================
*/
func derivePublic(in, out string) (bool, error) {
	private, err := os.ReadFile(in)
	if err != nil {
		return false, err
	}
	public, err := auth.PublicAgentSessionKeyRing(private)
	if err != nil {
		return false, err
	}
	current, err := os.ReadFile(out)
	if err == nil && bytes.Equal(current, public) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, writeAtomic(out, public, 0o644)
}

/*
================
writeAtomic

Writes beside the target and renames over it, so a reader never sees a
half-written file.
================
*/
func writeAtomic(path string, payload []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
