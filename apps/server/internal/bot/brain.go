/*
===========================================================================

brain.go - the bot brain state machine

The brain reads the world through WorldView and gives back actions through
Actions. It never knows the connection. The states are idle, search,
approach, fight, loot, recover and respawn.

The approach state walks toward a distant monster. The server clips a blocked
straight move to the first blocking contact, so the brain reads the committed
position from the movement acknowledgement (the caller adapts the socket) and
repeats from there. A target that does not get closer for a while is
blacklisted for a cooldown, so the bot gives up on an unreachable monster
instead of spinning. This mirrors the blacklist behaviour of the open-source
Silkroad bots (RSBot TargetBundle: a behind-obstacle mob is blacklisted).

Phase 2 of issue #74 (issue #76).

===========================================================================
*/
package bot

import (
	"math"
	"math/rand"
	"sort"

	"opensro.online/server/internal/game/world/simulation"
)

// State is one brain state.
type State uint8

const (
	StateIdle State = iota
	StateSearch
	StateApproach
	StateFight
	StateLoot
	StateRecover
	StateRespawn
	StateTravel
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateSearch:
		return "search"
	case StateApproach:
		return "approach"
	case StateFight:
		return "fight"
	case StateLoot:
		return "loot"
	case StateRecover:
		return "recover"
	case StateRespawn:
		return "respawn"
	case StateTravel:
		return "travel"
	default:
		return "unknown"
	}
}

// Self is the bot's own live state.
type Self struct {
	GID    uint32
	Region uint16
	X      float64
	Y      float64
	Z      float64
	HP     int64
	MaxHP  int64
	MP     int64
	Dead   bool
	// Level is the bot's own level, from the char-data packet and the level-up
	// presentation.
	Level int
	// Aggressor is the gid of the monster that most recently attacked the bot.
	// The world view exposes it only while the hit is fresh. A zero value
	// means no recent attacker. The brain prefers it as the target
	// (issue #185).
	Aggressor uint32
	// StatPoints is the absolute remaining stat-point pool the spender
	// tracks from the last level-up tail or a 0x30B3 type-3 update
	// (issue #200). The value is an estimate between the server's absolute
	// updates.
	StatPoints int
	// PositionKnown is set once the world reported the bot's own position.
	// Before the first movement acknowledgement the position is the zero
	// value, so the brain must not estimate an arrival from it.
	PositionKnown bool
	// NowMs is the current wall clock. The caller sets it, so the brain can
	// estimate a travel time without a clock of its own.
	NowMs int64
}

// WorldView is the brain's read model. The bot adapts the socket to it.
type WorldView interface {
	Self() Self
	Monsters() []Actor
	GroundItems() []GroundItem
}

// Actions is the brain's action surface.
type Actions interface {
	MoveTo(region uint16, x, y, z float64) error
	Attack(gid uint32) error
	UsePotion() error
	Respawn() error
	Pickup(gid uint32) error
	// Select records the gate NPC selection the portal request consumes
	// (issue #171).
	Select(gid uint32) error
	// Portal sends the directed gate-travel request (issue #171).
	Portal(gid, targetID uint32) error
}

// LevelBand pairs a level with the target the bot travels to when it reaches
// that level (issue #171). The bands select the hunting region: the highest
// band whose level the bot reached wins.
type LevelBand struct {
	Level  int
	Target Point
}

// SelectLevelBand returns the target of the highest band whose level the bot
// reached (issue #171). The second result is false when the level is below
// every band. A caller that keeps the single -target behavior passes the base
// target as a separate fallback.
func SelectLevelBand(levels []LevelBand, level int) (Point, bool) {
	best := -1
	for i := range levels {
		if levels[i].Level <= level && (best < 0 || levels[i].Level >= levels[best].Level) {
			best = i
		}
	}
	if best < 0 {
		return Point{}, false
	}
	return levels[best].Target, true
}

// BrainConfig tunes the brain.
type BrainConfig struct {
	// Refs is the monster refObjId set the brain treats as targets.
	Refs MonsterRefs
	// AttackRange is the melee distance in world units.
	AttackRange float64
	// ApproachHysteresis is the extra distance beyond AttackRange within which
	// the brain keeps its current fight/approach decision. It stops the state
	// from flipping when a target hovers at the attack edge. Attacks still go
	// out only within AttackRange, the server range. A zero value uses 8.
	ApproachHysteresis float64
	// PotionHPBelow is the HP fraction that triggers a potion.
	PotionHPBelow float64
	// LootRange is the distance within which the brain loots its own drops.
	LootRange float64
	// LootItems loots item drops too. The default loots gold heaps only.
	LootItems bool
	// LootGraceTicks holds the target choice for this many ticks after a
	// target is lost, so the own drop can appear before the brain picks a new
	// monster. The server grants the drop a tick or more after the monster
	// despawn frame. A zero value uses 10.
	LootGraceTicks int
	// TargetRange bounds the monster pick distance in world units. A zero
	// value uses the whole region.
	TargetRange float64
	// FightTicks bounds how long one target stays engaged without a kill. The
	// brain blacklists the target after it. A zero value uses 120.
	FightTicks int
	// LevelGoal marks the goal met when the bot reaches this level. A zero
	// value disables the goal.
	LevelGoal int
	// Retaliate prefers the monster that most recently attacked the bot as the
	// target, over the loot grace and a different current target (issue #185).
	// The service flag defaults on. A false value keeps the ordinary pick.
	Retaliate bool
	// AggressorTicks bounds the freshness window of Self.Aggressor. The world
	// view applies the same window, so the brain sees a zero aggressor once
	// the hit is older. A zero value uses 20 (about 10 s at a 500 ms tick).
	AggressorTicks int
	// ReissueRange is how far a target must move before the approach sends a
	// new move. A new move every tick restarts the walk and makes the bot
	// jitter, so the approach holds the current destination until the target
	// leaves this radius.
	ReissueRange float64
	// Pathfinder plans a route around an obstacle. Nil uses the straight
	// chord with the clip and the blacklist only.
	Pathfinder Pathfinder
	// MoveSpeed is the lower-bound travel speed in world units per second. The
	// approach waits for the estimated arrival before it sends the next move,
	// so a slow estimate is safe. A zero value uses the wire walk speed.
	MoveSpeed float64
	// StuckTicks is the number of approach ticks without progress before the
	// target is blacklisted.
	StuckTicks int
	// BlacklistTicks is how long a blacklisted target is skipped.
	BlacklistTicks int
	// Stochastic adds a bounded random choice. A deterministic brain keeps
	// the nearest target and a fixed order.
	Stochastic bool
	// Seed fixes the stochastic choices.
	Seed int64
	// Route is an authored travel route. The brain walks it in order before
	// it searches for a monster. The last waypoint is the destination.
	Route []Point
	// Target is a navigation destination. The brain plans a route from its
	// live position to the target and walks it before it hunts. It replaces
	// the authored Route.
	Target *Point
	// RoutePlanner plans the route to Target over the server navmesh. A nil
	// planner leaves the target unplanned.
	RoutePlanner Pathfinder
	// Radius is the hunt radius in world units. When no monster is reachable
	// the brain roams to a random point inside the radius of Target and
	// searches again (issue #170). A positive value enables the roam; a zero
	// or negative value disables it.
	Radius float64
	// Levels are the level bands (issue #171). When the bot reaches a band
	// level it travels to that band's target region and hunts there. An empty
	// list keeps the single Target behavior.
	Levels []LevelBand
	// Portals is the shipped teleport catalogue the brain plans cross-region
	// travel with (issue #171). A nil catalogue leaves cross-region travel to
	// the navmesh planner.
	Portals *PortalCatalog
	// ProgressDeadlineMs bounds a navigation hold without a confirmed position
	// change. Past it the brain enters the nav-failed state and hunts where it
	// is. A zero value uses 30000.
	ProgressDeadlineMs int64
	// NavRetries bounds the consecutive failed route plans before navigation
	// fails. A zero value uses 5.
	NavRetries int
}

// Brain is the phase-2 state machine.
type Brain struct {
	cfg       BrainConfig
	rng       *rand.Rand
	state     State
	target    *Actor
	blacklist map[uint32]int
	// levelGoalMet is set once the bot reached the level goal.
	levelGoalMet bool
	// rebirthWait bounds repeated requests while the server restores life.
	rebirthWait    int
	recoverySeen   bool
	recoveryHP     int64
	recoveryTicks  int
	recoveryReason string
	// lootGID is the drop the current loot attempt belongs to. lootTicks
	// counts the loot ticks without the drop disappearing; a drop that stays
	// is blacklisted, so an unreachable drop cannot block the loop.
	lootGID       uint32
	lootTicks     int
	lootBlacklist map[uint32]int
	// lootGraceLeft counts the loot grace ticks left after a target was lost.
	// It is armed when a target leaves the view and cleared when the own drop
	// is looted or the grace expires.
	lootGraceLeft int
	// approachGID is the target the progress window belongs to. arriveMs is
	// the estimated arrival of the move in flight; the approach sends no new
	// move before it, because the server starts a new move from the live
	// position. settleSeen/settleX/settleZ are the committed position at the
	// last arrival, so a repeated goal counts as no progress. moveGID and
	// moveX/moveZ are the destination the approach last sent, so the approach
	// does not restart the walk every tick. route is the planned waypoint list
	// and routeGID and routeGoalX/routeGoalZ identify the target it belongs
	// to. planFailed/planFailGID/planFailX/planFailZ hold a failed plan until
	// the target moves.
	approachGID uint32
	arriveMs    int64
	settleSeen  bool
	settleX     float64
	settleZ     float64
	stuckTicks  int
	// fightGID is the target the fight window belongs to. fightTicks counts
	// the fight ticks; a target that stays too long is blacklisted.
	fightGID     uint32
	fightTicks   int
	moveGID      uint32
	moveX, moveZ float64
	route        []Point
	routeGID     uint32
	routeGoalX   float64
	routeGoalZ   float64
	planFailed   bool
	planFailGID  uint32
	planFailX    float64
	planFailZ    float64
	// travel is the remaining authored travel route. The brain walks it
	// before it searches for a monster.
	travel []Point
	// navTarget is the navigation destination. navRoute is the planned route
	// to it, navLeg is the goal of the last sent leg and navSent reports that
	// a leg is in flight. A leg the server clipped makes the route stale.
	navTarget *Point
	navRoute  []Point
	navLeg    Point
	navSent   bool
	// roamTarget is the current roam destination and roamRoute the planned
	// route to it. roamLeg is the goal of the last sent leg and roamSent
	// reports that a leg is in flight. roamDeadlineMs bounds the roam, so a
	// roam cannot hold the search forever (issue #170).
	roamTarget     *Point
	roamRoute      []Point
	roamLeg        Point
	roamSent       bool
	roamDeadlineMs int64
	// portalHops is the planned gate route to the active band target.
	// portalPlanRegion is the region the plan was made from and portalPlanSet
	// reports a fresh plan. The server resets the scene on each crossing, so
	// the plan is rebuilt from the new region (issue #171).
	portalHops       []PortalHop
	portalPlanRegion uint16
	portalPlanSet    bool
	// portalSent reports that the select and portal requests went out and the
	// brain waits for the region change. portalWaitUntilMs bounds the wait.
	portalSent        bool
	portalWaitUntilMs int64
	// portalEvent is the last portal request, handed to the telemetry once.
	portalEvent *PortalEvent
	// navFailed is the final navigation failure state. The target is cleared,
	// so the brain hunts where it is instead of holding in travel.
	navFailed bool
	// holdReason is the last navigation hold reason, exposed through NavStatus.
	holdReason string
	// routeLen is the planned nav route length at the last navigation tick.
	routeLen int
	// navPlanFailed reports that the last nav plan failed.
	navPlanFailed bool
	// planRetries counts consecutive failed nav plans; a confirmed position
	// change resets it.
	planRetries int
	// progressSeen seeds the progress window. progressX/progressZ are the last
	// confirmed position and progressMs the wall clock at that confirmation.
	progressSeen bool
	progressX    float64
	progressZ    float64
	progressMs   int64
	// nowMs and positionKnown are the last tick's clock and position status,
	// so NavStatus can report without a Self.
	nowMs         int64
	positionKnown bool
}

// NewBrain builds a brain.
func NewBrain(cfg BrainConfig) *Brain {
	if cfg.AttackRange <= 0 {
		cfg.AttackRange = 30
	}
	if cfg.ApproachHysteresis <= 0 {
		cfg.ApproachHysteresis = 8
	}
	if cfg.PotionHPBelow <= 0 {
		cfg.PotionHPBelow = 0.4
	}
	if cfg.LootRange <= 0 {
		cfg.LootRange = 200
	}
	if cfg.LootGraceTicks <= 0 {
		cfg.LootGraceTicks = 10
	}
	if cfg.ReissueRange <= 0 {
		cfg.ReissueRange = 25
	}
	if cfg.MoveSpeed <= 0 {
		cfg.MoveSpeed = simulation.WalkSpeed
	}
	if cfg.StuckTicks <= 0 {
		cfg.StuckTicks = 8
	}
	if cfg.BlacklistTicks <= 0 {
		cfg.BlacklistTicks = 20
	}
	if cfg.FightTicks <= 0 {
		cfg.FightTicks = 120
	}
	if cfg.AggressorTicks <= 0 {
		cfg.AggressorTicks = 20
	}
	if cfg.ProgressDeadlineMs <= 0 {
		cfg.ProgressDeadlineMs = 30000
	}
	if cfg.NavRetries <= 0 {
		cfg.NavRetries = 5
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = 1
	}
	return &Brain{
		cfg:           cfg,
		rng:           rand.New(rand.NewSource(seed)),
		state:         StateIdle,
		blacklist:     map[uint32]int{},
		lootBlacklist: map[uint32]int{},
		navTarget:     cfg.Target,
		travel:        append([]Point(nil), cfg.Route...),
	}
}

// State returns the current state.
func (b *Brain) State() State { return b.state }

// Target returns the current target, or nil.
func (b *Brain) Target() *Actor { return b.target }

// LevelGoalMet reports whether the bot reached the level goal.
func (b *Brain) LevelGoalMet() bool { return b.levelGoalMet }

// NavStatus is a snapshot of the navigation hold, so the caller can record why
// the bot is in travel without reading brain internals. The JSON tags are the
// telemetry field names.
type NavStatus struct {
	// HoldReason is why the last navigation tick did not move: waiting-arrival,
	// position-unknown, no-planner, plan-failed, route-exhausted, leg-clipped,
	// arrived or nav-failed. A roam hold adds roaming, roam-arrived,
	// roam-timeout or roam-failed. A portal crossing adds portal-sent,
	// portal-wait, portal-arrived or portal-timeout. Empty means the brain
	// sent a move.
	HoldReason string `json:"holdReason"`
	// PositionKnown reports whether the world reported the bot's own position.
	PositionKnown bool `json:"positionKnown"`
	// RouteLen is the planned nav route length at the last tick.
	RouteLen int `json:"routeLen"`
	// PlanFailed reports that the last nav plan failed.
	PlanFailed bool `json:"planFailed"`
	// ArriveInMs is the remaining arrival estimate of the leg in flight.
	ArriveInMs int64 `json:"arriveInMs"`
	// SinceProgressMs is the time since the last confirmed position change.
	SinceProgressMs int64 `json:"sinceProgressMs"`
}

// NavStatus returns the navigation hold telemetry of the last tick.
func (b *Brain) NavStatus() NavStatus {
	arriveIn := b.arriveMs - b.nowMs
	if arriveIn < 0 {
		arriveIn = 0
	}
	since := b.nowMs - b.progressMs
	if since < 0 {
		since = 0
	}
	return NavStatus{
		HoldReason:      b.holdReason,
		PositionKnown:   b.positionKnown,
		RouteLen:        b.routeLen,
		PlanFailed:      b.navPlanFailed,
		ArriveInMs:      arriveIn,
		SinceProgressMs: since,
	}
}

// NavFailed reports whether navigation ended in the explicit failure state.
func (b *Brain) NavFailed() bool { return b.navFailed }

// Roaming reports whether the brain roams inside the hunt radius (issue #170).
func (b *Brain) Roaming() bool { return b.roamTarget != nil }

// RoamTarget returns the current roam destination, or nil.
func (b *Brain) RoamTarget() *Point { return b.roamTarget }

// TakePortalEvent returns the last portal request once, or nil when no portal
// request is pending (issue #171). The service records it as telemetry.
func (b *Brain) TakePortalEvent() *PortalEvent {
	event := b.portalEvent
	b.portalEvent = nil
	return event
}

// Tick runs one brain step.
func (b *Brain) Tick(view WorldView, actions Actions) error {
	b.ageBlacklist(b.blacklist)
	b.ageBlacklist(b.lootBlacklist)
	self := view.Self()
	b.nowMs = self.NowMs
	b.positionKnown = self.PositionKnown
	// Clear the navigation hold reason: a non-navigation tick must not report
	// a stale hold from an earlier travel tick. navigateStep/travelStep set it
	// again when they hold.
	b.holdReason = ""
	if b.cfg.LevelGoal > 0 && self.Level >= b.cfg.LevelGoal {
		b.levelGoalMet = true
	}
	if self.Dead {
		b.state = StateRespawn
		b.target = nil
		b.resetApproach()
		if b.rebirthWait > 0 {
			b.rebirthWait--
			return nil
		}
		b.rebirthWait = 9
		return actions.Respawn()
	}
	b.rebirthWait = 0
	if waiting, err := b.recover(self, actions); waiting || err != nil {
		b.state = StateRecover
		return err
	}
	// The level bands come first (issue #171): when the bot reached a band
	// level and the band target lies in another region, it travels there over
	// the NPC portals. In the band region the band target supersedes the
	// authored -target, so the brain hunts there.
	if len(b.cfg.Levels) > 0 {
		if target, ok := b.activeLevelBand(self); ok {
			if self.Region != target.Region {
				return b.regionTravelStep(self, target, actions)
			}
			b.navTarget = nil
		}
	}
	// The navigation target comes first: plan a route from the live position
	// and walk it, then hunt at the destination.
	if b.navTarget != nil {
		return b.navigateStep(self, actions)
	}
	// The authored travel route comes first: walk to the hunting area, then
	// hunt. The last waypoint is the destination.
	if len(b.travel) > 0 {
		return b.travelStep(self, actions)
	}
	// Retaliation (issue #185): the monster that most recently attacked the
	// bot takes priority over the loot grace and a different current target,
	// so the bot fights its attacker instead of walking to another monster.
	// The aggressor must be a visible monster that is not blacklisted. The
	// world view exposes it only while the hit is fresh.
	if b.cfg.Retaliate && self.Aggressor != 0 {
		if aggressor := b.visibleAggressor(view.Monsters(), self.Aggressor); aggressor != nil &&
			(b.target == nil || b.target.GID != aggressor.GID) {
			b.target = aggressor
			b.lootGraceLeft = 0
			b.resetApproach()
		}
	}
	// The roam continues while no monster is reachable (issue #170). A
	// reachable monster ends the roam, so the bot fights instead of walking
	// past it.
	if b.roamTarget != nil {
		if b.pick(view.Monsters(), self) != nil {
			b.resetRoam()
		} else {
			return b.roamStep(self, view.Monsters(), actions)
		}
	}
	if b.target == nil || !containsGID(view.Monsters(), b.target.GID) {
		// A target that just left the view arms a bounded loot grace: the
		// server grants the own drop a tick or more after the monster despawn
		// frame, so the ground-item list is often still empty at the first
		// tick. Hold the target choice for a few ticks, so the bot loots its
		// own drop before it walks to a new monster.
		lostTarget := b.target != nil && !containsGID(view.Monsters(), b.target.GID)
		b.resetApproach()
		if lostTarget {
			b.lootGraceLeft = b.cfg.LootGraceTicks
		}
		if drop := b.pickLoot(view.GroundItems(), self); drop != nil {
			// The own drop is on the ground: loot it and stop the grace.
			b.lootGraceLeft = 0
			b.state = StateLoot
			b.target = nil
			if drop.GID != b.lootGID {
				b.lootGID = drop.GID
				b.lootTicks = 0
			}
			b.lootTicks++
			if b.lootTicks <= b.cfg.StuckTicks {
				return actions.Pickup(drop.GID)
			}
			// The drop did not disappear. Blacklist it, so an unreachable
			// drop cannot block the hunt.
			b.lootBlacklist[drop.GID] = b.cfg.BlacklistTicks
		}
		if b.lootGraceLeft > 0 {
			// No own drop yet after a kill. Wait for the ground-item spawn
			// frame. Do not pick a new target, so the bot does not walk away
			// from the drop.
			b.lootGraceLeft--
			b.state = StateLoot
			b.target = nil
			return nil
		}
		b.lootGID = 0
		b.lootTicks = 0
		b.state = StateSearch
		b.target = b.pick(view.Monsters(), self)
		if b.target == nil {
			// No monster is reachable. Roam inside the hunt radius to find
			// one instead of standing still (issue #170).
			return b.roamStep(self, view.Monsters(), actions)
		}
	}
	distance := math.Hypot(b.target.X-self.X, b.target.Z-self.Z)
	if distance <= b.cfg.AttackRange {
		if b.target.GID != b.fightGID {
			b.fightGID = b.target.GID
			b.fightTicks = 0
		}
		b.fightTicks++
		if b.fightTicks > b.cfg.FightTicks {
			// The target stays in range without a kill. Give up, so an
			// unkillable target or a refused attack cannot hold the bot.
			b.giveUpFight()
			return nil
		}
		b.resetApproach()
		b.state = StateFight
		return actions.Attack(b.target.GID)
	}
	// Hysteresis: a target that drifted just past the attack edge keeps the
	// fight decision instead of restarting the walk. The brain sends no attack
	// outside AttackRange (the server range) and no move inside the band, so it
	// cannot attack and move in the same moment. The fight window still runs,
	// so a target that never returns cannot hold the bot.
	if b.state == StateFight && distance <= b.cfg.AttackRange+b.cfg.ApproachHysteresis {
		if b.target.GID != b.fightGID {
			b.fightGID = b.target.GID
			b.fightTicks = 0
		}
		b.fightTicks++
		if b.fightTicks > b.cfg.FightTicks {
			b.giveUpFight()
			return nil
		}
		return nil
	}
	b.state = StateApproach
	// The server acknowledges the goal at once but the character travels over
	// time. A move before the arrival starts from the live position, so the
	// planned leg would not hold. Wait for the estimated arrival.
	if b.arriveMs != 0 && self.NowMs < b.arriveMs {
		return nil
	}
	if b.stuck(self) {
		b.blacklist[b.target.GID] = b.cfg.BlacklistTicks
		b.target = nil
		b.resetApproach()
		b.state = StateSearch
		return nil
	}
	if b.cfg.Pathfinder != nil {
		return b.walkRoute(self, actions)
	}
	return b.straightMove(self, actions)
}

// travelStep walks the authored route one waypoint per arrival. The last
// waypoint ends the travel and the brain hunts.
func (b *Brain) travelStep(self Self, actions Actions) error {
	b.state = StateTravel
	if b.arriveMs != 0 && self.NowMs < b.arriveMs {
		b.holdReason = "waiting-arrival"
		return nil
	}
	for len(b.travel) > 1 && pointReached(self, b.travel[0]) {
		b.travel = b.travel[1:]
	}
	waypoint := b.travel[0]
	if pointReached(self, waypoint) {
		b.travel = b.travel[1:]
		b.arriveMs = 0
		b.holdReason = "route-exhausted"
		return nil
	}
	b.holdReason = ""
	return b.send(self, waypoint, actions)
}

// navigateStep walks to the navigation target. The brain plans a route from
// its live position with the navmesh planner and re-plans when the server
// clipped a leg. The acknowledgement carries the clipped destination, so a leg
// goal the bot did not reach is a clip. The last waypoint is the target and the
// brain hunts after it. A hold without confirmed progress is bounded: after
// ProgressDeadlineMs or NavRetries failed plans the brain enters the explicit
// nav-failed state and hunts where it is. It never roams unconditionally.
func (b *Brain) navigateStep(self Self, actions Actions) error {
	b.state = StateTravel
	b.confirmProgress(self)
	if b.arriveMs != 0 && self.NowMs < b.arriveMs {
		b.holdReason = "waiting-arrival"
		return nil
	}
	// The arrival deadline passed. A position that did not change for the whole
	// progress deadline means navigation is stuck; fail explicitly instead of
	// holding.
	if b.progressExpired(self) {
		b.failNav()
		return nil
	}
	if pointReached(self, *b.navTarget) {
		b.navTarget = nil
		b.navRoute = nil
		b.navSent = false
		b.holdReason = "arrived"
		return nil
	}
	if b.cfg.RoutePlanner == nil {
		b.holdReason = "no-planner"
		return nil
	}
	if !self.PositionKnown {
		b.holdReason = "position-unknown"
		return nil
	}
	if b.navSent && !pointReached(self, b.navLeg) {
		// The server clipped the leg. The planned route is stale.
		b.navRoute = nil
		b.holdReason = "leg-clipped"
	}
	if len(b.navRoute) == 0 {
		here := Point{Region: self.Region, X: self.X, Z: self.Z}
		route, ok := b.cfg.RoutePlanner.Plan(here, *b.navTarget)
		if !ok || len(route) == 0 {
			b.navPlanFailed = true
			b.planRetries++
			if b.planRetries > b.cfg.NavRetries {
				b.failNav()
				return nil
			}
			b.holdReason = "plan-failed"
			return nil
		}
		b.navPlanFailed = false
		b.planRetries = 0
		b.navRoute = route
	}
	for len(b.navRoute) > 1 && pointReached(self, b.navRoute[0]) {
		b.navRoute = b.navRoute[1:]
	}
	b.routeLen = len(b.navRoute)
	waypoint := b.navRoute[0]
	if pointReached(self, waypoint) {
		b.navRoute = b.navRoute[1:]
		b.arriveMs = 0
		b.navSent = false
		b.holdReason = "route-exhausted"
		return nil
	}
	b.navSent = true
	b.navLeg = waypoint
	b.holdReason = ""
	return b.send(self, waypoint, actions)
}

// portalWaitMs bounds a gate crossing. The server sends a scene reset on the
// crossing. Past this wait the brain drops the plan and re-plans or falls back
// to the navmesh (issue #171).
const portalWaitMs int64 = 8000

// activeLevelBand returns the target of the highest band the bot reached, or
// the base -target when the bot is below every band (issue #171). The second
// result is false when neither names a target.
func (b *Brain) activeLevelBand(self Self) (Point, bool) {
	if target, ok := SelectLevelBand(b.cfg.Levels, self.Level); ok {
		return target, true
	}
	if b.cfg.Target != nil {
		return *b.cfg.Target, true
	}
	return Point{}, false
}

// roamCenter returns the point the roam is centred on: the active level band
// target when bands are set, else the single -target (issue #171).
func (b *Brain) roamCenter(self Self) (Point, bool) {
	if len(b.cfg.Levels) > 0 {
		return b.activeLevelBand(self)
	}
	if b.cfg.Target != nil {
		return *b.cfg.Target, true
	}
	return Point{}, false
}

// regionTravelStep moves the bot to dest, which lies in another region. It
// plans gate hops over the portal catalogue and crosses them one by one. The
// server resets the scene on each crossing, so the plan is rebuilt from the
// new region. Without a catalogue or a path it falls back to the navmesh
// planner, which is a best effort across a region boundary (issue #171).
func (b *Brain) regionTravelStep(self Self, dest Point, actions Actions) error {
	b.state = StateTravel
	if b.cfg.Portals == nil || b.cfg.RoutePlanner == nil {
		return b.navigateTo(self, dest, actions)
	}
	if !b.portalPlanSet || b.portalPlanRegion != self.Region {
		hops, ok := b.cfg.Portals.Plan(self.Region, dest.Region)
		if !ok || len(hops) == 0 {
			b.clearPortalTravel()
			return b.navigateTo(self, dest, actions)
		}
		b.portalHops = hops
		b.portalPlanRegion = self.Region
		b.portalPlanSet = true
		// A fresh plan restarts the crossing.
		b.portalSent = false
	}
	hop := b.portalHops[0]
	gate := Point{Region: hop.GateRegion, X: hop.GateX, Y: hop.GateY, Z: hop.GateZ}
	if !pointReached(self, gate) {
		// Walk to the gate with the navigation planner. A scene reset clears
		// the crossing, so no stale portal request survives it.
		return b.navigateTo(self, gate, actions)
	}
	if !b.portalSent {
		if err := actions.Select(hop.GateGID); err != nil {
			return err
		}
		if err := actions.Portal(hop.GateGID, hop.DestinationID); err != nil {
			return err
		}
		b.portalSent = true
		b.portalWaitUntilMs = self.NowMs + portalWaitMs
		b.portalEvent = &PortalEvent{
			GateGID: hop.GateGID, DestinationID: hop.DestinationID,
			FromRegion: self.Region, ToRegion: hop.ArrivalRegion,
		}
		b.holdReason = "portal-sent"
		return nil
	}
	if self.Region == hop.ArrivalRegion {
		b.clearPortalTravel()
		b.holdReason = "portal-arrived"
		return nil
	}
	if self.NowMs >= b.portalWaitUntilMs {
		// The crossing did not complete. Drop the plan, so the next tick
		// re-plans or falls back to the navmesh.
		b.clearPortalTravel()
		b.holdReason = "portal-timeout"
		return nil
	}
	b.holdReason = "portal-wait"
	return nil
}

// navigateTo walks to a point with the navigation planner. It reuses
// navigateStep through navTarget, so the arrival estimate, the clip handling
// and the progress bound are shared (issue #171).
func (b *Brain) navigateTo(self Self, dest Point, actions Actions) error {
	point := dest
	b.navTarget = &point
	return b.navigateStep(self, actions)
}

// clearPortalTravel drops the planned crossings, so the next tick rebuilds the
// plan from the live region (issue #171).
func (b *Brain) clearPortalTravel() {
	b.portalHops = nil
	b.portalPlanSet = false
	b.portalPlanRegion = 0
	b.portalSent = false
	b.portalWaitUntilMs = 0
}

// roamAttempts bounds the candidate roam points the brain tries before it
// keeps searching in place.
const roamAttempts = 5

// roamStep walks to a random point inside the hunt radius when no monster is
// reachable. The point is chosen once per roam from the seeded rng, so a seeded
// run stays reproducible. The roam ends on arrival, on a bounded timeout, or
// when the roam cannot plan. It reuses the navigation planner and the send
// helper of the hunt-area navigation (issue #170).
func (b *Brain) roamStep(self Self, monsters []Actor, actions Actions) error {
	if !b.canRoam(self, monsters) {
		b.resetRoam()
		return nil
	}
	b.state = StateTravel
	b.holdReason = "roaming"
	if b.roamTarget == nil {
		target, route, ok := b.pickRoamPoint(self)
		if !ok {
			b.holdReason = "roam-failed"
			return nil
		}
		b.roamTarget = &target
		b.roamRoute = route
		b.roamSent = false
		b.roamDeadlineMs = self.NowMs + b.cfg.ProgressDeadlineMs
	}
	if b.arriveMs != 0 && self.NowMs < b.arriveMs {
		b.holdReason = "waiting-arrival"
		return nil
	}
	if self.NowMs >= b.roamDeadlineMs {
		// The roam made no arrival inside the bound. Give up and search
		// where the bot is, so the roam cannot hold the hunt.
		b.resetRoam()
		b.holdReason = "roam-timeout"
		return nil
	}
	if pointReached(self, *b.roamTarget) {
		b.resetRoam()
		b.holdReason = "roam-arrived"
		return nil
	}
	if b.roamSent && !pointReached(self, b.roamLeg) {
		// The server clipped the leg. The planned route is stale.
		b.roamRoute = nil
	}
	if len(b.roamRoute) == 0 {
		here := Point{Region: self.Region, X: self.X, Z: self.Z}
		route, ok := b.cfg.RoutePlanner.Plan(here, *b.roamTarget)
		if !ok || len(route) == 0 {
			b.resetRoam()
			b.holdReason = "roam-failed"
			return nil
		}
		b.roamRoute = route
	}
	for len(b.roamRoute) > 1 && pointReached(self, b.roamRoute[0]) {
		b.roamRoute = b.roamRoute[1:]
	}
	waypoint := b.roamRoute[0]
	if pointReached(self, waypoint) {
		b.roamRoute = b.roamRoute[1:]
		b.arriveMs = 0
		b.roamSent = false
		b.holdReason = "roaming"
		return nil
	}
	b.roamSent = true
	b.roamLeg = waypoint
	b.holdReason = ""
	return b.send(self, waypoint, actions)
}

// canRoam reports whether a roam may run: a positive radius, a hunt-area
// target and a planner, a known position and no monster in attack range. A
// zero or negative radius or a nil target disables the roam (issue #170).
func (b *Brain) canRoam(self Self, monsters []Actor) bool {
	if b.cfg.Radius <= 0 || b.cfg.RoutePlanner == nil {
		return false
	}
	if _, ok := b.roamCenter(self); !ok {
		return false
	}
	if !self.PositionKnown {
		return false
	}
	return !monsterInRange(monsters, self, b.cfg.AttackRange)
}

// pickRoamPoint picks a random point inside the hunt radius of the target and
// plans a route to it. The angle and the distance come from the seeded rng, so
// a seeded run stays reproducible. The brain tries roamAttempts candidates and
// reports false when none is reachable, so the bot keeps searching.
func (b *Brain) pickRoamPoint(self Self) (Point, []Point, bool) {
	center, ok := b.roamCenter(self)
	if !ok {
		return Point{}, nil, false
	}
	here := Point{Region: self.Region, X: self.X, Z: self.Z}
	for attempt := 0; attempt < roamAttempts; attempt++ {
		angle := b.rng.Float64() * 2 * math.Pi
		distance := math.Sqrt(b.rng.Float64()) * b.cfg.Radius
		candidate := Point{
			Region: center.Region,
			X:      center.X + math.Cos(angle)*distance,
			Y:      center.Y,
			Z:      center.Z + math.Sin(angle)*distance,
		}
		if route, ok := b.cfg.RoutePlanner.Plan(here, candidate); ok && len(route) > 0 {
			return candidate, route, true
		}
	}
	return Point{}, nil, false
}

// resetRoam clears the roam state, so the brain resumes the normal search.
func (b *Brain) resetRoam() {
	b.roamTarget = nil
	b.roamRoute = nil
	b.roamLeg = Point{}
	b.roamSent = false
	b.roamDeadlineMs = 0
	b.arriveMs = 0
}

// monsterInRange reports whether a monster in the bot's region is within
// radius of the bot.
func monsterInRange(monsters []Actor, self Self, radius float64) bool {
	for i := range monsters {
		if monsters[i].Region != self.Region {
			continue
		}
		if math.Hypot(monsters[i].X-self.X, monsters[i].Z-self.Z) <= radius {
			return true
		}
	}
	return false
}

// confirmProgress seeds the progress window on the first navigation tick and
// resets it whenever the live position changed. A confirmed position change
// also clears a held plan failure, because the bot made progress.
func (b *Brain) confirmProgress(self Self) {
	if !b.progressSeen {
		b.progressSeen = true
		b.progressX, b.progressZ = self.X, self.Z
		b.progressMs = self.NowMs
		return
	}
	if !samePosition(self, b.progressX, b.progressZ) {
		b.progressX, b.progressZ = self.X, self.Z
		b.progressMs = self.NowMs
		b.planRetries = 0
		b.navPlanFailed = false
	}
}

// progressExpired reports that the live position did not change for longer than
// ProgressDeadlineMs. The window is seeded on the first navigation tick.
func (b *Brain) progressExpired(self Self) bool {
	if !b.progressSeen {
		return false
	}
	return self.NowMs-b.progressMs > b.cfg.ProgressDeadlineMs
}

// failNav enters the final navigation failure state: it clears the target and
// the route, so the brain hunts where it is instead of holding in travel.
func (b *Brain) failNav() {
	b.navFailed = true
	b.navTarget = nil
	b.navRoute = nil
	b.navSent = false
	b.arriveMs = 0
	b.routeLen = 0
	b.holdReason = "nav-failed"
}

// straightMove sends one straight chord to the target. The move is held until
// the target moves, so the walk does not restart every tick.
func (b *Brain) straightMove(self Self, actions Actions) error {
	target := b.target
	if b.moveGID == target.GID &&
		math.Hypot(target.X-b.moveX, target.Z-b.moveZ) <= b.cfg.ReissueRange {
		return nil
	}
	b.moveGID = target.GID
	b.moveX, b.moveZ = target.X, target.Z
	return b.send(self, Point{Region: target.Region, X: target.X, Y: target.Y, Z: target.Z}, actions)
}

// send writes the move and starts the arrival estimate.
func (b *Brain) send(self Self, dest Point, actions Actions) error {
	b.noteArrival(self, dest)
	return actions.MoveTo(dest.Region, dest.X, dest.Y, dest.Z)
}

// arrivalMarginMs covers the tick quantisation and the wire rounding.
const arrivalMarginMs = 200

// noteArrival estimates the travel time from the committed position to the
// destination. The estimate uses a lower-bound speed, so the wait is never
// shorter than the travel.
func (b *Brain) noteArrival(self Self, dest Point) {
	// The bot learns its own position from the first movement
	// acknowledgement. An estimate from the zero position would be hours long
	// and would stall the route, so keep the gate open until the position is
	// known.
	if !self.PositionKnown {
		b.arriveMs = 0
		return
	}
	length := pointDistance(Point{Region: self.Region, X: self.X, Z: self.Z}, dest)
	b.arriveMs = self.NowMs + int64(length/b.cfg.MoveSpeed*1000) + arrivalMarginMs
}

// walkRoute walks the planned route one waypoint per arrival. It re-plans when
// the route is gone, belongs to another target, or the target moved past the
// reissue range. A failed plan is held until the target moves.
func (b *Brain) walkRoute(self Self, actions Actions) error {
	target := b.target
	if b.planFailed && target.GID == b.planFailGID &&
		math.Hypot(target.X-b.planFailX, target.Z-b.planFailZ) <= b.cfg.ReissueRange {
		return b.straightMove(self, actions)
	}
	b.planFailed = false
	if len(b.route) == 0 || b.routeGID != target.GID ||
		math.Hypot(target.X-b.routeGoalX, target.Z-b.routeGoalZ) > b.cfg.ReissueRange {
		return b.planRoute(self, actions)
	}
	for len(b.route) > 1 && pointReached(self, b.route[0]) {
		b.route = b.route[1:]
		b.moveGID = 0
	}
	waypoint := b.route[0]
	if pointReached(self, waypoint) {
		// At the planned goal. The caller re-plans on the next tick when the
		// target moved; otherwise the target is in range and the caller fights.
		return nil
	}
	if b.moveGID == target.GID && b.moveX == waypoint.X && b.moveZ == waypoint.Z {
		return nil
	}
	b.moveGID = target.GID
	b.moveX, b.moveZ = waypoint.X, waypoint.Z
	return b.send(self, waypoint, actions)
}

// planRoute plans a route to the target. A failed plan falls back to the
// straight chord and is held until the target moves.
func (b *Brain) planRoute(self Self, actions Actions) error {
	target := b.target
	from := Point{Region: self.Region, X: self.X, Y: self.Y, Z: self.Z}
	to := Point{Region: target.Region, X: target.X, Y: target.Y, Z: target.Z}
	route, ok := b.cfg.Pathfinder.Plan(from, to)
	if !ok {
		b.route = nil
		b.routeGID = 0
		b.planFailed = true
		b.planFailGID = target.GID
		b.planFailX, b.planFailZ = target.X, target.Z
		b.moveGID = 0
		return b.straightMove(self, actions)
	}
	b.planFailed = false
	b.route = route
	b.routeGID = target.GID
	b.routeGoalX, b.routeGoalZ = target.X, target.Z
	b.moveGID = 0
	return b.walkRoute(self, actions)
}

// waypointReached is the distance at which a waypoint counts as reached. It
// is below the pathfinder grid, so the bot does not skip a waypoint.
const waypointReached = 20.0

func pointReached(self Self, p Point) bool {
	return self.Region == p.Region && math.Hypot(self.X-p.X, self.Z-p.Z) <= waypointReached
}

// stuck reports whether the approach made no progress. The first approach
// stuck reports whether the approach made no progress. The first approach
// tick only seeds the window. The window measures the committed position at
// each arrival, so a repeated goal (a blocked target) is no progress.
func (b *Brain) stuck(self Self) bool {
	if b.target == nil || b.approachGID != b.target.GID {
		b.approachGID = b.target.GID
		b.settleSeen = false
		b.stuckTicks = 0
		return false
	}
	if !b.settleSeen || !samePosition(self, b.settleX, b.settleZ) {
		b.settleSeen = true
		b.settleX, b.settleZ = self.X, self.Z
		b.stuckTicks = 0
		return false
	}
	b.stuckTicks++
	return b.stuckTicks >= b.cfg.StuckTicks
}

func samePosition(self Self, x, z float64) bool {
	return math.Hypot(self.X-x, self.Z-z) <= 0.5
}

// giveUpFight blacklists the current target and returns to search, so a target
// that stays in range without a kill cannot hold the bot.
func (b *Brain) giveUpFight() {
	b.blacklist[b.target.GID] = b.cfg.BlacklistTicks
	b.target = nil
	b.fightGID = 0
	b.fightTicks = 0
	b.resetApproach()
	b.state = StateSearch
}

func (b *Brain) resetApproach() {
	b.approachGID = 0
	b.arriveMs = 0
	b.settleSeen = false
	b.settleX, b.settleZ = 0, 0
	b.stuckTicks = 0
	b.moveGID = 0
	b.moveX, b.moveZ = 0, 0
	b.route = nil
	b.routeGID = 0
	b.routeGoalX, b.routeGoalZ = 0, 0
	b.planFailed = false
	b.planFailGID = 0
	b.planFailX, b.planFailZ = 0, 0
}

// ageBlacklist drops expired blacklist entries.
func (b *Brain) ageBlacklist(entries map[uint32]int) {
	for gid, ticks := range entries {
		if ticks <= 1 {
			delete(entries, gid)
			continue
		}
		entries[gid] = ticks - 1
	}
}

// pickLoot returns the nearest own drop in range, or nil. The bot only loots
// a drop it earned and only in its own region.
func (b *Brain) pickLoot(items []GroundItem, self Self) *GroundItem {
	best := -1
	bestDistance := b.cfg.LootRange
	for i := range items {
		item := items[i]
		if !item.HasOwner || item.OwnerJID != self.GID || item.Region != self.Region {
			continue
		}
		if _, blocked := b.lootBlacklist[item.GID]; blocked {
			continue
		}
		if !b.cfg.LootItems && !item.IsGold {
			continue
		}
		d := math.Hypot(item.X-self.X, item.Z-self.Z)
		if d < bestDistance || (d == bestDistance && (best < 0 || item.GID < items[best].GID)) {
			bestDistance = d
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &items[best]
}

// visibleAggressor returns the aggressor actor when it is a visible monster
// that is not blacklisted, or nil. Retaliation prefers it as the target
// (issue #185).
func (b *Brain) visibleAggressor(monsters []Actor, gid uint32) *Actor {
	if _, blocked := b.blacklist[gid]; blocked {
		return nil
	}
	for i := range monsters {
		if monsters[i].GID == gid {
			return &monsters[i]
		}
	}
	return nil
}

// pick returns the nearest monster that is not blacklisted, or a bounded
// random one in stochastic mode.
func (b *Brain) pick(monsters []Actor, self Self) *Actor {
	candidates := make([]Actor, 0, len(monsters))
	for _, m := range monsters {
		// A monster in another region is not reachable with a move from this
		// region. A foreign-region target made the bot stand still and cycle
		// through the blacklist.
		if m.Region != self.Region {
			continue
		}
		if _, blocked := b.blacklist[m.GID]; blocked {
			continue
		}
		if b.cfg.TargetRange > 0 && math.Hypot(m.X-self.X, m.Z-self.Z) > b.cfg.TargetRange {
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		di := math.Hypot(candidates[i].X-self.X, candidates[i].Z-self.Z)
		dj := math.Hypot(candidates[j].X-self.X, candidates[j].Z-self.Z)
		if di == dj {
			return candidates[i].GID < candidates[j].GID
		}
		return di < dj
	})
	if b.cfg.Stochastic && len(candidates) > 1 {
		// Pick among the nearest three, so the run stays realistic but the
		// seed still fixes the choice.
		limit := len(candidates)
		if limit > 3 {
			limit = 3
		}
		return &candidates[b.rng.Intn(limit)]
	}
	return &candidates[0]
}

func containsGID(monsters []Actor, gid uint32) bool {
	for _, m := range monsters {
		if m.GID == gid {
			return true
		}
	}
	return false
}
