package simulation

import (
	"math"
	"testing"
)

func TestCombatSpacingCombinesBothBodiesAndActionReach(t *testing.T) {
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(4),
		TargetBodyRadius: BodyRadius(6),
		ActionReach:      ActionReach(6),
	}
	if got := spacing.AdmissionRadius(); got != 16 {
		t.Fatalf("admission radius = %v, want player body 4 + Baroi body 6 + sword reach 6", got)
	}
	if got := spacing.StandOffRadius(); got != 14 {
		t.Fatalf("stand-off radius = %v, want native inner melee radius", got)
	}
}

func TestCombatSpacingAuthorsRingGoalAndUsesSameRadiusForAdmission(t *testing.T) {
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(4),
		TargetBodyRadius: BodyRadius(6),
		ActionReach:      ActionReach(6),
	}
	actor := Spawn{RegionID: 0x62a8, X: 900, Y: 20, Z: 458}
	target := Spawn{RegionID: 0x62a8, X: 1000, Y: 20, Z: 458}
	goal, disposition := spacing.ApproachGoal(actor, target)
	if disposition != CombatApproachMove {
		t.Fatalf("disposition = %v, want move", disposition)
	}
	if got := WorldDistance2D(goal, target); math.Abs(got-14) > 0.01 {
		t.Fatalf("goal is %.3f units from target, want inner approach radius 14", got)
	}
	if !spacing.Contains(Spawn{RegionID: target.RegionID, X: target.X - 16, Y: target.Y, Z: target.Z}, target) {
		t.Fatal("admission rejected a pose on the same radius used to derive the goal")
	}
}

func TestCombatSpacingFailsClosedWithoutBodyAuthority(t *testing.T) {
	spacing := CombatSpacing{ActionReach: ActionReach(6)}
	actor := Spawn{RegionID: 0x62a8, X: 900, Z: 458}
	target := Spawn{RegionID: 0x62a8, X: 1000, Z: 458}
	if spacing.Valid() || spacing.Contains(actor, target) {
		t.Fatal("zero body radii must not silently recreate root-target combat")
	}
	if _, disposition := spacing.ApproachGoal(actor, target); disposition != CombatApproachInvalid {
		t.Fatalf("disposition = %v, want invalid", disposition)
	}
}

// Machine branch boundaries, not values derived from the implementation.
func TestCombatSpacingSeparatesInnerAndOuterActionRadii(t *testing.T) {
	for _, row := range []struct{ reach, inner, outer float64 }{
		{0, 9, 10}, {4, 13, 14}, {5, 13, 15}, {6, 14, 16}, {10, 18, 20}, {11, 17.7, 21}, {180, 136, 190},
	} {
		spacing := CombatSpacing{ActorBodyRadius: 4, TargetBodyRadius: 6, ActionReach: ActionReach(row.reach)}
		if math.Abs(spacing.StandOffRadius()-row.inner) > 0.0001 || spacing.AdmissionRadius() != row.outer {
			t.Fatalf("reach %v: got inner %v outer %v; want %v/%v", row.reach, spacing.StandOffRadius(), spacing.AdmissionRadius(), row.inner, row.outer)
		}
	}
}

// An actor and a target on different planes are never in reach and never
// approached, even at coordinates that would admit them on one plane: the
// outdoor sector grid and a cave floor share no space (#1646).
func TestCombatSpacingRefusesAcrossPlanes(t *testing.T) {
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(4),
		TargetBodyRadius: BodyRadius(6),
		ActionReach:      ActionReach(6),
	}
	outdoor := Spawn{RegionID: 0x62a8, X: 990, Y: 20, Z: 458}
	cave := Spawn{RegionID: 0x8001, X: 1000, Y: 20, Z: 458}
	if spacing.Contains(outdoor, cave) || spacing.Contains(cave, outdoor) {
		t.Fatal("a pair across the outdoor/cave boundary was admitted")
	}
	if _, disposition := spacing.ApproachGoal(outdoor, cave); disposition != CombatApproachInvalid {
		t.Fatalf("approach across planes = %v, want invalid", disposition)
	}
	sameCave := cave
	sameCave.X -= 10
	if !spacing.Contains(sameCave, cave) {
		t.Fatal("the same distance inside one cave was refused")
	}
}

/*
================
TestCombatSpacingCaveGoalStaysOnItsFloor

A cave floor's locals are its own frame, unbounded by the outdoor 1920
square (a Jangan cave floor sits near x -13580): the approach goal stays
on the actor's floor at the straight-line point, never folded into a
sector the cave does not have.
================
*/
func TestCombatSpacingCaveGoalStaysOnItsFloor(t *testing.T) {
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(4),
		TargetBodyRadius: BodyRadius(6),
		ActionReach:      ActionReach(6),
	}
	for _, row := range []struct{ x, z float64 }{{-13580, 5536}, {1700, 8008}, {900, 458}} {
		actor := Spawn{RegionID: 0x8006, X: row.x, Y: 0, Z: row.z}
		target := Spawn{RegionID: 0x8006, X: row.x + 100, Y: 0, Z: row.z}
		goal, disposition := spacing.ApproachGoal(actor, target)
		if disposition != CombatApproachMove {
			t.Fatalf("(%v, %v): disposition = %v, want move", row.x, row.z, disposition)
		}
		if goal.RegionID != actor.RegionID || goal.X != row.x+86 || goal.Z != row.z {
			t.Fatalf("(%v, %v): goal = %+v, want region %#x at (%v, %v)", row.x, row.z, goal, actor.RegionID, row.x+86, row.z)
		}
	}
}
