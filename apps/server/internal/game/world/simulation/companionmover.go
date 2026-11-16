/*
===========================================================================

companionmover.go - the one owner of an unridden companion's pose

A summoned companion stands in one of two places: on its follower (it
walks after its owner) or parked (a trade transport left where its
trader stepped off, or any companion whose follower was dropped). The
pose used to live in two loose session fields that callers swapped by
hand, and every swap that forgot to carry the pose across left the
companion "at" a stale spot: a ridden transport's cargo dropping at the
summon spot, a following transport snapping back to its dismount, a
pet read at its bind-time pose (issue #3465, PR #3464).

CompanionMover keeps both behind its own functions. Every transition
carries the pose: dropping the follower parks the companion where the
follower stood; a new follower starts from an explicit pose the caller
names (a summon, a relocation, a brood spot) or from where the
companion stands. While its owner rides it, the vehicle moves in the
rider's world instead; that world belongs to the rider, and the ride
door (action cosmount.go) hands its pose to Park on the way out.

The action owner holds its division lock around every call, as it does
for PetFollower.

===========================================================================
*/

package simulation

/*
================
CompanionMover
================
*/
type CompanionMover struct {
	gid      uint32
	follower *PetFollower
	parked   WorldState
	placed   bool
}

/*
================
Place

Admits the companion at pose with a fresh follower: a bind, a summon
or a relocation, where the companion appears rather than walks. The
parked pose is the same spot, so a follower dropped before its first
step leaves the companion there.
================
*/
func (m *CompanionMover) Place(gid uint32, pose Spawn) *PetFollower {
	m.gid = gid
	m.parked = WorldState{Spawn: pose}
	m.placed = true
	m.follower = NewPetFollower(gid, pose)
	return m.follower
}

/*
================
Park

The companion stands at pose with no follower: a trade transport its
trader stepped off, a ridden vehicle its rider left, or a record first
seen without a session pose.
================
*/
func (m *CompanionMover) Park(gid uint32, pose Spawn) {
	m.gid = gid
	m.parked = WorldState{Spawn: pose}
	m.placed = true
	m.follower = nil
}

/*
================
StopInPlace

Drops the follower and parks the companion where the follower stands
at nowMs. A mover with no follower keeps its parked pose.
================
*/
func (m *CompanionMover) StopInPlace(nowMs int64) {
	if m.follower == nil {
		return
	}
	m.gid = m.follower.GID()
	m.parked = WorldState{Spawn: m.follower.Position(nowMs)}
	m.placed = true
	m.follower = nil
}

/*
================
FollowFrom

Starts a follower for gid at pose, where it sets out: a brood spot,
or a transport just stepped off (the caller passes Pose). Replaces
any follower; the parked pose is not consulted while one walks.
================
*/
func (m *CompanionMover) FollowFrom(gid uint32, pose Spawn) *PetFollower {
	m.gid = gid
	m.follower = NewPetFollower(gid, pose)
	if !m.placed {
		m.parked = WorldState{Spawn: pose}
		m.placed = true
	}
	return m.follower
}

/*
================
Release

The companion left the world: no pose survives, and readers fall back
to its owner.
================
*/
func (m *CompanionMover) Release() {
	*m = CompanionMover{}
}

/*
================
Displace

A knockback or knockdown moves the companion where it stands: on its
follower, or its parked pose. Reports whether there was a pose to move.
================
*/
func (m *CompanionMover) Displace(pose Spawn, nowMs int64) bool {
	if m.follower != nil {
		m.follower.Displace(pose, nowMs)
		return true
	}
	if !m.placed {
		return false
	}
	m.parked.Spawn, m.parked.MoveSegment = pose, nil
	return true
}

/*
================
Follower

The walking follower for gid, or nil when the companion is parked (or
the follower belongs to another companion). Driving it - Advance,
Stop, Approach - is the pet AI's; replacing or dropping it goes through
the transitions above.
================
*/
func (m *CompanionMover) Follower(gid uint32) *PetFollower {
	if m.follower == nil || m.follower.GID() != gid {
		return nil
	}
	return m.follower
}

/*
================
AnyFollower

The walking follower whatever its companion, for the session sweeps
that act on a session's single follower (formation release, catch-up).
================
*/
func (m *CompanionMover) AnyFollower() *PetFollower {
	return m.follower
}

/*
================
World

The world that moves companion gid: its follower's, else its parked
pose's. false when the mover holds no pose for gid.
================
*/
func (m *CompanionMover) World(gid uint32) (WorldState, bool) {
	if f := m.Follower(gid); f != nil {
		world, _ := f.Presentation()
		return world, true
	}
	if !m.placed || m.gid != gid {
		return WorldState{}, false
	}
	return CloneWorldState(m.parked), true
}

/*
================
Pose

Where companion gid stands at nowMs, by World.
================
*/
func (m *CompanionMover) Pose(gid uint32, nowMs int64) (Spawn, bool) {
	if f := m.Follower(gid); f != nil {
		return f.Position(nowMs), true
	}
	if !m.placed || m.gid != gid {
		return Spawn{}, false
	}
	return m.parked.LiveSpawnAt(nowMs), true
}
