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
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	worldgeom "opensro.online/server/internal/game/world"
)

/*
================================================================================
Agent states, profiles, and WorldCallbacks port
================================================================================
*/

// AgentState describes the current lifecycle phase of an AI caravan agent.
type AgentState string

const (
	StateIdleInTown  AgentState = "IDLE_IN_TOWN"
	StateRecruiting  AgentState = "RECRUITING"
	StateTraveling   AgentState = "TRAVELING"
	StateInCombat    AgentState = "IN_COMBAT"
	StateParleying   AgentState = "PARLEYING"
	StateDelivering  AgentState = "DELIVERING"
	StateReinforcing AgentState = "REINFORCING"
)

// AgentUnit represents one active AI Caravan or Thief agent on the Silkroad.
type AgentUnit struct {
	Name              string
	Division          string
	Role              AgentRole
	Personality       Personality
	State             AgentState
	HomeTown          TownID
	OriginTown        TownID
	DestTown          TownID
	RegionID          uint16
	X                 float32
	Y                 float32
	Z                 float32
	MaxHP             int
	HP                int
	MaxMP             int
	MP                int
	WeaponCode        string
	JobSuitCode       string
	PrimarySkillID    uint32
	RunSpeed          float32
	RespawnPending    bool
	CargoStars        uint8
	CargoStacks       int
	StackValueGold    int64
	Route             []Waypoint
	WaypointIndex     int
	UnderAttack       bool
	LastAttacker      string
	LastPotionAtMs    int64
	LastSpokeAtMs     int64
	LastSpokenLine    string
	TownDepartAtMs    int64
	AvailableForHire  bool
	EscortingTrader   string
	ReinforcingTrader string
	// nextRemoteAtMs is the earliest time the next remote decision may be
	// asked for this agent.
	nextRemoteAtMs int64
	lastMoveAtMs   int64
}

// WorldHooks is an optional consumer-owned callback port invoked when an AI
// agent speaks in local chat, drops parley cargo stacks, or pays out an escort
// contract to a player.
type WorldHooks struct {
	Ferry             func(division, name string, sourceRef, targetID uint32) (Waypoint, bool)
	FollowFerry       func(division, name string, leader Waypoint) (Waypoint, bool)
	Move              func(name string, from, to Waypoint, distance float32) (Waypoint, bool)
	Player            func(division, name string) (PlayerPosition, bool)
	OnAgentChat       func(division, agentName string, regionID uint16, message string)
	OnParleyCargoDrop func(division, agentName string, regionID uint16, x, y, z float32, stacks int, stackGoldValue int64)
	OnContractReward  func(division, hunterPlayerName string, rewardGold int64, traderAgentName string)
	OnHireFee         func(division, playerName string, fee int64) bool
	Heal              func(division, name string, amount int64) int64
}

const (
	// remoteDecisionIntervalMs spaces one agent's remote decisions: at most
	// one request in flight per agent, and the next one asked no sooner
	// than this after the last (port-only pacing, not native).
	remoteDecisionIntervalMs int64 = 3_000

	// memoryFlushInterval is how often the flush loop writes dirty
	// memories and route heat to the store, off the tick.
	memoryFlushInterval = 5 * time.Second
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
	distress  map[string]DistressCall
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
		distress:     make(map[string]DistressCall),
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
	for _, unit := range roster {
		if terminal, ok := r.cfg.Terminals[unit.OriginTown]; ok && isTownRegion(unit.RegionID) {
			unit.RegionID, unit.X, unit.Y, unit.Z = terminal.RegionID, terminal.X, terminal.Y, terminal.Z
		}
		unit.Route = r.route(unit)
		if unit.Role == RoleThief && len(r.cfg.Highway) > 0 && len(unit.Route) > 1 {
			point := unit.Route[1]
			unit.RegionID, unit.X, unit.Y, unit.Z = point.RegionID, point.X, point.Y, point.Z
			unit.WaypointIndex = 1
		}
	}
	for _, unit := range roster {
		if unit.Role == RoleHunter && unit.EscortingTrader != "" {
			for _, leader := range roster {
				if leader.Name == unit.EscortingTrader {
					unit.RegionID, unit.X, unit.Y, unit.Z = leader.RegionID, leader.X+10, leader.Y, leader.Z
					unit.OriginTown, unit.DestTown = leader.OriginTown, leader.DestTown
					break
				}
			}
		}
	}
	r.agents[division] = roster
	return cloneUnits(roster)
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
	r.ensureMinActiveTradesLocked(division, nowMs)
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		r.mu.Unlock()
		return Decision{}, nil, false
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
	if contract, ok := r.contracts.ActiveContractFor(division, unit.Name); ok && contract.TraderIsAI {
		near := false
		for _, p := range nearby {
			if strings.EqualFold(p.Name, contract.HunterName) && p.JobType == domain.JobHunter && p.HPPercent > 0 && p.Distance <= 320 {
				near = true
			}
		}
		r.contracts.SampleEscort(division, unit.Name, near, nowMs)
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
		Distress:     r.activeDistressForLocked(division, unit, nowMs),
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
	if unit.Role == RoleTrader && !unit.UnderAttack && r.parleys.TruceActive(division, unit.Name, nowMs) {
		decision = Decision{Action: ActionContinueRoute, Source: BrainHardcoded}
	}
	decision.Dialogue = SafeDialogue(decision.Dialogue)
	decision.MemoryNote = SafeDialogue(decision.MemoryNote)
	prevSpokeAtMs := unit.LastSpokeAtMs
	prevLine := unit.LastSpokenLine
	effects := r.applyDecisionLocked(unit, decision, nowMs)
	if decision.Action == ActionReinforceAlly && unit.State == StateInCombat {
		decision.Action, decision.TargetName = ActionAttackTarget, unit.LastAttacker
	}
	snapshot := *unit
	var ferry *Waypoint
	if snapshot.State == StateTraveling && snapshot.WaypointIndex < len(snapshot.Route) && snapshot.Route[snapshot.WaypointIndex].FerryTargetID != 0 {
		point := snapshot.Route[snapshot.WaypointIndex]
		ferry = &point
	}
	r.mu.Unlock()
	if effects.drop > 0 && r.hooks.OnParleyCargoDrop != nil {
		r.hooks.OnParleyCargoDrop(division, snapshot.Name, snapshot.RegionID, snapshot.X, snapshot.Y, snapshot.Z, effects.drop, snapshot.StackValueGold)
	}
	if effects.reward != nil && r.hooks.OnContractReward != nil {
		contract := effects.reward
		r.hooks.OnContractReward(division, contract.HunterName, contract.RewardGold, snapshot.Name)
	}
	if effects.heal > 0 && r.hooks.Heal != nil {
		hp := r.hooks.Heal(division, snapshot.Name, int64(effects.heal))
		r.mu.Lock()
		if live := r.findAgentLocked(division, agentName); live != nil && !live.RespawnPending {
			live.HP = int(hp)
			snapshot = *live
		}
		r.mu.Unlock()
	}
	if ferry != nil && r.hooks.Ferry != nil {
		next, ok := r.hooks.Ferry(division, snapshot.Name, ferry.FerrySourceRef, ferry.FerryTargetID)
		if ok {
			r.mu.Lock()
			if live := r.findAgentLocked(division, agentName); live != nil && !live.RespawnPending && live.WaypointIndex == snapshot.WaypointIndex && live.RegionID == snapshot.RegionID && live.X == snapshot.X && live.Z == snapshot.Z {
				live.RegionID, live.X, live.Y, live.Z = next.RegionID, next.X, next.Y, next.Z
				live.WaypointIndex++
				snapshot = *live
			}
			r.mu.Unlock()
		}
	}

	if effects.follow != nil && r.hooks.FollowFerry != nil {
		next, ok := r.hooks.FollowFerry(division, snapshot.Name, *effects.follow)
		if ok {
			r.mu.Lock()
			if live := r.findAgentLocked(division, agentName); live != nil && !live.RespawnPending && live.RegionID == snapshot.RegionID && live.X == snapshot.X && live.Z == snapshot.Z {
				live.RegionID, live.X, live.Y, live.Z = next.RegionID, next.X, next.Y, next.Z
				snapshot = *live
			}
			r.mu.Unlock()
		}
	}
	if decision.Dialogue != "" && r.hooks.OnAgentChat != nil {
		if decision.Dialogue != prevLine || nowMs-prevSpokeAtMs >= 15_000 {
			r.hooks.OnAgentChat(division, snapshot.Name, snapshot.RegionID, decision.Dialogue)
		}
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

func (r *Runtime) applyDecisionLocked(unit *AgentUnit, d Decision, nowMs int64) agentEffects {
	effects := agentEffects{}
	switch d.Action {
	case ActionUsePotion:
		if nowMs-unit.LastPotionAtMs >= 1500 {
			heal := (unit.MaxHP * 35) / 100
			unit.HP += heal
			if unit.HP > unit.MaxHP {
				unit.HP = unit.MaxHP
			}
			effects.heal = heal
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
			effects.drop = dropStacks
		}
	case ActionAttackTarget:
		unit.State = StateInCombat
		var target Waypoint
		found := false
		if other := r.findAgentLocked(unit.Division, d.TargetName); other != nil {
			target, found = Waypoint{RegionID: other.RegionID, X: other.X, Y: other.Y, Z: other.Z}, true
		} else if r.hooks.Player != nil {
			if player, ok := r.hooks.Player(unit.Division, d.TargetName); ok {
				target, found = player.At, true
			}
		}
		if found && WaypointDistance(Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}, target) > 20 {
			r.moveLocked(unit, target, 5)
		}
	case ActionReinforceAlly:
		if call := r.activeDistressForLocked(unit.Division, unit, nowMs); call != nil {
			r.stepReinforcementLocked(unit, call)
		}
	case ActionContinueRoute, ActionAssistAlly, ActionRecruitEscort:
		if unit.State == StateIdleInTown || unit.State == StateRecruiting {
			// Town-staged agents stay in town until dispatched, hired, or called to reinforce.
			break
		}
		unit.State = StateTraveling
		if unit.Role == RoleHunter && unit.EscortingTrader != "" {
			if leader := r.findAgentLocked(unit.Division, unit.EscortingTrader); leader != nil {
				target := Waypoint{RegionID: leader.RegionID, X: leader.X, Y: leader.Y, Z: leader.Z}
				if WaypointDistance(Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}, target) > 320 {
					effects.follow = &target
				}
				r.moveLocked(unit, target, 5)
				break
			}
			if r.hooks.Player != nil {
				if leader, ok := r.hooks.Player(unit.Division, unit.EscortingTrader); ok && leader.HPPercent > 0 && leader.JobType == domain.JobTrader {
					if WaypointDistance(leader.At, Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}) > 320 {
						effects.follow = &leader.At
					}
					if WaypointDistance(leader.At, Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}) > 12 {
						r.moveLocked(unit, leader.At, 5)
					}
					break
				}
			}
			unit.EscortingTrader, unit.State, unit.AvailableForHire = "", StateIdleInTown, true
			r.contracts.Cancel(unit.Division, unit.Name)
			break
		}
		effects.reward = r.advanceAlongRouteLocked(unit, nowMs)
	}
	if d.Dialogue != "" {
		unit.LastSpokenLine = d.Dialogue
		unit.LastSpokeAtMs = nowMs
	}
	return effects
}

func (r *Runtime) advanceAlongRouteLocked(unit *AgentUnit, nowMs int64) *EscortContract {
	var completed *EscortContract
	if len(unit.Route) == 0 {
		unit.Route = r.route(unit)
		unit.WaypointIndex = 0
	}
	if unit.WaypointIndex >= len(unit.Route) {
		return nil
	}
	target := unit.Route[unit.WaypointIndex]
	elapsed := int64(100)
	if unit.lastMoveAtMs != 0 {
		elapsed = min(max(nowMs-unit.lastMoveAtMs, 0), 2000)
	}
	unit.lastMoveAtMs = nowMs
	speed := unit.RunSpeed
	if speed <= 0 {
		speed = 50
	}
	distance := float32(elapsed) * speed / 1000
	arrived := r.moveLocked(unit, target, distance)
	if !arrived {
		return nil
	}
	unit.WaypointIndex++
	if unit.WaypointIndex < len(unit.Route) {
		return nil
	}

	// Arrived at destination terminal: complete delivery & rotate route.
	unit.State = StateDelivering
	if unit.Role == RoleTrader {
		if contract, ok := r.contracts.CompleteAITraderRun(unit.Division, unit.Name, unit.RegionID, nowMs, r.memories); ok {
			completed = &contract
		}
		unit.CargoStacks = 10
		unit.State = StateRecruiting
		unit.TownDepartAtMs = nowMs + TownRestDurationMs
	} else if unit.Role == RoleHunter && unit.EscortingTrader == "" {
		unit.State = StateIdleInTown
		unit.RespawnPending = true
		unit.AvailableForHire = true
	}
	unit.OriginTown, unit.DestTown = unit.DestTown, unit.OriginTown
	unit.Route = r.route(unit)
	unit.WaypointIndex = 0
	r.ensureMinActiveTradesLocked(unit.Division, nowMs)
	return completed
}

/*
==================
RecordPlayerAttack

RecordPlayerAttack applies damage from attackerPlayerName onto agentName. If
agentName was inside an active parley truce window, the attack immediately
triggers a -100 Betrayal Vendetta. If the blow defeats the agent, robbery and
route danger heat are recorded in the persistent MemoryBook.
==================
*/
func (r *Runtime) RecordPlayerAttack(
	division, agentName, attackerPlayerName string,
	damage int,
	nowMs int64,
) (betrayedParley bool, defeated bool, memRow domain.CaravanAgentMemoryRow) {
	r.EnsureDivisionRoster(division)
	if _, row, betrayed := r.parleys.OnDamageDuringParley(division, agentName, attackerPlayerName, nowMs, r.memories); betrayed {
		betrayedParley = true
		memRow = row
	}

	r.mu.Lock()
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		r.mu.Unlock()
		return betrayedParley, false, memRow
	}
	unit.UnderAttack = true
	unit.LastAttacker = attackerPlayerName
	if damage > 0 {
		unit.HP -= damage
	}
	if unit.Role == RoleTrader && unit.HP > 0 {
		r.raiseDistressLocked(division, unit, attackerPlayerName, nowMs)
	}
	var stolenGold int64
	regionID := unit.RegionID
	if unit.HP <= 0 {
		defeated = true
		stolenGold = int64(unit.CargoStacks) * unit.StackValueGold
		unit.CargoStacks = 0
		r.contracts.Cancel(division, unit.Name)
		home := TownWaypoint(unit.HomeTown)
		if terminal, ok := r.cfg.Terminals[unit.HomeTown]; ok {
			home = terminal
		}
		unit.RegionID = home.RegionID
		unit.X = home.X
		unit.Y = home.Y
		unit.Z = home.Z
		unit.HP = unit.MaxHP
		unit.UnderAttack = false
		unit.State = StateIdleInTown
		unit.OriginTown = unit.HomeTown
		if unit.DestTown == unit.HomeTown {
			unit.DestTown = TownDonwhang
			if unit.HomeTown == TownDonwhang {
				unit.DestTown = TownJangan
			}
		}
		unit.Route, unit.WaypointIndex = nil, 0
		unit.TownDepartAtMs = nowMs + TownRestDurationMs
		if unit.Role == RoleHunter {
			unit.AvailableForHire = true
			unit.EscortingTrader = ""
			unit.ReinforcingTrader = ""
		}
		r.ensureMinActiveTradesLocked(division, nowMs)
	}
	r.mu.Unlock()

	if defeated {
		memRow = r.memories.RecordRobberyOrKill(division, agentName, attackerPlayerName, stolenGold, regionID, "", nowMs)
	}
	return betrayedParley, defeated, memRow
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
	if allowed, known := worldgeom.RegionPlayerCombat(regionID); known && !allowed {
		return true
	}
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
