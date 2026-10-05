/*
===========================================================================

monsteracquisition_job.go - the job-mode acquisition queries (selectors 3, 4)

A tactics row with flag 4 queries with type mask 5 and selector 3
(CAITactics_QueryFilter_Selector3, 546790); one with flag 0x80 with selector
4 (CAITactics_QueryFilter_Selector4, 546A30). Both weigh players and
companions as candidates of their own, read each one's job state (a
companion's is its owner's), and rank the job-mode candidates ahead of the
rest before the nearest.

===========================================================================
*/

package simulation

import (
	"math"
	"sort"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

const (
	jobStateTrader  = 1
	jobStateThief   = 2
	jobStateHunter  = 3
	jobStateSpecial = 4 // a state the v1.188 predicates name; v1.150 jobs are 1..3
)

/*
================
jobQuery

Selector 3 drops thieves and state 4 and prefers traders and hunters
(CGObjPC_IsJobStateIn134); selector 4 keeps only thieves (and state 4),
all of whom CGObjPC_IsJobStateIn24 prefers.
================
*/
type jobQuery struct {
	thieves bool
}

/*
================
admits
================
*/
func (q jobQuery) admits(job uint8) bool {
	if q.thieves {
		return job == jobStateThief
	}
	return job != jobStateSpecial && job != jobStateThief
}

/*
================
prefers
================
*/
func (q jobQuery) prefers(job uint8) bool {
	if q.thieves {
		return job == jobStateThief || job == jobStateSpecial
	}
	return job == jobStateTrader || job == jobStateHunter || job == jobStateSpecial
}

/*
================
jobQueryAcquisition

CollectQueryCandidates (5405B0) visits each sampled block's players, then
its companions, each tree in GID order. A candidate within sight replaces
the held one when it is preferred and the held one is not, or when both
agree and it is strictly nearer than the held truncated distance.
================
*/
func jobQueryAcquisition(actor monster.Instance, from monster.Pose, players []playerPose, sightRange float64, query jobQuery) (playerPose, bool) {
	sightFloat := float32(sightRange)
	if math.IsNaN(float64(sightFloat)) || sightFloat < 0 || sightFloat > 1920 {
		return playerPose{}, false
	}
	sight := uint32(sightFloat)
	blocks := acquisitionBlocks(from, sight)
	jobs := make(map[uint32]uint8, len(players))
	for _, p := range players {
		if p.OwnerGid == 0 {
			jobs[p.Gid] = p.JobState
		}
	}
	type candidate struct {
		player    playerPose
		block     int
		companion bool
		job       uint8
	}
	ordered := make([]candidate, 0, len(players))
	for _, p := range players {
		block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: p.Pose.RegionID, X: float64(float32(p.Pose.X)), Z: float64(float32(p.Pose.Z))})
		order, inside := blocks.order(block)
		if !inside || p.Gid == 0 || !monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, p.NativeBodyStatus) {
			continue
		}
		job, companion := p.JobState, p.OwnerGid != 0
		if companion {
			owner, known := jobs[p.OwnerGid]
			if !known || !ordinaryCompanionHostility(actor, p) {
				continue
			}
			job = owner
		} else if !ordinaryPlayerHostility(actor, p) {
			continue
		}
		if !query.admits(job) {
			continue
		}
		ordered = append(ordered, candidate{player: p, block: order, companion: companion, job: job})
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.block != b.block {
			return a.block < b.block
		}
		if a.companion != b.companion {
			return !a.companion
		}
		return a.player.Gid < b.player.Gid
	})
	var best candidate
	var bestDistance uint32
	held := false
	for _, c := range ordered {
		distance := float64(acquisitionDistance(from, c.player.Pose))
		if float64(sight) < distance {
			continue
		}
		take := !held
		if held {
			preferred, heldPreferred := query.prefers(c.job), query.prefers(best.job)
			take = preferred && !heldPreferred || preferred == heldPreferred && distance < float64(bestDistance)
		}
		if take {
			best, bestDistance, held = c, uint32(distance), true
		}
	}
	return best.player, held
}
