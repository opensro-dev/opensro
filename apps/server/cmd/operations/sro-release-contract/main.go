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

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
)

// wireContracts are the client-server wire contracts one release protocol
// fixes: the EnterWorld DTO and the character-list contract.
type wireContracts struct {
	bootstrap int
	roster    int
}

// releaseProtocols names every combination a release has shipped. A client
// and a server agree when they speak the same release protocol.
var releaseProtocols = map[int]wireContracts{
	2: {bootstrap: 2, roster: 1},
	3: {bootstrap: 2, roster: 2}, // worn items as (RefItemID, plus)
}

/*
================
releaseProtocol

The release protocol of the contracts compiled into this server. A new
combination must be added to releaseProtocols (and to the client's
compatibility declaration) before it can ship: a wire change that release
admission cannot see would let one component publish without the other.
================
*/
func releaseProtocol() int {
	current := wireContracts{bootstrap: enterworld.BootstrapProtocolVersion, roster: agentapi.CharacterRosterContractVersion}
	for protocol, contracts := range releaseProtocols {
		if contracts == current {
			return protocol
		}
	}
	panic(fmt.Sprintf("wire contracts %+v have no release protocol", current))
}

/*
================
main

The current server implements one release protocol and one authority
schema. Any future compatibility range needs an actual reader before it is
advertised.
================
*/
func main() {
	protocol := releaseProtocol()
	contract := map[string]int{
		"protocolMin":  protocol,
		"protocolMax":  protocol,
		"storeReadMin": store.CurrentVersion,
		"storeReadMax": store.CurrentVersion,
		"storeWrite":   store.CurrentVersion,
	}
	if err := json.NewEncoder(os.Stdout).Encode(contract); err != nil {
		panic(err)
	}
}
