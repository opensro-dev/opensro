/*
===========================================================================

runtime.go - the action runtime: player commands across every gameplay lane

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/world/calendar"
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
	"opensro.online/server/internal/game/namefilter"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/optionalteleport"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
	"opensro.online/server/internal/game/world/townarea"
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
	cosRefusalLog    sync.Map // cosground.go: cosRefusalKey -> *cosRefusalWindow (refusal log rollup)
	aggressionActors sync.Map // scheduled counters; character owns aggression entries
	// instanceWorlds tracks the instanced layers opened on demand
	// (instance_world.go): the owning lease and deadline per layer.
	instanceWorlds sync.Map // instanceWorldKey -> trackedInstanceWorld
	// questGates holds the quest instances' standing exit gates
	// (questgate.go).
	questGates questGateBook
	// jobWeeks holds, per division, the last week JobWeekTick handed to the
	// store (jobrank.go).
	jobWeeks sync.Map
	// gmSpeeds holds each GM's /SETSPEED percent (gmspeed.go), session only.
	gmSpeeds sync.Map
	// GMSetSpeed admits /SETSPEED (port-only, SRO_GM_SETSPEED; off is native).
	GMSetSpeed bool
	// FlagWar is the Capture the Flag schedule (native; flagwar.go):
	// SRO_FLAGWAR_START_UTC and SRO_FLAGWAR_MIN_PLAYERS, wired from env.
	FlagWar FlagWarSettings
	// FlagWarMatchStart is the Capture the Flag match owner's entry
	// point, installed by NewRuntime (flagwar_match.go).
	FlagWarMatchStart func(registrants []FlagWarRegistrant) []simulation.DivisionFrames
	flagWar           flagWarEvent
	// flagWarLive is the live Capture the Flag match book, keyed by
	// division (flagwar_match.go).
	flagWarLive flagWarMatches
	// TeamDeathTownRule sends a team death to the town rule (port-only,
	// SRO_TEAM_DEATH_REVIVE off; the zero value is native, teamdeath.go).
	TeamDeathTownRule bool
	// CaravanBanditCap caps the bandits per ambush (port-only,
	// SRO_CARAVAN_BANDIT_CAP; 0 is native, uncapped).
	CaravanBanditCap int
	// CaravanStarAmbush replaces an ambush's count with the fixed per-star
	// table (port-only, SRO_CARAVAN_STAR_AMBUSH; false is native).
	CaravanStarAmbush bool
	// TransportFollow has a dismounted trade transport follow its trader
	// instead of tethering it (port-only, SRO_TRANSPORT_FOLLOW; false is
	// native, transportfollow.go).
	TransportFollow bool
	// TransportFollowTether keeps the 100 m tether on a following
	// transport (port-only, SRO_TRANSPORT_FOLLOW_TETHER; acts only with
	// TransportFollow on, transportfollow.go).
	TransportFollowTether bool
	// JobGoodsTeleport lets a trader with a transport out use town gates,
	// return scrolls and the reverse return (port-only,
	// SRO_JOB_GOODS_TELEPORT; false is native, goodsteleport.go).
	JobGoodsTeleport bool
	// ThiefCartTownAreas, when set, also stops a thief's transport at the
	// walls of towns that run into battlefield regions (port-only,
	// SRO_THIEF_CART_TOWN_AREAS; nil is native, jobdress.go).
	ThiefCartTownAreas *townarea.Areas
	BerserkRoll        combat.Roll32767
	// WearRoll is the equipment wear roll's rand(); nil is the secure roll.
	WearRoll combat.Roll32767
	// RevivalRoll is the native teleport lookup's rand() for a fortress revival gate; nil is the
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
	teleportArrivals    sync.Map // simulation.WorldKey -> struct{}: arrivals awaiting game-ready (teleportprotection.go)
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
	// storageSessions is each character's open warehouse (storage.go):
	// world key -> the NPC, the character's own gid for a ticket, or the
	// guild manager. Memory only, as native's pc+0x1D88 record is.
	storageSessions sync.Map
	criticals       criticalHistory
	// groundDrops is the per-tick ground pass's placed-drop cache
	// (ground_scope.go).
	groundDrops groundDropCache
	// staticRefs is the published static reference set
	// (ground_references.go).
	staticRefs staticRefCache
	deps       Dependencies
	Ground     *grounditem.Registry
	Pending    *grounditem.PendingTracker
	Worlds     *simulation.WorldStore
	// Steps names TickHook's slow sub-steps (wired to the transport metrics).
	Steps simulation.StepTimer
	// PublicationHold receives publication-gate holds at or above the
	// slow threshold, labelled by their holder (wired to the transport
	// metrics, transport.RecordPublicationHold).
	PublicationHold   func(label string, elapsed time.Duration)
	SkillObjects      skillobject.Registry
	CanPlaceQuestTrap func(*enterworld.Character, string) ([]wire.Frame, bool)
	CaptureQuestTrap  func(*enterworld.Character, string, string, func() bool) ([]wire.Frame, bool)

	// effects is the server-owned active character-effect collection behind
	// 0x72CD cancel-active-effect. It stays private so packet handlers cannot
	// invent group identity or client-cancelability; ApplyCharacterEffect is
	// the checked producer boundary.
	effects         *statuseffect.Registry
	periodicEffects linkedpulse.Registry

	// partyAuras are open efr-kind-2 contexts. The tick owns joins.
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

	// cosAbnormals owns each summoned COS block (the character object on the pet).
	cosAbnormals cosAbnormalStore

	// cosDebuffs owns each summoned COS's hostile debuff words
	// (cosdebuff.go): runtime state only, keyed as the blocks are.
	cosDebuffs cosDebuffStore

	// Selected is the 0x745A object-selection plane and the authority the
	// matching 0x74B3 release consumes (select.go / targetrelease.go).
	Selected *SelectionStore

	// NpcDialogs owns the ephemeral 0x3773 choice session bound to the current
	// selected NPC. Quest hooks are injected by the composition root; action
	// retains selection/liveness and wire ownership.
	NpcDialogs *NpcDialogStore
	NpcQuests  NpcQuestHooks

	// GMQuests are the quest runtime's GM tools (gmquest.go), injected by the
	// composition root like NpcQuests.
	GMQuests GMQuestHooks

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
	// reverseMapPoints is the reverse return scroll's map table, empty
	// unless SRO_REVERSE_RETURN_MAP is on (ConfigureReverseReturnMap).
	reverseMapPoints []enterworld.ReverseMapPoint
	// reverseMapLevels holds an imported table's points by public id, for their
	// level bands; nil for the inferred table.
	reverseMapLevels map[uint32]optionalteleport.Point
	// gatePulleys maps a gate pulley's RefObjID to its gate's event zone
	// (ConfigurePortals, fortress_gates.go).
	gatePulleys map[uint32]uint32
	// structureUpgrades is the siege structure upgrade table by codename
	// (ConfigurePortals, fortress_upgrade.go).
	structureUpgrades structureUpgradeTable
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
	// re-sent to the owning guild when its war begins.
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
	// fortressRoster times the structure rosters (fortress_roster.go).
	fortressRosterMu sync.Mutex
	fortressRoster   fortressRosterClock

	// GachaCatalog is the strict v1.150 gachaitemset/gachanpcmap authority.
	// The composition root installs it before Register admits 0x7338/0x7053.
	GachaCatalog *gacha.Catalog
	// GachaPoolV2 says the catalog carries the port-only prize pool
	// (EnvGachaPoolV2); rolls are logged so the pool's supply is visible.
	GachaPoolV2   bool
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
	// petDiag throttles the attack pet fight log (petdiagnostics.go).
	petDiag petDiagnostics
	// placements counts non-walk placements inside a closed object
	// (placementguard.go).
	placements  placementGuard
	petSessions map[petOwnerKey]*petSession
	// cosOwners is the pet gid to owner index (cosownerindex.go):
	// whose pet a gid is, without walking the division's characters.
	cosOwners  cosOwnerIndex
	petShields petShieldStore
	wolfHowls  wolfHowlStore
	tailwinds  tailwindStore

	// Admission precedes game-ready/pet binding; teardown follows this owner.
	characterAdmissions sync.Map // simulation.WorldKey -> populationAdmission
	// walkInZones is the walk-in zone each admitted player stood in at the
	// last tick (walkin.go): only entering one teleports.
	walkInZones      sync.Map // simulation.WorldKey -> zone codename
	recoveryMu       sync.Mutex
	recoverySessions map[recoveryKey]*recoverySession
	petSkillWindows  petSkillWindowIndex
	paramJobOwners   petSkillWindowIndex
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

	// combatCopy, when set, replaces the combat roots' narrow copy
	// (characterSnapshotCombat); only the combat-copy differential test
	// sets it, to the full copy. Nil is the narrow copy.
	combatCopy func(divisionID string, character *enterworld.Character) *enterworld.Character
	// companionCopy, when set, replaces the pet pass's narrow copy
	// (characterSnapshotCompanionAdmitted); only the pet-copy
	// differential test sets it, to the full copy. Nil is the narrow
	// copy.
	companionCopy func(divisionID string, character *enterworld.Character) *enterworld.Character
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

	// CombatRoll is the rand() domain of the reference server's damage
	// formulas. Formula tests inject exact sequences; production uses an
	// independent cryptographic draw.
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
	// every bandit draw, in native order.
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

	// DropMix gives named item families their own beta drop rate in place
	// of DropPassRate; nil runs every family at DropPassRate (port-only,
	// not native; progression/dropmix.go).
	DropMix progression.DropMix

	// GoldRate multiplies every monster gold heap after the native rarity
	// multipliers. 0 or 1 is native; the closed-beta growth switch raises it.
	GoldRate int

	// RareRate multiplies the chance that an equipment drop is rare (SoX).
	// 0 or 1 is native; the closed-beta growth switch raises it.
	RareRate int

	// SealRates multiplies each rare seal's chance by degree and seal on top
	// of RareRate (port-only, not native, beta balance; owner 2026-10-13).
	// The zero value is off.
	SealRates progression.SealRates

	// SunRate widens the degree-9 Sun class of the rare table (port-only,
	// not native, beta balance; owner 2026-10-20). The zero value is off.
	SunRate progression.SunRate

	// ElixirDrops keeps every elixir out of monster drops (port-only, not
	// native, beta balance; owner 2026-10-13). The zero value is native.
	ElixirDrops progression.ElixirDrops

	// MaterialDrops keeps every alchemy material out of monster drops
	// (port-only, not native, beta balance; owner 2026-10-14). The zero
	// value is native.
	MaterialDrops progression.MaterialDrops

	// FastDenReturn halves the Bandit Den scroll's countdown (denreturn.go;
	// port-only, not native; owner 2026-10-14). False is native.
	FastDenReturn bool

	// DenNoTransportSummon refuses a transport during the Bandit Den
	// countdown (denreturn.go; port-only, not native). False is native.
	DenNoTransportSummon bool

	// UniqueDamageRank shows a unique's damage ranking to everyone who
	// damaged it (uniquedamagerank.go; port-only, not native). False, the
	// default, is native.
	UniqueDamageRank bool

	// Announce selects the port-only shard notices (announce.go); the zero
	// value is native: none.
	Announce  Announcements
	announcer announcer

	// NameChangeScroll enables the port-only name-change scroll (#1689,
	// SRO_NAME_CHANGE_SCROLL; namechange.go). The zero value is native:
	// the donor item keeps its unsupported-use answer and the mall lists
	// nothing extra.
	NameChangeScroll bool

	// SealDrops counts sealed drops for the weekly board (sealboard.go;
	// port-only, not native). Nil, the default, counts nothing; the
	// composition root installs the counter only when EnvSealBoard is on.
	SealDrops SealDropSink

	// ThiefPartyBonus raises a party's monster EXP and gold for each thief
	// in it (thiefparty.go; port-only, not native). False is native.
	ThiefPartyBonus bool

	// TradeJobBonus pays a trader and its party's hunters more gold for
	// travelling together (tradejobbonus.go; port-only, not native).
	TradeJobBonus bool

	// ShopOnly keeps degree-9 alchemy goods out of every drop (port-only,
	// not native, beta balance; owner 2026-10-13). The zero value is off.
	// MostWanted turns on the job-kill streak, its bonuses and bounty
	// (mostwanted.go; port-only, not native). False is native.
	MostWanted bool
	// MostWantedTest is the rule's temporary test pacing (port-only,
	// SRO_MOST_WANTED_TEST; false is the ordinary rule, mostwantedtest.go).
	MostWantedTest bool
	// MostWantedTestStreakStep is what a counted kill adds under the test
	// pacing (MostWantedTestStreakStepFromEnv); set with MostWantedTest.
	MostWantedTestStreakStep int64
	mostWantedPairs          mostWantedPairs
	// MostWantedBountyTransfer pays the bounty from the victim's gold, not
	// new gold (mostwanted.go; port-only, not native). False mints it.
	MostWantedBountyTransfer bool
	// MostWantedStreakNotice tells the killer its progress after every
	// counted kill (mostwantednotice.go; port-only, not native). False is
	// the current rule: nothing until a level.
	MostWantedStreakNotice bool
	// MostWantedCOSKills counts a job kill delivered on a companion for
	// Most Wanted like a player kill (mostwanted.go; port-only, not
	// native). False is native.
	MostWantedCOSKills bool

	// TraderCargoDrop keeps a loaded transport's goods in the world when
	// its trader dies riding or disconnects in the field
	// (tradercargodrop.go; port-only, not native). False is native.
	TraderCargoDrop bool
	// TradeTiers rates caravans by cargo gold and raises Jangan route profit
	// (tradetiers.go; port-only, not native). False is native.
	TradeTiers bool
	// RaisedGoldCaps admits a 10,000,000,000 stall price and a 1,000,000,000
	// warehouse gold deposit (goldcaps.go; port-only, not native). False is
	// native: 1,000,000,000 and 100,000,000, with the native u32 offer wire.
	RaisedGoldCaps bool
	// TeleportGrace makes a teleport arrival untouchable for the revival's
	// grace (teleportprotection.go; port-only, not native). False is native.
	TeleportGrace bool
	// PremiumUpgrade lets a strictly higher-grade premium ticket of the
	// same family replace the running lower one (premiumticket.go;
	// port-only, not native). False is native: 0x1894 while one runs.
	PremiumUpgrade bool
	// KillStats counts kills and EXP per online hour by level band
	// (killstats.go; port-only, not native). False counts nothing.
	KillStats bool
	// KillStatsFlush receives the drained rollup (the economy history
	// in the cmd wiring); nil keeps the rows in memory.
	KillStatsFlush func(rows []KillStatRow) error
	killStats      killStatTracker
	// tradeOrigins is each trade good's home town, from the shops.
	tradeOrigins map[uint32]tradeTown

	ShopOnly progression.ShopOnlyDegree9
	// SunEventOnly keeps degree-9 Seal of Sun gear out of drops and NPC
	// shops (port-only, not native; action/sunevent.go).
	SunEventOnly progression.SunEventOnly
	// UniqueRules: the unique reward switches (progression/uniquerules.go; port-only, not native). Zero is native.
	UniqueRules progression.UniqueRules

	// PetExpPace is the closed-beta growth multiplier for an attack pet's
	// EXP gain, read at the owner's level (progression GrowthRates.ExpPace,
	// petgrowth.go). Nil is native; the growth switch sets it (port-only).
	PetExpPace func(ownerLevel int64) float64
	// Port-only Hunter wolf pilot; off preserves native companion behavior.
	AttackPetExpansion bool
	// Port-only: DB-configured chance of extra pet gear per admitted normal equipment drop.
	PetGearDropFactor float64
	PetGearAffixes    []companion.AffixDef

	// PartyShareFloor raises every party member's EXP share to at least an
	// even split (partyRewardFactors). Off is native; the closed-beta growth
	// switch turns it on.
	PartyShareFloor bool

	// Now abstracts the clock for deterministic tests.
	Now func() time.Time
	// SetWorldHour moves the process world clock (/SETTIME); tests inject
	// it so the shared calendar stays put.
	SetWorldHour func(hour uint8) calendar.Value
	departureMu  sync.Mutex
	departures   map[uint64]pendingDeparture

	// UpdateQuestInventory is the quest lane's collect-objective updater. It
	// runs inside the item authority transaction so inventory and derived
	// quest progress cannot tear across a crash.
	UpdateQuestInventory func(character *enterworld.Character) ([]wire.Frame, bool)
	AdvanceQuestMinute   func(character *enterworld.Character) []wire.Frame
	AdvanceQuestItem     func(*enterworld.Character, int64) []wire.Frame
	ForgetQuestItem      func(*enterworld.Character)
	UseQuestItem         func(*enterworld.Character, string, simulation.Spawn, int64) ([]wire.Frame, bool)
	// CapturedFollowerDied tells a capture-escort quest its captured monster
	// died (a captured COS); called inside the owner's character door.
	CapturedFollowerDied        func(*enterworld.Character) []wire.Frame
	AdvanceQuestCalendar        func(nowMs int64)
	ReleaseQuestCapturesOnDeath func(*enterworld.Character) ([]wire.Frame, bool)
	QuestMonsterDrops           func(*enterworld.Character, string, uint8, func() (uint32, error)) []inventory.ItemAmount
	QuestTravelBlocks           func(*enterworld.Character) uint32
	// WalkInZoneAt names the walk-in event zone a live point stands in, or
	// "" (movement.WaterValidator.WalkInZoneAt; walkin.go). Nil disables
	// walk-in gates.
	WalkInZoneAt    func(simulation.Spawn) string
	UpdateQuestKill func(
		character *enterworld.Character,
		monsterCodename string,
		rarity uint8,
	) ([]wire.Frame, bool)
	// UpdateQuestTeleport reports a successful 0x7495 teleport use to
	// the quest lane (the used-a-teleport event: the use kind and the
	// gate's gid), raised once the move has landed, before the success
	// answer; its frames join the traveller's answer. Nil raises
	// nothing.
	UpdateQuestTeleport func(
		character *enterworld.Character,
		kind uint8,
		npcGID uint32,
	) []wire.Frame
	// NoteQuestKillSite reports where a monster a quest may care about
	// fell, in the killer's world, before UpdateQuestKill runs: a quest
	// instance raises its exit gate beside its boss (questgate.go). Nil
	// reports nothing.
	NoteQuestKillSite func(character *enterworld.Character, monsterCodename string, fell simulation.Spawn)

	// FortressGuildRole supplies the optional fortress membership role used
	// by siege-weapon equip requirements. Nil means no member record.
	FortressGuildRole FortressGuildRoleResolver

	// InsideClosedObject reports a pose inside an object no walk can enter
	// (movement.WaterValidator.InsideClosedObject); nil disables the
	// placement guard (placementguard.go).
	InsideClosedObject func(pose simulation.Spawn) (int, bool)

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
	// (it inserts uniquely), so a second proposal is dropped.
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

	// StatRecallWindows holds each recall scroll to its tier's level window
	// (statRecallWindow), set while the mall sells the scrolls
	// (commerce.StatRecallListed). Port-only, not native; false is today's
	// use, gated by the minimum level alone.
	StatRecallWindows bool

	// ApplyDeathPenalty is progression's door-free death updater: the EXP
	// and SP a death costs (pkdeath.go resolves which). Combat invokes it
	// from inside the fatal-HP character transaction.
	ApplyDeathPenalty func(character *enterworld.Character, penalty pk.DeathPenalty) ([]wire.Frame, bool)

	// UpdateJobExperience is progression's door-free job EXP updater
	// (the native job EXP grant): a job kill's share (pkreward.go). Nil pays
	// no job EXP.
	UpdateJobExperience func(character *enterworld.Character, delta int64) ([]wire.Frame, bool)

	// PushCharacterFrames delivers the actor's complete ordered progression
	// burst after the authority door closes. Pending pickups use captured
	// delivery when configured; detached runtimes retain this actor fallback.
	PushCharacterFrames func(divisionID, characterName string, frames []wire.Frame)
	// PushGuildFrames delivers one member's guild frames: its 0x3B29 deltas
	// only to a session holding guildID's block, every other frame as
	// PushCharacterFrames would (guild/baseline.go). Nil falls back to
	// PushCharacterFrames.
	PushGuildFrames func(divisionID, characterName string, guildID int64, frames []wire.Frame)
	// SilkWallet credits operator and GM silk grants (operator_silk.go);
	// RecordSilkGrant journals each committed grant.
	SilkWallet      SilkWallet
	RecordSilkGrant func(SilkGrant)
	// RecordUniqueKill queues each announced unique kill for the community
	// site's record (port-only). It runs inside the character door, so it
	// must only enqueue: the store write happens on its own goroutine.
	RecordUniqueKill func(division string, kill domain.UniqueKill)

	// CaptureCharacterFrames snapshots recipient sessions and their scene
	// revisions under the division lock, after the character door closes.
	// Capture must not send. Invoke the returned handle only after releasing
	// the division lock; a nil handle means no recipient was captured.
	CaptureCharacterFrames func(divisionID, characterName string) func([]wire.Frame)

	// CaptureObjectFrames is CaptureCharacterFrames keyed by the player's
	// object id, for tick batches that carry only a character id
	// (sealTickPrivate). The same capture rules apply.
	CaptureObjectFrames func(divisionID string, gid uint32) func([]wire.Frame)

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
	// chat restriction, sending its notice when it does. Nil means
	// no session can be restricted.
	ChatRestricted func(divisionID, characterName string) bool

	// PublicChat is the website's public chat transcript (chatfeed,
	// port-only): the Global Chatting line enters it, a GM ban purges the
	// banned character's lines. Nil keeps both in-game only.
	PublicChat *chatfeed.Feed
	// ChatShadowMuted reports a staff shadow-muted account (shadowmute,
	// port-only, not native): its Global Chatting line reaches only
	// itself. Nil mutes no one.
	ChatShadowMuted func(accountID string) bool
	// ModerationLog keeps the Global Chatting line for the staff
	// (chatlog, port-only, not native), muted lines included and
	// marked. Record only queues the line; nil keeps no log.
	ModerationLog *chatlog.Log

	// HonorPoints returns the character's usable Training Camp honor points
	// (the reference server's Training Camp member record, [member+0x34]+0x54). Nil reports 0.
	HonorPoints func(divisionID string, c *enterworld.Character) uint32

	// SpendHonorPoints deducts cost from Training Camp honor points and broadcasts
	// 0x3AC5 status 5 flag 3 (as the reference server's shop payment commit does).
	// Nil refuses honor deductions.
	SpendHonorPoints func(divisionID string, c *enterworld.Character, cost uint32) bool

	// honorMu serializes shop purchases against HonorPoints + SettleTrade +
	// SpendHonorPoints so concurrent 0x7034 requests cannot double-spend a
	// mentor's honor balance across the store door boundary. Always acquired
	// outside store.mu.
	honorMu sync.Mutex

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
	// tickGateCuts counts the TickHooks cut running by a held publication
	// gate (acquireTickGate). Tick goroutine only.
	tickGateCuts int
	// basicAttackCursor is the world key of the intent the last
	// advanceBasicAttackIntents call served last; the next call resumes
	// past it, so a burst spread over ticks cannot starve the tail of
	// the intent snapshot (#2143).
	basicAttackCursor string
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
	// NameFilter is the native name filter (AbuseFilter.txt) player-chosen
	// names pass (namefilter.go), shared with character creation at startup.
	NameFilter *namefilter.Filter
	// resurrections holds the unanswered resurrection proposals, one per
	// dead player (resurrection.go).
	resurrections resurrectionOffers
	// instanceInvites holds the unanswered instance invitations, one
	// per invited party member (instance_invite.go).
	instanceInvites instanceInviteBook
	// InstanceEventSink receives instanced-world lifecycle events for
	// the quest handler registry (instance_events.go); the composition
	// root points it at the quest runtime's dispatch.
	InstanceEventSink func(InstanceEvent)
	// instanceEvents queues lifecycle occurrences for delivery at the
	// next division-lock release: handlers never run under the lock
	// (instance_events.go).
	instanceEvents instanceEventQueue

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
		SetWorldHour:       calendar.SetHour,
		basicAttackIntents: make(map[string]basicAttackIntent),
		resurrections:      resurrectionOffers{byTarget: make(map[string]resurrectionOffer)},
		Exchanges:          exchange.New(),
		Stalls:             stall.New(),
	}

	if monsters != nil {
		monsters.SetAbnormalContext(monsterAbnormalContext{rt})
	}

	// Instance lifecycle occurrences are delivered when a division
	// lane is released, with no lock held (instance_events.go).
	rt.operations.onRelease = rt.flushInstanceEvents

	// The Capture the Flag match owner (flagwar_match.go) consumes
	// the registration queue at the daily start instant.
	rt.FlagWarMatchStart = rt.startFlagWarMatch

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
	// Around is published around other objects than the actor, each to
	// every session observing it (the recipient's own included).
	Around  []ObservedFrames
	Pending *PendingPickup

	// DiagnosticRefusal is an internal, non-wire reason for a silent native
	// refusal. The hub logs it at debug level; protocol behavior stays exact.
	DiagnosticRefusal string
}

/*
================
ObservedFrames

Public frames for the sessions that observe one object, as native
the reference server's nearby-session send sends around its argument: a
heal's effect add goes around each recipient, not around the caster.
================
*/
type ObservedFrames struct {
	SourceGID uint32
	Frames    []wire.Frame
}

/*
================
observedDivisionFrames

The Around routes of a result as tick batches, one per object.
================
*/
func observedDivisionFrames(division string, around []ObservedFrames) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, route := range around {
		if len(route.Frames) == 0 {
			continue
		}
		batch := simulation.DivisionFrames{DivisionID: division, SourceGID: route.SourceGID}
		for _, f := range route.Frames {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		out = append(out, batch)
	}
	return out
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

// ---- 0x72CD pickup ----

/*
==================
pickupRefusal
==================
*/
func pickupRefusal(errorCode uint8) OpResult {
	return OpResult{Frames: wire.PickupRefusalFrames(errorCode)}
}
