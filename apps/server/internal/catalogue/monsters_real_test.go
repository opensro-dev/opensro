/*
===========================================================================

monsters_real_test.go - the catalogue's monsters against the shard's data

Owner report (2026-10-13): the site listed retail rows nothing spawns, the
_L2/_L3 incarnations of the uniques, as MISSING monsters. Against the
licensed game data, every listed monster now has a spawn source and the
uniques' unplaced incarnations are gone.

===========================================================================
*/
package catalogue

import (
	"sort"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// realUniques are the uniques the shard places, by codename.
var realUniques = []string{"MOB_CH_TIGERWOMAN", "MOB_OA_URUCHI", "MOB_KK_ISYUTARU", "MOB_TK_BONELORD",
	"MOB_RM_TAHOMET", "MOB_EU_KERBEROS", "MOB_AM_IVY"}

/*
================
TestEveryListedMonsterHasASpawnSource
================
*/
func TestEveryListedMonsterHasASpawnSource(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	source := MonsterSource{Template: monster.LoadTemplate(dir), ItemID: func(string) uint32 { return 0 },
		Skills: enterworld.NewTextdataSkills(dir)}
	produced, err := producibleMonsters(source)
	if err != nil {
		t.Fatal(err)
	}
	monsters, err := BuildMonsters(source)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]Monster{}
	for _, m := range monsters {
		listed[m.Codename] = m
		if produced[m.ID] == "" || m.Status == StatusMissing {
			t.Errorf("%s (%d) is listed without a spawn source", m.Codename, m.ID)
		}
	}
	for _, codename := range realUniques {
		m, ok := listed[codename]
		if !ok || m.Unique == nil {
			t.Errorf("unique %s is not listed", codename)
			continue
		}
		t.Logf("%s (%d, %s): admitted by %s, regions %v", codename, m.ID, m.Name, produced[m.ID], m.Regions)
		for _, suffix := range []string{"_L2", "_L3"} {
			if _, ok := listed[codename+suffix]; ok {
				t.Errorf("%s%s is listed, but nothing spawns it", codename, suffix)
			}
		}
	}
	counts := map[string]int{}
	for _, m := range monsters {
		counts[produced[m.ID]]++
	}
	var names []string
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Logf("listed by %s: %d", name, counts[name])
	}
}
