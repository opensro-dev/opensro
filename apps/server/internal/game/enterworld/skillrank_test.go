/*
===========================================================================

skillrank_test.go - exact rank lookup excludes linked stages and ambiguity

A restoration request must not choose whichever duplicate a map happens
to visit first. Test real compact storage and the public lookup boundary.

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestLearnedRankIndexRejectsAmbiguousRoots
================
*/
func TestLearnedRankIndexRejectsAmbiguousRoots(t *testing.T) {
	data := &TextdataSkills{}
	data.once.Do(func() {})
	for _, row := range []SkillRow{
		{ID: 100, Group: 10, Level: 1},
		{ID: 101, Group: 10, Level: 1, ChainSub: true},
		{ID: 102, Group: 10, Level: 2},
		{ID: 103, Group: 10, Level: 2},
		{ID: 104, Group: 10, Level: 3, ChainSub: true},
	} {
		data.rows.set(row.ID, row)
	}
	data.indexLearnedRanks()
	root, ok := data.SkillByGroupLevel(10, 1)
	if !ok || root.ID != 100 {
		t.Fatalf("linked stage displaced root: %+v %v", root, ok)
	}
	for _, rank := range []int64{0, 2, 3, 4} {
		if row, ok := data.SkillByGroupLevel(10, rank); ok {
			t.Fatalf("invalid rank %d resolved to %+v", rank, row)
		}
	}
}
