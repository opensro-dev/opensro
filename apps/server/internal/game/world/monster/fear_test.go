/*
===========================================================================

fear_test.go - the feared source is excluded from target selection

===========================================================================
*/

package monster

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
)

/*
================
TestFearsOnlyTheActiveSource

Only the caster recorded in an active Fear slot is feared; other players
and an expired slot leave selection untouched.
================
*/
func TestFearsOnlyTheActiveSource(t *testing.T) {
	var block abnormal.Block
	block.Slots[abnormal.Fear].Active = true
	block.Slots[abnormal.Fear].SourceGID = 7
	feared := Instance{Abnormal: &block}
	if !feared.Fears(7) || feared.Fears(8) || feared.Fears(0) {
		t.Fatal("fear must name exactly its source")
	}
	block.Slots[abnormal.Fear].Active = false
	if feared.Fears(7) || (Instance{}).Fears(7) {
		t.Fatal("inactive or absent fear excluded a target")
	}
}
