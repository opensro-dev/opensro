/*
===========================================================================

spawnreplacement_test.go - the special-target replace table at a nest
spawn

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// replacementRef is the replace row's monster in these tests.
const replacementRef = 2

/*
================
replacementWorld

A champion-promoting nest whose every draw is word: 16300 promotes to
champion (16300 % 101 = 39, above the giant ratio) and its replace roll
is 49.7 of 100.
================
*/
func replacementWorld(t *testing.T, word uint32, table func(int64) []monster.SpawnReplacement) *lifecycleWorld {
	t.Helper()
	nest := lifecycleNest(100)
	nest.HasChampionTactics = true
	nest.ChampionGenPercentage = 100
	w := &lifecycleWorld{t: t, now: lifecycleBase}
	w.s = NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: lifecycleRef(1), replacementRef: replacementMonster()}, []monster.NestRow{nest}))
	w.s.SetTimeSource(func() time.Time { return w.now })
	w.s.SetRandomSource(constantWord(word))
	w.s.SetSpawnReplacements(table)
	return w
}

/*
================
replacementMonster
================
*/
func replacementMonster() monster.MonsterRef {
	ref := lifecycleRef(1)
	ref.RefObjID = replacementRef
	ref.Codename = "MOB_EV_WINTER_SNOWMAN"
	return ref
}

/*
================
TestSpawnReplacementTurnsAChampionIntoTheRowMonster

The open world's champion becomes the row's monster at the ordinary
grade; above the roll, or without a table, it stays the nest's champion.
================
*/
func TestSpawnReplacementTurnsAChampionIntoTheRowMonster(t *testing.T) {
	snowman := replacementMonster()
	table := func(int64) []monster.SpawnReplacement {
		return []monster.SpawnReplacement{{Ref: snowman, Grade: 1, NewGrade: 0, RatePercent: 50}}
	}
	live := replacementWorld(t, 16300, table).expectLive(0, 1)
	if live[0].Ref.RefObjID != replacementRef || live[0].Rarity()&0x0f != 0 {
		t.Fatalf("replaced spawn = ref %d grade %#02x, want ref %d at grade 0", live[0].Ref.RefObjID, live[0].Rarity(), replacementRef)
	}
	// 16400 % 101 = 38 is still a champion, but its roll is 50.05.
	for name, w := range map[string]*lifecycleWorld{
		"above the roll":  replacementWorld(t, 16400, table),
		"no table":        replacementWorld(t, 16300, nil),
		"an empty window": replacementWorld(t, 16300, func(int64) []monster.SpawnReplacement { return nil }),
	} {
		live := w.expectLive(0, 1)
		if live[0].Ref.RefObjID != 1 || live[0].Rarity()&0x0f != 1 {
			t.Fatalf("%s: spawn = ref %d grade %#02x, want the nest's champion", name, live[0].Ref.RefObjID, live[0].Rarity())
		}
	}
}
