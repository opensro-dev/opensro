/*
===========================================================================

hunting_guide_test.go - public scope and multiple-location regressions

===========================================================================
*/
package monster

import "testing"

/*
================
TestHuntingGuideFiltersAndKeepsAllLocations
================
*/
func TestHuntingGuideFiltersAndKeepsAllLocations(t *testing.T) {
	refs := map[uint32]MonsterRef{
		1: {RefObjID: 1, TypeID4: 1, Name: "Mangyang", NameStrID: "SN_MOB_CH_MANGNYANG", Level: 1},
		2: {RefObjID: 2, TypeID4: 4, Name: "Quest monster", Level: 1},
		3: {RefObjID: 3, TypeID4: 1, MonsterType: 3, Name: "Unique", Level: 20},
		4: {RefObjID: 4, TypeID4: 2, Name: "Job monster", Level: 10},
		5: {RefObjID: 5, TypeID4: 1, Structure: true, Name: "Structure", Level: 1},
		6: {RefObjID: 6, TypeID4: 1, Name: "Summon or admin only", Level: 1},
	}
	ordinary := NestRow{SpawnPoint: SpawnPoint{RefObjID: 1, RegionID: 0x619f, X: 100, Z: 200},
		WorldCode: "INS_DEFAULT", MaxCount: 1, RetailEvidence: true, PolicyPinned: true}
	nests := []NestRow{ordinary, ordinary}
	second := ordinary
	second.X = 400
	nests = append(nests, second)
	for _, change := range []func(*NestRow){
		func(n *NestRow) { n.MaxCount = 0 },
		func(n *NestRow) { n.HiveKey, n.HiveMaxCount = "disabled", 0 },
		func(n *NestRow) { n.WorldCode = "INS_FORT_JANGAN" },
		func(n *NestRow) { n.RegionID = 0x8001 },
		func(n *NestRow) { n.EventStructID = 1 },
		func(n *NestRow) { n.StartVacant = true },
		func(n *NestRow) { n.RetailEvidence = false },
		func(n *NestRow) { n.RefObjID = 2 },
		func(n *NestRow) { n.RefObjID = 3 },
		func(n *NestRow) { n.RefObjID = 4 },
		func(n *NestRow) { n.RefObjID = 5 },
	} {
		nest := ordinary
		nest.X = 900
		change(&nest)
		nests = append(nests, nest)
	}
	guide := (Template{Refs: refs, Nests: nests, SummonRefs: []uint32{6}}).HuntingGuide()
	if len(guide) != 1 || guide[0].RefObjID != 1 || guide[0].Name != "Mangyang" || guide[0].Level != 1 ||
		len(guide[0].Points) != 2 || guide[0].Points[0].X != 100 || guide[0].Points[1].X != 400 {
		t.Fatalf("public hunting guide lost locations or admitted special populations: %#v", guide)
	}
}
