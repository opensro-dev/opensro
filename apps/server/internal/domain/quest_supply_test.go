/*
===========================================================================

quest_supply_test.go - detached quest supply authority

Snapshot edits must not consume or restore the live character's daily quota.

===========================================================================
*/
package domain

import "testing"

/*
================
TestQuestSupplySnapshotOwnsItsMap
================
*/
func TestQuestSupplySnapshotOwnsItsMap(t *testing.T) {
	const questID = 189
	source := &Character{QuestSupplies: map[uint32]QuestSupplyState{
		questID: {Day: 7, Pending: true},
	}}
	copy := cloneCharacter(source)
	copy.QuestSupplies[questID] = QuestSupplyState{Day: 8}
	if source.QuestSupplies[questID] != (QuestSupplyState{Day: 7, Pending: true}) {
		t.Fatal("snapshot changed the authoritative supply quota")
	}
	if cloneCharacter(&Character{}).QuestSupplies != nil {
		t.Fatal("legacy absence became an allocated supply map")
	}
}
