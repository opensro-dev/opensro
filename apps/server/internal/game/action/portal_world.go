/*
===========================================================================

portal_world.go - teleports that cross into another game world

A teleport destination names its RefGameWorld (teleportdata GenWorldID).
CGObjPC_HandleTeleportUseRequest0x705A_Body (4F2B50) sends a teleport into
a siege world through the fortress entry rules, asks the destination world
whether it takes the PC (CGameWorld_CheckTransfer 5EC5B0), then moves the
PC into layer 1 (CGameWorld_SelectResidentLayerOne 5ECF90) through the PC's
world teleport (vtable +0x378). Refusals are the 0x1Cxx teleport errors;
the wire carries their low byte (portalNotice, category 13).

===========================================================================
*/
package action

import (
	"fmt"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
)

const (
	// 0x1C1A UIIT_MSG_FORT_ENTRANCE_FORTRESS_FAIL_01: a fortress gate refuses
	// a PC whose job state is not 4 (none) - one wearing a job suit.
	portalFortressJobSuit byte = 0x1a
	// 0x1C1D UIIT_MSG_FORT_NOT_POSSESS_FORTRESS_MEMBER: a fortress's other
	// portals (its portal stones and town portal) serve the owning guild.
	portalFortressOwnersOnly byte = 0x1d
	// 0x1C1B UIIT_MSG_FORT_PORTALSTONE_FAIL_04: one side of the war has
	// brought its MaxEntrance into the fortress.
	portalFortressFull byte = 0x1b
	// 0x1C1C UIIT_MSG_FORT_ENTRANCE_FORTRESS_FAIL_02: during a war only the
	// occupying guild and the applicants enter.
	portalFortressNotInWar byte = 0x1c
	// 0x1C1E UIIT_MSG_FORT_ETC_ERR_CANT_ENTER_5MINBEFORE: attackers wait five
	// minutes after a temporary capture.
	portalFortressCaptureWait byte = 0x1e
	// 4F2B50 enters every world on layer 1.
	portalWorldLayer = 1
)

/*
================
loadFortressCatalog

siegefortress.txt's enabled rows: Service, FortressID, CodeName, the Korean
name, NameStrID, the town gate, then the u16 pair at +0x78/+0x7A whose
second member is MaxEntrance.
================
*/
func loadFortressCatalog(dir string) ([]fortress.Catalog, error) {
	rows := enterworld.ReadTextdataFile(filepath.Join(dir, "siegefortress.txt"))
	if len(rows) == 0 {
		return nil, fmt.Errorf("siegefortress is absent or empty")
	}
	var out []fortress.Catalog
	for i, r := range rows {
		if r[0] != "1" {
			continue
		}
		if len(r) < 8 {
			return nil, fmt.Errorf("siegefortress row %d is truncated", i+1)
		}
		id, err := strconv.ParseUint(r[1], 10, 32)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("siegefortress row %d identity", i+1)
		}
		maximum, err := strconv.ParseUint(r[7], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("siegefortress row %d entrance limit", i+1)
		}
		out = append(out, fortress.Catalog{ID: uint32(id), CodeName: r[2], MaxEntrance: uint32(maximum)})
	}
	return out, nil
}

/*
================
portalTransferError

CGameWorldMgr_TransferStatusToTeleportError (5F9950) for the statuses
CheckTransfer can answer.
================
*/
func portalTransferError(status instance.Status) byte {
	switch status {
	case instance.MissingLayer, instance.MissingWorld:
		return 3
	case instance.AllocationFull:
		return 0x29
	case instance.PlayerLimitReached:
		return 0x25
	}
	return 2
}

/*
================
portalWorld

The world and layer a destination lies in.
================
*/
func portalWorld(destination portalDestination) instance.ID {
	return instance.Pack(destination.world, portalWorldLayer)
}

/*
================
portalWorldAdmission

The world half of 4F2B50: the destination's world and layer, after the
fortress entry rules for a siege world and the destination world's
transfer check. Runs under the division lock, outside the character
update, because the fortress head count reads other characters.
================
*/
func (rt *Runtime) portalWorldAdmission(division string, c *enterworld.Character, arrival portalDestination) (instance.ID, byte) {
	world, known := instance.Lookup(arrival.world)
	if !known {
		return 0, 2
	}
	if world.Siege() {
		if refusal := rt.fortressEntry(division, c, arrival, world); refusal != 0 {
			return 0, refusal
		}
	}
	destination := portalWorld(arrival)
	if rt.Monsters == nil {
		if uint32(destination) != domain.DefaultWorldInstance {
			return 0, portalTransferError(instance.MissingWorld)
		}
		return destination, 0
	}
	if status := rt.Monsters.CheckPopulationTransfer(division, destination, c.GMPrivilege); status != instance.Success {
		return 0, portalTransferError(status)
	}
	return destination, 0
}

/*
================
fortressEntry

4F2B50's siege branch. The destination gate's own building decides the
rule: a fortress gate (TypeId_IsFortressGate 4F8820) takes anyone not in a
job suit while no war runs (CSiegeFortressMgr_CheckWarEntry 61D3D0 returns
before its war checks when MainProcess_IsMode42425 is clear); every other
portal of the fortress takes only the owning guild
(CSiegeFortressMgr_IsGuildOwner 6353F0) or an allied one (IsGuildAllied
635440). Guild unions are not part of this server, so no guild is allied.
================
*/
func (rt *Runtime) fortressEntry(division string, c *enterworld.Character, destination portalDestination, world instance.Definition) byte {
	fortressID, ok := rt.Fortresses.ForWorld(world)
	if !ok {
		return 2
	}
	if destination.fortressGate {
		// Job state +0xF is 4 (none) unless a job suit is worn: job mode.
		if rt.jobDressed(c) {
			return portalFortressJobSuit
		}
		if rt.Fortresses.WarActive(division) {
			return rt.fortressWarEntry(division, c, fortressID)
		}
		return 0
	}
	var guildID int64
	if c.GuildID != nil {
		guildID = *c.GuildID
	}
	if !rt.Fortresses.GuildOwns(division, fortressID, guildID) {
		return portalFortressOwnersOnly
	}
	return 0
}

/*
================
fortressWarEntry

CSiegeFortressMgr_CheckWarEntry (61D3D0) past its war-mode test. The
occupying guild needs no application; everyone else must have applied.
Defenders (the occupying guild) may bring half of MaxEntrance; attackers
the whole of it, or half once the fortress is occupied, and only after the
post-capture wait. Each side counts the PCs already in the world's layer
(siege slots 39 and 40).
================
*/
func (rt *Runtime) fortressWarEntry(division string, c *enterworld.Character, fortressID uint32) byte {
	if c.GuildID == nil || *c.GuildID == 0 {
		return portalFortressNotInWar
	}
	guild := *c.GuildID
	record, ok := rt.Fortresses.Get(division, fortressID)
	if !ok {
		return 2
	}
	occupied := record.GuildID != 0
	defender := occupied && record.GuildID == guild
	if !defender && !record.Applicants[guild] {
		return portalFortressNotInWar
	}
	defenders, attackers := rt.fortressSides(division, fortressID, record.GuildID)
	if defender {
		if record.MaxEntrance>>1 <= defenders {
			return portalFortressFull
		}
		return 0
	}
	if !record.EntryOpen {
		return portalFortressCaptureWait
	}
	limit := record.MaxEntrance
	if occupied {
		limit >>= 1
	}
	if limit <= attackers {
		return portalFortressFull
	}
	return 0
}

/*
================
fortressSides

The PCs admitted to the fortress's world, split into the occupying guild
and everyone else.
================
*/
func (rt *Runtime) fortressSides(division string, fortressID uint32, owner int64) (defenders, attackers uint32) {
	var world instance.ID
	for _, definition := range instance.Shipped() {
		if id, ok := rt.Fortresses.ForWorld(definition); ok && id == fortressID {
			world = instance.Pack(definition.ID, portalWorldLayer)
		}
	}
	rt.characterAdmissions.Range(func(key, value any) bool {
		admission := value.(populationAdmission)
		if admission.lease.ID != world || admission.division != division {
			return true
		}
		c := rt.findCharacter(admission.division, admission.name)
		if c == nil {
			return true
		}
		if owner != 0 && c.GuildID != nil && *c.GuildID == owner {
			defenders++
		} else {
			attackers++
		}
		return true
	})
	return defenders, attackers
}

/*
================
setCharacterWorld

Writes the semantic world a PC now resides in. The default world is the
absent value, so field characters keep their records unchanged.
================
*/
func setCharacterWorld(c *enterworld.Character, world instance.ID) {
	if c.World == nil {
		c.World = &domain.CharacterWorld{}
	}
	if uint32(world) == domain.DefaultWorldInstance {
		c.World.PackedInstance = nil
		return
	}
	packed := uint32(world)
	c.World.PackedInstance = &packed
}
