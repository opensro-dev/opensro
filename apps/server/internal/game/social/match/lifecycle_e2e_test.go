/*
===========================================================================

lifecycle_e2e_test.go - match listings across disconnects and rebinds

===========================================================================
*/

package match_test

// Lifecycle exercise of the match board over the REAL transport (the
// e2e_wire_test.go harness): the board must not keep GHOST listings for
// a player whose transport is gone, and must not purge the rows of a
// rebind WINNER when its replaced predecessor finally closes.
//
//	disconnect  -> the close hook purges the owner's rows from BOTH
//	               boards (party + mentor); other owners' rows survive;
//	rebind      -> a second EnterWorld for the same character purges the
//	               stale rows on the OnWorldBound tail (the fresh client
//	               holds no registration snapshot) and NEVER reseeds;
//	replaced tab -> a loser whose close hook outraces the bind-time
//	               name-key clear must NOT purge the winner's rows (the
//	               presence guard: another live session holds the bind).

import (
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

// lifecycleSeeds is the two-character store for the lifecycle tests.
func lifecycleSeeds() []*enterworld.Character {
	return []*enterworld.Character{
		{
			Name:          e2eHeroName,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderMale),
			Level:         e2eInt64(10),
		},
		{
			Name:          e2eAliceName,
			ModelCodename: "CHAR_CH_WOMAN_ADVENTURER",
			RaceIndex:     e2eInt64(enterworld.RaceChina),
			Gender:        e2eInt64(enterworld.GenderFemale),
			Level:         e2eInt64(10),
		},
	}
}

// boardOwnerKey mirrors the lane's owner identity (the hub bind-key
// shape): division + ":" + lowercased name.
func boardOwnerKey(name string) string {
	return e2eDivision + ":" + strings.ToLower(name)
}

// ownRows reads the owner's live rows off both boards (page 1 scopes to
// the division; the own row rides row 0 when present).
func ownRows(board *match.Board, name string) (party []match.PartyEntry, mentor []match.MentorEntry) {
	_, _, partyRows := board.PartyPage(e2eDivision, boardOwnerKey(name), 1)
	for _, row := range partyRows {
		if strings.EqualFold(row.MasterName, name) {
			party = append(party, row)
		}
	}
	_, _, mentorRows := board.MentorPage(e2eDivision, boardOwnerKey(name), 1)
	for _, row := range mentorRows {
		if strings.EqualFold(row.Requester, name) {
			mentor = append(mentor, row)
		}
	}
	return party, mentor
}

/*
==================
waitForPurge

waitForPurge polls the board until the owner's rows are gone (the
close hook runs on the hub's async session teardown) or the deadline
fails the test.
==================
*/
func waitForPurge(t *testing.T, board *match.Board, name string) {
	t.Helper()
	wait.Eventually(t, 5*time.Second, "the board to purge the rows for "+name, func() bool {
		party, mentor := ownRows(board, name)
		return len(party) == 0 && len(mentor) == 0
	})
}

/*
==================
TestMatchDisconnectPurgesListings

TestMatchDisconnectPurgesListings proves the OnSessionClose leg: a
player who logs off leaves NO listing behind, on either board, while
another owner's rows survive untouched.
==================
*/
func TestMatchDisconnectPurgesListings(t *testing.T) {
	server := startMatchServer(t, t.TempDir(), lifecycleSeeds())
	board := server.runtime.Board()

	hero := dialWS(t, server.srv)
	helloWS(t, hero)
	enterWorld(t, hero, e2eHeroName)
	alice := dialWS(t, server.srv)
	helloWS(t, alice)
	enterWorld(t, alice, e2eAliceName)

	sendFrame(t, hero, match.OpPartyRegisterRequest, partyRequest(0, 1001, 2, 1, 15, 80, "hero party"))
	expectFrame(t, hero, match.OpPartyRegisterAck, "hero party register")
	sendFrame(t, hero, match.OpMentorRegisterRequest, mentorRequest(0, 7, 1, "hero camp"))
	expectFrame(t, hero, match.OpMentorRegisterAck, "hero mentor register")
	sendFrame(t, alice, match.OpPartyRegisterRequest, partyRequest(0, 2002, 0, 1, 1, 40, "alice party"))
	expectFrame(t, alice, match.OpPartyRegisterAck, "alice party register")

	sendFrame(t, hero, transport.OpBye, []byte{transport.ByeReasonNormal})
	hero.Close()
	waitForPurge(t, board, e2eHeroName)

	aliceParty, aliceMentor := ownRows(board, e2eAliceName)
	if len(aliceParty) != 1 || len(aliceMentor) != 0 {
		t.Fatalf("alice's rows after hero's purge: party %d, mentor %d, want 1 and 0", len(aliceParty), len(aliceMentor))
	}

	sendFrame(t, alice, transport.OpBye, []byte{transport.ByeReasonNormal})
}

/*
==================
TestMatchRebindPurgesListings

TestMatchRebindPurgesListings proves the OnWorldBound leg: a second
EnterWorld for the same character drops the stale rows (the fresh
client holds no registration snapshot), and the board NEVER reseeds -
the winner's first page answers the empty listing byte-exactly.
==================
*/
func TestMatchRebindPurgesListings(t *testing.T) {
	server := startMatchServer(t, t.TempDir(), lifecycleSeeds())
	board := server.runtime.Board()

	conn1 := dialWS(t, server.srv)
	helloWS(t, conn1)
	enterWorld(t, conn1, e2eHeroName)
	sendFrame(t, conn1, match.OpPartyRegisterRequest, partyRequest(0, 1001, 2, 1, 15, 80, "hero party"))
	expectFrame(t, conn1, match.OpPartyRegisterAck, "party register")
	sendFrame(t, conn1, match.OpMentorRegisterRequest, mentorRequest(0, 7, 1, "hero camp"))
	expectFrame(t, conn1, match.OpMentorRegisterAck, "mentor register")

	// The rebind: the OnWorldBound tail purges BEFORE the fresh session
	// sends anything, so its first page request must answer empty.
	conn2 := dialWS(t, server.srv)
	helloWS(t, conn2)
	enterWorld(t, conn2, e2eHeroName)
	sendFrame(t, conn2, match.OpPartyPageRequest, []byte{1})
	expectExactFrame(t, conn2, match.OpPartyListingPage, []byte(emptyListingPage), "post-rebind party page")
	sendFrame(t, conn2, match.OpMentorPageRequest, []byte{1})
	expectExactFrame(t, conn2, match.OpMentorListingPage, []byte(emptyListingPage), "post-rebind mentor page")

	party, mentor := ownRows(board, e2eHeroName)
	if len(party) != 0 || len(mentor) != 0 {
		t.Fatalf("rows after rebind: party %d, mentor %d, want 0 and 0", len(party), len(mentor))
	}

	sendFrame(t, conn2, transport.OpBye, []byte{transport.ByeReasonNormal})
}

/*
==================
TestMatchReplacedTabKeepsWinnerRows

TestMatchReplacedTabKeepsWinnerRows proves the presence guard: when
the loser's close hook runs while its name key is STILL set (the race
window server.go's bind-time clear normally shuts), the winner's live
rows must survive - another session already holds the bind key.
==================
*/
func TestMatchReplacedTabKeepsWinnerRows(t *testing.T) {
	server := startMatchServer(t, t.TempDir(), lifecycleSeeds())
	board := server.runtime.Board()
	bindKey := boardOwnerKey(e2eHeroName)

	conn1 := dialWS(t, server.srv)
	helloWS(t, conn1)
	enterWorld(t, conn1, e2eHeroName)
	// The exclusive bind runs in the world-bound tail of the game-ready
	// handler, after the frames the client holds; wait for it before reading
	// the hub from the test goroutine.
	gameReadyBarrier(t, conn1, "first world-bound tail")
	loser, bound := server.srv.Hub.BoundSession(bindKey)
	if !bound {
		t.Fatal("no session bound after the first enter-world")
	}

	// The rebind evicts the loser; the harness cleared its name key the
	// way server.go does. Re-set it to reconstruct the race window where
	// the close hook resolves the identity before the clear lands.
	conn2 := dialWS(t, server.srv)
	helloWS(t, conn2)
	enterWorld(t, conn2, e2eHeroName)
	gameReadyBarrier(t, conn2, "winner world-bound tail")
	winner, bound := server.srv.Hub.BoundSession(bindKey)
	if !bound || winner == loser {
		t.Fatal("rebind did not hand the bind key to a new session")
	}
	loser.BindCharacter(e2eDivision, e2eHeroName, 0)

	// The WINNER registers fresh rows.
	sendFrame(t, conn2, match.OpPartyRegisterRequest, partyRequest(0, 3003, 1, 0, 20, 60, "winner party"))
	expectFrame(t, conn2, match.OpPartyRegisterAck, "winner party register")
	sendFrame(t, conn2, match.OpMentorRegisterRequest, mentorRequest(0, 9, 1, "winner camp"))
	expectFrame(t, conn2, match.OpMentorRegisterAck, "winner mentor register")

	// The loser's close hook fires with a resolvable identity: the guard
	// must see the winner holding the bind and purge NOTHING.
	server.runtime.SessionClosed(loser)

	party, mentor := ownRows(board, e2eHeroName)
	if len(party) != 1 || len(mentor) != 1 {
		t.Fatalf("winner's rows after the loser's close: party %d, mentor %d, want 1 and 1", len(party), len(mentor))
	}

	sendFrame(t, conn2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
