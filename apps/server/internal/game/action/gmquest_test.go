/*
===========================================================================

gmquest_test.go - the GM's quest tools through the dispatcher and the runtime

The owner finds the GM, rechecks the privilege, hands the quest runtime the
character and delivers what it committed.

===========================================================================
*/
package action

import (
	"bytes"
	"errors"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/staff"
)

/*
================
gmQuestFixture

A runtime whose quest hooks record the call and answer a fixed result, with
its delivery captured.
================
*/
type gmQuestFixture struct {
	rt        *Runtime
	deps      *enterworld.Deps
	c         *enterworld.Character
	calls     []string
	result    GMQuestResult
	err       error
	delivered []wire.Frame
	peers     []wire.Frame
}

func newGMQuestFixture(t *testing.T) *gmQuestFixture {
	t.Helper()
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	f := &gmQuestFixture{rt: rt, deps: rt.deps.(*enterworld.Deps), c: c}
	record := func(label string) func(*enterworld.Character, string) (GMQuestResult, error) {
		return func(character *enterworld.Character, codename string) (GMQuestResult, error) {
			if character != c {
				t.Fatalf("%s got a character other than the GM", label)
			}
			f.calls = append(f.calls, label+":"+codename)
			return f.result, f.err
		}
	}
	rt.GMQuests = GMQuestHooks{
		Init: record("init"), Remove: record("remove"), Complete: record("complete"),
		Reset: func(character *enterworld.Character) (GMQuestResult, error) {
			f.calls = append(f.calls, "reset")
			return f.result, f.err
		},
	}
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { f.delivered = append(f.delivered, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { f.peers = append(f.peers, frames...) }
	return f
}

func gmQuestFrame(sub uint8, codename string) []byte {
	frame := []byte{sub, byte(len(codename)), 0}
	return append(frame, codename...)
}

func TestGMQuestToolsRunOnTheGMAndDeliverTheirFrames(t *testing.T) {
	f := newGMQuestFixture(t)
	f.c.GMPrivilege, f.c.GMCapabilities = true, uint32(staff.All)
	f.result = GMQuestResult{
		Frames:    []wire.Frame{{Opcode: 0x31ED, Payload: []byte{4}}},
		Broadcast: []wire.Frame{{Opcode: 0x3054, Payload: []byte{9}}},
	}
	for _, tc := range []struct {
		payload []byte
		call    string
	}{
		{gmQuestFrame(gmcommand.SubInitQ, "QNO_X"), "init:QNO_X"},
		{gmQuestFrame(gmcommand.SubRemoveQ, "QNO_X"), "remove:QNO_X"},
		{gmQuestFrame(gmcommand.SubCompQ, "QNO_X"), "complete:QNO_X"},
		{[]byte{gmcommand.SubResetQ}, "reset"},
	} {
		f.calls, f.delivered, f.peers = nil, nil, nil
		out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.c, tc.payload, f.rt)
		if !bytes.Equal(out.Ack, []byte{gmcommand.AckResultOK, tc.payload[0]}) {
			t.Fatalf("sub 0x%02X: %+v", tc.payload[0], out)
		}
		if len(f.calls) != 1 || f.calls[0] != tc.call {
			t.Fatalf("sub 0x%02X ran %v, want %q", tc.payload[0], f.calls, tc.call)
		}
		if len(f.delivered) != 1 || len(f.peers) != 1 {
			t.Fatalf("sub 0x%02X delivered %d private and %d peer frames, want 1 and 1", tc.payload[0], len(f.delivered), len(f.peers))
		}
	}
}

func TestGMQuestToolRefusalStillDeliversWhatWasCommitted(t *testing.T) {
	f := newGMQuestFixture(t)
	f.c.GMPrivilege, f.c.GMCapabilities = true, uint32(staff.All)
	f.result = GMQuestResult{Frames: []wire.Frame{{Opcode: 0x31ED, Payload: []byte{2}}}}
	f.err = errors.New("stage 4 refused")

	out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.c, gmQuestFrame(gmcommand.SubCompQ, "QTUTORIAL_CH"), f.rt)
	if !bytes.Equal(out.Ack, []byte{gmcommand.AckResultFail, gmcommand.SubCompQ}) {
		t.Fatalf("ack % X, want the failure", out.Ack)
	}
	if len(f.delivered) != 1 {
		t.Fatalf("delivered %d frames, want the committed stages' frame", len(f.delivered))
	}
}

func TestGMQuestToolsRefuseAPlayerWithoutTouchingTheJournal(t *testing.T) {
	f := newGMQuestFixture(t)
	for _, payload := range [][]byte{gmQuestFrame(gmcommand.SubInitQ, "QNO_X"), {gmcommand.SubResetQ}} {
		out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.c, payload, f.rt)
		if out.Ack != nil || len(f.calls) != 0 || len(f.delivered) != 0 {
			t.Fatalf("a player ran sub 0x%02X: %+v calls %v", payload[0], out, f.calls)
		}
	}
	// Dispatch bypassed: the owner rechecks the privilege itself, so a stale
	// snapshot cannot run a tool for a character that lost it.
	if f.rt.InitQuestGM(testDivision, f.c.Name, "QNO_X") || f.rt.ResetQuestsGM(testDivision, f.c.Name) || len(f.calls) != 0 {
		t.Fatalf("the owner ran a tool for a non-GM: %v", f.calls)
	}
}

func TestGMQuestToolsAreRefusedWhenTheQuestRuntimeIsNotWired(t *testing.T) {
	f := newGMQuestFixture(t)
	f.c.GMPrivilege, f.c.GMCapabilities = true, uint32(staff.All)
	f.rt.GMQuests = GMQuestHooks{}
	out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.c, gmQuestFrame(gmcommand.SubRemoveQ, "QNO_X"), f.rt)
	if !bytes.Equal(out.Ack, []byte{gmcommand.AckResultFail, gmcommand.SubRemoveQ}) {
		t.Fatalf("ack % X, want the failure", out.Ack)
	}
}
