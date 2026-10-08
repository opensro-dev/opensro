package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

func TestTargetReleaseClearsMatchingSelectionAndAnswersTalkClose(t *testing.T) {
	character := testCharacter()
	runtime, _ := newTestRuntime(character, testItems())
	const gid uint32 = 200001
	runtime.Selected.Set(testDivision, character.Name, gid)

	outcome := runtime.HandleTargetRelease(
		testDivision,
		character,
		selectBody(gid),
	)
	if outcome.Refusal != "" {
		t.Fatalf("matching release refused: %s", outcome.Refusal)
	}
	if outcome.Released != gid {
		t.Fatalf("released gid = %d, want %d", outcome.Released, gid)
	}
	if _, ok := runtime.Selected.Get(testDivision, character.Name); ok {
		t.Fatal("matching release left the selection recorded")
	}
	assertOpcodes(t, outcome.Frames, wire.OpTalkCloseResult)
	if got := outcome.Frames[0].Payload; !bytes.Equal(got, []byte{1}) {
		t.Fatalf("0xB4B3 body = % X, want mode-1 body 01", got)
	}
}

func TestTargetReleaseRefusalsPreserveSelection(t *testing.T) {
	character := testCharacter()
	runtime, _ := newTestRuntime(character, testItems())
	const selected uint32 = 200001
	runtime.Selected.Set(testDivision, character.Name, selected)

	for _, testCase := range []struct {
		name     string
		payload  []byte
		answered bool
	}{
		// No client sends a malformed body; it is dropped.
		{name: "malformed", payload: []byte{0x41, 0x0d, 0x03}},
		// A well-formed release of another gid is refused, and answered.
		{name: "different gid", payload: selectBody(selected + 1), answered: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			outcome := runtime.HandleTargetRelease(
				testDivision,
				character,
				testCase.payload,
			)
			if outcome.Refusal == "" {
				t.Fatal("invalid release was accepted")
			}
			assertTalkCloseRefusal(t, outcome.Frames, testCase.answered)
			if gid, ok := runtime.Selected.Get(testDivision, character.Name); !ok || gid != selected {
				t.Fatalf(
					"selection after refusal = %d/%v, want %d preserved",
					gid,
					ok,
					selected,
				)
			}
		})
	}
}

/*
================
TestTargetReleaseWithoutSelectionIsAnswered

BR-261007-0624: the client's release is an untagged barrier that waits
for 0xB4B3. A silent refusal left it waiting until it dropped the session
("Target release timed out"); the refusal is answered with mode 2.
================
*/
func TestTargetReleaseWithoutSelectionIsAnswered(t *testing.T) {
	character := testCharacter()
	runtime, _ := newTestRuntime(character, testItems())

	outcome := runtime.HandleTargetRelease(
		testDivision,
		character,
		selectBody(200001),
	)
	if outcome.Refusal == "" {
		t.Fatal("release without a current selection was accepted")
	}
	assertTalkCloseRefusal(t, outcome.Frames, true)
}

/*
================
assertTalkCloseRefusal

A refused release answers exactly 0xB4B3 [2, code] when answered, else
nothing.
================
*/
func assertTalkCloseRefusal(t *testing.T, frames []wire.Frame, answered bool) {
	t.Helper()
	if !answered {
		if len(frames) != 0 {
			t.Fatalf("dropped release emitted frames: %#v", frames)
		}
		return
	}
	assertOpcodes(t, frames, wire.OpTalkCloseResult)
	if got := frames[0].Payload; !bytes.Equal(got, []byte{2, wire.TalkCloseRefusedCode}) {
		t.Fatalf("0xB4B3 refusal body = % X, want mode 2 with its code", got)
	}
}
