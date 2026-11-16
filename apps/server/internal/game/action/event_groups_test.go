/*
===========================================================================

event_groups_test.go - the seasonal windows switch the event groups

===========================================================================
*/
package action

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/progression"
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
	if out := rt.advanceEventGroups(0); len(out) != 0 {
		t.Fatal("the first tick sent a change")
	}
	if out := rt.advanceEventGroups(0); len(out) != 0 {
		t.Fatal("a steady state sent a change")
	}
	edge := func(at time.Time, on bool) {
		t.Helper()
		*clock = at
		out := rt.advanceEventGroups(0)
		if len(out) != 1 || out[0].DivisionID != testDivision || len(out[0].Frames) != 1 {
			t.Fatalf("at %v: %+v", at, out)
		}
		frame := out[0].Frames[0]
		if frame.Opcode != eventGroupStateOpcode || !bytes.Equal(frame.Payload, EventGroupStatePayload(HalloweenEventGroup, on)) {
			t.Fatalf("at %v: opcode %#x payload %v", at, frame.Opcode, frame.Payload)
		}
		if again := rt.advanceEventGroups(0); len(again) != 0 {
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
	if out := rt.advanceEventGroups(0); len(out) != 0 {
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

/*
================
christmasTestRuntime

The wave fixture with the Halloween event off and the Christmas event's
switch as given, its window 2026-12-20 to 2027-01-03 UTC.
================
*/
func christmasTestRuntime(t *testing.T, on bool) (*Runtime, *time.Time) {
	t.Helper()
	rt, clock := waveTestRuntime(t, false)
	if on {
		t.Setenv(progression.EnvChristmasEvent, "on")
	} else {
		t.Setenv(progression.EnvChristmasEvent, "off")
	}
	t.Setenv(progression.EnvChristmasEventWindow, "2026-12-20T00:00:00Z/2027-01-03T00:00:00Z")
	rt.Christmas = progression.ChristmasEventFromEnv()
	return rt, clock
}

/*
================
TestChristmasEntryListsSnowAndDecoration

Inside the window an entry lists the native event group (its snow) and
the decoration layer's group; outside it, or with the switch off, none.
================
*/
func TestChristmasEntryListsSnowAndDecoration(t *testing.T) {
	rt, clock := christmasTestRuntime(t, true)
	*clock = time.Date(2026, 12, 19, 23, 59, 59, 0, time.UTC)
	if groups := rt.EntryEventGroups(); len(groups) != 0 {
		t.Fatalf("before the window: %v", groups)
	}
	*clock = time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC)
	want := []uint32{ChristmasEventGroup, ChristmasDecorationEventGroup}
	if groups := rt.EntryEventGroups(); !slices.Equal(groups, want) {
		t.Fatalf("inside the window: %v, want %v", groups, want)
	}
	*clock = time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC)
	if groups := rt.EntryEventGroups(); len(groups) != 0 {
		t.Fatalf("at the window's end: %v", groups)
	}
	off, offClock := christmasTestRuntime(t, false)
	*offClock = time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC)
	if groups := off.EntryEventGroups(); len(groups) != 0 {
		t.Fatalf("event off: %v", groups)
	}
}

/*
================
TestChristmasWindowEdgesSendBothGroups

The open and the close each send both groups' changes once, in one
division frame list, and the first tick only learns the state.
================
*/
func TestChristmasWindowEdgesSendBothGroups(t *testing.T) {
	rt, clock := christmasTestRuntime(t, true)
	*clock = time.Date(2026, 12, 19, 23, 59, 0, 0, time.UTC)
	if out := rt.advanceEventGroups(0); len(out) != 0 {
		t.Fatal("the first tick sent a change")
	}
	edge := func(at time.Time, on bool) {
		t.Helper()
		*clock = at
		out := rt.advanceEventGroups(0)
		if len(out) != 1 || out[0].DivisionID != testDivision || len(out[0].Frames) != 2 {
			t.Fatalf("at %v: %+v", at, out)
		}
		for i, group := range []uint32{ChristmasEventGroup, ChristmasDecorationEventGroup} {
			frame := out[0].Frames[i]
			if frame.Opcode != eventGroupStateOpcode || !bytes.Equal(frame.Payload, EventGroupStatePayload(group, on)) {
				t.Fatalf("at %v frame %d: opcode %#x payload %v", at, i, frame.Opcode, frame.Payload)
			}
		}
		if again := rt.advanceEventGroups(0); len(again) != 0 {
			t.Fatalf("at %v: the change went out twice", at)
		}
	}
	edge(time.Date(2026, 12, 20, 0, 0, 1, 0, time.UTC), true)
	edge(time.Date(2027, 1, 3, 0, 0, 1, 0, time.UTC), false)
}

/*
================
TestChristmasOffSendsNothing
================
*/
func TestChristmasOffSendsNothing(t *testing.T) {
	rt, clock := christmasTestRuntime(t, false)
	for _, at := range []time.Time{
		time.Date(2026, 12, 19, 23, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC),
	} {
		*clock = at
		if out := rt.advanceEventGroups(0); len(out) != 0 {
			t.Fatalf("at %v: the event off sent %+v", at, out)
		}
	}
}

/*
================
TestChristmasSnowfallsToggleOnlyTheNativeGroup

Inside the window the decoration stands while the native group follows
the snowfalls: an entry at 13:00 lists the decoration alone, and the
14:00 start and 15:00 stop each send the native group's change once.
================
*/
func TestChristmasSnowfallsToggleOnlyTheNativeGroup(t *testing.T) {
	rt, clock := christmasTestRuntime(t, true)
	*clock = time.Date(2026, 12, 25, 13, 0, 0, 0, time.UTC)
	if groups := rt.EntryEventGroups(); !slices.Equal(groups, []uint32{ChristmasDecorationEventGroup}) {
		t.Fatalf("between snowfalls: %v", groups)
	}
	if out := rt.advanceEventGroups(0); len(out) != 0 {
		t.Fatal("the first tick sent a change")
	}
	for _, step := range []struct {
		at time.Time
		on bool
	}{
		{time.Date(2026, 12, 25, 14, 0, 0, 0, time.UTC), true},
		{time.Date(2026, 12, 25, 15, 0, 0, 0, time.UTC), false},
	} {
		*clock = step.at
		out := rt.advanceEventGroups(0)
		if len(out) != 1 || len(out[0].Frames) != 1 ||
			!bytes.Equal(out[0].Frames[0].Payload, EventGroupStatePayload(ChristmasEventGroup, step.on)) {
			t.Fatalf("at %v: %+v", step.at, out)
		}
		if again := rt.advanceEventGroups(0); len(again) != 0 {
			t.Fatalf("at %v: the change went out twice", step.at)
		}
	}
}
