/*
===========================================================================

summon_tactics_controls_test.go - summoned monsters get whole tactics rows

===========================================================================
*/

package monster

import "testing"

/*
================
TestSummonedYetiCarriesItsWholeTacticsRow

Isyutaru summons MOB_KK_YETI at grade 0 (normal list) and grades 4/6 (the
other list). The v1.150 rows are 111 (ChampionTacticsID 110, passive) and
110 (aggressive, help refused, trace 500).
================
*/
func TestSummonedYetiCarriesItsWholeTacticsRow(t *testing.T) {
	ref := MonsterRef{Codename: "MOB_KK_YETI", BodyRadius: 5}
	never := func() float64 { return 0 }
	normal, ok := ResolveSummonTactics(ref, 0, never)
	if !ok || !normal.HasControls || normal.Controls.ID != 111 || normal.Controls.AggressType != 1 {
		t.Fatalf("grade 0 row: %+v", normal.Controls)
	}
	giant, ok := ResolveSummonTactics(ref, 4, never)
	if !ok || !giant.HasControls || giant.Controls.ID != 110 || giant.Controls.AggressType != 0 ||
		giant.Controls.HelpResponse != 2 || giant.Controls.TraceData != 500 {
		t.Fatalf("grade 4 row: %+v", giant.Controls)
	}
	if giant.SightRange != 120 || giant.TargetPolicy != 2 {
		t.Fatalf("evidence columns changed: sight %v policy %d", giant.SightRange, giant.TargetPolicy)
	}
}

/*
================
TestSummonTacticsJoinKeepsEvidenceWithoutAWholeRow

A reference with no v1.150 row keeps its evidence-only row, and a list
whose counts differ is not joined at all.
================
*/
func TestSummonTacticsJoinKeepsEvidenceWithoutAWholeRow(t *testing.T) {
	if row, ok := ResolveSummonTactics(MonsterRef{Codename: "MOB_RM_ROCKY_CLON2"}, 0, func() float64 { return 0 }); !ok || row.HasControls {
		t.Fatalf("a reference without a v1.150 row gained controls: %+v", row)
	}
	out := map[string][2][]SummonTactics{"MOB_X": {{{SightRange: 100, TargetPolicy: 2}}, nil}}
	policies := map[string][]TacticsControls{"MOB_X": {
		{ID: 1, ChampionID: 2, SightRange: 100, ChangeTarget: 2},
		{ID: 2, SightRange: 115, ChangeTarget: 2},
		{ID: 3, ChampionID: 2, SightRange: 100, ChangeTarget: 2},
	}}
	attachSummonTacticsControls(out, policies)
	if out["MOB_X"][0][0].HasControls {
		t.Fatal("a list with more v1.150 rows than evidence rows was joined")
	}
	// The other list holds a row the evidence does not list: not joined.
	policies["MOB_X"] = policies["MOB_X"][:2]
	attachSummonTacticsControls(out, policies)
	if out["MOB_X"][0][0].HasControls {
		t.Fatal("a reference with an unlisted v1.150 row was joined")
	}
	policies["MOB_X"] = policies["MOB_X"][:1]
	attachSummonTacticsControls(out, policies)
	if row := out["MOB_X"][0][0]; !row.HasControls || row.Controls.ID != 1 {
		t.Fatalf("the exact list was not joined: %+v", row)
	}
}

/*
================
TestSummonTacticsJoinCoverage

Pins how many summoned references gain whole rows, so a data change that
silently drops the join shows up here.
================
*/
func TestSummonTacticsJoinCoverage(t *testing.T) {
	joined := 0
	for _, groups := range summonTacticsByCode {
		for _, rows := range groups {
			if len(rows) > 0 && rows[0].HasControls {
				joined++
				break
			}
		}
	}
	t.Logf("%d references carry whole rows", joined)
	if joined == 0 {
		t.Fatal("no summoned reference carries a whole tactics row")
	}
}
