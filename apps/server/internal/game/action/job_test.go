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

Natively level 20 joins the trader guild at its NPC, once; the alias is
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
TestJobAliasChangeCostsLevelTimesFiftyThousand

Natively a first alias is free; changing one costs level x 50,000 gold
(0xC350), tested before the name and answered 0x4807 short; the check mode
never charges, and a paid change sends the new balance.
================
*/
func TestJobAliasChangeCostsLevelTimesFiftyThousand(t *testing.T) {
	rt, _, c := jobFixture(t)
	level, gold := int64(40), int64(0)
	c.Level, c.Gold = &level, &gold
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1}
	alias := func(mode uint8, name string) OpResult {
		return rt.HandleJobAlias(testDivision, c, wire.NewWriter(5+len(name)).U32(jobTestNpc).U8(mode).Str(name).Payload())
	}
	if out := alias(jobAliasModeCreate, "Merchant"); out.Frames[0].Payload[0] != 1 || c.Job.Alias != "Merchant" ||
		len(out.Frames) != 1 || *c.Gold != 0 {
		t.Fatalf("the first alias was not free: %x frames=%d gold=%d", out.Frames[0].Payload, len(out.Frames), *c.Gold)
	}
	const fee = 40 * 50000
	gold = fee - 1
	if out := alias(jobAliasModeCreate, "Peddler"); !bytes.Equal(out.Frames[0].Payload[:2], []byte{2, jobErrNoGold}) ||
		c.Job.Alias != "Merchant" || *c.Gold != fee-1 {
		t.Fatalf("a change short of the fee: %x alias %q gold %d", out.Frames[0].Payload, c.Job.Alias, *c.Gold)
	}
	if out := alias(jobAliasModeCheck, "Peddler"); out.Frames[0].Payload[0] != 1 || *c.Gold != fee-1 {
		t.Fatalf("the check mode charged or refused: %x gold %d", out.Frames[0].Payload, *c.Gold)
	}
	gold = fee + 5
	out := alias(jobAliasModeCreate, "Peddler")
	if out.Frames[0].Payload[0] != 1 || c.Job.Alias != "Peddler" || *c.Gold != 5 {
		t.Fatalf("the paid change: %x alias %q gold %d", out.Frames[0].Payload, c.Job.Alias, *c.Gold)
	}
	if len(out.Frames) != 2 || out.Frames[1].Opcode != wire.OpPointsUpdate ||
		!bytes.Equal(out.Frames[1].Payload, wire.GoldRefresh{Balance: 5}.Encode()) {
		t.Fatalf("no gold refresh after the fee: %+v", out.Frames)
	}
}

/*
================
TestJobSuitDressesAfterTenSeconds

Natively the suit move answers nothing but the dress bar; ten seconds later
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

Natively THIEFDEN answers 0x186A to anyone not dressed as a thief.
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

/*
================
TestJobSuitActivationHoldsTheSuit

Natively a suit just worn waits ten state ticks (+0x2178) before it is
active; until then it cannot come off (0x47), and a hit ends the wait.
================
*/
func TestJobSuitActivationHoldsTheSuit(t *testing.T) {
	rt, clock, c := jobFixture(t)
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Merchant"}
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobTestSuitSlot, DestSlot: jobSuitSlot, Quantity: 1})
	rt.HandleItemMove(testDivision, c, wear)
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	rt.advanceJobDresses(clock.NowMs())
	if enterworld.DressedJob(c) != domain.JobTrader || !rt.jobActivationPending(testDivision, c, clock.NowMs()) {
		t.Fatal("a freshly worn suit is active at once")
	}
	if code := rt.jobStripRefusal(testDivision, c, jobTestSuitSlot, clock.NowMs()); code != jobWearErrPending {
		t.Fatalf("strip during activation = %#x, want %#x", code, jobWearErrPending)
	}
	clock.now = clock.now.Add(jobActivationMs*time.Millisecond - time.Millisecond)
	if !rt.jobActivationPending(testDivision, c, clock.NowMs()) {
		t.Fatal("the activation ended early")
	}
	clock.now = clock.now.Add(time.Millisecond)
	if rt.jobActivationPending(testDivision, c, clock.NowMs()) {
		t.Fatal("the activation outlived its ten ticks")
	}
	rt.startJobActivation(testDivision, c, clock.NowMs())
	rt.endJobActivation(testDivision, c)
	if rt.jobActivationPending(testDivision, c, clock.NowMs()) {
		t.Fatal("a hit did not end the activation")
	}
}

/*
================
TestJobSuitStripOutsideTownAnswersAtOnce

Natively a worn suit comes off only where players
may not fight. Outside a town the move is refused at once with 0xA2,
which the client words as UIIT_MSG_JOBITEM_WEAR_ERR_NOPOSITION; in a town
the dress bar starts. Region 0x5F76 is the report's (thread "trader flag
cant out"), 0x61A8 is Zangan.
================
*/
func TestJobSuitStripOutsideTownAnswersAtOnce(t *testing.T) {
	const field, town = uint16(0x5F76), uint16(0x61A8)
	for _, region := range []uint16{field, town} {
		rt, _, c := jobFixture(t)
		c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Merchant"}
		for i := range c.MissionInventory {
			if c.MissionInventory[i].Slot == int64(jobTestSuitSlot) {
				c.MissionInventory[i].Slot = int64(jobSuitSlot)
			}
		}
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(world *simulation.WorldState) {
			world.Spawn = simulation.Spawn{RegionID: region, X: 960, Y: 20, Z: 960}
		})
		strip := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobSuitSlot, DestSlot: jobTestSuitSlot, Quantity: 1})
		out := rt.HandleItemMove(testDivision, c, strip)
		if len(out.Frames) != 1 {
			t.Fatalf("region %#x: frames %+v", region, out.Frames)
		}
		frame := out.Frames[0]
		switch region {
		case field:
			if frame.Opcode != wire.OpItemMoveResponse || len(frame.Payload) != 2 || frame.Payload[0] != wire.ResultError || frame.Payload[1] != jobWearErrNoPosition {
				t.Fatalf("outside a town: %#x % x, want the 0xA2 refusal", frame.Opcode, frame.Payload)
			}
		case town:
			if frame.Opcode != opJobDressBar {
				t.Fatalf("in a town: %#x % x, want the dress bar", frame.Opcode, frame.Payload)
			}
		}
	}
}

/*
================
TestJobAliasRefusesACharacterName

An alias may not equal another character's name - any casing, and even
while that character is pending deletion: a suited player speaks under
the alias, so taking a real name would impersonate its owner. The
reference server's alias validity never consults character names; that
impersonation is a native bug we do not copy (owner, 2026-10-28), so
the refusal is unconditional. A name nobody owns still takes.
================
*/
func TestJobAliasRefusesACharacterName(t *testing.T) {
	rt, _, c := jobFixture(t)
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1}
	other := testCharacter()
	other.ID, other.Name = 44, "Berk"
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {c, other}}
	alias := func(name string) OpResult {
		return rt.HandleJobAlias(testDivision, c, wire.NewWriter(5+len(name)).U32(jobTestNpc).U8(jobAliasModeCreate).Str(name).Payload())
	}
	if out := alias("berk"); !bytes.Equal(out.Frames[0].Payload[:2], []byte{2, jobErrAliasTaken}) || c.Job.Alias != "" {
		t.Fatalf("alias equal to a character name: %x alias %q", out.Frames[0].Payload, c.Job.Alias)
	}
	other.DeletePending = true
	if out := alias("Berk"); !bytes.Equal(out.Frames[0].Payload[:2], []byte{2, jobErrAliasTaken}) || c.Job.Alias != "" {
		t.Fatalf("alias equal to a deleted-pending name: %x alias %q", out.Frames[0].Payload, c.Job.Alias)
	}
	if out := alias("Merchant"); out.Frames[0].Payload[0] != 1 || c.Job.Alias != "Merchant" {
		t.Fatalf("a free alias refused: %x alias %q", out.Frames[0].Payload, c.Job.Alias)
	}
}
