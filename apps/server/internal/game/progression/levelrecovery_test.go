/*
===========================================================================

levelrecovery_test.go - level recovery, wire state, and transaction refusal.

These cases distinguish healing from increasing a maximum. They exercise
the public grant entry and the candidate callback used by production.

===========================================================================
*/

package progression

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestLevelRecoveryPublishesNewCurrents

A depleted character must receive the same final HP/MP that persistence
commits. A multilevel grant uses the final maxima, not the first crossing.
================
*/
func TestLevelRecoveryPublishesNewCurrents(t *testing.T) {
	character := levelupTestCharacter()
	character.CurrentHP, character.CurrentMP = int64Ptr(17), int64Ptr(9)
	runtime := newTestRuntime(character)
	result := runtime.GrantExperience(character, 600, 0, 0)
	if *character.CurrentHP != 228 || *character.CurrentMP != 228 {
		t.Fatalf("level recovery = %d/%d, want 228/228", *character.CurrentHP, *character.CurrentMP)
	}
	if len(result.Frames) != 4 {
		t.Fatalf("grant frames = %d, want presentation, maxima, currents, EXP", len(result.Frames))
	}
	frame := result.Frames[2]
	if frame.Opcode != vitalsUpdateOpcode || len(frame.Payload) != 15 {
		t.Fatalf("current-gauge frame = %+v", frame)
	}
	if binary.LittleEndian.Uint16(frame.Payload[4:]) != levelRecoverySource || frame.Payload[6] != healthAndManaMask {
		t.Fatalf("recovery header = %x", frame.Payload)
	}
	if binary.LittleEndian.Uint32(frame.Payload[7:]) != 228 || binary.LittleEndian.Uint32(frame.Payload[11:]) != 228 {
		t.Fatalf("wire currents = %x, want 228/228", frame.Payload)
	}
}

/*
================
TestLevelRecoveryRequiresLivingUpwardCrossing

Ordinary EXP and skill-EXP awards never heal, and a dead party member
cannot be resurrected by the party's experience reward.
================
*/
func TestLevelRecoveryRequiresLivingUpwardCrossing(t *testing.T) {
	for _, test := range []struct {
		name           string
		hp, exp, skill int64
	}{
		{name: "ordinary experience", hp: 17, exp: 50},
		{name: "skill experience", hp: 17, skill: 500},
		{name: "dead character", hp: 0, exp: 600},
	} {
		t.Run(test.name, func(t *testing.T) {
			character := levelupTestCharacter()
			character.CurrentHP, character.CurrentMP = int64Ptr(test.hp), int64Ptr(9)
			result := newTestRuntime(character).GrantExperience(character, test.exp, test.skill, 0)
			if *character.CurrentHP != test.hp || *character.CurrentMP != 9 {
				t.Fatalf("unexpected recovery: %d/%d", *character.CurrentHP, *character.CurrentMP)
			}
			for _, frame := range result.Frames {
				if frame.Opcode == vitalsUpdateOpcode {
					t.Fatal("non-recovery grant published a health update")
				}
			}
		})
	}
}

/*
================
TestLevelRecoveryFailureKeepsWholeGrantAtomic

Even a callback that changes its candidate before returning an error must
leave the stored character and outgoing burst completely untouched.
================
*/
func TestLevelRecoveryFailureKeepsWholeGrantAtomic(t *testing.T) {
	character := levelupTestCharacter()
	character.CurrentHP, character.CurrentMP = int64Ptr(17), int64Ptr(9)
	before := character.Snapshot()
	runtime := newTestRuntime(character)
	called := false
	runtime.RecoverLevelVitals = func(candidate *enterworld.Character) error {
		called = true
		if candidate == character || *candidate.Level != 3 || *character.Level != 1 {
			t.Fatal("recovery escaped the detached post-level candidate")
		}
		candidate.CurrentHP = int64Ptr(228)
		return errors.New("incomplete installed recovery projection")
	}
	result := runtime.GrantExperience(character, 600, 500, 0)
	if !called || len(result.Frames) != 0 || !reflect.DeepEqual(character.Snapshot(), before) {
		t.Fatalf("recovery refusal leaked state: called=%v frames=%+v character=%+v", called, result.Frames, character)
	}
}
