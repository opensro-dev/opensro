/*
===========================================================================

revival_parity_test.go - native recovery and protection across revival lanes

Exercise the production entry points so a fix to self-rebirth cannot leave
skill resurrection with different protection or packet publication.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestPresentRebirthRecoversFortyPercentAndPreservesMana

51019E/5101D4 multiply keeper floats by the widened 0.4f constant.
4DF49F first adds one HP and leaves existing mana intact.
================
*/
func TestPresentRebirthRecoversFortyPercentAndPreservesMana(t *testing.T) {
	for _, mana := range []int64{0, 17, 75, 190} {
		c := rebirthTestCharacter(1, 0)
		c.CurrentMP = testInt64(mana)
		rt, _ := newTestRuntime(c, testItems())
		// The level-one fixture has 200-point maxima. These are native
		// instruction results, independent of the port's recovery helper.
		wantHP, wantMP := int64(81), min(int64(200), mana+80)
		out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
		if len(out.Frames) < 3 || *c.CurrentHP != wantHP || *c.CurrentMP != wantMP {
			t.Fatalf("mana %d: got %d/%d, want %d/%d", mana, *c.CurrentHP, *c.CurrentMP, wantHP, wantMP)
		}
		payload := out.Frames[1].Payload
		if binary.LittleEndian.Uint32(payload[7:11]) != uint32(wantHP) ||
			binary.LittleEndian.Uint32(payload[11:15]) != uint32(wantMP) {
			t.Fatalf("revival wire disagrees with committed gauges: %x", payload)
		}
	}
}

/*
================
TestSkillResurrectionPublishesAndExpiresProtection

46CB30 calls 4DF290 with the in-place selector before skill recovery.
That base revival grants the same six-second field protection as self-rebirth.
================
*/
func TestSkillResurrectionPublishesAndExpiresProtection(t *testing.T) {
	c := rebirthTestCharacter(1, 0)
	rt, clock := newTestRuntime(c, testItems())
	rt.resurrections.put(testDivision, c.Name, resurrectionOffer{expiresMs: clock.NowMs() + resurrectionAnswerWindowMs})
	var actor, peers []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { actor = append(actor, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { peers = append(peers, frames...) }
	rt.ResurrectionConsent().ApplyConsent(nil, testDivision, c, 1, 1)
	if !enterworld.CharacterAlive(c) || c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatalf("skill revival omitted protection: HP %v body %d", c.CurrentHP, c.NativeBodyStatus)
	}
	owner := c.BodyStatusOwner
	clock.Advance(time.Second)
	duplicate, _ := rt.acceptResurrection(testDivision, c.Name, resurrectionOffer{}, clock.NowMs())
	if len(duplicate) != 0 || c.BodyStatusOwner != owner {
		t.Fatal("duplicate acceptance renewed a living player's protection")
	}
	for _, frames := range [][]wire.Frame{actor, peers} {
		found := false
		for _, frame := range frames {
			if frame.Opcode != wire.OpObjectStateRefresh {
				continue
			}
			state, err := wire.DecodeObjectStateRefresh(frame.Payload)
			if err == nil && state.StateType == wire.StateChannelBody && state.Value == untouchableBodyStatus {
				found = true
			}
		}
		if !found {
			t.Fatalf("protection omitted from observer frames: %+v", frames)
		}
	}
	clock.Advance((reviveUntouchableMs - 1001) * time.Millisecond)
	rt.advanceBodyRestores(clock.NowMs())
	if c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatal("protection expired early")
	}
	clock.Advance(time.Millisecond)
	rt.advanceBodyRestores(clock.NowMs())
	if c.NativeBodyStatus != 0 {
		t.Fatal("skill revival protection never expired")
	}
}

/*
================
TestPresentRebirthUsesInstalledRecoveryReductions

The one-HP base recovery and the forty-percent recovery are separate native
calls. A half reduction truncates the first to zero; MP suppression must not
discard mana that the corpse already retained.
================
*/
func TestPresentRebirthUsesInstalledRecoveryReductions(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{
		{Parameter: 0x8f, Value: 50},
		{Parameter: 0x90, Value: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rt.effects.Apply(statuseffect.Effect{
		DivisionID: testDivision, CharacterName: c.Name,
		SkillID: 99, SkillGroup: 99, InstanceToken: 900, Modifiers: modifiers,
	}) {
		t.Fatal("could not install recovery reductions")
	}
	c.CurrentHP, c.CurrentMP = testInt64(0), testInt64(9)
	out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
	wantHP := (maxHP * 2 / 5) / 2
	if len(out.Frames) == 0 || *c.CurrentHP != wantHP || *c.CurrentMP != 9 {
		t.Fatalf("reduced revival = %d/%d, want %d/9", *c.CurrentHP, *c.CurrentMP, wantHP)
	}
}
