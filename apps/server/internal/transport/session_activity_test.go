/*
===========================================================================

session_activity_test.go - a census span's traffic summary

The world entry's frame belongs to the span it closes; the span after
it counts only what arrived after the entry, with offsets from the
entry; unhandled opcodes never enter the list; every inbound frame is
counted by the read loop; and with no census installed nothing is
counted at all.

===========================================================================
*/
package transport

import (
	"sync/atomic"
	"testing"
	"time"
)

// opEnterWorldReady is the game opcode whose handler marks the world
// ready in these tests (0x3012 in the game).
const opEnterWorldReady = 0x3012

/*
================
activityHub

A hub with a census and a clock the test moves, and handlers for the
entry opcode (which marks the world ready) and one game opcode.
================
*/
func activityHub(t *testing.T, census CensusObserver) (*Hub, *Session, *atomic.Int64, chan struct{}) {
	t.Helper()
	h := newHub(testCfg())
	h.Census = census
	var clock atomic.Int64
	clock.Store(1_700_000_000_000)
	h.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	handled := make(chan struct{}, 16)
	h.Handle(opEnterWorldReady, func(s *Session, _ uint16, _ []byte) { s.TryMarkWorldReady() })
	h.Handle(0x7034, func(*Session, uint16, []byte) { handled <- struct{}{} })
	s, err := h.createSession()
	if err != nil {
		t.Fatal(err)
	}
	s.BindCharacter("global-official", "Hero", 42)
	return h, s, &clock, handled
}

/*
================
TestSpanSummaryClosesAtEntryAndEnd
================
*/
func TestSpanSummaryClosesAtEntryAndEnd(t *testing.T) {
	census := &recordingHistory{}
	h, s, clock, _ := activityHub(t, census)

	h.dispatch(s, Frame{Opcode: 0x7034})
	h.dispatch(s, Frame{Opcode: opEnterWorldReady})
	entered, ok := census.byKind("world_entered")
	if !ok || entered.Closing == nil {
		t.Fatalf("world entry carried no summary: %+v", entered)
	}
	if !hasOpcode(entered.Closing, opEnterWorldReady) || !hasOpcode(entered.Closing, 0x7034) {
		t.Fatalf("the entry frame is not in the span it closes: %+v", entered.Closing.Opcodes)
	}

	clock.Add(1500)
	for range 3 {
		h.dispatch(s, Frame{Opcode: 0x7034})
		clock.Add(100)
	}
	h.dispatch(s, Frame{Opcode: 0x7FFF}) // no handler
	h.closeSession(s, nil)
	ended, ok := census.byKind("ended")
	if !ok || ended.Closing == nil {
		t.Fatalf("session end carried no summary: %+v", ended)
	}
	want := []OpcodeActivity{{Opcode: 0x7034, Count: 3, FirstMs: 1500}}
	if len(ended.Closing.Opcodes) != 1 || ended.Closing.Opcodes[0] != want[0] {
		t.Fatalf("span after the entry: %+v, want %+v", ended.Closing.Opcodes, want)
	}
	if ended.Closing.FirstHandledMs != 1500 {
		t.Fatalf("first handled = %d, want 1500", ended.Closing.FirstHandledMs)
	}
}

/*
================
TestSpanSummaryCountsEveryInboundFrame

The read loop counts control frames and unhandled ones too: the
summary's frames-in is the wire, the opcode list what was answered.
================
*/
func TestSpanSummaryCountsEveryInboundFrame(t *testing.T) {
	census := &recordingHistory{}
	h, s, _, handled := activityHub(t, census)
	s.activity.take(0) // start the span at the test's beginning
	fc := newFakeConn(false)
	if err := s.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	fc.inbound <- Frame{Opcode: OpPing}
	fc.inbound <- Frame{Opcode: 0x7FFF}
	fc.inbound <- Frame{Opcode: 0x7034}
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the handled frame never dispatched")
	}
	h.closeSession(s, nil)
	ended, ok := census.byKind("ended")
	if !ok || ended.Closing == nil {
		t.Fatal("session end carried no summary")
	}
	if ended.Closing.FramesIn != 3 || len(ended.Closing.Opcodes) != 1 {
		t.Fatalf("summary %+v, want 3 frames in and one handled opcode", ended.Closing)
	}
}

/*
================
TestSpanSummaryCountsNothingWithoutACensus
================
*/
func TestSpanSummaryCountsNothingWithoutACensus(t *testing.T) {
	h, s, _, _ := activityHub(t, nil)
	h.dispatch(s, Frame{Opcode: 0x7034})
	if s.activity.opcodes != nil || s.activity.framesIn.Load() != 0 {
		t.Fatal("a session counted traffic with no census installed")
	}
}

// hasOpcode reports whether the summary lists opcode.
func hasOpcode(summary *ActivitySummary, opcode uint16) bool {
	for _, entry := range summary.Opcodes {
		if entry.Opcode == opcode {
			return true
		}
	}
	return false
}

/*
================
BenchmarkSpanActivityNote

What one handled frame costs a span while the census runs: the read
loop's count plus dispatch's opcode note.
================
*/
func BenchmarkSpanActivityNote(b *testing.B) {
	var activity spanActivity
	activity.take(0)
	b.ReportAllocs()
	now := int64(0)
	for b.Loop() {
		now++
		activity.noteFrame()
		activity.noteHandled(0x7034, now)
	}
}
