/*
===========================================================================
division_index_test.go - division routing and lifecycle metrics contracts
===========================================================================
*/
package transport

// The division->sessions index (hub.divisions) exists so a division push
// costs O(members), not O(all sessions). Its one hard correctness
// obligation: membership must track the EFFECTIVE division through every
// path that can change it — first Set, division change, key-priority
// flips, eviction/replacement, and close — because a session missing from
// its division set silently stops receiving pushes, which is a worse bug
// than the scan the index replaced.

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// indexedIDs flattens SessionsInDivision to a set of session IDs.
/*
================
indexedIDs
================
*/
func indexedIDs(hub *Hub, division string) map[uint64]bool {
	out := make(map[uint64]bool)
	for _, s := range hub.SessionsInDivision(division) {
		out[s.ID] = true
	}
	return out
}

/*
================
expectDivision
================
*/
func expectDivision(t *testing.T, hub *Hub, division string, want ...*Session) {
	t.Helper()
	got := indexedIDs(hub, division)
	if len(got) != len(want) {
		t.Fatalf("division %q has %d members %v, want %d", division, len(got), got, len(want))
	}
	for _, s := range want {
		if !got[s.ID] {
			t.Fatalf("division %q is missing session %d (members %v)", division, s.ID, got)
		}
	}
}

/*
================
newAttachedSession
================
*/
func newAttachedSession(t *testing.T, hub *Hub, gated bool) (*Session, *fakeConn) {
	t.Helper()
	s, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(gated)
	if err := s.attach(fc, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(s, nil) })
	return s, fc
}

// TestDivisionIndexFollowsPlayerContextAndClose covers every explicit
// player-context path that can change division membership.
/*
================
TestDivisionIndexFollowsPlayerContextAndClose
================
*/
func TestDivisionIndexFollowsPlayerContextAndClose(t *testing.T) {
	hub := newHub(testCfg())
	s1, _ := newAttachedSession(t, hub, false)
	s2, _ := newAttachedSession(t, hub, false)

	expectDivision(t, hub, "DIV_A") // empty before any bind

	s1.BindCharacter("DIV_A", "Alice", 0)
	s2.SetWorldSnapshot("DIV_A", struct{}{})
	expectDivision(t, hub, "DIV_A", s1, s2)

	// Division change moves the member between sets.
	s1.BindCharacter("DIV_B", "Alice", 0)
	expectDivision(t, hub, "DIV_A", s2)
	expectDivision(t, hub, "DIV_B", s1)

	// World publication moves the same identity into its authoritative world.
	s1.SetWorldSnapshot("DIV_C", struct{}{})
	expectDivision(t, hub, "DIV_B")
	expectDivision(t, hub, "DIV_C", s1)

	s1.ClearGameplayContext()
	expectDivision(t, hub, "DIV_C")

	// Close removes the member; rebinding a closed session cannot resurrect it.
	hub.closeSession(s2, nil)
	expectDivision(t, hub, "DIV_A")
	s2.BindCharacter("DIV_A", "Bob", 0)
	expectDivision(t, hub, "DIV_A")
}

// TestDivisionIndexAcrossEviction is the rebind/replacement leg: after
// BindExclusive swaps the character to the winner, the evicted victim is
// STILL indexed (matching the pre-index scan, which also enumerated it)
// but delivery to it is refused at the Session level, and
// its final teardown drops it from the index.
/*
================
TestDivisionIndexAcrossEviction
================
*/
func TestDivisionIndexAcrossEviction(t *testing.T) {
	hub := newHub(testCfg())
	victim, victimConn := newAttachedSession(t, hub, true) // gated: drain stays open
	winner, _ := newAttachedSession(t, hub, false)
	victim.BindCharacter("DIV_A", "Victim", 0)
	winner.BindCharacter("DIV_A", "Winner", 0)

	if _, replaced := hub.BindExclusive("d1:cg", victim); replaced {
		t.Fatal("first bind replaced something")
	}
	if old, replaced := hub.BindExclusive("d1:cg", winner); !replaced || old != victim {
		t.Fatalf("second bind: replaced=%v old=%v, want victim eviction", replaced, old)
	}

	// Mid-drain: both indexed, but a division push to the victim bounces.
	expectDivision(t, hub, "DIV_A", victim, winner)
	if err := victim.Send(0x3126, []byte{0x01}); !errors.Is(err, ErrSessionEvicted) {
		t.Fatalf("evicted victim Send err = %v, want ErrSessionEvicted", err)
	}
	if err := winner.Send(0x3126, []byte{0x02}); err != nil {
		t.Fatalf("winner Send err = %v", err)
	}

	// The victim's BYE ack finishes the teardown; only the winner remains.
	victimConn.inbound <- Frame{Opcode: OpBye, Payload: []byte{ByeReasonNormal}}
	waitUntil(t, "victim teardown", func() bool {
		_, ok := hub.Session(victim.ID)
		return !ok
	})
	expectDivision(t, hub, "DIV_A", winner)
}

// TestSessionLifecycleCounters pins the opened/closed metrics and the
// close-reason classification derived from the cause error.
/*
================
TestSessionLifecycleCounters
================
*/
func TestSessionLifecycleCounters(t *testing.T) {
	hub := newHub(testCfg())
	var sessions []*Session
	for i := 0; i < 4; i++ {
		s, err := hub.createSession()
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
	}
	hub.closeSession(sessions[0], nil)
	hub.closeSession(sessions[1], errGraceExpired)
	hub.closeSession(sessions[2], fmt.Errorf("%w on 0x7738: boom", errHandlerPanic))
	hub.closeSession(sessions[3], errors.New("transport: some transport error"))

	m := hub.Metrics()
	if m.SessionsOpened != 4 || m.SessionsClosed != 4 {
		t.Fatalf("opened/closed = %d/%d, want 4/4", m.SessionsOpened, m.SessionsClosed)
	}
	if m.SessionsClosedClean != 0 || m.SessionsClosedGraceExpired != 1 ||
		m.SessionsClosedHandlerPanic != 1 || m.SessionsClosedOther != 2 ||
		m.SessionsClosedSlowConsumer != 0 {
		t.Fatalf("close breakdown = clean %d grace %d panic %d other %d slow %d, want 0/1/1/2/0",
			m.SessionsClosedClean, m.SessionsClosedGraceExpired,
			m.SessionsClosedHandlerPanic, m.SessionsClosedOther, m.SessionsClosedSlowConsumer)
	}
	if m.LiveSessions != 0 {
		t.Fatalf("live sessions = %d, want 0", m.LiveSessions)
	}
}

// TestFrameCounters pins frames_in (every accepted inbound frame) and
// unhandled_opcode_frames (dispatched frames with no handler anywhere).
/*
================
TestFrameCounters
================
*/
func TestFrameCounters(t *testing.T) {
	hub := newHub(testCfg())
	s, fc := newAttachedSession(t, hub, false)
	handled := make(chan struct{}, 1)
	hub.Handle(0x7738, func(*Session, uint16, []byte) { handled <- struct{}{} })

	fc.inbound <- Frame{Opcode: 0x7738, Payload: []byte{0x01}} // handled
	<-handled
	fc.inbound <- Frame{Opcode: 0x7739, Payload: []byte{0x02}} // nobody registered
	waitUntil(t, "unhandled frame counted", func() bool {
		return hub.Metrics().UnhandledOpcodeFrames == 1
	})
	if got := hub.Metrics().FramesIn; got != 2 {
		t.Fatalf("frames_in = %d, want 2", got)
	}
	_ = s
}

// TestRecordTickDuration pins the tick seam's aggregation: last, mean,
// max, count, and the interval-relative overrun rule.
/*
================
TestRecordTickDuration
================
*/
func TestRecordTickDuration(t *testing.T) {
	hub := newHub(testCfg())
	if m := hub.Metrics(); m.TickCount != 0 || m.TickLastMs != 0 || m.TickMeanMs != 0 {
		t.Fatalf("unwired tick metrics not zero: %+v", m)
	}
	hub.RecordTickDuration(100*time.Millisecond, 250*time.Millisecond)
	hub.RecordTickDuration(300*time.Millisecond, 250*time.Millisecond)
	hub.RecordTickDuration(-5*time.Millisecond, 250*time.Millisecond) // clock skew clamps to 0

	m := hub.Metrics()
	if m.TickCount != 3 {
		t.Fatalf("tick count = %d, want 3", m.TickCount)
	}
	if m.TickLastMs != 0 {
		t.Fatalf("tick last = %vms, want 0 (clamped negative sample)", m.TickLastMs)
	}
	if m.TickMaxMs != 300 {
		t.Fatalf("tick max = %vms, want 300", m.TickMaxMs)
	}
	if want := (100.0 + 300.0) / 3.0; m.TickMeanMs < want-0.001 || m.TickMeanMs > want+0.001 {
		t.Fatalf("tick mean = %vms, want %v", m.TickMeanMs, want)
	}
	if m.TickOverruns != 1 {
		t.Fatalf("tick overruns = %d, want 1 (only the 300ms sample beat 250ms)", m.TickOverruns)
	}
}

// TestHandleErr pins the error-returning registration variant: reserved
// opcodes come back as an error (no panic), everything else registers.
/*
================
TestHandleErr
================
*/
func TestHandleErr(t *testing.T) {
	hub := newHub(testCfg())
	for _, op := range []uint16{OpHello, OpWelcome, OpPing, OpPong, OpBye} {
		if err := hub.HandleErr(op, func(*Session, uint16, []byte) {}); err == nil {
			t.Fatalf("HandleErr(0x%04X) = nil, want refusal", op)
		}
	}
	if err := hub.HandleErr(OpEnterWorld, func(*Session, uint16, []byte) {}); err != nil {
		t.Fatalf("HandleErr(EnterWorld) = %v", err)
	}
	if err := hub.HandleErr(0x7738, func(*Session, uint16, []byte) {}); err != nil {
		t.Fatalf("HandleErr(0x7738) = %v", err)
	}
}
