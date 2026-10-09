/*
===========================================================================

min_level_test.go - a compiled quest opens at its class's minimum level

CBasicQuest_MeetsPrerequisites (9262A0) admits a quest when the character's
level reaches condition table 0xC2 +0x4 (flag 1). The questdata level
(+0x23) only picks the marker. QNO_EU_EASTEU_4 sets 2 and lists 12: the
Sunset Witch offers it from level 2, under the red scroll until 12.

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestCompiledQuestOpensAtItsMinimumLevel
================
*/
func TestCompiledQuestOpensAtItsMinimumLevel(t *testing.T) {
	licensed.RequireGameData(t)
	rt := expansionRuntime(t)
	def, ok := rt.Defs.ByCodename("QNO_EU_EASTEU_4")
	if !ok {
		t.Fatal("QNO_EU_EASTEU_4 is not loaded")
	}
	if def.MinLevel != 2 || def.Level != 12 {
		t.Fatalf("EASTEU_4 minimum %d level %d, want 2 and 12", def.MinLevel, def.Level)
	}
	for _, tc := range []struct {
		level   int64
		offered bool
		marker  uint8
	}{
		{1, false, 0},
		{2, true, markerStateTooLow},
		{11, true, markerStateTooLow},
		{12, true, markerStateOffer},
	} {
		level, gold := tc.level, int64(0)
		c := &enterworld.Character{ID: 11, Name: "stablehand", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
		if _, offered := npcRow(rt, c, def.Codename, "NPC_EU_WITCH"); offered != tc.offered {
			t.Fatalf("level %d: offered %v, want %v", tc.level, offered, tc.offered)
		}
		marker, shown := rt.MarkerStates(c)[def.RefID]
		if shown != tc.offered || marker.State != tc.marker {
			t.Fatalf("level %d: marker %+v (shown %v), want state %d", tc.level, marker, shown, tc.marker)
		}
		_, err := rt.StartQuest(c, def.Codename)
		if (err == nil) != tc.offered {
			t.Fatalf("level %d: start error %v, want accepted %v", tc.level, err, tc.offered)
		}
	}
}
