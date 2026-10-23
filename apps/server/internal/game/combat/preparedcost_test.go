/*
===========================================================================

preparedcost_test.go - tests for preparedcost.go

===========================================================================
*/

package combat

import (
	"errors"
	"fmt"
	"io"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

func TestPreparedCostNativeRegions(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/combat/testdata/native-prepared-costs-20260920.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	count := 0
	for {
		var persistent, player, current, percent, hp, mp, berserk uint32
		var multiplier float32
		_, err := fmt.Fscan(f, &persistent, &player, &current, &percent, &multiplier, &hp, &mp, &berserk)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		gotHP := uint32(PreparedCost(current, 3, uint16(percent), persistent != 0, false, 100))
		gotMP := uint32(PreparedCost(current/2, 7, uint16(percent), persistent != 0, player != 0, multiplier))
		if gotHP != hp || gotMP != mp {
			t.Fatalf("case %d: got %d/%d, native %d/%d", count, gotHP, gotMP, hp, mp)
		}
		count++
	}
	if count != 288 {
		t.Fatal(count)
	}
}

func TestMPDecreaseWrapsAbove100(t *testing.T) {
	mask := enterworld.SkillParameterMask(1) << enterworld.ParameterWizardMPDecrease
	var values enterworld.SkillParameterValues
	values[enterworld.ParameterWizardMPDecrease] = 50
	if got := ApplyMPDecrease(100, mask, values); got != 50 {
		t.Fatal(got)
	}
	values[enterworld.ParameterWizardMPDecrease] = 150
	if got := ApplyMPDecrease(100, mask, values); got != -50 {
		t.Fatal(got)
	}
}
