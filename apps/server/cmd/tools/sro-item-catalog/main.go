// Export the same item references used by the server and GM command composer.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

func main() {
	dir := flag.String("textdata", ".generated/game-data/1.150/server/textdata", "Verified server textdata directory")
	flag.Parse()
	source := enterworld.NewTextdataItems(*dir)
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
