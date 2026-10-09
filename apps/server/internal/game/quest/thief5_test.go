/*
===========================================================================

thief5_test.go - QNO_CA_THIEF_5's reply closes one follow-up for good

8C6180 accepts the quest with either reply and ends the follow-up that
reply turned away from: the fake evidence (_02) ends QNO_CA_THIEF_6_2,
refusing the thief's deal (_04) ends QNO_CA_THIEF_6_1.

===========================================================================
*/
package quest

import (
	"slices"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestThiefReplyClosesTheOtherFollowUp
================
*/
func TestThiefReplyClosesTheOtherFollowUp(t *testing.T) {
	licensed.RequireGameData(t)
	for reply, kept := range []string{"QNO_CA_THIEF_6_1", "QNO_CA_THIEF_6_2"} {
		closed := map[string]string{"QNO_CA_THIEF_6_1": "QNO_CA_THIEF_6_2", "QNO_CA_THIEF_6_2": "QNO_CA_THIEF_6_1"}[kept]
		t.Run(kept, func(t *testing.T) {
			rt := expansionRuntime(t)
			def := mustQuest(t, rt, "QNO_CA_THIEF_5")
			c := deliveryCharacter(t, rt, "QNO_CA_THIEF_4")
			*c.Level = int64(max(def.Level, 32))
			// The Samarkand thief line is offered to European characters.
			c.ModelCodename = "CHAR_EU_MAN_NOBLE"
			offer, ok := npcRow(rt, c, def.Codename, "NPC_CA_SPECIAL")
			if !ok || len(offer.Branches) != 2 {
				t.Fatalf("the offer %+v (offered %v) has no two replies", offer, ok)
			}
			if _, err := rt.StartQuest(c, branchToken(def.Codename, reply)); err != nil {
				t.Fatalf("accept with reply %d: %v", reply, err)
			}
			if want := []uint32{mustQuest(t, rt, closed).RefID}; !slices.Equal(c.EndedQuestIds, want) {
				t.Fatalf("reply %d ended %v, want %s", reply, c.EndedQuestIds, closed)
			}
			holdItems(t, rt, c, def.CollectItemCodename, def.CollectCount)
			if _, err := rt.AdvanceNpcQuest(c, def.Codename, def.EndNpcCodename); err != nil {
				t.Fatalf("turn in: %v", err)
			}
			if !offeredAt(rt, c, kept, "NPC_CA_SPECIAL") || offeredAt(rt, c, closed, "NPC_CA_SPECIAL") {
				t.Fatalf("after reply %d: %s offered %v, %s offered %v", reply,
					kept, offeredAt(rt, c, kept, "NPC_CA_SPECIAL"), closed, offeredAt(rt, c, closed, "NPC_CA_SPECIAL"))
			}
		})
	}
}

/*
================
offeredAt
================
*/
func offeredAt(rt *Runtime, c *enterworld.Character, code, npc string) bool {
	row, ok := npcRow(rt, c, code, npc)
	return ok && !row.Complete && !row.Informational
}
