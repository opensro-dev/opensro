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
