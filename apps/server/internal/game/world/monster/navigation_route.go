package monster

import worldgeom "opensro.online/server/internal/game/world"

// NavigationRoute is immutable geometry, owned by one admitted mover. Points
// are wire-expressible destinations; each leg is revalidated at departure.
// The caller's goal remains separate from the next waypoint and clipped rest.
type NavigationRoute struct {
	status NavigationRouteStatus
	goal   Pose
	points []Pose
	// probes is the path queries the search spent (for metrics only).
	probes int
	// work is the planner's deterministic work in its own units, for
	// planners that count more than path queries (the cave route,
	// #1678); zero for planners that report probes only.
	work int
}

type NavigationRouteStatus uint8

const (
	NavigationRouteReady NavigationRouteStatus = iota
	NavigationGeometryUnavailable
	NavigationRouteBlocked
	NavigationSearchExhausted
)

func UnresolvedNavigationRoute(goal Pose, status NavigationRouteStatus) *NavigationRoute {
	if status == NavigationRouteReady {
		panic("unresolved navigation requires a failure reason")
	}
	return &NavigationRoute{goal: goal, status: status}
}
func (r *NavigationRoute) Status() NavigationRouteStatus { return r.status }

func NewNavigationRoute(goal Pose, points []Pose) *NavigationRoute {
	return &NavigationRoute{goal: goal, points: append([]Pose(nil), points...)}
}
func (r *NavigationRoute) Goal() Pose { return r.goal }

// WithProbes records the path queries the search spent; metrics read it.
func (r *NavigationRoute) WithProbes(n int) *NavigationRoute { r.probes = n; return r }

// Probes is the path queries the search spent.
func (r *NavigationRoute) Probes() int { return r.probes }

// WithWork records the planner's deterministic work; cost tests read it.
func (r *NavigationRoute) WithWork(n int) *NavigationRoute { r.work = n; return r }

// Work is the planner's deterministic work (zero when it counts probes only).
func (r *NavigationRoute) Work() int            { return r.work }
func (r *NavigationRoute) Len() int             { return len(r.points) }
func (r *NavigationRoute) Point(index int) Pose { return r.points[index] }

type navigationPhase uint8

const (
	navigationNone navigationPhase = iota
	navigationTravelling
	navigationWaiting
)

// This value belongs to the existing mover state machine, not a second AI.
// Failed routes retain intent with a bounded retry cadence. No shared cache
// can resurrect a cancelled target, leader or home request.
type navigationIntent struct {
	phase   navigationPhase
	goal    Pose
	route   *NavigationRoute
	index   int
	retryAt int64
	speed   float64
	channel uint8

	// The last route-search failure this intent is waiting out, keyed
	// exactly (the failed-route memo, #1820 part B): the pose and surface
	// owner the failed search ran with, and its outcome. BeginNavigation,
	// WaitNavigation and CancelNavigation all rebuild the intent, so the
	// record clears by construction; the waiting branch re-notes it after
	// each wait. The owner is part of the key because the cave search
	// starts from the mover's retained cell; a retry after a wait runs
	// with the unresolved owner, so a record taken under a retained owner
	// simply never matches and the real search runs once more.
	failFrom   Pose
	failOwner  worldgeom.NavOwner
	failStatus NavigationRouteStatus
	hasFail    bool
}

func (m MoverState) NavigationGoal() (Pose, bool) {
	return m.intent.goal, m.intent.phase != navigationNone
}
func (m MoverState) NavigationWaiting(now int64) bool {
	return m.intent.phase == navigationWaiting && now < m.intent.retryAt
}
func (m MoverState) NavigationHasDetour() bool {
	return m.intent.route != nil && m.intent.route.Len() > 1
}
func (m MoverState) NavigationWaypoint() (Pose, bool) {
	if m.intent.phase != navigationTravelling || m.intent.route == nil || m.intent.index >= m.intent.route.Len() {
		return Pose{}, false
	}
	return m.intent.route.Point(m.intent.index), true
}
func (m *MoverState) BeginNavigation(route *NavigationRoute) {
	m.intent = navigationIntent{phase: navigationTravelling, goal: route.Goal(), route: route, speed: m.intent.speed, channel: m.intent.channel}
}
func (m *MoverState) SetNavigationMotion(speed float64, channel uint8) {
	m.intent.speed, m.intent.channel = speed, channel
}
func (m MoverState) NavigationMotion() (float64, uint8) { return m.intent.speed, m.intent.channel }
func (m *MoverState) AdvanceNavigation()                { m.intent.index++ }
func (m *MoverState) WaitNavigation(goal Pose, retryAt int64) {
	m.intent = navigationIntent{phase: navigationWaiting, goal: goal, retryAt: retryAt, speed: m.intent.speed, channel: m.intent.channel}
}
func (m *MoverState) CancelNavigation() { m.intent = navigationIntent{} }

/*
================
MoverState.RouteFailure

The intent's failed-route record, when the mover is waiting out a failed
search (#1820 part B).
================
*/
func (m MoverState) RouteFailure() (Pose, worldgeom.NavOwner, NavigationRouteStatus, bool) {
	return m.intent.failFrom, m.intent.failOwner, m.intent.failStatus, m.intent.hasFail
}

/*
================
MoverState.NoteRouteFailure

Records the failed search this intent now waits out, called right after
WaitNavigation has rebuilt the intent. Only blocked and exhausted
searches are ever noted (the caller gates); an unavailable geometry or
a no-progress wait proves nothing about a recompute.
================
*/
func (m *MoverState) NoteRouteFailure(from Pose, owner worldgeom.NavOwner, status NavigationRouteStatus) {
	m.intent.failFrom, m.intent.failOwner, m.intent.failStatus, m.intent.hasFail = from, owner, status, true
}
