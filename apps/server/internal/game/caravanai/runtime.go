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
	"errors"
	"math"
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
	Name               string
	Division           string
	Role               AgentRole
	Personality        Personality
	State              AgentState
	HomeTown           TownID
	OriginTown         TownID
	DestTown           TownID
	RegionID           uint16
	X                  float32
	Y                  float32
	Z                  float32
	MaxHP              int
	HP                 int
	MaxMP              int
	MP                 int
	WeaponCode         string
	JobSuitCode        string
	PrimarySkillID     uint32
	RunSpeed           float32
	RespawnPending     bool
	DeadUntilMs        int64
	CargoStars         uint8
	CargoStacks        int
	StackValueGold     int64
	Route              []Waypoint
	WaypointIndex      int
	UnderAttack        bool
	LastAttacker       string
	LastPotionAtMs     int64
	LastSpokeAtMs      int64
	LastSpokenLine     string
	TownDepartAtMs     int64
	AvailableForHire   bool
	EscortingTrader    string
	ReinforcingTrader  string
	DedicatedEscortFor string
	PotionCharges      int
	// RunKind says why the agent is on the road (convoy.go).
	RunKind            RunKind
	standingCargoStars uint8
	// Positive counts require escorts. A negative value prefers solo after a delivery.
	escortPreference int
	// HiredBy names the player who hired the agent for one trade run.
	HiredBy   string
	HiredAtMs int64
	HireTown  TownID
	// GoodsCostGold is what the hirer paid for a hired Trader's goods.
	GoodsCostGold int64
	// CooldownUntilMs holds a robbed Trader's next own run.
	CooldownUntilMs int64
	// Vengeance is the Trader a killed Thief leads its gang back after.
	Vengeance string
	// Walking is set while the agent strolls (published as walk mode).
	Walking bool
	// nextRemoteAtMs is the earliest time the next remote decision may be
	// asked for this agent.
	nextRemoteAtMs     int64
	needsManaRecovery  bool
	lastMoveAtMs       int64
	transportHPPercent int
	combatProgressAtMs int64
	hireLeftTown       bool
	leftTownAtMs       int64
	// seenInParty: the hirer's party has held this hired agent (a hire ends
	// when the agent leaves it); hireDone: the hire's Traders all sold, so
	// this one is released at the next town.
	seenInParty       bool
	hireDone          bool
	leaderMissingAtMs int64
	catchingUp        bool
	combatRejoining   bool
	lastFerry         Waypoint
	escortMoving      bool
	// moveDueAtMs is when a follower whose spot drifted past its slack
	// sets off (its reaction time, followerTemper); 0 while none is due.
	moveDueAtMs int64
	// sellAt is the town whose shop a hired Trader's sale is priced at
	// (set as it sells, read by endHireLocked).
	sellAt           TownID
	roamDX, roamDZ   float64
	lastLeaderAt     Waypoint
	roamAtMs         int64
	progressFrom     Waypoint
	progressAtMs     int64
	pendingTeleport  *Waypoint
	rejoinParty      bool
	vengeanceAt      Waypoint
	vengeanceUntilMs int64
	walkFactor       float32
	// trail is the leader's footsteps a following Trader walks (convoy.go).
	trail []Waypoint
	// loot is the ground item the agent goes to pick up (loot.go).
	lootGID           uint32
	lootAt            Waypoint
	lootUntilMs       int64
	deathAttacker     string
	deathPos          Waypoint
	deathWasVengeance bool
	// CanResurrect reports whether this agent is trained with a Resurrection skill.
	CanResurrect bool
}

const combatStallTimeoutMs int64 = 5000

// WorldHooks is an optional consumer-owned callback port invoked when an AI
// agent speaks in local chat, drops parley cargo stacks, or pays out an escort
// contract to a player.
type WorldHooks struct {
	Ferry             func(division, name string, sourceRef, targetID uint32) (Waypoint, bool)
	FollowFerry       func(division, name string, leader Waypoint) (Waypoint, bool)
	Move              func(name string, from, to Waypoint, distance float32) (Waypoint, bool)
	Player            func(division, name string) (PlayerPosition, bool)
	Casting           func(division, name string) bool
	OnAgentChat       func(division, agentName string, regionID uint16, message string)
	OnParleyCargoDrop func(division, agentName string, regionID uint16, x, y, z float32, stacks int, stackGoldValue int64)
	OnContractFormed  func(division, playerName, agentName string)
	OnContractReward  func(division, hunterPlayerName string, rewardGold int64, traderAgentName string)
	OnHireFee         func(division, playerName string, fee int64) bool
	// StarGoods prices goods that rate stars on the agent's transport, as a
	// player's own load is rated (0 keeps GoodsForStars). CargoSale is what
	// the agent's goods sell for at town's shop. Both run outside the lock.
	StarGoods func(division, agentName string, stars uint8) int64
	CargoSale func(division, agentName string, town TownID) (int64, bool)
	// OnHirePayout credits a hirer: a hired Trader's sale or returned goods
	// cost, or a refused hire's fee.
	OnHirePayout func(division, playerName string, gold int64) bool
	// OnHireEnded reports a released hire (gold is what OnHirePayout got).
	OnHireEnded func(division, playerName, agentName string, end HireEnd, gold int64)
	Heal        func(division, name string, amount int64) int64
	// UseItem has the actor drink one item from its bag (an HP, MP or
	// transport potion); true when it was used. When set it replaces Heal.
	UseItem func(division, name, codename string) bool
	// PickUp has the actor, standing at it, pick up a ground item.
	PickUp func(division, name string, itemGID uint32) bool
}

// The potions agents drink: the largest HP and MP potions and the
// transport's HP potion, the supplies a player would carry.
const (
	HPPotionItem        = "ITEM_ETC_HP_POTION_05"
	MPPotionItem        = "ITEM_ETC_MP_POTION_05"
	TransportPotionItem = "ITEM_ETC_COS_HP_POTION_03"
	SpeedPotionItem     = "ITEM_MALL_MOVE_SPEED_UP_50"
	SpeedPotionFactor   = 1.5
)

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
	// leadBy is who each hirer chose to lead its hired Traders (the hirer
	// or one of them); partyOrder is each hirer's party member order (convoy.go).
	leadBy        map[string]string
	partyOrder    map[string][]string
	grand         map[string]*grandCaravan
	scheduledAtMs map[string]int64
	outbox        []outboxItem
	// manualRuns stops the standing-run scheduler (tests drive runs by hand).
	manualRuns bool
	// hireStore keeps hires across restarts (hires.go); savedHires is what
	// it holds, guarded by hireSaveMu (taken before mu, never after);
	// restoredHires (under mu) hands the boot's rows to the next flush.
	hireStore     HireStore
	hireSaveMu    sync.Mutex
	savedHires    map[string]domain.CaravanHireRow
	restoredHires []domain.CaravanHireRow
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
	hireStore, _ := store.(HireStore)
	return &Runtime{
		hireStore:     hireStore,
		savedHires:    make(map[string]domain.CaravanHireRow),
		cfg:           cfg,
		local:         NewHardcodedBrain(),
		remote:        remote,
		memories:      NewMemoryBook(store),
		parleys:       NewParleyTracker(),
		contracts:     NewContractBook(),
		hooks:         hooks,
		agents:        make(map[string][]*AgentUnit),
		distress:      make(map[string]DistressCall),
		inflight:      make(map[string]*remoteDecision),
		leadBy:        make(map[string]string),
		partyOrder:    make(map[string][]string),
		grand:         make(map[string]*grandCaravan),
		scheduledAtMs: make(map[string]int64),
		remoteCtx:     remoteCtx,
		cancelRemote:  cancelRemote,
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
			if err := r.flushHires(); err != nil {
				log.WithError(err).Warn("caravan-ai: hire flush failed; retrying next pass")
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
	return errors.Join(r.memories.Flush(), r.flushHires())
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
	hires := r.loadHires(division)

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.agents[division]; ok {
		return cloneUnits(existing)
	}
	roster := defaultDivisionRoster(division, heat)
	for _, unit := range roster {
		if terminal, ok := r.cfg.Terminals[unit.OriginTown]; ok && (isTownRegion(unit.RegionID) || unit.OriginTown == TownThiefDen) {
			if unit.OriginTown == TownThiefDen {
				angle := escortFormationAngle(unit.Name)
				spot := offsetPoint(terminal, 25*math.Cos(angle), 25*math.Sin(angle))
				unit.RegionID, unit.X, unit.Y, unit.Z = spot.RegionID, spot.X, spot.Y, spot.Z
			} else {
				unit.RegionID, unit.X, unit.Y, unit.Z = terminal.RegionID, terminal.X, terminal.Y, terminal.Z
			}
		}
		unit.PotionCharges = defaultPotionCharges(unit.Role)
		unit.transportHPPercent = FullHPPercent
		if unit.Role == RoleHunter && !unit.AvailableForHire && unit.EscortingTrader != "" {
			unit.DedicatedEscortFor = unit.EscortingTrader
		}
		unit.Route = r.route(unit)
		if unit.Role == RoleThief && unit.State == StateTraveling && len(r.cfg.Highway) > 0 && len(unit.Route) > 1 {
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
	r.restoreHiresLocked(division, hires, time.Now().UnixMilli())
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
	return r.StepAgentWithWorldThreat(ctx, division, agentName, nearby, false, nowMs)
}

/*
==================
StepAgentWithWorldThreat

Evaluates one AI caravan agent against nearby players at nowMs while
optionally deferring combat movement when an authoritative NPC threat owns
pursuit on the same tick.
==================
*/
func (r *Runtime) StepAgentWithWorldThreat(
	ctx context.Context,
	division, agentName string,
	nearby []ObservedPlayer,
	worldThreatActive bool,
	nowMs int64,
) (Decision, *AgentUnit, bool) {
	r.EnsureDivisionRoster(division)
	r.parleys.ResolveExpired(nowMs, r.memories)

	defer r.flushOutbox(strings.TrimSpace(division))
	// World hooks run outside r.mu (the combat path calls back into it).
	casting := r.hooks.Casting != nil && r.hooks.Casting(division, agentName)
	r.mu.Lock()
	r.scheduleRunsLocked(strings.TrimSpace(division), nowMs)
	unit := r.findAgentLocked(division, agentName)
	if unit == nil {
		r.mu.Unlock()
		return Decision{}, nil, false
	}
	if unit.DeadUntilMs > 0 {
		waitingForRess := nowMs < unit.DeadUntilMs
		if waitingForRess && unit.Role == RoleTrader && unit.HiredBy == "" && !r.hasLivingResurrectingEscortLocked(unit, nowMs) {
			waitingForRess = false
		}
		if waitingForRess {
			snapshot := *unit
			r.mu.Unlock()
			return Decision{Action: ActionContinueRoute, Source: BrainHardcoded}, &snapshot, true
		}
		r.completeRespawnLocked(unit, nowMs)
		snapshot := *unit
		r.mu.Unlock()
		return Decision{Action: ActionContinueRoute, Source: BrainHardcoded}, &snapshot, true
	}

	enriched := make([]ObservedPlayer, 0, len(nearby))
	for _, p := range nearby {
		if p.HPPercent <= 0 {
			continue
		}
		if mem, ok := r.memories.Relationship(division, unit.Name, p.Name); ok {
			p.TrustScore = mem.TrustScore
			p.Kills = mem.Kills
			p.Deaths = mem.Deaths
			p.GoldStolen = mem.GoldStolen
			p.LastNote = mem.LastNote
		}
		enriched = append(enriched, p)
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
		AgentName:          unit.Name,
		Role:               unit.Role,
		Personality:        unit.Personality,
		RegionID:           unit.RegionID,
		RegionHeat:         r.memories.RegionHeat(division, unit.RegionID),
		InTown:             isTownRegion(unit.RegionID),
		HPPercent:          percentOf(unit.HP, unit.MaxHP),
		MPPercent:          percentOf(unit.MP, unit.MaxMP),
		TransportHPPercent: unit.transportHPPercent,
		PotionDepleted:     unit.PotionCharges <= 0,
		Escorted:           r.hasLiveEscortLocked(unit),
		CargoStars:         unit.CargoStars,
		CargoStacks:        unit.CargoStacks,
		UnderAttack:        unit.UnderAttack,
		ParleyReady:        r.parleys.CanOfferParley(division, unit.Name, nowMs),
		NearbyAllies:       countFriendlyHunters(enriched),
		Distress:           r.activeDistressForLocked(division, unit, nowMs),
		Players:            enriched,
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
	lowMana := unit.needsManaRecovery || (unit.MaxMP > 0 && percentOf(unit.MP, unit.MaxMP) <= 80)
	// A real MP potion (UseItem) refills over a few seconds: the agent
	// drinks once per cooldown and goes on meanwhile, not standing still.
	if r.hooks.UseItem == nil && unit.PotionCharges > 0 && lowMana {
		decision = Decision{Action: ActionUsePotion, Source: BrainHardcoded}
	} else if unit.PotionCharges <= 0 {
		unit.needsManaRecovery = false
	}
	mustRejoin := r.escortMustRejoinLocked(unit)
	if mustRejoin || (unit.HiredBy != "" && decision.Action == ActionRecruitEscort) {
		decision = Decision{Action: ActionContinueRoute, Source: BrainHardcoded}
	} else if worldThreatActive {
		decision = Decision{Action: ActionAttackTarget, Source: BrainHardcoded}
	}
	decision.Dialogue = SafeDialogue(decision.Dialogue)
	decision.MemoryNote = SafeDialogue(decision.MemoryNote)
	prevSpokeAtMs := unit.LastSpokeAtMs
	prevLine := unit.LastSpokenLine
	effects := r.applyDecisionLocked(unit, decision, worldThreatActive, mustRejoin, casting, nowMs)
	if decision.Action == ActionReinforceAlly && unit.State == StateInCombat {
		decision.Action, decision.TargetName = ActionAttackTarget, unit.LastAttacker
	}
	snapshot := *unit
	var ferry *Waypoint
	if snapshot.State == StateTraveling && !r.waitForEscortLocked(unit) && snapshot.WaypointIndex < len(snapshot.Route) && snapshot.Route[snapshot.WaypointIndex].FerryTargetID != 0 {
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
	if effects.pickup != 0 && r.hooks.PickUp != nil {
		r.hooks.PickUp(division, snapshot.Name, effects.pickup)
	}
	if effects.potion != "" && r.hooks.UseItem(division, snapshot.Name, effects.potion) && effects.potion == MPPotionItem {
		r.mu.Lock()
		if live := r.findAgentLocked(division, agentName); live != nil {
			live.needsManaRecovery = false
		}
		r.mu.Unlock()
	}
	if effects.heal > 0 && r.hooks.Heal != nil {
		hp := r.hooks.Heal(division, snapshot.Name, int64(effects.heal))
		r.mu.Lock()
		if live := r.findAgentLocked(division, agentName); live != nil && !live.RespawnPending {
			live.HP = int(hp)
			live.needsManaRecovery = false
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
				live.lastFerry = *ferry
				snapshot = *live
			}
			r.mu.Unlock()
		}
	}

	followed := false
	if effects.follow != nil && r.hooks.FollowFerry != nil {
		next, ok := r.hooks.FollowFerry(division, snapshot.Name, *effects.follow)
		if ok {
			followed = true
			r.mu.Lock()
			if live := r.findAgentLocked(division, agentName); live != nil && !live.RespawnPending && live.RegionID == snapshot.RegionID && live.X == snapshot.X && live.Z == snapshot.Z {
				live.RegionID, live.X, live.Y, live.Z = next.RegionID, next.X, next.Y, next.Z
				live.lastFerry = *effects.follow
				snapshot = *live
			}
			r.mu.Unlock()
		}
	}
	if !followed && effects.teleport != nil {
		r.mu.Lock()
		if live := r.findAgentLocked(division, agentName); live != nil && !live.RespawnPending {
			live.RegionID, live.X, live.Y, live.Z = effects.teleport.RegionID, effects.teleport.X, effects.teleport.Y, effects.teleport.Z
			live.lastMoveAtMs = nowMs
			snapshot = *live
		}
		r.mu.Unlock()
	}
	if decision.Dialogue != "" && r.hooks.OnAgentChat != nil {
		if decision.Dialogue != prevLine || nowMs-prevSpokeAtMs >= 15_000 {
			r.hooks.OnAgentChat(division, snapshot.Name, snapshot.RegionID, decision.Dialogue)
		}
	}
	return decision, &snapshot, true
}

// agentKey names one agent in one division.
func agentKey(division, agentName string) string {
	return strings.TrimSpace(division) + "|" + strings.ToLower(strings.TrimSpace(agentName))
}

func (r *Runtime) applyDecisionLocked(unit *AgentUnit, d Decision, worldThreatActive, mustRejoin, casting bool, nowMs int64) agentEffects {
	effects := agentEffects{}
	defer func() { unit.AvailableForHire = hireable(unit) }()
	unit.Walking = false
	if tp := unit.pendingTeleport; tp != nil {
		unit.pendingTeleport = nil
		unit.place(*tp, nowMs)
		unit.catchingUp, unit.roamAtMs = false, 0
	}
	if unit.PotionCharges < defaultPotionCharges(unit.Role) && unit.HP > 0 {
		// Every agent restocks in town or the Den, so it leaves on every
		// run (own, hired or patrol) with a full supply.
		if _, inTown := r.placeAtLocked(position(unit)); inTown {
			unit.PotionCharges = defaultPotionCharges(unit.Role)
		}
	}
	if unit.HiredBy != "" {
		r.refreshHireLeaderLocked(unit)
		if r.superviseHireLocked(unit, nowMs) && unit.HiredBy != "" {
			// The hirer is away: hold position until it returns or the hire ends.
			return effects
		}
	}
	if d.Action != ActionAttackTarget && !worldThreatActive {
		unit.combatProgressAtMs = 0
	}
	// Seamless 80% HP / Transport HP / MP potion drinking for all AI roles:
	// drinks real potions on cooldown without interrupting combat or movement.
	if r.hooks.UseItem != nil && unit.PotionCharges > 0 && nowMs-unit.LastPotionAtMs >= 1500 {
		switch {
		case unit.MaxHP > 0 && percentOf(unit.HP, unit.MaxHP) <= 80:
			effects.potion = HPPotionItem
		case unit.transportHPPercent > 0 && unit.transportHPPercent <= 80:
			effects.potion = TransportPotionItem
		case unit.needsManaRecovery || (unit.MaxMP > 0 && percentOf(unit.MP, unit.MaxMP) <= 80):
			effects.potion = MPPotionItem
		}
		if effects.potion != "" {
			unit.LastPotionAtMs = nowMs
			unit.PotionCharges--
			unit.needsManaRecovery = false
		}
	}
	if casting {
		unit.combatProgressAtMs = nowMs
		return effects
	}
	switch d.Action {
	case ActionUsePotion:
		needHP := (unit.MaxHP > 0 && percentOf(unit.HP, unit.MaxHP) <= 80) || (unit.transportHPPercent > 0 && unit.transportHPPercent <= 80)
		canUse := unit.PotionCharges > 0
		if canUse && effects.potion == "" && nowMs-unit.LastPotionAtMs >= 1500 {
			if r.hooks.UseItem != nil {
				switch {
				case unit.MaxHP > 0 && percentOf(unit.HP, unit.MaxHP) <= 80:
					effects.potion = HPPotionItem
				case unit.transportHPPercent > 0 && unit.transportHPPercent <= 80:
					effects.potion = TransportPotionItem
				default:
					effects.potion = MPPotionItem
				}
			} else {
				heal := (unit.MaxHP * 35) / 100
				unit.HP += heal
				if unit.HP > unit.MaxHP {
					unit.HP = unit.MaxHP
				}
				effects.heal = heal
			}
			unit.LastPotionAtMs = nowMs
			if unit.Role == RoleThief || needHP {
				unit.PotionCharges--
			}
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
		if worldThreatActive {
			break
		}
		var target Waypoint
		found := false
		if other := r.findAgentLocked(unit.Division, d.TargetName); other != nil {
			if !other.RespawnPending && other.HP > 0 {
				target, found = Waypoint{RegionID: other.RegionID, X: other.X, Y: other.Y, Z: other.Z}, true
			}
		} else if r.hooks.Player != nil {
			if player, ok := r.hooks.Player(unit.Division, d.TargetName); ok && player.HPPercent > 0 {
				target, found = player.At, true
			}
		}
		if !found {
			unit.combatProgressAtMs = 0
			unit.State = StateTraveling
			break
		}
		if found {
			before := Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}
			dist := WaypointDistance(before, target)
			if dist > attackStopDistance(unit) {
				r.moveLocked(unit, target, stepDistanceFor(unit))
			}
			after := Waypoint{RegionID: unit.RegionID, X: unit.X, Z: unit.Z}
			newDist := WaypointDistance(after, target)
			if nowMs > 0 {
				if InCastRange(unit, newDist) || WaypointDistance(before, after) >= 0.5 {
					unit.combatProgressAtMs = nowMs
				} else if unit.combatProgressAtMs == 0 {
					unit.combatProgressAtMs = nowMs
				} else if nowMs-unit.combatProgressAtMs >= combatStallTimeoutMs {
					unit.combatProgressAtMs = 0
					unit.State = StateTraveling
					if unit.EscortingTrader == "" && unit.HiredBy == "" {
						effects.reward = r.advanceAlongRouteLocked(unit, nowMs)
					}
				}
			}
		}
	case ActionReinforceAlly:
		if call := r.activeDistressForLocked(unit.Division, unit, nowMs); call != nil {
			r.stepReinforcementLocked(unit, call)
		}
	case ActionContinueRoute, ActionAssistAlly, ActionRecruitEscort:
		switch {
		case !mustRejoin && unit.lootGID != 0 && nowMs < unit.lootUntilMs:
			// Loot in reach first: a Trader's own dropped goods, a Thief's haul.
			if WaypointDistance(position(unit), unit.lootAt) <= lootPickupReach {
				effects.pickup = unit.lootGID
				unit.lootGID = 0
			} else {
				unit.Walking = false
				r.moveLocked(unit, unit.lootAt, stepDistanceFor(unit))
			}
		case unit.EscortingTrader != "":
			// Escorts, grand-caravan members and hired agents follow their leader.
			unit.State = StateTraveling
			effects.follow = r.stepFollowLocked(unit, nowMs)
		case unit.HiredBy != "":
			// The leading hired Trader drives its hirer's run.
			unit.State = StateTraveling
			r.stepHiredLeadLocked(unit, nowMs)
		case unit.Vengeance != "":
			// A killed Thief leads its gang back after the Trader.
			unit.State = StateTraveling
			r.stepVengeanceLocked(unit, nowMs)
		case unit.State == StateIdleInTown || unit.State == StateRecruiting:
			// Town agents stroll around town until dispatched, hired or called to reinforce.
			r.wanderInTownLocked(unit, nowMs)
		default:
			unit.State = StateTraveling
			effects.reward = r.advanceAlongRouteLocked(unit, nowMs)
		}
	}
	if d.Dialogue != "" {
		unit.LastSpokenLine = d.Dialogue
		unit.LastSpokeAtMs = nowMs
	}
	return effects
}

func (r *Runtime) advanceAlongRouteLocked(unit *AgentUnit, nowMs int64) *EscortContract {
	if r.waitForEscortLocked(unit) {
		unit.lastMoveAtMs, unit.progressAtMs = nowMs, 0
		return nil
	}
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
		if target.FerryTargetID != 0 {
			unit.progressAtMs = 0
			return nil
		}
		// A stuck run skips an off-road bend, and in the end is set onto
		// the waypoint it cannot reach rather than standing forever.
		stalled := unit.noteProgress(nowMs)
		switch {
		case (target.Scenic || (unit.HiredBy != "" && unit.Role == RoleTrader)) && stalled >= stuckSkipMs:
			// A hired Trader never teleports: it tries the next waypoint.
			unit.WaypointIndex++
			unit.progressAtMs = 0
			return nil
		case stalled >= stuckTeleportMs:
			unit.place(target, nowMs)
		default:
			return nil
		}
	}
	unit.progressAtMs = 0
	unit.WaypointIndex++
	if unit.WaypointIndex < len(unit.Route) {
		return nil
	}

	// Arrived at destination terminal: complete delivery & rotate route.
	if unit.HiredBy != "" {
		r.endHireLocked(r.hiresOfLocked(unit.Division, unit.HiredBy), HireCompleted, nowMs)
		return nil
	}
	unit.State = StateDelivering
	if unit.Role == RoleTrader {
		if contract, ok := r.contracts.CompleteAITraderRun(unit.Division, unit.Name, unit.RegionID, nowMs, r.memories); ok {
			completed = &contract
		}
		r.finishRunLocked(unit, nowMs)
		unit.CargoStacks = 10
		unit.State = StateIdleInTown
		unit.TownDepartAtMs = nowMs + TownRestDurationMs
	} else if unit.Role == RoleHunter && unit.EscortingTrader == "" {
		unit.State = StateIdleInTown
		unit.RunKind = RunNone
	}
	unit.OriginTown, unit.DestTown = unit.DestTown, unit.OriginTown
	unit.Route = r.route(unit)
	unit.WaypointIndex = 0
	return completed
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
