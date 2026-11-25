/*
===========================================================================

runtime.go - AI Caravan & Thief agent roster and state-machine runtime

Coordinates the player-like AI Traders, Hunters, and Thieves operating on the
Jangan <-> Donwhang <-> Hotan highway, stepping their trade runs, combat
potions/skills, parley cargo drops, mercenary contracts, and persistent
Grudge & Trust memories.

===========================================================================
*/
package caravanai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

/*
================================================================================
Agent states, profiles, and WorldCallbacks port
================================================================================
*/

// AgentState describes the current lifecycle phase of an AI caravan agent.
type AgentState string

const (
	StateIdleInTown AgentState = "IDLE_IN_TOWN"
	StateRecruiting AgentState = "RECRUITING"
	StateTraveling  AgentState = "TRAVELING"
	StateInCombat   AgentState = "IN_COMBAT"
	StateParleying  AgentState = "PARLEYING"
	StateDelivering AgentState = "DELIVERING"
	// StateDead: the agent fell where it stands and waits out its
	// respawn delay (agentRespawnDelayMs) before reappearing at its
	// home town. Port-only pacing; native players own a different
	// death flow and agents are not native.
	StateDead AgentState = "DEAD"
)

// AgentUnit represents one active AI Caravan or Thief agent on the Silkroad.
type AgentUnit struct {
	// CharacterID is the agent's synthetic character identity
	// (entities.go): stable per roster slot, never a store rowid.
	CharacterID int64
	Name        string
	Division    string
	Role        AgentRole
	Personality Personality
	State       AgentState
	HomeTown    TownID
	OriginTown  TownID
	DestTown    TownID
	RegionID    uint16
	X           float32
	Y           float32
	Z           float32
	MaxHP       int
	HP          int
	MaxMP       int
	MP          int
	WeaponCode  string
	JobSuitCode string
	// ModelCode is the shipped character model the agent wears
	// (entities.go): the roster's men and women match their suits.
	ModelCode      string
	PrimarySkillID uint32
	// Level is the agent's combat level for damage taken (slice 3c):
	// the defender level the strike formula weighs. Port-only flavour;
	// agents mitigate nothing in this slice.
	Level          uint8
	CargoStars     uint8
	CargoStacks    int
	StackValueGold int64
	Route          []Waypoint
	WaypointIndex  int
	UnderAttack    bool
	LastAttacker   string
	// DeadUntilMs is when a dead agent (StateDead) respawns at its
	// home town; zero while the agent lives.
	DeadUntilMs    int64
	LastPotionAtMs int64
	LastSpokeAtMs  int64
	LastSpokenLine string
	// nextRemoteAtMs is the earliest time the next remote decision may be
	// asked for this agent.
	nextRemoteAtMs int64
	// lastAdvanceMs is when the agent last moved along its route; the
	// next step's distance budget is the elapsed time at walk speed.
	// Zero means it has not moved yet.
	lastAdvanceMs int64
}

// Pose is an agent's position on the world surface: a region and its
// region-local coordinates, Y included (the surface height there).
type Pose struct {
	RegionID uint16
	X        float32
	Y        float32
	Z        float32
}

// Walker is the consumer-owned movement port: one step of at most maxDist
// world units from `from` toward `to` over the real nav surface, returning
// the new pose and whether `to` was reached. The shard wires it to the
// monster march (PlanMonsterPath); without a Walker an agent holds its
// position instead of inventing movement.
type Walker interface {
	Step(from, to Pose, maxDist float64) (next Pose, arrived bool)
}

// WorldHooks is an optional consumer-owned callback port invoked when an AI
// agent speaks in local chat, drops parley cargo stacks, or pays out an escort
// contract to a player. Walker is the movement port above.
type WorldHooks struct {
	OnAgentChat       func(division, agentName string, regionID uint16, message string)
	OnParleyCargoDrop func(division, agentName string, regionID uint16, x, y, z float32, stacks int, stackGoldValue int64)
	OnContractReward  func(division, hunterPlayerName string, rewardGold int64, traderAgentName string)
	Walker            Walker
}

const (
	// remoteDecisionIntervalMs spaces one agent's remote decisions: at most
	// one request in flight per agent, and the next one asked no sooner
	// than this after the last (port-only pacing, not native).
	remoteDecisionIntervalMs int64 = 3_000

	// memoryFlushInterval is how often the flush loop writes dirty
	// memories and route heat to the store, off the tick.
	memoryFlushInterval = 5 * time.Second

	// walkSpeedUnitsPerSec is the agents' pace along the highway: the
	// on-foot walk pace the world uses for characters (simulation
	// WalkSpeed, 20 units/sec). Port-only tuning, not native.
	walkSpeedUnitsPerSec = 20.0
)

// Runtime manages the division roster of AI Caravan & Thief agents.
//
// The tick never waits on a remote decision. Every step decides with the
// local (hardcoded) brain; when a remote brain is configured it is asked
// in the background, and its answer replaces the local one on a later
// step only while the agent is still in the state it was asked about.
type Runtime struct {
	mu        sync.Mutex
	cfg       Config
	local     *HardcodedBrain
	remote    Brain
	memories  *MemoryBook
	parleys   *ParleyTracker
	contracts *ContractBook
	hooks     WorldHooks
	agents    map[string][]*AgentUnit
	// inflight holds each agent's outstanding remote decision.
	inflight map[string]*remoteDecision
	// remoteCtx ends every remote request at Close.
	remoteCtx    context.Context
	cancelRemote context.CancelFunc
	remoteJobs   sync.WaitGroup
}

// remoteDecision is one remote brain request: its answer, and the agent
// state it was asked about.
type remoteDecision struct {
	result chan Decision
	state  AgentState
}

/*
==================
NewRuntime

NewRuntime constructs an AI Caravan & Thief Runtime with the provided Config,
MemoryStore, and optional WorldHooks. The owner runs Run on its own
goroutine and calls Close at shutdown, before the store closes.
==================
*/
func NewRuntime(cfg Config, store MemoryStore, hooks WorldHooks) *Runtime {
	var remote Brain
	if brain, isRemote := NewBrain(cfg).(*LLMBrain); isRemote {
		remote = brain
	}
	remoteCtx, cancelRemote := context.WithCancel(context.Background())
	return &Runtime{
		cfg:          cfg,
		local:        NewHardcodedBrain(),
		remote:       remote,
		memories:     NewMemoryBook(store),
		parleys:      NewParleyTracker(),
		contracts:    NewContractBook(),
		hooks:        hooks,
		agents:       make(map[string][]*AgentUnit),
		inflight:     make(map[string]*remoteDecision),
		remoteCtx:    remoteCtx,
		cancelRemote: cancelRemote,
	}
}

/*
==================
Run

Writes dirty memories and route heat every memoryFlushInterval until ctx
ends. A failed write is kept and retried on the next pass.
==================
*/
func (r *Runtime) Run(ctx context.Context) {
	ticker := time.NewTicker(memoryFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.memories.Flush(); err != nil {
				log.WithError(err).Warn("caravan-ai: memory flush failed; retrying next pass")
			}
		}
	}
}

/*
==================
Close

Ends every remote request, waits for their goroutines, and writes the last
dirty rows. Called after the tick and Run have stopped and before the store
closes.
==================
*/
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.cancelRemote()
	r.remoteJobs.Wait()
	return r.memories.Flush()
}

/*
==================
Enabled

Enabled reports whether the AI Caravan & Thief runtime is active.
==================
*/
func (r *Runtime) Enabled() bool {
	return r != nil && r.cfg.Enabled
}

/*
==================
Config

Config returns the runtime's resolved configuration.
==================
*/
func (r *Runtime) Config() Config {
	if r == nil {
		return Config{}
	}
	return r.cfg
}

/*
==================
Memories

Memories returns the runtime's persistent Grudge & Trust MemoryBook.
==================
*/
func (r *Runtime) Memories() *MemoryBook {
	if r == nil {
		return nil
	}
	return r.memories
}

/*
==================
Parleys

Parleys returns the runtime's active Bribe / Parley tracker.
==================
*/
func (r *Runtime) Parleys() *ParleyTracker {
	if r == nil {
		return nil
	}
	return r.parleys
}

/*
==================
Contracts

Contracts returns the runtime's two-way Mercenary ContractBook.
==================
*/
func (r *Runtime) Contracts() *ContractBook {
	if r == nil {
		return nil
	}
	return r.contracts
}

/*
==================
EnsureDivisionRoster

EnsureDivisionRoster hydrates persisted memories and spawns the default
Jangan <-> Donwhang <-> Hotan roster of AI Traders, Hunters, and Thieves for
division if not already present.
==================
*/
func (r *Runtime) EnsureDivisionRoster(division string) []*AgentUnit {
	division = strings.TrimSpace(division)
	if division == "" {
		division = "default"
	}
	r.mu.Lock()
	if existing, ok := r.agents[division]; ok {
		out := cloneUnits(existing)
		r.mu.Unlock()
		return out
	}
	r.mu.Unlock()

	_ = r.memories.LoadDivision(division)
	heat := r.memories.HeatSnapshot(division)

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.agents[division]; ok {
		return cloneUnits(existing)
	}
	roster := defaultDivisionRoster(division, heat)
	r.agents[division] = roster
	return cloneUnits(roster)
}

func defaultDivisionRoster(division string, heat map[uint16]int) []*AgentUnit {
	jangan := TownWaypoint(TownJangan)
	donwhang := TownWaypoint(TownDonwhang)
	hotan := TownWaypoint(TownHotan)

	routeJD := SelectRoute(TownJangan, TownDonwhang, 4, PersonalityCautious, heat)
	routeDH := SelectRoute(TownDonwhang, TownHotan, 5, PersonalityGreedy, heat)
	routeHJ := SelectRoute(TownHotan, TownJangan, 3, PersonalityHonorable, heat)
	viperDock, viperIndex := ferrySpawn(routeJD)

	return []*AgentUnit{
		{
			CharacterID:    AgentCharacterIDBase,
			ModelCode:      "CHAR_CH_MAN_MERCHANT",
			Name:           "[AI]Trader_Jin",
			Division:       division,
			Role:           RoleTrader,
			Personality:    PersonalityCautious,
			State:          StateTraveling,
			HomeTown:       TownJangan,
			OriginTown:     TownJangan,
			DestTown:       TownDonwhang,
			RegionID:       jangan.RegionID,
			X:              jangan.X,
			Y:              jangan.Y,
			Z:              jangan.Z,
			MaxHP:          12000,
			HP:             12000,
			MaxMP:          6000,
			MP:             6000,
			WeaponCode:     "ITEM_CH_SPEAR_07_C_RARE",
			JobSuitCode:    "ITEM_CH_M_TRADE_TRADER_02",
			PrimarySkillID: 318,
			Level:          60,
			CargoStars:     4,
			CargoStacks:    10,
			StackValueGold: 25000,
			Route:          routeJD,
		},
		{
			CharacterID:    AgentCharacterIDBase + 1,
			ModelCode:      "CHAR_CH_WOMAN_MERCHANT",
			Name:           "[AI]Trader_Mei",
			Division:       division,
			Role:           RoleTrader,
			Personality:    PersonalityGreedy,
			State:          StateTraveling,
			HomeTown:       TownDonwhang,
			OriginTown:     TownDonwhang,
			DestTown:       TownHotan,
			RegionID:       donwhang.RegionID,
			X:              donwhang.X,
			Y:              donwhang.Y,
			Z:              donwhang.Z,
			MaxHP:          11500,
			HP:             11500,
			MaxMP:          6500,
			MP:             6500,
			WeaponCode:     "ITEM_CH_BOW_07_C_RARE",
			JobSuitCode:    "ITEM_CH_W_TRADE_TRADER_04",
			PrimarySkillID: 260,
			Level:          60,
			CargoStars:     5,
			CargoStacks:    12,
			StackValueGold: 30000,
			Route:          routeDH,
		},
		{
			CharacterID:    AgentCharacterIDBase + 2,
			ModelCode:      "CHAR_CH_MAN_FIGHTER",
			Name:           "[AI]Hunter_Kwan",
			Division:       division,
			Role:           RoleHunter,
			Personality:    PersonalityHonorable,
			State:          StateTraveling,
			HomeTown:       TownJangan,
			OriginTown:     TownHotan,
			DestTown:       TownJangan,
			RegionID:       hotan.RegionID,
			X:              hotan.X,
			Y:              hotan.Y,
			Z:              hotan.Z,
			MaxHP:          14000,
			HP:             14000,
			MaxMP:          5500,
			MP:             5500,
			WeaponCode:     "ITEM_CH_BLADE_07_C_RARE",
			JobSuitCode:    "ITEM_CH_M_TRADE_HUNTER_02",
			PrimarySkillID: 112,
			Level:          65,
			Route:          routeHJ,
		},
		{
			CharacterID:    AgentCharacterIDBase + 3,
			ModelCode:      "CHAR_CH_MAN_ADVENTURER",
			Name:           "[AI]Thief_Viper",
			Division:       division,
			Role:           RoleThief,
			Personality:    PersonalityVengeful,
			State:          StateTraveling,
			HomeTown:       TownDonwhang,
			OriginTown:     TownJangan,
			DestTown:       TownDonwhang,
			RegionID:       viperDock.RegionID,
			X:              viperDock.X,
			Y:              viperDock.Y,
			Z:              viperDock.Z,
			MaxHP:          11000,
			HP:             11000,
			MaxMP:          5500,
			MP:             5500,
			WeaponCode:     "ITEM_CH_TBLADE_07_C_RARE",
			JobSuitCode:    "ITEM_CH_M_TRADE_THIEF_02",
			PrimarySkillID: 318,
			Level:          65,
			Route:          routeJD,
			WaypointIndex:  viperIndex,
		},
	}
}

/*
==================
ferrySpawn

Where the thief waits: the near dock of the route's first ferry
crossing. Returns that dock and the route index of the far dock, so the
agent's first step is the crossing itself. A route without a crossing
starts at its first anchor.
==================
*/
func ferrySpawn(route []Waypoint) (Waypoint, int) {
	for i := 1; i < len(route); i++ {
		if route[i].Ferry && route[i-1].Ferry {
			return route[i-1], i
		}
	}
	if len(route) > 0 {
		return route[0], 0
	}
	return Waypoint{}, 0
}

/*
==================
StepAgent

StepAgent evaluates one AI caravan agent against nearby players at nowMs,
executing potions, parley cargo bribes, route movement, or combat targeting.
==================
*/
func (r *Runtime) StepAgent(
	ctx context.Context,
	division, agentName string,
	nearby []ObservedPlayer,
	nowMs int64,
) (Decision, *AgentUnit, bool) {
	r.EnsureDivisionRoster(division)
	r.parleys.ResolveExpired(nowMs, r.memories)

	r.mu.Lock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		r.mu.Unlock()
		return Decision{}, nil, false
	}
	if unit.State == StateDead {
		if nowMs < unit.DeadUntilMs {
			// Dead agents neither decide nor move; the snapshot
			// still reports them so callers see the corpse's place.
			snapshot := *unit
			r.mu.Unlock()
			return Decision{}, &snapshot, true
		}
		unit.respawnAtHomeLocked(nowMs)
	}

	enriched := make([]ObservedPlayer, len(nearby))
	for i, p := range nearby {
		if mem, ok := r.memories.Relationship(division, unit.Name, p.Name); ok {
			p.TrustScore = mem.TrustScore
			p.Kills = mem.Kills
			p.Deaths = mem.Deaths
			p.GoldStolen = mem.GoldStolen
			p.LastNote = mem.LastNote
		}
		enriched[i] = p
	}

	sit := Situation{
		AgentName:    unit.Name,
		Role:         unit.Role,
		Personality:  unit.Personality,
		RegionID:     unit.RegionID,
		RegionHeat:   r.memories.RegionHeat(division, unit.RegionID),
		InTown:       isTownRegion(unit.RegionID),
		HPPercent:    percentOf(unit.HP, unit.MaxHP),
		MPPercent:    percentOf(unit.MP, unit.MaxMP),
		CargoStars:   unit.CargoStars,
		CargoStacks:  unit.CargoStacks,
		UnderAttack:  unit.UnderAttack,
		ParleyReady:  r.parleys.CanOfferParley(division, unit.Name, nowMs),
		NearbyAllies: countFriendlyHunters(enriched),
		Players:      enriched,
	}
	r.mu.Unlock()

	decision := r.local.Decide(ctx, sit)

	r.mu.Lock()
	unit = r.findAgentLocked(division, agentName)
	if unit == nil {
		r.mu.Unlock()
		return decision, nil, false
	}
	if remote, ok := r.remoteDecisionLocked(unit, sit, nowMs); ok {
		decision = remote
	}
	r.applyDecisionLocked(unit, decision, nowMs)
	snapshot := *unit
	r.mu.Unlock()

	if decision.Dialogue != "" && r.hooks.OnAgentChat != nil {
		r.hooks.OnAgentChat(division, snapshot.Name, snapshot.RegionID, decision.Dialogue)
	}
	return decision, &snapshot, true
}

/*
==================
remoteDecisionLocked

Takes the agent's remote answer when it has arrived, or asks for one in the
background; never blocks. An answer applies only while the agent is still in
the state it was asked about and its target is still nearby; otherwise it is
dropped and the local decision stands. A new request goes out at most once
per remoteDecisionIntervalMs per agent, and only when the brain mode wants
this situation (hybrid asks only during encounters).
==================
*/
func (r *Runtime) remoteDecisionLocked(unit *AgentUnit, sit Situation, nowMs int64) (Decision, bool) {
	if r.remote == nil {
		return Decision{}, false
	}
	key := agentKey(unit.Division, unit.Name)
	if req, pending := r.inflight[key]; pending {
		select {
		case d := <-req.result:
			delete(r.inflight, key)
			if req.state == unit.State && targetNearby(d.TargetName, sit.Players) {
				return d, true
			}
		default:
		}
		return Decision{}, false
	}
	if !wantsRemote(r.cfg.BrainMode, sit) || nowMs < unit.nextRemoteAtMs {
		return Decision{}, false
	}
	unit.nextRemoteAtMs = nowMs + remoteDecisionIntervalMs
	req := &remoteDecision{result: make(chan Decision, 1), state: unit.State}
	r.inflight[key] = req
	r.remoteJobs.Add(1)
	go func() {
		defer r.remoteJobs.Done()
		// The channel holds one answer, so the send never blocks even
		// when the agent is gone and nobody reads it.
		req.result <- r.remote.Decide(r.remoteCtx, sit)
	}()
	return Decision{}, false
}

/*
==================
wantsRemote

Whether the brain mode asks the remote brain about this situation: llm
always, hybrid only during an encounter (players nearby or under attack).
==================
*/
func wantsRemote(mode BrainMode, sit Situation) bool {
	switch mode {
	case BrainLLM:
		return true
	case BrainHybrid:
		return len(sit.Players) > 0 || sit.UnderAttack
	default:
		return false
	}
}

/*
==================
targetNearby

Whether a remote decision's target (empty for none) is still among the
players the agent sees now.
==================
*/
func targetNearby(target string, players []ObservedPlayer) bool {
	if target == "" {
		return true
	}
	for _, p := range players {
		if strings.EqualFold(p.Name, target) {
			return true
		}
	}
	return false
}

// agentKey names one agent in one division.
func agentKey(division, agentName string) string {
	return strings.TrimSpace(division) + "|" + strings.ToLower(strings.TrimSpace(agentName))
}

func (r *Runtime) applyDecisionLocked(unit *AgentUnit, d Decision, nowMs int64) {
	switch d.Action {
	case ActionUsePotion:
		if nowMs-unit.LastPotionAtMs >= 1500 {
			heal := (unit.MaxHP * 35) / 100
			unit.HP += heal
			if unit.HP > unit.MaxHP {
				unit.HP = unit.MaxHP
			}
			unit.LastPotionAtMs = nowMs
		}
	case ActionOfferParley:
		dropStacks := ComputeParleyCargoDrop(unit.CargoStacks, unit.Personality)
		if dropStacks > 0 {
			unit.CargoStacks -= dropStacks
			unit.State = StateParleying
			unit.UnderAttack = false
			dropGold := int64(dropStacks) * unit.StackValueGold
			r.parleys.StartParley(unit.Division, unit.Name, d.TargetName, unit.RegionID, dropStacks, dropGold, nowMs)
			if r.hooks.OnParleyCargoDrop != nil {
				r.hooks.OnParleyCargoDrop(unit.Division, unit.Name, unit.RegionID, unit.X, unit.Y, unit.Z, dropStacks, unit.StackValueGold)
			}
		}
	case ActionAttackTarget:
		unit.State = StateInCombat
	case ActionContinueRoute:
		unit.State = StateTraveling
		r.advanceAlongRouteLocked(unit, nowMs)
	}
	if d.Dialogue != "" {
		unit.LastSpokenLine = d.Dialogue
		unit.LastSpokeAtMs = nowMs
	}
}

/*
==================
advanceAlongRouteLocked

Moves the agent one tick along its route. Between two Ferry anchors the
step is the ferry ride: the pose lands on the far dock at once. Every
other step goes through the consumer's Walker, budgeted by the time
since the agent last moved at walkSpeedUnitsPerSec; with no Walker the
agent holds its position. Reaching the final anchor completes the run
(deliverAgentLocked).
==================
*/
func (r *Runtime) advanceAlongRouteLocked(unit *AgentUnit, nowMs int64) {
	if len(unit.Route) == 0 {
		heat := r.memories.HeatSnapshot(unit.Division)
		unit.Route = SelectRoute(unit.OriginTown, unit.DestTown, unit.CargoStars, unit.Personality, heat)
		unit.WaypointIndex = 0
	}
	if unit.WaypointIndex >= len(unit.Route) {
		return
	}
	target := unit.Route[unit.WaypointIndex]

	// The ferry ride: two adjacent Ferry anchors are the two docks of
	// one crossing, so arrival is the hop itself.
	if unit.WaypointIndex > 0 && target.Ferry && unit.Route[unit.WaypointIndex-1].Ferry {
		unit.RegionID = target.RegionID
		unit.X = target.X
		unit.Z = target.Z
		// The far dock's own anchor height, the pose the march proof
		// starts the next hop from (not the near dock's carried height).
		unit.Y = target.Y
		unit.lastAdvanceMs = nowMs
		unit.WaypointIndex++
		if unit.WaypointIndex >= len(unit.Route) {
			r.deliverAgentLocked(unit, nowMs)
		}
		return
	}

	walker := r.hooks.Walker
	if walker == nil {
		unit.lastAdvanceMs = nowMs
		return
	}
	var dtMs int64
	if unit.lastAdvanceMs > 0 && nowMs > unit.lastAdvanceMs {
		dtMs = nowMs - unit.lastAdvanceMs
	}
	unit.lastAdvanceMs = nowMs
	from := Pose{RegionID: unit.RegionID, X: unit.X, Y: unit.Y, Z: unit.Z}
	to := Pose{RegionID: target.RegionID, X: target.X, Y: target.Y, Z: target.Z}
	next, arrived := walker.Step(from, to, walkSpeedUnitsPerSec*float64(dtMs)/1000)
	unit.RegionID = next.RegionID
	unit.X = next.X
	unit.Y = next.Y
	unit.Z = next.Z
	if !arrived {
		return
	}
	// On the anchor exactly, so the next chord starts from proven
	// ground; Y stays the walker's surface height.
	unit.X = target.X
	unit.Z = target.Z
	unit.RegionID = target.RegionID
	unit.WaypointIndex++
	if unit.WaypointIndex >= len(unit.Route) {
		r.deliverAgentLocked(unit, nowMs)
	}
}

/*
==================
deliverAgentLocked

Completes a run whose final anchor was reached: pays any escort
contract, restocks a trader, and rotates the route back the other way.
==================
*/
func (r *Runtime) deliverAgentLocked(unit *AgentUnit, nowMs int64) {
	unit.State = StateDelivering
	if unit.Role == RoleTrader {
		if contract, ok := r.contracts.CompleteAITraderRun(unit.Division, unit.Name, unit.RegionID, nowMs, r.memories); ok {
			if r.hooks.OnContractReward != nil {
				r.hooks.OnContractReward(unit.Division, contract.HunterName, contract.RewardGold, unit.Name)
			}
		}
		unit.CargoStacks = 10
	}
	unit.OriginTown, unit.DestTown = unit.DestTown, unit.OriginTown
	heat := r.memories.HeatSnapshot(unit.Division)
	unit.Route = SelectRoute(unit.OriginTown, unit.DestTown, unit.CargoStars, unit.Personality, heat)
	unit.WaypointIndex = 0
}

/*
/*
==================
HandlePlayerCommand

HandlePlayerCommand parses interactive player chat commands directed at AI
caravan agents:
  - "!accept <TraderAgentName>" (Hunter player accepts an AI Trader escort run)
  - "!hire <HunterAgentName>"   (Trader player hires an AI Hunter escort)
  - "!memory <AgentName>"       (Inspects how an AI agent remembers the player)

==================
*/
func (r *Runtime) HandlePlayerCommand(
	division, playerName string,
	playerJob uint8,
	rawText string,
	nowMs int64,
) (reply string, handled bool) {
	fields := strings.Fields(strings.TrimSpace(rawText))
	if len(fields) < 2 {
		return "", false
	}
	cmd := strings.ToLower(fields[0])
	targetAgent := fields[1]
	r.EnsureDivisionRoster(division)

	switch cmd {
	case "!accept":
		if playerJob != domain.JobHunter {
			return "Only Hunter guild members can accept a caravan escort contract.", true
		}
		r.mu.Lock()
		unit := r.findAgentLocked(division, targetAgent)
		if unit == nil || unit.Role != RoleTrader {
			r.mu.Unlock()
			return fmt.Sprintf("No AI Trader named %s is recruiting here.", targetAgent), true
		}
		contract := r.contracts.BindAITraderEscort(division, unit.Name, playerName, unit.OriginTown, unit.DestTown, unit.CargoStars, nowMs)
		r.mu.Unlock()
		return FormatContractReply(contract), true

	case "!hire":
		if playerJob != domain.JobTrader {
			return "Only Traders can hire an AI Hunter mercenary escort.", true
		}
		r.mu.Lock()
		unit := r.findAgentLocked(division, targetAgent)
		if unit == nil || unit.Role != RoleHunter {
			r.mu.Unlock()
			return fmt.Sprintf("No AI Hunter named %s is available for hire.", targetAgent), true
		}
		contract := r.contracts.HireAIHunter(division, playerName, unit.Name, unit.OriginTown, unit.DestTown, nowMs)
		r.mu.Unlock()
		return FormatContractReply(contract), true

	case "!memory":
		row, ok := r.memories.Relationship(division, targetAgent, playerName)
		if !ok {
			return fmt.Sprintf("%s has no recorded history with %s yet (Trust: 0).", targetAgent, playerName), true
		}
		return fmt.Sprintf(
			"%s's memory of %s -> Trust: %d, Kills: %d, Deaths: %d, Gold Stolen: %d (%s)",
			row.AgentName, row.PlayerName, row.TrustScore, row.Kills, row.Deaths, row.GoldStolen, row.LastNote,
		), true
	default:
		return "", false
	}
}

func (r *Runtime) findAgentLocked(division, agentName string) *AgentUnit {
	roster := r.agents[strings.TrimSpace(division)]
	for _, u := range roster {
		if strings.EqualFold(u.Name, strings.TrimSpace(agentName)) {
			return u
		}
	}
	return nil
}

func cloneUnits(src []*AgentUnit) []*AgentUnit {
	out := make([]*AgentUnit, len(src))
	for i, u := range src {
		cp := *u
		out[i] = &cp
	}
	return out
}

func isTownRegion(regionID uint16) bool {
	return regionID == RegionJanganTown || regionID == RegionDonwhangTown || regionID == RegionHotanTown
}

// FullHPPercent is what a vital reads when its maximum is unknown.
const FullHPPercent = 100

func percentOf(cur, maxVal int) int {
	return HPPercent(int64(cur), int64(maxVal))
}

/*
==================
HPPercent

current as a whole percentage of maximum, clamped to [0, 100]; a maximum
of zero or less reads as full.
==================
*/
func HPPercent(current, maximum int64) int {
	if maximum <= 0 {
		return FullHPPercent
	}
	pct := (current * 100) / maximum
	if pct < 0 {
		return 0
	}
	if pct > FullHPPercent {
		return FullHPPercent
	}
	return int(pct)
}

func countFriendlyHunters(players []ObservedPlayer) int {
	n := 0
	for _, p := range players {
		if p.JobType == domain.JobHunter && p.TrustScore > domain.CaravanVendettaScore {
			n++
		}
	}
	return n
}
