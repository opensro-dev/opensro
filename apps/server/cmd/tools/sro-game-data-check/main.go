/*
===========================================================================

main.go - sro-game-data-check: will this server release start on this data?

Shipped in the server release bundle (ops/release/bundle.py). The release
deploy runs it with SRO_SERVER_GAME_DATA_ROOT naming the archive the new
GameWorld will open, before any notice or stop; a non-zero exit refuses
the release while the old server keeps running. See gamedatacheck.

===========================================================================
*/

package main

import (
	"fmt"
	"os"

	"opensro.online/server/internal/game/gamedatacheck"
)

func main() {
	result, err := gamedatacheck.Check()
	if err != nil {
		fmt.Fprintf(os.Stderr, "server game data refused: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("server game data accepted: manifest %s (%s)\n", result.ManifestDigest, result.BundleRoot)
}
