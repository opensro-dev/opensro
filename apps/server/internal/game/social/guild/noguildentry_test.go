package guild_test

// The Phase A pin: a character's enter-world emits ZERO guild bytes.
// The v1.150 client renders the retail no-guild UI from an EMPTY guild
// entry with NO server frame (makeEmptyGuildEntryBlock hasGuildFlag04=0,
// and the client's guild-block deserializer SGuildData_ReadGuildBlock_826610 ALWAYS sets
// hasGuildFlag04=1 on receipt) - so the correct no-guild posture is the
// ABSENCE of 0x32C4/0xB663/0xB6B8/0x3B29 from the entered stream, which
// this test sweeps over the full transport outcome of HandleEnterWorld
// (the bootstrap packet sequence plus the community seed frames, the
// exact frame list RegisterEnterWorld pushes onto the session).

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

func TestEnterWorldEmitsNoGuildFrames(t *testing.T) {
	t.Parallel()
	race := int64(enterworld.RaceChina)
	gender := int64(enterworld.GenderMale)
	character := &enterworld.Character{
		ID:            1,
		Name:          "guildless",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     &race,
		Gender:        &gender,
	}
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: source,
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(source)
	// The production seed seam (server.go wires SeedFramesFunc): the
	// sweep must cover the seeded stream, not just Build's packets.
	deps.CommunitySeedFramesFor = community.SeedFrames

	outcome := enterworld.HandleEnterWorld(deps, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, enterworld.DefaultDivisionID, "guildless"),
	))
	if !outcome.OK {
		t.Fatalf("enter world failed: %+v", outcome.Result)
	}
	if len(outcome.Frames) == 0 {
		t.Fatal("entered stream is empty - the sweep would prove nothing")
	}

	absent := map[uint16]string{
		guild.OpGuildInfoAbsent:      "0x32C4 guild info",
		guild.OpGuildAckB663Absent:   "0xB663 guild ack",
		guild.OpGuildAckB6B8Absent:   "0xB6B8 guild ack",
		guild.OpGuildDeltaPushAbsent: "0x3B29 guild delta push",
	}
	seeds := 0
	for index, frame := range outcome.Frames {
		if what, hit := absent[frame.NativeOpcode]; hit {
			t.Errorf("frame[%d] is %s - no-guild means ZERO guild bytes", index, what)
		}
		if frame.NativeOpcode == community.OpFriendRosterPush || frame.NativeOpcode == community.OpLetterListAnswer {
			seeds++
		}
	}
	if seeds != 2 {
		t.Fatalf("community seed frames in the stream = %d, want 2 (the sweep must cover the seeded stream)", seeds)
	}
}
