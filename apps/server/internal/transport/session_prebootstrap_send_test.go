/*
===========================================================================

session_prebootstrap_send_test.go - nothing precedes the bootstrap

Review test for #2332 (opensro2): the entry gate now opens between
Build and publish, so producers that resolve the entering session by
its early BindCharacter identity and publish outside the receipt lane
(action.SendFrames -> SendBatch, and Session.Send) must not reach the
queue ahead of the scene bootstrap either. Post-Build commits are real
state the snapshot lacks, so like receipts they belong behind it.

===========================================================================
*/
package transport

import (
	"testing"
)

/*
================
TestDirectSendWaitsBehindTheBootstrap

A receipt parks; a direct batch and a direct send issued in the same
loading window must join the queue behind the bootstrap too, in the
order they were issued - never ahead of it.
================
*/
func TestDirectSendWaitsBehindTheBootstrap(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	const reset, receipt, directBatch, directSend = 0x366a, 0xb06d, 0x3417, 0x3b29
	queueOpcodes := func() []uint16 {
		session.mu.Lock()
		defer session.mu.Unlock()
		out := make([]uint16, len(session.queue))
		for i, frame := range session.queue {
			out[i] = frame.Opcode
		}
		return out
	}

	session.BeginSceneAdmission()
	session.MarkSceneSnapshotBuilt()
	revision, valid := session.SceneReceiptRevision()
	if !valid {
		t.Fatal("receipt revision must be valid while loading")
	}
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: receipt, Payload: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	// The SendFrames path (PushCharacterFrames & friends) and a plain
	// Send, issued while the bootstrap is still being encoded.
	if err := session.SendBatch([]Frame{{Opcode: directBatch, Payload: []byte{2}}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Send(directSend, []byte{3}); err != nil {
		t.Fatal(err)
	}
	if got := queueOpcodes(); len(got) != 0 {
		t.Fatalf("frames reached the queue before the bootstrap: %v", got)
	}
	if err := session.SendSceneReset([]Frame{{Opcode: reset, Payload: []byte{9}}}); err != nil {
		t.Fatal(err)
	}
	want := []uint16{reset, receipt, directBatch, directSend}
	got := queueOpcodes()
	if len(got) != len(want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
}

/*
================
TestPrebootstrapMachineryAndAdmissionAnswerStayImmediate

The park fences game traffic only: a transport-control frame and the
admission answer batch (the EnterWorld result and what rides with a
refusal) reach the queue at once, ahead of any parked game frame.
================
*/
func TestPrebootstrapMachineryAndAdmissionAnswerStayImmediate(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	const game = 0x3417

	session.BeginSceneAdmission()
	session.MarkSceneSnapshotBuilt()
	if err := session.Send(game, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := session.Send(OpPong, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if err := session.SendBatch([]Frame{{Opcode: OpEnterWorldResult, Payload: []byte{3}}, {Opcode: game, Payload: []byte{4}}}); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	got := make([]uint16, len(session.queue))
	for i, frame := range session.queue {
		got[i] = frame.Opcode
	}
	parked := session.pendingFrames
	session.mu.Unlock()
	want := []uint16{OpPong, OpEnterWorldResult, game}
	if len(got) != len(want) || parked != 1 {
		t.Fatalf("queue = %v parked = %d, want %v with 1 parked", got, parked, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
}

/*
================
TestDirectSendBeforeTheSnapshotStaysImmediate

Scene loading alone does not open the park: a load whose bootstrap is
not coming through the enter lane (a recall reset, a relocation) still
answers at once. Only the enter lane's snapshot-built mark opens the
window, and its bootstrap then precedes what parked inside it.
================
*/
func TestDirectSendBeforeTheSnapshotStaysImmediate(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	const reset, early, late = 0x366a, 0x3417, 0x3b29
	queueOpcodes := func() []uint16 {
		session.mu.Lock()
		defer session.mu.Unlock()
		out := make([]uint16, len(session.queue))
		for i, frame := range session.queue {
			out[i] = frame.Opcode
		}
		return out
	}

	session.BeginSceneAdmission()
	if err := session.Send(early, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if got := queueOpcodes(); len(got) != 1 || got[0] != early {
		t.Fatalf("unmarked loading scene held its answer: %v", got)
	}
	session.MarkSceneSnapshotBuilt()
	if err := session.Send(late, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if got := queueOpcodes(); len(got) != 1 {
		t.Fatalf("marked window did not park: %v", got)
	}
	if err := session.SendSceneReset([]Frame{{Opcode: reset, Payload: []byte{9}}}); err != nil {
		t.Fatal(err)
	}
	want := []uint16{early, reset, late}
	got := queueOpcodes()
	if len(got) != len(want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
}
