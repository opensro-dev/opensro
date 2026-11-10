/*
===========================================================================

pool_test.go - combat eligibility and the installed encounter closure

===========================================================================
*/
package sookarena

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"
)

/*
================
TestPoolRejectsNonCombatActors
================
*/
func TestPoolRejectsNonCombatActors(t *testing.T) {
	ref := monster.MonsterRef{RefObjID: 1, Codename: "MOB_TEST", Level: 10, MaxHP: 100, ExpToGive: 100, CombatPinned: true, RewardActionPinned: true, RunSpeed: 20, ModelPath: "char/test.bsr", DefaultSkillIDs: [10]uint32{1}}
	refs := []monster.MonsterRef{ref}
	for _, change := range []func(*monster.MonsterRef){func(r *monster.MonsterRef) { r.TrainingDummy = true }, func(r *monster.MonsterRef) { r.Structure = true }, func(r *monster.MonsterRef) { r.ExpToGive = 0 }, func(r *monster.MonsterRef) { r.RunSpeed = 0 }, func(r *monster.MonsterRef) { r.DefaultSkillIDs = [10]uint32{} }, func(r *monster.MonsterRef) { r.ModelPath = "" }} {
		bad := ref
		change(&bad)
		refs = append(refs, bad)
	}
	pool := BuildPool(refs)
	if len(pool.Normal) != 1 || len(pool.Normals(10)) != 1 || len(pool.Normals(20)) != 1 || len(pool.Normals(9)) != 0 {
		t.Fatal("eligibility or level ceiling")
	}
}

/*
================
TestPoolExcludesCTFMonsters
================
*/
func TestPoolExcludesCTFMonsters(t *testing.T) {
	ref := monster.MonsterRef{RefObjID: 1, Codename: "MOB_CH_TEST", Level: 85, MaxHP: 100, ExpToGive: 100, CombatPinned: true, RewardActionPinned: true, RunSpeed: 20, ModelPath: "char/test.bsr", DefaultSkillIDs: [10]uint32{1}}
	refs := []monster.MonsterRef{ref}
	for _, code := range []string{"MOB_EV_WOLF", "MOB_EV_RAVEN", "MOB_EV_PENGUIN", "MOB_EV_KANGAROO", "MOB_EV_JINN", "MOB_QT_01_ROCKY_CLON2"} {
		for _, kind := range []uint8{0, 3} {
			event := ref
			event.Codename, event.MonsterType = code, kind
			refs = append(refs, event)
		}
	}
	pool := BuildPool(refs)
	if len(pool.Normal) != 1 || pool.Normal[0].Codename != ref.Codename || len(pool.Unique) != 0 {
		t.Fatal("CTF references entered the survival encounter pool")
	}
}

/*
================
TestInstalledPoolHasEncountersAcrossNativeLevels
================
*/
func TestInstalledPoolHasEncountersAcrossNativeLevels(t *testing.T) {
	pool := BuildPool(monster.LoadTemplate(gamedatatest.TextdataDir(t)).CreatableRefs())
	for _, level := range []int{1, 20, 40, 60, 80, 90} {
		if len(pool.Normals(level)) == 0 {
			t.Fatalf("level %d has no encounters", level)
		}
		boss, ok := pool.Boss(level)
		if (level >= 20 && !ok) || (ok && int(boss.Level) > level) {
			t.Fatalf("level %d invalid boss %+v", level, boss)
		}
	}
	for _, playerLevel := range []int{20, 40, 60, 80, 90} {
		for stage := 0; stage < 5; stage++ {
			low, target := StageBounds(playerLevel, stage)
			refs := pool.StageNormals(playerLevel, stage)
			if len(refs) == 0 {
				t.Fatalf("player level=%d stage=%d no encounters", playerLevel, stage)
			}
			for _, ref := range refs {
				if int(ref.Level) > target || int(ref.Level) < low {
					t.Fatalf("level %d exceeds encounter target %d", ref.Level, target)
				}
			}
		}
	}
	t.Logf("eligible normal=%d unique=%d", len(pool.Normal), len(pool.Unique))
}

/*
================
TestArenaTombPoolUsesRegularSpeciesAndStageBounds
================
*/
func TestArenaTombPoolUsesRegularSpeciesAndStageBounds(t *testing.T) {
	seed := monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: {RefObjID: 1}}, nil)
	refs, err := seed.WithQinShiArenaRefs("")
	if err != nil {
		t.Fatal(err)
	}
	pool := BuildPool(refs.CreatableRefs())
	if len(pool.Normal) != 30 || len(pool.Unique) != 0 {
		t.Fatalf("normal=%d unique=%d", len(pool.Normal), len(pool.Unique))
	}
	low, high := 1000, 0
	for _, ref := range pool.Normal {
		low, high = min(low, int(ref.Level)), max(high, int(ref.Level))
	}
	if low != 81 || high != 100 {
		t.Fatalf("cave levels %d..%d", low, high)
	}
	for stage := range 5 {
		low, high := StageBounds(90, stage)
		selected := pool.StageNormals(90, stage)
		if len(selected) == 0 {
			t.Fatalf("stage %d empty", stage+1)
		}
		for _, ref := range selected {
			if int(ref.Level) < low || int(ref.Level) > high {
				t.Fatal("cave monster exceeds stage bounds")
			}
		}
	}
}
