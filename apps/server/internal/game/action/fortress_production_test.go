/*
===========================================================================

fortress_production_test.go - smith and trainer orders through the wire
dispatcher, the fortress authority and the real authority store

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	forgeTestSmithItem   = uint32(19227)
	forgeTestTrainerItem = uint32(19569)
	// A fortress the catalog serves but nobody holds.
	forgeTestEmptyFortress = uint32(2)
)

/*
================
fortressForgeFixture

The tax fixture's master, holding fortress 1, beside a staff NPC that
offers both the smith's and the trainer's services, with the v1.150 rows
for one item of each.
================
*/
func fortressForgeFixture(t *testing.T) (*doorRuntime, int64) {
	t.Helper()
	d := fortressTaxFixture(t)
	rt := d.rt
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressSmith).With(simulation.NpcServiceFortressTrainer)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: fortressTaxTestID}, {ID: forgeTestEmptyFortress}})
	if err := rt.Fortresses.Restore(testDivision, d.authority.Fortresses()); err != nil {
		t.Fatal(err)
	}
	items := rt.deps.ItemReferences().(staticItemSource)
	for _, row := range []struct {
		ref      uint32
		codename string
		typeIDs  [4]int64
	}{{forgeTestSmithItem, "ITEM_ETC_SIEGE_SMITH_TEST", [4]int64{3, 3, 3, 1}}, {forgeTestTrainerItem, "ITEM_ETC_SIEGE_TRAINER_TEST", [4]int64{3, 3, 3, 2}}} {
		items[row.codename] = &enterworld.ItemRef{RefObjID: row.ref, Codename: row.codename, TypeIDs: row.typeIDs,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50})}
	}
	rt.fortressForges = map[uint32]fortressForgeRow{
		forgeTestSmithItem:   {gold: 32417, gp: 590, minutes: 10},
		forgeTestTrainerItem: {gold: 5400, gp: 36, minutes: 130},
	}
	holder, _ := rt.Fortresses.Get(testDivision, fortressTaxTestID)
	return d, holder.GuildID
}

/*
================
forgeRequest
================
*/
func forgeRequest(action uint8, fortressID, ref uint32, count uint16) []byte {
	w := wire.NewWriter(15).U32(fortressTaxTestNPC).U8(action).U32(fortressID)
	switch action {
	case siege.ActionSmithQuery, siege.ActionTrainerQuery:
	case siege.ActionSmithCancel, siege.ActionTrainerCancel:
		w.U32(ref)
	default:
		w.U32(ref).U16(count)
	}
	return w.Payload()
}

/*
================
forgeReply

The 0xB1E1 frame of a result, which follows any guild-point, gold or item
frames.
================
*/
func forgeReply(t *testing.T, out OpResult) []byte {
	t.Helper()
	for _, frame := range out.Frames {
		if frame.Opcode == opFortressInteractionResult {
			return frame.Payload
		}
	}
	t.Fatalf("no fortress reply in %+v", out.Frames)
	return nil
}

/*
================
setForgeGuild

Writes the holder's guild points and the master's own fortress role.
================
*/
func setForgeGuild(t *testing.T, d *doorRuntime, gp uint32, role uint8) {
	t.Helper()
	_, refused := d.authority.Guilds().UpdateGuildAs(testDivision, d.character.ID, "forge-fixture", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.GP = gp
			for i := range m {
				m[i].FortressRole = role
			}
			return g, m, true
		})
	if refused.Refused() {
		t.Fatal(refused)
	}
}

/*
================
TestFortressSmithOrderLifecycle

Query, the start's refusals and its price, the countdown, collection into
a fresh stack and a full bag, the war period, and an unrefunded cancel.
================
*/
func TestFortressSmithOrderLifecycle(t *testing.T) {
	d, guildID := fortressForgeFixture(t)
	rt, c := d.rt, d.character
	send := func(action uint8, fortressID, ref uint32, count uint16) []byte {
		t.Helper()
		return forgeReply(t, rt.HandleFortressInteraction(testDivision, c, forgeRequest(action, fortressID, ref, count)))
	}
	expect := func(got []byte, want ...byte) {
		t.Helper()
		if !bytes.Equal(got, want) {
			t.Fatalf("reply %x, want %x", got, want)
		}
	}
	fid := byte(fortressTaxTestID)
	expect(send(siege.ActionSmithQuery, fortressTaxTestID, 0, 0), 0x0d, 1, fid, 0, 0, 0, 0)
	expect(send(siege.ActionSmithQuery, 9, 0, 0), 0x0d, 2, domain.FortressForgeErrUnknown)
	expect(send(siege.ActionSmithProduce, 9, forgeTestSmithItem, 1), 0x0e, 2, domain.FortressForgeErrUnknown)
	expect(send(siege.ActionSmithProduce, forgeTestEmptyFortress, forgeTestSmithItem, 1), 0x0e, 2, domain.FortressForgeErrOwner)
	expect(send(siege.ActionSmithProduce, fortressTaxTestID, 4242, 1), 0x0e, 2, domain.FortressForgeErrUnknown)
	expect(send(siege.ActionSmithProduce, fortressTaxTestID, forgeTestTrainerItem, 1), 0x0e, 2, domain.FortressForgeErrUnknown)
	expect(send(siege.ActionSmithProduce, fortressTaxTestID, forgeTestSmithItem, 21), 0x0e, 2, domain.FortressForgeErrCount)
	expect(send(siege.ActionSmithProduce, fortressTaxTestID, forgeTestSmithItem, 10), 0x0e, 2, domain.FortressForgeErrGold)
	gold := int64(400000)
	d.authority.MutateCharacter(c, "forge-fixture", func() { c.Gold = &gold })
	setForgeGuild(t, d, 5899, 0)
	expect(send(siege.ActionSmithProduce, fortressTaxTestID, forgeTestSmithItem, 10), 0x0e, 2, domain.FortressForgeErrGP)
	setForgeGuild(t, d, 6000, 0)
	// 10 x 32417 gold, 10 x 590 GP and 10 x 10 minutes: no smith-role member.
	got := send(siege.ActionSmithProduce, fortressTaxTestID, forgeTestSmithItem, 10)
	want := wire.NewWriter(20).U8(0x0e).U8(1).U32(fortressTaxTestID).U32(forgeTestSmithItem).U16(10).U64(6000).Payload()
	expect(got, want...)
	g, _, _ := rt.Guilds.Guild(testDivision, guildID)
	if *c.Gold != 400000-324170 || g.GP != 100 {
		t.Fatalf("payment gold=%d GP=%d", *c.Gold, g.GP)
	}
	expect(send(siege.ActionSmithProduce, fortressTaxTestID, forgeTestSmithItem, 1), 0x0e, 2, domain.FortressForgeErrBusy)
	expect(send(siege.ActionSmithQuery, fortressTaxTestID, 0, 0),
		wire.NewWriter(24).U8(0x0d).U8(1).U32(fortressTaxTestID).U8(1).U32(forgeTestSmithItem).U16(10).U8(0).U64(6000).Payload()...)
	expect(send(siege.ActionSmithCollect, fortressTaxTestID, forgeTestSmithItem, 4), 0x10, 2, domain.FortressForgeErrNotDone)
	rt.Fortresses.AdvanceItemForges(rt.Now().UnixMilli() + 6000*1000)
	expect(send(siege.ActionSmithCollect, fortressTaxTestID, forgeTestTrainerItem, 4), 0x10, 2, domain.FortressForgeErrUnknown)
	expect(send(siege.ActionSmithCollect, fortressTaxTestID, forgeTestSmithItem, 11), 0x10, 2, domain.FortressForgeErrQuantity)
	out := rt.HandleFortressInteraction(testDivision, c, forgeRequest(siege.ActionSmithCollect, fortressTaxTestID, forgeTestSmithItem, 4))
	expect(forgeReply(t, out), wire.NewWriter(12).U8(0x10).U8(1).U32(fortressTaxTestID).U32(forgeTestSmithItem).U16(4).Payload()...)
	if out.Frames[0].Opcode != wire.OpItemMoveResponse {
		t.Fatalf("collected stack not granted first: %+v", out.Frames)
	}
	stacks := 0
	for _, row := range c.MissionInventory {
		if row.RefObjID == forgeTestSmithItem && row.StackCount == 4 {
			stacks++
		}
	}
	if stacks != 1 {
		t.Fatalf("collected stack rows %d in %+v", stacks, c.MissionInventory)
	}
	expect(send(siege.ActionSmithQuery, fortressTaxTestID, 0, 0),
		wire.NewWriter(24).U8(0x0d).U8(1).U32(fortressTaxTestID).U8(1).U32(forgeTestSmithItem).U16(6).U8(1).U64(0).Payload()...)
	d.authority.MutateCharacter(c, "forge-fixture", func() {
		for slot := int64(0); slot < int64(inventory.BagEnd(c)); slot++ {
			taken := false
			for _, row := range c.MissionInventory {
				taken = taken || row.Slot == slot
			}
			if !taken {
				c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: forgeTestSmithItem,
					Codename: "ITEM_ETC_SIEGE_SMITH_TEST", VarianceBits: "0", StackCount: 1})
			}
		}
	})
	expect(send(siege.ActionSmithCollect, fortressTaxTestID, forgeTestSmithItem, 1), 0x10, 2, domain.FortressForgeErrBag)
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	expect(send(siege.ActionSmithCancel, fortressTaxTestID, forgeTestSmithItem, 0), 0x0f, 2, domain.FortressForgeErrWar)
	if reply := send(siege.ActionSmithQuery, fortressTaxTestID, 0, 0); reply[1] != 1 {
		t.Fatalf("the query refused during war: %x", reply)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
	expect(send(siege.ActionSmithCancel, fortressTaxTestID, forgeTestTrainerItem, 0), 0x0f, 2, domain.FortressForgeErrUnknown)
	expect(send(siege.ActionSmithCancel, fortressTaxTestID, forgeTestSmithItem, 0),
		wire.NewWriter(10).U8(0x0f).U8(1).U32(fortressTaxTestID).U32(forgeTestSmithItem).Payload()...)
	expect(send(siege.ActionSmithCancel, fortressTaxTestID, forgeTestSmithItem, 0), 0x0f, 2, domain.FortressForgeErrNone)
	expect(send(siege.ActionSmithCollect, fortressTaxTestID, forgeTestSmithItem, 1), 0x10, 2, domain.FortressForgeErrNone)
	if g, _, _ := rt.Guilds.Guild(testDivision, guildID); g.GP != 100 || *c.Gold != 400000-324170 {
		t.Fatalf("cancel refunded: gold=%d GP=%d", *c.Gold, g.GP)
	}
}

/*
================
TestFortressTrainerOrderTakesTheRoleDiscount

A member holding exactly the trainer's role takes 15% off the price and
the time (632660's x87 0.85, truncated); the trainer refuses the smith's
items and answers on its own action numbers.
================
*/
func TestFortressTrainerOrderTakesTheRoleDiscount(t *testing.T) {
	d, guildID := fortressForgeFixture(t)
	rt, c := d.rt, d.character
	gold := int64(10000)
	d.authority.MutateCharacter(c, "forge-fixture", func() { c.Gold = &gold })
	setForgeGuild(t, d, 100, fortressRoleTrainer)
	send := func(action uint8, ref uint32, count uint16) []byte {
		t.Helper()
		return forgeReply(t, rt.HandleFortressInteraction(testDivision, c, forgeRequest(action, fortressTaxTestID, ref, count)))
	}
	if got := send(siege.ActionTrainerProduce, forgeTestSmithItem, 1); !bytes.Equal(got, []byte{0x12, 2, domain.FortressForgeErrUnknown}) {
		t.Fatalf("trainer took a smith item: %x", got)
	}
	// 5400 x 0.85 = 4590 gold, 36 x 0.85 = 30 GP, 130 minutes x 0.85 = 6630 s.
	want := wire.NewWriter(20).U8(0x12).U8(1).U32(fortressTaxTestID).U32(forgeTestTrainerItem).U16(1).U64(6630).Payload()
	if got := send(siege.ActionTrainerProduce, forgeTestTrainerItem, 1); !bytes.Equal(got, want) {
		t.Fatalf("trainer start %x, want %x", got, want)
	}
	g, _, _ := rt.Guilds.Guild(testDivision, guildID)
	if *c.Gold != 10000-4590 || g.GP != 100-30 {
		t.Fatalf("discounted payment gold=%d GP=%d", *c.Gold, g.GP)
	}
	if got := send(siege.ActionSmithQuery, 0, 0); !bytes.Equal(got, []byte{0x0d, 1, byte(fortressTaxTestID), 0, 0, 0, 0}) {
		t.Fatalf("the trainer's order showed at the smith: %x", got)
	}
	if got := send(siege.ActionTrainerQuery, 0, 0); len(got) != 22 || got[0] != 0x11 || got[6] != 1 {
		t.Fatalf("trainer query %x", got)
	}
}
