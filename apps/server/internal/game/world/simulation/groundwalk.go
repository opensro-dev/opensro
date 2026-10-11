/*
===========================================================================

groundwalk.go - accepted ground poses and finite native movement steps

The world store advances this state under its own lock before publishing a
snapshot. Intent remains in Spawn; readers and persistence use the last
geometry-accepted pose, never an interpolation through an untested chord.

===========================================================================
*/
package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
)

const nativeGroundStepLimit = 160
const nativeGroundComponentMinimum = float32(0.01)

/*
================
GroundWalkConfig
================
*/
type GroundWalkConfig struct {
	Now  func() int64
	Step func(Spawn, NavOwner, Spawn) (Spawn, NavOwner, bool)
}

/*
================
GroundWalkState
================
*/
type GroundWalkState struct {
	origin        Spawn
	commandOrigin Spawn
	goal          Spawn
	started       int64
	At            int64   `json:"at"`
	Pose          Spawn   `json:"pose"`
	Speed         float32 `json:"speed"`
	owner         NavOwner
	directionX    float32
	directionZ    float32
}

/*
================
GroundUpdate

Coalesced persistence work; a stop belongs to one motion revision. The
movement owner drains it outside the world-store/character read locks.
================
*/
type GroundUpdate struct {
	Key      string
	Revision uint64
	Stopped  bool
	Arrived  bool
	// Tether is the native reason byte of a tether refusal (tether.go),
	// zero for any other stop.
	Tether uint8
}

/*
================
NativeGroundStep

48BFF0 stores speed*dt as float32, caps its magnitude at 160, multiplies
the stored direction components and clears components smaller than .01.
================
*/
func NativeGroundStep(speed, elapsedSeconds, directionX, directionZ float32) (float32, float32) {
	distance := float32(math.Abs(float64(float32(float64(speed) * float64(elapsedSeconds)))))
	if distance > nativeGroundStepLimit {
		distance = nativeGroundStepLimit
	}
	x := float32(float64(directionX) * float64(distance))
	z := float32(float64(directionZ) * float64(distance))
	if math.Abs(float64(x)) < float64(nativeGroundComponentMinimum) {
		x = 0
	}
	if math.Abs(float64(z)) < float64(nativeGroundComponentMinimum) {
		z = 0
	}
	return x, z
}

/*
================
NativeGroundDestination

48C5AE..48C63F clamps only when the stored step norm exceeds the stored
remaining norm. Equality retains the original vector, including its Y.
================
*/
func NativeGroundDestination(step, remaining [3]float32) [3]float32 {
	squaredStep := float32(float64(step[1])*float64(step[1]) + float64(step[0])*float64(step[0]) + float64(step[2])*float64(step[2]))
	squaredRemaining := float32(float64(remaining[0])*float64(remaining[0]) + float64(remaining[1])*float64(remaining[1]) + float64(remaining[2])*float64(remaining[2]))
	stepLength := float32(math.Sqrt(float64(squaredStep)))
	remainingLength := float32(math.Sqrt(float64(squaredRemaining)))
	if stepLength > remainingLength {
		return remaining
	}
	return step
}

/*
================
NativeGroundDirection

485C20 stores atan2 as float32; 48BA10 stores its sine/cosine into the
actor direction consumed by 48BFF0. Destination commands turn every tick.
================
*/
func NativeGroundDirection(dx, dz float32) (float32, float32) {
	yaw := float32(math.Atan2(float64(dz), float64(dx)))
	return float32(math.Cos(float64(yaw))), float32(math.Sin(float64(yaw)))
}

/*
================
NativeGroundArrived

48C790 accepts a planar distance no greater than .5, or a positive dot
between goal-current and original-command-source-current (passed goal).
================
*/
func NativeGroundArrived(dx, dz, originX, originZ float32) bool {
	squared := float32(float64(dx)*float64(dx) + float64(dz)*float64(dz))
	distance := float32(math.Sqrt(float64(squared)))
	if distance <= 0.5 {
		return true
	}
	return float32(float64(dx)*float64(originX)+float64(dz)*float64(originZ)) > 0
}

/*
================
groundActive
================
*/
func (w WorldState) groundActive() bool {
	g := w.Ground
	return g != nil && w.MoveSegment.Valid() && w.MoveSegment.Ground &&
		g.started == w.MoveSegment.StartedAtMs && g.origin == w.MoveSegment.From && g.goal == w.Spawn
}

/*
================
PersistedSpawn
================
*/
func (w WorldState) PersistedSpawn() Spawn {
	if w.groundActive() {
		return w.Ground.Pose
	}
	return w.Spawn
}

/*
================
GroundRevision
================
*/
func (w WorldState) GroundRevision() uint64 { return w.groundRevision }

/*
================
MovingAt

Nominal arrival is a wire estimate. A capped or blocked-step owner remains
active until its accepted pose reaches the goal or an explicit stop clears it.
================
*/
func (w WorldState) MovingAt(nowMs int64) bool {
	return w.groundActive() || w.MoveSegment.Valid() && nowMs < w.MoveSegment.ArrivesAtMs
}

/*
================
GroundActive
================
*/
func (w WorldState) GroundActive() bool { return w.groundActive() }

/*
================
ConfigureGroundWalk

Installed once by production composition, before sessions are admitted.
The geometry callback must not take a character or world-store lock.
================
*/
func (st *WorldStore) ConfigureGroundWalk(config GroundWalkConfig) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.groundConfig = config
	st.groundUpdates = make(map[string]GroundUpdate)
}

/*
================
DrainGroundUpdates
================
*/
func (st *WorldStore) DrainGroundUpdates() []GroundUpdate {
	st.mu.Lock()
	defer st.mu.Unlock()
	updates := make([]GroundUpdate, 0, len(st.groundUpdates))
	for key, update := range st.groundUpdates {
		updates = append(updates, update)
		delete(st.groundUpdates, key)
	}
	return updates
}

/*
================
GroundRevisionCurrent
================
*/
func (st *WorldStore) GroundRevisionCurrent(key string, revision uint64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	state, ok := st.states[key]
	return ok && state.groundRevision == revision
}

/*
================
bindGroundWalk
================
*/
func (st *WorldStore) bindGroundWalk(key string, w *WorldState, previous *MoveSegment, owner NavOwner) {
	if st.groundConfig.Step == nil || w.MoveSegment == previous {
		return
	}
	commandOrigin := Spawn{}
	continuation := previous.Valid() && previous.Ground && w.MoveSegment.Valid() && w.MoveSegment.GroundContinuation
	if continuation && w.Ground != nil {
		commandOrigin = w.Ground.commandOrigin
	}
	if !continuation {
		w.groundRevision++
	}
	w.Ground = nil
	delete(st.groundUpdates, key)
	if !w.MoveSegment.Valid() || !w.MoveSegment.Ground {
		return
	}
	w.Spawn.X, w.Spawn.Y, w.Spawn.Z = float64(float32(w.Spawn.X)), float64(float32(w.Spawn.Y)), float64(float32(w.Spawn.Z))
	w.Ground = &GroundWalkState{origin: w.MoveSegment.From, goal: w.Spawn, started: w.MoveSegment.StartedAtMs,
		At: w.MoveSegment.StartedAtMs, Pose: w.MoveSegment.From, owner: owner}
	w.Ground.commandOrigin = w.MoveSegment.From
	if continuation {
		w.Ground.commandOrigin = commandOrigin
	}
	a := worldgeom.RegionXZ{RegionID: w.MoveSegment.From.RegionID, X: w.MoveSegment.From.X, Z: w.MoveSegment.From.Z}
	b := worldgeom.RegionXZ{RegionID: w.Spawn.RegionID, X: w.Spawn.X, Z: w.Spawn.Z}
	dx, dz := worldgeom.Delta(a, b)
	w.Ground.directionX, w.Ground.directionZ = NativeGroundDirection(float32(dx), float32(dz))
	if w.MoveSegment.GroundAngular {
		yaw := float32(float64(w.Spawn.Angle) / HeadingWordScale * twoPi)
		w.Ground.directionX, w.Ground.directionZ = float32(math.Cos(float64(yaw))), float32(math.Sin(float64(yaw)))
	}
	walk, run := w.MovementSpeeds()
	w.Ground.Speed = run
	if w.MovementMode == WalkMode {
		w.Ground.Speed = walk
	}
}

/*
================
advanceGroundWalk
================
*/
func (st *WorldStore) advanceGroundWalk(key string, w *WorldState) {
	if st.groundConfig.Now == nil || st.groundConfig.Step == nil || !w.groundActive() {
		return
	}
	now := st.groundConfig.Now()
	g := *w.Ground
	if now <= g.At {
		return
	}
	walk, run := w.MovementSpeeds()
	speed := run
	if w.MovementMode == WalkMode {
		speed = walk
	}
	a := worldgeom.RegionXZ{RegionID: g.Pose.RegionID, X: g.Pose.X, Z: g.Pose.Z}
	b := worldgeom.RegionXZ{RegionID: g.goal.RegionID, X: g.goal.X, Z: g.goal.Z}
	dx, dz := worldgeom.Delta(a, b)
	origin := worldgeom.RegionXZ{RegionID: g.commandOrigin.RegionID, X: g.commandOrigin.X, Z: g.commandOrigin.Z}
	ox, oz := worldgeom.Delta(a, origin)
	angular := w.MoveSegment.GroundAngular
	if !angular && NativeGroundArrived(float32(dx), float32(dz), float32(ox), float32(oz)) {
		w.Spawn = g.Pose
		w.MoveSegment = nil
		w.SetGoalOwner(g.owner)
		w.Ground = nil
		st.groundUpdates[key] = GroundUpdate{Key: key, Revision: w.groundRevision, Arrived: true}
		return
	}
	if !angular {
		g.directionX, g.directionZ = NativeGroundDirection(float32(dx), float32(dz))
	}
	sx, sz := NativeGroundStep(speed, float32(float64(now-g.At)/1000), g.directionX, g.directionZ)
	// Pos_GetRelativePlanar 430AD0 supplies zero Y to the native destination clamp.
	step := [3]float32{sx, 0, sz}
	if !angular {
		step = NativeGroundDestination(step, [3]float32{float32(dx), 0, float32(dz)})
	}
	goal := NormalizeSpawnFrame(Spawn{RegionID: g.Pose.RegionID,
		X: float64(float32(float64(float32(g.Pose.X)) + float64(step[0]))),
		Y: float64(float32(float64(float32(g.Pose.Y)) + float64(step[1]))),
		Z: float64(float32(float64(float32(g.Pose.Z)) + float64(step[2]))), Angle: g.goal.Angle})
	arrives := !angular && goal.RegionID == g.goal.RegionID && goal.X == g.goal.X && goal.Z == g.goal.Z
	if step[0] == 0 && step[1] == 0 && step[2] == 0 {
		g.At = now
		w.Ground = &g
		return
	}
	if tether, ok := st.tethers[key]; ok && tether.Refuses(g.Pose, goal) {
		// 4F1230 stops the player where it stands (vf4B0) before any
		// geometry runs for the refused step.
		w.Spawn = g.Pose
		w.MoveSegment = nil
		w.SetGoalOwner(g.owner)
		w.Ground = nil
		st.groundUpdates[key] = GroundUpdate{Key: key, Revision: w.groundRevision, Stopped: true, Tether: tether.Reason}
		return
	}
	accepted, owner, blocked := st.groundConfig.Step(g.Pose, g.owner, goal)
	g.Pose, g.owner, g.At = accepted, owner, now
	w.Ground = &g
	if blocked || arrives {
		w.Spawn = accepted
		w.MoveSegment = nil
		w.SetGoalOwner(owner)
		w.Ground = nil
	}
	if blocked || arrives {
		st.groundUpdates[key] = GroundUpdate{Key: key, Revision: w.groundRevision, Stopped: blocked, Arrived: !blocked}
	}
}
