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
	command       bool
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
	if shared, handled := rt.sharePetPickup(division, c, q, petPickupAt{at: at, sharedOwner: sharedOwner, now: now, command: attempt.command}); handled {
		return shared
	}
	result := failureResult(wire.ErrCodeInvalidRequest)
	var picked grounditem.Item
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
				session.pickupCommand = attempt.command
				// Entering PICKITEM is a state change.
				resetPetStuck(session)
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
			picked = item
		}
		public := wire.PickupBroadcastFrames(anim, item.Gid, remainder)
		frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: receipt}}
		if gold != nil {
			frames = append(frames, *gold)
		}
		result = OpResult{Frames: append(frames, public...), Broadcast: public}
		return true
	})
	if q.MovementType == wire.MoveTypeCosPickup && len(result.Broadcast) > 0 && picked.RefObjID != 0 && !picked.IsGold() {
		result.Recipients = append(result.Recipients, rt.partyLootNotice(division, c, c, picked, now.UnixMilli())...)
	}
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
	command     bool
}

/*
================
sharePetPickup

A pet's pickup runs the party's item share as the owner's own pickup does:
The reference server's ground pickup serves move type 0x11 as it serves 6,
and its party loot selection picks the recipient by the
party's rotation. Non-alchemy quest items (the reference server's pet pickup
recipient selection) also route to
the owner's player bag (0x1C -> the player's ground-pickup grant) and update
quest inventory rather than entering the pet container. When the rotation
names another member, the drop is a personal quest item, or the heap is
gold the party splits, the grant is the player pickup's (the recipient's
inventory, the gold shares), played by the pet. Otherwise the pet keeps
its own pickup into its bag, which the v1.150 client defines: its pickup
result table (client 77DD10) reports a full pet bag (B4). Trade goods never
share (the party loot selection's first branch). The rotation is asked once, here, after the
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
	recipient := c
	if !item.IsGold() {
		recipient = rt.partyPickupRecipient(division, c, item, now.UnixMilli())
	}
	split := item.IsGold() && rt.partyGoldShares(division, recipient, item.GoldAmount, now.UnixMilli()) != nil
	if recipient == c && !split && !isPersonalQuestPickupItem(item) {
		return OpResult{}, false
	}
	actor := wire.PickupAnim{Gid: q.CosGID, Heading: wire.HeadingByteFromAngle(at.Angle)}
	if p.command && recipient != c {
		hasFreeSlot := false
		rt.deps.Read(division, func() {
			_, hasFreeSlot = bagOf(recipient).FirstFreeBagSlot()
		})
		if !hasFreeSlot {
			// The reference server's pet pickup recipient selection probes
			// the first empty slot (0x0D) of the rotated party recipient's bag
			// and spills to that recipient's pickup pet
			// whenever no empty slot exists (al == 0xFF), without attempting a
			// partial-stack merge in the recipient's player bag.
			return rt.grantPeerCosPickup(division, c, recipient, item, actor, p), true
		}
	}
	result := rt.grantPickupTo(pickupGrant{division: division, worldKey: simulation.WorldKey(division, c.Name), picker: c,
		recipient: recipient, snapshot: rt.characterSnapshot(division, c), item: item, actor: &actor})
	result.Frames = withoutActionState(result.Frames)
	if isPickupFault(result, wire.ErrCodeStorageFull) {
		if !p.command {
			if recipient != c {
				// The reference server's ground pickup on 0x706D routes
				// 0xB06D [2, 0x07] to the full recipient and 0xB06D [2, 0xDA] to
				// the picker (without player OpActionState on move type 0x11).
				return OpResult{
					Frames:     []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, wire.ErrCodePartyInventoryFull}}},
					Recipients: []RecipientFrames{{CharacterID: recipient.ID, Frames: result.Frames}},
				}, true
			}
			return result, true
		}
		if recipient != c {
			return rt.grantPeerCosPickup(division, c, recipient, item, actor, p), true
		}
		// Natively, when the owner's player bag is full on a personal
		// quest drop, an item-share party emits 0x189C (3 s retry
		// backoff); solo or non-item-share emits 0x18B5 (owner bag full).
		if rt.inSharedLootParty(division, c, now.UnixMilli()) {
			return failureResult(cosPickupRetryBackoff), true
		}
		return failureResult(cosPickupOwnerBagFull), true
	}
	return result, true
}

/*
================
withoutActionState

A pet pickup executes on the companion actor, never the player's
auto-command actor, so it never releases the player's 0xB2CD latch.
================
*/
func withoutActionState(frames []wire.Frame) []wire.Frame {
	out := make([]wire.Frame, 0, len(frames))
	for _, frame := range frames {
		if frame.Opcode != wire.OpActionState {
			out = append(out, frame)
		}
	}
	return out
}

/*
================
findPickupPetGID

The reference server's pickup-pet lookup: returns the GID of the character's
live, unmounted summoned pickup pet (band 4, vtable +0x43C == 1), or 0.
Must run inside the character authority door.
================
*/
func (rt *Runtime) findPickupPetGID(c *enterworld.Character) uint32 {
	if c == nil || c.DeletePending || enterworld.CurrentHP(c) == 0 {
		return 0
	}
	for _, pet := range c.Companions() {
		if pet == nil || !pet.Summoned || pet.Mounted || pet.CurrentHP == 0 || pet.GID == 0 {
			continue
		}
		ref, valid := rt.cosReference(pet)
		if !valid || ref.TidWord>>11 != 4 {
			continue
		}
		if _, _, ok := rt.ownedCOSContainer(c, pet.GID); ok {
			return pet.GID
		}
	}
	return 0
}

/*
================
grantPeerCosPickup

The reference server's pet pickup recipient selection and pet ground pickup:
when the rotated party member's player bag is full,
redirect the pickup to that member's summoned pickup pet. If the
member has no pickup pet, answer the picker's owner with 0x9C; if the member's
pickup pet bag is also full (0x1807 -> 0x18B4), send 0xB4 for the member's pet
to the member and 0x9C for the picker pet to the picker's owner.
================
*/
func (rt *Runtime) grantPeerCosPickup(division string, picker, recipient *enterworld.Character, item grounditem.Item, anim wire.PickupAnim, p petPickupAt) OpResult {
	now := p.now
	result := failureResult(cosPickupRetryBackoff)
	var picked grounditem.Item
	var peerPetGID uint32
	var peerBagFull bool
	rt.deps.Update(recipient, "cos-ground-item", func() bool {
		peerPetGID = rt.findPickupPetGID(recipient)
		if peerPetGID == 0 {
			return false
		}
		bag, inv, valid := rt.ownedCOSContainer(recipient, peerPetGID)
		if !valid {
			return false
		}
		current, found := rt.characterGround(division, picker, item.Gid)
		if !found {
			result = failureResult(wire.ErrCodeTargetGone)
			return false
		}
		if current.TradeOwner != "" || current.OwnerJID != 0 && current.OwnerJID != enterworld.ObjectIDForCharacter(picker) && current.OwnerJID != p.sharedOwner {
			return false
		}
		current.Summon.RefreshRentalTimes(now.Unix())
		count := current.StackCount
		if count == 0 {
			count = 1
		}
		grant, fault := inv.GrantStack(inventory.Item{
			TradeOwner: current.TradeOwner, RecordID: current.RecordID, RefObjID: current.RefObjID,
			Codename: current.Codename, TypeFlags: current.TypeFlags, Quantity: count, Plus: current.Plus,
			VarianceBits: current.VarianceBits, Durability: current.Durability, MagicOptions: current.MagicOptions,
			TransformRefObjID: current.TransformRefObjID, Summon: domain.CloneCOS(current.Summon),
		}, rt.maxStackFor(current.TypeFlags, current.Codename))
		if fault != nil {
			peerBagFull = fault.Code == wire.ErrCodeStorageFull
			return false
		}
		candidate := *recipient.CompanionByGID(peerPetGID)
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
		body := wire.ItemBody{
			TradeOwner: granted.TradeOwner, TypeFlags: granted.TypeFlags, RefObjID: granted.RefObjID,
			Quantity: granted.Quantity, Plus: granted.Plus, VarianceBits: granted.VarianceBits,
			Durability: granted.Durability, MagicOptions: granted.MagicOptions,
			TransformRefObjID: granted.TransformRefObjID, Summon: domain.CloneCOS(granted.Summon),
		}
		plain := wire.EncodePickupItemResult(grant.DestSlot, body)
		receipt := wire.NewWriter(len(plain) + 4).U8(1).U8(wire.MoveTypeCosPickup).U32(peerPetGID).Bytes(plain[2:]).Payload()
		remainder := grant.GroundRemainder
		if remainder > 0 {
			rt.Ground.SetStackCount(division, current.Gid, remainder)
		} else if _, ok := rt.Ground.Remove(division, current.Gid); !ok {
			return false
		}
		bag.Rows = candidate.Container.Rows
		picked = current
		public := wire.PickupBroadcastFrames(anim, current.Gid, remainder)
		result = OpResult{
			Frames:    public,
			Broadcast: public,
			// The peer may never have seen the drop: its reference first (#1612).
			Recipients: []RecipientFrames{{
				CharacterID: recipient.ID,
				Frames: append(rt.groundReferences([]grounditem.Item{current}),
					wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: receipt}),
			}},
		}
		return true
	})
	if len(result.Broadcast) > 0 {
		result.Recipients = append(result.Recipients, rt.partyLootNotice(division, picker, recipient, picked, now.UnixMilli())...)
		return result
	}
	if peerBagFull && peerPetGID != 0 {
		result.Recipients = append(result.Recipients, RecipientFrames{
			CharacterID: recipient.ID,
			Frames:      []wire.Frame{cosPickupAcknowledgement(wire.CosCommand{CosGid: peerPetGID, TargetGid: item.Gid}, cosPickupBagFull)},
		})
	}
	return result
}
