/*
===========================================================================

companionmover_test.go - every mover transition keeps the pose

The stale-anchor bugs of PR #3464 were all one shape: an owner change
that did not carry the pose across. Each case here walks a companion
somewhere first, applies one transition, and asserts where it stands
after: in place for a stop, a displacement's target for a knockback,
the named pose for an explicit placement, nowhere after a release.

===========================================================================
*/
package simulation

import "testing"

const moverGID = uint32(77)

/*
================
walkedMover

A mover placed at the origin whose follower walked toward x=500 for
two seconds; returns it and the pose it reached.
================
*/
func walkedMover(t *testing.T) (*CompanionMover, Spawn) {
	t.Helper()
	var m CompanionMover
	start := Spawn{RegionID: 0x62a8, X: 100, Z: 100}
	open := func(_, to Spawn) (Spawn, *MoveError) { return to, nil }
	m.Place(moverGID, start).Advance(Spawn{RegionID: 0x62a8, X: 600, Z: 100}, 50, 1_000, open)
	reached, ok := m.Pose(moverGID, 3_000)
	if !ok || reached.X <= start.X {
		t.Fatalf("the follower never walked: %+v %v", reached, ok)
	}
	return &m, reached
}

/*
================
TestCompanionMoverTransitionsKeepThePose
================
*/
func TestCompanionMoverTransitionsKeepThePose(t *testing.T) {
	knock := Spawn{RegionID: 0x62a8, X: 50, Z: 50}
	named := Spawn{RegionID: 0x62a9, X: 900, Z: 900}
	for _, tc := range []struct {
		name  string
		apply func(m *CompanionMover)
		want  func(reached Spawn) (Spawn, bool)
	}{
		{"stop in place parks where it walked", func(m *CompanionMover) { m.StopInPlace(3_000) },
			func(reached Spawn) (Spawn, bool) { return reached, true }},
		{"a second stop keeps the parked pose", func(m *CompanionMover) { m.StopInPlace(3_000); m.StopInPlace(9_000) },
			func(reached Spawn) (Spawn, bool) { return reached, true }},
		{"following again sets out from where it stood", func(m *CompanionMover) {
			m.StopInPlace(3_000)
			pose, _ := m.Pose(moverGID, 3_000)
			m.FollowFrom(moverGID, pose)
		}, func(reached Spawn) (Spawn, bool) { return reached, true }},
		{"a knockback on the follower lands at its target", func(m *CompanionMover) { m.Displace(knock, 3_000) },
			func(Spawn) (Spawn, bool) { return knock, true }},
		{"a knockback while parked lands at its target", func(m *CompanionMover) { m.StopInPlace(3_000); m.Displace(knock, 3_000) },
			func(Spawn) (Spawn, bool) { return knock, true }},
		{"an explicit park names the pose", func(m *CompanionMover) { m.Park(moverGID, named) },
			func(Spawn) (Spawn, bool) { return named, true }},
		{"a placement names the pose", func(m *CompanionMover) { m.Place(moverGID, named) },
			func(Spawn) (Spawn, bool) { return named, true }},
		{"a release keeps no pose", func(m *CompanionMover) { m.Release() },
			func(Spawn) (Spawn, bool) { return Spawn{}, false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, reached := walkedMover(t)
			tc.apply(m)
			want, placed := tc.want(reached)
			got, ok := m.Pose(moverGID, 3_000)
			if ok != placed || got != want {
				t.Fatalf("pose %+v (%v), want %+v (%v)", got, ok, want, placed)
			}
			if world, ok := m.World(moverGID); ok != placed || (ok && world.LiveSpawnAt(3_000) != want) {
				t.Fatalf("world %+v (%v) disagrees with the pose %+v", world, ok, want)
			}
		})
	}
}

/*
================
TestCompanionMoverAnswersOnlyItsCompanion

A mover holds one companion's pose: another gid reads nothing, so a
caller never takes a stale sibling's pose for its own.
================
*/
func TestCompanionMoverAnswersOnlyItsCompanion(t *testing.T) {
	m, _ := walkedMover(t)
	if _, ok := m.Pose(moverGID+1, 3_000); ok || m.Follower(moverGID+1) != nil {
		t.Fatal("a mover answered for another companion")
	}
	m.StopInPlace(3_000)
	if _, ok := m.World(moverGID + 1); ok {
		t.Fatal("a parked mover answered for another companion")
	}
}
