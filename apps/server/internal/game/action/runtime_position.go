/*
===========================================================================

runtime_position.go - where a character stands, and writing it back

Split from runtime.go on its own responsibility: reading a character's
mutable state through the authority door (characterSnapshot) and every
question about its live pose - spawn resolution, navigation constraint
and ground-walk admission - ending in the record write-back
(writeBackWorld). The movement lane's reads and the kill lane's victim
lookups cross the package through these; nothing else lives here.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
characterSnapshot

characterSnapshot copies mutable character state while the authority read
door is held. Identity fields (ID and Name) never change after creation,
but inventory, progression, deletion state, and world state must never be
read through the live pointer outside a door.
==================
*/
func (rt *Runtime) characterSnapshot(
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if character == nil {
		return nil
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})

	return snapshot
}

/*
==================
characterSnapshotAdmitted

characterSnapshotAdmitted copies mutable character state while the
authority read door is held, but only when admit accepts the live
record: a rejected target never pays for its detached copy (#1820
part C). admit reads record fields only - it opens no door, the same
contract every read-door visitor already honours - so its verdict
weighs the same inputs characterSnapshot's copy would carry. The
copy's only normalizations (stat points, mission mask) feed neither
the life, deletion, world-instance nor companion facts admission
reads, and an admitted record detaches exactly as characterSnapshot
detaches it: same door, same publication lock, same copy.
==================
*/
func (rt *Runtime) characterSnapshotAdmitted(
	divisionID string,
	character *enterworld.Character,
	admit func(*enterworld.Character) bool,
) *enterworld.Character {
	if character == nil {
		return nil
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		if admit != nil && !admit(character) {
			return
		}
		snapshot = character.Snapshot()
	})

	return snapshot
}

/*
==================
characterSnapshotEquipment

characterSnapshot for readers that never leave the worn equipment and
never read the avatar inventory: the copy keeps only the equipment
rows and no AvatarInventory. Convert a call site only when every
reader of the copy is cleared for the narrow rows root by root (#1943
transitive audit) - a bag or avatar reader would silently see its
rows missing. Companion resolution walks bag rows (Companions reaches
the row Summon records), so a reader of this copy must never resolve
companions.
==================
*/
func (rt *Runtime) characterSnapshotEquipment(
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if character == nil {
		return nil
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		snapshot = character.SnapshotEquipment()
	})

	return snapshot
}

/*
==================
characterSnapshotCombat

characterSnapshot for the combat roots (#1943): equipment rows and the
whole avatar inventory, no bag rows. A root takes it only when no
reader of its copy reads a bag row or resolves companions; the
combat-copy differential test pins each converted root against the full
copy.
==================
*/
func (rt *Runtime) characterSnapshotCombat(
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if character == nil {
		return nil
	}
	if rt.combatCopy != nil {
		return rt.combatCopy(divisionID, character)
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		snapshot = character.SnapshotCombat()
	})

	return snapshot
}

/*
==================
characterSnapshotCombatAdmitted

characterSnapshotCombat with characterSnapshotAdmitted's door
contract: the copy is detached only when admit accepts the live
record. A root takes it under the same audit as characterSnapshotCombat
- no reader of the copy reads a bag row or resolves companions. The
combatCopy seam replaces the copy in tests exactly as it does for
characterSnapshotCombat; admission still judges the live record
first, in its own door pass, so a refused record pays for no copy
under the seam either.
=================
*/
func (rt *Runtime) characterSnapshotCombatAdmitted(
	divisionID string,
	character *enterworld.Character,
	admit func(*enterworld.Character) bool,
) *enterworld.Character {
	if character == nil {
		return nil
	}
	if rt.combatCopy != nil {
		admitted := admit == nil
		if !admitted {
			rt.deps.Read(divisionID, func() {
				admitted = admit(character)
			})
		}
		if !admitted {
			return nil
		}
		return rt.combatCopy(divisionID, character)
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		if admit != nil && !admit(character) {
			return
		}
		snapshot = character.SnapshotCombat()
	})

	return snapshot
}

/*
==================
characterSnapshotCompanionAdmitted

characterSnapshotAdmitted for the per-tick pet and brood passes
(#2030): the pet-owner view (domain SnapshotCompanion) - identity,
world, companions, life and status, the PvP and relation facts, and
the inventory's companion rows and relation slot. A root takes it only
when every reader of its copy reads within that list; companion
resolution and the relation rules see exactly what the full copy
shows. The companionCopy seam replaces the copy in tests
exactly as combatCopy does for the combat roots; admission still
judges the live record first, in its own door pass, so a refused
record pays for no copy under the seam either.
==================
*/
func (rt *Runtime) characterSnapshotCompanionAdmitted(
	divisionID string,
	character *enterworld.Character,
	admit func(*enterworld.Character) bool,
) *enterworld.Character {
	if character == nil {
		return nil
	}
	if rt.companionCopy != nil {
		admitted := admit == nil
		if !admitted {
			rt.deps.Read(divisionID, func() {
				admitted = admit(character)
			})
		}
		if !admitted {
			return nil
		}
		return rt.companionCopy(divisionID, character)
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		if admit != nil && !admit(character) {
			return
		}
		snapshot = character.SnapshotCompanion()
	})

	return snapshot
}

/*
==================
characterLifeState

The life facts a refused monster strike revalidates - alive, deletion
pending, native body status - read as values under the authority read
door. The strike lane used to detach the whole record for these three
fields (#1820 part C). Aliveness weighs the same inputs on the live
record as on the copy: the copy's only normalizations (stat points,
mission mask) feed neither vitals derivation nor these fields.
==================
*/
func (rt *Runtime) characterLifeState(
	divisionID string,
	character *enterworld.Character,
) (alive, deletePending bool, nativeBodyStatus uint8) {
	if character == nil {
		return false, false, 0
	}

	rt.deps.Read(divisionID, func() {
		alive = enterworld.CharacterAlive(character)
		deletePending = character.DeletePending
		nativeBodyStatus = character.NativeBodyStatus
	})

	return alive, deletePending, nativeBodyStatus
}

/*
==================
characterPartyJobClass

The party job class a job-admission refusal weighs, read as a value
under the authority read door. A COS target answers the job test with
its owner's state, and the owner's whole record used to be detached
for this one byte (#1943): the class derives from the dressed job
suit row, a record field the copy would carry unchanged.
==================
*/
func (rt *Runtime) characterPartyJobClass(
	divisionID string,
	character *enterworld.Character,
) uint8 {
	if character == nil {
		return enterworld.NoJobClass
	}

	class := enterworld.NoJobClass

	rt.deps.Read(divisionID, func() {
		class = enterworld.PartyJobClass(character)
	})

	return class
}

/*
=================
mountedCosGID

The movement gate's mounted question, read as values under the
authority read door. The gate used to detach the whole record -
equipment, bag rows and companions - to read the active COS's mounted
flag and GID (#1943); both are plain record fields the copy would
carry unchanged. An absent character or COS answers unmounted.
=================
*/
func (rt *Runtime) mountedCosGID(
	divisionID, characterName string,
) (petGID uint32, mounted bool) {
	character := rt.findCharacter(divisionID, characterName)
	if character == nil {
		return 0, false
	}

	rt.deps.Read(divisionID, func() {
		if character.ActiveCOS != nil {
			petGID, mounted = character.ActiveCOS.GID, character.ActiveCOS.Mounted
		}
	})

	return petGID, mounted
}

/*
==================
writeBackWorld

writeBackWorld persists the accepted ground pose onto the character record.
Authored displacement state keeps its existing destination semantics; the
ordinary ground mover never persists a destination it has not reached.

The write-back owns ONLY spawn/movementMode/spawnSet; every other world
field (dungeonMinimap, movementSourceSeeded, updatedAt, moveSegment echo,
and any future record keys) copies through untouched, like the Node
handlers' {...world} spread (item-lane mirror of SCOUT-B FINDING 500).
Copy-then-swap so aliases of the old record (the character snapshot's
shallow copy) stay unchanged.
==================
*/
func writeBackWorld(character *enterworld.Character, state simulation.WorldState) {
	spawn := state.PersistedSpawn()
	regionID := int64(spawn.RegionID)
	x, y, z := spawn.X, spawn.Y, spawn.Z
	angle := int64(spawn.Angle)
	mode := int64(state.MovementMode)
	next := enterworld.CharacterWorld{}
	if character.World != nil {
		next = *character.World
	}

	next.Spawn = &enterworld.WorldSpawn{
		RegionID: &regionID,
		X:        &x,
		Y:        &y,
		Z:        &z,
		Angle:    &angle,
	}

	next.MovementMode = &mode
	next.SpawnSet = state.SpawnSet
	character.World = &next
}

/*
==================
liveSpawn

liveSpawn samples the character's LIVE position - the bug-D plane. Every
position-dependent op reads THIS, never the goal in WorldState.Spawn.
==================
*/
func (rt *Runtime) liveSpawn(
	worldKey string,
	character *enterworld.Character,
	nowMs int64,
) simulation.Spawn {
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})

	return snapshot.LiveSpawnAt(nowMs)
}

/*
==================
LiveSpawnFor

The character's live position now, for lanes outside action that gate on
native range (the party invite's hit-range check).
==================
*/
func (rt *Runtime) LiveSpawnFor(divisionID string, character *enterworld.Character) simulation.Spawn {
	return rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli())
}

/*
==================
liveNav

liveNav is liveSpawn plus the surface owner retained at that point.
==================
*/
func (rt *Runtime) liveNav(
	worldKey string,
	character *enterworld.Character,
	nowMs int64,
) (simulation.Spawn, simulation.NavOwner) {
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})

	return snapshot.LiveSpawnAt(nowMs), snapshot.LiveOwnerAt(nowMs)
}

/*
==================
constrainWalk

constrainWalk routes a character move through the owner-aware constraint,
degrading to the owner-less one only in compositions that lack it.
==================
*/
func (rt *Runtime) constrainWalk(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if rt.ConstrainWalk != nil {
		return rt.ConstrainWalk(characterName, from, fromOwner, to)
	}

	if rt.ConstrainMovement != nil {
		committed, refusal := rt.ConstrainMovement(characterName, from, to)
		return committed, simulation.NavWalk{}, refusal
	}

	return to, simulation.NavWalk{}, nil
}

/*
==================
admitGroundWalk

Player approaches retain their intended goal while the world owner checks
each elapsed movement step. A skill displacement still resolves its whole
authored chord through constrainWalk before it commits.
==================
*/
func (rt *Runtime) admitGroundWalk(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if rt.AdmitGroundWalk != nil {
		return rt.AdmitGroundWalk(characterName, from, fromOwner, to)
	}
	return rt.constrainWalk(characterName, from, fromOwner, to)
}

/*
==================
samePlacement

samePlacement compares where a move lands. Walked goals stand on their
surface's height, so the requested int16 Y is not part of the comparison.
==================
*/
func samePlacement(a, b simulation.Spawn) bool {
	return a.RegionID == b.RegionID && a.X == b.X && a.Z == b.Z
}
