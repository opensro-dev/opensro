/*
===========================================================================

skilladmit_test.go - tests for skilladmit.go

===========================================================================
*/

package action

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedDanceRefusedUntilSelector
================
*/
func TestShippedDanceRefusedUntilSelector(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_DANCEA_WARRIOR_A_01")
	if !skill.Reqc.Dance || skill.Reqi.Pairs[0] != (enterworld.SkillReqiPair{Kind: 6, Value: 14}) {
		t.Fatalf("dance reqc %+v reqi %+v", skill.Reqc, skill.Reqi)
	}
	if !skill.Aura.Present || !skill.BuffModifiers.Dru || skill.Aura.Radius != 700 ||
		skill.Aura.Select != 5 || skill.BuffModifiers.DruWords != [2]uint32{10, 0} {
		t.Fatalf("party buff %+v", skill.Aura)
	}
	guard := shippedOffense(t, "SKILL_EU_BARD_BATTLAA_GUARD_A_01")
	if guard.SelectorMask&1 == 0 {
		t.Fatalf("scls %d", guard.SelectorMask)
	}
	for _, dancing := range []bool{false, true} {
		rt, clock, c, target := newCombatTestRuntime(t, 100000)
		src := rt.deps.SkillData().(staticSkillSource)
		src[skill.ID] = skill
		src[guard.ID] = guard
		c.Skills = append(c.Skills, skill.ID)
		weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
		weapon.TypeIDs[3] = 14
		c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
		mp := int64(50000)
		c.CurrentMP = &mp
		if dancing {
			if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: guard.ID, SkillGroup: guard.Group}) {
				t.Fatal("guard effect refused")
			}
		}
		deps := rt.deps.(*enterworld.Deps)
		member := func(id int64, name string, dx float64) *enterworld.Character {
			m := *c
			m.ID, m.Name = id, name
			m.CurrentHP, m.CurrentMP = testInt64(100), testInt64(10000)
			m.Skills = append([]uint32(nil), skill.ID)
			world := *c.World
			spawn := *world.Spawn
			x := *spawn.X + dx
			spawn.X = &x
			world.Spawn = &spawn
			m.World = &world
			deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(deps.Characters.(enterworld.StaticCharacterSource)[testDivision], &m)
			return &m
		}
		mate := member(4, "mate", 800)
		far := member(5, "far", 800)
		casterGID := enterworld.ObjectIDForCharacter(c)
		rt.RewardParties = func(string) []RewardParty {
			return []RewardParty{{Members: []uint32{casterGID, enterworld.ObjectIDForCharacter(mate), enterworld.ObjectIDForCharacter(far)}}}
		}
		r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
		if !dancing {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, 0x32}) {
				t.Fatalf("no selector %+v", r)
			}
			continue
		}
		if r.DiagnosticRefusal != "" {
			t.Fatalf("dance cast %+v", r)
		}
		param := func(name string) float32 {
			who := c
			if name == "mate" {
				who = mate
			} else if name == "far" {
				who = far
			}
			stats, _, err := rt.playerCombatStats(testDivision, who)
			if err != nil {
				t.Fatal(err)
			}
			v, ok := stats.Param(0x80)
			if !ok {
				t.Fatalf("%s param 80 absent", name)
			}
			return v
		}
		place := func(who *enterworld.Character, x float64) {
			rt.Worlds.Update(simulation.WorldKey(testDivision, who.Name),
				func() simulation.WorldState { return simulation.SeedWorldState(who) },
				func(w *simulation.WorldState) { w.Spawn.X = x; w.SpawnSet = true })
		}
		if param("caster") != 10 || param("mate") != 0 {
			t.Fatalf("cast installed targets caster %v mate %v", param("caster"), param("mate"))
		}
		rt.TickHook()(clock.NowMs())
		if param("mate") != 0 || param("far") != 0 {
			t.Fatalf("out of range joined mate %v far %v", param("mate"), param("far"))
		}
		origin := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
		place(mate, origin.X)
		rt.TickHook()(clock.NowMs())
		if param("mate") != 10 {
			t.Fatalf("walk in left dru %v", param("mate"))
		}
		// 584D95: a member two sectors away fails 430CE0, which is the keep
		// path, so distance is never read and the child stays.
		rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(mate) },
			func(w *simulation.WorldState) { w.Spawn.RegionID = origin.RegionID + 2 })
		rt.TickHook()(clock.NowMs())
		if param("mate") != 10 {
			t.Fatalf("another sector dropped the member %v", param("mate"))
		}
		rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(mate) },
			func(w *simulation.WorldState) { w.Spawn.RegionID = origin.RegionID })
		place(mate, origin.X+800)
		rt.TickHook()(clock.NowMs())
		if param("mate") != 0 {
			t.Fatalf("walk out left dru %v", param("mate"))
		}
		place(mate, origin.X)
		rt.TickHook()(clock.NowMs())
		dry := int64(10)
		c.CurrentMP = &dry
		clock.Advance(5000 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
		if param("mate") != 0 || param("caster") != 0 {
			t.Fatalf("dry pulse left caster %v mate %v", param("caster"), param("mate"))
		}
	}
}

/*
================
TestShippedGuardAuraAppliesOdar
================
*/
func TestShippedGuardAuraAppliesOdar(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_BATTLAA_GUARD_A_01")
	if !skill.Aura.Present || skill.BuffModifiers.Dru || !skill.BuffModifiers.Odar ||
		skill.BuffModifiers.OdarBits != 7 || skill.BuffModifiers.OdarWord != 20 || skill.Aura.Select != 5 {
		t.Fatalf("guard aura %+v", skill.Aura)
	}
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	src := rt.deps.SkillData().(staticSkillSource)
	src[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 14
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	mp := int64(50000)
	c.CurrentMP = &mp
	deps := rt.deps.(*enterworld.Deps)
	member := func(id int64, name string, dx float64) *enterworld.Character {
		m := *c
		m.ID, m.Name = id, name
		m.CurrentHP, m.CurrentMP = testInt64(100), testInt64(10000)
		world := *c.World
		spawn := *world.Spawn
		x := *spawn.X + dx
		spawn.X = &x
		world.Spawn = &spawn
		m.World = &world
		deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(deps.Characters.(enterworld.StaticCharacterSource)[testDivision], &m)
		return &m
	}
	mate := member(4, "mate", 800)
	casterGID := enterworld.ObjectIDForCharacter(c)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{casterGID, enterworld.ObjectIDForCharacter(mate)}}}
	}
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if r.DiagnosticRefusal != "" {
		t.Fatalf("guard cast %+v", r)
	}
	param := func(who *enterworld.Character) float32 {
		stats, _, err := rt.playerCombatStats(testDivision, who)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := stats.Param(0xAE)
		if !ok {
			t.Fatalf("%s param AE absent", who.Name)
		}
		return v
	}
	if param(c) == 0 || param(mate) != 0 {
		t.Fatalf("cast caster %v mate %v", param(c), param(mate))
	}
	origin := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(mate) },
		func(w *simulation.WorldState) { w.Spawn.X = origin.X; w.SpawnSet = true })
	rt.TickHook()(clock.NowMs())
	if param(mate) == 0 {
		t.Fatal("walk in left odar 0")
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(mate) },
		func(w *simulation.WorldState) { w.Spawn.X = origin.X + 800; w.SpawnSet = true })
	rt.TickHook()(clock.NowMs())
	if param(mate) != 0 {
		t.Fatalf("walk out left odar %v", param(mate))
	}
}

/*
================
TestShippedRecoveryAuraHealsLowestRatio
================
*/
func TestShippedRecoveryAuraHealsLowestRatio(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01")
	if !skill.Aura.Present || !skill.Aura.Eshp || skill.Aura.Select != 5 ||
		skill.Heal.HP != 445 || skill.Heal.HPPercent != 0 || skill.Aura.Radius != 250 {
		t.Fatalf("recovery aura %+v", skill.Aura)
	}
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	src := rt.deps.SkillData().(staticSkillSource)
	src[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 15
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	c.CurrentHP = testInt64(40)
	c.Level = testInt64(90)
	c.MaxLevel = testInt64(90)
	c.Intellect = testInt64(400)
	mp := int64(50000)
	c.CurrentMP = &mp
	deps := rt.deps.(*enterworld.Deps)
	member := func(id int64, name string) *enterworld.Character {
		m := *c
		m.ID, m.Name = id, name
		m.CurrentHP, m.CurrentMP = testInt64(1), testInt64(10000)
		world := *c.World
		spawn := *world.Spawn
		world.Spawn = &spawn
		m.World = &world
		deps.Characters.(enterworld.StaticCharacterSource)[testDivision] = append(deps.Characters.(enterworld.StaticCharacterSource)[testDivision], &m)
		return &m
	}
	mate := member(4, "mate")
	casterGID := enterworld.ObjectIDForCharacter(c)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{casterGID, enterworld.ObjectIDForCharacter(mate)}}}
	}
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] == 2 {
		t.Fatalf("recovery cast refusal %q payload %x cost %+v mp %d", r.DiagnosticRefusal, r.Frames[0].Payload, skill.Consumption, *c.CurrentMP)
	}
	origin := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	rt.Worlds.Update(simulation.WorldKey(testDivision, mate.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(mate) },
		func(w *simulation.WorldState) { w.Spawn.X = origin.X; w.SpawnSet = true })
	// The caster's slot-6 weapon adds mwhh's term (411080) to every heal.
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	ref := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	bits, _ := strconv.ParseUint(c.MissionInventory[0].VarianceBits, 10, 64)
	intellect, _ := stats.Param(2)
	lo, hi := combat.WeaponMagicalAttack(ref, bits, uint8(c.MissionInventory[0].Plus))
	gain := 445 + int64(combat.WeaponHealBonus(lo, hi, combat.AbsorptionRatio(stats.Level, intellect), skill.Heal.WeaponHPWord))
	if gain == 445 {
		t.Fatal("fixture weapon adds no heal term")
	}
	// First update: the set holds only the caster (select bit 0); the
	// heal (584E5B) runs before the join walk, so the caster is healed and
	// the mate joins after it.
	casterBefore, mateBefore := *c.CurrentHP, *mate.CurrentHP
	rt.TickHook()(clock.NowMs())
	if *c.CurrentHP != casterBefore+gain || *mate.CurrentHP != mateBefore {
		t.Fatalf("first update caster %d want %d, mate %d", *c.CurrentHP, casterBefore+gain, *mate.CurrentHP)
	}
	// Next scan: the mate at 1 HP is the lowest ratio.
	clock.Advance(time.Duration(skill.Abnormal.Pulse) * time.Millisecond)
	casterBefore = *c.CurrentHP
	rt.TickHook()(clock.NowMs())
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, mate)
	if *mate.CurrentHP != min(maxHP, mateBefore+gain) || *c.CurrentHP != casterBefore {
		t.Fatalf("lowest ratio mate %d want %d, caster %d", *mate.CurrentHP, min(maxHP, mateBefore+gain), *c.CurrentHP)
	}

	// 59FF80 retires every unprotected row at death, a party aura included:
	// the dead mate loses its child, the dead caster its aura and the set.
	auraOn := func(name string) bool {
		for _, effect := range rt.effects.Snapshot(testDivision, name) {
			if effect.SkillID == skill.ID && !effect.StopRequested {
				return true
			}
		}
		return false
	}
	if !auraOn(mate.Name) || !auraOn(c.Name) {
		t.Fatal("fixture: the aura is not on both")
	}
	rt.deps.Update(mate, "test-death", func() bool {
		mate.CurrentHP = testInt64(0)
		rt.settlePlayerDeathInDoor(testDivision, mate, clock.NowMs())
		return true
	})
	if auraOn(mate.Name) {
		t.Fatal("the dead mate kept Recovery Division")
	}
	rt.deps.Update(c, "test-death", func() bool {
		c.CurrentHP = testInt64(0)
		rt.settlePlayerDeathInDoor(testDivision, c, clock.NowMs())
		return true
	})
	clock.Advance(time.Duration(skill.Abnormal.Pulse) * time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if auraOn(c.Name) {
		t.Fatal("the dead caster kept Recovery Division")
	}
}

/*
================
TestDancePulseCutByBDMD
================
*/
func TestDancePulseCutByBDMD(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_BARD_DANCEA_WARRIOR_A_01")
	if !skill.Attack.Parameters.Has(enterworld.ParameterBardMPDecrease) || skill.Aura.PulseMP != 63 || skill.Aura.PulseMs != 5000 {
		t.Fatalf("pulse %+v mask %v", skill.Aura, skill.Attack.Parameters)
	}
	guard := shippedOffense(t, "SKILL_EU_BARD_BATTLAA_GUARD_A_01")
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	src := rt.deps.SkillData().(staticSkillSource)
	src[skill.ID] = skill
	src[guard.ID] = guard
	passive := enterworld.SkillRow{ID: 900001, Group: 900001, PassiveParameters: enterworld.SkillPassiveParameters{
		Pinned: true,
		Mask:   1 << enterworld.ParameterBardMPDecrease,
		Values: enterworld.SkillParameterValues{enterworld.ParameterBardMPDecrease: 20},
	}}
	src[passive.ID] = passive
	c.Skills = append(c.Skills, skill.ID, passive.ID)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 14
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	mp := int64(200)
	c.CurrentMP = &mp
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: guard.ID, SkillGroup: guard.Group}) {
		t.Fatal("guard effect refused")
	}
	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if r.DiagnosticRefusal != "" {
		t.Fatalf("cast %+v", r)
	}
	afterCast := *c.CurrentMP
	clock.Advance(5000 * time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if c.CurrentMP == nil || *c.CurrentMP != afterCast-50 {
		t.Fatalf("pulse mp %v after cast %d", c.CurrentMP, afterCast)
	}
}

/*
================
TestRootedCasterRefusesTeleport
================
*/
func TestRootedCasterRefusesTeleport(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); err != nil {
		t.Skip("shipped skilldata unavailable")
	}
	source := enterworld.NewTextdataSkills(dir)
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	var skill enterworld.SkillRow
	for _, projection := range source.SpawnSkillRows() {
		row, ok := source.SkillByID(projection.ID)
		if ok && row.CastGate.Tele {
			skill = row
			break
		}
	}
	if !skill.CastGate.Tele {
		t.Fatal("no shipped tele row")
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	mp := int64(50000)
	c.CurrentMP = &mp
	key := simulation.WorldKey(testDivision, c.Name)
	from := rt.liveSpawn(key, c, clock.NowMs())
	cast := wire.SkillAction{ActionId: skill.ID, HasGroundTarget: true, Region: from.RegionID, GroundX: uint16(from.X), GroundY: uint16(from.Y), GroundZ: uint16(from.Z)}.Encode()
	open := rt.HandleTargetInteract(testDivision, c, cast)
	if len(open.Frames) > 0 && bytes.Equal(open.Frames[0].Payload, []byte{2, 0x09}) {
		t.Fatalf("unrooted tele refused 0x3009 %+v", open)
	}
	rt.storePlayerAbnormal(testDivision, c.Name, &abnormal.Block{Mask: abnormal.Root.Bit()})
	rooted := rt.HandleTargetInteract(testDivision, c, cast)
	if len(rooted.Frames) != 1 || !bytes.Equal(rooted.Frames[0].Payload, []byte{2, 0x09}) {
		t.Fatalf("rooted tele %+v gate %+v", rooted, skill.CastGate)
	}
}

/*
================
TestRootedWizardTeleportStillTravels

58E010 refuses only tele and tel3 while rooted. The Wizard's Teleport is
tel2, so a rooted Wizard can still blink out of a bind.
================
*/
func TestRootedWizardTeleportStillTravels(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_WIZARD_PSYCHICA_TELEPORT_A_01")
	if !skill.CastGate.Tel2 || skill.CastGate.Tele || !skill.PositionEffect.Pinned {
		t.Fatalf("Teleport shape: gate %+v position %+v", skill.CastGate, skill.PositionEffect)
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	mp := int64(skill.Consumption.MP)
	c.CurrentMP = &mp
	c.Intellect = testInt64(1000)
	key := simulation.WorldKey(testDivision, c.Name)
	from := rt.liveSpawn(key, c, clock.NowMs())
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.storePlayerAbnormal(testDivision, c.Name, &abnormal.Block{Mask: abnormal.Root.Bit()})
	cast := wire.SkillAction{ActionId: skill.ID, HasGroundTarget: true, Region: from.RegionID,
		GroundX: uint16(from.X + 500), GroundY: uint16(from.Y), GroundZ: uint16(from.Z)}.Encode()
	out := rt.HandleTargetInteract(testDivision, c, cast)
	to := rt.liveSpawn(key, c, clock.NowMs())
	if len(out.Frames) != 3 || to.X != from.X+float64(skill.PositionEffect.Range) {
		t.Fatalf("rooted Teleport refused or did not travel: %+v %+v -> %+v", out.Frames, from, to)
	}
}

/*
================
TestShippedReqnThroughCastEntry
================
*/
func TestShippedReqnThroughCastEntry(t *testing.T) {
	skill := shippedReqnArmourRow(t)
	if !skill.Reqi.All || skill.Reqi.Count < 2 {
		t.Fatalf("reqn %+v", skill.Reqi)
	}
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	mp := int64(50000)
	c.CurrentMP = &mp
	bare := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(bare.Frames) != 1 || !bytes.Equal(bare.Frames[0].Payload, []byte{2, 0x0d}) {
		t.Fatalf("bare %+v reqi %+v", bare, skill.Reqi)
	}
	k := newReqiKit()
	for slot := int64(0); slot <= 5; slot++ {
		k.equip(slot, armour(10), 10)
	}
	k.equip(6, weapon(15), 10)
	c.MissionInventory = k.c.MissionInventory
	items := rt.deps.ItemReferences().(staticItemSource)
	for code, ref := range k.items {
		items[code] = ref
	}
	got := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(got.Frames) > 0 && len(got.Frames[0].Payload) >= 2 && got.Frames[0].Payload[0] == 2 && got.Frames[0].Payload[1] == 0x0d {
		t.Fatalf("equipped still 0x300d %+v", got)
	}
	// reqn: armour alone is not enough. Without reqn the (10,0) pair would admit.
	k = newReqiKit()
	for slot := int64(0); slot <= 5; slot++ {
		k.equip(slot, armour(10), 10)
	}
	c.MissionInventory = k.c.MissionInventory
	for code, ref := range k.items {
		items[code] = ref
	}
	partial := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(partial.Frames) != 1 || !bytes.Equal(partial.Frames[0].Payload, []byte{2, 0x0d}) {
		t.Fatalf("armour without the weapon %+v", partial)
	}
}

/*
================
shippedReqnArmourRow
================
*/
func shippedReqnArmourRow(t *testing.T) enterworld.SkillRow {
	t.Helper()
	skills := shippedSkills(t)
	for id := uint32(1); id < 40000; id++ {
		row, ok := skills.SkillByID(id)
		if !ok || !row.Reqi.All || row.Reqi.Count < 2 {
			continue
		}
		if row.Reqi.Pairs[0] == (enterworld.SkillReqiPair{Kind: 10, Value: 0}) && row.Reqi.Pairs[1] == (enterworld.SkillReqiPair{Kind: 6, Value: 15}) {
			return row
		}
	}
	t.Fatal("no shipped (10,0 6,15)+reqn row")
	return enterworld.SkillRow{}
}

/*
==================
TestSkillEquipmentWeaponKinds

58D6AA: without reqi the primary weapon's TID4 must be one of the two
kinds, bare hand counting as 1; a broken weapon is 0x300F whatever the
kind; TID4 16 needs a siege fortress this port never has.
==================
*/
func TestSkillEquipmentWeaponKinds(t *testing.T) {
	skill := enterworld.SkillRow{RequiredWeaponKinds: [2]uint8{7, 0xff}}
	for _, tc := range []struct {
		name  string
		tid4  int64
		dur   int64
		equip bool
		kinds [2]uint8
		want  uint16
	}{
		{"match", 7, 10, true, [2]uint8{7, 0xff}, 0},
		{"second kind", 7, 10, true, [2]uint8{3, 7}, 0},
		{"mismatch", 8, 10, true, [2]uint8{7, 0xff}, 0x300d},
		{"bare hand is kind 1", 0, 0, false, [2]uint8{1, 0xff}, 0},
		{"bare hand mismatch", 0, 0, false, [2]uint8{7, 0xff}, 0x300d},
		{"any", 8, 10, true, [2]uint8{0xff, 0xff}, 0},
		// A broken weapon's TID reads 0, i.e. kind 1, and then fails 5D0.
		{"broken", 7, 0, true, [2]uint8{7, 0xff}, 0x300f},
		{"fortress", 16, 10, true, [2]uint8{16, 0xff}, 0x3047},
	} {
		k := newReqiKit()
		if tc.equip {
			k.equip(6, weapon(tc.tid4), tc.dur)
		}
		skill.RequiredWeaponKinds = tc.kinds
		if got := skillEquipmentRefusal(k.c, k.items, skill); got != tc.want {
			t.Errorf("%s: %#x want %#x", tc.name, got, tc.want)
		}
	}
}

// 58D8F0 order: a cooling skill with the wrong weapon reports 0x3005, not
// 0x300D; command admission (0x37) carries no target check.
/*
================
TestSkillAdmissionOrderAndCommandMask
================
*/
func TestSkillAdmissionOrderAndCommandMask(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_SWORD_DOWNATTACK_A_01")
	now := clock.NowMs()
	registerOffensiveCooldown(c, skill, now)
	c.MissionInventory = nil
	if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitExecution); got != 0x3005 {
		t.Fatalf("cooldown first %#x", got)
	}
	if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitExecution&^admitCooldown); got != 0x300d {
		t.Fatalf("equipment second %#x", got)
	}
	c.OffensiveSkillCooldowns = nil
	c.SharedSkillCooldowns = nil
	skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	standing := &admitTarget{motion: 0}
	if got := rt.skillAdmission(testDivision, c, skill, now, standing, nil, admitCommand); got != 0 {
		t.Fatalf("command mask ran the knockdown check %#x", got)
	}
	if got := rt.skillAdmission(testDivision, c, skill, now, standing, nil, admitExecution); got != 0x3006 {
		t.Fatalf("execution mask skipped the knockdown check %#x", got)
	}
}

// noteParameterIndex is the only reqi writer. The offense loop used to
// append the pairs a second time; an admitted shield row then carried two.
/*
================
TestShippedReqiPairsRecordedOnce
================
*/
func TestShippedReqiPairsRecordedOnce(t *testing.T) {
	shield := shippedOffense(t, "SKILL_EU_WARRIOR_ONEHANDA_SHIELD_A_01")
	if shield.OffenseRefusal != "" || shield.Reqi.Count != 1 {
		t.Fatalf("shield reqi %+v %s", shield.Reqi, shield.OffenseRefusal)
	}
	skills := shippedSkills(t)
	want := [3]enterworld.SkillReqiPair{{Kind: 6, Value: 7}, {Kind: 6, Value: 8}, {Kind: 6, Value: 9}}
	for id := uint32(1); id < 40000; id++ {
		row, ok := skills.SkillByID(id)
		if ok && row.Reqi.Count >= 2 && row.Reqi.Pairs[1] == want[1] {
			if row.Reqi.Count != 3 || [3]enterworld.SkillReqiPair(row.Reqi.Pairs[:3]) != want {
				t.Fatalf("%s reqi %+v", row.Codename, row.Reqi)
			}
			return
		}
	}
	t.Fatal("no shipped (6,7 6,8 6,9) row")
}

// 58DAEF: freeze, sleep or stun refuses 0x3009; nmf lets the skill through;
// burn is not in 0x4041.
/*
================
TestSkillAdmissionDisabledCaster
================
*/
func TestSkillAdmissionDisabledCaster(t *testing.T) {
	for _, tc := range []struct {
		status abnormal.Status
		nmf    bool
		want   uint16
	}{
		{abnormal.Stun, false, 0x3009},
		{abnormal.Freeze, false, 0x3009},
		{abnormal.Sleep, false, 0x3009},
		{abnormal.Stun, true, 0},
		{abnormal.Burn, false, 0},
	} {
		rt, clock, c, monster := newCombatTestRuntime(t, 100000)
		skill := shippedOffense(t, "SKILL_CH_SWORD_DOWNATTACK_A_01")
		skill.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
		skill.CastGate.Nmf = tc.nmf
		seedPlayerStatus(rt, c, tc.status, 100000, clock.NowMs(), monster.Gid)
		if got := rt.skillAdmission(testDivision, c, skill, clock.NowMs(), nil, nil, admitCommand); got != tc.want {
			t.Errorf("status %d nmf %v: %#x want %#x", tc.status, tc.nmf, got, tc.want)
		}
	}
}

/*
===============================================================================

CASTER-STATE GATES (58DB22..58DF20)

===============================================================================
*/

// 58DB22: only a buff whose row carries rpkt (CSkillManager+0x1F8) blocks,
// and it blocks only rows that carry rpkt themselves.
/*
================
TestSkillAdmissionRpktGate
================
*/
func TestSkillAdmissionRpktGate(t *testing.T) {
	if kit := shippedOffense(t, "SKILL_FORT_REPAIR_KIT_01"); !kit.CastGate.Rpkt {
		t.Fatal("shipped repair kit lost its rpkt tag")
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	now := clock.NowMs()
	anyKind := [2]uint8{0xff, 0xff}
	repair := enterworld.SkillRow{ID: 1001, Group: 100, RequiredWeaponKinds: anyKind, CastGate: enterworld.SkillCastGate{Rpkt: true}}
	plain := enterworld.SkillRow{ID: 1002, Group: 101, RequiredWeaponKinds: anyKind}
	rpktBuff := enterworld.SkillRow{ID: 1003, Group: 102, RequiredWeaponKinds: anyKind, CastGate: enterworld.SkillCastGate{Rpkt: true}}
	src := rt.deps.SkillData().(staticSkillSource)
	src[repair.ID], src[plain.ID], src[rpktBuff.ID] = repair, plain, rpktBuff
	install := func(row enterworld.SkillRow) {
		t.Helper()
		if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID, SkillGroup: row.Group}) {
			t.Fatalf("buff %d refused", row.ID)
		}
	}

	// Rejects "any installed buff blocks": an ordinary buff must not.
	install(plain)
	if got := rt.skillAdmission(testDivision, c, repair, now, nil, nil, admitExecution); got != 0 {
		t.Fatalf("ordinary buff blocked rpkt row: %#x", got)
	}
	install(rpktBuff)
	if got := rt.skillAdmission(testDivision, c, repair, now, nil, nil, admitExecution); got != 0x3009 {
		t.Fatalf("rpkt buff: %#x, want 0x3009", got)
	}
	// Rejects "the buff blocks every skill".
	if got := rt.skillAdmission(testDivision, c, plain, now, nil, nil, admitExecution); got != 0 {
		t.Fatalf("rpkt buff blocked a row without rpkt: %#x", got)
	}
}

// 58DB38: a monster strictly inside five times the efr kind-3 word refuses
// 0x3038; one outside it never does (the inverted rule refused far monsters).
/*
================
TestSkillAdmissionQestMonsterArm
================
*/
func TestSkillAdmissionQestMonsterArm(t *testing.T) {
	shipped := shippedOffense(t, "SKILL_QNO_EU_IVY_2_01_01")
	if !shipped.CastGate.Qest || !shipped.CastGate.Efr3Present || shipped.CastGate.Efr3Radius != 20 {
		t.Fatalf("shipped qest row %+v", shipped.CastGate)
	}
	rt, clock, c, monster := newCombatTestRuntime(t, 100000)
	now := clock.NowMs()
	mover, ok := rt.Monsters.Mover(testDivision, monster.Gid)
	if !ok {
		t.Fatal("monster mover missing")
	}
	pose := mover.LivePoseAt(now, nil)
	offset := 100.0
	if pose.X > 1800 {
		offset = -100
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X + offset, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})

	for _, tc := range []struct {
		radius uint32
		want   uint16
	}{
		{21, 0x3038}, // 105 > 100
		{20, 0},      // 100 is not nearer than 100
		{1, 0},       // a monster far outside the area
	} {
		skill := enterworld.SkillRow{ID: 2001, RequiredWeaponKinds: [2]uint8{0xff, 0xff},
			CastGate: enterworld.SkillCastGate{Qest: true, Efr3Present: true, Efr3Radius: tc.radius}}
		if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitCommand); got != tc.want {
			t.Errorf("radius %d: %#x, want %#x", tc.radius, got, tc.want)
		}
	}
}

// 58DE1E: berserk refuses both words first; word 1 needs the refobj a
// transform scroll puts in the command (never a target's level); word 2
// refuses a rider or a job suit.
/*
================
TestSkillAdmissionMschWords
================
*/
func TestSkillAdmissionMschWords(t *testing.T) {
	if row := shippedOffense(t, "SKILL_ETC_TRANS_MONSTER_01"); !row.CastGate.MschPresent || row.CastGate.MschMode != 1 {
		t.Fatalf("transform row %+v", row.CastGate)
	}
	if row := shippedOffense(t, "SKILL_EU_ROG_TRANSFORMA_DUPLE_A_01"); !row.CastGate.MschPresent || row.CastGate.MschMode != 2 {
		t.Fatalf("duple row %+v", row.CastGate)
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	now := clock.NowMs()
	msch := func(mode uint32) enterworld.SkillRow {
		return enterworld.SkillRow{ID: 3000 + mode, RequiredWeaponKinds: [2]uint8{0xff, 0xff},
			CastGate: enterworld.SkillCastGate{MschPresent: true, MschMode: mode}}
	}
	admit := func(mode uint32) uint16 {
		return rt.skillAdmission(testDivision, c, msch(mode), now, nil, nil, admitExecution)
	}

	c.NativeBodyStatus = 1
	if admit(1) != 0x3031 || admit(2) != 0x3031 {
		t.Fatalf("berserk: word 1 %#x, word 2 %#x", admit(1), admit(2))
	}
	c.NativeBodyStatus = 0

	// Rejects the target-level reading: even a monster target far below the
	// caster cannot stand in for the command's transform reference.
	low := &admitTarget{motion: 0}
	if got := rt.skillAdmission(testDivision, c, msch(1), now, low, nil, admitExecution); got != 0x3006 {
		t.Fatalf("word 1 without a command refobj: %#x, want 0x3006", got)
	}

	if got := admit(2); got != 0 {
		t.Fatalf("word 2 on foot: %#x", got)
	}
	c.ActiveCOS = &enterworld.CharacterCOS{Mounted: true}
	if got := admit(2); got != 0x3039 {
		t.Fatalf("word 2 mounted: %#x, want 0x3039", got)
	}
	c.ActiveCOS = nil

	items := rt.deps.ItemReferences().(staticItemSource)
	suit := &enterworld.ItemRef{Codename: "ITEM_TEST_TRADER_SUIT", TypeIDs: [4]int64{3, 1, 7, 1}}
	items[suit.Codename] = suit
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 8, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), Durability: 1})
	if got := admit(2); got != 0x3039 {
		t.Fatalf("word 2 in a job suit: %#x, want 0x3039", got)
	}
}

// CGItem_IsJobSuit: TID 3/1/7 with TID4 1..3, in slot 8 only.
/*
================
TestWearsJobSuit
================
*/
func TestWearsJobSuit(t *testing.T) {
	for _, tc := range []struct {
		slot int64
		tid  [4]int64
		want bool
	}{
		{8, [4]int64{3, 1, 7, 1}, true},
		{8, [4]int64{3, 1, 7, 3}, true},
		{8, [4]int64{3, 1, 7, 4}, false},
		{8, [4]int64{3, 1, 6, 1}, false},
		{7, [4]int64{3, 1, 7, 1}, false},
	} {
		k := newReqiKit()
		k.equip(tc.slot, tc.tid, 1)
		if got := wearsJobSuit(k.c, k.items); got != tc.want {
			t.Errorf("slot %d tid %v: %v, want %v", tc.slot, tc.tid, got, tc.want)
		}
	}
	if wearsJobSuit(newReqiKit().c, newReqiKit().items) {
		t.Error("empty slot 8 is a job suit")
	}
}

// 58DF20: berserk blocks every hide row but a trap; stealth and
// invisibility are refused in battle (state+0xD), never for being hidden
// already or for holding a hide buff.
/*
================
TestSkillAdmissionHideWords
================
*/
func TestSkillAdmissionHideWords(t *testing.T) {
	hiding := shippedOffense(t, "SKILL_EU_ROG_STEALTHA_HIDING_A_01")
	invisible := shippedOffense(t, "SKILL_EU_WIZARD_COLDA_INVISIBLE_A_01")
	trap := shippedOffense(t, "SKILL_EU_WIZARD_FIREA_TRAP_A_01")
	if hiding.CastGate.HideGateMode != 1 || invisible.CastGate.HideGateMode != 2 ||
		trap.CastGate.HideGateMode != 4 || !trap.CastGate.TrapPresent || hiding.CastGate.TrapPresent {
		t.Fatalf("shipped hide rows %+v %+v %+v", hiding.CastGate, invisible.CastGate, trap.CastGate)
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0
	now := clock.NowMs()
	gate := func(from enterworld.SkillRow) enterworld.SkillRow {
		return enterworld.SkillRow{ID: from.ID, Group: from.Group, RequiredWeaponKinds: [2]uint8{0xff, 0xff}, CastGate: from.CastGate}
	}
	admit := func(row enterworld.SkillRow) uint16 {
		return rt.skillAdmission(testDivision, c, gate(row), now, nil, nil, admitExecution)
	}

	c.NativeBodyStatus = 1
	if admit(hiding) != 0x3031 || admit(trap) != 0 {
		t.Fatalf("berserk: hiding %#x, trap %#x", admit(hiding), admit(trap))
	}

	// Rejects the "already hidden" reading: body 5 and a hide buff pass.
	c.NativeBodyStatus = 5
	rt.deps.SkillData().(staticSkillSource)[hiding.ID] = gate(hiding)
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: hiding.ID, SkillGroup: hiding.Group}) {
		t.Fatal("hide buff refused")
	}
	if got := admit(hiding); got != 0 {
		t.Fatalf("hidden in peace: %#x, want 0", got)
	}
	c.NativeBodyStatus = 0

	c.BattleUntilMs = now + 1
	if admit(hiding) != 0x3028 || admit(invisible) != 0x3028 || admit(trap) != 0 {
		t.Fatalf("in battle: hiding %#x, invisible %#x, trap %#x", admit(hiding), admit(invisible), admit(trap))
	}
	c.BattleUntilMs = now
	if got := admit(hiding); got != 0 {
		t.Fatalf("battle ended at now: %#x", got)
	}
}

/*
===============================================================================

LINE OF SIGHT (58E490)

===============================================================================
*/

// A blocked line refuses 0x3010 at execution only, after every other
// check; distance never does.
/*
================
TestSkillAdmissionLineOfSight
================
*/
func TestSkillAdmissionLineOfSight(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	now := clock.NowMs()
	skill := enterworld.SkillRow{ID: 5001, Group: 500, CoolTimeMs: 1000, RequiredWeaponKinds: [2]uint8{0xff, 0xff}, ActionRange: 150, ActionRangePinned: true}
	caster := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, now)
	far := caster
	far.X += 5000
	target := &admitTarget{at: far}

	if got := rt.skillAdmission(testDivision, c, skill, now, target, nil, admitExecution); got != 0 {
		t.Fatalf("distance alone refused: %#x", got)
	}

	var asked []simulation.Spawn
	blocked := true
	rt.LineOfSight = func(from simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) bool {
		asked = append(asked, from, to)
		return !blocked
	}
	if got := rt.skillAdmission(testDivision, c, skill, now, target, nil, admitExecution); got != 0x3010 {
		t.Fatalf("blocked line: %#x, want 0x3010", got)
	}
	if len(asked) != 2 || asked[0] != caster || asked[1] != far {
		t.Fatalf("line traced %+v, want caster -> target", asked)
	}
	asked = nil
	if got := rt.skillAdmission(testDivision, c, skill, now, target, nil, admitCommand); got != 0 || len(asked) != 0 {
		t.Fatalf("command mask traced a line: %#x %+v", got, asked)
	}
	if got := rt.skillAdmission(testDivision, c, skill, now, nil, nil, admitExecution); got != 0 || len(asked) != 0 {
		t.Fatalf("targetless skill traced a line: %#x %+v", got, asked)
	}
	registerOffensiveCooldown(c, skill, now)
	if got := rt.skillAdmission(testDivision, c, skill, now, target, nil, admitExecution); got != 0x3005 {
		t.Fatalf("cooldown must precede the line: %#x", got)
	}
	c.OffensiveSkillCooldowns = nil
	c.SharedSkillCooldowns = nil
	blocked = false
	if got := rt.skillAdmission(testDivision, c, skill, now, target, nil, admitExecution); got != 0 {
		t.Fatalf("clear line: %#x", got)
	}
}

// 58DF8C: reqc bit 2 admits at or below 30 % HP only; 58E0BF refuses ao/pw
// to a rider as well as a seated caster.
/*
================
TestSkillAdmissionLowHPAndFooting
================
*/
func TestSkillAdmissionLowHPAndFooting(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	now := clock.NowMs()
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	low := enterworld.SkillRow{ID: 7001, RequiredWeaponKinds: [2]uint8{0xff, 0xff}, Reqc: enterworld.SkillReqc{Present: true, LowHP: true}}
	at := int64(float64(maxHP) * float64(float32(0.3)))
	for _, tc := range []struct {
		hp   int64
		want uint16
	}{{maxHP, 0x3036}, {at + 1, 0x3036}, {at, 0}, {1, 0}} {
		c.CurrentHP = testInt64(tc.hp)
		if got := rt.skillAdmission(testDivision, c, low, now, nil, nil, admitCommand); got != tc.want {
			t.Errorf("hp %d of %d: %#x, want %#x", tc.hp, maxHP, got, tc.want)
		}
	}

	footing := enterworld.SkillRow{ID: 7002, RequiredWeaponKinds: [2]uint8{0xff, 0xff}, CastGate: enterworld.SkillCastGate{Ao: true}}
	if got := rt.skillAdmission(testDivision, c, footing, now, nil, nil, admitExecution); got != 0 {
		t.Fatalf("ao on foot: %#x", got)
	}
	c.ActiveCOS = &enterworld.CharacterCOS{Mounted: true}
	if got := rt.skillAdmission(testDivision, c, footing, now, nil, nil, admitExecution); got != 0x3009 {
		t.Fatalf("ao while riding: %#x, want 0x3009", got)
	}
}
