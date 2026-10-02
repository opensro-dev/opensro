/*
===========================================================================

pk_test.go - persistence and snapshot isolation for player relations

===========================================================================
*/
package domain

import (
	"encoding/json"
	"testing"
)

/*
================
TestCriminalRecordSurvivesReloadWithoutResurrectingEventOrAggression
================
*/
func TestCriminalRecordSurvivesReloadWithoutResurrectingEventOrAggression(t *testing.T) {
	c := &Character{PK: &PKRecord{DailyCount: 3, TotalCount: 5, Penalty: 3600}, Aggressions: map[uint32]uint32{100003: 20}, EventMembership: &EventMembership{ID: 17, Team: 0}}
	if c.PVPState() != 2 || c.EventTeam() != 0 {
		t.Fatal("active state projection")
	}
	blob, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Character
	if err := json.Unmarshal(blob, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.PK == nil || *loaded.PK != *c.PK || loaded.PVPState() != 2 || loaded.EventTeam() != 255 || len(loaded.Aggressions) != 0 {
		t.Fatalf("reload: %+v", loaded)
	}
	loaded.PK.Penalty = 0
	if loaded.PVPState() != 0 {
		t.Fatal("counts alone do not imply active penalty")
	}
}

/*
================
TestNameAuthoritySnapshotDetachesAllMutableInputs
================
*/
func TestNameAuthoritySnapshotDetachesAllMutableInputs(t *testing.T) {
	c := &Character{PK: &PKRecord{Penalty: 1200}, Aggressions: map[uint32]uint32{100003: 20}, EventMembership: &EventMembership{ID: 17, Team: 0}}
	snapshot := cloneCharacter(c)
	c.PK.Penalty = 0
	c.Aggressions[100003] = 1
	c.EventMembership.Team = 1
	if snapshot.PVPState() != 2 || snapshot.EventTeam() != 0 || snapshot.Aggressions[100003] != 20 {
		t.Fatal("snapshot aliases live authority")
	}
	if c.PVPState() != 0 {
		t.Fatal("last aggression tick must clear grey before target protection")
	}
	delete(c.Aggressions, 100003)
	if c.PVPState() != 0 {
		t.Fatal("expired aggression is neutral")
	}
}
