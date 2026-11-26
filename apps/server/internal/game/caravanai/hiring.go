/*
===========================================================================

hiring.go - players hiring AI agents, who leads them, and how a hire ends

Hire offers and the hire itself, the lead choice (the hirer or one hired
Trader), the hands-off supervision that ends a hire when its Traders sold
or the hirer leaves the party, and the outbox flushed after the lock.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
)

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
		offer := HireOffer{Name: u.Name, Role: u.Role, Town: u.OriginTown, Fee: r.HireFee()}
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
	if unit.Role != RoleTrader && r.freeOfRoleLocked(division, unit.Role, r.homeLocked(unit)) <= HirePoolReserve {
		r.mu.Unlock()
		return fmt.Sprintf("Only %d free %ss are left in %s; they stay for other players.", HirePoolReserve, unit.Role.Title(), townTitle(r.homeLocked(unit)))
	}
	if len(r.hiresOfLocked(division, player)) >= MaxHiresPerPlayer {
		r.mu.Unlock()
		return fmt.Sprintf("You already lead %d hired companions.", MaxHiresPerPlayer)
	}
	goods := int64(0)
	trader := unit.Role == RoleTrader
	if trader {
		if town := r.nearestTownLocked(position(unit)); town != place {
			r.mu.Unlock()
			return fmt.Sprintf("%s waits in %s; Traders are hired in the town they wait in.", short, townTitle(town))
		}
		if stars == 0 {
			stars = unit.CargoStars
		}
		stars = min(max(stars, 1), MaxHireStars)
	}
	r.mu.Unlock()
	if trader {
		// Priced outside the lock: the world values the transport's load.
		goods = r.StarGoods(division, name, stars)
	}

	price := r.HireFee() + goods
	if r.hooks.OnHireFee == nil || !r.hooks.OnHireFee(division, player, price) {
		if goods > 0 {
			return fmt.Sprintf("Hiring %s costs %s gold: %s for the hire and %s for the goods.", short, GoldText(price), GoldText(r.HireFee()), GoldText(goods))
		}
		return fmt.Sprintf("Hiring %s costs %s gold.", short, GoldText(price))
	}

	r.mu.Lock()
	unit = r.findAgentLocked(division, agentName)
	if unit == nil || !hireable(unit) || len(r.hiresOfLocked(division, player)) >= MaxHiresPerPlayer ||
		(unit.Role != RoleTrader && r.freeOfRoleLocked(division, unit.Role, r.homeLocked(unit)) <= HirePoolReserve) {
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
	unit.seenInParty, unit.hireDone = false, false
	unit.RunKind, unit.State, unit.AvailableForHire = RunHired, StateTraveling, false
	unit.EscortingTrader, unit.GoodsCostGold, unit.TownDepartAtMs = player, goods, 0
	unit.OriginTown, unit.DestTown = town, nextTown(town)
	unit.Route, unit.WaypointIndex = nil, 0
	unit.catchingUp, unit.combatRejoining, unit.roamAtMs = false, false, 0
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
toggleLeadLocked / setLeadLocked / LeadState

Who leads a hirer's hired Traders: the hirer itself (the Traders follow
it) or one of them (the rest follow that Trader, which drives the run).
!leed toggles the hirer; the party board's lead button (0x7FCF
operation 2) names any of them. With no choice the first hired Trader in
party order leads.
================
*/
func (r *Runtime) toggleLeadLocked(division, player string) string {
	if len(r.hiresOfLocked(division, player)) == 0 {
		return "You have no hired caravan agents to lead."
	}
	key := agentKey(division, player)
	if strings.EqualFold(r.leadBy[key], player) {
		delete(r.leadBy, key)
		return "The first hired Trader in your party leads the caravan again."
	}
	r.leadBy[key] = player
	return "You lead the caravan: your hired Traders follow you."
}

func (r *Runtime) setLeadLocked(division, player, who string) string {
	key := agentKey(division, player)
	if strings.EqualFold(who, player) {
		if len(r.hiresOfLocked(division, player)) == 0 {
			return "You have no hired caravan agents to lead."
		}
		r.leadBy[key] = player
		return "You lead the caravan: your hired Traders follow you."
	}
	for _, t := range r.hiredTradersLocked(division, player) {
		if strings.EqualFold(t.Name, who) {
			r.leadBy[key] = t.Name
			return fmt.Sprintf("%s leads the caravan now.", DisplayName(t.Name))
		}
	}
	return "Only you or one of your hired Traders can lead the caravan."
}

// SetLead names who leads player's hired Traders (the party board's button).
func (r *Runtime) SetLead(division, player, who string) string {
	division = strings.TrimSpace(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setLeadLocked(division, player, who)
}

// LeadState is who leads player's hired Traders and who may: the hirer
// and its hired Traders, in lead order. Empty with no hired Trader.
func (r *Runtime) LeadState(division, player string) (leader string, candidates []string) {
	division = strings.TrimSpace(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	traders := r.hiredTradersLocked(division, player)
	if len(traders) == 0 {
		return "", nil
	}
	candidates = []string{player}
	for _, t := range traders {
		candidates = append(candidates, t.Name)
	}
	if strings.EqualFold(r.leadBy[agentKey(division, player)], player) {
		return player, candidates
	}
	return traders[0].Name, candidates
}

/*
================
SetPartyOrder / Hirers

The party's member order, set by the world each tick outside the lock (the
party runtime calls back into this one).
================
*/
func (r *Runtime) SetPartyOrder(division, player string, members []string, nowMs int64) {
	division = strings.TrimSpace(division)
	r.mu.Lock()
	r.partyOrder[agentKey(division, player)] = members
	r.partyLeftLocked(division, player, members, nowMs)
	r.mu.Unlock()
	r.flushOutbox(division)
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
	if unit.Role != RoleTrader || strings.EqualFold(r.leadBy[agentKey(unit.Division, unit.HiredBy)], unit.HiredBy) {
		unit.EscortingTrader = unit.HiredBy
		return
	}
	traders := r.hiredTradersLocked(unit.Division, unit.HiredBy)
	if len(traders) == 0 || traders[0] == unit {
		if unit.EscortingTrader != "" {
			// It takes over the driving: whatever route it had was planned
			// from somewhere else, so it plans afresh from here.
			unit.Route, unit.WaypointIndex = nil, 0
		}
		unit.EscortingTrader = ""
		return
	}
	unit.EscortingTrader = traders[0].Name
}

/*
================
hiredTradersLocked

A hirer's hired Traders in lead order: the chosen one first, then party
order, then hire order.
================
*/
func (r *Runtime) hiredTradersLocked(division, player string) []*AgentUnit {
	var traders []*AgentUnit
	for _, u := range r.hiresOfLocked(division, player) {
		if u.Role == RoleTrader {
			traders = append(traders, u)
		}
	}
	key := agentKey(division, player)
	chosen, order := r.leadBy[key], r.partyOrder[key]
	rank := func(u *AgentUnit) int {
		if strings.EqualFold(u.Name, chosen) {
			return -1
		}
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
	return traders
}

/*
================
superviseHireLocked

Hands-off hires: a hired Trader that left town sells its goods at the
shop of the next town it reaches (the hire town again only after
HireRunMinOutMs) and stays there; once all of a hirer's Traders sold, the
other hired agents are released as each reaches a town. The hirer
disbanding the party, or putting an agent out of it (partyLeftLocked),
ends it too, as do a job change and the hirer staying away. Reports true
when the unit must not move otherwise this step.
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
	if unit.IsDead(nowMs) {
		return false
	}
	if unit.Role != RoleTrader && !unit.carriesGoods && !unit.denCargo {
		// A Thief with a haul ends its hire after the sale (thiefcargo.go).
		if r.superviseHireTimeLocked(unit, p, nowMs) {
			return true
		}
	}
	place, inTown := r.placeAtLocked(position(unit))
	if !inTown {
		if !unit.hireLeftTown {
			unit.hireLeftTown, unit.leftTownAtMs = true, nowMs
		}
		return false
	}
	if unit.hireDone {
		r.endHireLocked([]*AgentUnit{unit}, HireCompleted, nowMs)
		return true
	}
	if unit.Role != RoleTrader || !unit.hireLeftTown || (place == unit.HireTown && nowMs-unit.leftTownAtMs < HireRunMinOutMs) {
		return false
	}
	// Arrived: the Trader rides to the town's shop and sells there.
	if shop := r.terminalLocked(place); unit.CargoStacks > 0 && WaypointDistance(position(unit), shop) > hireSellReach {
		unit.State = StateTraveling
		r.moveLocked(unit, shop, stepDistanceFor(unit))
		return true
	}
	player := unit.HiredBy
	unit.sellAt = place
	r.endHireLocked([]*AgentUnit{unit}, HireCompleted, nowMs)
	if len(r.hiredTradersLocked(unit.Division, player)) == 0 {
		for _, u := range r.hiresOfLocked(unit.Division, player) {
			u.hireDone = true
		}
	}
	return true
}

/*
================
partyLeftLocked

SetPartyOrder's check: a hired agent the hirer's party held and no longer
does (disbanded, or put out) ends its hire. Only while the hirer is
online, so a logout is left to HireLeaderMissingMs.
================
*/
func (r *Runtime) partyLeftLocked(division, player string, members []string, nowMs int64) {
	if r.hooks.Player == nil {
		return
	}
	if _, online := r.hooks.Player(division, player); !online {
		return
	}
	var gone []*AgentUnit
	for _, u := range r.hiresOfLocked(division, player) {
		if slices.ContainsFunc(members, func(m string) bool { return strings.EqualFold(m, u.Name) }) {
			u.seenInParty = true
		} else if u.seenInParty {
			gone = append(gone, u)
		}
	}
	r.endHireLocked(gone, HireAborted, nowMs)
}

/*
================
stepHiredLeadLocked

The leading hired Trader drives the run, standing still while its hirer
or another hired Trader is out of range (HiredLeadStopDistance).
================
*/
func (r *Runtime) stepHiredLeadLocked(unit *AgentUnit, nowMs int64) {
	if r.hooks.Player == nil {
		return
	}
	p, ok := r.hooks.Player(unit.Division, unit.HiredBy)
	if !ok || WaypointDistance(position(unit), p.At) > HiredLeadStopDistance || r.hiredTraderBehindLocked(unit) {
		unit.lastMoveAtMs, unit.progressAtMs = nowMs, 0
		return
	}
	if len(unit.Route) == 0 {
		r.planHiredRunLocked(unit, nowMs)
	}
	r.advanceAlongRouteLocked(unit, nowMs)
}

// HiredLeadStopDistance: the leading hired Trader stops while its hirer or
// another hired Trader of the hire is farther than this, and sets off
// again once all are within it.
const HiredLeadStopDistance float64 = 80 * UnitsPerMeter

/*
================
hiredTraderBehindLocked

Another living hired Trader of the lead's hire is out of range.
================
*/
func (r *Runtime) hiredTraderBehindLocked(lead *AgentUnit) bool {
	for _, u := range r.hiresOfLocked(lead.Division, lead.HiredBy) {
		if u == lead || u.Role != RoleTrader || u.HP <= 0 || u.DeadUntilMs > 0 || u.RespawnPending {
			continue
		}
		if WaypointDistance(position(u), position(lead)) > HiredLeadStopDistance {
			return true
		}
	}
	return false
}

/*
================
planHiredRunLocked

A hired Trader that takes the lead heads for the nearest town it may sell
in, from where it stands. In a town it leaves by that town's own roads,
from their start, and the shortest wins. Out on the road it joins the
road that brings it soonest to a town, the walk to the road weighing four
times the road itself (off-road ground is slow and may not be passable),
and never at a road's last point from afar (that would be a straight line
across the map). The town it was hired in counts only after
HireRunMinOutMs out; until then it is never the goal.
================
*/
func (r *Runtime) planHiredRunLocked(unit *AgentUnit, nowMs int64) {
	at := position(unit)
	homeAllowed := unit.hireLeftTown && nowMs-unit.leftTownAtMs >= HireRunMinOutMs
	town, inTown := r.placeAtLocked(at)
	inTown = inTown && town != TownThiefDen
	type plan struct {
		origin, dest TownID
		points       []Waypoint
		index        int
		cost         float64
	}
	var best *plan
	consider := func(key [2]TownID, points []Waypoint) {
		if len(points) < 2 || key[1] == TownThiefDen || key[1] == key[0] || (key[1] == unit.HireTown && !homeAllowed) {
			return
		}
		if inTown && key[0] != town {
			return
		}
		points = append([]Waypoint(nil), points...)
		for i, point := range points {
			if terminal, ok := r.cfg.Terminals[point.Town]; ok {
				points[i] = terminal
			}
		}
		index, cost := 0, roadLength(points, 0)
		if !inTown {
			var offRoad float64
			index, offRoad = routeJoin(at, points)
			if index == len(points)-1 && offRoad > HiredJoinReach {
				return
			}
			cost = 4*offRoad + roadLength(points, index)
		}
		if best == nil || cost < best.cost {
			best = &plan{origin: key[0], dest: key[1], points: points, index: index, cost: cost}
		}
	}
	for key, points := range r.cfg.Highway {
		consider(key, points)
	}
	for key, points := range r.cfg.Detours {
		consider(key, points)
	}
	for key, variants := range r.cfg.Routes {
		for _, points := range variants {
			consider(key, points)
		}
	}
	if best == nil {
		unit.Route, unit.WaypointIndex = r.route(unit), 0
		return
	}
	unit.OriginTown, unit.DestTown = best.origin, best.dest
	unit.Route, unit.WaypointIndex = best.points, best.index
}

// HiredJoinReach is how near a road's last point a Trader must stand to
// join the road there (anything farther is no road left to follow).
const HiredJoinReach float64 = 30 * UnitsPerMeter

/*
================
routeJoin / roadLength

Where a traveller at `at` joins a road and how far off the road it is:
the road segment nearest to it (by the distance to the segment, not to
its ends), joined at the segment's far end. roadLength is the road from
a waypoint to its end.
================
*/
func routeJoin(at Waypoint, points []Waypoint) (int, float64) {
	join, off := 0, WaypointDistance(at, points[0])
	for i := 0; i+1 < len(points); i++ {
		if d := segmentDistance(at, points[i], points[i+1]); d < off {
			join, off = i+1, d
		}
	}
	return join, off
}

// segmentDistance is how far p is from the segment a-b, from the three
// distances alone (they may cross regions).
func segmentDistance(p, a, b Waypoint) float64 {
	pa, pb, ab := WaypointDistance(p, a), WaypointDistance(p, b), WaypointDistance(a, b)
	switch {
	case ab < 1e-6 || pa*pa >= pb*pb+ab*ab:
		return pb
	case pb*pb >= pa*pa+ab*ab:
		return pa
	}
	along := (pa*pa - pb*pb + ab*ab) / (2 * ab)
	return math.Sqrt(max(pa*pa-along*along, 0))
}

func roadLength(points []Waypoint, from int) float64 {
	length := 0.0
	for i := from; i+1 < len(points); i++ {
		length += WaypointDistance(points[i], points[i+1])
	}
	return length
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
		var saleTown TownID
		if u.Role == RoleTrader && u.CargoStacks > 0 {
			gold = int64(u.CargoStacks) * u.StackValueGold
			if end == HireCompleted {
				gold = gold * int64(100+HiredGoodsMarginPerStarPct*int(u.CargoStars)) / 100
				saleTown = u.sellAt
			}
		}
		u.sellAt = ""
		r.outbox = append(r.outbox, outboxItem{chat: true, agent: u.Name, regionID: u.RegionID, text: hireFarewell(u, player, end)})
		r.releaseLocked(u, nowMs, true)
		r.outbox = append(r.outbox, outboxItem{player: player, agent: u.Name, gold: gold, end: end, saleTown: saleTown})
		if len(r.hiresOfLocked(u.Division, player)) == 0 {
			delete(r.leadBy, agentKey(u.Division, player))
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
		if item.notice {
			if r.hooks.OnHireNotice != nil {
				r.hooks.OnHireNotice(division, item.player, item.text)
			}
			continue
		}
		if item.contract {
			if r.hooks.OnContractFormed != nil {
				r.hooks.OnContractFormed(division, item.player, item.agent)
			}
			continue
		}
		if item.saleTown != "" && r.hooks.CargoSale != nil {
			// The goods sell as a player's would at that shop.
			if sale, ok := r.hooks.CargoSale(division, item.agent, item.saleTown); ok {
				item.gold = sale
			}
		}
		if item.gold > 0 && r.hooks.OnHirePayout != nil {
			r.hooks.OnHirePayout(division, item.player, item.gold)
		}
		if r.hooks.OnHireEnded != nil {
			r.hooks.OnHireEnded(division, item.player, item.agent, item.end, item.gold)
		}
	}
}

/*
================
StarGoods

What goods rated stars cost on the agent's transport: the world's own
pricing (WorldHooks.StarGoods, as a player's load is rated), else the
flat GoodsForStars. Called outside the lock.
================
*/
func (r *Runtime) StarGoods(division, agentName string, stars uint8) int64 {
	if r.hooks.StarGoods != nil {
		if gold := r.hooks.StarGoods(division, agentName, stars); gold > 0 {
			return gold
		}
	}
	return GoodsForStars(stars)
}

/*
================
DebugHires

!caravandebug (GM): each of player's hires as one line - state, last
decision, whom it follows, route progress, and how far the hirer and the
other hired Traders are. Lines stay under the client's chat limit.
================
*/
func (r *Runtime) DebugHires(division, player string) string {
	division = strings.TrimSpace(division)
	var at Waypoint
	hirerKnown := false
	if r.hooks.Player != nil {
		if p, ok := r.hooks.Player(division, player); ok {
			at, hirerKnown = p.At, true
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	units := r.hiresOfLocked(division, player)
	if len(units) == 0 {
		return "You have no hired caravan agents."
	}
	var lines []string
	for _, u := range units {
		hirer := -1.0
		if hirerKnown {
			hirer = WaypointDistance(position(u), at) / UnitsPerMeter
		}
		far := 0.0
		for _, o := range units {
			if o != u && o.Role == RoleTrader {
				far = max(far, WaypointDistance(position(u), position(o))/UnitsPerMeter)
			}
		}
		lead := u.EscortingTrader
		if lead == "" {
			lead = "-"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s f:%s r:%d/%d h:%.0fm t:%.0fm",
			DisplayName(u.Name), u.State, u.lastAction, DisplayName(lead), u.WaypointIndex, len(u.Route), hirer, far))
	}
	return strings.Join(lines, "\n")
}

/*
================
superviseHireTimeLocked

A Hunter's or Thief's hire time: a notice HireWarnMs before its hour runs
out and the end when it does; hired with no Trader, the end of the run
too - the hirer back in a town (or the Den) after HireOutRunMs out. A
shorter trip out does not count. True when the hire ended.
================
*/
func (r *Runtime) superviseHireTimeLocked(unit *AgentUnit, p PlayerPosition, nowMs int64) bool {
	expires := unit.HiredAtMs + HireDurationMs
	if nowMs >= expires {
		r.endHireLocked([]*AgentUnit{unit}, HireTimedOut, nowMs)
		return true
	}
	if nowMs >= expires-HireWarnMs && !unit.expiryWarned {
		unit.expiryWarned = true
		where := "a specialty shop"
		if unit.Role == RoleThief {
			where = "the Thief Den"
		}
		r.outbox = append(r.outbox, outboxItem{notice: true, player: unit.HiredBy, text: fmt.Sprintf("%s's hire ends in %d minutes. Extend it at %s.", DisplayName(unit.Name), HireWarnMs/60_000, where)})
	}
	if len(r.hiredTradersLocked(unit.Division, unit.HiredBy)) > 0 {
		unit.hirerOutAtMs = 0
		return false
	}
	if _, inTown := r.placeAtLocked(p.At); !inTown {
		if unit.hirerOutAtMs == 0 {
			unit.hirerOutAtMs = nowMs
		}
		return false
	}
	out := unit.hirerOutAtMs
	unit.hirerOutAtMs = 0
	if out > 0 && nowMs-out >= HireOutRunMs {
		r.endHireLocked([]*AgentUnit{unit}, HireCompleted, nowMs)
		return true
	}
	return false
}

/*
================
freeOfRoleLocked

How many agents of role wait free for hire at place (Thieves in the Den,
Hunters at their town).
================
*/
func (r *Runtime) freeOfRoleLocked(division string, role AgentRole, place TownID) int {
	n := 0
	for _, u := range r.agents[division] {
		if u.Role == role && hireable(u) && r.homeLocked(u) == place {
			n++
		}
	}
	return n
}

/*
================
ExtendHire

The hire window's renewal of the player's own Hunter or Thief: another
HireDurationMs for the hire fee, at a city shop (Hunters) or the Den
(Thieves), never more than HireMaxAheadMs ahead.
================
*/
func (r *Runtime) ExtendHire(division, player, agentName string, nowMs int64) string {
	division = strings.TrimSpace(division)
	if r.hooks.Player == nil {
		return "Caravan hiring is unavailable."
	}
	at, ok := r.hooks.Player(division, player)
	if !ok {
		return "Caravan help is hired in town."
	}
	r.mu.Lock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil || !strings.EqualFold(unit.HiredBy, player) || unit.Role == RoleTrader {
		r.mu.Unlock()
		return "Only your own hired Hunters and Thieves can be extended."
	}
	short := DisplayName(unit.Name)
	place, inTown := r.placeAtLocked(at.At)
	if !inTown || (unit.Role == RoleThief) != (place == TownThiefDen) {
		r.mu.Unlock()
		if unit.Role == RoleThief {
			return "Thieves' hires are extended in the Thief Den."
		}
		return "Hunters' hires are extended at a town's specialty shop."
	}
	if unit.HiredAtMs+HireDurationMs-nowMs > HireMaxAheadMs-HireDurationMs {
		r.mu.Unlock()
		return fmt.Sprintf("%s is already hired for the next %d minutes.", short, (unit.HiredAtMs+HireDurationMs-nowMs)/60_000)
	}
	r.mu.Unlock()
	if r.hooks.OnHireFee == nil || !r.hooks.OnHireFee(division, player, r.HireFee()) {
		return fmt.Sprintf("Extending %s's hire costs %s gold.", short, GoldText(r.HireFee()))
	}
	r.mu.Lock()
	unit = r.findAgentLocked(division, agentName)
	if unit == nil || !strings.EqualFold(unit.HiredBy, player) {
		r.mu.Unlock()
		if r.hooks.OnHirePayout != nil {
			r.hooks.OnHirePayout(division, player, r.HireFee())
		}
		return fmt.Sprintf("%s's hire already ended; your gold is returned.", short)
	}
	// The hour runs on from when it would have ended (or from now, if it is
	// past due): HiredAtMs carries the end, and it is persisted with the hire.
	unit.HiredAtMs = max(unit.HiredAtMs, nowMs-HireDurationMs) + HireDurationMs
	unit.expiryWarned = false
	left := (unit.HiredAtMs + HireDurationMs - nowMs) / 60_000
	r.mu.Unlock()
	return fmt.Sprintf("%s's hire is extended: %d minutes left. Paid %s gold.", short, left, GoldText(r.HireFee()))
}
