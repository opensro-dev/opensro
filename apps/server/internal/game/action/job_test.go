/*
===========================================================================

job_test.go - the job guilds, the job suit's dress timer and job mode

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	jobTestNpc      uint32 = 3001
	jobTestSuitSlot uint8  = 25
)

/*
================
jobFixture

The test character beside a selected trader guild NPC (NPC_CH_DOCTOR),
holding a trader suit in bag slot 25.
================
*/
func jobFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	suit := &enterworld.ItemRef{RefObjID: 9101, Codename: "ITEM_CH_M_TRADE_TRADER_04", Country: 3,
		TypeIDs: [4]int64{3, 1, 7, 1}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, RequiredSex: 2,
		Combat: &enterworld.ItemCombatRef{}}
	items[suit.Codename] = suit
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(jobTestSuitSlot),
		RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
	rt, clock := newTestRuntime(c, items)
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: jobTestNpc, RefObjID: 3011, Codename: "NPC_CH_DOCTOR", TalkFlags: 1,
		Services:      simulation.NpcServicesForCodename("NPC_CH_DOCTOR"),
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
	rt.NpcSpawn.Enabled = true
	rt.Selected.Set(testDivision, c.Name, jobTestNpc)
	return rt, clock, c
}

/*
================
TestJobGuildJoinAliasAndWithdraw

5120F0: level 20 joins the trader guild at its NPC, once; the alias is
checked then taken; withdrawing frees a trader at once.
================
*/
func TestJobGuildJoinAliasAndWithdraw(t *testing.T) {
	rt, _, c := jobFixture(t)
	join := wire.NewWriter(5).U32(jobTestNpc).U8(domain.JobTrader).Payload()
	if out := rt.HandleJobJoin(testDivision, c, join); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrLevel}) {
		t.Fatalf("level 1 joined: %x", out.Frames[0].Payload)
	}
	level := int64(20)
	c.Level = &level
	if out := rt.HandleJobJoin(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(domain.JobHunter).Payload()); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrNotGuild}) {
		t.Fatalf("the trader guild took a hunter: %x", out.Frames[0].Payload)
	}
	out := rt.HandleJobJoin(testDivision, c, join)
	if !bytes.Equal(out.Frames[0].Payload, []byte{1, domain.JobTrader, 1, 0, 0, 0, 0}) || c.Job.Type != domain.JobTrader {
		t.Fatalf("join = %x, job %+v", out.Frames[0].Payload, c.Job)
	}
	if out := rt.HandleJobJoin(testDivision, c, join); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrHasJob}) {
		t.Fatalf("a member joined twice: %x", out.Frames[0].Payload)
	}
	bad := rt.HandleJobAlias(testDivision, c, wire.NewWriter(9).U32(jobTestNpc).U8(jobAliasModeCreate).Str("a").Payload())
	if bad.Frames[0].Payload[0] != 2 || bad.Frames[0].Payload[1] != jobErrAliasRule {
		t.Fatalf("a one-letter alias passed: %x", bad.Frames[0].Payload)
	}
	check := rt.HandleJobAlias(testDivision, c, wire.NewWriter(10).U32(jobTestNpc).U8(jobAliasModeCheck).Str("Merchant").Payload())
	if check.Frames[0].Payload[0] != 1 || c.Job.Alias != "" {
		t.Fatalf("check = %x, alias %q", check.Frames[0].Payload, c.Job.Alias)
	}
	rt.HandleJobAlias(testDivision, c, wire.NewWriter(10).U32(jobTestNpc).U8(jobAliasModeCreate).Str("Merchant").Payload())
	if c.Job.Alias != "Merchant" {
		t.Fatalf("alias %q", c.Job.Alias)
	}
	withdraw := rt.HandleJobWithdraw(testDivision, c, wire.NewWriter(4).U32(jobTestNpc).Payload())
	if !bytes.Equal(withdraw.Frames[0].Payload, []byte{1}) || c.Job != (domain.CharacterJob{}) {
		t.Fatalf("withdraw = %x, job %+v", withdraw.Frames[0].Payload, c.Job)
	}
}

/*
================
TestJobSuitDressesAfterTenSeconds

524950: the suit move answers nothing but the dress bar; ten seconds later
the move is carried out and answered. Without an alias it refuses at once.
================
*/
func TestJobSuitDressesAfterTenSeconds(t *testing.T) {
	rt, clock, c := jobFixture(t)
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobTestSuitSlot, DestSlot: jobSuitSlot, Quantity: 1})
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1}
	if out := rt.HandleItemMove(testDivision, c, wear); out.Frames[0].Opcode != wire.OpItemMoveResponse || out.Frames[0].Payload[1] != jobWearErrNoAlias {
		t.Fatalf("no alias: %+v", out.Frames)
	}
	c.Job.Alias = "Merchant"
	out := rt.HandleItemMove(testDivision, c, wear)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opJobDressBar || out.Frames[0].Payload[6] != jobDressSeconds {
		t.Fatalf("dress began with %+v", out.Frames)
	}
	if enterworld.DressedJob(c) != 0 {
		t.Fatal("the suit was worn before the timer ended")
	}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	clock.now = clock.now.Add(jobDressSeconds*time.Second - time.Millisecond)
	rt.advanceJobDresses(clock.NowMs())
	if len(pushed) != 0 {
		t.Fatal("the dress ended early")
	}
	clock.now = clock.now.Add(time.Millisecond)
	rt.advanceJobDresses(clock.NowMs())
	if enterworld.DressedJob(c) != domain.JobTrader || len(pushed) == 0 || pushed[0].Opcode != wire.OpItemMoveResponse || pushed[0].Payload[0] != 1 {
		t.Fatalf("after ten seconds: job %d frames %+v", enterworld.DressedJob(c), pushed)
	}
}

/*
================
TestThiefDenScrollRefusesAnyoneButADressedThief

4A0380: THIEFDEN answers 0x186A to anyone not dressed as a thief.
================
*/
func TestThiefDenScrollRefusesAnyoneButADressedThief(t *testing.T) {
	c := testCharacter()
	scroll := &enterworld.ItemRef{RefObjID: 3790, Codename: "ITEM_ETC_SCROLL_RETURN_THIEFDEN_01", Country: 3,
		TypeIDs: [4]int64{3, 3, 3, 1}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, ReturnDestination: "THIEFDEN",
		ReturnTeleport: "STORE_TD_GATE", NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canUse": 1, "maxStack": 10, "itemParam1_29c": 300000, "itemParam2_2a0": 0})}
	items := testItems()
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 26, RefObjID: scroll.RefObjID,
		Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(26).U16(scroll.TypeFlags()).Payload(), errCodeThievesOnly)
}
