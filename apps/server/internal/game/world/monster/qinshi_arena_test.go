/*
===========================================================================
qinshi_arena_test.go - arena cave references never enable public cave spawns.
===========================================================================
*/
package monster

import (
	"reflect"
	"testing"
)

/*
================
TestQinShiArenaRefsKeepOriginalPopulation
================
*/
func TestQinShiArenaRefsKeepOriginalPopulation(t *testing.T) {
	seed := TemplateFromParts(map[uint32]MonsterRef{1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG"}}, []NestRow{{SpawnPoint: SpawnPoint{RefObjID: 1933, RegionID: 23687}}})
	seed.SummonRefs, seed.EvidenceMatches = []uint32{1933}, 1
	got, err := seed.WithQinShiArenaRefs("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Refs) != len(seed.Refs)+78 || len(seed.Refs) != 1 {
		t.Fatal("missing cave references or input mutation")
	}
	if !reflect.DeepEqual(got.Nests, seed.Nests) || !reflect.DeepEqual(got.SummonRefs, seed.SummonRefs) || got.EvidenceMatches != seed.EvidenceMatches {
		t.Fatal("arena reference import changed public population")
	}
	if len(got.NestIndexesInRegion(0x8007)) != 0 {
		t.Fatal("cave nests enabled")
	}
	if _, err := got.WithQinShiArenaRefs(""); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
}
