/*
===========================================================================

fortress_structure_aura_test.go - the positions', headquarters' and
flags' auras at war

===========================================================================
*/

package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
auraFixture

A Jangan fortress world whose aggressive flag, headquarters and
healing flag carry their authored default skills, the shipped skill
table behind them, and the fixture character inside in guild 77, the
fortress's holder.
================
*/
func auraFixture(t *testing.T) (*Runtime, *enterworld.Character, *fakeClock) {
	t.Helper()
	skills := shippedSkills(t)
	skillID := func(codename string) uint32 {
		row, ok := skills.SkillByCodename(codename)
		if !ok {
			t.Fatalf("no shipped skill %s", codename)
		}
		return row.ID
	}
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			19561: {RefObjID: 19561, MaxHP: 900, ScaleDenom: 100, Structure: true, CombatPinned: true, TypeID4: structureKindGuardTower,
				DefaultSkillIDs: [10]uint32{skillID("SKILL_FORT_STRUCTURE_AGGRESSIVE_FLAG")}},
			19562: {RefObjID: 19562, MaxHP: 900, ScaleDenom: 100, Structure: true, CombatPinned: true, TypeID4: structureKindFortStone,
				DefaultSkillIDs: [10]uint32{skillID("SKILL_FORT_STRUCTURE_HEADQUARTERS")}},
			19563: {RefObjID: 19563, MaxHP: 900, ScaleDenom: 100, Structure: true, CombatPinned: true, TypeID4: structureKindGuardTower,
				DefaultSkillIDs: [10]uint32{skillID("SKILL_FORT_STRUCTURE_HEALING_FLAG")}},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19561, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 91},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19562, RegionID: 0x62aa, X: 900, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 92},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19563, RegionID: 0x62aa, X: 500, Y: 20, Z: 500}, PolicyPinned: true, MaxCount: 1, EventStructID: 93},
		},
	)
	rt, c, clock := fortressFixtureWithPopulation(t, testFieldFortGate, template)
	watermark := int64(20)
	c.MaxLevel = &watermark
	deps := rt.deps.(*enterworld.Deps)
	deps.Skills = skills
	deps.Roster = &enterworld.Roster{
		Format: "sro-server-character-authority", Version: 3,
		Models: []enterworld.RosterModel{{
			Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
		}},
	}
	guild := int64(77)
	c.GuildID = &guild
	enterFortress(t, rt, c)
	rt.Monsters.AdvancePopulation(clock.NowMs() + monster.NestHiveTickMs)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			rt.Fortresses.Occupy(testDivision, record.ID, 77)
		}
	}
	return rt, c, clock
}

/*
================
fellAuraStructure

A fatal batch on the standing structure that carries one skill,
without the defeat queue.
================
*/
func fellAuraStructure(t *testing.T, rt *Runtime, skillID uint32) {
	t.Helper()
	for _, row := range rt.Monsters.WorldStructures(testDivision, instance.Pack(2, 1)) {
		if row.CurrentHP == 0 {
			continue
		}
		for _, id := range row.Ref.DefaultSkillIDs {
			if id != skillID {
				continue
			}
			results, ok := rt.Monsters.ApplyDamageBatch(testDivision, []simulation.MonsterDamagePlan{{
				GID: row.Gid, ExpectedHP: row.CurrentHP, Damage: row.CurrentHP,
			}})
			if !ok || len(results) != 1 || !results[0].Fatal {
				t.Fatalf("structure %d did not fall", row.Ref.RefObjID)
			}
			return
		}
	}
	t.Fatalf("no standing structure carries skill %d", skillID)
}

func auraSkillID(t *testing.T, rt *Runtime, codename string) uint32 {
	t.Helper()
	row, ok := shippedSkills(t).SkillByCodename(codename)
	if !ok {
		t.Fatalf("no shipped skill %s", codename)
	}
	return row.ID
}

/*
================
seatAuraCompanion

Summons the resident's attack pet and stands it at a point of the
fortress world: the equipped COS record, its bound session and its
follower, seated as companionTargetsFixture seats them.
================
*/
func seatAuraCompanion(t *testing.T, rt *Runtime, owner *enterworld.Character, at simulation.Spawn) *enterworld.CharacterCOS {
	t.Helper()
	ref := equipCombatTestPet(t, rt, owner, 3)
	pet := owner.ActiveCOS
	pet.Satiety = 3000
	rt.BindPetSession(testDivision, owner, 1)
	state := rt.petSessionFor(testDivision, owner.Name, pet.GID)
	if state == nil {
		rt.petMu.Lock()
		if rt.petSessions == nil {
			rt.petSessions = map[petOwnerKey]*petSession{}
		}
		state = &petSession{character: owner, ready: true}
		rt.petSessions[petOwnerKey{division: testDivision, name: strings.ToLower(owner.Name), gid: pet.GID}] = state
		rt.petMu.Unlock()
	}
	state.mover.FollowFrom(pet.GID, at)
	state.refObjID = ref.RefObjID
	return pet
}

/*
================
TestFortressFlagAuraInstallsForTheHolderSide

With no war the flag is silent. In the war the holder standing in its
area holds the flag's instance and its attack words; an enemy in the
same area holds nothing, and the holder that steps out loses the
instance on the next scan.
================
*/
func TestFortressFlagAuraInstallsForTheHolderSide(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	flag := auraSkillID(t, rt, "SKILL_FORT_STRUCTURE_AGGRESSIVE_FLAG")
	enemy := seatAtTheStone(t, rt, c, 51, "AuraEnemy", 88)
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 160, Y: 20, Z: 100})
	placeResident(rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 170, Y: 20, Z: 100})

	rt.advanceFortressStructureAuras(now)
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); holds {
		t.Fatal("the flag aura holds outside the war")
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); !holds {
		t.Fatal("the holder holds no flag instance in the war")
	}
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if flat, _ := stats.Param(0x80); flat != 10 {
		t.Fatalf("attack parameter 0x80 = %v, want the flag's flat 10", flat)
	}
	if _, holds := rt.fieldInstance(testDivision, enemy.Name, flag); holds {
		t.Fatal("the enemy holds the holder's flag instance")
	}

	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 2000, Y: 20, Z: 100})
	rt.advanceFortressStructureAuras(now + int64(enterworld.SkillFieldScanMs))
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); holds {
		t.Fatal("the holder keeps the flag instance outside its area")
	}
}

/*
================
TestFortressFlagAuraInstallsForAHolderCompanion

At war the holder's pet standing in the flag's area holds the flag's
damage word beside its owner and its damage rates rise by the word;
the enemy's pet at the same spot holds nothing. The war's end retires
the pet's word with the owner's instance (#4490).
================
*/
func TestFortressFlagAuraInstallsForAHolderCompanion(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	flag := auraSkillID(t, rt, "SKILL_FORT_STRUCTURE_AGGRESSIVE_FLAG")
	pet := seatAuraCompanion(t, rt, c, simulation.Spawn{RegionID: 0x62aa, X: 170, Y: 20, Z: 100})
	enemy := seatAtTheStone(t, rt, c, 53, "AuraPetEnemy", 88)
	enemyPet := seatAuraCompanion(t, rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 180, Y: 20, Z: 100})
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 160, Y: 20, Z: 100})
	placeResident(rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 165, Y: 20, Z: 100})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	words := rt.cosWordsHeld(testDivision, c.Name, pet.GID, now)
	if len(words) != 1 || words[0].SkillID != flag || words[0].Tag != cosWordDamageTag {
		t.Fatalf("the holder's pet holds %+v, want the flag's damage word", words)
	}
	ref, valid := rt.cosReference(pet)
	if !valid {
		t.Fatal("the pet's catalogue row does not resolve")
	}
	stats, err := cosCombatStats(ref, pet, rt.cosAbnormal(testDivision, c.Name, pet.GID), words)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PhysicalBasicRate != 10 || stats.MagicalBasicRate != 10 {
		t.Fatalf("pet damage rates %v/%v, want the flag's flat 10", stats.PhysicalBasicRate, stats.MagicalBasicRate)
	}
	if held := rt.cosWordsHeld(testDivision, enemy.Name, enemyPet.GID, now); len(held) != 0 {
		t.Fatalf("the enemy's pet holds %+v", held)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
	rt.advanceFortressStructureAuras(now + 1000)
	if held := rt.cosWordsHeld(testDivision, c.Name, pet.GID, now); len(held) != 0 {
		t.Fatalf("the pet's word outlives the war: %+v", held)
	}
}

/*
================
TestFortressHeadquartersRecoveryWordForACompanion

The holder's pet by the headquarters holds its recovery-rate word,
and the rates the pet's recovery pulse reads are the authored 100/100.
================
*/
func TestFortressHeadquartersRecoveryWordForACompanion(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	hq := auraSkillID(t, rt, "SKILL_FORT_STRUCTURE_HEADQUARTERS")
	pet := seatAuraCompanion(t, rt, c, simulation.Spawn{RegionID: 0x62aa, X: 950, Y: 20, Z: 100})
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 960, Y: 20, Z: 100})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	words := rt.cosWordsHeld(testDivision, c.Name, pet.GID, now)
	if len(words) != 1 || words[0].SkillID != hq || words[0].Tag != cosWordRecoveryTag ||
		words[0].First != 100 || words[0].Second != 100 {
		t.Fatalf("the pet holds %+v, want the headquarters' recovery word", words)
	}
	if hp, mp := rt.cosBuffRecoveryRates(testDivision, c.Name, pet.GID, now); hp != 100 || mp != 100 {
		t.Fatalf("recovery rates %d/%d, want 100/100", hp, mp)
	}
}

/*
================
TestFortressHealingFlagPulsesACompanion

One period in, the holder's hurt pet in the healing flag's area
recovers the authored five percent of its maxima, and the enemy's pet
at the same spot recovers nothing.
================
*/
func TestFortressHealingFlagPulsesACompanion(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	pet := seatAuraCompanion(t, rt, c, simulation.Spawn{RegionID: 0x62aa, X: 560, Y: 20, Z: 500})
	enemy := seatAtTheStone(t, rt, c, 54, "HealPetEnemy", 88)
	enemyPet := seatAuraCompanion(t, rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 570, Y: 20, Z: 500})
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 555, Y: 20, Z: 500})
	placeResident(rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 565, Y: 20, Z: 500})
	ref, valid := rt.cosReference(pet)
	if !valid {
		t.Fatal("the pet's catalogue row does not resolve")
	}
	maxHP := int64(uint32(cosParameter(ref, pet, nil, abnormalMaxHPParam)))
	maxMP := int64(uint32(cosParameter(ref, pet, nil, abnormalMaxMPParam)))
	pet.CurrentHP, pet.CurrentMP = uint32(maxHP/2), uint32(maxMP/2)
	enemyPet.CurrentHP = uint32(maxHP / 2)
	hpBefore, mpBefore, enemyBefore := pet.CurrentHP, pet.CurrentMP, enemyPet.CurrentHP
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	if pet.CurrentHP != hpBefore {
		t.Fatal("the healing flag healed the pet on first sight")
	}
	rt.advanceFortressStructureAuras(now + 5000)
	if got, want := int64(pet.CurrentHP), int64(hpBefore)+maxHP*5/100; got != want {
		t.Fatalf("pet at %d hit points after the pulse, want %d", got, want)
	}
	if got, want := int64(pet.CurrentMP), int64(mpBefore)+maxMP*5/100; got != want {
		t.Fatalf("pet at %d mana after the pulse, want %d", got, want)
	}
	if enemyPet.CurrentHP != enemyBefore {
		t.Fatal("the healing flag healed the enemy's pet")
	}
}

/*
================
TestFortressAuraEndsWithTheWarAndTheStructure

The war's end retires every hold; a fresh war casts again, and the
flag's fall retires what it held.
================
*/
func TestFortressAuraEndsWithTheWarAndTheStructure(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	flag := auraSkillID(t, rt, "SKILL_FORT_STRUCTURE_AGGRESSIVE_FLAG")
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 160, Y: 20, Z: 100})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); !holds {
		t.Fatal("the holder holds no flag instance in the war")
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
	rt.advanceFortressStructureAuras(now + 1000)
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); holds {
		t.Fatal("the flag instance outlives the war")
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now + 2000)
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); !holds {
		t.Fatal("the holder holds no flag instance in the fresh war")
	}
	fellAuraStructure(t, rt, flag)
	rt.advanceFortressStructureAuras(now + 3000)
	if _, holds := rt.fieldInstance(testDivision, c.Name, flag); holds {
		t.Fatal("the flag instance outlives the flag")
	}
}

/*
================
TestFortressHeadquartersRecoveryRates

The headquarters' resident recovers at the authored rates: the
instance it holds writes both recovery parameters at 100.
================
*/
func TestFortressHeadquartersRecoveryRates(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	hq := auraSkillID(t, rt, "SKILL_FORT_STRUCTURE_HEADQUARTERS")
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 960, Y: 20, Z: 100})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	if _, holds := rt.fieldInstance(testDivision, c.Name, hq); !holds {
		t.Fatal("the resident holds no headquarters instance")
	}
	rates := map[uint16]bool{itemParamHPRecovery: false, itemParamMPRecovery: false}
	for _, w := range rt.effects.ModifierWrites(testDivision, c.Name) {
		if _, watched := rates[w.Parameter]; watched && w.Channel == paramkeeper.PercentSum && w.Value == 100 {
			rates[w.Parameter] = true
		}
	}
	for parameter, found := range rates {
		if !found {
			t.Fatalf("recovery parameter %d not written at 100: %+v", parameter, rt.effects.ModifierWrites(testDivision, c.Name))
		}
	}
}

/*
================
TestFortressHealingFlagPulses

The healing flag takes no pulse on first sight; one period in, the
holder-side resident in its area recovers the authored five percent
of maximum health and mana, and an enemy at the same spot recovers
nothing.
================
*/
func TestFortressHealingFlagPulses(t *testing.T) {
	rt, c, clock := auraFixture(t)
	now := clock.NowMs()
	enemy := seatAtTheStone(t, rt, c, 52, "HealEnemy", 88)
	placeResident(rt, c, simulation.Spawn{RegionID: 0x62aa, X: 560, Y: 20, Z: 500})
	placeResident(rt, enemy, simulation.Spawn{RegionID: 0x62aa, X: 570, Y: 20, Z: 500})
	maxHP, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	wound := func(ch *enterworld.Character) (int64, int64) {
		half, halfMP := maxHP/2, maxMP/2
		ch.CurrentHP, ch.CurrentMP = &half, &halfMP
		return half, halfMP
	}
	hpBefore, mpBefore := wound(c)
	wound(enemy)
	enemyHP := enterworld.CurrentHP(enemy)
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.advanceFortressStructureAuras(now)
	if enterworld.CurrentHP(c) != hpBefore {
		t.Fatal("the healing flag healed on first sight")
	}
	rt.advanceFortressStructureAuras(now + 5000)
	if got, want := enterworld.CurrentHP(c), hpBefore+maxHP*5/100; got != want {
		t.Fatalf("holder at %d hit points after the pulse, want %d", got, want)
	}
	if got, want := enterworld.CurrentMP(c), mpBefore+maxMP*5/100; got != want {
		t.Fatalf("holder at %d mana after the pulse, want %d", got, want)
	}
	if enterworld.CurrentHP(enemy) != enemyHP {
		t.Fatal("the healing flag healed the enemy")
	}
}
