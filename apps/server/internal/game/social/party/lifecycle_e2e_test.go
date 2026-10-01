package party_test

// Replaced-tab exercise of the party disconnect hook: when a character
// re-enters the world, server.go clears the LOSER session's name key at
// bind time so its close resolves nothing - but the close hook can
// outrace that clear. In that window SessionClosed must notice ANOTHER
// live session already holds the bind key (the friend lane's
// FriendSessionClosed guard) and drop NOTHING: the character never went
// offline, and the winner's live party state must survive its
// predecessor's teardown.

import (
	"path/filepath"
	"testing"

	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/party"
)

// TestPartyReplacedTabKeepsWinnerMembership proves the presence guard on
// SessionClosed: a loser whose identity is STILL resolvable (the race
// window before the bind-time name-key clear lands) must not drop the
// winner's membership or consume its pending invitation.
func TestPartyReplacedTabKeepsWinnerMembership(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	server := startPartyServer(t, dir, true)
	bindKey := presence.BindKey(e2eDivision, e2eNameA)

	connA1 := dialWS(t, server.srv)
	helloWS(t, connA1)
	enterWorld(t, connA1, e2eNameA)
	loser, bound := server.srv.Hub.BoundSession(bindKey)
	if !bound {
		t.Fatal("no session bound after the first enter-world")
	}

	// A re-enters: the rebind evicts the loser and the harness clears
	// its name key the way server.go does. Re-set it to reconstruct the
	// race window where the close hook resolves the identity first.
	connA2 := dialWS(t, server.srv)
	helloWS(t, connA2)
	enterWorld(t, connA2, e2eNameA)
	winner, bound := server.srv.Hub.BoundSession(bindKey)
	if !bound || winner == loser {
		t.Fatal("rebind did not hand the bind key to a new session")
	}
	loser.BindCharacter(e2eDivision, e2eNameA, 0)

	// The WINNER's live state: a formed party and an outstanding
	// invitation prompt, both created after the rebind.
	if _, refusal := server.runtime.Registry().Form(e2eDivision,
		party.Member{MemberID: gidA, Name: e2eNameA},
		party.Member{MemberID: gidB, Name: e2eNameB}, 0); refusal != "" {
		t.Fatalf("forming the winner's party refused: %s", refusal)
	}
	server.runtime.Registry().SetPendingInvite(e2eDivision, e2eNameA, party.PendingInvite{
		Kind:        party.PendingInviteJoin,
		InviterName: e2eNameC,
	})

	// The loser's close hook fires with a resolvable identity: the guard
	// must see the winner holding the bind and drop NOTHING.
	server.runtime.SessionClosed(loser)

	if got := server.runtime.Registry().Count(); got != 1 {
		t.Fatalf("registry count after the loser's close = %d, want 1 (the winner's party must survive)", got)
	}
	if _, inParty := server.runtime.Registry().PartyOf(e2eDivision, e2eNameA); !inParty {
		t.Fatal("the winner's membership was dropped by the replaced tab's close")
	}
	invite, outstanding := server.runtime.Registry().TakePendingInvite(e2eDivision, e2eNameA)
	if !outstanding || invite.InviterName != e2eNameC {
		t.Fatalf("the winner's pending invitation was consumed by the replaced tab's close (outstanding %v, inviter %q)", outstanding, invite.InviterName)
	}
}
