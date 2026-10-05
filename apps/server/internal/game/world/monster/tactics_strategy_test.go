/*
===========================================================================

tactics_strategy_test.go - the strategy choices against the shipped rows

===========================================================================
*/

package monster

import "testing"

/*
================
TestShippedTacticsUseOnlyFixedAggression

53F4D0 copies AggressType to +0x168 and, for type 2, replaces it with
rand()%100 >= AggressData; acquisition then runs for 0 and 3 only (5478F0,
547AD0). The population evidence carries type 0 as Aggressive, which holds
while every shipped row is type 0 or 1. A row of type 2 or 3 needs the
per-activation roll first.
================
*/
func TestShippedTacticsUseOnlyFixedAggression(t *testing.T) {
	for key, pair := range populationTacticsControls {
		rows := []TacticsControls{pair.Normal}
		if pair.HasChampion {
			rows = append(rows, pair.Champion)
		}
		for _, row := range rows {
			if row.AggressType > 1 {
				t.Fatalf("%v: tactics %d has AggressType %d", key, row.ID, row.AggressType)
			}
		}
	}
}

/*
================
TestFixedQueryRowsAreTheThreeUniques

The IDs 53FC00 compares belong to Tiger Girl, Uruchi and Isyutaru in the
shipped rows, and none of them carries a redirect flag.
================
*/
func TestFixedQueryRowsAreTheThreeUniques(t *testing.T) {
	want := map[string]bool{"MOB_CH_TIGERWOMAN": true, "MOB_OA_URUCHI": true, "MOB_KK_ISYUTARU": true}
	seen := map[string]bool{}
	for key, pair := range populationTacticsControls {
		if pair.Normal.FixedQuery() {
			if !want[key.Codename] {
				t.Fatalf("%s runs the fixed query", key.Codename)
			}
			seen[key.Codename] = true
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("fixed-query uniques %v", seen)
	}
}
