/*
===========================================================================

summon_rows_test.go - every authored summon row the shipped data names is readable

A summon item reaches characterdata rows by its level thresholds. A row
authored in service but refused by buildCharacterRef fails a player's
summon at that level while the server boots: a transport bag of 141-177
slots once tripped a 140 cap, and the item mall's level 90/105/120
transports could not be called. This makes the shipped data fail the build
instead of the player.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestShippedSummonRowsAreAllReadable
================
*/
func TestShippedSummonRowsAreAllReadable(t *testing.T) {
	clientdata.RequireGameData(t)
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	if refused := items.RefusedSummonRows(); len(refused) != 0 {
		t.Fatalf("summon items reach characterdata rows that failed validation: %v", refused)
	}
	// The item mall's transports at their top forms, the rows that were lost.
	for _, code := range []string{"COS_T_BUFFALO_90", "COS_T_RHINOCEROS_105", "COS_T_WHITEELEPHANT_120"} {
		ref, ok := items.CharacterRefByCodename(code)
		if !ok || ref == nil || ref.InventoryCapacity <= 140 {
			t.Fatalf("%s is not readable with its authored bag (%+v)", code, ref)
		}
	}
}
