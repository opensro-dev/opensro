package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// Spawn is one settled placement on the sector grid: region + region-local
// position + full-circle u16 facing. It doubles as the move GOAL inside
// WorldState (the ack contract needs the destination) - use LiveSpawnAt for
// where the character actually IS.
//
// JSON tags mirror the Node character store's `world.spawn` shape so a
// persisted record round-trips between the Node fixture and the Go server
// (mid-move reconnect keeps its segment).
type Spawn struct {
	RegionID uint16  `json:"regionId"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	Angle    uint16  `json:"angle"`
}

// MoveSegment is one in-flight, constant-speed travel: the departure point
// plus absolute start/arrival stamps at the wire speed. It is IMMUTABLE once
// created - a resteer replaces the pointer, never mutates the fields - which
// is what makes lock-free snapshot reads from the tick loop safe.
//
// JSON tags mirror the Node `world.moveSegment` record.
type MoveSegment struct {
	From        Spawn `json:"from"`
	StartedAtMs int64 `json:"startedAtMs"`
	ArrivesAtMs int64 `json:"arrivesAtMs"`
	// Owners is the surface ownership the movement walk resolved along this
	// segment (navowner.go). Immutable and shared by pointer so segments stay
	// comparable. Runtime-only: native persists a float position and
	// re-resolves the cell on world entry (CheckPointValid).
	Owners *NavOwnerTrack `json:"-"`
}

// Valid mirrors the reference normalization gate (missionWorldStateForCharacter):
// a segment interpolates only when the stamps are ordered. A malformed or
// exhausted record degrades to "settled at spawn", never to a fault.
func (s *MoveSegment) Valid() bool {
	return s != nil && s.ArrivesAtMs > s.StartedAtMs
}

// WorldState is the character's movement world-state, the Go mirror of the
// `character.world` record server.mjs round-trips (JSON tags match the
// persisted shape; run Normalize after decoding foreign data).
type WorldState struct {
	// LifeRevision fences movement admitted before a death/rebirth transition.
	// Runtime-only: pending operations cannot survive a server restart.
	LifeRevision uint64 `json:"-"`
	// Derived live movement channels; the effect owner updates them atomically.
	Walk, Run float32 `json:"-"`
	// Spawn is the move GOAL / settled position plane.
	Spawn Spawn `json:"spawn"`
	// MoveSegment is the live-position plane's input; nil when settled.
	MoveSegment *MoveSegment `json:"moveSegment,omitempty"`
	// Nav is the retained native pNavCell (CGObj+0x7C) of Spawn, valid only
	// while NavAt equals Spawn; read it through GoalOwner/LiveOwnerAt. Never
	// re-derive ownership from a wire-quantized Y (navowner.go). Runtime-only
	// like the native cell pointer: a re-entered position is re-resolved.
	Nav   NavOwner `json:"-"`
	NavAt Spawn    `json:"-"`
	// MovementMode is WalkMode or RunMode.
	MovementMode uint8 `json:"movementMode"`
	// Sitting is the 0x7017 code-4 sit/stand posture. RUNTIME-ONLY by
	// design: the write-back plane (movement.writeBackWorld) owns exactly
	// spawn/movementMode/spawnSet, so posture never reaches the persisted
	// character record and a re-enter always stands (the enter-world spawn
	// block carries no posture channel either).
	Sitting bool `json:"-"`
	// PostureTransitionUntilMs is the wall-clock end of an in-flight
	// sit/stand transition; zero when settled. RUNTIME-ONLY for the same
	// reason as Sitting - a re-enter always stands, so a transition can
	// never outlive the session that started it.
	PostureTransitionUntilMs int64 `json:"-"`
	// AbnormalMotion is the hold the abnormal engine installs through vfunc
	// 55C (4A9D00): 0xA frozen and its 1.5 s thaw, 9 stunned, 0x13 asleep.
	// RUNTIME-ONLY: a re-entered character re-installs its abnormal block.
	AbnormalMotion monster.MotionHold `json:"-"`
	// SpawnSet reports whether any move has been accepted this life.
	SpawnSet bool `json:"spawnSet"`
	// MovementSourceSeeded reports whether the 0xB738 source block was
	// already shipped once (it rides only the first ack).
	MovementSourceSeeded bool `json:"movementSourceSeeded"`
}

// PostureTransitionMs is how long a sit/stand change occupies the character
// before the posture settles, during which further posture/gait requests are
// dropped and skills are refused.
//
// The MECHANISM is a later-retail lead: that build applies posture through a
// (state, target, duration) helper which installs an in-progress state at once
// and arms a timer carrying the final posture, and it uses ONE constant for
// both directions - unlike walk/run, which apply immediately with no duration
// at all. We are copying that shape, not that build's number.
//
// The MAGNITUDE is bracketed by our own v1.150 assets rather than imported:
// the client's stand-up clip (motion 0x0f) runs 966-2066 ms across the shipped
// roster with the mass at 1333/1600 ms, and the sit-down clip (0x0d) sits in
// the same band. A single 1500 ms transition lands inside our own range and
// close to its centre, so the posture settles about when the animation ends.
//
// HONEST LIMIT: no v1.150 server survives, so this exact number is NOT pinned
// to our build - only the mechanism and the band are evidenced. It is a single
// named constant so a future v1.150-authoritative figure is a one-line change.
const PostureTransitionMs int64 = 1500

// StandUp is the stand arm of the sit toggle for a server-initiated stand:
// a seated, settled character stands and the transition lockout starts.
// It reports whether the posture changed.
func (w *WorldState) StandUp(nowMs int64) bool {
	if !w.Sitting || nowMs < w.PostureTransitionUntilMs {
		return false
	}
	w.Sitting = false
	w.PostureTransitionUntilMs = nowMs + PostureTransitionMs
	return true
}

// Native motion bytes (CGObj state+0x2) this port models for players.
const (
	MotionNone       uint8 = 0
	MotionSitting    uint8 = 4
	MotionWall       uint8 = 0x11
	MotionPostureNow uint8 = 0x12
)

// MotionStateAt is the player's native motion byte (GetMotionState 4AA590
// reads state+0x2) as the world plane knows it. Writers in native: 4A9D00
// for the abnormal hold, 4B15A4/4B15B8 for the 0x12 posture change and
// seated 4, and 4AA3F5 for walk 2 / run 3, which it sets only while the
// character is moving. The standing-wall 0x11 lives with the skill owner.
func (w WorldState) MotionStateAt(nowMs int64) uint8 {
	if state := w.AbnormalMotion.StateAt(nowMs); state != MotionNone {
		return state
	}
	if nowMs < w.PostureTransitionUntilMs {
		return MotionPostureNow
	}
	if w.Sitting {
		return MotionSitting
	}
	if w.MoveSegment.Valid() && nowMs < w.MoveSegment.ArrivesAtMs {
		if w.MovementMode == WalkMode {
			return WalkMode
		}
		return RunMode
	}
	return MotionNone
}

// SettleDeath must run inside the same character commit door as fatal HP.
// The settled world is the corpse position, including region, height and facing;
// callers persist it before publishing death. Never resample travel at rebirth.
func (w *WorldState) SettleDeath(nowMs int64) {
	w.SettleLive(nowMs)
	w.Sitting = false
	w.PostureTransitionUntilMs = 0
	w.LifeRevision++
}

// Normalize applies the reference world-state hygiene: an invalid segment is
// dropped (settled at spawn), an unknown movement mode falls back to run, and
// spawnSet implies movementSourceSeeded exactly as server.mjs computes it.
func (w *WorldState) Normalize() {
	if !w.MoveSegment.Valid() {
		w.MoveSegment = nil
	}
	w.MovementMode = CoerceRunWalkMode(w.MovementMode, RunMode)
	if w.SpawnSet {
		w.MovementSourceSeeded = true
	}
}

// CoerceRunWalkMode mirrors coerceMissionRunWalkMode: only the two native
// run/walk modes pass; anything else takes the fallback.
func CoerceRunWalkMode(mode, fallback uint8) uint8 {
	if mode == WalkMode || mode == RunMode {
		return mode
	}
	return fallback
}

var (
	europeStartProfile = Spawn{RegionID: 0x6B4F, X: 1205, Y: 80, Z: 396, Angle: 0}
	chinaStartProfile  = Spawn{RegionID: 0x62A8, X: 960.418884, Y: 20, Z: 458.259766, Angle: 0}
)

// EuropeStartProfile returns the European race start placement.
func EuropeStartProfile() Spawn {
	return europeStartProfile
}

// ChinaStartProfile returns the Chinese race start placement.
func ChinaStartProfile() Spawn {
	return chinaStartProfile
}

// DefaultWorldState is a fresh character's world-state for a race start
// profile (server.mjs defaultMissionWorldStateForRace).
func DefaultWorldState(startProfile Spawn) WorldState {
	return WorldState{
		Spawn:        startProfile,
		MovementMode: RunMode,
	}
}

// LiveSpawnAt is the character's LIVE position: the in-flight move segment
// interpolated at nowMs, or the settled goal spawn when nothing is in flight.
// This is the bug-D fix plane (server.mjs missionLiveSpawnForWorld): any
// position-dependent op that reads WorldState.Spawn mid-move is reading the
// pathing TARGET - the wave-9 signature was gold dropped mid-run landing at
// the destination.
//
// The interpolation runs in the segment start's local frame (the shared
// sector-grid helpers keep cross-region deltas and the dungeon bit
// consistent), then re-expresses the point as region + region-local.
func (w WorldState) LiveSpawnAt(nowMs int64) Spawn {
	spawn := w.Spawn
	segment := w.MoveSegment
	if !segment.Valid() {
		return spawn
	}
	if nowMs >= segment.ArrivesAtMs {
		return spawn
	}

	t := float64(nowMs-segment.StartedAtMs) / float64(segment.ArrivesAtMs-segment.StartedAtMs)
	if t < 0 {
		t = 0
	}

	from := segment.From
	planar := worldgeom.Interpolate(
		worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z},
		worldgeom.RegionXZ{RegionID: spawn.RegionID, X: spawn.X, Z: spawn.Z},
		t,
	)
	return Spawn{
		RegionID: planar.RegionID,
		X:        planar.X,
		Y:        from.Y + (spawn.Y-from.Y)*t,
		Z:        planar.Z,
		Angle:    spawn.Angle,
	}
}

// MoveSegmentForTravel builds the segment LiveSpawnAt interpolates: departure
// point + absolute stamps at the wire speed for movementMode (server.mjs
// missionMoveSegmentForTravel). It returns nil for a zero-length hop, which
// also CLEARS any stale segment on the world-state write - every Spawn write
// must pair with a segment write or the live plane would interpolate against
// a goal it never had.
func MoveSegmentForTravel(liveFrom, nextSpawn Spawn, movementMode uint8, startedAtMs int64) *MoveSegment {
	speed := RunSpeed
	if movementMode == WalkMode {
		speed = WalkSpeed
	}
	return moveSegmentAtSpeed(liveFrom, nextSpawn, speed, startedAtMs)
}

func (w *WorldState) TravelSegment(from, to Spawn, mode uint8, now int64) *MoveSegment {
	walk, run := w.MovementSpeeds()
	speed := run
	if mode == WalkMode {
		speed = walk
	}
	return moveSegmentAtSpeed(from, to, float64(speed), now)
}

func (w *WorldState) MovementSpeeds() (float32, float32) {
	walk, run := w.Walk, w.Run
	if walk <= 0 {
		walk = WalkSpeed
	}
	if run <= 0 {
		run = RunSpeed
	}
	return walk, run
}

// UpdateMovementSpeeds preserves current position and remaining destination.
// No instantaneous teleport or stale-duration travel is introduced by expiry.
func (w *WorldState) UpdateMovementSpeeds(walk, run float32, now int64) bool {
	if walk <= 0 || run <= 0 || math.IsNaN(float64(walk)) || math.IsNaN(float64(run)) || math.IsInf(float64(walk), 0) || math.IsInf(float64(run), 0) {
		return false
	}
	oldWalk, oldRun := w.MovementSpeeds()
	if walk == oldWalk && run == oldRun {
		return false
	}
	live := w.LiveSpawnAt(now)
	moving := w.MoveSegment.Valid() && now < w.MoveSegment.ArrivesAtMs
	w.Walk, w.Run = walk, run
	if moving {
		w.MoveSegment = w.TravelSegment(live, w.Spawn, w.MovementMode, now)
	}
	return true
}

func moveSegmentAtSpeed(liveFrom, nextSpawn Spawn, speed float64, startedAtMs int64) *MoveSegment {
	distance := WorldDistance2D(liveFrom, nextSpawn)
	travelMs := math.Ceil(distance / speed * 1000)
	if !(travelMs > 0) {
		return nil
	}
	return &MoveSegment{
		From:        liveFrom,
		StartedAtMs: startedAtMs,
		ArrivesAtMs: startedAtMs + int64(travelMs),
	}
}

// SpawnFromMovement converts an accepted movement destination into the next
// goal spawn, facing the travel direction (server.mjs missionSpawnFromMovement).
//
// The goal is committed in its CANONICAL frame (NormalizeSpawnFrame): the wire
// echo in the 0xB738 ack still carries the request's own frame, but the plane
// that persists and re-enters the world must never store sector overflow.
func SpawnFromMovement(movement MovementRequest, previousSpawn Spawn) Spawn {
	next := NormalizeSpawnFrame(Spawn{
		RegionID: movement.RegionID,
		X:        movement.X,
		Y:        movement.Y,
		Z:        movement.Z,
		Angle:    previousSpawn.Angle,
	})
	if heading, ok := HeadingFromMovement(previousSpawn, next); ok {
		next.Angle = heading
	}
	return next
}

// NormalizeSpawnFrame folds a spawn whose region-local coordinates overflowed
// the outdoor sector grid back into the canonical frame: RegionID names the
// sector the position actually lands in and x/z sit inside [0, NativeRegionSize).
// Y and Angle pass through untouched.
//
// WHY THIS EXISTS: the goal plane historically stored the move destination in
// the frame of whatever region the REQUEST referenced, clamped to the wire
// range only. A character that traveled sectors away from its enter-world
// region persisted e.g. region 0x5E9E with x=5682 - the same world point as
// region 0x60A0 local 1842, but every regionId-keyed consumer (enter-world
// terrain residency, zone/BGM/dungeon lookups, settled drops, the settle
// correction) misread it by whole sectors, leaving the player floating in an
// unloaded void on re-enter. Mid-flight interpolation (LiveSpawnAt) already
// folded; this extends the same math to the settled plane.
//
// Dungeon regions (bit15) are exempt: the dungeon plane is a single region
// word whose locals are not bounded by the 1920-unit outdoor grid, so folding
// them would corrupt legal positions.
func NormalizeSpawnFrame(spawn Spawn) Spawn {
	position := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{
		RegionID: spawn.RegionID,
		X:        spawn.X,
		Z:        spawn.Z,
	})
	spawn.RegionID = position.RegionID
	spawn.X = position.X
	spawn.Z = position.Z
	return spawn
}
