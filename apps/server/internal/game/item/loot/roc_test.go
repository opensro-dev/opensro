/*
===========================================================================

roc_test.go - the Roc rewards join after the cap110 overlay and only when on

===========================================================================
*/

package loot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

/*
================
rocRewardsSource

The Roc's group 12 with one installed piece, in the Roc group range.
================
*/
func rocRewardsSource() cap110Rewards {
	return cap110Rewards{Version: 1,
		Random: []assignedRandom{{Monster: "MOB_RM_ROC", Group: 2000012, Min: 2, Max: 3, Probability: 1}},
		Groups: map[int][]groupDrop{2000012: {{Codename: "ITEM_CH_BLADE_11_A", Probability: 0.01}}}}
}

/*
================
writeRewards
================
*/
func writeRewards(t *testing.T, dir, name string, source cap110Rewards) {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestRocRewardsJoinOnlyWhenOn

Off, a written roc-rewards.txt changes nothing. On, the Roc rolls its
group beside the cap110 overlay's monster, whose rule stays; a missing
file leaves the catalogue as it was, and a file that names a monster the
cap110 overlay owns refuses.
================
*/
func TestRocRewardsJoinOnlyWhenOn(t *testing.T) {
	before := consumables
	t.Cleanup(func() { consumables = before })
	dir := t.TempDir()
	cap110, err := json.Marshal(rewardsSource())
	if err != nil {
		t.Fatal(err)
	}
	base, err := compileCap110Rewards(nativeConsumables, cap110)
	if err != nil {
		t.Fatal(err)
	}
	all := func() (uint32, error) { return 0, nil }

	consumables = base
	if err := InitRoc(dir, true); err != nil || len(consumables.random) != len(base.random) {
		t.Fatalf("a missing file changed the catalogue: %v", err)
	}
	writeRewards(t, dir, rocRewardsFile, rocRewardsSource())
	if err := InitRoc(dir, false); err != nil || len(AssignedDrops("MOB_RM_ROC", 8, all)) != 0 {
		t.Fatalf("off joined the Roc rewards: %v", err)
	}
	if err := InitRoc(dir, true); err != nil {
		t.Fatal(err)
	}
	if got := AssignedDrops("MOB_RM_ROC", 8, all); len(got) != 2 || got[0].Codename != "ITEM_CH_BLADE_11_A" {
		t.Fatalf("the Roc's group: %+v", got)
	}
	if len(consumables.random["MOB_SD_TEST_BOSS"]) != 1 {
		t.Fatal("the cap110 overlay's rule was lost")
	}

	consumables = base
	clash := rocRewardsSource()
	clash.Random[0].Monster = "MOB_SD_TEST_BOSS"
	writeRewards(t, dir, rocRewardsFile, clash)
	if err := InitRoc(dir, true); err == nil {
		t.Fatal("a Roc rule redefined a cap110 monster")
	}
}
