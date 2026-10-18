/*
===========================================================================

operator_grant.go - live operator item grants through the inventory authority

The private operator endpoint owns authentication and replay protection.
This owner resolves authored items, plans the whole batch, commits once, and
publishes ordinary inventory receipts without retiring the player's session.

A grant may carry an optional equipment socket. Such a grant is seated into
that socket, replacing the occupant, and additionally publishes the equip
visual to the actor and the division peers plus the base-stat block to the
actor - the same presentation a completed type-0x00 equip move emits.

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
)

const maxOperatorItemGrants = 32

/*
================
OperatorGrantItems

Use the reward planner so stack merging, capacity and reference publication
have one implementation. No partial grant survives a refused batch. Grants
with a Slot bypass the bag planner and replace the occupant of that equipment
socket instead. A replace DESTROYS the previous occupant: it is removed from
the authoritative inventory, with no bag copy and no ground drop.
================
*/
func (rt *Runtime) OperatorGrantItems(division, name string, grants []inventory.ItemAmount) error {
	if len(grants) == 0 || len(grants) > maxOperatorItemGrants {
		return fmt.Errorf("invalid item grant batch")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	if rt.Stalls.Keeping(division, c.Name) || rt.Exchanges.Trading(division, c.Name) {
		return fmt.Errorf("inventory is locked by a stall or exchange")
	}
	var actorFrames []wire.Frame
	var peerVisuals []wire.Frame
	var planErr error
	if !rt.deps.Update(c, "operator-grant-items", func() bool {
		// Bag grants keep the reward planner's stack, capacity and receipt
		// rules; only Slot == nil grants ride it.
		bagGrants := make([]inventory.ItemAmount, 0, len(grants))
		for _, grant := range grants {
			if grant.Slot == nil {
				bagGrants = append(bagGrants, grant)
			}
		}
		rows := c.MissionInventory
		var receipts []wire.Frame
		if len(bagGrants) != 0 {
			planned, plannedReceipts, err := rt.PlanQuestInventory(c, nil, bagGrants)
			if err != nil {
				planErr = err
				return false
			}
			rows = planned
			receipts = plannedReceipts
		}
		seated, visuals, seatFrames, err := rt.seatOperatorEquipment(c, division, rows, grants)
		if err != nil {
			planErr = err
			return false
		}
		c.MissionInventory = seated
		actorFrames = append(receipts, seatFrames...)
		peerVisuals = visuals
		return true
	}) {
		if planErr != nil {
			return planErr
		}
		return fmt.Errorf("item grant refused")
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, actorFrames)
	}
	if len(peerVisuals) != 0 && rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, peerVisuals)
	}
	return nil
}

/*
================
seatOperatorEquipment

Apply every slot-directed grant inside the caller's character transaction. It
resolves the codename exactly like the reward planner, builds a fresh
equipment row (Quantity 1, the grant's plus and perfect white stats, the
VarianceIntMin1c0 durability fallback) and seats it into the named socket,
replacing any occupant.

It returns:
  - the persisted rows after all placements,
  - the per-socket 0x3314 visuals for the actor AND the division peers,
  - the actor-only 0xB06D row frames plus the 0x343C base-stat frame.

An unresolved, non-equipment or out-of-band grant refuses the whole batch. The
caller has not mutated the character yet, so nothing is published.
================
*/
func (rt *Runtime) seatOperatorEquipment(
	c *enterworld.Character,
	divisionID string,
	rows []enterworld.InventoryRow,
	grants []inventory.ItemAmount,
) ([]enterworld.InventoryRow, []wire.Frame, []wire.Frame, error) {
	seatGrants := make([]inventory.ItemAmount, 0, len(grants))
	for _, grant := range grants {
		if grant.Slot != nil {
			seatGrants = append(seatGrants, grant)
		}
	}
	if len(seatGrants) == 0 {
		return rows, nil, nil, nil
	}
	if rt.deps.ItemReferences() == nil {
		return nil, nil, nil, fmt.Errorf("item references unavailable")
	}

	before := invItemsFromRowsWithin(rows, int64(inventory.BagEnd(c)))
	inv := inventory.New(before, inventory.BagEnd(c))
	// Run the same character-vs-itemdata equip gate as a real equip (level,
	// mastery, gender, country). Without it the operator grant could seat EU
	// armor whose mastery tier the character has not trained.
	inv.Requirements = equipRequirements(rt.deps.ItemReferences(), c, rt.FortressGuildRole)
	objectID := enterworld.ObjectIDForCharacter(c)

	// A socket-directed grant is delivered like a real equip: the item is
	// first granted into a free bag slot (a valid bag receipt the client
	// accepts), then moved into the socket with the type-0x00 move result the
	// client applies to BOTH containers. This mirrors itemmove.go exactly:
	// Transfer -> companion pair moves -> a type-0x00 0xB06D row -> the 0x3314
	// socket visuals. Emitting a quest 14/15 row for a socket < 13 instead
	// makes the client reject it ("Quest item references non-bag slot") and
	// drop the session.
	var actor []wire.Frame
	var peer []wire.Frame
	for _, grant := range seatGrants {
		ref, ok := rt.deps.ItemReferences().ItemRefByCodename(grant.Codename)
		if !ok || ref == nil {
			return nil, nil, nil, fmt.Errorf("unresolved item grant %s", grant.Codename)
		}
		// A socket-directed grant must be equipment; Plus and the white-stat
		// derivation only exist on the equipment family.
		if ref.TypeIDs[1] != 1 {
			return nil, nil, nil, fmt.Errorf("item grant %s is not equipment", grant.Codename)
		}
		item := inventory.Item{
			RefObjID:  ref.RefObjID,
			Codename:  ref.Codename,
			TypeFlags: ref.TypeFlags(),
			Quantity:  1,
		}
		item.Durability = 100
		if ref.VarianceIntMin1c0 != nil {
			item.Durability = uint32(clampInt64(*ref.VarianceIntMin1c0, 1, 0xffffffff))
		}
		item.Plus = min(grant.Plus, maxGrantPlus)
		if grant.PerfectStats {
			// Every white stat field of this item type at 31 (100%), with
			// the matching durability, as monster drops build them.
			item.VarianceBits = 0
			for field := 0; field < equipmentVarianceFieldCount(ref); field++ {
				item.VarianceBits |= uint64(31) << (field * 5)
			}
			item.Durability = combat.DurabilityFromVariance(ref, 31)
		}
		if grant.PerfectMagic {
			// The strongest catalog-valid blues for the item's type and
			// degree, as monster drops source them.
			item.MagicOptions = append([]uint64(nil), loot.BestEquipmentMagic(ref.Codename)...)
		}
		bagSlot, fault := inv.Grant(item)
		if fault != nil {
			return nil, nil, nil, fmt.Errorf("item grant %s: %w", grant.Codename, fault)
		}
		granted, _ := inv.At(bagSlot)
		actor = append(actor, wire.Frame{
			Opcode:  wire.OpItemMoveResponse,
			Payload: wire.NewWriter(4 + granted.Body().EncodedSize()).U8(1).U8(14).U8(bagSlot).U8(0).Bytes(granted.Body().Encode()).Payload(),
		})

		socket := *grant.Slot
		// A socket grant over an OCCUPIED socket is a swap: the previous
		// occupant lands in the staging bag slot. The operator path must not
		// leave it there (it clutters the inventory and fills it). Purge it
		// after the swap, below.
		_, occupied := inv.At(socket)
		if _, fault := inv.Transfer(bagSlot, socket, 0, 1); fault != nil {
			return nil, nil, nil, fmt.Errorf("equip %s: %w", grant.Codename, fault)
		}
		subMoves, fault := rt.completeEquipmentPair(inv, bagSlot, socket)
		if fault != nil {
			return nil, nil, nil, fmt.Errorf("equip %s: %w", grant.Codename, fault)
		}
		actor = append(actor, wire.Frame{
			Opcode:  wire.OpItemMoveResponse,
			Payload: wire.EncodeInventoryMoveResult(bagSlot, socket, 1, subMoves),
		})
		visuals := FramesFromSocketVisuals(objectID, inv.EquipVisualChanges(bagSlot, socket))
		for _, subMove := range subMoves {
			visuals = append(visuals, FramesFromSocketVisuals(objectID, inv.EquipVisualChanges(subMove.SourceSlot, subMove.DestSlot))...)
		}
		actor = append(actor, visuals...)
		peer = append(peer, visuals...)
		if occupied {
			// Discard the swapped-out piece: remove the row from the
			// authoritative inventory (no ground spawn) and tell the client
			// its staging bag slot is now empty (0xB06D op 15, the bag-slot
			// removal row). Without this the old gear stays in the bag.
			if _, fault := inv.Drop(bagSlot); fault != nil {
				return nil, nil, nil, fmt.Errorf("purge swapped %s: %w", grant.Codename, fault)
			}
			actor = append(actor, wire.Frame{
				Opcode:  wire.OpItemMoveResponse,
				Payload: []byte{1, 15, bagSlot, 0},
			})
		}
	}

	after := inv.Items()
	seated := alchemyRows(rows, before, after)

	// Publish the granted items' references BEFORE the 0xB06D move rows: the
	// client resolves each row's item body against this snapshot and rejects a
	// body whose reference it has never seen ("Unknown inventory reference").
	// PlanQuestInventory prepends the same commerce frame.
	reference := rt.commerceReferences(after, before)
	actor = append([]wire.Frame{reference}, actor...)
	peer = append([]wire.Frame{reference}, peer...)

	next := c.Snapshot()
	next.MissionInventory = seated
	display, err := rt.PlayerBaseStats(divisionID, next)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("operator grant base stats: %w", err)
	}
	actor = append(actor, wire.Frame{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(next, display)})
	return seated, peer, actor, nil
}

/*
================
SeatOperatorEquipment

Exposes the socket-grant path to other owners (the level-up gear rule). It
seats the grants into their sockets, purging the swapped-out gear, and
returns the new rows, the peer frames and the actor frames. The caller runs
inside a character transaction and pushes the peer frames.
================
*/
func (rt *Runtime) SeatOperatorEquipment(
	c *enterworld.Character,
	divisionID string,
	rows []enterworld.InventoryRow,
	grants []inventory.ItemAmount,
) ([]enterworld.InventoryRow, []wire.Frame, []wire.Frame, error) {
	return rt.seatOperatorEquipment(c, divisionID, rows, grants)
}
