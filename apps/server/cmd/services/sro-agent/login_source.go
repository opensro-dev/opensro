/*
===========================================================================

login_source.go - the Agent's login source key

Opens the key the Agent hashes login addresses under (agent server
login_source.go, #4815). It lives beside the Agent's history journal,
minted once and read on every boot, so a login's address and network
hashes stay comparable across restarts. Port-only, not native: the key
exists only while the census runs (SRO_CONNECTION_JOURNAL).

===========================================================================
*/
package main

import (
	"path/filepath"

	"opensro.online/server/internal/platform/connjournal"
)

// loginSourceKeyFile names the key beside the Agent's history journal.
const loginSourceKeyFile = "login-source.key"

/*
================
openLoginSourceKey

The Agent's login source key in directory, or nil with the census off.
================
*/
func openLoginSourceKey(directory string) ([]byte, error) {
	if !connjournal.Enabled() {
		return nil, nil
	}
	return connjournal.LoadOrCreateKey(filepath.Join(directory, loginSourceKeyFile))
}
