/*
===========================================================================

cosdeath.go - what a companion's death leaves behind, by kind

Every character death runs CGObjMob_CreditKillerOnDeath (4C42F0), whose
vtable+0x638 is CGObjCOS_DropTransportCargo (4D1FD0) for a COS: a
transport's goods go offline and publish unowned, the way a monster's
drops do. CGObjCOS_ProcessNormalDeath (52A000) then releases a vehicle
with kind 1, deleting its record, as the Clean command does; pets
(529F70) keep theirs on the summoner item for revival (cosabnormal.go). A
captured quest monster (CGObjCOS_Captured) is gone with its death, and
its quest hears it.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/domain"
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

4D1FD0: the cargo scatters around the dead transport, owned by no one.
A row the ground owner refuses stays lost with the vehicle.
================
*/
func (rt *Runtime) dropTransportCargo(o *cosAbnormalOwner) {
	at := rt.companionLiveSpawn(o.division, o.c, o.pet, o.now)
	now := time.UnixMilli(o.now)
	var dropped []grounditem.Item
	for _, item := range invItemsFromRowsWithin(o.pet.Container.Rows, int64(o.pet.Container.Capacity)) {
		planned := rt.scatterMonsterDrop(PlanItemDrop(item, item.Quantity, at, "", now), at, cosCargoRarity)
		if added := rt.addCharacterGround(o.division, o.c, planned); added.Gid != 0 {
			dropped = append(dropped, added)
		}
	}
	o.pet.Container.Rows = nil
	if len(dropped) == 0 {
		return
	}
	cargo := rt.groundReferences(dropped)
	for _, item := range dropped {
		cargo = append(cargo, wire.DropBroadcastFrames(item.SpawnRow(true))...)
	}
	o.public = append(o.public, cargo...)
}
