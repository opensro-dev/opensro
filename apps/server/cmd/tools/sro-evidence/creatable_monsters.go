/*
===========================================================================

creatable_monsters.go - the monster roster the asset bake must cover

Prints, as JSON, every monster reference the server can create
(monster.Template.CreatableRefs): the populated nests and summons, and
every other characterdata monster a GM's LOADMONSTER or a quest script may
name. The native client draws any of them on demand, so the bake covers
them all and the browser's reference catalogue publishes them all (#369).
The classifier stays in Go; asset builders need none of their own.

===========================================================================
*/
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
monsterRosterRef
================
*/
type monsterRosterRef struct {
	RefObjID           uint32 `json:"refObjId"`
	Codename           string `json:"codename"`
	RideModelPath      string `json:"rideModelPath,omitempty"`
	RiderTransformMode uint8  `json:"riderTransformMode,omitempty"`
	// TradeAppearance marks a thief or hunter: dressed from the trade
	// equipment table (861720), not drawn from a BSR.
	TradeAppearance bool `json:"tradeAppearance,omitempty"`
}

/*
================
monsterRosterOutput
================
*/
type monsterRosterOutput struct {
	Format      string             `json:"format"`
	Source      string             `json:"source"`
	TextdataDir string             `json:"textdataDir"`
	Count       int                `json:"count"`
	Refs        []monsterRosterRef `json:"refs"`
}

/*
================
runCreatableMonsters
================
*/
func runCreatableMonsters(args []string) error {
	dir, err := resolveEvidenceTextdataDir("creatable-monsters", args)
	if err != nil {
		return fmt.Errorf("textdata: %w", err)
	}
	refs := monster.LoadTemplate(dir).CreatableRefs()
	output := monsterRosterOutput{
		Format:      "sro-creatable-monster-roster",
		Source:      "monster.LoadTemplate(...).CreatableRefs: every characterdata row behind the binary CICMonster TypeID gate ((W & 0x7FE) == 0x0C6)",
		TextdataDir: dir,
		Count:       len(refs),
		Refs:        make([]monsterRosterRef, 0, len(refs)),
	}
	for _, ref := range refs {
		output.Refs = append(output.Refs, monsterRosterRef{
			RefObjID:           ref.RefObjID,
			Codename:           ref.Codename,
			RideModelPath:      ref.RideModelPath,
			RiderTransformMode: ref.RiderTransformMode,
			TradeAppearance:    ref.TradeAppearance(),
		})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}
