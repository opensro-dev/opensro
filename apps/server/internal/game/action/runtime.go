/*
===========================================================================

runtime.go - the action runtime: player commands across every gameplay lane

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/exchange"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/stall"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

/*
==================
Runtime

Runtime coordinates authoritative player actions across inventory, combat,
loot, NPC, quest, progression, and shared division state.

CONCURRENCY: action operations serialize per division, which preserves
ground-item and character transition order without making unrelated shards
wait for one another. The rare all-division TTL sweep owns the maintenance
barrier.
==================
*/
type Runtime struct {
	berserkActors    sync.Map // derived expiry index; character store owns state
	battleActors     sync.Map // battle-state expiry index (battlestate.go)
	aggressionActors sync.Map // scheduled counters; character owns aggression entries
	// jobWeeks holds, per division, the last week JobWeekTick handed to the
	// store (jobrank.go).
	jobWeeks    sync.Map
	BerserkRoll combat.Roll32767
	// WearRoll is CGObjPC_RollEquipmentWear's rand(); nil is the secure roll.
	WearRoll combat.Roll32767
	// RevivalRoll is 51D260's rand() for a fortress revival gate; nil is the
	// secure roll.
	RevivalRoll         combat.Roll32767
	RewardParties       func(division string) []RewardParty
	NextPartyLootMember func(division, name string) uint32
	RewardActorPresent  func(division, name string) bool
	// PetPolicies are the port-only pet rules (companion.PoliciesFromEnv);
	// the zero value is native.
	PetPolicies         companion.Policies
	returnGeneration    atomic.Uint64
	returnCasts         sync.Map // simulation.WorldKey -> pendingReturn; division lock owns changes
	playerDisplacements sync.Map // simulation.WorldKey -> playerDisplacement; a struck player's hold
	jobDresses          sync.Map // simulation.WorldKey -> jobDress (jobdress.go)
	jobActivations      sync.Map // simulation.WorldKey -> int64 end ms (jobdress.go)
	// caravans are the registered trade caravans (caravan.go); caravanMu
	// serializes the registry and caravanTickMs is its last advance.
	caravanMu     sync.Mutex
	caravans      *caravan.Registry
	caravanTickMs int64
	// guildStorageUsers is each guild warehouse's single user
	// (npcguildstorage.go): guildStorageKey -> character name.
	guildStorageUsers sync.Map
	criticals         criticalHistory
	deps              Dependencies
	Ground            *grounditem.Registry
	Pending           *grounditem.PendingTracker
	Worlds            *simulation.WorldStore
	// Steps names TickHook's slow sub-steps (wired to the transport metrics).
	Steps             simulation.StepTimer
	SkillObjects      skillobject.Registry
	CanPlaceQuestTrap func(*enterworld.Character, string) ([]wire.Frame, bool)
	CaptureQuestTrap  func(*enterworld.Character, string, string, func() bool) ([]wire.Frame, bool)

	// effects is the server-owned active character-effect collection behind
	// 0x72CD cancel-active-effect. It stays private so packet handlers cannot
	// invent group identity or client-cancelability; ApplyCharacterEffect is
	// the checked producer boundary.
	effects         *statuseffect.Registry
	periodicEffects linkedpulse.Registry

	// partyAuras are open efr-kind-2 contexts (5830B0). The tick owns joins.
	partyAuras  []partyAura
	partyAuraMu sync.Mutex

	// healsOverTime are the installed timed heals (skillhealtime.go).
	healsOverTime  []healOverTime
	healOverTimeMu sync.Mutex

	// walls are the actors' Force-wall slots (+0xC0C), keyed by wallKey.
	walls  map[string]*standingWall
	wallMu sync.Mutex

	// hawks are the casters' attacking-hawk records (+0xC10), keyed by
	// hawkKey (skillhawk.go).
	hawks  map[string]*summonedHawk
	hawkMu sync.Mutex

	// playerAbnormals owns each character's abnormal-state block.
	playerAbnormals playerAbnormalStore

	// cosAbnormals owns each summoned COS block (CGObjChar on the pet).
	cosAbnormals cosAbnormalStore

	// Selected is the 0x745A object-selection plane and the authority the
	// matching 0x74B3 release consumes (select.go / targetrelease.go).
	Selected *SelectionStore

	// NpcDialogs owns the ephemeral 0x3773 choice session bound to the current
	// selected NPC. Quest hooks are injected by the composition root; action
	// retains selection/liveness and wire ownership.
	NpcDialogs *NpcDialogStore
	NpcQuests  NpcQuestHooks

	// bodyRestores holds the timed body-mode restores (spawnprotection.go).
	bodyRestores *BodyRestoreQueue

	// Monsters is the shared division-keyed population authority. It is nil
	// only when the composition root's explicit monster kill switch is on.
	Monsters *simulation.MonsterState

	// NpcSpawn scopes the 0x745A liveness gate's NPC-gid domain to the
	// same env gates that put the roster on the wire (select.go); tests
	// override it directly.
	NpcSpawn enterworld.NpcSpawnConfig

	// NpcRoster is runtime-owned world policy. Each runtime receives an
	// independent copy so tests and future shard configuration cannot mutate
	// another runtime or bootstrap's object list.
	NpcRoster []simulation.NpcDef
	portals   *portalCatalog
	// gatePulleys maps a gate pulley's RefObjID to its gate's event zone
	// (ConfigurePortals, fortress_gates.go).
	gatePulleys map[uint32]uint32
	// Fortresses is the fortress occupation and war-mode authority
	// (ConfigurePortals installs it from siegefortress.txt).
	Fortresses *fortress.Authority
	// fortressForges are the forge rows by item (fortressForgeRows, set by
	// ConfigurePortals; fortress_production.go prices orders).
	fortressForges map[uint32]fortressForgeRow
	// Unions is the guild union authority the fortress war asks for the
	// holder's allies (fortress_allies.go).
	Unions       *union.Authority
	GuildWars    *guildwar.Authority
	GuildWarKill func(division string, combat domain.GuildWarCombat, nowMs int64)
	// Guilds is the persisted guild topology the fortress official reads
	// (level, members, master).
	Guilds enterworld.GuildStore
	// FortressWindows reports the start of the war running now or next,
	// which the fortress official shows; the fortress-war lane owns the
	// schedule.
	FortressWindows func(nowMs int64) time.Time
	// FortressWarDates reads the previous and current/next war from that lane.
	FortressWarDates func(nowMs int64) (previous, next time.Time)
	// FortressList is the lane's subtype-0 fortress list for a guild,
	// re-sent to the owning guild when its war begins (4E0680).
	FortressList func(guildID int64) []byte
	// fortressPhases are the fortress worlds' scheduled war phases.
	fortressPhasesMu sync.Mutex
	fortressPhases   []fortressPhase
	// structureDeaths are the fortress structures killed since the last
	// tick (fortress_capture.go).
	structureDeathsMu sync.Mutex
	structureDeaths   []structureDeath
	// FortressStore keeps the fortress structures' rows (fortress_persist.go).
	FortressStore     enterworld.FortressStore
	fortressPersistMu sync.Mutex
	fortressPersist   fortressPersistence

	// GachaCatalog is the strict v1.150 gachaitemset/gachanpcmap authority.
	// The composition root installs it before Register admits 0x7338/0x7053.
	GachaCatalog  *gacha.Catalog
	Alchemy       *alchemy.Catalog
	AlchemyRoll   alchemy.Roll
	compoundMu    sync.Mutex
	compoundJobs  map[compoundKey]compoundJob
	Commerce      *commerce.Catalog
	mallCatalog   *commerce.MallCatalog
	mallAuthority domain.MallAuthority
	// storageAuthority owns the account warehouse (storage.go).
	storageAuthority domain.StorageAuthority
	petMu            sync.Mutex
	petSessions      map[petOwnerKey]*petSession

	// Admission precedes game-ready/pet binding; teardown follows this owner.
	characterAdmissions sync.Map // simulation.WorldKey -> populationAdmission
	recoveryMu          sync.Mutex
	recoverySessions    map[recoveryKey]*recoverySession
	petSkillWindows     petSkillWindowIndex
	paramJobOwners      petSkillWindowIndex
	// premiumSpend is the online premium clocks' uncommitted spend
	// (premiumclock.go).
	premiumSpend premiumSpendLedger
	// pkOwners are the players whose PK record runs a clock (pkrecord.go).
	pkOwners petSkillWindowIndex
	// pulseAreas are the owners of live pulse areas (skillpulsearea.go).
	pulseAreas            pulseAreaClock
	commerceReferenceSeed []wire.Frame

	// UnlimitedItems names the item codenames whose use is never spent (the
	// operator's beta starter kit, SRO_BETA_STARTER_KIT). Set once at wiring.
	UnlimitedItems map[string]bool

	// MoveCOS delegates mounted movement to the sole movement/collision owner.
	MoveCOS func(string, *enterworld.Character, uint32, []byte) []wire.Frame
	// MoveCOSPublished orders native mounted movement acknowledgement with
	// its terminal collision event under the movement owner's operation lock.
	MoveCOSPublished func(string, *enterworld.Character, wire.CosCommand, func([]wire.Frame))
	// SteerCOS and StopCOS delegate the vehicle's 0x769E steer (tag 0x04)
	// and direction stop (tag 0x03) to the same owner. They return the
	// acting session's frames and the observers' frames.
	SteerCOS func(string, *enterworld.Character, uint32, uint16) ([]wire.Frame, []wire.Frame)
	StopCOS  func(string, *enterworld.Character, uint32, uint16) ([]wire.Frame, []wire.Frame)

	// GachaRoll returns a uniform value in [0,10000). Tests replace it;
	// production uses crypto/rand so concurrent rolls share no mutable PRNG.
	GachaRoll func() (uint32, error)

	// CombatRoll is the v1.188 CFormulae rand() domain. Formula tests inject
	// exact sequences; production uses an independent cryptographic draw.
	CombatRoll combat.Roll32767

	// CompanionRoll supplies the shared native spawn-position sampler without
	// consuming a combat or loot test sequence during actor admission.
	CompanionRoll           combat.Roll32767
	SpawnRegionAvailable    func(uint16) bool
	ConstrainCompanionSpawn func(simulation.Spawn, simulation.Spawn) simulation.Spawn
	CompanionSurfaceHeight  func(uint16, float64, float64, float64) (float64, bool)

	// DropRoll is independent from combat formula randomness. Reference-drop
	// generation and the post-generation player/level admission gate consume
	// this native rand() domain in order.
	DropRoll combat.Roll32767

	// CaravanRoll is the caravan owner's rand() domain: spawn timers and
	// every bandit draw (60BF30), in native order.
	CaravanRoll combat.Roll32767

	// DropPassRate multiplies every drop a kill makes: its unique prepass,
	// its assigned rewards, its ordinary passes (gold, equipment and
	// consumable rolls) and the capacity bounding them. 0 or 1 is native;
	// the closed-beta growth switch raises it (port-only, not native).
	DropPassRate int

	// DropCap bounds the ordinary items one kill leaves once DropPassRate has
	// grown its capacity: a uniform random subset survives. Gold heaps and a
	// unique's prepass are never cut. 0 leaves every planned item (port-only,
	// not native; the closed-beta growth switch sets it).
	DropCap int

	// GoldRate multiplies every monster gold heap after the native rarity
	// multipliers. 0 or 1 is native; the closed-beta growth switch raises it.
	GoldRate int

	// RareRate multiplies the chance that an equipment drop is rare (SoX).
	// 0 or 1 is native; the closed-beta growth switch raises it.
	RareRate int

	// PartyShareFloor raises every party member's EXP share to at least an
	// even split (partyRewardFactors). Off is native; the closed-beta growth
	// switch turns it on.
	PartyShareFloor bool

	// Now abstracts the clock for deterministic tests.
	Now         func() time.Time
	departureMu sync.Mutex
	departures  map[uint64]pendingDeparture

	// UpdateQuestInventory is the quest lane's collect-objective updater. It
	// runs inside the item authority transaction so inventory and derived
	// quest progress cannot tear across a crash.
	UpdateQuestInventory func(character *enterworld.Character) ([]wire.Frame, bool)
	AdvanceQuestMinute   func(character *enterworld.Character) []wire.Frame
	AdvanceQuestItem     func(*enterworld.Character, int64) []wire.Frame
	ForgetQuestItem      func(*enterworld.Character)
	UseQuestItem         func(*enterworld.Character, string, simulation.Spawn, int64) ([]wire.Frame, bool)
	// CapturedFollowerDied tells a capture-escort quest its captured monster
	// died (CGObjCOS_Captured); called inside the owner's character door.
	CapturedFollowerDied        func(*enterworld.Character) []wire.Frame
	AdvanceQuestCalendar        func(nowMs int64)
	ReleaseQuestCapturesOnDeath func(*enterworld.Character) ([]wire.Frame, bool)
	QuestMonsterDrops           func(*enterworld.Character, string, uint8, func() (uint32, error)) []inventory.ItemAmount
	QuestTravelBlocks           func(*enterworld.Character) uint32
	UpdateQuestKill             func(
		character *enterworld.Character,
		monsterCodename string,
		rarity uint8,
	) ([]wire.Frame, bool)

	// FortressGuildRole supplies the optional fortress membership role used
	// by siege-weapon equip requirements. Nil means no member record.
	FortressGuildRole FortressGuildRoleResolver

	// ConstrainMovement applies the shared server-authoritative geometry
	// policy to system-generated movement. A nil seam is supported only by
	// isolated unit tests; the composition root always wires it.
	ConstrainMovement func(
		characterName string,
		from, to simulation.Spawn,
	) (simulation.Spawn, *simulation.MoveError)

	// PlanCompanionRoute routes a pet around what its straight path cannot
	// cross: the monster AI's detour (movement.PlanMonsterRoute). Nil keeps
	// pets on their straight, clipped segment.
	PlanCompanionRoute simulation.PetRoutePlanner

	// ConstrainWalk is ConstrainMovement for a character walking from its
	// retained surface owner (movement.Runtime.ConstrainMovementFrom). Every
	// server-driven character move must use it and commit the returned walk,
	// or the next move re-guesses the surface from a quantized height.
	ConstrainWalk func(
		characterName string,
		from simulation.Spawn,
		fromOwner simulation.NavOwner,
		to simulation.Spawn,
	) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError)
	// AdmitGroundWalk retains an ordinary movement goal for runtime collision
	// stepping. Authored displacements keep the complete ConstrainWalk query.
	AdmitGroundWalk func(
		characterName string,
		from simulation.Spawn,
		fromOwner simulation.NavOwner,
		to simulation.Spawn,
	) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError)

	// ResolveNavOwner re-seats a point on a surface: the hinted owner (or an
	// edge neighbour) when it still contains the point, else the native
	// teleport rule. It returns that surface's height at the point.
	ResolveNavOwner func(p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool)

	// LineOfSight is the region manager's clear-line test that skill
	// admission phase 0x40 asks for each target (movement.Runtime.
	// LineOfSight). Nil admits every line; the composition root wires it.
	LineOfSight func(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) bool

	// ProposalPending reports an unanswered 0x3393 prompt of any lane for
	// a character. The native transaction manager holds one per player
	// (TransactionMgr_InsertUnique 46F420), so a second proposal is dropped.
	ProposalPending func(divisionID, characterName string) bool

	// UpdateExperience is the stat authority's door-free updater. The fatal-hit
	// path invokes it synchronously in its own character progression door; the
	// monster registry is ephemeral and owns no durable transaction. Nil keeps
	// detached unit runtimes fail-closed on rewards.
	UpdateExperience func(
		character *enterworld.Character,
		expDelta, skillExpDelta int64,
		sourceGid uint32,
	) ([]wire.Frame, bool)

	// RefundExperience is the stat authority's door-free refund of EXP lost
	// at death (a resurrection's share). Unlike UpdateExperience it is not a
	// gain, so the growth rates never scale it. Nil grants no refund.
	RefundExperience func(character *enterworld.Character, exp int64) ([]wire.Frame, bool)

	// RecallStatPoints is the stat authority's door-free stat point recall
	// (progression/statrecall.go): STR and INT back to their automatic values
	// and the spent points back to the pool. False changes nothing. Nil
	// refuses the recall scroll.
	RecallStatPoints func(character *enterworld.Character) ([]wire.Frame, bool)

	// ApplyDeathPenalty is progression's door-free death updater: the EXP
	// and SP a death costs (pkdeath.go resolves which). Combat invokes it
	// from inside the fatal-HP character transaction.
	ApplyDeathPenalty func(character *enterworld.Character, penalty pk.DeathPenalty) ([]wire.Frame, bool)

	// UpdateJobExperience is progression's door-free job EXP updater
	// (CGObjPC_AddJobExp 4E2830): a job kill's share (pkreward.go). Nil pays
	// no job EXP.
	UpdateJobExperience func(character *enterworld.Character, delta int64) ([]wire.Frame, bool)

	// PushCharacterFrames delivers the actor's complete ordered progression
	// burst after the authority door closes. Pending pickups use captured
	// delivery when configured; detached runtimes retain this actor fallback.
	PushCharacterFrames func(divisionID, characterName string, frames []wire.Frame)

	// CaptureCharacterFrames snapshots recipient sessions and their scene
	// revisions under the division lock, after the character door closes.
	// Capture must not send. Invoke the returned handle only after releasing
	// the division lock; a nil handle means no recipient was captured.
	CaptureCharacterFrames func(divisionID, characterName string) func([]wire.Frame)

	// PushDivisionPeerFrames delivers public presentation frames to every
	// same-division session except the acting character: pickup animation/world
	// changes and the gid-only level-up effect. The actor receives its full
	// ordered burst through PushCharacterFrames, so exclusion prevents dupes.
	PushDivisionPeerFrames func(divisionID, exceptCharacterName string, frames []wire.Frame)

	// PushShardPeerFrames delivers frames to every character session in the
	// division except the named character's, whether or not they observe it:
	// the Global Chatting line (globalchat.go). Nil drops the broadcast.
	PushShardPeerFrames func(divisionID, exceptCharacterName string, frames []wire.Frame)

	// ChatRestricted reports whether the character's session holds the GM
	// chat restriction, sending its notice when it does (4F0A00). Nil means
	// no session can be restricted.
	ChatRestricted func(divisionID, characterName string) bool

	// PushMonsterCast publishes prepared-cast results before the division
	// transaction ends. It must only enqueue (never perform network I/O).
	// Detached runtimes leave it nil and inspect returned frames instead.
	// Each result.Private entry names the character its frames go to.
	PushMonsterCast func(
		divisionID string,
		sourceGID uint32,
		result simulation.MonsterAttackResult,
	)

	// CanPickupOwnedDrop owns party item-sharing policy. Self-ownership is
	// checked locally; this seam answers only whether ownerJID is a member of
	// the actor's item-sharing party.
	CanPickupOwnedDrop func(divisionID, characterName string, ownerJID uint32) bool

	maintenance sync.RWMutex
	operations  divisionOperationLocks

	// castTokenCounter mints the per-cast 0xB245 instance token before the
	// item-operation lane. First token is 1: a live bracket never carries 0.
	castTokenCounter uint32
	// huntingReports is the last position each hunt link reported (huntingpoint.go).
	huntingReports huntingReports

	// pendingSkillFinalizes is the server-owned close side of accepted
	// 0xB245 brackets. The simulation tick drains due B505 frames by division;
	// this keeps timing on the one authoritative push clock and avoids a
	// goroutine/timer per cast.
	pendingSkillFinalizesMu sync.Mutex
	pendingSkillFinalizes   []pendingSkillFinalize
	pendingMonsterCasts     []pendingMonsterCast           // protected by pendingSkillFinalizesMu
	pendingProjectileCasts  []pendingProjectileCast        // protected by pendingSkillFinalizesMu
	currentSkillCommands    map[string]currentSkillCommand // same mutex; survives dequeue until release processing ends

	// pendingMonsterDefeats holds zero-HP reward sources through the authored
	// death clip. Their movers are inert, but their client animation state must
	// remain registry-live until event 0x64 launches the staged absorption VFX.
	pendingMonsterDefeatsMu sync.Mutex
	pendingMonsterDefeats   []pendingMonsterDefeat

	// monsterLegRecipients holds the private reward frames of kills made
	// inside a monster's attack (a Temptation fight, a returned hit) until
	// the action tick delivers them; the monster leg that commits the kill
	// publishes only to viewers and its target (creditedhit.go).
	monsterLegRecipientsMu sync.Mutex
	monsterLegRecipients   []simulation.DivisionFrames

	// basicAttackIntents is the server-owned continuation behind native
	// 0x72CD [01 01 01 gid]/[01 03 01 gid]. One intent per character replaces
	// timer arithmetic with explicit approach/strike/cancel states advanced by
	// the simulation tick.
	basicAttackIntentsMu sync.Mutex
	basicAttackIntents   map[string]basicAttackIntent
	// actionSessions owns the pending back command and private B2CD count.
	// Existing continuation and cast owners execute and commit gameplay.
	groundApproachStops sync.Map // motion revision terminal events; action tick owns retirement
	actionSessions      sync.Map

	// Exchanges holds the open player-to-player exchanges and their
	// requests (exchange.go).
	Exchanges *exchange.Registry
	// Stalls holds the street stalls and their visitors (stall.go), and
	// StallCategories the stall network's TypeID -> category table
	// (stall_network.go, ConfigureStallNetwork).
	Stalls          *stall.Registry
	StallCategories map[[4]int64]uint32
	// resurrections holds the unanswered resurrection proposals, one per
	// dead player (resurrection.go).
	resurrections resurrectionOffers

	// nextSweepAtMs rate-limits the TTL sweep to grounditem.SweepInterval:
	// the simulation tick calls the hook every 100ms, but the peek+sweep only
	// run on the declared 5s cadence.
	nextSweepAtMs atomic.Int64
}

/*
==================
NewRuntime

NewRuntime assembles a Runtime over the authoritative character and item
sources. The character source must be the same authority enter-world uses,
so both lanes resolve the same *Character pointers.
==================
*/
func NewRuntime(deps Dependencies, monsters *simulation.MonsterState) *Runtime {
	npcSpawns := enterworld.NpcSpawnConfig{}
	if source, ok := deps.(interface {
		NpcSpawnPolicy() enterworld.NpcSpawnConfig
	}); ok {
		npcSpawns = source.NpcSpawnPolicy()
	}

	rt := &Runtime{
		deps:               deps,
		Ground:             grounditem.NewRegistry(),
		Pending:            grounditem.NewPendingTracker(),
		Worlds:             simulation.NewWorldStore(),
		effects:            statuseffect.NewRegistry(),
		Selected:           NewSelectionStore(),
		NpcDialogs:         NewNpcDialogStore(),
		bodyRestores:       NewBodyRestoreQueue(),
		Monsters:           monsters,
		NpcSpawn:           npcSpawns,
		NpcRoster:          append([]simulation.NpcDef(nil), npcSpawns.Roster...),
		GachaRoll:          secureGachaRoll,
		AlchemyRoll:        secureAlchemyRoll,
		CombatRoll:         combat.SecureRoll32767,
		DropRoll:           combat.SecureRoll32767,
		CaravanRoll:        combat.SecureRoll32767,
		caravans:           caravan.NewRegistry(),
		Now:                time.Now,
		basicAttackIntents: make(map[string]basicAttackIntent),
		resurrections:      resurrectionOffers{byTarget: make(map[string]resurrectionOffer)},
		Exchanges:          exchange.New(),
		Stalls:             stall.New(),
	}

	if monsters != nil {
		monsters.SetAbnormalContext(monsterAbnormalContext{rt})
	}

	return rt
}

/*
==================
PendingPickup

PendingPickup describes an approach still in flight. No frames ride a
duplicate request; the simulation tick owns arrival and grant completion.
==================
*/
type PendingPickup struct {
	ItemGid uint32
	Eta     time.Duration
}

/*
==================
OpResult

OpResult is one applied operation: Frames is the acting session's complete
synchronous burst and Broadcast is the public peer projection. ActorPrivate
is the actor-only tail needed when the SAME operation is produced by the
simulation tick instead of a request handler: the tick publishes Broadcast to
everyone first, then routes ActorPrivate to the actor in the same turn.
Frames already includes both portions; request handlers must never send
ActorPrivate separately. Pending marks an approach when nothing rides the
wire at all.
==================
*/
type OpResult struct {
	Recipients   []RecipientFrames
	Frames       []wire.Frame
	Broadcast    []wire.Frame
	ActorPrivate []wire.Frame
	Pending      *PendingPickup

	// DiagnosticRefusal is an internal, non-wire reason for a silent native
	// refusal. The hub logs it at debug level; protocol behavior stays exact.
	DiagnosticRefusal string
}

/*
==================
failureResult
==================
*/
func failureResult(errorCode uint8) OpResult {
	return OpResult{
		Frames: []wire.Frame{
			{
				Opcode:  wire.OpItemMoveResponse,
				Payload: wire.EncodeItemMoveError(errorCode),
			},
		},
	}
}

// ---- Character row bridging ----

/*
==================
invItemsFromBag

invItemsFromBag arms a character's persisted rows, sockets and bag up to
its capacity byte, for the inventory engine: the decimal-string variance
becomes a real u64 and a zero stack reads as one unit, both exactly like
the fixture's wire re-arming.
==================
*/
func invItemsFromBag(c *enterworld.Character) []inventory.Item {
	return invItemsFromRowsWithin(c.MissionInventory, int64(inventory.BagEnd(c)))
}

/*
==================
bagOf

The character's inventory: its rows inside a bag of its own capacity.
==================
*/
func bagOf(c *enterworld.Character) *inventory.Inventory {
	return inventory.New(invItemsFromBag(c), inventory.BagEnd(c))
}

/*
==================
invItemsFromRowsWithin
==================
*/
func invItemsFromRowsWithin(rows []enterworld.InventoryRow, slotEnd int64) []inventory.Item {
	out := make([]inventory.Item, 0, len(rows))

	for _, row := range rows {
		if row.Slot < 0 || row.Slot >= slotEnd {
			continue
		}

		variance, err := strconv.ParseUint(row.VarianceBits, 10, 64)
		if err != nil {
			variance = 0
		}

		quantity := row.StackCount
		if quantity < 1 {
			quantity = 1
		}

		if quantity > 0xFFFF {
			quantity = 0xFFFF
		}

		out = append(out, inventory.Item{
			RecordID:          row.RecordID,
			TradeOwner:        row.TradeOwner,
			Slot:              uint8(row.Slot),
			RefObjID:          row.RefObjID,
			Codename:          row.Codename,
			TypeFlags:         row.TypeFlags,
			Plus:              uint8(clampInt64(row.Plus, 0, 0xFF)),
			VarianceBits:      variance,
			Durability:        uint32(clampInt64(row.Durability, 0, 0xFFFFFFFF)),
			Quantity:          uint16(quantity),
			MagicOptions:      append([]uint64(nil), row.MagicOptions...),
			TransformRefObjID: row.TransformRefObjID, Summon: domain.CloneCOS(row.Summon),
		})
	}

	return out
}

/*
==================
rowsFromInvItems

rowsFromInvItems persists the engine rows back onto the character record
(variance back to its decimal-string form).
==================
*/
func rowsFromInvItems(items []inventory.Item) []enterworld.InventoryRow {
	out := make([]enterworld.InventoryRow, 0, len(items))

	for _, item := range items {
		out = append(out, enterworld.InventoryRow{
			RecordID:          item.RecordID,
			TradeOwner:        item.TradeOwner,
			Slot:              int64(item.Slot),
			RefObjID:          item.RefObjID,
			Codename:          item.Codename,
			TypeFlags:         item.TypeFlags,
			Plus:              int64(item.Plus),
			VarianceBits:      strconv.FormatUint(item.VarianceBits, 10),
			Durability:        int64(item.Durability),
			StackCount:        int64(item.Quantity),
			MagicOptions:      append([]uint64(nil), item.MagicOptions...),
			TransformRefObjID: item.TransformRefObjID, Summon: domain.CloneCOS(item.Summon),
		})
	}

	return out
}

/*
==================
clampInt64
==================
*/
func clampInt64(value, min, max int64) int64 {
	if value < min {
		return min
	}

	if value > max {
		return max
	}

	return value
}

/*
==================
maxStackFor

maxStackFor ports missionItemMaxStack: the itemdata MaxStack column, gated
on the stackable-ETC class. A nil item source behaves like a missing row -
cap 1, the swap leg - which is the documented dev-mode degradation until
the itemdata loader lands.
==================
*/
func (rt *Runtime) maxStackFor(typeFlags uint16, codename string) uint16 {
	if !inventory.IsEtcStackableTypeFlags(typeFlags) {
		return 1
	}

	if rt.deps.ItemReferences() == nil {
		return 1
	}

	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(codename)
	if !ok || ref == nil || len(ref.NativeFields) == 0 {
		return 1
	}

	maxStack, ok := ref.NativeFields.Lookup("maxStack")
	if !ok || math.IsNaN(maxStack) || math.IsInf(maxStack, 0) {
		return 1
	}

	return uint16(clampInt64(int64(maxStack), 1, 0xFFFF))
}

/*
==================
goldOf
==================
*/
func goldOf(character *enterworld.Character) uint64 {
	if character == nil || character.Gold == nil || *character.Gold < 0 {
		return 0
	}

	return uint64(*character.Gold)
}

/*
==================
setGold
==================
*/
func setGold(character *enterworld.Character, balance uint64) {
	value := int64(math.MaxInt64)
	if balance <= uint64(math.MaxInt64) {
		value = int64(balance)
	}

	character.Gold = &value
}

/*
==================
characterSnapshot

characterSnapshot copies mutable character state while the authority read
door is held. Identity fields (ID and Name) never change after creation,
but inventory, progression, deletion state, and world state must never be
read through the live pointer outside a door.
==================
*/
func (rt *Runtime) characterSnapshot(
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if character == nil {
		return nil
	}

	var snapshot *enterworld.Character

	rt.deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})

	return snapshot
}

/*
==================
writeBackWorld

writeBackWorld persists the accepted ground pose onto the character record.
Authored displacement state keeps its existing destination semantics; the
ordinary ground mover never persists a destination it has not reached.

The write-back owns ONLY spawn/movementMode/spawnSet; every other world
field (dungeonMinimap, movementSourceSeeded, updatedAt, moveSegment echo,
and any future record keys) copies through untouched, like the Node
handlers' {...world} spread (item-lane mirror of SCOUT-B FINDING 500).
Copy-then-swap so aliases of the old record (the character snapshot's
shallow copy) stay unchanged.
==================
*/
func writeBackWorld(character *enterworld.Character, state simulation.WorldState) {
	spawn := state.PersistedSpawn()
	regionID := int64(spawn.RegionID)
	x, y, z := spawn.X, spawn.Y, spawn.Z
	angle := int64(spawn.Angle)
	mode := int64(state.MovementMode)
	next := enterworld.CharacterWorld{}
	if character.World != nil {
		next = *character.World
	}

	next.Spawn = &enterworld.WorldSpawn{
		RegionID: &regionID,
		X:        &x,
		Y:        &y,
		Z:        &z,
		Angle:    &angle,
	}

	next.MovementMode = &mode
	next.SpawnSet = state.SpawnSet
	character.World = &next
}

/*
==================
liveSpawn

liveSpawn samples the character's LIVE position - the bug-D plane. Every
position-dependent op reads THIS, never the goal in WorldState.Spawn.
==================
*/
func (rt *Runtime) liveSpawn(
	worldKey string,
	character *enterworld.Character,
	nowMs int64,
) simulation.Spawn {
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})

	return snapshot.LiveSpawnAt(nowMs)
}

/*
==================
LiveSpawnFor

The character's live position now, for lanes outside action that gate on
native range (the party invite's CGObjChar_CheckHitRange).
==================
*/
func (rt *Runtime) LiveSpawnFor(divisionID string, character *enterworld.Character) simulation.Spawn {
	return rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli())
}

/*
==================
liveNav

liveNav is liveSpawn plus the surface owner retained at that point.
==================
*/
func (rt *Runtime) liveNav(
	worldKey string,
	character *enterworld.Character,
	nowMs int64,
) (simulation.Spawn, simulation.NavOwner) {
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})

	return snapshot.LiveSpawnAt(nowMs), snapshot.LiveOwnerAt(nowMs)
}

/*
==================
constrainWalk

constrainWalk routes a character move through the owner-aware constraint,
degrading to the owner-less one only in compositions that lack it.
==================
*/
func (rt *Runtime) constrainWalk(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if rt.ConstrainWalk != nil {
		return rt.ConstrainWalk(characterName, from, fromOwner, to)
	}

	if rt.ConstrainMovement != nil {
		committed, refusal := rt.ConstrainMovement(characterName, from, to)
		return committed, simulation.NavWalk{}, refusal
	}

	return to, simulation.NavWalk{}, nil
}

/*
==================
admitGroundWalk

Player approaches retain their intended goal while the world owner checks
each elapsed movement step. A skill displacement still resolves its whole
authored chord through constrainWalk before it commits.
==================
*/
func (rt *Runtime) admitGroundWalk(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if rt.AdmitGroundWalk != nil {
		return rt.AdmitGroundWalk(characterName, from, fromOwner, to)
	}
	return rt.constrainWalk(characterName, from, fromOwner, to)
}

/*
==================
samePlacement

samePlacement compares where a move lands. Walked goals stand on their
surface's height, so the requested int16 Y is not part of the comparison.
==================
*/
func samePlacement(a, b simulation.Spawn) bool {
	return a.RegionID == b.RegionID && a.X == b.X && a.Z == b.Z
}

// ---- 0x72CD pickup ----

/*
==================
pickupRefusal
==================
*/
func pickupRefusal(errorCode uint8) OpResult {
	return OpResult{Frames: wire.PickupRefusalFrames(errorCode)}
}
