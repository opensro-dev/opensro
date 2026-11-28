package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
captureFixture

A Jangan fortress world with one fort stone and one guard tower, the
fixture character inside it in guild 77, and every frame pushed to it
recorded by subtype.
================
*/
func captureFixture(t *testing.T) (*Runtime, *enterworld.Character, *fakeClock, *[]byte) {
	t.Helper()
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			19553: {RefObjID: 19553, MaxHP: 900, ScaleDenom: 100, Structure: true, TypeID4: structureKindFortStone},
			19536: {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true, TypeID4: structureKindGuardTower, CanRevive: true, CostRevive: 100},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19553, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 84},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62aa, X: 140, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 85},
		},
	)
	rt, c, clock := fortressFixtureWithPopulation(t, testFieldFortGate, template)
	var subtypes []byte
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode == opFortressWarState && len(frame.Payload) > 0 {
				subtypes = append(subtypes, frame.Payload[0])
			}
		}
	}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	guild := int64(77)
	c.GuildID = &guild
	enterFortress(t, rt, c)
	rt.Monsters.AdvancePopulation(clock.NowMs() + monster.NestHiveTickMs)
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 1 {
		t.Fatal("the fortress world holds no guard tower")
	}
	return rt, c, clock, &subtypes
}

/*
================
killStructure

A fatal hit credited to c, then the death the combat owners queue.
================
*/
func killStructure(t *testing.T, rt *Runtime, c *enterworld.Character, kind uint8, nowMs int64) []simulation.DivisionFrames {
	t.Helper()
	lease, _ := rt.Monsters.PopulationLease(testDivision, instance.Pack(2, 1))
	for _, row := range rt.Monsters.PopulationInstances(testDivision, lease, []uint16{0x62aa}) {
		if row.Ref.TypeID4 != kind || row.CurrentHP == 0 {
			continue
		}
		results, ok := rt.Monsters.ApplyDamageBatch(testDivision, []simulation.MonsterDamagePlan{{
			GID: row.Gid, ExpectedHP: row.CurrentHP, Damage: row.CurrentHP, CreditGID: enterworld.ObjectIDForCharacter(c),
		}})
		if !ok || len(results) != 1 || !results[0].Fatal {
			t.Fatalf("structure %d did not die", row.Ref.RefObjID)
		}
		rt.queueMonsterDefeat(testDivision, row.Gid, nowMs)
		return rt.drainStructureDeaths(nowMs)
	}
	t.Fatalf("no standing structure of kind %d", kind)
	return nil
}

/*
================
TestTheStoneFallsToTheGuildThatBreaksIt

The reference server keeps a fallen structure in its destroyed state and tells the
fortress world (0x0B); the last tower starts the stone's countdown (0x0A);
the stone's fall hands the fortress to the breaker's guild, reinstalls
the structures and announces the conquest (subtype 8).
================
*/
func TestTheStoneFallsToTheGuildThatBreaksIt(t *testing.T) {
	rt, c, clock, subtypes := captureFixture(t)
	now := clock.NowMs()
	rt.FortressWarChanged(testDivision, now, true)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	if code := rt.Fortresses.StoneRefusal(testDivision, jangan, now); code == 0 {
		t.Fatal("the stone is open while its tower stands")
	}
	killStructure(t, rt, c, structureKindGuardTower, now)
	if string(*subtypes) != string([]byte{siege.SubtypeStructureState, siege.SubtypeTowersFallen}) {
		t.Fatalf("the tower's fall sent %x", *subtypes)
	}
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 0 {
		t.Fatal("the destroyed tower stands")
	}
	*subtypes = nil
	out := killStructure(t, rt, c, structureKindFortStone, now+1000)
	record, _ := rt.Fortresses.Get(testDivision, jangan)
	if record.Holder() != 77 || record.EntryOpen {
		t.Fatalf("after the stone fell %+v", record)
	}
	if len(out) != 1 || out[0].Frames[0].Payload[0] != siege.SubtypeConquest {
		t.Fatalf("conquest broadcast %+v", out)
	}
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 1 {
		t.Fatal("the capture did not reinstall the tower")
	}
	if c.World.PackedInstance == nil {
		t.Fatal("the new holder was sent away")
	}
	if owner, changed := rt.Fortresses.FinishWar(testDivision, jangan); owner != 77 || !changed {
		t.Fatalf("war end = %d %v", owner, changed)
	}
}

/*
================
seatAtTheStone

One more resident of the fortress world for the capture test: a clone
of the fixture character under its own name and guild, admitted and
seated inside Jangan.
================
*/
func seatAtTheStone(t *testing.T, rt *Runtime, base *enterworld.Character, id int64, name string, guild int64) *enterworld.Character {
	t.Helper()
	c := *base
	c.ID, c.Name = id, name
	c.GuildID = &guild
	world := *base.World
	spawn := *world.Spawn
	world.Spawn = &spawn
	c.World = &world
	deps := rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], &c)
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	if _, leased := rt.Monsters.PopulationLease(testDivision, instance.Pack(2, 1)); !leased {
		if _, status := rt.Monsters.AllocatePopulation(testDivision, instance.Pack(2, 1)); status != instance.Success {
			t.Fatal(status)
		}
	}
	if _, moved := rt.moveWorldMembership(testDivision, &c, instance.ID(domain.DefaultWorldInstance), instance.Pack(2, 1)); !moved {
		t.Fatal("the resident move was refused")
	}
	setCharacterWorld(&c, instance.Pack(2, 1))
	return &c
}

/*
================
TestCaptureResetsAttackersToTheFortressGate

On the stone's fall the new holder's side (the holder and the applied
guilds of its union) stays where it stands and hears the fresh fortress
list; every attacker, the old owner included, is reset to the fortress's
own gate point - still inside the world, not expelled to the town. A
resident outside the war keeps their place.
================
*/
func TestCaptureResetsAttackersToTheFortressGate(t *testing.T) {
	rt, c, clock, _ := captureFixture(t)
	now := clock.NowMs()
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	rt.FortressWarChanged(testDivision, now, true)
	rt.Fortresses.Occupy(testDivision, jangan, 7)
	rt.Fortresses.SetApplication(testDivision, jangan, 9, fortress.RequestAttack, true)
	rt.Fortresses.SetApplication(testDivision, jangan, 11, fortress.RequestAttack, true)
	rt.Fortresses.SetApplication(testDivision, jangan, 12, fortress.RequestAlly, true)
	rt.Unions = union.New()
	if _, err := rt.Unions.Join(testDivision, 9, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Unions.Join(testDivision, 7, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Unions.Join(testDivision, 9, 14); err != nil {
		t.Fatal(err)
	}
	breaker := int64(9)
	c.GuildID = &breaker
	old := seatAtTheStone(t, rt, c, 41, "OldHolder", 7)
	ally := seatAtTheStone(t, rt, c, 42, "NewAlly", 11)
	oldAlly := seatAtTheStone(t, rt, c, 43, "OldAlly", 12)
	by := seatAtTheStone(t, rt, c, 44, "Bystander", 13)
	quiet := seatAtTheStone(t, rt, c, 45, "QuietAlly", 14)

	heard := map[string][]byte{}
	resets := map[string]int{}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		for _, f := range frames {
			if f.Opcode == opFortressWarState && len(f.Payload) > 0 {
				heard[name] = append(heard[name], f.Payload[0])
			}
			if f.Opcode == enterworld.OpcodeResetClient {
				resets[name]++
			}
		}
	}
	rt.FortressList = func(int64) []byte { return []byte{0} }
	rt.RevivalRoll = func() (uint32, error) { return 0, nil }

	killStructure(t, rt, c, structureKindGuardTower, now)
	killStructure(t, rt, c, structureKindFortStone, now+1000)

	record, _ := rt.Fortresses.Get(testDivision, jangan)
	if record.Holder() != 9 {
		t.Fatalf("after the stone fell %+v", record)
	}
	gate, ok := rt.randomGate(rt.worldGates(2, gateKindFortressGate))
	if !ok {
		t.Fatal("Jangan has no fortress gate inside")
	}
	inside := func(x *enterworld.Character) bool {
		return x.World.PackedInstance != nil && instance.ID(*x.World.PackedInstance) == instance.Pack(2, 1)
	}
	atGate := func(x *enterworld.Character) bool {
		return inside(x) && x.World.Spawn != nil && x.World.Spawn.RegionID != nil && *x.World.Spawn.RegionID == int64(gate.spawn.RegionID)
	}
	heardList := func(name string) bool {
		for _, subtype := range heard[name] {
			if subtype == 0 {
				return true
			}
		}
		return false
	}
	if !inside(c) || resets[c.Name] != 0 || !heardList(c.Name) {
		t.Fatalf("the breaker: inside=%v resets=%d list=%v", inside(c), resets[c.Name], heardList(c.Name))
	}
	if !inside(ally) || resets[ally.Name] != 0 || !heardList(ally.Name) {
		t.Fatalf("the new holder's ally: inside=%v resets=%d list=%v", inside(ally), resets[ally.Name], heardList(ally.Name))
	}
	if !atGate(old) || resets[old.Name] == 0 || heardList(old.Name) {
		t.Fatalf("the old owner: atGate=%v resets=%d list=%v", atGate(old), resets[old.Name], heardList(old.Name))
	}
	if !atGate(oldAlly) || resets[oldAlly.Name] == 0 || heardList(oldAlly.Name) {
		t.Fatalf("the old owner's ally: atGate=%v resets=%d list=%v", atGate(oldAlly), resets[oldAlly.Name], heardList(oldAlly.Name))
	}
	if !inside(by) || resets[by.Name] != 0 || heardList(by.Name) {
		t.Fatalf("the bystander: inside=%v resets=%d list=%v", inside(by), resets[by.Name], heardList(by.Name))
	}
	if !inside(quiet) || resets[quiet.Name] != 0 || heardList(quiet.Name) {
		t.Fatalf("the unapplied ally: inside=%v resets=%d list=%v", inside(quiet), resets[quiet.Name], heardList(quiet.Name))
	}
}

/*
================
TestBootSettleFinishesOnlyTheHeldFortress

A shard booting outside a war finishes a fortress a crash left
temporarily held: the temporary holder becomes the owner and that
fortress's registrations release when its end phase runs. The other
fortress keeps its owner and its applicants.
================
*/
func TestBootSettleFinishesOnlyTheHeldFortress(t *testing.T) {
	rt, _, clock := fortressFixtureWithPopulation(t, testFieldFortGate, monster.TemplateFromParts(nil, nil))
	now := clock.NowMs()
	// The shipped data activates Jangan alone; the second row stands in
	// for a fortress the boot must not touch (INS_FORT_HT ships too).
	rt.Fortresses = fortress.New([]fortress.Catalog{
		{ID: 1, CodeName: "FORTRESS_JANGAN", TownGate: "GATE_CH"},
		{ID: 2, CodeName: "FORTRESS_HOTAN", TownGate: "GATE_CH"},
	})
	jangan, other := uint32(1), uint32(2)
	rt.Fortresses.Occupy(testDivision, jangan, 7)
	rt.Fortresses.SetApplication(testDivision, jangan, 9, fortress.RequestAttack, true)
	if !rt.Fortresses.Capture(testDivision, jangan, 9, now) {
		t.Fatal("the capture did not stand")
	}
	rt.Fortresses.Occupy(testDivision, other, 42)
	rt.Fortresses.SetApplication(testDivision, other, 5, fortress.RequestAttack, true)

	rt.FortressBootSettle(testDivision, now)
	rt.advanceFortressPhases(now)

	held, _ := rt.Fortresses.Get(testDivision, jangan)
	if held.GuildID != 9 || held.TempGuildID != 0 || len(held.Applicants) != 0 {
		t.Fatalf("the held fortress after the boot settle: %+v", held)
	}
	untouched, _ := rt.Fortresses.Get(testDivision, other)
	if untouched.GuildID != 42 || untouched.TempGuildID != 0 {
		t.Fatalf("the other fortress was settled too: %+v", untouched)
	}
	if _, applied := untouched.Applicants[5]; !applied {
		t.Fatalf("the other fortress lost its applicants: %+v", untouched.Applicants)
	}
}
