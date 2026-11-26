/*
===========================================================================

strint_uniques_detach_test.go - a detached copy keeps its point

Pins the release rule of SRO_STR_INT_UNIQUES: a copy's spawn point is held
by the live instance, not by its nest, so detaching it from the nest (the
native release path) does not hand its ground to another build. Only the
instance leaving the world frees the point.

===========================================================================
*/
package simulation

import (
	"testing"
	"time"
)

/*
================
TestStrIntDetachedCopyKeepsItsPoint

The species starts with three copies on three points. Detaching one keeps
all three points held; killing the detached one frees its point.
================
*/
func TestStrIntDetachedCopyKeepsItsPoint(t *testing.T) {
	w := strIntConcurrentWorld(t)
	live := w.expectLive(0, 3)
	state := w.s.divs["division"]
	if held := len(state.uniqueAnchors); held != 3 {
		t.Fatalf("held points = %d, want 3", held)
	}
	target := live[0]
	w.s.mu.Lock()
	detached := w.s.detachNestLocked(state, target.Gid, w.now.UnixMilli())
	w.s.mu.Unlock()
	if !detached {
		t.Fatal("the copy was not attached to a nest")
	}
	if held := len(state.uniqueAnchors); held != 3 {
		t.Fatalf("a detached copy released its point: held = %d, want 3", held)
	}
	if !w.s.Defeat("division", target.Gid, w.now.Add(time.Second)) {
		t.Fatal("defeating the detached copy was refused")
	}
	if held := len(state.uniqueAnchors); held != 2 {
		t.Fatalf("held points after the defeat = %d, want 2", held)
	}
}
