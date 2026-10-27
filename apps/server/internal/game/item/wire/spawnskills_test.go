/*
===========================================================================

spawnskills_test.go - the active effect list on a peer spawn row

===========================================================================
*/

package wire

import (
	"reflect"
	"testing"
)

/*
================
TestPeerSpawnRowCarriesActiveEffects

A buffed peer's row lists its effects in the 85FB20 shape: the token only
for a skill that has one, the status byte only for a skill that has one.
================
*/
func TestPeerSpawnRowCarriesActiveEffects(t *testing.T) {
	effects := []SpawnSkillEntry{
		{SkillID: 1201, Token: 77, HasToken: true, Status: 2, HasStatus: true},
		{SkillID: 1202, Token: 78, HasToken: true},
		{SkillID: 1203},
	}
	row := PlayerSpawnRow{Name: "Peer", Gid: 9, ScaleDenom: 100, SpawnSkills: effects}
	shape := func(id uint32) (bool, bool) { return id != 1203, id == 1201 }
	decoded, err := DecodePlayerSpawnRowWithSkills(row.Encode(), nil, false, shape)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.SpawnSkills, effects) || decoded.Name != "Peer" {
		t.Fatalf("effects = %+v name %q", decoded.SpawnSkills, decoded.Name)
	}
	if _, err := DecodePlayerSpawnRow(row.Encode(), nil, false); err == nil {
		t.Fatal("a row with effects decoded without their skill shape")
	}
}
