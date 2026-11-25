/*
===========================================================================

peerface_wire_test.go - a guild change reaches the players already watching

Pins #4413 (BUG-097): a player who already sees another keeps the old
guild nameplate unless the server re-sends it. Echo watches Fox; Fox
founds a guild (Echo gets 0x365F with the new face), then dissolves
it (Echo gets 0x37E6). A player who does not watch gets neither.

===========================================================================
*/
package guild_test

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/transport"
)

const (
	peerFaceNameEcho = "e2eFaceEcho"
	peerFaceNameFox  = "e2eFaceFox"
	peerFaceBanner   = "FaceBanner"
	// An arbitrary frame that stands in for Fox's spawn row: it only has
	// to carry the scope change that makes Echo observe Fox.
	peerFaceIntroOpcode uint16 = 0x3015
)

/*
================
peerFaceWatch

Makes viewer's live scene observe gid, the way a spawn row does.
================
*/
func peerFaceWatch(t *testing.T, hub *transport.Hub, viewer string, gid uint32) {
	t.Helper()
	sessions := hub.CharacterSessions(guildE2EDivision, viewer)
	if len(sessions) != 1 {
		t.Fatalf("%s has %d sessions", viewer, len(sessions))
	}
	revision, active := sessions[0].SceneRevision()
	if !active {
		t.Fatalf("%s has no live scene", viewer)
	}
	intro := transport.Frame{
		Opcode:  peerFaceIntroOpcode,
		Payload: []byte{0},
		Scope:   []transport.ObjectScopeChange{{GID: gid, Visible: true}},
	}
	if err := sessions[0].SendUnobservedBatch(revision, gid, []transport.Frame{intro}); err != nil {
		t.Fatal(err)
	}
}

/*
================
peerFaceGID
================
*/
func peerFaceGID(t *testing.T, hub *transport.Hub, name string) uint32 {
	t.Helper()
	sessions := hub.CharacterSessions(guildE2EDivision, name)
	if len(sessions) != 1 {
		t.Fatalf("%s has %d sessions", name, len(sessions))
	}
	gid, ok := sessions[0].CharacterObjectID()
	if !ok {
		t.Fatalf("%s has no object id", name)
	}
	return gid
}

/*
================
TestGuildChangeReachesWatchingPlayers
================
*/
func TestGuildChangeReachesWatchingPlayers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	race := func() *int64 { v := enterworld.RaceChina; return &v }
	gender := func() *int64 { v := enterworld.GenderMale; return &v }
	server := startGuildServer(t, dir, []*enterworld.Character{
		{Name: peerFaceNameEcho, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
		{Name: peerFaceNameFox, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: race(), Gender: gender()},
	})
	hub := server.srv.Hub

	connEcho := guildDialWS(t, server.srv)
	guildHelloWS(t, connEcho)
	guildEnterWorldSeeds(t, connEcho, peerFaceNameEcho)
	connFox := guildDialWS(t, server.srv)
	guildHelloWS(t, connFox)
	guildEnterWorldSeeds(t, connFox, peerFaceNameFox)

	foxGID := peerFaceGID(t, hub, peerFaceNameFox)
	peerFaceWatch(t, hub, peerFaceNameEcho, foxGID)
	guildExpectFrame(t, connEcho, peerFaceIntroOpcode, "Fox's stand-in spawn")

	// ---- Fox founds a guild: Echo's nameplate of Fox binds it ----
	guildSendFrame(t, connFox, guild.OpGuildCreateRequest, mutE2ECreatePayload(0, peerFaceBanner))
	guildExpectFrame(t, connFox, guild.OpGuildCreateAck, "Fox's create ack")
	got := guildExpectFrame(t, connEcho, guild.OpPeerGuildAppearance, "Echo's 0x365F")
	// u32 gid, u32 guild id, ascii name, ascii grant, u32 crest,
	// u32 alliance, u32 alliance crest, u8 fortress role (client 75FE90).
	want := binary.LittleEndian.AppendUint32(nil, foxGID)
	want = binary.LittleEndian.AppendUint32(want, uint32(guildE2EGuildID))
	want = binary.LittleEndian.AppendUint16(want, uint16(len(peerFaceBanner)))
	want = append(want, peerFaceBanner...)
	want = binary.LittleEndian.AppendUint16(want, 0)
	want = append(want, make([]byte, 12)...)
	// The role byte is the founder row's stored fortress role, the same
	// byte Fox's spawn row carries (movement resolvePeerGuildBase).
	fox := guildE2ECharacter(t, server.authority, peerFaceNameFox)
	member, ok := server.authority.Guilds().GuildMember(guildE2EDivision, guildE2EGuildID, fox.ID)
	if !ok {
		t.Fatal("founder row missing")
	}
	want = append(want, member.FortressRole)
	if !bytes.Equal(got, want) {
		t.Fatalf("0x365F = % X, want % X", got, want)
	}

	// ---- Fox dissolves it: Echo's nameplate of Fox loses the guild ----
	guildSendFrame(t, connFox, guild.OpGuildBreakRequest, leaveE2EPayload(0))
	guildExpectFrame(t, connFox, guild.OpGuildBreakAck, "Fox's break ack")
	guildExpectFrame(t, connFox, guild.OpGuildUpdatePush, "Fox's subOp 1")
	got = guildExpectFrame(t, connEcho, guild.OpPeerGuildCleared, "Echo's 0x37E6")
	if want := binary.LittleEndian.AppendUint32(nil, foxGID); !bytes.Equal(got, want) {
		t.Fatalf("0x37E6 = % X, want % X", got, want)
	}

	// Fox's own client learned both changes from its acks; the face
	// frames went only to the watcher.
	guildSendFrame(t, connFox, transport.OpBye, []byte{transport.ByeReasonNormal})
	guildSendFrame(t, connEcho, transport.OpBye, []byte{transport.ByeReasonNormal})
}
