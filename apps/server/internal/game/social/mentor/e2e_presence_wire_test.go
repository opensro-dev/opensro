/*
===========================================================================

e2e_presence_wire_test.go - the camp member connect/disconnect delta
over the real transport

A camp member's world entry and final session close each broadcast
the 0x3AC5 status-5 flag-1 byte to the sitting camp (invite.go's
WorldBound / SessionClosed hooks); the subject itself is seeded, not
delta'd. Driven over the harness startCampServer composes.

===========================================================================
*/
package mentor_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/transport"
)

// campAwaitTCStatus reads until the next 0x3AC5 frame and returns its
// payload. World frames for the moving member (peer spawn/despawn)
// are not this lane's and may interleave; a DUPLICATE status frame is
// still caught by the caller's barrier afterwards.
func campAwaitTCStatus(t *testing.T, c *websocket.Conn, what string) []byte {
	t.Helper()
	for {
		f := campNextFrame(t, c, what)
		if f.Opcode == mentor.OpTCStatus {
			return f.Payload
		}
	}
}

/*
================
TestMentorMemberPresenceBroadcastEndToEnd

Master and student share a camp. The student's Bye + close delivers
{5, gidS, 1, 1} to the master; the student's fresh enter-world
reseeds them (byte-asserted, queue-drained - no self delta) and
delivers {5, gidS, 1, 0} to the master; a barrier then proves the
master's stream carried nothing else.
================
*/
func TestMentorMemberPresenceBroadcastEndToEnd(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "authority")

	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	seeds := []*enterworld.Character{
		{Name: campE2ENameM, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(60)},
		{Name: campE2ENameS, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender(), Level: campLevel(20)},
	}
	env := startCampServer(t, dir, seeds)
	master := campE2ECharacter(t, env.authority, campE2ENameM)
	stud := campE2ECharacter(t, env.authority, campE2ENameS)
	gidM := enterworld.ObjectIDForCharacter(master)
	gidS := enterworld.ObjectIDForCharacter(stud)

	connM := campDialWS(t, env.srv)
	campHelloWS(t, connM)
	campEnterWorld(t, connM, campE2ENameM)
	connS := campDialWS(t, env.srv)
	campHelloWS(t, connS)
	campEnterWorld(t, connS, campE2ENameS)

	campOpenCamp(t, connM, env.authority, master, 60)
	campSendFrame(t, connM, mentor.OpTCInviteRequest, campU32(gidS))
	if got, want := campExpectFrame(t, connS, mentor.OpInvitationProposal, "S's type-9 prompt"), campPromptOracle(gidM, campE2ENameM); !bytes.Equal(got, want) {
		t.Fatalf("0x3393 prompt = % X, want % X", got, want)
	}
	campSendFrame(t, connS, mentor.OpInvitationProposal, []byte{0x01, 0x01})
	roster := []campE2EMember{
		{gid: gidM, name: campE2ENameM, kind: mentor.MemberKindMaster, level: 60},
		{gid: gidS, name: campE2ENameS, kind: mentor.MemberKindStudent, level: 20},
	}
	if got, want := campExpectFrame(t, connS, mentor.OpTCStatus, "the joiner's seed"), campSeedOracle(gidS, roster); !bytes.Equal(got, want) {
		t.Fatalf("joiner's 0x3AC5 seed = % X, want % X", got, want)
	}
	if got, want := campExpectFrame(t, connM, mentor.OpTCStatus, "the master's join row"), campJoinOracle(roster[1]); !bytes.Equal(got, want) {
		t.Fatalf("master's 0x3AC5 join row = % X, want % X", got, want)
	}

	// ---- disconnect: the master sees status 5 flag 1 byte 1 ----
	campSendFrame(t, connS, transport.OpBye, []byte{transport.ByeReasonNormal})
	connS.Close()
	wantOffline := append(append([]byte{0x05}, campU32(gidS)...), 0x01, 0x01)
	if got := campAwaitTCStatus(t, connM, "M sees S disconnect"); !bytes.Equal(got, wantOffline) {
		t.Fatalf("0x3AC5 disconnect delta = % X, want % X", got, wantOffline)
	}

	// ---- reconnect: the student reseeds, the master sees byte 0 ----
	connS2 := campDialWS(t, env.srv)
	campHelloWS(t, connS2)
	if got, want := campEnterWorldWithSeed(t, connS2, campE2ENameS), campSeedOracle(gidS, roster); !bytes.Equal(got, want) {
		t.Fatalf("reconnect 0x3AC5 seed = % X, want % X", got, want)
	}
	wantOnline := append(append([]byte{0x05}, campU32(gidS)...), 0x01, 0x00)
	if got := campAwaitTCStatus(t, connM, "M sees S reconnect"); !bytes.Equal(got, wantOnline) {
		t.Fatalf("0x3AC5 connect delta = % X, want % X", got, wantOnline)
	}
	campRoundTrip(t, connM, "after S's connect delta")

	campSendFrame(t, connM, transport.OpBye, []byte{transport.ByeReasonNormal})
	campSendFrame(t, connS2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
