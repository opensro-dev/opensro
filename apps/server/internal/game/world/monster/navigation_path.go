package monster

import worldgeom "opensro.online/server/internal/game/world"

// NavigationPath is an immutable, admitted world-surface path. Keeping it on
// the mover makes combat, visibility, death and AI read the same surface even
// when they call LivePoseAt without a terrain resolver. The adapter must close
// over immutable geometry only; it must never call back into MonsterState.
type NavigationPath struct {
	from, goal, rest Pose
	result           uint32
	height           func(float64, Pose) (float64, bool)
	owners           []worldgeom.NavOwnerSpan
}

func NewNavigationPath(from, goal, rest Pose, result uint32, height func(float64, Pose) (float64, bool)) *NavigationPath {
	if height == nil {
		panic("navigation path requires its surface owner")
	}
	return &NavigationPath{from: from, goal: goal, rest: rest, result: result, height: height}
}
func (p *NavigationPath) Rest() Pose     { return p.rest }
func (p *NavigationPath) Goal() Pose     { return p.goal }
func (p *NavigationPath) Result() uint32 { return p.result }

// AdoptNavigation binds the immutable path to exactly this segment. A later
// replacement/settle cannot accidentally reuse an old surface cursor.
func (m *MoverState) AdoptNavigation(path *NavigationPath) { m.navigation = path }
func (m MoverState) navigationMatches() bool {
	if m.navigation == nil {
		return false
	}
	a, b := m.From, m.To
	a.Heading, b.Heading = m.navigation.from.Heading, m.navigation.rest.Heading
	return a == m.navigation.from && b == m.navigation.rest
}

// restsOnNavigation reports a settled mover standing exactly on the adopted
// path's rest (the heading aside).
func (m MoverState) restsOnNavigation() bool {
	pose := m.Pose
	pose.Heading = m.navigation.rest.Heading
	return pose == m.navigation.rest
}
func (m MoverState) MovementGoal() Pose {
	if m.navigationMatches() {
		return m.navigation.goal
	}
	return m.To
}

// WithOwners returns a copy of the path carrying the surface owners the
// movement authority walked along from -> rest (world/navowner.go). The next
// plan starts from the owner under the mover instead of re-guessing it from
// height (the server's movement query walks from the stored source cell).
func (p *NavigationPath) WithOwners(spans []worldgeom.NavOwnerSpan) *NavigationPath {
	if p == nil {
		return nil
	}
	next := *p
	next.owners = append([]worldgeom.NavOwnerSpan(nil), spans...)
	return &next
}

// LiveNavOwner is the surface owner under LivePoseAt(nowMs): the walked span
// at the live fraction of the adopted path, or its end once arrived. A
// settled mover (the segment cleared, the pose set to the rest) still standing
// on the adopted path's rest keeps the rest's owner, as the original keeps the
// cell in the actor's position record between moves; any other pose change
// drops it. It is unresolved when no owned path covers the mover.
func (m MoverState) LiveNavOwner(nowMs int64) worldgeom.NavOwner {
	if m.navigation == nil || len(m.navigation.owners) == 0 {
		return worldgeom.NavOwner{}
	}
	if !m.navigationMatches() {
		if m.ArriveMs > m.DepartMs || !m.restsOnNavigation() {
			return worldgeom.NavOwner{}
		}
		return worldgeom.OwnerAtFraction(m.navigation.owners, 1)
	}
	if !m.InFlight(nowMs) {
		if m.ArriveMs > m.DepartMs {
			return worldgeom.OwnerAtFraction(m.navigation.owners, 1)
		}
		return worldgeom.NavOwner{}
	}
	t := float64(nowMs-m.DepartMs) / float64(m.ArriveMs-m.DepartMs)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return worldgeom.OwnerAtFraction(m.navigation.owners, t)
}
