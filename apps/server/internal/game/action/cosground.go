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
)

// Caller holds the division action lock. A follower's live plane is mandatory:
// the player's pose is never substituted for a moving pet's position.
func (rt *Runtime) applyCosGround(division string, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	return rt.applyCosGroundAt(division, c, q, rt.Now(), true)
}

func (rt *Runtime) applyCosGroundAt(division string, c *enterworld.Character, q wire.ItemMoveRequest, now time.Time, allowApproach bool) OpResult {
	rt.petMu.Lock()
	session := rt.petSessions[petOwnerKey{division, strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if session == nil || session.character != c || session.follower == nil || session.follower.GID() != q.CosGID {
		return failureResult(wire.ErrCodeInvalidRequest)
	}

	if allowApproach {
		session.pickup = nil
	}
	at := session.follower.Position(now.UnixMilli())
	var sharedOwner uint32
	if item, ok := rt.characterGround(division, c, q.GroundGID); ok && item.OwnerJID != 0 && rt.CanPickupOwnedDrop != nil && rt.CanPickupOwnedDrop(division, c.Name, item.OwnerJID) {
		sharedOwner = item.OwnerJID
	}
	result := failureResult(wire.ErrCodeInvalidRequest)
	rt.deps.Update(c, "cos-ground-item", func() bool {
		bag, inv, valid := rt.ownedCOSContainer(c, q.CosGID)
		if !valid || c.ActiveCOS.Mounted || enterworld.CurrentHP(c) == 0 {
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
		from := grounditem.Point{RegionID: at.RegionID, X: float32(at.X), Z: float32(at.Z)}
		distance := grounditem.Distance2D(from, item.Position)
		if !grounditem.SameWorld(from, item.Position) || math.IsNaN(distance) {
			return false
		}
		if distance > grounditem.ExecuteRange {
			if allowApproach {
				copy := q
				session.pickup = &copy
				session.pickupDeadline = now.UnixMilli() + 9000
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
			grant, fault := inv.GrantStack(inventory.Item{RecordID: item.RecordID, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags, Quantity: count, Plus: item.Plus, VarianceBits: item.VarianceBits, Durability: item.Durability, MagicOptions: item.MagicOptions, TransformRefObjID: item.TransformRefObjID}, rt.maxStackFor(item.TypeFlags, item.Codename))
			if fault != nil {
				return false
			}
			candidate := *c.ActiveCOS
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
			body := wire.ItemBody{TypeFlags: granted.TypeFlags, RefObjID: granted.RefObjID, Quantity: granted.Quantity, Plus: granted.Plus, VarianceBits: granted.VarianceBits, Durability: granted.Durability, MagicOptions: granted.MagicOptions, TransformRefObjID: granted.TransformRefObjID}
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
