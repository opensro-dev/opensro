/*
===========================================================================

cosdeath.go - what a companion's death leaves behind, by kind

Every character death runs the reference server's kill credit, whose
vtable+0x638 drops a COS's transport cargo: a
transport's goods go offline and publish unowned, the way a monster's
drops do. The COS death then releases a vehicle
with kind 1, deleting its record, as the Clean command does; pets
keep theirs on the summoner item for revival (cosabnormal.go). A
captured quest monster is gone with its death, and
its quest hears it.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
settleCompanionDeath

Called inside the owner's character door when a companion first dies.
================
*/
func (rt *Runtime) settleCompanionDeath(o *cosAbnormalOwner) {
	// Every COS death tells its owner first.
	o.private = append(o.private, cosRemovalNotice(o.pet.GID))
	ref := o.ref
	if ref == nil {
		found := false
		if ref, found = rt.cosReference(o.pet); !found {
			return
		}
	}
	switch band := ref.TidWord >> 11; band {
	case cosBandRiding, cosBandTransport:
		if o.c.ActiveCOS != o.pet {
			return
		}
		if band == cosBandTransport && o.pet.Container != nil {
			rt.dropTransportCargo(o)
		}
		o.c.ActiveCOS = nil
	case domain.MercenaryBand:
		o.c.RemoveMercenary(o.pet.GID)
	case domain.CapturedCOSBand:
		if o.c.CapturedCOS != o.pet {
			return
		}
		o.c.CapturedCOS = nil
		if rt.CapturedFollowerDied != nil {
			o.private = append(o.private, rt.CapturedFollowerDied(o.c)...)
		}
	}
}

/*
================
dropTransportCargo

Natively the cargo scatters around the dead transport, owned by no one.
================
*/
func (rt *Runtime) dropTransportCargo(o *cosAbnormalOwner) {
	o.public = append(o.public, rt.scatterTransportCargo(o.division, o.c, o.pet, o.now)...)
}

/*
================
scatterTransportCargo

The native cargo drop, shared with the port-only trader rule (tradercargodrop.go):
every row of pet's container lands unowned around the transport, and the
container empties. A row the ground owner refuses stays lost with the
vehicle. The caller holds c's door; returns the drops' public frames.
================
*/
func (rt *Runtime) scatterTransportCargo(division string, c *enterworld.Character, pet *enterworld.CharacterCOS, nowMs int64) []wire.Frame {
	at := rt.companionLiveSpawn(division, c, pet, nowMs)
	now := time.UnixMilli(nowMs)
	var dropped []grounditem.Item
	for _, item := range invItemsFromRowsWithin(pet.Container.Rows, int64(pet.Container.Capacity)) {
		planned := rt.scatterMonsterDrop(PlanItemDrop(item, item.Quantity, at, "", now), at, cosCargoRarity)
		if added := rt.addCharacterGround(division, c, planned); added.Gid != 0 {
			dropped = append(dropped, added)
		}
	}
	pet.Container.Rows = nil
	if len(dropped) == 0 {
		return nil
	}
	cargo := rt.groundReferences(dropped)
	for _, item := range dropped {
		cargo = append(cargo, wire.DropBroadcastFrames(item.SpawnRow(true))...)
	}
	return cargo
}
