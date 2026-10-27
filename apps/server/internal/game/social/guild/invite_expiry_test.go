/*
===========================================================================

invite_expiry_test.go - the 30 s answer window of a guild invitation

===========================================================================
*/

package guild

import (
	"testing"
	"time"
)

// The reference server expires a proposal after 30 s, not at 30 s; an expired one
// neither blocks new proposals nor answers.
func TestInvitationAnswerWindow(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	r := NewInviteRuntime(nil, nil)
	r.Now = func() time.Time { return now }
	r.setPending("d", "Target", PendingInvite{InviterName: "Inviter"})

	now = now.Add(30 * time.Second)
	if !r.HasPendingInvite("d", "target") {
		t.Fatal("invitation expired at 30 s")
	}
	now = now.Add(time.Millisecond)
	if r.HasPendingInvite("d", "target") {
		t.Fatal("invitation outlived its window")
	}
	if _, ok := r.takePending("d", "target"); ok {
		t.Fatal("an expired invitation answered")
	}
}
