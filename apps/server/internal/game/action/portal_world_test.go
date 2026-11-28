package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/staff"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	// STORE_CH_FORT_GATE1 (teleport 40, field), STORE_JA_FORT_GATE1
	// (teleport 43, INS_FORT_JA) and STORE_JA_CHARGE_GATE (teleport 46, a
	// portal stone inside the fortress).
	testFieldFortGate  uint32 = 23285
	testInnerFortGate  uint32 = 22574
	testFortChargeGate uint32 = 22577
)

/*
================
fortressPortalFixture

A runtime with the shipped portal catalog, an admitted session and the
named gate standing at the player.
================
*/
func fortressPortalFixture(t *testing.T, gateRef uint32) (*Runtime, *enterworld.Character) {
	rt, c, _ := fortressFixtureWithClock(t, gateRef)
	return rt, c
}

/*
================
fortressFixtureWithClock
================
*/
func fortressFixtureWithClock(t *testing.T, gateRef uint32) (*Runtime, *enterworld.Character, *fakeClock) {
	t.Helper()
	return fortressFixtureWithPopulation(t, gateRef, monster.TemplateFromParts(nil, nil))
}

/*
================
fortressFixtureWithPopulation

The fortress fixture over a given monster template.
================
*/
func fortressFixtureWithPopulation(t *testing.T, gateRef uint32, template monster.Template) (*Runtime, *enterworld.Character, *fakeClock) {
	t.Helper()
	clientdata.RequireGameData(t)
	rt, c, clock, _ := returnFixture(t, 30000)
	rt.Monsters = simulation.NewMonsterState(template)
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 900000 + gateRef, RefObjID: gateRef, Codename: "GATE",
		Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, c, clock
}

/*
================
usePortal
================
*/
func usePortal(rt *Runtime, c *enterworld.Character, gateRef, target uint32) OpResult {
	gid := 900000 + gateRef
	rt.NpcRoster[0].ObjectID, rt.NpcRoster[0].RefObjID = gid, gateRef
	rt.Selected.Set(testDivision, c.Name, gid)
	return rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(gid).U8(2).U32(target).Payload())
}

/*
================
TestFortressGateMovesThePlayerIntoTheFortressWorld

teleportdata's GenWorldID puts GATE_JA_FORT_GATE1 in INS_FORT_JA: the
character's world and its population membership both follow the gate,
and the way back returns both to the field.
================
*/
func TestFortressGateMovesThePlayerIntoTheFortressWorld(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	out := usePortal(rt, c, testFieldFortGate, 43)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("fortress gate did not enter: %+v", out)
	}
	fortressWorld := instance.Pack(2, 1)
	if instance.ID(domain.CharacterWorldInstance(c)) != fortressWorld {
		t.Fatalf("character world %08x", domain.CharacterWorldInstance(c))
	}
	if lease, ok := rt.EntryPopulationLease(testDivision, c.Name); !ok || lease.ID != fortressWorld {
		t.Fatalf("population membership did not follow: %+v %v", lease, ok)
	}
	out = usePortal(rt, c, testInnerFortGate, 40)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("return gate did not enter: %+v", out)
	}
	if c.World.PackedInstance != nil {
		t.Fatal("the field is stored as the absent default world")
	}
	if lease, ok := rt.EntryPopulationLease(testDivision, c.Name); !ok || uint32(lease.ID) != domain.DefaultWorldInstance {
		t.Fatalf("membership did not return to the field: %+v", lease)
	}
}

/*
================
TestFortressGateRefusesAJobSuit

Natively job state 4 (no job) is required at a fortress gate.
================
*/
func TestFortressGateRefusesAJobSuit(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	dressTrader(c)
	out := usePortal(rt, c, testFieldFortGate, 43)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, portalFortressJobSuit}) {
		t.Fatalf("job suit entered the fortress: %+v", out)
	}
	if c.World.PackedInstance != nil {
		t.Fatal("refused gate changed the world")
	}
}

/*
================
TestFortressPortalStoneServesOnlyTheOwningGuild

The portal stones inside a fortress are not fortress gates (TypeID4 3): an
unoccupied fortress has no owner, so every player is refused 0x1C1D.
================
*/
func TestFortressPortalStoneServesOnlyTheOwningGuild(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	if out := usePortal(rt, c, testFieldFortGate, 43); len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("fortress gate did not enter: %+v", out)
	}
	guild := int64(7)
	c.GuildID = &guild
	out := usePortal(rt, c, testFortChargeGate, 47)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, portalFortressOwnersOnly}) {
		t.Fatalf("portal stone served a non-owner: %+v", out)
	}
}

/*
================
TestFortressPortalStoneServesTheTemporaryHolder

The inner gates judge the holder, not the owner: once a capture stands
the capturing guild uses the charge gate and the old owner's guild is
refused 0x1C1D, though nothing about the stone moved.
================
*/
func TestFortressPortalStoneServesTheTemporaryHolder(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	enterFortress(t, rt, c)
	var jangan uint32
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	rt.Fortresses.Occupy(testDivision, jangan, 7)
	owner := int64(7)
	c.GuildID = &owner
	if out := usePortal(rt, c, testFortChargeGate, 47); len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("the owner's guild was refused its charge gate: %+v", out)
	}
	if !rt.Fortresses.Capture(testDivision, jangan, 9, rt.Now().UnixMilli()) {
		t.Fatal("the capture did not stand")
	}
	out := usePortal(rt, c, testFortChargeGate, 47)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, portalFortressOwnersOnly}) {
		t.Fatalf("portal stone served the old owner over the holder: %+v", out)
	}
	captor := int64(9)
	c.GuildID = &captor
	if out := usePortal(rt, c, testFortChargeGate, 47); len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("the temporary holder was refused its charge gate: %+v", out)
	}
}

/*
================
enterFortress
================
*/
func enterFortress(t *testing.T, rt *Runtime, c *enterworld.Character) {
	t.Helper()
	if out := usePortal(rt, c, testFieldFortGate, 43); len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("fortress gate did not enter: %+v", out)
	}
}

/*
================
inField
================
*/
func inField(t *testing.T, rt *Runtime, c *enterworld.Character, what string) {
	t.Helper()
	if c.World.PackedInstance != nil {
		t.Fatalf("%s left the character in world %08x", what, *c.World.PackedInstance)
	}
	if lease, ok := rt.EntryPopulationLease(testDivision, c.Name); !ok || uint32(lease.ID) != domain.DefaultWorldInstance {
		t.Fatalf("%s left the membership in %+v", what, lease)
	}
}

/*
================
TestReturnScrollLeavesTheFortressForTheAppointedTown

Natively the appointed town resolves through its teleport, world included:
a return from inside a fortress lands in the field world.
================
*/
func TestReturnScrollLeavesTheFortressForTheAppointedTown(t *testing.T) {
	rt, c, clock := fortressFixtureWithClock(t, testFieldFortGate)
	enterFortress(t, rt, c)
	useReturn(rt, c)
	if c.World.LastRecallPoint == nil || c.World.LastRecallPoint.World != 2 {
		t.Fatalf("recall point inside the fortress: %+v", c.World.LastRecallPoint)
	}
	clock.Advance(30 * time.Second)
	advanceAndDrainReturns(rt, clock.NowMs())
	inField(t, rt, c, "the return scroll")
}

/*
================
TestReverseReturnGoesBackIntoTheRecordedWorld
================
*/
func TestReverseReturnGoesBackIntoTheRecordedWorld(t *testing.T) {
	region, x := int64(17221), 812.0
	c := &enterworld.Character{World: &domain.CharacterWorld{
		LastDeathPoint: &domain.WorldPoint{WorldSpawn: domain.WorldSpawn{RegionID: &region, X: &x}, World: 2}}}
	point, refusal := reverseReturnPoint(c, reverseReturnLastDeath)
	if refusal != 0 || point.world != instance.Pack(2, 1) || point.spawn.RegionID != 17221 {
		t.Fatalf("reverse return point %+v (%d)", point, refusal)
	}
}

/*
================
TestTownRevivalLeavesTheFortress
================
*/
func TestTownRevivalLeavesTheFortress(t *testing.T) {
	rt, c, clock := fortressFixtureWithClock(t, testFieldFortGate)
	enterFortress(t, rt, c)
	zero := int64(0)
	c.CurrentHP = &zero
	rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, clock.NowMs())
	out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
	if len(out.Frames) == 0 {
		t.Fatalf("revival refused: %+v", out)
	}
	inField(t, rt, c, "town revival")
}

/*
================
TestGMWarpLeavesTheFortressForTheField
================
*/
func TestGMWarpLeavesTheFortressForTheField(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	enterFortress(t, rt, c)
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(_ *enterworld.Character, r uint16) bool { return r == 25416 }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	c.GMPrivilege, c.GMCapabilities = true, uint32(staff.All)
	if !rt.WarpGM(testDivision, c.Name, wire.Position{RegionID: 25416, X: 703, Y: 42, Z: 1575}) {
		t.Fatal("warp refused")
	}
	inField(t, rt, c, "the GM warp")
}

/*
================
TestFortressWarBeginsByClearingTheFortress

The native war phase mode 0, five seconds after the war begins: an unoccupied fortress
sends everyone to its town gate (GATE_CH, in the field); the war's end
(mode 2) does so at once.
================
*/
func TestFortressWarBeginsByClearingTheFortress(t *testing.T) {
	rt, c, clock := fortressFixtureWithClock(t, testFieldFortGate)
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	enterFortress(t, rt, c)
	now := clock.NowMs()
	rt.FortressWarChanged(testDivision, now, true)
	rt.advanceFortressPhases(now + fortressBeginDelayMs - 1)
	if c.World.PackedInstance == nil {
		t.Fatal("expelled before the five-second wait")
	}
	rt.advanceFortressPhases(now + fortressBeginDelayMs)
	inField(t, rt, c, "the war's beginning")
	gate, _ := rt.fortressTownGate("GATE_CH")
	if *c.World.Spawn.RegionID != int64(gate.spawn.RegionID) {
		t.Fatalf("expelled to region %d, want the town gate's %d", *c.World.Spawn.RegionID, gate.spawn.RegionID)
	}
	enterFortress(t, rt, c)
	rt.FortressWarChanged(testDivision, now, false)
	rt.advanceFortressPhases(now)
	inField(t, rt, c, "the war's end")
}

/*
================
TestFortressWarEntryCapsAndCaptureWait

The war-entry check, in the original's order: a guild that neither
owns the fortress nor applied hears 0x1C; for five minutes after a
capture only the holder's side enters (the attackers hear 0x1E); then
each side counts the PCs already inside - the holder's side against
half of MaxEntrance, the attackers against half of it while the
fortress has an owner - and a full side hears 0x1B. The capturing
guild applied, as it must to fight: the application, not the capture,
admits it past the participant test.
================
*/
func TestFortressWarEntryCapsAndCaptureWait(t *testing.T) {
	rt, c, clock := fortressFixtureWithClock(t, testFieldFortGate)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1, CodeName: "FORTRESS_JANGAN", MaxEntrance: 4}})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.Fortresses.Occupy(testDivision, 1, 77)
	enterAs := func(guild int64) byte {
		if guild == 0 {
			c.GuildID = nil
		} else {
			c.GuildID = &guild
		}
		return rt.fortressWarEntry(testDivision, c, 1)
	}
	if got := enterAs(0); got != portalFortressNotInWar {
		t.Fatalf("a guildless entry answered %#x", got)
	}
	if got := enterAs(55); got != portalFortressNotInWar {
		t.Fatalf("an uninvolved guild answered %#x", got)
	}
	rt.Fortresses.SetApplication(testDivision, 1, 42, fortress.RequestAttack, true)
	if got := enterAs(42); got != 0 {
		t.Fatalf("an applied attacker answered %#x", got)
	}
	rt.Fortresses.SetApplication(testDivision, 1, 88, fortress.RequestAttack, true)
	if !rt.Fortresses.Capture(testDivision, 1, 88, clock.NowMs()) {
		t.Fatal("the capture did not stand")
	}
	if got := enterAs(42); got != portalFortressCaptureWait {
		t.Fatalf("an attacker in the capture wait answered %#x", got)
	}
	if got := enterAs(88); got != 0 {
		t.Fatalf("the temporary holder answered %#x", got)
	}
	rt.Fortresses.Advance(clock.NowMs() + fortress.CaptureWaitMs)
	if got := enterAs(42); got != 0 {
		t.Fatalf("an attacker after the wait answered %#x", got)
	}
	deps := rt.deps.(*enterworld.Deps)
	stand := func(name string, id int64, guild int64) {
		member := *c
		member.ID, member.Name = id, name
		member.GuildID = &guild
		fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], &member)
		rt.characterAdmissions.Store(simulation.WorldKey(testDivision, name), populationAdmission{
			lease: instance.Lease{ID: instance.Pack(2, portalWorldLayer)}, division: testDivision, name: name})
	}
	stand("holderTwo", 31, 88)
	stand("holderThree", 32, 88)
	if got := enterAs(88); got != portalFortressFull {
		t.Fatalf("a defender past half of MaxEntrance answered %#x", got)
	}
	stand("attackerTwo", 33, 42)
	stand("attackerThree", 34, 42)
	if got := enterAs(42); got != portalFortressFull {
		t.Fatalf("an attacker past half of MaxEntrance answered %#x", got)
	}
}
