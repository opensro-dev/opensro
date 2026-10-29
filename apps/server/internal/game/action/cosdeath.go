/*
===========================================================================

cosdeath.go - what a companion's death leaves behind, by kind

Every character death runs the reference server's kill credit, whose
vtable+0x638 drops a COS's transport cargo: a
transport's goods go offline and publish unowned, the way a monster's
drops do. PublishDropsWithGoldBonus would reset a drop's ground owner
to the winner, but it skips any item CGObj_IsTradeGoods names - which
is every trade good there is (0x482de0; the reset's jne at 0x4c41de) -
so cargo stays unowned whoever killed the transport. The fatal dealer
is still recorded on the settlement, for the scatter's log line only.
The COS death then releases a vehicle
with kind 1, deleting its record, as the Clean command does; pets
keep theirs on the summoner item for revival (cosabnormal.go). A
captured quest monster is gone with its death, and
its quest hears it.

===========================================================================
*/
package action

import (
	"time"

	log "github.com/sirupsen/logrus"

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

Natively the cargo scatters around the dead transport, owned by no
one: the winner reset in PublishDropsWithGoldBonus never reaches a
trade good (see the file banner).
================
*/
func (rt *Runtime) dropTransportCargo(o *cosAbnormalOwner) {
	o.public = append(o.public, rt.scatterTransportCargo(o.division, o.c, o.pet, o.now, "death", o.winnerName)...)
}

/*
================
scatterTransportCargo

The native cargo drop, shared with the port-only trader rule (tradercargodrop.go):
every row of pet's container lands unowned around the transport, and
the container empties. winnerName names the fatal dealer for the log
line only ("" when no hit dealt the death); a bag is never reserved to
it, as native never reserves trade goods. A row the ground owner
refuses stays lost with the vehicle - the scatter log line below is
the only trace it leaves. The caller holds c's door; returns the
drops' public frames.
================
*/
func (rt *Runtime) scatterTransportCargo(division string, c *enterworld.Character, pet *enterworld.CharacterCOS, nowMs int64, cause, winnerName string) []wire.Frame {
	at := rt.companionLiveSpawn(division, c, pet, nowMs)
	now := time.UnixMilli(nowMs)
	var dropped []grounditem.Item
	for _, item := range invItemsFromRowsWithin(pet.Container.Rows, int64(pet.Container.Capacity)) {
		planned := rt.scatterMonsterDrop(PlanItemDrop(item, item.Quantity, at, "", now), at, cosCargoRarity)
		if added := rt.addCharacterGround(division, c, planned); added.Gid != 0 {
			dropped = append(dropped, added)
		}
	}
	killerKind := "none"
	if winnerName != "" {
		killerKind = "player"
	}
	log.WithFields(log.Fields{
		"cause": cause, "killer": killerKind, "winner": winnerName,
		"owner": c.Name, "bags": len(dropped),
	}).Info("trade: transport cargo scattered")
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
