/*
===========================================================================

playerpose.go - the player and companion poses the monster leg plans against

One tick's worth of who a monster can chase, strike or flee: the live
player positions inside a division - each with the movement command it
last gave, kept apart from the pose it stands at - and the summoned
companions projected as strike targets. The leg samples these once per
division and every planner reads them as plain values; the simulation
package owns their shape, the action owner fills them.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
playerMovementIntent

playerMovementIntent is one immutable target-command snapshot. It is kept
separate from playerPose.Pose because the command destination is an
invalidation input, while the live pose owns combat and approach geometry.
==================
*/
type playerMovementIntent struct {
	destination Spawn
	inFlight    bool
	present     bool
}

/*
================
capturePlayerMovementIntent
================
*/
func capturePlayerMovementIntent(world WorldState, nowMs int64) playerMovementIntent {
	return playerMovementIntent{
		destination: world.Spawn,
		inFlight:    world.MovingAt(nowMs),
		present:     true,
	}
}

// playerPose is one live player position inside a division at tick time.
/*
================
playerPose
================
*/
type playerPose struct {
	NativeBodyStatus uint8
	Gid              uint32
	Pose             Spawn
	MovementIntent   playerMovementIntent
	BodyRadius       BodyRadius
	// Guard is the player's first-attack protection (the Bard's Noise),
	// read from the action owner when the leg samples its players.
	Guard monster.FirstAttackGuard
	// OwnerGid names the player a companion entry belongs to; zero for a
	// player. Companions are targets, never acquisition candidates.
	OwnerGid uint32
	// Band is a companion's COS band (TypeID 4).
	Band uint8
	// JobState is a player's job state: the dressed job, 4 outside
	// job mode.
	JobState uint8
	// Mounted: whether the player is mounted on a COS when
	// acquisition chooses a companion.
	Mounted bool
}

/*
================
CompanionTarget

One summoned, living companion a monster may strike: the action owner's
projection of its world pose, body and status.
================
*/
type CompanionTarget struct {
	Gid              uint32
	Pose             Spawn
	BodyRadius       BodyRadius
	NativeBodyStatus uint8
	Band             uint8
}

/*
==================
chaseGuidance

chaseGuidance returns the movement state the pursuit owner last observed.
Direct test fixtures and stationary actors may omit MovementIntent; their
live pose then acts as a settled command destination.
==================
*/
func (player playerPose) chaseGuidance() monster.ChaseGuidance {
	intent := player.MovementIntent
	destination := player.Pose
	if intent.present {
		destination = intent.destination
	}
	return monster.NewChaseGuidance(monster.Pose{
		RegionID: destination.RegionID,
		X:        destination.X,
		Y:        destination.Y,
		Z:        destination.Z,
		Heading:  destination.Angle,
	}, intent.present && intent.inFlight)
}
