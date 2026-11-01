/*
===========================================================================

gmmonster_publication_test.go - monster rewards versus scene replacement

Both entry points must retain the outer gate across commit and capture, and
every rewarded party member must be captured before a replacement bootstrap.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestGMMonsterRewardsCaptureEveryRecipientUnderPublication
================
*/
func TestGMMonsterRewardsCaptureEveryRecipientUnderPublication(t *testing.T) {
	for _, zoe := range []bool{false, true} {
		for _, replaceActor := range []bool{false, true} {
			t.Run(fmt.Sprintf("zoe=%t/replaceActor=%t", zoe, replaceActor), func(t *testing.T) {
				f := newMobKillFixture(t)
				peer := *f.gm
				peer.ID, peer.Name = 4, "party-member"
				characters := fixtureCharacters(f.rt.deps.(*enterworld.Deps).Characters)
				characters[testDivision] = append(characters[testDivision], &peer)
				gmGID, peerGID := enterworld.ObjectIDForCharacter(f.gm), enterworld.ObjectIDForCharacter(&peer)
				f.rt.RewardParties = func(string) []RewardParty {
					return []RewardParty{{Order: 1, Options: 1, Members: []uint32{gmGID, peerGID}}}
				}
				lane := f.rt.operations.lane(testDivision, &f.rt.maintenance)
				assertPublication := func() {
					if lane.publication.TryLock() {
						lane.publication.Unlock()
						t.Fatal("scene replacement could interleave commit and capture")
					}
				}
				f.rt.UpdateExperience = func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
					assertPublication()
					return []wire.Frame{{Opcode: wire.OpExpUpdate, Payload: []byte{1}}}, true
				}
				// Scene revisions model the transport receipt capability: a
				// replacement snapshot already includes the committed award.
				revisions := map[uint32]uint64{gmGID: 1, peerGID: 1}
				captured, delivered := map[uint32]int{}, map[uint32]int{}
				f.rt.CaptureObjectFrames = func(division string, gid uint32) func([]wire.Frame) {
					assertPublication()
					if division != testDivision {
						t.Fatal("wrong division")
					}
					captured[gid]++
					revision := revisions[gid]
					return func(frames []wire.Frame) {
						if revisions[gid] == revision && hasOpcode(frames, wire.OpExpUpdate) {
							delivered[gid]++
						}
					}
				}
				var killed bool
				if zoe {
					killed = f.rt.KillGMZoe(testDivision, f.gm.Name, 1933, 1)
				} else {
					killed = f.rt.KillGMMonster(testDivision, f.gm.Name, f.gid, true)
				}
				if !killed || captured[gmGID] != 1 || captured[peerGID] != 1 {
					t.Fatalf("kill=%v captures=%v, want both recipients captured", killed, captured)
				}
				replaced, retained := peerGID, gmGID
				if replaceActor {
					replaced, retained = gmGID, peerGID
				}
				unlock := f.rt.LockPublication(testDivision, "test")
				revisions[replaced]++
				unlock()
				for _, batch := range f.rt.drainMonsterLegRecipients() {
					if batch.Deliver != nil {
						batch.Deliver()
					}
				}
				if delivered[replaced] != 0 || delivered[retained] != 1 {
					t.Fatalf("reward replayed into a replaced scene: %v", delivered)
				}
			})
		}
	}
}
