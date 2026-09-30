/*
===========================================================================

main.go - sro-item-catalog: export the server's item references as JSON

Exports the same item references the server and the GM command composer
use, for the operations dashboard (apps/server-observatory). The textdata
comes from the verified server game-data projection (gamedata.Resolve,
which also opens the packed .srogz), unless -textdata names a directory.

===========================================================================
*/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/gamedata"
)

/*
================
textdataDir
================
*/
func textdataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	paths, err := gamedata.Resolve()
	if err != nil {
		return "", fmt.Errorf("server game data: %w", err)
	}
	return paths.TextdataDir, nil
}

/*
================
main
================
*/
func main() {
	override := flag.String("textdata", "", "Server textdata directory (default: the verified game-data projection)")
	flag.Parse()
	dir, err := textdataDir(*override)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	source := enterworld.NewTextdataItems(dir)
	rows := make([]map[string]any, 0)
	for _, command := range source.ItemCommandReferences() {
		item, ok := source.ItemRefByCodename(command.Codename)
		if !ok || command.RefObjID == 0 {
			continue
		}
		rows = append(rows, map[string]any{
			"id": command.RefObjID, "codename": command.Codename, "name": item.Name,
			"icon": item.Icon, "descriptionSymbol": item.DescriptionSymbol,
			"typeFlags": command.TypeFlags, "maxStack": command.MaxStack,
			"country": item.Country, "sex": item.RequiredSex, "requiredStr": item.RequiredStr,
			"requiredInt": item.RequiredInt, "requirementTypes": item.ReqQuadTypes,
			"requirementValues": item.ReqQuadValues, "recoveryHP": item.RecoveryHP,
			"recoveryMP": item.RecoveryMP, "recoveryHPPercent": item.RecoveryHPPercent,
			"recoveryMPPercent": item.RecoveryMPPercent,
		})
	}
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "No server item references loaded")
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"version": 1, "generatedAt": time.Now().UTC(), "source": "v1.150 server item references", "items": rows}); err != nil {
		panic(err)
	}
}
