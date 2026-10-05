/*
===========================================================================

statuscast_test.go - damage-free hostile status casts through the offense owner

The Wizard's Lightning Shock, Root and Mesh Root, the Warrior's Axis
Quiver and the Rogue's Poison Field prepare, release one zero-damage
record per victim and leave only the authored status and aggression.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// statusCastDualAxeKind is the dual axe TID4 Axis Quiver names in
	// columns 50/51 (9, 255).
	statusCastDualAxeKind = 9

	// statusCastCrossbowKind and statusCastDaggerKind are the primary weapon
	// TID4 values of Poison Field's reqi(6,12) and reqi(6,13) pairs.
	statusCastCrossbowKind = 12
	statusCastDaggerKind   = 13

	// statusCastStunTag and statusCastPoisonTag are the st and ps block tags
	// (abnormal sources 0x7374 and 0x7073) the two rows author.
	statusCastStunTag   = 0x7374
	statusCastPoisonTag = 0x7073

	// statusCastEquipmentRefusal is 58D480's 0x300D, of which the cast
	// result carries the low byte.
	statusCastEquipmentRefusal = 0x300d

	// statusCastPoisonPulseMs is 590680's poison period when the row
	// authors no puls.
	statusCastPoisonPulseMs = 2000
)

/*
================
statusCastAreaFixture

Five monsters: three side by side at the primary's spawn, one 1000 above
and one 1000 along X. The caster stands three units from the primary so a
melee reach needs no approach. Returns the targets in spawn order.
================
*/
func statusCastAreaFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, []monster.Instance) {
	t.Helper()
	rt, clock, c, primary := newCombatTestRuntime(t, 1000000)
	var nests []monster.NestRow
	for _, offset := range [][2]float64{{0, 0}, {1, 0}, {2, 0}, {3, 1000}, {1004, 0}} {
		spawn := primary.Spawn
		spawn.X += offset[0]
		spawn.Y += offset[1]
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	// NewRuntime wired the abnormal context into the replaced state only.
	rt.Monsters.SetAbnormalContext(monsterAbnormalContext{rt})
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: primary.Spawn.RegionID, X: primary.Spawn.X - 3, Y: primary.Spawn.Y, Z: primary.Spawn.Z}
			w.SpawnSet = true
		})
	return rt, clock, c, rt.Monsters.InstancesInRegions(testDivision, []uint16{primary.Spawn.RegionID})
}

/*
================
equipStatusCastSkill

Teach a European caster the row with a certain status roll and wield a
primary weapon of the given TID4.
================
*/
func equipStatusCastSkill(rt *Runtime, c *enterworld.Character, skill enterworld.SkillRow, tag uint32, weaponKind uint8) {
	index, _ := abnormal.SourceIndex(tag)
	skill.Abnormal.Params[index].Args[1] = 100
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	// Two casts' worth stays under the level-1 fixture's maximum MP, so the
	// debit is observed exactly rather than through a clamp.
	c.CurrentMP = testInt64(2 * int64(skill.Consumption.MP))
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(weaponKind)
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
}

/*
================
TestStatusCastAppliesStatusWithoutDamage

Force the roll so the status must land and survive the tick that released
it; the monster keeps its HP and records the caster with the authored
aggression and no damage credit.
================
*/
func TestStatusCastAppliesStatusWithoutDamage(t *testing.T) {
	cases := []struct {
		code   string
		tag    uint32
		status abnormal.Status
	}{
		{"SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_A_01", 0x6665, abnormal.Fear},
		{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_A_01", 0x7274, abnormal.Root},
		{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_B_01", 0x7274, abnormal.Root},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 1000000)
			skill := shippedOffense(t, tc.code)
			if !skill.StatusCast || skill.Attack.Present || skill.ActionCastingTimeMs == 0 {
				t.Fatalf("catalog shape: status=%v attack=%v cast=%d refusal=%q", skill.StatusCast, skill.Attack.Present, skill.ActionCastingTimeMs, skill.OffenseRefusal)
			}
			index, _ := abnormal.SourceIndex(tc.tag)
			skill.Abnormal.Params[index].Args[1] = 100
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.RaceIndex = testInt64(enterworld.RaceEurope)
			c.ModelCodename = "CHAR_EU_MAN_NOBLE"
			c.Skills = []uint32{skill.ID}
			c.Intellect = testInt64(2000)
			c.CurrentMP = testInt64(10000)
			// Equip the authored Wizard weapon (staff or wand).
			weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
			c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			rt.CombatRoll = func() (uint32, error) { return 10, nil }
			before, _ := rt.Monsters.Get(testDivision, target.Gid)
			mp := enterworld.CurrentMP(c)

			cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}
			start := rt.HandleTargetInteract(testDivision, c, cast.Encode())
			start = assertAndSeparateActionSession(t, start)
			if len(start.Frames) == 0 || len(rt.pendingProjectileCasts) != 1 {
				t.Fatalf("status cast was not prepared: %+v", start)
			}
			// The world tick samples its instant before the registry clock
			// admits the status, then updates abnormals at that same instant.
			tick := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
			clock.Advance(time.Duration(tick-clock.NowMs()+5) * time.Millisecond)
			released := rt.advanceProjectileCasts(tick)
			if len(released) == 0 {
				t.Fatal("prepared status cast never released")
			}
			rt.advanceMonsterAbnormals(tick)

			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP != before.CurrentHP {
				t.Fatalf("status cast dealt damage: HP %d -> %d", before.CurrentHP, after.CurrentHP)
			}
			if after.Abnormal == nil || !after.Abnormal.Slots[tc.status].Active {
				t.Fatalf("status %v did not land", tc.status)
			}
			if len(after.Opponents) == 0 || after.Opponents[0].GID != enterworld.ObjectIDForCharacter(c) ||
				after.Opponents[0].Damage != 0 || after.Opponents[0].Aggression < int32(skill.Threat.Flat) {
				t.Fatalf("aggression ledger %+v, authored %d", after.Opponents, skill.Threat.Flat)
			}
			if enterworld.CurrentMP(c) >= mp {
				t.Fatalf("MP not debited: %d -> %d", mp, enterworld.CurrentMP(c))
			}
		})
	}
}

/*
================
TestStatusCastCatalogShape

Lightning Impact centres on its caster; Mana Drain, which names only
players, stays refused; programs that also carry att remain attacks.
================
*/
func TestStatusCastCatalogShape(t *testing.T) {
	source := shippedSkillSource(t)
	impact, ok := source.SkillByCodename("SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_B_01")
	if !ok || !impact.StatusCast || impact.TargetRequired || impact.OffensiveArea.Shape != 1 || impact.OffensiveArea.Radius != 120 {
		t.Fatalf("caster-centred Lightning Impact: %+v %q", impact.OffensiveArea, impact.OffenseRefusal)
	}
	if drain, _ := source.SkillByCodename("SKILL_EU_WIZARD_COLDA_MANADRY_A_01"); drain.StatusCast {
		t.Fatal("Mana Drain (Enemy_P only) admitted against monsters")
	}
	bolt, ok := source.SkillByCodename("SKILL_EU_WIZARD_COLDA_POINT_A_01")
	if !ok || bolt.StatusCast || !bolt.Attack.Present || !bolt.DirectOffensePinned {
		t.Fatalf("ordinary attack reclassified: status=%v", bolt.StatusCast)
	}
	shock, _ := source.SkillByCodename("SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_A_01")
	if _, executable := enterworld.OffensiveSequence(source, shock.ID); !executable || shock.Threat.Flat != 155 || shock.Threat.Percent != 0 {
		t.Fatalf("Lightning Shock plan: executable=%v threat=%+v", executable, shock.Threat)
	}
	mesh, _ := source.SkillByCodename("SKILL_EU_WIZARD_EARTHA_ABNORMAL_B_01")
	if mesh.OffensiveArea.Shape != 2 || mesh.OffensiveArea.Radius != 50 || mesh.OffensiveArea.MaxTargets != 3 {
		t.Fatalf("Mesh Root area %+v", mesh.OffensiveArea)
	}
}

/*
================
TestLightningImpactFrightensMonstersAroundTheCaster

The untargeted cast prepares, then at release rolls Fear on the monster
beside the caster without damage, publishes its result and closes.
================
*/
func TestLightningImpactFrightensMonstersAroundTheCaster(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_B_01")
	index, _ := abnormal.SourceIndex(0x6665)
	skill.Abnormal.Params[index].Args[1] = 100
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	mover, _ := rt.Monsters.Mover(testDivision, target.Gid)
	pose := mover.LivePoseAt(clock.NowMs(), nil)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X + 30, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})
	before, _ := rt.Monsters.Get(testDivision, target.Gid)

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 || len(rt.pendingProjectileCasts) != 1 {
		t.Fatalf("Lightning Impact was not prepared: %+v", out)
	}
	release := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	batches := rt.advanceProjectileCasts(release)
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP != before.CurrentHP || after.Abnormal == nil || !after.Abnormal.Slots[abnormal.Fear].Active {
		t.Fatalf("Fear did not land without damage: HP %d -> %d", before.CurrentHP, after.CurrentHP)
	}
	results := false
	for _, batch := range batches {
		for _, f := range batch.Frames {
			results = results || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) > 6 && f.Payload[0] == 1
		}
	}
	if !results {
		t.Fatalf("release carried no area results: %+v", batches)
	}
	closed := false
	for _, batch := range rt.drainSkillFinalizes(release + int64(skill.ActionDurationMs)) {
		for _, f := range batch.Frames {
			closed = closed || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2
		}
	}
	if !closed {
		t.Fatal("Lightning Impact never closed its cast bracket")
	}
}

/*
================
TestAxisQuiverStunsAroundThePrimaryWithoutDamage

Axis Quiver 1 (efr 1,2,50,3,0,24 st tnt2 242,0) stuns the primary and the
two monsters beside it, deals no damage and records the flat tnt2 word as
each victim's aggression. The monsters 1000 away are untouched, and the
caster resumes the basic attack afterwards (ContinueBasicAttack).
================
*/
func TestAxisQuiverStunsAroundThePrimaryWithoutDamage(t *testing.T) {
	rt, clock, c, targets := statusCastAreaFixture(t)
	skill := shippedOffense(t, "SKILL_EU_WARRIOR_DUALA_STUN_A_01")
	if !skill.StatusCast || !skill.TargetRequired || skill.OffensiveArea.MaxTargets != 3 || skill.Threat.Flat != 242 {
		t.Fatalf("catalog shape: status=%v area=%+v threat=%+v refusal=%q", skill.StatusCast, skill.OffensiveArea, skill.Threat, skill.OffenseRefusal)
	}
	equipStatusCastSkill(rt, c, skill, statusCastStunTag, statusCastDualAxeKind)
	mp := enterworld.CurrentMP(c)

	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	start = assertAndSeparateActionSession(t, start)
	if len(start.Frames) == 0 || len(rt.pendingProjectileCasts) != 1 {
		t.Fatalf("Axis Quiver was not prepared: %+v", start)
	}
	// Column 19 = 1: the combat intent returns to the basic attack after
	// the action closes (4AEC9E..4AECB3).
	if intents := rt.combatIntentSnapshot(); len(intents) != 1 || !intents[0].ResumeBasic {
		t.Fatalf("Axis Quiver does not continue the basic attack: %+v", intents)
	}
	tick := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	clock.Advance(time.Duration(tick-clock.NowMs()+5) * time.Millisecond)
	if released := rt.advanceProjectileCasts(tick); len(released) == 0 {
		t.Fatal("prepared Axis Quiver never released")
	}
	rt.advanceMonsterAbnormals(tick)

	gid := enterworld.ObjectIDForCharacter(c)
	for i, target := range targets {
		after, _ := rt.Monsters.Get(testDivision, target.Gid)
		if after.CurrentHP != target.CurrentHP {
			t.Fatalf("monster %d took damage: HP %d -> %d", i, target.CurrentHP, after.CurrentHP)
		}
		stunned := after.Abnormal != nil && after.Abnormal.Slots[abnormal.Stun].Active
		if near := i < 3; stunned != near {
			t.Fatalf("monster %d stunned=%v, want %v", i, stunned, near)
		}
		if i >= 3 {
			continue
		}
		if len(after.Opponents) == 0 || after.Opponents[0].GID != gid || after.Opponents[0].Damage != 0 ||
			after.Opponents[0].Aggression != int32(skill.Threat.Flat) {
			t.Fatalf("monster %d aggression ledger %+v, authored %d", i, after.Opponents, skill.Threat.Flat)
		}
	}
	if enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) {
		t.Fatalf("MP %d -> %d, cost %d", mp, enterworld.CurrentMP(c), skill.Consumption.MP)
	}
}

/*
================
TestPoisonFieldPoisonsMonstersAroundTheCaster

Poison Field 1 prepares without a target and, with either of its reqi
weapons (crossbow or dagger), poisons the monsters within 60 of the
caster without damage at release and without aggression (no tant/tnt2).
================
*/
func TestPoisonFieldPoisonsMonstersAroundTheCaster(t *testing.T) {
	for _, kind := range []uint8{statusCastCrossbowKind, statusCastDaggerKind} {
		rt, clock, c, targets := statusCastAreaFixture(t)
		skill := shippedOffense(t, "SKILL_EU_ROG_POISONA_ROUND_A_01")
		if !skill.StatusCast || skill.TargetRequired || !skill.Reqi.Present || skill.Threat.Present {
			t.Fatalf("catalog shape: status=%v reqi=%+v refusal=%q", skill.StatusCast, skill.Reqi, skill.OffenseRefusal)
		}
		equipStatusCastSkill(rt, c, skill, statusCastPoisonTag, kind)
		mp := enterworld.CurrentMP(c)

		out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
		if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 || len(rt.pendingProjectileCasts) != 1 {
			t.Fatalf("weapon %d: Poison Field was not prepared: %+v", kind, out)
		}
		release := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
		if batches := rt.advanceProjectileCasts(release); len(batches) == 0 {
			t.Fatalf("weapon %d: prepared Poison Field never released", kind)
		}
		for i, target := range targets {
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP != target.CurrentHP {
				t.Fatalf("weapon %d: monster %d took damage at release: HP %d -> %d", kind, i, target.CurrentHP, after.CurrentHP)
			}
			poisoned := after.Abnormal != nil && after.Abnormal.Slots[abnormal.Poison].Active
			if near := i < 3; poisoned != near {
				t.Fatalf("weapon %d: monster %d poisoned=%v, want %v", kind, i, poisoned, near)
			}
			for _, opponent := range after.Opponents {
				if opponent.Aggression != 0 || opponent.Damage != 0 {
					t.Fatalf("weapon %d: monster %d ledger %+v", kind, i, after.Opponents)
				}
			}
		}
		if enterworld.CurrentMP(c) != mp-int64(skill.Consumption.MP) {
			t.Fatalf("weapon %d: MP %d -> %d, cost %d", kind, mp, enterworld.CurrentMP(c), skill.Consumption.MP)
		}
		// Without puls the poison ticks every 2000 ms (590680); the first
		// tick is the only damage the cast ever causes.
		tick := release + statusCastPoisonPulseMs + 1
		clock.Advance(time.Duration(tick-clock.NowMs()) * time.Millisecond)
		rt.advanceMonsterAbnormals(tick)
		for i, target := range targets[:3] {
			if after, _ := rt.Monsters.Get(testDivision, target.Gid); after.CurrentHP >= target.CurrentHP {
				t.Fatalf("weapon %d: monster %d poison never ticked: HP %d", kind, i, after.CurrentHP)
			}
		}
	}
}

/*
================
TestPoisonFieldRefusesAnotherWeapon

With neither reqi weapon (a dual axe here) 58D480 refuses the command
with 0x300D before anything is prepared or debited.
================
*/
func TestPoisonFieldRefusesAnotherWeapon(t *testing.T) {
	rt, _, c, targets := statusCastAreaFixture(t)
	skill := shippedOffense(t, "SKILL_EU_ROG_POISONA_ROUND_A_01")
	equipStatusCastSkill(rt, c, skill, statusCastPoisonTag, statusCastDualAxeKind)
	mp := enterworld.CurrentMP(c)

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || len(frame.Payload) != 2 || frame.Payload[0] != 2 || frame.Payload[1] != uint8(statusCastEquipmentRefusal&0xff) {
		t.Fatalf("wrong-weapon Poison Field result %+v", out)
	}
	if len(rt.pendingProjectileCasts) != 0 || enterworld.CurrentMP(c) != mp {
		t.Fatal("refused Poison Field was prepared or charged")
	}
	for _, target := range targets {
		if after, _ := rt.Monsters.Get(testDivision, target.Gid); after.Abnormal != nil && after.Abnormal.Slots[abnormal.Poison].Active {
			t.Fatal("refused Poison Field poisoned a monster")
		}
	}
}
