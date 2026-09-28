/*
===========================================================================

naturalrecovery_test.go - resident recovery cadence and authority boundaries

Drive simulation time explicitly. Pulses must obey motion, session lifetime,
keeper rates and committed gauge publication without wall-clock sleeps.

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
================
TestNaturalRecoveryNativeAmounts
================
*/
func TestNaturalRecoveryNativeAmounts(t *testing.T) {
	for _, tc := range []struct {
		maximum int64
		rate    float32
		want    int64
	}{
		{1000, standingRecoveryRate, 8}, {1000, sittingRecoveryRate, 80},
		{125, standingRecoveryRate, 1}, {125, sittingRecoveryRate, 10},
		{124, standingRecoveryRate, 1}, {124, sittingRecoveryRate, 9},
		{1, standingRecoveryRate, 0}, {2, sittingRecoveryRate, 1}, {0, sittingRecoveryRate, 0},
		{1000, 4.8, 48}, {1000, 48, 480}, {1000, 100, 500},
	} {
		if got := naturalRecoveryAmount(tc.maximum, tc.rate); got != tc.want {
			t.Fatalf("%+v: got %d", tc, got)
		}
	}
}

/*
================
TestNaturalRecoverySessionCadenceAndWire
================
*/
func TestNaturalRecoverySessionCadenceAndWire(t *testing.T) {
	c := testCharacter()
	hp, mp := int64(1), int64(1)
	c.CurrentHP = &hp
	c.CurrentMP = &mp
	rt, clock := newTestRuntime(c, testItems())
	start := clock.NowMs()
	if got := rt.advanceNaturalRecovery(start + 4000); len(got) != 0 {
		t.Fatal("healed offline character")
	}
	rt.BindRecoverySession(testDivision, c, 10)
	if got := rt.advanceNaturalRecovery(start + 3999); len(got) != 0 {
		t.Fatal("early pulse")
	}
	clock.Advance(time.Second)
	rt.BindRecoverySession(testDivision, c, 10)
	got := rt.TickHook()(start + 4000)
	if len(got) != 1 || got[0].OnlyCharacterID != c.ID || len(got[0].Frames) != 1 {
		t.Fatalf("missing private pulse: %+v", got)
	}
	f := got[0].Frames[0]
	p := f.Payload
	if f.Opcode != simulation.OpVitalsUpdate || len(p) != 15 || binary.LittleEndian.Uint16(p[4:]) != 0x10 || p[6] != 3 || binary.LittleEndian.Uint32(p[7:]) != uint32(enterworld.CurrentHP(c)) || binary.LittleEndian.Uint32(p[11:]) != uint32(enterworld.CurrentMP(c)) {
		t.Fatalf("wrong native vitals: %x", p)
	}
	if enterworld.CurrentHP(c) != 1+naturalRecoveryAmount(enterworld.DerivedMaxHP(c), standingRecoveryRate) {
		t.Fatal("wrong standing recovery")
	}
	if len(rt.advanceNaturalRecovery(start+4000)) != 0 {
		t.Fatal("duplicate pulse")
	}
	rt.ForgetCharacter(testDivision, "ASD2")
	if len(rt.advanceNaturalRecovery(start+8000)) != 0 || len(rt.recoverySessions) != 0 {
		t.Fatal("disconnect retained recovery")
	}
}

/*
================
TestNaturalRecoveryMovementPostureCombatAndDeath
================
*/
func TestNaturalRecoveryMovementPostureCombatAndDeath(t *testing.T) {
	for _, mode := range []string{"moving", "transition", "casting", "dead", "deleted", "sitting", "full"} {
		t.Run(mode, func(t *testing.T) {
			c := testCharacter()
			hp, mp := int64(1), int64(1)
			c.CurrentHP = &hp
			c.CurrentMP = &mp
			rt, clock := newTestRuntime(c, testItems())
			now := clock.NowMs() + 4000
			rt.BindRecoverySession(testDivision, c, 1)
			rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
				switch mode {
				case "moving":
					w.MoveSegment = &simulation.MoveSegment{From: w.Spawn, StartedAtMs: now - 1, ArrivesAtMs: now + 1}
				case "transition":
					w.PostureTransitionUntilMs = now + 1
				case "sitting":
					w.Sitting = true
				}
			})
			switch mode {
			case "dead":
				hp = 0
			case "deleted":
				c.DeletePending = true
			case "casting":
				rt.queueSkillFinalize(testDivision, c.Name, enterworld.ObjectIDForCharacter(c), now+1000, wire.SkillCastFinalizeFrame(1))
			case "full":
				hp = enterworld.DerivedMaxHP(c)
				mp = enterworld.DerivedMaxMP(c)
			}
			got := rt.advanceNaturalRecovery(now)
			if mode == "sitting" {
				if len(got) != 1 || enterworld.CurrentHP(c) != 1+naturalRecoveryAmount(enterworld.DerivedMaxHP(c), sittingRecoveryRate) {
					t.Fatal("missing sitting bonus")
				}
			} else if len(got) != 0 {
				t.Fatalf("healed while %s", mode)
			}
			if rt.recoverySessions[recoveryKey{testDivision, "asd2"}].nextMs != now+4000 {
				t.Fatal("blocked pulse did not advance timer")
			}
		})
	}
}

/*
================
TestNaturalRecoveryClampReplacementAndOverrun
================
*/
func TestNaturalRecoveryClampReplacementAndOverrun(t *testing.T) {
	c := testCharacter()
	hp, mp := enterworld.DerivedMaxHP(c)-1, enterworld.DerivedMaxMP(c)-1
	c.CurrentHP = &hp
	c.CurrentMP = &mp
	rt, clock := newTestRuntime(c, testItems())
	start := clock.NowMs()
	rt.BindRecoverySession(testDivision, c, 1)
	clock.Advance(time.Second)
	rt.BindRecoverySession(testDivision, c, 2)
	if len(rt.advanceNaturalRecovery(start+4000)) != 0 {
		t.Fatal("replacement inherited timer")
	}
	if len(rt.advanceNaturalRecovery(start+5000)) != 1 || enterworld.CurrentHP(c) != enterworld.DerivedMaxHP(c) || enterworld.CurrentMP(c) != enterworld.DerivedMaxMP(c) {
		t.Fatal("cap not clamped")
	}
	if len(rt.advanceNaturalRecovery(start+9000)) != 0 {
		t.Fatal("full stats published again")
	}
	rt.advanceNaturalRecovery(start + 25000)
	if rt.recoverySessions[recoveryKey{testDivision, "asd2"}].nextMs != start+17000 {
		t.Fatal("overrun must consume one period per tick, retaining residual")
	}
}

/*
================
TestNaturalRecoveryPublishesOnlyCommittedChanges
================
*/
func TestNaturalRecoveryPublishesOnlyCommittedChanges(t *testing.T) {
	c := testCharacter()
	hp := int64(1)
	c.CurrentHP = &hp
	rt, clock := newTestRuntime(c, testItems())
	rt.BindRecoverySession(testDivision, c, 1)
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(_ *enterworld.Character, _ string, _ func() bool) bool { return false }
	if len(rt.advanceNaturalRecovery(clock.NowMs()+4000)) != 0 || enterworld.CurrentHP(c) != 1 {
		t.Fatal("refused transaction leaked")
	}
}
