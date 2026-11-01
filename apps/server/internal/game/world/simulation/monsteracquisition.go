/*
===========================================================================

monsteracquisition.go - native acquisition ordering and target lookup

===========================================================================
*/

package simulation

import (
	"cmp"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"slices"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// ordinaryPlayerAcquisition is the player and companion projection of selector
// 2 in the reference server across outdoor and dungeon regions
// When the winning target is a COS whose owner is mounted on a COS,
// the native acquire routines swap the combat target to the owner
// before setting it.
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
	// Candidates name their player by index into players: copying whole
	// poses into a slice sized to the division cost every leg an
	// allocation (#1820 part C). The stack buffer holds the usual few.
	var stack [acquisitionCandidateStack]acquisitionCandidate
	ordered := stack[:0]
	for index, player := range players {
		if player.OwnerGid != 0 {
			continue // a companion is weighed with its owner
		}
		block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
			RegionID: player.Pose.RegionID, X: float64(float32(player.Pose.X)), Z: float64(float32(player.Pose.Z)),
		})
		// A job monster's own side is skipped with its companions (a COS
		// answers with its owner's job): native target validation refuses
		// every strike on them.
		// Inference, not a native branch: native acquisition has no job test,
		// but a monster that kept choosing a target it may never strike would
		// stand at it forever; passing it over is the visible native result.
		if order, exists := blocks.order(block); exists && player.Gid != 0 &&
			!actor.JobTargetRefused(player.JobState) &&
			monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, player.NativeBodyStatus) &&
			ordinaryPlayerHostility(actor, player) {
			ordered = append(ordered, acquisitionCandidate{index: int32(index), gid: player.Gid, block: int32(order)})
		}
	}
	// The native terrain query visits rows, then columns; its block walk
	// traverses each block's player tree and passes the object GID on.
	// Sort a detached projection; never reorder the shared session snapshot.
	slices.SortFunc(ordered, compareAcquisitionCandidates)
	var best playerPose
	var bestDistance uint32
	for _, candidate := range ordered {
		target, distance := nearestOwnerOrCompanion(actor, from, players[candidate.index], players)
		if acquisitionRankAccepts(best.Gid, bestDistance, distance, sight) {
			best, bestDistance = target, uint32(distance)
		}
	}
	if best.Gid == 0 {
		return playerPose{}, false
	}
	return mountedOwnerRedirect(best, players), true
}

// acquisitionCandidateStack is how many in-sight candidates the
// acquisition queries hold without a heap allocation; more spill to the
// heap through append, with the same order.
const acquisitionCandidateStack = 32

/*
================
acquisitionCandidate

One in-sight candidate of an acquisition query: its index into the
leg's players, and the sort key the native block walk visits it by
(block order, players before companions, then GID).
================
*/
type acquisitionCandidate struct {
	index     int32
	gid       uint32
	block     int32
	companion bool
	job       uint8
}

/*
================
compareAcquisitionCandidates

Block order, then players before companions, then GID. GIDs are unique
within a leg's players, so the order is total and an unstable sort gives
the one result sort.Slice gave.
================
*/
func compareAcquisitionCandidates(a, b acquisitionCandidate) int {
	if a.block != b.block {
		return cmp.Compare(a.block, b.block)
	}
	if a.companion != b.companion {
		if a.companion {
			return 1
		}
		return -1
	}
	return cmp.Compare(a.gid, b.gid)
}

/*
================
mountedOwnerRedirect

When the chosen target is a companion whose owner is mounted on a
COS, the combat target becomes the owner.
================
*/
func mountedOwnerRedirect(target playerPose, players []playerPose) playerPose {
	if target.OwnerGid == 0 {
		return target
	}
	for _, p := range players {
		if p.OwnerGid == 0 && p.Gid == target.OwnerGid {
			if p.Mounted {
				return p
			}
			return target
		}
	}
	return target
}

/*
================
nearestOwnerOrCompanion

The native nearest-owner-or-companion selection: a candidate player
stands for itself and each companion it owns that the monster may strike
(status and hostility, judged with the COS's own identity); the nearest
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

For a COS target, the native hostility judge reads the companion's
own identity and body status (a Fear exclusion names the companion),
the fellow band (5) is never hostile, and the first-attack
protection is its owner's.
================
*/
func ordinaryCompanionHostility(actor monster.Instance, companion playerPose) bool {
	// The native judge refuses a grab pet first; remembered targets are
	// refused the same way.
	if companion.Band == domain.PickupPetBand {
		return false
	}
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

The native judge checks detection before Fear's source exclusion.
Project the live abnormal slot into the existing hostility owner
instead of filtering the candidate first.

The observer carries the monster's full type word, level and grade,
and the player its first-attack protection (the Bard's Noise), so the
judge's last branch decides the protection. The type word used to be
the client TID alone, which never matched that branch's test value.
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

// Native ranking compares the exact integer, not float32(integer).
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

// The native radius query clamps its extent to 1..320; the terrain
// query further clamps to 1..310 and samples (x-r,x,x+r) in each of
// three z rows.
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
		// A Whole dungeon block (no grid cell) neighbours its whole region
		// (worldgeom.InterestBlock.Adjacent), so it ranks with the first
		// block of that dungeon.
		if s.blocks[i] == block || block.Whole && s.blocks[i].Dungeon == block.Dungeon {
			return i, true
		}
	}
	return 0, false
}

/*
================
outdoorAcquisitionBlocks

The outdoor plane's acquisition blocks: the sight square, clamped to
[1, 310], over the sector grid. The cave plane's arm is
dungeonAcquisitionBlocks; acquisitionBlocks (plane_monster.go) picks.
================
*/
func outdoorAcquisitionBlocks(from monster.Pose, sight uint32) acquisitionBlockSet {
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

// The native distance stores each region-relative displacement as
// float32, squares those values, rounds their sum to float32, then
// takes its square root.
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
	flags := actor.Nest.NativeTacticsFlags
	// The native acquire routines pick the selector by flags in both
	// outdoor and dungeon regions: 4 first, then 0x80, then 0x100.
	switch {
	case flags&0x4 != 0:
		return jobQueryAcquisition(actor, from, divisionPlayers, sightRange, jobQuery{})
	case flags&0x80 != 0:
		return jobQueryAcquisition(actor, from, divisionPlayers, sightRange, jobQuery{thieves: true})
	case flags&0x100 == 0:
		return ordinaryPlayerAcquisition(actor, from, divisionPlayers, sightRange)
	}
	// Selector 5 (flag 0x100) queries fortress gates, towers and
	// camps beside players; keep this player projection until the fortress
	// war structure query lands.
	best := playerPose{}
	bestDistance := sightRange
	found := false
	for _, player := range divisionPlayers {
		if player.OwnerGid != 0 || !monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, player.NativeBodyStatus) {
			continue
		}
		if !ordinaryPlayerHostility(actor, player) {
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

// fellowCOSBand is the fellow companion's band (TypeID 4 = 5).
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

/*
================
dungeonAcquisitionBlocks

A dungeon monster ranks the players of its own message block and then
of that block's linked neighbours, not the outdoor 3x3 sight sample.
The neighbour list is in link order: the native linker visits blocks
by ascending linear index ((dimY*x + y)*dimZ + z) and links both ways
once, so each block's list ascends by x, then z (inference from that
loop; one layer, so y never varies). A position the grid cannot place
keeps its Whole block alone.
================
*/
func dungeonAcquisitionBlocks(from monster.Pose) acquisitionBlockSet {
	var blocks acquisitionBlockSet
	own := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
		RegionID: from.RegionID, X: float64(float32(from.X)), Z: float64(float32(from.Z)),
	})
	blocks.blocks[0] = own
	blocks.count = 1
	if own.Whole {
		return blocks
	}
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if x == 0 && z == 0 {
				continue
			}
			neighbor := own
			neighbor.X += x
			neighbor.Z += z
			blocks.blocks[blocks.count] = neighbor
			blocks.count++
		}
	}
	return blocks
}
