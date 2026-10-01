package match_test

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/match"
	"testing"
)

func TestLiveRegistrationAndModifyEnforceEveryJobPurpose(t *testing.T) {
	server := startMatchServer(t, t.TempDir(), lifecycleSeeds())
	connection := dialWS(t, server.srv)
	helloWS(t, connection)
	enterWorld(t, connection, e2eHeroName)
	character := server.deps.Characters.CharactersForDivision(e2eDivision)[0]
	for _, c := range server.deps.Characters.CharactersForDivision(e2eDivision) {
		if c.Name == e2eHeroName {
			character = c
		}
	}
	entryID := uint32(0)
	for job := uint8(1); job <= 4; job++ {
		server.authority.MutateCharacter(character, "test job equipment", func() {
			character.MissionInventory = nil
			if job != 4 {
				character.MissionInventory = []domain.InventoryRow{{Slot: 8, TypeFlags: 0x3ac | uint16(job)<<11}}
			}
		})
		for purpose := uint8(0); purpose < 4; purpose++ {
			allowed := job == 4 && purpose < 2 || (job == 1 || job == 3) && purpose == 2 || job == 2 && purpose == 3
			request := partyRequest(0, 0, 3, purpose, 1, 90, "Eligibility")
			sendFrame(t, connection, match.OpPartyRegisterRequest, request)
			if !allowed {
				expectExactFrame(t, connection, match.OpPartyRegisterAck, []byte{2, 0x23}, "incompatible registration")
				continue
			}
			entryID++
			expectExactFrame(t, connection, match.OpPartyRegisterAck, concat([]byte{1}, partyRequest(entryID, 0, 3, purpose, 1, 90, "Eligibility")), "eligible registration")
			for other := uint8(0); other < 4; other++ {
				sendFrame(t, connection, match.OpPartyModifyRequest, partyRequest(entryID, 0, 3, other, 1, 90, "Modified"))
				valid := job == 4 && other < 2 || (job == 1 || job == 3) && other == 2 || job == 2 && other == 3
				if valid {
					expectExactFrame(t, connection, match.OpPartyModifyAck, concat([]byte{1}, partyRequest(entryID, 0, 3, other, 1, 90, "Modified")), "eligible modification")
				} else {
					expectExactFrame(t, connection, match.OpPartyModifyAck, []byte{2, 0x23}, "incompatible modification")
				}
			}
			// A suit change after opening a form must be read from live authority.
			saved := character.MissionInventory
			server.authority.MutateCharacter(character, "test changed job", func() {
				if job == 4 {
					character.MissionInventory = []domain.InventoryRow{{Slot: 8, TypeFlags: 0xbac}}
				} else {
					character.MissionInventory = nil
				}
			})
			sendFrame(t, connection, match.OpPartyModifyRequest, partyRequest(entryID, 0, 3, purpose, 1, 90, "Stale"))
			expectExactFrame(t, connection, match.OpPartyModifyAck, []byte{2, 0x23}, "stale job rejected")
			server.authority.MutateCharacter(character, "test restore job", func() { character.MissionInventory = saved })
			sendFrame(t, connection, match.OpPartyDeleteRequest, u32le(entryID))
			expectExactFrame(t, connection, match.OpPartyDeleteAck, concat([]byte{1}, u32le(entryID)), "delete fixture")
		}
	}
}

func TestLiveJoinJobPairsAndSuitChangesDuringConsent(t *testing.T) {
	for ownerJob := uint8(1); ownerJob <= 4; ownerJob++ {
		for joinerJob := uint8(1); joinerJob <= 4; joinerJob++ {
			t.Run(fmt.Sprintf("%d-%d", ownerJob, joinerJob), func(t *testing.T) {
				seeds := lifecycleSeeds()
				for _, c := range seeds {
					c.Level = e2eInt64(30)
				}
				server := startJoinServer(t, t.TempDir(), seeds)
				owner := dialWS(t, server.srv)
				helloWS(t, owner)
				enterWorld(t, owner, e2eHeroName)
				joiner := dialWS(t, server.srv)
				helloWS(t, joiner)
				enterWorld(t, joiner, e2eAliceName)
				characters := server.deps.Characters.CharactersForDivision(e2eDivision)
				var ownerChar, joinerChar *enterworld.Character
				for _, c := range characters {
					if c.Name == e2eHeroName {
						ownerChar = c
					}
					if c.Name == e2eAliceName {
						joinerChar = c
					}
				}
				setJob := func(c *enterworld.Character, job uint8) {
					server.authority.MutateCharacter(c, "test job equipment", func() {
						c.MissionInventory = nil
						if job != 4 {
							c.MissionInventory = []domain.InventoryRow{{Slot: 8, TypeFlags: 0x3ac | uint16(job)<<11}}
						}
					})
				}
				setJob(ownerChar, ownerJob)
				setJob(joinerChar, joinerJob)
				purpose := uint8(2)
				if ownerJob == 2 {
					purpose = 3
				}
				if ownerJob == 4 {
					purpose = 0
				}
				sendFrame(t, owner, match.OpPartyRegisterRequest, partyRequest(0, 0, 3, purpose, 1, 90, "Jobs"))
				expectExactFrame(t, owner, match.OpPartyRegisterAck, concat([]byte{1}, partyRequest(1, 0, 3, purpose, 1, 90, "Jobs")), "register")
				sendFrame(t, joiner, match.OpPartyJoinRequest, u32le(1))
				allowed := ownerJob == 4 && joinerJob == 4 || ownerJob == 2 && joinerJob == 2 || (ownerJob == 1 || ownerJob == 3) && (joinerJob == 1 || joinerJob == 3)
				if !allowed {
					expectExactFrame(t, joiner, match.OpPartyJoinAck, []byte{1, 0}, "incompatible join")
					return
				}
				member, ok := server.partyRt.MaskedMemberInfoFor(e2eDivision, e2eAliceName)
				if !ok {
					t.Fatal("missing member")
				}
				for request := uint32(1); request <= 3; request++ {
					if request > 1 {
						sendFrame(t, joiner, match.OpPartyJoinRequest, u32le(1))
					}
					expectExactFrame(t, owner, match.OpPartyJoinRequest, match.EncodePartyJoinNotify75BF(request, 1, match.PartyApplicant{JobClass: joinerJob}, member), "job-aware notify")
					changed := joinerChar
					job := joinerJob
					if request == 2 {
						changed = ownerChar
						job = ownerJob
					}
					if request < 3 {
						other := uint8(4)
						if job == 4 {
							other = 1
						}
						setJob(changed, other)
					}
					sendFrame(t, owner, match.OpPartyJoinAnswer, concat(u32le(request), u32le(1), []byte{1}))
					result := byte(0)
					if request == 3 {
						result = 1
						expectExactFrame(t, joiner, 0xB0D5, concat([]byte{1}, u32le(enterworld.ObjectIDForCharacter(joinerChar))), "joiner formation")
						expectFrame(t, joiner, 0x35D6, "joiner roster")
						expectExactFrame(t, owner, 0xB0D5, concat([]byte{1}, u32le(enterworld.ObjectIDForCharacter(ownerChar))), "owner formation")
						expectFrame(t, owner, 0x35D6, "owner roster")
						roster, formed := server.partyRt.Registry().PartyOf(e2eDivision, e2eHeroName)
						if !formed || len(roster.Members) != 2 {
							t.Fatal("compatible job pair did not form roster")
						}
					}
					expectExactFrame(t, joiner, match.OpPartyJoinAck, []byte{1, result}, "approval revalidation")
					if request < 3 {
						setJob(changed, job)
					}
				}
			})
		}
	}
}
