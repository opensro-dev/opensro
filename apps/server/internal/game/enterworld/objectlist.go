package enterworld

import (
	"opensro.online/server/internal/domain"
	"os"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/simulation"
)

// Object-list row assembly (the 0x3417 chunks between 0x30CB start and
// 0x330A finalize): division ground drops always, plus the NPC roster when
// MISSION_SPAWN_NPCS=1 (buildMissionObjectListPackets order: NPC rows first,
// then ground rows).

// NpcSpawnConfig controls whether and where the configured NPC roster spawns.
type NpcSpawnConfig struct {
	// Enabled makes NPC create rows ride the object
	// list and the refObjSnapshot carries their PK2 mirror rows.
	Enabled bool
	// AtPlayer anchors the roster at the start
	// profile instead of the fixed Constantinople shop anchor (shared
	// servers keep getting Balbardo parked on playtesters otherwise).
	AtPlayer bool
	// Roster is the immutable shipped npcpos/characterdata projection. An
	// enabled production composition must provide a non-empty roster.
	Roster []simulation.NpcDef
}

// NpcSpawnConfigFromEnv reads the NPC spawn policy.
func NpcSpawnConfigFromEnv() NpcSpawnConfig {
	return NpcSpawnConfig{
		// Static world population is normal gameplay, not an opt-in diagnostic.
		// MISSION_SPAWN_NPCS=0 is the explicit emergency kill switch.
		Enabled:  os.Getenv("MISSION_SPAWN_NPCS") != "0",
		AtPlayer: os.Getenv("MISSION_SPAWN_NPCS_AT_PLAYER") == "1",
	}
}

func (c NpcSpawnConfig) roster() []simulation.NpcDef {
	return c.Roster
}

// Anchor ports resolveMissionNpcSpawnAnchor: the player's start placement
// with AtPlayer, else the fixed Constantinople shop anchor.
func (c NpcSpawnConfig) Anchor(start simulation.Spawn) simulation.Spawn {
	if c.AtPlayer {
		return start
	}
	return simulation.NpcShopSpawn()
}

// npcSpawnAnchor is Anchor for the entry's start profile.
func (c NpcSpawnConfig) npcSpawnAnchor(entry *LocalPlayerEntry) simulation.Spawn {
	return c.Anchor(simulation.Spawn{
		RegionID: uint16(entry.StartProfile.RegionID),
		X:        entry.StartProfile.X,
		Y:        entry.StartProfile.Y,
		Z:        entry.StartProfile.Z,
		Angle:    uint16(entry.StartProfile.Angle),
	})
}

// NpcObjectListRows builds the 0x3417 NPC create rows for the roster.
func (c NpcSpawnConfig) NpcObjectListRows(entry *LocalPlayerEntry) []Packet {
	if !c.Enabled {
		return nil
	}
	anchor := c.npcSpawnAnchor(entry)
	viewer := simulation.Spawn{RegionID: uint16(entry.StartProfile.RegionID), X: entry.StartProfile.X, Y: entry.StartProfile.Y, Z: entry.StartProfile.Z}
	roster := c.roster()
	rows := make([]Packet, 0, len(roster))
	for _, npc := range roster {
		if !simulation.NpcVisibleAt(npc, viewer) {
			continue
		}
		row := NewPacket(OpcodeObjectListChunk, simulation.BuildNpcCreateRow(npc, anchor))
		row.Scope = []domain.ObjectScopeChange{{GID: npc.ObjectID, Visible: true}}
		rows = append(rows, row)
	}
	return rows
}

// NpcRefObjSnapshot ports buildMissionRefObjSnapshot: the RefObjData/TID
// mirror rows for the object-list NPCs; empty when spawns are disabled
// (nothing on the wire to seed).
func (c NpcSpawnConfig) NpcRefObjSnapshot() []RefObjRow {
	if !c.Enabled {
		return []RefObjRow{}
	}
	roster := c.roster()
	rows := make([]RefObjRow, 0, len(roster))
	seen := make(map[uint32]bool)
	for _, npc := range roster {
		if seen[npc.RefObjID] {
			continue
		}
		seen[npc.RefObjID] = true
		var storeGroups *[]simulation.NpcTalkStoreGroup
		if len(npc.NpcTalkStoreGroups) != 0 {
			copyOfGroups := append(
				[]simulation.NpcTalkStoreGroup(nil),
				npc.NpcTalkStoreGroups...,
			)
			storeGroups = &copyOfGroups
		}
		kind := "npc"
		if npc.Teleport != nil {
			kind = "teleport"
		}
		rows = append(rows, RefObjRow{
			Teleport:           npc.Teleport,
			RefObjID:           npc.RefObjID,
			TidWord:            npc.TidWord,
			Codename:           npc.Codename,
			NameStrID:          npc.NameStrID,
			Name:               npc.Name,
			Level:              npc.Level,
			MaxHP:              npc.MaxHP,
			Kind:               kind,
			NpcTalkStoreGroups: storeGroups,
		})
	}
	return rows
}

// GroundObjectListRows builds the 0x3417 chunks for a division's ground
// drops (the CIItem rows, same wire the 0x30D7 single spawn uses minus the
// appear tail - the list path skips it).
func GroundObjectListRows(items []grounditem.Item) []Packet {
	rows := make([]Packet, 0, len(items))
	for _, item := range items {
		row := NewPacket(OpcodeObjectListChunk, item.SpawnRow(false).Encode())
		row.Scope = []domain.ObjectScopeChange{{GID: item.Gid, Visible: true}}
		rows = append(rows, row)
	}
	return rows
}
