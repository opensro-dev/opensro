/*
===========================================================================

account_world_test.go - the account world seat's hand-back

A same-character takeover links the claim it replaced. When the
replacement dies before its world bind - teardown, not the
entry-failure release - the seat returns to the holder it displaced,
which still stands in the world; freeing it instead would let a third
session enter a second character of the account beside the first.

===========================================================================
*/
package transport

import "testing"

/*
================
TestAccountWorldSeatReturnsToTheDisplacedHolder

Claims one character from a first session, takes the seat over from
a second, retires the second before any world bind, and checks the
seat is the first holder's again: a different character is refused
and the first holder claiming anew is a no-op.
================
*/
func TestAccountWorldSeatReturnsToTheDisplacedHolder(t *testing.T) {
	hub := accountHub(t, 4)
	first := admit(t, hub, "alice-1")
	claim, refused := hub.ClaimAccountWorld(first, "global-official", "alpha")
	if claim == nil || refused {
		t.Fatalf("the first claim failed: claim=%v refused=%v", claim != nil, refused)
	}
	replacement := admit(t, hub, "alice-2")
	takeover, refused := hub.ClaimAccountWorld(replacement, "global-official", "alpha")
	if takeover == nil || refused {
		t.Fatalf("the same-character takeover failed: claim=%v refused=%v", takeover != nil, refused)
	}

	// The replacement dies before its world bind displaced anything:
	// its teardown must hand the seat back, not free it.
	hub.closeSession(replacement, nil)

	third := admit(t, hub, "alice-3")
	if _, refused := hub.ClaimAccountWorld(third, "global-official", "beta"); !refused {
		t.Fatal("a different character claimed the seat while the first holder stands in the world")
	}
	if claim, refused := hub.ClaimAccountWorld(first, "global-official", "alpha"); claim != nil || refused {
		t.Fatalf("the first holder no longer holds its seat: claim=%v refused=%v", claim != nil, refused)
	}
}

/*
================
TestAccountMultiWorldAdmitsSeveralCharacters

The closed beta's switch (SRO_BETA_MULTI_CHARACTER, port-only, not
native): a second session of the account entering a different
character is admitted, not refused. With the switch off the same
claim is refused - the native one world seat.
================
*/
func TestAccountMultiWorldAdmitsSeveralCharacters(t *testing.T) {
	for _, multi := range []bool{false, true} {
		hub := accountHub(t, 4)
		hub.cfg.AccountMultiWorld = multi
		first := admit(t, hub, "alice-1")
		if _, refused := hub.ClaimAccountWorld(first, "global-official", "alpha"); refused {
			t.Fatalf("multi=%v: the first character was refused", multi)
		}
		second := admit(t, hub, "alice-2")
		_, refused := hub.ClaimAccountWorld(second, "global-official", "beta")
		if refused == multi {
			t.Fatalf("multi=%v: a second character's claim refused=%v", multi, refused)
		}
	}
}
