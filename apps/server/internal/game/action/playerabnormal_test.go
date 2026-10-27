/*
===========================================================================

playerabnormal_test.go - tests for playerabnormal.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
stunSkill
================
*/
func stunSkill(rt *Runtime) {
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
	index, _ := abnormal.SourceIndex(0x7374)
	skill.Abnormal.Params[index] = abnormal.Param{Present: true, Args: [6]uint32{4000, 50, 3}}
	skills[2] = skill
}

/*
================
TestMonsterStunLandsOnThePlayer
================
*/
func TestMonsterStunLandsOnThePlayer(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	stunSkill(rt)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted || !result.TargetAlive {
		t.Fatalf("stun attack %+v", result)
	}
	block := rt.playerAbnormal(testDivision, c.Name)
	if block == nil || !block.Has(abnormal.Stun) || block.Slots[abnormal.Stun].Grade != 3 {
		t.Fatalf("stun block %+v", block)
	}
	var snapshot, shared bool
	for _, frame := range privateFramesOf(result) {
		if frame.Opcode == 0x36C7 {
			snapshot = true
		}
	}
	for _, frame := range result.Frames {
		if frame.Opcode == simulation.OpVitalsUpdate && abnormalMaskOf(frame.Payload) == block.Mask {
			shared = true
		}
	}
	if !snapshot || !shared {
		t.Fatalf("publication target=%v public=%v", result.Private, result.Frames)
	}
}

// Client 77C110: mask, then each set bit low to high as duration/100, elapsed/100,
// and one byte (level for 0x203F, grade for 0x017FCFC0, otherwise 0).
/*
================
TestPlayerAbnormalSnapshotPayload
================
*/
func TestPlayerAbnormalSnapshotPayload(t *testing.T) {
	const now int64 = 10_000
	// Slot 12 (bit 0x1000) is in neither 0x203F nor 0x017FCFC0, so its byte is 0.
	const plain abnormal.Status = 12
	block := &abnormal.Block{Mask: abnormal.Burn.Bit() | plain.Bit() | abnormal.Stun.Bit()}
	block.Slots[abnormal.Burn] = abnormal.Slot{Active: true, StartedAt: now - 500, Record: abnormal.Record{DurationMs: 3000, Level: 6}}
	block.Slots[plain] = abnormal.Slot{Active: true, StartedAt: now - 100, Record: abnormal.Record{DurationMs: 8000, Level: 9, Grade: 4}}
	block.Slots[abnormal.Stun] = abnormal.Slot{Active: true, StartedAt: now - 200, Record: abnormal.Record{DurationMs: 4000, Grade: 3}}
	got := playerAbnormalSnapshotPayload(block, now)
	want := wire.NewWriter(0).U32(block.Mask).
		U16(30).U16(5).U8(6).
		U16(80).U16(1).U8(0).
		U16(40).U16(2).U8(3).Payload()
	if string(got) != string(want) {
		t.Fatalf("payload %x want %x", got, want)
	}
}

/*
================
TestPlayerAbnormalGates
================
*/
func TestPlayerAbnormalGates(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	cases := []struct {
		status abnormal.Status
		move   bool
		cast   bool
	}{
		{abnormal.Freeze, true, true},
		{abnormal.Sleep, true, true},
		{abnormal.Root, true, false},
		{abnormal.Stun, true, true},
		{abnormal.Slow, false, false},
	}
	for _, tc := range cases {
		rt.storePlayerAbnormal(testDivision, c.Name, &abnormal.Block{Mask: tc.status.Bit()})
		if rt.PlayerMovementBlocked(testDivision, c.Name) != tc.move || rt.playerCastBlocked(testDivision, c.Name) != tc.cast {
			t.Fatalf("%v move %v cast %v", tc.status, rt.PlayerMovementBlocked(testDivision, c.Name), rt.playerCastBlocked(testDivision, c.Name))
		}
	}
}

/*
================
TestPlayerBurnTicksOncePerPeriodAndKills
================
*/
func TestPlayerBurnTicksOncePerPeriodAndKills(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100)
	now := clock.NowMs()
	record := abnormal.Record{Status: abnormal.Burn, Level: 4, DurationMs: 30000, Rate24: 10, Scale20: 1, SourceGID: instance.Gid}
	if owner := rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, now); owner == nil || !rt.playerAbnormal(testDivision, c.Name).Has(abnormal.Burn) {
		t.Fatal("burn did not admit")
	}
	before := enterworld.CurrentHP(c)
	if frames := rt.advancePlayerAbnormals(now); len(frames) == 0 || enterworld.CurrentHP(c) >= before {
		t.Fatalf("first tick hp %d frames %v", enterworld.CurrentHP(c), frames)
	}
	once := enterworld.CurrentHP(c)
	if again := rt.advancePlayerAbnormals(now + 2000); enterworld.CurrentHP(c) != once || len(again) != 0 {
		t.Fatalf("tick inside 2000ms hp %d frames %v", enterworld.CurrentHP(c), again)
	}
	debit := before - once
	c.CurrentHP = testInt64(debit)
	var fatal []simulation.Frame
	for _, batch := range rt.advancePlayerAbnormals(now + 2001) {
		fatal = append(fatal, batch.Frames...)
	}
	if enterworld.CharacterAlive(c) || rt.playerAbnormal(testDivision, c.Name) != nil {
		t.Fatalf("lethal tick left hp %d block %v", enterworld.CurrentHP(c), rt.playerAbnormal(testDivision, c.Name))
	}
	if !hasDeathBaseline(fatal, enterworld.ObjectIDForCharacter(c)) || !hasLifeDead(fatal, enterworld.ObjectIDForCharacter(c)) {
		t.Fatalf("lethal frames %+v", fatal)
	}
}

/*
================
TestMonsterHitRetiresStatusesByDamageLane
================
*/
func TestMonsterHitRetiresStatusesByDamageLane(t *testing.T) {
	for _, flags := range []uint32{5, 9, 13} {
		rt, clock, c, instance := newCombatTestRuntime(t, 100)
		instance.Ref.DefaultSkillIDs[0] = 2
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent, skill.Attack.Flags = 1, 1, 100, flags
		skill.ReplacementPinned = true
		skill.Replacement.MatchesExecutionSelector = true
		skills[2] = skill
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		now := clock.NowMs()
		var records []abnormal.Record
		for _, status := range []abnormal.Status{abnormal.Root, abnormal.Sleep, abnormal.Stun} {
			records = append(records, abnormal.Record{Status: status, Grade: 2, DurationMs: 20000, SourceGID: instance.Gid})
		}
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, records, now)
		result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(c), 2, now)
		mask := uint32(0)
		if block := rt.playerAbnormal(testDivision, c.Name); block != nil {
			mask = block.Mask
		}
		want := uint32(0)
		if flags&8 == 0 {
			want = abnormal.Root.Bit()
		}
		if !result.Accepted || enterworld.CurrentHP(c) >= 100 || mask != want {
			t.Fatalf("flags %x: accepted %v hp %d mask %x want %x", flags, result.Accepted, enterworld.CurrentHP(c), mask, want)
		}
	}
}

/*
================
TestForgetCharacterDropsBlockAndDetachesSource
================
*/
func TestForgetCharacterDropsBlockAndDetachesSource(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	source := fixtureCharacters(deps.Characters)
	peer := &enterworld.Character{ID: 9, Name: "peer", CurrentHP: testInt64(100), Level: testInt64(1)}
	source[testDivision] = append(source[testDivision], peer)
	rt.storePlayerAbnormal(testDivision, c.Name, &abnormal.Block{Mask: abnormal.Stun.Bit()})
	held := &abnormal.Block{Mask: abnormal.Root.Bit()}
	held.Slots[abnormal.Root] = abnormal.Slot{Active: true, Record: abnormal.Record{SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name}}
	rt.storePlayerAbnormal(testDivision, peer.Name, held)
	rt.ForgetCharacter(testDivision, c.Name)
	if rt.playerAbnormal(testDivision, c.Name) != nil {
		t.Fatal("own block survived forget")
	}
	left := rt.playerAbnormal(testDivision, peer.Name)
	if left == nil || !left.Has(abnormal.Root) || left.Slots[abnormal.Root].SourceGID != 0 || left.Slots[abnormal.Root].SourceName != "" {
		t.Fatalf("source detach %+v", left)
	}
}

/*
================
abnormalMaskOf
================
*/
func abnormalMaskOf(payload []byte) uint32 {
	if len(payload) < 11 || payload[6] != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(payload[7:11])
}

/*
================
hasDeathBaseline
================
*/
func hasDeathBaseline(frames []simulation.Frame, gid uint32) bool {
	for _, frame := range frames {
		if frame.Opcode != simulation.OpVitalsUpdate || len(frame.Payload) < 11 || frame.Payload[6] != 1 {
			continue
		}
		if binary.LittleEndian.Uint32(frame.Payload[0:4]) == gid && binary.LittleEndian.Uint32(frame.Payload[7:11]) == 0 {
			return true
		}
	}
	return false
}

/*
================
hasLifeDead
================
*/
func hasLifeDead(frames []simulation.Frame, gid uint32) bool {
	for _, frame := range frames {
		if frame.Opcode != wire.OpObjectStateRefresh {
			continue
		}
		value, err := wire.DecodeObjectStateRefresh(frame.Payload)
		if err == nil && value.Gid == gid && value.StateType == wire.StateChannelLife && value.Value == wire.LifeStateDead {
			return true
		}
	}
	return false
}

/*
================
TestTickPublicBatchesScopeSourceGID

Public tick publications must name their source GID so routeHookFrames scopes
delivery to observers of that monster, player, or COS rather than broadcasting
to out-of-sight sessions across the division.
================
*/
func TestTickPublicBatchesScopeSourceGID(t *testing.T) {
	t.Run("monster-player-cos-abnormals", func(t *testing.T) {
		rt, clock, c, instance := newCombatTestRuntime(t, 1000)
		now := clock.NowMs()
		playerGID := enterworld.ObjectIDForCharacter(c)

		mobBurn := abnormal.Record{Status: abnormal.Burn, Level: 4, DurationMs: 30000, Rate24: 10, Scale20: 1, SourceGID: playerGID}
		rt.Monsters.ApplyDamageSequence(testDivision, instance.Gid, instance.CurrentHP, []simulation.MonsterDamagePlan{{
			GID:             instance.Gid,
			CreditGID:       playerGID,
			Abnormal:        []abnormal.Record{mobBurn},
			AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, []abnormal.Record{mobBurn}),
		}})
		mobBatches := rt.advanceMonsterAbnormals(now)
		assertPublicBatchesSourceGID(t, "monster abnormal", mobBatches, instance.Gid, 0)

		pcBurn := abnormal.Record{Status: abnormal.Burn, Level: 4, DurationMs: 30000, Rate24: 10, Scale20: 1, SourceGID: instance.Gid}
		if rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{pcBurn}, now) == nil {
			t.Fatal("player burn did not admit")
		}
		pcBatches := rt.advancePlayerAbnormals(now)
		assertPublicBatchesSourceGID(t, "player abnormal", pcBatches, playerGID, 0)

		equipCombatTestPet(t, rt, c, 3)
		c.ActiveCOS.CurrentHP = 500
		cosOwner := rt.newCosAbnormalOwner(testDivision, c, now)
		cosOwner.sources = rt.captureAbnormalSources(testDivision, cosOwner.block, []abnormal.Record{pcBurn})
		if !cosOwner.block.Apply(cosOwner, pcBurn, now) {
			t.Fatal("cos burn did not admit")
		}
		cosOwner.commit()
		cosBatches := rt.advanceCosAbnormals(now)
		assertPublicBatchesSourceGID(t, "cos abnormal", cosBatches, c.ActiveCOS.GID, c.ID)
	})

	t.Run("party-aura-retire", func(t *testing.T) {
		rt, clock, c, _ := marchFixture(t, hitMarchFirstID)
		mate := nearbyCharacter(rt, c, 12, "aura-gid-mate", hitMarchRadius-1)
		rt.RewardParties = func(string) []RewardParty {
			return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
		}
		if result := castSelf(rt, c, hitMarchFirstID); result.DiagnosticRefusal != "" {
			t.Fatalf("aura cast refused: %+v", result)
		}
		rt.advancePartyAuras(clock.NowMs())
		key := simulation.WorldKey(testDivision, mate.Name)
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(mate) }, func(w *simulation.WorldState) {
			w.Spawn.X += hitMarchRadius + 500
		})
		auraBatches := rt.advancePartyAuras(clock.NowMs())
		assertPublicBatchesSourceGID(t, "party aura retire", auraBatches, enterworld.ObjectIDForCharacter(mate), 0)
		token := rt.effects.Snapshot(testDivision, c.Name)[0].InstanceToken
		rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: hitMarchFirstID, InstanceToken: token}.Encode())
		retiredBatches := rt.drainStoppedCharacterEffects()
		assertPublicBatchesSourceGID(t, "stopped character effects", retiredBatches, enterworld.ObjectIDForCharacter(c), 0)
	})

	t.Run("berserk-battle-aggression-fallbacks", func(t *testing.T) {
		rt, clock, c, _ := newCombatTestRuntime(t, 1000)
		now := clock.NowMs()
		playerGID := enterworld.ObjectIDForCharacter(c)

		c.BerserkPoints = 5
		rt.HandleBerserk(testDivision, c, []byte{1})
		berserkBatches := rt.advanceBerserk(now + 60000)
		assertPublicBatchesSourceGID(t, "berserk expire", berserkBatches, playerGID, 0)

		rt.enterBattleState(testDivision, c, now)
		battleBatches := rt.advanceBattleStates(now + battleStateMs)
		assertPublicBatchesSourceGID(t, "battle state expire", battleBatches, playerGID, 0)

		peer := &enterworld.Character{ID: c.ID + 1, Name: "aggression-peer", CurrentHP: testInt64(100), Level: testInt64(1)}
		rt.registerPlayerAttack(testDivision, c, peer, now)
		aggressionBatches := rt.advancePlayerAggressions(now + 19000)
		assertPublicBatchesSourceGID(t, "player aggression tick", aggressionBatches, playerGID, 0)
	})
}

/*
================
assertPublicBatchesSourceGID
================
*/
func assertPublicBatchesSourceGID(t *testing.T, label string, batches []simulation.DivisionFrames, wantGID uint32, wantExcept int64) {
	t.Helper()
	sawPublic := false
	for _, batch := range batches {
		if batch.OnlyCharacterID != 0 {
			continue
		}
		sawPublic = true
		if batch.SourceGID != wantGID || batch.ExceptCharacterID != wantExcept {
			t.Fatalf("%s public batch SourceGID=%#x ExceptCharacterID=%d, want SourceGID=%#x ExceptCharacterID=%d",
				label, batch.SourceGID, batch.ExceptCharacterID, wantGID, wantExcept)
		}
	}
	if !sawPublic {
		t.Fatalf("%s produced no public DivisionFrames: %+v", label, batches)
	}
}
