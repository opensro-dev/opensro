/*
===========================================================================

unique_nests.go - the world map's unique nest table (port-only)

Port-only, not native: the v1.150 world map draws no unique nest. Prints,
as the client's /assets/data/uniqueNests.json, every nest of the base
population template that spawns a unique (monster.Template.UniqueNests).
The rule stays in Go; the asset build writes this output and decides
nothing of its own, so a server-side nest change reaches the map.

The flag-gated port-only populations (Cap110, Pharaoh Tomb, temple) are
left out: the client cannot know a shard's switches.

===========================================================================
*/
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensro.online/server/internal/game/world/monster"
)

// The client's table contract (foundation/ui/unique-nests.ts).
const (
	uniqueNestFormat  = "sro-unique-nests"
	uniqueNestVersion = 1
)

/*
================
uniqueNest
================
*/
type uniqueNest struct {
	RefObjID uint32  `json:"refObjId"`
	RegionID uint16  `json:"regionId"`
	X        float64 `json:"x"`
	Z        float64 `json:"z"`
}

/*
================
uniqueNestTable
================
*/
type uniqueNestTable struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	Nests   []uniqueNest `json:"nests"`
}

/*
================
uniqueNestTableFor

The table one template publishes, nests in template (npcpos) order.
================
*/
func uniqueNestTableFor(template monster.Template) uniqueNestTable {
	table := uniqueNestTable{Format: uniqueNestFormat, Version: uniqueNestVersion, Nests: []uniqueNest{}}
	for _, nest := range template.UniqueNests() {
		table.Nests = append(table.Nests, uniqueNest{RefObjID: nest.RefObjID, RegionID: nest.RegionID, X: nest.X, Z: nest.Z})
	}
	return table
}

/*
================
runUniqueNests
================
*/
func runUniqueNests(args []string) error {
	dir, err := resolveEvidenceTextdataDir("unique-nests", args)
	if err != nil {
		return fmt.Errorf("textdata: %w", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(uniqueNestTableFor(monster.LoadTemplate(dir))); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}
