/*
===========================================================================

activity_test.go - every dispatched game frame reports its account

OnSessionActivity is how the beta silk owner (SRO_BETA_SILK_ACTIVE_ONLY)
learns that an account is playing: any player-sent command counts, from
any lane, and a frame no handler takes does not.

===========================================================================
*/

package transport

import "testing"

/*
================
TestEveryHandledFrameReportsItsAccount
================
*/
func TestEveryHandledFrameReportsItsAccount(t *testing.T) {
	hub, s, _ := limitedSession(t, rlCfg(1000, 1000))
	hub.admitAccountSession("acct-1", s)
	var seen []string
	hub.OnSessionActivity(func(account string) { seen = append(seen, account) })
	hub.Handle(0x7021, func(*Session, uint16, []byte) {}) // a movement-lane command
	hub.Handle(0x7150, func(*Session, uint16, []byte) {}) // an alchemy command

	hub.dispatch(s, Frame{Opcode: 0x7021})
	hub.dispatch(s, Frame{Opcode: 0x7150})
	if len(seen) != 2 || seen[0] != "acct-1" || seen[1] != "acct-1" {
		t.Fatalf("handled frames reported %v, want acct-1 twice", seen)
	}
	hub.dispatch(s, Frame{Opcode: 0x7999}) // no handler
	if len(seen) != 2 {
		t.Fatalf("an unhandled opcode counted as activity: %v", seen)
	}
}

/*
================
TestUnadmittedSessionReportsNoActivity

A session attached without a HELLO admission has no account, so the hook
is never called with an empty one.
================
*/
func TestUnadmittedSessionReportsNoActivity(t *testing.T) {
	hub, s, _ := limitedSession(t, rlCfg(1000, 1000))
	called := false
	hub.OnSessionActivity(func(string) { called = true })
	hub.Handle(0x7021, func(*Session, uint16, []byte) {})
	hub.dispatch(s, Frame{Opcode: 0x7021})
	if called {
		t.Fatal("an unadmitted session reported activity")
	}
}
