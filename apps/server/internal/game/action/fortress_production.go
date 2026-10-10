/*
===========================================================================

fortress_production.go - the fortress smith's and trainer's orders

0x71E1 actions 0x0D..0x10 (smith, NPC service 0x1A) and 0x11..0x14
(trainer, 0x1B): query, start, cancel and collect one order per staff
member. The handlers are the CSiegeFortressMgr_HandleSmith and
HandleTrainer families (6324A0..6333C0); the v1.150 replies are what
754A40 reads, which match the v1.188 writers byte for byte after the
[action][result] header:

	query   [fortress u32][present u8]{ref u32, count u16, done u8, remaining i64}
	start   [fortress u32][ref u32][count u16][remaining i64]
	cancel  [fortress u32][ref u32]
	collect [fortress u32][ref u32][collected u16]

Remaining time is in seconds (CIFFortressMakeItemWnd 65A280 counts down a
count * minutes * 60 total with a one-second timer). The authority keeps
the orders (world/fortress/forge.go); the store commits payment and
collection with them (data/store/fortress_item_forge.go).

===========================================================================
*/
package action

import (
	"fmt"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/fortress"
)

const (
	// 632767 / 632B5C test the actor's fortress role against these bits;
	// a member holding exactly the bit is the staff's own role for the
	// discount (CGuild_HasFortressRole8Member 5D13C0, ...16Member 5D1350).
	fortressRoleSmith   uint8 = 0x08
	fortressRoleTrainer uint8 = 0x10
	// 632660 refuses more than twenty items in one order (0x2816).
	fortressForgeMaxCount = 20
	// The x87 float 0.85 the price and the time take when the staff role
	// is held (632660: fconvert of 0.850000024f).
	fortressForgeDiscount = float32(0.85)
)

/*
================
fortressForgeRow

One siegefortressitemforge.txt row: gold and guild points per item and
the minutes one item takes (CRefData_FindSiegeFortressItemForge 63BCA0:
+0xC, +0x10, +0x14).
================
*/
type fortressForgeRow struct {
	gold, gp, minutes uint32
}

/*
================
loadFortressForges

Enabled rows keyed by item: Service, Group, ItemRefID, Gold, GP, Minutes.
================
*/
func loadFortressForges(dir string) (map[uint32]fortressForgeRow, error) {
	rows := enterworld.ReadTextdataFile(filepath.Join(dir, "siegefortressitemforge.txt"))
	if len(rows) == 0 {
		return nil, fmt.Errorf("siegefortressitemforge is absent or empty")
	}
	out := make(map[uint32]fortressForgeRow, len(rows))
	for i, r := range rows {
		if r[0] != "1" {
			continue
		}
		if len(r) < 6 {
			return nil, fmt.Errorf("siegefortressitemforge row %d is truncated", i+1)
		}
		var values [4]uint32
		for j := range values {
			v, err := strconv.ParseUint(r[2+j], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("siegefortressitemforge row %d column %d", i+1, 3+j)
			}
			values[j] = uint32(v)
		}
		if values[0] == 0 {
			return nil, fmt.Errorf("siegefortressitemforge row %d names no item", i+1)
		}
		out[values[0]] = fortressForgeRow{gold: values[1], gp: values[2], minutes: values[3]}
	}
	return out, nil
}

/*
================
fortressProduction

Which staff member an action addresses: the trainer's actions are the
smith's plus four, with their own role bit.
================
*/
type fortressProduction struct {
	trainer bool
	role    uint8
	action  uint8 // the smith's action number (0x0D..0x10)
}

/*
================
productionFor
================
*/
func productionFor(action uint8) fortressProduction {
	if action >= siege.ActionTrainerQuery {
		return fortressProduction{trainer: true, role: fortressRoleTrainer, action: action - 4}
	}
	return fortressProduction{role: fortressRoleSmith, action: action}
}

/*
================
trainerItem

61DAC0's class test: the trainer's items are TypeID 3/3/3/2; every other
item, or an unknown one, is the smith's.
================
*/
func (rt *Runtime) trainerItem(ref uint32) bool {
	items := rt.deps.ItemReferences()
	if items == nil {
		return false
	}
	row, ok := items.ItemRefByID(ref)
	return ok && row.TypeIDs == [4]int64{3, 3, 3, 2}
}

/*
================
forgeKind

The authority's classifier for this staff member's orders.
================
*/
func (rt *Runtime) forgeKind(p fortressProduction) fortress.ForgeKind {
	return func(ref uint32) bool { return rt.trainerItem(ref) == p.trainer }
}

/*
================
fortressProducer

The holder and authority checks every action but the query repeats, in
632660's order: the actor's guild holds the fortress (0x2806), then the
actor is its master or holds the staff's role bit (0x2817). The commits
recheck both under the store lock.
================
*/
func (rt *Runtime) fortressProducer(division string, c *enterworld.Character, holder int64, p fortressProduction) uint8 {
	if rt.Guilds == nil || holder == 0 || c.GuildID == nil || *c.GuildID != holder {
		return domain.FortressForgeErrOwner
	}
	_, members, ok := rt.Guilds.Guild(division, holder)
	if !ok {
		return domain.FortressForgeErrOwner
	}
	for _, member := range members {
		if member.CharID == c.ID && (member.Grade == 0 || member.FortressRole&p.role != 0) {
			return 0
		}
	}
	return domain.FortressForgeErrRole
}

/*
================
fortressForgeFactor

0.85 when a member of the holder holds exactly the staff's role, else 1.
================
*/
func (rt *Runtime) fortressForgeFactor(division string, holder int64, p fortressProduction) float64 {
	if _, members, ok := rt.Guilds.Guild(division, holder); ok {
		for _, member := range members {
			if member.FortressRole == p.role {
				return float64(fortressForgeDiscount)
			}
		}
	}
	return 1
}

/*
================
fortressForgeRemaining

The countdown in whole seconds, never below zero: an order past its end
is marked done by the next siege tick.
================
*/
func fortressForgeRemaining(forge domain.FortressItemForgeRecord, nowMs int64) int64 {
	if forge.Done || forge.EndsAtMs <= nowMs {
		return 0
	}
	return (forge.EndsAtMs - nowMs) / 1000
}

/*
================
fortressProductionReply
================
*/
func fortressProductionReply(action uint8, body func(w *wire.Writer)) OpResult {
	w := wire.NewWriter(24).U8(action).U8(1)
	body(w)
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: w.Payload()}}}
}

/*
================
fortressProductionService

NPC admission has already passed (fortressServiceQuery: services 0x1A and
0x1B are the hired smith's and trainer's).
================
*/
func (rt *Runtime) fortressProductionService(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	if rt.Fortresses == nil {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	p := productionFor(request.Action)
	switch p.action {
	case siege.ActionSmithQuery:
		return rt.fortressProductionQuery(division, request, p)
	case siege.ActionSmithProduce:
		return rt.fortressProductionStart(division, c, request, p)
	case siege.ActionSmithCancel:
		return rt.fortressProductionCancel(division, c, request, p)
	default:
		return rt.fortressProductionCollect(division, c, request, p)
	}
}

/*
================
fortressProductionQuery

6324A0: only an unknown fortress refuses (3).
================
*/
func (rt *Runtime) fortressProductionQuery(division string, request siege.Interaction, p fortressProduction) OpResult {
	_, forge, present, ok := rt.Fortresses.ItemForge(division, request.Fortress, rt.forgeKind(p))
	if !ok {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	now := rt.Now().UnixMilli()
	return fortressProductionReply(request.Action, func(w *wire.Writer) {
		w.U32(request.Fortress)
		if !present {
			w.U8(0)
			return
		}
		done := uint8(0)
		if forge.Done {
			done = 1
		}
		w.U8(1).U32(forge.ItemRefID).U16(forge.Count).U8(done).U64(uint64(fortressForgeRemaining(forge, now)))
	})
}

/*
================
fortressProductionStart

632660's order: war period, fortress, running order, holder, role, the
forge row, the staff's item class, the count, then gold and guild points
in the commit. A zero count is refused with the count's code: INFERENCE,
the native accepts it and writes an empty order nothing can collect.
================
*/
func (rt *Runtime) fortressProductionStart(division string, c *enterworld.Character, request siege.Interaction, p fortressProduction) OpResult {
	if rt.Fortresses.WarActive(division) {
		return fortressRefusal(request.Action, domain.FortressForgeErrWar)
	}
	kind := rt.forgeKind(p)
	holder, _, present, ok := rt.Fortresses.ItemForge(division, request.Fortress, kind)
	if !ok {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	if present {
		return fortressRefusal(request.Action, domain.FortressForgeErrBusy)
	}
	if code := rt.fortressProducer(division, c, holder, p); code != 0 {
		return fortressRefusal(request.Action, code)
	}
	row, ok := rt.fortressForges[request.Reference]
	if !ok || rt.trainerItem(request.Reference) != p.trainer {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	count := request.Value16
	if count == 0 || count > fortressForgeMaxCount {
		return fortressRefusal(request.Action, domain.FortressForgeErrCount)
	}
	// 632660 multiplies as unsigned 32-bit, then truncates the x87
	// product with the float factor; the products here stay exact.
	factor := rt.fortressForgeFactor(division, holder, p)
	gold := int64(float64(row.gold*uint32(count)) * factor)
	gp := uint32(float64(row.gp*uint32(count)) * factor)
	seconds := int64(factor * float64(row.minutes*uint32(count)*60))
	now := rt.Now().UnixMilli()
	forge := domain.FortressItemForgeRecord{FortressID: request.Fortress, ItemRefID: request.Reference, Count: count,
		StartedAtMs: now, EndsAtMs: now + seconds*1000}
	code, err := rt.Fortresses.StartItemForge(division, kind, domain.FortressItemForgeStart{Forge: forge, ActorID: c.ID,
		Role: p.role, Gold: gold, GP: gp})
	if err != nil {
		code = domain.FortressForgeErrFailure
	}
	if code != 0 {
		return fortressRefusal(request.Action, code)
	}
	out := fortressProductionReply(request.Action, func(w *wire.Writer) {
		w.U32(request.Fortress).U32(request.Reference).U16(count).U64(uint64(seconds))
	})
	return rt.withHolderGP(division, c, holder, out)
}

/*
================
withHolderGP

The start charges the holder's guild points: every member sees the new
total, as the staff hire's commit publishes it; the actor also sees gold.
================
*/
func (rt *Runtime) withHolderGP(division string, c *enterworld.Character, holder int64, out OpResult) OpResult {
	row, members, ok := rt.Guilds.Guild(division, holder)
	if !ok {
		return out
	}
	update := wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeGuildGp3B29(row.GP)}
	for _, member := range members {
		if member.CharID != c.ID && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, member.Name, []wire.Frame{update})
		}
	}
	out.Frames = append([]wire.Frame{update, goldFrame(c)}, out.Frames...)
	return out
}

/*
================
fortressProductionCancel

632E50: war period, fortress, holder, role, then the order (0x2819) and
its item (3). Nothing is refunded.
================
*/
func (rt *Runtime) fortressProductionCancel(division string, c *enterworld.Character, request siege.Interaction, p fortressProduction) OpResult {
	if rt.Fortresses.WarActive(division) {
		return fortressRefusal(request.Action, domain.FortressForgeErrWar)
	}
	holder, _, _, ok := rt.Fortresses.ItemForge(division, request.Fortress, rt.forgeKind(p))
	if !ok {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	if code := rt.fortressProducer(division, c, holder, p); code != 0 {
		return fortressRefusal(request.Action, code)
	}
	code, err := rt.Fortresses.CancelItemForge(division, request.Fortress, rt.forgeKind(p), request.Reference)
	if err != nil {
		code = domain.FortressForgeErrFailure
	}
	if code != 0 {
		return fortressRefusal(request.Action, code)
	}
	return fortressProductionReply(request.Action, func(w *wire.Writer) {
		w.U32(request.Fortress).U32(request.Reference)
	})
}

/*
================
fortressProductionCollect

633170: war period, fortress, holder, role, the order (0x2819), its item
(3), done (0x281A), the count against the order and the item's stack
(0x281B), a free bag slot (0x281C). The collected items land as one new
stack in that slot (InsertItemWithCreatedItemInDataBase), never merged
into an existing one. A zero count is refused with the quantity code
(INFERENCE, as the start's zero count).
================
*/
func (rt *Runtime) fortressProductionCollect(division string, c *enterworld.Character, request siege.Interaction, p fortressProduction) OpResult {
	if rt.Fortresses.WarActive(division) {
		return fortressRefusal(request.Action, domain.FortressForgeErrWar)
	}
	kind := rt.forgeKind(p)
	holder, forge, present, ok := rt.Fortresses.ItemForge(division, request.Fortress, kind)
	if !ok {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	if code := rt.fortressProducer(division, c, holder, p); code != 0 {
		return fortressRefusal(request.Action, code)
	}
	if !present {
		return fortressRefusal(request.Action, domain.FortressForgeErrNone)
	}
	if forge.ItemRefID != request.Reference {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	if !forge.Done {
		return fortressRefusal(request.Action, domain.FortressForgeErrNotDone)
	}
	items := rt.deps.ItemReferences()
	var ref *enterworld.ItemRef
	if items != nil {
		ref, _ = items.ItemRefByID(forge.ItemRefID)
	}
	if ref == nil {
		return fortressRefusal(request.Action, domain.FortressForgeErrUnknown)
	}
	count := request.Value16
	if count == 0 || count > forge.Count || count > rt.maxStackFor(ref.TypeFlags(), ref.Codename) {
		return fortressRefusal(request.Action, domain.FortressForgeErrQuantity)
	}
	slot, free := inventory.New(invItemsFromBag(c), inventory.BagEnd(c)).FirstFreeBagSlot()
	if !free {
		return fortressRefusal(request.Action, domain.FortressForgeErrBag)
	}
	item := inventory.Item{Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: count}
	code, err := rt.Fortresses.CollectItemForge(division, kind, domain.FortressItemForgeCollect{Forge: forge, ActorID: c.ID,
		Role: p.role, Item: rowsFromInvItems([]inventory.Item{item})[0]}, count)
	if err != nil {
		code = domain.FortressForgeErrFailure
	}
	if code != 0 {
		return fortressRefusal(request.Action, code)
	}
	frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodePickupItemResult(item.Slot, item.Body())}}
	if rt.UpdateQuestInventory != nil {
		quest, _ := rt.UpdateQuestInventory(c)
		frames = append(frames, quest...)
	}
	out := fortressProductionReply(request.Action, func(w *wire.Writer) {
		w.U32(request.Fortress).U32(request.Reference).U16(count)
	})
	out.Frames = append(frames, out.Frames...)
	return out
}
