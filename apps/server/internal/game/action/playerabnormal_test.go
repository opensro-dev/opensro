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

// 77C110: mask, then each set bit low to high as duration/100, elapsed/100,
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
