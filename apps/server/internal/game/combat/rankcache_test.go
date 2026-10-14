/*
===========================================================================

rankcache_test.go - tests for rankcache.go

The cache must be invisible: every answer equals a fresh learnedRankRows,
whatever writer changed the skill list, from any number of goroutines.

===========================================================================
*/

package combat

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
rankFixture

Two skill groups of three ranks each: 101..103 in group 1, 201..203 in
group 2, each rank a distinct passive so a wrong row is visible.
================
*/
func rankFixture() passiveSkills {
	skills := passiveSkills{}
	for group := uint32(1); group <= 2; group++ {
		for level := int64(1); level <= 3; level++ {
			id := group*100 + uint32(level)
			skills[id] = enterworld.SkillRow{ID: id, Group: group, Level: level,
				PassiveCritical: enterworld.SkillPassiveCritical{Pinned: true, Flat: uint32(id)}}
		}
	}
	return skills
}

/*
================
assertRankRowsFresh
================
*/
func assertRankRowsFresh(t *testing.T, step string, c *domain.Character, skills enterworld.SkillDataSource) {
	t.Helper()
	got := learnedRankRowsCached(c, skills)
	want := learnedRankRows(c, skills, nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: cached rows %v, fresh rows %v", step, rankRowIDs(got), rankRowIDs(want))
	}
}

/*
================
rankRowIDs
================
*/
func rankRowIDs(rows []enterworld.SkillRow) []uint32 {
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

/*
================
TestRankRowCacheFollowsEveryWriterOfTheSkillList

Each way the server changes c.Skills (character_lifecycle seeds it,
progression's learn appends or overwrites a rank in place, withdrawal
deletes and overwrites, a clone copies it, a reload brings another source):
the cached answer always equals a fresh one.
================
*/
func TestRankRowCacheFollowsEveryWriterOfTheSkillList(t *testing.T) {
	skills := rankFixture()
	c := &domain.Character{ID: 910001, Skills: []uint32{101, 201}}
	defer ForgetRankRows(c.ID)
	assertRankRowsFresh(t, "seeded", c, skills)
	assertRankRowsFresh(t, "repeat (hit)", c, skills)
	c.Skills = append(c.Skills, 102)
	assertRankRowsFresh(t, "learn appends", c, skills)
	c.Skills[0] = 103
	assertRankRowsFresh(t, "learn overwrites in place", c, skills)
	c.Skills = slices.Delete(c.Skills, 1, 2)
	assertRankRowsFresh(t, "withdraw deletes", c, skills)
	c.Skills[len(c.Skills)-1] = 202
	assertRankRowsFresh(t, "withdraw overwrites in place", c, skills)
	clone := *c
	clone.Skills = slices.Clone(c.Skills)
	assertRankRowsFresh(t, "clone", &clone, skills)
	other := rankFixture()
	other[103] = enterworld.SkillRow{ID: 103, Group: 1, Level: 3, PassiveCritical: enterworld.SkillPassiveCritical{Pinned: true, Flat: 9999}}
	assertRankRowsFresh(t, "another source", c, other)
	c.Skills = nil
	assertRankRowsFresh(t, "emptied", c, skills)
}

/*
================
TestRankRowCacheIsSafeForConcurrentReaders

Readers on many goroutines, each with its own snapshot of one character,
while a writer's snapshots keep changing the list (run under -race).
================
*/
func TestRankRowCacheIsSafeForConcurrentReaders(t *testing.T) {
	skills := rankFixture()
	const id = 910002
	defer ForgetRankRows(id)
	lists := [][]uint32{{101}, {101, 201}, {102, 202}, {103, 203}, {201}}
	var wait sync.WaitGroup
	errors := make(chan string, 64)
	for reader := range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range 200 {
				c := &domain.Character{ID: id, Skills: slices.Clone(lists[(reader+i)%len(lists)])}
				if got, want := learnedRankRowsCached(c, skills), learnedRankRows(c, skills, nil); !reflect.DeepEqual(got, want) {
					errors <- fmt.Sprintf("list %v: cached %v, fresh %v", c.Skills, rankRowIDs(got), rankRowIDs(want))
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for message := range errors {
		t.Fatal(message)
	}
}

/*
================
TestRankRowCacheRowsAreNeverMutatedByTheProjections

The cached rows are shared: running every projection that reads them must
leave them byte for byte as they were.
================
*/
func TestRankRowCacheRowsAreNeverMutatedByTheProjections(t *testing.T) {
	skills := rankFixture()
	c := &domain.Character{ID: 910003, Skills: []uint32{103, 202}, Level: pointer(30), Strength: pointer(40), Intellect: pointer(40), CurrentHP: pointer(100)}
	defer ForgetRankRows(c.ID)
	rows := learnedRankRowsCached(c, skills)
	before := slices.Clone(rows)
	if _, _, err := PlayerStats(c, Catalogs{Items: itemRefs{}, Skills: skills}); err != nil {
		t.Fatal(err)
	}
	learnedPassives(c, skills, itemRefs{}, 0)
	learnedStatusResistance(c, skills, itemRefs{})
	learnedDamageReturn(c, skills, itemRefs{})
	if !reflect.DeepEqual(rows, before) {
		t.Fatal("a projection mutated the shared cached rows")
	}
}

/*
================
TestRankRowCacheEvictsAndForgets
================
*/
func TestRankRowCacheEvictsAndForgets(t *testing.T) {
	skills := rankFixture()
	const first = 920000
	for i := range rankRowCacheMax + 1 {
		learnedRankRowsCached(&domain.Character{ID: first + int64(i), Skills: []uint32{101}}, skills)
	}
	rankRows.mu.Lock()
	_, evicted := rankRows.entries[first]
	_, kept := rankRows.entries[first+rankRowCacheMax]
	size := rankRows.order.Len()
	rankRows.mu.Unlock()
	if evicted || !kept || size > rankRowCacheMax {
		t.Fatalf("LRU: oldest kept=%v newest kept=%v size=%d (max %d)", evicted, kept, size, rankRowCacheMax)
	}
	ForgetRankRows(first + rankRowCacheMax)
	rankRows.mu.Lock()
	_, still := rankRows.entries[first+rankRowCacheMax]
	rankRows.mu.Unlock()
	if still {
		t.Fatal("ForgetRankRows kept the entry")
	}
	for i := range rankRowCacheMax + 1 {
		ForgetRankRows(first + int64(i))
	}
}
