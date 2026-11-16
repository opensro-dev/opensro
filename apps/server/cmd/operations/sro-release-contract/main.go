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
	"fmt"
	"os"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/releaseprotocol"
)

/*
================
main

A server speaks exactly one release protocol (releaseprotocol.Require
refuses every other) and writes one authority schema; it opens that
schema and the store.StoreReadHeadroom newer ones (#3091). A protocol
change ships only as a coordinated server and client release. storeUpgradeFrom is the oldest
schema its offline upgrade (sro-authority-upgrade) converts; the receiver
runs that upgrade, with the game server stopped, before the new one starts.
================
*/
func main() {
	if err := json.NewEncoder(os.Stdout).Encode(releaseContract()); err != nil {
		fmt.Fprintln(os.Stderr, "sro-release-contract:", err)
		os.Exit(1)
	}
}

/*
================
contract

The printed contract. Fields are declared in key order, so the output
is byte-identical to the map this replaced (encoding/json sorts map
keys) and compatibility.json stays comparable line for line.
================
*/
type contract struct {
	ProtocolMax      int `json:"protocolMax"`
	ProtocolMin      int `json:"protocolMin"`
	StoreReadMax     int `json:"storeReadMax"`
	StoreReadMin     int `json:"storeReadMin"`
	StoreUpgradeFrom int `json:"storeUpgradeFrom"`
	StoreWrite       int `json:"storeWrite"`
}

/*
================
releaseContract
================
*/
func releaseContract() contract {
	return contract{
		ProtocolMax:      releaseprotocol.Current,
		ProtocolMin:      releaseprotocol.Current,
		StoreReadMax:     store.StoreReadMaxVersion,
		StoreReadMin:     store.CurrentVersion,
		StoreUpgradeFrom: store.UpgradeFromVersion,
		StoreWrite:       store.CurrentVersion,
	}
}
