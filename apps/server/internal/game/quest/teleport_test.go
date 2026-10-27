/*
===========================================================================

teleport_test.go - the used-a-teleport event's lane door

===========================================================================
*/

package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestTeleportUpdaterStaysSilentWithoutSubscribers

No shipped programme subscribes to the used-a-teleport event, so the
door answers no frames; a nil or deletion-pending character is not
told either.
================
*/
func TestTeleportUpdaterStaysSilentWithoutSubscribers(t *testing.T) {
	update := (&Runtime{}).TeleportUpdater()
	if frames := update(nil, 1, 2001); frames != nil {
		t.Fatalf("nil character answered %v", frames)
	}
	pending := &enterworld.Character{DeletePending: true}
	if frames := update(pending, 1, 2001); frames != nil {
		t.Fatalf("deletion-pending character answered %v", frames)
	}
	if frames := update(&enterworld.Character{}, 1, 2001); frames != nil {
		t.Fatalf("an unsubscribed event answered %v", frames)
	}
}
