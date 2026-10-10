/*
===========================================================================

fortress_dismantle.go - a fortress guild dismisses its summoned objects and
demolishes its structures

0x71E1 actions 0x16 and 0x17. CGObjPC_HandleSiegeAction705E (519E60) skips
the NPC range check for both and hands the target object to
CSiegeFortressMgr_HandleSummonedObjectDismissal (633EC0) or
CSiegeFortressMgr_HandleStructureDemolition (634020); each reads the
fortress ID that follows. Success goes through the siege database queue and
answers from CSiegeFortress_HandleDatabaseResult (6232C0): 0x16 with
{0x16, 1}, 0x17 with {0x17, 1, zone}.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	// Low bytes of the 0x16 and 0x17 refusals (633EC0, 634020).
	fortressErrNotSiegeObject   uint8 = 0x2a // 0x282A
	fortressErrNotStructure     uint8 = 0x2b // 0x282B
	fortressErrNotOwnGuild      uint8 = 0x2c // 0x282C
	fortressErrDismissRole      uint8 = 0x2e // 0x282E
	fortressErrDemolishRole     uint8 = 0x2f // 0x282F
	fortressRoleCommander       uint8 = 1    // CGuildMember_IsFortressRole1 (5CFFB0)
	fortressRoleSubCommander    uint8 = 2    // CGuildMember_IsFortressRole2 (5CFFC0)
	fortressRoleBattleCommander uint8 = 4    // CGuildMember_IsFortressRole4 (5CFFD0)
)

/*
================
guildFortressRole

The character's fortress role byte from its guild member record (the
native member record the role predicates read); 0 without one. Each
predicate compares the byte for equality, not as a mask.
================
*/
func (rt *Runtime) guildFortressRole(division string, c *enterworld.Character) uint8 {
	if rt.Guilds == nil || c.GuildID == nil {
		return 0
	}
	_, members, found := rt.Guilds.Guild(division, *c.GuildID)
	if !found {
		return 0
	}
	for _, member := range members {
		if member.CharID == c.ID {
			return member.FortressRole
		}
	}
	return 0
}

/*
================
siegeObject

A fortress-summoned object (vfunc +0x3C4) and the guild it belongs to
(vfunc +0x648).
================
*/
type siegeObject struct {
	guildID int64
}

/*
================
fortressSiegeObject

The summoned siege object gid names. The port summons none yet: the
trainer's products (actions 0x11..0x14) create them (#483), so every
target answers false and 0x16 refuses 0x282A until then.
================
*/
func (rt *Runtime) fortressSiegeObject(_ string, _ uint32) (siegeObject, bool) {
	return siegeObject{}, false
}

/*
================
releaseSiegeObject

SiegeObject_Release (621FD0): the object leaves its fortress and the world.
The summoned-object owner (#483) supplies it with the objects themselves;
until then there is nothing to release.
================
*/
func (rt *Runtime) releaseSiegeObject(_ string, _ uint32) bool {
	return false
}

/*
================
fortressDismissObject

633EC0 in its refusal order: not a summoned siege object (0x282A), another
guild's (0x282C), a member that is neither commander nor battle commander
(0x282E), an unknown fortress (3). Success releases the object through
the siege queue (SiegeObject_Release 621FD0, query 0x11), whose result
answers {0x16, 1} (6232C0); the release belongs to the object's owner
(#483).
================
*/
func (rt *Runtime) fortressDismissObject(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	object, ok := rt.fortressSiegeObject(division, request.Target)
	if !ok {
		return fortressRefusal(request.Action, fortressErrNotSiegeObject)
	}
	if c.GuildID == nil || *c.GuildID != object.guildID {
		return fortressRefusal(request.Action, fortressErrNotOwnGuild)
	}
	role := rt.guildFortressRole(division, c)
	if role != fortressRoleCommander && role != fortressRoleBattleCommander {
		return fortressRefusal(request.Action, fortressErrDismissRole)
	}
	if rt.Fortresses == nil {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	if _, exists := rt.Fortresses.Get(division, request.Fortress); !exists {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	if !rt.releaseSiegeObject(division, request.Target) {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: []byte{request.Action, 1}}}}
}

/*
================
fortressStructureOwner

The fortress whose world holds structure gid, with that world and the
structure's guild (vfunc +0x644: the fortress holder).
================
*/
func (rt *Runtime) fortressStructureOwner(division string, gid uint32) (uint32, instance.ID, monster.Instance, int64, bool) {
	for fortressID, world := range rt.fortressWorlds() {
		for _, row := range rt.Monsters.WorldStructures(division, world) {
			if row.Gid != gid {
				continue
			}
			record, _ := rt.Fortresses.Get(division, fortressID)
			return fortressID, world, row, record.Holder(), true
		}
	}
	return 0, 0, monster.Instance{}, 0, false
}

/*
================
fortressDemolishStructure

634020 in its refusal order: not a siege structure (0x282B), another
guild's (0x282C), a member that is none of commander, sub-commander and
battle commander (0x282F), then an unknown fortress or a structure whose
state is already 1 (3). Success empties the zone (the structure leaves the
fortress and the world, 6232C0 case 0x15), stores the vacancy and answers
{0x17, 1, zone}.
================
*/
func (rt *Runtime) fortressDemolishStructure(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	fortressID, world, row, holder, ok := rt.fortressStructureOwner(division, request.Target)
	if !ok {
		return fortressRefusal(request.Action, fortressErrNotStructure)
	}
	if c.GuildID == nil || holder == 0 || *c.GuildID != holder {
		return fortressRefusal(request.Action, fortressErrNotOwnGuild)
	}
	role := rt.guildFortressRole(division, c)
	if role != fortressRoleCommander && role != fortressRoleSubCommander && role != fortressRoleBattleCommander {
		return fortressRefusal(request.Action, fortressErrDemolishRole)
	}
	// CSiegeFortress_FindStructureByKey (6227A0) looks the structure up in
	// the requested fortress; vfunc +0x654 is its state.
	if _, exists := rt.Fortresses.Get(division, request.Fortress); !exists || request.Fortress != fortressID ||
		row.StructureState == structureDestroyedMask {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	zone := row.Nest.EventStructID
	if !rt.Monsters.SetStructureOccupant(division, world, zone, 0, rt.Now().UnixMilli()) {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	rt.storeStructureVacancy(division, fortressID, zone, holder)
	reply := wire.NewWriter(6).U8(request.Action).U8(1).U32(zone)
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: reply.Payload()}}}
}

/*
================
storeStructureVacancy

The vacated zone's row: RefObjID 0 (QUERY_SIEGE_STRUCT_REMOVE's effect).
The periodic save writes standing structures only, so the row is written
here and remembered as saved.
================
*/
func (rt *Runtime) storeStructureVacancy(division string, fortressID, zone uint32, holder int64) {
	if rt.FortressStore == nil {
		return
	}
	record := domain.FortressStructureRecord{FortressID: fortressID, EventStructID: zone, OwnerGuildID: holder}
	rt.fortressPersistMu.Lock()
	defer rt.fortressPersistMu.Unlock()
	if rt.FortressStore.SaveFortressStructure(division, record, true) != nil {
		return
	}
	if rt.fortressPersist.saved == nil {
		rt.fortressPersist.saved = map[savedStructureKey]domain.FortressStructureRecord{}
	}
	rt.fortressPersist.saved[savedStructureKey{division, fortressID, zone}] = record
}
