/*
===========================================================================

christmas_decoration_test.go - the decoration-only mode's event groups

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
decorationTestRuntime

A runtime with the Christmas switch on in mode decoration over the
2026 season's window, read the way the composition root reads it.
================
*/
func decorationTestRuntime(t *testing.T) (*Runtime, *time.Time) {
	t.Helper()
	rt, clock := waveTestRuntime(t, false)
	t.Setenv(progression.EnvChristmasEvent, "on")
	t.Setenv(progression.EnvChristmasEventWindow, "2026-12-20T00:00:00Z/2027-01-03T00:00:00Z")
	t.Setenv(progression.EnvChristmasEventMode, string(progression.ChristmasModeDecoration))
	rt.Christmas = progression.ChristmasEventFromEnv()
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	rt.ChristmasDecoration = progression.ChristmasDecorationFromEnv()
	return rt, clock
}

/*
================
TestChristmasDecorationListsTownGroupsOnly

Inside the window an entry lists the decoration layer and the town
carols, during a snowfall hour too, and never the native event group:
no event snow and no start notice. Outside the window it lists none.
================
*/
func TestChristmasDecorationListsTownGroupsOnly(t *testing.T) {
	rt, clock := decorationTestRuntime(t)
	want := []uint32{ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup}
	// 12:00 and 12:30 fall inside the winter timetable's snowfall hour.
	for _, at := range []time.Time{
		time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 25, 12, 30, 0, 0, time.UTC),
		time.Date(2027, 1, 2, 23, 59, 59, 0, time.UTC),
	} {
		*clock = at
		if groups := rt.EntryEventGroups(); !slices.Equal(groups, want) {
			t.Fatalf("at %v: %v, want %v", at, groups, want)
		}
	}
	for _, at := range []time.Time{
		time.Date(2026, 12, 19, 23, 59, 59, 0, time.UTC),
		time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC),
	} {
		*clock = at
		if groups := rt.EntryEventGroups(); len(groups) != 0 {
			t.Fatalf("outside the window at %v: %v", at, groups)
		}
	}
}

/*
================
TestChristmasDecorationWindowEdges

The open and the close each send the two town groups' changes once;
the first tick only learns the state.
================
*/
func TestChristmasDecorationWindowEdges(t *testing.T) {
	rt, clock := decorationTestRuntime(t)
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
		for i, group := range []uint32{ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup} {
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
	// A snowfall hour inside the window sends nothing.
	*clock = time.Date(2026, 12, 25, 12, 30, 0, 0, time.UTC)
	if out := rt.advanceEventGroups(0); len(out) != 0 {
		t.Fatalf("a snowfall hour sent %+v", out)
	}
	edge(time.Date(2027, 1, 3, 0, 0, 1, 0, time.UTC), false)
}
