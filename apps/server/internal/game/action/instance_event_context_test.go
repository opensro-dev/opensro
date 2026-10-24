/*
===========================================================================

instance_event_context_test.go - instance handlers never run under the
division lock

Issue #1433: Setup/Enter/Leave/Update/Dead/PartyChanged were reported
to handlers while the reporting code held the division lock, but
InvitationAnswered was reported without it. A handler that called back
into the runtime (InvitePartyToInstance takes the same lock) deadlocked
the division from every kind except InvitationAnswered. Handlers now
all run after the lock is released; this test's sink re-enters the
runtime from EVERY kind and would deadlock the old shape (the watchdog
turns the deadlock into a failure instead of a hung suite).

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestInstanceHandlersMayReenterFromEveryKind

Drives each event kind with a sink that calls back into the runtime
(lock-taking entry points) while handling the event. Completion, in
order, with the lock free in the handler is the assertion.
================
*/
func TestInstanceHandlersMayReenterFromEveryKind(t *testing.T) {
	f := newInstanceInviteFixture(t, instanceInviteShareOptions)

	done := make(chan struct{})
	go func() {
		defer close(done)
		probed := make(map[InstanceEventKind]bool)
		f.rt.InstanceEventSink = func(event InstanceEvent) {
			f.events = append(f.events, event)
			if probed[event.Kind] {
				return
			}
			probed[event.Kind] = true
			// The direct probe: the lane lock is not reentrant, so
			// taking it here deadlocks iff this kind is still
			// reported under it. The NotePartyChanged call is the
			// realistic shape of the same re-entry (it takes the
			// lock to look the character up).
			unlock := f.rt.lockDivision(event.Division)
			unlock()
			f.rt.NotePartyChanged(testDivision, "NoSuchCharacter")
		}

		result := f.invite(t)
		if result.OpenStatus != instance.Success || result.LeaderStatus != instance.Success {
			t.Errorf("invite = %+v, want opened and leader moved", result)
		}
		f.rt.InstanceInviteConsent().ApplyConsent(nil, testDivision, f.members[0], 1, 1)
		memberA := f.members[0]
		if memberA.World == nil || memberA.World.InstanceReturn == nil {
			t.Error("MemberA not inside after accepting")
		}
		f.rt.recordInstanceDeath(testDivision, memberA)
		f.rt.NotePartyChanged(testDivision, "MemberA")
		if status := f.rt.ReturnCharacterFromInstance(testDivision, memberA); status != instance.Success {
			t.Errorf("member return = %v", status)
		}
		if len(probed) == 0 {
			t.Error("the sink never ran: no event was delivered")
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: an instance handler ran under the division lock (or delivery stalled)")
	}

	kinds := f.kinds()
	for _, want := range []InstanceEventKind{
		InstanceEventSetup, InstanceEventEnter, InstanceEventInvitationAnswered,
		InstanceEventDead, InstanceEventPartyChanged, InstanceEventLeave,
	} {
		found := false
		for _, got := range kinds {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("events = %v, missing kind %v", kinds, want)
		}
	}
}
