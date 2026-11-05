/*
===========================================================================

skillpulse_test.go - native standalone linked result layout

The expected bytes pin the B3C6 mode-2 contract independently of cast framing.

===========================================================================
*/

package wire

import (
	"bytes"
	"testing"
)

/*
================
TestSkillPulseNativeLayout
================
*/
func TestSkillPulseNativeLayout(t *testing.T) {
	frame := SkillPulseFrame(1, 9105, []SkillAreaTarget{{GID: 2, Impacts: []SkillCastTargetImpact{{ResultFlags: 1, Damage: 25}}}})
	want := []byte{2, 1, 0, 0, 0, 145, 35, 0, 0, 1, 1, 2, 0, 0, 0, 0, 1, 25, 0, 0, 0, 0, 0, 0}
	if frame.Opcode != OpSkillPulse || !bytes.Equal(frame.Payload, want) {
		t.Fatalf("pulse %04x %x; want %x", frame.Opcode, frame.Payload, want)
	}
}

/*
================
TestSkillDamageTickNativeLayout

Mode, source, the death byte, target, count, then three bytes per tick;
a tick is capped at the three bytes the client reads.
================
*/
func TestSkillDamageTickNativeLayout(t *testing.T) {
	frame := SkillDamageTickFrame(9001, true, 2, []uint32{25, 0x1000000})
	want := []byte{1, 0x29, 0x23, 0, 0, 0x80, 2, 0, 0, 0, 2, 25, 0, 0, 0xff, 0xff, 0xff}
	if frame.Opcode != OpSkillPulse || !bytes.Equal(frame.Payload, want) {
		t.Fatalf("tick %04x %x; want %x", frame.Opcode, frame.Payload, want)
	}
}

/*
================
TestSkillTrapResultsNativeLayout
================
*/
func TestSkillTrapResultsNativeLayout(t *testing.T) {
	frame := SkillTrapResultsFrame(1, []SkillAreaTarget{{GID: 2, Impacts: []SkillCastTargetImpact{{ResultFlags: 1, Damage: 25}}}})
	want := []byte{3, 1, 0, 0, 0, 1, 1, 2, 0, 0, 0, 0, 1, 25, 0, 0, 0, 0, 0, 0}
	if frame.Opcode != OpSkillPulse || !bytes.Equal(frame.Payload, want) {
		t.Fatalf("trap result %04x %x; want %x", frame.Opcode, frame.Payload, want)
	}
}
