/*
===========================================================================

skillrecovery_test.go - tests for skillrecovery.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
TestSelfRecoveryCommitsHealingCostAndCompleteBracket
==================
*/
func TestSelfRecoveryCommitsHealingCostAndCompleteBracket(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	if !skill.Recovery.SelfFlatPinned || skill.Heal.HP != 89 {
		t.Fatalf("recovery not admitted: %+v", skill)
	}

	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentHP = testInt64(1)
	c.CurrentMP = testInt64(int64(skill.Consumption.MP))

	request := wire.SkillAction{ActionId: skill.ID}.Encode()

	result := rt.HandleTargetInteract(testDivision, c, request)
	result = assertAndSeparateActionSession(t, result)
	assertOpcodes(t, result.Frames, wire.OpSkillCastResult)
	if *c.CurrentHP != 1 || *c.CurrentMP != int64(skill.Consumption.MP) {
		t.Fatal("cast start applied healing or cost")
	}

	deadline := clock.NowMs() + int64(skill.ActionCastingTimeMs)
	cooldown := clock.NowMs() + int64(skill.CoolTimeMs)
	if c.OffensiveSkillCooldowns[skill.Group] != cooldown {
		t.Fatal("cooldown not registered at acceptance")
	}

	if early := rt.advanceProjectileCasts(deadline); len(early) != 0 || *c.CurrentHP != 1 {
		t.Fatal("release at equality", early)
	}

	released := rt.advanceProjectileCasts(deadline + 1)
	if c.OffensiveSkillCooldowns[skill.Group] != cooldown {
		t.Fatal("release restarted cooldown")
	}

	if len(released) != 2 ||
		len(released[0].Frames) != 1 ||
		released[0].Frames[0].Opcode != wire.OpSkillEffectControl ||
		released[1].OnlyCharacterID != c.ID ||
		len(released[1].Frames) != 1 ||
		released[1].Frames[0].Opcode != simulation.OpVitalsUpdate {
		t.Fatal("release routing", released)
	}

	if binary.LittleEndian.Uint16(released[1].Frames[0].Payload[4:]) != 0x40 {
		t.Fatal("recovery source flag lost")
	}

	if *c.CurrentHP != 90 || *c.CurrentMP != 0 {
		t.Fatalf("HP/MP=%d/%d", *c.CurrentHP, *c.CurrentMP)
	}

	if len(result.Frames[0].Payload) != 19 || result.Frames[0].Payload[18] != 0 {
		t.Fatal("self cast fabricated target results")
	}

	token := binary.LittleEndian.Uint32(result.Frames[0].Payload[10:])
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, request))
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Cancel: true}.Encode())
	if *c.CurrentHP != 90 {
		t.Fatal("busy cast recovered twice")
	}

	life, _ := skill.ActionLifecycleMs()
	assertSkillCastClose(
		t,
		rt.TickHook()(clock.At(time.Duration(life)*time.Millisecond).UnixMilli()),
		testDivision,
		token,
	)
	c.CurrentMP = testInt64(1000)
	clock.now = clock.At(time.Duration(skill.CoolTimeMs+skill.ActionCastingTimeMs+1) * time.Millisecond)
	c.CurrentHP = testInt64(enterworld.DerivedMaxHP(c) - 1)

	result = rt.HandleTargetInteract(testDivision, c, request)
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 1 {
		t.Fatal("second preparation", result)
	}

	rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	if *c.CurrentHP != enterworld.DerivedMaxHP(c) {
		t.Fatal("healing did not clamp at maximum")
	}
}

/*
==================
TestSelfRecoveryRejectsUnlearnedDeadTargetedAndUnaffordable
==================
*/
func TestSelfRecoveryRejectsUnlearnedDeadTargetedAndUnaffordable(t *testing.T) {
	for _, variant := range []string{"unlearned", "dead", "targeted", "ground", "mp"} {
		t.Run(variant, func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 100000)
			skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentHP = testInt64(10)
			c.CurrentMP = testInt64(1000)
			cast := wire.SkillAction{ActionId: skill.ID}

			switch variant {
			case "unlearned":
				c.Skills = c.Skills[:len(c.Skills)-1]

			case "dead":
				c.CurrentHP = testInt64(0)

			case "targeted":
				cast.HasTarget = true
				cast.TargetGid = target.Gid

			case "ground":
				cast.HasGroundTarget = true

			case "mp":
				c.CurrentMP = testInt64(0)
			}

			hp, mp := *c.CurrentHP, *c.CurrentMP

			result := rt.HandleTargetInteract(testDivision, c, cast.Encode())
			if *c.CurrentHP != hp ||
				*c.CurrentMP != mp ||
				len(result.Broadcast) > 0 ||
				rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("refusal changed authority or published action")
			}

			if len(result.Frames) > 0 && result.Frames[0].Payload[0] == 1 {
				t.Fatal("refusal returned success")
			}
		})
	}
}

/*
==================
TestRecoveryReleaseDoorRefusalCannotPublishHealingOrSpend
==================
*/
func TestRecoveryReleaseDoorRefusalCannotPublishHealingOrSpend(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentHP = testInt64(10)
	c.CurrentMP = testInt64(100)

	result := rt.HandleTargetInteract(
		testDivision,
		c,
		wire.SkillAction{ActionId: skill.ID}.Encode(),
	)
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 1 {
		t.Fatal("prepare failed", result)
	}

	rt.deps.(*enterworld.Deps).UpdateCharacter = func(*enterworld.Character, string, func() bool) bool {
		return false
	}

	frames := rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	if *c.CurrentHP != 10 ||
		*c.CurrentMP != 100 ||
		c.OffensiveSkillCooldowns[skill.Group] != clock.NowMs()+int64(skill.CoolTimeMs) ||
		len(frames) != 1 ||
		len(frames[0].Frames) != 1 ||
		frames[0].Frames[0].Opcode != wire.OpSkillEffectControl ||
		frames[0].Frames[0].Payload[0] != 2 ||
		rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("rejected authority transaction escaped into gameplay")
	}
}

/*
==================
TestSelfRecoveryPreparationCanBeInvalidated
==================
*/
func TestSelfRecoveryPreparationCanBeInvalidated(t *testing.T) {
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")

	for _, reason := range []string{"death", "mp", "unlearned", "cancel", "disconnect"} {
		t.Run(reason, func(t *testing.T) {
			rt, clock, c, _ := newCombatTestRuntime(t, 100000)
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentHP = testInt64(10)
			c.CurrentMP = testInt64(100)

			start := rt.HandleTargetInteract(
				testDivision,
				c,
				wire.SkillAction{ActionId: skill.ID}.Encode(),
			)
			start = assertAndSeparateActionSession(t, start)
			if len(start.Frames) != 1 {
				t.Fatal("start", start)
			}

			switch reason {
			case "death":
				c.CurrentHP = testInt64(0)

			case "mp":
				c.CurrentMP = testInt64(0)

			case "unlearned":
				c.Skills = c.Skills[:len(c.Skills)-1]

			case "cancel":
				if len(rt.cancelPreparingProjectile(testDivision, c.Name)) != 1 {
					t.Fatal("cancel")
				}

			case "disconnect":
				rt.ForgetCharacter(testDivision, c.Name)
			}

			hp, mp := *c.CurrentHP, *c.CurrentMP
			now := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
			frames := rt.advanceProjectileCasts(now)
			for _, batch := range frames {
				for _, f := range batch.Frames {
					if f.Opcode == simulation.OpVitalsUpdate {
						t.Fatal("invalid cast published healing")
					}
				}
			}

			if *c.CurrentHP != hp ||
				*c.CurrentMP != mp ||
				c.OffensiveSkillCooldowns[skill.Group] != clock.NowMs()+int64(skill.CoolTimeMs) ||
				rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("invalidated cast changed authority")
			}

			if len(rt.advanceProjectileCasts(now+1)) != 0 {
				t.Fatal("cast released twice")
			}
		})
	}
}

/*
==================
TestZeroCastingRecoveryRemainsImmediate
==================
*/
func TestZeroCastingRecoveryRemainsImmediate(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	skill.ActionCastingTimeMs = 0
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentHP = testInt64(1)
	c.CurrentMP = testInt64(int64(skill.Consumption.MP))

	result := rt.HandleTargetInteract(
		testDivision,
		c,
		wire.SkillAction{ActionId: skill.ID}.Encode(),
	)
	result = assertAndSeparateActionSession(t, result)
	assertOpcodes(t, result.Frames, wire.OpSkillCastResult, simulation.OpVitalsUpdate)
	if *c.CurrentHP != 90 || *c.CurrentMP != 0 || len(rt.pendingProjectileCasts) != 0 {
		t.Fatal("zero cast acquired a preparation wait")
	}
}

/*
==================
TestRecoveryPreparationDoorRefusalPublishesNothing
==================
*/
func TestRecoveryPreparationDoorRefusalPublishesNothing(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentHP = testInt64(10)
	c.CurrentMP = testInt64(100)

	rt.deps.(*enterworld.Deps).UpdateCharacter = func(*enterworld.Character, string, func() bool) bool {
		return false
	}

	result := rt.HandleTargetInteract(
		testDivision,
		c,
		wire.SkillAction{ActionId: skill.ID}.Encode(),
	)
	if len(result.Frames) != 0 ||
		len(result.Broadcast) != 0 ||
		rt.castTokenCounter != 0 ||
		rt.hasOpenSkillCast(testDivision, c.Name) ||
		len(c.OffensiveSkillCooldowns) != 0 ||
		*c.CurrentMP != 100 ||
		*c.CurrentHP != 10 {
		t.Fatal("failed preparation escaped authority door", result)
	}
}

/*
==================
TestShippedTargetHealReachesPartyMember
==================
*/
func TestShippedTargetHealReachesPartyMember(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	if !skill.Heal.Present ||
		!skill.Heal.WeaponHP ||
		skill.Heal.HP != 73 ||
		skill.Heal.WeaponHPWord != 150 ||
		!skill.Targets.Self ||
		!skill.Targets.Ally ||
		!skill.Targets.Party ||
		skill.Targets.Building ||
		skill.ActionRange != 150 {
		t.Fatalf("target heal %+v targets %+v range %v", skill.Heal, skill.Targets, skill.ActionRange)
	}

	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	src := rt.deps.SkillData().(staticSkillSource)
	src[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)

	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 15
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	c.Level = testInt64(1)
	c.CurrentHP = testInt64(40)
	c.CurrentMP = testInt64(50000)

	deps := rt.deps.(*enterworld.Deps)
	m := *c
	m.ID, m.Name = 4, "mate"
	m.CurrentHP, m.CurrentMP = testInt64(1), testInt64(50000)

	world := *c.World
	spawn := *world.Spawn
	world.Spawn = &spawn
	m.World = &world
	deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(
		deps.Characters.(enterworld.StaticCharacterSource)[testDivision],
		&m,
	)

	mateGID := enterworld.ObjectIDForCharacter(&m)

	origin := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	rt.Worlds.Update(
		simulation.WorldKey(testDivision, m.Name),
		func() simulation.WorldState {
			return simulation.SeedWorldState(&m)
		},
		func(w *simulation.WorldState) {
			w.Spawn.X = origin.X
			w.SpawnSet = true
		},
	)

	r := rt.HandleTargetInteract(
		testDivision,
		c,
		wire.SkillAction{
			ActionId:  skill.ID,
			HasTarget: true,
			TargetGid: mateGID,
		}.Encode(),
	)
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] == 2 {
		t.Fatalf("target heal cast %q payload %x", r.DiagnosticRefusal, r.Frames[0].Payload)
	}

	if skill.ActionCastingTimeMs > 0 {
		rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	}

	if *m.CurrentHP != 98 || *c.CurrentHP != 40 {
		t.Fatalf("mate %d caster %d", *m.CurrentHP, *c.CurrentHP)
	}
}

/*
==================
supportPair

A cleric caster (staff kind 15) and a party mate standing on the caster.
==================
*/
type supportPair struct {
	rt    *Runtime
	clock *fakeClock
	c, m  *enterworld.Character
}

/*
================
newSupportPair
================
*/
func newSupportPair(t *testing.T, skills ...enterworld.SkillRow) supportPair {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	src := rt.deps.SkillData().(staticSkillSource)
	for _, skill := range skills {
		src[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
	}

	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 15
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	c.CurrentHP = testInt64(40)
	c.CurrentMP = testInt64(50000)

	m := *c
	m.ID, m.Name = 4, "mate"
	m.CurrentHP, m.CurrentMP = testInt64(1), testInt64(50000)
	world := *c.World
	spawn := *world.Spawn
	world.Spawn = &spawn
	m.World = &world
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(
		deps.Characters.(enterworld.StaticCharacterSource)[testDivision],
		&m,
	)

	casterGID, mateGID := enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(&m)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{casterGID, mateGID}}}
	}

	pair := supportPair{rt: rt, clock: clock, c: c, m: &m}
	pair.placeMate(0)
	return pair
}

// placeMate stands the mate dx along X from the caster.
/*
================
placeMate
================
*/
func (p supportPair) placeMate(dx float64) {
	origin := p.rt.liveSpawn(simulation.WorldKey(testDivision, p.c.Name), p.c, p.clock.NowMs())
	p.rt.Worlds.Update(
		simulation.WorldKey(testDivision, p.m.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(p.m) },
		func(w *simulation.WorldState) {
			w.Spawn.X = origin.X + dx
			w.SpawnSet = true
		},
	)
}

/*
================
cast
================
*/
func (p supportPair) cast(skillID uint32) OpResult {
	return p.rt.HandleTargetInteract(testDivision, p.c, wire.SkillAction{
		ActionId:  skillID,
		HasTarget: true,
		TargetGid: enterworld.ObjectIDForCharacter(p.m),
	}.Encode())
}

// finishCast releases a cast with a casting time.
/*
================
finishCast
================
*/
func (p supportPair) finishCast(skill enterworld.SkillRow) {
	if skill.ActionCastingTimeMs > 0 {
		p.rt.advanceProjectileCasts(p.clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	}
}

/*
==================
TestShippedTargetHealWalksIntoReach

58D8F0 never refuses a target for range: a far heal walks the caster to
the mate and the tick casts it on arrival. The building-only repair kit
still refuses a player.
==================
*/
func TestShippedTargetHealWalksIntoReach(t *testing.T) {
	heal := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	kit := shippedOffense(t, "SKILL_FORT_REPAIR_KIT_01")
	if !kit.Targets.Building || kit.Targets.Self || kit.Targets.Ally || kit.Targets.Animal {
		t.Fatalf("repair kit targets %+v", kit.Targets)
	}
	p := newSupportPair(t, heal, kit)
	p.placeMate(200)

	far := p.cast(heal.ID)
	if far.DiagnosticRefusal != "" || !hasOpcode(far.Frames, simulation.OpMovementAck) || *p.m.CurrentHP != 1 {
		t.Fatalf("far heal %q %+v hp %d", far.DiagnosticRefusal, far.Frames, *p.m.CurrentHP)
	}
	intents := p.rt.combatIntentSnapshot()
	if len(intents) != 1 || !intents[0].SupportCast || intents[0].SkillID != heal.ID {
		t.Fatalf("support intent %+v", intents)
	}

	// Rejects the walk-and-forget approach: arriving must cast.
	p.clock.Advance(30 * time.Second)
	p.rt.TickHook()(p.clock.NowMs())
	p.finishCast(heal)
	if *p.m.CurrentHP <= 1 {
		t.Fatalf("arrival did not heal: hp %d", *p.m.CurrentHP)
	}
	if len(p.rt.combatIntentSnapshot()) != 0 {
		t.Fatal("support intent outlived its cast")
	}

	p.clock.Advance(30 * time.Second)
	p.rt.TickHook()(p.clock.NowMs())
	p.placeMate(0)
	*p.m.CurrentHP = 1
	p.rt.RewardParties = nil // a building row admits only a party mate
	building := p.cast(kit.ID)
	if len(building.Frames) == 0 || building.Frames[0].Payload[0] != 2 ||
		building.Frames[0].Payload[1] != 0x06 || *p.m.CurrentHP != 1 {
		t.Fatalf("building heal %+v hp %d", building.Frames, *p.m.CurrentHP)
	}
}

// A new command supersedes the walk: the heal never lands.
/*
================
TestSupportIntentSupersededByCommand
================
*/
func TestSupportIntentSupersededByCommand(t *testing.T) {
	heal := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	p := newSupportPair(t, heal)
	p.placeMate(200)
	if far := p.cast(heal.ID); !hasOpcode(far.Frames, simulation.OpMovementAck) {
		t.Fatalf("far heal %+v", far)
	}
	p.rt.ClearCombatIntent(testDivision, p.c.Name)
	p.clock.Advance(30 * time.Second)
	p.rt.TickHook()(p.clock.NowMs())
	p.finishCast(heal)
	if *p.m.CurrentHP != 1 {
		t.Fatalf("cancelled heal landed: hp %d", *p.m.CurrentHP)
	}
}

/*
==================
TestCastHealPercentArms

5A0850's percent arms, expected values read off the disassembly:
hp - ftol(unsigned(hp * hp%) / -100) raises the flat by its own percent;
nmh (+0x598) instead takes ftol(maxHP * hp% / 100); mp% always uses the
flat. SKILL_FORT_REPAIR_KIT_01 is the only shipped percent heal and it
targets buildings, so the arms are driven with a heal block directly.
==================
*/
func TestCastHealPercentArms(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, c)

	for _, tc := range []struct {
		heal   enterworld.SkillHeal
		hp, mp int64
	}{
		{
			enterworld.SkillHeal{
				HP:        200,
				HPPercent: 15,
				MP:        80,
				MPPercent: 50,
			},
			230,
			120,
		},
		{
			enterworld.SkillHeal{
				HP:        0,
				HPPercent: 1,
			},
			0,
			0,
		},
		{
			enterworld.SkillHeal{
				HP:        200,
				HPPercent: 15,
				OfMaxHP:   true,
			},
			maxHP * 15 / 100,
			0,
		},
	} {
		skill := enterworld.SkillRow{Heal: tc.heal}
		hp, mp, ok := rt.skillHealAmounts(testDivision, c, c, skill, healCast)
		if !ok || hp != tc.hp || mp != tc.mp {
			t.Errorf("heal %+v: %d/%d want %d/%d", tc.heal, hp, mp, tc.hp, tc.mp)
		}
	}
}

/*
===============================================================================

RESURRECTION

===============================================================================
*/

/*
==================
TestShippedResurrectionRows

resu is {level ceiling, EXP percent}; the special row also carries rmut.
==================
*/
func TestShippedResurrectionRows(t *testing.T) {
	target := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_TARGET_A_01")
	if r := target.Abnormal; !r.AdmitDeadParty || r.ResuMaxLevel != 30 || r.ResuExpPercent != 5 || r.Rmut != 0 {
		t.Fatalf("rebirth target resu %+v", r)
	}
	if h := target.Heal; !h.Present || h.HP != 152 || h.HPPercent != 0 || h.MP != 152 || h.MPPercent != 0 {
		t.Fatalf("rebirth target heal %+v", h)
	}
	special := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_SPECIAL_A_01")
	if r := special.Abnormal; !r.AdmitDeadParty || r.ResuMaxLevel != 60 || r.ResuExpPercent != 10 || r.Rmut != 10268 {
		t.Fatalf("rebirth special resu %+v", r)
	}
}

/*
==================
TestResurrectionAmounts

594780..594848 against hand-computed values: the percent product wraps at
32 bits, and the EXP is a truncated float share of |last loss|.
==================
*/
func TestResurrectionAmounts(t *testing.T) {
	for _, tc := range []struct {
		heal         enterworld.SkillHeal
		maxHP, maxMP int64
		hp, mp       int64
	}{
		{enterworld.SkillHeal{Present: true, HP: 152, MP: 152}, 5000, 3000, 152, 152},
		{enterworld.SkillHeal{Present: true, HPPercent: 100}, 1000, 800, 1000, 0},
		{enterworld.SkillHeal{Present: true, HP: 10, HPPercent: 50, MP: 20, MPPercent: 25}, 1001, 401, 510, 120},
		// 100 * 50,000,000 wraps to 705,032,704 before the division.
		{enterworld.SkillHeal{Present: true, HPPercent: 100}, 50_000_000, 0, 7_050_327, 0},
		{enterworld.SkillHeal{HP: 152, MP: 152}, 5000, 3000, 0, 0},
	} {
		hp, mp := resurrectionVitals(tc.heal, tc.maxHP, tc.maxMP)
		if hp != tc.hp || mp != tc.mp {
			t.Errorf("heal %+v of %d/%d: %d/%d, want %d/%d", tc.heal, tc.maxHP, tc.maxMP, hp, mp, tc.hp, tc.mp)
		}
	}
	for _, tc := range []struct {
		loss     int64
		percent  uint32
		murderer bool
		want     int64
	}{
		{2000, 5, false, 100},
		{-2000, 5, false, 100},
		{12345, 10, false, 1234},
		{0, 50, false, 0},
		{2000, 5, true, 50},
		{12345, 10, true, 617},
	} {
		if got := resurrectionExp(tc.loss, tc.percent, tc.murderer); got != tc.want {
			t.Errorf("exp of %d at %d%% (murderer %v): %d, want %d", tc.loss, tc.percent, tc.murderer, got, tc.want)
		}
	}
}

/*
==================
TestResurrectionProposalAndAnswer

The cast proposes; only a yes from a still-dead player revives. Rejects
the instant revive, the invented alive refusal and the 0x3008 code.
==================
*/
func TestResurrectionProposalAndAnswer(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_TARGET_A_01")
	p := newSupportPair(t, skill)
	p.m.Level = testInt64(1)
	p.m.LastExpLoss = 2000
	casterGID := enterworld.ObjectIDForCharacter(p.c)
	consent := p.rt.ResurrectionConsent()

	var granted int64
	p.rt.UpdateExperience = func(_ *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		granted = exp
		return []wire.Frame{{Opcode: wire.OpExpUpdate}}, true
	}
	var pushed, peers []wire.Frame
	p.rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		if name == p.m.Name {
			pushed = append(pushed, frames...)
		}
	}
	p.rt.PushDivisionPeerFrames = func(_, except string, frames []wire.Frame) {
		if except == p.m.Name {
			peers = append(peers, frames...)
		}
	}

	// cast runs one cast through its release; it returns the request result
	// and every frame addressed to the mate alone.
	cast := func() (OpResult, []wire.Frame) {
		p.clock.Advance(time.Minute)
		p.rt.TickHook()(p.clock.NowMs())
		*p.c.CurrentMP = 50000
		r := p.cast(skill.ID)
		var toMate []wire.Frame
		for _, to := range r.Recipients {
			if to.CharacterID == p.m.ID {
				toMate = append(toMate, to.Frames...)
			}
		}
		if skill.ActionCastingTimeMs > 0 {
			for _, batch := range p.rt.advanceProjectileCasts(p.clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1) {
				if batch.OnlyCharacterID != p.m.ID {
					continue
				}
				for _, f := range batch.Frames {
					toMate = append(toMate, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
				}
			}
		}
		return r, toMate
	}
	refusal := func(r OpResult) uint8 {
		if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 2 {
			return 0
		}
		return r.Frames[0].Payload[1]
	}

	// Above word 0: 0x3012 (58D0CE).
	*p.m.CurrentHP = 0
	p.m.Level = testInt64(31)
	if r, _ := cast(); refusal(r) != 0x12 {
		t.Fatalf("level ceiling %+v", r.Frames)
	}
	p.m.Level = testInt64(1)

	// A living target is admitted and proposed nothing.
	*p.m.CurrentHP = 100
	if r, toMate := cast(); r.DiagnosticRefusal != "" || refusal(r) != 0 || len(toMate) != 0 {
		t.Fatalf("living target %q %+v %+v", r.DiagnosticRefusal, r.Frames, toMate)
	}
	if *p.m.CurrentHP != 100 || consent.HasPendingInvite(testDivision, p.m.Name) {
		t.Fatal("living target was offered a revival")
	}

	// A dead target gets 0x3393 {4, caster} and stays dead.
	*p.m.CurrentHP = 0
	r, prompt := cast()
	if refusal(r) != 0 || len(prompt) != 1 || prompt[0].Opcode != 0x3393 || len(prompt[0].Payload) != 5 ||
		prompt[0].Payload[0] != 4 || binary.LittleEndian.Uint32(prompt[0].Payload[1:]) != casterGID {
		t.Fatalf("proposal %+v after %+v", prompt, r.Frames)
	}
	if enterworld.CharacterAlive(p.m) || !consent.HasPendingInvite(testDivision, p.m.Name) {
		t.Fatal("cast revived instead of proposing")
	}
	if again := p.rt.proposeResurrection(testDivision, p.c, p.m, skill, p.clock.NowMs()); len(again) != 0 {
		t.Fatal("second proposal while one waits")
	}

	// No: consumed, still dead.
	consent.ApplyConsent(nil, testDivision, p.m, 1, 2)
	if enterworld.CharacterAlive(p.m) || consent.HasPendingInvite(testDivision, p.m.Name) || len(pushed) != 0 {
		t.Fatal("a refused proposal changed something")
	}

	// Timeout: a late yes does nothing.
	if _, prompt := cast(); len(prompt) != 1 {
		t.Fatal("no proposal after a refusal")
	}
	// The offer is stamped at release and still stands exactly 30 s later;
	// one millisecond more expires it (46F1E0).
	p.clock.Advance(time.Duration(int64(skill.ActionCastingTimeMs)+1+resurrectionAnswerWindowMs) * time.Millisecond)
	if !consent.HasPendingInvite(testDivision, p.m.Name) {
		t.Fatal("offer expired at, not after, 30 s")
	}
	p.clock.Advance(time.Millisecond)
	consent.ApplyConsent(nil, testDivision, p.m, 1, 1)
	if enterworld.CharacterAlive(p.m) {
		t.Fatal("an expired proposal revived")
	}

	// Yes: 1 HP plus 152, MP plus 152, 5% of the 2000 lost, loss cleared.
	if r, prompt := cast(); len(prompt) != 1 {
		t.Fatalf("no proposal after a timeout: %q %+v", r.DiagnosticRefusal, r.Frames)
	}
	*p.m.CurrentMP = 0
	consent.ApplyConsent(nil, testDivision, p.m, 1, 1)
	if !enterworld.CharacterAlive(p.m) || *p.m.CurrentHP != 153 || *p.m.CurrentMP != 152 {
		t.Fatalf("revived hp %d mp %d", *p.m.CurrentHP, *p.m.CurrentMP)
	}
	if granted != 100 || p.m.LastExpLoss != 0 {
		t.Fatalf("exp granted %d, loss left %d", granted, p.m.LastExpLoss)
	}
	if len(pushed) < 3 || pushed[0].Opcode != wire.OpObjectSourceCorrection ||
		pushed[1].Opcode != simulation.OpVitalsUpdate || pushed[2].Opcode != wire.OpObjectStateRefresh ||
		!hasOpcode(pushed, wire.OpExpUpdate) {
		t.Fatalf("revived player frames %+v", pushed)
	}
	life, err := wire.DecodeObjectStateRefresh(pushed[2].Payload)
	if err != nil || life.StateType != wire.StateChannelLife || life.Value == wire.LifeStateDead {
		t.Fatalf("life frame %+v %v", life, err)
	}
	if len(peers) != 2 || peers[1].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("peer frames %+v", peers)
	}
	consent.ApplyConsent(nil, testDivision, p.m, 1, 1)
	if *p.m.CurrentHP != 153 {
		t.Fatal("a repeated answer applied twice")
	}
}

/*
==================
TestResurrectionWaitsBehindAnotherLane

TransactionMgr_InsertUnique: a pending party or guild prompt drops the
proposal.
==================
*/
func TestResurrectionWaitsBehindAnotherLane(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_TARGET_A_01")
	p := newSupportPair(t, skill)
	p.m.Level = testInt64(25)
	*p.m.CurrentHP = 0
	p.rt.ProposalPending = func(_, name string) bool { return name == p.m.Name }
	r := p.cast(skill.ID)
	p.finishCast(skill)
	for _, to := range r.Recipients {
		if to.CharacterID == p.m.ID {
			t.Fatalf("proposed over a pending prompt: %+v", to.Frames)
		}
	}
	if p.rt.ResurrectionConsent().HasPendingInvite(testDivision, p.m.Name) {
		t.Fatal("offer recorded over a pending prompt")
	}
}

/*
==================
TestFaithRaisesHealPercents

59425E: a heal reading getv HLRU adds the caster's HLRU (Faith's setv) to
both percent words; a heal without the getv is unchanged. The recipient's
own passives do not count.
==================
*/
func TestFaithRaisesHealPercents(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	table := rt.deps.SkillData().(staticSkillSource)
	var faith enterworld.SkillRow
	faith.ID, faith.Group, faith.Level = 90002, 90002, 1
	faith.PassiveParameters.Pinned = true
	faith.PassiveParameters.Mask = enterworld.SkillParameterMask(1) << enterworld.ParameterHealRecoveryUp
	faith.PassiveParameters.Values[enterworld.ParameterHealRecoveryUp] = 10
	table[faith.ID] = faith
	c.Skills = append(c.Skills, faith.ID)
	recipient := nearbyCharacter(rt, c, 21, "patient", 1)
	recipient.Skills = nil

	heal := enterworld.SkillHeal{HP: 200, HPPercent: 15, MP: 80, MPPercent: 50}
	for _, tc := range []struct {
		reads  bool
		hp, mp int64
	}{{false, 230, 120}, {true, 250, 128}} {
		skill := enterworld.SkillRow{Heal: heal}
		if tc.reads {
			skill.Attack.Parameters = enterworld.SkillParameterMask(1) << enterworld.ParameterHealRecoveryUp
		}
		hp, mp, ok := rt.skillHealAmounts(testDivision, recipient, c, skill, healCast)
		if !ok || hp != tc.hp || mp != tc.mp {
			t.Errorf("reads HLRU %v: %d/%d want %d/%d", tc.reads, hp, mp, tc.hp, tc.mp)
		}
	}
}
