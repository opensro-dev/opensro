// The spawnable-npcs subcommand prints the mission server's data-authored NPC
// object-list roster as JSON. Asset builders consume this command instead of
// maintaining a separate roster.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

type npcRosterRef struct {
	RefObjID uint32 `json:"refObjId"`
	Codename string `json:"codename"`
}

type npcRosterOutput struct {
	Format      string         `json:"format"`
	Source      string         `json:"source"`
	TextdataDir string         `json:"textdataDir"`
	Count       int            `json:"count"`
	Refs        []npcRosterRef `json:"refs"`
}

func runSpawnableNPCs(args []string) error {
	dir, err := resolveEvidenceTextdataDir("spawnable-npcs", args)
	if err != nil {
		return fmt.Errorf("textdata: %w", err)
	}

	roster := simulation.LoadNpcWorldRoster(dir)
	if len(roster) == 0 {
		return fmt.Errorf("npc roster: no enabled NPC positions resolved under %s", dir)
	}
	refs := make([]npcRosterRef, 0, len(roster))
	seenRefObjIDs := make(map[uint32]struct{}, len(roster))
	for _, npc := range roster {
		// The runtime roster is position-shaped: repeated authored placements
		// intentionally become separate world objects. This command is consumed
		// by model/resource builders, whose identity is RefObjID-shaped. Publish
		// each model type once while retaining deterministic first-seen order.
		if _, duplicate := seenRefObjIDs[npc.RefObjID]; duplicate {
			continue
		}
		seenRefObjIDs[npc.RefObjID] = struct{}{}
		refs = append(refs, npcRosterRef{
			RefObjID: npc.RefObjID,
			Codename: npc.Codename,
		})
	}
	// Port-only, not native: the seasonal looks a placed NPC may wear
	// (simulation/npc_looks.go) spawn under their own reference, so their
	// models are published with the placed ones. Not placed themselves,
	// they come after every placed type.
	for _, codename := range progression.SeasonalLookCodenames() {
		look, ok := simulation.LoadNpcLookRef(dir, codename)
		if !ok {
			continue
		}
		if _, duplicate := seenRefObjIDs[look.RefObjID]; duplicate {
			continue
		}
		seenRefObjIDs[look.RefObjID] = struct{}{}
		refs = append(refs, npcRosterRef{RefObjID: look.RefObjID, Codename: look.Codename})
	}
	output := npcRosterOutput{
		Format:      "sro-spawnable-npc-roster",
		Source:      "simulation.LoadNpcWorldRoster: unique RefObj types from enabled npcpos.txt positions joined with the native NPC TID gate and characterdata, then the seasonal look references (simulation.LoadNpcLookRef)",
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
