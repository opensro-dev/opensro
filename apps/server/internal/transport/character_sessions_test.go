/*
===========================================================================

character_sessions_test.go - delivery's session lookup by bound character

Hub.CharacterSessions and Session.CharacterObjectID are what the game
world's frame delivery resolves recipients with. They read the hub index and
the session binding only, so the action runtime can publish from inside a
character door without touching the character store.

===========================================================================
*/

package transport

import "testing"

/*
================
TestCharacterSessionsFollowTheBinding

A session is found by its bound name (case-insensitively), in its own
division only, carries the object id it was bound with, and leaves the
lookup when its gameplay context is cleared.
================
*/
func TestCharacterSessionsFollowTheBinding(t *testing.T) {
	hub := newHub(testCfg())
	alice, _ := newAttachedSession(t, hub, false)
	bob, _ := newAttachedSession(t, hub, false)
	elsewhere, _ := newAttachedSession(t, hub, false)
	alice.BindCharacter("DIV_A", "Alice", 101)
	bob.BindCharacter("DIV_A", "Bob", 102)
	elsewhere.BindCharacter("DIV_B", "Alice", 103)

	found := hub.CharacterSessions("DIV_A", "aLICE")
	if len(found) != 1 || found[0] != alice {
		t.Fatalf("Alice in DIV_A = %v, want her one session", found)
	}
	if gid, ok := found[0].CharacterObjectID(); !ok || gid != 101 {
		t.Fatalf("Alice's object id = %d/%v, want 101", gid, ok)
	}
	if found := hub.CharacterSessions("DIV_A", "Carol"); len(found) != 0 {
		t.Fatalf("an unbound name found %v", found)
	}

	alice.ClearGameplayContext()
	if found := hub.CharacterSessions("DIV_A", "Alice"); len(found) != 0 {
		t.Fatalf("a cleared session is still delivered to: %v", found)
	}
	if gid, ok := alice.CharacterObjectID(); ok || gid != 0 {
		t.Fatalf("a cleared session kept object id %d", gid)
	}
}
