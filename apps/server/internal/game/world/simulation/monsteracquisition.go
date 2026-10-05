/*
===========================================================================

monsteracquisition.go - native acquisition ordering and target lookup

===========================================================================
*/

package simulation

import (
	"math"
	"opensro.online/server/internal/game/abnormal"
	"sort"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// ordinaryPlayerAcquisition is the outdoor, player-only projection of selector
// 2 (5464E0) in the v1.188 research server. Special tactics selectors and dungeon
// cell enumeration remain outside this projection. The independent observer
// and hostility checks precede ranking; companion selection remains open.
/*
================
ordinaryPlayerAcquisition
================
*/
func ordinaryPlayerAcquisition(actor monster.Instance, from monster.Pose, players []playerPose, sightRange float64) (playerPose, bool) {
	if len(players) == 0 {
		return playerPose{}, false
	}
	sightFloat := float32(sightRange) // tactics+15C, then truncating conversion
	if math.IsNaN(float64(sightFloat)) || sightFloat < 0 || sightFloat > 1920 {
		return playerPose{}, false // outside the native query's admitted range
	}
	sight := uint32(sightFloat)
	blocks := acquisitionBlocks(from, sight)
	type candidate struct {
		player playerPose
		block  int
	}
	ordered := make([]candidate, 0, len(players))
	for _, player := range players {
		if player.OwnerGid != 0 {
			continue // a companion is weighed with its owner (5464E0)
		}
		block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
			RegionID: player.Pose.RegionID, X: float64(float32(player.Pose.X)), Z: float64(float32(player.Pose.Z)),
		})
		if order, exists := blocks.order(block); exists && player.Gid != 0 &&
			monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, player.NativeBodyStatus) &&
			ordinaryPlayerHostility(actor, player) {
			ordered = append(ordered, candidate{player: player, block: order})
		}
	}
	// 53AE20 visits rows, then columns. 534740/5405B0 traverse each block's
	// player tree; 534118 passes object+8 (GID) to its insertion at 41BBA0.
	// Sort a detached projection; never reorder the shared session snapshot.
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].block != ordered[j].block {
			return ordered[i].block < ordered[j].block
		}
		return ordered[i].player.Gid < ordered[j].player.Gid
	})
	var best playerPose
	var bestDistance uint32
	for _, candidate := range ordered {
		target, distance := nearestOwnerOrCompanion(actor, from, candidate.player, players)
		if acquisitionRankAccepts(best.Gid, bestDistance, distance, sight) {
			best, bestDistance = target, uint32(distance)
		}
	}
	return best, best.Gid != 0
}

/*
================
nearestOwnerOrCompanion

CAITactics_SelectNearestOwnerOrCompanion (5464E0): a candidate player
stands for itself and each companion it owns that the monster may strike
(status and hostility, 5298C0 with the COS's own identity); the nearest
wins, a tie keeping the earlier (the player first, then container order).
================
*/
func nearestOwnerOrCompanion(actor monster.Instance, from monster.Pose, owner playerPose, players []playerPose) (playerPose, float32) {
	best, bestDistance := owner, acquisitionDistance(from, owner.Pose)
	for _, companion := range players {
		if companion.OwnerGid != owner.Gid ||
			!monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, companion.NativeBodyStatus) ||
			!ordinaryCompanionHostility(actor, companion) {
			continue
		}
		if distance := acquisitionDistance(from, companion.Pose); distance < bestDistance {
			best, bestDistance = companion, distance
		}
	}
	return best, bestDistance
}

/*
================
ordinaryCompanionHostility

5298C0 for a COS target: 5299E0 reads the companion's own identity and
body status (a Fear exclusion names the companion), the fellow band (5,
CGObj_IsFellowCOS 483980) is never hostile, and the first-attack
protection is its owner's (COS+0x1CD8).
================
*/
func ordinaryCompanionHostility(actor monster.Instance, companion playerPose) bool {
	observer := actor.Observer()
	if actor.Abnormal != nil {
		fear := actor.Abnormal.Slots[abnormal.Fear]
		observer.RestrictionD34 = actor.Abnormal.Mask
		observer.Restriction118C = fear.Active
		observer.ExcludedGID = fear.SourceGID
	}
	return monster.AllowsHostility(observer, companion.Guard.Protect(monster.HostilityTarget{
		GID: companion.Gid, BodyStatus: companion.NativeBodyStatus, RejectedType3C: companion.Band == fellowCOSBand,
	}))
}

/*
================
ordinaryPlayerHostility

5299E0 checks detection before Fear's source exclusion. Project the live
abnormal slot into the existing hostility owner instead of filtering the
candidate first. Remembered-opponent lookup only calls 540DE0 (547E04).

The observer carries the monster's full type word, level and grade, and
the player its first-attack protection (the Bard's Noise), so 5299E0's
last branch decides the protection. The type word used to be the client
TID alone, which never matched that branch's 0x8C6.
================
*/
func ordinaryPlayerHostility(actor monster.Instance, player playerPose) bool {
	observer := actor.Observer()
	if actor.Abnormal != nil {
		fear := actor.Abnormal.Slots[abnormal.Fear]
		observer.RestrictionD34 = actor.Abnormal.Mask
		observer.Restriction118C = fear.Active
		observer.ExcludedGID = fear.SourceGID
	}
	return monster.AllowsHostility(observer, player.Guard.Protect(monster.HostilityTarget{
		GID: player.Gid, BodyStatus: player.NativeBodyStatus, Player: true,
	}))
}

// 546687..546758: comparisons use the exact integer, not float32(integer).
// Finite world positions are the adapter's input domain. A zero accumulator
// intentionally allows replacement even by a farther in-range candidate.
/*
================
acquisitionRankAccepts
================
*/
func acquisitionRankAccepts(target, best uint32, distance float32, sight uint32) bool {
	d := float64(distance)
	return !math.IsNaN(d) && d >= 0 && d <= float64(sight) &&
		(target == 0 || best == 0 || d < float64(best))
}

// 531180 clamps query extent to 1..320; CRgnTerrain::query (53AE20)
// further clamps to 1..310 and samples (x-r,x,x+r) in each of three z rows.
// Repeated blocks are omitted. This is broad-phase selection, not the sight
// predicate: a player elsewhere in a sampled block still reaches ranking.
/*
================
acquisitionBlockSet
================
*/
type acquisitionBlockSet struct {
	blocks [9]worldgeom.InterestBlock
	count  int
}

/*
================
order
================
*/
func (s acquisitionBlockSet) order(block worldgeom.InterestBlock) (int, bool) {
	for i := 0; i < s.count; i++ {
		if s.blocks[i] == block {
			return i, true
		}
	}
	return 0, false
}

/*
================
acquisitionBlocks
================
*/
func acquisitionBlocks(from monster.Pose, sight uint32) acquisitionBlockSet {
	radius := float32(sight)
	if radius < 1 {
		radius = 1
	}
	if radius > 310 {
		radius = 310
	}
	x0, z0 := float32(from.X)-radius, float32(from.Z)-radius
	var blocks acquisitionBlockSet
	for z := 0; z < 3; z++ {
		for x := 0; x < 3; x++ {
			block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
				RegionID: from.RegionID,
				X:        float64(float32(float64(x0) + float64(x)*float64(radius))),
				Z:        float64(float32(float64(z0) + float64(z)*float64(radius))),
			})
			if _, exists := blocks.order(block); !exists {
				blocks.blocks[blocks.count] = block
				blocks.count++
			}
		}
	}
	return blocks
}

// 430BA0 stores each region-relative displacement as float32. 53D7A0
// squares those values, rounds their sum to float32, then takes its square root.
/*
================
acquisitionDistance
================
*/
func acquisitionDistance(from monster.Pose, to Spawn) float32 {
	return monster.NativeActorDistance(from, monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z})
}

/*
================
nearestPlayerWithin
================
*/
func nearestPlayerWithin(from monster.Pose, divisionPlayers []playerPose, sightRange float64) (playerPose, bool) {
	return nearestEligiblePlayer(monster.Instance{}, from, divisionPlayers, sightRange)
}

/*
================
nearestEligiblePlayer
================
*/
func nearestEligiblePlayer(actor monster.Instance, from monster.Pose, divisionPlayers []playerPose, sightRange float64) (playerPose, bool) {
	if !IsDungeonRegion(from.RegionID) && actor.Nest.NativeTacticsFlags&0x184 == 0 {
		return ordinaryPlayerAcquisition(actor, from, divisionPlayers, sightRange)
	}
	// Special selectors 3/4/5 and dungeon cell queries are not selector 2.
	// Retain their existing projection until those distinct contracts close.
	best := playerPose{}
	bestDistance := sightRange
	found := false
	for _, player := range divisionPlayers {
		if player.OwnerGid != 0 || !monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, player.NativeBodyStatus) {
			continue
		}
		// The first-attack protection holds for every acquisition scan.
		if monster.FirstAttackProtected(actor.Observer(), player.Guard.Protect(monster.HostilityTarget{GID: player.Gid, Player: true})) {
			continue
		}
		d := planarDistanceSpawn(player.Pose, poseToSpawn(from))
		if d <= bestDistance {
			best, bestDistance, found = player, d, true
		}
	}
	return best, found
}

/*
================
playerByGid
================
*/
func playerByGid(divisionPlayers []playerPose, gid uint32) (playerPose, bool) {
	for _, player := range divisionPlayers {
		if player.Gid == gid {
			return player, true
		}
	}
	return playerPose{}, false
}

/*
================
eligiblePlayerByGid
================
*/
func eligiblePlayerByGid(actor monster.Instance, players []playerPose, gid uint32) (playerPose, bool) {
	player, exists := playerByGid(players, gid)
	return player, exists &&
		monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, player.NativeBodyStatus)
}

// fellowCOSBand is CGObj_IsFellowCOS's band (TypeID 4 = 5, 483980).
const fellowCOSBand = 5

/*
================
companionTargets
================
*/
func (ops *MonsterMoverOps) companionTargets(divisionID string, owner playerPose, nowMs int64) []CompanionTarget {
	if ops.Companions == nil {
		return nil
	}
	return ops.Companions(divisionID, owner.Gid, nowMs)
}

/*
================
appendCompanionTargets

Companion entries follow their owner, carrying its first-attack guard;
their motion is the follower's settled pose.
================
*/
func appendCompanionTargets(players []playerPose, companions []CompanionTarget) []playerPose {
	if len(companions) == 0 {
		return players
	}
	owner := players[len(players)-1]
	for _, c := range companions {
		players = append(players, playerPose{Gid: c.Gid, Pose: c.Pose, BodyRadius: c.BodyRadius,
			NativeBodyStatus: c.NativeBodyStatus, Guard: owner.Guard, OwnerGid: owner.Gid, Band: c.Band})
	}
	return players
}
