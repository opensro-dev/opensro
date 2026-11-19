/*
===========================================================================

muhan_npcs.go - the Muhan arenas' NPCs as world-scoped NPCs

Port-only, not native (#3314, #3349): present only while SRO_MUHAN is
on. Each of the eight arena worlds holds a manager and two change NPCs
(NPC_MUHAN_*), authored as nests in their world (muhan-npcs.txt,
scripts/muhan_import.py).

Their characterdata rows are NPC class (TID 1/2/2, the CICNPC branch the
roster loader admits) with no hit points, so they are NPCs, never
monster-population nests: a nest instance with zero hit points is a
corpse to every liveness check, and the client decodes a CICNPC row,
not a monster row with a grade byte. All eight arenas share region
27091, so they cannot join the static roster either: each stands only
in its own arena world, through the world NPCs (WorldNpc) the action
runtime publishes beside the quest gates.

This file loads them only. Which of them change a player, and how, is
the pairing's (action/muhan_change.go, which gives those Talk).
INFERENCE, recorded on #3349: neither binary shows a manager's
function, so a manager offers what its row resolves to.

===========================================================================
*/
package simulation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/muhan"
)

const (
	// muhanNpcsFile is the import's NPC nest file (Version 2 rows).
	muhanNpcsFile    = "muhan-npcs.txt"
	muhanNpcsVersion = 2
	// muhanNpcPrefix is the arena NPC family.
	muhanNpcPrefix = "NPC_MUHAN_"

	// muhanNpcGIDBase starts the arena NPC band inside the static NPC
	// range: above the Magic Pop machines and operators (npcmagicpop.go,
	// 240000..240031) and below the teleport gates at 250000 + RefObjID.
	// One identity per row, stable in the file's authored order; the
	// same identity stands in every layer of its arena world.
	muhanNpcGIDBase uint32 = 241000
	muhanNpcGIDSpan        = 1000
)

/*
================
WorldNpc

A static NPC that stands only in one world definition's layers, never
in the field roster: a viewer sees it only while its own world
instance is of that definition.
================
*/
type WorldNpc struct {
	World instance.DefinitionID
	Npc   NpcDef
}

/*
================
muhanNpcRow

The fields of one muhan-npcs.txt nest an NPC reads. The import writes
the full nest row; the rest is monster tactics an NPC has none of.
================
*/
type muhanNpcRow struct {
	Codename string  `json:"Codename"`
	WorldID  int     `json:"WorldID"`
	RegionID uint16  `json:"RegionID"`
	X        float64 `json:"X"`
	Y        float64 `json:"Y"`
	Z        float64 `json:"Z"`
	MaxCount int     `json:"MaxCount"`
}

/*
================
LoadMuhanArenaNpcs

Off never reads the file. On refuses the boot when the file is absent
or any row does not resolve: the managers and change NPCs are how
anyone uses an arena, so a partial set must not reach gameplay.
================
*/
func LoadMuhanArenaNpcs(textdataDir string, enabled bool) ([]WorldNpc, error) {
	if !enabled {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, muhanNpcsFile))
	if err != nil {
		return nil, fmt.Errorf("muhan npcs: %w", err)
	}
	return parseMuhanArenaNpcs(data, loadNpcWorldRefs(textdataDir))
}

/*
================
parseMuhanArenaNpcs

Every row names an NPC_MUHAN_* NPC-class reference, a registered arena
world and one standing NPC; a reference stands once per world.
================
*/
func parseMuhanArenaNpcs(data []byte, refs map[uint32]npcWorldRef) ([]WorldNpc, error) {
	var document struct {
		Version int           `json:"Version"`
		Rows    []muhanNpcRow `json:"Rows"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("muhan npcs: %w", err)
	}
	if document.Version != muhanNpcsVersion || len(document.Rows) == 0 {
		return nil, fmt.Errorf("muhan npcs: version %d with %d rows, want version %d with rows",
			document.Version, len(document.Rows), muhanNpcsVersion)
	}
	if len(document.Rows) > muhanNpcGIDSpan {
		return nil, fmt.Errorf("muhan npcs: %d rows exceed the %d-identity band", len(document.Rows), muhanNpcGIDSpan)
	}
	byCodename := make(map[string]npcWorldRef, len(refs))
	for _, ref := range refs {
		byCodename[ref.codename] = ref
	}
	seen := make(map[string]bool, len(document.Rows))
	npcs := make([]WorldNpc, 0, len(document.Rows))
	for index, row := range document.Rows {
		ref, found := byCodename[row.Codename]
		if !found || !strings.HasPrefix(row.Codename, muhanNpcPrefix) {
			return nil, fmt.Errorf("muhan npcs: %q is not an NPC_MUHAN_* NPC reference", row.Codename)
		}
		if _, arena := muhan.Lookup(row.WorldID); !arena {
			return nil, fmt.Errorf("muhan npcs: %s stands in unknown world %d", row.Codename, row.WorldID)
		}
		key := fmt.Sprintf("%d/%s", row.WorldID, row.Codename)
		if row.MaxCount != 1 || seen[key] {
			return nil, fmt.Errorf("muhan npcs: %s must stand once in world %d", row.Codename, row.WorldID)
		}
		seen[key] = true
		spawn := Spawn{RegionID: row.RegionID, X: row.X, Y: row.Y, Z: row.Z}
		npc := newWorldNpc(ref, muhanNpcGIDBase+uint32(index), spawn, worldNpcLookups{})
		npcs = append(npcs, WorldNpc{World: instance.DefinitionID(row.WorldID), Npc: npc})
	}
	return npcs, nil
}
