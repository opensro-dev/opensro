/*
===========================================================================

fortress_structures.go - the fortress structure roster for the asset bake

A fortress may hold any live structure: the starting ones, every upgrade
stage and every barricade a guild installs. The client loads a structure's
characterdata BSR like any actor (CICATStruct_LoadVisual 4F8200), so the
bake publishes every structure reference with hit points; the _00 sites
without them are unbuilt and never stream.

===========================================================================
*/
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
runFortressStructures
================
*/
func runFortressStructures(args []string) error {
	dir, err := resolveEvidenceTextdataDir("fortress-structures", args)
	if err != nil {
		return fmt.Errorf("textdata: %w", err)
	}
	template := monster.LoadTemplate(dir)
	refs := make([]monsterRosterRef, 0)
	for _, ref := range template.Refs {
		if ref.Structure && ref.MaxHP > 0 {
			refs = append(refs, monsterRosterRef{RefObjID: ref.RefObjID, Codename: ref.Codename})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].RefObjID < refs[j].RefObjID })
	output := monsterRosterOutput{
		Format:      "sro-fortress-structure-roster",
		Source:      "monster.LoadTemplate: characterdata rows of the CICATStruct TypeID band ((W & 0x7FE) == 0x2C6) with hit points",
		TextdataDir: dir,
		Count:       len(refs),
		Refs:        refs,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}
