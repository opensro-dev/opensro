/*
===========================================================================

tradepickup.go - picking trade goods up off the ground

Trade goods never go to the bag. CPlayer_SelectPartyLootRecipient (525DC0)
takes them out of the party's item share and admits them by the picker's
job: a trader only their own goods, a thief anyone's but their own, and
nobody else. The picker needs a summoned trade transport close by, and the
goods go into it as a type 0x11 (COS pickup) move answered for the
transport (CPlayer_ExecuteGroundPickup 526090).

===========================================================================
*/
package action

import (
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// 525DC0's refusals; v1.150 shows the low byte as a category 1 notice.
const (
	// tradePickupErrNoCart is 0x1861, UIIT_MSG_STRGERR_DONT_HAVE_ANY_TRADECART_TO_PICK_SPECIALTY.
	tradePickupErrNoCart uint8 = 0x61
	// tradePickupErrNotBuyer is 0x1863, UIIT_MSG_STRGERR_YOU_ARENT_BUYER_OF_THIS_SPECIALTY:
	// a trader picking another's goods, or a hunter.
	tradePickupErrNotBuyer uint8 = 0x63
	// tradePickupErrNoJob is 0x1864, a player outside job mode (no v1.150 text).
	tradePickupErrNoJob uint8 = 0x64
	// tradePickupErrOwnGoods is 0x186F, UIIT_MSG_STRGERR_ROBBER_CANT_PICK_HIS_ITEM.
	tradePickupErrOwnGoods uint8 = 0x6F
	// tradePickupErrCartFar is 525DC0's 4: the transport stands 1000 or more away.
	tradePickupErrCartFar uint8 = 4
)

// tradePickupCartRange is 525DC0's 1000.0: the transport must stand closer.
const tradePickupCartRange = 1000.0

/*
================
isPickupTradeGoods

525DC0 asks CGObj_IsTradeGoods (482DE0), which in v1.188 is any 3.3.8
item. INFERENCE: v1.150 also files its event food (3.3.8.0) and fortress
manuals (3.3.8.8) under 3.3.8; only 3.3.8.1 and 3.3.8.2 are trade goods
(ITEM_ETC_TRADE_*), so only those take the trade branch.
================
*/
func isPickupTradeGoods(flags uint16) bool {
	tid4 := flags >> 11
	return inventory.IsTradeGoods(flags) && (tid4 == 1 || tid4 == 2)
}

/*
================
tradePickupRefusal

525DC0's job test, by the job state (CGObjPC_GetJobState): the goods'
owner is the buyer's job alias (CGItem_OwnerNameMatchesPlayer).
================
*/
func tradePickupRefusal(c *enterworld.Character, owner string) uint8 {
	own := owner != "" && owner == c.Job.Alias
	switch enterworld.DressedJob(c) {
	case domain.JobTrader:
		if !own {
			return tradePickupErrNotBuyer
		}
	case domain.JobThief:
		if own {
			return tradePickupErrOwnGoods
		}
	case domain.JobHunter:
		return tradePickupErrNotBuyer
	default:
		return tradePickupErrNoJob
	}
	return 0
}

/*
================
grantTradeGoodsPickup

The trade goods arm of the player's pickup: the job test, then the
transport, then the grant into its cargo with the 0x11 receipt the client
applies to the transport's bag. The picker's own animation plays it.
================
*/
func (rt *Runtime) grantTradeGoodsPickup(g pickupGrant) OpResult {
	c, item, division := g.picker, g.item, g.division
	if code := tradePickupRefusal(c, item.TradeOwner); code != 0 {
		return pickupRefusal(code)
	}
	cart := c.ActiveCOS
	ref, valid := rt.cosReference(cart)
	if cart == nil || !cart.Summoned || cart.CurrentHP == 0 || !valid || !isVehicleCOS(ref.TidWord) {
		return pickupRefusal(tradePickupErrNoCart)
	}
	now := rt.Now().UnixMilli()
	a := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	b := rt.cosLiveSpawn(division, c, now)
	if !(math.Hypot(simulation.WorldDistance2D(a, b), a.Y-b.Y) < tradePickupCartRange) {
		return pickupRefusal(tradePickupErrCartFar)
	}
	world := rt.Worlds.Snapshot(g.worldKey, func() simulation.WorldState { return simulation.SeedWorldState(g.snapshot) })
	anim := wire.PickupAnim{Gid: enterworld.ObjectIDForCharacter(c), Heading: wire.HeadingByteFromAngle(world.Spawn.Angle)}
	result := pickupRefusal(wire.ErrCodeInvalidRequest)
	rt.deps.Update(c, "trade-goods-pickup", func() bool {
		bag, inv, ok := rt.ownedCOSContainer(c, cart.GID)
		if !ok {
			result = pickupRefusal(tradePickupErrNoCart)
			return false
		}
		count := item.StackCount
		if count == 0 {
			count = 1
		}
		grant, fault := inv.GrantStack(inventory.Item{TradeOwner: item.TradeOwner, RecordID: item.RecordID,
			RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags, Quantity: count,
			Plus: item.Plus, VarianceBits: item.VarianceBits, Durability: item.Durability,
			MagicOptions: item.MagicOptions}, rt.maxStackFor(item.TypeFlags, item.Codename))
		if fault != nil {
			result = pickupRefusal(fault.Code)
			return false
		}
		granted, present := inv.At(grant.DestSlot)
		if !present {
			return false
		}
		if grant.GroundRemainder > 0 {
			rt.Ground.SetStackCount(division, item.Gid, grant.GroundRemainder)
		} else if _, removed := rt.Ground.Remove(division, item.Gid); !removed {
			result = pickupRefusal(wire.ErrCodeTargetGone)
			return false
		}
		bag.Rows = rowsFromInvItems(inv.Items())
		body := wire.ItemBody{TradeOwner: granted.TradeOwner, TypeFlags: granted.TypeFlags, RefObjID: granted.RefObjID,
			Quantity: granted.Quantity, Plus: granted.Plus, VarianceBits: granted.VarianceBits,
			Durability: granted.Durability, MagicOptions: granted.MagicOptions}
		plain := wire.EncodePickupItemResult(grant.DestSlot, body)
		receipt := wire.NewWriter(len(plain) + 4).U8(1).U8(wire.MoveTypeCosPickup).U32(cart.GID).Bytes(plain[2:]).Payload()
		// The picker's burst is the bag pickup's (PickupItemGrantFrames) with
		// the transport's receipt: scoop, receipt, despawn, action release.
		public := wire.PickupBroadcastFrames(anim, item.Gid, grant.GroundRemainder)
		frames := append([]wire.Frame{public[0], {Opcode: wire.OpItemMoveResponse, Payload: receipt}}, public[1:]...)
		frames = append(frames, wire.Frame{Opcode: wire.OpActionState, Payload: wire.ReleaseActionState().Encode()})
		result = OpResult{Frames: frames, Broadcast: public}
		return true
	})
	if len(result.Broadcast) > 0 {
		// 4E9676: goods loaded into a vehicle make it a caravan.
		rt.registerCaravan(division, c)
	}
	return result
}
