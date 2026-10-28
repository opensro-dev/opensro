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
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	worldgeom "opensro.online/server/internal/game/world"
	"time"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/game/siege"
	livepresence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/worldarea"
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
	divisionID    string
	hub           *transport.Hub
	deps          *enterworld.Deps
	items         *action.Runtime
	movement      *movement.Runtime
	water         *movement.WaterValidator
	presence      *livepresence.Directory
	chat          *chat.Runtime
	chatFeed      *chatfeed.Feed
	parties       *party.Runtime
	friends       *community.FriendRequests
	guildInvites  *guild.InviteRuntime
	unions        *guild.UnionRuntime
	guildWars     *guild.WarRuntime
	mentorInvites *mentor.InviteRuntime
	matches       *match.Runtime
	siege         *siege.Lane
	quests        *quest.Runtime
	// betaSilk earns the beta's Item Mall silk (nil when SRO_BETA_SILK is off).
	betaSilk *action.BetaSilk
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
	deps, err := newBootstrapDependencies(authorityStore, devPaths, characterRoster, ownedShard, textdata)
	if err != nil {
		return nil, err
	}
	items := action.NewRuntime(deps, deps.MonsterState)
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
	// One clock for the bootstrap and the tick sweep, read through the runtime
	// so a swapped clock reaches both; world entry hands re-raised pet-skill
	// windows to the same sweep that retires them.
	deps.Now = func() time.Time { return items.Now() }
	deps.TrackTimedWindows = items.TrackTimedWindows
	deps.RestoreEntryEffects = items.RestoreTimedSkillJobs
	deps.AdmitCharacterSession = items.AdmitCharacterSession
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
	// SRO_SUN_EVENT_ONLY (port-only, not native): degree-9 Seal of Sun gear
	// leaves monster drops and NPC shops; it returns only as an event reward.
	items.SunEventOnly = progression.SunEventOnlyFromEnv()
	if items.SunEventOnly.Enabled() {
		log.Infof("loot: degree-9 Seal of Sun gear neither drops nor is sold (%s)", progression.EnvSunEventOnly)
	}
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
	// SRO_MALL_EXTRA_GOODS (port-only, not native): authored packages the
	// v1.150 mall does not list; "off" is the shipped mall (commerce/mallextra.go).
	mallExtra, err := commerce.MallExtraGoodsFromEnv()
	if err != nil {
		return nil, err
	}
	if len(mallExtra) > 0 {
		log.Infof("mall: %d extra Item Mall goods from %s", len(mallExtra), commerce.EnvMallExtraGoods)
	}
	// SRO_NAME_CHANGE_SCROLL (port-only, not native, #1689): the mall
	// sells the name-change scroll and the action runtime serves its use.
	items.NameChangeScroll = action.NameChangeScrollFromEnv()
	if items.NameChangeScroll {
		mallExtra = append(mallExtra, commerce.MallExtraGood{
			Tab: action.NameChangeMallTab, Package: action.NameChangeMallPackage,
			Item: action.NameChangeScrollItem, Silk: action.NameChangePriceSilk,
		})
		log.Infof("mall: name change scroll listed at %d silk (%s, port-only, not native)", action.NameChangePriceSilk, action.EnvNameChangeScroll)
	}
	if err := items.ConfigureMall(devPaths.TextdataDir, mallAuthority, mallExtra); err != nil {
		return nil, fmt.Errorf("mall catalogue: %w", err)
	}
	items.StatRecallWindows = commerce.StatRecallListed(mallExtra)
	deps.SceneReferenceFrames = items.CommerceReferenceSeed
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
	// Port-only, not native: Alexandria's regions exist only with SRO_ALEXANDRIA.
	hiddenAlexandria, err := alexandria.Hidden(devPaths.TextdataDir)
	if err != nil {
		return nil, err
	}
	water.HideAlexandria(hiddenAlexandria)
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
		for _, division := range authorityStore.DivisionIDs() {
			deps.MonsterState.StartDivision(division)
		}
	}
	movementRuntime := movement.NewRuntime(deps, items.Worlds)
	movementRuntime.Validator = water
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
	movementRuntime.EnableGroundWalk()
	items.ConstrainMovement = movementRuntime.ConstrainMovement
	items.SpawnRegionAvailable = water.SpawnRegionAvailable
	items.ConstrainCompanionSpawn = water.ConstrainCompanionSpawn
	items.InsideClosedObject = water.InsideClosedObject
	items.CompanionSurfaceHeight = water.WalkableSpawnHeightAt
	items.ConstrainWalk = movementRuntime.ConstrainMovementFrom
	items.PlanCompanionRoute = simulation.PetRouteFromMonster(water.PlanMonsterRoute)
	items.AdmitGroundWalk = movementRuntime.AdmitGroundWalk
	items.LineOfSight = movementRuntime.LineOfSight
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
	movementRuntime.AdvanceResidentRegion = items.AdvanceResidentRegion
	movementRuntime.CompanionPresentations = items.CompanionPresentations
	movementRuntime.SpawnSkills = items.EntrySkills
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
			guild, _, ok := deps.Guilds.Guild(ownedShard.ID, guildID)
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

	return &gameplayPlane{
		divisionID:    ownedShard.ID,
		hub:           ts.Hub,
		deps:          deps,
		items:         items,
		movement:      movementRuntime,
		water:         water,
		presence:      presence,
		parties:       parties,
		guildInvites:  guildInvites,
		unions:        unions,
		guildWars:     guildWars,
		mentorInvites: mentorInvites,
		matches:       matches,
		siege:         siegeRuntime,
		betaSilk:      betaSilk,
	}, nil
}

/*
================
appendGroundObjectRows

Adds live ground objects after authored NPC rows while retaining the existing
bootstrap producer and its visibility decisions.
================
*/
func appendGroundObjectRows(deps *enterworld.Deps, items *action.Runtime) {
	npcRows := deps.ObjectListRows
	deps.ObjectListRows = func(
		divisionID string,
		character *enterworld.Character,
		entry *enterworld.LocalPlayerEntry,
	) []enterworld.Packet {
		var rows []enterworld.Packet
		if npcRows != nil {
			rows = npcRows(divisionID, character, entry)
		}
		rows = append(rows, items.GroundObjectRows(divisionID, character, entry)...)
		return append(rows, items.SkillObjectRows(divisionID, character, entry)...)
	}
}

/*
================
connectInvitationLanes

Shares the single outstanding-consent rule across party, guild, mentor,
and resurrection invitations without moving their state ownership.
================
*/
func connectInvitationLanes(
	parties *party.Runtime,
	guildInvites *guild.InviteRuntime,
	unions *guild.UnionRuntime,
	mentorInvites *mentor.InviteRuntime,
	items *action.Runtime,
) {
	resurrections := items.ResurrectionConsent()
	exchanges := items.ExchangeConsent()
	instanceInvites := items.InstanceInviteConsent()
	parties.AddConsentArm(exchanges)
	parties.AddConsentArm(guildInvites)
	parties.AddConsentArm(unions)
	parties.AddConsentArm(mentorInvites)
	parties.AddConsentArm(resurrections)
	parties.AddConsentArm(instanceInvites)

	// A player holds one unanswered proposal at a time, whatever its
	// lane (the server's unique transaction insert): every lane refuses to
	// propose while another's proposal waits.
	partyPending := parties.Registry().HasPendingInviteFor
	guildInvites.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	unions.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	mentorInvites.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	items.ProposalPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	parties.PartyChanged = items.NotePartyChanged
}

/*
================
register

Installs validated cross-owner callbacks before registering transport
handlers. Progression callbacks operate on the caller's candidate and must
never acquire another character transaction.
================
*/
func (game *gameplayPlane) register(hub *transport.Hub, loadQuests questDefinitionLoader) error {
	if game.betaSilk != nil {
		// SRO_BETA_SILK_ACTIVE_ONLY (port-only): any player-sent command
		// counts as play (#1604 P5), so the transport hub reports every
		// dispatched game frame; NoteActivity is inert with the flag off.
		hub.OnSessionActivity(game.betaSilk.NoteActivity)
	}
	if err := game.deps.Validate(); err != nil {
		return fmt.Errorf("bootstrap dependencies: %w", err)
	}
	if err := game.movement.ValidateSecurityPolicy(); err != nil {
		return err
	}
	if game.items.ConstrainMovement == nil {
		return fmt.Errorf("action: pickup movement constraint is required")
	}
	if game.items.CaptureCharacterFrames == nil {
		return fmt.Errorf("action: scene-bound private receipt capture is required")
	}
	if game.items.SpawnRegionAvailable == nil || game.items.ConstrainCompanionSpawn == nil || game.items.CompanionSurfaceHeight == nil {
		return fmt.Errorf("action: companion region and collision admission are required")
	}

	definitions, err := loadQuests()
	if err != nil {
		return fmt.Errorf("quest definitions: %w", err)
	}
	masteryOverride, err := progression.BetaMasteryFromEnv()
	if err != nil {
		return err
	}
	game.deps.MasteryTotalOverride = masteryOverride
	// Port-only, not native: storage quick moves prefer a matching stack.
	game.deps.StorageAutoStack = action.StorageAutoStackFromEnv()
	// Port-only, not native: the Bandit Den scroll counts down half as long,
	// and the client's delay bar learns it at world entry.
	// Port-only, not native: the job-kill streak, its bonuses and bounty.
	game.items.MostWanted = action.MostWantedFromEnv()
	if game.items.MostWanted {
		log.Infof("job: most wanted streaks, bonuses and bounties ON (%s)", action.EnvMostWanted)
	}
	// Port-only, not native, temporary: Most Wanted's test pacing.
	game.items.MostWantedTest = action.MostWantedTestFromEnv()
	step, err := action.MostWantedTestStreakStepFromEnv()
	if err != nil {
		return err
	}
	game.items.MostWantedTestStreakStep = step
	if game.items.MostWantedTest {
		log.Infof("job: most wanted test pacing ON: %d kills a level, no anti-farm window, %d gold bounty a level (%s)",
			10/step, tunables.Current().Int(tunables.MostWantedTestBounty), action.EnvMostWantedTest)
	}
	// Port-only, not native: the bounty comes out of the victim's gold.
	game.items.MostWantedBountyTransfer = action.MostWantedBountyTransferFromEnv()
	if game.items.MostWantedBountyTransfer {
		log.Infof("job: most wanted bounties paid from the victim's gold (%s)", action.EnvMostWantedBountyTransfer)
	}
	// Port-only, not native: the killer hears its progress per counted kill.
	game.items.MostWantedStreakNotice = action.MostWantedStreakNoticeFromEnv()
	if game.items.MostWantedStreakNotice {
		log.Infof("job: most wanted progress notice per counted kill ON (%s)", action.EnvMostWantedStreakNotice)
	}
	// Port-only, not native: a companion's job kill counts for its owner.
	game.items.MostWantedCOSKills = action.MostWantedCOSKillsFromEnv()
	if game.items.MostWantedCOSKills {
		log.Infof("job: most wanted counts companion-delivered job kills ON (%s)", action.EnvMostWantedCOSKills)
	}
	// Port-only, not native: a trader's goods stay in the world when the
	// trader dies riding or disconnects in the field.
	game.items.TraderCargoDrop = action.TraderCargoDropFromEnv()
	if game.items.TraderCargoDrop {
		log.Infof("trade: transport cargo drops on rider death and field disconnect (%s)", action.EnvTraderCargoDrop)
	}
	// Port-only, not native: caravans rated by cargo gold, Jangan route profit.
	game.deps.TradeTiers = game.items.TradeTiers
	// SRO_TELEPORT_GRACE (port-only, not native): teleport arrivals get the
	// revival's untouchable grace from game-ready.
	game.items.TeleportGrace = action.TeleportGraceFromEnv()
	if game.items.TeleportGrace {
		log.Infof("action: teleport arrivals are untouchable for the revival's grace (%s, port-only, not native)", action.EnvTeleportGrace)
	}
	if game.items.TradeTiers {
		log.Infof("trade: stars by cargo gold and Jangan route profit (%s)", action.EnvTradeTiers)
	}
	// Port-only, not native: trader and hunter suits hide worn armour
	// (presentation, published to the client).
	game.deps.JobSuitHidesArmor = action.JobSuitHidesArmorFromEnv()
	if game.deps.JobSuitHidesArmor {
		log.Infof("job: trader and hunter suits hide worn armour (%s)", action.EnvJobSuitHidesArmor)
	}
	// Port-only, not native: level-20 thief suits wear the Moonlit Ash look
	// (presentation, published to the client).
	game.deps.MoonlitThiefSuit = action.MoonlitThiefSuitFromEnv()
	if game.deps.MoonlitThiefSuit {
		log.Infof("job: level-20 thief suits wear Moonlit Ash (%s)", action.EnvMoonlitThiefSuit)
	}
	game.items.FastDenReturn = action.FastDenReturnFromEnv()
	game.deps.FastDenReturn = game.items.FastDenReturn
	if game.items.FastDenReturn {
		log.Infof("return: Bandit Den scroll countdown halved (%s)", action.EnvFastDenReturn)
	}
	// Port-only, not native: a unique's attackers see its damage ranking.
	game.items.UniqueDamageRank = action.UniqueDamageRankFromEnv()
	if game.items.UniqueDamageRank {
		log.Infof("unique: damage ranking shown at each unique kill (%s)", action.EnvUniqueDamageRank)
	}
	// Port-only, not native: shard notices for seal drops, high plus and job
	// kills (announce.go).
	game.items.Announce = action.AnnouncementsFromEnv()
	if a := game.items.Announce; a.SoxDrop || a.Plus || a.JobKill || a.MostWanted {
		log.Infof("announce: seal drops %v, plus %v, job kills %v, most wanted %v", a.SoxDrop, a.Plus, a.JobKill, a.MostWanted)
	}
	// Port-only, not native: a party's thieves raise its EXP and gold.
	game.items.ThiefPartyBonus = action.ThiefPartyBonusFromEnv()
	if game.items.ThiefPartyBonus {
		log.Infof("party: +10%% EXP and gold per thief (%s)", action.EnvThiefPartyBonus)
	}
	// Port-only, not native: traders and their hunters earn more together.
	game.items.TradeJobBonus = action.TradeJobBonusFromEnv()
	if game.items.TradeJobBonus {
		log.Infof("trade: party traders and hunters share a gold bonus (%s)", action.EnvTradeJobBonus)
	}
	// Port-only, not native: no transport comes out during the Den countdown.
	game.items.DenNoTransportSummon = action.DenNoTransportSummonFromEnv()
	if game.items.DenNoTransportSummon {
		log.Infof("return: no transport summon during the Bandit Den countdown (%s)", action.EnvDenNoTransportSummon)
	}
	// Port-only, not native: a GM's /SETSPEED.
	game.items.GMSetSpeed = action.GMSetSpeedFromEnv()
	if game.items.GMSetSpeed {
		log.Infof("gm: /SETSPEED ON (%s)", action.EnvGMSetSpeed)
	}
	// Capture the Flag (native): the daily start and the house minimum.
	flagWar, err := action.FlagWarSettingsFromEnv()
	if err != nil {
		return err
	}
	game.items.FlagWar = flagWar
	log.Infof("flag war: daily start %02d:%02d UTC, minimum %d players (%s, %s)",
		flagWar.StartMinuteOfDay/60, flagWar.StartMinuteOfDay%60, flagWar.MinPlayers,
		action.EnvFlagWarStartUTC, action.EnvFlagWarMinPlayers)
	// A team death revives in place natively; "off" is the port-only town rule.
	teamDeathRevive, err := action.TeamDeathReviveFromEnv()
	if err != nil {
		return err
	}
	game.items.TeamDeathTownRule = !teamDeathRevive
	if game.items.TeamDeathTownRule {
		log.Infof("rebirth: team deaths follow the town rule (%s off)", action.EnvTeamDeathRevive)
	}
	// Port-only, not native: the bandits per caravan ambush.
	banditCap, err := caravan.BanditCapFromEnv()
	if err != nil {
		return err
	}
	game.items.CaravanBanditCap = banditCap
	if banditCap > 0 {
		log.Infof("caravan: at most %d bandits per ambush (%s)", banditCap, caravan.EnvBanditCap)
	}
	game.items.CaravanStarAmbush = caravan.StarAmbushFromEnv()
	if game.items.CaravanStarAmbush {
		log.Infof("caravan: a fixed ambush per star replaces the rolled count (%s)", caravan.EnvStarAmbush)
	}
	game.items.TransportFollow = action.TransportFollowFromEnv()
	if game.items.TransportFollow {
		log.Infof("cos: a dismounted trade transport follows its trader (%s)", action.EnvTransportFollow)
	}
	game.items.TransportFollowTether = action.TransportFollowTetherFromEnv()
	if game.items.TransportFollow && game.items.TransportFollowTether {
		log.Infof("cos: a following transport keeps its trader within 100 m (%s)", action.EnvTransportFollowTether)
	}
	// Port-only, not native: town teleports admit a trader's transport.
	game.items.JobGoodsTeleport = action.JobGoodsTeleportFromEnv()
	if game.items.JobGoodsTeleport {
		log.Infof("job: town gates and return scrolls admit a trader's transport (%s)", action.EnvJobGoodsTeleport)
	}
	if game.deps.StorageAutoStack {
		log.Infof("storage: quick moves auto-stack ON (%s)", action.EnvStorageAutoStack)
	}
	stats := progression.NewRuntime(game.deps)
	if err = applyLevelCap(stats); err != nil {
		return err
	}
	// The configured cap reaches the party-matching bound directly and
	// the client through the world-entry blob (publishedLevelCap keeps
	// the native blob byte-identical with the expansion off).
	game.matches.LevelLimit = stats.LevelLimit
	game.deps.LevelCap = publishedLevelCap(stats.LevelLimit)
	stats.MasteryTotalOverride = masteryOverride
	if masteryOverride != 0 {
		log.Infof("progression: beta total mastery allowance %d for both races (%s)", masteryOverride, progression.EnvBetaMastery)
	}
	stats.Growth = progression.BetaGrowthFromEnv()
	game.items.AttackPetExpansion = companion.AttackPetExpansionEnabled()
	if stats.Growth.Enabled {
		log.Infof("progression: beta growth ON (%s): every level at the level-%d kill pace, skill EXP at that pace x%d, drop passes x%d (at most %d ordinary items a kill, 0 = no cap), gold x%d, rare (SoX) x%d", progression.EnvBetaGrowth, progression.BetaReferenceLevel, stats.Growth.SkillExpRate, stats.Growth.DropRate, stats.Growth.DropCap, stats.Growth.GoldRate, stats.Growth.RareRate)
		game.items.DropPassRate = stats.Growth.DropRate
		game.items.DropCap = stats.Growth.DropCap
		// Port-only, not native: per-family rates inside the beta rate.
		mix, err := progression.BetaDropMixFromEnv()
		if err != nil {
			return err
		}
		game.items.DropMix = mix
		if mix != nil {
			log.Infof("progression: beta drop mix ON (%s): %v, other families x%d", progression.EnvBetaDropMix, mix, stats.Growth.DropRate)
		}
		game.items.GoldRate = stats.Growth.GoldRate
		game.items.RareRate = stats.Growth.RareRate
		// Attack pets grow at their owner's level pace (owner decision 2026-10-11).
		growth, deps := stats.Growth, game.deps
		game.items.PetExpPace = func(ownerLevel int64) float64 { return growth.ExpPace(deps.LevelData(), ownerLevel) }
		game.items.PartyShareFloor = true
	}
	// Port-only, not native, beta balance (owner 2026-10-13): degree-9
	// alchemy goods only from the shop.
	game.items.ShopOnly = progression.BetaShopOnlyDegree9FromEnv()
	if game.items.ShopOnly.Enabled() {
		log.Infof("progression: degree-9 Lucky Powder, magic and attribute stones no longer drop (%s)", progression.EnvBetaShopOnlyDegree9)
	}
	// Port-only, not native, beta balance (owner 2026-10-13): per-seal rare
	// rates on top of RareRate. Off is today's rates.
	game.items.SealRates = progression.BetaSealRatesFromEnv()
	if game.items.SealRates.Enabled() {
		log.Infof("progression: beta seal rates ON (%s): degrees 1-8 Star/Moon/Sun x3, degree 9 Star x3 Moon x2 Sun x1, on top of rare (SoX) x%d", progression.EnvBetaSealRates, max(game.items.RareRate, 1))
	}
	// Port-only, not native, beta balance (owner 2026-10-20): the degree-9
	// Sun class of the rare table widens by the rate. The event rule still
	// wins over it - Sun then drops nothing, which must not be silent.
	game.items.SunRate = progression.SunRateFromEnv()
	if game.items.SunRate.Active() {
		log.Infof("loot: degree-9 Seal of Sun drops at class mass up to x%d on levels 76-90, capped at the Moon class (#1605 P1) (%s=%d)", game.items.SunRate.MassMultiplier(), progression.EnvSunDropRate, game.items.SunRate.Rate())
		if game.items.SunEventOnly.Enabled() {
			log.Warnf("loot: %s is on while %s=%d: the event rule excludes every degree-9 Sun drop, so the rate widens a class that drops nothing; turn one of them off", progression.EnvSunEventOnly, progression.EnvSunDropRate, game.items.SunRate.Rate())
		}
	}
	stats.Withdrawal = game.items.WithdrawalHooks()
	stats.BaseStats = game.deps.PlayerBaseStats
	stats.RecoverLevelVitals = func(character *enterworld.Character) error {
		return game.items.RecoverLevelVitals(game.divisionID, character)
	}
	quests, err := quest.NewRuntime(game.deps, definitions, stats.ExperienceUpdater())
	if err != nil {
		return fmt.Errorf("quest runtime: %w", err)
	}
	game.quests = quests
	game.items.InstanceEventSink = func(event action.InstanceEvent) {
		quests.DispatchInstanceEvent(quest.InstanceEvent{
			Kind:       quest.InstanceEventKind(event.Kind),
			Division:   event.Division,
			Definition: event.Definition,
			World:      event.World,
			Name:       event.Name,
			Gid:        event.Gid,
			Accepted:   event.Accepted,
		})
	}
	if quests.InstantInventoryExpansion = quest.InstantInventoryExpansionFromEnv(); quests.InstantInventoryExpansion {
		log.Infof("quest: instant inventory expansion ON (%s, port-only): reward slots are usable at the turn-in", quest.EnvInstantInventoryExpansion)
	}
	game.deps.NormalizeEntryQuests = quests.NormalizeEntryRecords
	if err := game.validateQuestMarkerRoster(); err != nil {
		return err
	}
	// The quest runtime's doors and the item lane's quest hooks live
	// with the quest composition (wiring_quests.go).
	game.wireQuestDoors(quests)
	game.items.UpdateExperience = stats.ExperienceUpdater()
	game.items.RefundExperience = stats.ExperienceRefundUpdater()
	game.items.RecallStatPoints = stats.StatRecallUpdater()
	game.items.ApplyDeathPenalty = stats.DeathPenaltyUpdater()
	game.items.UpdateJobExperience = stats.JobExperienceUpdater()
	// Delivery resolves sessions from their bindings alone and never reads the
	// character store: the action runtime publishes from inside character
	// doors (the store's write lock), where any store read would deadlock.
	game.items.PushCharacterFrames = func(divisionID, characterName string, frames []wire.Frame) {
		for _, session := range hub.CharacterSessions(divisionID, characterName) {
			action.SendFrames(session, frames)
		}
	}
	// A member's guild frames: 0x3B29 deltas only while the session holds
	// that guild's block (guild/baseline.go), every other frame as is, in
	// one enqueue.
	game.items.PushGuildFrames = func(divisionID, characterName string, guildID int64, frames []wire.Frame) {
		for _, session := range hub.CharacterSessions(divisionID, characterName) {
			guild.WithBlock(session, guildID, func(holds bool) {
				if kept := guild.FilterDeltas(holds, frames); len(kept) > 0 {
					action.SendFrames(session, kept)
				}
			})
		}
	}
	game.items.PushDivisionPeerFrames = func(divisionID, exceptCharacterName string, frames []wire.Frame) {
		if exceptCharacterName != "" {
			// A character without a bound session is not in the world, so
			// nobody can observe it and its frames have no audience.
			sources := hub.CharacterSessions(divisionID, exceptCharacterName)
			if len(sources) == 0 {
				return
			}
			if sourceGID, ok := sources[0].CharacterObjectID(); ok {
				action.BroadcastObservedFrames(hub, divisionID, sources[0].ID, sourceGID, frames)
			}
			return
		}
		for _, session := range hub.SessionsInDivision(divisionID) {
			if _, _, ok := session.CharacterBinding(); ok {
				action.SendFrames(session, frames)
			}
		}
	}

	game.items.PushShardPeerFrames = func(divisionID, exceptCharacterName string, frames []wire.Frame) {
		except := map[uint64]bool{}
		for _, session := range hub.CharacterSessions(divisionID, exceptCharacterName) {
			except[session.ID] = true
		}
		for _, session := range hub.SessionsInDivision(divisionID) {
			if _, _, ok := session.CharacterBinding(); ok && !except[session.ID] && session.WorldReady() {
				action.SendFrames(session, frames)
			}
		}
	}
	game.items.ChatRestricted = func(divisionID, characterName string) bool {
		for _, session := range hub.CharacterSessions(divisionID, characterName) {
			if restriction.Report(session, transport.CommandRestrictionChat) {
				return true
			}
		}
		return false
	}

	game.items.PushMonsterCast = func(division string, source uint32, result simulation.MonsterAttackResult) {
		public := make([]wire.Frame, len(result.Frames))
		for i, f := range result.Frames {
			public[i] = wire.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope}
		}
		action.BroadcastObservedFrames(hub, division, 0, source, public)
		for _, recipient := range result.Private {
			private := make([]wire.Frame, len(recipient.Frames))
			for i, f := range recipient.Frames {
				private[i] = wire.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope}
			}
			if len(private) > 0 {
				game.items.PushCharacterFrames(division, recipient.CharacterName, private)
			}
		}
	}

	enterworld.Register(hub, game.deps)
	game.items.Register(hub)
	game.movement.Register(hub)
	game.movement.RegisterActions(hub)
	stats.Register(hub)
	quest.Register(hub, quests)

	community.Register(hub, game.deps)
	game.friends = community.RegisterFriend(hub, game.deps, game.presence)
	// The friend request's consent shares 0x3012 with game-ready
	// (enterworld.ConsentAnswerLength).
	game.deps.FriendConsent = game.friends.Consent
	community.RegisterLetter(hub, game.deps, game.presence)
	game.chat = chat.Register(hub, game.deps, game.presence, game.parties.Registry())
	// The website's public chat feed (port-only): beta chat, the Global
	// Chatting item and notices; a GM ban purges the player's lines.
	game.chatFeed = chatfeed.New()
	game.chat.Feed = game.chatFeed
	game.items.PublicChat = game.chatFeed
	game.chat.Unions = game.unions
	game.chat.Stalls = game.items.Stalls
	gmcommand.Register(hub, game.deps, game.presence, game.items)
	game.matches.Register(hub)
	game.parties.Register(hub)
	guild.Register(hub, game.deps, game.presence, game.items, game.unions)
	game.unions.Register(hub)
	game.guildWars.Register(hub)
	game.guildInvites.Register(hub)
	game.mentorInvites.LevelData = game.deps.Levels
	game.mentorInvites.GrantGraduateExperience = func(divisionID string, charID int64, expDelta int64) {
		character := game.deps.CharacterByID(divisionID, charID)
		if character == nil {
			return
		}
		result := stats.GrantUnscaledExperience(character, expDelta)
		if len(result.Frames) > 0 {
			game.items.PushCharacterFrames(divisionID, character.Name, result.Frames)
		}
		if len(result.Broadcast) > 0 {
			game.items.PushDivisionPeerFrames(divisionID, character.Name, result.Broadcast)
		}
	}
	game.mentorInvites.SyncMasterHonorBuffs = game.items.SyncTrainingCampHonorBuffs
	game.items.HonorPoints = game.mentorInvites.HonorPoints
	game.items.SpendHonorPoints = game.mentorInvites.SpendHonorPoints
	game.mentorInvites.Register(hub)
	return nil
}
