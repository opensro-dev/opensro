/*
===========================================================================

halloween_layer_test.go - the event window switches the decoration group

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"
)

/*
================
TestEntryListsTheGroupOnlyInsideTheWindow
================
*/
func TestEntryListsTheGroupOnlyInsideTheWindow(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	*clock = time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	if groups := rt.EntryEventGroups(); len(groups) != 0 {
		t.Fatalf("before the window: %v", groups)
	}
	*clock = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if groups := rt.EntryEventGroups(); len(groups) != 1 || groups[0] != HalloweenEventGroup {
		t.Fatalf("inside the window: %v", groups)
	}
	off, offClock := waveTestRuntime(t, false)
	*offClock = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if groups := off.EntryEventGroups(); len(groups) != 0 {
		t.Fatalf("event off: %v", groups)
	}
}

/*
================
TestWindowEdgesSendTheGroupChangeOnce

The first tick only learns the state; the open and the close each go out
once, to the open-world division, as 0x3347 (group, state).
================
*/
func TestWindowEdgesSendTheGroupChangeOnce(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	*clock = time.Date(2026, 11, 2, 23, 59, 0, 0, time.UTC)
	if out := rt.advanceHalloweenLayer(0); len(out) != 0 {
		t.Fatal("the first tick sent a change")
	}
	if out := rt.advanceHalloweenLayer(0); len(out) != 0 {
		t.Fatal("a steady state sent a change")
	}
	edge := func(at time.Time, on bool) {
		t.Helper()
		*clock = at
		out := rt.advanceHalloweenLayer(0)
		if len(out) != 1 || out[0].DivisionID != testDivision || len(out[0].Frames) != 1 {
			t.Fatalf("at %v: %+v", at, out)
		}
		frame := out[0].Frames[0]
		if frame.Opcode != eventGroupStateOpcode || !bytes.Equal(frame.Payload, EventGroupStatePayload(HalloweenEventGroup, on)) {
			t.Fatalf("at %v: opcode %#x payload %v", at, frame.Opcode, frame.Payload)
		}
		if again := rt.advanceHalloweenLayer(0); len(again) != 0 {
			t.Fatalf("at %v: the change went out twice", at)
		}
	}
	edge(time.Date(2026, 11, 3, 0, 0, 1, 0, time.UTC), true)
	edge(time.Date(2026, 11, 5, 0, 0, 1, 0, time.UTC), false)
}

/*
================
TestRestartInsideTheWindowSendsNothing
================
*/
func TestRestartInsideTheWindowSendsNothing(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	*clock = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if out := rt.advanceHalloweenLayer(0); len(out) != 0 {
		t.Fatal("a restart inside the window broadcast the group")
	}
}

/*
================
TestEventGroupStatePayload
================
*/
func TestEventGroupStatePayload(t *testing.T) {
	if got := EventGroupStatePayload(5, true); !bytes.Equal(got, []byte{5, 0, 0, 0, 1}) {
		t.Fatalf("on: %v", got)
	}
	if got := EventGroupStatePayload(5, false); !bytes.Equal(got, []byte{5, 0, 0, 0, 0}) {
		t.Fatalf("off: %v", got)
	}
}
