/*
===========================================================================

convoy.go - escort roaming, hiring, standing runs and the grand caravan

Port-only. Distances are world units: 10 units are one in-game metre (skill
ranges read 180 units as 18m).

  - Escorts roam within 30m of their leader, catch up outside that radius,
    and teleport if more than 150m behind.
  - Any idle agent can be hired at a specialty shop for one trade run, at
    most an hour. Hunters and Traders hire Hunters and Traders; Thieves
    hire Thieves. Hired Traders carry goods the hirer pays for and pay the
    sale back on delivery.
  - The scheduler keeps up to two solo and two escorted runs on the road.
    An hourly grand caravan adds two five-star Traders and six Hunters.
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

	EscortRoamRadius        float64 = 30 * UnitsPerMeter
	EscortFollowDistance    float64 = 90 * UnitsPerMeter
	escortCombatDistance    float64 = 100 * UnitsPerMeter
	EscortTeleportDistance  float64 = 150 * UnitsPerMeter
	escortFerryWaitDistance         = 20 * UnitsPerMeter

	HireFeeGold int64 = BaseHunterHireFeeGold
	// HireLeaderMissingMs ends a hire whose hirer stays offline or out of
	// the main world this long.
	HireLeaderMissingMs int64 = 2 * 60_000
	// A Hunter or Thief hire lasts HireDurationMs, renewable at a shop
	// (ExtendHire, at most HireMaxAheadMs ahead), with a notice
	// HireWarnMs before it ends. Hired with no Trader, it also ends when
	// the hirer is back in a town after HireOutRunMs out.
	HireDurationMs int64 = 60 * 60_000
	HireMaxAheadMs int64 = 2 * HireDurationMs
	HireWarnMs     int64 = 5 * 60_000
	HireOutRunMs   int64 = 20 * 60_000
	// HirePoolReserve Hunters (in a town) or Thieves (in the Den) always
	// stay free for other players.
	HirePoolReserve = 2
	// HireRunMinOutMs: a hired Trader back in the town it was hired in sells
	// only after this long outside (a quick step out is not a run).
	HireRunMinOutMs int64 = 10 * 60_000
	// hireSellReach is how near the town's trade shop a hired Trader sells.
	hireSellReach float64 = 8 * UnitsPerMeter
	// HiredGoodsMarginPerStarPct is the sale margin a hired Trader earns its
	// hirer per cargo star on delivery.
	HiredGoodsMarginPerStarPct = 15
	MaxHiresPerPlayer          = 7

	RobbedCooldownMs int64 = 30 * 60_000

	StandingSoloRuns     = 2
	StandingEscortedRuns = 2
	EscortsPerRun        = 3

	GrandCaravanIntervalMs int64 = 60 * 60_000
	GrandCaravanMusterMs   int64 = 2 * 60_000
	GrandCaravanFirstMs          = GrandCaravanIntervalMs - GrandCaravanMusterMs
	grandCaravanRetryMs    int64 = 60_000
	grandCaravanTraders          = 2
	grandCaravanHunters          = 6
	grandCaravanCargoStars       = 5

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
	// saleTown prices a finished hire's goods at that town's shop
	// (WorldHooks.CargoSale) when the flush pays; gold is the fallback.
	saleTown TownID
	end      HireEnd
	chat     bool
	contract bool
	// notice is a message for player alone (WorldHooks.OnHireNotice).
	notice   bool
	regionID uint16
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
		(u.State == StateIdleInTown || u.State == StateRecruiting) && !u.RespawnPending && u.DeadUntilMs == 0 && u.HP > 0
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
	// Extend: the player's own Hunter or Thief, renewable here.
	Extend bool
}

/*
================
HireRoster

Every agent of the kind hired at place (Thieves in the Den, Hunters and
Traders at a city shop), available ones first.
================
*/
func (r *Runtime) HireRoster(division, player string, playerJob uint8, place TownID, nowMs int64) []HireRow {
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
			if u.Role != RoleTrader {
				left := max(u.HiredAtMs+HireDurationMs-nowMs, 0)
				row.Status = fmt.Sprintf("Working for you (%d min left)", (left+59_999)/60_000)
				row.Extend = true
			}
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
		case u.Role != RoleTrader && r.freeOfRoleLocked(division, u.Role, row.Town) <= HirePoolReserve:
			row.Status = "Kept free for others"
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
	den := r.terminalLocked(TownThiefDen)
	if (den.RegionID != 0 && at.RegionID == den.RegionID) || WaypointDistance(at, den) <= denRadius {
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
		if leader.RespawnPending || leader.HP <= 0 {
			return Waypoint{}, nil, false
		}
		return position(leader), leader, true
	}
	if r.hooks.Player != nil {
		if p, ok := r.hooks.Player(unit.Division, unit.EscortingTrader); ok && p.HPPercent > 0 {
			return p.At, nil, true
		}
	}
	return Waypoint{}, nil, false
}

/*
================
stepFollowLocked

Roam within 30m of the leader, run after it at 30m, teleport past 150m
or when a catch-up stalls. If an escorted AI Trader is dead on the ground
and a living Hunter in the convoy can resurrect them, Hunters stay to revive
and guard the Trader instead of disbanding to town.
================
*/
func (r *Runtime) stepFollowLocked(unit *AgentUnit, nowMs int64) *Waypoint {
	leader, leaderUnit, ok := r.leaderPositionLocked(unit)
	if !ok {
		if unit.Role == RoleHunter && unit.EscortingTrader != "" {
			if fallen := r.findAgentLocked(unit.Division, unit.EscortingTrader); fallen != nil && fallen.DeadUntilMs > 0 && nowMs < fallen.DeadUntilMs && r.hasLivingResurrectingEscortLocked(fallen, nowMs) {
				target := position(fallen)
				if unit.CanResurrect {
					if WaypointDistance(position(unit), target) > ResurrectCastRange*0.75 {
						r.moveLocked(unit, target, stepDistanceFor(unit))
					}
				} else if WaypointDistance(position(unit), target) > EscortRoamRadius {
					r.moveLocked(unit, target, stepDistanceFor(unit))
				}
				return nil
			}
		}
		if unit.HiredBy == "" {
			r.releaseLocked(unit, nowMs, true)
		}
		return nil
	}
	dist := WaypointDistance(position(unit), leader)
	if leaderUnit != nil {
		index, route := leaderUnit.WaypointIndex, leaderUnit.Route
		guide := r.ferryGuideLocked(leaderUnit)
		if guide.WaypointIndex < len(guide.Route) && guide.Route[guide.WaypointIndex].FerryTargetID != 0 {
			board := position(guide)
			if WaypointDistance(position(unit), board) > 120 {
				r.moveLocked(unit, board, stepDistanceFor(unit))
			}
			return nil
		}
		if dist > 320 {
			landing := leaderUnit.lastFerry
			if index > 0 && index <= len(route) && route[index-1].FerryTargetID != 0 {
				landing = route[index-1]
			}
			if landing.FerryTargetID != 0 && WaypointDistance(leader, landing) <= 320 &&
				WaypointDistance(position(unit), landing) > 320 {
				return &landing
			}
		}
	}
	if unit.Role == RoleTrader {
		return r.stepTraderFollowLocked(unit, leader, leaderUnit, nowMs)
	}
	// A Thief hauling goods is never moved to its leader: it is on its
	// way to the Den with them (thiefcargo.go).
	if !unit.carriesGoods && (dist > EscortTeleportDistance || (unit.catchingUp && unit.noteProgress(nowMs) >= followStuckMs)) {
		angle := escortFormationAngle(unit.Name)
		unit.place(offsetPoint(leader, 28*math.Cos(angle), 28*math.Sin(angle)), nowMs)
		unit.catchingUp, unit.escortMoving, unit.roamAtMs = false, false, 0
		return nil
	}
	// Whenever a follower falls outside the 30m escort radius, immediately catch up
	// toward the leader instead of wandering randomly behind.
	if dist >= EscortRoamRadius {
		unit.catchingUp = true
	}
	if unit.catchingUp && dist <= EscortRoamRadius*0.65 {
		unit.catchingUp = false
	}
	if unit.catchingUp {
		unit.escortMoving = true
		var follow *Waypoint
		if leaderUnit != nil && dist > EscortFollowDistance {
			r.stepEscortFollowLocked(unit, leaderUnit)
		} else {
			if leaderUnit == nil && dist > 320 {
				follow = &leader
			}
			r.moveLocked(unit, leader, stepDistanceFor(unit))
		}
		unit.noteProgress(nowMs)
		return follow
	}
	if nowMs >= unit.roamAtMs {
		angle := escortFormationAngle(unit.Name)
		swing, interval := 0.45, int64(15_000)
		if leaderUnit == nil && WaypointDistance(unit.lastLeaderAt, leader) < 20 {
			swing, interval = 1.5, 7_000
		}
		angle += (rand.Float64() - 0.5) * swing
		radius := 150 + rand.Float64()*90
		unit.roamDX, unit.roamDZ = radius*math.Cos(angle), radius*math.Sin(angle)
		unit.roamAtMs = nowMs + interval + rand.Int64N(interval/2)
		unit.lastLeaderAt = leader
	}
	target := offsetPoint(leader, unit.roamDX, unit.roamDZ)
	distToSlot := WaypointDistance(position(unit), target)
	// Each follower has its own slack, reaction and pace (followerTemper),
	// so a party does not set off together in one direction the moment its
	// leader moves: one lets the leader get further ahead, one reacts late,
	// and each one that sets off may pick a fresh spot around the leader.
	slack, reactMs, paceFactor := followerTemper(unit.Name)
	switch {
	case unit.escortMoving && distToSlot <= 8:
		unit.escortMoving, unit.moveDueAtMs = false, 0
		unit.progressAtMs = 0
	case !unit.escortMoving && distToSlot > slack:
		if unit.moveDueAtMs == 0 {
			unit.moveDueAtMs = nowMs + reactMs
		} else if nowMs >= unit.moveDueAtMs {
			unit.escortMoving, unit.moveDueAtMs = true, 0
			if rand.IntN(2) == 0 {
				angle := escortFormationAngle(unit.Name) + (rand.Float64()-0.5)*1.4
				radius := 120 + rand.Float64()*140
				unit.roamDX, unit.roamDZ = radius*math.Cos(angle), radius*math.Sin(angle)
				target = offsetPoint(leader, unit.roamDX, unit.roamDZ)
				distToSlot = WaypointDistance(position(unit), target)
			}
		}
	case !unit.escortMoving:
		unit.moveDueAtMs = 0
	}
	if unit.escortMoving {
		step := stepDistanceFor(unit)
		// Near its spot a follower keeps to the leader's pace, scaled by its
		// own temper, instead of sprint-stop stuttering.
		if leaderUnit != nil && distToSlot <= 80 {
			pace := stepDistanceFor(leaderUnit) * float32(paceFactor)
			if pace < 3.5 {
				pace = 3.5
			}
			if step > pace {
				step = pace
			}
		}
		r.moveLocked(unit, target, step)
		if unit.noteProgress(nowMs) >= followStuckMs {
			angle := escortFormationAngle(unit.Name)
			unit.place(offsetPoint(leader, 28*math.Cos(angle), 28*math.Sin(angle)), nowMs)
			unit.escortMoving, unit.roamAtMs = false, 0
		}
	}
	return nil
}

/*
================
followerTemper

A follower's own way of keeping up, fixed per name: how far its spot may
drift before it moves (slack, 4-9m), how long it takes to react (0.3-1.5s)
and how much faster than the leader it closes in (1.0-1.3x).
================
*/
func followerTemper(name string) (slack float64, reactMs int64, paceFactor float64) {
	var h uint32 = 2166136261
	for i := len(name) - 1; i >= 0; i-- {
		h ^= uint32(name[i])
		h *= 16777619
	}
	slack = 40 + float64(h%51)
	reactMs = 300 + int64((h>>8)%1201)
	paceFactor = 1 + float64((h>>20)%31)/100
	return slack, reactMs, paceFactor
}

func escortFormationAngle(name string) float64 {
	var h uint32 = 2166136261
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	// Spread escorts across 8 clean slots around the leader.
	return float64(h%8) * (2 * math.Pi / 8)
}

// traderSlotRadius is how far a following Trader keeps from its leader.
// A following Trader walks the leader's trail: a footstep every trailStep,
// at most trailMax of them, each passed once it is within trailReached.
const (
	traderSlotRadius float64 = 5 * UnitsPerMeter
	trailStep        float64 = 3 * UnitsPerMeter
	trailReached     float64 = 2 * UnitsPerMeter
	trailMax                 = 400
)

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
	// The leader's footsteps: a way the leader really walked, around the
	// railings and walls a straight line to the slot would run into.
	if n := len(unit.trail); n == 0 || WaypointDistance(unit.trail[n-1], leader) >= trailStep {
		unit.trail = append(unit.trail, leader)
		if len(unit.trail) > trailMax {
			unit.trail = unit.trail[len(unit.trail)-trailMax:]
		}
	}
	here := position(unit)
	for i := len(unit.trail) - 1; i >= 0; i-- {
		if WaypointDistance(here, unit.trail[i]) <= trailReached {
			// Already at (or past) this footstep: go on from the next one.
			unit.trail = unit.trail[i+1:]
			break
		}
	}
	dist := WaypointDistance(here, slot)
	if dist <= 15 {
		unit.catchingUp, unit.progressAtMs, unit.trail = false, 0, nil
		return nil
	}
	unit.catchingUp = true
	if leaderUnit != nil && unit.HiredBy == "" && WaypointDistance(position(unit), leader) > 320 {
		// Far behind another agent: its route (and ferries) brings it back.
		// A hired Trader follows the footsteps instead: its leader's route
		// may have been planned from anywhere when it took the lead.
		r.stepEscortFollowLocked(unit, leaderUnit)
		unit.noteProgress(nowMs)
		return nil
	}
	var follow *Waypoint
	if WaypointDistance(position(unit), leader) > 320 {
		follow = &leader
	}
	target := slot
	if len(unit.trail) > 0 {
		target = unit.trail[0]
	}
	r.moveLocked(unit, target, stepDistanceFor(unit))
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
		if unit.Role != RoleTrader && unit.State == StateIdleInTown {
			unit.place(offsetPoint(home, 20, 20), nowMs)
			unit.pendingTeleport = nil
			unit.roamAtMs = 0
			return
		}
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
	if u.RunKind == RunGrand && u.Role == RoleTrader {
		u.CargoStars = u.standingCargoStars
	}
	u.lastFerry = Waypoint{}
	u.DedicatedEscortFor, u.EscortingTrader, u.ReinforcingTrader = "", "", ""
	u.HiredBy, u.hireLeftTown, u.leftTownAtMs, u.leaderMissingAtMs, u.GoodsCostGold = "", false, 0, 0, 0
	u.seenInParty, u.hireDone = false, false
	u.RunKind, u.State = RunNone, StateIdleInTown
	u.UnderAttack, u.catchingUp, u.combatRejoining, u.roamAtMs, u.progressAtMs = false, false, false, 0, 0
	town := r.homeLocked(u)
	u.OriginTown, u.DestTown = town, nextTown(town)
	u.Route, u.WaypointIndex = nil, 0
	if u.DeadUntilMs > 0 || u.HP <= 0 {
		u.pendingTeleport = nil
		u.AvailableForHire = false
		return
	}
	if at, inTown := r.placeAtLocked(position(u)); teleportHome && (!inTown || at != town) {
		angle := escortFormationAngle(u.Name)
		tp := offsetPoint(r.terminalLocked(town), 25*math.Cos(angle), 25*math.Sin(angle))
		u.place(tp, nowMs)
		u.pendingTeleport = nil
	}
	if u.Role == RoleTrader {
		u.CargoStacks = 10
		u.TownDepartAtMs = nowMs + TownRestDurationMs
	}
	u.AvailableForHire = hireable(u)
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
	if kind == RunGrand {
		trader.CargoStars = trader.standingCargoStars
	}
	if kind == RunSolo || kind == RunEscorted {
		trader.escortPreference = r.assignedEscortCountLocked(trader) - 1
		if trader.escortPreference == 0 {
			trader.escortPreference = -1
		}
	}
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
	if trader.RunKind == RunSolo || trader.RunKind == RunEscorted {
		trader.escortPreference = min(EscortsPerRun+1, r.assignedEscortCountLocked(trader)+1)
	}
	if trader.RunKind == RunGrand {
		trader.CargoStars = trader.standingCargoStars
	}
	if trader.RunKind == RunGrand && trader.EscortingTrader != "" {
		// A follower Trader of the grand caravan: only its own guards leave.
		for _, u := range r.agents[trader.Division] {
			if u != trader && u.HiredBy == "" && (strings.EqualFold(u.EscortingTrader, trader.Name) || strings.EqualFold(u.ReinforcingTrader, trader.Name)) {
				r.releaseLocked(u, nowMs, true)
			}
		}
	} else {
		for _, u := range r.agents[trader.Division] {
			if u == trader || u.HiredBy != "" {
				continue
			}
			if (trader.RunKind == RunGrand && u.RunKind == RunGrand) || strings.EqualFold(u.DedicatedEscortFor, trader.Name) || strings.EqualFold(u.EscortingTrader, trader.Name) || strings.EqualFold(u.ReinforcingTrader, trader.Name) {
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

func (r *Runtime) eligibleTraderLocked(division string, nowMs int64, kind RunKind) *AgentUnit {
	var best *AgentUnit
	for _, u := range r.agents[division] {
		if !eligibleTrader(u, nowMs) || (kind == RunSolo && u.escortPreference > 0) ||
			(kind == RunEscorted && u.escortPreference < 0) {
			continue
		}
		if best == nil || (kind == RunEscorted && u.escortPreference > 0 && best.escortPreference == 0) ||
			((kind == RunSolo || (u.escortPreference > 0) == (best.escortPreference > 0)) && u.TownDepartAtMs < best.TownDepartAtMs) {
			best = u
		}
	}
	return best
}

func (r *Runtime) assignedEscortCountLocked(trader *AgentUnit) int {
	n := 0
	for _, u := range r.agents[trader.Division] {
		if u.Role == RoleHunter && u.DedicatedEscortFor == trader.Name {
			n++
		}
	}
	return n
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
	guard.catchingUp, guard.combatRejoining, guard.roamAtMs = false, false, 0
	guard.lastFerry = Waypoint{}
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
		trader := r.eligibleTraderLocked(division, nowMs, RunSolo)
		if trader == nil {
			break
		}
		r.dispatchTraderLocked(trader, RunSolo, nil, nowMs)
	}
	// Killed highway Thieves are replaced by rested town Thieves.
	// Also recover any un-hired Thief stranded in Thief Den with a stale travel/vengeance state.
	patrols := 0
	var spare []*AgentUnit
	for _, u := range r.agents[division] {
		if u.Role != RoleThief || u.HiredBy != "" {
			continue
		}
		if place, inPlace := r.placeAtLocked(position(u)); inPlace && place == TownThiefDen && !u.IsDead(nowMs) && !u.RespawnPending {
			if u.RunKind == RunVengeance && u.EscortingTrader != "" {
				leader := r.findAgentLocked(division, u.EscortingTrader)
				if leader == nil || leader.RunKind != RunVengeance || leader.IsDead(nowMs) {
					r.releaseLocked(u, nowMs, false)
				}
			} else if u.State != StateIdleInTown && u.RunKind == RunNone {
				if len(u.Route) > 2 && u.OriginTown != TownThiefDen {
					u.WaypointIndex = 2
					u.place(u.Route[2], nowMs)
					u.pendingTeleport = nil
				} else {
					r.releaseLocked(u, nowMs, false)
				}
			}
		}
		if hireable(u) {
			if nowMs >= u.TownDepartAtMs {
				spare = append(spare, u)
			}
		} else if u.State != StateIdleInTown && !u.IsDead(nowMs) {
			patrols++
		}
	}
	for ; patrols < rosterHighwayThieves && len(spare) > 0; patrols++ {
		// A replacement leaves the Den for an ambush spot on a city road.
		thief := spare[0]
		spare = spare[1:]
		from := rosterTowns[rand.IntN(2)]
		thief.OriginTown, thief.DestTown = from, nextTown(from)
		thief.State, thief.AvailableForHire = StateTraveling, false
		thief.Route, thief.WaypointIndex = r.route(thief), 0
		if len(thief.Route) > 2 {
			thief.WaypointIndex = 2
			thief.place(thief.Route[2], nowMs)
			thief.pendingTeleport = nil
		}
	}
	for ; escorted < StandingEscortedRuns; escorted++ {
		trader := r.eligibleTraderLocked(division, nowMs, RunEscorted)
		if trader == nil {
			break
		}
		wanted := trader.escortPreference
		if wanted == 0 {
			wanted = EscortsPerRun
		}
		guards := r.pickEscortsLocked(trader, wanted)
		if len(guards) == 0 {
			break
		}
		r.dispatchTraderLocked(trader, RunEscorted, guards, nowMs)
	}
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
