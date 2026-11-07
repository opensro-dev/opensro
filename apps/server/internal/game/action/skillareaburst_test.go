/*
===========================================================================

skillareaburst_test.go - Booming Chord through the untargeted area-burst owner

The untargeted command strikes the monsters around the caster at once,
with the per-victim reduction cascade, and charges its cost even when no
monster stands in range.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// boomingChordCode is tier 1: efr 1 1 80 5 35 24, mc 2 1.
	boomingChordCode = "SKILL_EU_BARD_BATTLAA_EXPLOSION_A_01"
	// boomingMonsterHP keeps every victim alive through the strike.
	boomingMonsterHP = 1000000
	// areaResultTargetCount is the target-count byte of a B070 area result
	// (prefix, steering flag, impact count, target count).
	areaResultTargetCount = 20
)

/*
================
boomingFixture

The combat fixture with its single monster replaced by one nest per
caster-relative X offset, a Bard with Booming Chord learned and a harp in
the weapon slot (harp is false to equip the fixture's sword unchanged).
================
*/
func boomingFixture(t *testing.T, offsets []float64, harp bool) (*Runtime, *enterworld.Character, enterworld.SkillRow, []monster.Instance) {
	t.Helper()
	return boomingFixtureHP(t, boomingMonsterHP, offsets, harp)
}

/*
================
boomingFixtureHP

boomingFixture with the victims' HP chosen by the caller, so a test
can field monsters the strike kills.
================
*/
func boomingFixtureHP(t *testing.T, hp uint32, offsets []float64, harp bool) (*Runtime, *enterworld.Character, enterworld.SkillRow, []monster.Instance) {
	t.Helper()
	rt, clock, c, primary := newCombatTestRuntime(t, hp)
	row, ok := shippedSkillSource(t).SkillByCodename(boomingChordCode)
	if !ok || !row.AreaBurst {
		t.Fatalf("%s not on the area-burst route: %q", boomingChordCode, row.OffenseRefusal)
	}
	caster := *c.World.Spawn.X
	var nests []monster.NestRow
	for _, offset := range offsets {
		spawn := primary.Spawn
		spawn.X = caster + offset
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	monsters := rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
	if len(monsters) != len(offsets) {
		t.Fatalf("materialized %d monsters, want %d", len(monsters), len(offsets))
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{row.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	if harp {
		weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
		weapon.TypeIDs[3] = int64(row.RequiredWeaponKinds[0])
		c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	}
	return rt, c, row, monsters
}

/*
================
TestBoomingChordStrikesMonstersAroundTheCaster

Three monsters inside the 80-unit burst and one beyond it: the untargeted
command damages the three in selection order at 100, 65 and 42 percent
(35 percent less per victim, cumulative), spares the far one, charges the
prepared MP once, publishes one area result under one token and closes
its bracket after the action duration.
================
*/
func TestBoomingChordStrikesMonstersAroundTheCaster(t *testing.T) {
	rt, c, skill, monsters := boomingFixture(t, []float64{3, 30, 60, 200}, true)
	prepared, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil || prepared == 0 {
		t.Fatalf("prepared cost %d: %v", prepared, err)
	}
	_, _, _, beforeMP := rt.playerKeeperVitals(testDivision, c)

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	open, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || open.Payload[0] != 1 || len(open.Payload) <= areaResultTargetCount || open.Payload[areaResultTargetCount] != 3 {
		t.Fatalf("Booming Chord did not publish three results: %+v", out)
	}
	var lost []uint32
	for _, before := range monsters {
		after, _ := rt.Monsters.Get(testDivision, before.Gid)
		lost = append(lost, before.CurrentHP-after.CurrentHP)
	}
	if lost[0] == 0 || lost[3] != 0 {
		t.Fatalf("damage near/far: %v", lost)
	}
	for i, percent := range []uint64{100, 65, 42} {
		if want := uint32(uint64(lost[0]) * percent / 100); lost[i] != want {
			t.Fatalf("victim %d lost %d, want %d (%d percent of %d)", i, lost[i], want, percent, lost[0])
		}
	}
	_, _, _, afterMP := rt.playerKeeperVitals(testDivision, c)
	if beforeMP-afterMP != prepared || rt.castTokenCounter != 1 {
		t.Fatalf("MP %d -> %d (prepared %d), tokens %d", beforeMP, afterMP, prepared, rt.castTokenCounter)
	}
	closed := false
	for _, batch := range rt.drainSkillFinalizes(rt.Now().UnixMilli() + int64(skill.ActionDurationMs)) {
		for _, f := range batch.Frames {
			closed = closed || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == skillCastFinalizeLen && f.Payload[0] == 2
		}
	}
	if !closed {
		t.Fatal("Booming Chord never closed its cast bracket")
	}
}

/*
================
TestBoomingChordChargesWithoutVictims

No monster in range: the cast still opens, debits its MP and installs its
cooldown, and nothing is damaged.
================
*/
func TestBoomingChordChargesWithoutVictims(t *testing.T) {
	rt, c, skill, monsters := boomingFixture(t, []float64{300}, true)
	prepared, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, beforeMP := rt.playerKeeperVitals(testDivision, c)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if open, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || open.Payload[0] != 1 {
		t.Fatalf("empty Booming Chord refused: %+v", out)
	}
	_, _, _, afterMP := rt.playerKeeperVitals(testDivision, c)
	if beforeMP-afterMP != prepared {
		t.Fatalf("MP %d -> %d, prepared %d", beforeMP, afterMP, prepared)
	}
	if after, _ := rt.Monsters.Get(testDivision, monsters[0].Gid); after.CurrentHP != monsters[0].CurrentHP {
		t.Fatal("out-of-range monster damaged")
	}
	if !skillCoolingDown(c, skill, rt.Now().UnixMilli()) {
		t.Fatal("cooldown not installed")
	}
}

/*
================
TestBoomingChordRequiresHarp

The authored weapon column still gates the untargeted route: without a
harp nothing is charged and nothing is damaged.
================
*/
func TestBoomingChordRequiresHarp(t *testing.T) {
	rt, c, skill, monsters := boomingFixture(t, []float64{3}, false)
	_, _, _, beforeMP := rt.playerKeeperVitals(testDivision, c)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	_, _, _, afterMP := rt.playerKeeperVitals(testDivision, c)
	after, _ := rt.Monsters.Get(testDivision, monsters[0].Gid)
	if afterMP != beforeMP || after.CurrentHP != monsters[0].CurrentHP || rt.castTokenCounter != 0 {
		t.Fatalf("cast without harp: MP %d -> %d, HP %d -> %d", beforeMP, afterMP, monsters[0].CurrentHP, after.CurrentHP)
	}
}

/*
================
TestAreaBurstBuildsTheRewardRosterOnlyForAKill

#2030 strike gate, burst site: a burst whose monster victims all
survive their plans doors the caster, the player victims and the COS
owners and builds no reward roster; a victim whose plan can kill
keeps the roster-wide door.

The witness is the roster build's own product: the character list
the burst hands its door. Two bystanders stand in the division
outside the cast; only a division-wide roster build can put them on
that list - the narrow door holds the caster, the player victims
and the COS owners alone, and player victims are population players,
never character-source records, so the bystanders cannot leak in as
victims. The older witness could pass for the wrong reason: the
party callback is also read by the share lanes (partyShareRoster),
so counting its calls never isolated this build. The area lane's
pin (TestAreaStrikeBuildsTheRewardRosterOnlyForAKill) witnesses the
same way.
================
*/
func TestAreaBurstBuildsTheRewardRosterOnlyForAKill(t *testing.T) {
	doorNames := func(rt *Runtime) *[]string {
		names := &[]string{}
		rt.deps.(*enterworld.Deps).UpdateCharacters = func(characters []*enterworld.Character, label string, update func() bool) bool {
			if label == "player-area-burst" {
				for _, character := range characters {
					*names = append(*names, character.Name)
				}
			}
			return update()
		}
		return names
	}
	addBystanders := func(rt *Runtime, c *enterworld.Character) (string, string) {
		bystanderA := *c
		bystanderA.ID, bystanderA.Name = 4, "burst-bystander-a"
		bystanderB := *c
		bystanderB.ID, bystanderB.Name = 5, "burst-bystander-b"
		door := rt.deps.(*enterworld.Deps).Characters.(*storeDoor)
		door.inner = enterworld.StaticCharacterSource{testDivision: {c, &bystanderA, &bystanderB}}
		return bystanderA.Name, bystanderB.Name
	}

	rt, c, skill, monsters := boomingFixture(t, []float64{3, 30}, true)
	nameA, nameB := addBystanders(rt, c)
	doored := doorNames(rt)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if open, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || open.Payload[0] != 1 {
		t.Fatalf("burst refused: %+v", out)
	}
	if after, _ := rt.Monsters.Get(testDivision, monsters[0].Gid); after.CurrentHP == monsters[0].CurrentHP {
		t.Fatal("the burst did not land")
	}
	for _, name := range *doored {
		if name == nameA || name == nameB {
			t.Fatalf("a burst that cannot kill built the reward roster (door held %v)", *doored)
		}
	}
	if got := rt.surpriseFatalCount.Load(); got != 0 {
		t.Fatalf("a plain burst counted %d surprise fatals", got)
	}

	rt2, c2, skill2, slain := boomingFixtureHP(t, 1, []float64{3}, true)
	nameA2, nameB2 := addBystanders(rt2, c2)
	doored2 := doorNames(rt2)
	out2 := rt2.HandleTargetInteract(testDivision, c2, wire.SkillAction{ActionId: skill2.ID}.Encode())
	if open, ok := findFrame(out2.Frames, wire.OpSkillCastResult); !ok || open.Payload[0] != 1 {
		t.Fatalf("killing burst refused: %+v", out2)
	}
	if after, found := rt2.Monsters.Get(testDivision, slain[0].Gid); found && after.CurrentHP != 0 {
		t.Fatal("the killing burst left its victim alive")
	}
	held := map[string]bool{}
	for _, name := range *doored2 {
		held[name] = true
	}
	if !held[nameA2] || !held[nameB2] {
		t.Fatalf("a killing burst never built the reward roster (door held %v)", *doored2)
	}
	if got := rt2.surpriseFatalCount.Load(); got != 0 {
		t.Fatalf("a killing burst counted %d surprise fatals", got)
	}
}
