/*
===========================================================================

structure_refs_test.go - fortress structures' repair and revive fields

===========================================================================
*/
package monster

import (
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestStructureRefsCarryRepairAndRevive

_RefObjCommon columns 23, 27 and 28 on the shipped structures: the Jangan
fort stone's CostRepair is its 187200, no structure authors a CostRevive,
and 42 of the 52 structures the loader serves may be revived. (The raw
file holds 66 STRUCTURE_ rows, 44 revivable; the loader drops the records
without hit points, the unbuilt _00 sites, and the pulleys, which are
NPCs.)
================
*/
func TestStructureRefsCarryRepairAndRevive(t *testing.T) {
	refs := LoadMonsterRefs(gamedatatest.TextdataDir(t))
	stone, ok := refs[19553]
	if !ok || !stone.Structure || stone.CostRepair != 187200 {
		t.Fatalf("Jangan fort stone %+v", stone)
	}
	structures, revivable := 0, 0
	for _, ref := range refs {
		if !ref.Structure || !strings.HasPrefix(ref.Codename, "STRUCTURE_") {
			continue
		}
		structures++
		if ref.CanRevive {
			revivable++
		}
		if ref.CostRevive != 0 {
			t.Errorf("%s authors CostRevive %d", ref.Codename, ref.CostRevive)
		}
	}
	if structures != 52 || revivable != 42 {
		t.Fatalf("%d structures, %d revivable; want 52 and 42", structures, revivable)
	}
}
