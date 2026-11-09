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

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/recovery"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
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
/*
================
TestAdvanceNaturalRecoveryScratchDropsStaleKeys

The recovery pass refills its due-key scratch every tick (#2033):
a forgotten session leaves no key behind for the next pass, and a
session bound again afterwards is found again.
================
*/
func TestAdvanceNaturalRecoveryScratchDropsStaleKeys(t *testing.T) {
	c := testCharacter()
	hp, mp := enterworld.DerivedMaxHP(c)-1, enterworld.DerivedMaxMP(c)-1
	c.CurrentHP = &hp
	c.CurrentMP = &mp
	rt, clock := newTestRuntime(c, testItems())
	rt.BindRecoverySession(testDivision, c, 1)
	now := clock.NowMs() + 4000
	rt.advanceNaturalRecovery(now)
	want := recoveryKey{testDivision, "asd2"}
	if len(rt.recoveryKeys) != 1 || rt.recoveryKeys[0] != want {
		t.Fatalf("scratch holds %v due keys, want [%v]", rt.recoveryKeys, want)
	}
	rt.forgetRecoverySession(testDivision, c.Name)
	rt.advanceNaturalRecovery(now + 60_000)
	if got := len(rt.recoveryKeys); got != 0 {
		t.Fatalf("scratch holds %d keys after the session was forgotten, want 0", got)
	}
	rt.BindRecoverySession(testDivision, c, 2)
	rt.advanceNaturalRecovery(now + 64_000)
	if len(rt.recoveryKeys) != 1 || rt.recoveryKeys[0] != want {
		t.Fatalf("scratch holds %v due keys after rebind, want [%v]", rt.recoveryKeys, want)
	}
}

/*
================
TestNaturalRecoveryScratchWritesDoNotLeakAcrossPulses

The pulse assembles its modifier writes in the lane scratch (#2033):
a boost installed for one pulse must not ride the next pulse after the
installation is forgotten.
================
*/
func TestNaturalRecoveryScratchWritesDoNotLeakAcrossPulses(t *testing.T) {
	c := testCharacter()
	hp, mp := enterworld.DerivedMaxHP(c), enterworld.DerivedMaxMP(c)
	c.CurrentHP = &hp
	c.CurrentMP = &mp
	rt, clock := newTestRuntime(c, testItems())
	rt.BindRecoverySession(testDivision, c, 1)
	now := clock.NowMs()
	if got := rt.advanceNaturalRecovery(now + 4000); len(got) != 0 {
		t.Fatalf("full resident pulsed: %+v", got)
	}
	modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{{Parameter: itemParamMaxHP, Value: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name,
		SkillID: 900, SkillGroup: 900, InstanceToken: 900, State: statuseffect.StateActive, Modifiers: modifiers}) {
		t.Fatal("install")
	}
	got := rt.advanceNaturalRecovery(now + 8000)
	if len(got) != 1 || enterworld.CurrentHP(c) <= hp {
		t.Fatalf("boosted pulse: frames %+v hp %d", got, enterworld.CurrentHP(c))
	}
	healed := enterworld.CurrentHP(c)
	rt.effects.Forget(testDivision, c.Name)
	if got := rt.advanceNaturalRecovery(now + 12000); len(got) != 0 || enterworld.CurrentHP(c) != healed {
		t.Fatalf("forgotten boost still rode the pulse: frames %+v hp %d", got, enterworld.CurrentHP(c))
	}
}

/*
================
TestRecoverySessionHeldKeysFindPopulatedEntries

The session folds its abnormal and effects keys once at bind (#2033):
with both stores populated by someone else, the keyed reads still find
the resident's own entries however the writer cased the name, and miss
cleanly once they are gone.
================
*/
func TestRecoverySessionHeldKeysFindPopulatedEntries(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.BindRecoverySession(testDivision, c, 1)
	rt.recoveryMu.Lock()
	session := rt.recoverySessions[recoveryKey{testDivision, "asd2"}]
	rt.recoveryMu.Unlock()
	if session == nil || session.abnormalKey != playerAbnormalKey(testDivision, c.Name) ||
		session.effectsKey != statuseffect.OwnerKey(testDivision, c.Name) {
		t.Fatalf("session keys %+v", session)
	}
	otherBlock := &abnormal.Block{Mask: 2}
	rt.storePlayerAbnormal(testDivision, "SomeoneElse", otherBlock)
	modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{{Parameter: 5, Value: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: "SomeoneElse",
		SkillID: 901, SkillGroup: 901, InstanceToken: 901, State: statuseffect.StateActive, Modifiers: modifiers}) {
		t.Fatal("install other")
	}
	ownBlock := &abnormal.Block{Mask: 1}
	rt.storePlayerAbnormal(testDivision, "ASD2", ownBlock)
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: "ASD2",
		SkillID: 902, SkillGroup: 902, InstanceToken: 902, State: statuseffect.StateActive, Modifiers: modifiers}) {
		t.Fatal("install own")
	}
	if got := rt.playerAbnormalKeyed(session.abnormalKey); got != ownBlock {
		t.Fatalf("keyed abnormal read %+v", got)
	}
	if got := rt.effects.ModifierWritesKeyed(session.effectsKey); len(got) != 1 || got[0].Value != 20 {
		t.Fatalf("keyed modifier read %+v", got)
	}
	rt.storePlayerAbnormal(testDivision, c.Name, nil)
	rt.effects.Forget(testDivision, c.Name)
	if got := rt.playerAbnormalKeyed(session.abnormalKey); got != nil {
		t.Fatalf("forgotten block read %+v", got)
	}
	if got := rt.effects.ModifierWritesKeyed(session.effectsKey); len(got) != 0 {
		t.Fatalf("forgotten writes read %+v", got)
	}
}

/*
================
TestPotionAdmissionSessionHoldsFoldedKeys

A potion admission that precedes resident binding creates the session
itself; it holds the same folded keys a binding would, so the pulse
reads the resident's own abnormal and effect entries through it.
================
*/
func TestPotionAdmissionSessionHoldsFoldedKeys(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.admitPotionRecovery(testDivision, c, recovery.Admission{}, rt.Now().UnixMilli())
	rt.recoveryMu.Lock()
	session := rt.recoverySessions[recoveryKey{testDivision, "asd2"}]
	rt.recoveryMu.Unlock()
	if session == nil || session.abnormalKey != playerAbnormalKey(testDivision, c.Name) ||
		session.effectsKey != statuseffect.OwnerKey(testDivision, c.Name) {
		t.Fatalf("potion session keys %+v", session)
	}
	ownBlock := &abnormal.Block{Mask: 1}
	rt.storePlayerAbnormal(testDivision, "ASD2", ownBlock)
	modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{{Parameter: 5, Value: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: "ASD2",
		SkillID: 902, SkillGroup: 902, InstanceToken: 902, State: statuseffect.StateActive, Modifiers: modifiers}) {
		t.Fatal("install own")
	}
	if got := rt.playerAbnormalKeyed(session.abnormalKey); got != ownBlock {
		t.Fatalf("keyed abnormal read %+v", got)
	}
	if got := rt.effects.ModifierWritesKeyed(session.effectsKey); len(got) != 1 || got[0].Value != 20 {
		t.Fatalf("keyed modifier read %+v", got)
	}
}

/*
================
TestRecoveryPulseAppendsToCallerFrameSet

The division lane assembles the tick's frame set in the caller's
slice (#2033): a seeded prefix survives untouched, the committed
pulse's wrapper lands behind it, and a pulse that does not commit
returns the set unmoved.
================
*/
func TestRecoveryPulseAppendsToCallerFrameSet(t *testing.T) {
	c := testCharacter()
	hp, mp := enterworld.DerivedMaxHP(c)-1, enterworld.DerivedMaxMP(c)-1
	c.CurrentHP = &hp
	c.CurrentMP = &mp
	rt, clock := newTestRuntime(c, testItems())
	start := clock.NowMs()
	rt.BindRecoverySession(testDivision, c, 1)
	out := []simulation.DivisionFrames{{DivisionID: testDivision, OnlyCharacterID: -1}}
	out = rt.advanceNaturalRecoveryInDivision(recoveryKey{testDivision, "asd2"}, start+5000, out)
	if len(out) != 2 || out[0].OnlyCharacterID != -1 || len(out[0].Frames) != 0 ||
		out[1].DivisionID != testDivision || out[1].OnlyCharacterID != c.ID ||
		len(out[1].Frames) != 1 || out[1].Frames[0].Opcode != simulation.OpVitalsUpdate {
		t.Fatalf("pulse set %+v", out)
	}
	out = rt.advanceNaturalRecoveryInDivision(recoveryKey{testDivision, "asd2"}, start+9000, out)
	if len(out) != 2 {
		t.Fatalf("uncommitted pulse moved the set %+v", out)
	}
}

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
