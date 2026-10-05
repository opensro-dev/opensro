package enterworld

import (
	"opensro.online/server/internal/domain"
	"os"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// Monster object-list leg. Retail population is enabled by default and is
// scoped to the same native interest area used by live visibility updates.
// The environment flag is a diagnostic kill switch, not a prerequisite gate.

// MonsterSpawnConfig carries the diagnostic population kill switch.
type MonsterSpawnConfig struct {
	// Enabled is the monster population plane, ON BY DEFAULT: retail has
	// monsters, so requiring an env var to get them was itself a
	// divergence (HUMAN RULING, board seq388; flip approved as item B,
	// seq401). MISSION_SPAWN_MONSTERS=0 is the debug KILL SWITCH.
	// Emission is scoped per viewer through the registry's interest
	// seam - never spawn-everything.
	Enabled bool
}

// MonsterSpawnConfigFromEnv reads the population kill switch (unset = ON,
// "0" = off, matching the retail posture).
func MonsterSpawnConfigFromEnv() MonsterSpawnConfig {
	return MonsterSpawnConfig{
		Enabled: os.Getenv("MISSION_SPAWN_MONSTERS") != "0",
	}
}

// monsterWireDef converts a registry instance to the mission-plane row.
// Native sub_859d40 consumes name-mask bit 0 as the visible CIGIDObject
// name at +0x108, so this is the localized characterdata/textdataname value,
// never the internal MOB_* codename during a healthy retail-data load.
func monsterWireDef(instance monster.Instance, nowMs int64) simulation.MonsterDef {
	return simulation.MonsterWireDefFromInstance(instance, nowMs)
}

// MonsterObjectListInstances resolves which monster instances ride this
// bootstrap's object list: the native interest area at the login pose (the
// 320-unit block neighbourhood the tick's visibility stream also diffs
// against; see worldgeom.InterestVisible). The tick seeds from the recorded
// list rather than assuming the sets still agree after the client loads.
// Development rooms add their anchors to the normal immutable population
// template; this resolver has no synthetic at-player lane.
func (c MonsterSpawnConfig) MonsterObjectListInstances(registry *simulation.MonsterState, divisionID string, entry *LocalPlayerEntry) []monster.Instance {
	if !c.Enabled || registry == nil || entry == nil {
		return nil
	}
	return registry.PopulationInterestInstances(divisionID, entry.Population, worldgeom.RegionXZ{
		RegionID: uint16(entry.StartProfile.RegionID),
		X:        entry.StartProfile.X,
		Z:        entry.StartProfile.Z,
	}, registry.CurrentTimeMillis())
}

// RecordMonsterObjectList stores the gids a player's object list created so
// the first visibility tick reconciles against what the client holds.
func RecordMonsterObjectList(registry *simulation.MonsterState, divisionID string, character *Character, instances []monster.Instance) {
	if registry == nil || character == nil {
		return
	}
	gids := make([]uint32, len(instances))
	for i, instance := range instances {
		gids[i] = instance.Gid
	}
	registry.RecordObjectList(divisionID, simulation.PlayerObjectID(character.ID), gids)
}

// MonsterObjectListRows builds the 0x3417 create rows for the resolved
// instances (rides between the 0x30CB start and 0x330A finalize like the
// NPC and ground rows).
func MonsterObjectListRows(
	registry *simulation.MonsterState,
	divisionID string,
	instances []monster.Instance,
	nowMs int64,
	ground monster.GroundResolver,
) []Packet {
	rows := make([]Packet, 0, len(instances))
	for _, instance := range instances {
		pose := monster.Pose{
			RegionID: instance.Spawn.RegionID,
			X:        instance.Spawn.X,
			Y:        instance.Spawn.Y,
			Z:        instance.Spawn.Z,
		}
		if registry != nil {
			if mover, ok := registry.Mover(divisionID, instance.Gid); ok {
				pose = mover.LivePoseAt(nowMs, ground)
			}
		}
		row := simulation.BuildMonsterCreateRow(monsterWireDef(instance, nowMs), instance.Gid, simulation.Spawn{
			RegionID: pose.RegionID,
			X:        pose.X,
			Y:        pose.Y,
			Z:        pose.Z,
			Angle:    pose.Heading,
		})
		packet := NewPacket(OpcodeObjectListChunk, row)
		packet.Scope = []domain.ObjectScopeChange{{GID: instance.Gid, Visible: true}}
		rows = append(rows, packet)
	}
	return rows
}

// MonsterRefObjSnapshot builds the refObjSnapshot mirror rows (kind
// "monster") for the FULL spawnable roster, not just the instances on
// this object list. The client seeds its RefObj mirror ONCE per session
// before the packet loop; a refObjId first seen on a later 0x30D7 (the
// player walked into a new region) is silently dropped if it was not in
// the snapshot (WIP seq70 C contract - soft-assert at
// remoteSpawnHost.ts:405-411; incremental roster streaming has no client
// seam and would be divergence). 178 rows against the shipped v1.150
// npcpos (RZ seq234 admit set) - snapshot-sized, not object-list-sized.
func (c MonsterSpawnConfig) MonsterRefObjSnapshot(registry *simulation.MonsterState) []RefObjRow {
	if !c.Enabled || registry == nil {
		return []RefObjRow{}
	}
	refs := registry.SpawnableRefs()
	rows := make([]RefObjRow, 0, len(refs))
	for _, ref := range refs {
		rows = append(rows, RefObjRow{
			RefObjID:  ref.RefObjID,
			TidWord:   ref.TidWord,
			Codename:  ref.Codename,
			NameStrID: ref.NameStrID,
			Name:      ref.DisplayName(),
			Level:     ref.Level,
			MaxHP:     ref.MaxHP,
			Kind:      refObjKind(ref),
		})
	}
	return rows
}

/*
================
refObjKind

The mirror row's kind: fortress structures decode as CICATStruct.
================
*/
func refObjKind(ref monster.MonsterRef) string {
	if ref.Structure {
		return "structure"
	}
	return "monster"
}
