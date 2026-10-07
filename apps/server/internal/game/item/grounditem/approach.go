/*
===========================================================================

approach.go - ground-item reach and the authoritative pickup approach latch

Keeps one pending approach per character. Completion rechecks live range;
an estimated travel time alone never grants an item.

===========================================================================
*/
package grounditem

import (
	"math"
	"sort"
	"sync"
	"time"

	worldgeom "opensro.online/server/internal/game/world"
)

// World geometry and movement constants.
const (
	// RegionSize is the edge length of one sector on the native region grid.
	RegionSize = worldgeom.OutdoorRegionSize
	// DungeonSectorBit marks a region id as belonging to a dungeon world. It
	// stays out of the sector arithmetic; two positions must agree on it to be
	// comparable at all.
	DungeonSectorBit uint16 = worldgeom.DungeonRegionBit

	// WalkSpeed and RunSpeed are the per-second speeds shipped on every spawn
	// block (+0x24c and +0x250).
	WalkSpeed = 20.0
	RunSpeed  = 50.0

	// MovementModeWalk is the character movement mode meaning "walking";
	// anything else is treated as running.
	MovementModeWalk uint8 = 2

	// ExecuteRange is how close a character must be for a pickup to execute
	// rather than trigger an approach.
	//
	// MEASURED (supersedes the provisional 50u): the retail-family
	// SR_GameServer.exe pickup executor sub_526090 loads a single global
	// float32 = 10.0 (.rdata:0x00b45b44 = 0x41200000), feeds it to the reach
	// checker sub_4a9050, and compares it INCLUSIVELY (distance <= 10.0)
	// against a LINEAR region-aware distance (post-fsqrt, 1920u/region). The
	// client ships no player pickup range because the value is
	// server-authoritative, and there is no too-far refusal in the item
	// error family - retail always walks you there.
	ExecuteRange = 10.0
)

// Point is a position on the region grid. Y is omitted because every distance
// rule in the item plane is horizontal.
/*
================
Point
================
*/
type Point struct {
	RegionID uint16
	X        float32
	Z        float32
}

// SameWorld reports whether two positions are in the same world, meaning they
// agree on the dungeon bit. Sector arithmetic across the boundary is
// meaningless.
/*
================
SameWorld
================
*/
func SameWorld(a, b Point) bool {
	return worldgeom.SamePlane(a.RegionID, b.RegionID)
}

// Distance2D returns the horizontal distance between two positions in native
// units, accounting for the sector each one sits in.
/*
================
Distance2D
================
*/
func Distance2D(a, b Point) float64 {
	return worldgeom.Distance(
		worldgeom.RegionXZ{RegionID: a.RegionID, X: float64(a.X), Z: float64(a.Z)},
		worldgeom.RegionXZ{RegionID: b.RegionID, X: float64(b.X), Z: float64(b.Z)},
	)
}

// SpeedForMovementMode returns the per-second travel speed for a movement
// mode.
/*
================
SpeedForMovementMode
================
*/
func SpeedForMovementMode(movementMode uint8) float64 {
	if movementMode == MovementModeWalk {
		return WalkSpeed
	}
	return RunSpeed
}

// Approach is the verdict on whether a pickup may execute now.
/*
================
Approach
================
*/
type Approach struct {
	// InRange is true when the pickup should execute immediately.
	InRange bool
	// Distance is the measured horizontal distance in native units.
	Distance float64
	// Travel is how long the walk to the item takes. Zero when in range.
	Travel time.Duration
}

// PlanApproach decides whether a pickup executes now or has to walk first.
//
// Retail does not refuse an out-of-range interact: the server walks the
// character to the drop and executes on arrival. The client sub_693190
// one-byte 0x72CD lane is a throttled CANCEL/recovery command, not a pickup
// heartbeat; completion therefore remains server-owned. A cross-world pair
// can never be in range.
/*
================
PlanApproach
================
*/
func PlanApproach(from, to Point, movementMode uint8) Approach {
	if !SameWorld(from, to) {
		return Approach{InRange: false, Distance: math.Inf(1)}
	}

	distance := Distance2D(from, to)
	if distance <= ExecuteRange {
		return Approach{InRange: true, Distance: distance}
	}

	seconds := distance / SpeedForMovementMode(movementMode)
	// Round up so the pending approach never matures a tick early.
	travel := time.Duration(math.Ceil(seconds*1000.0)) * time.Millisecond
	return Approach{InRange: false, Distance: distance, Travel: travel}
}

// Pending is a pickup approach in flight.
/*
================
Pending
================
*/
type Pending struct {
	Key              string
	DivisionID       string
	CharacterName    string
	ItemGid          uint32
	ArrivesAt        time.Time
	MovementRevision uint64
}

// PendingTracker holds the one approach each character may have in flight.
//
// The native target-move latch is a single slot: issuing any new command
// replaces it (sub_67b0e0), which is why arming an approach overwrites rather
// than queues.
/*
================
PendingTracker
================
*/
type PendingTracker struct {
	mu      sync.Mutex
	pending map[string]Pending
}

// NewPendingTracker returns an empty PendingTracker.
/*
================
NewPendingTracker
================
*/
func NewPendingTracker() *PendingTracker {
	return &PendingTracker{pending: make(map[string]Pending)}
}

// Arm records an approach, replacing any previous one for the same key.
/*
================
Arm
================
*/
func (t *PendingTracker) Arm(key string, itemGid uint32, arrivesAt time.Time) {
	t.ArmOwned(key, "", "", itemGid, arrivesAt)
}

// ArmOwned records the authority identity needed by the server tick to finish
// an approach without another client packet. The native client sends one
// object-action request; its one-byte 0x72CD form is a cancel/recovery command,
// not a pickup heartbeat. Keeping division/name beside the timer also avoids
// parsing the opaque PendingKey when the completion owner is resolved.
/*
================
ArmOwned
================
*/
func (t *PendingTracker) ArmOwned(key, divisionID, characterName string, itemGid uint32, arrivesAt time.Time) {
	t.ArmGround(Pending{
		Key:           key,
		DivisionID:    divisionID,
		CharacterName: characterName,
		ItemGid:       itemGid,
		ArrivesAt:     arrivesAt,
	})
}

// Peek returns the approach in flight without consuming it.
/*
================
Peek
================
*/
func (t *PendingTracker) Peek(key string) (Pending, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.pending[key]
	return entry, ok
}

// Clear removes any approach in flight. It is safe to call when none is armed,
// which is what every superseding command does.
/*
================
Clear
================
*/
func (t *PendingTracker) Clear(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.pending, key)
}

// Due returns an ownership snapshot of approaches whose server-side travel
// timer matured. Entries remain in the tracker until TakeMatured consumes
// them under the division operation lock; this makes a concurrent cancel or
// replacement win cleanly instead of granting from a stale snapshot.
/*
================
Due
================
*/
func (t *PendingTracker) Due(now time.Time) []Pending {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Pending, 0, len(t.pending))
	for _, entry := range t.pending {
		if entry.DivisionID == "" || entry.CharacterName == "" || entry.ArrivesAt.After(now) {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DivisionID != out[j].DivisionID {
			return out[i].DivisionID < out[j].DivisionID
		}
		if out[i].CharacterName != out[j].CharacterName {
			return out[i].CharacterName < out[j].CharacterName
		}
		return out[i].ItemGid < out[j].ItemGid
	})
	return out
}

// TakeMatured consumes the approach for key when it targets itemGid and its
// arrival time has passed, reporting whether the pickup may now execute.
//
// The second result is the time still to wait, so a duplicate execute request
// can observe the same in-flight approach without consuming or extending it.
/*
================
TakeMatured
================
*/
func (t *PendingTracker) TakeMatured(key string, itemGid uint32, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.pending[key]
	if !ok || entry.ItemGid != itemGid {
		return false, 0
	}
	if remaining := entry.ArrivesAt.Sub(now); remaining > 0 {
		return false, remaining
	}
	delete(t.pending, key)
	return true, 0
}
