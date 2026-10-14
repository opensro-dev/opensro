/*
===========================================================================

listing_count_e2e_test.go - a party listing's member count follows the party

A listing names its party; the count each page shows is the party's size
when the page is asked for, not when the listing was made. Driven over the
real transport with the wiring.go seams (startJoinServer).

===========================================================================
*/
package match_test

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/game/social/party"
)

/*
================
TestPartyListingPageShowsTheLiveMemberCount

Mira lists while partyless (count 1), Jack joins through the listing and
the party forms. Every later page, Mira's own and Kate's, shows 2.
================
*/
func TestPartyListingPageShowsTheLiveMemberCount(t *testing.T) {
	seeds := []*enterworld.Character{
		{Name: e2eMiraName, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: e2eInt64(enterworld.RaceChina), Gender: e2eInt64(enterworld.GenderMale), Level: e2eInt64(60)},
		{Name: e2eJackName, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: e2eInt64(enterworld.RaceChina), Gender: e2eInt64(enterworld.GenderMale), Level: e2eInt64(20)},
		{Name: e2eKateName, ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: e2eInt64(enterworld.RaceChina), Gender: e2eInt64(enterworld.GenderMale), Level: e2eInt64(25)},
	}
	server := startJoinServer(t, filepath.Join(t.TempDir(), "authority"), seeds)
	mira := dialWS(t, server.srv)
	helloWS(t, mira)
	enterWorld(t, mira, e2eMiraName)
	jack := dialWS(t, server.srv)
	helloWS(t, jack)
	enterWorld(t, jack, e2eJackName)
	kate := dialWS(t, server.srv)
	helloWS(t, kate)
	enterWorld(t, kate, e2eKateName)

	listing := func(members uint8) match.PartyEntry {
		return match.PartyEntry{
			EntryID: 1, PartyNumber: 777, MasterName: e2eMiraName,
			RaceByte:    uint8(enterworld.NativeCountryByte9C(seeds[0])),
			MemberCount: members, TypeBits: 5, Purpose: 0, MinLevel: 1, MaxLevel: 80, Title: "mira party",
		}
	}

	sendFrame(t, mira, match.OpPartyRegisterRequest, partyRequest(0, 777, 5, 0, 1, 80, "mira party"))
	expectExactFrame(t, mira, match.OpPartyRegisterAck,
		concat([]byte{1}, u32le(1), u32le(777), []byte{5, 0, 1, 80}, wideStr("mira party")),
		"mira party register ack")
	sendFrame(t, kate, match.OpPartyPageRequest, []byte{1})
	expectExactFrame(t, kate, match.OpPartyListingPage,
		match.EncodePartyListingB588(1, 1, []match.PartyEntry{listing(1)}), "page before anyone joins")

	sendFrame(t, jack, match.OpPartyJoinRequest, u32le(1))
	jackRow := party.EncodeMaskedMemberRow(chinaRow(gidJack, e2eJackName, 20))
	expectExactFrame(t, mira, match.OpPartyJoinRequest,
		match.EncodePartyJoinNotify75BF(1, 1, match.PartyApplicant{JobClass: 4}, jackRow), "owner notify for jack")
	sendFrame(t, mira, match.OpPartyJoinAnswer, joinAnswer(1, 1, 1))
	formed := []party.MemberRow{chinaRow(gidMira, e2eMiraName, 60), chinaRow(gidJack, e2eJackName, 20)}
	expectJoinSeed(t, mira, gidMira, gidMira, 5, formed, "mira formed seed")
	expectJoinSeed(t, jack, gidJack, gidMira, 5, formed, "jack formed seed")
	expectExactFrame(t, jack, match.OpPartyJoinAck, []byte{1, 1}, "jack join complete")

	sendFrame(t, kate, match.OpPartyPageRequest, []byte{1})
	expectExactFrame(t, kate, match.OpPartyListingPage,
		match.EncodePartyListingB588(1, 1, []match.PartyEntry{listing(2)}), "page after jack joined")
	sendFrame(t, mira, match.OpPartyPageRequest, []byte{1})
	expectExactFrame(t, mira, match.OpPartyListingPage,
		match.EncodePartyListingB588(1, 1, []match.PartyEntry{listing(2)}), "own page after jack joined")
}
