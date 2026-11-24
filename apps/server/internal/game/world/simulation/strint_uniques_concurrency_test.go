/*
===========================================================================

strint_uniques_concurrency_test.go - the three builds share the ground

Pins the port-only rule SRO_STR_INT_UNIQUES adds to the population: the
base and its STR and INT copies each keep the species' whole anchor list in
a hive of their own, so all three can be alive at once, and a spawn takes
only a point no other copy holds, so two of them never stand together.

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// strIntAnchorXs are the species' three shared spawn points.
var strIntAnchorXs = []float64{10, 20, 30}

// strIntConcurrentRef is one build of the species.
func strIntConcurrentRef(id uint32, build monster.UniqueBuild) monster.MonsterRef {
	codename := map[monster.UniqueBuild]string{
		monster.UniqueBuildAny: "MOB_CONCURRENT",
		monster.UniqueBuildSTR: "MOB_CONCURRENT_STR",
		monster.UniqueBuildINT: "MOB_CONCURRENT_INT",
	}[build]
	return monster.MonsterRef{
		RefObjID: id, TidWord: 0x00C6, Codename: codename,
		MonsterType: 3, MaxHP: 100, ScaleDenom: 100, UniqueBuild: build,
	}
}

// strIntConcurrentNest is one anchor of one build, in that build's hive.
func strIntConcurrentNest(refID uint32, hive string, x float64) monster.NestRow {
	return monster.NestRow{
		SpawnPoint:         monster.SpawnPoint{RefObjID: refID, RegionID: lifecycleRegion, X: x, Y: 10, Z: 100},
		RetailEvidence:     true,
		PolicyPinned:       true,
		MaxCount:           1,
		Respawn:            true,
		RespawnDelayMinSec: 10,
		RespawnDelayMaxSec: 10,
		HiveKey:            hive,
		HiveMaxCount:       1,
		UniqueShare:        "MOB_CONCURRENT",
	}
}

/*
================
strIntConcurrentWorld

Three builds, the same three anchors each, three hives - the shape the
append gives a species.
================
*/
func strIntConcurrentWorld(t *testing.T) *lifecycleWorld {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1: strIntConcurrentRef(1, monster.UniqueBuildAny),
		2: strIntConcurrentRef(2, monster.UniqueBuildSTR),
		3: strIntConcurrentRef(3, monster.UniqueBuildINT),
	}
	type build struct {
		id   uint32
		hive string
	}
	var nests []monster.NestRow
	for _, spec := range []build{{1, "share"}, {2, "share:_STR"}, {3, "share:_INT"}} {
		for _, x := range strIntAnchorXs {
			nests = append(nests, strIntConcurrentNest(spec.id, spec.hive, x))
		}
	}
	w := &lifecycleWorld{t: t, now: lifecycleBase}
	w.s = NewMonsterState(monster.TemplateFromParts(refs, nests))
	w.s.SetTimeSource(func() time.Time { return w.now })
	w.s.SetRandomSource(constantWord(16384))
	return w
}

/*
================
TestStrIntBuildsStandTogetherOnSeparatePoints

All three builds are up at once on the species' shared anchor list, and no
two of them stand on the same point.
================
*/
func TestStrIntBuildsStandTogetherOnSeparatePoints(t *testing.T) {
	w := strIntConcurrentWorld(t)
	live := w.expectLive(0, 3)
	builds := map[monster.UniqueBuild]bool{}
	points := map[float64]bool{}
	for _, instance := range live {
		builds[instance.Ref.UniqueBuild] = true
		points[instance.Spawn.X] = true
	}
	for _, want := range []monster.UniqueBuild{monster.UniqueBuildAny, monster.UniqueBuildSTR, monster.UniqueBuildINT} {
		if !builds[want] {
			t.Fatalf("build %d is not up with the others", want)
		}
	}
	if len(points) != len(live) {
		t.Fatalf("points = %v, want one per build", points)
	}
}

/*
================
TestStrIntRespawnAvoidsAnOccupiedPoint

A copy that dies comes back only on a point no other copy holds: with the
other two standing, only the freed point is left for it.
================
*/
func TestStrIntRespawnAvoidsAnOccupiedPoint(t *testing.T) {
	w := strIntConcurrentWorld(t)
	live := w.expectLive(0, 3)
	held := map[float64]bool{}
	var freed float64
	for _, instance := range live {
		if instance.Ref.UniqueBuild == monster.UniqueBuildSTR {
			freed = instance.Spawn.X
			continue
		}
		held[instance.Spawn.X] = true
	}
	for _, instance := range live {
		if instance.Ref.UniqueBuild == monster.UniqueBuildSTR {
			w.kill(0, instance.Gid)
		}
	}
	w.expectLive(0, 2)
	refilled := w.expectLive(60*1000, 3)
	for _, instance := range refilled {
		if instance.Ref.UniqueBuild != monster.UniqueBuildSTR {
			continue
		}
		if instance.Spawn.X != freed {
			t.Fatalf("the STR copy returned to x=%v, want the freed point %v", instance.Spawn.X, freed)
		}
		if held[instance.Spawn.X] {
			t.Fatalf("the STR copy stands on a point another copy holds")
		}
		return
	}
	t.Fatal("the STR copy did not return")
}

/*
================
TestStrIntAnchorFreesOnDeath

A point is released when its unique dies, so the same point is available
again: killing the base frees its point, and the base's own hive takes it
back because the other two hold the rest.
================
*/
func TestStrIntAnchorFreesOnDeath(t *testing.T) {
	w := strIntConcurrentWorld(t)
	live := w.expectLive(0, 3)
	var baseGid uint32
	var basePoint float64
	for _, instance := range live {
		if instance.Ref.UniqueBuild == monster.UniqueBuildAny {
			baseGid, basePoint = instance.Gid, instance.Spawn.X
		}
	}
	w.kill(0, baseGid)
	refilled := w.expectLive(60*1000, 3)
	for _, instance := range refilled {
		if instance.Ref.UniqueBuild == monster.UniqueBuildAny {
			if instance.Spawn.X != basePoint {
				t.Fatalf("the base returned to x=%v, want its freed point %v", instance.Spawn.X, basePoint)
			}
			return
		}
	}
	t.Fatal("the base did not return")
}
