/*
===========================================================================

wiring_gameplay.go - gameplay authorities and production runtime composition.

Constructs one shard's owners and connects their transaction-safe entry
points before transport registration. Runtime callbacks capture this shard
identity rather than resolving characters while another owner holds a lock.

===========================================================================
*/
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	worldgeom "opensro.online/server/internal/game/world"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/honorfeed"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/macro"
	"opensro.online/server/internal/game/magiccube"
	"opensro.online/server/internal/game/moderation"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/siege"
	livepresence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/contributor"
	"opensro.online/server/internal/game/social/eventbot"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/sookarena"
	"opensro.online/server/internal/game/uniquestatus"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
	"opensro.online/server/internal/game/world/worldarea"
	"opensro.online/server/internal/platform/jobhonor"
	"opensro.online/server/internal/transport"
)

/*
================
gameplayPlane

The composition root joins concrete authorities to consumer-owned gameplay
ports. Runtimes retain only the capabilities declared by their packages.
================
*/
type gameplayPlane struct {
	divisionID string
	hub        *transport.Hub
	deps       *enterworld.Deps
	items      *action.Runtime
	// jobHonor is the job honor ledger the public board reads; nil off.
	jobHonor *jobhonor.Service
	// honorFeed is the Job Honor ranking feed (nil when SRO_JOB_HONOR
	// is off; wiring_jobhonor.go).
	honorFeed *honorfeed.Service
	// hotRouteSeen is the hour each session last heard the hot route
	// announced (wiring_jobhonor.go's hotRouteTick; only used while
	// honorFeed is built).
	hotRouteSeen map[string]int64
	// honorBuffSeen is what each session's tier honor buffs were last
	// reconciled against (wiring_jobhonor.go's honorBuffTick; only
	// used while honorFeed is built).
	honorBuffSeen map[string]honorBuffKey
	// jobHonorTierBytes is the last tier roster pushed,
	// jobHonorTierReached the sessions those bytes have reached, and
	// jobHonorTierNext the tick time it is next rebuilt (wiring_
	// jobhonor.go's jobHonorTierTick; only used while honorFeed is
	// built).
	jobHonorTierBytes   []byte
	jobHonorTierReached map[string]bool
	jobHonorTierNext    int64
	movement            *movement.Runtime
	water               *movement.WaterValidator
	// planes owns every AI route, monster and pet alike: the outdoor
	// detour in the field, the cave's region route on a dungeon floor.
	planes   simulation.Planes
	presence *livepresence.Directory
	// mutes is the chat mute book (moderation_wiring.go), set once the
	// shard is known; the console's conduct actions reach it.
	mutes *moderation.Book
	// gmModeration is what the GM's moderation commands act on
	// (moderation_wiring.go); its mute book is filled in beside mutes.
	gmModeration *gmcommand.Moderation
	// staff is the staff book and host allowlist (wiring_staff.go); nil
	// leaves the console's staff actions refused.
	staff        *staffSources
	chat         *chat.Runtime
	chatFeed     *chatfeed.Feed
	parties      *party.Runtime
	friends      *community.FriendRequests
	guildInvites *guild.InviteRuntime
	unions       *guild.UnionRuntime
	guildWars    *guild.WarRuntime
	// gpAccrual is the guild GP hunting accrual (issue #4051): the
	// progression mint notes into it, the tick drains it.
	gpAccrual     *guild.GpAccrual
	mentorInvites *mentor.InviteRuntime
	matches       *match.Runtime
	siege         *siege.Lane
	quests        *quest.Runtime
	// betaSilk earns the beta's Item Mall silk (nil when SRO_BETA_SILK is off).
	betaSilk *action.BetaSilk
	// eventBot is the EventBot chat-event host (nil when SRO_EVENT_BOT
	// is off; wiring_eventbot.go, port-only #3533).
	eventBot *eventbot.Host
	// contributors is the Contributor title (nil when
	// SRO_CONTRIBUTOR_TITLES is off; wiring_contributor.go).
	contributors *contributor.Registry
	// uniqueStatus is the Unique Status feed (nil when
	// SRO_UNIQUE_STATUS is off; wiring_uniquestatus.go).
	uniqueStatus *uniquestatus.Service
	// caravanAgents is the AI Caravan & Thief runtime (nil when
	// SRO_AI_CARAVANS is off; wiring_caravanai.go).
	caravanAgents *caravanai.Runtime
	// caravanPeers is the agents' peer-presentation leg (slice 3a,
	// wiring_caravanai.go); peerReferences is the item reference
	// catalog every peer-shaped spawn draws from (wiring.go).
	caravanPeers   *caravanPeerLeg
	peerReferences *action.PeerReferenceCatalog
	// caravanStrikeFrames are this tick's agent-strike publications,
	// filled by the strike hook while the roster steps and flushed
	// by caravanAgentsTick (slice 3e).
	caravanStrikeFrames []simulation.DivisionFrames
	// caravanChatLines are the lines the agents spoke this tick,
	// filled by the chat hook while the roster steps and flushed as
	// local chat by caravanAgentsTick (slice 4).
	caravanChatLines []caravanChatLine
}

/*
================
newGameplayPlane

Builds the shard's shared authorities before any gameplay handler is exposed.
All effect-aware projections refer to the same action runtime.
================
*/
func newGameplayPlane(
	ts *transport.Server,
	authorityStore *store.Store,
	ownedShard shard.Definition,
	devPaths enterworld.DevPaths,
	characterRoster *enterworld.Roster,
	worldAuthorityDir string,
	authoredAreas *worldarea.Catalog,
	textdata *enterworld.TextdataCatalogs,
) (*gameplayPlane, error) {
	// SRO_CHRISTMAS_EVENT_MODE=so_ok_2006 (port-only, not native): So-ok
	// wears Santa So-Ok for the window (wiring_christmas.go).
	devPaths.NpcSpawns.Look = sookChristmasNpcLook()
	deps, err := newBootstrapDependencies(authorityStore, devPaths, characterRoster, ownedShard, textdata)
	if err != nil {
		return nil, err
	}
	items := action.NewRuntime(deps, deps.MonsterState)
	items.ConfigureSookArena(os.Getenv(sookarena.Env))
	// SRO_STYRIA_CLASH (port-only, not native): the Styria Clash
	// arena engine (#3648); its schedule is wired in wiring_register.
	items.ConfigureStyria(os.Getenv(styria.Env))
	// SRO_ROC (port-only, not native): the Roc worlds' schedule and steps.
	items.ConfigureRoc(monster.RocEnabled())
	// SRO_PHARAOH_TEMPLE (port-only, not native): the sanctums' scheduled gates.
	items.ConfigureTempleSanctums(pharaohtemple.Enabled())
	// The Sarin gate's schedule rides the shipped tomb (#4135).
	items.ConfigureTombGate(monster.QinShiTombFromEnv())
	// SRO_AUTO_MACRO (port-only, not native): the SRO Macro settings lane.
	items.ConfigureMacro(macro.Enabled(os.Getenv))
	if err := loadPetGearPolicy(items, authorityStore); err != nil {
		return nil, err
	}
	items.CaptureCharacterFrames = func(division, name string) func([]wire.Frame) {
		return captureCharacterFrames(ts.Hub, division, name)
	}
	items.CaptureObjectFrames = func(division string, gid uint32) func([]wire.Frame) {
		return captureObjectFrames(ts.Hub, division, gid)
	}
	items.PetPolicies = companion.PoliciesFromEnv()
	items.AttackPetExpansion = companion.AttackPetExpansionEnabled()
	items.NasrunSocket = nasrun.Enabled(os.Getenv(nasrun.Env))
	items.NasrunTokens = items.NasrunSocket
	items.MagicCube = magiccube.Enabled(os.Getenv(magiccube.Env))
	items.Guilds = deps.Guilds
	items.UnlimitedItems = enterworld.StarterKitCodenames(deps.StarterKit)
	if err := items.ValidateLootReferences(); err != nil {
		return nil, fmt.Errorf("loot catalogue: %w", err)
	}
	deps.PlayerBaseStats = func(character *enterworld.Character) (wire.BaseStats, error) {
		return items.PlayerBaseStats(ownedShard.ID, character)
	}
	// SRO_QINSHI_TOMB (port-only, not native): the tomb's later-data doors
	// and spawns join at catalog load; unset is the sealed native tomb.
	// The catalog loaders read the flag themselves (world/monster); this
	// line only leaves the operator a boot record.
	if monster.QinShiTombFromEnv() {
		log.Infof("portals: Qin-Shi Tomb ON (%s): the door by Jangan admits level 70 and above", monster.EnvQinShiTomb)
	}
	if err := items.ConfigurePortals(devPaths.TextdataDir); err != nil {
		return nil, fmt.Errorf("portal catalogue: %w", err)
	}
	// Port-only, not native: the reverse return scroll's map destinations.
	if err := configureReverseReturnMap(items, devPaths.TextdataDir); err != nil {
		return nil, err
	}
	// Port-only, not native (#3349): the Muhan change NPCs' pairing. Off
	// reads nothing and installs nothing.
	muhanTransforms, err := monster.LoadMuhanTransforms(devPaths.TextdataDir, monster.MuhanEnabled())
	if err != nil {
		return nil, err
	}
	if err := items.ConfigureMuhanTransforms(muhanTransforms); err != nil {
		return nil, err
	}
	// One clock for the bootstrap and the tick sweep, read through the runtime
	// so a swapped clock reaches both; world entry hands re-raised pet-skill
	// windows to the same sweep that retires them.
	deps.Now = func() time.Time { return items.Now() }
	deps.TrackTimedWindows = items.TrackTimedWindows
	deps.TrackMostWanted = items.TrackMostWanted
	deps.RestoreEntryEffects = items.RestoreTimedSkillJobs
	deps.AdmitCharacterSession = items.AdmitCharacterSession
	deps.SpiritSkills = items.SpiritSkillsAt
	deps.RetireCharacterSession = items.ForgetCharacterSession
	deps.EntryPopulationLease = items.EntryPopulationLease
	deps.EntrySkills = items.EntrySkills
	deps.EntryMovementSpeeds = items.EntryMovementSpeeds
	deps.EntryActionSpeed = items.EntryActionSpeed
	deps.EntryCompanionActionSpeed = items.EntryCompanionActionSpeed
	deps.EntryCompanionSpawn = items.EntryCompanionSpawn
	deps.EntryCompanionMovementSpeeds = items.EntryCompanionMovementSpeeds
	if err := items.ConfigureAlchemy(devPaths.TextdataDir); err != nil {
		return nil, fmt.Errorf("alchemy catalogue: %w", err)
	}
	if err := items.ConfigureGacha(devPaths.TextdataDir); err != nil {
		return nil, fmt.Errorf("gacha catalogue: %w", err)
	}
	if err := items.ConfigureStallNetwork(devPaths.TextdataDir); err != nil {
		return nil, err
	}
	items.NameFilter = textdata.NameFilter
	// SRO_SHOP_EXTRA_GOODS (port-only, not native): operator NPC shop goods;
	// "off" is the v1.150 stock (commerce/extragoods.go).
	extraGoods, err := commerce.ExtraGoodsFromEnv()
	if err != nil {
		return nil, err
	}
	if len(extraGoods) > 0 {
		log.Infof("commerce: %d extra NPC shop goods from %s", len(extraGoods), commerce.EnvShopExtraGoods)
	}
	// SRO_ELIXIR_DROPS_OFF (port-only, not native): elixirs leave every
	// monster drop; the shop's extra goods must then sell all eight.
	items.ElixirDrops = progression.ElixirDropsFromEnv()
	if err := items.ElixirDrops.CheckSource(func(codename string) bool {
		for _, good := range extraGoods {
			if good.Codename == codename {
				return true
			}
		}
		return false
	}, commerce.EnvShopExtraGoods); err != nil {
		return nil, err
	}
	if items.ElixirDrops.Off() {
		log.Infof("loot: elixir drops OFF (%s): the shop sells them", progression.EnvElixirDropsOff)
	}
	// SRO_ALCHEMY_MATERIAL_DROPS_OFF (port-only, not native): with the
	// switch on, alchemy materials leave every monster drop. The deployed
	// default is off (community vote, 2026-10-24): the stone chain uses
	// them again.
	items.MaterialDrops = progression.MaterialDropsFromEnv()
	if items.MaterialDrops.Off() {
		log.Infof("loot: alchemy material drops OFF (%s)", progression.EnvAlchemyMaterialDropsOff)
	}
	// SRO_ABOVE_DEGREE9_DROPS_OFF (port-only, not native; owner
	// 2026-11-16): while the area polls run, no monster drops an item above
	// degree 9.
	items.AboveDegree9Drops = progression.AboveDegree9DropsFromEnv()
	if items.AboveDegree9Drops.Off() {
		log.Infof("loot: drops above degree %d OFF (%s)", progression.MaxDropDegree, progression.EnvAboveDegree9DropsOff)
	}
	// SRO_SUN_EVENT_ONLY (port-only, not native): degree-9 Seal of Sun gear
	// leaves monster drops and NPC shops; it returns only as an event reward.
	items.SunEventOnly = progression.SunEventOnlyFromEnv()
	if items.SunEventOnly.Enabled() {
		log.Infof("loot: degree-9 Seal of Sun gear neither drops nor is sold (%s)", progression.EnvSunEventOnly)
	}
	// SRO_HALLOWEEN_EVENT (port-only, not native in this client build):
	// the Halloween Day event's switch and UTC window (wiring_halloween.go).
	items.Halloween = halloweenEventFromEnv()
	// SRO_HALLOWEEN_HELMET_SEAL (port-only, not native): the event
	// helmets stay sealed at the transfer doors after the event.
	items.HalloweenHelmetSeal = halloweenHelmetSealFromEnv()
	// SRO_CHRISTMAS_EVENT and its modes (port-only, not native in this
	// client build): wiring_christmas.go composes them.
	wireChristmasEvent(items, deps.ItemReferences(), authorityStore)
	// SRO_WEATHER: the native climate cycle, on by default (wiring_weather.go).
	startWeatherFromEnv(items)
	// SRO_UNIQUE_GAP_EXEMPT / SRO_UNIQUE_POP_TICKET / SRO_UNIQUE_MAX_LEVEL_LOOT
	// / SRO_UNIQUE_SEAL_DROPS (port-only, not native): uniques ignore the
	// level-gap admission, one Magic Pop ticket per kill, a top-band unique's
	// equipment rolls at the shard's highest installed level, and each kill
	// guarantees its seal reward.
	items.UniqueRules = progression.UniqueRulesFromEnv()
	if items.UniqueRules.GapExempt || items.UniqueRules.PopTicket || items.UniqueRules.MaxLevelLoot || items.UniqueRules.SealDrops {
		log.Infof("loot: unique rewards (port-only): gap exemption %t, pop ticket %t, max-level loot %t, seal drops %t (%s, %s, %s, %s)",
			items.UniqueRules.GapExempt, items.UniqueRules.PopTicket, items.UniqueRules.MaxLevelLoot, items.UniqueRules.SealDrops,
			progression.EnvUniqueGapExempt, progression.EnvUniquePopTicket, progression.EnvUniqueMaxLevelLoot, progression.EnvUniqueSealDrops)
	}
	// SRO_JANGAN_XL_POTIONS (port-only, not native; owner 2026-11-07):
	// the X-Large potions join Jangan's potion tab at Hotan's price, as
	// extra goods on the same catalogue. "off" is the native Jangan shop.
	xlGoods, err := commerce.JanganXLPotionGoodsFromEnv()
	if err != nil {
		return nil, err
	}
	extraGoods = append(extraGoods, xlGoods...)
	// SRO_NASRUN (port-only, not native, #3456 3b): the Devil's Spirit
	// token goods on the honor stores (commerce/nasrungoods.go).
	extraGoods = append(extraGoods, commerce.NasrunGoodsFromEnv()...)
	// SRO_ALEXANDRIA_HIDE_D11_GOODS (port-only, not native, #2842):
	// degree-11 goods leave Alexandria's own shops as ConfigureCommerce
	// loads them, so it is set first. "off" (the default) is native:
	// the original game sold them there.
	hideD11, err := commerce.AlexandriaHideD11GoodsFromEnv()
	if err != nil {
		return nil, err
	}
	items.AlexandriaHideD11Goods = hideD11
	if hideD11 {
		log.Infof("shops: Alexandria's degree-11 goods are hidden (%s, port-only, not native)", commerce.EnvAlexandriaHideD11Goods)
	}
	// SRO_TRADE_TIERS (port-only, not native) prices the shops' trade goods
	// as ConfigureCommerce loads them, so it is set first.
	items.TradeTiers = action.TradeTiersFromEnv()
	if err := items.ConfigureCommerce(devPaths.TextdataDir, extraGoods); err != nil {
		return nil, fmt.Errorf("commerce catalogue: %w", err)
	}
	items.ConfigureStorage(authorityStore)
	betaSilk, mallAuthority, err := composeBetaSilk(authorityStore, deps)
	if err != nil {
		return nil, err
	}
	// The Item Mall's catalogue and its port-only goods (wiring_itemmall.go).
	if err := configureItemMall(items, devPaths.TextdataDir, mallAuthority, deps.ItemReferences()); err != nil {
		return nil, err
	}
	deps.SceneReferenceFrames = items.CommerceReferenceSeed
	// The commerce seed's digest folds into the entry constants digest
	// (#4421): shop data changing under a held session invalidates its
	// mark, so a stale seed can never be kept by the digest gate.
	{
		hash := sha256.New()
		for _, frame := range items.CommerceReferenceSeed() {
			_, _ = hash.Write(frame.Payload)
		}
		deps.SeedDigest = hex.EncodeToString(hash.Sum(nil))
	}
	// #1689 (port-only): the name-change scroll's queue door. The store's
	// refusal vocabulary is translated at this boundary so the gameplay
	// packages never import the store.
	deps.QueueRename = func(divisionID string, character *enterworld.Character, name string, nowMs int64) (enterworld.RenameQueueOutcome, error) {
		err := authorityStore.QueueCharacterRename(divisionID, character, name, nowMs)
		switch {
		case err == nil:
			return enterworld.RenameQueued, nil
		case errors.Is(err, store.ErrCharacterNameInvalid):
			return enterworld.RenameRefusedInvalidName, nil
		case errors.Is(err, store.ErrCharacterNameConflict):
			return enterworld.RenameRefusedNameTaken, nil
		case errors.Is(err, store.ErrNameReserved):
			return enterworld.RenameRefusedNameReserved, nil
		case errors.Is(err, store.ErrRenamePending):
			return enterworld.RenameRefusedAlreadyQueued, nil
		case errors.Is(err, store.ErrRenameCooldown):
			return enterworld.RenameRefusedCooldown, nil
		default:
			return enterworld.RenameRefusedInvalidName, err
		}
	}
	deps.ReconcileSceneObjects = items.ReconcileScene
	deps.ExtraRefItemCodenames = items.GroundRefItemCodenames
	deps.StaticRefItemCodenames = items.StaticRefItemCodenames
	deps.ExtraMagicOptionIDs = items.AlchemyMagicOptionIDs
	deps.AvatarMagicOptions = items.AvatarMagicOptions
	items.Ground.Restore(authorityStore.GroundSnapshotForRestore())
	authorityStore.AttachGround(items.Ground)

	logAuthorityReady(authorityStore, deps)
	appendGroundObjectRows(deps, items)

	water := movement.NewAuthorityValidator(worldAuthorityDir)
	if err := wireAlexandria(water, deps, devPaths.TextdataDir); err != nil {
		return nil, err
	}
	if err := water.ValidateSecurityAssets(); err != nil {
		return nil, err
	}
	water.SetClosedObjectRescue(movement.ClosedObjectRescueFromEnv())
	water.SetCaveSeamStep(movement.CaveSeamStepFromEnv())
	water.SetCaveModernChase(movement.CaveModernChaseFromEnv())
	water.SetCaveObstacleEscape(movement.CaveObstacleEscapeFromEnv())
	water.SetDeepWaterRefusal(movement.DeepWaterRefusalFromEnv())
	// Native dungeon interest is the 3x3 message-block neighbourhood of each
	// dungeon's DOF grid (worldgeom dungeon_grid.go). Installed before the
	// first session so every interest query sees the same table.
	dungeonGrids := water.DungeonMessageGrids()
	worldgeom.InstallDungeonMessageGrids(dungeonGrids)
	log.Infof("world: dungeon message grids installed for %d dungeon region(s)", len(dungeonGrids))
	navigationStarted := time.Now()
	navigationRegions, err := water.PreloadOutdoorNavigation()
	if err != nil {
		return nil, err
	}
	log.Infof("movement: preloaded %d outdoor navigation regions in %s before gameplay readiness", navigationRegions, time.Since(navigationStarted))
	if deps.MonsterState != nil {
		deps.MonsterState.EnableRegionDormancy()
		deps.MonsterState.SetSpawnGroundResolver(water.WalkableSpawnHeightAt)
		deps.MonsterState.SetSpawnCollisionTest(water.SpawnMoveTest)
		deps.MonsterState.SetSpawnRegionAvailability(water.SpawnRegionAvailable)
		deps.MonsterState.SetPopulationPlayers(items.PopulationPlayers)
		installWinterSpawnReplacements(deps.MonsterState, items)
		for _, division := range authorityStore.DivisionIDs() {
			deps.MonsterState.StartDivision(division)
		}
	}
	movementRuntime := movement.NewRuntime(deps, items.Worlds)
	movementRuntime.Validator = water
	// SRO_POSITION_STREAM (port-only, not native): the ticker reads the
	// same switch (worldsession.NewTicker).
	movementRuntime.PositionStream = simulation.PositionStreamFromEnv()
	movementRuntime.PathGuard = movement.NewPathGuardFromEnv(water)
	movementRuntime.ClientClip = movement.NewClientClipFromEnv(water)
	// Surface ownership (native source pNavCell): every character walk starts
	// from its retained owner and commits the owner it reached.
	movementRuntime.Nav = water
	movementRuntime.CanEnterRegion = func(character *enterworld.Character, regionID uint16) bool {
		return authoredAreas.CanEnterRegion(regionID, character.GMPrivilege)
	}
	// A thief in job mode may not bring its transport into a town (#721).
	movementRuntime.TownEntryBlocked = items.ThiefCargoTownBlocked
	if err := configureThiefCartTownAreas(items, movementRuntime, devPaths.TextdataDir); err != nil {
		return nil, err
	}
	movementRuntime.EnableGroundWalk()
	items.ConstrainMovement = movementRuntime.ConstrainMovement
	items.SpawnRegionAvailable = water.SpawnRegionAvailable
	items.ConstrainCompanionSpawn = water.ConstrainCompanionSpawn
	items.InsideClosedObject = water.InsideClosedObject
	items.CompanionSurfaceHeight = water.WalkableSpawnHeightAt
	items.ConstrainWalk = movementRuntime.ConstrainMovementFrom
	planes := movement.NewPlanes(movementRuntime, water)
	items.PlanCompanionRoute = simulation.PetRouteFromPlanes(planes)
	items.AdmitGroundWalk = movementRuntime.AdmitGroundWalk
	items.LineOfSight = movementRuntime.LineOfSight
	// The NPC talk gate's sight arm (#4125, owner ruling 2026-11-23).
	items.TalkSight = movementRuntime.TalkSight
	items.ResolveNavOwner = water.ResolveNavOwner
	items.WalkInZoneAt = water.WalkInZoneAt
	items.MoveCOS = movementRuntime.HandleCOSMove
	items.MoveCOSPublished = movementRuntime.HandleCOSMovePublished
	items.SteerCOS = movementRuntime.HandleCOSSteer
	items.StopCOS = movementRuntime.HandleCOSStop
	movementRuntime.UsePendingTracker(items.Pending)
	movementRuntime.ClearCombatIntent = items.ClearCombatIntent
	movementRuntime.GroundBlocked = items.RetireGroundApproach
	movementRuntime.MovementBlocked = items.PlayerMovementBlocked
	movementRuntime.RetireMoveEffects = items.RetireMoveEffects
	movementRuntime.AttackLocked = items.PlayerAttackLocked
	movementRuntime.MotionLocked = items.PlayerMotionLocked
	movementRuntime.InteractionLocked = items.PlayerInteractionLocked
	movementRuntime.AdvanceResidentRegion = items.AdvanceResidentRegion
	movementRuntime.CompanionPresentations = items.CompanionPresentations
	movementRuntime.SpawnSkills = items.SpawnEntrySkills
	movementRuntime.ActionSpeed = items.EntryActionSpeed
	deps.SpawnTerrainHeight = water.TerrainHeightAt
	deps.SpawnSurfaceHeight = water.WalkableSpawnHeightAt
	deps.RelocateStrandedSpawn = water.RelocateStrandedSpawn
	deps.AdoptEntrySpawn = items.AdoptEntrySpawn

	presence := livepresence.NewDirectory(ts.Hub)
	unionAuthority := union.New()
	if err := unionAuthority.Restore(ownedShard.ID, deps.Alliances); err != nil {
		return nil, err
	}
	items.Unions = unionAuthority
	movementRuntime.Unions = unionAuthority
	movementRuntime.Stalls = items.Stalls
	unions := guild.NewUnionRuntime(deps, presence, unionAuthority, items.Fortresses)
	warAuthority, err := guildwar.New(ownedShard.ID, deps.GuildWars)
	if err != nil {
		return nil, err
	}
	guildWars := guild.NewWarRuntime(deps, presence, unions, warAuthority)
	items.GuildWars = warAuthority
	items.GuildWarKill = guildWars.RecordKill
	guildWars.Near = items.GuildWarMastersNear
	unions.GuildWars = warAuthority

	communitySeeds := community.SeedFramesFunc(presence, deps.Letters)
	deps.CommunitySeedFramesFor = func(divisionID string, character *enterworld.Character) []enterworld.Packet {
		frames := communitySeeds(divisionID, character)
		frames = guild.AppendSeedFrame(frames, deps.Guilds, presence, divisionID, character)
		if frame, ok := unions.SeedFrame(divisionID, character); ok {
			frames = append(frames, frame)
		}
		if frame, ok := guildWars.SeedFrame(divisionID, character); ok {
			frames = append(frames, frame)
		}
		return frames
	}

	parties := party.NewRuntime(deps, presence)
	parties.UseMemberVitals(items.GameplayVitals)
	parties.UseLivePose(items.LiveSpawnFor)
	parties.UseMemberGuild(func(divisionID string, character *enterworld.Character) string {
		if character == nil || character.GuildID == nil || deps.Guilds == nil {
			return ""
		}
		guild, ok := deps.Guilds.GuildRecord(divisionID, *character.GuildID)
		if !ok {
			return ""
		}
		return guild.Name
	})
	items.NextPartyLootMember = parties.Registry().NextLootMember
	items.RewardActorPresent = func(division, name string) bool {
		s, ok := presence.SessionByName(division, name)
		return ok && s.WorldReady()
	}
	items.RewardParties = func(division string) []action.RewardParty {
		var out []action.RewardParty
		for _, p := range parties.Registry().RewardSnapshots(division) {
			row := action.RewardParty{Order: p.ObjectOrder, Options: p.OptionBits, LeaderID: p.LeaderID}
			for _, m := range p.Members {
				row.Members = append(row.Members, m.MemberID)
			}
			out = append(out, row)
		}
		return out
	}
	items.CanPickupOwnedDrop = parties.Registry().CanPickupOwnedDrop
	guildInvites := guild.NewInviteRuntime(deps, presence)
	guildInvites.Unions = unions
	mentorInvites := mentor.NewInviteRuntime(deps, presence)
	deps.OnCharacterLevelChanged = func(character *enterworld.Character) {
		mentorInvites.NotifyMemberLevel(ownedShard.ID, character)
	}
	connectInvitationLanes(parties, guildInvites, unions, mentorInvites, items)
	connectWarInvitations(warInvitationLanes{parties: parties, guilds: guildInvites, unions: unions, mentors: mentorInvites, items: items, wars: guildWars})

	matches := match.NewRuntime(deps, presence)
	matches.MemberCountFor = func(divisionID, characterName string) int {
		if snapshot, ok := parties.Registry().PartyOf(divisionID, characterName); ok {
			return len(snapshot.Members)
		}
		return 1
	}
	matches.PartyListingAuthority = parties.ListingAuthority
	matches.PartyListingOwnerFor = parties.MatchListingOwner
	matches.PartyJoinPrecheck = parties.MatchJoinPrecheck
	matches.CommitPartyJoin = parties.AdmitMatchJoin
	matches.PartyMemberInfoFor = parties.MaskedMemberInfoFor
	matches.MentorJoinPrecheck = mentorInvites.MatchJoinPrecheck
	matches.CommitMentorJoin = mentorInvites.CommitMatchJoin

	if err := items.Fortresses.Restore(ownedShard.ID, deps.Fortresses); err != nil {
		return nil, err
	}
	items.FortressStore = deps.Fortresses
	siegeRuntime, err := siege.NewLane(siege.LaneConfig{
		Division:   ownedShard.ID,
		Fortresses: items.Fortresses,
		GuildName: func(guildID int64) string {
			guild, ok := deps.Guilds.GuildRecord(ownedShard.ID, guildID)
			if !ok {
				return ""
			}
			return guild.Name
		},
		WarChanged: func(nowMs int64, active bool) []simulation.DivisionFrames {
			return items.FortressWarChanged(ownedShard.ID, nowMs, active)
		},
	})
	if err != nil {
		return nil, err
	}
	items.FortressWindows = siegeRuntime.WarStart
	items.FortressWarDates = siegeRuntime.WarDates
	items.FortressList = siegeRuntime.FortressList

	game := &gameplayPlane{
		divisionID:    ownedShard.ID,
		hub:           ts.Hub,
		deps:          deps,
		items:         items,
		movement:      movementRuntime,
		water:         water,
		planes:        planes,
		presence:      presence,
		parties:       parties,
		guildInvites:  guildInvites,
		unions:        unions,
		guildWars:     guildWars,
		mentorInvites: mentorInvites,
		matches:       matches,
		siege:         siegeRuntime,
		betaSilk:      betaSilk,
		eventBot:      buildEventBot(ownedShard.ID, deps, presence, authorityStore, items),
	}
	// The Unique Status feed (port-only, #3531; nil while off).
	wireUniqueStatus(game, authorityStore)
	return game, nil
}
