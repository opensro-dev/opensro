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
	"opensro.online/server/internal/game/enterworld"
)

/*
================
main

The current server implements one bootstrap protocol and one authority schema.
Any future compatibility range needs an actual reader before it is advertised.
================
*/
func main() {
	contract := map[string]int{
		"protocolMin":  enterworld.BootstrapProtocolVersion,
		"protocolMax":  enterworld.BootstrapProtocolVersion,
		"storeReadMin": store.CurrentVersion,
		"storeReadMax": store.CurrentVersion,
		"storeWrite":   store.CurrentVersion,
	}
	if err := json.NewEncoder(os.Stdout).Encode(contract); err != nil {
		panic(err)
	}
}
