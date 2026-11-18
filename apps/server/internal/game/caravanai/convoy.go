/*
===========================================================================

convoy.go - escort roaming, hiring, standing runs and the grand caravan

Port-only. Distances are world units: 10 units are one in-game metre (skill
ranges read 180 units as 18m).

  - Followers roam within 50m of whoever they follow, run after them past
    120m and are teleported to them past 200m.
  - Any idle agent can be hired at a specialty shop for one trade run, at
    most an hour. Hunters and Traders hire Hunters and Traders; Thieves
    hire Thieves. Hired Traders carry goods the hirer pays for and pay the
    sale back on delivery.
  - Two solo runs and two runs with three Hunters each are kept on the road;
    every few hours every idle Trader and Hunter musters in one town and
    leaves as one grand caravan.
  - A robbed Trader waits 30 minutes before its next own run.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
	worldgeom "opensro.online/server/internal/game/world"
)

const (
	UnitsPerMeter = 10

	EscortRoamRadius       float64 = 50 * UnitsPerMeter
	EscortFollowDistance   float64 = 120 * UnitsPerMeter
	EscortTeleportDistance float64 = 200 * UnitsPerMeter

	HireFeeGold       int64 = BaseHunterHireFeeGold
	HireMaxDurationMs int64 = 60 * 60_000
	// HireLeaderMissingMs ends a hire whose hirer stays offline or out of
	// the main world this long.
	HireLeaderMissingMs int64 = 2 * 60_000
	// HireRunMinOutMs: back in the town the hirer left counts as a finished
	// run only after this long outside (a quick step out is not a run).
	HireRunMinOutMs int64 = 10 * 60_000
	// HiredGoodsMarginPerStarPct is the sale margin a hired Trader earns its
	// hirer per cargo star on delivery.
	HiredGoodsMarginPerStarPct = 15
	MaxHiresPerPlayer          = 7

	RobbedCooldownMs int64 = 30 * 60_000

	StandingSoloRuns     = 2
	StandingEscortedRuns = 2
	EscortsPerRun        = 3

	GrandCaravanFirstMs    int64 = 30 * 60_000
	GrandCaravanIntervalMs int64 = 3 * 60 * 60_000
	GrandCaravanMusterMs   int64 = 2 * 60_000

	townWanderRadius float64 = 30 * UnitsPerMeter
	townWanderInner  float64 = 8 * UnitsPerMeter
	// WalkStepPerTick is the client's walk speed (20 units/s) per 100ms step.
	WalkStepPerTick      float32 = 2
	distressAnswerRadius float64 = 300 * UnitsPerMeter

	scenicDetourMinLeg float64 = 1500
	stuckSkipMs        int64   = 20_000
	stuckTeleportMs    int64   = 60_000
	followStuckMs      int64   = 10_000
)

// RunKind says why an agent is on the road.
type RunKind string

const (
	RunNone     RunKind = ""
	RunSolo     RunKind = "solo"
	RunEscorted RunKind = "escorted"
	RunGrand    RunKind = "grand"
	RunHired    RunKind = "hired"
)

// HireEnd says why a hire ended.
type HireEnd string

const (
	HireCompleted HireEnd = "completed"
	HireTimedOut  HireEnd = "timed out"
	HireAborted   HireEnd = "ended"
)

// HireOffer is one agent a player may hire right now.
type HireOffer struct {
	Name  string
	Role  AgentRole
	Town  TownID
	Fee   int64
	Goods int64
}

// outboxItem is a world callback queued under the lock and fired after it.
type outboxItem struct {
	player, agent, text string
	gold                int64
	end                 HireEnd
	chat                bool
	contract            bool
	regionID            uint16
}

type grandCaravan struct {
	nextAtMs   int64
	departAtMs int64
	town       TownID
}

/*
================
Title
================
*/
func (role AgentRole) Title() string {
	switch role {
	case RoleTrader:
		return "Trader"
	case RoleHunter:
		return "Hunter"
	default:
		return "Thief"
	}
}

/*
================
CanHire

Hunters and Traders hire Hunters and Traders; Thieves hire Thieves.
================
*/
func CanHire(playerJob uint8, role AgentRole) bool {
	switch playerJob {
	case domain.JobHunter, domain.JobTrader:
		return role == RoleHunter || role == RoleTrader
	case domain.JobThief:
		return role == RoleThief
	default:
		return false
	}
}

func hireable(u *AgentUnit) bool {
	return u.HiredBy == "" && u.RunKind == RunNone && u.DedicatedEscortFor == "" && u.EscortingTrader == "" &&
		(u.State == StateIdleInTown || u.State == StateRecruiting) && !u.RespawnPending && u.HP > 0
}

// HiredGoodsPerStarGold is what a hired Trader's goods cost per star;
// MaxHireStars is the highest rating a hirer may choose.
const (
	HiredGoodsPerStarGold int64 = 100_000
	MaxHireStars          uint8 = 5
)

func GoodsForStars(stars uint8) int64 {
	return int64(stars) * HiredGoodsPerStarGold
}

func hiredGoodsCost(u *AgentUnit) int64 {
	return GoodsForStars(u.CargoStars)
}

// HireRow is one agent in the hire window: who, and whether the viewer may
// hire it here now (or why not).
type HireRow struct {
	Name      string
	Role      AgentRole
	Town      TownID
	Stars     uint8
	Available bool
	Status    string
}

/*
================
HireRoster

Every agent of the kind hired at place (Thieves in the Den, Hunters and
Traders at a city shop), available ones first.
================
*/
func (r *Runtime) HireRoster(division, player string, playerJob uint8, place TownID) []HireRow {
	division = strings.TrimSpace(division)
	r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	full := len(r.hiresOfLocked(division, player)) >= MaxHiresPerPlayer
	var out []HireRow
	for _, u := range r.agents[division] {
		if (u.Role == RoleThief) != (place == TownThiefDen) {
			continue
		}
		row := HireRow{Name: u.Name, Role: u.Role, Town: r.homeLocked(u), Stars: u.CargoStars}
		switch {
		case u.HiredBy != "" && strings.EqualFold(u.HiredBy, player):
			row.Status = "Working for you"
		case u.HiredBy != "":
			row.Status = "Hired by " + u.HiredBy
		case u.RespawnPending || u.HP <= 0:
			row.Status = "Recovering"
		case u.RunKind == RunVengeance:
			row.Status = "Out hunting a caravan"
		case u.Role == RoleThief && !hireable(u):
			row.Status = "Out on the roads"
		case !hireable(u):
			row.Status = "On a trade run"
		case u.Role == RoleTrader && row.Town != place:
			row.Status = "Waiting in " + townTitle(row.Town)
		case !CanHire(playerJob, u.Role):
			row.Status = "Won't work for you"
		case full:
			row.Status = "You lead too many already"
		default:
			row.Available, row.Status = true, "Available"
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Available != out[j].Available {
			return out[i].Available
		}
		if out[i].Role != out[j].Role {
			return out[i].Role > out[j].Role
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// PlaceAt says which hiring place (a city or the Thief Den) a point is in.
func (r *Runtime) PlaceAt(at Waypoint) (TownID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.placeAtLocked(at)
}

func position(u *AgentUnit) Waypoint {
	return Waypoint{RegionID: u.RegionID, X: u.X, Y: u.Y, Z: u.Z}
}

func offsetPoint(base Waypoint, dx, dz float64) Waypoint {
	p := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: base.RegionID, X: float64(base.X) + dx, Z: float64(base.Z) + dz})
	return Waypoint{RegionID: p.RegionID, X: float32(p.X), Y: base.Y, Z: float32(p.Z)}
}

func (r *Runtime) terminalLocked(town TownID) Waypoint {
	if t, ok := r.cfg.Terminals[town]; ok {
		return t
	}
	return TownWaypoint(town)
}

// denRadius is how near the Thief Den buyer counts as being in the Den.
const denRadius float64 = 80 * UnitsPerMeter

/*
================
homeLocked / placeAtLocked

Thieves live in the Thief Den; everyone else in the nearest city. placeAt
says which home, if any, a point is in (a city's safe zone or the Den).
================
*/
func (r *Runtime) homeLocked(u *AgentUnit) TownID {
	if u.Role == RoleThief {
		return TownThiefDen
	}
	return r.nearestTownLocked(position(u))
}

func (r *Runtime) placeAtLocked(at Waypoint) (TownID, bool) {
	if WaypointDistance(at, r.terminalLocked(TownThiefDen)) <= denRadius {
		return TownThiefDen, true
	}
	if isTownRegion(at.RegionID) {
		return r.nearestTownLocked(at), true
	}
	return "", false
}

func GoldText(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func (r *Runtime) nearestTownLocked(at Waypoint) TownID {
	best, bestDist := TownJangan, math.MaxFloat64
	for _, town := range rosterTowns {
		if d := WaypointDistance(at, r.terminalLocked(town)); d < bestDist {
			best, bestDist = town, d
		}
	}
	return best
}

/*
================
noteProgress

How long the unit has stood within 5 units of the same spot while trying to
move; zero right after real progress.
================
*/
func (u *AgentUnit) noteProgress(nowMs int64) int64 {
	here := position(u)
	if u.progressAtMs == 0 || WaypointDistance(here, u.progressFrom) >= 5 {
		u.progressFrom, u.progressAtMs = here, nowMs
		return 0
	}
	return nowMs - u.progressAtMs
}

func (u *AgentUnit) place(at Waypoint, nowMs int64) {
	u.RegionID, u.X, u.Y, u.Z = at.RegionID, at.X, at.Y, at.Z
	u.lastMoveAtMs, u.progressAtMs = nowMs, 0
}

/*
================
leaderPositionLocked

Where the unit's leader (an agent or a player) stands.
================
*/
func (r *Runtime) leaderPositionLocked(unit *AgentUnit) (Waypoint, *AgentUnit, bool) {
	if leader := r.findAgentLocked(unit.Division, unit.EscortingTrader); leader != nil {
		return position(leader), leader, true
	}
	if r.hooks.Player != nil {
		if p, ok := r.hooks.Player(unit.Division, unit.EscortingTrader); ok {
			return p.At, nil, true
		}
	}
	return Waypoint{}, nil, false
}

/*
================
stepFollowLocked

Roam within 50m of the leader, run after it past 120m, teleport past 200m
or when a catch-up stalls.
================
*/
func (r *Runtime) stepFollowLocked(unit *AgentUnit, nowMs int64) *Waypoint {
	leader, leaderUnit, ok := r.leaderPositionLocked(unit)
	if !ok {
		if unit.HiredBy == "" {
			r.releaseLocked(unit, nowMs, true)
		}
		return nil
	}
	if unit.Role == RoleTrader {
		return r.stepTraderFollowLocked(unit, leader, leaderUnit, nowMs)
	}
	dist := WaypointDistance(position(unit), leader)
	if dist > EscortTeleportDistance || (unit.catchingUp && unit.noteProgress(nowMs) >= followStuckMs) {
		angle := rand.Float64() * 2 * math.Pi
		unit.place(offsetPoint(leader, 30*math.Cos(angle), 30*math.Sin(angle)), nowMs)
		unit.catchingUp, unit.roamAtMs = false, 0
		return nil
	}
	if dist > EscortFollowDistance {
		unit.catchingUp = true
	}
	if unit.catchingUp && dist <= EscortRoamRadius*0.6 {
		unit.catchingUp = false
	}
	if unit.catchingUp {
		var follow *Waypoint
		if leaderUnit != nil {
			follow = r.stepEscortFollowLocked(unit, leaderUnit)
		} else {
			if dist > 320 {
				follow = &leader
			}
			r.moveLocked(unit, leader, stepDistanceFor(unit))
		}
		unit.noteProgress(nowMs)
		return follow
	}
	unit.progressAtMs = 0
	if nowMs >= unit.roamAtMs {
		angle := rand.Float64() * 2 * math.Pi
		radius := 150 + rand.Float64()*(EscortRoamRadius-150)
		unit.roamDX, unit.roamDZ = radius*math.Cos(angle), radius*math.Sin(angle)
		unit.roamAtMs = nowMs + 3000 + rand.Int64N(4000)
	}
	target := offsetPoint(leader, unit.roamDX, unit.roamDZ)
	if WaypointDistance(position(unit), target) > 8 {
		r.moveLocked(unit, target, stepDistanceFor(unit))
	}
	return nil
}

// traderSlotRadius is how far a following Trader keeps from its leader.
const traderSlotRadius float64 = 5 * UnitsPerMeter

/*
================
stepTraderFollowLocked

A Trader keeps its own slot 5m off its leader: no strolling and never a
teleport; when it falls behind it runs until it is back in the slot.
================
*/
func (r *Runtime) stepTraderFollowLocked(unit *AgentUnit, leader Waypoint, leaderUnit *AgentUnit, nowMs int64) *Waypoint {
	// The slot angle comes from the name, so two Traders never share one.
	var h uint32
	for _, c := range unit.Name {
		h = h*31 + uint32(c)
	}
	angle := float64(h%360) * math.Pi / 180
	slot := offsetPoint(leader, traderSlotRadius*math.Cos(angle), traderSlotRadius*math.Sin(angle))
	unit.roamAtMs, unit.Walking = 0, false
	dist := WaypointDistance(position(unit), slot)
	if dist <= 15 {
		unit.catchingUp, unit.progressAtMs = false, 0
		return nil
	}
	unit.catchingUp = true
	if leaderUnit != nil && WaypointDistance(position(unit), leader) > 320 {
		// Far behind another agent: its route (and ferries) brings it back.
		follow := r.stepEscortFollowLocked(unit, leaderUnit)
		unit.noteProgress(nowMs)
		return follow
	}
	var follow *Waypoint
	if WaypointDistance(position(unit), leader) > 320 {
		follow = &leader
	}
	r.moveLocked(unit, slot, stepDistanceFor(unit))
	unit.noteProgress(nowMs)
	return follow
}

/*
================
wanderInTownLocked

Idle agents stroll around their town's specialty shop; one left outside
town walks back to it.
================
*/
func (r *Runtime) wanderInTownLocked(unit *AgentUnit, nowMs int64) {
	home := r.terminalLocked(unit.OriginTown)
	if _, inTown := r.placeAtLocked(position(unit)); !inTown {
		r.moveLocked(unit, home, stepDistanceFor(unit))
		if unit.noteProgress(nowMs) >= stuckTeleportMs {
			unit.place(home, nowMs)
		}
		return
	}
	if nowMs >= unit.roamAtMs {
		angle := rand.Float64() * 2 * math.Pi
		// A ring around the shop: near enough to find, never on its doorstep.
		radius := townWanderInner + rand.Float64()*(townWanderRadius-townWanderInner)
		unit.roamDX, unit.roamDZ = radius*math.Cos(angle), radius*math.Sin(angle)
		unit.roamAtMs = nowMs + 8000 + rand.Int64N(12_000)
		unit.progressAtMs = 0
	}
	target := offsetPoint(home, unit.roamDX, unit.roamDZ)
	if WaypointDistance(position(unit), target) <= 6 {
		unit.progressAtMs = 0
		return
	}
	// A stroll walks: the client plays the walk cycle at walk speed.
	unit.Walking = true
	step := WalkStepPerTick
	if unit.walkFactor > 0 {
		step *= unit.walkFactor
	}
	r.moveLocked(unit, target, step)
	if unit.noteProgress(nowMs) >= 6000 {
		// A building is in the way: choose another spot.
		unit.roamAtMs = 0
	}
}

/*
================
releaseLocked

Ends an agent's run or escort duty and makes it hireable again in the
nearest town (teleported there when asked and outside town).
================
*/
func (r *Runtime) releaseLocked(u *AgentUnit, nowMs int64, teleportHome bool) {
	u.DedicatedEscortFor, u.EscortingTrader, u.ReinforcingTrader = "", "", ""
	u.HiredBy, u.hireLeftTown, u.leftTownAtMs, u.leaderMissingAtMs, u.GoodsCostGold = "", false, 0, 0, 0
	u.RunKind, u.State = RunNone, StateIdleInTown
	u.UnderAttack, u.catchingUp, u.roamAtMs, u.progressAtMs = false, false, 0, 0
	town := r.homeLocked(u)
	u.OriginTown, u.DestTown = town, nextTown(town)
	u.Route, u.WaypointIndex = nil, 0
	if at, inTown := r.placeAtLocked(position(u)); teleportHome && (!inTown || at != town) {
		tp := offsetPoint(r.terminalLocked(town), 20, 20)
		u.pendingTeleport = &tp
	}
	if u.Role == RoleTrader {
		u.CargoStacks = 10
		u.TownDepartAtMs = nowMs + TownRestDurationMs
	}
	u.AvailableForHire = true
}

/*
================
finishRunLocked

An own (not hired) Trader delivered: its escorts, or the whole grand
caravan, are released in the arrival town.
================
*/
func (r *Runtime) finishRunLocked(trader *AgentUnit, nowMs int64) {
	kind := trader.RunKind
	for _, u := range r.agents[trader.Division] {
		if u == trader || u.HiredBy != "" {
			continue
		}
		if (kind == RunGrand && u.RunKind == RunGrand) || strings.EqualFold(u.DedicatedEscortFor, trader.Name) || strings.EqualFold(u.EscortingTrader, trader.Name) {
			r.releaseLocked(u, nowMs, false)
		}
	}
	trader.RunKind = RunNone
	trader.DedicatedEscortFor, trader.EscortingTrader = "", ""
}

/*
================
disbandRunLocked

A robbed Trader's escorts (or its grand caravan) give up the run and go
back to the nearest town.
================
*/
func (r *Runtime) disbandRunLocked(trader *AgentUnit, nowMs int64) {
	if trader.RunKind == RunGrand && trader.EscortingTrader != "" {
		// A follower Trader of the grand caravan: only its own guards leave.
		for _, u := range r.agents[trader.Division] {
			if u != trader && u.HiredBy == "" && strings.EqualFold(u.EscortingTrader, trader.Name) {
				r.releaseLocked(u, nowMs, true)
			}
		}
	} else {
		for _, u := range r.agents[trader.Division] {
			if u == trader || u.HiredBy != "" {
				continue
			}
			if (trader.RunKind == RunGrand && u.RunKind == RunGrand) || strings.EqualFold(u.DedicatedEscortFor, trader.Name) || strings.EqualFold(u.EscortingTrader, trader.Name) {
				r.releaseLocked(u, nowMs, true)
			}
		}
	}
	trader.RunKind = RunNone
	trader.DedicatedEscortFor, trader.EscortingTrader = "", ""
}

/*
================
eligibleTraderLocked

The rested, unhired, not-robbed town Trader that has waited longest.
================
*/
func eligibleTrader(u *AgentUnit, nowMs int64) bool {
	return u.Role == RoleTrader && hireable(u) && nowMs >= u.TownDepartAtMs && nowMs >= u.CooldownUntilMs
}

func (r *Runtime) eligibleTraderLocked(division string, nowMs int64) *AgentUnit {
	var best *AgentUnit
	for _, u := range r.agents[division] {
		if eligibleTrader(u, nowMs) && (best == nil || u.TownDepartAtMs < best.TownDepartAtMs) {
			best = u
		}
	}
	return best
}

/*
================
pickEscortsLocked

Idle Hunters for a Trader's run, the ones in its town first.
================
*/
func (r *Runtime) pickEscortsLocked(trader *AgentUnit, n int) []*AgentUnit {
	var pool []*AgentUnit
	for _, u := range r.agents[trader.Division] {
		if u.Role == RoleHunter && hireable(u) {
			pool = append(pool, u)
		}
	}
	at := position(trader)
	sort.SliceStable(pool, func(i, j int) bool {
		return WaypointDistance(position(pool[i]), at) < WaypointDistance(position(pool[j]), at)
	})
	return pool[:min(n, len(pool))]
}

/*
================
assignEscortLocked
================
*/
func (r *Runtime) assignEscortLocked(guard, leader *AgentUnit, kind RunKind) {
	guard.HP, guard.MP = guard.MaxHP, guard.MaxMP
	guard.PotionCharges = defaultPotionCharges(guard.Role)
	guard.transportHPPercent = FullHPPercent
	guard.UnderAttack, guard.AvailableForHire = false, false
	guard.EscortingTrader, guard.DedicatedEscortFor, guard.ReinforcingTrader = leader.Name, leader.Name, ""
	guard.RunKind, guard.State, guard.TownDepartAtMs = kind, StateTraveling, 0
	guard.OriginTown, guard.DestTown = leader.OriginTown, leader.DestTown
	guard.Route, guard.WaypointIndex = nil, 0
	guard.catchingUp, guard.roamAtMs = false, 0
	if WaypointDistance(position(guard), position(leader)) > 300 {
		tp := offsetPoint(position(leader), 12, 12)
		guard.pendingTeleport = &tp
	}
}

/*
================
scheduleRunsLocked

Keeps StandingSoloRuns solo Traders and StandingEscortedRuns Traders with
EscortsPerRun Hunters on the road, and runs the grand caravan clock.
Caller holds r.mu.
================
*/
func (r *Runtime) scheduleRunsLocked(division string, nowMs int64) {
	if r.manualRuns || (nowMs != 0 && r.scheduledAtMs[division] == nowMs) {
		return
	}
	r.scheduledAtMs[division] = nowMs
	r.grandCaravanLocked(division, nowMs)
	solo, escorted := 0, 0
	for _, u := range r.agents[division] {
		if u.Role != RoleTrader || u.HiredBy != "" || !isActiveTradeRunState(u.State) {
			continue
		}
		switch u.RunKind {
		case RunSolo:
			solo++
		case RunEscorted:
			escorted++
		}
	}
	for ; solo < StandingSoloRuns; solo++ {
		trader := r.eligibleTraderLocked(division, nowMs)
		if trader == nil {
			break
		}
		r.dispatchTraderLocked(trader, RunSolo, nil, nowMs)
	}
	// Killed highway Thieves are replaced by rested town Thieves.
	patrols := 0
	var spare []*AgentUnit
	for _, u := range r.agents[division] {
		if u.Role != RoleThief || u.HiredBy != "" {
			continue
		}
		if hireable(u) {
			if nowMs >= u.TownDepartAtMs {
				spare = append(spare, u)
			}
		} else if u.State != StateIdleInTown {
			patrols++
		}
	}
	for ; patrols < rosterHighwayThieves && len(spare) > 0; patrols++ {
		// A replacement leaves the Den for an ambush spot on a city road.
		thief := spare[0]
		spare = spare[1:]
		from := rosterTowns[rand.IntN(2)]
		thief.OriginTown, thief.DestTown = from, nextTown(from)
		thief.State, thief.Route, thief.WaypointIndex = StateTraveling, r.route(thief), 0
		if len(thief.Route) > 2 {
			thief.WaypointIndex = 2
			tp := thief.Route[2]
			thief.pendingTeleport = &tp
		}
	}
	for ; escorted < StandingEscortedRuns; escorted++ {
		trader := r.eligibleTraderLocked(division, nowMs)
		if trader == nil {
			break
		}
		guards := r.pickEscortsLocked(trader, EscortsPerRun)
		if len(guards) == 0 {
			break
		}
		r.dispatchTraderLocked(trader, RunEscorted, guards, nowMs)
	}
}

/*
================
grandCaravanLocked

Every GrandCaravanIntervalMs all idle Traders and Hunters muster at the
specialty shop of the town most of them are in, and leave together after
GrandCaravanMusterMs: the first Trader leads, the other Traders follow it
and the Hunters are shared out between the Traders.
================
*/
func (r *Runtime) grandCaravanLocked(division string, nowMs int64) {
	g := r.grand[division]
	if g == nil {
		g = &grandCaravan{nextAtMs: nowMs + GrandCaravanFirstMs}
		r.grand[division] = g
	}
	if g.departAtMs != 0 {
		if nowMs >= g.departAtMs {
			g.departAtMs = 0
			r.departGrandLocked(division, g.town, nowMs)
		}
		return
	}
	if nowMs < g.nextAtMs {
		return
	}
	g.nextAtMs = nowMs + GrandCaravanIntervalMs
	r.musterGrandLocked(division, g, nowMs)
}

/*
================
StartGrandCaravan

Starts the grand caravan muster now (the next one follows the interval).
================
*/
func (r *Runtime) StartGrandCaravan(division string, nowMs int64) bool {
	r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.grand[division]
	if g == nil {
		g = &grandCaravan{}
		r.grand[division] = g
	}
	if g.departAtMs != 0 {
		return false
	}
	g.nextAtMs = nowMs + GrandCaravanIntervalMs
	return r.musterGrandLocked(division, g, nowMs)
}

func (r *Runtime) musterGrandLocked(division string, g *grandCaravan, nowMs int64) bool {
	var members []*AgentUnit
	towns := map[TownID]int{}
	traders := 0
	for _, u := range r.agents[division] {
		if (u.Role == RoleTrader && hireable(u) && nowMs >= u.CooldownUntilMs) || (u.Role == RoleHunter && hireable(u)) {
			members = append(members, u)
			towns[u.OriginTown]++
			if u.Role == RoleTrader {
				traders++
			}
		}
	}
	if traders == 0 {
		return false
	}
	g.town = TownJangan
	for _, town := range rosterTowns {
		if towns[town] > towns[g.town] {
			g.town = town
		}
	}
	g.departAtMs = nowMs + GrandCaravanMusterMs
	shop := r.terminalLocked(g.town)
	for i, u := range members {
		u.RunKind, u.State, u.AvailableForHire = RunGrand, StateRecruiting, false
		u.OriginTown, u.DestTown = g.town, nextTown(g.town)
		u.TownDepartAtMs = 0
		if WaypointDistance(position(u), shop) > townWanderRadius*2 {
			angle := float64(i) * 2 * math.Pi / float64(len(members))
			tp := offsetPoint(shop, 60*math.Cos(angle), 60*math.Sin(angle))
			u.pendingTeleport = &tp
		}
	}
	r.outbox = append(r.outbox, outboxItem{chat: true, agent: members[0].Name, regionID: shop.RegionID,
		text: fmt.Sprintf("Grand caravan! %d Traders and %d Hunters muster in %s and leave for %s in %d minutes.",
			traders, len(members)-traders, townTitle(g.town), townTitle(nextTown(g.town)), GrandCaravanMusterMs/60_000)})
	return true
}

func (r *Runtime) departGrandLocked(division string, town TownID, nowMs int64) {
	var traders, guards []*AgentUnit
	for _, u := range r.agents[division] {
		if u.RunKind != RunGrand || u.HiredBy != "" || u.State != StateRecruiting {
			continue
		}
		if u.Role == RoleTrader {
			traders = append(traders, u)
		} else {
			guards = append(guards, u)
		}
	}
	if len(traders) == 0 {
		for _, u := range guards {
			r.releaseLocked(u, nowMs, false)
		}
		return
	}
	lead := traders[0]
	lead.OriginTown, lead.DestTown = town, nextTown(town)
	r.dispatchTraderLocked(lead, RunGrand, nil, nowMs)
	for _, u := range traders[1:] {
		r.assignEscortLocked(u, lead, RunGrand)
		u.CargoStacks = 10
	}
	for i, u := range guards {
		r.assignEscortLocked(u, traders[i%len(traders)], RunGrand)
	}
}

func townTitle(town TownID) string {
	if town == "" {
		return ""
	}
	return strings.ToUpper(string(town[:1])) + string(town[1:])
}

/*
================
scenicRoute

Solo Traders leave the beaten track: each long open leg bends through a
point 60-150m off the road, so they are harder to find.
================
*/
func scenicRoute(points []Waypoint) []Waypoint {
	if len(points) < 2 {
		return points
	}
	out := []Waypoint{points[0]}
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		if b.FerryTargetID == 0 && a.Town == "" && b.Town == "" {
			dx, dz := worldgeom.Delta(worldgeom.RegionXZ{RegionID: a.RegionID, X: float64(a.X), Z: float64(a.Z)}, worldgeom.RegionXZ{RegionID: b.RegionID, X: float64(b.X), Z: float64(b.Z)})
			if leg := math.Hypot(dx, dz); leg >= scenicDetourMinLeg {
				side := 600 + rand.Float64()*900
				if rand.IntN(2) == 0 {
					side = -side
				}
				bend := offsetPoint(a, dx/2-dz/leg*side, dz/2+dx/leg*side)
				bend.Y, bend.Name, bend.Scenic = (a.Y+b.Y)/2, "back road", true
				out = append(out, bend)
			}
		}
		out = append(out, b)
	}
	return out
}

/*
================
HireOffers

The agents playerJob may hire now, Hunters and Traders before Thieves.
================
*/
func (r *Runtime) HireOffers(division string, playerJob uint8) []HireOffer {
	r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []HireOffer
	for _, u := range r.agents[strings.TrimSpace(division)] {
		if !CanHire(playerJob, u.Role) || !hireable(u) {
			continue
		}
		offer := HireOffer{Name: u.Name, Role: u.Role, Town: u.OriginTown, Fee: HireFeeGold}
		if u.Role == RoleTrader {
			offer.Goods = hiredGoodsCost(u)
		}
		out = append(out, offer)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role > out[j].Role
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *Runtime) hiresOfLocked(division, player string) []*AgentUnit {
	var out []*AgentUnit
	for _, u := range r.agents[division] {
		if u.HiredBy != "" && strings.EqualFold(u.HiredBy, player) {
			out = append(out, u)
		}
	}
	return out
}

/*
================
HireAgent

Charges the flat fee (plus a Trader's goods), then the agent works for one
trade run, an hour at most. Hunters and Thieves appear at the hirer; a
Trader is hired in the town it waits in and walks over, loaded with goods
of the chosen star rating (stars 0: its own rating).
================
*/
func (r *Runtime) HireAgent(division, player string, playerJob uint8, agentName string, nowMs int64) string {
	return r.HireAgentStars(division, player, playerJob, agentName, 0, nowMs)
}

func (r *Runtime) HireAgentStars(division, player string, playerJob uint8, agentName string, stars uint8, nowMs int64) string {
	division = strings.TrimSpace(division)
	r.EnsureDivisionRoster(division)
	if r.hooks.Player == nil {
		return "Caravan hiring is unavailable."
	}
	at, ok := r.hooks.Player(division, player)
	if !ok || at.HPPercent <= 0 {
		return "Caravan help is hired in town."
	}
	r.mu.Lock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		r.mu.Unlock()
		return fmt.Sprintf("Nobody called %s is looking for work.", agentName)
	}
	name, short := unit.Name, DisplayName(unit.Name)
	if !CanHire(playerJob, unit.Role) {
		r.mu.Unlock()
		switch {
		case playerJob == domain.JobThief:
			return fmt.Sprintf("%s spits: \"I don't work for thieves.\"", short)
		case unit.Role == RoleThief:
			return fmt.Sprintf("%s laughs: \"Hunters and merchants don't hire my kind.\"", short)
		default:
			return "Put on your job suit before you hire caravan help."
		}
	}
	place, inTown := r.placeAtLocked(at.At)
	if !inTown || (unit.Role == RoleThief) != (place == TownThiefDen) {
		r.mu.Unlock()
		if unit.Role == RoleThief {
			return "Thieves are hired in the Thief Den."
		}
		return "Hunters and Traders are hired at a town's specialty shop."
	}
	if !hireable(unit) {
		r.mu.Unlock()
		return fmt.Sprintf("%s is already on the road and cannot be hired now.", short)
	}
	if len(r.hiresOfLocked(division, player)) >= MaxHiresPerPlayer {
		r.mu.Unlock()
		return fmt.Sprintf("You already lead %d hired companions.", MaxHiresPerPlayer)
	}
	goods := int64(0)
	if unit.Role == RoleTrader {
		if town := r.nearestTownLocked(position(unit)); town != place {
			r.mu.Unlock()
			return fmt.Sprintf("%s waits in %s; Traders are hired in the town they wait in.", short, townTitle(town))
		}
		if stars == 0 {
			stars = unit.CargoStars
		}
		stars = min(max(stars, 1), MaxHireStars)
		goods = GoodsForStars(stars)
	}
	r.mu.Unlock()

	price := HireFeeGold + goods
	if r.hooks.OnHireFee == nil || !r.hooks.OnHireFee(division, player, price) {
		if goods > 0 {
			return fmt.Sprintf("Hiring %s costs %s gold: %s for the hire and %s for the goods.", short, GoldText(price), GoldText(HireFeeGold), GoldText(goods))
		}
		return fmt.Sprintf("Hiring %s costs %s gold.", short, GoldText(price))
	}

	r.mu.Lock()
	unit = r.findAgentLocked(division, agentName)
	if unit == nil || !hireable(unit) || len(r.hiresOfLocked(division, player)) >= MaxHiresPerPlayer {
		r.mu.Unlock()
		if r.hooks.OnHirePayout != nil {
			r.hooks.OnHirePayout(division, player, price)
		}
		return fmt.Sprintf("%s was just hired by someone else; your gold is returned.", short)
	}
	bindHire(unit, player, place, goods, nowMs)
	if unit.Role == RoleTrader {
		unit.CargoStars, unit.CargoStacks, unit.StackValueGold = stars, 10, goods/10
	}
	if unit.Role != RoleTrader {
		// Traders walk over from the shop; the others appear at the hirer.
		tp := offsetPoint(at.At, 12, 0)
		unit.pendingTeleport = &tp
	}
	r.outbox = append(r.outbox, outboxItem{chat: true, agent: name, regionID: at.At.RegionID, text: hireGreeting(unit, player)})
	r.mu.Unlock()
	defer r.flushOutbox(division)

	if r.hooks.OnContractFormed != nil {
		r.hooks.OnContractFormed(division, player, name)
	}
	if goods > 0 {
		return fmt.Sprintf("%s joins you for one trade run. Paid %s gold (%s for goods).", short, GoldText(price), GoldText(goods))
	}
	return fmt.Sprintf("%s joins you for one trade run. Paid %s gold.", short, GoldText(price))
}

// bindHire puts unit on player's hire, made in town at hiredAtMs.
func bindHire(unit *AgentUnit, player string, town TownID, goods, hiredAtMs int64) {
	unit.HiredBy, unit.HiredAtMs, unit.HireTown = player, hiredAtMs, town
	unit.hireLeftTown, unit.leftTownAtMs, unit.leaderMissingAtMs = false, 0, 0
	unit.RunKind, unit.State, unit.AvailableForHire = RunHired, StateTraveling, false
	unit.EscortingTrader, unit.GoodsCostGold, unit.TownDepartAtMs = player, goods, 0
	unit.OriginTown, unit.DestTown = town, nextTown(town)
	unit.Route, unit.WaypointIndex = nil, 0
	unit.catchingUp, unit.roamAtMs = false, 0
}

/*
================
hireGreeting / hireFarewell

What a hired agent says in local chat.
================
*/
func hireGreeting(u *AgentUnit, player string) string {
	switch u.Role {
	case RoleTrader:
		return fmt.Sprintf("A pleasure, %s. My %d-star wagon is loaded; let's make this run count.", player, u.CargoStars)
	case RoleThief:
		return fmt.Sprintf("Your coin, your call, %s. Point me at a fat caravan.", player)
	default:
		return fmt.Sprintf("Lead the way, %s. Nobody touches your cargo while I'm around.", player)
	}
}

func hireFarewell(u *AgentUnit, player string, end HireEnd) string {
	switch end {
	case HireCompleted:
		return fmt.Sprintf("A good run, %s. Find me in town if you ride again.", player)
	case HireTimedOut:
		return fmt.Sprintf("That's my hour, %s. I'm heading home.", player)
	default:
		return fmt.Sprintf("Our deal is off, %s. I'm heading home.", player)
	}
}

/*
================
ToggleLead

!leed: the hirer leads its hired Traders instead of the first one in the
party.
================
*/
func (r *Runtime) toggleLeadLocked(division, player string) string {
	if len(r.hiresOfLocked(division, player)) == 0 {
		return "You have no hired caravan agents to lead."
	}
	key := agentKey(division, player)
	r.leads[key] = !r.leads[key]
	if r.leads[key] {
		return "You lead the caravan: your hired Traders follow you."
	}
	return "The first hired Trader in your party leads the caravan again."
}

/*
================
SetPartyOrder / Hirers

The party's member order, set by the world each tick outside the lock (the
party runtime calls back into this one).
================
*/
func (r *Runtime) SetPartyOrder(division, player string, members []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partyOrder[agentKey(division, player)] = members
}

func (r *Runtime) Hirers(division string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, u := range r.agents[strings.TrimSpace(division)] {
		if u.HiredBy != "" && !seen[strings.ToLower(u.HiredBy)] {
			seen[strings.ToLower(u.HiredBy)] = true
			out = append(out, u.HiredBy)
		}
	}
	return out
}

/*
================
refreshHireLeaderLocked

A hired Hunter or Thief follows its hirer. Hired Traders follow the first
of them in party order (hire order when not in the party) unless the hirer
typed !leed; that first Trader drives the run itself.
================
*/
func (r *Runtime) refreshHireLeaderLocked(unit *AgentUnit) {
	if unit.HiredBy == "" {
		return
	}
	if unit.Role != RoleTrader || r.leads[agentKey(unit.Division, unit.HiredBy)] {
		unit.EscortingTrader = unit.HiredBy
		return
	}
	var traders []*AgentUnit
	for _, u := range r.hiresOfLocked(unit.Division, unit.HiredBy) {
		if u.Role == RoleTrader {
			traders = append(traders, u)
		}
	}
	order := r.partyOrder[agentKey(unit.Division, unit.HiredBy)]
	rank := func(u *AgentUnit) int {
		for i, name := range order {
			if strings.EqualFold(name, u.Name) {
				return i
			}
		}
		return len(order)
	}
	sort.SliceStable(traders, func(i, j int) bool {
		if ri, rj := rank(traders[i]), rank(traders[j]); ri != rj {
			return ri < rj
		}
		return traders[i].HiredAtMs < traders[j].HiredAtMs
	})
	if traders[0] == unit {
		unit.EscortingTrader = ""
		return
	}
	unit.EscortingTrader = traders[0].Name
}

/*
================
superviseHireLocked

Ends a hire at the end of the run (back in a town), after an hour, when the
hirer drops the job or stays away; reports true when the unit must not move
this step.
================
*/
func (r *Runtime) superviseHireLocked(unit *AgentUnit, nowMs int64) bool {
	var p PlayerPosition
	ok := false
	if r.hooks.Player != nil {
		p, ok = r.hooks.Player(unit.Division, unit.HiredBy)
	}
	if !ok {
		if unit.leaderMissingAtMs == 0 {
			unit.leaderMissingAtMs = nowMs
		} else if nowMs-unit.leaderMissingAtMs >= HireLeaderMissingMs {
			r.endHireLocked(r.hiresOfLocked(unit.Division, unit.HiredBy), HireAborted, nowMs)
		}
		return true
	}
	unit.leaderMissingAtMs = 0
	if unit.rejoinParty {
		// A hire restored at boot: back into the hirer's party.
		unit.rejoinParty = false
		r.outbox = append(r.outbox, outboxItem{contract: true, player: unit.HiredBy, agent: unit.Name})
	}
	if !CanHire(p.JobType, unit.Role) {
		r.endHireLocked([]*AgentUnit{unit}, HireAborted, nowMs)
		return true
	}
	if nowMs-unit.HiredAtMs >= HireMaxDurationMs {
		r.endHireLocked([]*AgentUnit{unit}, HireTimedOut, nowMs)
		return true
	}
	place, inTown := r.placeAtLocked(p.At)
	if !inTown {
		if !unit.hireLeftTown {
			unit.hireLeftTown, unit.leftTownAtMs = true, nowMs
		}
		return false
	}
	if unit.hireLeftTown && (place != unit.HireTown || nowMs-unit.leftTownAtMs >= HireRunMinOutMs) {
		r.endHireLocked(r.hiresOfLocked(unit.Division, unit.HiredBy), HireCompleted, nowMs)
		return true
	}
	return false
}

/*
================
stepHiredLeadLocked

The leading hired Trader drives the run, waiting while its hirer is more
than 120m behind.
================
*/
func (r *Runtime) stepHiredLeadLocked(unit *AgentUnit, nowMs int64) {
	if r.hooks.Player == nil {
		return
	}
	p, ok := r.hooks.Player(unit.Division, unit.HiredBy)
	if !ok || WaypointDistance(position(unit), p.At) > EscortFollowDistance {
		unit.lastMoveAtMs, unit.progressAtMs = nowMs, 0
		return
	}
	if len(unit.Route) == 0 {
		unit.Route, unit.WaypointIndex = r.route(unit), 0
	}
	r.advanceAlongRouteLocked(unit, nowMs)
}

/*
================
endHireLocked

Releases hired units. A hired Trader still holding goods pays its hirer the
sale (cost plus the per-star margin) on a finished run, or the cost back
otherwise; robbed goods pay nothing.
================
*/
func (r *Runtime) endHireLocked(units []*AgentUnit, end HireEnd, nowMs int64) {
	for _, u := range units {
		if u.HiredBy == "" {
			continue
		}
		player := u.HiredBy
		gold := int64(0)
		if u.Role == RoleTrader && u.CargoStacks > 0 {
			gold = int64(u.CargoStacks) * u.StackValueGold
			if end == HireCompleted {
				gold = gold * int64(100+HiredGoodsMarginPerStarPct*int(u.CargoStars)) / 100
			}
		}
		r.outbox = append(r.outbox, outboxItem{chat: true, agent: u.Name, regionID: u.RegionID, text: hireFarewell(u, player, end)})
		r.releaseLocked(u, nowMs, true)
		r.outbox = append(r.outbox, outboxItem{player: player, agent: u.Name, gold: gold, end: end})
		if len(r.hiresOfLocked(u.Division, player)) == 0 {
			delete(r.leads, agentKey(u.Division, player))
			delete(r.partyOrder, agentKey(u.Division, player))
		}
	}
}

/*
================
DismissHires

!dismiss: the hirer ends every hire now (Traders return the goods' cost).
================
*/
func (r *Runtime) dismissLocked(division, player string, nowMs int64) string {
	units := r.hiresOfLocked(division, player)
	if len(units) == 0 {
		return "You have no hired caravan agents."
	}
	r.endHireLocked(units, HireAborted, nowMs)
	return fmt.Sprintf("Dismissed %d hired caravan agents.", len(units))
}

/*
================
flushOutbox

Fires queued payouts, hire ends and announcements after the lock.
================
*/
func (r *Runtime) flushOutbox(division string) {
	r.mu.Lock()
	items := r.outbox
	r.outbox = nil
	r.mu.Unlock()
	for _, item := range items {
		if item.chat {
			if r.hooks.OnAgentChat != nil {
				r.hooks.OnAgentChat(division, item.agent, item.regionID, item.text)
			}
			continue
		}
		if item.contract {
			if r.hooks.OnContractFormed != nil {
				r.hooks.OnContractFormed(division, item.player, item.agent)
			}
			continue
		}
		if item.gold > 0 && r.hooks.OnHirePayout != nil {
			r.hooks.OnHirePayout(division, item.player, item.gold)
		}
		if r.hooks.OnHireEnded != nil {
			r.hooks.OnHireEnded(division, item.player, item.agent, item.end, item.gold)
		}
	}
}
