package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
)

// BodyRadius is the RefObjChar BCRadius contribution to actor contact
// spacing. It is deliberately distinct from ActionReach: retail combines the
// two, while the old server accidentally treated weapon/skill reach as the
// complete center-to-center radius.
type BodyRadius float64

// ActionReach is the authored weapon or skill reach beyond the two actors'
// body radii.
type ActionReach float64

// CombatSpacing is the complete geometry contract for one directed combat
// interaction. It is a value snapshot: callers resolve mutable equipment and
// target identity first, then use this one value for admission and movement.
type CombatSpacing struct {
	ActorBodyRadius  BodyRadius
	TargetBodyRadius BodyRadius
	ActionReach      ActionReach
}

// CombatApproachDisposition classifies a spacing request without making the
// caller infer lifecycle state from an unchanged coordinate.
type CombatApproachDisposition uint8

const (
	CombatApproachInvalid CombatApproachDisposition = iota
	CombatApproachHold
	CombatApproachMove
)

const combatApproachQuantizationInset = 1.0

// Valid rejects missing or corrupt authority data. Body radii must be
// positive because a zero silently recreates the root-overlap defect; reach
// may be zero for a body-contact action.
func (spacing CombatSpacing) Valid() bool {
	actor := float64(spacing.ActorBodyRadius)
	target := float64(spacing.TargetBodyRadius)
	reach := float64(spacing.ActionReach)
	return finitePositive(actor) && finitePositive(target) && finiteNonNegative(reach)
}

// AdmissionRadius returns the center-to-center radius at which the action is
// legal. Retail's AutoCommand action setup starts with both actors' vtable
// +0x560 BCRadius values and then applies action-specific reach/modifiers.
func (spacing CombatSpacing) AdmissionRadius() float64 {
	if !spacing.Valid() {
		return 0
	}
	return float64(spacing.ActorBodyRadius) +
		float64(spacing.TargetBodyRadius) +
		float64(spacing.ActionReach)
}

// StandOffRadius is the inner approach radius, not the outer attack radius.
// The retail server: reach >4 and <=10 subtracts 2;
// reach >10 uses 0.7 of reach for the inner radius, retaining full outer reach.
// Both still include both bodies. Collapsing the two radii to a one-unit inset
// makes walking targets outrun the margin between navigation ticks forever.
// For reach <=4 retain our wire-quantization inset (explicit compatibility
// policy); the adjacent-version server alone cannot certify v1.150 constants.
func (spacing CombatSpacing) StandOffRadius() float64 {
	if !spacing.Valid() {
		return 0
	}
	reach := float64(spacing.ActionReach)
	if reach > 4 {
		inner := reach - 2
		if reach > 10 {
			inner = reach * 0.7
		}
		return float64(spacing.ActorBodyRadius) + float64(spacing.TargetBodyRadius) + inner
	}
	return math.Max(0, spacing.AdmissionRadius()-combatApproachQuantizationInset)
}

// Contains reports whether two live poses are already within the action's
// complete center-to-center radius.
func (spacing CombatSpacing) Contains(actor, target Spawn) bool {
	return spacing.Valid() &&
		worldgeom.SamePlane(actor.RegionID, target.RegionID) &&
		WorldDistance2D(actor, target) <= spacing.AdmissionRadius()
}

// ApproachGoal derives the only movement destination allowed for a combat
// approach. It consumes the same CombatSpacing value as Contains, preventing
// admission and pursuit from drifting into independent formulas.
func (spacing CombatSpacing) ApproachGoal(actor, target Spawn) (Spawn, CombatApproachDisposition) {
	if !spacing.Valid() || !worldgeom.SamePlane(actor.RegionID, target.RegionID) {
		return actor, CombatApproachInvalid
	}

	targetLocal := RegionLocalToSeedLocal(actor.RegionID, target.RegionID,
		Vec3{X: target.X, Y: target.Y, Z: target.Z}, NativeRegionSize)
	dx, dz := targetLocal.X-actor.X, targetLocal.Z-actor.Z
	distance := math.Hypot(dx, dz)
	if distance <= spacing.AdmissionRadius() {
		return actor, CombatApproachHold
	}

	travelDistance := math.Max(0, distance-spacing.StandOffRadius())
	seed := Vec3{
		X: actor.X + dx/distance*travelDistance,
		Y: target.Y,
		Z: actor.Z + dz/distance*travelDistance,
	}
	regionID := RegionIDFromSeedLocal(actor.RegionID, seed.X, seed.Z, NativeRegionSize)
	local := SeedLocalToRegionLocal(actor.RegionID, regionID, seed, NativeRegionSize)
	local.X = math.Round(local.X)
	local.Z = math.Round(local.Z)
	goal := NormalizeSpawnFrame(Spawn{
		RegionID: regionID,
		X:        local.X,
		Y:        local.Y,
		Z:        local.Z,
		Angle:    actor.Angle,
	})
	return goal, CombatApproachMove
}

func finitePositive(value float64) bool {
	return finiteNonNegative(value) && value > 0
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
