/*
===========================================================================

macro_test.go - the SRO Macro lane on the runtime, flag off and on

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/macro"
)

/*
================
TestMacroLaneOffIsNative

Off sends no 0x3555 at login and answers no 0x7554, and the character
is untouched.
================
*/
func TestMacroLaneOffIsNative(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, nil)
	if got := rt.macroEntryFrames(c); got != nil {
		t.Fatalf("login frames with the lane off: %+v", got)
	}
	result := rt.HandleMacroRequest(testDivision, c, []byte{0, macro.KindHunt, 1})
	if len(result.Frames) != 0 || c.Macro.HuntOn {
		t.Fatalf("off answered %+v, hunt on %v", result.Frames, c.Macro.HuntOn)
	}
}

/*
================
TestMacroLaneStoresAndReplays

On, a switch is stored and acknowledged, and the next login replays it.
================
*/
func TestMacroLaneStoresAndReplays(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, nil)
	rt.ConfigureMacro(true)

	result := rt.HandleMacroRequest(testDivision, c, []byte{0, macro.KindHunt, 1})
	if len(result.Frames) != 1 || result.Frames[0].Opcode != macro.OpAnswer ||
		!bytes.Equal(result.Frames[0].Payload, []byte{0, macro.KindHunt, 1, 0, 1}) {
		t.Fatalf("switch answer = %+v", result.Frames)
	}
	if !c.Macro.HuntOn {
		t.Fatal("hunt switch was not stored on the character")
	}

	packets := rt.macroEntryFrames(c)
	if len(packets) != 1 || packets[0].NativeOpcode != macro.OpConfig {
		t.Fatalf("login frames = %+v", packets)
	}
	if want := macro.ConfigPayload(c.Macro); !bytes.Equal(packets[0].Payload, want) {
		t.Fatalf("login replay % x, want % x", packets[0].Payload, want)
	}
}
