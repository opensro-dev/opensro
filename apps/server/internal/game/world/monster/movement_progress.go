package monster

import "math"

// MovementProgress is the value owner for the tactics record's
// +184/+19C/+148.
// Command records must be admitted before calling Issued; failed movement
// admission is not an arrival notification. Simulation integration must keep
// the resulting event in the same transaction as state and membership effects.
type MovementProgress struct {
	origin, lastIssued Pose
	failures           uint8
}

// The native step updates the last command origin on every
// admitted command, but
// replaces the sequence origin only when the actor is not already moving.
func (p *MovementProgress) Issued(live Pose, alreadyMoving bool) {
	p.lastIssued = live
	if !alreadyMoving {
		p.origin = live
	}
}

func (p MovementProgress) SequenceOrigin() Pose   { return p.origin }
func (p MovementProgress) LastIssuedOrigin() Pose { return p.lastIssued }
func (p MovementProgress) Failures() uint8        { return p.failures }

// The move-stopped counter counts completed movements shorter
// than 20 from the sequence
// origin. A successful movement does not clear previous failures. The native
// byte wraps; it is not a saturating counter or a consecutive-failure counter.
func (p *MovementProgress) Arrived(live Pose) bool {
	x, y, z := NativeTacticsRelative(p.origin, live)
	distance := math.Sqrt(float64(float32(float64(x)*float64(x) + float64(y)*float64(y) + float64(z)*float64(z))))
	return p.arrivedDistance(distance)
}

func (p *MovementProgress) arrivedDistance(distance float64) bool {
	if !(distance < 20) {
		return false
	}
	p.failures++
	return p.failures >= 10
}

// The state change resets before state exit/entry, including
// native state re-entry; the command release also resets when
// detaching. Neither operation changes the origins.
func (p *MovementProgress) ResetFailures() { p.failures = 0 }

type StuckResponse uint8

const (
	// BATTLE's override consumes event 40 but leaves BATTLE and attachment
	// intact when its trace mode does not request a target warp.
	StuckRetainBattle StuckResponse = iota
	StuckWarpToBattleTarget
	StuckDetachAndIdle
	StuckWarpToControllerAndIdle
)

// The extended-event dispatch sends event 40 through the state
// table. BATTLE's slot overrides the base handler; all other
// state tables use the shared one.
// The trace selector is the byte at RefTactics +79. Object lookup and warp
// admission belong to the caller; missing objects do not change this choice.
func NativeStuckResponse(inBattle bool, traceSelector uint8, tid uint16) StuckResponse {
	if inBattle {
		if traceSelector == 2 {
			return StuckWarpToBattleTarget
		}
		return StuckRetainBattle
	}
	// The attack-or-pickup COS test reads COS kinds 3/4; the fellow
	// test reads kind 5.
	cos := tid&0x7fe == 0x1c6
	kind := tid >> 11
	if cos && kind >= 3 && kind <= 5 {
		return StuckWarpToControllerAndIdle
	}
	return StuckDetachAndIdle
}
