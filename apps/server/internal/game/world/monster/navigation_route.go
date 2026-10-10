package monster

// NavigationRoute is immutable geometry, owned by one admitted mover. Points
// are wire-expressible destinations; each leg is revalidated at departure.
// The caller's goal remains separate from the next waypoint and clipped rest.
type NavigationRoute struct {
	status NavigationRouteStatus
	goal   Pose
	points []Pose
	// probes is the path queries the search spent (for metrics only).
	probes int
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
func (r *NavigationRoute) Probes() int          { return r.probes }
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
