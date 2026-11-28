/*
===========================================================================

wiring_register.go - one shard's gameplay lanes onto the transport hub

newGameplayPlane (wiring_gameplay.go) builds the shard's owners; register
installs their cross-owner callbacks and then registers each lane's
transport handlers. Split from wiring_gameplay.go on that boundary:
construction there, hub registration here.

===========================================================================
*/
package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/social/chatfeed"
	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/shadowmute"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
	"opensro.online/server/internal/transport"
)

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
	// SRO_BETA_MASTERY and SRO_BETA_MASTERY_EARLY (port-only, not native):
	// wiring_mastery.go.
	if err = readMasteryAllowance(game.deps); err != nil {
		return err
	}
	// Port-only, not native (owner, 2026-11-07, #2769): world entry raises
	// a character to the 2.5-page inventory base, never lowers one.
	game.deps.InventoryPages = enterworld.InventoryPagesFromEnv()
	if game.deps.InventoryPages {
		log.Infof("inventory: 2.5-page base ON (%s); off is the native one page", enterworld.EnvInventoryPages)
	}
	// Port-only, not native: storage quick moves prefer a matching stack.
	game.deps.StorageAutoStack = action.StorageAutoStackFromEnv()
	// Port-only, not native (owner, 2026-11-07): item links in player chat;
	// the client learns the per-line limit at world entry.
	if chat.ItemLinksFromEnv() {
		game.deps.ChatItemLinks = tunables.Current().Int(tunables.ChatItemLinksPerMessage)
	}
	if err := game.wireJobRules(); err != nil {
		return err
	}
	// Port-only, not native: a trader's goods stay in the world when the
	// trader dies riding or disconnects in the field.
	game.items.TraderCargoDrop = action.TraderCargoDropFromEnv()
	if game.items.TraderCargoDrop {
		log.Infof("trade: transport cargo drops on rider death and field disconnect (%s)", action.EnvTraderCargoDrop)
	}
	// Port-only, not native: the client's sort button on the bag, the
	// warehouses and companion bags (presentation; the moves are ordinary).
	game.deps.InventorySort = action.InventorySortFromEnv()
	if game.deps.InventorySort {
		log.Infof("items: inventory sort button ON (%s)", action.EnvInventorySort)
	}
	// Port-only, not native: ten-billion stall prices and wider warehouse
	// deposits (a billion, or the shard's chosen value).
	game.items.RaisedGoldCaps = action.RaisedGoldCapsFromEnv()
	game.items.StorageGoldDepositLimit = action.StorageGoldDepositLimitFromEnv()
	game.deps.RaisedGoldCaps = game.items.RaisedGoldCaps
	game.deps.StorageGoldDepositLimit = game.items.StorageGoldDepositLimit
	if game.items.RaisedGoldCaps || game.items.StorageGoldDepositLimit != action.NativeStorageGoldDepositLimit {
		log.Infof("trade: stall prices to %d and warehouse deposits to %d ON (%s)",
			action.StallPriceLimitFromEnv(), game.items.StorageGoldDepositLimit, action.EnvRaisedGoldCaps)
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
	// Port-only, not native: the Bandit Den scroll counts down half as long,
	// and the client's delay bar learns it at world entry.
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
	// Port-only, not native: a GM's /WEATHER (owner 2026-11-26).
	game.items.GMWeather = action.GMWeatherFromEnv()
	if game.items.GMWeather {
		log.Infof("gm: /WEATHER ON (%s)", action.EnvGMWeather)
	}
	// Port-only, not native: a GM's /FORGE item forge (issue #1980).
	game.items.GMItemForge = action.GMItemForgeFromEnv()
	if game.items.GMItemForge {
		log.Infof("gm: /FORGE ON (%s); forged items are bound", action.EnvGMItemForge)
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
	// Styria Clash (port-only, not native, #3648): the daily start and
	// the smallest side, read whenever the engine is configured.
	styriaSettings, err := action.StyriaSettingsFromEnv()
	if err != nil {
		return err
	}
	game.items.Styria = styriaSettings
	if styria.Enabled() {
		log.Infof("styria clash: daily start %02d:%02d UTC, minimum %d per side (%s, %s)",
			styriaSettings.StartMinuteOfDay/60, styriaSettings.StartMinuteOfDay%60, styriaSettings.MinPerSide,
			action.EnvStyriaStartUTC, action.EnvStyriaMinPerSide)
	}
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
	// Guild GP hunting accrual (issue #4051): the SP mint notes each
	// whole SP into the tracker; the tick drains it through the credit
	// door and session close credits the remainder while still guilded.
	game.gpAccrual = guild.NewGpAccrual(game.deps, game.presence)
	stats.SPMintedHook = func(character *enterworld.Character, spMinted int64) {
		game.gpAccrual.NoteSPMinted(game.divisionID, character, spMinted)
	}
	// The configured cap reaches the party-matching bound directly and
	// the client through the world-entry blob (publishedLevelCap keeps
	// the native blob byte-identical with the expansion off).
	game.matches.LevelLimit = stats.LevelLimit
	game.deps.LevelCap = publishedLevelCap(stats.LevelLimit)
	applyMasteryAllowance(stats, game.deps)
	stats.Growth = progression.BetaGrowthFromEnv()
	game.items.PetInventoryPages, err = companion.PetInventoryPagesFromEnv()
	if err != nil {
		return err
	}
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
	game.deps.NormalizeEntryQuests = quests.NormalizeEntryRecords
	game.deps.RemoveExpiredEventBoxes = game.items.RemoveExpiredEventItems
	game.deps.RemoveExpiredRentItems = game.items.RemoveExpiredRentItems
	game.deps.EntryEventFrames = game.items.EntryEventFrames
	game.deps.EntryEventGroups = game.items.EntryEventGroups
	game.deps.SceneWeatherFrames = game.items.SceneWeatherFrames
	if err := game.wireQuestRoster(quests); err != nil {
		return err
	}
	// The quest runtime's doors and the item lane's quest hooks live
	// with the quest composition (wiring_quests.go).
	game.wireQuestDoors(quests)
	game.items.UpdateExperience = stats.ExperienceUpdater()
	// The Academy cumulated buff earns and drains on each member's kill
	// share (#2270); the runtime's ledger keeps it door-free in the grant.
	game.items.TrainingCampKill = game.mentorInvites.CampKillGrant
	game.items.RefundExperience = stats.ExperienceRefundUpdater()
	game.items.RecallStatPoints = stats.StatRecallUpdater()
	game.items.RecallSkills = stats.SkillRecallUpdater()
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
	// Port-only, not native: the staff's chat shadow-mute list, kept beside
	// the shard's other operator stores (shadowmute).
	shadowMutes := shadowmute.Open(store.DirForShardFromEnv(game.divisionID), store.WriteFileAtomic)
	game.chat.ShadowMuted = shadowMutes.Muted
	game.items.ChatShadowMuted = shadowMutes.Muted
	// The GM's moderation commands (gmcommand/moderation.go): the
	// shadow-mute pair always, the rest behind SRO_GM_MODERATION.
	game.gmModeration = &gmcommand.Moderation{Enabled: gmcommand.GMModerationFromEnv(), ShadowMutes: shadowMutes, Logout: game.items}
	if game.gmModeration.Enabled {
		log.Infof("gm: /MUTE, /UNMUTE, /BLOCKLOGOUT and /ALLOWLOGOUT ON (%s)", gmcommand.EnvGMModeration)
	}
	// Port-only, not native: the staff's moderation chat log (chatlog),
	// daily files beside the mute list in the shard's authority
	// directory - never the public site or API. Retention is
	// SRO_CHAT_LOG_RETAIN_DAYS (default 30).
	moderationLog := chatlog.Open(store.DirForShardFromEnv(game.divisionID), chatlog.RetainDaysFromEnv())
	game.chat.ModerationLog = moderationLog
	game.items.ModerationLog = moderationLog
	wireChatReports(game)
	game.installEventBotChat()
	if err := wireContributorTitles(game); err != nil {
		return err
	}
	game.wireCaravanChat()
	wirePlusFailNotice(game)
	game.chat.Unions = game.unions
	game.chat.Stalls = game.items.Stalls
	if game.deps.ChatItemLinks > 0 {
		links := &chat.ItemLinks{Source: game.items, Limit: int(game.deps.ChatItemLinks)}
		game.chat.ItemLinks = links
		game.items.ChatItemLinks = links
		log.Infof("chat: item links ON, %d per line (%s)", links.Limit, chat.EnvChatItemLinks)
	}
	// /FINDUSER reads the live region through the same owner (an
	// optional port: a missing method would silently fall back to the
	// saved row, so the build pins it).
	var _ gmcommand.LivePositions = game.items
	gmcommand.Register(hub, game.deps, game.presence, gmLaneOwner{Runtime: game.items, desk: game.gmModeration})
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
