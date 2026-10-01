/*
===========================================================================

main.go - report the compatibility versions compiled into a server release.

Release preparation compares this output with its admission declaration. The
wire and persistence owners remain authoritative; copying version numbers into
a release manifest cannot make an incompatible server eligible for deployment.

===========================================================================
*/
package main

import (
	"encoding/json"
	"os"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/releaseprotocol"
)

/*
================
main

A server speaks exactly one release protocol (releaseprotocol.Require
refuses every other) and one authority schema. A protocol change ships only
as a coordinated server and client release. storeUpgradeFrom is the oldest
schema its offline upgrade (sro-authority-upgrade) converts; the receiver
runs that upgrade, with the game server stopped, before the new one starts.
================
*/
func main() {
	contract := map[string]int{
		"protocolMin":      releaseprotocol.Current,
		"protocolMax":      releaseprotocol.Current,
		"storeReadMin":     store.CurrentVersion,
		"storeReadMax":     store.CurrentVersion,
		"storeWrite":       store.CurrentVersion,
		"storeUpgradeFrom": store.UpgradeFromVersion,
	}
	if err := json.NewEncoder(os.Stdout).Encode(contract); err != nil {
		panic(err)
	}
}
