/*
===========================================================================

cosplayerkill_test.go - tests for cosplayerkill.go

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPlayerCOSKillEasesThePenaltyByLevelGap

Natively, with a COS victim, a COS four or more levels above the killer eases
10, one of the killer's level 5, and a killer without a penalty gains
nothing.
================
*/
func TestPlayerCOSKillEasesThePenaltyByLevelGap(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	c := testCharacter()
	level := uint8(rewardLevel(c))
	for _, tc := range []struct {
		cosLevel uint8
		want     uint32
	}{
		{level + 4, 90},
		{level, 95},
	} {
		c.PK = &domain.PKRecord{Penalty: 100}
		frames, _ := rt.creditPlayerCOSKill(testDivision, c, tc.cosLevel)
		if c.PK.Penalty != tc.want {
			t.Fatalf("COS level %d vs killer %d: penalty %d, want %d", tc.cosLevel, level, c.PK.Penalty, tc.want)
		}
		if !hasOpcode(frames, wire.OpPKPenalty) {
			t.Fatalf("COS level %d: no 0x30F2 for the killer", tc.cosLevel)
		}
	}
	c.PK = nil
	if frames, public := rt.creditPlayerCOSKill(testDivision, c, level+4); len(frames) != 0 || len(public) != 0 {
		t.Fatalf("a killer without a penalty got frames %v %v", frames, public)
	}
}
