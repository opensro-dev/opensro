/*
===========================================================================

skillrank.go - resolve a learned root at an exact rank

Training and withdrawal share the same authored group ladder. Linked attack
stages are never learned roots, even when they share the group's rank.

===========================================================================
*/
package enterworld

/*
================
skillRankKey

Group and rank identify learned roots independently of linked cast stages.
================
*/
type skillRankKey struct {
	group uint32
	level int64
}

/*
================
indexLearnedRanks

Build at catalog load, outside gameplay transactions. Zero marks ambiguity
so map order can never decide which root a restoration request receives.
================
*/
func (t *TextdataSkills) indexLearnedRanks() {
	t.byRank = make(map[skillRankKey]uint32)
	for _, row := range t.rows.values() {
		if row.ChainSub {
			continue
		}
		key := skillRankKey{row.Group, row.Level}
		if _, exists := t.byRank[key]; exists {
			t.byRank[key] = 0
		} else {
			t.byRank[key] = row.ID
		}
	}
}

/*
================
SkillByGroupLevel

Native 59FA03 follows the previous-rank chain. The port resolves the same
root from the catalog; an ambiguous ladder refuses instead of picking an
arbitrary map iteration result.
================
*/
func (t *TextdataSkills) SkillByGroupLevel(group uint32, level int64) (SkillRow, bool) {
	t.once.Do(t.load)
	id := t.byRank[skillRankKey{group, level}]
	if id == 0 {
		return SkillRow{}, false
	}
	return t.rows.lookup(id)
}
