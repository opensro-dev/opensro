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
	"fmt"
	log "github.com/sirupsen/logrus"
	"time"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/game/siege"
	livepresence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/social/match"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/social/union"
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
	parties       *party.Runtime
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
	deps, err := newBootstrapDependencies(
		authorityStore,
		devPaths,
		characterRoster,
		ownedShard,
		textdata,
	)
	if err != nil {
		return nil, err
	}
	items := action.NewRuntime(deps, deps.MonsterState)
	items.CaptureCharacterFrames = func(division, name string) func([]wire.Frame) {
		return captureCharacterFrames(ts.Hub, division, name)
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
	if err := items.ConfigurePortals(devPaths.TextdataDir); err != nil {
		return nil, fmt.Errorf("portal catalogue: %w", err)
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
	if err := items.ConfigureCommerce(devPaths.TextdataDir); err != nil {
		return nil, fmt.Errorf("commerce catalogue: %w", err)
	}
	items.ConfigureStorage(authorityStore)
	// SRO_BETA_SILK (port-only, not native): silk earned per in-world hour,
	// credited into the account's real wallet (action/betasilk.go).
	betaSilkRate, err := action.BetaSilkFromEnv()
	if err != nil {
		return nil, err
	}
	betaSilk := action.NewBetaSilk(authorityStore, betaSilkRate)
	var mallAuthority domain.MallAuthority = authorityStore
	if betaSilk != nil {
		mallAuthority = betaSilk
		deps.EntryBetaSilk = betaSilk.Starter
		log.Infof("item mall: beta silk ON (%s): %d silk per in-world hour", action.EnvBetaSilk, betaSilkRate)
	}
	if err := items.ConfigureMall(devPaths.TextdataDir, mallAuthority); err != nil {
		return nil, fmt.Errorf("mall catalogue: %w", err)
	}
	deps.SceneReferenceFrames = items.CommerceReferenceSeed
	deps.ReconcileSceneObjects = items.ReconcileGroundScope
	deps.ExtraRefItemCodenames = items.GroundRefItemCodenames
	deps.StaticRefItemCodenames = items.StaticRefItemCodenames
	deps.ExtraMagicOptionIDs = items.AlchemyMagicOptionIDs
	deps.AvatarMagicOptions = items.AvatarMagicOptions
	items.Ground.Restore(authorityStore.GroundSnapshotForRestore())
	authorityStore.AttachGround(items.Ground)

	logAuthorityReady(authorityStore, deps)
	appendGroundObjectRows(deps, items)

	water := movement.NewAuthorityValidator(worldAuthorityDir)
	if err := water.ValidateSecurityAssets(); err != nil {
		return nil, err
	}
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
	movementRuntime.EnableGroundWalk()
	items.ConstrainMovement = movementRuntime.ConstrainMovement
	items.SpawnRegionAvailable = water.SpawnRegionAvailable
	items.ConstrainCompanionSpawn = water.ConstrainCompanionSpawn
	items.CompanionSurfaceHeight = water.WalkableSpawnHeightAt
	items.ConstrainWalk = movementRuntime.ConstrainMovementFrom
	items.PlanCompanionRoute = simulation.PetRouteFromMonster(water.PlanMonsterRoute)
	items.AdmitGroundWalk = movementRuntime.AdmitGroundWalk
	items.LineOfSight = movementRuntime.LineOfSight
	items.ResolveNavOwner = water.ResolveNavOwner
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
	guildWars.InFortress = items.GuildWarMasterInFortress
	unions.GuildWars = warAuthority

	communitySeeds := community.SeedFramesFunc(presence, deps.Letters)
	deps.CommunitySeedFramesFor = func(
		divisionID string,
		character *enterworld.Character,
	) []enterworld.Packet {
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
	parties.UseMasteries(party.MasteriesFromEnv())
	if party.MasteriesFromEnv() {
		log.Infof("party: member rows carry the two main masteries ON (%s=off disables)", party.EnvPartyMasteries)
	}
	items.NextPartyLootMember = parties.Registry().NextLootMember
	items.RewardActorPresent = func(division, name string) bool {
		s, ok := presence.SessionByName(division, name)
		return ok && s.WorldReady()
	}
	items.RewardParties = func(division string) []action.RewardParty {
		var out []action.RewardParty
		for _, p := range parties.Registry().RewardSnapshots(division) {
			row := action.RewardParty{Order: p.ObjectOrder, Options: p.OptionBits}
			for _, m := range p.Members {
				row.Members = append(row.Members, m.MemberID)
			}
			out = append(out, row)
		}
		return out
	}
	items.CanPickupOwnedDrop = func(divisionID, characterName string, ownerJID uint32) bool {
		snapshot, ok := parties.Registry().PartyOf(divisionID, characterName)
		if !ok || snapshot.OptionBits&party.PartyOptionItemShare == 0 {
			return false
		}
		for _, member := range snapshot.Members {
			if member.MemberID == ownerJID {
				return true
			}
		}
		return false
	}
	guildInvites := guild.NewInviteRuntime(deps, presence)
	guildInvites.Unions = unions
	mentorInvites := mentor.NewInviteRuntime(deps, presence)
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
		rows = append(rows, enterworld.GroundObjectListRows(items.CharacterGroundItems(divisionID, character))...)
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
	parties.AddConsentArm(exchanges)
	parties.AddConsentArm(guildInvites)
	parties.AddConsentArm(unions)
	parties.AddConsentArm(mentorInvites)
	parties.AddConsentArm(resurrections)

	// A player holds one unanswered proposal at a time, whatever its
	// lane (TransactionMgr_InsertUnique 46F420): every lane refuses to
	// propose while another's proposal waits.
	partyPending := parties.Registry().HasPendingInviteFor
	guildInvites.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name)
	}
	unions.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name)
	}
	mentorInvites.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name)
	}
	items.ProposalPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name)
	}
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
	if game.deps.StorageAutoStack {
		log.Infof("storage: quick moves auto-stack ON (%s)", action.EnvStorageAutoStack)
	}
	stats := progression.NewRuntime(game.deps)
	stats.MasteryTotalOverride = masteryOverride
	if masteryOverride != 0 {
		log.Infof("progression: beta total mastery allowance %d for both races (%s)", masteryOverride, progression.EnvBetaMastery)
	}
	stats.Growth = progression.BetaGrowthFromEnv()
	if stats.Growth.Enabled {
		log.Infof("progression: beta growth ON (%s): every level at the level-%d kill pace, skill EXP at that pace x%d, drop passes x%d (at most %d ordinary items a kill, 0 = no cap), gold x%d, rare (SoX) x%d", progression.EnvBetaGrowth, progression.BetaReferenceLevel, stats.Growth.SkillExpRate, stats.Growth.DropRate, stats.Growth.DropCap, stats.Growth.GoldRate, stats.Growth.RareRate)
		game.items.DropPassRate = stats.Growth.DropRate
		game.items.DropCap = stats.Growth.DropCap
		game.items.GoldRate = stats.Growth.GoldRate
		game.items.RareRate = stats.Growth.RareRate
		// Attack pets grow at their owner's level pace (owner decision 2026-10-11).
		growth, deps := stats.Growth, game.deps
		game.items.PetExpPace = func(ownerLevel int64) float64 { return growth.ExpPace(deps.LevelData(), ownerLevel) }
		game.items.PartyShareFloor = true
	}
	stats.Withdrawal = game.items.WithdrawalHooks()
	stats.BaseStats = game.deps.PlayerBaseStats
	stats.RecoverLevelVitals = func(character *enterworld.Character) error {
		return game.items.RecoverLevelVitals(game.divisionID, character)
	}
	quests, err := quest.NewRuntime(
		game.deps,
		definitions,
		stats.ExperienceUpdater(),
	)
	if err != nil {
		return fmt.Errorf("quest runtime: %w", err)
	}
	game.quests = quests
	if quests.InstantInventoryExpansion = quest.InstantInventoryExpansionFromEnv(); quests.InstantInventoryExpansion {
		log.Infof("quest: instant inventory expansion ON (%s, port-only): reward slots are usable at the turn-in", quest.EnvInstantInventoryExpansion)
	}
	game.deps.NormalizeEntryQuests = quests.NormalizeEntryRecords
	if err := game.validateQuestMarkerRoster(); err != nil {
		return err
	}
	quests.PlanInventory = game.items.PlanQuestInventory
	quests.SpawnQuestMonster = func(character *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		return game.items.SpawnQuestMonster(game.divisionID, character, request)
	}
	game.items.CanPlaceQuestTrap = quests.CanPlaceTrap
	game.items.CaptureQuestTrap = quests.CaptureQuestTrap
	game.items.UpdateQuestInventory = quests.InventoryUpdater()
	game.items.UpdateQuestKill = quests.KillUpdater()
	game.items.QuestTravelBlocks = quests.TravelBlocks
	game.items.AdvanceQuestMinute = quests.AdvanceMinute
	game.items.AdvanceQuestItem = quests.AdvanceItemUse
	game.items.ForgetQuestItem = quests.ForgetItemUse
	game.items.UseQuestItem = quests.BeginItemUse
	game.items.ReleaseQuestCapturesOnDeath = quests.ReleaseCapturesOnDeath
	game.items.AdvanceQuestCalendar = quests.AdvanceCalendar
	game.items.QuestMonsterDrops = quests.MonsterDrops
	game.items.NpcQuests = action.NpcQuestHooks{
		Prepare: quests.PrepareNpcQuest,
		Options: func(divisionID string, character *enterworld.Character, npcCodename string) []action.NpcQuestOption {
			var rows []quest.NpcOption
			var resuscitation bool
			game.deps.Read(divisionID, func() {
				rows = quests.OptionsForNpc(character, npcCodename)
				resuscitation = quest.ResuscitationAvailable(character, npcCodename)
			})
			out := make([]action.NpcQuestOption, 0, len(rows))
			for _, row := range rows {
				pages := make([]action.NpcDialogPage, 0, len(row.Pages))
				for _, page := range row.Pages {
					pages = append(pages, action.NpcDialogPage{PromptSymbol: page.PromptSymbol, ReplySymbol: page.ReplySymbol,
						RefuseSymbol: page.RefuseSymbol, RefuseResponseSymbol: page.RefuseResponseSymbol})
				}
				branches := make([]action.NpcDialogBranch, 0, len(row.Branches))
				for _, branch := range row.Branches {
					branches = append(branches, action.NpcDialogBranch{Codename: branch.Codename, ReplySymbol: branch.ReplySymbol,
						AcceptResponseSymbol: branch.AcceptResponseSymbol})
				}
				out = append(out, action.NpcQuestOption{
					Codename: row.Codename, TitleSymbol: row.TitleSymbol,
					PromptSymbol: row.PromptSymbol, Complete: row.Complete,
					AcceptResponseSymbol: row.AcceptResponseSymbol, DenyResponseSymbol: row.DenyResponseSymbol,
					Pages: pages, Branches: branches, Informational: row.Informational, SideTalk: row.SideTalk,
					AcceptRowSymbol: row.AcceptRowSymbol,
				})
			}
			if resuscitation {
				out = append(out, action.NpcQuestOption{
					Codename: quest.ResuscitationService, TitleSymbol: "SN_TALK_QSP_ALL_POTION_1_07",
					PromptSymbol: "SN_TALK_QSP_ALL_POTION_1_00", Immediate: true,
				})
			}
			return out
		},
		Accept: func(character *enterworld.Character, codename string) ([]wire.Frame, error) {
			result, err := quests.StartQuest(character, codename)
			return result.Frames, err
		},
		Finish: func(character *enterworld.Character, codename, npcCodename string) ([]wire.Frame, error) {
			if codename == quest.ResuscitationService {
				result, err := quests.OpenResuscitation(character, npcCodename)
				return result.Frames, err
			}
			result, err := quests.AdvanceNpcQuest(character, codename, npcCodename)
			return result.Frames, err
		},
		Refuse: func(character *enterworld.Character, codename string) ([]wire.Frame, error) {
			result, err := quests.RefuseQuestOffer(character, codename)
			return result.Frames, err
		},
	}
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
	game.items.PushDivisionPeerFrames = func(divisionID, exceptCharacterName string, frames []wire.Frame) {
		if exceptCharacterName != "" {
			// A character without a bound session is not in the world, so
			// nobody can observe it and its frames have no audience.
			sources := hub.CharacterSessions(divisionID, exceptCharacterName)
			if len(sources) == 0 {
				return
			}
			if sourceGID, ok := sources[0].CharacterObjectID(); ok {
				action.BroadcastObservedFrames(hub, divisionID, 0, sourceGID, frames)
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
	community.RegisterFriend(hub, game.deps, game.presence)
	community.RegisterLetter(hub, game.deps, game.presence)
	game.chat = chat.Register(hub, game.deps, game.presence, game.parties.Registry())
	game.chat.Unions = game.unions
	game.chat.Stalls = game.items.Stalls
	gmcommand.Register(hub, game.deps, game.presence, game.items)
	game.matches.Register(hub)
	game.parties.Register(hub)
	guild.Register(hub, game.deps, game.presence, game.items, game.unions)
	game.unions.Register(hub)
	game.guildWars.Register(hub)
	game.guildInvites.Register(hub)
	game.mentorInvites.Register(hub)
	return nil
}

/*
================
captureCharacterFrames

Capture is read-only under the action division lock. The returned delivery
runs outside that lock so queue failure can synchronously retire gameplay.
================
*/
func captureCharacterFrames(hub *transport.Hub, division, name string) func([]wire.Frame) {
	type recipient struct {
		session  *transport.Session
		revision uint64
	}
	var recipients []recipient
	for _, session := range hub.CharacterSessions(division, name) {
		revision, valid := session.SceneReceiptRevision()
		if valid {
			recipients = append(recipients, recipient{session: session, revision: revision})
		}
	}
	if len(recipients) == 0 {
		return nil
	}
	return func(frames []wire.Frame) {
		batch := make([]transport.Frame, len(frames))
		for i, frame := range frames {
			batch[i] = transport.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: transport.ScopeChanges(frame.Scope)}
		}
		for _, recipient := range recipients {
			if err := recipient.session.SendSceneReceiptBatch(recipient.revision, batch); err != nil {
				log.WithError(err).WithField("session", recipient.session.ID).Debug("action: captured private receipt refused")
			}
		}
	}
}
