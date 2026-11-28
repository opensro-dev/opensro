/*
===========================================================================

textdata_recovery_test.go - the shipped rows' natural-recovery column

characterdata field 40 is the flat HP a standing monster regains every
four seconds. Two rows pin the column: the loader must land exactly on
it, not on a neighbour.

===========================================================================
*/

package monster

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestShippedRecoveryColumn
================
*/
func TestShippedRecoveryColumn(t *testing.T) {
	tier.Long(t, "shipped recovery column")
	clientdata.RequireGameData(t)
	refs := LoadMonsterRefs(gamedatatest.TextdataDir(t))
	want := map[string]uint32{
		"MOB_CH_MANGNYANG":  3,
		"MOB_CH_TIGERWOMAN": 18,
	}
	for _, ref := range refs {
		if expected, ok := want[ref.Codename]; ok {
			if ref.RecoveryHP != expected {
				t.Errorf("%s RecoveryHP = %d, want %d", ref.Codename, ref.RecoveryHP, expected)
			}
			delete(want, ref.Codename)
		}
	}
	for codename := range want {
		t.Errorf("%s not found in the shipped monster refs", codename)
	}
}
