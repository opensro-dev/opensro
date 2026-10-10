/*
===========================================================================

cosground.go - pet inventory pickup and drop transactions

The pet's live position and persisted container own every grant the party
leaves with the owner; a pickup the party's item share hands to another
member, or gold it splits, is granted as a player pickup (sharePetPickup).
Both manual inventory operations and native automatic pickup commands
enter this owner.

===========================================================================
*/
package action

import (
	"math"
	"strings"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const cosPickupApproachTimeoutMs = 9000

/*
================
cosGroundAttempt
================
*/
type cosGroundAttempt struct {
	now           time.Time
	allowApproach bool
}

/*
================
applyCosGround

Caller holds the division action lock. Replacing an approach retires its
command acknowledgement before the next request acquires the pending slot.
================
*/
func (rt *Runtime) applyCosGround(division string, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	rt.petMu.Lock()
	session := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: q.CosGID}]
	rt.petMu.Unlock()
	var retired []wire.Frame
	if session != nil && session.character == c && session.pickup != nil {
		result := finishPendingCosPickup(session, failureResult(wire.ErrCodeInvalidRequest))
		retired = result.Frames
	}
	result := rt.applyCosGroundAt(division, c, q, cosGroundAttempt{now: rt.Now(), allowApproach: true})
	result.Frames = append(retired, result.Frames...)
	return result
}

/*
================
applyCosGroundAt

A follower's live plane is mandatory; the player's pose is never substituted
for a moving pet's position. Revalidate reservation and capacity at arrival.
================
*/
func (rt *Runtime) applyCosGroundAt(division string, c *enterworld.Character, q wire.ItemMoveRequest, attempt cosGroundAttempt) OpResult {
	now, allowApproach := attempt.now, attempt.allowApproach
	rt.petMu.Lock()
	session := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: q.CosGID}]
	rt.petMu.Unlock()
	if session == nil || session.character != c || session.follower == nil || session.follower.GID() != q.CosGID {
		return failureResult(wire.ErrCodeInvalidRequest)
	}

	if allowApproach {
		session.pickup = nil
		session.pickupCommand = false
	}
	at := session.follower.Position(now.UnixMilli())
	var sharedOwner uint32
	if item, ok := rt.characterGround(division, c, q.GroundGID); ok && item.OwnerJID != 0 && rt.CanPickupOwnedDrop != nil && rt.CanPickupOwnedDrop(division, c.Name, item.OwnerJID) {
		sharedOwner = item.OwnerJID
	}
	if shared, handled := rt.sharePetPickup(division, c, q, petPickupAt{at: at, sharedOwner: sharedOwner, now: now}); handled {
		return shared
	}
	result := failureResult(wire.ErrCodeInvalidRequest)
	rt.deps.Update(c, "cos-ground-item", func() bool {
		bag, inv, valid := rt.ownedCOSContainer(c, q.CosGID)
		if !valid || c.CompanionByGID(q.CosGID).Mounted || enterworld.CurrentHP(c) == 0 {
			return false
		}
		if q.MovementType == wire.MoveTypeCosDrop {
			dropped, fault := inv.Drop(q.SourceSlot)
			if fault != nil {
				return false
			}
			added := rt.addCharacterGround(division, c, PlanItemDrop(dropped, dropped.Quantity, at, c.Name, now))
			if added.Gid == 0 {
				return false
			}
			bag.Rows = rowsFromInvItems(inv.Items())
			public := append(rt.groundReferences([]grounditem.Item{added}), wire.DropBroadcastFrames(added.SpawnRow(true))...)
			result = OpResult{Frames: append([]wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.NewWriter(7).U8(1).U8(wire.MoveTypeCosDrop).U32(q.CosGID).U8(q.SourceSlot).Payload()}}, public...), Broadcast: public}
			return true
		}
		item, found := rt.characterGround(division, c, q.GroundGID)
		if !found || item.OwnerJID != 0 && item.OwnerJID != enterworld.ObjectIDForCharacter(c) && item.OwnerJID != sharedOwner {
			return false
		}
		item.Summon.RefreshRentalTimes(now.Unix())
		from := grounditem.Point{RegionID: at.RegionID, X: float32(at.X), Z: float32(at.Z)}
		distance := grounditem.Distance2D(from, item.Position)
		if !grounditem.SameWorld(from, item.Position) || math.IsNaN(distance) {
			return false
		}
		if distance > grounditem.ExecuteRange {
			if allowApproach {
				copy := q
				session.pickup = &copy
				session.pickupDeadline = now.UnixMilli() + cosPickupApproachTimeoutMs
				result = OpResult{}
			}
			return false
		}
		anim := wire.PickupAnim{Gid: q.CosGID, Heading: wire.HeadingByteFromAngle(at.Angle)}
		var receipt []byte
		var gold *wire.Frame
		var remainder uint16
		if item.IsGold() {
			if goldOf(c) > uint64(math.MaxInt64)-uint64(item.GoldAmount) {
				return false
			}
			if _, ok := rt.Ground.Remove(division, item.Gid); !ok {
				return false
			}
			setGold(c, goldOf(c)+uint64(item.GoldAmount))
			receipt = wire.NewWriter(11).U8(1).U8(wire.MoveTypeCosPickup).U32(q.CosGID).U8(wire.PickupGoldSlot).U32(item.GoldAmount).Payload()
			gold = &wire.Frame{Opcode: wire.OpPointsUpdate, Payload: (wire.GoldRefresh{Balance: goldOf(c), Notify: true}).Encode()}
		} else {
			count := item.StackCount
			if count == 0 {
				count = 1
			}
			grant, fault := inv.GrantStack(inventory.Item{TradeOwner: item.TradeOwner, RecordID: item.RecordID, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags, Quantity: count, Plus: item.Plus, VarianceBits: item.VarianceBits, Durability: item.Durability, MagicOptions: item.MagicOptions, TransformRefObjID: item.TransformRefObjID, Summon: domain.CloneCOS(item.Summon)}, rt.maxStackFor(item.TypeFlags, item.Codename))
			if fault != nil {
				result = failureResult(fault.Code)
				return false
			}
			candidate := *c.CompanionByGID(q.CosGID)
			candidate.Container = &domain.COSContainer{Capacity: bag.Capacity, Rows: rowsFromInvItems(inv.Items())}
			refs := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
			ref, ok := refs.CharacterRefByCodename(candidate.Codename)
			if !ok {
				return false
			}
			if _, err := enterworld.BuildCOSRecord(&candidate, ref, rt.deps.ItemReferences()); err != nil {
				return false
			}
			granted, ok := inv.At(grant.DestSlot)
			if !ok {
				return false
			}
			body := wire.ItemBody{TradeOwner: granted.TradeOwner, TypeFlags: granted.TypeFlags, RefObjID: granted.RefObjID, Quantity: granted.Quantity, Plus: granted.Plus, VarianceBits: granted.VarianceBits, Durability: granted.Durability, MagicOptions: granted.MagicOptions, TransformRefObjID: granted.TransformRefObjID, Summon: domain.CloneCOS(granted.Summon)}
			plain := wire.EncodePickupItemResult(grant.DestSlot, body)
			receipt = wire.NewWriter(len(plain) + 4).U8(1).U8(wire.MoveTypeCosPickup).U32(q.CosGID).Bytes(plain[2:]).Payload()
			remainder = grant.GroundRemainder
			if remainder > 0 {
				rt.Ground.SetStackCount(division, item.Gid, remainder)
			} else if _, ok := rt.Ground.Remove(division, item.Gid); !ok {
				return false
			}
			bag.Rows = candidate.Container.Rows
		}
		public := wire.PickupBroadcastFrames(anim, item.Gid, remainder)
		frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: receipt}}
		if gold != nil {
			frames = append(frames, *gold)
		}
		result = OpResult{Frames: append(frames, public...), Broadcast: public}
		return true
	})
	return result
}

/*
================
petPickupAt

Where and when the pet stands for its pickup, and the reserved owner the
party lets it take from (0 when none).
================
*/
type petPickupAt struct {
	at          simulation.Spawn
	sharedOwner uint32
	now         time.Time
}

/*
================
sharePetPickup

A pet's pickup runs the party's item share as the owner's own pickup does:
CPlayer_ExecuteGroundPickup (526090) serves move type 0x11 as it serves 6,
and CPlayer_SelectPartyLootRecipient (525DC0) picks the recipient by the
party's rotation. When the rotation names another member, or the heap is
gold the party splits, the grant is the player pickup's (the recipient's
inventory, the gold shares), played by the pet. Otherwise the pet keeps
its own pickup into its bag, which the v1.150 client defines: its pickup
result table (77DD10) reports a full pet bag (B4). Trade goods never
share (525DC0's first branch). The rotation is asked once, here, after the
pet path's own admissions: a live, unmounted pet with its container, a live
owner, and a drop the owner may take (its own, unreserved, or reserved to a
member the party shares with).
================
*/
func (rt *Runtime) sharePetPickup(division string, c *enterworld.Character, q wire.ItemMoveRequest, p petPickupAt) (OpResult, bool) {
	at, now := p.at, p.now
	if q.MovementType != wire.MoveTypeCosPickup {
		return OpResult{}, false
	}
	item, found := rt.characterGround(division, c, q.GroundGID)
	if !found || item.TradeOwner != "" {
		return OpResult{}, false
	}
	if item.OwnerJID != 0 && item.OwnerJID != enterworld.ObjectIDForCharacter(c) && item.OwnerJID != p.sharedOwner {
		return OpResult{}, false
	}
	admitted := false
	rt.deps.Read(division, func() {
		_, _, valid := rt.ownedCOSContainer(c, q.CosGID)
		admitted = valid && !c.CompanionByGID(q.CosGID).Mounted && enterworld.CurrentHP(c) != 0
	})
	if !admitted {
		return OpResult{}, false
	}
	from := grounditem.Point{RegionID: at.RegionID, X: float32(at.X), Z: float32(at.Z)}
	distance := grounditem.Distance2D(from, item.Position)
	if !grounditem.SameWorld(from, item.Position) || math.IsNaN(distance) || distance > grounditem.ExecuteRange {
		return OpResult{}, false
	}
	recipient := rt.partyPickupRecipient(division, c, item, now.UnixMilli())
	split := item.IsGold() && rt.partyGoldShares(division, recipient, item.GoldAmount, now.UnixMilli()) != nil
	if recipient == c && !split {
		return OpResult{}, false
	}
	actor := wire.PickupAnim{Gid: q.CosGID, Heading: wire.HeadingByteFromAngle(at.Angle)}
	return rt.grantPickupTo(pickupGrant{division: division, worldKey: simulation.WorldKey(division, c.Name), picker: c,
		recipient: recipient, snapshot: rt.characterSnapshot(division, c), item: item, actor: &actor}), true
}
