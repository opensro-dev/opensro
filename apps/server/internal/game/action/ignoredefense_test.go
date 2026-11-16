/*
===========================================================================

ignoredefense_test.go - action-layer tests for MATTR_AVATAR_MDIA rolls and imbues

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
playerStatsWithMDIA

Builds a player snapshot with a deterministic attack interval and an optional
0xBC ignore-defense modifier through PlayerStatsWithModifiers.
================
*/
func playerStatsWithMDIA(t *testing.T, rt *Runtime, c *enterworld.Character, mdia float32) combat.Stats {
	t.Helper()
	var writes []paramkeeper.Write
	if mdia != 0 {
		writes = []paramkeeper.Write{{Parameter: 0xbc, Channel: paramkeeper.Flat, Source: 2, Value: mdia}}
	}
	stats, _, err := combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), writes, nil)
	if err != nil {
		t.Fatalf("PlayerStatsWithModifiers: %v", err)
	}
	stats.PhysicalAttackMin = 100
	stats.PhysicalAttackMax = 100
	stats.MagicalAttackMin = 100
	stats.MagicalAttackMax = 100
	stats.CriticalRate = 0
	return stats
}

/*
================
TestResolveCombatRequestIgnoreDefenseHistoryAndStreamOrder

Verifies the 0x4D probability history isolation, player-vs-non-player gating,
zero-chance / PvP roll non-consumption, critical+mdia flag composition, and
native critical -> mdia -> lane random-stream ordering.
================
*/
func TestResolveCombatRequestIgnoreDefenseHistoryAndStreamOrder(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 10000)
	attacker := playerStatsWithMDIA(t, rt, c, 5)
	plainAttacker := playerStatsWithMDIA(t, rt, c, 0)
	playerDefender := playerStatsWithMDIA(t, rt, c, 0)
	monsterDefender := combat.Stats{Level: 1, MaxLevel: 1, PhysicalDefense: 20, ParryRate: 100, EvasionRate: 50}
	player := criticalActor{division: testDivision, character: c.Name}
	monster := criticalActor{division: testDivision, monster: target.Gid}

	// Player vs non-player with successful roll sets 0x20 and initializes 0x4D history.
	zeroRoll := func() (uint32, error) { return 0, nil }
	rt.CombatRoll = zeroRoll
	skill := enterworld.SkillRow{ID: 77, Attack: enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}}
	wantProc, err := combat.ResolveCalculation(attacker, monsterDefender, combat.AttackCalculation{
		Attack: skill.Attack, OriginalFlags: skill.Attack.Flags, Lanes: 4, Player: true, IgnoreDefense: true,
	}, zeroRoll)
	if err != nil {
		t.Fatalf("resolve expected proc damage: %v", err)
	}
	wantNormal, err := combat.ResolveCalculation(attacker, monsterDefender, combat.AttackCalculation{
		Attack: skill.Attack, OriginalFlags: skill.Attack.Flags, Lanes: 4, Player: true, IgnoreDefense: false,
	}, zeroRoll)
	if err != nil {
		t.Fatalf("resolve expected normal damage: %v", err)
	}
	if wantProc.Damage <= wantNormal.Damage {
		t.Fatalf("expected proc damage %d > normal damage %d", wantProc.Damage, wantNormal.Damage)
	}

	out, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: skill, attacker: attacker, defender: monsterDefender, lanes: 4,
	})
	if err != nil {
		t.Fatalf("resolve player vs monster: %v", err)
	}
	if out.Defender.ResultFlags&combat.IgnoreDefenseResultFlag == 0 || out.Defender.Damage != wantProc.Damage {
		t.Fatalf("procced hit = %+v, want flag %#x and damage %d", out.Defender, combat.IgnoreDefenseResultFlag, wantProc.Damage)
	}
	hist := rt.criticals.actors[player][0x4D00004D]
	if !hist.Initialized || hist.Threshold != -90 {
		t.Fatalf("0x4D history = %+v, want initialized threshold -90", hist)
	}

	// Second hit with threshold -90 misses and restores normal defense reduction.
	second, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: skill, attacker: attacker, defender: monsterDefender, lanes: 4,
	})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.Defender.ResultFlags&combat.IgnoreDefenseResultFlag != 0 || second.Defender.Damage != wantNormal.Damage {
		t.Fatalf("suppressed repeat = %+v, want no 0x20 flag and damage %d", second.Defender, wantNormal.Damage)
	}

	// Block history (0x4200004D) lives in its own slot separate from mdia (0x4D00004D).
	blockingDefender := monsterDefender
	blockingDefender.BlockRate = 5
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	if _, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: skill, attacker: attacker, defender: blockingDefender, lanes: 4,
	}); err != nil {
		t.Fatalf("blocking resolve: %v", err)
	}
	mdiaHist := rt.criticals.actors[player][0x4D00004D]
	blockHist := rt.criticals.actors[player][0x4200004D]
	if !mdiaHist.Initialized || mdiaHist.Threshold != -80 {
		t.Fatalf("mdia history after miss = %+v, want initialized threshold -80", mdiaHist)
	}
	if !blockHist.Initialized || blockHist.Threshold != 10 {
		t.Fatalf("block history = %+v, want initialized threshold 10", blockHist)
	}
	if mdiaHist == blockHist {
		t.Fatalf("mdia and block histories must be separate entries: %+v", mdiaHist)
	}

	// Player vs player creates no 0x4D history, sets no 0x20 flag, and consumes no mdia draw.
	pvpSkill := enterworld.SkillRow{ID: 78, Attack: enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}}
	plainPvpRolls := 0
	rt.CombatRoll = func() (uint32, error) {
		plainPvpRolls++
		return 0, nil
	}
	if _, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: pvpSkill, attacker: plainAttacker, defender: playerDefender, lanes: 4,
	}); err != nil {
		t.Fatalf("resolve plain player vs player: %v", err)
	}
	pvpRolls := 0
	rt.CombatRoll = func() (uint32, error) {
		pvpRolls++
		return 0, nil
	}
	pvpOut, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: pvpSkill, attacker: attacker, defender: playerDefender, lanes: 4,
	})
	if err != nil {
		t.Fatalf("resolve player vs player: %v", err)
	}
	if pvpRolls != plainPvpRolls {
		t.Fatalf("pvp consumed %d rolls, want %d (same as attacker without 0xBC)", pvpRolls, plainPvpRolls)
	}
	if pvpOut.Defender.ResultFlags&combat.IgnoreDefenseResultFlag != 0 {
		t.Fatalf("pvp hit got 0x20 flag: %+v", pvpOut.Defender)
	}
	if _, ok := rt.criticals.actors[player][0x4D00004E]; ok {
		t.Fatal("pvp hit created 0x4D history")
	}

	// Monster actor creates no 0x4D history and sets no 0x20 flag.
	mobSkill := enterworld.SkillRow{ID: 79, Attack: enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}}
	mobOut, err := rt.resolveCombatRequest(combatRequest{
		actor: monster, skill: mobSkill, attacker: monsterDefender, defender: monsterDefender, lanes: 4,
	})
	if err != nil {
		t.Fatalf("resolve monster actor: %v", err)
	}
	if mobOut.Defender.ResultFlags&combat.IgnoreDefenseResultFlag != 0 {
		t.Fatalf("monster hit got 0x20 flag: %+v", mobOut.Defender)
	}
	if _, ok := rt.criticals.actors[monster][0x4D00004F]; ok {
		t.Fatal("monster hit created 0x4D history")
	}

	// Chance 0 consumes no mdia roll (only 4 lane rolls).
	rolls := 0
	rt.CombatRoll = func() (uint32, error) {
		rolls++
		return 0, nil
	}
	zeroSkill := enterworld.SkillRow{ID: 80, Attack: enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}}
	if _, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: zeroSkill, attacker: plainAttacker, defender: monsterDefender, lanes: 4,
	}); err != nil {
		t.Fatalf("zero-chance resolve: %v", err)
	}
	if rolls != 4 {
		t.Fatalf("zero-chance consumed %d rolls, want 4 lane rolls only", rolls)
	}

	// Stream order: critical rolls first (10 misses 3%), then mdia (0 hits 5%), then 4 lane rolls (50).
	critAttacker := attacker
	critAttacker.CriticalRate = 3
	stream := []uint32{10, 0, 50, 50, 50, 50}
	idx := 0
	rt.CombatRoll = func() (uint32, error) {
		v := stream[idx]
		idx++
		return v, nil
	}
	orderSkill := enterworld.SkillRow{ID: 81, Attack: enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}}
	orderOut, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: orderSkill, attacker: critAttacker, defender: monsterDefender, lanes: 4,
	})
	if err != nil {
		t.Fatalf("stream-order resolve: %v", err)
	}
	if idx != len(stream) {
		t.Fatalf("consumed %d rolls, want %d", idx, len(stream))
	}
	if orderOut.Defender.ResultFlags != 0x21 {
		t.Fatalf("stream-order flags = %#x, want 0x21 (mdia proc without critical)", orderOut.Defender.ResultFlags)
	}

	// Proc + critical on a fresh skill ID yields flags 0x22 and critical ignore-defense damage.
	bothSkill := enterworld.SkillRow{ID: 83, Attack: enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}}
	wantCritProc, err := combat.ResolveCalculation(critAttacker, monsterDefender, combat.AttackCalculation{
		Attack: bothSkill.Attack, OriginalFlags: bothSkill.Attack.Flags, Lanes: 4, Player: true, Critical: true, IgnoreDefense: true,
	}, zeroRoll)
	if err != nil {
		t.Fatalf("resolve expected critical+mdia damage: %v", err)
	}
	bothStream := []uint32{0, 0, 0, 0, 0, 0}
	bothIdx := 0
	rt.CombatRoll = func() (uint32, error) {
		v := bothStream[bothIdx]
		bothIdx++
		return v, nil
	}
	bothOut, err := rt.resolveCombatRequest(combatRequest{
		actor: player, skill: bothSkill, attacker: critAttacker, defender: monsterDefender, lanes: 4,
	})
	if err != nil {
		t.Fatalf("critical+mdia resolve: %v", err)
	}
	if bothIdx != len(bothStream) {
		t.Fatalf("critical+mdia consumed %d rolls, want %d", bothIdx, len(bothStream))
	}
	if bothOut.Defender.ResultFlags != 0x22 || bothOut.Defender.Damage != wantCritProc.Damage {
		t.Fatalf("critical+mdia = %+v, want flags 0x22 and damage %d", bothOut.Defender, wantCritProc.Damage)
	}
}

/*
================
TestImbueShareInheritsIgnoreDefense

Verifies that when a primary impact procs MATTR_AVATAR_MDIA (0x20), the active
weapon imbue's damage share also ignores the defender's parry and defense.
================
*/
func TestImbueShareInheritsIgnoreDefense(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.NowMs()); !imbue.Pinned {
		t.Fatal("fixture imbue did not activate")
	}

	plain := playerStatsWithMDIA(t, rt, c, 0)
	mdia := playerStatsWithMDIA(t, rt, c, 5)
	defender := combat.Stats{
		Level: 1, MaxLevel: 1,
		PhysicalDefense: 20, ParryRate: 100,
		MagicalDefense: 20, MagicalParry: 100,
	}
	skill := enterworld.SkillRow{ID: 82, Attack: enterworld.SkillAttack{Present: true, Flags: 5, Percent: 100, Value5: 100}}
	rt.CombatRoll = func() (uint32, error) { return 0, nil }

	base, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, plain, defender, clock.NowMs(), false)
	if err != nil {
		t.Fatalf("base impact: %v", err)
	}
	procced, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, mdia, defender, clock.NowMs(), false)
	if err != nil {
		t.Fatalf("mdia impact: %v", err)
	}
	if procced.ResultFlags&combat.IgnoreDefenseResultFlag == 0 {
		t.Fatalf("mdia impact missing 0x20 flag: %+v", procced)
	}
	if procced.MagicalDamage <= base.MagicalDamage {
		t.Fatalf("imbue magical damage: mdia=%d base=%d; imbue must also ignore defense/parry", procced.MagicalDamage, base.MagicalDamage)
	}
}
