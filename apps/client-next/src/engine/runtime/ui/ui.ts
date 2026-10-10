/*
===========================================================================

ui.ts - retained UI state, input routing and scene publication

Owns focus, window state and the UI resource children. Gameplay arrives as a
read-only snapshot; actions leave through the session command port. Resource
children are stepped every frame, even while their screens are hidden.
Visibility gates new requests, never collection of outstanding work.

===========================================================================
*/

// CGInterface_ExecuteActionCommand 695420 case 1006: trade with the
// selected player.
const ACTION_EXCHANGE = 1006;
// 695420 case 9 opens the own stall's title prompt, case 0xD toggles the
// stall network window (CGInterface_ToggleStallNetworkWindow 69DB80).
const ACTION_STALL = 1009;
const ACTION_STALL_NETWORK = 1013;
// 5A2890: a title CStringCheck_IsTextAllowed refuses raises 0x0A/0x38.
const STALL_TITLE_REFUSED = "UIIT_MSG_FLEAMARKET_ERR_NOT_ALLOWED_FMARKETNAME";
const STALL_PROMPT_TEXT = "stall-prompt-text";
const STALL_PROMPT_QUANTITY = "stall-prompt-quantity";
const STALL_PROMPT_PRICE = "stall-prompt-price";
const STALL_CHAT_TEXT = "stall-chat-text";
// CIFChatModule rows are 16 pixels; the input row sits under them.
const STALL_CHAT_ROW = 16;
import { ACTION_FORTRESS_RETURN } from "@/engine/foundation/gameplay/fortress-return";
import {
	companionItemTargetCommand,
	armsItemTargetCursor,
	isReverseReturnScroll
} from "@/engine/foundation/gameplay/cos-item-use";
import {
	createStoragePanel,
	firstFreeSlot,
	storageQuickMove,
	storagePages,
	storageSlotControlId
} from "@/engine/foundation/ui/storage-panel";
import {
	STORAGE_GOLD_DEPOSIT,
	STORAGE_GOLD_WITHDRAW,
	STORAGE_MOVE_DEPOSIT,
	STORAGE_MOVE_ROOM,
	STORAGE_MOVE_WITHDRAW,
	STORAGE_PAGE_SLOTS
} from "@/engine/foundation/gameplay/storage-room";
import { berserkHud, berserkEntryFlash } from "@/engine/foundation/ui/berserk-hud";
import { resolveTextOverlaps } from "@/engine/foundation/rendering/ui-glyphs";
import { portalMenu } from "@/engine/foundation/gameplay/portal";
import { restoreSlotEntry } from "@/engine/foundation/gameplay/commerce";
import type { BugReportControl } from "@/engine/contracts/bug-report";
import { equipmentDropSlot } from "@/engine/foundation/gameplay/equipment-drop";
import { itemEquipmentOverlay, equipmentWarningUv } from "@/engine/foundation/ui/item-equipment-overlay";
import { itemCountQuads } from "@/engine/foundation/ui/item-count";
import { returnScrollBar } from "@/engine/foundation/ui/return-scroll";
import { nameColorRgba } from "@/engine/foundation/gameplay/name-color";
import { containsPoint, topmostControlAt } from "@/engine/foundation/ui/hit-test";
import { minimapZoomTarget, advanceMinimapZoom } from "@/engine/foundation/ui/minimap-zoom";
import { minimapTiles } from "@/engine/foundation/ui/minimap-tiles";
import { minimapFloorQueries, minimapPoseKey, minimapSameFloor } from "@/engine/foundation/ui/minimap-floor";
import {
	minimapMarkers,
	minimapOffset,
	minimapEdge,
	minimapHunting,
	rosterPositions
} from "@/engine/foundation/ui/minimap-markers";
import { createMinimapResources } from "./hud/minimap";
import { createSkillTrainingCache } from "./hud/skill-training";
import {
	createWithdrawalDialog,
	WITHDRAWAL_CONFIRM_SIZE,
	WITHDRAWAL_CONFIRM_BUTTON_Y,
	WITHDRAWAL_CONFIRM_FILL_HEIGHT,
	RESUSCITATION_CONFIRM_SIZE,
	RESUSCITATION_CONFIRM_BUTTON_Y,
	RESUSCITATION_CONFIRM_FILL_HEIGHT,
	WITHDRAWAL_SKILL_FRAME_HEIGHT
} from "./hud/withdrawal";
import { isRestorationPotion } from "@/engine/foundation/gameplay/withdrawal";
import { playerInfoJob } from "@/engine/foundation/gameplay/player-info-job";
import { MALL_NOTIFY_HEIGHT, MALL_NOTIFY_WIDTH, mallNotifyOrigin } from "@/engine/foundation/ui/mall-notify";
import { jobContributionSelf, jobRankPage, jobRankPages } from "@/engine/foundation/gameplay/job-rank";
import { createMapTeleport } from "./hud/map-teleport";
import { SkillSlot_Resolve } from "./hud/skill-slot";
import { academyRank } from "@/engine/foundation/gameplay/academy";
import { createWindowPlacement } from "./hud/window-placement";
import { createItemMall } from "./hud/item-mall";
import {
	mallControl,
	compactMallBagLayout,
	mallCategories,
	mallDescription,
	mallMenuControl,
	mallMenuArtwork,
	mallTabControl,
	mallCategoryIcon,
	mallPageLayout,
	mallCurrencyRows,
	mallQuestionLayout,
	MALL_ROWS_PER_PAGE
} from "@/engine/foundation/ui/item-mall-layout";
import {
	quickslotCooldownQuads,
	quickslotItemCooldownQuads,
	inventoryItemCooldownQuads,
	quickslotTimerPaths
} from "@/engine/foundation/ui/quickslot-cooldown";
import { extendedQuickslotOptions, type ExtendedQuickslotOptions } from "@/engine/foundation/ui/extended-quickslot";
import { skillCooldown } from "@/engine/foundation/gameplay/skill-cooldowns";
import {
	skillPressFeedback,
	skillPressFeedbackActive,
	skillQueueChip
} from "@/engine/foundation/ui/skill-press-feedback";
import {
	masteryTrainingReason,
	skillMetadataById,
	skillTrainingReason
} from "@/engine/foundation/gameplay/skill-catalog";
import { alchemySelection, alchemySlotCapacity } from "@/engine/foundation/ui/alchemy-selection";
import { npcTalkLayout, npcChoiceColor } from "@/engine/foundation/ui/npc-talk";
import {
	merchantBinding,
	merchantSelection,
	merchantCommand,
	merchantQuote,
	merchantPage,
	merchantDialogPage,
	type MerchantSelection
} from "@/engine/foundation/ui/merchant";
import { createNpcPanel } from "./hud/npc-panel";
import {
	createUniqueBanner,
	uniqueBannerPaths,
	createNoticeBanner,
	notificationBannerPaths
} from "./hud/unique-banner";
import { createQuestBanner, questBannerPaths } from "./hud/quest-banner";
import { createQuestTimers } from "./hud/quest-timers";
import { createAutoPotionInput } from "./hud/auto-potion-input";
import { createCosHud } from "./hud/cos-hud";
import { createCompactHud, compactHudLayout, compactWindowDrag, compactCloseControl } from "./hud/compact-hud";
import { fitUiGroup } from "@/engine/foundation/ui/layout";
import { createExperimentalHud, EXPERIMENTAL_TABS } from "./hud/experimental-hud";
import { renderScales, type ExperimentalOptions } from "@/engine/foundation/ui/experimental-options";
import {
	rememberedWindows,
	type RememberedWindow,
	type WindowPositions
} from "@/engine/foundation/ui/window-positions";
import { createRepairHud } from "./hud/repair-hud";
import { createSkinChangeHud } from "./hud/skin-change-hud";
import { createGlobalChatHud } from "./hud/global-chat-hud";
import { createReverseReturnHud } from "./hud/reverse-return-hud";
import { createSpecialtyDealHud } from "./hud/specialty-deal-hud";
import {
	dealSums,
	isTradeGoods,
	NATIVE_LANGUAGE_ENGLISH,
	tradeGoldBasis,
	tradeScale,
	tradeScaleQuantity,
	tradeScaleRow,
	tradeScaleRows
} from "@/engine/foundation/gameplay/specialty-deal";
import { GLOBAL_CHAT_MAX_LENGTH, isGlobalChatItem } from "@/engine/foundation/gameplay/global-chat";
import { createJobHud } from "./hud/job-hud";
import { fortressMiniIndicators } from "@/engine/foundation/ui/fortress-mini-info";
import {
	createFortressStaffHud,
	createFortressWarHud,
	createFortressScheduleHud,
	FORTRESS_SCHEDULE_ROWS
} from "./hud/fortress-war-hud";
import { createFortressTaxHud, FORTRESS_TAX_MAX, FORTRESS_TAX_MIN } from "./hud/fortress-tax-hud";
import { createFortressProductionHud, FORTRESS_PRODUCTION_ROWS } from "./hud/fortress-production-hud";
import {
	FORTRESS_PRODUCTION_CANCEL,
	FORTRESS_PRODUCTION_COLLECT,
	FORTRESS_PRODUCTION_QUERY,
	FORTRESS_PRODUCTION_START,
	FORTRESS_ROLE_COMMANDER,
	type FortressStaff,
	fortressProductionAction,
	fortressGrantRole,
	fortressProductionFactor,
	fortressProductionMayOperate,
	fortressProductionPrice,
	fortressProductionRemaining,
	fortressProductionTimeText
} from "@/engine/foundation/gameplay/fortress-production";
import { alchemyEffectCell, alchemyEffectTexture, alchemyResultLines } from "@/engine/foundation/ui/alchemy-result";
import { createUnionHud } from "./hud/union-hud";
import { createGuildWarHud } from "./hud/guild-war-hud";
import { guildWarRequest, warScoreLimits, WAR_MAX_STAKE, WAR_UNLIMITED } from "@/engine/foundation/gameplay/guild-war";
import { createExchangeHud } from "./hud/exchange-hud";
import {
	createStallHud,
	stallNetworkOrder,
	stallPromptCommand,
	stallPromptLive,
	STALL_CELL_PITCH_X,
	STALL_CELL_PITCH_Y,
	STALL_COMBO_DEGREE,
	STALL_COMBO_LARGE,
	STALL_COMBO_MEDIUM,
	STALL_CHAT_LIMIT,
	STALL_PROMPT_SIZE,
	STALL_TEXT_LIMIT,
	STALL_SLOT_IMAGES,
	stallTradingPresentation,
	type StallNetworkSort,
	type StallPrompt
} from "./hud/stall-hud";
import { createStallNetworkCategories } from "./hud/stall-network-categories";
import { STALL_CHAT_CHANNEL, STALL_SLOTS, type StallListing } from "@/engine/foundation/gameplay/stall";
import { textAllowed } from "@/engine/foundation/ui/character-create";
import { createGrantPowerHud, GRANT_RIGHTS } from "./hud/grant-power-hud";
import { compositeItemCaption, compositeItemLayout, createCompositeItemHud } from "./hud/composite-item-hud";
import { allianceButtons, allianceLeader } from "@/engine/foundation/ui/alliance-guild";
import {
	fortressWarDates,
	fortressWarFormat,
	fortressWarQuestionKey,
	fortressWarRequest,
	fortressWarSlots,
	FORTRESS_WAR_APPLY_ROWS,
	FORTRESS_WAR_APPLY_SLOT_HEIGHT,
	FORTRESS_WAR_APPLY_SLOT_WIDTH
} from "@/engine/foundation/ui/fortress-war-apply";
import { createGuildManagerHud } from "./hud/guild-manager-hud";
import { createMagicOptionHud, MAGIC_OPTION_LIST_ROWS } from "./hud/magic-option-hud";
import {
	premiumCommand,
	REVERSE_RETURN_LAST_DEATH,
	REVERSE_RETURN_LAST_RECALL
} from "@/engine/foundation/gameplay/count-job";
import {
	AVATAR_MAGIC_OPTION_FUNCTION,
	avatarMagicOptionCount,
	avatarMagicOptionText,
	avatarPartSymbol,
	grantableAvatarPart
} from "@/engine/foundation/gameplay/avatar-magic-option";
import {
	guildLevelUpPrice,
	guildManagerRows,
	guildSoldierRows,
	guildSoldierPrompt,
	MASTER_RELEASE_VOTE
} from "@/engine/foundation/gameplay/guild-manager";
import { noticeText } from "@/engine/foundation/ui/notice-text";
import {
	JOB_ALIAS_CHECK,
	JOB_ALIAS_CREATE,
	JOB_OUTCOME_COLLECT,
	JOB_OUTCOME_QUERY,
	JOB_RANK_ACTIVITY,
	JOB_RANK_CONTRIBUTION,
	jobGuildsOffered,
	jobMenuRows,
	noJob
} from "@/engine/foundation/gameplay/job-guild";
import { isSkinChangeScroll, skinDraftRange, type SkinDraftKey } from "@/engine/foundation/gameplay/skin-change";
import { repairAllCost } from "@/engine/foundation/gameplay/repair";
import { createSlotEffectClock } from "./hud/slot-effects";
import { itemIsRare, itemSlotOverlays, itemSlotWash, slotSeed } from "@/engine/foundation/ui/item-slot-effects";
import {
	COS_CLASS_ATTACK,
	COS_CLASS_GUILD,
	COS_CLASS_PICKUP,
	COS_CLASS_TRANSPORT,
	COS_COMMAND_ATTACK,
	COS_COMMAND_CANCEL,
	COS_COMMAND_CLEAN,
	COS_COMMAND_FOLLOW,
	COS_COMMAND_INFO,
	COS_COMMAND_RIDE,
	COS_COMMAND_STANCE,
	cosAbilities,
	cosAttackText,
	cosClass,
	cosCommandButtons,
	cosCommandEnabled,
	cosCommandIcon,
	cosCommandLabel,
	cosCommandLayout,
	type CosCommandContext,
	cosHpText,
	cosInfoSections,
	cosExperienceText,
	cosRentText,
	cosSatietyText,
	cosStanceToggle,
	cosStatusChrome,
	cosStatusRatios,
	cosStatusRect
} from "@/engine/foundation/ui/cos-command";
import { questObjectivePresentation } from "@/engine/foundation/ui/quest-presentation";
import { uniqueBannerQuads } from "@/engine/foundation/ui/unique-banner";
import { nearestGroundItem } from "@/engine/foundation/gameplay/ground-item";
import {
	mainPopupGeometry,
	mainPopupFrame,
	isMainPopupPage,
	type MainPopupPage
} from "@/engine/foundation/ui/main-popup";
import { isUiPanel, type UiPanel } from "@/engine/foundation/ui/panels";
import { moneyPresentation } from "@/engine/foundation/ui/money-presentation";
import { groundItemName, groundItemNameVisible } from "@/engine/foundation/ui/ground-item-label";
import {
	partyMatchButtons,
	partyMatchRows,
	partyAutoCandidates,
	partyActiveJob,
	partyPurposeAllowed,
	partyDefaultPurpose,
	type PartyListing
} from "@/engine/foundation/gameplay/party-matching";
import { createSkillGauge, createGaugePresentation } from "./hud/progression-bars";
import { experienceBar, EXPERIENCE_BAR, gaugeFill } from "@/engine/foundation/ui/progression-bars";
import { createRegionBanner } from "./hud/region-banner";
import { regionBannerQuads } from "@/engine/foundation/ui/region-banner";
import { buffTooltip } from "@/engine/foundation/ui/buff-tooltip";
import {
	masteryPractice,
	PRACTICE_MASTERY,
	PRACTICE_SKILL,
	type PracticeRequest
} from "@/engine/foundation/ui/practice-box";
import { masteryTooltip } from "@/engine/foundation/ui/mastery-tooltip";
import { petMiniInfo } from "@/engine/foundation/ui/pet-mini-info";
import { pkStatusTooltip } from "@/engine/foundation/gameplay/pk-status";
import { mouseModeLabel } from "@/engine/foundation/ui/mouse-modes";
import { tooltipDescription } from "@/engine/foundation/ui/tooltip-description";
import { tooltipItems, actionTooltipKey } from "@/engine/foundation/ui/tooltip-target";
import { itemTooltip } from "@/engine/foundation/ui/item-tooltip";
import { commerceTooltip } from "@/engine/foundation/ui/commerce-tooltip";
import { effectivePartyOptions } from "@/engine/foundation/ui/party-options";
import { monsterPartyNameplate } from "@/engine/foundation/ui/monster-nameplate";
import { skillTooltip } from "@/engine/foundation/ui/skill-tooltip";
import { tooltipColor, type TooltipRow } from "@/engine/foundation/ui/tooltip-rows";
import { latticeCells } from "@/engine/foundation/ui/inventory-layout";
import { sameUiQuads, sameUiSemantics } from "@/engine/foundation/ui/ui-equality";
import { createRetainedLayout } from "@/engine/foundation/ui/retained-layout";
import { buffBoard, buffTimerRoot } from "@/engine/foundation/ui/buff-board";
import { entityCrestFiles } from "@/engine/foundation/ui/guild-crest";
import { checkedNameError } from "@/engine/foundation/ui/character-create";
import { barChrome } from "@/engine/foundation/ui/bar";
import {
	partyDistanceShade,
	partyMembers,
	partyLocalPose,
	partyOverlay,
	partyPortraitGid,
	partyRosterPose,
	partyShadeImage,
	partyShadeImages
} from "@/engine/foundation/ui/party-overlay";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import {
	admittanceOverlay,
	boardSuppression,
	buffViewerIcons,
	collectActiveBuffs,
	createBuffViewer,
	emptyBuffViewer,
	partyBuffViewer,
	skillLookup,
	targetBuffViewer,
	type BuffViewerIcon,
	type BuffViewerState,
	type SkillLookup
} from "@/engine/foundation/ui/buff-viewer";
import { matchingSlots } from "@/engine/foundation/ui/matching-slots";
import {
	defaultVideoOptions,
	DEFAULT_FRAME_LIMIT,
	frameLimits,
	videoOptions,
	videoRows,
	displaySizes,
	displaySizeIndex,
	changeVideo,
	resetVideoRecord,
	type VideoOptions
} from "@/engine/foundation/rendering/video-options";
import {
	defaultInputOptions,
	inputOptions,
	inputLabels,
	captureBinding,
	virtualKey,
	bindingName,
	type InputOptions
} from "@/engine/foundation/ui/input-options";
import { sightMode, type SightMode } from "@/engine/foundation/rendering/camera-options";
import {
	initialAudioOptions,
	defaultAudioOptions,
	audioOptions,
	type AudioOptions
} from "@/engine/foundation/audio/options";
import {
	AUDIO_SLIDER_MAX,
	audioSliderLevel,
	audioSliderPosition,
	stepAudioLevel,
	audioLevelText
} from "@/engine/foundation/audio/volume-control";
import { chatScrollbar } from "@/engine/foundation/ui/chat-scrollbar";
import { overheadLayout } from "@/engine/foundation/ui/overhead-layout";
import { vitalWarning } from "@/engine/foundation/ui/vital-warning";
import {
	beginnerMarkShown,
	blindableCharacter,
	hiddenSilkCos,
	nameInRange,
	riderBoardPosition,
	overheadBoardVisible
} from "@/engine/foundation/ui/name-visibility";
import { chatBlocks, chatBlockError } from "@/engine/foundation/gameplay/chat-blocks";
import {
	gameOptionRows,
	defaultGameOptions,
	initialGameOptions,
	gameOptions,
	gameOption,
	type GameOptions
} from "@/engine/foundation/gameplay/game-options";
import {
	autoPotionDraft,
	autoPotionDraftChoice,
	autoPotionEntry,
	autoPotionWord,
	defaultAutoPotion,
	type AutoPotionDraft
} from "@/engine/foundation/gameplay/auto-potion";
import { selectChatTab, composeChat, chatTabPrefix, chatFeedbackText } from "@/engine/foundation/ui/chat-presentation";
import { textBoardLines, textLines } from "@/engine/foundation/ui/text-lines";
import { createSpeech } from "./hud/speech";
import { createMessageScroll } from "./hud/scroll";
import { createHudMessages } from "./hud/messages";
import { damageTextAssets } from "@/engine/foundation/ui/damage-text";
import { targetStatus, compactTargetStatus, fortressTargetKind } from "@/engine/foundation/ui/target-status";
import { fortressActive, fortressDeleteAction } from "@/engine/foundation/gameplay/fortress";
import {
	loadingPresentation,
	type LoadingPresentation,
	type LoadingRequest
} from "@/engine/foundation/ui/loading-presentation";
import { feedbackLevels } from "@/engine/foundation/gameplay/feedback-levels";
import { systemMessageLayout } from "@/engine/foundation/ui/system-message-layout";
import { comboBoxChrome } from "@/engine/foundation/ui/combo-box";
import { verticalSpinChrome } from "@/engine/foundation/ui/vertical-spin";
import { stretchRing } from "@/engine/foundation/ui/stretch-ring";
import { chatLayout } from "@/engine/foundation/ui/chat-layout";
import {
	worldMapImagePaths,
	worldMapDemand,
	MAP_LOCAL_MARKER,
	worldMapPageAt,
	worldMapPages,
	worldMapPresentation,
	type MapMarker,
	mapLabelVisible
} from "@/engine/foundation/ui/world-map";
import {
	experienceReadout,
	minimapCoordinates,
	minimapRotation,
	skillPointReadouts
} from "@/engine/foundation/ui/hud-readouts";
import { tooltipBubble, hudTooltipKey } from "@/engine/foundation/ui/helper-bubble";
import { playerAbilityValues } from "@/engine/foundation/gameplay/player-stats";
import { academyLayout } from "@/engine/foundation/ui/academy-layout";
import { createGuideResources } from "./guide/resources";
import { automaticGuide } from "@/engine/foundation/gameplay/guide";
import {
	missionLoadingAssets,
	missionLoadingQuads,
	loadingScreenQuads,
	regionLoadingBackground,
	travelLoadingBackground
} from "@/engine/foundation/ui/mission-loading";
import { catalogMessage } from "@/engine/foundation/ui/catalog-message";
import { gachaPrizes, isGachaTicket } from "@/engine/foundation/gameplay/gacha-catalog";
import {
	quickSlotCommand,
	quickSlotItemSlot,
	hotbarSlot,
	extendedSlot,
	quickSlotDrag,
	quickSlotDrop,
	HELPER_ACTION_ID,
	TRACE_ACTION_ID
} from "@/engine/foundation/gameplay/quickslots";
import { itemActivation } from "@/engine/foundation/gameplay/item-activation";
import { createIconPaths } from "@/engine/foundation/ui/icon";
import { createLocalization } from "./localization/localization";
import { createTitleUi } from "./title/title";
import { serverListRefreshDue, titleStatusKey, titleStatusMessage } from "@/engine/foundation/ui/title-status";
import { buttonAccess, buttonTextColor } from "@/engine/foundation/ui/button-state";
import { createUiText } from "./text/text";
import { createUiAssets } from "./resources/resources";
import { frameParts, frameRing } from "@/engine/foundation/ui/frame-ring";
import { normalTile } from "@/engine/foundation/ui/normal-tile";
import { createHudResources } from "./hud/resources";
import { createWindowWarm, skillWindowIcons } from "./warm/window-warm";
import {
	authoredRect,
	authoredClientRect,
	authoredPaintOrder,
	type AuthoredControl,
	type AuthoredLayout
} from "@/engine/foundation/ui/authored-layout";
import {
	partyProposalAssets,
	partyProposalLayout,
	guildProposalLayout,
	proposalLayout,
	MESSAGE_FRAME,
	MESSAGE_TILE,
	PARTY_OPTION
} from "@/engine/foundation/ui/party-proposal";
import {
	commandInteger,
	DEBUG_COMMAND_DEBUG,
	DEBUG_COMMAND_ITEM,
	DEBUG_COMMAND_MESSAGE_CLEAR,
	DEBUG_COMMAND_NULL,
	DEBUG_COMMAND_PLAYER_COUNT,
	debugCommand,
	type DebugCommand
} from "@/engine/foundation/ui/debug-commands";
import { messageBox } from "@/engine/foundation/ui/message-box";
import { resourceErrorLines } from "@/engine/foundation/ui/resource-error";
import { disconnectDialog } from "@/engine/foundation/ui/disconnect-dialog";
import { noticeDialog } from "@/engine/foundation/ui/notice-dialog";
import { rebirthDialog } from "@/engine/foundation/ui/rebirth-dialog";
import {
	createResurrectionPrompt,
	resurrectionBoxLayout,
	resurrectionNoteColor,
	resurrectionQuestion
} from "@/engine/foundation/ui/resurrection-proposal";
import { messageBoxLines, textMessageBoxLayout } from "@/engine/foundation/ui/text-message-box";
import { systemMenu, SYSTEM_MENU_HEIGHT, EXPERIMENTAL_MENU_ID } from "@/engine/foundation/ui/system-menu";
import { inventorySlots, inventoryLattice } from "@/engine/foundation/ui/inventory-layout";
import { guideTokens } from "@/engine/foundation/ui/guide-content";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { SessionCommand, ServerRecord, CharacterRecord } from "@/engine/contracts/session";
import type { UiView, UiEvent, UiRect, UiQuad, UiControl, UiSemantics, UiScene } from "@/engine/contracts/ui";
import type { EntityState } from "@/engine/contracts/world";
import type { UiTexture } from "@/engine/contracts/texture";

// CIFCosInfo_RefreshSatietyDependentStats (6A4600) font colour 0xFF999999.
const COS_LOW_SATIETY = [ 0x99 / 255, 0x99 / 255, 0x99 / 255, 1 ] as const;
// No video option combo is open (slot -1 is the screen-size combo).
const VIDEO_COMBO_CLOSED = -99;
const WORLD_MAP_WIDTH = 652;
const WORLD_MAP_HEIGHT = 424;
const VIDEO_FRAME_LIMIT_SLOT = -3;
const VIDEO_VISIBLE_ROWS = 6;
const VIDEO_SCROLL_MAX = videoRows().length + 1 - VIDEO_VISIBLE_ROWS;
const ROOT = "/assets/images/Media_extracted/", BUTTON = ROOT + "interface/ifcommon/com_button.png";
// 683B40 assigns the notice root this texture after creating it.
const MALL_NOTICE_FRAME = ROOT + "interface/mall/mall_communicate.png";
const PARTS = frameParts();
const FRAME = ROOT + "interface/frame/mframe_wnd_";
const PARTY_MATCH_RANGE_SEPARATOR_ID = 43;
// The bug reporter (issue #90): its chat command and its Option window row.
const BUG_COMMAND = /^\/bug(?:\s+|$)/i;
const BUG_REPORTS_DISABLED = "Bug reports are disabled on this server.";
const BUG_REPORTS_UNAVAILABLE = "Connecting to the bug reporter; the report window opens as soon as it answers.";
// The skin change scroll's window (CIFChangePlayerModel).
const SKIN_PANEL = "Skin change";
// CIFWholeChat, the Global Chatting item's window, and its line's edit box.
const GLOBAL_CHAT_PANEL = "Global chat";
const GLOBAL_CHAT_TEXT = "wholechat-text";
// CIFSpecialtyDeal, the trade goods window, and its quantity edit (control 30).
const SPECIALTY_DEAL_PANEL = "Specialty deal";
const SPECIALTY_DEAL_COUNT = "specialty-deal-count";
// CIFFortressWarApplyWnd, opened by the fortress official's answer.
const FORTRESS_WAR_PANEL = "Fortress war application";
const FORTRESS_SCHEDULE_PANEL = "Fortress war schedule";
// CIFTaxManagement, the fortress manager's first row (fortress-tax-hud.ts).
const FORTRESS_TAX_PANEL = "Fortress tax";
const FORTRESS_TAX_RATE = "fortress-tax-rate";
const FORTRESS_TAX_AMOUNT = "fortress-tax-amount";
// CIFFortressMakeItemWnd, the smith's and trainer's production window
// (fortress-production-hud.ts), and its count box's edit (MsgBoxMakeItem 116).
const FORTRESS_PRODUCTION_PANEL = "Fortress production";
const FORTRESS_PRODUCTION_COUNT = "fortress-production-count";
// 52C870 mode 0xA: the count edit takes two characters.
const FORTRESS_PRODUCTION_COUNT_LENGTH = 2;
// CIFFortressMakeItemWnd_OnCreate (65AD70): list rows of 32 px; the gauge
// marks (232..236) read 0% to 100% in quarters.
const FORTRESS_PRODUCTION_ROW_HEIGHT = 32;
const FORTRESS_PRODUCTION_MARKS = 5;
// 52A960: the make-item box is CIFMessageBox's Create client (16,40,284,122)
// inside its frame.
const FORTRESS_PRODUCTION_BOX: readonly [number, number] = [ 316, 178 ];
// 52A7E0 caps the levy edit at 64 characters.
const FORTRESS_TAX_AMOUNT_LENGTH = 64;
// 664DA0 positions the slider's arrows at 0 and 270 and gives the thumb
// 234 px of travel between them (CIFScrollBar_SetPageStep 0xEA).
const FORTRESS_TAX_ARROW = 20;
const FORTRESS_TAX_ARROW_RIGHT = 270;
const FORTRESS_TAX_TRAVEL = 234;
// CIFJobRank and CIFJobContributionRank share one panel (job-hud.ts).
const JOB_RANK_PANEL = "Job ranking";
// The smith's avatar magic option window (CIFGrantMagicAttributeWnd).
const GRANT_PANEL = "Magic option";
// The slider's thumb travel inside GDR_SLIDER_CTRL (prev 2..22, next at 125).
const SKIN_SLIDER_TRAVEL = 85;
// Item slot controls a carry can leave: inventory, avatar, storage, pet bag.
const ITEM_SLOT_PREFIXES = [ "slot:", "avatar:", "storage-slot:", "cos-slot:" ] as const;
const BUTTON_FOCUS = BUTTON.replace( ".png", "_focus.png" ),
	BUTTON_PRESS = BUTTON.replace( ".png", "_press.png" ),
	BUTTON_DISABLE = BUTTON.replace( ".png", "_disable.png" );
const CLOSE = ROOT + "interface/ifcommon/com_windowclose.png",
	CLOSE_PRESS = CLOSE.replace( ".png", "_press.png" ),
	CLOSE_FOCUS = CLOSE.replace( ".png", "_focus.png" );

/*
================
UiFrameProbe

Optional observer supplied by the frame owner. UI code never looks up a
global profiler and the observer cannot change the published product.
================
*/
export interface UiFrameProbe {
	detailBegin( stage: string ): void;
	detailEnd( stage: string ): void;
}
/*
================
UiExtensions

Browser-only integrations are grouped separately from native preferences.
Sinks added after the positional list (window positions) live here too,
rather than as another createUi parameter.
================
*/
export interface UiExtensions {
	bugReport?: BugReportControl | null;
	saveExperimental?: ( value: ExperimentalOptions ) => void;
	saveWindowPositions?: ( value: WindowPositions ) => void;
}

// Sole owner of UI navigation, focus projection and pending UI intent. Gameplay is read-only.
/*
================
createUi
================
*/
export function createUi(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	commands: ( command: SessionCommand ) => void,
	publish: ( scene: UiScene | null ) => void,
	texture: ( id: string, image: UiTexture | null ) => void,
	base: string,
	apiBase: string,
	clickSound: () => void = () => {},
	sound: ( kind: "open" | "close" | "message" | "quest" ) => void = () => {},
	loadingVariant: () => number = () => 1,
	chooseTip: ( count: number ) => number = () => 0,
	saveOptions: ( value: GameOptions ) => void = () => {},
	audioPreference: ( value: AudioOptions, commit: boolean ) => void = () => {},
	saveSight: ( value: SightMode ) => void = () => {},
	saveBindings: ( value: InputOptions ) => void = () => {},
	saveVideo: ( value: VideoOptions ) => void = () => {},
	saveBlocks: ( value: readonly string[] ) => void = () => {},
	saveQuickslots: ( value: ExtendedQuickslotOptions ) => void = () => {},
	extensions: UiExtensions = {}
) {
	const bugReport = extensions.bugReport;
	const experimental = createExperimentalHud();
	let consolePhase: 0 | 1 | 2 | 3 = 0, consoleY = -112, consoleLast = 0, consoleText = "", gmObserved = 0;
	let consoleRows: string[] = [], consoleHistory: string[] = [], consoleHistoryIndex = 0;
	// +0x7BC, the /Debug flag: the native debug-message switch (690C40 case 0).
	let debugMessages = false;
	let video = defaultVideoOptions(), videoDraft = video, videoScroll = 0, videoCombo = VIDEO_COMBO_CLOSED;
	let bindings = defaultInputOptions(), bindingDraft = bindings, bindingSelected = -1, bindingScroll = 0;
	let sight: SightMode = 0, sightDraft: SightMode = 0;
	let audioSaved = initialAudioOptions(), audioDraft = audioSaved;
	let localBlocks: readonly string[] = [];
	let blockTab: 0 | 1 = 0, blockOffset = 0, blockSelected = "", blockInput = "";
	let blockDialog: "add" | "remove" | null = null;
	let options = initialGameOptions(), optionDraft = options;
	let optionTab = 0;
	let optionScroll = [ 0, 0 ];
	let beginnerDraft = false;
	let potionCombo = "";
	let potionComboOffset = 0;
	let potionDraft: AutoPotionDraft = autoPotionDraft( defaultAutoPotion() );
	const commonWindowPaths = [
		BUTTON,
		BUTTON_FOCUS,
		BUTTON_PRESS,
		BUTTON_DISABLE,
		CLOSE,
		CLOSE_PRESS,
		CLOSE_FOCUS,
		...PARTS.map( p => FRAME + p + ".png" )
	];
	const combatGlyphPaths = damageTextAssets();
	let buffTick = -1;
	// Derived two-state presentation clock; body channel 4 remains authoritative.
	let berserkActor = 0, berserkStarted = 0, berserkFrame = -1, berserkDisplayed = 0;
	let equipmentWarningPhase = 0, equipmentWarningDue = 0, equipmentWarningVisible = false;
	// Low HP/MP caution atlas (100 ms cells) animates on retained world frames too.
	let cautionVisible = false, cautionFrame = -1;
	let targetGradeIcon = "";
	// CIFBuffViewer owners keep their one-second diff state: the target window
	// (581330), each quick-party slot (5BA840) and the local board's suppression
	// refresh (6E12D0 timer 0xA -> 6E64A0).
	const targetBuffs = createBuffViewer( targetBuffViewer() );
	let targetBuffState: BuffViewerState = emptyBuffViewer();
	let skillIndex: { catalog: readonly SkillMetadata[] | undefined; lookup: SkillLookup; } = {
		catalog: undefined,
		lookup: skillLookup( undefined )
	};
	/*
	================
	skillsOf
	================
	*/
	const skillsOf = ( catalog: readonly SkillMetadata[] | undefined ) => {
		if ( skillIndex.catalog !== catalog ) skillIndex = { catalog, lookup: skillLookup( catalog ) };
		return skillIndex.lookup;
	};
	let partyBuffs = new Map<number, ReturnType<typeof createBuffViewer>>(),
		partyBuffStates = new Map<number, BuffViewerState>(),
		partyRoster = "";
	let boardSuppressed: ReadonlySet<number> = new Set(), boardSuppressionDue = 0;
	/*
	================
	stepBuffViewers
	================
	*/
	function stepBuffViewers( next: UiView, now: number ) {
		const game = next.session?.phase === "world" ? next.gameplay : undefined,
			skill = skillsOf( game?.skillCatalog ),
			effects = game?.attachedEffects ?? [];
		/*
		================
		mask
		================
		*/
		const mask = ( gid: number ) => gid ? game?.vitals.find( v => v.gid === gid )?.abnormal ?? 0 : 0;
		const target = game ? next.entities.find( e => e.gid === game.target ) : undefined;
		// 5814D0: only CICMonster, CICCos and CICUser targets show the viewer.
		const subject = target && [ "monster", "cos", "player", "local-player" ].includes( target.kind ) ?
			target.gid :
			0;
		let changed = false;
		const targetState = targetBuffs.step( subject, now, effects, mask( subject ), skill );
		if ( targetState !== targetBuffState ) {
			targetBuffState = targetState;
			changed = true;
		}
		// 5BCBB0 rebuilds every slot whenever the roster is rearranged.
		const members = game?.social?.leader ? partyMembers( game ) : [],
			roster = members.map( m => m.id + ":" + m.name ).join( "," );
		if ( roster !== partyRoster ) {
			partyRoster = roster;
			partyBuffs = new Map();
			partyBuffStates = new Map();
			changed = true;
		}
		for ( const member of members ) {
			const gid = next.entities.find( e => e.kind === "player" && e.name === member.name )?.gid ?? 0;
			let viewer = partyBuffs.get( member.id );
			if ( !viewer ) {
				viewer = createBuffViewer( partyBuffViewer() );
				partyBuffs.set( member.id, viewer );
			}
			const state = viewer.step( gid, now, effects, mask( gid ), skill );
			if ( state !== partyBuffStates.get( member.id ) ) {
				partyBuffStates.set( member.id, state );
				changed = true;
			}
		}
		if ( !game ) {
			if ( boardSuppressed.size ) {
				boardSuppressed = new Set();
				changed = true;
			}
		} else if ( now >= boardSuppressionDue ) {
			boardSuppressionDue += 1000;
			if ( boardSuppressionDue <= now ) boardSuppressionDue = now + 1000;
			const slots = (game.buffSlots ?? []).filter( slot => slot.effect.gid === game.localGid ).map( slot => ({
				kind: "buff",
				id: slot.effect.skill,
				serial: slot.serial,
				suppressed: boardSuppressed.has( slot.serial )
			}) );
			const flagged = new Set(
				boardSuppression( slots, collectActiveBuffs( effects, game.localGid, skill ), skill ).filter( slot =>
					slot.suppressed
				).map( slot => slot.serial )
			);
			if (
				flagged.size !== boardSuppressed.size || [ ...flagged ].some( serial => !boardSuppressed.has( serial ) )
			) {
				boardSuppressed = flagged;
				changed = true;
			}
		}
		return changed;
	}
	let recallConfirm: number | null = null;
	let buffDismiss: { skillId: number; instance: number; } | null = null;
	let loading: LoadingPresentation | null = null;
	let loadingGeneration = 0, loadingTravelRevision: number | undefined;
	const levels = feedbackLevels();
	const notificationBanner = createNoticeBanner( "notificationBanner" ),
		uniqueBanner = createUniqueBanner(),
		questBanner = createQuestBanner( createUniqueBanner() ),
		questTimers = createQuestTimers();
	const cosHud = createCosHud();
	const autoPotionInput = createAutoPotionInput();
	const repairHud = createRepairHud();
	const skinHud = createSkinChangeHud();
	const globalChatHud = createGlobalChatHud();
	const reverseScrollHud = createReverseReturnHud();
	const specialtyDealHud = createSpecialtyDealHud();
	let specialtyCombo = false;
	const jobHud = createJobHud();
	const fortressWarHud = createFortressWarHud();
	const fortressScheduleHud = createFortressScheduleHud();
	const fortressTaxHud = createFortressTaxHud();
	const fortressProductionHud = createFortressProductionHud();
	// The step clock the production window counts down on, and the second it
	// last drew (65A5E0 runs on a 1000 ms timer).
	let productionClock = 0, productionSecond = -1;
	// The last reinforcement outcome shown, the effect it started, and the
	// step clock the effect plays on (62B0B0, alchemy-result.ts).
	let alchemyOutcomeSeen = 0, alchemyClock = 0;
	let alchemyEffect: { readonly flags: number; readonly startedAt: number; } | null = null;
	const fortressStaffHud = createFortressStaffHud();
	const unionHud = createUnionHud();
	const guildWarHud = createGuildWarHud();
	const exchangeHud = createExchangeHud();
	const stallHud = createStallHud();
	const grantPowerHud = createGrantPowerHud();
	const compositeItemHud = createCompositeItemHud();
	const guildManagerHud = createGuildManagerHud();
	const magicOptionHud = createMagicOptionHud();
	const slotEffects = createSlotEffectClock();
	const spGauge = createSkillGauge();
	const skillTraining = createSkillTrainingCache();
	const withdrawal = createWithdrawalDialog();
	const mapTeleport = createMapTeleport();
	const gauges = createGaugePresentation();
	const regionBanner = createRegionBanner();
	const hudMessages = createHudMessages( chooseTip );
	const speech = createSpeech();
	let academyWasVisible = false;
	const hud = createHudResources( assets, base ),
		guideResources = createGuideResources( assets, base ),
		minimapResources = createMinimapResources( assets, base ),
		stallCategories = createStallNetworkCategories( assets, base );
	const npcPanel = createNpcPanel(), windowPlacement = createWindowPlacement();
	// The warehouse window's page (storage-panel.ts).
	const storagePanel = createStoragePanel();
	const resurrectionPrompt = createResurrectionPrompt();
	const itemMall = createItemMall();
	// CIFMallNotifyWnd (mall-notify.ts): "pending" until the session's first
	// world entry decides it, as 683B40's +0x6FC latch; then shown or closed.
	let mallNotice: "pending" | "open" | "closed" = "pending";
	const windowWarm = createWindowWarm();

	let guideThumbTravel = 0, guideScrollMax = 0, guideIndexMax = 0;
	let guideX: number | null = null, guideY: number | null = null;
	let academySelection = 0, academySort = "ID_BTN", academyAscending = true;
	let academyName = "", academyGrade = 5, academyKind = 2, academyCombo = 0;
	let academyFilter = { name: "", grade: 5, kind: 2 };
	let guideSidebar = false, guideTab: "general" | "events" | "quests" = "general", guideIndexScroll = 0;
	const guideGroups = new Set<number>();
	let guideEvent = 0, guideObserved = 0, guideRequested = 0, guidePending = 0, guideScroll = 0;
	let guideOrigin: { regionId: number; x: number; z: number; } | null = null;
	let rememberedMainPopup: MainPopupPage = "Character";
	const compactHud = createCompactHud();
	const chatScroll = createMessageScroll( "chat-scroll" ), statusScroll = createMessageScroll( "status-scroll" );
	const chatLayoutCache = createRetainedLayout<ReturnType<typeof chatLayout>>(),
		statusLayoutCache = createRetainedLayout<ReturnType<typeof systemMessageLayout>>();
	/*
	================
	HudSection
	================
	*/
	type HudSection = { quads: UiQuad[]; controls: UiControl[]; blocks: UiRect[]; paths: string[]; };
	const playerLayoutCache = createRetainedLayout<HudSection>(), barLayoutCache = createRetainedLayout<HudSection>();
	const petLayoutCache = createRetainedLayout<HudSection>();
	let layoutResourcesRevision = 0;
	let chatHidden = false, whispersOpen = false, statusFilterOpen = false;
	const statusFilters = new Set<string>( [ "gain", "fight", "status", "party", "game" ] );
	let chatRows = 2, chatTab = 0, statusRows = 2;
	let minimapZoom = 160, minimapTarget = 160, minimapLast: number | null = null;
	let abilityDetails = false,
		mapSmall = false,
		mapFollow = true,
		mapPage = 0,
		mapX = 100,
		mapY = 100,
		mapPan: [number, number] = [ 0, 0 ],
		mapCenter: import("@/engine/contracts/gameplay").Pose | null = null,
		mapWorldLoading = false;
	const title = createTitleUi( assets, base );
	let serverList = false,
		serverDraft = "",
		serverOffset = 0,
		hover: string | null = null,
		pressed: string | null = null;
	const text = createUiText( assets, base ), resources = createUiAssets( assets, texture, base );
	let diagnosticError: string | null = null;
	let minimapQueryKey = "";
	let deathIdentity = 0, deathDismissed = false, deathPosition: readonly [number, number] | null = null;
	let deathLifeAt = 0, deathStateAt: number | null = null, rebirthDue = false, deathRequested = false;
	let deathObserved: Pick<UiView, "session" | "gameplay" | "entities"> | null = null;
	// Self-rebirth prompt, native timing. LIFE=dead (0x3122) only arms CICharactor timer 0xE (5000 ms,
	// 777BFE). The death state begins when the fatal impact lands, or at zero HP with no fatal pending
	// (vitals deathState, 77A33A); entering it arms CICPlayer timer 0xF, the prompt 3000 ms later (864AE0).
	// Both edges come from snapshots, so camera-only frames advance the deadline without reading them.
	/*
	================
	stepRebirthPrompt
	================
	*/
	function stepRebirthPrompt( next: UiView, now: number ) {
		let changed = false;
		if (
			!deathObserved || deathObserved.session !== next.session || deathObserved.gameplay !== next.gameplay ||
			deathObserved.entities !== next.entities
		) {
			deathObserved = { session: next.session, gameplay: next.gameplay, entities: next.entities };
			const phase = next.session?.phase, local = next.gameplay?.localGid;
			const world = phase === "world" || (phase === "disconnected" || phase === "reconnecting") && !!local;
			const dead = world ?
				next.entities.find( e => e.gid === local && e.appearanceState?.[0] === 2 )?.gid ?? 0 :
				0;
			if ( dead !== deathIdentity ) {
				deathIdentity = dead;
				deathDismissed = false;
				deathRequested = false;
				deathPosition = null;
				focus = null;
				composing = false;
				deathLifeAt = now;
				changed = true;
			}
			if ( !(world && next.gameplay?.vitals?.find( v => v.gid === local )?.deathState) ) deathStateAt = null;
			else if ( deathStateAt === null ) deathStateAt = now;
		}
		const due = !!deathIdentity &&
			(deathRequested || now >= Math.min( deathStateAt ?? Infinity, deathLifeAt + 5000 ) + 3000);
		if ( due !== rebirthDue ) {
			rebirthDue = due;
			changed = true;
		}
		return changed;
	}
	/*
	================
	requestRebirthPrompt

	6B3E90 and corpse selection reach 6813E0: dead self-selection creates
	confirmation type 3 immediately, retaining it when already present.
	================
	*/
	function requestRebirthPrompt( gid: number ) {
		if (
			!gid || view?.session?.phase !== "world" || gid !== deathIdentity ||
			gid !== view.gameplay?.localGid || rebirthDue && !deathDismissed
		) return;
		deathRequested = true;
		deathDismissed = false;
		deathPosition = null;
		dirty = true;
	}
	let inviteIdentity = "",
		invitePosition: readonly [number, number] | null = null,
		disconnectPosition: readonly [number, number] | null = null;
	let noticeDialogSequence = 0,
		activeNoticeDialog: import("@/engine/foundation/gameplay/system-notices").SystemNotice["dialog"],
		noticeDialogPosition: readonly [number, number] | null = null,
		noticeDialogFrame: UiRect | null = null;
	let systemX: number | null = null, systemY: number | null = null;
	let dollYaw = .100000001, avatarView = false;
	let tooltipMemo: {
		item: import("@/engine/contracts/gameplay").InventoryItem | undefined;
		skill: number;
		progression: import("@/engine/foundation/gameplay/progression").Progression | undefined;
		learned: readonly number[] | undefined;
		catalog: unknown;
		country: number | undefined;
		sex: number | undefined;
		rows: readonly TooltipRow[];
	} | null = null;
	// The item riding the cursor, by the slot control it left (slot:, avatar:,
	// storage-slot:, cos-slot:). A drag or a click-carry (the bridge's carry)
	// both move it; the release or the next press places it.
	let carriedItem: { source: string; slot: number; x: number; y: number; avatar?: boolean; } | null = null;
	// The structure target window's delete question (517750 -> 517CA0).
	let structureRemoval: { gid: number; action: number; fortress: number; name: string; } | null = null;
	/*
	================
	carriedRow

	The item row a carry shows, read from the container its source names.
	================
	*/
	function carriedRow(
		carried: NonNullable<typeof carriedItem>,
		game: UiView["gameplay"] | undefined
	) {
		if ( carried.source.startsWith( "storage-slot:" ) ) {
			return game?.storage?.items.find( row => row.slot === carried.slot );
		}
		if ( carried.source.startsWith( "cos-slot:" ) ) {
			return game?.cosRecords?.find( r => r.gid === cosGid )?.inventory?.find( row => row.slot === carried.slot );
		}
		return (carried.avatar ? game?.avatarInventory : game?.inventory)?.find( row => row.slot === carried.slot );
	}
	const localization = createLocalization( assets, base );
	let titleNotice: { status?: number; argument?: number; until: number; } | null = null, uiNow = 0;
	let nextPoll = 0,
		lastSessionRevision = -1,
		serversRequested = false,
		serversRetryAt = 0,
		lastNativeTitleStatus: number | undefined,
		loginReplyPending = false;
	let view: UiView | null = null,
		revision = 0,
		dirty = true,
		disposed = false,
		focus: string | null = null,
		composing = false,
		selection = [ 0, 0 ];
	let lastProduct: {
		width: number;
		height: number;
		damageText: boolean;
		quads: readonly UiQuad[];
		semantics: import("@/engine/contracts/ui").UiSemantics;
	} | null = null;
	let servers: readonly ServerRecord[] = [],
		roster: readonly CharacterRecord[] = [],
		selectedServer = "",
		selectedCharacter = "",
		account = "",
		password = "",
		endpoint = apiBase;
	let partySettings = false, partyDraft = 0;
	let popupPosition: readonly [number, number] | null = null;

	let socialName = "",
		socialSubject = "",
		socialContents = "",
		socialAmount = "",
		socialMember = 0,
		socialPage = 0,
		confirmSocial = "",
		partyOptions = 4;
	let goldAmount = "", confirmDrop = "", goldDialog: false | "drop" | "deposit" | "withdraw" = false;
	let groundDrop: { slot: number; refObjId: number; } | null = null;
	let splitStack: { slot: number; refObjId: number; quantity: number; } | null = null, splitAmount = "1";
	let shopOpenRequest: { gid: number; tab: number; revision: number; } | null = null;
	let shopWarning: { selection: MerchantSelection; name: string; quick: boolean; } | null = null;
	let shopPage = 0, shopTab = 0, shopDialog = false, shopChoice: MerchantSelection | null = null, shopQuantity = "1";
	let shopPosition: readonly [number, number] | null = null;
	let cosTab = 1,
		cosDraft = 0,
		guildDialog = "",
		guildTab = 0,
		guildSort = 121,
		guildDescending = false,
		guildNameMode = 0;
	let alchemyMode: import("@/engine/contracts/item-process").AlchemyMode = "reinforce",
		alchemySlots: number[] = [],
		alchemyQuantity = "1",
		processPage = 0,
		gachaPage = 0,
		gachaWasVisible = false,
		gachaEntry = 1,
		gachaSlot = -1;
	let selectedAction = 4000;
	let partyMatchSelection = 0, partyMatchOffset = 0;
	let partyMatchSort: keyof PartyListing = "id", partyMatchDescending = false;
	let partySearch = { name: "", purpose: 4, min: 1, max: 90 },
		partySearchDraft = { name: "", purpose: 4, min: "1", max: "90" },
		partyPurposeOpen = false;
	let partyProgressTick = -1, partyProgressVisible = false;
	let partyAuto = { purpose: 0, race: 2, exp: 0, item: 0 };
	let partyDialog: "register" | "modify" | "delete" | "auto" | null = null,
		partyForm = { purpose: 0, min: "1", max: "90", title: "" };
	const partyPurposes = [
		"UIIT_CTL_PARTYMATCH_PSEARCH_OBJECTCOMBAT",
		"UIIT_STT_QUEST",
		"UIIT_CTL_PARTYMATCH_PSEARCH_FIND_OBJECTTRADER",
		"UIIT_CTL_PARTYMATCH_PSEARCH_FIND_OBJECTTHIEF",
		"UIIT_CTL_PARTYMATCH_PSEARCH_OBJECTALL"
	];
	const partyListingPurposes = partyPurposes.slice( 0, 4 );
	let questPosition: readonly [number, number] = [ 0, 0 ], questDetailScroll = 0, questDetailMax = 0;
	let practice: PracticeRequest | null = null, questDetails = false;
	let skillTab = 0, selectedMastery = 0, skillScroll = 0;
	let selectedSkill = 0, skillPage = 0, hotbarPage = 0, trainingMode = false, clearHotbar = false;
	let carriedShortcut: { id: string; x: number; y: number; } | null = null;
	let quickslotTime = 0, quickslotTick = -1;
	let extVertical = true,
		extDouble = true,
		extOpen = true,
		extTransparent = true,
		extSlotLock = false,
		extPositionLock = false,
		extOptions = false;
	let extPosition: readonly [number, number] | null = null;
	let extDraft = [ true, false, false, true ];
	/*
	================
	persistQuickslots
	================
	*/
	function persistQuickslots() {
		saveQuickslots( {
			open: extOpen,
			vertical: extVertical,
			double: extDouble,
			transparent: extTransparent,
			slotLock: extSlotLock,
			positionLock: extPositionLock,
			position: extPosition
		} );
	}
	/*
	================
	persistWindowPositions

	Retire the placement session before UI teardown, including pagehide.
	leave() makes a later disposal after logout a no-op.
	================
	*/
	function persistWindowPositions( retire = true ) {
		if ( !view ) return;
		const root = hud.data()?.root;
		if ( !retire ) {
			// Native rejection saves before layout and before lazy children
			// exist. Asset admission must precede that authored-origin snapshot.
			if ( !root ) return;
			const own: Partial<
				Record<RememberedWindow, readonly [number, number]>
			> = {};
			for ( const window of rememberedWindows().slice( 0, 5 ) ) {
				const node = Object.values( root ).find( node => node.id === window.nativeId );
				if ( !node ) throw Error( "Missing eager window: " + window.key );
				own[window.key] = [ node.rect[0], node.rect[1] ];
			}
			const remembered = windowPlacement.snapshot( view.width, view.height, own );
			if ( remembered ) extensions.saveWindowPositions?.( remembered );
			return;
		}
		const popup = mainPopupFrame( view.width, view.height, popupPosition );
		const own: Partial<
			Record<RememberedWindow, readonly [number, number]>
		> = {
			mainPopup: [ popup[0], popup[1] ],
			worldMap: [ mapX, mapY ],
			...(guideX !== null && guideY !== null ? { gameGuide: [ guideX, guideY ] as const } : {}),
			...(extPosition ? { extendedQuickslot: extPosition } : {})
		};
		for (
			const [key, name, id] of [
				[ "store", "GDR_STORE", "Shop" ],
				[ "storageRoom", "GDR_STORAGEROOM", "Storage" ],
				[ "exchange", "GDR_EXCHANGE", "Exchange" ]
			] as const
		) {
			const frame = windowPlacement.read( "window-drag:" + id ), node = root?.[name];
			if ( frame && frame[2] > 0 ) own[key] = [ frame[0], frame[1] ];
			else if ( node ) {
				own[key] = [
					Math.trunc( view.width / 2 ) - Math.trunc( node.rect[2] / 2 ),
					Math.trunc( view.height / 2 ) - Math.trunc( node.rect[3] / 2 )
				];
			}
		}
		const remembered = windowPlacement.leave( view.width, view.height, own );
		if ( remembered ) extensions.saveWindowPositions?.( remembered );
	}
	const expandedQuests = new Set<number>();
	let selectedQuest = 0, trackedQuest = 0, confirmAbandon = false, questPage = 0, chatPage = 0;
	let chatText = "", chatTarget = "", chatChannel = 1, chatFeedbackObserved = 0;
	let caretDue = 0, caretVisible = true;
	let focusRevision = 0;
	let focusRequest: UiSemantics["focusRequest"];
	let windowMissing: string[] = [];
	const admittedWindows = new Map<string, HudSection & { key: string; }>();
	let panel: UiPanel | "" = "",
		inventorySlot = -1,
		inventoryPage = 0,
		cosSlot = -1,
		cosPage = 0,
		cosPlayerPage = 0,
		cosGid = 0,
		serverPage = 0,
		pending = false,
		rosterRequested = false,
		lastPhase = "",
		message = "";
	// The UI resolves the same icons on every layout; its resolver remembers them.
	const iconPath = createIconPaths();
	let controls: UiControl[] = [], blocks: UiRect[] = [], paths: string[] = [];
	const white = [ 1, 1, 1, 1 ] as const, gold = [ .94, .85, .63, 1 ] as const;
	// CGWndListHost::LayoutScrollbar 6F3620: content insets also govern the scrollbar.
	/*
	================
	optionListTrack
	================
	*/
	function optionListTrack( r: UiRect, insetX: number, insetY: number ): UiRect {
		return [ r[0] + r[2] - insetX - 16, r[1] + insetY + 16, 16, r[3] - insetY * 2 - 48 ];
	}
	// Every panel entry uses this owner. Open/select is idempotent; only explicit
	// toggle callers may close an already selected panel. Validate before changing
	// drafts or dispatching commands. Initialization belongs here, never in links.
	/*
	================
	canLeavePanel
	================
	*/
	function canLeavePanel() {
		return panel !== "Magic Pop" || ![ "rolling", "waiting" ].includes( view?.gameplay?.gacha?.phase ?? "" );
	}
	/*
	================
	hudCopy

	Input notifications and rendered dialogs share the system-text catalogue.
	The display-name catalogue cannot resolve UI messages (native 68D430).
	================
	*/
	function hudCopy( key: string ) {
		return hud.data()?.strings[key] ?? "";
	}
	/*
	================
	resurrectionLayout

	Type 4 is confirm box kind 4 at its native geometry; type 8 (an rmut
	revival) is a simple message box sized by its measured line.
	================
	*/
	function resurrectionLayout(
		width: number,
		height: number,
		position: readonly [number, number] | null,
		mutation = false
	) {
		if ( !mutation ) return resurrectionBoxLayout( width, height, position );
		return textMessageBoxLayout(
			width,
			height,
			resurrectionQuestion( true ).map( key => text.run( hudCopy( key ) ).width ),
			position
		);
	}
	/*
	================
	skillWindowWarmPaths

	Every icon the skill window can show for the player's masteries, with the
	focus variant its group rows swap to on hover (see the Skills draw).
	================
	*/
	function skillWindowWarmPaths(): string[] {
		const catalog = hud.data()?.skillUi, masteries = view?.gameplay?.progression?.masteries ?? [];
		if ( !catalog ) return [];
		const { icons, groups } = skillWindowIcons( catalog, masteries.map( m => m.id ) );
		const out: string[] = [];
		for ( const icon of icons ) {
			const path = iconPath( icon );
			if ( path ) out.push( path );
		}
		for ( const icon of groups ) {
			const path = iconPath( icon );
			if ( path ) out.push( path, path.replace( ".png", "_focus.png" ) );
		}
		return out;
	}
	/*
	================
	setPanel
	================
	*/
	/*
	================
	fortressWarView

	The application window's content for the official in conversation:
	his fortresses (matched by RefObjID, 662E80) and the guild's standing.
	================
	*/
	function fortressWarView() {
		const game = view?.gameplay, npc = fortressWarHud.npc();
		const official = view?.entities.find( e => e.gid === npc );
		if ( npc === null || !game?.fortress || !official ) return null;
		const application = game.fortressApplication ?? null,
			social = game.social,
			slots = fortressWarSlots(
				game.fortress,
				official.refObjId,
				application,
				social?.guild?.name ?? "",
				social?.alliances?.map( a => a.name ) ?? []
			);
		return { npc, application, slots };
	}

	/*
 ================
 fortressStaffView

 5D7AD0: only the occupying guild opens employment. All three staff types
 exist in the shipped v1.150 forge group; the master may hire unused bits.
 ================
 */
	function fortressStaffView() {
		const game = view?.gameplay, state = game?.fortress, social = game?.social;
		const world = state?.worlds.find( row => row.id === (state.worldId & 0xffff) );
		const row = state?.fortresses.find( row => row.code === world?.code );
		const owner = state?.wars.find( war => war.id === row?.id )?.name;
		const member = social?.guild?.members.find( member => member.id === social.self );
		return {
			fortress: row?.id,
			holder: !!owner && owner === social?.guild?.name,
			// GuildData_IsAllyGuildName: the holder is one of the local union's guilds.
			ally: !!owner && !!social?.alliances?.some( guild => guild.name === owner ),
			// 5D7AD0 / 5D8930 / 665470 test the fortress commander role
			// (GuildMember_IsFortressRole1), not the guild grade.
			commander: member?.role === FORTRESS_ROLE_COMMANDER,
			member,
			flags: state?.staffFlags ?? 0
		};
	}

	/*
	================
	structureRemoveAction

	The 0x71E1 action the target's delete button sends, or 0 when 516BC0
	hides it (fortressDeleteAction).
	================
	*/
	function structureRemoveAction( target: EntityState ): number {
		const game = view?.gameplay, state = game?.fortress, social = game?.social, kind = fortressTargetKind( target );
		const staff = fortressStaffView();
		if ( !kind || !state || staff.fortress === undefined ) return 0;
		const member = social?.guild?.members.find( row => row.id === social.self );
		return fortressDeleteAction( kind, {
			war: fortressActive( state ),
			holder: staff.holder,
			role: member?.role ?? 0,
			ownObject: !!social?.guild && target.guildId === social.guild.id
		} );
	}

	/*
	================
	fortressProductionItems

	The open window's catalog: SetStaff (65C370) puts every forge-group row
	of its staff member into the one tab, in file order.
	================
	*/
	function fortressProductionItems() {
		const staff = fortressProductionHud.staff();
		return (view?.gameplay?.fortressForge ?? []).filter( item => item.staff === staff );
	}

	/*
	================
	sendFortressProduction

	One 0x71E1 production step for the open window's staff member.
	================
	*/
	function sendFortressProduction(
		smithAction: number,
		extra: { reference?: number; count?: number; stackLimit?: number; }
	) {
		const gid = fortressProductionHud.npc();
		if ( gid === null || view?.session?.phase !== "world" || view.gameplay?.target !== gid ) return;
		sendGameplay( {
			kind: "fortress-production",
			gid,
			fortress: fortressProductionHud.fortress(),
			action: fortressProductionAction( fortressProductionHud.staff(), smithAction ),
			...extra
		} );
	}

	/*
	================
	closeGuide
	================
	*/
	function closeGuide() {
		// 69CAB2 remembers the origin before 69CB0F destroys the guide section.
		if ( panel !== "Game Guide" ) return;
		if ( guideX !== null && guideY !== null ) windowPlacement.remember( "gameGuide", [ guideX, guideY ] );
		guideX = guideY = null;
	}
	/*
	================
	setPanel
	================
	*/
	function setPanel( next: UiPanel | "", intent: "open" | "toggle" | "select" | "warm" = "open" ) {
		// An unseen warm build (window-warm.ts) switches the drawn window only:
		// no enter/leave hooks, sounds or transient resets.
		if ( intent === "warm" ) {
			panel = next;
			return true;
		}
		if ( intent === "toggle" && panel === next ) next = "";
		if ( panel === next ) return false;
		if ( !canLeavePanel() ) return false;
		// Native 6A2350 resolves an owned COS before 69D920 creates its window.
		// Apply the same admission to hotkeys, menu links and contextual opens.
		if ( next === "COS inventory" && !view?.gameplay?.cosRecords?.some( r => !r.dead && r.hp > 0 ) ) {
			return false;
		}
		closeGuide();
		shopOpenRequest = null;
		if ( next !== "Shop" ) repairHud.reset();
		if ( next !== SKIN_PANEL ) skinHud.close();
		if ( next !== FORTRESS_WAR_PANEL ) fortressWarHud.close();
		if ( next !== FORTRESS_SCHEDULE_PANEL ) fortressScheduleHud.close();
		if ( next !== FORTRESS_TAX_PANEL ) fortressTaxHud.close();
		if ( next !== FORTRESS_PRODUCTION_PANEL ) fortressProductionHud.close();
		if ( next !== JOB_RANK_PANEL ) jobHud.closeRank();
		// Leave hooks run only after admission. Never restore drafts or close a
		// server workflow for a rejected switch or a repeated open/select action.
		blockDialog = null;
		blockSelected = "";
		blockOffset = 0;
		if ( shopDialog || shopWarning ) closeShopDialog();
		guildDialog = "";
		practice = null;
		// 69CDF0 owns QuestInfo independently. 589660 hides main-popup pages,
		// including Quests, without closing that details window or its prompt.
		confirmDrop = "";
		confirmSocial = "";
		unionHud.reset();
		guildWarHud.reset();
		grantPowerHud.close();
		if ( next !== "Guild tools" ) socialMember = 0;
		socialPage = 0;
		inventorySlot = -1;
		goldDialog = false;
		splitStack = null;
		partySettings = false;
		carriedItem = null;
		if ( panel === "Option" ) audioPreference( audioSaved, false );
		if ( panel === "Alchemy" ) sendGameplay( { kind: "alchemy-close" } );
		if ( panel === "Magic Pop" ) sendGameplay( { kind: "gacha-close" } );
		if ( panel === GRANT_PANEL ) sendGameplay( { kind: "magic-option-close" } );
		if ( panel && !next ) sound( "close" );
		if ( !next ) {
			for ( const owner of admittedWindows.keys() ) {
				if ( questDetails && (owner === "quest-details" || owner === "quest-abandon") ) continue;
				admittedWindows.delete( owner );
			}
		}
		const wasOpen = !!panel;
		panel = next;
		if ( isMainPopupPage( next ) ) rememberedMainPopup = next;
		// Enter hooks are shared by sidebar, menu, keyboard and contextual links.
		if ( next === "Experimental" ) experimental.open();
		if ( next === "Option" ) {
			videoDraft = videoOptions( video );
			videoScroll = 0;
			videoCombo = VIDEO_COMBO_CLOSED;
			bindingDraft = inputOptions( bindings );
			bindingSelected = -1;
			bindingScroll = 0;
			sightDraft = sight;
			audioDraft = { ...audioSaved };
			optionTab = 0;
			optionDraft = { ...options };
			optionScroll = [ 0, 0 ];
			beginnerDraft = !!((view?.entities.find( e => e.gid === view?.gameplay?.localGid )?.visualFlags ?? 0) & 1);
		}
		if ( next === "COS inventory" ) {
			cosSlot = -1;
			cosPage = 0;
			cosPlayerPage = 0;
			cosDraft = view?.gameplay?.cosRecords?.find( r => r.gid === cosGid )?.commandMode ?? 0;
		}
		if ( next === "Auto Potion" ) {
			potionDraft = autoPotionDraft( view?.gameplay?.autoPotion ?? defaultAutoPotion() );
			potionCombo = "";
		}
		if ( next === "Alchemy" ) {
			alchemySlots = [];
			processPage = 0;
			sendGameplay( { kind: "alchemy-open" } );
		}
		if ( next === "Party Matching" ) {
			partyMatchSelection = 0;
			if ( view?.gameplay?.partyMatching && !view.gameplay.partyMatching.pending ) {
				sendGameplay( { kind: "party-match-page", page: 1 } );
			}
		}
		if ( next === "Academy Matching" ) {
			academySelection = 0;
			if ( view?.gameplay?.academy && !view.gameplay.academy.request ) {
				sendGameplay( { kind: "academy-page", page: 0 } );
			}
		}
		// 69C150 sounds the resulting visible window, including a hotkey retarget.
		// Sidebar selection calls 589960 directly and keeps its button-click cue.
		if ( panel && (!wasOpen || intent !== "select") ) sound( "open" );
		return true;
	}
	/*
	================
	panelNamed

	A panel named by a control id. An unregistered name is a defect in the
	control that carries it, so it is refused loudly instead of opening a
	window no sweep has checked.
	================
	*/
	function panelNamed( name: string ): UiPanel {
		if ( !isUiPanel( name ) ) throw Error( "Unregistered UI panel: " + name );
		return name;
	}
	// Session teardown clears presentation only; it must not send gameplay commands.
	/*
	================
	executeAction
	================
	*/
	function executeAction( id: number ) {
		const game = view?.gameplay;
		if ( !game ) return;
		const windows: Record<number, UiPanel> = { 1010: "Alchemy", 1012: "Auto Potion", 1014: "Academy Matching" };
		if ( windows[id] ) {
			setPanel( windows[id]! );
			dirty = true;
			return;
		}
		if ( id === 1007 ) {
			if ( game.target ) {
				sendGameplay( {
					kind: "party-invite",
					gid: game.target,
					options: effectivePartyOptions( game.social, partyOptions )
				} );
			}
			return;
		}
		if ( id === 1008 ) {
			// The worker picks the item; this frame's view may still hold the last.
			sendGameplay( { kind: "pickup-nearest" } );
			return;
		}
		if ( id === 1002 ) {
			if ( game.target ) sendGameplay( { kind: "attack", gid: game.target } );
			return;
		}
		if ( id === ACTION_STALL ) {
			sendGameplay( { kind: "stall-name", alchemy: panel === "Alchemy" } );
			return;
		}
		if ( id === ACTION_STALL_NETWORK ) {
			sendGameplay( { kind: "stall-network-open", open: !game.stall?.network.open } );
			return;
		}
		if ( id === HELPER_ACTION_ID ) {
			sendGameplay( { kind: "helper-mark" } );
			return;
		}
		if (
			id === 1000 || id === 1001 || id === TRACE_ACTION_ID || id === 5000 || id >= 4000 && id <= 4006 ||
			id === ACTION_FORTRESS_RETURN || id === ACTION_EXCHANGE
		) {
			sendGameplay( { kind: "action-command", id } );
		}
	}
	/*
	================
	resetPanel
	================
	*/
	function resetPanel() {
		compactHud.reset();
		closeGuide();
		withdrawal.close();
		blockDialog = null;
		blockSelected = "";
		blockInput = "";
		blockOffset = 0;
		panel = "";
		admittedWindows.clear();
		practice = null;
		questDetails = false;
		confirmAbandon = false;
		skillTab = 0;
		selectedMastery = 0;
		skillScroll = 0;
		expandedQuests.clear();
		windowMissing = [];
	}
	/*
	================
	closeServer
	================
	*/
	function closeServer() {
		if ( serverList ) {
			serverList = false;
			sound( "close" );
			dirty = true;
		}
	}
	/*
	================
	requestServers
	================
	*/
	function requestServers() {
		pending = true;
		commands( { kind: "servers", apiBase: endpoint } );
	}
	/*
	================
	acceptServer

	Accepts from the rows on screen even while a refresh is in flight: the
	refresh replaces an unavailable selection, and login validates the server.
	================
	*/
	function acceptServer() {
		if ( serverList && servers.some( s => s.id === serverDraft && s.operating ) ) {
			selectedServer = serverDraft;
			closeServer();
		}
	}
	/*
	================
	moveAvatar
	================
	*/
	function moveAvatar( equip: boolean, source: number, destination?: number ) {
		const game = view?.gameplay;
		if ( !game || game.inventoryPending ) return;
		const bag = game.inventory,
			avatars = game.avatarInventory ?? [],
			item = (equip ? bag : avatars).find( i => i.slot === source );
		if ( !item || (item.typeFlags & 0x7fe) !== 0x6ac ) return;
		if ( equip ) {
			if (
				avatars.some( i => i.typeFlags >>> 11 === item.typeFlags >>> 11 ) ||
				item.typeFlags >>> 11 === 3 && !avatars.some( i => i.typeFlags >>> 11 === 2 )
			) return;
			destination = Array.from( { length: 4 }, ( _, i ) => i ).find( i => !avatars.some( a => a.slot === i ) );
		} else {destination ??= Array.from( { length: Math.max( 0, (game.inventorySlotCount ?? 13) - 13 ) }, ( _, i ) =>
				i + 13 ).find( i =>
					!bag.some( a =>
						a.slot === i
					)
				);}
		if (
			destination === undefined || !equip && (destination < 13 || destination >= (game.inventorySlotCount ?? 13))
		) return;
		sendGameplay( { kind: "avatar-move", equip, source, destination } );
		inventorySlot = -1;
	}
	/*
	================
	sendGameplay
	================
	*/
	function sendGameplay( command: Extract<SessionCommand, { kind: "gameplay"; }>["command"] ) {
		if ( command.kind === "inventory-move" ) {
			const inventory = view?.gameplay?.inventory ?? [];
			const { source: sourceSlot, destination: destinationSlot } = command;
			const source = inventory.find( row => row.slot === sourceSlot );
			const target = inventory.find( row => row.slot === destinationSlot );
			if ( source && target ) command = companionItemTargetCommand( source, target ) ?? command;
		}
		if ( command.kind === "item-use" ) {
			const item = view?.gameplay?.inventory.find( row => row.slot === command.slot );
			// 572858 dispatches hotbar items through the same cursor arming as
			// an inventory activation. A confirmed use already has its target.
			if (
				item && armsItemTargetCursor( item.typeFlags ) && command.summonerSlot === undefined &&
				command.revivalSlot === undefined
			) {
				if ( item.slot < 13 || view?.gameplay?.inventoryPending ) return;
				carriedItem = null;
				carriedShortcut = null;
				inventorySlot = -1;
				repairHud.disarm();
				cosHud.armItemTarget( item );
				dirty = true;
				return;
			}
			if ( item && isRestorationPotion( item ) ) {
				withdrawal.open( item.refObjId );
				dirty = true;
				return;
			}
			// 69D4F0 opens CIFWholeChat on the Global Chatting item's slot; its
			// Use button sends the line inside the item's use (6D1EE0).
			if ( item && isGlobalChatItem( item.typeFlags ) && command.message === undefined ) {
				globalChatHud.open( item.slot );
				focusAtEnd( GLOBAL_CHAT_TEXT, "" );
				dirty = true;
				return;
			}
			// 6971B0 case 0x1E: the reverse return scroll asks for its point
			// first; the row's pick runs the use with that byte.
			if ( item && isReverseReturnScroll( item.typeFlags ) && command.reverseChoice === undefined ) {
				if ( !view?.gameplay?.localGid || view.gameplay.inventoryPending || view.travel ) return;
				reverseScrollHud.open( item, view.gameplay.localGid );
				dirty = true;
				return;
			}
			// The skin scroll opens CIFChangePlayerModel; its confirm uses it.
			if ( item && isSkinChangeScroll( item.typeFlags ) && !command.skin ) {
				const game = view?.gameplay, local = view?.entities.find( e => e.gid === game?.localGid );
				if ( !game?.playerModels?.length || !local || !setPanel( SKIN_PANEL ) ) return;
				skinHud.open( item.slot, game.playerModels, local.refObjId, local.bodyShape ?? 0xff );
				dirty = true;
				return;
			}
		}
		commands( { kind: "gameplay", command } );
	}
	/*
	================
	beginWhisper

	6AC0F0 replaces the draft with "$name " and rejects the local name.
	6ACD90 and 6B8600 focus the same editor after selecting a recipient.
	================
	*/
	function beginWhisper( name: string ) {
		if ( !name || name === view?.session?.character || /\s/.test( name ) ) return;
		chatTarget = name;
		chatChannel = 2;
		whispersOpen = false;
		chatText = "$" + name + " ";
		focusAtEnd( "chat-text", chatText );
		dirty = true;
	}
	/*
	================
	executeSkill

	The learned skill board and both shortcut bars share one press path. The
	worker decides a cooling-down press (skill-queue.ts: send, hold or deny),
	so it is forwarded here like any other; the worker and server remain
	responsible for target and actor eligibility.
	================
	*/
	function executeSkill( id: number ) {
		const game = view?.gameplay;
		if ( !game?.skills?.includes( id ) || hud.data()?.tooltipSkills.get( id )?.basicActivity === 0 ) return;
		sendGameplay( { kind: "skill", skillId: id, ...(game.target ? { gid: game.target } : {}) } );
	}
	/*
	================
	useInventorySlot
	================
	*/
	function useInventorySlot( slot: number ): boolean {
		const game = view?.gameplay;
		if ( !game ) return false;
		const command = itemActivation( slot, game.inventory, game.inventorySlotCount, game.inventoryPending );
		if ( !command ) return false;
		sendGameplay( command );
		inventorySlot = -1;
		return true;
	}
	/*
	================
	merchantRows
	================
	*/
	function merchantRows( game: UiView["gameplay"] | undefined ) {
		return game?.shop?.cosGid ?
			game.cosRecords?.find( record => record.gid === game.shop?.cosGid )?.inventory ?? [] :
			game?.inventory ?? [];
	}
	/*
	================
	openShopSale
	================
	*/
	function openShopSale(
		item: NonNullable<UiView["gameplay"]>["inventory"][number],
		quick = false,
		acknowledged = false
	) {
		const game = view?.gameplay;
		if ( !game?.shop ) return;
		if ( game.inventoryPending || game.shop.error || game.target !== game.shop.npc ) return;
		const choice = merchantSelection( "sell", item.slot, game.shop, merchantRows( game ) );
		if ( !choice || choice.binding !== merchantBinding( item ) ) return;
		const sale = game.shop.saleQuotes?.find( q =>
			q.slot === item.slot && (q.cosGid ?? 0) === (game.shop?.cosGid ?? 0) && q.refObjId === item.refObjId &&
			q.quantity === item.quantity
		);
		if ( !acknowledged && sale?.noBuyback ) {
			shopWarning = { selection: choice, name: item.name ?? "", quick };
			focus = null;
			focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
			dirty = true;
			return;
		}
		if ( quick ) {
			sendGameplay( merchantCommand( choice, item.quantity ) );
			return;
		}
		if ( game.shop.cosGid && isTradeGoods( item.typeFlags ) ) {
			openSpecialtySale( choice, item );
			return;
		}
		beginShopDialog( choice, String( item.quantity ) );
		if ( shopChoice ) sendGameplay( { kind: "shop-open", gid: shopChoice.npc } );
	}
	/*
	================
	openSpecialtyPurchase

	CIFSpecialtyDeal_OpenForPackageItem (64A0E0): a trade goods offer bought
	into the transport opens at one full stack (+0x1A8), priced at the
	package's buy price (+0x7D0).
	================
	*/
	function openSpecialtyPurchase( index: number ) {
		const game = view?.gameplay, shop = game?.shop, offer = shop?.offers[index];
		if ( !game || !shop?.cosGid || !offer || game.inventoryPending || shop.error || game.target !== shop.npc ) {
			return;
		}
		const choice = merchantSelection( "buy", index, shop, merchantRows( game ) );
		if ( !choice ) return;
		closeShopDialog();
		specialtyCombo = false;
		specialtyDealHud.open( {
			mode: "buy",
			selection: choice,
			name: offer.name,
			refObjId: offer.refObjId,
			cosGid: shop.cosGid,
			unitBuy: Number( offer.price ),
			stack: offer.maxStack,
			count: offer.maxStack
		} );
		focusAndSelect( SPECIALTY_DEAL_COUNT, 0, String( offer.maxStack ).length );
	}
	/*
	================
	openSpecialtySale

	CIFSpecialtyDeal_OpenForSaleItem (6496E0): the transport's trade goods
	sold back open at the whole stack. The purchase sum uses the goods'
	reference price (RefItemData +0xB0); the sale sum is the server's quote.
	================
	*/
	function openSpecialtySale(
		choice: MerchantSelection,
		item: NonNullable<UiView["gameplay"]>["inventory"][number]
	) {
		const game = view?.gameplay;
		if ( !game?.shop?.cosGid ) return;
		closeShopDialog();
		specialtyCombo = false;
		specialtyDealHud.open( {
			mode: "sell",
			selection: choice,
			name: item.name ?? "",
			refObjId: item.refObjId,
			cosGid: game.shop.cosGid,
			unitBuy: item.tooltip?.fields.price ?? 0,
			stack: item.quantity,
			count: item.quantity
		} );
		focusAndSelect( SPECIALTY_DEAL_COUNT, 0, String( item.quantity ).length );
	}
	/*
	================
	specialtyStock

	The transport's stock of the deal's goods: what each request moved is the
	change in it.
	================
	*/
	function specialtyStock( game: UiView["gameplay"] | undefined ) {
		const deal = specialtyDealHud.state();
		if ( !deal ) return 0;
		return (game?.cosRecords?.find( r => r.gid === deal.cosGid )?.inventory ?? []).reduce(
			( n, row ) => row.refObjId === deal.refObjId ? n + row.quantity : n,
			0
		);
	}
	/*
	================
	specialtyLimit

	The quantity edit's numeric limit: a purchase can spend the whole purse
	(6492C0: CIFEdit_SetNumericLimit64( gold / unit buy )); a sale sells at
	most the transport's stack (6496E0).
	================
	*/
	function specialtyLimit( game: UiView["gameplay"] | undefined ) {
		const deal = specialtyDealHud.state();
		if ( !deal ) return 0;
		if ( deal.mode === "sell" ) return deal.stack;
		if ( deal.unitBuy <= 0 ) return 0;
		const most = BigInt( game?.progression?.gold ?? "0" ) / BigInt( deal.unitBuy );
		return Number( most > BigInt( Number.MAX_SAFE_INTEGER ) ? BigInt( Number.MAX_SAFE_INTEGER ) : most );
	}
	/*
	================
	pickSpecialtyScale

	CIFSpecialtyDeal_OnTradeScaleSelected (64A9E0): a TRADESCALE row sets the
	quantity that purchase scale holds.
	================
	*/
	function pickSpecialtyScale( row: number ) {
		const deal = specialtyDealHud.state(), game = view?.gameplay;
		specialtyCombo = false;
		if ( !deal || deal.mode !== "buy" || deal.dealing ) return;
		const inputs = specialtyInputs( game );
		const quantity = tradeScaleQuantity( {
			row,
			basis: inputs.basis,
			speed2: inputs.speed2,
			unitBuy: deal.unitBuy,
			maxStack: deal.stack,
			capacity: inputs.capacity
		} );
		specialtyDealHud.type( String( quantity ), specialtyLimit( game ) );
	}
	/*
	================
	sendSpecialtyChunk

	One CGInterface_RequestItemMove of the deal loop (64A660): the shop or
	transport move of this many goods, through the same merchant command the
	shop dialog sends.
	================
	*/
	function sendSpecialtyChunk( quantity: number ) {
		const deal = specialtyDealHud.state();
		if ( deal && quantity > 0 ) sendGameplay( merchantCommand( deal.selection, quantity ) );
	}
	/*
	================
	confirmSpecialtyDeal

	CIFSpecialtyDeal_OnConfirm (64A8F0): starts the loop from the transport's
	stock; a purchase past one stack raises the "currently purchasing" notice.
	================
	*/
	function confirmSpecialtyDeal() {
		const deal = specialtyDealHud.state(), game = view?.gameplay;
		if ( !deal || deal.dealing || !game?.shop || game.inventoryPending || game.target !== deal.selection.npc ) {
			return;
		}
		const started = specialtyDealHud.confirm( specialtyStock( game ), uiNow );
		specialtyCombo = false;
		if ( !started ) return;
		if ( started.progress ) hudMessages.append( hudCopy( "UIIT_MSG_SPECIALTY_ALLBUY_PROGRESS_WINDOW" ) );
		sendSpecialtyChunk( started.chunk );
	}
	/*
	================
	specialtyInputs

	The trade scale's inputs: the levelgold basis at the local level, the
	transport's reference run speed and bag capacity. The client ships the
	English text tables (type.txt Language = English), so the combo lists all
	five TRADESCALE rows unless the shard rule says otherwise.
	================
	*/
	function specialtyInputs( game: UiView["gameplay"] | undefined ) {
		const deal = specialtyDealHud.state(), data = hud.data();
		const record = deal ? game?.cosRecords?.find( r => r.gid === deal.cosGid ) : undefined;
		const level = game?.progression?.level ?? 0;
		return {
			basis: tradeGoldBasis( data?.tradeGoldBases[level] ?? 0 ),
			speed2: record ? data?.cosReferences.get( record.refObjId )?.speed2 ?? 0 : 0,
			capacity: record?.status ?? 0,
			rows: tradeScaleRows( NATIVE_LANGUAGE_ENGLISH, view?.session?.nativeServerName ),
			gold: BigInt( game?.progression?.gold ?? "0" )
		};
	}
	/*
	================
	updateAudioDraft
	================
	*/
	function updateAudioDraft( value: AudioOptions ) {
		audioDraft = value;
		audioPreference( audioDraft, false );
	}
	/*
	================
	appendConsoleLine
	================
	*/
	function appendConsoleLine( value: string ) {
		consoleRows = [ ...consoleRows.slice( -99 ), value ];
	}
	/*
	================
	runDebugCommand

	CGInterface_OnCommandMessage (690C40) for the console family. The other
	command.txt families (camera, weather, time, render toggles) are not
	ported yet and leave the line as typed.
	================
	*/
	function runDebugCommand( command: DebugCommand ) {
		switch ( command.id ) {
			case DEBUG_COMMAND_DEBUG:
				// Case 0 flips +0x7BC and reports the new state.
				debugMessages = !debugMessages;
				appendConsoleLine( debugMessages ? "DebugMsg On" : "DebugMsg Off" );
				break;
			case DEBUG_COMMAND_MESSAGE_CLEAR:
				// Case 1 empties the console's lines, the command's echo included.
				consoleRows = [];
				break;
			case DEBUG_COMMAND_NULL:
				// Port-only, not native: case 4 writes through a null pointer to
				// crash the client on purpose; the port ignores the line.
				break;
			case DEBUG_COMMAND_PLAYER_COUNT:
				// Id 0 is below the 691CC4 table: 690C40 does nothing with it.
				break;
			case DEBUG_COMMAND_ITEM:
				// The 0x190 branch: exactly one word, read by wcstol, then
				// CIFInventory_ExecuteItemAction(slot, -1, -1) as a double click.
				if ( command.args.length === 1 ) useInventorySlot( commandInteger( command.args[0]! ) );
				break;
		}
	}
	/*
	================
	focusAtEnd
	================
	*/
	function focusAtEnd( id: string, value: string ) {
		if ( id === "chat-text" ) compactHud.open( "chat" );
		focus = id;
		selection = [ value.length, value.length ];
		focusRequest = { id, revision: ++focusRevision, caret: value.length };
	}
	/*
	================
	focusAndSelect
	================
	*/
	function focusAndSelect( id: string, start: number, end: number ) {
		focus = id;
		selection = [ start, end ];
		focusRequest = { id, revision: ++focusRevision, caret: end, anchor: start };
	}
	/*
	================
	openStallPrompt
	================
	*/
	function openStallPrompt( prompt: StallPrompt ) {
		stallHud.open( prompt );
		if ( prompt.kind === "title" || prompt.kind === "greeting" ) {
			focusAndSelect( STALL_PROMPT_TEXT, 0, prompt.text.length );
		} else if ( prompt.kind === "price" ) focusAndSelect( STALL_PROMPT_PRICE, 0, prompt.price.length );
		dirty = true;
	}
	/*
	================
	sendGlobalChat

	CIFGlobalChatItem_OnSend (6D1EE0): with an item left, an empty line
	prints UIIT_STT_WHOLECHAT_INPUTMSG and a line the abuse filter refuses
	(CStringCheck_ContainsAbuse 790C70, the same two tests as 790B60)
	prints UIIT_MSG_WHOLECHATERR_NOTINUSEMSG; otherwise the item is used
	with the line. The line is cleared either way and the window stays.
	================
	*/
	function sendGlobalChat( slot: number, line: string ) {
		const item = view?.gameplay?.inventory.find( row => row.slot === slot );
		if ( item && isGlobalChatItem( item.typeFlags ) && item.quantity > 0 ) {
			const rules = hud.data()?.nameRules;
			if ( !line ) hudMessages.append( hudCopy( "UIIT_STT_WHOLECHAT_INPUTMSG" ) );
			else if ( rules && !textAllowed( line, rules ) ) {
				hudMessages.append( hudCopy( "UIIT_MSG_WHOLECHATERR_NOTINUSEMSG" ) );
			} else sendGameplay( { kind: "item-use", slot, message: line } );
		}
		globalChatHud.sent();
		focusAtEnd( GLOBAL_CHAT_TEXT, "" );
	}
	/*
	================
	answerStallPrompt

	CIFStall_OnTitleDialogResult 5A2890: a title the abuse filter refuses
	(CStringCheck_IsTextAllowed 790B60) raises 0x0A/0x38 and gives up the
	stall being named.
	================
	*/
	function answerStallPrompt( accept: boolean ) {
		const prompt = stallHud.prompt(), stall = view?.gameplay?.stall;
		stallHud.close();
		focus = null;
		focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
		dirty = true;
		if ( !prompt || !stall ) return;
		const rules = hud.data()?.nameRules;
		if ( accept && prompt.kind === "title" && rules && !textAllowed( prompt.text, rules ) ) {
			hudMessages.append( hudCopy( STALL_TITLE_REFUSED ) );
			if ( stall.phase === "naming" ) sendGameplay( { kind: "stall-name-cancel" } );
			return;
		}
		const greeting = hudCopy( "UIIT_STT_STALL_DEFAULT_OWNERMSG" ).replace( "%s", view?.session?.character ?? "" );
		const command = stallPromptCommand( prompt, stall, accept, greeting );
		if ( command ) sendGameplay( command );
	}
	/*
	================
	beginShopDialog
	================
	*/
	function beginShopDialog( choice: MerchantSelection | null, quantity: string ) {
		shopChoice = choice;
		shopQuantity = quantity;
		shopDialog = !!choice;
		shopPosition = null;
		composing = false;
		const game = view?.gameplay,
			quote = merchantQuote( choice, game?.shop, merchantRows( game ), quantity, game?.progression?.gold );
		if ( quote?.quantityMode === "editable" ) focusAndSelect( "shop-quantity", 0, quantity.length );
		else {
			focus = null;
			focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
		}
	}
	/*
	================
	closeShopDialog
	================
	*/
	function closeShopDialog() {
		shopWarning = null;
		shopDialog = false;
		shopChoice = null;
		shopPosition = null;
		focus = null;
		composing = false;
		focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
	}
	/*
	================
	cosCommandContext

	What the command bar reads about the selected companion (6A1BE0).
	================
	*/
	function cosCommandContext(): CosCommandContext {
		const game = view?.gameplay, record = game?.cosRecords?.find( r => r.gid === cosHud.selected() );
		const local = view?.entities.find( e => e.gid === game?.localGid );
		return {
			record,
			reference: record && hud.data()?.cosReferences.get( record.refObjId ),
			ownerDead: !!game?.vitals.find( v => v.gid === game.localGid )?.deathState,
			mounted: !!local?.mountedOn
		};
	}
	/*
	================
	executeCosCommand

	CICCos_ExecuteActionCommand (6A2350). Info opens even while the owner is
	dead; every other command needs the arm 6A1BE0 shows enabled.
	================
	*/
	function executeCosCommand( command: number ) {
		const context = cosCommandContext(), record = context.record;
		if ( !record || !cosCommandEnabled( command, context ) ) return;
		const cls = cosClass( record.band );
		switch ( command ) {
			case COS_COMMAND_INFO:
				if ( cls === COS_CLASS_GUILD ) return;
				if ( panel === "COS inventory" ) {
					setPanel( "" );
					return;
				}
				// CGInterface_SetCosInventoryVisible's third argument opens a
				// transport's or pickup pet's container page.
				cosGid = record.gid;
				if ( setPanel( "COS inventory" ) ) {
					cosTab = (cls === COS_CLASS_TRANSPORT || cls === COS_CLASS_PICKUP) && record.inventory ? 1 : 0;
				}
				return;
			case COS_COMMAND_RIDE:
				sendGameplay( { kind: "cos-ride", gid: record.gid, mounted: !context.mounted } );
				return;
			case COS_COMMAND_ATTACK:
				// The selected target is the pet's; the client attacks monsters only.
				if ( view?.gameplay?.target ) {
					sendGameplay( { kind: "cos-pet-attack", gid: view.gameplay.target, pet: record.gid } );
				}
				return;
			case COS_COMMAND_FOLLOW:
				sendGameplay( { kind: "cos-follow", gid: record.gid } );
				return;
			case COS_COMMAND_CANCEL:
				sendGameplay( { kind: "cos-cancel", gid: record.gid } );
				return;
			case COS_COMMAND_CLEAN:
				// A transport asks first (type 0xD box); its cargo is dropped.
				if ( cls === COS_CLASS_TRANSPORT ) cosHud.askClean( record.gid );
				else sendGameplay( { kind: "cos-clean", gid: record.gid } );
				dirty = true;
				return;
			case COS_COMMAND_STANCE:
				if ( cls === COS_CLASS_ATTACK ) {
					sendGameplay( {
						kind: "cos-behavior",
						gid: record.gid,
						mode: cosStanceToggle( record.commandMode )
					} );
				}
				return;
		}
	}
	/*
	================
	activate
	================
	*/
	function activate( id: string ) {
		if ( panel === "Experimental" && id.startsWith( "experimental-" ) ) {
			const row = EXPERIMENTAL_TABS.flatMap( tab => tab.rows ).find( candidate => candidate.id === id );
			if ( id.startsWith( "experimental-tab:" ) ) experimental.selectTab( Number( id.slice( 17 ) ) );
			else if ( id.startsWith( "experimental-render-scale:" ) ) {
				experimental.selectRenderScale( Number( id.slice( "experimental-render-scale:".length ) ) );
			} else if ( row && row.key !== "renderScale" ) experimental.toggle( row.key );
			else if ( id === "experimental-default" ) experimental.reset();
			else if ( id === "experimental-confirm" ) {
				extensions.saveExperimental?.( experimental.confirm() );
				setPanel( "" );
			} else if ( id === "experimental-cancel" ) setPanel( "" );
			dirty = true;
			return;
		}
		// CIFCOSStatus maps no left-click message (6A9BC0: only 0x8074 and
		// 0xD7), so the portrait's own 0x27 does nothing; selection is the
		// right-button release (6A9E40), a touch hold on touch screens.
		if ( id.startsWith( "cos-status:" ) ) return;
		if ( id === "cos-command-toggle" ) {
			cosHud.toggle();
			dirty = true;
			return;
		}
		if ( id.startsWith( "cos-command:" ) ) {
			executeCosCommand( Number( id.slice( 12 ) ) );
			return;
		}
		if ( id.startsWith( "item-mall-" ) && itemMall.compactAction( id ) ) {
			dirty = true;
			return;
		}
		if ( id === "mall-notice-close" || id === "mall-notice-enter" ) {
			// Button 5 closes the notice; button 4 (UIIT_STT_SILKMALL_DIRECT_ENTER)
			// enters the mall. INFERENCE: the notice has done its job once the
			// mall opens, so entering closes it too.
			mallNotice = "closed";
			dirty = true;
			if ( id === "mall-notice-close" ) return;
			id = "item-mall";
		}
		if ( id === "item-mall" ) {
			if ( view?.gameplay && !view.gameplay.inventoryPending ) sendGameplay( { kind: "mall-open" } );
			itemMall.open();
			dirty = true;
			return;
		}
		// The frame's X and the window's own Close button (node 8) both close it.
		if ( id === "item-mall-close" || id === "item-mall-close-button" ) {
			itemMall.close();
			dirty = true;
			return;
		}
		if ( id === "item-mall-home" ) {
			itemMall.browse( -1 );
			dirty = true;
			return;
		}
		if ( id.startsWith( "item-mall-category:" ) ) {
			itemMall.browse( Number( id.slice( "item-mall-category:".length ) ) );
			dirty = true;
			return;
		}

		if ( id.startsWith( "item-mall-tab:" ) && view?.gameplay ) {
			const state = itemMall.read( view.gameplay.itemMall );
			itemMall.browse( state.category, Number( id.slice( "item-mall-tab:".length ) ) );
			dirty = true;
			return;
		}
		if ( id.startsWith( "item-mall-buy:" ) && view?.gameplay?.itemMall ) {
			const state = itemMall.read( view.gameplay.itemMall );
			const offer = state.offers[Number( id.slice( "item-mall-buy:".length ) )];
			if ( offer && !view.gameplay.inventoryPending ) itemMall.choose( offer );
			dirty = true;
			return;
		}
		if ( id.startsWith( "item-mall-page:" ) && view?.gameplay ) {
			const state = itemMall.read( view.gameplay.itemMall );
			itemMall.paginate( Number( id.slice( "item-mall-page:".length ) ), state.count );
			dirty = true;
			return;
		}
		if ( id.startsWith( "item-mall-bag-page:" ) && view?.gameplay ) {
			const game = view.gameplay;
			const slots = inventorySlots( 0, 0, game.inventorySlotCount ?? 0, game.equipmentSlotCount ?? 13, 0 );
			itemMall.selectBagPage( Number( id.slice( "item-mall-bag-page:".length ) ), slots.pages );
			dirty = true;
			return;
		}
		if ( id.startsWith( "item-mall-reserve:" ) && view?.gameplay?.itemMall ) {
			const state = itemMall.read( view.gameplay.itemMall );
			const offer = state.offers[Number( id.slice( "item-mall-reserve:".length ) )];
			if ( offer && !view.gameplay.inventoryPending ) itemMall.askReserve( offer );
			dirty = true;
			return;
		}
		if ( id.startsWith( "item-mall-wear:" ) && view?.gameplay?.itemMall ) {
			const state = itemMall.read( view.gameplay.itemMall );
			const offer = state.offers[Number( id.slice( "item-mall-wear:".length ) )];
			if ( offer && itemMall.wearEnabled( offer, view.gameplay.itemMall ) ) {
				if ( itemMall.canWear( offer, view.gameplay.itemMall ) ) itemMall.wear( offer, view.gameplay.itemMall );
				else hudMessages.append( hudCopy( "UIIT_MSG_STRGERR_EQUIPITEM" ), 0xffff7070 );
			}
			dirty = true;
			return;
		}
		if ( (id === "item-mall-root:4" || id === "item-mall-buy-all") && view?.gameplay?.itemMall ) {
			itemMall.askBatch( id === "item-mall-root:4" ? "worn" : "basket", view.gameplay.itemMall );
			dirty = true;
			return;
		}
		if ( id === "item-mall-question-confirm" && view?.gameplay?.itemMall ) {
			if ( !view.gameplay.inventoryPending ) itemMall.confirmQuestion( view.gameplay.itemMall );
			dirty = true;
			return;
		}
		if ( id === "item-mall-question-cancel" ) {
			itemMall.cancelQuestion();
			dirty = true;
			return;
		}
		if ( id === "item-mall-root:5" ) {
			itemMall.takeOff();
			dirty = true;
			return;
		}

		if ( id === "item-mall-points" ) {
			itemMall.showPoints();
			dirty = true;
			return;
		}
		if ( id === "item-mall-points-close" ) {
			itemMall.closePoints();
			dirty = true;
			return;
		}
		if ( id === "item-mall-points-apply" && view?.gameplay?.itemMall ) {
			const state = itemMall.read( view.gameplay.itemMall );
			itemMall.edit( state.quantity, state.pointDraft, view.gameplay.itemMall );
			itemMall.closePoints();
			dirty = true;
			return;
		}

		if ( id === "item-mall-purchase" && view?.gameplay?.itemMall ) {
			const request = itemMall.purchase( view.gameplay.itemMall );
			if ( request && !view.gameplay.inventoryPending ) sendGameplay( { kind: "mall-buy", request } );
			dirty = true;
			return;
		}
		if ( id === "item-mall-cancel" ) {
			itemMall.cancel();
			dirty = true;
			return;
		}
		if ( (id === "item-mall-quantity-up" || id === "item-mall-quantity-down") && view?.gameplay?.itemMall ) {
			const state = itemMall.read( view.gameplay.itemMall );
			itemMall.edit( state.quantity + (id.endsWith( "-up" ) ? 1 : -1), state.points, view.gameplay.itemMall );
			dirty = true;
			return;
		}

		if ( id === "return-cancel" ) {
			sendGameplay( { kind: "return-cancel" } );
			return;
		}
		if ( id === "gathering-cancel" ) {
			sendGameplay( { kind: "gathering-cancel" } );
			return;
		}
		if ( !view ) return;
		if ( groundDrop ) {
			if ( id === "ground-drop-cancel" ) {
				groundDrop = null;
				dirty = true;
			} else if ( id === "ground-drop-confirm" && !view.gameplay?.inventoryPending ) {
				const expected = groundDrop, item = view.gameplay?.inventory.find( i => i.slot === expected.slot );
				groundDrop = null;
				if ( item?.refObjId === expected.refObjId ) sendGameplay( { kind: "item-drop", slot: item.slot } );
				dirty = true;
			}
			return;
		}
		if ( id.startsWith( "withdrawal-" ) ) {
			if ( id === "withdrawal-close" ) {
				if ( withdrawal.boundToNpc() ) sendGameplay( { kind: "npc-close" } );
				withdrawal.close();
			} else if ( id === "withdrawal-cancel" ) withdrawal.select( "" );
			else if ( id === "withdrawal-decrease" ) withdrawal.adjust( 1 );
			else if ( id === "withdrawal-recover" ) withdrawal.adjust( -1 );
			else if ( id.startsWith( "withdrawal-choice:" ) ) {
				withdrawal.select( id.slice( "withdrawal-choice:".length ) );
			} else if ( id === "withdrawal-confirm" && view.gameplay ) {
				const state = withdrawal.read(
					view.gameplay,
					hud.data()?.masteryCosts ?? {},
					hud.data()?.withdrawalGoldPrices
				);
				if ( state.command ) {
					sendGameplay( state.command );
					withdrawal.select( "" );
				}
			}
			dirty = true;
			return;
		}
		if ( withdrawal.confirming() ) return;
		if ( practice && !id.startsWith( "skill-confirm-" ) ) return;
		if ( confirmAbandon && !id.startsWith( "quest-abandon-" ) ) return;
		const phase = view.session?.phase ?? "signed-out";
		if ( npcPanel.event( { kind: "activate", id } ) ) {
			dirty = true;
			return;
		}
		if ( id === "npc-recall-designate" ) {
			const g = view.gameplay, c = g?.npcConversation;
			if ( c?.phase === "menu" && c.gid === g?.target && ((g.targetCapabilities ?? 0) & 0x40) ) {
				recallConfirm = c.gid;
				focus = null;
				composing = false;
				dirty = true;
			}
			return;
		}
		if ( id.startsWith( "npc-guild-soldier:" ) && guildManagerHud.soldiers() ) {
			const conversation = view.gameplay?.npcConversation;
			if ( conversation?.phase === "menu" ) {
				sendGameplay( {
					kind: "guild-soldier-attribute",
					gid: conversation.gid,
					attribute: Number( id.slice( "npc-guild-soldier:".length ) )
				} );
			}
			return;
		}
		if ( id.startsWith( "npc-guild:" ) ) {
			// 5DA1B0 cases 0x12..0x1D: the guild manager's rows (guild-manager.ts).
			const conversation = view.gameplay?.npcConversation, social = view?.gameplay?.social;
			if ( !conversation || conversation.phase !== "menu" ) return;
			const npc = conversation.gid, row = id.slice( 10 );
			if ( row === "create" || row === "master-leave" ) {
				guildManagerHud.openField( row, npc );
				focusAtEnd( "guild-manager-text", "" );
			} else if ( row === "level-up" ) guildManagerHud.ask( "level-up", npc, social?.guild?.level ?? 0 );
			else if ( row === "dissolve" || row === "secede" || row === "release" ) guildManagerHud.ask( row, npc );
			else if ( row === "compensation" ) sendGameplay( { kind: "guild-compensation", gid: npc } );
			else if ( row === "vote" ) {
				const vote = social?.guild?.votes?.find( v => v.kind === MASTER_RELEASE_VOTE );
				if ( vote ) guildManagerHud.showVote( vote.remainingMs );
			} else if ( row === "warehouse" ) {
				if ( !canLeavePanel() ) return;
				sendGameplay( { kind: "storage-open-guild", gid: npc } );
				storagePanel.reset();
				setPanel( "Storage" );
			}
			dirty = true;
			return;
		}
		if ( id.startsWith( "npc-job-" ) ) {
			// 5DA1B0 cases 0x1E, 0x1F and 0x20/0x21: the join and withdrawal
			// questions and the alias window, for the guild NPC in conversation.
			const conversation = view.gameplay?.npcConversation, job = Number( id.slice( id.indexOf( ":" ) + 1 ) );
			if ( !conversation || conversation.phase !== "menu" ) return;
			if ( id.startsWith( "npc-job-join:" ) ) jobHud.ask( "join", conversation.gid, job );
			else if ( id.startsWith( "npc-job-withdraw:" ) ) jobHud.ask( "withdraw", conversation.gid, job );
			else if ( id.startsWith( "npc-job-alias:" ) ) {
				jobHud.openAlias( conversation.gid, !!view.gameplay?.job?.alias );
				focusAtEnd( "job-alias-text", "" );
			} else if ( id.startsWith( "npc-job-rank:" ) || id.startsWith( "npc-job-contribution:" ) ) {
				// Cases 0x22 and 0x23: the cached list, or 0x737E for it.
				sendGameplay( {
					kind: "job-rank",
					gid: conversation.gid,
					job,
					rank: id.startsWith( "npc-job-rank:" ) ? JOB_RANK_ACTIVITY : JOB_RANK_CONTRIBUTION
				} );
			} else if ( id.startsWith( "npc-job-outcome:" ) || id.startsWith( "npc-job-collect:" ) ) {
				// Cases 0x24 and 0x25: tell, then collect, the week's outcome.
				sendGameplay( {
					kind: "job-outcome",
					gid: conversation.gid,
					mode: id.startsWith( "npc-job-outcome:" ) ? JOB_OUTCOME_QUERY : JOB_OUTCOME_COLLECT
				} );
			} else if ( id.startsWith( "npc-job-previous:" ) ) {
				sendGameplay( { kind: "job-previous", gid: conversation.gid } );
			}
			dirty = true;
			return;
		}
		if ( id.startsWith( "npc-reverse-return:" ) ) {
			const conversation = view.gameplay?.npcConversation;
			if ( conversation && conversation.phase === "menu" ) {
				sendGameplay( {
					kind: "travel-gate",
					gid: conversation.gid,
					type: 5,
					target: Number( id.slice( 19 ) )
				} );
			}
			return;
		}
		if ( id.startsWith( "npc-portal:" ) ) {
			const conversation = view.gameplay?.npcConversation;
			if ( conversation && conversation.phase === "menu" ) {
				sendGameplay( {
					kind: "travel-gate",
					gid: conversation.gid,
					type: 2,
					target: Number( id.slice( 11 ) )
				} );
			}
			return;
		}
		if ( id === "npc-talk" || id === "npc-close" || id === "npc-talkend" || id.startsWith( "npc-choice:" ) ) {
			sendGameplay(
				id === "npc-talk" ?
					{ kind: "npc-talk" } :
					id.startsWith( "npc-choice:" ) ?
					{ kind: "npc-choice", choice: Number( id.slice( 11 ) ) } :
					{ kind: "npc-close" }
			);
			dirty = true;
			return;
		}
		if ( id === "rebirth-point" || id === "rebirth-alternate" ) {
			if ( !rebirthDue || deathDismissed ) return;
			if ( id === "rebirth-alternate" && (view.gameplay?.progression?.level ?? Infinity) > 10 ) {
				deathDismissed = true;
				dirty = true;
				return;
			}
			sendGameplay( { kind: "rebirth", choice: id === "rebirth-point" ? 1 : 2 } );
			dirty = true;
			return;
		}
		if ( stallHud.prompt() ) {
			if ( id === "submit" ) id = "stall-prompt-ok";
			if ( id === "stall-prompt-ok" || id === "stall-prompt-cancel" ) {
				answerStallPrompt( id === "stall-prompt-ok" );
			}
			return;
		}
		if ( splitStack ) {
			if ( id === "submit" ) id = "split-confirm";
			if ( ![ "split-confirm", "split-cancel" ].includes( id ) ) return;
			if ( id === "split-confirm" ) {
				const game = view.gameplay,
					item = game?.inventory.find( i => i.slot === splitStack!.slot ),
					quantity = Math.max(
						1,
						Math.min( splitStack.quantity - 1, Math.trunc( Number( splitAmount ) || 1 ) )
					),
					destination = Array.from( {
						length: Math.max( 0, (game?.inventorySlotCount ?? 13) - (game?.equipmentSlotCount ?? 13) )
					}, ( _, i ) => i + (game?.equipmentSlotCount ?? 13) ).find( slot =>
						!game?.inventory.some( i => i.slot === slot )
					);
				if (
					game && !game.inventoryPending && item?.refObjId === splitStack.refObjId &&
					item.quantity === splitStack.quantity && destination !== undefined
				) sendGameplay( { kind: "inventory-move", source: item.slot, destination, quantity } );
			}
			splitStack = null;
			focus = null;
			dirty = true;
			return;
		}
		if ( goldDialog ) {
			if ( id === "submit" ) id = "drop-gold";
			if ( ![ "drop-gold", "gold-cancel" ].includes( id ) ) return;
		}
		if ( blockDialog ) {
			if ( id === "submit" ) id = "blocking-ok";
			if ( ![ "blocking-ok", "blocking-cancel" ].includes( id ) ) return;
		}
		if ( id === "submit" && consolePhase !== 0 && view.gameplay?.eligibility?.gm ) {
			if ( consoleText && !composing ) {
				appendConsoleLine( consoleText );
				consoleHistory = [ ...consoleHistory.slice( -199 ), consoleText ];
				consoleHistoryIndex = consoleHistory.length;
				// 50F260 tries a GM command, then the command.txt table. Their names
				// are disjoint (the table lookup is case-sensitive), so the table
				// match decides which owner takes the line.
				const command = debugCommand( hud.data()?.commandTable ?? new Map(), consoleText );
				if ( command ) runDebugCommand( command );
				else sendGameplay( { kind: "gm-command", line: consoleText } );
				consoleText = "";
				selection = [ 0, 0 ];
				focusRequest = { id: "gm-input", revision: ++focusRevision, caret: 0 };
				dirty = true;
			}
			return;
		}
		if ( id === "submit" && focus === STALL_CHAT_TEXT ) id = "stall-chat-send";
		if ( id === "submit" && shopDialog && panel === "Shop" ) {
			if ( composing ) return;
			id = "shop-trade";
		}
		if ( id === "submit" ) {
			if ( view.frontend?.dialog || view.frontend?.creation ) return;
			id = phase === "world" ? "chat-send" : phase === "character-select" ? "enter" : "login";
		}
		const control = controls.find( c => c.id === id );
		if ( control?.disabled ) return;
		// World map town icons are CIFWorldMap hit areas (57F43D button-up), not
		// CIFButtons; retail plays no click for them.
		if ( control?.kind === "button" && !id.startsWith( "server:" ) && !id.startsWith( "map-town:" ) ) clickSound();
		if (
			control && (chatScroll.event( { kind: "activate", id } ) || statusScroll.event( { kind: "activate", id } ))
		) {
			dirty = true;
			return;
		}
		if ( id.startsWith( "blocking-tab:" ) ) {
			blockTab = id === "blocking-tab:1" ? 1 : 0;
			blockSelected = "";
			blockOffset = 0;
		} else if ( id.startsWith( "blocking-row:" ) ) {
			const rows = blockTab === 0 ? view.gameplay?.chat?.blocked ?? [] : localBlocks;
			blockSelected = rows[Number( id.slice( 13 ) )] ?? "";
		} else if ( id === "blocking-up" ) blockOffset = Math.max( 0, blockOffset - 1 );
		else if ( id === "blocking-down" ) blockOffset++;
		else if ( id === "blocking-add" || id === "blocking-remove" ) {
			if ( blockTab === 0 && view.gameplay?.chat?.blockPending ) return;
			if ( id === "blocking-remove" && !blockSelected ) return;
			blockDialog = id === "blocking-add" ? "add" : "remove";
			blockInput = "";
			focus = blockDialog === "add" ? "blocking-name" : null;
			selection = [ 0, 0 ];
			focusRequest = { id: focus, revision: ++focusRevision, caret: 0 };
		} else if ( id === "blocking-cancel" ) {
			blockDialog = null;
			focus = null;
			focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
		} else if ( id === "blocking-ok" && blockDialog ) {
			const name = blockDialog === "add" ? blockInput : blockSelected, add = blockDialog === "add";
			if ( name ) {
				if ( blockTab === 1 ) {
					const key = add ? chatBlockError( name, localBlocks ) : null;
					if ( key ) hudMessages.append( hud.data()?.strings[key] ?? "" );
					else {try {
							saveBlocks( add ? [ ...localBlocks, name ] : localBlocks.filter( n => n !== name ) );
						} catch ( error ) {
							hudMessages.append( "Could not save block list: " + String( error ) );
							dirty = true;
							return;
						}}
				} else {
					const rules = hud.data()?.nameRules;
					if ( add && !rules ) return;
					const key = add ? checkedNameError( name, rules! ) : null;
					if ( key ) hudMessages.append( hud.data()?.strings[key] ?? "" );
					else sendGameplay( { kind: "whisper-block", name, blocked: add } );
				}
			}
			blockDialog = null;
			focus = null;
			focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
		} else if ( id === "stat-str" || id === "stat-int" ) {
			sendGameplay( { kind: "stat-increase", stat: id === "stat-str" ? "str" : "int" } );
		} else if ( id.startsWith( "option-tab:" ) ) {
			const index = Number( id.slice( 11 ) );
			if ( Number.isInteger( index ) && index >= 0 && index < 5 ) {
				optionTab = index;
				bindingSelected = -1;
				videoCombo = VIDEO_COMBO_CLOSED;
			}
		} else if ( id.startsWith( "option-toggle:" ) ) {
			const key = gameOption( id.slice( 14 ) );
			optionDraft = { ...optionDraft, [key]: !optionDraft[key] };
		} else if ( id.startsWith( "option-video-record:" ) ) {
			const active = Number( id.slice( 20 ) );
			if ( active === 0 || active === 1 ) {
				videoDraft = { ...videoDraft, active };
				videoCombo = VIDEO_COMBO_CLOSED;
			}
		} else if ( id.startsWith( "option-video-combo:" ) ) {
			const n = Number( id.slice( 19 ) );
			if ( n === -1 || n === VIDEO_FRAME_LIMIT_SLOT || videoRows().some( r => r.slot === n ) ) {
				videoCombo = videoCombo === n ?
					VIDEO_COMBO_CLOSED :
					n;
			}
		} else if ( id.startsWith( "option-video-choice:" ) ) {
			const [, slot, value] = id.split( ":" );
			if ( Number( slot ) === VIDEO_FRAME_LIMIT_SLOT ) {
				const frameLimit = frameLimits()[Number( value )];
				if ( frameLimit !== undefined ) videoDraft = { ...videoDraft, frameLimit };
			} else if ( Number( slot ) === -1 ) {
				const size = displaySizes()[Number( value )];
				if ( size ) {
					const { displaySize: _previous, ...rest } = videoDraft;
					videoDraft = size[0] ? { ...rest, displaySize: size } : rest;
				}
			} else videoDraft = changeVideo( videoDraft, Number( slot ), Number( value ) );
			videoCombo = VIDEO_COMBO_CLOSED;
		} else if ( id === "option-video-up" || id === "option-video-down" ) {
			videoScroll = Math.max( 0, Math.min( VIDEO_SCROLL_MAX, videoScroll + (id.endsWith( "up" ) ? -1 : 1) ) );
			videoCombo = VIDEO_COMBO_CLOSED;
		} else if ( id.startsWith( "option-bind:" ) ) {
			const n = Number( id.slice( 12 ) );
			if ( Number.isInteger( n ) && n >= 0 && n < 34 ) bindingSelected = n;
		} else if ( id.startsWith( "option-mouse:" ) ) {
			const n = Number( id.slice( 13 ) );
			if ( n === 0 || n === 1 ) bindingDraft = { ...bindingDraft, mouseMode: n };
		} else if ( id === "option-input-up" || id === "option-input-down" ) {
			bindingScroll = Math.max( 0, Math.min( 12, bindingScroll + (id.endsWith( "up" ) ? -1 : 1) ) );
		} else if ( id.startsWith( "option-sight:" ) ) sightDraft = sightMode( Number( id.slice( 13 ) ) );
		else if ( id === "option-beginner" ) beginnerDraft = !beginnerDraft;
		else if ( id.startsWith( "option-mute:" ) ) {
			const key = id.slice( 12 );
			if ( key === "muteBgm" || key === "muteEffects" || key === "muteEnvironment" ) {
				updateAudioDraft( { ...audioDraft, [key]: !audioDraft[key] } );
			}
		} else if ( id.startsWith( "option-audio-step:" ) ) {
			const [, key, delta] = id.split( ":" );
			if ( (key === "bgm" || key === "effects" || key === "environment") && (delta === "-1" || delta === "1") ) {
				updateAudioDraft( {
					...audioDraft,
					[key]: stepAudioLevel( audioDraft[key], Number( delta ) )
				} );
			}
		} else if ( id === "option-default" ) {
			if ( optionTab === 0 ) {
				videoDraft = resetVideoRecord( videoDraft );
				videoCombo = VIDEO_COMBO_CLOSED;
			} else if ( optionTab === 1 ) updateAudioDraft( defaultAudioOptions() );
			else if ( optionTab === 2 ) {
				sightDraft = 0;
				sight = 0;
				saveSight( 0 );
			} else if ( optionTab === 3 ) {
				bindingDraft = defaultInputOptions();
				bindingSelected = -1;
			} else if ( optionTab === 4 ) {
				optionDraft = defaultGameOptions();
				beginnerDraft = (view.gameplay?.progression?.maxLevel ?? view.gameplay?.progression?.level ?? 1) <= 19;
			}
		} else if ( id === "option-cancel" ) setPanel( "" );
		else if ( id === "option-apply" || id === "option-ok" ) {
			// Apply commits every tab, as OK does; only OK also closes. Returning
			// after the video tab left game options and the beginner mark unsent.
			video = videoOptions( videoDraft );
			saveVideo( video );
			saveOptions( optionDraft );
			const local = view.entities.find( e => e.gid === view?.gameplay?.localGid );
			if ( local && !!((local.visualFlags ?? 0) & 1) !== beginnerDraft ) {
				sendGameplay( { kind: "beginner-mark", enabled: beginnerDraft } );
			}
			bindings = inputOptions( bindingDraft );
			saveBindings( bindings );
			sight = sightDraft;
			saveSight( sight );
			audioSaved = { ...audioDraft };
			audioPreference( audioSaved, true );
			if ( id === "option-ok" ) setPanel( "" );
			else dirty = true;
		} else if ( id.startsWith( "option-scroll:" ) ) {
			const [group, delta] = id.slice( 14 ).split( ":" ).map( Number );
			if ( group === 0 || group === 1 ) {
				optionScroll[group] = Math.max(
					0,
					Math.min( group === 0 ? 3 : 4, optionScroll[group]! + (delta ?? 0) )
				);
			}
		} else if ( id === "potion-save" ) {
			sendGameplay( { kind: "auto-potion-save", settings: potionDraft } );
			setPanel( "" );
		} else if ( id.startsWith( "potion-enable:" ) ) {
			const key = id.slice( 14 );
			if ( key === "hp" || key === "mp" || key === "cure" ) {
				const entry = autoPotionEntry( potionDraft[key] );
				const saved = autoPotionEntry( (view.gameplay?.autoPotion ?? defaultAutoPotion())[key] );
				// 63DC30 restores the committed threshold when re-enabling a slider.
				const percent = !entry.enabled && key !== "cure" ?
					Math.max( 1, Math.min( 100, saved.percent ) ) :
					entry.percent;
				potionDraft = {
					...potionDraft,
					[key]: autoPotionWord( { ...entry, percent, enabled: !entry.enabled } )
				};
			}
		} else if ( id.startsWith( "potion-combo:" ) ) {
			potionCombo = potionCombo === id ? "" : id;
			potionComboOffset = 0;
		} else if ( id === "potion-combo-up" || id === "potion-combo-down" ) {
			potionComboOffset = Math.max( 0, Math.min( 6, potionComboOffset + (id.endsWith( "-up" ) ? -1 : 1) ) );
		} else if ( id.startsWith( "potion-choice:" ) ) {
			const [, key, part, value] = id.split( ":" );
			if ( (key === "hp" || key === "mp" || key === "cure") && (part === "page" || part === "key") ) {
				potionDraft = autoPotionDraftChoice( potionDraft, key, part, Number( value ) );
			}
			potionCombo = "";
		} else if ( id.startsWith( "potion-step:" ) ) {
			const [, key, delta] = id.split( ":" );
			if ( key === "hp" || key === "mp" ) {
				const entry = autoPotionEntry( potionDraft[key] );
				if ( !entry.enabled ) return;
				potionDraft = {
					...potionDraft,
					[key]: autoPotionWord( {
						...entry,
						percent: Math.max( 1, Math.min( 100, entry.percent + Number( delta ) ) )
					} )
				};
			} else if ( key === "time" ) {
				potionDraft = {
					...potionDraft,
					timing: (potionDraft.timing & 128) |
						Math.max( 5, Math.min( 95, (potionDraft.timing & 127) + Number( delta ) * 5 ) )
				};
			}
		} else if ( id === "potion-delay" ) potionDraft = { ...potionDraft, timing: potionDraft.timing ^ 128 };
		// 63E3BF..63E3CE: 500..9500ms, 500ms steps; preserve the enable bit.
		else if ( id === "potion-time-up" || id === "potion-time-down" ) {
			potionDraft = {
				...potionDraft,
				timing: (potionDraft.timing & 128) |
					Math.max( 5, Math.min( 95, (potionDraft.timing & 127) + (id === "potion-time-up" ? 5 : -5) ) )
			};
		} else if ( id === "gacha-open" && view.gameplay?.target ) {
			sendGameplay( { kind: "gacha-open", gid: view.gameplay.target } );
		} else if ( id.startsWith( "alchemy-mode:" ) ) {
			alchemyMode = id.slice( 13 ) as typeof alchemyMode;
			alchemySlots = [];
			alchemyQuantity = "1";
		} else if ( id.startsWith( "alchemy-slot:" ) ) {
			const item = view.gameplay?.inventory.find( r => r.slot === Number( id.slice( 13 ) ) );
			if ( item && !view.gameplay?.inventoryPending ) {
				const next = alchemySelection( alchemyMode, alchemySlots, item );
				if ( next ) {
					alchemyMode = next.mode;
					alchemySlots = next.slots;
					alchemyQuantity = "1";
				}
			}
		} else if ( id === "alchemy-cancel" ) sendGameplay( { kind: "alchemy-cancel" } );
		else if ( id === "alchemy-start" ) {
			sendGameplay( {
				kind: "alchemy-start",
				mode: alchemyMode,
				slots: [ ...alchemySlots ],
				...([ "compound", "advanced", "dissolve" ].includes( alchemyMode ) ?
					{ quantity: Number( alchemyQuantity ) } :
					{})
			} );
		} else if ( id.startsWith( "gacha-prize:" ) ) gachaEntry = Number( id.slice( 12 ) );
		else if ( id.startsWith( "gacha-card:" ) ) gachaSlot = Number( id.slice( 11 ) );
		else if ( id === "gacha-prev" ) gachaPage = Math.max( 0, gachaPage - 1 );
		else if ( id === "gacha-next" ) gachaPage++;
		else if ( id === "gacha-roll" ) sendGameplay( { kind: "gacha-roll", entry: gachaEntry, slot: gachaSlot } );
		else if ( id === "process-next" ) processPage++;
		else if ( id === "process-prev" ) processPage = Math.max( 0, processPage - 1 );
		else if ( id === "invite-accept" || id === "invite-refuse" || id === "invite-close" ) {
			sendGameplay( { kind: "social-consent", accept: id === "invite-accept" } );
		} else if ( id === "resurrection-accept" || id === "resurrection-refuse" ) {
			sendGameplay( { kind: "resurrection-consent", accept: id === "resurrection-accept" } );
		} else if ( id === "party-invite" && view.gameplay?.target ) {
			sendGameplay( {
				kind: "party-invite",
				gid: view.gameplay.target,
				options: effectivePartyOptions( view.gameplay.social, partyOptions )
			} );
		} else if ( id === "party-settings" ) {
			partySettings = true;
			partyDraft = partyOptions;
		} else if ( id === "party-settings-cancel" ) partySettings = false;
		else if ( id === "party-settings-ok" ) {
			partyOptions = partyDraft;
			partySettings = false;
		} else if ( id.startsWith( "party-setting:" ) ) {
			const [bit, value] = id.slice( 14 ).split( ":" ).map( Number );
			if ( bit === 1 || bit === 2 ) partyDraft = (partyDraft & ~bit) | (value ? bit : 0);
			else if ( bit === 4 ) partyDraft ^= 4;
		} else if ( id.startsWith( "party-option:" ) ) partyOptions ^= Number( id.slice( 13 ) );
		else if ( id.startsWith( "social-member:" ) ) {
			socialMember = Number( id.slice( 14 ) );
			confirmSocial = "";
		} else if ( id === "social-next" ) socialPage++;
		else if ( id === "social-prev" ) socialPage = Math.max( 0, socialPage - 1 );
		else if ( (id === "party-leave" || id === "party-disband") && panel === "Party" ) {
			sendGameplay( { kind: "party-leave" } );
		} else if ( id.startsWith( "party-member-kick:" ) ) {
			sendGameplay( { kind: "party-kick", id: Number( id.slice( 18 ) ) } );
		} else if ( [ "party-leave", "party-kick", "guild-leave", "guild-dissolve", "guild-kick" ].includes( id ) ) {
			if ( confirmSocial !== id ) confirmSocial = id;
			else {
				const gid = view.gameplay?.target ?? 0,
					member = view.gameplay?.social?.guild?.members.find( m => m.id === socialMember );
				if ( id === "party-leave" ) sendGameplay( { kind: id } );
				else if ( id === "party-kick" ) sendGameplay( { kind: id, id: socialMember } );
				else if ( id === "guild-kick" && member ) sendGameplay( { kind: id, name: member.name } );
				else if ( id === "guild-leave" || id === "guild-dissolve" ) sendGameplay( { kind: id, gid } );
				confirmSocial = "";
			}
		} else if ( id === "guild-create" ) {
			sendGameplay( { kind: id, gid: view.gameplay?.target ?? 0, name: socialName } );
		} else if ( id === "guild-invite" && view.gameplay?.target ) {
			sendGameplay( { kind: id, gid: view.gameplay.target } );
		} else if ( id === "guild-union-invite" ) {
			// 701190 sends the selected player (a mounted COS stands for its rider).
			if ( view.gameplay?.target ) sendGameplay( { kind: id, gid: view.gameplay.target } );
		} else if ( id === "stall-chat-send" ) {
			const text = stallHud.chat();
			if ( text.trim() && !view.gameplay?.chat?.pending && view.gameplay?.stall?.phase !== "none" ) {
				sendGameplay( { kind: "chat", channel: STALL_CHAT_CHANNEL, text } );
				stallHud.typeChat( "" );
			}
		} else if ( id === "stall-close" || id === "stall-leave" ) {
			sendGameplay( { kind: id } );
		} else if ( id === "stall-trading" ) {
			const stall = view.gameplay?.stall;
			// 0x71A8 kind 5: an open stall closes for modification at once.
			if ( stall?.phase === "owner" && stall.open ) {
				sendGameplay( { kind: "stall-open", open: false, network: false } );
			} else if ( stall?.phase === "owner" ) openStallPrompt( { kind: "register" } );
		} else if ( id === "stall-change-title" ) {
			const stall = view.gameplay?.stall;
			if ( stall?.phase === "owner" && !stall.open ) openStallPrompt( { kind: "title", text: stall.title } );
		} else if ( id === "stall-change-greeting" ) {
			const stall = view.gameplay?.stall;
			if ( stall?.phase === "owner" ) openStallPrompt( { kind: "greeting", text: stall.greeting } );
		} else if ( id.startsWith( "stall-slot:" ) ) {
			const stall = view.gameplay?.stall, slot = Number( id.slice( 11 ) );
			if ( stall?.phase === "visitor" && stall.open && stall.offers.some( row => row.slot === slot ) ) {
				openStallPrompt( { kind: "buy", slot } );
			}
		} else if ( id.startsWith( "stall-modify:" ) ) {
			const stall = view.gameplay?.stall,
				slot = Number( id.slice( 13 ) ),
				offer = stall?.offers.find( row => row.slot === slot );
			if ( stall?.phase === "owner" && !stall.open && offer ) {
				const carried = view.gameplay?.inventory.find( item => item.slot === offer.bagSlot )?.quantity ??
					offer.quantity;
				openStallPrompt( {
					kind: "price",
					slot,
					bagSlot: offer.bagSlot,
					carried,
					quantity: String( offer.quantity ),
					price: String( offer.price ),
					modify: true
				} );
			}
		} else if ( id === "stall-net-close" ) {
			sendGameplay( { kind: "stall-network-open", open: false } );
		} else if ( id.startsWith( "stall-net-combo:" ) ) {
			stallHud.toggleCombo( Number( id.slice( 16 ) ) );
		} else if ( id.startsWith( "stall-net-choice:" ) ) {
			stallHud.choose( Number( id.slice( 17 ) ) );
		} else if ( id.startsWith( "stall-net-sort:" ) ) {
			stallHud.sortBy( id.slice( 15 ) as StallNetworkSort );
		} else if ( id.startsWith( "stall-net-row:" ) ) {
			stallHud.selectRow( Number( id.slice( 14 ) ) );
		} else if ( id === "stall-net-search" || id === "stall-net-prev" || id === "stall-net-next" ) {
			const draft = stallHud.network(),
				network = view.gameplay?.stall?.network,
				child = stallCategories.roots()?.[draft.large]?.children[draft.medium];
			if ( network && stallHud.searchReady( uiNow ) ) {
				// The pages walk the last search; a new search starts at its first.
				const step = id === "stall-net-next" ? 1 : -1,
					page = id === "stall-net-search" ? 0 : network.page + step,
					category = id === "stall-net-search" ? child?.id ?? 0 : network.category;
				if ( category && page >= 0 && (id === "stall-net-search" || page < network.pages) ) {
					stallHud.searched( uiNow );
					stallHud.selectRow( -1 );
					sendGameplay( { kind: "stall-network-search", category, page, degree: draft.degree } );
				}
			}
		} else if ( id === "stall-net-buy" ) {
			const row = stallHud.network().row;
			if ( row >= 0 && view.gameplay?.stall?.network.rows[row] ) openStallPrompt( { kind: "network-buy", row } );
		} else if ( id === "exchange-confirm" || id === "exchange-cancel" || id === "exchange-close" ) {
			sendGameplay( { kind: id === "exchange-close" ? "exchange-cancel" : id } );
		} else if ( id === "exchange-gold-set" ) {
			sendGameplay( { kind: "exchange-gold", amount: Number( exchangeHud.gold() || 0 ) } );
		} else if ( id.startsWith( "exchange-my:" ) ) {
			const slot = Number( id.slice( 12 ) );
			if ( view.gameplay?.exchange?.own.some( row => row.slot === slot ) ) {
				sendGameplay( { kind: "exchange-take", slot } );
			}
		} else if ( id.startsWith( "war-" ) ) {
			const state = guildWarHud.state(), social = view?.gameplay?.social;
			if (
				id === "war-scroll-up" || id === "war-scroll-down" || id === "war-members-up" ||
				id === "war-members-down" || id === "war-combo-up" || id === "war-combo-down"
			) {
				const target = id.startsWith( "war-combo" ) ?
					"combo" :
					id.startsWith( "war-members" ) ?
					"members" :
					"enemies";
				const count = target === "combo" ?
					[ 8, 32, 25, 7 ][state.combo - 23] ?? 0 :
					target === "members" ?
					social?.guild?.members.length ?? 0 :
					social?.wars?.length ?? 0;
				guildWarHud.scroll(
					target,
					id.endsWith( "up" ) ? -1 : 1,
					count,
					target === "combo" ? state.combo === 23 ? 8 : 7 : target === "members" ? 3 : guildTab === 2 ? 6 : 9
				);
			} else {
				if ( id === "war-confirm" && state.mode === "input" && social ) {
					try {
						guildWarRequest( social, { kind: "guild-war-declare", terms: state.terms } );
					} catch {
						return;
					}
					if ( state.terms.period === 0 ) return;
				}
				const command = guildWarHud.command( id === "war-close" ? "war-cancel" : id, social );
				if ( command ) sendGameplay( command );
				if ( id === "war-declare" ) focusAndSelect( "war-name", 0, state.draft.name.length );
				if ( id === "war-money-open" ) focusAndSelect( "war-money", 0, String( state.draft.stake ).length );
			}
		} else if ( id === "union-sort:name" || id === "union-sort:level" ) {
			unionHud.sortBy( id === "union-sort:name" ? "name" : "level" );
		} else if ( id === "guild-union-exit" ) unionHud.ask( { kind: "exit", guild: 0, name: "" } );
		else if ( id === "guild-union-expel" ) {
			// 5F7670 asks only with a guild selected.
			const ally = view.gameplay?.social?.alliances?.find( row => row.id === socialMember );
			if ( ally ) unionHud.ask( { kind: "expel", guild: ally.id, name: ally.name } );
		} else if ( id === "union-ask-yes" || id === "union-ask-no" ) {
			const asked = unionHud.answer();
			if ( asked && id === "union-ask-yes" ) {
				sendGameplay(
					asked.kind === "exit" ?
						{ kind: "guild-union-leave" } :
						{ kind: "guild-union-kick", id: asked.guild }
				);
			}
		} else if ( id === "academy-notice-submit" ) {
			sendGameplay( { kind: "academy-notice", subject: socialSubject, contents: socialContents } );
			if ( socialSubject.split( "\0", 1 )[0] && socialContents.split( "\0", 1 )[0] ) guildDialog = "";
		} else if ( id === "guild-notice" ) {
			sendGameplay( { kind: id, subject: socialSubject, contents: socialContents } );
		} else if ( id === "guild-donate" ) sendGameplay( { kind: id, amount: Number( socialAmount ) } );
		else if ( id === "guild-title" ) sendGameplay( { kind: id, id: socialMember, name: socialName } );
		else if ( id === "guild-role" ) sendGameplay( { kind: id, id: socialMember, role: Number( socialAmount ) } );
		// In retail, CIFWorldMap 57F43D swallows map button messages while AUTO MOVE is on.
		// By design, client-next allows clicking into a city even in Automatic Movement State,
		// and automatically switches to Manual Movement State (mapFollow = false).
		else if ( id.startsWith( "map-town:" ) ) {
			const page = Number( id.slice( 9 ) );
			if ( worldMapPages().some( p => p.id === page ) ) {
				mapPage = page;
				mapFollow = false;
				mapPan = [ 0, 0 ];
				mapCenter = view.gameplay?.pose ? { ...view.gameplay.pose } : null;
			}
		} else if ( id.startsWith( "party-match-row:" ) ) partyMatchSelection = Number( id.slice( 16 ) );
		else if ( id === "party-match:56" || id === "party-match-prev" || id === "party-match-next" ) {
			const state = view.gameplay?.partyMatching;
			if ( state && !state.pending ) {
				partyMatchOffset = 0;
				sendGameplay( {
					kind: "party-match-page",
					page: state.page + (id === "party-match-prev" ? -1 : id === "party-match-next" ? 1 : 0)
				} );
			}
		} else if ( id === "party-match:15" && partyMatchSelection ) {
			sendGameplay( { kind: "party-match-join", id: partyMatchSelection } );
		} else if ( id === "party-match:16" ) {
			const row = view.gameplay?.partyMatching?.rows.find( r => r.id === partyMatchSelection );
			if ( row ) {
				beginWhisper( row.name );
			}
		} else if ( id === "party-match:55" ) {
			const min = Math.max( 1, Number( partySearchDraft.min ) || 1 ), max = Number( partySearchDraft.max ) || 90;
			if ( min <= max ) {
				partySearch = { ...partySearchDraft, min, max };
				partyMatchOffset = 0;
				partyMatchSelection = 0;
			}
		} else if ( id === "party-match:purpose" ) partyPurposeOpen = !partyPurposeOpen;
		else if ( id.startsWith( "party-purpose:" ) ) {
			partySearchDraft.purpose = Number( id.slice( 14 ) );
			partyPurposeOpen = false;
		} else if ( /^party-match:6[0-3567]$/.test( id ) ) {
			const key =
				({ 60: "id", 61: "type", 62: "race", 63: "name", 65: "purpose", 66: "members", 67: "min" } as const)[
					Number( id.slice( 12 ) ) as 60
				];
			if ( key ) {
				partyMatchDescending = partyMatchSort === key ? !partyMatchDescending : false;
				partyMatchSort = key;
				partyMatchSelection = 0;
			}
		} else if ( id === "party-match:18" || id === "party-match:19" ) {
			const own = view.gameplay?.partyMatching?.own;
			partyDialog = id === "party-match:19" ? "modify" : "register";
			const job = partyActiveJob( view.gameplay?.inventory ?? [] );
			partyForm = own ?
				{ purpose: own.purpose, min: String( own.min ), max: String( own.max ), title: own.title } :
				{ purpose: partyDefaultPurpose( job ), min: "1", max: "90", title: "" };
		} else if ( id === "party-match:20" ) partyDialog = "delete";
		else if ( id === "party-match:17" ) partyDialog = "auto";
		else if ( id === "party-auto-stop" ) sendGameplay( { kind: "party-match-auto-stop" } );
		else if ( id.startsWith( "party-form-auto:" ) ) {
			const [key, index] = id.slice( 16 ).split( ":" );
			if ( key === "purpose" || key === "race" || key === "exp" || key === "item" ) {
				partyAuto[key] = Number( index );
			}
		} else if ( id === "party-form-cancel" ) {
			partyDialog = null;
			focus = null;
		} else if ( id.startsWith( "party-form-purpose:" ) ) {
			const purpose = Number( id.slice( 19 ) );
			if ( partyPurposeAllowed( partyActiveJob( view.gameplay?.inventory ?? [] ), purpose ) ) {
				partyForm.purpose = purpose;
			}
		} else if ( id === "party-form-confirm" ) {
			if ( partyDialog === "delete" ) {
				sendGameplay( { kind: "party-match-delete" } );
				partyDialog = null;
			} else if ( partyDialog === "auto" ) {
				const game = view.gameplay,
					rows = partyMatchRows(
						game?.partyMatching?.rows ?? [],
						partySearch,
						partyMatchSort,
						partyMatchDescending
					),
					type = (partyAuto.exp === 0 ? 1 : 0) | (partyAuto.item === 0 ? 2 : 0) |
						(effectivePartyOptions( game?.social, partyOptions ) & 4),
					ids = partyAutoCandidates(
						rows,
						partyAuto.purpose,
						partyAuto.race,
						type,
						game?.progression?.level ?? 1
					);
				sendGameplay( { kind: "party-match-auto", ids } );
				partyDialog = null;
			} else if ( partyDialog ) {
				if (
					!partyPurposeAllowed( partyActiveJob( view.gameplay?.inventory ?? [] ), partyForm.purpose )
				) return;
				const min = Number( partyForm.min ), max = Number( partyForm.max );
				if ( min >= 1 && max <= 90 && min <= max && partyForm.title.length ) {
					sendGameplay( {
						kind: partyDialog === "modify" ? "party-match-modify" : "party-match-register",
						registration: {
							party: 0,
							type: effectivePartyOptions( view.gameplay?.social, partyOptions ),
							purpose: partyForm.purpose,
							min,
							max,
							title: partyForm.title
						}
					} );
					partyDialog = null;
					focus = null;
				} else {hudMessages.append(
						hud.data()?.strings[
							!partyForm.title.length ?
								"UIIT_MSG_PARTYMATCH_RECORD_ERROR_TITLE" :
								"UIIT_MSG_PARTYMATCH_RECORD_ERROR_LEVEL"
						] ?? ""
					);}
			}
		} else if ( id.startsWith( "party-answer:" ) ) {
			const request = view.gameplay?.partyMatching?.request;
			if ( request ) {
				sendGameplay( {
					kind: "party-match-answer",
					a: request.a,
					b: request.b,
					answer: Number( id.slice( 13 ) ) as 0 | 1 | 2
				} );
			}
		} else if ( id.startsWith( "action:" ) ) selectedAction = Number( id.slice( 7 ) );
		else if ( id === "skills-mode" ) {
			trainingMode = !trainingMode;
			skillPage = 0;
			selectedSkill = 0;
		} else if ( id === "hotbar-clear" ) clearHotbar = !clearHotbar;
		else if ( id.startsWith( "skill-tab:" ) ) {
			skillTab = Number( id.slice( 10 ) );
			selectedMastery = 0;
			skillScroll = 0;
		} else if ( id.startsWith( "skill-mastery:" ) ) {
			selectedMastery = Number( id.slice( 14 ) );
			skillScroll = 0;
		} else if ( id === "skill-scroll-up" || id === "skill-scroll-down" ) {
			skillScroll = Math.max( 0, skillScroll + (id.endsWith( "up" ) ? -1 : 1) );
		} else if ( id.startsWith( "quest-track:" ) ) {
			const idn = Number( id.slice( 12 ) );
			if ( view.gameplay?.quests?.some( q => q.refId === idn ) ) trackedQuest = trackedQuest === idn ? 0 : idn;
		} else if ( id.startsWith( "quest-expand:" ) ) {
			const idn = Number( id.slice( 13 ) );
			if ( expandedQuests.has( idn ) ) expandedQuests.delete( idn );
			else expandedQuests.add( idn );
			selectedQuest = idn;
		} else if ( id === "quest-scroll-up" || id === "quest-scroll-down" ) {
			questPage = Math.max( 0, questPage + (id.endsWith( "up" ) ? -1 : 1) );
		} else if ( id.startsWith( "skill-learn:" ) ) practice = { mode: PRACTICE_SKILL, id: Number( id.slice( 12 ) ) };
		else if ( id === "skill-confirm-cancel" ) practice = null;
		else if ( id === "skill-confirm-ok" && practice ) {
			// 5DE690 re-checks the request before it sends either level-up.
			const game = view.gameplay, costs = hud.data()?.masteryCosts;
			if ( practice.mode === PRACTICE_MASTERY ) {
				if (
					game?.progression && costs && !game.trainingPending &&
					!masteryTrainingReason( practice.id, game.progression, costs )
				) sendGameplay( { kind: "mastery-train", id: practice.id } );
			} else {
				const row = game ? skillMetadataById( game, practice.id ) : undefined;
				if (
					row && game?.progression && !game.trainingPending &&
					!skillTrainingReason( row, game.skills ?? [], game.skillCatalog ?? [], game.progression )
				) sendGameplay( { kind: "skill-train", id: practice.id } );
			}
			practice = null;
		} else if ( id === "quest-details-close" ) questDetails = false;
		else if ( id === "quest-detail-up" || id === "quest-detail-down" ) {
			questDetailScroll = Math.max(
				0,
				Math.min( questDetailMax, questDetailScroll + (id.endsWith( "up" ) ? -1 : 1) * (text.height() + 5) )
			);
		} else if ( id.startsWith( "mastery:" ) ) {
			// The board's level-up opens the practice box in its mastery face;
			// nothing is sent until the box is confirmed.
			practice = { mode: PRACTICE_MASTERY, id: Number( id.slice( 8 ) ) };
		} else if ( id.startsWith( "skill:" ) ) selectedSkill = Number( id.slice( 6 ) );
		else if ( id === "skills-next" ) skillPage++;
		else if ( id === "skills-prev" ) skillPage = Math.max( 0, skillPage - 1 );
		else if ( id === "guide-indicator" ) {
			guideTab = "events";
			guideEvent = guidePending;
			guidePending = 0;
			guideScroll = 0;
			setPanel( "Game Guide" );
		} else if ( id.startsWith( "guide-article:" ) ) {
			guideEvent = Number( id.slice( 14 ) );
			guideScroll = 0;
		} else if ( id === "academy-open" ) setPanel( "Academy", "toggle" );
		else if ( id.startsWith( "academy-row:" ) ) academySelection = Number( id.slice( 12 ) );
		else if ( id.startsWith( "academy-sort:" ) ) {
			const key = id.slice( 13 );
			academyAscending = key === academySort ? !academyAscending : true;
			academySort = key;
		} else if ( id === "academy-search" ) {
			academyFilter = { name: academyName, grade: academyGrade, kind: academyKind };
			academySelection = 0;
		} else if ( id === "academy-whisper" ) {
			const row = view.gameplay?.academy?.rows.find( r => r.id === academySelection );
			if ( row ) {
				beginWhisper( row.name );
			}
		} else if ( id.startsWith( "academy-combo:" ) ) {
			const n = Number( id.slice( 14 ) );
			academyCombo = academyCombo === n ? 0 : n;
		} else if ( id.startsWith( "academy-option:" ) ) {
			const [which, index] = id.slice( 15 ).split( ":" ).map( Number );
			if ( which === 1 && index !== undefined && index >= 0 && index <= 5 ) academyGrade = index;
			else if ( which === 2 && index !== undefined && index >= 0 && index <= 2 ) academyKind = index;
			academyCombo = 0;
		} else if ( id === "academy-join" ) sendGameplay( { kind: "academy-join", id: academySelection } );
		else if ( id === "academy-refresh" || id === "academy-prev" || id === "academy-next" ) {
			const a = view.gameplay?.academy;
			if ( a ) {
				sendGameplay( {
					kind: "academy-page",
					page: id === "academy-prev" ?
						Math.max( 0, a.page - 1 ) :
						id === "academy-next" ?
						a.page + 1 :
						a.page
				} );
			}
		} else if ( id === "guide-sidebar" ) guideSidebar = !guideSidebar;
		else if ( id.startsWith( "guide-tab:" ) ) {
			const tab = id.slice( 10 );
			if ( tab === "general" || tab === "events" || tab === "quests" ) {
				if ( guideTab !== tab ) {
					guideTab = tab;
					guideEvent = 0;
					guideGroups.clear();
					guideIndexScroll = 0;
					guideScroll = 0;
				}
			}
		} else if ( id.startsWith( "guide-group:" ) ) {
			const group = Number( id.slice( 12 ) );
			guideEvent = group;
			guideScroll = 0;
			if ( guideGroups.has( group ) ) guideGroups.delete( group );
			else guideGroups.add( group );
		} else if ( id === "guide-index-next" || id === "guide-index-down" ) guideIndexScroll++;
		else if ( id === "guide-index-prev" || id === "guide-index-up" ) {
			guideIndexScroll = Math.max( 0, guideIndexScroll - 1 );
		} else if ( id === "guide-down" ) guideScroll += 100;
		else if ( id === "guide-up" ) guideScroll = Math.max( 0, guideScroll - 100 );
		else if ( id === "hud-menu" ) setPanel( isMainPopupPage( panel ) ? "" : rememberedMainPopup );
		else if ( id.startsWith( "compact-" ) ) {
			compactHud.toggle( id.slice( 8 ) );
			focus = null;
		} else if ( id === "chat-hide" ) chatHidden = !chatHidden;
		else if ( id === "chat-whispers" ) whispersOpen = !whispersOpen;
		else if ( id.startsWith( "whisper-name:" ) ) {
			beginWhisper( id.slice( 13 ) );
		} else if ( id.startsWith( "chat-line:" ) ) {
			const recipient = controls.find( control => control.id === id && !control.disabled )?.whisperTarget;
			if ( recipient ) beginWhisper( recipient );
		} else if ( id === "status-filter" ) statusFilterOpen = !statusFilterOpen;
		else if ( id.startsWith( "status-filter:" ) ) {
			const key = id.slice( 14 );
			if ( statusFilters.has( key ) ) statusFilters.delete( key );
			else statusFilters.add( key );
		} else if ( id === "status-size" ) statusRows = statusRows % 6 + 1;
		else if ( id === "chat-size" ) chatRows = (chatRows + 1) % 7;
		else if ( id.startsWith( "chat-tab:" ) ) {
			chatTab = Number( id.slice( 9 ) );
			chatText = selectChatTab( chatText, chatTab );
			chatScroll.reset();
			focusAtEnd( "chat-text", chatText );
		} // 57AEA0 resizes, then 57A570 recentres on the player in either mode.
		else if ( id === "map-size" ) {
			mapSmall = !mapSmall;
			mapPan = [ 0, 0 ];
			mapCenter = mapFollow ? null : view.gameplay?.pose ? { ...view.gameplay.pose } : null;
		} // 575E90 flips AUTO MOVE only; the view stays where AUTO last centred it.
		else if ( id === "map-follow" ) {
			mapFollow = !mapFollow;
			mapCenter = view.gameplay?.pose ? { ...view.gameplay.pose } : null;
			mapPan = [ 0, 0 ];
		} // 57F410 is 57F0B0(world) + 57A570. By design, clicking World map switches
		// to Manual Movement State (mapFollow = false) so the continent view is preserved.
		else if ( id === "map-world" ) {
			mapPage = 0;
			mapFollow = false;
			mapCenter = view.gameplay?.pose ? { ...view.gameplay.pose } : null;
			mapPan = [ 0, 0 ];
		} else if ( id === "berserk" ) {
			if ( view?.berserkGauge?.displayed === 5 ) {
				sendGameplay( { kind: "berserk" } );
			}
		} else if ( id === "ability-details" ) abilityDetails = !abilityDetails;
		else if ( id === "minimap-in" ) minimapTarget = minimapZoomTarget( minimapTarget, 1 );
		else if ( id === "minimap-out" ) minimapTarget = minimapZoomTarget( minimapTarget, -1 );
		else if ( id === "hotbar-prev" ) hotbarPage = (hotbarPage + 3) % 4;
		else if ( id === "hotbar-next" ) hotbarPage = (hotbarPage + 1) % 4;
		else if ( id === "skill-cast" && view.gameplay?.skills?.includes( selectedSkill ) ) {
			sendGameplay( {
				kind: "skill",
				skillId: selectedSkill,
				...(view.gameplay.target ? { gid: view.gameplay.target } : {})
			} );
		} else if ( id === "ext-open" ) {
			extOpen = !extOpen;
			persistQuickslots();
		} else if ( id === "ext-horizontal" ) {
			extVertical = false;
			persistQuickslots();
		} else if ( id === "ext-vertical" ) {
			extVertical = true;
			persistQuickslots();
		} else if ( id === "ext-options" ) {
			extOptions = !extOptions;
			extDraft = [ extTransparent, extSlotLock, extPositionLock, extDouble ];
		} else if ( id === "ext-double" ) extDraft[3] = !extDraft[3];
		else if ( id === "ext-transparent" ) extDraft[0] = !extDraft[0];
		else if ( id === "ext-slot-lock" ) extDraft[1] = !extDraft[1];
		else if ( id === "ext-position-lock" ) extDraft[2] = !extDraft[2];
		else if ( id === "ext-options-close" ) extOptions = false;
		else if ( id === "ext-options-ok" || id === "ext-options-apply" ) {
			[extTransparent, extSlotLock, extPositionLock, extDouble] = extDraft as [
				boolean,
				boolean,
				boolean,
				boolean
			];
			persistQuickslots();
			if ( id === "ext-options-ok" ) extOptions = false;
		} else if ( id.startsWith( "hotbar:" ) ) {
			const slot = Number( id.slice( 7 ) ), game = view.gameplay;
			if ( !Number.isInteger( slot ) || slot < 0 || slot >= 51 ) return;
			if ( clearHotbar ) {
				if ( slot >= 41 && extSlotLock ) return;
				sendGameplay( { kind: "quickslot-bind", slot, skillId: 0 } );
			} else {
				const binding = game?.quickSlots?.find( row => row.slot === slot );
				if ( binding?.kind === 0x4e ) {
					hotbarPage = binding.payload;
					dirty = true;
					return;
				}
				if ( binding?.kind === 0x49 ) {
					executeSkill( binding.payload );
					return;
				}
				if ( binding?.kind === 0x4a ) {
					executeAction( binding.payload & 0xffffff );
					return;
				}
				// 0x25 dispatches the pet command bar's own command (6F2520 ->
				// CICCos_ExecuteActionCommand 6A2350).
				if ( binding?.kind === 0x25 ) {
					const mountedOn = view.entities.find( e => e.gid === game?.localGid )?.mountedOn;
					// Attack while riding the active COS strikes from the saddle
					// (0x769E command 2 for the ridden COS), as a world click does.
					if (
						binding.payload === COS_COMMAND_ATTACK && game?.activeCos && mountedOn === game.activeCos.gid &&
						!game.activeCos.dead && game.target
					) {
						sendGameplay( { kind: "cos-attack", gid: game.target } );
					} else executeCosCommand( binding.payload );
					return;
				}
				// 572770 uses a bag item only while nothing is held on the cursor.
				if ( binding?.kind === 0x46 && carriedItem ) return;
				const command = binding && game ?
					quickSlotCommand( binding, game, view.entities.find( e => e.gid === game.localGid )?.mountedOn ) :
					null;
				if ( command ) sendGameplay( command );
			}
		} else if ( id === "quest-next" ) {
			questPage = Math.min(
				Math.max( 0, Math.ceil( (view.gameplay?.quests?.length ?? 0) / 4 ) - 1 ),
				questPage + 1
			);
		} else if ( id === "quest-prev" ) questPage = Math.max( 0, questPage - 1 );
		else if ( id === "chat-older" ) chatPage++;
		else if ( id === "chat-newer" ) chatPage = Math.max( 0, chatPage - 1 );
		else if ( id.startsWith( "quest:" ) ) {
			const refId = Number( id.slice( 6 ) ), quest = view.gameplay?.quests?.find( q => q.refId === refId );
			if ( quest && [ 1, 2, 7, 8 ].includes( quest.u10 ?? 0 ) ) {
				selectedQuest = refId;
				questDetailScroll = 0;
				questDetails = true;
				confirmAbandon = false;
			}
		} else if ( id === "quest-abandon" ) confirmAbandon = true;
		else if ( id === "quest-abandon-yes" ) {
			sendGameplay( { kind: "quest-abandon", refId: selectedQuest } );
			confirmAbandon = false;
			questDetails = false;
		} else if ( id === "quest-abandon-no" ) confirmAbandon = false;
		else if ( id === "quest-reward" ) {
			sendGameplay( { kind: "quest-reward", refId: selectedQuest } );
			questDetails = false;
		} else if (
			id === "chat-send" && BUG_COMMAND.test( chatText.slice( chatTabPrefix( chatTab ).length ) )
		) {
			// /bug opens the bug reporter (issue #90); it is never sent as chat.
			const text = chatText.slice( chatTabPrefix( chatTab ).length ).trim().replace( BUG_COMMAND, "" );
			const opened = bugReport ? bugReport.open( text ) : "off";
			if ( opened === "off" ) hudMessages.append( BUG_REPORTS_DISABLED );
			else if ( opened === "unavailable" ) hudMessages.append( BUG_REPORTS_UNAVAILABLE );
			chatText = chatTabPrefix( chatTab );
			selection = [ chatText.length, chatText.length ];
			focus = null;
			focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
			dirty = true;
			return;
		} else if (
			id === "chat-send" && premiumCommand( chatText.slice( chatTabPrefix( chatTab ).length ), hudCopy )
		) {
			// 6AD990: /Return, /Reverse Return and /Resurrection spend a premium
			// package's limited use; they are never sent as chat.
			const command = premiumCommand( chatText.slice( chatTabPrefix( chatTab ).length ), hudCopy )!;
			sendGameplay( { kind: "premium-command", command } );
			chatText = chatTabPrefix( chatTab );
			selection = [ chatText.length, chatText.length ];
			focus = null;
			focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
			dirty = true;
			return;
		} else if ( id.startsWith( "premium-reverse:" ) ) {
			sendGameplay( { kind: "premium-command", command: "reverse-return", choice: Number( id.slice( 16 ) ) } );
		} else if ( id === "premium-reverse-cancel" ) sendGameplay( { kind: "premium-command-cancel" } );
		else if ( id.startsWith( "reverse-scroll:" ) ) {
			const command = reverseScrollHud.choose( Number( id.slice( "reverse-scroll:".length ) ), view );
			if ( command ) sendGameplay( command );
			dirty = true;
		} else if ( id === "reverse-scroll-cancel" ) {
			reverseScrollHud.close();
			dirty = true;
		} else if ( id.startsWith( "count-job:" ) ) {
			// 6E2840: a package slot (kind 5) opens its package window.
			compositeItemHud.open( Number( id.slice( "count-job:".length ) ) );
			dirty = true;
		} else if ( id.startsWith( "composite-item:" ) ) {
			// 6AFB40: the button runs its row, then the window closes.
			const packageRefObjId = compositeItemHud.packageId();
			compositeItemHud.close();
			dirty = true;
			if ( packageRefObjId !== null ) {
				sendGameplay( {
					kind: "count-job-use",
					packageRefObjId,
					itemRefObjId: Number( id.slice( "composite-item:".length ) )
				} );
			}
		} else if ( id === "composite-item-cancel" || id === "composite-item-close" ) {
			compositeItemHud.close();
			dirty = true;
		} else if ( id === "chat-send" && !view.gameplay?.chat?.pending ) {
			const draft = composeChat(
				panel === "Chat" ?
					(chatChannel === 2 ?
						"$" + chatTarget + " " :
						({ 4: "#", 5: "@", 11: "%" } as Record<number, string>)[chatChannel] ?? "") + chatText :
					chatText
			);
			if ( draft ) {
				const key = draft.channel === 4 && !view.gameplay?.social?.members.length ?
					"UIIT_MSG_PARTYERR_CANT_FIND_PARTY" :
					draft.channel === 5 && !view.gameplay?.social?.guild ?
					"UIIT_MSG_GUILD_NON_EXISTING" :
					null;
				if ( key ) hudMessages.append( hud.data()?.strings[key] ?? "" );
				else sendGameplay( { kind: "chat", ...draft } );
				chatText = key ? "" : chatTabPrefix( chatTab ) || (draft.channel === 2 ? "$" + draft.target + " " : "");
			} else {
				focus = null;
				focusRequest = { id: null, revision: ++focusRevision, caret: 0 };
				dirty = true;
				return;
			}
			selection = [ chatText.length, chatText.length ];
			focusRequest = { id: "chat-text", revision: ++focusRevision, caret: chatText.length };
		} else if ( id.startsWith( "chat-channel:" ) ) {
			chatChannel = Number( id.slice( 13 ) );
			chatPage = 0;
		} else if ( id === "native:servers" ) {
			if ( !serverList ) sound( "open" );
			serverList = true;
			serverDraft = selectedServer;
			serverOffset = Math.max( 0, servers.findIndex( s => s.id === serverDraft ) - 12 );
			// A second request would abort the in-flight one for the same reply.
			if ( !pending && view?.session?.phase !== "listing-servers" ) requestServers();
		} else if ( id === "native:server-accept" ) acceptServer();
		else if ( id === "native:server-cancel" ) closeServer();
		else if ( id === "native:server-prev" ) serverOffset = Math.max( 0, serverOffset - 1 );
		else if ( id === "native:server-next" ) {
			serverOffset = Math.min( Math.max( 0, servers.length - 13 ), serverOffset + 1 );
		} else if ( id === "login" && !pending ) {
			if ( !account || !password || !selectedServer ) return;
			pending = true;
			loginReplyPending = true;
			titleNotice = { until: uiNow + 15000 };
			sound( "message" );
			commands( { kind: "login", apiBase: endpoint, id: account, password, serverId: selectedServer } );
		} else if ( id === "servers" && !pending ) requestServers();
		else if ( id.startsWith( "server:" ) ) serverDraft = id.slice( 7 );
		else if ( id === "dock:back" ) selectedCharacter = "";
		else if ( id === "enter" && !pending && selectedCharacter ) {
			pending = true;
			commands( { kind: "enter-world", character: selectedCharacter } );
		} else if ( id === "logout" || id === "disconnect-confirm" ) {
			commands( { kind: "logout" } );
			password = "";
			roster = [];
			rosterRequested = false;
			resetPanel();
		} else if ( id === "reconnect" && !pending ) {
			pending = true;
			commands( { kind: "reconnect" } );
		} else if ( id === "disconnect" ) commands( { kind: "disconnect" } );
		else if ( id === "system-restart" ) {
			setPanel( "" );
			commands( { kind: "restart" } );
		} else if ( id === "system-exit" ) {
			setPanel( "" );
			commands( { kind: "exit" } );
		} else if ( id === "close" || id === "companion-close" || id === "potion-cancel" ) {
			confirmDrop = "";
			confirmAbandon = false;
			// Closing the shop or the warehouse, or the inventory beside either,
			// ends the NPC interaction: CIFWnd_OnInventoryOrStorageClose (5B0D20) and the
			// store's own close (53D4BC) call CGInterface_SetNpcShopVisible( 0 ),
			// which hides the talk menus and releases the NPC target (69FB95).
			if ( panel === "Shop" || panel === "Storage" ) sendGameplay( { kind: "npc-close" } );
			setPanel( "" );
			inventorySlot = -1;
		} else if ( id === "select-window:Character-stats" ) setPanel( "Character", "select" );
		// Window ids carry the panel name as text; only registered panels open.
		else if ( id.startsWith( "select-window:" ) ) setPanel( panelNamed( id.slice( 14 ) ), "select" );
		else if ( id.startsWith( "open-window:" ) ) setPanel( panelNamed( id.slice( 12 ) ) );
		else if ( id.startsWith( "toggle-window:" ) ) setPanel( panelNamed( id.slice( 14 ) ), "toggle" );
		else if ( id === "cos-bag" ) setPanel( "COS inventory" );
		else if ( id === "cos-cycle" ) {
			const records = view.gameplay?.cosRecords?.filter( r => r.inventory && !r.dead && r.hp > 0 ) ?? [];
			cosGid = records[(records.findIndex( r => r.gid === cosGid ) + 1) % records.length]?.gid ?? 0;
			cosSlot = -1;
			cosPage = 0;
		} else if ( id.startsWith( "cos-slot:" ) ) cosSlot = Number( id.slice( 9 ) );
		else if ( id.startsWith( "cos-player:" ) ) inventorySlot = Number( id.slice( 11 ) );
		else if ( (id === "cos-drop" || id === "cos-pickup") && view.gameplay ) {
			const game = view.gameplay, record = game.cosRecords?.find( r => r.gid === cosGid );
			if ( record && !record.dead && record.hp > 0 && !game.inventoryPending ) {
				if ( id === "cos-drop" && record.inventory?.some( row => row.slot === cosSlot ) ) {
					sendGameplay( { kind: "cos-drop", gid: record.gid, slot: cosSlot } );
					cosSlot = -1;
				}
				if (
					id === "cos-pickup" && game.target &&
					view.entities.some( e => e.gid === game.target && e.kind === "ground-item" )
				) sendGameplay( { kind: "cos-pickup", gid: record.gid, target: game.target } );
			}
		} else if ( id === "cos-next" ) {
			cosPage++;
			cosSlot = -1;
		} else if ( id === "cos-prev" ) {
			cosPage = Math.max( 0, cosPage - 1 );
			cosSlot = -1;
		} else if ( id === "cos-player-next" ) {
			cosPlayerPage++;
			inventorySlot = -1;
		} else if ( id === "cos-player-prev" ) {
			cosPlayerPage = Math.max( 0, cosPlayerPage - 1 );
			inventorySlot = -1;
		} else if ( (id === "to-cos" || id === "from-cos") && view.gameplay ) {
			const game = view.gameplay,
				record = game.cosRecords?.find( r => r.gid === cosGid ),
				toCos = id === "to-cos";
			if (
				record?.inventory && !record.dead && record.hp > 0 && !game.inventoryPending &&
				game.inventorySlotCount !== undefined && game.equipmentSlotCount !== undefined
			) {
				const start = toCos ? 0 : game.equipmentSlotCount,
					end = toCos ? record.status : game.inventorySlotCount;
				const source = toCos ? inventorySlot : cosSlot,
					selected = toCos ? cosSlot : inventorySlot;
				// A chosen slot is sent as is; a quick transfer leaves the slot to
				// the worker, which knows the stack limits (cosQuickDestination).
				const destination = selected >= start && selected < end ? selected : undefined;
				if ( (toCos ? game.inventory : record.inventory).some( row => row.slot === source ) ) {
					sendGameplay( {
						kind: "cos-transfer",
						gid: record.gid,
						toCos,
						source,
						...(destination === undefined ? {} : { destination })
					} );
					inventorySlot = -1;
					cosSlot = -1;
				}
			}
		} else if ( id === "npc-fortress-staff" || id.startsWith( "npc-fortress-hire:" ) ) {
			const conversation = view?.gameplay?.npcConversation, staff = fortressStaffView();
			if ( conversation?.phase === "menu" && staff.holder && staff.fortress !== undefined ) {
				if ( id === "npc-fortress-staff" ) {
					fortressStaffHud.open( conversation.gid, staff.fortress );
					sendGameplay( { kind: "fortress-staff", gid: conversation.gid, fortress: staff.fortress } );
				} else {fortressStaffHud.ask(
						Number( id.slice( "npc-fortress-hire:".length ) ),
						staff.flags,
						staff.commander
					);}
			}
		} else if ( id === "npc-fortress-schedule" ) {
			const game = view?.gameplay, conversation = game?.npcConversation, state = game?.fortress;
			const world = state?.worlds.find( row => row.id === (state.worldId & 0xffff) );
			const fortress = state?.fortresses.find( row => row.code === world?.code );
			if ( conversation?.phase === "menu" && fortress ) {
				fortressScheduleHud.request( conversation.gid, state?.serviceSequence ?? 0 );
				sendGameplay( { kind: "fortress-schedule", gid: conversation.gid, fortress: fortress.id } );
			}
		} else if ( id.startsWith( "npc-fortress-production:" ) ) {
			// 5D8C86: the smith's row checks that the player's guild holds the
			// fortress (else notice 0x10/0x1E); the trainer's row does not. Each
			// sends its query, whose answer opens the window.
			const conversation = view?.gameplay?.npcConversation, staff = fortressStaffView();
			const who: FortressStaff = id.endsWith( ":trainer" ) ? "trainer" : "smith";
			if ( conversation?.phase === "menu" && staff.fortress !== undefined ) {
				if ( who === "smith" && !staff.holder ) {
					hudMessages.append( hudCopy( "UIIT_MSG_GUILDERR_PERMISSION_DENIED" ) );
				} else {
					const queryId = fortressProductionHud.request(
						conversation.gid,
						who,
						staff.fortress
					);
					sendGameplay( {
						kind: "fortress-production",
						queryId,
						gid: conversation.gid,
						fortress: staff.fortress,
						action: fortressProductionAction( who, FORTRESS_PRODUCTION_QUERY )
					} );
				}
			}
		} else if ( id === "fortress-production-close" ) {
			setPanel( "" );
		} else if ( id.startsWith( "fortress-production-make:" ) ) {
			const item = fortressProductionItems().find( row =>
				row.refObjId === Number( id.slice( "fortress-production-make:".length ) )
			);
			if ( item ) {
				fortressProductionHud.askMake( item );
				// 52C870 mode 0xA moves the keyboard focus into the count edit.
				focusAtEnd( FORTRESS_PRODUCTION_COUNT, "" );
			}
		} else if ( id === "fortress-production-cancel" ) {
			fortressProductionHud.askCancel();
		} else if ( id === "fortress-production-complete" ) {
			// CIFFortressMakeItemWnd_SendCollect (656430): the request takes at
			// most one stack (fortressServiceRequest clamps it).
			const order = fortressProductionHud.order();
			const item = fortressProductionItems().find( row => row.refObjId === order?.refObjId );
			if ( order ) {
				sendFortressProduction( FORTRESS_PRODUCTION_COLLECT, {
					reference: order.refObjId,
					count: order.count,
					stackLimit: item?.maxStack ?? 1
				} );
			}
		} else if ( id === "npc-fortress-tax" ) {
			// 5D8930 action 0x33 row 1: 0x71E1 action 0; the window shows at once.
			const conversation = view?.gameplay?.npcConversation, staff = fortressStaffView();
			if ( conversation?.phase === "menu" && staff.fortress !== undefined ) {
				fortressTaxHud.request(
					conversation.gid,
					staff.fortress,
					view?.gameplay?.fortress?.serviceSequence ?? 0
				);
				sendGameplay( { kind: "fortress-tax", gid: conversation.gid, fortress: staff.fortress } );
			}
		} else if ( id === "fortress-tax-close" ) {
			setPanel( "" );
		} else if ( id === "fortress-tax-prev" || id === "fortress-tax-next" ) {
			fortressTaxHud.slide( fortressTaxHud.draft() + (id === "fortress-tax-prev" ? -1 : 1) );
		} else if ( id === "fortress-tax-modify" || id === "fortress-tax-collect" ) {
			// 665470 enables both buttons for the fortress commander only (827DB0).
			if ( fortressStaffView().commander ) {
				if ( id === "fortress-tax-modify" ) fortressTaxHud.askRate();
				else {
					fortressTaxHud.askCollect();
					const asked = fortressTaxHud.question();
					// 52C870 mode 9 moves the keyboard focus into the levy edit.
					if ( asked?.kind === "collect" ) focusAtEnd( FORTRESS_TAX_AMOUNT, asked.amount );
				}
			}
		} else if ( id === "fortress-schedule-close" ) {
			setPanel( "" );
		} else if ( id === "job-rank-close" ) {
			setPanel( "" );
		} else if ( id === "job-rank-prev" || id === "job-rank-next" ) {
			const open = jobHud.rank(),
				list = open && view.gameplay?.jobRanks?.lists.find( l => l.job === open.job && l.kind === open.kind );
			jobHud.pageRank( id === "job-rank-prev" ? -1 : 1, jobRankPages( list?.rows.length ?? 0 ) );
		} else if ( id === "fortress-schedule-prev" || id === "fortress-schedule-next" ) {
			fortressScheduleHud.page(
				id === "fortress-schedule-prev" ? -1 : 1,
				view?.gameplay?.fortress?.service?.applicants?.length ?? 0
			);
		} else if ( id === "npc-fortress-war" ) {
			// 5D8930 action 0x34 row 1: 0x71E1 subtype 6 asks for the status.
			const conversation = view.gameplay?.npcConversation;
			if ( conversation && conversation.phase === "menu" ) {
				fortressWarHud.request( conversation.gid );
				sendGameplay( { kind: "fortress-war-status", gid: conversation.gid } );
			}
		} else if ( id === "fortress-war-close" ) {
			setPanel( "" );
		} else if ( id.startsWith( "fortress-war-slot:" ) ) {
			const fortress = Number( id.slice( "fortress-war-slot:".length ) ),
				slot = fortressWarView()?.slots.find( r => r.fortress === fortress );
			if ( slot?.enabled ) fortressWarHud.ask( slot.question, fortress );
		} else if ( id === "skin-cancel" || id === "skin-close" ) {
			setPanel( "" );
		} else if ( id === "skin-confirm" ) {
			const open = skinHud.state(), choice = skinHud.choice();
			if ( open && choice && skinHud.changed() ) {
				sendGameplay( { kind: "item-use", slot: open.slot, skin: choice } );
				setPanel( "" );
			}
		} else if ( id === "skin:male" || id === "skin:female" ) {
			skinHud.set( "sex", id === "skin:male" ? 1 : 0 );
		} else if ( id.startsWith( "skin:" ) ) {
			const [, key, step] = id.split( ":" ), draft = skinHud.state()?.draft;
			if ( draft && (key === "figure" || key === "height" || key === "volume") ) {
				skinHud.set( key, draft[key] + (step === "next" ? 1 : -1) );
			}
		} else if ( id.startsWith( "skin-rotate:" ) ) {
			skinHud.rotate( id === "skin-rotate:left" ? -1 : id === "skin-rotate:right" ? 1 : 0 );
		} else if ( id.startsWith( "shop-repair:" ) ) {
			// 5B1C00 arms the repair cursor, or puts an armed one away; 5B2B10
			// totals the cost (789630) and asks before 0x746F mode 2, or says
			// nothing needs it.
			if ( id === "shop-repair:GDR_STORE_BTN_REPAIR" ) {
				if ( repairHud.armed() ) repairHud.disarm();
				else {
					carriedItem = null;
					carriedShortcut = null;
					inventorySlot = -1;
					repairHud.arm();
				}
			} else {
				const cost = repairAllCost( view.gameplay?.inventory ?? [] );
				if ( cost ) repairHud.ask( cost );
				else hudMessages.append( hudCopy( "UIIT_MSG_STRGERR_THERE_IS_NO_ITEM_TO_REPAIR" ), 0xffffffff );
			}
		} else if ( id.startsWith( "slot:" ) && repairHud.armed() ) {
			// 567290: the armed cursor judges each clicked item and stays armed,
			// so several items can be repaired in a row (gameplay owns the verdict).
			sendGameplay( { kind: "shop-repair", mode: 1, slot: Number( id.slice( 5 ) ) } );
		} else if ( id.startsWith( "slot:" ) ) {
			confirmDrop = "";
			const slot = Number( id.slice( 5 ) );
			if ( inventorySlot < 0 ) inventorySlot = slot;
			else if ( inventorySlot !== slot && !view.gameplay?.inventoryPending ) {
				const item = view.gameplay?.inventory.find( i => i.slot === inventorySlot );
				if ( item ) {
					sendGameplay( {
						kind: "inventory-move",
						source: inventorySlot,
						destination: slot,
						quantity: item.quantity
					} );
				}
				inventorySlot = -1;
			} else inventorySlot = -1;
		} else if ( id === "use" && inventorySlot >= 0 && view.gameplay ) useInventorySlot( inventorySlot );
		else if ( id === "drop-item" && view.gameplay ) {
			const item = view.gameplay.inventory.find( row => row.slot === inventorySlot );
			if ( item && item.slot >= 13 && !view.gameplay.inventoryPending ) {
				groundDrop = { slot: item.slot, refObjId: item.refObjId };
				inventorySlot = -1;
			}
		} else if ( id === "inventory-gold" ) {
			// With the warehouse open the inventory's money button deposits.
			goldDialog = panel === "Storage" ? "deposit" : "drop";
			goldAmount = "0";
			focusAndSelect( "gold-amount", 0, 1 );
		} else if ( id === "gold-cancel" ) {
			goldDialog = false;
			focus = null;
		} else if ( id === "drop-gold" && view.gameplay && goldDialog !== "drop" && goldDialog !== false ) {
			const amount = Number( goldAmount ),
				balance = goldDialog === "withdraw" ? view.gameplay.storage?.gold : view.gameplay.progression?.gold;
			if (
				!view.gameplay.inventoryPending && /^\d+$/.test( goldAmount ) && Number.isSafeInteger( amount ) &&
				amount > 0 && amount <= 0xffffffff && balance !== undefined && BigInt( amount ) <= BigInt( balance )
			) {
				sendGameplay( {
					kind: "storage-move",
					move: {
						type: goldDialog === "withdraw" ? STORAGE_GOLD_WITHDRAW : STORAGE_GOLD_DEPOSIT,
						source: 0,
						destination: 0,
						quantity: 0,
						gold: amount
					}
				} );
				goldDialog = false;
				focus = null;
				goldAmount = "";
			}
		} else if ( id === "drop-gold" && view.gameplay ) {
			const amount = Number( goldAmount ), balance = view.gameplay.progression?.gold;
			if (
				!view.gameplay.inventoryPending && /^\d+$/.test( goldAmount ) && Number.isSafeInteger( amount ) &&
				amount > 0 && amount <= 100000000 && balance !== undefined && BigInt( amount ) <= BigInt( balance )
			) {
				sendGameplay( { kind: "gold-drop", amount } );
				goldDialog = false;
				focus = null;
				goldAmount = "";
			}
		} else if ( id === "magic-option-open" && view.gameplay?.target ) {
			// 5DA1B0 case 0x2F; B338 lock 0x80000000 then shows the window.
			sendGameplay( { kind: "magic-option-open", gid: view.gameplay.target } );
		} else if ( id.startsWith( "magic-option-row:" ) ) magicOptionHud.choose( id.slice( 17 ) );
		else if ( id === "magic-option-up" || id === "magic-option-down" ) {
			const grant = view.gameplay?.magicOption,
				item = view.gameplay?.inventory.find( r => r.slot === grant?.item ),
				part = item ? grantableAvatarPart( item.typeFlags ) : null;
			magicOptionHud.scroll(
				id === "magic-option-up" ? -1 : 1,
				grant?.parts.find( p => p.part === part )?.options.length ?? 0
			);
		} else if ( id === "magic-option-confirm" ) {
			const codename = magicOptionHud.state().codename;
			if ( codename ) sendGameplay( { kind: "magic-option-grant", codename } );
		} else if ( id === "magic-option-cancel" || id === "magic-option-close" ) setPanel( "" );
		else if ( id === "storage-open" && view.gameplay?.target ) {
			if ( !canLeavePanel() ) return;
			sendGameplay( { kind: "storage-open", gid: view.gameplay.target } );
			storagePanel.reset();
			setPanel( "Storage" );
		} else if ( (id === "storage-prev" || id === "storage-next") && view.gameplay?.storage ) {
			storagePanel.turn( id === "storage-next" ? 1 : -1, view.gameplay.storage.capacity );
		} else if ( id === "storage-gold" && view.gameplay?.storage?.phase === "open" ) {
			goldDialog = "withdraw";
			goldAmount = "0";
			focusAndSelect( "gold-amount", 0, 1 );
		} else if ( (id === "shop-open" || id.startsWith( "shop-group:" )) && view.gameplay?.target ) {
			if ( !canLeavePanel() ) return;
			const branches = view.entities.find( e => e.gid === view?.gameplay?.target )?.merchantBranches,
				branch = id === "shop-open" ? branches?.[0] : branches?.find( b => b.id === Number( id.slice( 11 ) ) );
			if ( id !== "shop-open" && !branch ) return;
			if ( panel === "Shop" && view.gameplay.shop?.npc === view.gameplay.target ) return;
			if ( shopOpenRequest || view.gameplay.inventoryPending ) return;
			sendGameplay( { kind: "shop-open", gid: view.gameplay.target } );
			shopOpenRequest = {
				gid: view.gameplay.target,
				tab: branch?.tabs[0] ?? 0,
				revision: view.gameplay.shopCompletionRevision ?? 0
			};
		} else if ( id === "shop-next" ) {
			shopPage++;
			shopChoice = null;
		} else if ( id === "shop-prev" ) {
			shopPage = Math.max( 0, shopPage - 1 );
			shopChoice = null;
		} else if ( id.startsWith( "shop-tab:" ) ) {
			// 5B28F0: only a different tab resets the page; the open tab's
			// button just refills the page already shown.
			const tab = Number( id.slice( 9 ) );
			if ( tab !== shopTab ) {
				shopTab = tab;
				shopPage = 0;
			}
			shopChoice = null;
			shopDialog = false;
		} else if ( id.startsWith( "shop-offer:" ) || id.startsWith( "shop-buyback:" ) ) {
			const offer = id.startsWith( "shop-offer:" ) ?
				view.gameplay?.shop?.offers[Number( id.slice( 11 ) )] :
				undefined;
			if ( offer && view.gameplay?.shop?.cosGid && isTradeGoods( offer.items?.[0]?.typeFlags ?? 0 ) ) {
				openSpecialtyPurchase( Number( id.slice( 11 ) ) );
			} else if ( view.gameplay?.shop ) {
				beginShopDialog(
					merchantSelection(
						id.startsWith( "shop-offer:" ) ? "buy" : "buyback",
						Number( id.slice( id.indexOf( ":" ) + 1 ) ),
						view.gameplay.shop,
						merchantRows( view.gameplay )
					),
					"1"
				);
			}
		} else if ( id === "shop-cancel" ) closeShopDialog();
		else if ( id === "shop-quantity-up" || id === "shop-quantity-down" ) {
			const q = merchantQuote(
				shopChoice,
				view.gameplay?.shop,
				merchantRows( view.gameplay ),
				shopQuantity,
				view.gameplay?.progression?.gold
			);
			if ( q ) {
				shopQuantity = String(
					Math.max( 1, Math.min( q.maximum, Number( shopQuantity ) + (id === "shop-quantity-up" ? 1 : -1) ) )
				);
			}
		} else if ( id.startsWith( "cos-tab:" ) ) {
			cosTab = Number( id.slice( 8 ) );
			cosDraft = view.gameplay?.cosRecords?.find( r => r.gid === cosGid )?.commandMode ?? 0;
		} else if ( id.startsWith( "cos-setting:" ) ) cosDraft ^= Number( id.slice( 12 ) );
		else if ( id.startsWith( "cos-radio:" ) ) {
			const [, mask, index] = id.split( ":" ),
				bit = Number( mask ),
				on = bit === 128 ? index === "0" : index === "1";
			cosDraft = on ? cosDraft | bit : cosDraft & ~bit;
		} else if ( id === "cos-save" ) {
			const record = view.gameplay?.cosRecords?.find( r => r.gid === cosGid );
			if ( record && record.commandMode !== cosDraft ) {
				sendGameplay( { kind: "cos-behavior", gid: cosGid, mode: cosDraft } );
			}
		} else if ( id === "cos-reset" ) {
			cosDraft = view.gameplay?.cosRecords?.find( r => r.gid === cosGid )?.commandMode ?? 0;
		} else if ( id.startsWith( "guild-tab:" ) ) {
			const tab = Number( id.slice( 10 ) );
			if ( tab === 4 ) setPanel( "Blocking" );
			else if ( tab !== guildTab ) {
				guildTab = tab;
				socialMember = 0;
				socialPage = 0;
			}
		} // 5C50B0 / 81AFB0: only the local academy master opens mode 2.
		else if ( id === "academy-notice" ) {
			const camp = view.gameplay?.academy;
			if ( camp?.member && camp.members?.some( m => m.kind === 0 && m.id === camp.localMemberId ) ) {
				guildDialog = "academy-notice";
				socialSubject = "";
				socialContents = "";
			}
		} else if ( id === "guild-dialog:authority" ) {
			const guild = view.gameplay?.social?.guild;
			if ( guild ) grantPowerHud.open( guild.members );
		} else if ( id.startsWith( "guild-grant:" ) ) {
			const [member, right] = id.slice( 12 ).split( ":" ).map( Number );
			grantPowerHud.toggle( member!, right! );
		} else if ( id === "guild-grant-prev" || id === "guild-grant-next" ) {
			grantPowerHud.scroll( id === "guild-grant-next" ? 1 : -1 );
		} else if ( id === "guild-grant-ok" ) {
			const grants = grantPowerHud.grants();
			if ( grants.length ) sendGameplay( { kind: "guild-permissions", grants } );
			grantPowerHud.close();
		} else if ( id === "guild-grant-cancel" ) grantPowerHud.close();
		else if ( id.startsWith( "guild-dialog:" ) ) {
			guildDialog = id.slice( 13 );
			socialSubject = view.gameplay?.social?.guild?.subject ?? "";
			socialContents = view.gameplay?.social?.guild?.contents ?? "";
		} else if ( id === "guild-dialog-close" ) guildDialog = "";
		else if ( id.startsWith( "guild-role-choice:" ) ) {
			socialAmount = String( fortressGrantRole( Number( id.slice( 18 ) ) ) );
		} else if ( id === "guild-sort:126" ) guildNameMode = (guildNameMode + 1) % 3;
		else if ( id.startsWith( "guild-sort:" ) ) {
			const sort = Number( id.slice( 11 ) );
			guildDescending = guildSort === sort ? !guildDescending : false;
			guildSort = sort;
			socialPage = 0;
		} else if ( id.startsWith( "alchemy-count:" ) ) {
			alchemyQuantity = String( Math.max( 1, Number( alchemyQuantity ) + Number( id.slice( 14 ) ) ) );
		} else if ( id === "alchemy-all" ) {
			alchemyQuantity = String(
				alchemySlots.slice( 1 ).reduce(
					( n, slot ) => n + (view?.gameplay?.inventory.find( r => r.slot === slot )?.quantity ?? 0),
					0
				)
			);
			activate( "alchemy-start" );
		} else if ( id === "shop-trade" ) {
			const game = view.gameplay,
				quote = merchantQuote(
					shopChoice,
					game?.shop,
					merchantRows( game ),
					shopQuantity,
					game?.progression?.gold
				);
			if (
				shopDialog && shopChoice && quote?.valid && game && !game.inventoryPending &&
				game.target === shopChoice.npc
			) {
				sendGameplay( merchantCommand( shopChoice, quote.quantity ) );
				closeShopDialog();
			}
		} else if ( id === "mount" && view.gameplay?.target ) {
			sendGameplay( { kind: "mount", gid: view.gameplay.target } );
		} else if ( id === "pickup" && view.gameplay?.target ) {
			sendGameplay( { kind: "pickup", gid: view.gameplay.target } );
		} else if ( id === "attack" && view.gameplay?.target ) {
			sendGameplay( { kind: "attack", gid: view.gameplay.target } );
		} else if ( id.startsWith( "party-target:" ) ) {
			sendGameplay( { kind: "select", gid: Number( id.slice( 13 ) ) } );
		} else if ( id === "self-target" && view.gameplay?.localGid ) {
			sendGameplay( { kind: "select", gid: view.gameplay.localGid } );
			requestRebirthPrompt( view.gameplay.localGid );
		} else if ( id === "clear-target" ) sendGameplay( { kind: "release-target" } );
		else if ( id === "target-structure-remove" ) {
			// 517750 asks before 517CA0 sends anything.
			const game = view.gameplay, target = view.entities.find( row => row.gid === game?.target );
			const action = target ? structureRemoveAction( target ) : 0, fortress = fortressStaffView().fortress;
			if ( target && action && fortress !== undefined ) {
				structureRemoval = { gid: target.gid, action, fortress, name: target.name };
			}
		} else if ( id === "inventory-next" ) {
			// 59DF10 pages the bag alone: the 13 sockets hold no page.
			const bag = inventorySlots(
				0,
				0,
				view.gameplay?.inventorySlotCount ?? 0,
				view.gameplay?.equipmentSlotCount ?? 13,
				0
			);
			inventoryPage = Math.min( bag.pages - 1, inventoryPage + 1 );
		} else if ( id === "inventory-prev" ) inventoryPage = Math.max( 0, inventoryPage - 1 );
		else if ( id.startsWith( "inventory-page:" ) ) inventoryPage = Number( id.slice( 15 ) );
		else if ( id === "equipment-view" ) {
			avatarView = !avatarView;
			inventorySlot = -1;
			carriedItem = null;
		} else if ( id.startsWith( "avatar:" ) ) { if ( inventorySlot >= 13 ) moveAvatar( true, inventorySlot ); }
		else if ( id === "doll-left" ) dollYaw -= .1;
		else if ( id === "doll-right" ) dollYaw += .1;
		else if ( id === "doll-reset" ) dollYaw = .100000001;
		else if ( id === "server-next" ) serverPage++;
		else if ( id === "server-prev" ) serverPage = Math.max( 0, serverPage - 1 );
		dirty = true;
	}
	/*
	================
	optionOrigin
	================
	*/
	function optionOrigin(): UiRect {
		return windowPlacement.read( "window-drag:Option" ) ??
			[
				Math.max( 0, Math.floor( ((view?.width ?? 386) - 386) / 2 ) ),
				Math.max( 0, Math.floor( ((view?.height ?? 413) - 413) / 2 ) ),
				386,
				413
			];
	}
	return {
		/*
		================
		mallPreview
		================
		*/
		mallPreview() {
			return itemMall.previewRequest();
		},
		/*
		================
		skinPreview

		The skin change window's body for the mannequin, or null.
		================
		*/
		skinPreview() {
			return panel === SKIN_PANEL ? skinHud.preview() : null;
		},
		/*
		================
		mallPreviewState
		================
		*/
		mallPreviewState( value: import("@/engine/contracts/item-mall").MallPreviewState ) {
			if ( itemMall.previewState( value ) ) dirty = true;
		},
		/*
		================
		runtimeError
		================
		*/
		runtimeError( message: string ) {
			hudMessages.append( "Client error: " + message, 0xffff7070 );
			dirty = true;
		},
		/*
		================
		resourceError
		================
		*/
		resourceError: () =>
			resources.error() ?? hud.error() ?? text.error() ?? guideResources.error() ?? minimapResources.error() ??
				null,
		/*
		================
		entryReady

		Basic HUD and Help artwork must be decoded before releasing world entry.
		Visible-window demand remains independent of this stable baseline.
		================
		*/
		entryReady() {
			const main = hud.data(), guide = guideResources.data();
			return !!main && !!guide && resources.residentAll( main.warmPaths ) &&
				resources.residentAll( guide.warmPaths );
		},
		/*
		================
		stats
		================
		*/
		stats: () => ({
			...resources.stats(),
			layoutRetention: {
				chat: chatLayoutCache.stats(),
				status: statusLayoutCache.stats(),
				player: playerLayoutCache.stats(),
				bar: barLayoutCache.stats()
			},
			panel,
			windowMissing,
			windowReady: !([ "Academy Matching", "Game Guide", "Quests" ].includes( panel ) && !guideResources.data()),
			hudSettling: hud.settling(),
			error: diagnosticError
		}),
		/*
		================
		event
		================
		*/
		event( event: UiEvent ) {
			if ( disposed ) return;
			// An edit's normalized value (a clamped quantity, a filtered digit) can
			// equal the value already published, so value comparison would publish
			// nothing and the field would keep the raw keystrokes. Forget the last
			// product: the next step republishes and the bridge reconciles the field.
			if ( event.kind === "edit" ) {
				lastProduct = null;
				dirty = true;
			}
			// Ahead of every modal gate: an abandoned carry must always clear.
			if ( event.kind === "drag-cancel" ) {
				if ( carriedItem?.source === event.id ) carriedItem = null;
				if ( carriedShortcut?.id === event.id ) carriedShortcut = null;
				dirty = true;
				return;
			}
			if ( itemMall.read().visible ) {
				if ( event.kind === "edit" && view?.gameplay?.itemMall ) {
					const state = itemMall.read( view.gameplay.itemMall );
					if ( event.id === "item-mall-point-value" && /^\d{0,10}$/.test( event.value ) ) {
						itemMall.editPointDraft( Number( event.value ), view.gameplay.itemMall );
					}
					if ( event.id === "item-mall-quantity" && /^\d{0,5}$/.test( event.value ) ) {
						itemMall.edit( Number( event.value ), state.points, view.gameplay.itemMall );
					}
					dirty = true;
					return;
				}

				// F10 shuts the mall it opened, as every window hotkey toggles; an
				// open question or point box is cancelled with the window.
				if ( event.kind === "key" && event.code === "F10" ) {
					activate( "item-mall-close" );
					return;
				}
				if ( event.kind === "key" && event.code === "Escape" ) {
					activate(
						itemMall.read().pointDialog ?
							"item-mall-points-close" :
							itemMall.read().question ?
							"item-mall-question-cancel" :
							itemMall.read().selected ?
							"item-mall-cancel" :
							"item-mall-close"
					);
					return;
				}
				if ( event.kind === "activate" && event.id.startsWith( "item-mall" ) ) {
					activate( event.id );
					return;
				}
				if (
					event.kind === "key" || event.kind === "activate" || event.kind === "double-activate" ||
					event.kind === "right-activate" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "world-select" || event.kind === "whisper-target"
				) return;
			}
			if ( event.kind === "whisper-target" ) {
				const gid = event.gid;
				const entity = view?.entities.find( row => row.gid === gid && row.kind === "player" );
				if ( view?.session?.phase !== "world" || !entity ) return;
				event = { kind: "activate", id: "whisper-name:" + entity.name };
			}
			if ( event.kind === "world-select" ) {
				requestRebirthPrompt( event.gid );
				return;
			}
			// Both physical Enter keys are native VK_RETURN, including modal handlers.
			if ( event.kind === "key" && event.code === "NumpadEnter" ) event = { ...event, code: "Enter" };
			if ( activeNoticeDialog && view?.session?.phase === "world" ) {
				if ( event.kind === "activate" && event.id === "notice-dialog-confirm" ) {
					activeNoticeDialog = undefined;
					noticeDialogPosition = null;
					dirty = true;
					return;
				}
				if ( event.kind === "drag" && event.id === "notice-dialog-drag" && noticeDialogFrame ) {
					noticeDialogPosition = [ noticeDialogFrame[0] + event.dx, noticeDialogFrame[1] + event.dy ];
					dirty = true;
					return;
				}
				// Kind 9 has no native Enter/Escape handler. Block underlying controls.
				if (
					event.kind === "key" || event.kind === "activate" || event.kind === "double-activate" ||
					event.kind === "right-activate" || event.kind === "scroll" || event.kind === "drag" ||
					event.kind === "drag-end" || event.kind === "edit"
				) return;
			}
			if ( reverseScrollHud.reconcile( view ) ) dirty = true;
			if ( reverseScrollHud.active() ) {
				if ( event.kind === "key" && event.code === "Escape" ) {
					reverseScrollHud.close();
					dirty = true;
				} else if (
					event.kind === "activate" &&
					[ "reverse-scroll:2", "reverse-scroll:3", "reverse-scroll-cancel" ].includes( event.id )
				) {
					activate( event.id );
				}
				// Message box 0x1E owns input until a destination or Cancel wins.
				return;
			}
			if ( shopWarning ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "shop-warning-cancel"
				) {
					shopWarning = null;
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "shop-warning-confirm"
				) {
					const pending = shopWarning;
					shopWarning = null;
					dirty = true;
					const game = view?.gameplay,
						item = merchantRows( game ).find( i =>
							pending.selection.kind === "sell" && i.slot === pending.selection.slot
						);
					if (
						item && game?.shop && pending.selection.npc === game.shop.npc &&
						(pending.selection.kind === "buyback" || pending.selection.cosGid === game.shop.cosGid) &&
						merchantBinding( item ) === pending.selection.binding
					) openShopSale( item, pending.quick, true );
					return;
				}
				if (
					event.kind === "key" || event.kind === "activate" || event.kind === "double-activate" ||
					event.kind === "right-activate" || event.kind === "scroll" || event.kind === "drag" ||
					event.kind === "drag-end" || event.kind === "edit"
				) return;
			}
			if ( recallConfirm !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "recall-cancel"
				) {
					recallConfirm = null;
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "recall-confirm"
				) {
					const gid = recallConfirm;
					recallConfirm = null;
					dirty = true;
					const g = view?.gameplay;
					if (
						view?.session?.phase === "world" && g?.npcConversation?.phase === "menu" &&
						g.npcConversation.gid === gid && g.target === gid && ((g.targetCapabilities ?? 0) & 0x40)
					) sendGameplay( { kind: "recall-appoint", gid } );
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if ( fortressProductionHud.question() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "fortress-production-no"
				) {
					fortressProductionHud.takeQuestion();
					dirty = true;
					return;
				}
				if ( event.kind === "edit" && event.id === FORTRESS_PRODUCTION_COUNT ) {
					fortressProductionHud.edit( event.value );
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "fortress-production-yes"
				) {
					// The make box starts the order; the cancel box's confirm reaches
					// CIFFortressMakeItemWnd_OnCancelMsgBox (656480).
					const asked = fortressProductionHud.takeQuestion();
					dirty = true;
					if ( asked?.kind === "make" && Number( asked.count ) > 0 ) {
						sendFortressProduction( FORTRESS_PRODUCTION_START, {
							reference: asked.item.refObjId,
							count: Number( asked.count )
						} );
					} else if ( asked?.kind === "cancel" ) {
						sendFortressProduction( FORTRESS_PRODUCTION_CANCEL, { reference: asked.order.refObjId } );
					}
					return;
				}
				if ( event.kind === "activate" ) return;
			}
			if ( fortressTaxHud.question() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "fortress-tax-no"
				) {
					fortressTaxHud.takeQuestion();
					dirty = true;
					return;
				}
				if ( event.kind === "edit" && event.id === FORTRESS_TAX_AMOUNT ) {
					fortressTaxHud.edit( event.value );
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "fortress-tax-yes"
				) {
					// CIFTaxManagement_OnMsgBoxResult (664CE0).
					const asked = fortressTaxHud.takeQuestion(),
						gid = fortressTaxHud.npc(),
						context = fortressTaxHud.context();
					dirty = true;
					if ( !asked || gid === null || !context || view?.session?.phase !== "world" ) return;
					if ( view.gameplay?.target !== gid ) return;
					if ( asked.kind === "rate" ) {
						// An unchanged ratio is refused here, never sent.
						if ( asked.from === asked.to ) {
							hudMessages.append( hudCopy( "UIIT_MSG_FORT_MANAGER_TAX_ERROR_03" ) );
						} else {sendGameplay( {
								kind: "fortress-tax-rate",
								gid,
								fortress: context.fortress,
								rate: asked.to
							} );}
					} else if ( asked.amount ) {
						sendGameplay( {
							kind: "fortress-tax-collect",
							gid,
							fortress: context.fortress,
							gold: asked.amount
						} );
					}
					return;
				}
				if ( event.kind === "activate" ) return;
			}
			if ( fortressStaffHud.question() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "fortress-staff-no"
				) {
					fortressStaffHud.takeQuestion();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "fortress-staff-yes"
				) {
					const asked = fortressStaffHud.takeQuestion(), staff = fortressStaffView();
					dirty = true;
					if (
						asked && staff.holder && staff.commander && !(staff.flags & asked.flag) &&
						view?.session?.phase === "world" && view.gameplay?.npcConversation?.phase === "menu" &&
						view.gameplay.npcConversation.gid === asked.gid && view.gameplay.target === asked.gid
					) sendGameplay( { kind: "fortress-staff", ...asked } );
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if ( fortressWarHud.question() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "fortress-war-no"
				) {
					fortressWarHud.takeQuestion();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "fortress-war-yes"
				) {
					const asked = fortressWarHud.takeQuestion(), npc = fortressWarHud.npc();
					dirty = true;
					if ( asked && npc !== null && view?.session?.phase === "world" ) {
						sendGameplay( {
							kind: "fortress-war-apply",
							gid: npc,
							fortress: asked.fortress,
							...fortressWarRequest( asked.question )
						} );
					}
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if ( guildManagerHud.question() || guildManagerHud.field() || guildManagerHud.vote() ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "guild-manager-no"
				) {
					// A declined claim box drops the quote (5D4050's No).
					if ( guildManagerHud.takeQuestion()?.kind === "compensation" ) {
						sendGameplay( { kind: "compensation-dismiss" } );
					}
					guildManagerHud.reset();
					dirty = true;
					return;
				}
				if ( guildManagerHud.field() && event.kind === "edit" && event.id === "guild-manager-text" ) {
					guildManagerHud.type( event.value );
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "guild-manager-yes"
				) {
					dirty = true;
					if ( guildManagerHud.vote() ) {
						guildManagerHud.reset();
						return;
					}
					if ( view?.session?.phase !== "world" ) return;
					const asked = guildManagerHud.takeQuestion();
					if ( asked ) {
						if ( asked.kind === "level-up" ) sendGameplay( { kind: "guild-level-up", gid: asked.npc } );
						else if ( asked.kind === "dissolve" ) {
							sendGameplay( { kind: "guild-dissolve", gid: asked.npc } );
						} else if ( asked.kind === "secede" ) sendGameplay( { kind: "guild-leave", gid: asked.npc } );
						else if ( asked.kind === "release" ) sendGameplay( { kind: "guild-release", gid: asked.npc } );
						else {
							sendGameplay( { kind: "guild-compensation-claim", gid: asked.npc } );
							sendGameplay( { kind: "compensation-dismiss" } );
						}
						return;
					}
					const entry = guildManagerHud.field();
					if ( entry?.text && entry.kind === "create" ) {
						guildManagerHud.takeField();
						sendGameplay( { kind: "guild-create", gid: entry.npc, name: entry.text } );
					} else if ( entry?.text ) {
						// 5D3EB0: the master names a member; only a member may take over.
						const member = view.gameplay?.social?.guild?.members.find( m => m.name === entry.text );
						if ( member && member.grade !== 0 ) {
							guildManagerHud.takeField();
							sendGameplay( { kind: "guild-master-leave", gid: entry.npc, id: member.id } );
						}
					}
					return;
				}
				if ( event.kind === "activate" ) return;
			}
			if ( jobHud.confirm() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "job-confirm-no"
				) {
					jobHud.takeConfirm();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "job-confirm-yes"
				) {
					const asked = jobHud.takeConfirm();
					dirty = true;
					if ( asked && view?.session?.phase === "world" ) {
						sendGameplay(
							asked.kind === "join" ?
								{ kind: "job-join", gid: asked.npc, job: asked.job } :
								{ kind: "job-withdraw", gid: asked.npc }
						);
					}
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			const specialtyDeal = specialtyDealHud.state();
			if ( specialtyDeal !== null ) {
				// CIFSpecialtyDeal: Esc or the frame's close leaves it unless a deal
				// is running; Enter in the count edit confirms like OK (control 40).
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "specialty-deal-close"
				) {
					if ( !specialtyDeal.dealing ) specialtyDealHud.close();
					specialtyCombo = false;
					focus = null;
					dirty = true;
					return;
				}
				if ( event.kind === "edit" && event.id === SPECIALTY_DEAL_COUNT ) {
					specialtyDealHud.type( event.value, specialtyLimit( view?.gameplay ) );
					dirty = true;
					return;
				}
				if ( event.kind === "activate" && event.id === "specialty-deal-scale" ) {
					if ( specialtyDeal.mode === "buy" && !specialtyDeal.dealing ) specialtyCombo = !specialtyCombo;
					dirty = true;
					return;
				}
				if ( event.kind === "activate" && event.id.startsWith( "specialty-deal-row:" ) ) {
					pickSpecialtyScale( Number( event.id.slice( 19 ) ) );
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing && focus === SPECIALTY_DEAL_COUNT ||
					event.kind === "activate" && event.id === "specialty-deal-ok"
				) {
					confirmSpecialtyDeal();
					dirty = true;
					return;
				}
			}
			const wholeChat = globalChatHud.state();
			if ( wholeChat !== null ) {
				// CIFWholeChat_OnKey (6D2290): Enter sends, Esc closes.
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && (event.id === "wholechat-exit" || event.id === "wholechat-close")
				) {
					globalChatHud.close();
					focus = null;
					dirty = true;
					return;
				}
				if ( event.kind === "edit" && event.id === GLOBAL_CHAT_TEXT ) {
					globalChatHud.type( event.value );
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing && focus === GLOBAL_CHAT_TEXT ||
					event.kind === "activate" && event.id === "wholechat-use"
				) {
					sendGlobalChat( wholeChat.slot, wholeChat.text );
					dirty = true;
					return;
				}
			}
			const aliasWindow = jobHud.alias();
			if ( aliasWindow !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "job-alias-cancel"
				) {
					jobHud.closeAlias();
					dirty = true;
					return;
				}
				if ( event.kind === "edit" && event.id === "job-alias-text" ) {
					jobHud.typeAlias( event.value );
					dirty = true;
					return;
				}
				if ( event.kind === "activate" && (event.id === "job-alias-check" || event.id === "job-alias-ok") ) {
					// 6461C0 asks whether the name is free (mode 0); 646470 takes it (mode 1).
					if ( aliasWindow.text && view?.session?.phase === "world" ) {
						sendGameplay( {
							kind: "job-alias",
							gid: aliasWindow.npc,
							mode: event.id === "job-alias-ok" ? JOB_ALIAS_CREATE : JOB_ALIAS_CHECK,
							alias: aliasWindow.text
						} );
						if ( event.id === "job-alias-ok" ) jobHud.closeAlias();
					}
					dirty = true;
					return;
				}
				if ( event.kind === "activate" ) return;
			}
			if ( repairHud.confirmCost() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "repair-all-cancel"
				) {
					repairHud.takeConfirm();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "repair-all-confirm"
				) {
					repairHud.takeConfirm();
					dirty = true;
					if ( view?.session?.phase === "world" ) sendGameplay( { kind: "shop-repair", mode: 2, slot: 0 } );
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if ( cosHud.targetUse() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === cosHud.targetUseBox() + "-cancel"
				) {
					cosHud.takeTargetUse();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === cosHud.targetUseBox() + "-confirm"
				) {
					const command = cosHud.takeTargetUse();
					if ( command && view?.session?.phase === "world" && !view.gameplay?.inventoryPending ) {
						sendGameplay( command );
					}
					dirty = true;
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if ( cosHud.itemTargetCursor() !== null ) {
				if ( event.kind === "right-activate" || event.kind === "key" && event.code === "Escape" ) {
					cosHud.takeTargetUse();
					dirty = true;
					return;
				}
				if ( event.kind === "activate" && event.id.startsWith( "slot:" ) ) {
					if ( controls.some( c => c.id === event.id && !c.disabled ) ) {
						cosHud.chooseItemTarget(
							view?.gameplay?.inventory.find( item => item.slot === Number( event.id.slice( 5 ) ) )
						);
						dirty = true;
					}
					return;
				}
				if (
					event.kind === "drag" || event.kind === "drag-end" || event.kind === "double-activate"
				) return;
			}
			if ( repairHud.armed() ) {
				// 564046 checks the cursor mode before right-button item use.
				if ( event.kind === "right-activate" || event.kind === "key" && event.code === "Escape" ) {
					repairHud.disarm();
					dirty = true;
					return;
				}
				// 5672D1..5672DD branches on cursor mode before CTRL/SHIFT/ALT or pickup.
				if ( event.kind === "activate" && event.id.startsWith( "slot:" ) ) {
					if ( controls.some( c => c.id === event.id && !c.disabled ) ) activate( event.id );
					return;
				}
				if (
					(event.kind === "drag" || event.kind === "drag-end" || event.kind === "double-activate") &&
					ITEM_SLOT_PREFIXES.some( prefix => event.id.startsWith( prefix ) )
				) return;
			}
			if ( structureRemoval ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "structure-remove-cancel"
				) {
					structureRemoval = null;
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "structure-remove-confirm"
				) {
					// 517CA0 on OK: {target, 0x16 or 0x17, fortress}.
					const { gid, action, fortress } = structureRemoval;
					structureRemoval = null;
					dirty = true;
					if ( view?.session?.phase === "world" ) {
						sendGameplay( { kind: "fortress-dismantle", gid, action, fortress } );
					}
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if ( cosHud.cleanConfirm() !== null ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "cos-clean-cancel"
				) {
					cosHud.takeClean();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "cos-clean-confirm"
				) {
					// CGInterface_OnMsgBoxResult case 0xA sends 0x7618 on OK.
					const gid = cosHud.takeClean();
					dirty = true;
					if ( gid !== null && view?.session?.phase === "world" ) sendGameplay( { kind: "cos-clean", gid } );
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			// Map teleport: confirmation modal, then the GM warp.
			const teleportTarget = mapTeleport.pending();
			if ( teleportTarget ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "map-teleport-cancel"
				) {
					mapTeleport.clear();
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "map-teleport-confirm"
				) {
					mapTeleport.clear();
					dirty = true;
					if ( view?.session?.phase === "world" && view.gameplay?.eligibility?.gm ) {
						sendGameplay( { kind: "gm-command", line: mapTeleport.command( teleportTarget ) } );
					}
					return;
				}
				if ( event.kind !== "hover" ) return;
			}
			if (
				event.kind === "region-double" && event.id === "map-pan" &&
				view?.session?.phase === "world" && view.gameplay?.eligibility?.gm
			) {
				if ( mapTeleport.pick( event.x, event.y ) ) dirty = true;
				return;
			}
			if ( buffDismiss ) {
				if (
					event.kind === "key" && event.code === "Escape" ||
					event.kind === "activate" && event.id === "buff-dismiss-cancel"
				) {
					buffDismiss = null;
					dirty = true;
					return;
				}
				if (
					event.kind === "key" && event.code === "Enter" && !composing ||
					event.kind === "activate" && event.id === "buff-dismiss-confirm"
				) {
					const pending = buffDismiss;
					buffDismiss = null;
					dirty = true;
					if (
						view?.session?.phase === "world" &&
						view.gameplay?.buffSlots?.some( s =>
							s.state !== "departing" && !s.secondary && s.effect.gid === view!.gameplay!.localGid &&
							s.effect.skill === pending.skillId && s.effect.token === pending.instance
						)
					) sendGameplay( { kind: "effect-cancel", skillId: pending.skillId, token: 0 } );
					return;
				}
				if (
					event.kind === "key" || event.kind === "activate" || event.kind === "double-activate" ||
					event.kind === "right-activate" || event.kind === "scroll" || event.kind === "drag" ||
					event.kind === "drag-end" || event.kind === "edit"
				) return;
			}

			if ( groundDrop ) {
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( "ground-drop-cancel" );
					else if ( event.code === "Enter" && !composing ) activate( "ground-drop-confirm" );
					return;
				}
				if (
					event.kind === "scroll" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "double-activate" || event.kind === "edit"
				) return;
				if ( "id" in event && event.id !== null && !event.id.startsWith( "ground-drop-" ) ) return;
			}
			if ( event.kind === "drag" && !controls.some( c => c.id === event.id && c.draggable && !c.disabled ) ) {
				return;
			}
			if ( event.kind === "chat-blocks" ) {
				localBlocks = chatBlocks( event.value );
				dirty = true;
				return;
			}
			if ( view?.gameplay?.partyMatching?.request || partyDialog ) {
				const request = !!view?.gameplay?.partyMatching?.request;
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( request ? "party-answer:2" : "party-form-cancel" );
					else if ( event.code === "Enter" && !composing ) {
						activate( request ? "party-answer:1" : "party-form-confirm" );
					}
					return;
				}
				if (
					event.kind === "scroll" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "double-activate"
				) return;
				if (
					"id" in event && event.id !== null &&
					!(request ? event.id.startsWith( "party-answer:" ) : event.id.startsWith( "party-form-" ))
				) return;
			}
			if ( shopDialog && panel === "Shop" ) {
				if (
					event.kind === "edit" && event.id === "shop-quantity" &&
					merchantQuote(
							shopChoice,
							view?.gameplay?.shop,
							merchantRows( view?.gameplay ),
							shopQuantity,
							view?.gameplay?.progression?.gold
						)?.quantityMode !== "editable"
				) return;
				if ( (event.kind === "edit" || event.kind === "focus") && event.id === "shop-quantity" ) {
					caretVisible = true;
					caretDue = 0;
				}
				if ( event.kind === "drag" && event.id === "shop-dialog-drag" && view ) {
					const confirm = shopChoice?.kind !== "buy",
						box = messageBox( view.width, view.height, 327, confirm ? 175 : 177, shopPosition );
					shopPosition = [ box.frame[0] + event.dx, box.frame[1] + event.dy ];
					dirty = true;
					return;
				}
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( "shop-cancel" );
					else if ( event.code === "Enter" && !composing ) activate( "shop-trade" );
					return;
				}
				if (
					event.kind === "drag" || event.kind === "drag-end" || event.kind === "double-activate" ||
					event.kind === "scroll"
				) return;
				if (
					"id" in event && event.id !== null &&
					![
						"shop-cancel",
						"shop-trade",
						"shop-quantity",
						"shop-quantity-up",
						"shop-quantity-down",
						"submit"
					].includes( event.id )
				) return;
			}
			if ( guildDialog ) {
				if ( event.kind === "key" && event.code === "Escape" ) {
					guildDialog = "";
					dirty = true;
					return;
				}
				if (
					event.kind === "drag" || event.kind === "drag-end" || event.kind === "double-activate" ||
					event.kind === "scroll"
				) return;
				if ( "id" in event && event.id !== null && !controls.some( c => c.id === event.id ) ) return;
			}
			if ( event.kind === "scroll" && controls.some( c => c.id === "war-combo-thumb" ) ) {
				const state = guildWarHud.state();
				guildWarHud.scroll(
					"combo",
					Math.sign( event.delta ),
					[ 8, 32, 25, 7 ][state.combo - 23] ?? 0,
					state.combo === 23 ? 8 : 7
				);
				dirty = true;
				return;
			}
			if ( event.kind === "scroll" && panel === "Guild" ) {
				if ( grantPowerHud.isOpen() ) grantPowerHud.scroll( Math.sign( event.delta ) );
				else if ( guildTab === 2 || guildTab === 1 && guildWarHud.state().relation === 1 ) {
					const state = guildWarHud.state(), social = view?.gameplay?.social;
					const memberControl = controls.find( c => c.id === "war-members-up" );
					const members = !!memberControl && event.y >= memberControl.rect[1] - 16;
					guildWarHud.scroll(
						members ? "members" : "enemies",
						Math.sign( event.delta ),
						members ? social?.guild?.members.length ?? 0 : social?.wars?.length ?? 0,
						members ? 3 : guildTab === 2 ? 6 : 9
					);
				} else socialPage = Math.max( 0, socialPage + Math.sign( event.delta ) );
				dirty = true;
				return;
			}
			if ( event.kind === "scroll" && panel === "Shop" && view && !view.gameplay?.inventoryPending ) {
				const frame = windowPlacement.read( "window-drag:Shop" ),
					zone = controls.find( c => c.id === "shop-scroll-area" );
				const inside = zone ?
					containsPoint( zone.rect, event.x, event.y ) :
					!!frame && containsPoint( [ frame[0] + 18, frame[1] + 60, 227, 219 ], event.x, event.y );
				if ( inside ) {
					const page = merchantPage(
						view.gameplay?.shop,
						shopTab,
						shopPage + Math.sign( event.delta ),
						view.entities.find( e => e.gid === view?.gameplay?.target )?.merchantBranches?.find( b =>
							b.tabs.includes( shopTab )
						)?.tabs
					);
					shopPage = page.page;
					dirty = true;
					return;
				}
			}
			if ( event.kind === "scroll" && panel === FORTRESS_PRODUCTION_PANEL ) {
				fortressProductionHud.scroll( Math.sign( event.delta ), fortressProductionItems().length );
				dirty = true;
				return;
			}
			if ( event.kind === "scroll" && panel === "Party Matching" ) {
				partyMatchOffset = Math.max( 0, partyMatchOffset + Math.sign( event.delta ) );
				dirty = true;
				return;
			}
			if (
				partyProgressVisible && !view?.gameplay?.partyMatching?.request &&
				(event.kind === "key" || event.kind === "activate" || event.kind === "double-activate" ||
					event.kind === "right-activate" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "scroll" ||
					event.kind === "edit")
			) return;
			if ( stallHud.prompt() ) {
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( "stall-prompt-cancel" );
					else if ( event.code === "Enter" && !composing ) activate( "stall-prompt-ok" );
					return;
				}
				if (
					event.kind === "scroll" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "double-activate"
				) return;
				if (
					"id" in event && event.id !== null && !event.id.startsWith( "stall-prompt-" ) &&
					event.id !== "submit"
				) {
					return;
				}
			}
			if ( splitStack ) {
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( "split-cancel" );
					else if ( event.code === "Enter" && !composing ) activate( "split-confirm" );
					return;
				}
				if (
					event.kind === "scroll" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "double-activate"
				) return;
				if (
					"id" in event && event.id !== null &&
					![ "split-confirm", "split-cancel", "split-amount", "submit" ].includes( event.id )
				) return;
			}
			if ( goldDialog ) {
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( "gold-cancel" );
					else if ( event.code === "Enter" && !composing ) activate( "drop-gold" );
					return;
				}
				if (
					event.kind === "scroll" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "double-activate"
				) return;
				if (
					"id" in event && event.id !== null &&
					![ "drop-gold", "gold-cancel", "gold-amount", "submit" ].includes( event.id )
				) return;
			}
			if ( blockDialog ) {
				if ( event.kind === "key" ) {
					if ( event.code === "Escape" ) activate( "blocking-cancel" );
					else if ( event.code === "Enter" && !composing ) activate( "blocking-ok" );
					return;
				}
				if (
					event.kind === "scroll" || event.kind === "drag" || event.kind === "drag-end" ||
					event.kind === "double-activate"
				) return;
				if (
					"id" in event && event.id !== null &&
					![ "blocking-ok", "blocking-cancel", "blocking-name", "submit" ].includes( event.id )
				) return;
			}
			if ( event.kind === "drag" && event.id === "blocking-thumb" && panel === "Blocking" ) {
				const rows = blockTab === 0 ? view?.gameplay?.chat?.blocked ?? [] : localBlocks;
				blockOffset = Math.max(
					0,
					Math.min(
						Math.max( 0, rows.length - 10 ),
						blockOffset + event.dy * Math.max( 0, rows.length - 10 ) / 185
					)
				);
				dirty = true;
				return;
			}
			if ( event.kind === "scroll" && panel === "Blocking" ) {
				const area = controls.find( c => c.id === "blocking-list" );
				if ( area && containsPoint( area.rect, event.x, event.y ) ) {
					blockOffset = Math.max( 0, blockOffset + Math.sign( event.delta ) );
					dirty = true;
				}
				return;
			}

			// 50EBC0 owns history independently of console output. At either list
			// sentinel native leaves the editor unchanged; Down does not restore a draft.
			if (
				event.kind === "key" && focus === "gm-input" && consolePhase !== 0 && view?.gameplay?.eligibility?.gm &&
				(event.code === "ArrowUp" || event.code === "ArrowDown")
			) {
				if ( event.code === "ArrowUp" && consoleHistoryIndex >= 0 ) consoleHistoryIndex--;
				else if ( event.code === "ArrowDown" && consoleHistoryIndex < consoleHistory.length ) {
					consoleHistoryIndex++;
				}
				const line = consoleHistory[consoleHistoryIndex];
				if ( line !== undefined ) {
					consoleText = line;
					selection = [ line.length, line.length ];
					focusRequest = { id: "gm-input", revision: ++focusRevision, caret: line.length };
					dirty = true;
				}
				return;
			}
			if (
				event.kind === "key" && view?.session?.phase === "world" && view.gameplay?.eligibility?.gm &&
				((event.code === "Backquote" && event.shift) || (event.code === "Escape" && consolePhase !== 0))
			) {
				consolePhase = consolePhase === 0 || consolePhase === 2 ? 1 : 2;
				focus = consolePhase === 1 ? "gm-input" : null;
				focusRequest = { id: focus, revision: ++focusRevision, caret: consoleText.length };
				dirty = true;
				return;
			}
			if ( event.kind === "video-preferences" ) {
				video = videoOptions( event.value );
				if ( panel !== "Option" ) videoDraft = videoOptions( video );
				dirty = true;
				return;
			}
			if ( event.kind === "window-positions" ) {
				windowPlacement.load( event.value );
				return;
			}
			if ( event.kind === "quickslot-preferences" ) {
				const row = extendedQuickslotOptions( event.value );
				extOpen = row.open;
				extVertical = row.vertical;
				extDouble = row.double;
				extTransparent = row.transparent;
				extSlotLock = row.slotLock;
				extPositionLock = row.positionLock;
				dirty = true;
				return;
			}
			if ( event.kind === "input-preferences" ) {
				bindings = inputOptions( event.value );
				if ( panel !== "Option" ) bindingDraft = inputOptions( bindings );
				dirty = true;
				return;
			}
			if (
				event.kind === "key" && withdrawal.active() &&
				(withdrawal.confirming() || event.code === "Escape")
			) {
				if ( event.code === "Escape" ) {
					if ( withdrawal.confirming() ) withdrawal.select( "" );
					else activate( "withdrawal-close" );
					dirty = true;
				} else if ( event.code === "Enter" ) {
					activate( "withdrawal-confirm" );
				}
				return;
			}
			if ( event.kind === "key" && practice ) {
				if ( event.code === "Escape" ) practice = null;
				else if ( event.code === "Enter" ) activate( "skill-confirm-ok" );
				dirty = true;
				return;
			}
			if ( event.kind === "key" && confirmAbandon ) {
				if ( event.code === "Escape" ) confirmAbandon = false;
				else if ( event.code === "Enter" ) activate( "quest-abandon-yes" );
				dirty = true;
				return;
			}
			if ( event.kind === "key" && questDetails && event.code === "Escape" ) {
				questDetails = false;
				dirty = true;
				return;
			}
			if ( event.kind === "key" && event.code === "Escape" && compositeItemHud.packageId() !== null ) {
				// 69F450: Escape closes the package window.
				compositeItemHud.close();
				dirty = true;
				return;
			}
			if ( questDetails && event.kind === "drag" && event.id === "quest-detail-drag" && view ) {
				questPosition = [
					Math.max( 0, Math.min( Math.max( 0, view.width - 376 ), questPosition[0] + event.dx ) ),
					Math.max( 0, Math.min( Math.max( 0, view.height - 384 ), questPosition[1] + event.dy ) )
				];
				dirty = true;
				return;
			}
			if ( questDetails && event.kind === "drag" && event.id === "quest-detail-thumb" ) {
				questDetailScroll = Math.max(
					0,
					Math.min( questDetailMax, questDetailScroll + event.dy * questDetailMax / 206 )
				);
				dirty = true;
				return;
			}
			if (
				questDetails && event.kind === "scroll" &&
				controls.some( c => c.id === "quest-detail-drag" && containsPoint( c.rect, event.x, event.y ) )
			) {
				questDetailScroll = Math.max(
					0,
					Math.min( questDetailMax, questDetailScroll + Math.sign( event.delta ) * (text.height() + 5) )
				);
				dirty = true;
				return;
			}
			if ( event.kind === "key" && panel === "Option" && optionTab === 3 && bindingSelected >= 0 ) {
				if ( event.code === "Escape" ) {
					bindingSelected = -1;
					dirty = true;
					return;
				}
				bindingDraft = captureBinding( bindingDraft, bindingSelected, virtualKey( event.code ) );
				dirty = true;
				return;
			}
			if ( event.kind === "focus" && bindingSelected >= 0 && event.id !== "option-bind:" + bindingSelected ) {
				bindingSelected = -1;
			}
			if ( event.kind === "camera-preferences" ) {
				sight = sightMode( event.value );
				if ( panel !== "Option" ) sightDraft = sight;
				dirty = true;
				return;
			}
			if ( event.kind === "audio-preferences" ) {
				audioSaved = audioOptions( event.value );
				if ( panel !== "Option" ) audioDraft = { ...audioSaved };
				dirty = true;
				return;
			}
			if ( event.kind === "experimental-preferences" ) {
				experimental.restore( event.value );
				dirty = true;
				return;
			}
			if ( event.kind === "preferences" ) {
				const previous = options;
				options = gameOptions( event.value );
				if ( panel === "Option" ) {
					// Rebase untouched checkboxes on acknowledged preferences. A delayed
					// platform update must not turn an unrelated OK into a stale overwrite.
					const draft = { ...optionDraft };
					for ( const [key] of gameOptionRows() ) {
						if (
							draft[key] === previous[key] || (key === "windowMode" && previous[key] !== options[key])
						) draft[key] = options[key];
					}
					optionDraft = draft;
				} else optionDraft = options;
				dirty = true;
				return;
			}
			if ( event.kind === "drag" && event.id === "ext-drag" && view && !extPositionLock ) {
				const origin = extPosition ?? [ Math.max( 0, view.width - 106 ), 181 ];
				extPosition = [
					Math.max( 0, Math.min( view.width - 44, origin[0] + event.dx ) ),
					Math.max( 0, Math.min( view.height - 44, origin[1] + event.dy ) )
				];
				dirty = true;
				return;
			}
			if (
				event.kind === "drag" &&
				(event.id.startsWith( "skill:" ) || event.id.startsWith( "hotbar:" ) ||
					event.id.startsWith( "action:" ))
			) {
				// A carried shortcut keeps following the pointer after its own slot
				// leaves the screen: F1-F4 switch the bar mid-drag, and the drop
				// below moves it onto the other bar.
				const source = controls.find( c => c.id === event.id && c.draggable && !c.disabled );
				const prior = carriedShortcut?.id === event.id ?
					carriedShortcut :
					source ?
					{ id: event.id, x: source.rect[0] + source.rect[2] / 2, y: source.rect[1] + source.rect[3] / 2 } :
					null;
				if ( prior ) {
					carriedShortcut = { ...prior, x: prior.x + event.dx, y: prior.y + event.dy };
					dirty = true;
				}
				return;
			}
			// The shortcut this drag carried, whether or not its slot is still drawn.
			const carried = event.kind === "drag-end" && carriedShortcut?.id === event.id;
			if ( event.kind === "drag-end" ) carriedShortcut = null;
			if ( event.kind === "drag-end" && event.id === "ext-drag" ) {
				persistQuickslots();
				return;
			}
			// 57F430 button-up opens the town when released over the pressed icon, even
			// after the pointer moved. The bridge reports a moved press as drag-end and
			// suppresses its click, so a jittery click must be honoured here.
			if ( event.kind === "drag-end" && event.id.startsWith( "map-town:" ) ) {
				if ( panel === "Map" && topmostControlAt( controls, event.x, event.y )?.id === event.id ) {
					activate( event.id );
				}
				dirty = true;
				return;
			}
			if ( event.kind === "drag-end" && view?.gameplay ) {
				const target = topmostControlAt( controls, event.x, event.y );
				if ( target?.id.startsWith( "hotbar:" ) ) {
					const slot = Number( target.id.slice( 7 ) ),
						source = controls.find( c => c.id === event.id && c.draggable && !c.disabled );
					if (
						(source || carried) &&
						!(extSlotLock &&
							(slot >= 41 || event.id.startsWith( "hotbar:" ) && Number( event.id.slice( 7 ) ) >= 41))
					) {
						for ( const binding of quickSlotDrop( event.id, slot, view.gameplay ) ) {
							sendGameplay( { kind: "quickslot-set", binding } );
						}
					}
					carriedItem = null;
					dirty = true;
					return;
				}
				// 573100 -> 567E20 / 572E00: releasing a shortcut into the world
				// clears its binding. Slot-to-slot drops above own their swap; blocked
				// controls and locked extended slots must not become world drops.
				if (
					!target && !blocks.some( r => containsPoint( r, event.x, event.y ) ) && event.x >= 0 &&
					event.y >= 0 && event.x < view.width && event.y < view.height && event.id.startsWith( "hotbar:" )
				) {
					const slot = Number( event.id.slice( 7 ) ),
						source = controls.find( c => c.id === event.id && c.draggable && !c.disabled );
					if (
						(source || carried) && !(slot >= 41 && extSlotLock) &&
						view.gameplay.quickSlots?.some( row => row.slot === slot && row.kind !== 0 )
					) sendGameplay( { kind: "quickslot-set", binding: { slot, kind: 0, payload: 0 } } );
					dirty = true;
					return;
				}
			}
			if ( event.kind === "drag-end" ) {
				if (
					carriedItem?.source === event.id && !event.id.startsWith( "slot:" ) &&
					!event.id.startsWith( "avatar:" )
				) {
					carriedItem = null;
					dirty = true;
				}
				// CIFStall: an offer dragged back onto the bag leaves the stall.
				if ( event.id.startsWith( "stall-slot:" ) && view?.gameplay?.stall?.phase === "owner" ) {
					const target = topmostControlAt( controls, event.x, event.y ),
						slot = Number( event.id.slice( 11 ) ),
						stall = view.gameplay.stall;
					if (
						target?.id.startsWith( "slot:" ) && !stall.open && stall.offers.some( row => row.slot === slot )
					) sendGameplay( { kind: "stall-remove", slot } );
					dirty = true;
					return;
				}
				if (
					panel === "Storage" && event.id.startsWith( "storage-slot:" ) && view?.gameplay?.storage &&
					!view.gameplay.inventoryPending
				) {
					const target = topmostControlAt( controls, event.x, event.y ),
						source = Number( event.id.slice( 13 ) ),
						item = view.gameplay.storage.items.find( r => r.slot === source );
					if ( item && target && !target.disabled ) {
						if ( target.id.startsWith( "slot:" ) ) {
							sendGameplay( {
								kind: "storage-move",
								move: {
									type: STORAGE_MOVE_WITHDRAW,
									source,
									destination: Number( target.id.slice( 5 ) ),
									quantity: 0,
									gold: 0
								}
							} );
						} else if ( target.id.startsWith( "storage-slot:" ) && target.id !== event.id ) {
							sendGameplay( {
								kind: "storage-move",
								move: {
									type: STORAGE_MOVE_ROOM,
									source,
									destination: Number( target.id.slice( 13 ) ),
									quantity: item.quantity,
									gold: 0
								}
							} );
						}
					}
					dirty = true;
					return;
				}
				if (
					(panel === "COS inventory" || panel === "Shop" && view?.gameplay?.shop?.cosGid === cosGid) &&
					event.id.startsWith( "cos-slot:" ) && view?.gameplay &&
					!view.gameplay.inventoryPending
				) {
					const target = topmostControlAt( controls, event.x, event.y );
					const source = Number( event.id.slice( 9 ) ),
						record = view.gameplay.cosRecords?.find( r => r.gid === cosGid ),
						item = record?.inventory?.find( r => r.slot === source );
					if ( record && item && target && !target.disabled ) {
						if ( panel === "Shop" && target.id.startsWith( "shop-" ) ) openShopSale( item );
						else if ( target.id.startsWith( "slot:" ) ) {
							sendGameplay( {
								kind: "cos-transfer",
								gid: cosGid,
								toCos: false,
								source,
								destination: Number( target.id.slice( 5 ) )
							} );
						} else if ( target.id.startsWith( "cos-slot:" ) && target.id !== event.id ) {
							sendGameplay( {
								kind: "cos-inventory-move",
								gid: cosGid,
								source,
								destination: Number( target.id.slice( 9 ) ),
								quantity: item.quantity
							} );
						}
					}
					dirty = true;
					return;
				}
				const carried = carriedItem;
				carriedItem = null;
				dirty = true;
				if (
					carried &&
					(carried.avatar ? event.id.startsWith( "avatar:" ) : event.id === "slot:" + carried.slot) &&
					[ "Inventory", "Shop", "Alchemy", GRANT_PANEL, "COS inventory", "Storage" ].includes( panel ) &&
					view?.session?.phase === "world" && !view.gameplay?.inventoryPending && controls.some( c =>
						c.id === event.id && c.draggable && !c.disabled
					)
				) {
					const target = topmostControlAt( controls, event.x, event.y );
					const item = (carried.avatar ? view?.gameplay?.avatarInventory : view?.gameplay?.inventory)?.find(
						i => i.slot === carried.slot
					);
					if ( carried.avatar ) {
						if ( target?.id.startsWith( "slot:" ) ) {
							moveAvatar( false, carried.slot, Number( target.id.slice( 5 ) ) );
						}
						return;
					}
					// CIFExchange: a bag item dropped on the own side goes on the table.
					if ( item && target && !target.disabled && target.id.startsWith( "exchange-my:" ) ) {
						sendGameplay( { kind: "exchange-put", slot: item.slot } );
						return;
					}
					// CIFStall_OnInventorySlotDrop 5A11C0: a bag item dropped on an
					// empty cell of the own stall asks its count and price (5A1A40).
					const stall = view?.gameplay?.stall;
					if ( item && target && !target.disabled && target.id.startsWith( "stall-slot:" ) ) {
						const slot = Number( target.id.slice( 11 ) );
						if (
							stall?.phase === "owner" && !stall.open && !stall.offers.some( row => row.slot === slot )
						) {
							openStallPrompt( {
								kind: "price",
								slot,
								bagSlot: item.slot,
								carried: item.quantity,
								quantity: String( item.quantity ),
								price: "",
								modify: false
							} );
						}
						return;
					}
					if (
						item && target && !target.disabled && panel === "Alchemy" && target.id.startsWith( "alchemy-" )
					) {
						activate( "alchemy-slot:" + item.slot );
						return;
					}
					// 6EB570: an inventory item dropped on the grant window's slot.
					if ( item && target?.id === "magic-option-slot" && !target.disabled && panel === GRANT_PANEL ) {
						sendGameplay( { kind: "magic-option-take", slot: item.slot } );
						return;
					}
					const room = view?.gameplay?.storage;
					if (
						item && target && !target.disabled && panel === "Storage" && room?.phase === "open" &&
						target.id.startsWith( "storage-slot:" )
					) {
						// 5B0BF0: an occupied target lands on the page's first free slot.
						const slot = Number( target.id.slice( 13 ) ),
							start = slot - slot % STORAGE_PAGE_SLOTS,
							destination = room.items.some( r => r.slot === slot ) ?
								firstFreeSlot(
									room.items,
									start,
									Math.min( room.capacity, start + STORAGE_PAGE_SLOTS )
								) :
								slot;
						if ( destination !== null ) {
							sendGameplay( {
								kind: "storage-move",
								move: {
									type: STORAGE_MOVE_DEPOSIT,
									source: item.slot,
									destination,
									quantity: 0,
									gold: 0
								}
							} );
						}
						return;
					}
					if (
						item && target && !target.disabled && panel === "COS inventory" &&
						target.id.startsWith( "cos-slot:" )
					) {
						sendGameplay( {
							kind: "cos-transfer",
							gid: cosGid,
							toCos: true,
							source: item.slot,
							destination: Number( target.id.slice( 9 ) )
						} );
						return;
					}
					if ( item && target && panel === "Shop" && target.id.startsWith( "shop-" ) ) {
						openShopSale( item );
						return;
					}
					if ( target?.id.startsWith( "avatar:" ) ) {
						moveAvatar( true, carried.slot );
						return;
					}
					const game = view.gameplay!, equipmentEnd = game.equipmentSlotCount ?? 13;
					if (
						item && item.slot >= equipmentEnd && target && !target.disabled &&
						(target.id === "equipment-drop-zone" ||
							target.id.startsWith( "slot:" ) && Number( target.id.slice( 5 ) ) < equipmentEnd)
					) {
						const destination = equipmentDropSlot(
							item.typeFlags,
							game.inventory,
							target.id.startsWith( "slot:" ) ? Number( target.id.slice( 5 ) ) : undefined
						);
						if ( destination !== undefined ) {
							sendGameplay( {
								kind: "inventory-move",
								source: item.slot,
								destination,
								quantity: item.quantity
							} );
						}
						dirty = true;
						return;
					}
					if (
						item && !target && !blocks.some( r => containsPoint( r, event.x, event.y ) ) && event.x >= 0 &&
						event.y >= 0 && event.x < view.width && event.y < view.height
					) {
						if ( item.slot >= equipmentEnd ) {
							groundDrop = { slot: item.slot, refObjId: item.refObjId };
							for ( const key of [ "UIIT_MSG_DROP_WARNING_1", "UIIT_MSG_DROP_WARNING_2" ] ) {
								hudMessages.append( hudCopy( key ) );
							}
						} else {hudMessages.append(
								hudCopy( "UIIT_MSG_STRGERR_CANT_DROP_EQUIPED_ITEM_DIRECTLY" )
							);}
						dirty = true;
						return;
					}
					if (
						item && target?.id.startsWith( "slot:" ) && !target.disabled &&
						Number( target.id.slice( 5 ) ) !== carried.slot
					) {
						sendGameplay( {
							kind: "inventory-move",
							source: carried.slot,
							destination: Number( target.id.slice( 5 ) ),
							quantity: item.quantity
						} );
					}
				}
				return;
			}
			if ( event.kind === "double-activate" && (event.ctrl || event.shift || event.alt) ) return;
			if ( event.kind === "double-activate" && view?.gameplay && !view.gameplay.inventoryPending ) {
				if ( panel === "Shop" && event.id.startsWith( view.gameplay.shop?.cosGid ? "cos-slot:" : "slot:" ) ) {
					const item = merchantRows( view.gameplay ).find( r =>
						r.slot === Number( event.id.slice( event.id.indexOf( ":" ) + 1 ) )
					);
					if ( item ) {
						openShopSale( item );
						dirty = true;
					}
					return;
				}
				if ( panel === "Alchemy" && event.id.startsWith( "slot:" ) ) {
					activate( "alchemy-slot:" + event.id.slice( 5 ) );
					dirty = true;
					return;
				}
				if ( panel === GRANT_PANEL && event.id.startsWith( "slot:" ) ) {
					sendGameplay( { kind: "magic-option-take", slot: Number( event.id.slice( 5 ) ) } );
					dirty = true;
					return;
				}
				if (
					panel === "COS inventory" && (event.id.startsWith( "slot:" ) || event.id.startsWith( "cos-slot:" ))
				) {
					const to = event.id.startsWith( "slot:" );
					if ( to ) {
						inventorySlot = Number( event.id.slice( 5 ) );
						cosSlot = -1;
					} else {
						cosSlot = Number( event.id.slice( 9 ) );
						inventorySlot = -1;
					}
					activate( to ? "to-cos" : "from-cos" );
					return;
				}
			}
			if (
				event.kind === "drag" &&
				[ "war-scroll-thumb", "war-members-thumb", "war-combo-thumb" ].includes( event.id ) &&
				controls.some( c => c.id === event.id )
			) {
				const state = guildWarHud.state(), social = view?.gameplay?.social;
				const target = event.id === "war-combo-thumb" ?
					"combo" :
					event.id === "war-members-thumb" ?
					"members" :
					"enemies";
				const count = target === "combo" ?
					[ 8, 32, 25, 7 ][state.combo - 23] ?? 0 :
					target === "members" ?
					social?.guild?.members.length ?? 0 :
					social?.wars?.length ?? 0;
				const visible = target === "combo" ?
					(state.combo === 23 ? 8 : 7) :
					target === "members" ?
					3 :
					guildTab === 2 ?
					6 :
					9;
				const travel = target === "combo" ?
					visible * 18 - 48 :
					target === "members" ?
					24 :
					guildTab === 2 ?
					93 :
					159;
				guildWarHud.scroll( target, event.dy * Math.max( 0, count - visible ) / travel, count, visible );
				dirty = true;
				return;
			}

			if (
				event.kind === "drag" && event.id === "potion-combo-thumb" && panel === "Auto Potion" && potionCombo &&
				controls.some( c => c.id === event.id && !c.disabled )
			) {
				potionComboOffset = Math.max( 0, Math.min( 6, potionComboOffset + event.dy / 4 ) );
				dirty = true;
				return;
			}
			if ( event.kind === "drag" && event.id === "option-video-thumb" && panel === "Option" && optionTab === 0 ) {
				videoScroll = Math.max(
					0,
					Math.min( VIDEO_SCROLL_MAX, videoScroll + event.dy * VIDEO_SCROLL_MAX / 129 )
				);
				videoCombo = VIDEO_COMBO_CLOSED;
				dirty = true;
				return;
			}
			if ( event.kind === "drag" && event.id === "option-input-thumb" && panel === "Option" && optionTab === 3 ) {
				bindingScroll = Math.max( 0, Math.min( 12, bindingScroll + event.dy * 12 / 101 ) );
				dirty = true;
				return;
			}
			if (
				event.kind === "drag" && event.id.startsWith( "option-scroll:" ) && event.id.endsWith( "-thumb" ) &&
				panel === "Option" && optionTab === 4
			) {
				const group = Number( event.id.slice( 14 ).split( "-" )[0] );
				if ( group === 0 || group === 1 ) {
					const manager = hud.data()?.windows.ifoption_game?.["GDR_GAME_OPTION_SCROLLMANAGER_" + (group + 1)];
					if ( manager ) {
						const range = group === 0 ? 3 : 4;
						optionScroll[group] = Math.max(
							0,
							Math.min(
								range,
								optionScroll[group]! +
									event.dy * range / Math.max( 1, optionListTrack( manager.rect, 5, 6 )[3] )
							)
						);
						dirty = true;
					}
				}
				return;
			}
			if (
				event.kind === "drag" && ITEM_SLOT_PREFIXES.some( prefix => event.id.startsWith( prefix ) ) &&
				[ "Inventory", "Shop", "Alchemy", GRANT_PANEL, "COS inventory", "Storage" ].includes( panel )
			) {
				const node = controls.find( c => c.id === event.id && !c.disabled && c.draggable );
				if ( !node ) return;
				const avatar = event.id.startsWith( "avatar:" ),
					slot = avatar ?
						view?.gameplay?.avatarInventory?.find( i =>
							i.typeFlags >>> 11 === Number( event.id.slice( 7 ) )
						)?.slot :
						Number( event.id.slice( event.id.indexOf( ":" ) + 1 ) );
				if ( slot === undefined ) return;
				// Pointer carry replaces click selection; cancel/drop must not leave a second source armed.
				inventorySlot = -1;
				confirmDrop = "";
				const prior = carriedItem?.source === event.id ?
					carriedItem :
					{ source: event.id, slot, avatar, x: node.rect[0] + 16, y: node.rect[1] + 16 };
				carriedItem = { ...prior, x: prior.x + event.dx, y: prior.y + event.dy };
				dirty = true;
				return;
			}

			// Retired controls must not keep a captured drag alive behind a modal.
			if (
				event.kind === "drag" && (event.id === "npc-drag" || event.id.startsWith( "window-drag:" )) &&
				windowPlacement.drag( event.id, event.dx, event.dy )
			) {
				dirty = true;
				return;
			}
			// Only the published NPC window receives input; modal guards run first.
			if ( controls.some( c => c.id === "npc-close" ) ) {
				if ( (event.kind === "drag" || event.kind === "scroll") && npcPanel.event( event ) ) {
					dirty = true;
					return;
				}
				if ( event.kind === "key" && event.code === "Escape" ) {
					activate( "npc-close" );
					return;
				}
			}
			if ( event.kind === "drag" && event.id === "resurrection-drag" && view ) {
				const mutation = !!view.gameplay?.social?.resurrection?.mutation,
					layout = resurrectionLayout( view.width, view.height, resurrectionPrompt.position(), mutation ),
					next = resurrectionLayout( view.width, view.height, [
						layout.frame[0] + event.dx,
						layout.frame[1] + event.dy
					], mutation );
				resurrectionPrompt.place( [ next.frame[0], next.frame[1] ] );
				dirty = true;
				return;
			}
			if ( event.kind === "drag" && event.id === "invite-drag" && view ) {
				const type = view.gameplay?.social?.invitation?.type,
					layout = proposalLayout( type, view.width, view.height, undefined, invitePosition ),
					position: readonly [number, number] = [ layout.frame[0] + event.dx, layout.frame[1] + event.dy ],
					next = proposalLayout( type, view.width, view.height, undefined, position );
				invitePosition = [ next.frame[0], next.frame[1] ];
				dirty = true;
				return;
			}
			if ( event.kind === "drag" && event.id === "disconnect-drag" && view ) {
				const layout = disconnectDialog( view.width, view.height, disconnectPosition ),
					next = disconnectDialog( view.width, view.height, [
						layout.frame[0] + event.dx,
						layout.frame[1] + event.dy
					] );
				disconnectPosition = [ next.frame[0], next.frame[1] ];
				dirty = true;
				return;
			}
			if (
				event.kind === "drag" && event.id === "rebirth-drag" && rebirthDue && !deathDismissed &&
				view?.session?.phase === "world"
			) {
				const layout = rebirthDialog( view.width, view.height, deathPosition ),
					next = rebirthDialog( view.width, view.height, [
						layout.frame[0] + event.dx,
						layout.frame[1] + event.dy
					] );
				deathPosition = [ next.frame[0], next.frame[1] ];
				dirty = true;
				return;
			}
			if (
				event.kind === "drag" && event.id === "main-popup-drag" &&
				[
					"Character",
					"Inventory",
					"Party",
					"Skills",
					"Quests",
					"Actions",
					"Academy",
					"Shop",
					"Alchemy",
					"COS inventory"
				].includes( panel ) && view
			) {
				const x = popupPosition?.[0] ?? Math.max( 42, view.width - 388 ),
					y = popupPosition?.[1] ?? Math.max( 0, view.height - 478 );
				const [px, py] = mainPopupFrame( view.width, view.height, [ x + event.dx, y + event.dy ] );
				popupPosition = [ px, py ];
				dirty = true;
				return;
			}
			if ( event.kind === "drag" && event.id === "system-drag" && panel === "System" ) {
				systemX = Math.max( 0, Math.min( (view?.width ?? 214) - 214, (systemX ?? 0) + event.dx ) );
				systemY = Math.max(
					0,
					Math.min( (view?.height ?? SYSTEM_MENU_HEIGHT) - SYSTEM_MENU_HEIGHT, (systemY ?? 0) + event.dy )
				);
				dirty = true;
				return;
			}
			if ( view?.session?.phase === "disconnected" ) {
				if ( event.kind === "activate" && event.id === "disconnect-confirm" ) activate( event.id );
				else if ( event.kind === "key" && event.code === "Enter" ) activate( "disconnect-confirm" );
				else if ( event.kind === "hover" ) {
					hover = event.id === "disconnect-confirm" ? event.id : null;
					dirty = true;
				} else if ( event.kind === "press" ) {
					pressed = event.id === "disconnect-confirm" ? event.id : null;
					dirty = true;
				} else if ( event.kind === "focus" ) {
					focus = event.id === "disconnect-confirm" ? event.id : null;
					dirty = true;
				}
				return;
			}
			// A proposal box (an invitation or the resurrection question) is modal.
			const proposalOpen = controls.some( c => c.id === "invite-drag" || c.id === "resurrection-drag" );
			if ( (event.kind === "drag" || event.kind === "scroll") && proposalOpen ) {
				return;
			}
			if (
				view?.session?.phase === "world" && (event.kind === "drag" || event.kind === "scroll" && !panel) &&
				((controls.some( c => c.id === "chat-text" ) && chatScroll.event( event )) ||
					(controls.some( c => c.id === "status-panel" ) && statusScroll.event( event )))
			) {
				dirty = true;
				return;
			}
			if (
				event.kind === "drag" &&
				(event.id === "skill-scroll-thumb" && panel === "Skills" ||
					event.id === "quest-scroll-thumb" && panel === "Quests")
			) {
				const prefix = panel === "Skills" ? "skill-scroll" : "quest-scroll",
					up = controls.find( c => c.id === prefix + "-up" ),
					down = controls.find( c => c.id === prefix + "-down" );
				if ( up && down ) {
					const travel = Math.max( 1, down.rect[1] - up.rect[1] - 32 ),
						range = panel === "Skills" ?
							Math.max(
								0,
								(hud.data()?.skillUi.groups.filter( g => g.mastery === selectedMastery ).length ?? 0) -
									4
							) :
							Math.max(
								0,
								(view?.gameplay?.quests ?? []).reduce(
									( n, q ) => n + 1 + (expandedQuests.has( q.refId ) ? q.contents.length : 0),
									0
								) - 12
							);
					if ( panel === "Skills" ) {
						skillScroll = Math.max( 0, Math.min( range, skillScroll + event.dy * range / travel ) );
					} else questPage = Math.max( 0, Math.min( range, questPage + event.dy * range / travel ) );
				}
				dirty = true;
				return;
			}
			if ( event.kind === "scroll" && (panel === "Skills" || panel === "Quests") ) {
				const prefix = panel === "Skills" ? "skill-scroll" : "quest-scroll",
					up = controls.find( c => c.id === prefix + "-up" ),
					down = controls.find( c => c.id === prefix + "-down" );
				if (
					up && down && event.x >= up.rect[0] - 342 && event.x <= up.rect[0] + 16 && event.y >= up.rect[1] &&
					event.y <= down.rect[1] + 16
				) {
					if ( panel === "Skills" ) skillScroll = Math.max( 0, skillScroll + Math.sign( event.delta ) );
					else questPage = Math.max( 0, questPage + Math.sign( event.delta ) );
					dirty = true;
				}
				return;
			}
			if ( event.kind === "drag" ) {
				if ( panel === "Map" ) {
					if ( event.id === "map-drag" ) {
						mapX = Math.max( 0, mapX + event.dx );
						mapY = Math.max( 0, mapY + event.dy );
					} else if ( (event.id === "map-pan" || event.id.startsWith( "map-town:" )) && !mapFollow ) {
						mapPan = [ mapPan[0] + event.dx, mapPan[1] + event.dy ];
					}
					dirty = true;
					return;
				}
				if ( panel !== "Game Guide" ) return;
				if ( event.id === "guide-index-thumb" ) {
					guideIndexScroll = Math.max(
						0,
						Math.min( guideIndexMax, guideIndexScroll + event.dy * guideIndexMax / 282 )
					);
					dirty = true;
					return;
				}
				if ( event.id === "guide-thumb" && guideThumbTravel > 0 ) {
					guideScroll = Math.max(
						0,
						Math.min( guideScrollMax, guideScroll + event.dy * guideScrollMax / guideThumbTravel )
					);
					dirty = true;
					return;
				}
				if ( event.id === "guide-drag" && panel === "Game Guide" ) {
					guideX = Math.max(
						0,
						Math.min(
							(view?.width ?? 420) - 420,
							(guideX ?? Math.floor( ((view?.width ?? 420) - 420) / 2 )) + event.dx
						)
					);
					guideY = Math.max(
						0,
						Math.min(
							(view?.height ?? 452) - 452,
							(guideY ?? Math.floor( ((view?.height ?? 452) - 452) / 2 )) + event.dy
						)
					);
					dirty = true;
				}
				return;
			}
			if ( event.kind === "scroll" && panel === "Option" && (optionTab === 0 || optionTab === 3) ) {
				const page = hud.data()?.windows[optionTab === 0 ? "ifoption_video" : "ifoption_input"],
					node = page && Object.values( page ).find( n => n.type === "CIFScrollManager" );
				if ( node ) {
					const x = optionOrigin()[0] + 11 + node.rect[0], y = optionOrigin()[1] + 62 + node.rect[1];
					if ( event.x >= x && event.x < x + node.rect[2] && event.y >= y && event.y < y + node.rect[3] ) {
						if ( optionTab === 0 ) {
							videoScroll = Math.max(
								0,
								Math.min( VIDEO_SCROLL_MAX, videoScroll + Math.sign( event.delta ) )
							);
							videoCombo = VIDEO_COMBO_CLOSED;
						} else bindingScroll = Math.max( 0, Math.min( 12, bindingScroll + Math.sign( event.delta ) ) );
						dirty = true;
					}
				}
				return;
			}
			if ( event.kind === "scroll" ) {
				if ( panel === "Auto Potion" && potionCombo ) {
					const rows = controls.filter( c => c.id.startsWith( "potion-choice:" ) );
					if (
						rows.some( c =>
							event.x >= c.rect[0] && event.x < c.rect[0] + c.rect[2] + 16 && event.y >= c.rect[1] &&
							event.y < c.rect[1] + c.rect[3]
						)
					) {
						potionComboOffset = Math.max( 0, Math.min( 6, potionComboOffset + Math.sign( event.delta ) ) );
						dirty = true;
					}
					return;
				}
				if ( panel === "Option" && optionTab === 4 ) {
					const px = optionOrigin()[0] + 11, py = optionOrigin()[1] + 62;
					for ( const group of [ 0, 1 ] ) {
						const node = hud.data()?.windows.ifoption_game
							?.["GDR_GAME_OPTION_SCROLLMANAGER_" + (group + 1)];
						if (
							node && event.x >= px + node.rect[0] && event.x < px + node.rect[0] + node.rect[2] &&
							event.y >= py + node.rect[1] && event.y < py + node.rect[1] + node.rect[3]
						) {
							optionScroll[group] = Math.max(
								0,
								Math.min( group === 0 ? 3 : 4, optionScroll[group]! + Math.sign( event.delta ) )
							);
							dirty = true;
						}
					}
					return;
				}
				if ( panel === "Game Guide" ) {
					if (
						guideSidebar && event.x >= (guideX ?? Math.floor( ((view?.width ?? 420) - 420) / 2 )) - 206 &&
						event.x < (guideX ?? Math.floor( ((view?.width ?? 420) - 420) / 2 )) &&
						event.y >= (guideY ?? Math.floor( ((view?.height ?? 452) - 452) / 2 )) + 45 &&
						event.y < (guideY ?? Math.floor( ((view?.height ?? 452) - 452) / 2 )) + 435
					) {
						guideIndexScroll = Math.max( 0, guideIndexScroll + Math.sign( event.delta ) );
						dirty = true;
					} else if (
						event.x >= (guideX ?? Math.floor( ((view?.width ?? 420) - 420) / 2 )) &&
						event.x < (guideX ?? Math.floor( ((view?.width ?? 420) - 420) / 2 )) + 420 &&
						event.y >= (guideY ?? Math.floor( ((view?.height ?? 452) - 452) / 2 )) + 35 &&
						event.y < (guideY ?? Math.floor( ((view?.height ?? 452) - 452) / 2 )) + 435
					) {
						guideScroll = Math.max(
							0,
							Math.min( guideScrollMax, guideScroll + Math.sign( event.delta ) * 30 )
						);
						dirty = true;
					}
				}
				return;
			}
			if ( proposalOpen ) {
				if ( event.kind === "key" || event.kind === "edit" || event.kind === "double-activate" ) return;
				if ( event.id !== null && !controls.some( c => c.id === event.id ) ) return;
			}
			if ( event.kind === "activate" ) {
				// 570120 / 567290: CTRL shop transaction takes priority over SHIFT/ALT.
				// A CTRL buy asks for one package when it holds several items, else
				// for the item's MaxStack (ItemData +0x1A8, the first ItemData column),
				// so equipment buys one. The quantity editor's purchaseLimit (6C0540)
				// is not a CTRL input.
				if (
					event.ctrl && panel === "Shop" && view?.gameplay?.shop &&
					(event.id.startsWith( "shop-offer:" ) ||
						event.id.startsWith( view.gameplay.shop.cosGid ? "cos-slot:" : "slot:" ))
				) {
					const game = view.gameplay, shop = game.shop!;
					if ( game.inventoryPending || shop.error || game.target !== shop.npc ) return;
					if ( event.id.startsWith( "shop-offer:" ) ) {
						const offer = shop.offers[Number( event.id.slice( 11 ) )];
						if ( offer ) {
							const choice = merchantSelection(
								"buy",
								Number( event.id.slice( 11 ) ),
								shop,
								merchantRows( game )
							);
							if ( choice ) {
								sendGameplay(
									merchantCommand(
										choice,
										(offer.contents?.length ?? 1) > 1 ? 1 : offer.maxStack
									)
								);
							}
						}
					} else {
						const item = merchantRows( game ).find( row =>
							row.slot === Number( event.id.slice( event.id.indexOf( ":" ) + 1 ) )
						);
						if ( item ) {
							// 567290: mall (ItemTid_IsMallItem) and rare (CSOItemData_IsRare)
							// items refuse a quick sell.
							if ( (item.typeFlags & 0x1f) === 0xd || itemIsRare( item ) || item.summon?.state === 2 ) {
								message = hud.data()?.strings["UIIT_MSG_STRGERR_CANT_QUICKSELL_CASHITEM"] ?? "";
								dirty = true;
								return;
							}
							openShopSale( item, true );
						}
					}
					carriedItem = null;
					inventorySlot = -1;
					dirty = true;
					return;
				}
				// Ctrl+click moves an item across the open warehouse as right-click
				// does: port-only, not native (owner decision 2026-10-10).
				if (
					event.ctrl && panel === "Storage" && view?.gameplay &&
					(event.id.startsWith( "slot:" ) || event.id.startsWith( "storage-slot:" ))
				) {
					const move = storageQuickMove( event.id, view.gameplay );
					if ( move ) sendGameplay( { kind: "storage-move", move } );
					carriedItem = null;
					inventorySlot = -1;
					dirty = true;
					return;
				}
				// Ctrl+click moves an item across the open pet bag as To Pet / From
				// Pet do with no slot chosen, so the worker picks the destination
				// (cosQuickDestination); the bag side is the inventory window's
				// slot: rows, as the Alt+click sibling reads them. Port-only, not
				// native: the warehouse rule extended to the pet (owner decision
				// 2026-10-10).
				if (
					event.ctrl && panel === "COS inventory" && view?.gameplay &&
					(event.id.startsWith( "cos-slot:" ) ||
						event.id.startsWith( "slot:" ) &&
							Number( event.id.slice( 5 ) ) >= (view.gameplay.equipmentSlotCount ?? 13))
				) {
					const toCos = event.id.startsWith( "slot:" ),
						slot = Number( event.id.slice( event.id.indexOf( ":" ) + 1 ) );
					inventorySlot = toCos ? slot : -1;
					cosSlot = toCos ? -1 : slot;
					activate( toCos ? "to-cos" : "from-cos" );
					carriedItem = null;
					inventorySlot = -1;
					cosSlot = -1;
					dirty = true;
					return;
				}
				// Ctrl+click puts a bag item into the open alchemy window as
				// double-click does: port-only, not native (owner decision 2026-10-10).
				if ( event.ctrl && panel === "Alchemy" && event.id.startsWith( "slot:" ) ) {
					activate( "alchemy-slot:" + event.id.slice( 5 ) );
					carriedItem = null;
					inventorySlot = -1;
					dirty = true;
					return;
				}
				if (
					event.shift && event.id.startsWith( "slot:" ) &&
					[ "Inventory", "Shop", "COS inventory", "Storage" ].includes( panel )
				) {
					const game = view?.gameplay,
						item = game?.inventory.find( i => i.slot === Number( event.id.slice( 5 ) ) );
					// 567A83 -> 59C050: Shift-click etc stacks; never split timed services.
					if (
						item && !game?.inventoryPending && item.slot >= (game?.equipmentSlotCount ?? 13) &&
						(item.typeFlags & 0x7e) === 0x6c && item.quantity > 1 &&
						!((item.typeFlags & 0xfffe) === 0x7eec && ((item.tooltip?.fields.itemParam6_2b0 ?? 0) & 2))
					) {
						splitStack = { slot: item.slot, refObjId: item.refObjId, quantity: item.quantity };
						splitAmount = "1";
						carriedItem = null;
						inventorySlot = -1;
						focusAndSelect( "split-amount", 0, 1 );
						dirty = true;
					}
					return;
				}
				if ( event.alt && (event.id.startsWith( "slot:" ) || event.id.startsWith( "cos-slot:" )) ) {
					if ( panel === "COS inventory" ) {
						if ( event.id.startsWith( "slot:" ) ) {
							inventorySlot = Number( event.id.slice( 5 ) );
							cosSlot = -1;
							activate( "to-cos" );
						} else {
							cosSlot = Number( event.id.slice( 9 ) );
							inventorySlot = -1;
							activate( "from-cos" );
						}
						dirty = true;
					}
					return;
				}
				if ( !composing ) activate( event.id );
			} else if ( event.kind === "right-activate" ) {
				if (
					view?.session?.phase !== "world" || !view.gameplay ||
					!controls.some( c => c.id === event.id && c.rightActivate && !c.disabled )
				) return;
				// 564030 cancels an active carry before dispatching the pressed icon.
				if ( carriedItem || carriedShortcut || clearHotbar ) {
					carriedItem = null;
					carriedShortcut = null;
					clearHotbar = false;
					dirty = true;
					return;
				}
				// CIFCOSStatus_OnRightButtonRelease (6A9E40) selects the companion
				// and makes it the player's target.
				if ( event.id.startsWith( "cos-status:" ) ) {
					const gid = Number( event.id.slice( 11 ) );
					cosHud.select( gid );
					sendGameplay( { kind: "cos-select", gid } );
					sendGameplay( { kind: "select", gid } );
					dirty = true;
					return;
				}
				if ( event.id.startsWith( "action:" ) ) {
					executeAction( Number( event.id.slice( 7 ) ) );
					return;
				}
				if ( event.id.startsWith( "skill:" ) ) {
					executeSkill( Number( event.id.slice( 6 ) ) );
					return;
				}
				if ( event.id.startsWith( "hotbar:" ) ) {
					activate( event.id );
					return;
				}
				if (
					panel === "Storage" && (event.id.startsWith( "slot:" ) || event.id.startsWith( "storage-slot:" ))
				) {
					const move = storageQuickMove( event.id, view.gameplay );
					if ( move ) sendGameplay( { kind: "storage-move", move } );
					return;
				}
				if ( event.id.startsWith( "slot:" ) ) {
					const item = view.gameplay.inventory.find( row => row.slot === Number( event.id.slice( 5 ) ) );
					if ( item && (item.typeFlags & 0x7fe) === 0x6ac ) moveAvatar( true, item.slot );
					else if ( item && useInventorySlot( item.slot ) ) dirty = true;
					return;
				}
				if ( event.id.startsWith( "avatar:" ) ) {
					const item = view.gameplay.avatarInventory?.find( row =>
						row.typeFlags >>> 11 === Number( event.id.slice( 7 ) )
					);
					if ( item ) moveAvatar( false, item.slot );
					return;
				}
				const cancel = buffBoard( view.gameplay, view.simulationTimeMs ?? 0 ).find( row => row.id === event.id )
					?.cancel;
				if ( !cancel ) return;
				if ( cancel.mode === "confirm" ) {
					buffDismiss = { skillId: cancel.skillId, instance: cancel.instance };
					dirty = true;
				} else sendGameplay( { kind: "effect-cancel", skillId: cancel.skillId, token: cancel.token } );
			} else if ( event.kind === "double-activate" ) {
				if ( event.shift ) return;
				if ( event.id.startsWith( "avatar:" ) ) {
					const item = view?.gameplay?.avatarInventory?.find( i =>
						i.typeFlags >>> 11 === Number( event.id.slice( 7 ) )
					);
					if ( item ) moveAvatar( false, item.slot );
					return;
				}
				if ( event.id.startsWith( "action:" ) ) {
					executeAction( Number( event.id.slice( 7 ) ) );
					return;
				}
				if ( event.id.startsWith( "slot:" ) && view?.gameplay ) {
					const avatar = view.gameplay.inventory.find( i =>
						i.slot === Number( event.id.slice( 5 ) ) && (i.typeFlags & 0x7fe) === 0x6ac
					);
					if ( avatar ) {
						moveAvatar( true, avatar.slot );
						return;
					}
					if ( useInventorySlot( Number( event.id.slice( 5 ) ) ) ) dirty = true;
					return;
				}
				if ( !composing && serverList && event.id.startsWith( "server:" ) ) {
					activate( event.id );
					if ( !pending && servers.some( row => row.id === serverDraft && row.operating ) ) clickSound();
					acceptServer();
				}
			} else if ( event.kind === "hover" ) {
				hover = event.id;
				dirty = true;
			} else if ( event.kind === "press" ) {
				pressed = event.id;
				dirty = true;
			} else if ( event.kind === "focus" ) {
				focus = event.id;
				if ( !focus ) composing = false;
				dirty = true;
			} else if ( event.kind === "edit" ) {
				if ( event.id.startsWith( "party-search-" ) ) {
					const key = event.id.slice( 13 );
					if ( key === "name" ) partySearchDraft.name = event.value.slice( 0, 13 );
					else if ( key === "min" || key === "max" ) {
						partySearchDraft[key] = event.value.replace( /[^0-9]/g, "" ).slice( 0, 3 );
					}
				} else if ( event.id.startsWith( "party-form-" ) ) {
					const key = event.id.slice( 11 );
					if ( key === "title" ) partyForm.title = event.value.slice( 0, 50 );
					else if ( key === "min" || key === "max" ) {
						partyForm[key] = event.value.replace( /[^0-9]/g, "" ).slice( 0, 3 );
					}
				} else if ( event.id.startsWith( "option-audio:" ) && panel === "Option" ) {
					const key = event.id.slice( 13 ), n = Number( event.value );
					if (
						(key === "bgm" || key === "effects" || key === "environment") && Number.isInteger( n ) &&
						n >= 0 && n <= AUDIO_SLIDER_MAX
					) updateAudioDraft( { ...audioDraft, [key]: audioSliderLevel( n ) } );
				} else if ( event.id.startsWith( "potion-percent:" ) ) {
					const key = event.id.slice( 15 ), percent = Number( event.value );
					if (
						(key === "hp" || key === "mp") && autoPotionEntry( potionDraft[key] ).enabled &&
						Number.isInteger( percent ) && percent >= 1 && percent <= 100
					) {
						potionDraft = {
							...potionDraft,
							[key]: autoPotionWord( { ...autoPotionEntry( potionDraft[key] ), percent } )
						};
					}
				} else if ( event.id === "blocking-name" && blockDialog === "add" ) {
					blockInput = event.value.slice( 0, 13 );
				} else if ( event.id === "academy-name" ) academyName = event.value;
				else if ( event.id === "alchemy-quantity" ) alchemyQuantity = event.value;
				else if ( event.id === FORTRESS_TAX_RATE && panel === FORTRESS_TAX_PANEL ) {
					// The slider's position is the ratio plus 20 (664C00).
					fortressTaxHud.slide( Number( event.value ) + FORTRESS_TAX_MIN );
				} else if ( event.id === "shop-quantity" ) {
					const game = view?.gameplay;
					const quote = merchantQuote(
						shopChoice,
						game?.shop,
						merchantRows( game ),
						event.value,
						game?.progression?.gold
					);
					// 6C0540 installs the offer limit; 521A85..521AD1 replaces an
					// oversized numeric draft with that limit before notifying the
					// dialog. Keep the displayed amount and the submitted quote equal.
					const digits = event.value.replace( /[^0-9]/g, "" );
					if ( quote ) shopQuantity = digits ? String( Math.min( quote.maximum, Number( digits ) ) ) : "";
				} else if ( event.id === "split-amount" && splitStack ) {
					const raw = event.value.replace( /[^0-9]/g, "" ).slice( 0, 5 );
					splitAmount = raw ? String( Math.min( splitStack.quantity - 1, Number( raw ) ) ) : "";
				} else if ( event.id === "gold-amount" ) {
					// A withdrawal draws on the warehouse, so its draft clamps to the
					// room's gold; the drop and deposit spend the bag's
					// (BR-261010-1655-2B68: an empty bag zeroed every withdrawal).
					const raw = event.value.replace( /[^0-9]/g, "" ).slice( 0, 20 ),
						limit = BigInt(
							(goldDialog === "withdraw" ?
								view?.gameplay?.storage?.gold :
								view?.gameplay?.progression?.gold) ?? 0
						);
					goldAmount = raw ? String( BigInt( raw ) > limit ? limit : BigInt( raw ) ) : "";
					confirmDrop = "";
				} else if ( event.id === "social-name" ) socialName = event.value;
				else if ( event.id === "social-subject" ) socialSubject = event.value;
				else if ( event.id === "social-contents" ) socialContents = event.value;
				else if ( event.id === "social-amount" ) socialAmount = event.value;
				else if ( event.id === STALL_CHAT_TEXT ) stallHud.typeChat( event.value );
				else if ( event.id === STALL_PROMPT_TEXT ) stallHud.type( "text", event.value );
				else if ( event.id === "war-name" || event.id === "war-money" ) {
					guildWarHud.type( event.id, event.value, Number( view?.gameplay?.progression?.gold ?? 0 ) );
				} else if ( event.id === STALL_PROMPT_QUANTITY ) stallHud.type( "quantity", event.value );
				else if ( event.id === STALL_PROMPT_PRICE ) stallHud.type( "price", event.value );
				else if ( event.id === "exchange-gold" ) {
					exchangeHud.type( event.value, Number( view?.gameplay?.progression?.gold ?? 0 ) );
				} else if ( event.id === "gm-input" ) consoleText = event.value;
				else if ( event.id === "chat-text" ) chatText = event.value.slice( 0, 100 );
				else if ( event.id === "chat-target" ) chatTarget = event.value;
				else if ( event.id === "account" ) account = event.value;
				else if ( event.id === "password" ) password = event.value;
				else if ( event.id === "endpoint" ) {
					endpoint = event.value;
					servers = [];
					selectedServer = "";
					serversRequested = false;
				}
				selection = [ event.start, event.end ];
				composing = event.composing;
				dirty = true;
			} else if ( event.kind === "key" ) {
				if ( event.code === "Escape" ) {
					if ( goldDialog ) {
						goldDialog = false;
						focus = null;
						dirty = true;
						return;
					}
					confirmDrop = "";
					closeServer();
					confirmAbandon = false;
					setPanel( panel ? "" : view?.session?.phase === "world" ? "System" : "" );
					inventorySlot = -1;
					dirty = true;
				} else if (
					view?.session?.phase === "world" && (!focus || !controls.some( c =>
						c.id === focus &&
						(c.kind === "text" || c.kind === "password" || c.kind === "range" || c.captureKeys)
					)) && !composing
				) {
					const vk = virtualKey( event.code ), binding = vk ? bindings.keys.indexOf( vk ) : -1;
					if ( binding < 0 && event.code === "F9" ) {
						sight = sightMode( (sight + 1) % 3 );
						saveSight( sight );
						dirty = true;
						return;
					}
					// The underbar button's own tooltip, UIIT_STT_SILKMALL_SHORT_KEY,
					// reads "Item Mall(F10)". The native dispatch of that system key was
					// not located (it is not in Game_OnKeyDown or the binding table), so
					// F10 opens the mall exactly as the button does (ItemMallEvent_OpenItemMall).
					if ( binding < 0 && event.code === "F10" ) {
						activate( "item-mall" );
						return;
					}
					if ( binding < 0 && event.code === "Enter" ) {
						focusAtEnd( "chat-text", chatText );
						dirty = true;
						return;
					}
					if ( binding === 8 ) {
						activate( "berserk" );
						return;
					}
					if ( binding === 12 ) {
						sendGameplay( { kind: "action-command", id: 1000 } );
						return;
					}
					if ( binding === 16 || binding === 17 ) {
						const record = view.gameplay?.cosRecords?.find( r =>
							r.gid === cosGid && (binding === 16 || !r.dead && r.hp > 0) &&
							(r.band === 3 || r.band === 4)
						) ?? view.gameplay?.cosRecords?.find( r =>
							(binding === 16 || !r.dead && r.hp > 0) && (r.band === 3 || r.band === 4)
						);
						if ( record ) {
							sendGameplay( { kind: binding === 16 ? "cos-cancel" : "cos-follow", gid: record.gid } );
						}
						return;
					}
					if ( binding === 15 ) {
						const game = view.gameplay;
						const local = view.entities.find( entity => entity.gid === game?.localGid );
						const record = game?.cosRecords?.find( r =>
							(r.band === 1 || r.band === 2) && !r.dead && r.hp > 0
						);
						if ( record && local ) {
							sendGameplay( {
								kind: "cos-ride",
								gid: record.gid,
								mounted: local.mountedOn !== record.gid
							} );
						}
						return;
					}
					if ( binding < 0 && /^F[1-4]$/.test( event.code ) ) {
						hotbarPage = Number( event.code.slice( 1 ) ) - 1;
						dirty = true;
						return;
					}
					if ( /^Digit[0-9]$/.test( event.code ) ) {
						activate( "hotbar:" + hotbarSlot( hotbarPage, Number( event.code.slice( 5 ) ) || 10 ) );
						return;
					}
					const names: Record<number, UiPanel> = {
						0: "Character",
						1: "Inventory",
						2: "Skills",
						3: "Actions",
						4: "Party",
						5: "Quests",
						6: "Guild",
						7: "Map",
						9: "Game Guide",
						14: "COS inventory",
						21: "Auto Potion",
						22: "COS inventory",
						23: "Party Matching",
						24: "Alchemy",
						25: "Stall network"
					};
					if ( binding === 11 ) activate( "hotbar:0" );
					else if ( binding === 31 ) activate( "academy-open" );
					else if ( binding >= 0 && names[binding] ) {
						setPanel( names[binding]!, "toggle" );
						dirty = true;
					} else if ( binding === 13 && view.gameplay ) {
						const pose = view.gameplay.pose, gid = pose ? nearestGroundItem( view.entities, pose ) : 0;
						if ( gid ) sendGameplay( { kind: "pickup", gid } );
					}
				}
			}
		},
		/*
		================
		blocks
		================
		*/
		blocks( x: number, y: number ) {
			return blocks.some( r => containsPoint( r, x, y ) );
		},
		/*
		================
		cursor

		A UI cursor mode that replaces the hover cursor everywhere, as
		CGInterface_SetCursorMode does: the armed repair hammer (0x96).
		================
		*/
		cursor(): import("@/engine/foundation/ui/world-cursor").WorldCursor | null {
			return cosHud.itemTargetCursor() ?? repairHud.cursor();
		},
		/*
		================
		step
		================
		*/
		step( next: UiView, now = 0, probe?: UiFrameProbe ): UiSemantics | null {
			if ( reverseScrollHud.reconcile( next ) ) dirty = true;
			quickslotTime = next.simulationTimeMs ?? now;
			if (
				cosHud.reconcileItemTarget(
					next.gameplay?.inventory ?? [],
					next.session?.phase === "world" && !next.travel
				)
			) dirty = true;
			if (
				(repairHud.armed() || repairHud.confirmCost() !== null) &&
				(next.session?.phase !== "world" || next.travel || !next.gameplay?.shop ||
					next.gameplay.shop.npc !== next.gameplay.target || next.gameplay.shop.error)
			) {
				repairHud.reset();
				dirty = true;
			}
			if ( disposed ) return null;
			// 683B40 creates the notice on the session's first world entry only.
			const sessionPhase = next.session?.phase;
			if ( sessionPhase !== "world" && sessionPhase !== "disconnected" && sessionPhase !== "reconnecting" ) {
				mallNotice = "pending";
			} else if ( sessionPhase === "world" && !next.travel && mallNotice === "pending" && hud.data() ) {
				mallNotice = hud.data()!.mallNotify.open ? "open" : "closed";
				dirty = true;
			}
			// 0x366A reset -> CGInterface_CloseTransientWindowsOnReset (685400) destroys
			// the ItemMall section. A world transfer starting is that reset here.
			if ( next.travel && !view?.travel && itemMall.read().visible ) {
				itemMall.close();
				dirty = true;
			}
			if ( panel === "COS inventory" && !next.gameplay?.cosRecords?.some( r => !r.dead && r.hp > 0 ) ) {
				setPanel( "" );
				dirty = true;
			}
			// The worker closed the room (NPC released, travel, world leave).
			if ( panel === "Storage" && !next.gameplay?.storage ) {
				setPanel( "" );
				dirty = true;
			}
			// The official's answer opens or refreshes the application window.
			fortressScheduleHud.observe( next.gameplay?.fortress, next.gameplay?.target ?? undefined );
			fortressTaxHud.observe(
				next.gameplay?.fortress,
				next.gameplay?.target ?? undefined,
				next.session?.phase === "world" && next.gameplay?.npcConversation?.phase === "menu"
			);
			if ( fortressTaxHud.npc() !== null && panel !== FORTRESS_TAX_PANEL && canLeavePanel() ) {
				setPanel( FORTRESS_TAX_PANEL );
				dirty = true;
			}
			if ( panel === FORTRESS_TAX_PANEL && fortressTaxHud.npc() === null ) {
				setPanel( "" );
				dirty = true;
			}
			productionClock = next.simulationTimeMs ?? now;
			fortressProductionHud.observe(
				next.gameplay?.fortress,
				next.gameplay?.target ?? undefined,
				next.session?.phase === "world" && next.gameplay?.npcConversation?.phase === "menu"
			);
			if ( fortressProductionHud.npc() !== null && panel !== FORTRESS_PRODUCTION_PANEL && canLeavePanel() ) {
				setPanel( FORTRESS_PRODUCTION_PANEL );
				dirty = true;
			}
			if ( panel === FORTRESS_PRODUCTION_PANEL && fortressProductionHud.npc() === null ) {
				setPanel( "" );
				dirty = true;
			}
			// 65A5E0 redraws the countdown once a second.
			if ( panel === FORTRESS_PRODUCTION_PANEL && Math.floor( now / 1000 ) !== productionSecond ) {
				productionSecond = Math.floor( now / 1000 );
				dirty = true;
			}
			// 62B0B0: a finished reinforcement writes its outcome to the chat and
			// plays the success or failure effect once in the open window.
			alchemyClock = now;
			const outcome = next.gameplay?.alchemy?.outcome;
			if ( outcome && outcome.sequence > alchemyOutcomeSeen ) {
				alchemyOutcomeSeen = outcome.sequence;
				if ( next.gameplay?.alchemy?.visible ) {
					for ( const line of alchemyResultLines( outcome, hudCopy ) ) hudMessages.append( line );
					alchemyEffect = { flags: outcome.flags, startedAt: now };
				}
				dirty = true;
			}
			if ( alchemyEffect ) {
				if ( !alchemyEffectCell( now - alchemyEffect.startedAt ) ) alchemyEffect = null;
				dirty = true;
			}
			fortressStaffHud.observe(
				next.session?.phase === "world" && next.gameplay?.npcConversation?.phase === "menu" &&
					next.gameplay.target === next.gameplay.npcConversation.gid ?
					next.gameplay.npcConversation.gid :
					undefined
			);
			// A collected outcome closes the talk (75D5C0); a rank answer opens its window.
			if ( jobHud.observe( next.gameplay?.jobRanks?.opened, next.gameplay?.jobOutcome ) ) {
				sendGameplay( { kind: "npc-close" } );
				dirty = true;
			}
			if ( jobHud.rank() && panel !== JOB_RANK_PANEL && canLeavePanel() ) {
				setPanel( JOB_RANK_PANEL );
				dirty = true;
			}
			if ( fortressScheduleHud.isOpen() && panel !== FORTRESS_SCHEDULE_PANEL && canLeavePanel() ) {
				setPanel( FORTRESS_SCHEDULE_PANEL );
				dirty = true;
			}
			if (
				panel === FORTRESS_SCHEDULE_PANEL &&
				(!fortressScheduleHud.isOpen() || next.gameplay?.npcConversation?.phase !== "menu")
			) {
				setPanel( "" );
				dirty = true;
			}
			fortressWarHud.observe( next.gameplay?.fortressApplication?.sequence );
			if ( fortressWarHud.npc() !== null && panel !== FORTRESS_WAR_PANEL && canLeavePanel() ) {
				setPanel( FORTRESS_WAR_PANEL );
				dirty = true;
			}
			if ( panel === FORTRESS_WAR_PANEL && next.gameplay?.npcConversation?.phase !== "menu" ) {
				setPanel( "" );
				dirty = true;
			}
			// A warehouse ticket opens the room without the talk menu's click.
			if ( panel !== "Storage" && next.gameplay?.storage && !view?.gameplay?.storage && canLeavePanel() ) {
				storagePanel.reset();
				setPanel( "Storage" );
				dirty = true;
			}
			if ( shopOpenRequest ) {
				const request = shopOpenRequest, game = next.gameplay;
				if ( next.session?.phase !== "world" || game?.target !== request.gid ) shopOpenRequest = null;
				else if ( (game.shopCompletionRevision ?? 0) !== request.revision ) {
					shopOpenRequest = null;
					if ( game.shop?.npc === request.gid && !game.shop.error ) {
						setPanel( "Shop" );
						shopPage = 0;
						shopTab = request.tab;
						shopChoice = null;
						shopDialog = false;
					}
					dirty = true;
				}
			}
			// Every frame, unlike chat() below, which runs only when the HUD is
			// assembled: a running bug recording's timer and cap.
			bugReport?.recordingFrame();
			const minimapDelta = minimapLast === null ? 0 : Math.max( 0, now - minimapLast );
			minimapLast = now;
			const zoom = advanceMinimapZoom( minimapZoom, minimapTarget, minimapDelta );
			if ( zoom !== minimapZoom ) {
				minimapZoom = zoom;
				dirty = true;
			}
			const consoleDelta = consoleLast ? Math.max( 0, (now - consoleLast) / 1000 ) : 0;
			consoleLast = now;
			if ( consolePhase === 1 || consolePhase === 2 ) dirty = true;
			// Gameplay durations use the worker's fixed clock, not the main thread's
			// performance origin. Advance retained gauges even with no new packets.
			const nextBuffTick = next.session?.phase === "world" &&
					next.gameplay?.buffSlots?.some( s =>
						s.state === "departing" || s.effect.remainingMs !== undefined
					) ?
				Math.floor( (next.simulationTimeMs ?? 0) / 100 ) :
				-1;
			const slotTick = (next.gameplay?.skillCooldowns?.length || next.gameplay?.itemCooldowns?.length ||
					next.gameplay?.returnScroll || next.gameplay?.questGathering ||
					skillPressFeedbackActive( next.gameplay, quickslotTime )) ?
				Math.floor( quickslotTime / 16 ) :
				-1;
			if ( slotTick !== quickslotTick ) {
				quickslotTick = slotTick;
				dirty = true;
			}
			if ( slotEffects.due( next.simulationTimeMs ?? 0 ) ) dirty = true;
			if ( now >= equipmentWarningDue ) {
				equipmentWarningDue = now + 80;
				equipmentWarningPhase = (equipmentWarningPhase + 1) % 17;
				if ( equipmentWarningVisible ) dirty = true;
			}
			const caution = cautionVisible ? Math.floor( now / 100 ) % 8 : -1;
			if ( caution !== cautionFrame ) {
				cautionFrame = caution;
				dirty = true;
			}
			if ( nextBuffTick !== buffTick ) {
				buffTick = nextBuffTick;
				dirty = true;
			}
			if ( stepBuffViewers( next, now ) ) dirty = true;
			if (
				view?.hoveredEntity !== next.hoveredEntity || view?.dropNamesHeld !== next.dropNamesHeld ||
				view?.blindHeld !== next.blindHeld
			) dirty = true;
			if (
				focus === "chat-text" || focus === "gm-input" || focus === "gold-amount" || focus === "shop-quantity"
			) {
				if ( !caretDue ) {
					caretDue = now + 200;
					caretVisible = true;
				} else if ( now >= caretDue ) {
					caretDue = now + 200;
					caretVisible = !caretVisible;
					dirty = true;
				}
			} else {
				caretDue = 0;
				caretVisible = true;
			}
			// Hidden screens still collect their admitted work. Only new requests
			// depend on visibility; otherwise completions retain the shared slots.
			if ( title.step() ) dirty = true;
			if ( text.step() ) {
				dirty = true;
				layoutResourcesRevision++;
			}
			const guideNeeded = next.session?.phase === "world";
			if ( guideResources.step( guideNeeded ) ) dirty = true;
			guildManagerHud.observeSoldiers(
				next.session?.phase === "world" && next.gameplay?.npcConversation?.phase === "menu" ?
					next.gameplay.npcConversation.gid :
					undefined,
				next.gameplay?.social?.soldierAttributeSequence ?? 0
			);
			if ( npcPanel.observe( next.session?.phase === "world" ? next.gameplay?.npcConversation : undefined ) ) {
				dirty = true;
			}
			if ( withdrawal.observe( next.session?.phase === "world" ? next.gameplay?.restorationRevision ?? 0 : 0 ) ) {
				dirty = true;
			}
			if ( trackedQuest && !next.gameplay?.quests?.some( q => q.refId === trackedQuest ) ) {
				trackedQuest = 0;
				dirty = true;
			}
			if ( minimapResources.step( trackedQuest !== 0, next.session?.phase === "world" ) ) dirty = true;
			const questPositions = (next.gameplay?.quests?.find( q => q.refId === trackedQuest )?.targetIds ?? [])
				.flatMap( id => {
					const p = minimapResources.positions()?.get( id );
					return p ? [ p ] : [];
				} );
			const minimapQueries = minimapFloorQueries( next.gameplay ?? null, questPositions ),
				queryKey = minimapQueries.map( minimapPoseKey ).join( "|" );
			if ( next.session?.phase === "world" && queryKey !== minimapQueryKey ) {
				minimapQueryKey = queryKey;
				sendGameplay( { kind: "minimap-floors", poses: minimapQueries } );
			}
			if ( hud.step( next.session?.phase === "world" || next.frontend?.phase === "dock" ) ) {
				dirty = true;
				layoutResourcesRevision++;
			}
			// The stall prompts follow the stall: naming opens the title entry
			// with the default title (5A1DF0 mode 1); a stall that moved on
			// closes its prompt.
			const stallState = next.gameplay?.stall, stallPrompt = stallHud.prompt();
			if ( stallState?.phase === "naming" && !stallPrompt ) {
				openStallPrompt( {
					kind: "title",
					text: hudCopy( "UIIT_STT_STALL_DEFAULT_TITLE" ).replace( "%s", next.session?.character ?? "" )
				} );
			} else if ( stallPrompt && !stallPromptLive( stallPrompt, stallState ) ) {
				stallHud.close();
				dirty = true;
			}
			if ( !stallState?.network.open ) stallHud.resetNetwork();
			// 69FBB0: the exchange opens beside the inventory tab.
			if ( exchangeHud.opened( !!next.gameplay?.exchange?.open ) && panel !== "Inventory" ) {
				setPanel( "Inventory" );
				dirty = true;
			}
			if ( stallCategories.step( !!stallState?.network.open ) ) dirty = true;
			if ( localization.step( now, next.session?.phase === "world" ) ) {
				dirty = true;
				tooltipMemo = null;
			}
			if ( resources.step( paths, now ) ) {
				dirty = true;
				layoutResourcesRevision++;
			}
			const joining = next.gameplay?.partyMatching?.joining,
				joiningElapsed = joining ? Math.max( 0, (next.simulationTimeMs ?? now) - joining.since ) : 10000,
				joiningTick = joiningElapsed < 10000 ? Math.floor( joiningElapsed / 200 ) : -1;
			if ( joiningTick !== partyProgressTick ) {
				partyProgressTick = joiningTick;
				dirty = true;
			}
			partyProgressVisible = next.session?.phase === "world" && joiningElapsed < 10000;
			if (
				next.gameplay?.localGid !== view?.gameplay?.localGid ||
				next.session?.phase !== "world" && next.session?.phase !== "disconnected" &&
					next.session?.phase !== "reconnecting"
			) gauges.reset();
			if ( gauges.advance() ) dirty = true;
			if (
				spGauge.step(
					next.session?.phase === "world" ? next.gameplay?.progression?.skillExperience : undefined
				)
			) dirty = true;
			if ( next.session?.phase === "world" ) {
				if (
					notificationBanner.step(
						next.gameplay?.notices ?? [],
						now,
						notificationBannerPaths.every( p => !!resources.size( p ) ) && !!hud.data(),
						hudCopy
					)
				) dirty = true;
				if (
					uniqueBanner.step(
						next.gameplay?.notices ?? [],
						now,
						uniqueBannerPaths.every( p => !!resources.size( p ) ) && !!hud.data(),
						hudCopy
					)
				) dirty = true;
				const timer = questTimers.step(
					next.gameplay?.quests ?? [],
					now,
					next.gameplay?.fortress?.worldId ?? 0x10001,
					guideResources.data()?.questPresentation.records
				);
				if ( timer.changed ) dirty = true;
				if (
					questBanner.step(
						next.gameplay?.questProgress ?? [],
						now,
						guideResources.data()?.questPresentation.text,
						questBannerPaths.every( p => !!resources.size( p ) ),
						next.gameplay?.notices ?? [],
						timer.notices,
						hudCopy
					)
				) dirty = true;
			}
			const regionArt = hud.data()?.root.GDR_REGION_INFO_VIEW;
			const regionReady = !!regionArt?.texture && resources.has( regionArt.texture ) &&
				text.extentHeight( 4 ) > 0;
			if (
				regionBanner.step(
					next.session?.phase === "world" && next.worldReady && !next.travel && regionReady ?
						next.gameplay?.pose?.regionId :
						undefined,
					hud.data()?.regionCodes,
					hud.data()?.zones,
					now
				)
			) dirty = true;
			if ( stepRebirthPrompt( next, now ) ) dirty = true;
			// World frontend camera snapshots change every frame but do not change HUD
			// geometry. Keep time-dependent damage/loading outside this retained path.
			const displayedBerserk = next.session?.phase === "world" ? next.berserkGauge?.displayed ?? 0 : 0;
			if ( displayedBerserk !== berserkDisplayed ) {
				berserkDisplayed = displayedBerserk;
				if ( displayedBerserk === 5 ) sendGameplay( { kind: "guide-event", event: 9 } );
			}
			const berserkGid = next.session?.phase === "world" ?
				next.entities.find( e => e.gid === next.gameplay?.localGid && e.appearanceState?.[2] === 1 )?.gid ?? 0 :
				0;
			if ( berserkGid !== berserkActor ) {
				berserkActor = berserkGid;
				berserkStarted = now;
				dirty = true;
			}
			const nextBerserkFrame = berserkActor ? Math.floor( Math.min( 63000, now - berserkStarted ) / 50 ) : -1;
			if ( nextBerserkFrame !== berserkFrame ) {
				berserkFrame = nextBerserkFrame;
				dirty = true;
			}
			// The full-screen entry flash (8CEF20) fades continuously; redrawing it
			// only on the 50 ms atlas frame stepped its brightness into a flicker.
			if ( berserkActor && berserkEntryFlash( now - berserkStarted ) > 0 ) dirty = true;
			const stableWorld = next.session?.phase === "world" && next.frontend?.phase === "world" &&
				view?.frontend?.phase === "world" &&
				!loading && !next.travel && next.worldTransitionRegion === undefined && next.worldReady &&
				now < hudMessages.deadline() &&
				now < speech.deadline() &&
				view.resourceError === next.resourceError && view.worldError === next.worldError &&
				view.worldRetrying === next.worldRetrying &&
				view.worldReady === next.worldReady && view.travel === next.travel &&
				view.worldTransitionRegion === next.worldTransitionRegion &&
				view.frontend.error === next.frontend.error && view.frontend.status === next.frontend.status &&
				view.frontend.creation?.status === next.frontend.creation?.status &&
				view.frontend.selectedCharacter === next.frontend.selectedCharacter &&
				view.session === next.session && view.gameplay === next.gameplay && view.entities === next.entities &&
				view.hoveredEntity === next.hoveredEntity &&
				view.berserkGauge?.displayed === next.berserkGauge?.displayed && view.width === next.width &&
				view.height === next.height;
			if ( stableWorld && !dirty ) return null;
			if (
				!loading && !next.frontend && !dirty && now < nextPoll && view?.resourceError === next.resourceError &&
				view?.worldError === next.worldError && view?.worldRetrying === next.worldRetrying &&
				view?.worldReady === next.worldReady &&
				view?.travel === next.travel && view?.worldTransitionRegion === next.worldTransitionRegion &&
				view?.session === next.session &&
				view?.berserkGauge?.displayed === next.berserkGauge?.displayed && view?.gameplay === next.gameplay &&
				view?.entities === next.entities && view?.width === next.width && view?.height === next.height
			) return null;
			const resizedWorld = !!view && (view.width !== next.width || view.height !== next.height) &&
				(view.session?.phase === "world" ||
					((view.session?.phase === "disconnected" || view.session?.phase === "reconnecting") &&
						!!view.gameplay?.localGid));
			nextPoll = now + 100;
			view = next;
			probe?.detailBegin( "ui-assembly" );
			uiNow = now;
			const specialty = specialtyDealHud.state();
			if ( specialty ) {
				const shop = next.gameplay?.shop;
				if ( !shop || shop.npc !== specialty.selection.npc || (shop.cosGid ?? 0) !== specialty.cosGid ) {
					// The shop or its transport went away: nothing more can move.
					specialtyDealHud.close();
					specialtyCombo = false;
				} else if ( specialty.dealing ) {
					const chunk = specialtyDealHud.observe(
						!!next.gameplay?.inventoryPending,
						specialtyStock( next.gameplay ),
						now
					);
					if ( chunk ) sendSpecialtyChunk( chunk );
				}
				// The loop and its 5000 ms timer advance on frames, not only on news.
				dirty = true;
			}
			const phase = next.session?.phase ?? "signed-out";
			if ( phase === "world" ) {
				for ( const notice of next.gameplay?.notices ?? [] ) {
					if ( notice.sequence !== undefined && notice.sequence > noticeDialogSequence ) {
						noticeDialogSequence = notice.sequence;
						if ( notice.dialog ) {
							dirty = true;
							activeNoticeDialog = notice.dialog;
							noticeDialogPosition = null;
							focus = null;
							composing = false;
						}
					}
				}
			} else {
				activeNoticeDialog = undefined;
				noticeDialogPosition = null;
				noticeDialogFrame = null;
				if ( !next.gameplay?.localGid ) noticeDialogSequence = 0;
			}
			if (
				recallConfirm !== null &&
				(phase !== "world" || next.gameplay?.npcConversation?.phase !== "menu" ||
					next.gameplay.npcConversation.gid !== recallConfirm || next.gameplay.target !== recallConfirm ||
					!((next.gameplay.targetCapabilities ?? 0) & 0x40))
			) {
				recallConfirm = null;
				dirty = true;
			}
			if ( phase !== lastPhase && phase === "authenticating" ) titleNotice = { until: now + 15000 };
			if (
				phase === "world" || phase === "signed-out" || phase === "disconnected" ||
				titleNotice && now >= titleNotice.until
			) titleNotice = null;
			const titleProcess = !!next.frontend &&
				[ "loading-title", "intro", "login-reveal", "login", "login-accepted" ].includes( next.frontend.phase );
			if ( !titleProcess ) {
				titleNotice = null;
				if ( next.frontend ) password = "";
			}
			if ( phase !== "world" || !next.gameplay?.eligibility?.gm ) {
				consolePhase = 0;
				consoleY = -112;
				consoleText = "";
				consoleRows = [];
				consoleHistory = [];
				consoleHistoryIndex = 0;
				// Leaving the world rebaselines (-1) rather than rewinds: retained
				// replies must not be shown again on re-entry or after a teleport.
				if ( phase !== "world" ) gmObserved = -1;
			}
			if ( consolePhase === 1 ) {
				consoleY = Math.min( 0, consoleY + Math.trunc( consoleDelta * 400 ) );
				if ( consoleY === 0 ) consolePhase = 3;
				dirty = true;
			}
			if ( consolePhase === 2 ) {
				consoleY = Math.max( -112, consoleY - Math.trunc( consoleDelta * 400 ) );
				if ( consoleY === -112 ) consolePhase = 0;
				dirty = true;
			}
			const retainedWorld = (phase === "disconnected" || phase === "reconnecting") && !!next.gameplay?.localGid;
			if ( phase !== "world" ) {
				splitStack = null;
				groundDrop = null;
			}
			if (
				phase !== "world" || next.gameplay?.inventoryPending || next.gameplay?.social?.invitation ||
				next.gameplay?.social?.resurrection
			) {
				carriedItem = null;
			}
			const invitation = phase === "world" || retainedWorld ? next.gameplay?.social?.invitation : null,
				identity = invitation ? invitation.type + ":" + invitation.gid : "";
			if ( identity !== inviteIdentity ) {
				inviteIdentity = identity;
				invitePosition = null;
			}
			// 7644E0 (types 4 and 8) clears the pending death-box timer 0xF and
			// retires the death box (kind 3) when a question opens; selecting
			// oneself while dead brings it back (6813E0, portrait or corpse).
			const proposer = phase === "world" || retainedWorld ? next.gameplay?.social?.resurrection?.gid ?? 0 : 0;
			if ( proposer && resurrectionPrompt.opens( proposer ) ) {
				deathDismissed = true;
				deathRequested = false;
			}
			resurrectionPrompt.sync( proposer );
			if ( phase !== "disconnected" ) disconnectPosition = null;
			const gachaVisible = phase === "world" && !!next.gameplay?.gacha?.visible;
			if ( gachaVisible && !gachaWasVisible ) {
				gachaPage = 0;
				gachaSlot = -1;
				setPanel( "Magic Pop" );
			}
			gachaWasVisible = gachaVisible;
			// B338 lock 0x80000000 (75AE50) shows the grant window with the inventory.
			if (
				magicOptionHud.sync(
					phase === "world" && !!next.gameplay?.magicOption?.visible,
					next.gameplay?.magicOption?.item ?? null
				)
			) setPanel( GRANT_PANEL );
			if ( next.session && next.session.revision !== lastSessionRevision ) {
				if (
					(loginReplyPending || next.session.nativeTitleStatus !== lastNativeTitleStatus) && titleProcess &&
					titleStatusKey( next.session.nativeTitleStatus, next.session.nativeTitleArgument )
				) {
					titleNotice = {
						status: next.session.nativeTitleStatus,
						argument: next.session.nativeTitleArgument,
						until: now + 15000
					};
					sound( "message" );
				}
				if ( next.session.phase !== "authenticating" ) loginReplyPending = false;
				lastNativeTitleStatus = next.session.nativeTitleStatus;
				lastSessionRevision = next.session.revision;
				pending = false;
			}
			// Native CPSTitle only ever writes the password edit from the control itself,
			// so a rejected attempt stays typed for correction. Authentication can finish
			// before CPSTitle retires; its visible edit survives the accepted-login fade.
			// A restored session can enter world without ever receiving a server list.
			// Returning from logout starts a new discovery; a list reply completes it.
			if ( phase === "signed-out" && lastPhase !== phase && lastPhase !== "listing-servers" ) {
				serversRequested = false;
			}
			if (
				phase !== "world" ||
				buffDismiss &&
					!next.gameplay?.buffSlots?.some( s =>
						s.state !== "departing" && s.effect.gid === next.gameplay!.localGid &&
						s.effect.skill === buffDismiss!.skillId && s.effect.token === buffDismiss!.instance
					)
			) {
				if ( buffDismiss ) {
					buffDismiss = null;
					dirty = true;
				}
			}
			if ( phase !== "world" ) autoPotionInput.reset();
			else {
				// 572770 reads the selected/dragged control; 561D50 checks the
				// Item Mall child (69B2E0), not arbitrary nonmodal windows.
				const blocked = !!(carriedItem || carriedShortcut), itemMallOpen = itemMall.read().visible;
				if ( autoPotionInput.sync( blocked, itemMallOpen ) ) {
					sendGameplay( { kind: "auto-potion-input", blocked, itemMallOpen } );
				}
			}
			const compositePackage = compositeItemHud.packageId();
			if (
				compositePackage !== null &&
				!(phase === "world" && next.gameplay?.countJobs?.some( r => r.packageRefObjId === compositePackage ))
			) compositeItemHud.close();
			if ( phase !== "world" ) cosHud.reset();
			else if (
				cosHud.reconcile(
					next.gameplay?.cosStatusRecords ?? next.gameplay?.cosRecords ?? [],
					next.gameplay?.selectedCosGid
				)
			) dirty = true;
			if ( phase !== lastPhase ) {
				if ( retainedWorld ) {
					hover = null;
					pressed = null;
					focus = null;
					composing = false;
					focusRequest = undefined;
				} else {
					hudMessages.reset();
					notificationBanner.reset();
					uniqueBanner.reset();
					questBanner.reset();
					questTimers.reset();
				}
				pending = false;
				message = "";
				dirty = true;
				lastPhase = phase;
				if (
					!next.frontend && ![ "signed-out", "listing-servers", "authenticating", "failed" ].includes( phase )
				) password = "";
				if ( phase === "signed-out" ) {
					rosterRequested = false;
					roster = [];
				}
				if ( phase === "world" ) {
					// 6A06B0: the interface opens its windows where the last session
					// left them at this screen size.
					const remembered = windowPlacement.enter( next.width, next.height );
					if ( remembered ) {
						popupPosition = remembered.mainPopup ?? null;
						[mapX, mapY] = remembered.worldMap ??
							[
								Math.trunc( next.width / 2 ) - WORLD_MAP_WIDTH / 2,
								Math.trunc( next.height / 2 ) - WORLD_MAP_HEIGHT / 2
							];
						guideX = guideY = null;
						extPosition = null;
					}
				}
				if ( phase !== "world" && !retainedWorld ) {
					// 6A01B0: logout writes them back before the session's windows go.
					persistWindowPositions();
					guildWarHud.reset( true );
					windowPlacement.reset();
					itemMall.reset();
					carriedShortcut = null;
					if ( panel === "Option" ) audioPreference( audioSaved, false );
					chatFeedbackObserved = -1;
					focusRequest = undefined;
					speech.reset();
					academyWasVisible = false;
					rememberedMainPopup = "Character";
					chatScroll.reset();
					statusScroll.reset();
					chatHidden = false;
					whispersOpen = false;
					statusFilterOpen = false;
					minimapZoom = minimapTarget = 160;
					minimapLast = null;
					abilityDetails = false;
					chatRows = 2;
					chatTab = 0;
					statusRows = 2;
					mapFollow = true;
					mapSmall = false;
					mapPan = [ 0, 0 ];
					mapCenter = null;
					guideObserved = 0;
					guideRequested = 0;
					guidePending = 0;
					guideScroll = 0;
					guideOrigin = null;
					admittedWindows.clear();
					guideSidebar = false;
					guideTab = "general";
					guideEvent = 0;
					guideX = null;
					guideY = null;
					guideGroups.clear();
					guideIndexScroll = 0;
					goldAmount = "";
					confirmDrop = "";
					inventorySlot = -1;
					selectedAction = 4000;
					partyDialog = null;
					partyPurposeOpen = false;
					partyMatchSelection = 0;
					partyMatchOffset = 0;
					partySearch = { name: "", purpose: 4, min: 1, max: 90 };
					resetPanel();
					socialName = "";
					socialSubject = "";
					socialContents = "";
					socialAmount = "";
					socialMember = 0;
					socialPage = 0;
					confirmSocial = "";
					selectedSkill = 0;
					skillPage = 0;
					hotbarPage = 0;
					trainingMode = false;
					clearHotbar = false;
					selectedQuest = 0;
					trackedQuest = 0;
					minimapQueryKey = "";
					confirmAbandon = false;
					questPage = 0;
					chatPage = 0;
					chatText = "";
					chatTarget = "";
					inventoryPage = 0;
					cosSlot = -1;
					cosPage = 0;
					cosPlayerPage = 0;
					cosGid = 0;
				}
			}
			if ( resizedWorld && (phase === "world" || retainedWorld) ) {
				// GraphicApply relays layout to live children; a closed guide is absent.
				if ( panel === "Game Guide" && guideX !== null && guideY !== null ) {
					guideX = Math.trunc( next.width / 2 ) - 210;
					guideY = Math.trunc( next.height / 2 ) - 226;
				}
				const extended = hud.data()?.extended[Number( extVertical ) * 2 + Number( extDouble )];
				if ( extPosition && extended && !compactHudLayout( next.width, next.height ) ) {
					// 548490 sizes the widget root from header ID10, not the protruding slots.
					const header = Object.values( extended ).find( node => node.id === 10 )!;
					extPosition = [
						Math.min( extPosition[0], next.width - header.rect[2] ),
						Math.min( extPosition[1], next.height - header.rect[3] )
					];
				}
			}
			if ( phase === "world" && windowPlacement.needsInitialSave() ) persistWindowPositions( false );
			if ( next.session?.servers ) {
				servers = next.session.servers;
				if ( !servers.some( s => s.id === selectedServer && s.operating ) ) {
					selectedServer = servers.find( s => s.operating )?.id ?? "";
				}
			}
			if ( !serversRequested && phase === "signed-out" ) {
				serversRequested = true;
				requestServers();
			}
			// Every shard "Check" (maintenance): keep asking until one runs.
			const serversRetry = serverListRefreshDue(
				servers,
				serverList && phase === "signed-out",
				pending,
				now,
				serversRetryAt
			);
			if ( serversRetry !== null ) {
				serversRetryAt = serversRetry;
				requestServers();
			}
			if ( next.session?.characters ) roster = next.session.characters;
			if ( next.frontend ) selectedCharacter = next.frontend.selectedCharacter ?? "";
			if ( phase === "character-select" && !next.session?.characters && !rosterRequested ) {
				rosterRequested = true;
				commands( { kind: "roster" } );
			}
			if ( !roster.some( c => c.name === selectedCharacter ) ) {
				selectedCharacter = next.frontend ?
					"" :
					roster.find( c => !c.deletePending )?.name ?? "";
			}
			// Rebuild after an owned input/resource change or an active timed effect.
			const game = next.gameplay,
				pose = game?.pose,
				local = game?.vitals.find( v => v.gid === game.localGid ),
				target = next.entities.find( e => e.gid === game?.target );
			const training = skillTraining.read( game?.skillCatalog, game?.skills );
			itemMall.observe( game?.itemMall );
			if ( game?.itemMall && !game.inventoryPending ) {
				const request = itemMall.takeNextPurchase( game.itemMall );
				if ( request ) sendGameplay( { kind: "mall-buy", request } );
			}
			dirty = false;
			windowMissing = [];
			gauges.begin();
			let hudCorner: UiRect | undefined;
			let quads: UiQuad[] = [];
			controls = [];
			blocks = [];
			paths = [ ...commonWindowPaths ];
			const fontPath = text.path();
			if ( fontPath ) paths.push( fontPath );
			const w = next.width, h = next.height, full: UiRect = [ 0, 0, w, h ];
			const compact = compactHud.layout( w, h );
			let targetBottom = 0, compactTelemetryTop = 4;
			if ( berserkActor ) {
				const alpha = berserkEntryFlash( now - berserkStarted );
				if ( alpha > 0 ) {
					quads.push( {
						rect: full,
						uv: [ 0, 0, 1, 1 ],
						clip: full,
						texture: "",
						color: [ 1, 1, 1, alpha ]
					} );
				}
			}
			/*
			================
			windowOrigin
			================
			*/
			function windowOrigin(
				name: string,
				initial: UiRect,
				id = "window-drag:" + name,
				options: { drag?: UiRect; nativeExtent?: readonly [number, number]; } = {}
			) {
				const r = windowPlacement.frame( id, initial, [ w, h ], options.nativeExtent );
				const drag = options.drag;
				controls.push( {
					id,
					label: name,
					kind: "region",
					draggable: true,
					rect: drag ?
						[ r[0] + drag[0], r[1] + drag[1], drag[2], drag[3] ] :
						[ r[0] + 10, r[1], r[2] - 21, 34 ]
				} );
				return r;
			}
			/*
			================
			rect
			================
			*/
			function rect(
				r: UiRect,
				color: readonly [number, number, number, number],
				textureId = "",
				uv: UiRect = [ 0, 0, 1, 1 ],
				clip: UiRect = full
			) {
				if ( r[2] > 0 && r[3] > 0 ) quads.push( { rect: r, color, texture: textureId, uv, clip } );
			}
			// CIFStateSlot children render after the icon: the admittance overlay
			// (6E8410), then the gauge at {0, cell, cell, 4} (6E7FA0).
			/*
			================
			buffViewer
			================
			*/
			function buffViewer( prefix: string, icons: readonly BuffViewerIcon[], ox: number, oy: number ) {
				for ( const icon of icons ) {
					const r: UiRect = [ ox + icon.x, oy + icon.y, icon.size, icon.size ],
						gauge: UiRect = [ r[0], r[1] + icon.size, icon.size, 4 ];
					for ( const layer of [ icon.path, ...icon.overlay ] ) {
						if ( layer ) {
							paths.push( layer );
							if ( resources.has( layer ) ) rect( r, white, layer );
						}
					}
					for ( const layer of icon.gauge ) {
						paths.push( layer );
						if ( resources.has( layer ) ) rect( gauge, white, layer );
					}
					controls.push( {
						id: prefix + icon.id,
						label: icon.label,
						helpSource: icon.helpSource,
						kind: "region",
						rect: r
					} );
					blocks.push( r );
				}
			}
			/*
			================
			image
			================
			*/
			function image(
				r: UiRect,
				path: string,
				color: UiQuad["color"] = white,
				uv: UiRect = [ 0, 0, 1, 1 ],
				clip: UiRect = full
			) {
				paths.push( path );
				if ( resources.has( path ) ) rect( r, color, path, uv, clip );
			}
			/*
			================
			label
			================
			*/
			function label(
				value: string,
				x: number,
				y: number,
				color: readonly [number, number, number, number] = white,
				clip: UiRect = full
			) {
				quads.push(
					...text.quads( value, [ x, y, Math.max( 0, clip[0] + clip[2] - x ), 22 ], clip, color, {
						vAlign: 0
					} )
				);
			}
			// A window is one resource admission. Keep demand while withholding BOTH
			// pixels and hit areas, so a cold open cannot expose an empty clickable shell.
			/*
			================
			beginWindow
			================
			*/
			function beginWindow() {
				const mark = [ quads.length, controls.length, blocks.length, paths.length ] as const;
				paths.push( ...commonWindowPaths );
				return mark;
			}
			/*
			================
			endWindow
			================
			*/
			function endWindow( mark: readonly [number, number, number, number], owner = "primary" ) {
				const admittedWindow = admittedWindows.get( owner );
				windowMissing.push( ...paths.slice( mark[3] ).filter( path => !resources.has( path ) ) );
				const key = phase + ":" + w + ":" + h;
				if (
					!fontPath || !resources.has( fontPath ) ||
					paths.slice( mark[3] ).some( path => !resources.has( path ) )
				) {
					quads.length = mark[0];
					controls.length = mark[1];
					blocks.length = mark[2];
					// Keep a fully admitted popup until its replacement is ready. Old image
					// demand stays alive; stale content controls cannot dispatch new-tab actions.
					if ( admittedWindow?.key === key ) {
						quads.push( ...admittedWindow.quads );
						blocks.push( ...admittedWindow.blocks );
						paths.push( ...admittedWindow.paths );
						controls.push(
							...admittedWindow.controls.map( c => ({
								...c,
								disabled: c.id === "close" || c.id.startsWith( "select-window:" ) ? c.disabled : true
							}) )
						);
					}
				} else {
					// Fit within this window before caching/publication; text behind a modal
					// must not become a neighboring column of that modal.
					if ( compact ) {
						fitUiGroup( quads, controls, mark[0], mark[1], full, full, {
							blocks,
							firstBlock: mark[2],
							disableDrag: compactWindowDrag
						} );
						for ( let i = mark[1]; i < controls.length; i++ ) {
							controls[i] = compactCloseControl( controls[i]!, full );
						}
					}
					const painted = resolveTextOverlaps( quads.slice( mark[0] ) );
					quads.length = mark[0];
					quads.push( ...painted );
					admittedWindows.set( owner, {
						key,
						quads: painted,
						controls: controls.slice( mark[1] ),
						blocks: blocks.slice( mark[2] ),
						paths: paths.slice( mark[3] )
					} );
				}
			}
			/*
			================
			windowBox
			================
			*/
			function windowBox( title: string, x: number, y: number, width: number, height: number ) {
				const r: UiRect = [ x, y, width, height ];
				blocks.push( r );
				rect( r, [ .06, .075, .065, .96 ] );
				quads.push(
					...frameRing( r, FRAME, PARTS.map( part => resources.size( FRAME + part + ".png" ) ), full )
				);
				quads.push(
					...text.quads( title, [ x + 10, y + 12, width - 21, 12 ], full, white, { hAlign: 1, vAlign: 1 } )
				);
			}
			/*
			================
			button
			================
			*/
			function button(
				id: string,
				value: string,
				x: number,
				y: number,
				width = 120,
				disabled = false,
				selected = false,
				clientTop?: number
			) {
				const r: UiRect = [ x, y, width, 24 ];
				blocks.push( r );
				controls.push( { id, label: value, rect: r, kind: "button", disabled, selected } );
				const down = !disabled && ((pressed === id && hover === id) || selected),
					path = disabled ?
						BUTTON_DISABLE :
						down ?
						BUTTON_PRESS :
						((pressed === null && hover === id) || focus === id) ?
						BUTTON_FOCUS :
						BUTTON;
				if ( [ BUTTON, BUTTON_FOCUS, BUTTON_PRESS, BUTTON_DISABLE ].every( resources.has ) ) {
					rect( r, white, path );
				}
				quads.push(
					...text.quads(
						value,
						[ x + (down ? 1 : 0), y + (down ? 1 : 0) + (clientTop ?? 0), width, 24 - (clientTop ?? 0) ],
						r,
						buttonTextColor( [ 254 / 255, 251 / 255, 216 / 255, 1 ], buttonAccess( false, disabled ) ),
						{ hAlign: 1, vAlign: clientTop === undefined ? 1 : 0 }
					)
				);
			}
			/*
			================
			closeButton
			================
			*/
			function closeButton( x: number, y: number, id = "close" ) {
				const r: UiRect = [ x, y, 16, 16 ];
				blocks.push( r );
				controls.push( { id, label: "Close", rect: r, kind: "button" } );
				if ( resources.has( CLOSE ) && resources.has( CLOSE_PRESS ) && resources.has( CLOSE_FOCUS ) ) {
					rect( r, white, pressed === id ? CLOSE_PRESS : hover === id ? CLOSE_FOCUS : CLOSE );
				}
			}
			/*
			================
			drawCosHud

			CIFCOSManager: one CIFCOSStatus icon per owned companion (6AA290 bind,
			6A9C50 gauges, the selected one outlined) and the CIFCOSCommand panel
			of the selected companion, anchored to the under bar (6A34F0).
			================
			*/
			function drawCosHud(
				data: NonNullable<ReturnType<typeof hud.data>>,
				records: readonly import("@/engine/contracts/gameplay").CosRecord[],
				barX: number,
				barY: number
			) {
				const shown = records.filter( r => cosClass( r.band ) !== null );
				const compactMargin = 15, compactPitch = 60, compactRowHeight = 90;
				const compactColumns = Math.max( 1, Math.floor( (w - compactMargin * 2) / compactPitch ) );
				let statusBottom = targetBottom;
				shown.forEach( ( record, i ) => {
					const cls = cosClass( record.band )!, chrome = cosStatusChrome( cls );
					const reference = data.cosReferences.get( record.refObjId );
					const nativeRect = cosStatusRect( w, i, cls );
					const r: UiRect = compact ?
						[
							compactMargin + i % compactColumns * compactPitch,
							targetBottom + compactMargin + Math.floor( i / compactColumns ) * compactRowHeight,
							nativeRect[2],
							nativeRect[3]
						] :
						nativeRect;
					const [x, y] = r;
					statusBottom = Math.max( statusBottom, y + r[3] );
					if ( record.gid === cosHud.selected() ) {
						image( [ x - 11, y - 11, chrome.outlineSize[0], chrome.outlineSize[1] ], chrome.outline );
					}
					image( r, chrome.frame );
					const icon = iconPath( reference?.icon );
					if ( icon ) image( [ x + 6, y + 7, 32, 32 ], icon );
					const ratios = cosStatusRatios( record, reference );
					const gauges = [
						[ chrome.showHp, ratios.hp, "am_hp", 47 ],
						[ chrome.showHgp, ratios.hgp, "am_hgp", 56 ]
					] as const;
					for ( const [shown, ratio, name, dy] of gauges ) {
						const path = "/assets/images/Media_extracted/interface/animal/" + name + ".png";
						const size = resources.size( path );
						if ( !shown || ratio === null ) continue;
						paths.push( path );
						if ( size ) {
							image( [ x + 4, y + dy, size[0] * ratio, size[1] ], path, white, [ 0, 0, ratio, 1 ] );
						}
					}
					blocks.push( r );
					const name = record.name || hudCopy( "UIIT_STT_COSNEWUI_TITLE" );
					controls.push( {
						id: "cos-status:" + record.gid,
						label: name,
						helpText: name,
						rect: r,
						kind: "button",
						rightActivate: true
					} );
				} );
				const context = cosCommandContext(), record = context.record;
				if ( !record ) return;
				const commands = cosCommandButtons( cosClass( record.band )! );
				if ( !commands.length ) return;
				const nativeLayout = cosCommandLayout( barX, barY, commands.length );
				const layout = compact ?
					cosCommandLayout(
						barX + compactMargin - nativeLayout.rows[0]!.at[0],
						barY + statusBottom + compactMargin - nativeLayout.rows[0]!.at[1],
						commands.length
					) :
					nativeLayout;
				const animal = "/assets/images/Media_extracted/interface/animal/";
				if ( cosHud.open() ) {
					commands.forEach( ( command, i ) => {
						const row = layout.rows[i]!, size = resources.size( row.frame );
						paths.push( row.frame );
						if ( size ) image( [ row.at[0], row.at[1], size[0], size[1] ], row.frame );
						image( row.slot, cosCommandIcon( command, context ) );
						blocks.push( row.slot );
						const help = hudCopy( cosCommandLabel( command, context ) );
						controls.push( {
							id: "cos-command:" + command,
							label: help,
							helpText: help,
							rect: row.slot,
							kind: "button",
							disabled: !cosCommandEnabled( command, context )
						} );
					} );
				}
				const board = animal + "am_ctrl_tab.png", boardSize = resources.size( board );
				paths.push( board );
				if ( boardSize ) image( [ layout.board[0], layout.board[1], boardSize[0], boardSize[1] ], board );
				// The control's rect comes from the base art and every state is
				// requested up front: switching to a not-yet-loaded focus image
				// must not drop the control under the cursor (it flickered).
				const id = "cos-command-toggle",
					base = animal + (cosHud.open() ? "am_ctrl_close" : "am_ctrl_open"),
					states = [ base + ".png", base + "_focus.png", base + "_press.png" ],
					wanted = states[pressed === id && hover === id ? 2 : hover === id ? 1 : 0]!,
					toggle = resources.has( wanted ) ? wanted : states[0]!,
					toggleSize = resources.size( states[0]! );
				paths.push( ...states );
				if ( toggleSize ) {
					const r: UiRect = [ layout.toggle[0], layout.toggle[1], toggleSize[0], toggleSize[1] ];
					image( r, toggle );
					blocks.push( r );
					controls.push( { id, label: "", rect: r, kind: "button" } );
				}
			}
			/*
			================
			retainedHud
			================
			*/
			function retainedHud(
				cache: ReturnType<typeof createRetainedLayout<HudSection>>,
				key: readonly unknown[],
				build: () => void
			) {
				const product = cache.read( key, () => {
					const q = quads.length, c = controls.length, b = blocks.length, p = paths.length;
					build();
					return {
						quads: quads.splice( q ),
						controls: controls.splice( c ),
						blocks: blocks.splice( b ),
						paths: paths.splice( p )
					};
				} );
				quads.push( ...product.quads );
				controls.push( ...product.controls );
				blocks.push( ...product.blocks );
				paths.push( ...product.paths );
			}
			/*
			================
			authoredGauge
			================
			*/
			function authoredGauge(
				slot: string,
				identity: number,
				node: import("@/engine/foundation/ui/authored-layout").AuthoredControl,
				ox: number,
				oy: number,
				fraction: number
			) {
				const value = gauges.read( slot, identity, fraction );
				if ( !node.texture ) return;
				paths.push( node.texture );
				if ( resources.has( node.texture ) ) {
					quads.push(
						...gaugeFill(
							authoredRect( node, ox, oy ),
							node.uv,
							node.texture,
							value.current,
							value.target,
							full
						)
					);
				}
			}
			/*
			================
			authoredImage
			================
			*/
			function authoredImage(
				node: AuthoredControl,
				ox: number,
				oy: number,
				path = node.texture,
				fraction = 1,
				tint: UiQuad["color"] = white
			) {
				if ( !path ) return;
				const r = authoredRect( node, ox, oy );
				image( [ r[0], r[1], r[2] * fraction, r[3] ], path, tint, [
					node.uv[0],
					node.uv[1],
					node.uv[2] * fraction,
					node.uv[3]
				] );
			}

			if ( gmObserved < 0 ) gmObserved = Math.max( 0, ...(game?.gmReplies ?? []).map( reply => reply.sequence ) );
			for ( const reply of game?.gmReplies ?? [] ) {
				if ( reply.sequence > gmObserved ) {
					gmObserved = reply.sequence;
					const value = reply.text ?? (reply.key ? hudCopy( reply.key ) : "");
					if ( reply.console ) appendConsoleLine( value );
					else hudMessages.append( value );
				}
			}
			/*
			================
			authoredText
			================
			*/
			function authoredText( node: AuthoredControl, ox: number, oy: number, value: string ) {
				quads.push(
					...text.quads( value, authoredClientRect( node, ox, oy ), full, node.color, {
						fontIndex: node.fontIndex,
						hAlign: node.hAlign,
						vAlign: node.vAlign
					} )
				);
			}
			/*
			================
			authoredSkillPoints

			A skill point count in its static: the first of skillPointReadouts
			that fits the static's width (port-only shortening, hud-readouts.ts).
			The underbar's GDR_STATIC_SP (48) and the restoration page's
			GDR_SKILL_TEXT_SP_NUM (45) overflow at beta counts.
			================
			*/
			function authoredSkillPoints( node: AuthoredControl, ox: number, oy: number, points: number ) {
				const room = authoredClientRect( node, ox, oy )[2], readouts = skillPointReadouts( points );
				authoredText(
					node,
					ox,
					oy,
					readouts.find( value => text.run( value, 0, node.fontIndex ).width <= room ) ?? readouts.at( -1 )!
				);
			}
			/*
			================
			drawTransientBanners

			Desktop retains the native draw order and fixed origins. Compact mode
			wraps native-size text below the occupied combat header, warning first.
			================
			*/
			function drawTransientBanners( top?: number ) {
				const rows = [
					{
						value: uniqueBanner.value( hudCopy ),
						alpha: uniqueBanner.alpha(),
						paths: uniqueBannerPaths,
						y: 130,
						color: [ 0, 52, 92 ] as const
					},
					{
						value: notificationBanner.value( hudCopy ),
						alpha: notificationBanner.alpha(),
						paths: notificationBannerPaths,
						y: 100,
						color: [ 128, 45, 67 ] as const
					},
					{
						value: questBanner.value(),
						alpha: questBanner.alpha(),
						paths: questBannerPaths,
						y: 160,
						color: [ 0, 91, 66 ] as const
					}
				];
				if ( top !== undefined ) rows.sort( ( a, b ) => a.y - b.y );
				let bottom = top ?? 0;
				for ( const row of rows ) {
					paths.push( ...row.paths );
					if ( row.alpha <= 0 ) continue;
					const lines = top === undefined ?
						[ row.value ] :
						textLines( row.value, Math.max( 1, w - 80 ), value => text.run( value, 2 ).width );
					const lineHeight = text.extentHeight( 0, 2 ),
						width = Math.ceil( Math.max( 0, ...lines.map( value => text.run( value, 2 ).width ) ) ),
						height = lineHeight * lines.length,
						y = top === undefined ? row.y : bottom,
						x = (w >> 1) - (width >> 1);
					quads.push(
						...uniqueBannerQuads(
							row.value,
							row.alpha,
							w,
							h,
							width,
							height,
							row.paths,
							resources.size,
							y,
							row.color
						)
					);
					lines.forEach( ( value, index ) =>
						quads.push(
							...text.quads( value, [ x, y + index * lineHeight, width, lineHeight ], full, [
								1,
								1,
								1,
								row.alpha
							], { fontIndex: 0, fontStyle: 2, hAlign: 1, vAlign: 0 } )
						)
					);
					bottom = y + height + 12;
				}
				return bottom;
			}
			/*
			================
			comboBox
			================
			*/
			function comboBox( r: UiRect, id: string, label: string, value: string, disabled = false ) {
				const chrome = comboBoxChrome(
					r,
					resources.size,
					full,
					!disabled && pressed === id ? 2 : !disabled && hover === id ? 1 : 0
				);
				paths.push( ...chrome.paths );
				quads.push( ...chrome.quads );
				quads.push( ...text.quads( value, chrome.textRect, r, white, { hAlign: 1, vAlign: 1 } ) );
				controls.push( { id, label, rect: r, kind: "button", disabled } );
			}
			/*
			================
			authoredButton
			================
			*/
			function authoredButton(
				node: AuthoredControl,
				ox: number,
				oy: number,
				id: string,
				label: string,
				disabled = false
			) {
				const r = authoredRect( node, ox, oy ), family = hud.buttonFamily( node.texture );
				paths.push( ...family );
				controls.push( { id, label, rect: r, kind: "button", disabled } );
				blocks.push( r );
				const state = disabled ?
					3 :
					pressed === id && hover === id ?
					2 :
					pressed === null && (hover === id || focus === id) ?
					1 :
					0;
				if ( family.every( resources.has ) ) rect( r, white, family[state]! );
			}
			/*
			================
			authoredLabeledButton
			================
			*/
			function authoredLabeledButton(
				node: AuthoredControl,
				ox: number,
				oy: number,
				id: string,
				caption: string,
				disabled = false
			) {
				authoredButton( node, ox, oy, id, caption, disabled );
				authoredText( node, ox, oy, caption );
			}
			// Resource sections are reverse-insertion lists (9BD240), so native child
			// paint order is the reverse of controlsByName, not numeric control ID.
			/*
			================
			authoredChrome
			================
			*/
			function authoredChrome( node: AuthoredControl, ox: number, oy: number ) {
				// CIFScrollManager inherits CIFFrame (6F3830), but some resources omit its skin.
				if ( node.type === "CIFScrollManager" && !node.texture ) return;
				const r = authoredRect( node, ox, oy );
				if (
					node.type === "CIFFrame" || node.type === "CIFSubFrame" || node.type === "CIF_NPCTalk" ||
					node.type === "CIFScrollManager"
				) {
					paths.push( ...PARTS.map( p => node.texture + p + ".png" ) );
					quads.push(
						...frameRing(
							r,
							node.texture,
							PARTS.map( p => resources.size( node.texture + p + ".png" ) ),
							full
						)
					);
					if ( node.text ) {
						quads.push(
							...text.quads(
								hudCopy( node.text ),
								[ r[0] + 10, r[1] + 7, r[2] - 20, 12 ],
								full,
								node.color,
								{ hAlign: 1, vAlign: 0 }
							)
						);
					}
				} else if ( node.type === "CIFBarWnd" ) {
					const bar = barChrome( r, node.texture, resources.size, full );
					paths.push( ...bar.paths );
					quads.push( ...bar.quads );
					if ( node.text ) authoredText( node, ox, oy, hudCopy( node.text ) );
				} else if ( node.type === "CIFStretchWnd" ) {
					const ring = stretchRing( r, node.texture, resources.size, full );
					paths.push( ...ring.paths );
					quads.push( ...ring.quads );
				} else if ( node.type === "CIFLattice" ) {
					for ( const cell of latticeCells( r[0], r[1], Math.floor( r[2] / 36 ), Math.floor( r[3] / 36 ) ) ) {
						const path = node.texture + cell.part + ".png";
						paths.push( path );
						if ( resources.has( path ) ) {
							rect( cell.rect, white, path );
						}
					}
				} else if ( node.type === "CIFNormalTile" ) {
					paths.push( node.texture );
					quads.push( ...normalTile( r, node.texture, resources.size( node.texture ), full ) );
				} else if ( node.type === "CIFStatic" || node.type === "CIFPartySlot" ) {
					if ( node.texture ) authoredImage( node, ox, oy );
					if ( node.type === "CIFStatic" && node.text ) authoredText( node, ox, oy, hudCopy( node.text ) );
				} else if ( node.type === "CIFPML" ) {
					const out = text.guide( guideTokens( hudCopy( node.text ) ), r, r, node.color, resources.size );
					quads.push( ...out.quads );
					paths.push( ...out.paths );
				}
			}
			/*
			================
			partyEdit
			================
			*/
			function partyEdit(
				node: AuthoredControl,
				ox: number,
				oy: number,
				id: string,
				value: string,
				maxLength: number
			) {
				const r = authoredRect( node, ox, oy );
				controls.push( { id, label: node.name, kind: "text", value, rect: r, maxLength } );
				const start = Math.min( value.length, selection[0] ?? 0 ),
					end = Math.min( value.length, selection[1] ?? start ),
					before = text.run( value.slice( 0, start ) ).width,
					through = text.run( value.slice( 0, end ) ).width;
				if ( focus === id && end > start ) {
					rect( [ r[0] + before, r[1], through - before, r[3] ], [ .2, .4, .7, .6 ], "", [ 0, 0, 1, 1 ], r );
				}
				quads.push( ...text.quads( value, r, r, white, { overflow: "clip" } ) );
				if ( focus === id && caretVisible ) {
					rect( [ r[0] + through, r[1], 1, r[3] ], white, "", [ 0, 0, 1, 1 ], r );
				}
			}
			/*
			================
			simpleMessageBox

			The box every caller of CGInterface_ShowSimpleMessageBox (6888C0)
			opens: the tiled CIFMessageBox client under its frame ring, the
			caption, the body lines left at (30,65) and the centred Yes/No pair
			(52E720), sized by its lines (textMessageBoxLayout). Its sisters draw
			through this one owner so they share one look. The caller owns the
			blocks and controls around it.
			================
			*/
			function simpleMessageBox(
				box: { title: string; lines: readonly string[]; yes: string; no: string; yesDisabled?: boolean; }
			) {
				const lines = messageBoxLines( box.lines, value => text.run( value ).width );
				const layout = textMessageBoxLayout( w, h, lines.map( line => text.run( line ).width ) );
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( box.title, layout.title, full, white, { hAlign: 1, vAlign: 0 } )
				);
				for ( const [i, line] of lines.entries() ) {
					quads.push( ...text.quads( line, layout.lines[i]!, full, white, { vAlign: 0 } ) );
				}
				button( box.yes, hudCopy( "UIIT_CTL_YES" ), layout.accept[0], layout.accept[1], 76, !!box.yesDisabled );
				button( box.no, hudCopy( "UIIT_CTL_NO" ), layout.refuse[0], layout.refuse[1], 76 );
				return layout;
			}
			/*
			================
			nativePage
			================
			*/
			function nativePage( layout: AuthoredLayout, px: number, py: number, exclude: readonly number[] = [] ) {
				for ( const node of authoredPaintOrder( layout ) ) {
					if ( !exclude.includes( node.id ) ) authoredChrome( node, px, py );
				}
			}
			/*
			================
			nativeFrame
			================
			*/
			function nativeFrame( node: AuthoredControl, px: number, py: number, caption: string, closeId = "close" ) {
				const r: UiRect = [ px, py, node.rect[2], node.rect[3] ];
				blocks.push( r );
				paths.push( ...PARTS.map( p => node.texture + p + ".png" ) );
				quads.push(
					...frameRing( r, node.texture, PARTS.map( p => resources.size( node.texture + p + ".png" ) ), full )
				);
				// CIFMainFrame::OnCreate 6F8230: shared title/drag width is frame minus 21.
				closeButton( px + r[2] - 26, py + 10, closeId );
				quads.push(
					...text.quads( caption, [ px + 10, py + 12, r[2] - 21, 12 ], full, white, { hAlign: 1, vAlign: 0 } )
				);
			}
			/*
			================
			nativeTab
			================
			*/
			function nativeTab(
				id: string,
				caption: string,
				r: UiRect,
				selected: boolean,
				style: { family: string; disabled?: boolean; client?: UiRect; }
			) {
				const { family, disabled = false, client = [ 0, 0, 0, 0 ] } = style;
				const content: UiRect = [
					r[0] + client[0],
					r[1] + client[1],
					r[2] - client[0] - client[2],
					r[3] - client[1] - client[3]
				];
				const path = ROOT + "interface/ifcommon/" + family + (selected ? "_on" : "_off") + ".png";
				image( r, path );
				controls.push( { id, label: caption, rect: r, kind: "button", selected, disabled } );
				quads.push(
					...text.quads( caption, content, full, white, {
						hAlign: 1,
						vAlign: 1,
						fontStyle: selected ? 2 : 0
					} )
				);
			}
			equipmentWarningVisible = false;
			cautionVisible = false;
			slotEffects.beginPaint();
			/*
			================
			equipmentOverlay
			================
			*/
			function equipmentOverlay(
				item: import("@/engine/contracts/gameplay").InventoryItem | undefined,
				r: UiRect
			) {
				if ( !item || !next.gameplay ) return;
				const gender = next.session?.characters?.find( row => row.name === next.session?.character )?.gender;
				const overlay = itemEquipmentOverlay( item, next.gameplay.progression ?? { masteries: [] }, {
					country: next.gameplay.guide?.country,
					sex: gender === 0 ? 1 : gender === 1 ? 0 : undefined
				}, next.gameplay.inventory );
				if ( overlay ) {
					const warning = overlay === "icon_item_warning";
					if ( warning ) equipmentWarningVisible = true;
					image(
						r,
						ROOT + "icon/" + overlay + ".png",
						white,
						warning ? equipmentWarningUv( equipmentWarningPhase ) : [ 0, 0, 1, 1 ]
					);
				}
			}
			/*
			================
			itemEffects

			Item-slot painters share the same overlays and animation clock.
			================
			*/
			function itemEffects( id: string, owned: import("@/engine/contracts/gameplay").InventoryItem, r: UiRect ) {
				// CIFSlotWithHelp's item effects (item-slot-effects.ts): the dead
				// companion wash, then the animated sheets. 0x3645 flashes target
				// the inventory and equipment slots.
				const wash = itemSlotWash( owned );
				if ( wash ) rect( r, wash );
				const flashes = id.startsWith( "slot:" ) ?
					(game?.itemFlashes ?? []).filter( f => f.slot === owned.slot ) :
					[];
				const overlays = itemSlotOverlays(
					owned,
					r,
					slotSeed( id + ":" + owned.refObjId ),
					next.simulationTimeMs ?? 0,
					flashes
				);
				for ( const overlay of overlays ) {
					image( overlay.rect, overlay.path, white, overlay.uv );
					slotEffects.mark();
				}
			}
			/*
			================
			nativeItem
			================
			*/
			function nativeItem(
				id: string,
				item: { readonly icon?: string; readonly name?: string; readonly quantity?: number; } | undefined,
				r: UiRect,
				disabled = false,
				selected = false
			) {
				const path = iconPath( item?.icon );
				if ( path ) {
					image( r, path );
					if ( item && "typeFlags" in item ) {
						const owned = item as import("@/engine/contracts/gameplay").InventoryItem;
						equipmentOverlay( owned, r );
						itemEffects( id, owned, r );
					}
				}
				controls.push( {
					id,
					label: item?.name ?? "",
					rect: r,
					kind: "button",
					disabled,
					selected,
					rightActivate: !!item && (id.startsWith( "slot:" ) || id.startsWith( "storage-slot:" )),
					draggable: !!item && !repairHud.armed() && cosHud.itemTargetCursor() === null,
					carry: !repairHud.armed() && cosHud.itemTargetCursor() === null && !!item &&
						ITEM_SLOT_PREFIXES.some( prefix => id.startsWith( prefix ) )
				} );
				itemCount( item, r );
			}
			/*
			================
			itemCount
			================
			*/
			function itemCount(
				item:
					| { readonly quantity?: number; readonly typeFlags?: number; readonly refObjId?: number; }
					| undefined,
				r: UiRect
			) {
				const unlimited = item?.refObjId !== undefined && !!game?.unlimitedItems?.includes( item.refObjId );
				for ( const q of itemCountQuads( item, r, full, unlimited ) ) {
					// The infinity sign is solid quads; digit sprites are resources.
					if ( !q.texture ) {
						quads.push( q );
						continue;
					}
					paths.push( q.texture );
					if ( resources.has( q.texture ) ) quads.push( q );
				}
			}
			/*
			================
			nativeSpin
			================
			*/
			function nativeSpin(
				node: AuthoredControl,
				px: number,
				py: number,
				previous: string,
				next: string,
				page: number,
				pages: number
			) {
				const r = authoredRect( node, px, py ), base = ROOT + "interface/ifcommon/";
				for (
					const [id, side, xx, disabled] of [ [ previous, "left", r[0], page <= 0 ], [
						next,
						"right",
						r[0] + r[2] - 16,
						page + 1 >= pages
					] ] as const
				) {
					const n: AuthoredControl = {
						...node,
						type: "CIFButton",
						rect: [ xx, r[1], 16, 16 ],
						texture: base + "com_" + side + "_arrow.png"
					};
					authoredButton( n, 0, 0, id, id, disabled );
				}
				quads.push(
					...text.quads( String( page + 1 ), [ r[0] + 16, r[1], r[2] - 32, r[3] ], full, white, {
						hAlign: 1,
						vAlign: 1
					} )
				);
			}
			/*
			================
			mainPopup
			================
			*/
			function mainPopup( page: MainPopupPage, popup: ReturnType<typeof mainPopupGeometry> ) {
				const main = hud.data()!.windows.ifmainpopup!;
				const { caption, backing, frame } = popup, [px, py] = frame;
				blocks.push( frame );
				quads.push(
					...frameRing( frame, FRAME, PARTS.map( p => resources.size( FRAME + p + ".png" ) ), full )
				);
				controls.push( {
					id: "main-popup-drag",
					label: hudCopy( caption ),
					kind: "button",
					draggable: true,
					rect: [ px + 10, py, 367, 34 ]
				} );
				closeButton(
					px + 362,
					py + 10,
					page === "Inventory" && panel !== "Inventory" ? "companion-close" : "close"
				);
				quads.push(
					...text.quads( hudCopy( caption ), [ px + 10, py + 12, 367, 12 ], full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				authoredImage( main.GDR_MAINPOPUP_LEFT_DECO_STATIC!, px, py );
				if ( backing ) authoredImage( backing, px, py );
				for (
					const [name, destination] of [
						[ "CHAR", "Character" ],
						[ "INV", "Inventory" ],
						[ "SKILL", "Skills" ],
						[ "ACT", "Actions" ],
						[ "PARTY", "Party" ],
						[ "QUEST", "Quests" ],
						[ "APPRENTICE", "Academy" ]
					] as const
				) authoredButton( main["GDR_BTN_" + name]!, px, py, "select-window:" + destination, destination );
			}
			const x = Math.max( 0, Math.round( (w - 380) / 2 ) ), y = Math.max( 0, Math.round( (h - 520) / 2 ) );
			const operationStatus = next.frontend?.creation?.status ?? next.frontend?.status;
			diagnosticError = next.worldError ?? next.resourceError ?? next.session?.error ?? game?.error ??
				game?.chat?.error ?? next.frontend?.error ?? guideResources.error() ?? minimapResources.error() ??
				hud.error() ?? text.error() ?? resources.error() ?? null;
			// CPSTitle 73E7B0 replaces timer 1000 with a 15000 ms deadline. A camera
			// phase never renews it; 7425E0 retires the title process after its fade.
			const error = (operationStatus ?
				catalogMessage( operationStatus, title.catalog( operationStatus.key ) ) :
				undefined) ??
				(next.frontend && titleNotice ?
					(titleNotice.status === undefined ?
						"UIO_MSG_ERROR_CITATION" :
						titleStatusMessage( titleNotice.status, titleNotice.argument, title.catalog )) :
					message) ??
				"";
			const titlePending = pending || next.frontend?.entryPending === true || phase === "listing-servers" ||
				phase === "authenticating";
			// CPSMission 7292E0 creates the artwork and gauge as one screen;
			// 728E10 retires them only at load completion. Session diagnostics do
			// not end this lifetime. Resume can precede the frontend snapshot.
			const missionFrontend = !next.frontend ||
				[ "world", "loading-world" ].includes( next.frontend.phase );
			const awaitingWorldResources = missionFrontend && !retainedWorld &&
				([ "connecting", "entering-world", "reconnecting" ].includes( phase ) ||
					phase === "world" && !next.worldReady);
			const missionLoading = (awaitingWorldResources || next.frontend?.phase === "loading-world" ||
				next.worldTransitionRegion !== undefined || !!next.travel) && phase !== "disconnected";
			// World load end (683B40) calls 575D10(worldMap, 1): every completed load,
			// teleports included, re-arms AUTO MOVE.
			if ( missionLoading ) mapWorldLoading = true;
			else if ( mapWorldLoading && phase === "world" && next.worldReady ) {
				mapWorldLoading = false;
				mapFollow = true;
				mapPan = [ 0, 0 ];
				mapCenter = null;
			}
			const frontendLoading = next.frontend &&
				[ "loading-title", "loading-create", "loading-race", "loading-dock" ].includes( next.frontend.phase );
			let request: LoadingRequest | null = null;
			if ( frontendLoading ) {
				const f = next.frontend!;
				request = {
					key: f.phase + ":" + f.generation,
					background: ROOT + "interface/loading/loading_charactercustom" +
						(f.phase === "loading-create" && f.race === 1 ? "" : "_europe") + ".png",
					progress: f.loadingProgress ?? 0,
					complete: false,
					startup: f.phase === "loading-title",
					status: f.loadingStatus ?? "Loading scene assets"
				};
			} else if ( missionLoading ) {
				const revision = next.travel?.revision;
				if (
					!loading?.key.startsWith( "mission:" ) ||
					revision !== undefined && loadingTravelRevision !== undefined && revision !== loadingTravelRevision
				) {
					loadingGeneration++;
					loadingTravelRevision = revision;
				} else if ( revision !== undefined ) loadingTravelRevision = revision;
				const key = "mission:" + loadingGeneration;
				const background = loading?.key === key ?
					loading.background :
					next.travel ?
					travelLoadingBackground( next.travel, loadingVariant() ) :
					(next.frontend?.phase === "loading-world" || next.worldTransitionRegion === undefined) ?
					missionLoadingQuads( w, h, loadingVariant(), 0 )[1]!.texture :
					regionLoadingBackground( next.worldTransitionRegion! );
				request = {
					key,
					background,
					progress: next.loadingProgress ?? 0,
					complete: phase === "world" && next.worldReady && next.worldTransitionRegion === undefined,
					startup: false,
					status: "Loading world assets"
				};
			}
			// Browser inference: the frontend owns dock/title controls. A missing
			// snapshot waits on native transition art; transport state alone must
			// not construct a second roster/login interface.
			if ( !request && !next.frontend && !retainedWorld && phase !== "world" && phase !== "disconnected" ) {
				const dock = phase === "character-select" || phase === "loading-roster";
				request = {
					key: dock ? "awaiting-dock" : "awaiting-title",
					background: ROOT + "interface/loading/loading_charactercustom_europe.png",
					progress: 0,
					complete: false,
					startup: !dock,
					status: "Loading scene assets"
				};
			}

			loading = loadingPresentation(
				loading,
				request,
				now,
				phase === "disconnected" || next.frontend?.phase === "failed"
			);
			const worldVisible = (phase === "world" || retainedWorld) && !loading && next.worldReady &&
				(!next.frontend || next.frontend.phase === "world");
			if ( !(phase === "world" || retainedWorld) ) windowWarm.reset();
			// One unseen window build per idle frame; see window-warm.ts.
			const warming = windowWarm.begin(
				worldVisible && !!hud.data() && !!game?.progression && panel === "" && !carriedItem && !shopOpenRequest
			);
			// The unseen build sets the panel directly: no enter/leave hooks, no
			// transient resets (setPanel would cancel a pending shop open).
			if ( warming !== null ) {
				setPanel( warming, "warm" );
				if ( warming === "Skills" ) windowWarm.add( skillWindowWarmPaths() );
				// A map drag reaches tiles outside the warm build's view.
				if ( warming === "Map" ) windowWarm.add( worldMapImagePaths( hud.data()?.mapIcons ) );
			}
			if ( worldVisible && hud.data() ) {
				if ( chatFeedbackObserved < 0 ) {
					chatFeedbackObserved = Math.max(
						0,
						...(game?.chat?.feedback ?? []).map( notice => notice.sequence )
					);
				}
				for ( const notice of game?.chat?.feedback ?? [] ) {
					if ( notice.sequence > chatFeedbackObserved ) {
						chatFeedbackObserved = notice.sequence;
						hudMessages.append( chatFeedbackText( notice, hudCopy ) );
					}
				}
			}
			if ( loading ) {
				blocks.push( full );
				if ( loading.startup && next.frontend ) {
					const output = title.render(
						next.frontend,
						w,
						h,
						account,
						password,
						servers,
						selectedServer,
						titlePending,
						serverList,
						{
							hover,
							pressed,
							focus,
							selection,
							draft: serverDraft,
							offset: serverOffset,
							now,
							message: error,
							credentialsLocked: loginReplyPending || phase === "authenticating"
						},
						roster,
						selectedCharacter
					);
					paths.push( ...output.paths );
				}
				if ( !loading.startup ) {
					const output = loadingScreenQuads( w, h, loading.background ?? "", loading.progress ).filter( (
						_,
						i
					) => i !== 1 || loading!.background !== null );
					quads.push( ...output );
					paths.push( ...output.map( q => q.texture ).filter( Boolean ) );
				}
			} else if ( next.frontend && ![ "world", "loading-world", "failed" ].includes( next.frontend.phase ) ) {
				const output = title.render(
					next.frontend,
					w,
					h,
					account,
					password,
					servers,
					selectedServer,
					titlePending,
					serverList,
					{
						hover,
						pressed,
						focus,
						selection,
						draft: serverDraft,
						offset: serverOffset,
						now,
						message: error,
						credentialsLocked: loginReplyPending || phase === "authenticating"
					},
					roster,
					selectedCharacter
				);
				quads.push( ...output.quads );
				controls.push( ...output.controls );
				paths = output.paths;
				if ( (next.frontend.phase === "dock" || next.frontend.phase === "create") && !next.frontend.dialog ) {
					blocks.push( ...output.controls.map( control => control.rect ) );
				} else blocks.push( full );
				for ( const row of output.labels ) label( row.value, row.x, row.y, [ 1, 1, 1, row.alpha ] );
			} else if ( !worldVisible && !retainedWorld ) {
				blocks.push( full );
				rect( full, [ 0, 0, 0, 1 ] );
			} else {
				const gauge = next.berserkGauge;
				const character = roster.find( c => c.name === next.session?.character ), hudData = hud.data();
				if ( compact && hudData ) {
					const player = hudData.root.GDR_PLAYER_MINI_INFO!.rect;
					targetBottom = player[1] + player[3] + 4;
				}
				if ( hudData ) {
					const hpMax = game?.progression?.stats?.maxHp ?? local?.maxHp ?? character?.maxHp,
						mpMax = game?.progression?.stats?.maxMp ?? local?.maxMp ?? character?.maxMp;
					const warning = (options.hpWarning && vitalWarning( local?.hp, hpMax )) ||
						(options.mpWarning && vitalWarning( local?.mp, mpMax ));
					cautionVisible = !!warning;
					const hpGauge = gauges.read(
							"mini-hp",
							game?.localGid ?? 0,
							(local?.hp ?? 0) / Math.max( 1, hpMax ?? 1 ),
							"empty"
						),
						mpGauge = gauges.read(
							"mini-mp",
							game?.localGid ?? 0,
							(local?.mp ?? 0) / Math.max( 1, mpMax ?? 1 ),
							"empty"
						);
					retainedHud( playerLayoutCache, [
						hudData,
						layoutResourcesRevision,
						w,
						h,
						hover,
						pressed,
						focus,
						abilityDetails,
						options.hpWarning,
						options.mpWarning,
						warning ? Math.floor( now / 100 ) % 8 : -1,
						local?.hp,
						local?.mp,
						hpMax,
						mpMax,
						hpGauge.current,
						hpGauge.target,
						mpGauge.current,
						mpGauge.target,
						game?.localGid,
						game?.target === game?.localGid,
						game?.fortress,
						// GDR_PMI_PK and its tooltip (6B5150).
						game?.pkStatus,
						game?.social?.guild,
						game?.guide?.country,
						next.session?.character,
						game?.progression?.level ?? character?.level,
						game?.progression?.statPoints,
						gauge?.displayed,
						berserkActor,
						berserkFrame,
						...(abilityDetails ? Object.values( game?.progression?.stats ?? {} ) : [])
					], () => {
						const root = hudData.root.GDR_PLAYER_MINI_INFO!, p = hudData.player, [px, py] = root.rect;
						authoredImage( root, 0, 0 );
						blocks.push( authoredRect( root, 0, 0 ) );
						// GDR_PMI_SELECT (ID 200) frames the whole panel while the local
						// character is the target, as GDR_QPS_SELECT frames a party slot.
						if ( game?.localGid && game.target === game.localGid ) {
							authoredImage( p.GDR_PMI_SELECT!, px, py );
						}
						// Clicking the panel anywhere but its buttons selects your own
						// character, so a targeted buff or heal can be aimed at yourself.
						// The buttons are pushed after this control and sit above it.
						if ( game?.localGid ) {
							controls.push( {
								id: "self-target",
								label: "Select yourself",
								kind: "button",
								rect: authoredRect( root, 0, 0 )
							} );
						}
						for ( const vital of [ "HP", "MP" ] as const ) {
							const current = vital === "HP" ? local?.hp : local?.mp,
								max = vital === "HP" ?
									(game?.progression?.stats?.maxHp ?? local?.maxHp ?? character?.maxHp) :
									(game?.progression?.stats?.maxMp ?? local?.maxMp ?? character?.maxMp);
							if ( current !== undefined && max !== undefined ) {
								const node = p["GDR_PMI_GAUGE_" + vital]!,
									path = ROOT + "interface/playerminiinfo/pmi_" + vital.toLowerCase() + ".png",
									value = vital === "HP" ? hpGauge : mpGauge;
								paths.push( path );
								if ( resources.has( path ) ) {
									quads.push(
										...gaugeFill(
											authoredRect( node, px, py ),
											node.uv,
											path,
											value.current,
											value.target,
											full
										)
									);
								}
								authoredText(
									p["GDR_PMI_TXT_" + vital]!,
									px,
									py,
									`${Math.max( 0, Math.min( current, max ) )} / ${max}`
								);
							}
						}
						// Native caution atlas: eight 128x32 cells, four columns, 100ms per cell.
						for ( const vital of [ "HP", "MP" ] as const ) {
							const hp = vital === "HP",
								node = p["GDR_PMI_EFFECT_" + vital]!,
								current = hp ? local?.hp : local?.mp,
								max = hp ?
									(game?.progression?.stats?.maxHp ?? local?.maxHp ?? character?.maxHp) :
									(game?.progression?.stats?.maxMp ?? local?.maxMp ?? character?.maxMp);
							paths.push( node.texture );
							if (
								(hp ? options.hpWarning : options.mpWarning) && vitalWarning( current, max ) &&
								resources.has( node.texture )
							) {
								const frame = Math.floor( now / 100 ) % 8;
								rect( authoredRect( node, px, py ), white, node.texture, [
									(frame % 4) / 4,
									Math.floor( frame / 4 ) / 2,
									.25,
									.5
								] );
							}
						}
						authoredImage( p.GDR_PMI_PICTURE!, px, py, ROOT + "interface/playerminiinfo/pmi_face.png" );
						if ( game?.localGid ) {
							quads.push( {
								portraitGid: game.localGid,
								texture: "__portrait",
								rect: authoredRect( p.GDR_PMI_PICTURE!, px, py ),
								uv: [ 0, 0, 1, 1 ],
								color: white,
								clip: full
							} );
						}
						const country = game?.guide?.country;
						if ( country === 0 || country === 1 ) {
							authoredImage(
								p.GDR_PMI_RACE_MARK!,
								px,
								py,
								ROOT + "interface/ifcommon/com_kindred_" + (country === 0 ? "china" : "europe") + ".png"
							);
						}
						authoredButton(
							p.GDR_PMI_BTN_CINFO!,
							px,
							py,
							"ability-details",
							hudCopy( "UIIT_STT_CHARACTER_ABILITY_VIEW_OFF" ) || "See/hide major ability"
						);
						if ( abilityDetails ) {
							authoredImage( p.GDR_PMI_STA_CINFOBG!, px, py );
							blocks.push( authoredRect( p.GDR_PMI_STA_CINFOBG!, px, py ) );
							const stats = game?.progression?.stats,
								values = stats ?
									playerAbilityValues( stats, game?.progression?.level ?? character?.level ?? 1 ) :
									{};
							for (
								const key of [
									"STR",
									"INT",
									"PHYATT",
									"MAGATT",
									"PHYDEF",
									"MAGDEF",
									"HIT",
									"PARRY",
									"PHYBAL",
									"MAGBAL"
								]
							) {
								const node = p["GDR_PMI_TXT_" + key];
								if ( node ) authoredText( node, px, py, hudCopy( node.text ) || node.text );
								const value = values[key + "DAT"];
								if ( value !== undefined ) {
									authoredText( p["GDR_PMI_TXT_" + key + "DAT"]!, px, py, value );
								}
							}
						}
						// 6B5150: GDR_PMI_PK shows while any PK counter is non-zero.
						const pkTip = game?.pkStatus && pkStatusTooltip( game.pkStatus, hudCopy );
						if ( pkTip && p.GDR_PMI_PK ) {
							authoredImage( p.GDR_PMI_PK, px, py, ROOT + "icon/etc/stwin_pk.png" );
							const rect = authoredRect( p.GDR_PMI_PK, px, py );
							controls.push( { id: "GDR_PMI_PK", kind: "region", label: pkTip, helpText: pkTip, rect } );
							blocks.push( rect );
						}
						if ( game?.fortress ) {
							for (
								const indicator of fortressMiniIndicators(
									game.fortress,
									!!game.social?.guild,
									hudCopy
								)
							) {
								const node = p[indicator.control];
								if ( !node ) continue;
								authoredImage( node, px, py, indicator.image ?? node.texture );
								const rect = authoredRect( node, px, py );
								controls.push( {
									id: indicator.control,
									kind: "region",
									label: indicator.text,
									helpText: indicator.text,
									rect
								} );
								blocks.push( rect );
							}
						}
						authoredText( p.GDR_PMI_TXT_ID!, px, py, next.session?.character ?? "" );
						const level = game?.progression?.level ?? character?.level;
						if ( level !== undefined ) {
							authoredText(
								p.GDR_PMI_TXT_LEVEL!,
								px,
								py,
								(hudCopy( "UIIT_STT_LV_LEVEL" ) || "Lv %d").replace( "%d", String( level ) )
							);
						}
						if ( berserkActor ) {
							const animation = berserkHud( now - berserkStarted ),
								burn = ROOT + "interface/playerminiinfo/pmi_jahwan_burn.png",
								size = resources.size( burn );
							paths.push( burn );
							for ( let i = 0; i < 5; i++ ) {
								const circle = p["GDR_PMI_CIRCLE" + i]!;
								if ( resources.has( circle.texture ) && animation.circles[i]! > 0 ) {
									rect(
										authoredRect( circle, px, py ),
										[ 1, 1, 1, animation.circles[i]! ],
										circle.texture,
										circle.uv
									);
								}
								if ( size && animation.fire[i]! > 0 ) {
									const frame = (animation.frame + i * 2) % 12, columns = Math.floor( size[0] / 16 );
									rect(
										authoredRect( p["GDR_PMI_DECO_HWAN_SLOT" + i]!, px, py ),
										[ 1, 1, 1, animation.fire[i]! ],
										burn,
										[
											(frame % columns) * 16 / size[0],
											Math.floor( frame / columns ) * 28 / size[1],
											16 / size[0],
											28 / size[1]
										]
									);
								}
							}
							for ( const name of [ "GDR_PMI_HWAN_EFF_FACE", "GDR_PMI_HWAN_EFF_GLOW" ] ) {
								const n = p[name]!;
								if ( resources.has( n.texture ) && animation.glow > 0 ) {
									rect( authoredRect( n, px, py ), [ 1, 1, 1, animation.glow ], n.texture, n.uv );
								}
							}
						} else {for ( let i = 0; i < Math.min( 5, gauge?.displayed ?? 0 ); i++ ) {
								authoredImage( p["GDR_PMI_CIRCLE" + (4 - i)]!, px, py );
							}}
						if (
							gauge?.displayed === 5 &&
							next.entities.find( e => e.gid === game?.localGid )?.appearanceState?.[2] !== 1
						) authoredButton( p.GDR_PMI_BTN_JAHWAN!, px, py, "berserk", "Berserk" );
						if ( (game?.progression?.statPoints ?? 0) > 0 ) {
							authoredButton(
								p.GDR_PMI_BTN_STATUP!,
								px,
								py,
								"select-window:Character-stats",
								"Character"
							);
						}
					} );
				}
				// CIFPetMiniInfo (6B3AD0 / 6B34C0): the attack pet's panel, child 100
				// of the player mini window.
				const pet = hudData && game ? petMiniInfo( game.cosRecords, hudData.cosReferences ) : null;
				const petPanel = hudData?.player.GDR_PMI_PET_MINI_INFO, petLayout = hudData?.windows.ifpetminiinfo;
				if ( hudData && pet && petPanel && petLayout ) {
					if ( compact ) {
						const player = hudData.root.GDR_PLAYER_MINI_INFO!.rect;
						const petBounds = authoredRect( petPanel, player[0], player[1] );
						targetBottom = Math.max( targetBottom, petBounds[1] + petBounds[3] + 4 );
					}
					const [px, py] = hudData.root.GDR_PLAYER_MINI_INFO!.rect,
						hpPath = ROOT + "interface/playerminiinfo/pmi_pet_hp.png",
						hgpPath = ROOT + "interface/playerminiinfo/pmi_pet_hgp.png",
						facePath = ROOT + "interface/playerminiinfo/pmi_pet_face.png",
						icon = iconPath( pet.icon ),
						hpGauge = gauges.read( "pet-mini-hp", pet.gid, pet.hp ?? 0, "empty" ),
						hgpGauge = gauges.read( "pet-mini-hgp", pet.gid, pet.hgp, "empty" ),
						cautionFrame = pet.caution ? Math.floor( now / 100 ) % 8 : -1;
					retainedHud( petLayoutCache, [
						hudData,
						layoutResourcesRevision,
						pet.gid,
						pet.name,
						pet.level,
						icon,
						hpGauge.current,
						hpGauge.target,
						hgpGauge.current,
						hgpGauge.target,
						cautionFrame
					], () => {
						authoredImage( petPanel, px, py );
						const [qx, qy] = authoredRect( petPanel, px, py );
						blocks.push( authoredRect( petPanel, px, py ) );
						// 6B3760 backs the picture with pmi_pet_face, an opaque black disc,
						// and 6B3AD0 sets the icon drawn over it, as the player window's
						// portrait sits over pmi_face. Drawn last, the disc hid the icon.
						authoredImage( petLayout.GDR_PET_MINI_PICTURE!, qx, qy, facePath );
						if ( icon ) {
							paths.push( icon );
							if ( resources.has( icon ) ) {
								rect( authoredRect( petLayout.GDR_PET_MINI_PICTURE!, qx, qy ), white, icon );
							}
						}
						authoredText(
							petLayout.GDR_PET_MINI_TXT_NAME!,
							qx,
							qy,
							pet.name?.length ? pet.name : hudCopy( "UIIT_STT_COSNEWUI_TITLE" )
						);
						authoredText(
							petLayout.GDR_PET_MINI_TXT_LEVEL!,
							qx,
							qy,
							hudCopy( "UIIT_STT_LV_LEVEL" ).replace( "%d", String( pet.level ) )
						);
						// A zero-size CIFGauge takes its texture's size (112x8).
						for (
							const [node, path, value] of [
								[ petLayout.GDR_PET_MINI_GAUGE_HP!, hpPath, hpGauge ],
								[ petLayout.GDR_PET_MINI_GAUGE_HGP!, hgpPath, hgpGauge ]
							] as const
						) {
							paths.push( path );
							if ( !resources.has( path ) ) continue;
							const size = resources.size( path ), [gx, gy] = authoredRect( node, qx, qy );
							if ( !size ) continue;
							const [gw, gh] = size;
							quads.push(
								...gaugeFill( [ gx, gy, gw, gh ], node.uv, path, value.current, value.target, full )
							);
						}
						// Native caution atlas: eight 128x32 cells, four columns, 100ms per cell.
						const caution = petLayout.GDR_PET_MINI_EFFECT_HP!;
						paths.push( caution.texture );
						if ( cautionFrame >= 0 && resources.has( caution.texture ) ) {
							rect( authoredRect( caution, qx, qy ), white, caution.texture, [
								(cautionFrame % 4) / 4,
								Math.floor( cautionFrame / 4 ) / 2,
								.25,
								.5
							] );
						}
					} );
				}
				if ( hudData && game && hudData.root.GDR_MAGICSTATEBOARD ) {
					const origin = authoredRect( hudData.root.GDR_MAGICSTATEBOARD, 0, 0 );
					if ( game.buffSlots?.length ) paths.push( "/assets/images/Media_extracted/icon/buf_effect.png" );
					for ( const icon of buffBoard( game, next.simulationTimeMs ?? 0, boardSuppressed ) ) {
						if ( !icon.path ) continue;
						const r: UiRect = [ origin[0] + icon.x, origin[1] + icon.y, 20, 20 ];
						paths.push( icon.path );
						if ( icon.departure ) {
							const sprite = "/assets/images/Media_extracted/icon/buf_effect.png",
								frame = icon.departure.frame;
							paths.push( sprite );
							const color = [ 1, 1, 1, icon.departure.alpha ] as const;
							if ( resources.has( icon.path ) ) rect( r, color, icon.path );
							// 6E7B40: 50x52 cell from a 512x64 sheet, centered on the 20px slot.
							if ( resources.has( sprite ) ) {
								rect( [ r[0] - 15, r[1] - 16, 50, 52 ], color, sprite, [
									frame * 50 / 512,
									0,
									50 / 512,
									52 / 64
								] );
							}
							blocks.push( r );
							continue;
						}
						if ( resources.has( icon.path ) ) rect( r, white, icon.path );
						if ( icon.suppressed ) {
							for ( const layer of admittanceOverlay() ) {
								paths.push( layer );
								if ( resources.has( layer ) ) rect( r, white, layer );
							}
						}
						controls.push( {
							id: icon.id,
							label: icon.label,
							helpText: icon.helpText,
							helpSource: icon.helpSource,
							// A package slot is clicked open (6E2840, the board's
							// vtable +0x70 left-button handler); the rest only hover.
							kind: icon.id.startsWith( "count-job:" ) ? "button" : "region",
							rightActivate: !!icon.cancel,
							rect: r
						} );
						blocks.push( r );
						// 6E7FA0 @0x6E7FE3 builds every gauge from one rect, {0, slot height, slot
						// width, 4}, so the kind-3 slot's two controls overlay rather than stack:
						// id 2 (+0x388) carries s_stateodd_time02_gauge and the primary window,
						// id 3 (+0x38C) carries s_stateodd_time_gauge and the second window, and
						// only the first control receives the s_stateodd_time backing.
						if ( icon.fraction !== null ) {
							const bg = buffTimerRoot + "s_stateodd_time.png", two = icon.secondary !== undefined;
							const first = buffTimerRoot +
								(two ? "s_stateodd_time02_gauge.png" : "s_stateodd_time_gauge.png");
							const gauge: UiRect = [ r[0], r[1] + 20, 20, 4 ];
							paths.push( bg, first );
							if ( resources.has( bg ) ) rect( gauge, white, bg );
							if ( resources.has( first ) && icon.fraction > 0 ) {
								rect( [ gauge[0], gauge[1], 20 * icon.fraction, 4 ], white, first, [
									0,
									0,
									icon.fraction,
									1
								] );
							}
							if ( two && icon.secondary !== null && icon.secondary !== undefined ) {
								const second = buffTimerRoot + "s_stateodd_time_gauge.png";
								paths.push( second );
								if ( resources.has( second ) && icon.secondary > 0 ) {
									rect( [ gauge[0], gauge[1], 20 * icon.secondary, 4 ], white, second, [
										0,
										0,
										icon.secondary,
										1
									] );
								}
							}
						}
					}
				}
				if ( hudData && game?.social?.leader ) {
					const origin = authoredRect( hudData.root.GDR_QUICKPARTYBOARD!, 0, 0 ),
						slot = hudData.windows.ifquickpartyslot!,
						localPose = partyLocalPose( game, next.entities );
					for ( const shadeImage of partyShadeImages() ) paths.push( ROOT + shadeImage );
					for (
						const row of partyOverlay( game, next.entities, h, origin[0], origin[1], options.partyBuffs )
					) {
						const [x, y] = row.position;
						authoredImage(
							{ ...hudData.windows.ifquickpartywnd!.GDR_QPB_SLOT_0!, rect: [ 0, 0, 122, 40 ] },
							x,
							y
						);
						// The authored selection image has an opaque interior. Treat it
						// as row backing so selecting a member cannot cover their data.
						if ( row.entity && row.entity.gid === game.target ) authoredImage( slot.GDR_QPS_SELECT!, x, y );
						authoredText( slot.GDR_QPS_TXT_ID!, x, y, row.member.name );
						authoredGauge(
							"quick-party-hp:" + row.member.id,
							row.member.id,
							slot.GDR_QPS_GAUGE_HP!,
							x,
							y,
							row.hp
						);
						authoredGauge(
							"quick-party-mp:" + row.member.id,
							row.member.id,
							slot.GDR_QPS_GAUGE_MP!,
							x,
							y,
							row.mp
						);
						const race = row.member.country ?? row.entity?.countryByte9c;
						if ( race === 0 || race === 1 ) {
							authoredImage(
								slot.GDR_QPS_PARTY_RACE_MARK!,
								x,
								y,
								ROOT + "interface/ifcommon/com_kindred_" + (race === 0 ? "china" : "europe") + "16.png"
							);
						}
						if ( row.leader ) authoredImage( slot.GDR_QPS_PARTY_LEADER!, x, y );
						quads.push( {
							portraitGid: partyPortraitGid( row.member.id ),
							texture: "__portrait",
							rect: authoredRect( slot.GDR_QPS_PICTURE!, x, y ),
							uv: [ 0, 0, 1, 1 ],
							color: white,
							clip: full
						} );
						// 5BD0A0: a visible member is measured at its live pose, one
						// out of view at its roster record.
						const shade = localPose ?
							partyShadeImage(
								partyDistanceShade( localPose, row.entity ?? partyRosterPose( row.member ) )
							) :
							null;
						if ( shade ) authoredImage( slot.GDR_QPS_STATUS!, x, y, ROOT + shade );
						if ( row.entity ) {
							const r: UiRect = [ x, y, 122, 40 ];
							controls.push( {
								id: "party-target:" + row.entity.gid,
								label: row.member.name,
								kind: "button",
								rect: r,
								selected: row.entity.gid === game.target
							} );
							blocks.push( r );
						}
						// The member's two main masteries: roster data, so they show for a member
						// out of view and without the buff preference.
						for ( const entry of row.masteries ) {
							const mastery = hudData.skillUi.masteries.find( m => m.id === entry.id );
							if ( !mastery ) continue;
							const path = iconPath( mastery.icon ), r: UiRect = [ ...entry.rect ];
							if ( path ) image( r, path );
							controls.push( {
								id: "party-mastery:" + row.member.id + ":" + entry.id,
								label: hudCopy( mastery.name ),
								helpText: hudCopy( mastery.name ),
								kind: "region",
								rect: r
							} );
						}
						// 5BA840 passes no character, so party abnormal cells carry no grade.
						const buff = authoredRect( slot.GDR_QPS_PARTY_BUFF!, x, y ),
							state = partyBuffStates.get( row.member.id );
						if ( options.partyBuffs && row.entity && state ) {
							buffViewer(
								"party-buff:" + row.member.id + ":",
								buffViewerIcons(
									partyBuffViewer(),
									state,
									row.entity.gid,
									skillsOf( game.skillCatalog ),
									{ unlevelled: true }
								),
								buff[0],
								buff[1]
							);
						}
					}
				}
				if ( hudData && panel !== "Chat" && (!compact || compact.overlay === "chat") ) {
					const lines = game?.chat?.lines ?? [],
						key: unknown[] = [
							hudData,
							layoutResourcesRevision,
							w,
							h,
							chatRows,
							compact?.bottom,
							chatTab,
							chatText,
							hover,
							pressed,
							chatScroll.offset(),
							chatHidden,
							caretVisible,
							focus === "chat-text",
							selection[0] ?? 0,
							selection[1] ?? 0,
							composing
						];
					for ( const line of lines ) key.push( line.channel, line.name, line.gid, line.text, line.outgoing );
					const output = chatLayoutCache.read(
						key,
						() =>
							chatLayout( {
								chatTimestamps: experimental.state().saved.chatTimestamps,
								compact: compact ?
									{
										bottom: compact.bottom,
										maxRows: Math.max( 1, Math.floor( (h - compact.bottom - 100) / 56 ) )
									} :
									undefined,
								layout: hudData.chat,
								width: w,
								height: h,
								rows: chatRows,
								tab: chatTab,
								input: chatText,
								lines,
								welcome: hudCopy( "UIIT_STT_STARTING_MSG" ),
								copy: hudCopy,
								size: resources.size,
								text: ( value, r, c, color, style ) => text.quads( value, r, c, color, style ),
								hover,
								pressed,
								offset: chatScroll.offset(),
								hidden: chatHidden,
								measure: value => text.run( value ).width,
								editState: {
									caretVisible,
									caretHeight: text.height() + 2,
									focused: focus === "chat-text",
									start: selection[0] ?? 0,
									end: selection[1] ?? 0,
									composing
								}
							} )
					);
					chatScroll.geometry( output.scrolling );
					quads.push( ...output.quads );
					paths.push( ...output.paths );
					controls.push( ...output.controls );
					blocks.push( ...output.blocks );
				}
				if ( hudData ) {
					if ( !compact ) drawTransientBanners();
					const regionArt = hudData.root.GDR_REGION_INFO_VIEW;
					if ( regionArt?.texture ) paths.push( regionArt.texture );
					const banner = regionBanner.value();
					if ( banner && regionReady ) {
						quads.push(
							...regionBannerQuads(
								banner,
								regionBanner.alpha(),
								w,
								h,
								text.quads,
								text.extentHeight( 4 ),
								regionArt
							)
						);
					}
					const lines = hudMessages.step(
						now,
						hudData.tips,
						game?.progression?.level ?? character?.level ?? 1,
						game?.guide?.country,
						game?.notices ?? [],
						hudCopy,
						options.guide,
						statusFilters
					);
					if ( !compact || compact.overlay === "status" ) {
						const output = statusLayoutCache.read(
							[
								hudData,
								layoutResourcesRevision,
								w,
								h,
								compact?.top,
								statusRows,
								hover,
								pressed,
								statusScroll.offset(),
								!!compact || statusFilterOpen || !options.hideSystemMessages,
								...lines.flatMap( row => [ row.value, row.colorArgb ] )
							],
							() =>
								systemMessageLayout(
									hudData.status,
									w,
									compact ? compact.top + 90 : h,
									compact ? Math.min( statusRows, 1 ) : statusRows,
									lines,
									resources.size,
									( value, r, c, color ) =>
										text.quads( value, r, c, color, {
											fontIndex: 0,
											fontStyle: 0,
											hAlign: 0,
											vAlign: 0
										} ),
									hover,
									pressed,
									statusScroll.offset(),
									value => text.run( value ).width,
									!!compact || statusFilterOpen || !options.hideSystemMessages,
									compact ? { width: Math.min( 353, w - 8 ) } : {}
								)
						);
						statusScroll.geometry( output.scrolling );
						quads.push( ...output.quads );
						paths.push( ...output.paths );
						controls.push( ...output.controls );
						blocks.push( ...output.blocks );
					}
				}
				if ( target && hudData ) {
					// A companion's spawn carries no max HP: native reads its
					// reference's +0x1B0, and an owned one's HP from its record.
					const record = target.kind === "cos" ?
						game?.cosRecords?.find( r => r.gid === target.gid ) :
						undefined;
					const maxHp = target.maxHp ??
						(target.kind === "cos" ? hudData.cosReferences.get( target.refObjId )?.maxHp : undefined);
					// The local character, selected from its portrait, wears the
					// player layout (5814D0 handles the local CICUser too).
					const shown = target.kind === "local-player" ? { ...target, kind: "player" as const } : target;
					// CIFTargetNPC_SetTargetAndLayout (5823B0) shows the 168x4 gauge for
					// every non-combat COS, grab pets included.
					const hp = game?.vitals.find( v => v.gid === target.gid )?.hp ?? record?.hp,
						nativeOutput = targetStatus(
							hudData.targets,
							maxHp === target.maxHp ? shown : { ...shown, maxHp },
							game?.progression?.level ?? character?.level ?? 1,
							hp,
							hudCopy,
							value => text.run( value ).width,
							targetGradeIcon
						);
					const playerRect = hudData.root.GDR_PLAYER_MINI_INFO!.rect;
					const compactOutput = compact && nativeOutput ?
						compactTargetStatus( nativeOutput, shown.kind, w - playerRect[0] - playerRect[2] - 8 ) :
						null;
					const output = compactOutput ?? nativeOutput;
					if ( output ) {
						targetGradeIcon = output.gradeIcon;
						// A narrow target shares the player's row; neither bitmap font is shrunk.
						const tx = compactOutput ? w - output.width - 4 : Math.trunc( (w - output.width) / 2 );
						let ty = compact && !compactOutput && tx < playerRect[0] + playerRect[2] + 4 ?
							playerRect[1] + playerRect[3] + 4 :
							7;
						if ( compact && pet && petPanel && petLayout ) {
							const petRect = authoredRect( petPanel, playerRect[0], playerRect[1] );
							if (
								tx < petRect[0] + petRect[2] + 4 && tx + output.width + 4 > petRect[0] &&
								ty < petRect[1] + petRect[3] + 4 && ty + output.height + 4 > petRect[1]
							) ty = petRect[1] + petRect[3] + 4;
						}
						compactTelemetryTop = ty + output.height + 4;
						targetBottom = Math.max( targetBottom, compactTelemetryTop );
						blocks.push( [ tx, ty, output.width, output.height ] );
						if ( compactOutput ) {
							controls.push( {
								id: "target-info",
								kind: "region",
								label: compactOutput.helpText,
								helpText: compactOutput.helpText,
								rect: [ tx, ty, output.width, output.height ]
							} );
						}
						for ( const image of output.images ) {
							if (
								image.fraction !== undefined && (target.kind === "monster" || target.kind === "cos")
							) authoredGauge( "target-hp", target.gid, image.node, tx, ty, image.fraction );
							else authoredImage( image.node, tx, ty, undefined, image.fraction );
						}
						for ( const row of output.texts ) {
							if ( !compactOutput ) authoredText( row.node, tx, ty, row.value );
							else {
								const box = authoredClientRect( row.node, tx, ty );
								quads.push( ...text.quads( row.value, box, box, row.node.color, {
									fontIndex: row.node.fontIndex,
									hAlign: row.node.hAlign,
									vAlign: row.node.vAlign,
									overflow: "ellipsis"
								} ) );
							}
						}
						authoredButton( output.close, tx, ty, "clear-target", "Clear target" );
						if ( output.remove && structureRemoveAction( target ) ) {
							// The compact layout keeps the delete glyph left of its close.
							const remove = compactOutput ?
								{ ...output.remove, rect: [ output.width - 40, 7, 16, 16 ] as UiRect } :
								output.remove;
							authoredButton( remove, tx, ty, "target-structure-remove", "Remove structure" );
						}
						// 5814D0 moves GDR_TW_BUFF to (window x, frame bottom + 1).
						if ( game && [ "monster", "cos", "player" ].includes( target.kind ) ) {
							const buffStart = controls.length, nativeBuffLayout = targetBuffViewer();
							const buffLayout = compactOutput ?
								{
									...nativeBuffLayout,
									columns: Math.max( 1, Math.floor( output.width / nativeBuffLayout.pitch ) )
								} :
								nativeBuffLayout;
							buffViewer(
								"target-buff:",
								buffViewerIcons(
									buffLayout,
									targetBuffState,
									target.gid,
									skillsOf( game.skillCatalog )
								),
								tx,
								ty + output.height + 1
							);
							for ( let i = buffStart; i < controls.length; i++ ) {
								compactTelemetryTop = Math.max(
									compactTelemetryTop,
									controls[i]!.rect[1] + controls[i]!.rect[3] + 8
								);
							}
							targetBottom = Math.max( targetBottom, compactTelemetryTop );
						}
					}
				}
				if ( compact && hudData ) {
					// Banners own a separate strip below player, pet, target buffs and telemetry.
					targetBottom = drawTransientBanners( Math.max( targetBottom, compactTelemetryTop + 20 ) + 12 );
				}
				if ( target?.kind === "npc" && target.refObjId === 9251 ) {
					button(
						"gacha-open",
						"Magic Pop",
						w / 2 - 110,
						102,
						100,
						game?.inventoryPending || !!game?.targetPending
					);
				}
				const barX = compact ? 0 : Math.trunc( (w - 800) / 2 ), barY = compact ? compact.top : h - 52;
				if ( hudData && !compact ) {
					retainedHud( barLayoutCache, [
						hudData,
						layoutResourcesRevision,
						w,
						h,
						hover,
						pressed,
						focus,
						hotbarPage,
						game?.progression?.experience,
						game?.progression?.level,
						game?.progression?.skillExperience,
						game?.progression?.skillPoints,
						spGauge.current(),
						spGauge.target()
					], () => {
						const bar = hudData.bar, root = hudData.root.GDR_UNDERBAR!;
						authoredImage( root, barX - root.rect[0], barY - root.rect[1] );
						blocks.push( [ barX, barY, 800, 52 ] );
						for ( const name of [ "GDR_DECORATE_1", "GDR_DECORATE_2", "GDR_DECORATE_3" ] ) {
							authoredImage( bar[name]!, barX, barY );
						}
						authoredButton( bar.GDR_BTN_MENU!, barX, barY, "hud-menu", "Menu" );
						authoredButton( bar.GDR_BTN_OPTION!, barX, barY, "toggle-window:System", "System" );
						authoredButton( bar.GDR_BTN_COMMUNITY!, barX, barY, "toggle-window:Guild", "Community" );
						// CIFButton 7 (ub_mall_button, with focus and press art): a
						// region never activates, so the mall opened only from F10.
						authoredButton(
							bar.GDR_BTN_ITEM_MALL!,
							barX,
							barY,
							"item-mall",
							hudCopy( "UIIT_STT_SILKMALL_SHORT_KEY" )
						);
						// CIFUnderBar's message map (initialized by BB2C90): control 13
						// (QUICKSLOTUP) runs 572760, page + 1; control 12 (QUICKSLOTDOWN)
						// runs 572750, page - 1. Both wrap through 571E40.
						authoredButton( bar.GDR_BTN_QUICKSLOTUP!, barX, barY, "hotbar-next", "Next quickslot bar" );
						authoredButton(
							bar.GDR_BTN_QUICKSLOTDOWN!,
							barX,
							barY,
							"hotbar-prev",
							"Previous quickslot bar"
						);
						authoredText( bar.GDR_STATIC_QUICKSLOT!, barX, barY, "F" + (hotbarPage + 1) );
						const required = levels.get( game?.progression?.level ?? 0 )?.[0];
						paths.push( EXPERIENCE_BAR );
						if (
							required && game?.progression?.experience !== undefined && resources.has( EXPERIENCE_BAR )
						) quads.push( ...experienceBar( game.progression.experience, required, barX, barY, full ) );
						if ( game?.progression?.experience !== undefined && game.progression.level !== undefined ) {
							authoredText(
								bar.GDR_STATIC_EXP!,
								barX,
								barY,
								experienceReadout( game.progression.experience, game.progression.level, levels )
							);
						}
						const sp = bar.GDR_GAUGE_SP!;
						paths.push( sp.texture );
						if ( resources.has( sp.texture ) ) {
							quads.push(
								...gaugeFill(
									authoredRect( sp, barX, barY ),
									sp.uv,
									sp.texture,
									spGauge.current(),
									spGauge.target(),
									full
								)
							);
						}
						if ( game?.progression?.skillPoints !== undefined ) {
							authoredSkillPoints( bar.GDR_STATIC_SP!, barX, barY, game.progression.skillPoints );
						}
					} );
				}
				if (
					hudData && game?.cosRecords?.length &&
					(!compact || compact.overlay === "pets" && compact.top > targetBottom)
				) {
					const shown = (game.cosStatusRecords ?? game.cosRecords).map( record =>
						game.cosRecords?.find( current => current.gid === record.gid ) ?? record
					);
					const mark = beginWindow();
					drawCosHud( hudData, shown, barX, barY );
					if ( compact ) {
						fitUiGroup(
							quads,
							controls,
							mark[0],
							mark[1],
							[ 0, targetBottom, w, compact.top - targetBottom ],
							full,
							{
								blocks,
								firstBlock: mark[2],
								disableDrag: compactWindowDrag
							}
						);
					}
				}
				if ( hudData && whispersOpen && (!compact || compact.overlay === "chat") ) {
					const wx = 0,
						wy = compact ?
							Math.max( 80, compact.top - 200 ) :
							h - 52 - (chatRows ? 62 + chatRows * 56 : 20),
						node = hudData.chat.GDR_WHISPERLIST!;
					authoredImage( node, wx, wy );
					const r = authoredRect( node, wx, wy );
					blocks.push( r );
					quads.push(
						...text.quads( hudCopy( "UIIT_CTL_WHISPER_LIST" ), [ r[0], r[1] + 7, r[2], 14 ], full, white, {
							hAlign: 1
						} )
					);
					const names = [
						...new Set(
							(game?.chat?.lines ?? []).filter( line => line.channel === 2 && line.name ).map( line =>
								line.name!
							)
						)
					].slice( -6 );
					names.forEach( ( name, i ) => {
						const row: UiRect = [ r[0] + 8, r[1] + 38 + i * 14, 110, 14 ];
						quads.push( ...text.quads( name, row, r, white ) );
						controls.push( { id: "whisper-name:" + name, label: name, rect: row, kind: "button" } );
					} );
				}
				if ( hudData && statusFilterOpen && (!compact || compact.overlay === "status") ) {
					const sx = w - 357,
						sy = compact ? Math.max( 80, compact.top - 200 ) : h - 90 - (statusRows * 42 + 33),
						node = hudData.status.GDR_SYETEM_MESSAGE_OPTBOARD!,
						r = authoredRect( node, sx, sy );
					authoredImage( node, sx, sy );
					blocks.push( r );
					quads.push(
						...text.quads(
							hudCopy( "UIIT_PAG_CHATTING_SYS_MSG_FILTER" ),
							[ r[0] + 8, r[1] + 16, 131, 14 ],
							r,
							white
						)
					);
					const keys = [ "GAIN", "BATTLE", "STATE", "PARTY", "GAME_SYS" ],
						categories = [ "gain", "fight", "status", "party", "game" ],
						ys = [ 46, 67, 87, 107, 128 ];
					categories.forEach( ( category, i ) => {
						const ty = r[1] + ys[i]!,
							check: UiRect = [ r[0] + 114, ty - 2, 16, 16 ],
							path = ROOT + "interface/ifcommon/com_checkbutton_" +
								(statusFilters.has( category ) ? "on" : "off") + ".png";
						paths.push( path );
						if ( resources.has( path ) ) rect( check, white, path );
						quads.push(
							...text.quads(
								hudCopy( "UIIT_STT_CHATTING_" + keys[i] + "_MSG" ),
								[ r[0] + 16, ty, 93, 14 ],
								r,
								white
							)
						);
						controls.push( {
							id: "status-filter:" + category,
							label: hudCopy( "UIIT_STT_CHATTING_" + keys[i] + "_MSG" ),
							rect: check,
							kind: "button",
							selected: statusFilters.has( category )
						} );
					} );
				}
				/*
				================
				quickSlotCell
				================
				*/
				function quickSlotCell(
					slot: number,
					r: UiRect,
					label: string,
					options: { locked?: boolean; iconAlpha?: number; } = {}
				) {
					const { locked = false, iconAlpha = 1 } = options;
					const binding = game?.quickSlots?.find( row => row.slot === slot ),
						skill = binding?.kind === 0x49 ? training.skill( binding.payload ) : undefined;
					const item = binding ?
						game?.inventory.find( item => item.slot === quickSlotItemSlot( binding ) ) :
						undefined;
					const action = binding?.kind === 0x4a ?
						hudData?.actions.find( a => a.id === (binding.payload & 0xffffff) ) :
						undefined;
					const name = skill ?
						localization.text( skill.nameSymbol, skill.name ) :
						item ?
						(item.name ?? String( item.refObjId )) + " x" + item.quantity :
						action ?
						hudCopy( action.name ) :
						binding?.kind === 0x4e ?
						"Quickslot page " + (binding.payload + 1) :
						binding ?
						"Unavailable" :
						"Empty";
					controls.push( {
						id: "hotbar:" + slot,
						label: label + ": " + name,
						kind: "button",
						rect: r,
						rightActivate: !!binding,
						draggable: !!binding && !locked
					} );
					blocks.push( r );
					const icon = iconPath( skill?.icon ?? item?.icon ?? action?.icon );
					// A held press outlines the slot; a denied one shakes and tints it.
					const feedback = skill ? skillPressFeedback( game, skill.id, r, full, quickslotTime ) : undefined;
					if ( icon ) {
						paths.push( icon );
						const ir: UiRect = feedback?.offsetX ? [ r[0] + feedback.offsetX, r[1], r[2], r[3] ] : r;
						if ( resources.has( icon ) ) rect( ir, [ 1, 1, 1, iconAlpha ], icon );
					}
					const cooldown = skill ?
						skillCooldown( game?.skillCooldowns ?? [], skill.id, skill.cooldownGroup ?? 0, quickslotTime ) :
						null;
					if ( skill ) {
						paths.push( ...quickslotTimerPaths() );
						const timer = quickslotCooldownQuads(
							game?.skillCooldowns ?? [],
							skill.id,
							skill.cooldownGroup ?? 0,
							quickslotTime,
							r,
							full
						);
						quads.push( ...timer.filter( q => resources.has( q.texture ) ) );
						if ( feedback ) quads.push( ...feedback.quads );
					}
					if ( item ) {
						itemEffects( "hotbar:" + slot, item, r );
						paths.push( ...quickslotTimerPaths() );
						quads.push(
							...quickslotItemCooldownQuads(
								game?.itemCooldowns ?? [],
								item,
								quickslotTime,
								r,
								full
							).filter( q => resources.has( q.texture ) )
						);
					}
					itemCount( item, r );
				}
				if ( compact ) {
					const tools = [
						[ "hud-menu", "Menu" ],
						[ "toggle-window:System", "Opts" ],
						[ "toggle-window:Guild", "Guild" ],
						[ "item-mall", "Mall" ],
						[ "compact-chat", "Chat" ],
						[ "compact-status", "Log" ],
						[ "compact-extra", "Extra" ],
						[ "compact-map", "Map" ],
						[ "compact-pets", "Pets" ],
						[ "hotbar-prev", "<" ],
						[ "hotbar-next", ">" ]
					];
					tools.forEach( ( [id, caption], index ) => {
						const r = compact.tools[index]!;
						button( id!, caption!, r[0], r[1] + 8, r[2], false, id === "compact-" + compact.overlay );
						controls[controls.length - 1] = { ...controls[controls.length - 1]!, rect: r };
						blocks.push( r );
					} );
				}
				for ( let n = 0; n <= 10; n++ ) {
					const node = hudData?.bar["GDR_TMPQS_" + n];
					if ( !node ) continue;
					if ( compact ) {
						const r = compact.slots[n]!;
						const backing = hudData!.root.GDR_UNDERBAR!;
						image( [ r[0] + 2, r[1] + 2, 40, 40 ], backing.texture, white, [
							(node.rect[0] - 4) / backing.rect[2],
							(node.rect[1] - 4) / backing.rect[3],
							40 / backing.rect[2],
							40 / backing.rect[3]
						] );
						blocks.push( r );
					}
					quickSlotCell(
						hotbarSlot( hotbarPage, n ),
						compact ?
							[ compact.slots[n]![0] + 6, compact.slots[n]![1] + 6, 32, 32 ] :
							authoredRect( node, barX, barY ),
						n === 0 ? "M" : String( n % 10 )
					);
					const number = hudData!.bar["GDR_QS_NUMBER_" + (n === 0 ? "M" : n % 10)];
					if ( number ) {
						const r = compact?.slots[n];
						authoredImage( number, r ? r[0] + 6 - node.rect[0] : barX, r ? r[1] + 6 - node.rect[1] : barY );
					}
				}
				// The skill that casts next, over shortcut slot 1 (skill-press-feedback.ts).
				const slotOne = hudData?.bar.GDR_TMPQS_1;
				if ( slotOne ) {
					const chip = skillQueueChip(
						game,
						compact ?
							[ compact.slots[1]![0] + 6, compact.slots[1]![1] + 6, 32, 32 ] :
							authoredRect( slotOne, barX, barY ),
						full,
						quickslotTime
					);
					const icon = chip ? iconPath( training.skill( game!.skillQueue!.skill )?.icon ) : undefined;
					if ( chip && icon ) {
						paths.push( icon );
						quads.push( ...chip.under );
						if ( resources.has( icon ) ) rect( chip.icon, [ 1, 1, 1, chip.alpha ], icon );
						quads.push( ...chip.over );
					}
				}
				if ( hudData && (!compact || compact.overlay === "extra") ) {
					const compactMark = beginWindow();
					const layout = hudData.extended[Number( extVertical ) * 2 + Number( extDouble )]!;
					const header = Object.values( layout ).find( n => n.id === 10 )!;
					if ( !extPosition && !compact ) {
						extPosition = windowPlacement.takeRemembered( "extendedQuickslot", w, h, [
							header.rect[2],
							header.rect[3]
						] ) ??
							[ w - header.rect[2] - 26, 181 ];
					}
					const ex = compact ? Math.max( 0, (w - header.rect[2]) / 2 ) : extPosition![0],
						ey = compact ? 80 : extPosition![1],
						alpha = extTransparent ? 110 / 255 : 1;
					controls.push( {
						id: "ext-drag",
						label: "Move extended quickslot bar",
						kind: "button",
						rect: authoredRect( header, ex, ey ),
						draggable: !extPositionLock
					} );
					for ( const node of authoredPaintOrder( layout ) ) {
						// 548700 changes child CTextBoard alpha. 5405D0 resets ALPHAOP
						// before 568420 draws cooldown/quantity overlays, so those stay independent.
						if ( node.id >= 100 ) {
							if ( extOpen ) {
								quickSlotCell(
									extendedSlot( node.id - 100 ),
									authoredRect( node, ex, ey ),
									String( node.id - 99 ),
									{ locked: extSlotLock, iconAlpha: alpha }
								);
							}
							continue;
						}
						const firstQuad = quads.length;
						if ( node.id >= 20 ) { if ( extOpen ) authoredImage( node, ex, ey ); }
						else if ( node.id === 10 ) authoredImage( node, ex, ey );
						else if ( node.id >= 11 && node.id <= 14 ) {
							const ids = [ "ext-options", "ext-horizontal", "ext-vertical", "ext-open" ],
								labels = [
									"Extended quickslot options",
									"Horizontal quickslots",
									"Vertical quickslots",
									extOpen ? "Collapse quickslots" : "Expand quickslots"
								];
							authoredButton( node, ex, ey, ids[node.id - 11]!, labels[node.id - 11]! );
						}
						if ( extTransparent ) {
							for ( let i = firstQuad; i < quads.length; i++ ) {
								const q = quads[i]!;
								quads[i] = { ...q, color: [ q.color[0], q.color[1], q.color[2], q.color[3] * alpha ] };
							}
						}
					}
					if ( extOptions ) {
						const option = hudData.windows.ifextquickslotoption!,
							frame = Object.values( hudData.extended[4]! )[0]!,
							ox = Math.max(
								0,
								Math.min( w - 290, extVertical ? (ex < w / 2 ? ex + header.rect[2] : ex - 290) : ex )
							),
							oy = Math.max(
								0,
								Math.min( h - 223, extVertical ? ey : ey < h / 2 ? ey + header.rect[3] : ey - 223 )
							);
						nativeFrame( frame, ox, oy, hudCopy( frame.text ), "ext-options-close" );
						for ( const node of authoredPaintOrder( option ) ) {
							const index = node.id - 25;
							if ( index >= 0 && index < 4 ) {
								const values = extDraft,
									ids = [ "ext-transparent", "ext-slot-lock", "ext-position-lock", "ext-double" ];
								const r = authoredRect( node, ox, oy ),
									path = ROOT + "interface/ifcommon/com_checkbutton_" +
										(values[index] ? "on" : "off") + ".png";
								image( r, path );
								controls.push( {
									id: ids[index]!,
									label: [
										"Transparency",
										"Lock shortcuts",
										"Lock position",
										"Two rows or columns"
									][index]!,
									kind: "button",
									rect: r,
									selected: values[index]
								} );
							} else if ( node.id === 35 || node.id === 36 ) {
								authoredLabeledButton(
									node,
									ox,
									oy,
									node.id === 35 ? "ext-options-ok" : "ext-options-apply",
									hudCopy( node.text )
								);
							} else authoredChrome( node, ox, oy );
						}
					}
					if ( compact ) {
						fitUiGroup(
							quads,
							controls,
							compactMark[0],
							compactMark[1],
							[ 0, 80, w, Math.max( 1, compact.top - 80 ) ],
							full,
							{ blocks, firstBlock: compactMark[2], disableDrag: compactWindowDrag }
						);
					}
				}
				if ( clearHotbar ) {
					button(
						"hotbar-clear",
						clearHotbar ? "Done" : "Clear slot",
						compact ? w - 84 : barX + 652,
						barY - 40,
						80,
						false,
						clearHotbar
					);
				}
				// The current map is a world-lifetime resource, not a popup-lifetime
				// resource. Closing M must not release the image and flash on the next open.
				if ( mapFollow && pose ) {
					mapPage = worldMapPageAt( pose );
					mapPan = [ 0, 0 ];
					mapCenter = null;
				}
				const mapWidth = mapSmall ? 268 : WORLD_MAP_WIDTH, mapHeight = mapSmall ? 296 : WORLD_MAP_HEIGHT;
				const mapLeft = Math.min( mapX, Math.max( 0, w - mapWidth ) ),
					mapTop = Math.min( mapY, Math.max( 0, h - mapHeight ) );
				mapX = mapLeft;
				mapY = mapTop;
				const mapHits: UiControl[] = [];
				// 57FE60's marker passes, in its order: quest NPCs (57B1C0), hunting points
				// (57B550), then the apprenticeship and party rosters (57CE80). Each is a
				// 16x16 quad centred on the projected world position; the world map has no
				// 47px ring, so unlike the minimap there is no edge clamp and no arrow
				// sprite swap, and off-page members are simply clipped by the window.
				const mapMarkers: MapMarker[] = [];
				if ( pose ) {
					for ( const id of game?.quests?.find( q => q.refId === trackedQuest )?.targetIds ?? [] ) {
						const target = minimapResources.positions()?.get( id );
						if ( target && minimapSameFloor( pose, target, game ?? null ) ) {
							mapMarkers.push( {
								regionId: target.regionId,
								x: target.x,
								z: target.z,
								rotation: 0,
								path: ROOT + "interface/worldmap/wmap_sign_questnpc.png"
							} );
						}
					}
					for ( const point of game?.huntingPoints ?? [] ) {
						const live = next.entities.find( e => e.gid === point.gid ) ?? point;
						mapMarkers.push( {
							regionId: live.regionId,
							x: live.x,
							z: live.z,
							rotation: -point.angle * Math.PI * 2 / 65536,
							path: ROOT + "interface/worldmap/wmap_sign_huntingpoint.png"
						} );
					}
					// Beta operator roster (port-only, beta-map.ts): every other online
					// player, drawn before the party pass so party signs stay on top.
					for ( const player of game?.betaPlayers ?? [] ) {
						if ( player.gid === game?.localGid ) continue;
						mapMarkers.push( {
							regionId: player.regionId,
							x: player.x,
							z: player.z,
							rotation: 0,
							path: ROOT + "interface/minimap/mm_sign_otherplayer.png"
						} );
					}
					for ( const row of rosterPositions( pose, game ?? null, next.entities ) ) {
						mapMarkers.push( {
							regionId: row.regionId,
							x: row.x,
							z: row.z,
							rotation: 0,
							path: ROOT + "interface/worldmap/wmap_sign_" + row.kind + ".png"
						} );
					}
				}
				// A closed map only demands its images, so opening it is instant: the
				// demand is computed without the projection (worldMapDemand). Its label
				// glyphs (the largest allocation of a HUD step) and click hits are built
				// while it is open. The font atlas is demanded either way.
				const mapOpen = panel === "Map";
				// The teleport's inverse pick must track the painted projection: it
				// is only valid while the map is open and a pose exists, so record the
				// frame under the same condition that builds the projection.
				if ( mapOpen && pose ) {
					mapTeleport.view(
						mapPage,
						[ mapLeft + 6, mapTop + 34, mapWidth - 12, mapHeight - 40 ],
						mapPan,
						mapCenter ?? pose
					);
				}
				const mapProjection = pose && mapOpen ?
					worldMapPresentation(
						pose,
						mapPage,
						[ mapLeft + 6, mapTop + 34, mapWidth - 12, mapHeight - 40 ],
						mapPan,
						mapCenter ?? pose,
						hudData?.mapLabels,
						hudData?.mapIcons,
						mapMarkers
					) :
					null;
				// MANUAL drags (AUTO swallows them, 57F43D) move the stored position by the
				// clamped delta (579920); keep the displayed pan so reversing never has to
				// unwind an overshoot first.
				if ( mapProjection && !mapFollow ) mapPan = mapProjection.pan;
				// 57E9A0 builds the 6200 overlay in ascending record id, so the SN_ labels
				// (11xxx-14xxx) enter before the kind-2 icons (92xxx) and the icons paint
				// over them; 57FE60 then runs every marker pass after that whole overlay.
				// Paint background, label glyphs, icons, markers - the arrow is last and
				// can never be hidden by a shop icon or a zone label.
				// 57BBB0 traverses icons first, then labels. 57ED01 centres each label by
				// its own font extent, subtracting the integer half-width from the anchor.
				const fontPath = text.path();
				if ( !mapOpen && fontPath ) paths.push( fontPath );
				const mapImages = mapProjection ?
					[
						...mapProjection.background,
						...mapProjection.overlay,
						...(mapOpen ? mapProjection.labels : []).flatMap( ( { label: entry, x, y, clip } ) => {
							const width = text.run( entry.text, 0, entry.font ).width,
								height = text.extentHeight( entry.font ),
								left = x - Math.floor( width / 2 );
							if ( !mapLabelVisible( [ left, y, width, height ], clip ) ) return [];
							return text.quads(
								entry.text,
								[ left, y, width, height ],
								clip,
								entry.color,
								{ fontIndex: entry.font, hAlign: 0, vAlign: 0 }
							);
						} ),
						...mapProjection.markers
					] :
					[];
				for ( const { icon, rect: r } of mapOpen ? mapProjection?.hits ?? [] : [] ) {
					const page = worldMapPages().find( p => p.id === icon.destination );
					if ( page ) {
						mapHits.push( {
							id: "map-town:" + page.id,
							label: hudCopy( "UIIT_STT_" + page.name ),
							rect: r,
							kind: "button",
							draggable: true
						} );
					}
				}
				if ( mapOpen ) paths.push( ...mapImages.map( q => q.texture ) );
				else if ( pose ) {
					worldMapDemand(
						mapPage,
						[ mapLeft + 6, mapTop + 34, mapWidth - 12, mapHeight - 40 ],
						mapPan,
						mapCenter ?? pose,
						hudData?.mapIcons ?? [],
						paths
					);
					for ( const row of mapMarkers ) paths.push( row.path );
					paths.push( MAP_LOCAL_MARKER );
				}
				if ( panel === "Map" && hudData ) {
					// Map tiles and icons stream in as a pan reveals them (demanded above, and
					// absent textures simply do not draw). Gating admission on them disabled
					// map-pan mid-drag, which cancelled the drag. Chrome stays gated.
					const admission = beginWindow();
					const mw = mapWidth, mh = mapHeight, mx = mapLeft, my = mapTop;
					if ( mapFollow && pose ) {
						mapPage = worldMapPageAt( pose );
						mapPan = [ 0, 0 ];
						mapCenter = null;
					}
					const page = worldMapPages().find( row => row.id === mapPage ),
						caption = hudCopy( page ? "UIIT_STT_" + page.name : "UIIT_PAG_WORLDMAP" ) || "World Map";
					windowBox( caption, mx, my, mw, mh );
					const inner: UiRect = [ mx + 6, my + 34, mw - 12, mh - 40 ];
					// The map surface is CIFWorldMap itself, not a CIFButton: pressing or
					// dragging it never activates or plays SND_BUTTON_CLICK.
					controls.push( { id: "map-pan", label: "Pan map", kind: "region", draggable: true, rect: inner }, {
						id: "map-drag",
						label: "Move map",
						kind: "button",
						draggable: true,
						rect: [ mx + 20, my + 4, mw - 96, 24 ]
					} );
					quads.push( ...mapImages );
					controls.push( ...mapHits );
					const nodes = hudData.map;
					authoredButton(
						{ ...nodes.GDR_WM_BTN_WNDSIZE!, rect: [ mw - 44, 10, 16, 16 ] },
						mx,
						my,
						"map-size",
						"Change map size"
					);
					if ( mapPage !== 0 ) {
						authoredButton(
							{ ...nodes.GDR_WM_BTN_TO_WMAP!, rect: [ mw - 62, 10, 16, 16 ] },
							mx,
							my,
							"map-world",
							"World map"
						);
					}
					if ( !mapSmall ) {
						const node = nodes.GDR_WM_BTN_AUTO_MOVE!,
							caption = hudCopy(
								mapFollow ? "UIIT_STT_WORLDMAP_AUTO_MOVE" : "UIIT_STT_WORLDMAP_MANUAL_MOVE"
							);
						authoredLabeledButton( node, mx, my, "map-follow", caption );
					}
					closeButton( mx + mw - 25, my + 10 );
					endWindow( admission );
				}
				if ( hudData && (!compact || compact.overlay === "map") ) {
					const mapMark = [ quads.length, controls.length, blocks.length ] as const;
					const root = hudData.root.GDR_MINIMAP!,
						nodes = hudData.minimap,
						mx = Math.max( 0, w - 129 ),
						my = compact ? Math.max( hudData.root.GDR_PLAYER_MINI_INFO!.rect[3] + 4, targetBottom ) : 0;
					hudCorner = [ mx, my, root.rect[2], root.rect[3] ];
					authoredImage( root, mx - root.rect[0], my - root.rect[1] );
					blocks.push( hudCorner );
					const inner = authoredRect( nodes.GDR_MINIMAP_ALPHA!, mx, my ),
						mask = { texture: nodes.GDR_MINIMAP_ALPHA!.texture, rect: inner };
					paths.push( mask.texture );
					if ( pose ) {
						const cx = inner[0] + inner[2] / 2, cy = inner[1] + inner[3] / 2;
						/*
						================
						sprite
						================
						*/
						const sprite = ( r: UiRect, path: string, rotation = 0 ) => {
							paths.push( path );
							if ( resources.has( path ) && resources.has( mask.texture ) ) {
								quads.push( {
									rect: r,
									texture: path,
									uv: [ 0, 0, 1, 1 ],
									color: white,
									clip: inner,
									mask,
									rotation
								} );
							}
						};
						const floor = minimapResources.dungeons()?.get( pose.regionId )?.find( f =>
							f.floorIndex === game?.navigationFloor
						);
						for ( const tile of minimapTiles( pose, minimapZoom, floor, minimapResources.art() ) ) {
							sprite( [ cx + tile.x, cy + tile.y, minimapZoom, minimapZoom ], tile.path );
						}
						const markers = [ ...minimapMarkers( pose, game, next.entities, minimapZoom, inner[2] / 2 ) ];
						for ( const id of game?.quests?.find( q => q.refId === trackedQuest )?.targetIds ?? [] ) {
							const target = minimapResources.positions()?.get( id );
							if ( !target || !minimapSameFloor( pose, target, game ?? null ) ) continue;
							const offset = minimapOffset( pose, target, minimapZoom );
							if ( offset ) markers.push( minimapEdge( offset, "quest" ) );
						}
						markers.push( ...minimapHunting( pose, game, next.entities, minimapZoom ) );
						for ( const marker of markers ) {
							sprite(
								[
									cx + marker.x - marker.size / 2,
									cy + marker.y - marker.size / 2,
									marker.size,
									marker.size
								],
								marker.path,
								marker.rotation
							);
						}
						sprite(
							[ cx - 8, cy - 8, 16, 16 ],
							ROOT + "interface/minimap/mm_sign_character.png",
							minimapRotation( pose.angle )
						);
					}
					if ( pose ) {
						const [xText, yText] = minimapCoordinates( pose );
						authoredText(
							nodes.GDR_MINIMAP_TEXT_AREANAME!,
							mx,
							my,
							hudData.zones[String( pose.regionId )] ?? ""
						);
						authoredText( nodes.GDR_MINIMAP_TEXT_POS_X!, mx, my, xText );
						authoredText( nodes.GDR_MINIMAP_TEXT_POS_Y!, mx, my, yText );
						const floor = minimapResources.dungeons()?.get( pose.regionId )?.find( f =>
								f.floorIndex === game?.navigationFloor
							),
							node = nodes.GDR_MINIMAP_DUNGEON_FLOOR_INFO;
						if ( floor && node ) {
							authoredImage( node, mx, my );
							authoredText(
								node,
								mx,
								my,
								hudData.zones[floor.floorLabel] ?? hudData.strings[floor.floorLabel] ?? ""
							);
						}
					}
					authoredButton( nodes.GDR_MINIMAP_ZOOMIN!, mx, my, "minimap-in", "Zoom in" );
					authoredButton( nodes.GDR_MINIMAP_ZOOMOUT!, mx, my, "minimap-out", "Zoom out" );
					authoredButton( nodes.GDR_MINIMAP_BTN_TOG_MAP!, mx, my, "toggle-window:Map", "Map" );
					if ( compact ) {
						fitUiGroup(
							quads,
							controls,
							mapMark[0],
							mapMark[1],
							[ 0, my, w, Math.max( 1, compact.top - my ) ],
							full,
							{ blocks, firstBlock: mapMark[2], sourceBounds: hudCorner }
						);
					}
				}
				if ( panel === "System" && hudData ) {
					const admission = beginWindow();
					const sx = systemX ?? Math.trunc( (w - 214) / 2 ),
						sy = systemY ?? Math.trunc( (h - SYSTEM_MENU_HEIGHT) / 2 );
					systemX = sx;
					systemY = sy;
					const authored = hudData.windows.ifsystemwnd!,
						layout = systemMenu( authored, sx, sy ),
						inner = authored.GDR_SYSTEM_FRAME!,
						tile = authored.GDR_SYSTEM_BGTILE!;
					blocks.push( layout.frame );
					controls.push( {
						id: "system-drag",
						label: hudCopy( "UIIT_PAG_SYSTEM" ),
						kind: "button",
						draggable: true,
						rect: [ sx + 10, sy, 193, 34 ]
					} );
					paths.push( tile.texture, ...PARTS.map( p => inner.texture + p + ".png" ) );
					quads.push(
						...frameRing(
							layout.frame,
							FRAME,
							PARTS.map( p => resources.size( FRAME + p + ".png" ) ),
							full
						)
					);
					quads.push( ...normalTile( layout.tile, tile.texture, resources.size( tile.texture ), full ) );
					quads.push(
						...frameRing(
							layout.inner,
							inner.texture,
							PARTS.map( p => resources.size( inner.texture + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads( hudCopy( "UIIT_PAG_SYSTEM" ), [ sx + 10, sy + 12, 193, 12 ], full, white, {
							hAlign: 1,
							vAlign: 0
						} )
					);
					closeButton( sx + 188, sy + 10 );
					for ( const node of layout.buttons ) {
						const id = node.id === EXPERIMENTAL_MENU_ID ? "open-window:Experimental" : node.id === 10 ?
							"open-window:Option" :
							node.id === 11 ?
							"open-window:Game Guide" :
							node.id === 13 ?
							"system-restart" :
							"system-exit";
						authoredLabeledButton(
							node,
							sx,
							sy,
							id,
							node.id === EXPERIMENTAL_MENU_ID ? "Experimental" : hudCopy( node.text )
						);
					}
					endWindow( admission );
				}
				if ( panel === "Auto Potion" && hudData ) {
					const admission = beginWindow();
					const [px, py] = windowOrigin( "Auto Potion", [
							Math.max( 0, (w - 394) / 2 ),
							Math.max( 0, (h - 520) / 2 ),
							394,
							520
						] ),
						layout = hudData.windows.ifautopotion!,
						slot = hudData.windows.ifautopotionslot!;
					windowBox( hudCopy( "UIIT_STT_MACROPOTION" ) || "Auto Potion", px, py, 394, 520 );
					closeButton( px + 369, py + 10 );
					const bg = layout.GDR_AUTO_POTION_BG_1!;
					paths.push( bg.texture );
					quads.push(
						...normalTile( authoredRect( bg, px, py ), bg.texture, resources.size( bg.texture ), full )
					);
					for ( const name of [ "GDR_AUTO_POTION_FRAME_1", "GDR_AUTO_POTION_FRAME_2" ] ) {
						const node = layout[name]!, r = authoredRect( node, px, py );
						paths.push( ...PARTS.map( p => node.texture + p + ".png" ) );
						quads.push(
							...frameRing(
								r,
								node.texture,
								PARTS.map( p => resources.size( node.texture + p + ".png" ) ),
								full
							)
						);
					}
					const desc = layout.GDR_AUTO_POTION_DESC_PML!,
						descRect = authoredRect( desc, px, py ),
						descOut = text.guide(
							guideTokens( hudCopy( desc.text ) ),
							descRect,
							descRect,
							desc.color,
							resources.size
						);
					quads.push( ...descOut.quads );
					paths.push( ...descOut.paths );
					/*
					================
					check
					================
					*/
					const check = (
						node: AuthoredControl,
						x: number,
						y: number,
						id: string,
						enabled: boolean,
						caption: string
					) => {
						const r = authoredRect( node, x, y ),
							path = ROOT + "interface/ifcommon/com_checkbutton_" + (enabled ? "on" : "off") + ".png";
						image( r, path );
						controls.push( { id, label: caption, rect: r, kind: "button", selected: enabled } );
					};
					const combos: { key: string; part: string; r: UiRect; selected: number; count: number; }[] = [];
					for ( const key of [ "hp", "mp", "cure" ] as const ) {
						const entry = autoPotionEntry( potionDraft[key] ),
							page = key === "cure" ? potionDraft.curePage : Math.floor( (entry.slot - 1) / 10 ),
							n = key === "cure" ? potionDraft.cureKey + 1 : (entry.slot - 1) % 10 + 1;
						if ( key !== "cure" ) {
							const parent = layout["GDR_AUTO_POTION_SLOT_" + key.toUpperCase()]!,
								sx = px + parent.rect[0],
								sy = py + parent.rect[1];
							check(
								slot.GDR_AUTOPOTION_SLOT_CHECKBOX!,
								sx,
								sy,
								"potion-enable:" + key,
								entry.enabled,
								key.toUpperCase()
							);
							authoredText( slot.GDR_AUTOPOTION_SLOT_NAME!, sx, sy, key.toUpperCase() );
							authoredImage( slot.GDR_AUTOPOTION_SLOT_DATA_BOX!, sx, sy );
							authoredText( slot.GDR_AUTOPOTION_SLOT_DATA!, sx, sy, String( entry.percent ) );
							authoredText( slot.GDR_AUTOPOTION_SLOT_DATA_UNIT_STA!, sx, sy, "%" );
							for ( const tail of [ "QUICKSLOT_STA", "QUICKSLOT_BELT_STA" ] ) {
								const node = slot["GDR_AUTOPOTION_SLOT_" + tail]!;
								authoredText( node, sx, sy, hudCopy( node.text ) );
							}
							const rail = slot.GDR_AUTOPOTION_SLOT_SLIDERCTRL!, r = authoredRect( rail, sx, sy );
							authoredImage( rail, sx, sy );
							for ( const delta of [ -1, 1 ] ) {
								controls.push( {
									id: "potion-step:" + key + ":" + delta,
									label: key.toUpperCase() + (delta < 0 ? " decrease" : " increase"),
									kind: "button",
									rect: [ delta < 0 ? r[0] : r[0] + r[2] - 20, r[1] + 2, 20, 16 ],
									disabled: !entry.enabled || (delta < 0 ? entry.percent <= 1 : entry.percent >= 100)
								} );
							}
							controls.push( {
								id: "potion-percent:" + key,
								disabled: !entry.enabled,
								label: key.toUpperCase() + " threshold",
								kind: "range",
								rect: [ r[0] + 20, r[1] + 2, 292, 16 ],
								min: 1,
								max: 100,
								value: String( entry.percent )
							} );
							const thumb = ROOT + "interface/ifcommon/com_scroll_button.png";
							paths.push( thumb );
							if ( entry.enabled && resources.has( thumb ) ) {
								rect(
									[ r[0] + 20 + (Math.max( 1, entry.percent ) - 1) / 99 * 276, r[1] + 2, 16, 16 ],
									white,
									thumb
								);
							}
							for (
								const [tail, value] of [ [ "MIN", "1" ], [ "CENTER", "50" ], [ "MAX", "100" ] ] as const
							) authoredText( slot["GDR_AUTOPOTION_SLOT_" + tail + "_STA"]!, sx, sy, value );
							combos.push( {
								key,
								part: "page",
								r: authoredRect( slot.GDR_AUTOPOTION_SLOT_QUICKSLOT_BELT_COMBOBOX!, sx, sy ),
								selected: page,
								count: 4
							}, {
								key,
								part: "key",
								r: authoredRect( slot.GDR_AUTOPOTION_SLOT_QUICKSLOT_COMBOBOX!, sx, sy ),
								selected: n,
								count: 10
							} );
						} else {
							check(
								layout.GDR_AUTO_POTION_ABNORMAL_CHECKBOX!,
								px,
								py,
								"potion-enable:cure",
								entry.enabled,
								hudCopy( "UIIT_STT_MACROPOTION_ABNORMAL" )
							);
							for ( const tail of [ "NAME", "QUICKSLOT_STA", "QUICKSLOT_BELT_STA" ] ) {
								const node = layout["GDR_AUTO_POTION_ABNORMAL_" + tail]!;
								authoredText( node, px, py, hudCopy( node.text ) );
							}
							combos.push( {
								key,
								part: "page",
								r: authoredRect( layout.GDR_AUTO_POTION_ABNORMAL_QUICKSLOT_BELT_COMBOBOX!, px, py ),
								selected: page,
								count: 4
							}, {
								key,
								part: "key",
								r: authoredRect( layout.GDR_AUTO_POTION_ABNORMAL_QUICKSLOT_COMBOBOX!, px, py ),
								selected: n,
								count: 10
							} );
						}
					}
					check(
						layout.GDR_AUTO_POTION_DELAY_CHECKBOX!,
						px,
						py,
						"potion-delay",
						!!(potionDraft.timing & 128),
						hudCopy( "UIIT_STT_MACROPOTION_DELAY" )
					);
					authoredText( layout.GDR_AUTO_POTION_DELAY_NAME!, px, py, hudCopy( "UIIT_STT_MACROPOTION_DELAY" ) );
					/*
					================
					spinState
					================
					*/
					const delayRect = authoredRect( layout.GDR_AUTO_POTION_DELAY_VERTSPINCTRL!, px, py ),
						spinState = ( id: string ) => pressed === id && hover === id ? 2 : hover === id ? 1 : 0,
						spin = verticalSpinChrome(
							delayRect,
							resources.size,
							full,
							spinState( "potion-time-up" ),
							spinState( "potion-time-down" )
						);
					paths.push( ...spin.paths );
					quads.push(
						...spin.quads,
						...text.quads( ((potionDraft.timing & 127) / 10).toFixed( 1 ), spin.textRect, full, white, {
							hAlign: 0,
							vAlign: 1
						} )
					);
					controls.push(
						{
							id: "potion-time",
							label: hudCopy( "UIIT_STT_MACROPOTION_DELAY" ),
							kind: "region",
							rect: spin.textRect,
							value: ((potionDraft.timing & 127) / 10).toFixed( 1 )
						},
						{ id: "potion-time-up", label: "Increase potion delay", kind: "button", rect: spin.up },
						{ id: "potion-time-down", label: "Decrease potion delay", kind: "button", rect: spin.down }
					);
					for ( const [name, id] of [ [ "OK", "potion-save" ], [ "CANCEL", "potion-cancel" ] ] as const ) {
						const node = layout["GDR_AUTO_POTION_" + name + "_BTN"]!;
						authoredLabeledButton( node, px, py, id, hudCopy( node.text ) );
					}
					for ( const combo of combos ) {
						comboBox(
							combo.r,
							"potion-combo:" + combo.key + ":" + combo.part,
							combo.key + " " + combo.part,
							combo.part === "page" ?
								combo.selected < 0 ? "" : "F" + (combo.selected + 1) :
								combo.selected === 0 ?
								"" :
								String( combo.selected % 10 )
						);
					}
					const open = combos.find( combo => potionCombo === "potion-combo:" + combo.key + ":" + combo.part );
					if ( open ) {
						const r = open.r,
							first = open.count > 4 ? Math.min( open.count - 4, Math.round( potionComboOffset ) ) : 0,
							visible = Math.min( 4, open.count ),
							list: UiRect = [ r[0], r[1] + 20, r[2], visible * 18 ];
						rect( list, [ 0, 0, 0, 1 ] );
						blocks.push( list );
						for ( let i = first; i < first + visible; i++ ) {
							const value = open.part === "page" ? i : i + 1,
								cell: UiRect = [
									r[0],
									list[1] + (i - first) * 18,
									r[2] - (open.count > 4 ? 16 : 0),
									18
								];
							if ( value === open.selected ) rect( cell, [ .2, .25, .3, 1 ] );
							quads.push(
								...text.quads(
									open.part === "page" ? "F" + (i + 1) : String( value % 10 ),
									[ cell[0] + 4, cell[1], cell[2] - 8, cell[3] ],
									cell,
									white
								)
							);
							controls.push( {
								id: "potion-choice:" + open.key + ":" + open.part + ":" + value,
								label: open.key + " " + open.part + " " + value,
								rect: cell,
								kind: "button",
								selected: value === open.selected
							} );
						}
						if ( open.count > 4 ) {
							const scroll = chatScrollbar(
								"potion-combo",
								[ list[0] + list[2] - 16, list[1] + 16, 16, list[3] - 48 ],
								open.count,
								4,
								open.count - 4 - first,
								resources.size,
								full,
								hover,
								pressed
							);
							paths.push( ...scroll.paths );
							quads.push( ...scroll.quads );
							controls.push(
								...scroll.controls.map( c => ({
									...c,
									disabled: c.id.endsWith( "-up" ) ?
										first === 0 :
										c.id.endsWith( "-down" ) ?
										first + 4 === open.count :
										false
								}) )
							);
						}
					}
					endWindow( admission );
				}
				if ( panel === "Experimental" && hudData ) {
					const admission = beginWindow();
					// Options-style tabs over one framed list: a header naming the tab,
					// then a checkbox or numeric selection with its help line below it.
					// The window grows with the selected tab, as CIFOption::OnTab does.
					const { tab, draft } = experimental.state(), page = EXPERIMENTAL_TABS[tab]!;
					const rowPitch = 46, listTop = 98, width = 386;
					const listHeight = page.rows.length * rowPitch + 12, height = listTop + listHeight + 50;
					const [px, py] = windowOrigin( "Experimental", [
						(w - width) / 2,
						(h - height) / 2,
						width,
						height
					] );
					const layout = hudData.windows.ifoption!, slot = hudData.windows.ifgameoptionslot!;
					windowBox( "Experimental", px, py, width, height );
					closeButton( px + width - 26, py + 10 );
					const tabWidth = 78, tabStart = (width - (EXPERIMENTAL_TABS.length * tabWidth - 2)) / 2;
					for ( let i = 0; i < EXPERIMENTAL_TABS.length; i++ ) {
						nativeTab(
							"experimental-tab:" + i,
							EXPERIMENTAL_TABS[i]!.title,
							[ px + tabStart + i * tabWidth, py + 40, tabWidth - 2, 24 ],
							tab === i,
							{ family: "com_tab", client: [ 0, 9, 0, 6 ] }
						);
					}
					authoredChrome( { ...layout.GDR_OPTION_BGTILE!, rect: [ 27, 78, 332, height - 140 ] }, px, py );
					authoredChrome(
						{ ...layout.GDR_OPTION_WND_GAME!, type: "CIFFrame", rect: [ 11, 62, 364, height - 108 ] },
						px,
						py
					);
					// Browser-only section reuses the native Set Game header and inset frame.
					const section = hudData.windows.ifoption_game!.GDR_GAME_OPTION_TAB_1!;
					authoredImage( { ...section, rect: [ 25, 70, 196, 28 ] }, px, py );
					authoredText( { ...section, rect: [ 25, 70, 196, 28 ] }, px, py, page.section );
					authoredChrome(
						{
							...hudData.windows.ifoption_game!.GDR_GAME_OPTION_SCROLLMANAGER_1!,
							rect: [ 25, listTop, 336, listHeight ]
						},
						px,
						py
					);
					for ( let i = 0; i < page.rows.length; i++ ) {
						const row = page.rows[i]!, top = listTop + 8 + i * rowPitch;
						authoredText(
							{
								...slot.GDR_GAME_OPTION_SLOT_STA1!,
								rect: [ 39, top + 4, row.key === "renderScale" ? 130 : 270, 16 ],
								client: [ 0, 2, 0, 0 ]
							},
							px,
							py,
							row.label
						);
						if ( row.key === "renderScale" ) {
							const choiceWidth = 54, choicePitch = 56, choiceLeft = 177;
							for ( const [index, scale] of renderScales().entries() ) {
								button(
									row.id + ":" + scale,
									scale + "%",
									px + choiceLeft + index * choicePitch,
									py + top,
									choiceWidth,
									false,
									draft.renderScale === scale
								);
							}
						} else {
							const enabled = draft[row.key];
							image(
								[ px + 331, py + top + 4, 16, 16 ],
								ROOT + "interface/ifcommon/com_checkbutton_" + (enabled ? "on" : "off") + ".png"
							);
							controls.push( {
								id: row.id,
								label: row.label,
								kind: "button",
								rect: [ px + 35, py + top, 316, 40 ],
								selected: enabled
							} );
						}
						authoredText(
							{
								...slot.GDR_GAME_OPTION_SLOT_STA1!,
								rect: [ 39, top + (row.key === "renderScale" ? 26 : 22), 304, 16 ],
								client: [ 0, 0, 0, 0 ],
								color: [ 180 / 255, 180 / 255, 180 / 255, 1 ]
							},
							px,
							py,
							row.description
						);
					}
					for (
						const [index, key, id] of [ [ 0, "DEF", "default" ], [ 1, "OK", "confirm" ], [
							2,
							"CANC",
							"cancel"
						] ] as const
					) {
						const node = layout["GDR_OPTION_BTN_" + key]!;
						authoredLabeledButton(
							{ ...node, rect: [ 52 + index * 103, height - 34, ...node.size ] },
							px,
							py,
							"experimental-" + id,
							hudCopy( node.text )
						);
					}
					endWindow( admission );
				}
				if ( panel === "Option" && hudData ) {
					const admission = beginWindow();
					// CIFOption::OnTab 5C92A0 changes height, background and button positions.
					const geometry = [
						{ height: 413, tile: [ 27, 130, 332, 230 ], row: 379, x: [ 29, 113, 197 ] },
						{ height: 319, tile: [ 27, 100, 332, 165 ], row: 285, x: [ 52, 155, 258 ] },
						{ height: 312, tile: [ 27, 78, 332, 180 ], row: 278, x: [ 52, 155, 258 ] },
						{ height: 413, tile: [ 27, 103, 332, 256 ], row: 379, x: [ 52, 155, 258 ] },
						{ height: 415, tile: [ 27, 78, 332, 281 ], row: 380, x: [ 52, 155, 258 ] }
					][optionTab]!;
					const [px, py] = windowOrigin( "Option", [
							optionOrigin()[0],
							optionOrigin()[1],
							386,
							geometry.height
						] ),
						layout = hudData.windows.ifoption!,
						gameLayout = hudData.windows.ifoption_game!,
						slotLayout = hudData.windows.ifgameoptionslot!;
					blocks.push( [ px, py, 386, geometry.height ] );
					quads.push(
						...frameRing(
							[ px, py, 386, geometry.height ],
							FRAME,
							PARTS.map( p => resources.size( FRAME + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads( hudCopy( "UIIT_PAG_OPTION" ), [ px + 10, py + 12, 365, 12 ], full, white, {
							hAlign: 1,
							vAlign: 0
						} )
					);
					closeButton( px + 360, py + 10 );
					const tile = { ...layout.GDR_OPTION_BGTILE!, rect: geometry.tile as unknown as UiRect };
					authoredChrome( tile, px, py );
					const tabs = [ "VIDEO", "AUDIO", "CAMERA", "INPUT", "GAME" ];
					// 5C9D7B sets (0,9,0,6) insets; 53FB10 changes format flags, not alignment.
					// CTextBoard 5404AF/5404BB keeps both axes centered.
					for ( let i = 0; i < 5; i++ ) {
						nativeTab(
							"option-tab:" + i,
							hudCopy( "UIIT_CTL_MENU_" + tabs[i] + "SET" ),
							[ px + 40 + i * 62, py + 40, 60, 24 ],
							optionTab === i,
							{ family: "com_tab", client: [ 0, 9, 0, 6 ] }
						);
					}
					for (
						const [index, key, id] of [ [ 0, "DEF", "option-default" ], [ 1, "OK", "option-ok" ], [
							2,
							"CANC",
							"option-cancel"
						], [ 3, "APPLY", "option-apply" ] ] as const
					) {
						if ( index === 3 && optionTab !== 0 ) continue;
						const source = layout["GDR_OPTION_BTN_" + key]!,
							node = {
								...source,
								rect: [
									index === 3 ? 281 : geometry.x[index]!,
									geometry.row,
									source.rect[2],
									source.rect[3]
								] as UiRect
							};
						authoredLabeledButton( node, px, py, id, hudCopy( node.text ) );
					}
					const ox = px + 11,
						oy = py + 62,
						page = hudData.windows["ifoption_" + tabs[optionTab]!.toLowerCase()]!,
						host = Object.values( layout ).find( node => node.id === 10 + optionTab )!;
					authoredChrome( { ...host, type: "CIFFrame" }, px, py );
					if ( optionTab !== 4 ) {
						for ( const node of authoredPaintOrder( page ) ) {
							authoredChrome(
								node,
								ox,
								oy
							);
						}
					}
					if ( optionTab === 0 ) {
						const manager = page.GDR_OPT_VIDEO_DETAIL_OPT!,
							bounds = authoredRect( manager, ox, oy ),
							slot = hudData.windows.ifvideooptionslot!,
							rows = [ ...videoRows(), {
								slot: VIDEO_FRAME_LIMIT_SLOT,
								key: "Frame rate",
								entries: frameLimits().map( fps => fps ? `${fps} FPS` : "Display refresh rate" ),
								supported: true
							} ],
							combos: {
								slot: number;
								r: UiRect;
								entries: readonly string[];
								selected: number;
								label: string;
								disabled: boolean;
							}[] = [];
						for ( let i = 0; i < 2; i++ ) {
							const node = Object.values( page ).find( n => n.id === 25 + i )!,
								r = authoredRect( node, ox, oy );
							for ( const suffix of [ "", "_press" ] ) {
								paths.push( ROOT + "interface/option/opt_video_tab_back" + suffix + ".png" );
							}
							const path = ROOT + "interface/option/opt_video_tab_back" +
								(videoDraft.active === i ? "_press" : "") + ".png";
							if ( resources.has( path ) ) rect( r, white, path );
							authoredText( node, ox, oy, hudCopy( node.text ) );
							controls.push( {
								id: "option-video-record:" + i,
								label: hudCopy( node.text ),
								rect: r,
								kind: "button",
								selected: videoDraft.active === i
							} );
						}
						// Screen size: the game area is the chosen mode, centred on the page
						// in physical pixels (platform displayScale). Hardware
						// gamma is not a browser display mode.
						combos.push( {
							slot: -1,
							r: authoredRect( page.GDR_OPT_VIDEO_CB_SS!, ox, oy ),
							entries: displaySizes().map( ( [width, height] ) =>
								width === 0 ? "Native" : width + " x " + height
							),
							selected: displaySizeIndex( videoDraft.displaySize ),
							label: hudCopy( page.GDR_OPT_VIDEO_ST_SS!.text ),
							disabled: false
						}, {
							slot: -2,
							r: authoredRect( page.GDR_OPT_VIDEO_SP_BR!, ox, oy ),
							entries: [ "UIIT_STT_NORMAL" ],
							selected: 0,
							label: hudCopy( page.GDR_OPT_VIDEO_ST_BR!.text ),
							disabled: true
						} );
						for (
							const [i, row] of rows.slice( Math.round( videoScroll ), Math.round( videoScroll ) + 6 )
								.entries()
						) {
							const x = bounds[0] + 7,
								y = bounds[1] + 7 + i * 30,
								path = ROOT + "interface/option/opt_video_control_02.png";
							paths.push( path );
							if ( resources.has( path ) ) rect( [ x, y, 308, 28 ], white, path );
							combos.push( {
								slot: row.slot,
								r: authoredRect( slot.GDR_OPT_VOS_CB!, x, y ),
								entries: row.entries,
								selected: row.slot === VIDEO_FRAME_LIMIT_SLOT ?
									frameLimits().indexOf( videoDraft.frameLimit ?? DEFAULT_FRAME_LIMIT ) :
									videoDraft.records[videoDraft.active][row.slot]!,
								label: row.slot === VIDEO_FRAME_LIMIT_SLOT ? row.key : hudCopy( row.key ),
								disabled: !row.supported
							} );
						}
						const scroll = chatScrollbar(
							"option-video",
							optionListTrack( bounds, 7, 7 ),
							rows.length,
							VIDEO_VISIBLE_ROWS,
							VIDEO_SCROLL_MAX - videoScroll,
							resources.size,
							full,
							hover,
							pressed
						);
						paths.push( ...scroll.paths );
						quads.push( ...scroll.quads );
						controls.push( ...scroll.controls );
						/*
						================
						entryText
						================
						*/
						const entryText = ( value: string ) => value.startsWith( "UI" ) ? hudCopy( value ) : value;
						for ( const combo of combos ) {
							comboBox(
								combo.r,
								"option-video-combo:" + combo.slot,
								combo.label,
								entryText( combo.entries[combo.selected] ?? "" ),
								combo.disabled && combo.slot < 0
							);
						}
						// Native reverse child traversal paints the label after its combo sibling.
						for (
							const [i, row] of rows.slice( Math.round( videoScroll ), Math.round( videoScroll ) + 6 )
								.entries()
						) {
							authoredText(
								slot.GDR_OPT_VOS_ST!,
								bounds[0] + 7,
								bounds[1] + 7 + i * 30,
								row.slot === VIDEO_FRAME_LIMIT_SLOT ? row.key : hudCopy( row.key )
							);
						}
						const open = combos.find( c =>
							c.slot === videoCombo && (c.slot >= -1 || c.slot === VIDEO_FRAME_LIMIT_SLOT)
						);
						if ( open ) {
							const r = open.r, list: UiRect = [ r[0], r[1] + 20, r[2], open.entries.length * 18 ];
							rect( list, [ 0, 0, 0, 1 ] );
							blocks.push( list );
							for ( const [i, value] of open.entries.entries() ) {
								const cell: UiRect = [ list[0], list[1] + i * 18, list[2], 18 ];
								if ( i === open.selected ) rect( cell, [ .2, .25, .3, 1 ] );
								quads.push(
									...text.quads(
										entryText( value ),
										[ cell[0] + 4, cell[1], cell[2] - 8, 18 ],
										cell,
										white
									)
								);
								controls.push( {
									id: "option-video-choice:" + open.slot + ":" + i,
									label: entryText( value ),
									rect: cell,
									kind: "button",
									selected: i === open.selected,
									disabled: open.disabled
								} );
							}
						}
					}
					if ( optionTab === 3 ) {
						const slot = hudData.windows.ifkeyoptionslot!,
							manager = Object.values( page ).find( n => n.type === "CIFScrollManager" )!,
							bounds = authoredRect( manager, ox, oy ),
							labels = inputLabels();
						for ( let i = 0; i < 2; i++ ) {
							const r: UiRect = [ ox + 29, oy + 43 + i * 22, 16, 16 ],
								key = mouseModeLabel( i as 0 | 1 );
							for ( const state of [ "on", "off" ] ) {
								paths.push( ROOT + "interface/ifcommon/com_radiobutton_" + state + ".png" );
							}
							const path = ROOT + "interface/ifcommon/com_radiobutton_" +
								(bindingDraft.mouseMode === i ? "on" : "off") + ".png";
							if ( resources.has( path ) ) rect( r, white, path );
							quads.push( ...text.quads( hudCopy( key ), [ r[0] + 30, r[1], 299, 16 ], full, white ) );
							controls.push( {
								id: "option-mouse:" + i,
								label: hudCopy( key ),
								rect: [ r[0], r[1], 326, 20 ],
								kind: "button",
								selected: bindingDraft.mouseMode === i
							} );
						}
						for ( let i = 0; i < 10; i++ ) {
							const index = Math.round( bindingScroll ) * 2 + i;
							if ( index >= 34 ) break;
							const x = bounds[0] + 5 + (i % 2) * 155, y = bounds[1] + 6 + Math.floor( i / 2 ) * 30;
							for ( const state of [ "", "_select" ] ) {
								paths.push( ROOT + "interface/option/opt_key" + state + ".png" );
							}
							const path = ROOT + "interface/option/opt_key" +
								(bindingSelected === index ? "_select" : "") + ".png";
							if ( resources.has( path ) ) rect( [ x, y, 156, 28 ], white, path );
							const nodes = Object.values( slot ),
								key = nodes.find( n => n.rect[0] === 6 )!,
								name = nodes.find( n => n.rect[0] === 65 )!;
							authoredText( key, x, y, bindingName( bindingDraft.keys[index]! ) );
							authoredText( name, x, y, hudCopy( labels[index]! ) );
							controls.push( {
								id: "option-bind:" + index,
								label: hudCopy( labels[index]! ) + " " + bindingName( bindingDraft.keys[index]! ),
								rect: [ x, y, 156, 28 ],
								kind: "button",
								captureKeys: true,
								selected: bindingSelected === index
							} );
						}
						const scroll = chatScrollbar(
							"option-input",
							optionListTrack( bounds, 5, 6 ),
							17,
							5,
							12 - bindingScroll,
							resources.size,
							full,
							hover,
							pressed
						);
						paths.push( ...scroll.paths );
						quads.push( ...scroll.quads );
						controls.push( ...scroll.controls );
					}
					if ( optionTab === 1 || optionTab === 2 ) {
						paths.push(
							ROOT + "interface/ifcommon/com_radiobutton_on.png",
							ROOT + "interface/ifcommon/com_radiobutton_off.png"
						);
					}
					if ( optionTab === 2 ) {
						for ( const [i, key] of [ "FREE", "THIRD_PERSON", "QUARTER_VIEW" ].entries() ) {
							const node = Object.values( page ).find( n => n.id === 10 + i )!,
								r = authoredRect( node, ox, oy ),
								path = ROOT + "interface/ifcommon/com_radiobutton_" +
									(sightDraft === i ? "on" : "off") + ".png";
							image( r, path );
							controls.push( {
								id: "option-sight:" + i,
								label: hudCopy( "UIIT_STT_SIGHT_" + key + "_DESC" ),
								kind: "button",
								rect: [ r[0], r[1] - 7, 310, 36 ],
								selected: sightDraft === i
							} );
						}
					}
					if ( optionTab === 1 ) {
						for (
							const [i, key, mute] of [ [ 0, "bgm", "muteBgm" ], [ 1, "effects", "muteEffects" ], [
								2,
								"environment",
								"muteEnvironment"
							] ] as const
						) {
							const slider = Object.values( page ).find( n => n.id === 19 + i )!,
								check = Object.values( page ).find( n => n.id === 13 + i )!,
								caption = Object.values( page ).find( n => n.id === 10 + i )!,
								muteLabel = Object.values( page ).find( n => n.id === 16 + i )!;
							const r = authoredRect( slider, ox, oy ),
								c = authoredRect( check, ox, oy ),
								path = ROOT + "interface/ifcommon/com_radiobutton_" +
									(audioDraft[mute] ? "on" : "off") + ".png";
							const position = audioSliderPosition( audioDraft[key] ), max = AUDIO_SLIDER_MAX;
							const valueText = audioLevelText( audioDraft[key], audioDraft[mute] );
							paths.push( path );
							if ( resources.has( path ) ) rect( c, white, path );
							authoredText( muteLabel, ox, oy, hudCopy( muteLabel.text ) );
							controls.push( {
								id: "option-mute:" + mute,
								label: hudCopy( caption.text ) + " " + hudCopy( muteLabel.text ),
								kind: "button",
								rect: [ ox + muteLabel.rect[0], c[1], c[0] + c[2] - ox - muteLabel.rect[0], 16 ],
								selected: audioDraft[mute]
							} );
							controls.push( {
								id: "option-audio:" + key,
								label: hudCopy( caption.text ),
								kind: "range",
								rect: [ r[0], r[1], 202, 16 ],
								min: 0,
								max,
								value: String( position ),
								valueText,
								helpText: valueText
							} );
							const thumb = ROOT + "interface/ifcommon/com_scroll_button.png";
							paths.push( thumb );
							if ( resources.has( thumb ) ) {
								rect(
									[ r[0] + Math.trunc( position * 186 / max ), r[1], 16, 16 ],
									white,
									thumb
								);
							}
							for ( const delta of [ -1, 1 ] ) {
								const arrow = ROOT + "interface/ifcommon/com_" + (delta < 0 ? "left" : "right") +
										"_bigarrow.png",
									a: UiRect = [ r[0] + (delta < 0 ? -21 : 202), r[1] - 3, 24, 24 ];
								paths.push( arrow );
								if ( resources.has( arrow ) ) rect( a, white, arrow );
								controls.push( {
									id: "option-audio-step:" + key + ":" + delta,
									label: hudCopy( caption.text ) +
										(delta < 0 ?
											" -" :
											" +"),
									kind: "button",
									rect: a,
									disabled: delta < 0 ? position === 0 : position === max
								} );
							}
						}
					}
					if ( optionTab === 4 ) {
						for ( const group of [ 0, 1 ] as const ) {
							const tab = gameLayout["GDR_GAME_OPTION_TAB_" + (group + 1)]!,
								manager = gameLayout["GDR_GAME_OPTION_SCROLLMANAGER_" + (group + 1)]!;
							authoredChrome( { ...manager, type: "CIFFrame" }, ox, oy );
							authoredImage( tab, ox, oy );
							authoredText( tab, ox, oy, hudCopy( tab.text ) );
							const bounds = authoredRect( manager, ox, oy );
							blocks.push( bounds );
							const rows: { key: string; text: string; enabled: boolean; disabled: boolean; }[] =
								gameOptionRows().filter( row => row[4] === group ).map( ( [key, , text] ) => ({
									key,
									text,
									enabled: optionDraft[key],
									disabled: false
								}) );
							if ( group === 0 ) {
								rows.splice( 5, 0, {
									key: "beginner",
									text: "UIIT_STT_FIRSTSTEP_MARK_SIGN",
									enabled: beginnerDraft,
									disabled:
										(game?.progression?.maxLevel ?? game?.progression?.level ?? character?.level ??
											255) > 19
								} );
							}
							const first = Math.round( optionScroll[group]! ) * 2;
							rows.slice( first, first + 6 ).forEach( ( row, i ) => {
								const sx = bounds[0] + 5 + (i % 2) * 155,
									sy = bounds[1] + 6 + Math.floor( i / 2 ) * 30,
									labelNode = slotLayout.GDR_GAME_OPTION_SLOT_STA1!,
									check = slotLayout.GDR_GAME_OPTION_SLOT_CHECKBOX!,
									r = authoredRect( check, sx, sy ),
									path = ROOT + "interface/ifcommon/com_checkbutton_" + (row.enabled ? "on" : "off") +
										".png";
								const skin = ROOT + "interface/option/opt_key02.png";
								paths.push( skin );
								if ( resources.has( skin ) ) rect( [ sx, sy, 156, 28 ], white, skin );
								paths.push( path );
								const caption = hudCopy( row.text );
								// 5C8810 keeps the authored label bounds and enables style 5 bit 1.
								// 780EA0 draws that shadow at (+1,+1) in black before the glyph.
								const labelRect = authoredClientRect( labelNode, sx, sy );
								const labelColor = row.disabled ? [ .5, .5, .5, 1 ] as const : labelNode.color;
								const style = {
									fontIndex: labelNode.fontIndex,
									hAlign: labelNode.hAlign,
									vAlign: labelNode.vAlign
								};
								const labelQuads = text.quads( caption, labelRect, full, labelColor, style ).map(
									q => ({ ...q, textLayout: undefined })
								);
								quads.push(
									...labelQuads.map( q => ({
										...q,
										rect: [ q.rect[0] + 1, q.rect[1] + 1, q.rect[2], q.rect[3] ] as UiRect,
										color: [ 0, 0, 0, labelColor[3] ] as const
									}) ),
									...labelQuads
								);
								// Resource child order paints the checkbox after its label.
								if ( resources.has( path ) ) rect( r, row.disabled ? [ .5, .5, .5, 1 ] : white, path );
								controls.push( {
									id: row.key === "beginner" ? "option-beginner" : "option-toggle:" + row.key,
									label: caption,
									rect: r,
									kind: "button",
									selected: row.enabled,
									disabled: row.disabled
								} );
							} );
							const total = Math.ceil( rows.length / 2 ),
								range = total - 3,
								scroll = chatScrollbar(
									"option-scroll:" + group,
									optionListTrack( bounds, 5, 6 ),
									total,
									3,
									range - optionScroll[group]!,
									resources.size,
									full,
									hover?.replace( ":-1", "-up" ).replace( ":1", "-down" ) ?? null,
									pressed?.replace( ":-1", "-up" ).replace( ":1", "-down" ) ?? null
								);
							paths.push( ...scroll.paths );
							quads.push( ...scroll.quads );
							controls.push(
								...scroll.controls.map( c => ({
									...c,
									id: c.id.replace( "-up", ":-1" ).replace( "-down", ":1" ),
									disabled: c.id.endsWith( "-up" ) ?
										first === 0 :
										c.id.endsWith( "-down" ) ?
										first + 6 >= rows.length :
										false
								}) )
							);
						}
					}
					endWindow( admission );
				}
				if ( panel === "Character" && hudData ) {
					const admission = beginWindow(),
						popup = mainPopupGeometry( "Character", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						[px, py] = popup.frame,
						[ox, oy] = popup.pane,
						layout = hudData.windows.ifplayerinfo_trijob2!;
					mainPopup( "Character", popup );
					const p = game?.progression,
						level = p?.level ?? character?.level,
						stats = p?.stats,
						values: Record<string, string> = {
							GDR_PI_TEXT_LEVEL: level === undefined ?
								"" :
								hudCopy( "UIIT_STT_LV_LEVEL" ).replace( "%d", String( level ) ),
							GDR_PI_TEXT_CURXP2: p?.experience ?? "",
							GDR_PI_TEXT_NEXTXP2: level === undefined ? "" : levels.get( level )?.[0] ?? "",
							GDR_PI_TEXT_STAT2: p?.statPoints === undefined ? "" : String( p.statPoints ),
							GDR_PI_TEXT_STRENGTH_DAT: stats ? String( stats.strength ) : "",
							GDR_PI_TEXT_INTELLECT_DAT: stats ? String( stats.intellect ) : "",
							GDR_PI_TEXT_HP_DAT: stats && local?.hp !== undefined ?
								Math.min( local.hp, stats.maxHp ) + " / " + stats.maxHp :
								"",
							GDR_PI_TEXT_MP_DAT: stats && local?.mp !== undefined ?
								Math.min( local.mp, stats.maxMp ) + " / " + stats.maxMp :
								""
						};
					// 59FFA0: the native empty job/academy branches explicitly publish these values.
					const academySelf = game?.academy?.members?.find( m => m.id === game.academy?.localMemberId );
					values.GDR_PI_HONOR_DATA = game?.academy?.member ?
						(academySelf ? String( academySelf.honor ) : "") :
						hudCopy( "UIIT_STT_TC_CAMP_NOT_JOIN" );
					// 59FFA0's job block: alias, icon, grade title, grade and experience.
					const job = playerInfoJob(
						game?.job ?? noJob(),
						next.entities.find( e => e.gid === game?.localGid )?.countryByte9c,
						hudData.jobExpThresholds,
						hudCopy
					);
					values.GDR_PI_JOB_ALIAS = job.alias;
					values.GDR_PI_JOB_TITLE = job.title;
					values.GDR_PI_JOB_GRADE = job.grade;
					values.GDR_PI_JOB_EXP = job.exp;
					if ( stats && level !== undefined ) {
						const v = playerAbilityValues( stats, level );
						for (
							const key of [ "PHYATT", "PHYDEF", "PHYBAL", "HIT", "MAGATT", "MAGDEF", "MAGBAL", "PARRY" ]
						) values["GDR_PI_TEXT_" + key + "_DAT"] = v[(key + "DAT") as keyof typeof v] ?? "";
					}
					for ( const node of authoredPaintOrder( layout ) ) {
						authoredChrome( node, ox, oy );
						if ( node.id === 1 ) {
							quads.push(
								...text.quads(
									next.session?.character ?? "",
									[ ox + 10, oy + 7, 344, 12 ],
									full,
									white,
									{ hAlign: 1, vAlign: 0 }
								)
							);
						}
						if ( node.name in values ) authoredText( node, ox, oy, values[node.name]! );
						if ( node.id === 71 && job.icon ) {
							authoredImage(
								{ ...node, uv: [ 0, 0, 1, 1 ] },
								ox,
								oy,
								"/assets/images/Media_extracted/interface/ifcommon/" + job.icon + ".png"
							);
						}
						if ( node.id === 74 ) authoredImage( node, ox, oy, undefined, job.fraction );
						if ( node.type === "CIFGauge" && node.id !== 74 ) {
							const hp = node.id === 20,
								max = hp ? stats?.maxHp : stats?.maxMp,
								current = hp ? local?.hp : local?.mp;
							authoredImage(
								node,
								ox,
								oy,
								undefined,
								max && current !== undefined ? Math.max( 0, Math.min( 1, current / max ) ) : 0
							);
						}
						if ( node.type === "CIFButton" ) {
							authoredButton(
								node,
								ox,
								oy,
								node.id === 14 ? "stat-str" : "stat-int",
								hudCopy( node.id === 14 ? "PARAM_STR" : "PARAM_INT" ),
								(p?.statPoints ?? 0) === 0
							);
						}
					}
					endWindow( admission );
				}
				if ( panel === "Academy" && hudData ) {
					const admission = beginWindow(),
						popup = mainPopupGeometry( "Academy", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						[px, py] = popup.frame,
						[ox, oy] = popup.pane,
						layout = hudData.windows.ifapprenticeship!;
					mainPopup( "Academy", popup );
					const camp = game?.academy,
						leader = camp?.member ? camp.members?.find( m => m.kind === 0 ) : undefined,
						assistants = camp?.members?.filter( m => m.kind === 1 ) ?? [],
						students = camp?.members?.filter( m => m.kind === 2 ) ?? [];
					const rankKeys = [
						"",
						"UIIT_STT_TC_FIRST_STUDENT",
						"UIIT_STT_TC_STUDENT",
						"UIIT_STT_TC_SECOND_STUDENT",
						"UIIT_STT_TC_THIRD_STUDENT",
						"UIIT_STT_TC_FIRST_GUARDIAN",
						"UIIT_STT_TC_SECOND_GUARDIAN",
						"UIIT_STT_TC_GUARDIAN"
					];
					for ( const node of authoredPaintOrder( layout ) ) {
						if ( node.id === 140 || !leader && [ 14, 15, 41, 42, 43, 44, 45, 46 ].includes( node.id ) ) {
							continue;
						}
						if ( node.type === "CIFApprenticeShipSlot" ) {
							authoredImage( node, ox, oy );
							const member = node.id < 32 ? assistants[node.id - 30] : students[node.id - 32];
							if ( member ) {
								const sx = ox + node.rect[0], sy = oy + node.rect[1], rank = academyRank( member );
								for ( const child of authoredPaintOrder( hudData.windows.ifapprenticeshipslot! ) ) {
									if ( child.type === "CIFButton" ) continue;
									authoredChrome( child, sx, sy );
									if ( child.id === 11 ) authoredText( child, sx, sy, member.name );
									if ( child.id === 13 ) {
										authoredText( child, sx, sy, member.entryLevel + "(" + member.level + ")" );
									}
									if ( child.id === 14 && rank ) {
										authoredText( child, sx, sy, hudCopy( rankKeys[rank]! ) );
									}
									if ( child.id === 10 && rank ) {
										authoredImage(
											child,
											sx,
											sy,
											ROOT + "interface/ifcommon/com_icon_level_" + rank + ".png"
										);
									}
									if ( child.id === 20 && member.offline ) {
										authoredImage( child, sx, sy, ROOT + "interface/party/pt_no_face.png" );
									}
								}
							}
							continue;
						}
						if ( node.id === 61 ) {
							const r = authoredRect( node, ox, oy ), path = ROOT + "interface/guild/gil_windo02_off.png";
							image( r, path );
							if ( camp?.member ) {
								authoredText(
									{ ...node, client: [ 70, 7, 0, 0 ] },
									ox,
									oy,
									camp.subject || hudCopy( "UIIT_MSG_GUILD_COMMON_NOTEXIST" )
								);
							}
							continue;
						}
						if ( node.type === "CIFButton" ) {
							const id = node.id === 22 ?
									"open-window:Academy Matching" :
									node.id === 20 ?
									"academy-invite" :
									node.id === 48 ?
									"academy-dismiss" :
									"academy-notice",
								copy = node.id === 48 ? hudCopy( "UIIT_STT_PARTY_DISSOLVE" ) : hudCopy( node.text ),
								disabled = node.id !== 22 &&
									(id !== "academy-notice" || !camp?.member ||
										!camp.members?.some( m => m.kind === 0 && m.id === camp.localMemberId ));
							authoredButton( node, ox, oy, id, copy, disabled );
							if ( copy ) {
								authoredText(
									disabled ? { ...node, color: tooltipColor( 0xff999999 ) } : node,
									ox,
									oy,
									copy
								);
							}
							continue;
						}
						authoredChrome( node, ox, oy );
						if ( leader ) {
							if ( node.id === 42 ) {
								authoredText( { ...node, color: tooltipColor( 0xffffe194 ) }, ox, oy, leader.name );
							}
							if ( node.id === 44 ) {
								authoredText( node, ox, oy, leader.entryLevel + "(" + leader.level + ")" );
							}
							if ( node.id === 45 && academyRank( leader ) === 7 ) {
								authoredText( node, ox, oy, hudCopy( "UIIT_STT_TC_GUARDIAN" ) );
							}
							if ( node.id === 46 && leader.offline ) {
								authoredImage( node, ox, oy, ROOT + "interface/party/pt_no_face.png" );
							}
						}
					}
					endWindow( admission );
				}
				if ( panel === "Party" && hudData ) {
					const admission = beginWindow(),
						popup = mainPopupGeometry( "Party", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						[px, py] = popup.frame,
						[ox, oy] = popup.pane,
						layout = hudData.windows.ifparty!,
						social = game?.social;
					mainPopup( "Party", popup );
					const leader = social?.members.find( m => m.id === social.leader ),
						members = social?.members.filter( m => m.id !== social.leader ) ?? [],
						hasParty = !!social?.leader,
						displayOptions = effectivePartyOptions( social, partyOptions );
					/*
					================
					memberPortrait

					GDR_PTY_PICTURE and GDR_PTYSLOT_PICTURE are CIFStaticWithPictureClip:
					no texture of their own, a rendered head shot of the member. The
					identities are the ones the HUD already renders (the local player's
					gid, partyPortraitGid for the others), so the window adds none.
					================
					*/
					const memberPortrait = ( memberId: number, rect: UiRect ) => {
						const gid = memberId === social?.self ? game?.localGid : partyPortraitGid( memberId );
						if ( !gid ) return;
						quads.push( {
							portraitGid: gid,
							texture: "__portrait",
							rect,
							uv: [ 0, 0, 1, 1 ],
							color: white,
							clip: full
						} );
					};
					for ( const node of authoredPaintOrder( layout ) ) {
						if ( !hasParty && [ 14, 15, 41, 43, 44 ].includes( node.id ) ) continue;
						if ( !hasParty && (node.id === 52 || node.id === 53) ) {
							authoredImage( node, ox, oy, undefined, 1, tooltipColor( 0xffa79b7a ) );
							continue;
						}
						if ( node.id === 1 ) {
							authoredChrome( { ...node, text: "" }, ox, oy );
							const caption = hudCopy( node.text ) +
								((displayOptions & 4) ? " (" + hudCopy( "UIIT_STT_PARTY_JOIN_ANYONE" ) + ")" : "");
							quads.push(
								...text.quads( caption, [ ox + 10, oy + 7, 344, 12 ], full, node.color, {
									hAlign: 1,
									vAlign: 0
								} )
							);
							continue;
						}
						authoredChrome( node, ox, oy );
						if ( node.id === 42 ) {
							authoredText(
								leader ? node : { ...node, color: tooltipColor( 0xffa79b7a ), hAlign: 1 },
								ox,
								oy,
								leader?.name ?? hudCopy( "UIIT_STT_NO_PARTY_LEADER" )
							);
						}
						if ( node.id === 44 ) {
							authoredText(
								node,
								ox,
								oy,
								String( leader?.level ?? game?.progression?.level ?? character?.level ?? "" )
							);
						}
						if ( node.id === 45 ) {
							authoredText(
								{ ...node, color: leader?.guild ? white : tooltipColor( 0xff999999 ) },
								ox,
								oy,
								leader?.guild || hudCopy( "UIIT_STT_NO_GUILD" )
							);
						}
						if ( node.id === 41 ) {
							authoredImage( { ...node, texture: hudData.popupArt.portrait }, ox, oy );
							if ( leader ) memberPortrait( leader.id, authoredRect( node, ox, oy ) );
						}
						if ( node.type === "CIFGauge" && !hasParty ) {
							authoredImage(
								{ ...node, texture: node.texture.replace( ".png", "_disable.png" ) },
								ox,
								oy
							);
							continue;
						}
						if ( node.type === "CIFGauge" ) {
							const hp = node.id === 46,
								max = hp ? local?.maxHp : local?.maxMp,
								current = hp ? local?.hp : local?.mp;
							authoredImage(
								node,
								ox,
								oy,
								undefined,
								leader && leader.id !== social?.self ?
									((leader.status >> (hp ? 0 : 4)) & 15) / 10 :
									max && current !== undefined ?
									Math.max( 0, Math.min( 1, current / max ) ) :
									0
							);
						}
						if ( node.id === 49 || node.id === 50 ) {
							authoredText(
								hasParty ? node : { ...node, color: tooltipColor( 0xffa79b7a ) },
								ox,
								oy,
								hudCopy(
									node.id === 49 ?
										(displayOptions & 1 ? "UIIT_STT_PARTY_EXP_SHARE" : "UIIT_STT_PARTY_EXP_SELF") :
										(displayOptions & 2 ? "UIIT_STT_PARTY_ITEM_SHARE" : "UIIT_STT_PARTY_ITEM_SELF")
								)
							);
						}
						if ( node.type === "CIFButton" ) {
							const id = node.id === 20 ?
									"party-invite" :
									node.id === 21 ?
									"party-settings" :
									node.id === 22 ?
									"open-window:Party Matching" :
									"party-disband",
								copy = node.id === 48 ? hudCopy( "UIIT_STT_PARTY_DISSOLVE" ) : hudCopy( node.text );
							// 75E3A0 / 75B220 -> CIFParty_SetMatchingButtonEnabled (5B7A20):
							// settings lock while an own listing exists.
							const disabled = node.id === 48 ?
								!hasParty || social?.self !== social?.leader :
								node.id === 21 ?
								hasParty || !!game?.partyMatching?.own :
								false;
							authoredButton( node, ox, oy, id, copy, disabled );
							authoredText(
								disabled ? { ...node, color: tooltipColor( 0xff999999 ) } : node,
								ox,
								oy,
								copy
							);
						}
						if ( node.type === "CIFPartySlot" ) {
							const member = members[node.id - 30];
							if ( !member ) continue;
							const sx = ox + node.rect[0], sy = oy + node.rect[1];
							for ( const child of authoredPaintOrder( hudData.windows.ifpartyslot! ) ) {
								if ( child.type === "CIFButton" ) {
									const self = member.id === social?.self;
									if ( self || social?.self === social?.leader ) {
										const id = self ? "party-leave" : "party-member-kick:" + member.id,
											copy = hudCopy( self ? "UIIT_CTL_LEAVE_PARTY" : "UIIT_CTL_BAN_PARTY" );
										authoredLabeledButton( child, sx, sy, id, copy );
									}
									continue;
								}
								authoredChrome( child, sx, sy );
								if ( child.name === "GDR_PTYSLOT_STATIC_NAME" ) {
									authoredText( child, sx, sy, member.name );
								}
								if ( child.name === "GDR_PTYSLOT_PICTURE" ) {
									memberPortrait( member.id, authoredRect( child, sx, sy ) );
								}
								if ( child.name === "GDR_PTYSLOT_STATIC_LEVEL_DATA" ) {
									authoredText( child, sx, sy, String( member.level ) );
								}
								// 5B93A0: the member's guild in white, or the grey no-guild text.
								if ( child.name === "GDR_PTYSLOT_STATIC_GUILD" ) {
									authoredText(
										member.guild ?
											{ ...child, color: white } :
											{ ...child, color: tooltipColor( 0xff999999 ) },
										sx,
										sy,
										member.guild || hudCopy( "UIIT_STT_NO_GUILD" )
									);
								}
								// 5B93A0 -> 81D5B0: the kindred mark of the member's own reference.
								if (
									child.name === "GDR_PTYSLOT_RACE" && (member.country === 0 || member.country === 1)
								) {
									authoredImage(
										child,
										sx,
										sy,
										ROOT + "interface/ifcommon/com_kindred_" +
											(member.country === 0 ? "china" : "europe") +
											"16.png"
									);
								}
								if ( child.type === "CIFGauge" ) {
									const hp = child.texture.includes( "pt_hp" ),
										current = hp ? local?.hp : local?.mp,
										max = hp ? local?.maxHp : local?.maxMp;
									if ( member.id === social?.self ) {
										authoredGauge(
											"party-self-" + child.id,
											member.id,
											child,
											sx,
											sy,
											current !== undefined && max ? current / max : 0
										);
									} else {authoredImage(
											child,
											sx,
											sy,
											undefined,
											Math.min( 1, ((member.status >> (hp ? 0 : 4)) & 15) / 10 )
										);}
								}
							}
						}
					}
					if ( partySettings ) {
						const sx = Math.max( 0, (w - 300) / 2 ),
							sy = Math.max( 0, (h - 200) / 2 ),
							mode = hudData.windows.ifsetpartymode!;
						blocks.push( [ sx, sy, 300, 200 ] );
						paths.push( ...PARTS.map( p => MESSAGE_FRAME + p + ".png" ) );
						quads.push(
							...frameRing(
								[ sx, sy, 300, 200 ],
								MESSAGE_FRAME,
								PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
								full
							)
						);
						quads.push(
							...text.quads(
								hudCopy( "UIIT_STT_CONFIRM_BOX" ),
								[ sx + 10, sy + 11, 279, 12 ],
								full,
								white,
								{ hAlign: 1, vAlign: 0 }
							)
						);
						for ( const node of authoredPaintOrder( mode ) ) {
							if ( node.id === 18 ) {
								authoredText( { ...node, rect: [ 12, 132, 246, 17 ] }, sx, sy, hudCopy( node.text ) );
								continue;
							}
							authoredChrome( node, sx, sy );
							if ( node.type === "CIFButton" ) {
								const id = node.id === 20 ? "party-settings-ok" : "party-settings-cancel";
								authoredLabeledButton( node, sx, sy, id, hudCopy( node.text ) );
							}
							if ( node.type === "CIFRadioButton" || node.type === "CIFCheckBox" ) {
								const bit = node.id === 13 ? 1 : node.id === 14 ? 2 : 4;
								for ( let row = 0; row < (bit === 4 ? 1 : 2); row++ ) {
									const r: UiRect = [ sx + node.rect[0], sy + node.rect[1] + row * 22, 16, 16 ],
										on = bit === 4 ? !!(partyDraft & 4) : Number( !!(partyDraft & bit) ) === row,
										path = ROOT + "interface/ifcommon/com_radiobutton_" + (on ? "on" : "off") +
											".png";
									image( r, path );
									const copy = bit === 4 ?
										hudCopy( "UIIT_STT_PARTY_INVITATION_ANYONE" ) :
										hudCopy(
											"UIIT_STT_PARTY_" + (bit === 1 ? "EXP" : "ITEM") + "_" +
												(row ? "SHARE" : "SELF")
										);
									controls.push( {
										id: "party-setting:" + bit + ":" + row,
										label: copy,
										kind: "button",
										rect: r,
										selected: on
									} );
									if ( bit !== 4 ) {
										quads.push( ...text.quads( copy, [ r[0] + 20, r[1], 100, 16 ], full, white ) );
									}
								}
							}
						}
					}
					endWindow( admission );
					if ( partySettings ) {
						controls = controls.filter( c => c.id.startsWith( "party-setting" ) );
						blocks.push( full );
					}
				}
				if (
					[ "Inventory", "Shop", "Alchemy", GRANT_PANEL, "COS inventory", "Storage" ].includes( panel ) &&
					!(panel === "Shop" && game?.shop?.cosGid) && hudData
				) {
					const admission = beginWindow();
					const popup = mainPopupGeometry( "Inventory", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						[px, py] = popup.frame,
						main = hudData.windows.ifmainpopup!,
						bag = hudData.windows.ifinventory!,
						equipment = hudData.windows.ifequipment!,
						[bx, by] = popup.pane,
						[ex, ey] = authoredRect( main.GDR_EQUIPMENT!, px, py );
					mainPopup( "Inventory", popup );
					const frame = bag.GDR_INVENTORY_FRAME!;
					paths.push( ...PARTS.map( p => frame.texture + p + ".png" ) );
					quads.push(
						...frameRing(
							authoredRect( frame, bx, by ),
							frame.texture,
							PARTS.map( p => resources.size( frame.texture + p + ".png" ) ),
							full
						)
					);
					const lattice = bag.GDR_INVENTORY_LATTICE!;
					for ( const cell of inventoryLattice( bx + lattice.rect[0], by + lattice.rect[1] ) ) {
						const path = lattice.texture + cell.part + ".png";
						paths.push( path );
						if ( resources.has( path ) ) rect( cell.rect, white, path );
					}
					const outline = bag.GDR_INVENTORY_LATTICE_OUTLINE!,
						ring = stretchRing( authoredRect( outline, bx, by ), outline.texture, resources.size, full );
					paths.push( ...ring.paths );
					quads.push( ...ring.quads );
					const slots = inventorySlots(
						bx,
						by,
						game?.inventorySlotCount ?? 0,
						game?.equipmentSlotCount ?? 13,
						inventoryPage
					);
					inventoryPage = slots.page;
					for ( let page = 0; page < slots.pages; page++ ) {
						nativeTab(
							"inventory-page:" + page,
							hudCopy( page ? "UIIT_STT_INVENTORY_EXTENSION_TEB" : "UIIT_CTL_BELOINGING" ),
							[ bx + 4 + 62 * page, by - 23, 60, 24 ],
							page === inventoryPage,
							{ family: "com_tab" }
						);
					}
					/*
					================
					itemSlot
					================
					*/
					function itemSlot( slot: number, r: UiRect, enabled: boolean, empty?: string ) {
						const item = game?.inventory.find( i => i.slot === slot ),
							path = enabled ? (item ? iconPath( item.icon ) : empty) : hudData!.popupArt.blocked;
						if ( path ) {
							image( r, path );
							equipmentOverlay( item, r );
							if ( enabled && item ) itemEffects( "slot:" + slot, item, r );
						}
						controls.push( {
							id: "slot:" + slot,
							label: item?.name ??
								(slot < 13 ? "Equipment slot " + slot : "Empty bag slot " + (slot - 13)),
							rect: r,
							kind: "button",
							// 699359: a move while one is pending is dropped (highlights reset);
							// native slots stay enabled, so hover and tooltips keep working.
							disabled: !enabled,
							selected: inventorySlot === slot,
							rightActivate: !!item || repairHud.armed(),
							draggable: !!item && !repairHud.armed() && cosHud.itemTargetCursor() === null,
							carry: !repairHud.armed() && cosHud.itemTargetCursor() === null && !!item
						} );
						if ( enabled && item ) {
							for (
								const q of inventoryItemCooldownQuads(
									game?.itemCooldowns ?? [],
									item,
									quickslotTime,
									r,
									full
								)
							) {
								paths.push( q.texture );
								if ( resources.has( q.texture ) ) quads.push( q );
							}
						}
						itemCount( item, r );
					}
					for ( const s of slots.slots ) itemSlot( s.slot, s.rect, s.enabled );
					// 5952F9/59530D explicitly draw hidden equipment children 4 then 5.
					const ef = Object.values( equipment ).find( n => n.id === 4 )!,
						eb = Object.values( equipment ).find( n => n.id === 5 )!;
					paths.push( eb.texture, ...PARTS.map( p => ef.texture + p + ".png" ) );
					quads.push(
						...frameRing(
							authoredRect( ef, ex, ey ),
							ef.texture,
							PARTS.map( p => resources.size( ef.texture + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...normalTile( authoredRect( eb, ex, ey ), eb.texture, resources.size( eb.texture ), full )
					);
					if ( game?.localGid ) {
						quads.push( {
							doll: { gid: game.localGid, yaw: dollYaw },
							texture: "__doll",
							rect: [ ex + 2, ey + 15, 176, 318 ],
							uv: [ 0, 0, 1, 1 ],
							color: white,
							clip: full
						} );
					}
					controls.push( {
						id: "equipment-drop-zone",
						label: "Equip item",
						kind: "button",
						rect: [ ex + 2, ey + 15, 176, 318 ],
						// 699359 drops moves while one is pending; the drop zone stays enabled.
						disabled: false
					} );
					for (
						const node of Object.values( equipment ).filter( n =>
							n.id >= 100 && n.id <= 112 && (!avatarView || n.id === 108)
						)
					) {
						const slot = node.id - 100,
							r = authoredRect( node, ex, ey ),
							large = slot === 6 || slot === 7,
							path = hudData.popupArt.sockets[slot]!;
						paths.push( path );
						if ( resources.has( path ) ) {
							rect(
								[ r[0] - (large ? 12 : 4), r[1] - (large ? 12 : 4), large ? 56 : 40, large ? 56 : 40 ],
								white,
								path
							);
						}
						itemSlot( slot, r, true );
					}
					// 592C50: avatar mode retains the special-dress socket, replaces the
					// other twelve equipment controls, and swaps buttons 13/14 in place.
					if ( avatarView ) {
						for ( const node of Object.values( equipment ).filter( n => n.id >= 113 && n.id <= 116 ) ) {
							const type = node.id - 112,
								r = authoredRect( node, ex, ey ),
								item = game?.avatarInventory?.find( i => i.typeFlags >>> 11 === type ),
								path = ROOT + "interface/equipment/equip_slot_" +
									[ "helm", "cloth", "pandernt", "plag" ][type - 1] + ".png";
							paths.push( path );
							if ( resources.has( path ) ) {
								rect( [ r[0] - 4, r[1] - 4, 40, 40 ], white, path );
							}
							const icon = item && iconPath( item.icon );
							if ( icon ) {
								paths.push( icon );
								if ( resources.has( icon ) ) rect( r, white, icon );
								equipmentOverlay( item, r );
								if ( item ) itemEffects( "avatar:" + type, item, r );
							}
							controls.push( {
								id: "avatar:" + type,
								label: item?.name ?? "Avatar slot " + type,
								rect: r,
								kind: "button",
								// 699359 drops moves while one is pending; avatar slots stay enabled.
								disabled: false,
								rightActivate: !!item || repairHud.armed(),
								draggable: !!item && !repairHud.armed() && cosHud.itemTargetCursor() === null,
								carry: !repairHud.armed() && cosHud.itemTargetCursor() === null && !!item
							} );
						}
					}
					authoredButton(
						Object.values( equipment ).find( n => n.id === (avatarView ? 14 : 13) )!,
						ex,
						ey,
						"equipment-view",
						avatarView ? "Equipment" : "Avatar"
					);
					for (
						const [id, action] of [ [ 10, "doll-left" ], [ 11, "doll-right" ], [
							12,
							"doll-reset"
						] ] as const
					) {
						const node = Object.values( equipment ).find( n => n.id === id )!;
						authoredButton(
							node,
							ex,
							ey,
							action,
							action === "doll-left" ?
								"Rotate left" :
								action === "doll-right" ?
								"Rotate right" :
								"Reset rotation"
						);
					}
					authoredImage( bag.GDR_INVENTORY_STA_MONEY!, bx, by );
					authoredButton(
						bag.GDR_INVENTORY_BTN_MONEY!,
						bx,
						by,
						"inventory-gold",
						hudCopy( "UIIT_STT_GOLD" )
					);
					authoredText( bag.GDR_INVENTORY_STA_GOLD!, bx, by, hudCopy( "UIIT_STT_GOLD" ) );
					if ( game?.progression?.gold !== undefined ) {
						const money = moneyPresentation( game.progression.gold );
						authoredText( { ...bag.GDR_INVENTORY_STA_MONEY!, color: money.color }, bx, by, money.text );
					}
					// Beside a service window the inventory is a companion with its own close
					// identity. It retains only its own admission: replaying the standalone
					// window would publish a second "close" beside the service window's.
					endWindow( admission, panel === "Inventory" ? "primary" : "companion:Inventory" );
				}
				if ( panel === "Actions" && hudData ) {
					const admission = beginWindow(),
						popup = mainPopupGeometry( "Actions", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						[px, py] = popup.frame,
						[ox, oy] = popup.pane;
					mainPopup( "Actions", popup );
					const page = hudData.windows.ifaction!,
						actionLocal = next.entities.find( e => e.gid === game?.localGid );
					for ( const node of authoredPaintOrder( page ) ) {
						authoredChrome(
							node,
							ox,
							oy
						);
					}
					for ( const action of hudData.actions ) {
						const node = Object.values( page ).find( n =>
							n.type === "CIFSlotWithHelp" && n.id === action.slot
						);
						if ( !node ) throw Error( "Missing authored action slot" );
						const r = authoredRect( node, ox, oy ),
							icon = iconPath(
								action.id === 1000 ?
									"action/icon_cha_" + (actionLocal?.movementMode === 4 ? "stand" : "sit") + ".ddj" :
									action.id === 1001 ?
									"action/icon_cha_" + (actionLocal?.movementMode === 2 ? "run" : "walk") + ".ddj" :
									action.icon
							),
							id = "action:" + action.id;
						if ( icon ) {
							paths.push( icon );
							if ( resources.has( icon ) ) rect( r, white, icon );
						}
						controls.push( {
							id,
							label: hudCopy( action.name ),
							rightActivate: true,
							rect: r,
							kind: "button",
							draggable: true,
							selected: selectedAction === action.id
						} );
					}
					endWindow( admission );
				}
				if ( (panel === "Skills" || withdrawal.active()) && hudData ) {
					const admission = beginWindow(),
						popup = mainPopupGeometry( "Skills", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						restoring = withdrawal.active(),
						root = hudData.root.GDR_SKILLWITHDRAWAL!,
						withdrawalFrame = restoring ?
							windowOrigin( "withdrawal", [
								Math.max( 0, (w - root.rect[2]) / 2 ),
								Math.max( 0, (h - root.rect[3]) / 2 ),
								root.rect[2],
								root.rect[3]
							] ) :
							popup.frame,
						[px, py] = withdrawalFrame,
						child = hudData.windows.ifskillwithdrawal!.GDR_SKILL!,
						[ox, oy] = restoring ? [ px + child.rect[0], py + child.rect[1] ] : popup.pane,
						withdrawalState = restoring && game ?
							withdrawal.read( game, hudData.masteryCosts, hudData.withdrawalGoldPrices ) :
							null;
					if ( restoring ) {
						blocks.push( withdrawalFrame );
						quads.push(
							...frameRing(
								withdrawalFrame,
								FRAME,
								PARTS.map( p => resources.size( FRAME + p + ".png" ) ),
								full
							)
						);
						quads.push(
							...text.quads(
								hudCopy( root.text ),
								[ px + 10, py + 12, root.rect[2] - 21, 12 ],
								full,
								white,
								{ hAlign: 1, vAlign: 0 }
							)
						);
						closeButton( px + root.rect[2] - 26, py + 10, "withdrawal-close" );
					} else mainPopup( "Skills", popup );
					const page = restoring ? hudData.withdrawalPage : hudData.windows.ifskill!,
						board = hudData.windows.ifskillboard!,
						catalog = hudData.skillUi,
						learned = training,
						masteries = game?.progression?.masteries ?? [];
					if ( restoring ) {
						// Create and Withdrawal both call a different control GDR_SKILL_BG.
						// Native keeps both IDs (6 and 19); preserve the tab backing too.
						authoredChrome( hudData.windows.ifskill!.GDR_SKILL_BG!, ox, oy );
					}
					for ( const node of authoredPaintOrder( page ) ) {
						if ( node.name === "GDR_SKILL_BOARD" || node.text ) continue;
						const chrome = restoring && node.id === 5 ?
							{
								...node,
								rect: [
									node.rect[0],
									node.rect[1],
									node.rect[2],
									WITHDRAWAL_SKILL_FRAME_HEIGHT
								] as UiRect
							} :
							node;
						authoredChrome( chrome, ox, oy );
					}
					const available = catalog.masteries.filter( m => masteries.some( a => a.id === m.id ) ),
						tabs = [ ...new Set( available.map( m => m.tab ) ) ];
					if ( !tabs.includes( skillTab ) ) skillTab = tabs[0] ?? 0;
					for ( const [i, tab] of tabs.entries() ) {
						nativeTab(
							"skill-tab:" + tab,
							hudCopy( available.find( m => m.tab === tab )!.tabName ),
							[ ox + 4 + 62 * i, oy - 23, 60, 24 ],
							tab === skillTab,
							{ family: "com_tab" }
						);
					}
					const active = available.filter( m => m.tab === skillTab );
					if ( !active.some( m => m.id === selectedMastery ) ) selectedMastery = active[0]?.id ?? 0;
					for ( const [i, m] of active.entries() ) {
						const r: UiRect = [ ox + 12 + i * 68, oy + 4, 68, 28 ],
							path = ROOT + "interface/skill/skl_mastery_tab_" +
								(m.id === selectedMastery ? "on" : "off") + ".png";
						image( r, path );
						authoredText(
							hudData.windows.ifskill_mastery!.GDR_STM_STATIC_TITLE!,
							r[0],
							r[1],
							hudCopy( m.name )
						);
						controls.push( {
							id: "skill-mastery:" + m.id,
							label: hudCopy( m.name ),
							rect: r,
							kind: "button",
							selected: m.id === selectedMastery
						} );
					}
					authoredChrome(
						{
							...page.GDR_SKILL_BOARD!,
							type: "CIFFrame"
						},
						ox,
						oy
					);
					const bx = ox + 6,
						by = oy + 29,
						mastery = available.find( m => m.id === selectedMastery ),
						level = masteries.find( m => m.id === selectedMastery )?.level ?? 0;
					authoredImage( board.GDR_SKILLBOARD_MASTERYBOARD!, bx, by );
					if ( mastery ) {
						authoredText(
							board.GDR_SKILLBOARD_MASTERYNAME!,
							bx,
							by,
							hudCopy( mastery.name ) + " " + hudCopy( "PARAM_MASTERY" )
						);
						authoredText( board.GDR_SKILLBOARD_MASTERYLEV!, bx, by, "Lv " + level );
						const icon = iconPath( mastery.icon );
						if ( icon ) {
							paths.push( icon );
							if ( resources.has( icon ) ) rect( [ bx + 7, by + 7, 32, 32 ], white, icon );
							controls.push( {
								id: "mastery-info:" + mastery.id,
								label: hudCopy( mastery.name ),
								rect: [ bx + 7, by + 7, 32, 32 ],
								kind: "region"
							} );
						}
						const removable = withdrawalState?.rows.find( row =>
							row.kind === "mastery-withdraw" && row.id === mastery.id
						);
						if ( restoring && removable && !removable.blocked ) {
							authoredButton(
								{
									...board.GDR_SKILLBOARD_BTNLEVUP!,
									texture: ROOT + "interface/recycle/rec_setup_button.png"
								},
								bx,
								by,
								"withdrawal-choice:mastery-withdraw:" + mastery.id,
								hudCopy( "UIIT_STT_CIRCULATION_WITHDRAW_MASTERY_WND" ),
								!withdrawalState?.quantity || !!game?.trainingPending
							);
						} else if (
							!restoring &&
							game?.progression &&
							!masteryTrainingReason( mastery.id, game.progression, hudData.masteryCosts )
						) {
							authoredButton(
								board.GDR_SKILLBOARD_BTNLEVUP!,
								bx,
								by,
								"mastery:" + mastery.id,
								hudCopy( "PARAM_MASTERY_LEVEL_TOTAL" ),
								!!game.trainingPending
							);
						}
					}
					const mgr = board.GDR_SKILLBOARD_BOARD!,
						bounds = authoredRect( mgr, bx, by ),
						groups = catalog.groups.filter( g => g.mastery === selectedMastery ).sort( ( a, b ) =>
							a.row - b.row
						);
					skillScroll = Math.min( skillScroll, Math.max( 0, groups.length - 4 ) );
					for (
						const [i, group] of groups.slice( Math.floor( skillScroll ), Math.floor( skillScroll ) + 4 )
							.entries()
					) {
						// 6F3C30 positions rows using originY (0) and pitch (55). The 3 set by
						// 5840C0/6F3700 reserves scrollbar space; it is not a row inset.
						const gx = bounds[0],
							gy = bounds[1] + i * 55,
							skin = ROOT + "interface/skill/skl_mastery_bar.png";
						paths.push( skin );
						if ( resources.has( skin ) ) rect( [ gx, gy, 328, 60 ], white, skin );
						const groupId = "skill-group:" + group.mastery + ":" + group.row,
							gi = iconPath( group.icon ),
							gr: UiRect = [ gx + 7, gy + 8, 20, 20 ];
						if ( gi ) {
							const focus = gi.replace( ".png", "_focus.png" );
							paths.push( gi, focus );
							if ( resources.has( gi ) ) {
								rect( gr, white, hover === groupId && resources.has( focus ) ? focus : gi );
							}
							controls.push( {
								id: groupId,
								label: hudCopy( group.name ),
								helpText: hudCopy( group.name ),
								rect: gr,
								kind: "region"
							} );
						}
						for ( let col = 0; col < 8; col++ ) {
							const slot = SkillSlot_Resolve( {
									candidates: catalog.slots[selectedMastery + ":" + group.row + ":" + col] ?? [],
									training: learned,
									masteries,
									progression: game?.progression
								} ),
								entry = slot.entry,
								owned = slot.owned,
								r: UiRect = [ gx + 37 + 36 * col, gy + 3, 32, 32 ];
							const icon = slot.icon.kind === "skill" ?
								iconPath( slot.icon.icon ) :
								slot.icon.kind === "mastery-disable" ?
								ROOT + "interface/skill/skl_mastery_disable.png" :
								ROOT + "interface/skill/skl_mastery_nothing.png";
							if ( icon ) {
								paths.push( icon );
								if ( resources.has( icon ) ) {
									rect( r, slot.alpha < 1 ? [ 1, 1, 1, slot.alpha ] : white, icon );
								}
							}
							if ( !entry ) continue;
							const removable = owned &&
								withdrawalState?.rows.find( row =>
									row.kind === "skill-withdraw" && row.id === owned.id
								);
							if ( restoring && removable && !removable.blocked ) {
								authoredButton(
									{
										...hudData.windows.ifskill_slot!.GDR_STMS_BTN_LEVELUP!,
										texture: ROOT + "interface/recycle/rec_set_button.png"
									},
									r[0],
									r[1],
									"withdrawal-choice:skill-withdraw:" + removable.id,
									hudCopy( "UIIT_STT_CIRCULATION_WITHDRAW_SKILL" ),
									!withdrawalState?.quantity || !!game?.trainingPending
								);
							} else if ( !restoring && slot.button.kind === "learn" ) {
								const ref = slot.button.skill,
									node = {
										...hudData.windows.ifskill_slot!.GDR_STMS_BTN_LEVELUP!,
										texture: ROOT + "interface/skill/" +
											(slot.button.upgrade ? "skl_button_up" : "skl_button_add") + ".png"
									};
								authoredButton(
									node,
									r[0],
									r[1],
									"skill-learn:" + ref.id,
									localization.text( ref.nameSymbol, ref.name )
								);
							} else if ( !restoring && slot.button.kind === "max" ) {
								const node = {
									...hudData.windows.ifskill_slot!.GDR_STMS_BTN_LEVELUP!,
									texture: ROOT + "interface/skill/skl_level_max.png"
								};
								authoredImage( node, r[0], r[1] );
							}
							// 589050 disables the drag source for passive skills (info+61 == 0).
							controls.push( {
								id: "skill:" + entry.id,
								label: localization.text( entry.name, "" ),
								rect: r,
								kind: "button",
								rightActivate: !restoring && !!owned &&
									(hudData.tooltipSkills.get( entry.id )?.basicActivity ?? 0) !== 0,
								draggable: !!owned && (hudData.tooltipSkills.get( entry.id )?.basicActivity ?? 0) !== 0,
								selected: entry.id === selectedSkill
							} );
							if ( owned ) {
								const tab = ROOT + "interface/skill/skl_level_tab.png";
								paths.push( tab );
								if ( resources.has( tab ) ) rect( [ r[0] + 19, r[1] + 17, 16, 16 ], white, tab );
								for ( const [n, d] of String( owned.level ).split( "" ).entries() ) {
									const digit = ROOT + "interface/skill/skl_lv_number_" + d + ".png";
									paths.push( digit );
									if ( resources.has( digit ) ) {
										rect(
											[ r[0] + (owned.level < 10 ? 26 : 24 + n * 5), r[1] + 24, 8, 8 ],
											white,
											digit
										);
									}
								}
							}
						}
					}
					const scroll = chatScrollbar(
						"skill-scroll",
						[ bounds[0] + 326, bounds[1] + 19, 16, 177 ],
						groups.length,
						4,
						Math.max( 0, groups.length - 4 ) - skillScroll,
						resources.size,
						full,
						hover,
						pressed
					);
					paths.push( ...scroll.paths );
					quads.push( ...scroll.quads );
					controls.push( ...scroll.controls );
					for ( const node of Object.values( page ) ) {
						if ( node.text ) authoredText( node, ox, oy, hudCopy( node.text ) );
					}
					if ( restoring && withdrawalState ) {
						const item = withdrawalState.potion, icon = iconPath( item?.icon );
						if ( icon ) image( authoredRect( page.GDR_SKILL_ICON_QSP_ALL_POTION!, ox, oy ), icon );
						authoredText( page.GDR_SKILL_TEXT_QSP_ITEM_NAME!, ox, oy, item?.name ?? "" );
						authoredText(
							page.GDR_SKILL_TEXT_QSP_ITEM_NUM!,
							ox,
							oy,
							`${withdrawalState.quantity} ${hudCopy( "UIIT_STT_UNIT" )}`
						);
					}
					authoredSkillPoints( page.GDR_SKILL_TEXT_SP_NUM!, ox, oy, game?.progression?.skillPoints ?? 0 );
					const model = next.entities.find( e => e.gid === game?.localGid )?.refObjId,
						country = model === undefined ? game?.guide?.country : hudData.countries[model],
						cap = game?.masteryTotalOverride ?? (country === 0 ?
							300 :
							country === 1 ?
							Math.min( 2 * (game?.progression?.level ?? 0), 240 ) :
							0);
					authoredText(
						page.GDR_SKILL_TEXT_TOTAL_MASTERYLEV_NUM!,
						ox,
						oy,
						masteries.reduce( ( n, m ) => n + m.level, 0 ) + "/" + cap
					);
					// Restoration coexists with inventory; its cold-resource cache must
					// never replay the primary popup's controls into this second window.
					endWindow( admission, restoring ? "withdrawal" : "primary" );
				}
				if ( panel === "Quests" && hudData ) {
					const admission = beginWindow(),
						popup = mainPopupGeometry( "Quests", hudData.windows.ifmainpopup!, w, h, popupPosition ),
						[px, py] = popup.frame,
						[ox, oy] = popup.pane,
						page = hudData.windows.ifquest!,
						main = hudData.windows.ifquestslotmain!,
						sub = hudData.windows.ifquestslotsub!,
						blank = hudData.windows.ifquestslot!;
					mainPopup( "Quests", popup );
					for ( const node of authoredPaintOrder( page ) ) {
						authoredChrome(
							node,
							ox,
							oy
						);
					}
					const rows: (readonly [NonNullable<NonNullable<typeof game>["quests"]>[number], number])[] = [];
					for ( const q of game?.quests ?? [] ) {
						rows.push( [ q, -1 ] );
						if ( expandedQuests.has( q.refId ) ) q.contents.forEach( ( _, i ) => rows.push( [ q, i ] ) );
					}
					questPage = Math.min( questPage, Math.max( 0, rows.length - 12 ) );
					const bounds = authoredRect( page.GDR_QUEST_SCROLLMGR!, ox, oy );
					for ( let i = 0; i < 12; i++ ) {
						const sx = bounds[0], sy = bounds[1] + i * 27, row = rows[Math.floor( questPage ) + i];
						if ( !row ) {
							authoredImage( blank.GDR_QUESTSLOT_EMPTY_WND!, sx, sy );
							continue;
						}
						const [q, content] = row;
						if ( content >= 0 ) {
							const objective = questObjectivePresentation(
								q.contents[content]!,
								guideResources.data()?.questPresentation.text ?? {}
							);
							authoredImage( sub.GDR_QUESTSLOT_SUB_WND!, sx, sy );
							authoredText(
								{ ...sub.GDR_QUESTSLOT_SUB_CONTENTS!, color: objective.color },
								sx,
								sy,
								objective.description
							);
							authoredText(
								{ ...sub.GDR_QUESTSLOT_SUB_STATUS!, color: objective.color },
								sx,
								sy,
								hudCopy( objective.statusKey )
							);
							continue;
						}
						authoredImage( main.GDR_QUESTSLOT_MAIN_WND!, sx, sy );
						const bar = main.GDR_QUESTLOST_MAIN_TITLE_BAR!,
							path = ROOT + "interface/quest/qst_colorbar_" +
								([ "blue", "green", "red", "lightgreen" ][q.u09] ?? "blue") + ".png";
						paths.push( path );
						if ( resources.has( path ) ) rect( authoredRect( bar, sx, sy ), white, path );
						if ( trackedQuest === q.refId ) authoredImage( main.GDR_QUESTLOST_MAIN_SELECTED_BAR!, sx, sy );
						const meta = guideResources.data()?.questPresentation.records[q.refId],
							title = meta?.title ?? "";
						authoredText(
							main.GDR_QUESTSLOT_TITLE!,
							sx,
							sy,
							title + " (" + (q.u08 & 15) + "/" + (q.u08 >>> 4) + ")"
						);
						controls.push( {
							id: "quest-track:" + q.refId,
							label: title,
							kind: "button",
							rect: authoredRect( bar, sx, sy ),
							selected: trackedQuest === q.refId
						} );
						authoredText( main.GDR_QUESTSLOT_REMAIN_TIME!, sx, sy, questTimers.text( q, hudCopy ) );
						const arrow = {
							...main.GDR_QUESTSLOT_MAIN_DROPDOWN_BTN!,
							texture: ROOT + "interface/ifcommon/com_qst_" +
								(expandedQuests.has( q.refId ) ? "down" : "right") + "arrow_button.png"
						};
						authoredButton( arrow, sx, sy, "quest-expand:" + q.refId, title );
						authoredButton( main.GDR_QUESTSLOT_MAIN_CONTENT_BTN!, sx, sy, "quest:" + q.refId, title );
					}
					const scroll = chatScrollbar(
						"quest-scroll",
						optionListTrack( bounds, 0, 0 ),
						Math.max( 12, rows.length ),
						12,
						Math.max( 0, rows.length - 12 ) - questPage,
						resources.size,
						full,
						hover,
						pressed
					);
					paths.push( ...scroll.paths );
					quads.push( ...scroll.quads );
					controls.push( ...scroll.controls );
					endWindow( admission );
				}
				if ( questDetails && hudData ) {
					const q = game?.quests?.find( q => q.refId === selectedQuest ),
						page = hudData.windows.ifquestreward!,
						px = questPosition[0],
						py = questPosition[1],
						admission = beginWindow();
					blocks.push( [ px, py, 376, 384 ] );
					controls.push( {
						id: "quest-detail-drag",
						label: "Move quest details",
						kind: "button",
						draggable: true,
						rect: [ px, py, 376, 384 ]
					} );
					for ( const node of authoredPaintOrder( page ) ) {
						if ( node.type !== "CIFButton" && node.type !== "CIFCloseButton" ) {
							authoredChrome( node, px, py );
						}
					}
					const meta = guideResources.data()?.questPresentation.records[selectedQuest];
					authoredText( page.GDR_QUESTREWARD_TITLE!, px, py, meta?.rewardTitle ?? "" );
					if ( q ) {
						const r = authoredRect( page.GDR_QUESTREWARD_CONTENTS!, px, py );
						const result = text.guide(
							guideTokens( meta?.rewardBody ?? "" ),
							[ r[0], r[1] - questDetailScroll, r[2], r[3] ],
							r,
							gold,
							resources.size
						);
						questDetailMax = Math.max( 0, result.height - r[3] );
						questDetailScroll = Math.min( questDetailScroll, questDetailMax );
						quads.push( ...result.quads );
						paths.push( ...result.paths );
					}
					const give = page.GDR_QUESTREWARD_GIVEUP!,
						caption = hudCopy( q?.u10 === 2 ? "UIIT_STT_QUEST_REWARD" : "UIIT_STT_QUEST_GIVEUP" );
					authoredLabeledButton( give, px, py, q?.u10 === 2 ? "quest-reward" : "quest-abandon", caption );
					const close = page.GDR_QUESTREWARD_CLOSE!;
					closeButton( px + close.rect[0], py + close.rect[1], "quest-details-close" );
					const track = authoredRect( page.GDR_QUESTREWARD_SCROLL!, px, py );
					for (
						const [id, skin, yy] of [ [ "quest-detail-up", "up", track[1] - 16 ], [
							"quest-detail-down",
							"down",
							track[1] + 222
						], [
							"quest-detail-thumb",
							"button",
							track[1] + Math.trunc( questDetailMax ? questDetailScroll * 206 / questDetailMax : 0 )
						] ] as const
					) {
						const path = ROOT + "interface/guide/gd_scroll_" + skin + ".png",
							r: UiRect = [ track[0], yy, 16, 16 ];
						image( r, path );
						controls.push( {
							id,
							label: id === "quest-detail-thumb" ?
								"Scroll quest details" :
								skin === "up" ?
								"Scroll up" :
								"Scroll down",
							rect: r,
							kind: "button",
							draggable: skin === "button"
						} );
					}
					endWindow( admission, "quest-details" );
				}
				if ( practice && panel === "Skills" && hudData ) {
					const mastery = practice.mode === PRACTICE_MASTERY,
						row = mastery ? undefined : training.skill( practice.id ),
						page = hudData.windows.ifskillpracticebox!,
						px = Math.floor( (w - 320) / 2 ),
						py = Math.floor( (h - 334) / 2 ),
						prefix = ROOT + "interface/messagebox/msgbox2_window_";
					controls = [];
					const admission = beginWindow();
					blocks.push( full );
					blocks.push( [ px, py, 320, 334 ] );
					paths.push( ...PARTS.map( p => prefix + p + ".png" ) );
					quads.push(
						...frameRing(
							[ px, py, 320, 334 ],
							prefix,
							PARTS.map( p => resources.size( prefix + p + ".png" ) ),
							full
						)
					);
					// CIFSkillPracticeBox_SetMode (5DDF00) toggles the six controls
					// CIFSkillPracticeBox_OnCreate (5DE4E0) stores: ids 11-13 and 15 (slot
					// frame, slot, skill name, level number) for the skill face, ids 16-17
					// (mastery band and name) for the mastery face. The "Lv" caption (14)
					// is never stored, so it stays in both faces. 5DE5F8 initializes
					// control 8's opaque fill; authored Color is only editor data.
					const hidden = mastery ?
						[
							page.GDR_SKLPB_SLOTDECO,
							page.GDR_SKLPB_SLOT,
							page.GDR_SKLPB_SKILLNAME,
							page.GDR_SKLPB_SKILLLEV
						] :
						[ page.GDR_SKLPB_MASTERYNAME, page.GDR_SKLPB_MNDECO ];
					for ( const node of authoredPaintOrder( page ) ) {
						if ( hidden.includes( node ) || node.type === "CIFButton" ) continue;
						if ( node === page.GDR_SKLPB_FILL_COLOR ) {
							rect( authoredRect( node, px, py ), [ 15 / 255, 15 / 255, 15 / 255, 1 ] );
						} else authoredChrome( node, px, py );
					}
					quads.push(
						...text.quads(
							hudCopy(
								mastery ?
									"UIIT_STT_CIRCULATION_PRACTICE_MASTERY_WND" :
									"UIIT_STT_CIRCULATION_PRACTICE_SKILL_WND"
							),
							messageBox( w, h, 320, 334, [ px, py ] ).title,
							full,
							white,
							{ hAlign: 1, vAlign: 0 }
						)
					);
					if ( row ) {
						const presentation = hudData.skillUi.skills.find( s => s.id === row.id ),
							description = localization.text( presentation?.study, "" ),
							node = page.GDR_SKLPB_DESCRIPTION!,
							dr = authoredRect( node, px, py );
						// 5DE657 reserves 20 pixels of CIFEdit width; 5DE1BF assigns plain text,
						// not PML. Preserve authored font metrics and explicit line breaks.
						quads.push(
							...text.box( description, [ dr[0], dr[1], dr[2] - 20, dr[3] ], dr, node.color, {
								fontIndex: node.fontIndex,
								hAlign: node.hAlign
							} )
						);
						authoredText(
							page.GDR_SKLPB_NEXTSTATEMSG!,
							px,
							py,
							hudCopy( row.level === 1 ? "UIIT_STT_SKILL_LEARN" : "UIIT_STT_SKILL_REINFORCEMENT_MSG" )
								.replace( "%s", localization.text( row.nameSymbol, row.name ) ).replace(
									"%d",
									String( row.level )
								)
						);
						authoredText(
							page.GDR_SKLPB_SKILLNAME!,
							px,
							py,
							localization.text( row.nameSymbol, row.name )
						);
						authoredText( page.GDR_SKLPB_SKILLLEV!, px, py, String( row.level ) );
						authoredText( page.GDR_SKLPB_NEEDSP_AMOUNT!, px, py, String( row.spCost ) );
						const icon = iconPath( row.icon );
						if ( icon ) {
							paths.push( icon );
							if ( resources.has( icon ) ) {
								rect( authoredRect( page.GDR_SKLPB_SLOT!, px, py ), white, icon );
							}
						}
					}
					// 5DE040 mastery face: name, description, the next-level message and
					// the current level's SP cost.
					const record = mastery ? hudData.tooltipMasteries.get( practice.id ) : undefined,
						level = mastery ?
							game?.progression?.masteries.find( m => m.id === record?.id )?.level :
							undefined,
						next = level === undefined ? null : masteryPractice( level, hudData.masteryCosts );
					if ( record && next ) {
						const name = hudCopy( record.name ),
							node = page.GDR_SKLPB_DESCRIPTION!,
							dr = authoredRect( node, px, py );
						quads.push(
							...text.box(
								hudCopy( record.description ),
								[ dr[0], dr[1], dr[2] - 20, dr[3] ],
								dr,
								node.color,
								{
									fontIndex: node.fontIndex,
									hAlign: node.hAlign
								}
							)
						);
						authoredText( page.GDR_SKLPB_MASTERYNAME!, px, py, name );
						authoredText(
							page.GDR_SKLPB_NEXTSTATEMSG!,
							px,
							py,
							hudCopy( "UIIT_STT_SKILL_LEARN" ).replace( "%s", name ).replace(
								"%d",
								String( next.nextLevel )
							)
						);
						authoredText( page.GDR_SKLPB_NEEDSP_AMOUNT!, px, py, String( next.cost ) );
					}
					for (
						const [node, id] of [ [ page.GDR_SKLPB_BTN_PRACTICE!, "skill-confirm-ok" ], [
							page.GDR_SKLPB_BTN_CANCEL!,
							"skill-confirm-cancel"
						] ] as const
					) authoredLabeledButton( node, px, py, id, hudCopy( node.text ) );
					endWindow( admission, "skill-confirm" );
				}
				if ( confirmAbandon && questDetails ) {
					controls = [];
					const admission = beginWindow(),
						meta = guideResources.data()?.questPresentation.records[selectedQuest];
					blocks.push( full );
					// 5C2290 asks through a 6888C0 simple message box, type 1. Its
					// line is plain text under the 600-pixel wrap (52DB20), so one line.
					simpleMessageBox( {
						title: hudCopy( "UIIT_STT_AGREEMENT_BOX" ),
						lines: [
							hudCopy( meta?.warn ? "UIIT_MSG_QUEST_GIVEUP_WINDOW_2" : "UIIT_MSG_QUEST_GIVEUP_WINDOW_1" )
						],
						yes: "quest-abandon-yes",
						no: "quest-abandon-no"
					} );
					endWindow( admission, "quest-abandon" );
				}
				const compositePackage = compositeItemHud.packageId(),
					compositeRoot = hudData?.root.GDR_COMPOSITE_ITEM,
					compositeNodes = hudData?.windows.ifcompositeitemwnd;
				if ( compositePackage !== null && game && compositeRoot && compositeNodes ) {
					// CIFCompositeItemWnd (6AF780): centred (69F0F0), one sys_button
					// per limited item and a cancel button, grown to their count.
					const rows = (game.countJobs ?? []).filter( r => r.packageRefObjId === compositePackage ),
						box = compositeItemLayout( rows.length ),
						width = compositeRoot.rect[2],
						px = Math.max( 0, Math.floor( (w - width) / 2 ) ),
						py = Math.max( 0, Math.floor( (h - box.height) / 2 ) ),
						tile = compositeNodes.GDR_COMPOSITE_BGTILE!,
						frame = compositeNodes.GDR_COMPOSITE_FRAME!,
						admission = beginWindow();
					windowBox( hudCopy( compositeRoot.text ), px, py, width, box.height );
					closeButton( px + width - 26, py + 10, "composite-item-close" );
					authoredChrome(
						{ ...tile, rect: [ tile.rect[0], tile.rect[1], tile.rect[2], box.tileHeight ] },
						px,
						py
					);
					authoredChrome(
						{ ...frame, rect: [ frame.rect[0], frame.rect[1], frame.rect[2], box.frameHeight ] },
						px,
						py
					);
					const buttonNode = ( r: UiRect ): AuthoredControl => ({
						...frame,
						type: "CIFButton",
						texture: ROOT + "interface/system/sys_button.png",
						rect: r,
						client: [ 0, 0, 0, 0 ],
						text: "",
						color: white,
						hAlign: 1,
						vAlign: 1
					});
					rows.forEach( ( row, i ) => {
						authoredLabeledButton(
							buttonNode( box.buttons[i]! ),
							px,
							py,
							"composite-item:" + row.itemRefObjId,
							compositeItemCaption( row.itemName ?? "", row.uses ),
							row.uses === 0
						);
					} );
					authoredLabeledButton(
						buttonNode( box.buttons[rows.length]! ),
						px,
						py,
						"composite-item-cancel",
						hudCopy( "UIIT_CTL_CANCEL" )
					);
					endWindow( admission, "composite-item" );
				}
				// 6AD990's type 0x24 confirm box and the scroll's box 0x1E (6971B0)
				// share the layout: the reverse return's two points and Cancel.
				const reverseBox = game?.reverseReturnChoice ?
					{
						title: "UIIT_CTL_PREMIUM_REVERSE_RETURN",
						prefix: "premium-reverse:",
						cancel: "premium-reverse-cancel"
					} :
					reverseScrollHud.active() ?
					{
						title: "UIIT_MSG_QUESTION_SILKMALL_ITEM_USE_REVERSE_PORTAL",
						prefix: "reverse-scroll:",
						cancel: "reverse-scroll-cancel"
					} :
					null;
				if ( reverseBox ) {
					controls = [];
					const admission = beginWindow(), box = guildProposalLayout( w, h );
					blocks.push( full );
					paths.push( ...partyProposalAssets() );
					quads.push(
						...frameRing(
							box.frame,
							MESSAGE_FRAME,
							PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
							full
						)
					);
					quads.push( ...normalTile( box.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ) );
					quads.push(
						...text.quads( hudCopy( reverseBox.title ), box.title, full, white, {
							hAlign: 1,
							vAlign: 0
						} )
					);
					for (
						const [index, choice, key] of [
							[ 0, REVERSE_RETURN_LAST_RECALL, "UIIT_MSG_ITEM_USE_REVERSE_PORTAL_RETRUN_TO_LAST_RETURN" ],
							[ 1, REVERSE_RETURN_LAST_DEATH, "UIIT_MSG_ITEM_USE_REVERSE_PORTAL_RETRUN_TO_LAST_DEATH" ]
						] as const
					) {
						button(
							reverseBox.prefix + choice,
							hudCopy( key ),
							box.frame[0] + 16,
							box.frame[1] + 44 + index * 26,
							box.frame[2] - 32
						);
					}
					button(
						reverseBox.cancel,
						hudCopy( "UIIT_CTL_CANCEL" ),
						box.refuse[0],
						box.refuse[1],
						box.refuse[2]
					);
					endWindow( admission, "premium-reverse-return" );
				}
				if ( panel === "Blocking" && hudData ) {
					const admission = beginWindow(),
						[px, py] = windowOrigin( "Blocking", [
							Math.max( 0, Math.floor( (w - 477) / 2 ) ),
							Math.max( 0, Math.floor( (h - 400) / 2 ) ),
							477,
							400
						] ),
						ox = px + 19,
						oy = py + 92;
					windowBox( hudCopy( "UIIT_STT_BLOCKMAN_LIST" ), px, py, 477, 400 );
					closeButton( px + 451, py + 10 );
					button( "open-window:Guild", "Community", px + 16, py + 35, 110 );
					const tabRoot = ROOT + "interface/guild/gil_subj_tab_";
					paths.push( tabRoot + "on.png", tabRoot + "off.png" );
					for (
						const [index, caption] of [ [ 0, hudCopy( "UIIT_STT_GET_WHISPER" ) ], [
							1,
							hudCopy( "UIIT_CTL_WARENETWORK_DETAIL_TOTAL" )
						] ] as const
					) {
						const r: UiRect = [ px + 25 + index * 76, py + 68, 76, 28 ];
						if ( resources.has( tabRoot + "on.png" ) && resources.has( tabRoot + "off.png" ) ) {
							rect( r, white, tabRoot + (blockTab === index ? "on" : "off") + ".png" );
						}
						quads.push(
							...text.quads( caption, [ r[0], r[1] + 8, 76, 20 ], full, gold, { hAlign: 1, vAlign: 0 } )
						);
						controls.push( { id: "blocking-tab:" + index, label: caption, rect: r, kind: "button" } );
					}
					const prefix = blockTab === 0 ? "WHISPER" : "CHATTING",
						page = hudData.windows[blockTab === 0 ? "ifwhisperblocking" : "ifchattingblocking"]!,
						slot = hudData.windows[blockTab === 0 ? "ifwhisperblockingslot" : "ifchattingblockingslot"]!,
						rows = blockTab === 0 ? game?.chat?.blocked ?? [] : localBlocks;
					blockOffset = Math.max( 0, Math.min( Math.max( 0, rows.length - 10 ), blockOffset ) );
					const firstBlock = Math.round( blockOffset );
					if ( blockSelected && !rows.includes( blockSelected ) ) blockSelected = "";
					for ( const node of authoredPaintOrder( page ) ) {
						if ( node.type === "CIFButton" ) {
							if ( node.name.endsWith( "BTN_1" ) || node.name.endsWith( "BTN_2" ) ) continue;
							authoredImage( node, ox, oy );
							if ( node.text ) authoredText( node, ox, oy, hudCopy( node.text ) );
						} else if ( node.type === "CIFWnd" ) {
							const bar = barChrome( authoredRect( node, ox, oy ), node.texture, resources.size, full );
							paths.push( ...bar.paths );
							quads.push( ...bar.quads );
						} else if ( node.type === "CIFStretchWnd" ) {
							const out = stretchRing( authoredRect( node, ox, oy ), node.texture, resources.size, full );
							paths.push( ...out.paths );
							quads.push( ...out.quads );
						} else authoredChrome( node, ox, oy );
					}
					authoredText(
						page["GDR_" + prefix + "_BLOCKING_NUMBER"]!,
						ox,
						oy,
						": " + rows.length + (blockTab === 0 ? " / 20" : "")
					);
					for (
						const [suffix, id, disabled] of [ [
							"BTN_1",
							"blocking-add",
							blockTab === 0 && !!game?.chat?.blockPending
						], [
							"BTN_2",
							"blocking-remove",
							!blockSelected || blockTab === 0 && !!game?.chat?.blockPending
						] ] as const
					) {
						const node = page["GDR_" + prefix + "_BLOCKING_" + suffix]!, r = authoredRect( node, ox, oy );
						const family = [ "", "_focus", "_press", "_disable" ].map( s =>
							node.texture.replace( ".png", s + ".png" )
						);
						paths.push( ...family );
						if ( family.every( resources.has ) ) {
							rect(
								r,
								white,
								family[disabled ? 3 : pressed === id && hover === id ? 2 : hover === id ? 1 : 0]!
							);
						}
						authoredText(
							{ ...node, color: buttonTextColor( node.color, buttonAccess( false, disabled ) ) },
							ox,
							oy,
							hudCopy( node.text )
						);
						controls.push( { id, label: hudCopy( node.text ), rect: r, kind: "button", disabled } );
					}
					if ( blockTab === 0 && game?.chat?.blockError ) {
						quads.push(
							...text.quads( game.chat.blockError, [ ox + 346, oy + 108, 88, 125 ], full, gold )
						);
					}
					const select = ROOT + "interface/guild/gil_bar02_select.png",
						deselect = ROOT + "interface/guild/gil_bar02_deselect.png";
					paths.push( select, deselect );
					controls.push( {
						id: "blocking-list",
						label: "Block list",
						rect: [ ox + 11, oy + 34, 328, 233 ],
						kind: "button"
					} );
					rows.slice( firstBlock, firstBlock + 10 ).forEach( ( name, i ) => {
						const sx = ox + 11, sy = oy + 34 + i * 23, r: UiRect = [ sx, sy, 312, 24 ];
						if ( resources.has( select ) && resources.has( deselect ) ) {
							rect( r, white, name === blockSelected ? select : deselect );
						}
						authoredImage( slot["GDR_" + prefix + "_BLOCKING_SLOT_CONTACT"]!, sx, sy );
						authoredText( slot["GDR_" + prefix + "_BLOCKING_SLOT_NAME"]!, sx, sy, name );
						controls.push( {
							id: "blocking-row:" + (firstBlock + i),
							label: name,
							rect: r,
							kind: "button"
						} );
					} );
					const scroll = chatScrollbar(
						"blocking",
						[ ox + 323, oy + 50, 16, 185 ],
						Math.max( 10, rows.length ),
						10,
						Math.max( 0, rows.length - 10 ) - blockOffset,
						resources.size,
						full,
						hover,
						pressed
					);
					paths.push( ...scroll.paths );
					quads.push( ...scroll.quads );
					controls.push( ...scroll.controls );
					// Blocking help uses the same native helper-bubble owner as the HUD.
					controls.push( {
						id: "blocking-help",
						label: hudCopy(
							blockTab === 0 ?
								"UIIT_STT_CHATING_SHUT_SAVE_SERVER_HELP" :
								"UIIT_STT_CHATING_SHUT_SAVE_PC_HELP"
						),
						rect: authoredRect( page["GDR_" + prefix + "_BLOCK_LIST_BTN_SHAPE_END"]!, ox, oy ),
						kind: "button"
					} );
					endWindow( admission );
					if ( blockDialog === "remove" ) {
						// 61B6C0 / 61E340 ask through a 6888C0 simple message box, type 1:
						// white text and Yes/No, unlike the entry window below.
						const mark = beginWindow();
						controls = [];
						blocks.push( full );
						simpleMessageBox( {
							title: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
							lines: [ hudCopy( "UIIT_STT_BLOCKMAN_DELETE_WND" ).replace( "%s", blockSelected ) ],
							yes: "blocking-ok",
							no: "blocking-cancel"
						} );
						endWindow( [ mark[0], 0, mark[2], mark[3] ], "modal:" + panel );
						blocks.push( full );
					} else if ( blockDialog ) {
						const mark = beginWindow(),
							dx = Math.max( 0, Math.floor( (w - 306) / 2 ) ),
							dy = Math.max( 0, Math.floor( (h - 151) / 2 ) );
						controls = [];
						blocks.push( full );
						paths.push( ...partyProposalAssets() );
						quads.push(
							...frameRing(
								[ dx, dy, 306, 151 ],
								MESSAGE_FRAME,
								PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
								full
							)
						);
						quads.push(
							...normalTile(
								[ dx + 16, dy + 40, 274, 95 ],
								MESSAGE_TILE,
								resources.size( MESSAGE_TILE ),
								full
							)
						);
						quads.push(
							...text.quads(
								hudCopy( "UIIT_STT_CONFIRM_BOX" ),
								[ dx + 10, dy + 11, 285, 12 ],
								full,
								white,
								{ hAlign: 1, vAlign: 0 }
							)
						);
						const copy = hudCopy(
							blockDialog === "add" ? "UIIT_STT_BLOCKMAN_ENTRY_WND" : "UIIT_STT_BLOCKMAN_DELETE_WND"
						).replace( "%s", blockSelected );
						quads.push(
							...text.quads(
								copy,
								[ dx + 1, dy + 49, 305, blockDialog === "add" ? 12 : 45 ],
								full,
								gold,
								{ hAlign: 1, vAlign: 0 }
							)
						);
						if ( blockDialog === "add" ) {
							const r: UiRect = [ dx + 19, dy + 73, 266, 20 ];
							rect( [ dx + 12, dy + 71, 281, 25 ], [ 0, 0, 0, 1 ] );
							controls.push( {
								id: "blocking-name",
								label: copy,
								kind: "text",
								value: blockInput,
								rect: r,
								maxLength: 13
							} );
							const start = Math.min( blockInput.length, selection[0] ?? 0 ),
								end = Math.min( blockInput.length, selection[1] ?? start ),
								before = text.run( blockInput.slice( 0, start ) ).width,
								through = text.run( blockInput.slice( 0, end ) ).width;
							if ( focus === "blocking-name" && end > start ) {
								rect( [ r[0] + before, r[1], through - before, 20 ], [ .2, .4, .7, .6 ], "", [
									0,
									0,
									1,
									1
								], r );
							}
							quads.push( ...text.quads( blockInput, r, r, white, { overflow: "clip" } ) );
							if ( focus === "blocking-name" && caretVisible ) {
								rect( [ r[0] + through, r[1], 1, text.height() + 2 ], white, "", [ 0, 0, 1, 1 ], r );
							}
							if ( composing ) {
								rect(
									[ r[0], r[1] + 18, text.run( blockInput ).width, 1 ],
									white,
									"",
									[ 0, 0, 1, 1 ],
									r
								);
							}
						}
						button( "blocking-ok", hudCopy( "UIIT_STT_OK" ) || "OK", dx + 72, dy + 114, 76 );
						button( "blocking-cancel", hudCopy( "UIIT_STT_CANCEL" ) || "Cancel", dx + 160, dy + 114, 76 );
						endWindow( [ mark[0], 0, mark[2], mark[3] ], "modal:" + panel );
						blocks.push( full );
					}
				}
				if ( panel === "Party Matching" && hudData ) {
					const admission = beginWindow(),
						[px, py] = windowOrigin( "Party Matching", [
							Math.max( 0, Math.floor( (w - 785) / 2 ) ),
							Math.max( 0, Math.floor( (h - 480) / 2 ) ),
							785,
							480
						] ),
						page = hudData.windows.ifpartymatch!;
					blocks.push( [ px, py, 785, 480 ] );
					quads.push(
						...frameRing(
							[ px, py, 785, 480 ],
							FRAME,
							PARTS.map( p => resources.size( FRAME + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads(
							hudCopy( "UIIT_PAG_PARTYMATCH_PSEARCH" ),
							[ px + 10, py + 12, 765, 12 ],
							full,
							white,
							{ hAlign: 1, vAlign: 0 }
						)
					);
					closeButton( px + 759, py + 10 );
					const match = game?.partyMatching,
						slot = hudData.windows.ifpartymatchslot!,
						social = game?.social,
						localName = social?.localName ?? next.session?.character ?? "",
						buttons = partyMatchButtons( {
							own: match?.own ?? null,
							ownName: match?.own ? localName : "",
							localName,
							inParty: !!social?.leader,
							leader: !!social?.leader && social.leader === social.self,
							members: Math.max( 1, social?.members.length ?? 0 ),
							options: social?.options ?? 0,
							level: game?.progression?.level ?? 0,
							rows: match?.rows.length ?? 0
						} );
					for ( const node of authoredPaintOrder( page ) ) {
						if ( node.type === "CIFButton" ) {
							const enabled = node.id === 56 || node.id === 55 || node.id >= 60 && node.id <= 67 ||
								node.id >= 15 && node.id <= 20 && buttons[node.id as 15];
							authoredLabeledButton(
								node,
								px,
								py,
								"party-match:" + node.id,
								hudCopy( node.text ),
								!!match?.pending || !enabled
							);
						} else if ( node.type === "CIFComboBox" ) {
							comboBox(
								authoredRect( node, px, py ),
								"party-match:purpose",
								hudCopy( "UIIT_CTL_PARTYMATCH_PSEARCH_OBJECT" ),
								hudCopy( partyPurposes[partySearchDraft.purpose]! )
							);
						} else if ( node.type === "CIFEdit" ) {
							const key = node.id === 47 ? "name" : node.id === 49 ? "min" : "max";
							partyEdit(
								node,
								px,
								py,
								"party-search-" + key,
								partySearchDraft[key],
								key === "name" ? 13 : 3
							);
						} else if ( node.id === PARTY_MATCH_RANGE_SEPARATOR_ID ) {
							// 6374CF installs this caption after creating SearchInfo.
							authoredText( node, px, py, "~" );
						} else if ( !node.name.endsWith( "DUMY" ) ) authoredChrome( node, px, py );
					}
					const filtered = partyMatchRows(
						match?.rows ?? [],
						partySearch,
						partyMatchSort,
						partyMatchDescending
					);
					partyMatchOffset = Math.min( partyMatchOffset, Math.max( 0, filtered.length - 12 ) );
					for (
						const { row, rect: r } of matchingSlots(
							page,
							"CIFPartyMatchSlot",
							filtered.slice( partyMatchOffset ),
							px,
							py
						)
					) {
						const [sx, sy] = r,
							prefix = ROOT + "interface/ifcommon/com_bar01" +
								(row && row.id === partyMatchSelection ? "select" : "") + "_";
						const bar = barChrome( r, prefix, resources.size, full );
						paths.push( ...bar.paths );
						quads.push( ...bar.quads );
						if ( !row ) continue;
						const own = row.name === next.session?.character,
							color: UiQuad["color"] = own ? [ 135 / 255, 233 / 255, 1, 1 ] : white;
						const values: Record<string, string> = {
							ID: String( row.id ),
							NAME: row.name,
							RACE: hudCopy( "UIIT_CTL_PARTYMATCH_AUTOMATCH_RACE_" + (row.race === 0 ? "CH" : "EU") ),
							SUBJECT: row.title,
							OBJECT: hudCopy( partyListingPurposes[row.purpose] ?? "" ),
							NUMBER: row.members + "/" + (row.type & 1 ? 8 : 4),
							LEVEL: row.min + "~" + row.max
						};
						for ( const [key, value] of Object.entries( values ) ) {
							const n = slot["GDR_PARTYMATCH_SLOT_" + key]!;
							quads.push(
								...text.quads( value, authoredRect( n, sx, sy ), r, color, {
									fontIndex: n.fontIndex,
									hAlign: n.hAlign,
									vAlign: n.vAlign
								} )
							);
						}
						const mark = slot.GDR_PARTYMATCH_SLOT_MARK!,
							path = ROOT + "interface/party/pt_" + (row.type & 1 ? "association" : "eachone") + ".png";
						paths.push( path );
						if ( resources.has( path ) ) rect( authoredRect( mark, sx, sy ), color, path );
						controls.push( {
							id: "party-match-row:" + row.id,
							label: row.title,
							rect: r,
							kind: "button",
							selected: row.id === partyMatchSelection
						} );
					}
					for (
						const [id, dx, skin, disabled] of [ [
							"party-match-prev",
							360,
							"left",
							!match || !!match.pending || match.page <= 1
						], [
							"party-match-next",
							394,
							"right",
							!match || !!match.pending || match.page >= match.pages
						] ] as const
					) {
						const path = ROOT + "interface/ifcommon/com_" + skin + "_arrow" + (disabled ? "_disable" : "") +
								".png",
							r: UiRect = [ px + dx, py + 437, 16, 16 ];
						image( r, path );
						controls.push( { id, label: skin, kind: "button", rect: r, disabled } );
					}
					quads.push(
						...text.quads( String( match?.page ?? 1 ), [ px + 376, py + 441, 18, 12 ], full, white, {
							fontIndex: 2,
							hAlign: 1
						} )
					);
					if ( partyPurposeOpen ) {
						const r: UiRect = [ px + 253, py + 95, 100, 74 ];
						rect( r, [ 0, 0, 0, 1 ] );
						for ( let i = 0; i < 5; i++ ) {
							const cell: UiRect = [ r[0] + 2, r[1] + 2 + i * 14, r[2] - 4, 14 ];
							quads.push( ...text.quads( hudCopy( partyPurposes[i]! ), cell, cell, white ) );
							controls.push( {
								id: "party-purpose:" + i,
								label: hudCopy( partyPurposes[i]! ),
								rect: cell,
								kind: "button"
							} );
						}
					}
					endWindow( admission );
				}

				if (
					game?.npcConversation && game.npcConversation.phase !== "closed" && hudData &&
					guideResources.data() && !(panel === "Shop" && game.shop?.npc === game.npcConversation.gid)
				) {
					const mark = beginWindow(),
						root = hudData.root.GDR_NPCWINDOW!,
						child = hudData.windows.if_npcwindow!.GDR_NW_NPCTALK!,
						layout = hudData.windows.if_npctalk!;
					const [px, py] = windowOrigin( "NPC conversation", root.rect, "npc-drag" );
					nativeFrame( root, px, py, target?.name ?? "", "npc-close" );
					const origin = [ px + child.rect[0], py + child.rect[1] ] as const;
					// CIF_NPCTalk inherits CIFFrame (5D4A10 -> 6F64E0); its own
					// decorative ring is separate from the parent and its tiled child.
					authoredChrome( child, px, py );
					nativePage( layout, origin[0], origin[1], [ 6, 10, 11, 12 ] );
					/*
					================
					copy
					================
					*/
					const copy = ( symbol: string ) =>
						guideResources.data()!.questPresentation.text[symbol] ?? hudCopy( symbol );
					const capabilities = game.targetCapabilities ?? 0;
					// 5D7870's outcome page replaces the menu of the NPC that told it.
					const outcomePage = game.npcConversation.phase === "menu" &&
							jobHud.outcome() === game.npcConversation.gid,
						outcomeReward = game.jobOutcome?.reward ?? 0;
					const output = npcTalkLayout( {
						state: game.npcConversation,
						layout,
						origin,
						copy,
						measure: value => text.run( value, 0 ).width,
						draw: ( value, r, c, color ) =>
							text.quads( value, r, c, color, { fontIndex: 0, hAlign: 0, vAlign: 0 } ),
						size: resources.size,
						hover,
						pressed,
						top: npcPanel.top(),
						// NPC capability bits: 1 shop, 2 talk, 4 storage, 0x40 recall, 0x80 teleport,
						// 0x20000000 reverse return.
						canShop: !!(capabilities & 0x801),
						branches: target?.merchantBranches,
						choiceColor: symbol =>
							npcChoiceColor(
								symbol,
								game.progression?.level ?? 0,
								code => guideResources.data()!.quests.records.find( row => row.symbol === code )?.level
							),
						canPortal: !!(capabilities & 0x80),
						portalRows: npcPanel.destinations() ?
							portalMenu(
								hudData.portals,
								target?.refObjId ?? 0,
								key => hudData.zones[key] ?? copy( key ),
								game.targetTaxRate ?? 0
							) :
							null,
						canTalk: !!(capabilities & 2),
						prompt: outcomePage ?
							copy( "UIIT_STT_OUTCOME_WINDOW" ).replace( "%s", game.social?.localName ?? "" ).replace(
								"%s",
								outcomeReward.toLocaleString( "en-US" )
							) :
							guildManagerHud.soldiers() ?
							guildSoldierPrompt( game.social?.guild?.flags ?? 0, copy ) :
							fortressStaffHud.target() ?
							copy( "UIIT_STT_FORT_MANAGER_HIRE" ) :
							target?.kind === "teleport" ?
							target.name :
							"",
						canRecall: !!(capabilities & 0x40),
						canReverseReturn: !!(capabilities & 0x20000000),
						canStorage: !!(capabilities & 4),
						canFortressOfficial: !!(capabilities & 0x800000),
						canFortressManager: !!(capabilities & 0x400000) &&
							(fortressStaffView().holder || fortressStaffView().ally),
						canFortressHire: !!(capabilities & 0x400000) && fortressStaffView().holder,
						canFortressSmith: !!(capabilities & 0x2000000),
						canFortressTrainer: !!(capabilities & 0x4000000),
						// 5D7870: the outcome page offers only the collect row (0x25),
						// and only for a reward above zero.
						guildSoldierRows: outcomePage ?
							(outcomeReward > 0 ?
								[ {
									id: "npc-job-collect:" + (game.job?.type ?? 0),
									label: copy( "UIIT_STT_NPC_CHATTING_HUNTERMENU_OUTCOME" )
								} ] :
								[]) :
							guildManagerHud.soldiers() ?
							guildSoldierRows().map( row => ({ id: row.id, label: copy( row.symbol ) }) ) :
							null,
						fortressStaffRows: fortressStaffHud.target() ?
							[ [ 1, "BATTLEAIDE" ], [ 2, "SMITH" ], [ 4, "TRAINER" ] ].map( ( [flag, name] ) => ({
								id: "npc-fortress-hire:" + flag,
								label: copy( "SN_FORTRESS_MANAGER_EMPLOY_" + name ) + " " +
									copy( "SN_FORTRESS_MANAGER_EMPLOY_FEE" ),
								disabled: !fortressStaffView().commander ||
									!!(fortressStaffView().flags & Number( flag ))
							}) ) :
							null,
						canMagicOption: !!(capabilities & AVATAR_MAGIC_OPTION_FUNCTION),
						// 5D9100 lists the guild set ahead of the job menu.
						jobRows: [
							...guildManagerRows( capabilities, game.social?.guild, game.social?.localName ?? "" ).map(
								row => ({
									id: "npc-guild:" + row.row,
									label: copy( row.symbol )
								})
							),
							...jobMenuRows( jobGuildsOffered( capabilities ), game.job ?? noJob() ).map( row => ({
								id: row.id,
								label: copy( row.symbol )
							}) )
						]
					} );
					npcPanel.geometry( output );
					quads.push( ...output.quads );
					controls.push( ...output.controls );
					paths.push( ...output.paths );
					// Independent window admission: a cold NPC must not reuse another popup's
					// retained image or expose hit regions before its chrome/font is ready.
					const missing = paths.slice( mark[3] ).filter( path => !resources.has( path ) );
					windowMissing.push( ...missing );
					if ( !fontPath || !resources.has( fontPath ) || missing.length ) {
						quads.length = mark[0];
						controls.length = mark[1];
						blocks.length = mark[2];
					}
				}
				if ( panel === "Shop" && hudData ) {
					const admission = beginWindow(),
						root = hudData.root.GDR_STORE!,
						page = hudData.windows.ifstore!,
						[px, py] = windowOrigin( "Shop", [
							Math.max( 0, w - 388 - 254 - 16 ),
							Math.max( 0, h - 478 ),
							root.rect[2],
							root.rect[3]
						] );
					const shop = game?.shop,
						valid = !!shop && shop.npc === game?.target && !shop.error,
						busy = !!game?.inventoryPending;
					nativeFrame( root, px, py, shop?.name ?? "" );
					nativePage( page, px, py );
					if ( compact ) {
						controls.push( {
							id: "shop-scroll-area",
							kind: "region",
							label: "Shop items",
							rect: [ px + 18, py + 60, 227, 219 ]
						} );
					}
					const projection = merchantPage(
							shop,
							shopTab,
							shopPage,
							target?.merchantBranches?.find( b => b.tabs.includes( shopTab ) )?.tabs
						),
						{ tabs, pages } = projection;
					shopTab = projection.tab;
					shopPage = projection.page;
					for ( let i = 0; i < Math.min( 4, tabs.length ); i++ ) {
						nativeTab(
							"shop-tab:" + tabs[i],
							localization.text(
								shop?.tabs?.find( t => t.index === tabs[i] )?.labelSymbol,
								hudCopy( shop?.tabs?.find( t => t.index === tabs[i] )?.labelSymbol ?? "" )
							),
							[ px + 16 + i * 56, py + 38, 56, 24 ],
							tabs[i] === shopTab,
							// 5B22B3: shop content insets are 4, 4, 7, 4.
							{ family: "com_short_tab", disabled: busy, client: [ 4, 4, 7, 4 ] }
						);
					}
					for ( let i = 0; i < 30; i++ ) {
						const e = projection.slots[i], node = page["GDR_STORE_SLOT_" + (100 + i)]!;
						nativeItem(
							e ? "shop-offer:" + e.index : "shop-empty:" + i,
							e?.item,
							authoredRect( node, px, py ),
							!valid || busy || !e
						);
						const offered = e?.item.items?.find( item => item.refObjId === e.item.refObjId );
						if ( e && offered ) {
							itemEffects( "shop-offer:" + e.index, offered, authoredRect( node, px, py ) );
						}
					}
					for ( let i = 0; i < 5; i++ ) {
						const entry = restoreSlotEntry( shop?.buyback ?? [], i ),
							index = shop?.buyback?.indexOf( entry! ) ?? -1,
							node = page["GDR_STORE_ICON_SLOT_0" + (i + 1)]!;
						nativeItem(
							entry ? "shop-buyback:" + index : "shop-buyback-empty:" + i,
							entry,
							authoredRect( node, px, py ),
							!valid || busy || !entry
						);
						if ( entry?.item ) {
							itemEffects( "shop-buyback:" + index, entry.item, authoredRect( node, px, py ) );
						}
					}
					nativeSpin( page.GDR_STORE_SPIN_PAGE!, px, py, "shop-prev", "shop-next", shopPage, pages );
					// Repair remains a typed gameplay operation; never route its button to buy/sell.
					for ( const node of [ page.GDR_STORE_BTN_REPAIR!, page.GDR_STORE_BTN_REPAIRALL! ] ) {
						authoredLabeledButton(
							node,
							px,
							py,
							// The control name, not its numeric resource id, names the button.
							"shop-repair:" + node.name,
							hudCopy( node.text ),
							!valid || busy
						);
					}
					endWindow( admission, "service:Shop" );
				}
				if ( panel === "Alchemy" && hudData ) {
					const admission = beginWindow(),
						frame = hudData.windows.ifnewalchemybox!,
						root = Object.values( hudData.root ).find( node => node.id === 0x2c )!,
						[px, py] = windowOrigin(
							"Alchemy",
							[ Math.max( 0, w - 388 - 392 ), Math.max( 0, h - 478 ), 376, 378 ],
							"window-drag:Alchemy",
							{
								drag: frame.GDR_ALCHEMYBOX_DRAG!.rect,
								// 61FFD3 leaves the root at its authored extent; the tall pane is a child.
								nativeExtent: [ root.rect[2], root.rect[3] ]
							}
						),
						processing = [ "compound", "advanced", "dissolve" ].includes( alchemyMode ),
						page = hudData.windows[processing ? "ifalchemyprocess" : "ifnewalchemyreinforce"]!,
						busy = !!game?.inventoryPending;
					const top = ROOT + "interface/alchemy/alcm_window_2.png";
					paths.push( top );
					if ( resources.has( top ) ) rect( [ px, py, 376, 172 ], white, top );
					blocks.push( [ px, py, 376, 378 ] );
					authoredText( frame.GDR_ALCHEMYBOX_TITLE!, px, py, hudCopy( "UIIT_CTL_ALCHEMYBOX" ) );
					authoredButton( frame.GDR_ALCHEMYBOX_CLOSE!, px, py, "close", hudCopy( "UIIT_CTL_CLOSE" ), busy );
					const pane = frame[
						processing ?
							"GDR_ALCHEMYBOX_ELEMENT_MANUFACTURING" :
							"GDR_ALCHEMYBOX_REINFORCE_EQUIPMENT"
					]!;
					authoredImage( pane, px, py );
					const pml = {
						...frame.GDR_ALCHEMYBOX_PML_TEXT!,
						text: processing ?
							"UIIT_STT_ALCHEMYBOX_MATERIAL_PROCESSING_TEXT" :
							"UIIT_STT_ALCHEMYBOX_REINFORCE_ATTR_TEXT"
					};
					authoredChrome( pml, px, py );
					for ( let i = 0; i < 2; i++ ) {
						const selected = processing === (i === 0),
							r: UiRect = [ px + 18 + i * 86, py + 49, 80, 24 ],
							path = ROOT + "interface/alchemy/alcm_tab_" + (selected ? "on" : "off") + ".png",
							lamp = ROOT + "interface/alchemy/alcm_lamp_" + (i ? "reinforcement" : "enchant") + "_" +
								(selected ? "on" : "off") + ".png";
						paths.push( path, lamp );
						if ( resources.has( path ) ) {
							rect( r, white, path );
						}
						if ( resources.has( lamp ) ) rect( [ r[0], r[1], 20, 24 ], white, lamp );
						const title = hudCopy(
							i ? "UIIT_STT_ALCHEMYBOX_REINFORCE_ATTR" : "UIIT_STT_ALCHEMYBOX_MATERIAL_PROCESSING"
						);
						quads.push( ...text.quads( title, [ r[0] + 20, r[1] + 5, 60, 19 ], full, white ) );
						controls.push( {
							id: "alchemy-mode:" + (i ? "reinforce" : "compound"),
							label: title,
							rect: r,
							kind: "button",
							disabled: busy,
							selected
						} );
					}
					nativePage( page, px, py + 150 );
					// CIFAlchemyReinforce's effect sprite, control 50 (625600).
					const deco = page.GDR_AB_REINFORCE_PROCESS_DECO,
						cell = alchemyEffect && alchemyEffectCell( alchemyClock - alchemyEffect.startedAt );
					if ( !processing && deco && alchemyEffect && cell ) {
						const effect = ROOT + alchemyEffectTexture( alchemyEffect.flags ) + ".png";
						paths.push( effect );
						if ( resources.has( effect ) ) image( authoredRect( deco, px, py + 150 ), effect, white, cell );
					}
					const slots = Object.values( page ).filter( n => n.type === "CIFSlotWithHelp" ).sort( ( a, b ) =>
						a.id - b.id
					);
					slots.forEach( ( node, i ) => {
						const slot = alchemySlots[i],
							item = game?.inventory.find( row => row.slot === slot ),
							closed = i >= alchemySlotCapacity( alchemyMode ),
							r = authoredRect( node, px, py + 150 );
						if ( closed ) {
							const path = ROOT + "interface/alchemy/alcm_slot_closed.png";
							image( r, path );
						}
						nativeItem(
							slot === undefined ? "alchemy-empty:" + i : "alchemy-slot:" + slot,
							item,
							r,
							busy || closed
						);
					} );
					for ( const node of Object.values( page ).filter( n => n.type === "CIFButton" ) ) {
						if (
							node.name.endsWith( "_CANCEL" ) ?
								!busy :
								node.name.endsWith( "_PROCESS" ) && busy
						) continue;
						const id = node.name.endsWith( "_CANCEL" ) ?
							"alchemy-cancel" :
							node.name.endsWith( "_LEFT" ) ?
							"alchemy-count:-1" :
							node.name.endsWith( "_RIGHT" ) ?
							"alchemy-count:1" :
							node.name.endsWith( "_ALL_PROCESS" ) ?
							"alchemy-all" :
							"alchemy-start";
						authoredLabeledButton(
							node,
							px,
							py + 150,
							id,
							hudCopy( node.text ),
							id === "alchemy-cancel" ? !busy : busy || alchemySlots.length < 2
						);
					}
					const edit = page.GDR_AB_MANUFACTURING_EDIT_COUNT;
					if ( edit ) partyEdit( edit, px, py + 150, "alchemy-quantity", alchemyQuantity, 10 );
					endWindow( admission, "service:Alchemy" );
				}
				if ( panel === "Storage" && hudData && hudData.windows.ifstorageroom ) {
					const admission = beginWindow(),
						root = hudData.root.GDR_STORAGEROOM!,
						[px, py] = windowOrigin( "Storage", [
							Math.max( 0, w - 388 - root.rect[2] - 8 ),
							Math.max( 0, h - 478 ),
							root.rect[2],
							root.rect[3]
						] ),
						layout = hudData.windows.ifstorageroom,
						nodes = Object.values( layout ),
						room = game?.storage,
						capacity = room?.capacity ?? 0,
						busy = !!game?.inventoryPending || room?.phase !== "open",
						page = storagePanel.page( capacity ),
						slotIds = Array.from( { length: STORAGE_PAGE_SLOTS }, ( _, i ) => storageSlotControlId( i ) );
					nativeFrame( root, px, py, hudCopy( root.text ) );
					// Slots, the spinner, the gold amount and the money button are live.
					nativePage( layout, px, py, [ ...slotIds, 10, 11, 13 ] );
					for ( let i = 0; i < STORAGE_PAGE_SLOTS; i++ ) {
						const node = nodes.find( n => n.id === storageSlotControlId( i ) );
						if ( !node ) continue;
						const slot = page * STORAGE_PAGE_SLOTS + i;
						nativeItem(
							"storage-slot:" + slot,
							room?.items.find( r => r.slot === slot ),
							authoredRect( node, px, py ),
							busy || slot >= capacity
						);
					}
					const spin = nodes.find( n => n.id === 13 );
					if ( spin ) {
						nativeSpin( spin, px, py, "storage-prev", "storage-next", page, storagePages( capacity ) );
					}
					const moneyButton = nodes.find( n => n.id === 11 );
					if ( moneyButton ) {
						authoredButton( moneyButton, px, py, "storage-gold", hudCopy( "UIIT_STT_GOLD" ), busy );
					}
					const money = nodes.find( n => n.id === 10 );
					if ( money && room ) {
						const shown = moneyPresentation( room.gold );
						authoredText( { ...money, color: shown.color }, px, py, shown.text );
					}
					endWindow( admission, "service:Storage" );
				}
				if (
					panel === FORTRESS_SCHEDULE_PANEL && fortressScheduleHud.isOpen() &&
					hudData?.windows.iffortressbusiness && hudData.windows.iffortressbusinessslot &&
					hudData.root.GDR_FORTRESS_BUSINESS
				) {
					const admission = beginWindow(), root = hudData.root.GDR_FORTRESS_BUSINESS;
					const layout = hudData.windows.iffortressbusiness, slots = hudData.windows.iffortressbusinessslot;
					const [px, py] = windowOrigin( FORTRESS_SCHEDULE_PANEL, [
						Math.max( 0, (w - root.rect[2]) / 2 ),
						Math.max( 0, (h - root.rect[3]) / 2 ),
						root.rect[2],
						root.rect[3]
					] );
					nativeFrame( root, px, py, hudCopy( root.text ), "fortress-schedule-close" );
					nativePage( layout, px, py, [ 520, 521, 550, 551, 552, 553, 554 ] );
					for ( const [id, width] of [ [ 550, 146 ], [ 551, 48 ], [ 552, 96 ] ] as const ) {
						const node = Object.values( layout ).find( row => row.id === id );
						if ( node ) {
							authoredLabeledButton(
								{ ...node, rect: [ node.rect[0], node.rect[1], width, 20 ] },
								px,
								py,
								"fortress-schedule-head:" + id,
								hudCopy( node.text ),
								true
							);
						}
					}
					const reply = game?.fortress?.service;
					for (
						const [index, name] of [
							"GDR_FORTRESS_BUSINESS_PREWAR_EDIT",
							"GDR_FORTRESS_BUSINESS_NEXTWAR_EDIT"
						].entries()
					) {
						const node = layout[name], date = reply?.schedules?.[index];
						if ( node && date?.[0] ) {
							const weekday = [ "SUN", "MON", "TUS", "WED", "THU", "FRI", "SAT" ][date[2] ?? 0];
							authoredText(
								node,
								px,
								py,
								fortressWarFormat( hudCopy( "UIIT_STT_FORT_MANAGER_WAR_SCHEDULE_TIME" ), [
									date[0],
									date[1] ?? 0,
									date[3] ?? 0,
									hudCopy( "UIIT_STT_FORT_MANAGER_WAR_SCHEDULE_" + weekday ),
									date[4] ?? 0,
									date[5] ?? 0
								] )
							);
						}
					}
					const applicants = reply?.applicants ?? [], top = fortressScheduleHud.offset();
					for ( const [index, guild] of applicants.slice( top, top + FORTRESS_SCHEDULE_ROWS ).entries() ) {
						const lx = px + 26, ly = py + 268 + index * 25;
						image( [ lx, ly, 290, 25 ], ROOT + "interface/guild/gil_bar02_deselect.png" );
						for (
							const [id, value] of [ [ 10, guild.name ], [ 11, String( guild.level ) ], [
								12,
								hudCopy(
									guild.side === 0 ?
										"UIIT_CTL_FORT_OFFICAL_OCCUPYAPPLY" :
										"UIIT_CTL_FORT_OFFICAL_UNIONAPPLY"
								)
							] ] as const
						) {
							const node = Object.values( slots ).find( row => row.id === id );
							if ( node ) authoredText( node, lx, ly, value );
						}
					}
					button( "fortress-schedule-prev", "<", px + 260, py + 222, 28, top === 0 );
					button(
						"fortress-schedule-next",
						">",
						px + 292,
						py + 222,
						28,
						top + FORTRESS_SCHEDULE_ROWS >= applicants.length
					);
					endWindow( admission, "service:" + FORTRESS_SCHEDULE_PANEL );
				}
				if (
					panel === FORTRESS_PRODUCTION_PANEL && fortressProductionHud.npc() !== null &&
					hudData?.windows.iffortressmakeitemwnd && hudData.windows.iffortressmakeitemwndslot &&
					hudData.root.GDR_FORTRESS_MAKEITEM_WND
				) {
					// CIFFortressMakeItemWnd: OnCreate 65AD70 lays it out, SetStaff 65C370
					// titles it and fills its one tab, RefreshProductionState 65A280 and
					// OnTimer 65A5E0 show the order.
					const admission = beginWindow(),
						root = hudData.root.GDR_FORTRESS_MAKEITEM_WND,
						layout = hudData.windows.iffortressmakeitemwnd,
						slotLayout = hudData.windows.iffortressmakeitemwndslot,
						byId = ( id: number ) => Object.values( layout ).find( n => n.id === id ),
						slotById = ( id: number ) => Object.values( slotLayout ).find( n => n.id === id ),
						staff = fortressProductionHud.staff(),
						items = fortressProductionItems(),
						order = fortressProductionHud.order(),
						done = fortressProductionHud.done( productionClock ),
						asking = fortressProductionHud.question() !== null,
						may = fortressProductionMayOperate( fortressStaffView().member, staff ),
						[px, py] = windowOrigin( FORTRESS_PRODUCTION_PANEL, [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] );
					nativeFrame(
						root,
						px,
						py,
						hudCopy( staff === "smith" ? "SN_FORTRESS_SMITH_PRODUCT" : "UIIT_STT_FORT_ETC_TRAINING" ),
						"fortress-production-close"
					);
					nativePage( layout, px, py, [ 212, 213, 231, 232, 233, 234, 235, 236, 238, 240 ] );
					// CreateTabs (65C0F0) makes one 72x24 tab at (25,55).
					nativeTab(
						"fortress-production-tab",
						hudCopy( staff === "smith" ? "UIIT_STT_FORT_ETC_SIEGEWEAPON" : "SN_TAB_VEHICLE" ),
						[ px + 25, py + 55, 72, 24 ],
						true,
						{ family: "com_tab" }
					);
					for ( let i = 0; i < FORTRESS_PRODUCTION_MARKS; i++ ) {
						const mark = byId( 232 + i );
						if ( mark ) authoredText( mark, px, py, i * 25 + "%" );
					}
					const list = byId( 210 ),
						slotIcon = slotById( 11 ),
						slotName = slotById( 12 ),
						make = slotById( 13 );
					const rowWidth = slotById( 10 )?.rect[2] ?? 0;
					if ( list ) {
						const [lx, ly] = authoredRect( list, px, py ), top = fortressProductionHud.top();
						for ( let row = 0; row < FORTRESS_PRODUCTION_ROWS; row++ ) {
							const item = items[top + row];
							if ( !item ) break;
							const sy = ly + row * FORTRESS_PRODUCTION_ROW_HEIGHT;
							// AddListRow (659FD0) skins each row with gil_bar02_deselect.
							image(
								[ lx, sy, rowWidth, FORTRESS_PRODUCTION_ROW_HEIGHT ],
								ROOT + "interface/guild/gil_bar02_deselect.png"
							);
							nativePage( slotLayout, lx, sy, [ 11, 12, 13, 14, 15 ] );
							const name = item.name ?? String( item.refObjId );
							if ( slotIcon ) {
								const r = authoredRect( slotIcon, lx, sy ), icon = iconPath( item.icon );
								if ( icon ) image( r, icon );
								controls.push( {
									id: "fortress-production-item:" + item.refObjId,
									label: name,
									helpText: name,
									kind: "region",
									rect: r
								} );
							}
							if ( slotName ) authoredText( slotName, lx, sy, name );
							// SetRowsMakeEnabled (659CE0): no row makes while an order exists.
							if ( make ) {
								authoredLabeledButton(
									make,
									lx,
									sy,
									"fortress-production-make:" + item.refObjId,
									hudCopy( make.text ),
									!may || !!order || asking
								);
							}
						}
					}
					if ( order ) {
						const item = items.find( row => row.refObjId === order.refObjId );
						const icon = byId( 212 ), name = byId( 213 ), count = byId( 238 ), left = byId( 231 );
						const label = item?.name ?? String( order.refObjId );
						if ( icon ) {
							const r = authoredRect( icon, px, py ), path = iconPath( item?.icon );
							if ( path ) image( r, path );
							controls.push( {
								id: "fortress-production-order",
								label,
								helpText: label,
								kind: "region",
								rect: r
							} );
						}
						if ( name ) authoredText( name, px, py, label );
						if ( count ) authoredText( count, px, py, String( order.count ) );
						const remaining = fortressProductionRemaining( order, productionClock );
						if ( left && !done ) {
							authoredText( left, px, py, fortressProductionTimeText( remaining, hudCopy ) );
						}
						const gauge = byId( 240 );
						if ( gauge && item ) {
							// 65A280: the gauge is the elapsed share of count x minutes,
							// discounted when the guild holds the staff member's role.
							const total = fortressProductionPrice(
								item,
								order.count,
								fortressProductionFactor( view?.gameplay?.social?.guild?.members ?? [], staff )
							).seconds;
							authoredImage(
								gauge,
								px,
								py,
								gauge.texture,
								done || total <= 0 ? 1 : Math.max( 0, Math.min( 1, (total - remaining) / total ) )
							);
						}
						// 65A280: a running order shows cancel, a finished one complete.
						const button = byId( done ? 214 : 215 );
						if ( button ) {
							authoredLabeledButton(
								button,
								px,
								py,
								done ? "fortress-production-complete" : "fortress-production-cancel",
								hudCopy( button.text ),
								!may || asking
							);
						}
					}
					endWindow( admission, "service:" + FORTRESS_PRODUCTION_PANEL );
				}
				if (
					panel === FORTRESS_TAX_PANEL && fortressTaxHud.npc() !== null && hudData?.windows.iftaxmanagement &&
					hudData.root.GDR_TAX_MANAGEMENT
				) {
					// CIFTaxManagement: OnCreate 664DA0 lays it out, Refresh 665470 fills it.
					const admission = beginWindow(), root = hudData.root.GDR_TAX_MANAGEMENT;
					const layout = hudData.windows.iftaxmanagement, context = fortressTaxHud.context();
					const node = ( id: number ) => Object.values( layout ).find( row => row.id === id );
					const [px, py] = windowOrigin( FORTRESS_TAX_PANEL, [
						Math.max( 0, (w - root.rect[2]) / 2 ),
						Math.max( 0, (h - root.rect[3]) / 2 ),
						root.rect[2],
						root.rect[3]
					] );
					nativeFrame( root, px, py, hudCopy( root.text ), "fortress-tax-close" );
					// 19..21 carry help text only (style 0x80); the frames draw the captions.
					nativePage( layout, px, py, [ 19, 20, 21, 30, 31, 32, 33, 34, 35, 36, 70, 71, 80 ] );
					for (
						const [id, key] of [
							[ 19, "UIIT_MSG_TP_FORT_TAX_TARGET" ],
							[ 20, "UIIT_MSG_TP_FORT_TAX_CHANGE" ],
							[ 21, "UIIT_MSG_TP_FORT_TAX_LEVY" ]
						] as const
					) {
						const help = node( id );
						if ( help ) {
							controls.push( {
								id: "fortress-tax-help:" + id,
								label: hudCopy( help.text ),
								kind: "region",
								rect: authoredRect( help, px, py ),
								helpText: hudCopy( key )
							} );
						}
					}
					const row = view?.gameplay?.fortress?.fortresses.find( r => r.id === context?.fortress );
					const name = node( 27 );
					if ( name && context ) authoredText( name, px, py, row?.nameStrId ? hudCopy( row.nameStrId ) : "" );
					// 665470 ticks box 30 + i from siegefortress column 10 bit i, then disables it.
					for ( let i = 0; i < 7; i++ ) {
						const box = node( 30 + i );
						if ( !box ) continue;
						const on = !!context && !!((row?.taxTargets ?? 0) & (1 << i));
						authoredImage(
							{ ...box, rect: [ box.rect[0], box.rect[1], 16, 16 ] },
							px,
							py,
							box.texture.replace( "_off", on ? "_on" : "_off" )
						);
					}
					const rate = fortressTaxHud.draft();
					for (
						const [id, value] of [
							[ 52, String( rate ) ],
							[ 53, "%" ],
							[ 54, FORTRESS_TAX_MIN + "%" ],
							[ 55, FORTRESS_TAX_MIN / 2 + "%" ],
							[ 56, "0%" ],
							[ 57, FORTRESS_TAX_MAX / 2 + "%" ],
							[ 58, FORTRESS_TAX_MAX + "%" ]
						] as const
					) {
						const label = node( id );
						if ( label ) authoredText( label, px, py, value );
					}
					// Port-only, not native: 665470 prints the treasury with "%d", which
					// shows only its low 32 bits; the port shows the whole amount.
					const treasury = node( 67 );
					if ( treasury && context ) authoredText( treasury, px, py, context.gold );
					const slider = node( 80 );
					if ( slider ) {
						const r = authoredRect( slider, px, py ), position = rate - FORTRESS_TAX_MIN;
						authoredImage( slider, px, py );
						// The left arrow's DDJ path is misspelt in 664DA0
						// (com_qst_lefttarrow_button.ddj), so native draws none there.
						const right = ROOT + "interface/ifcommon/com_qst_rightarrow_button.png",
							thumb = ROOT + "interface/ifcommon/com_scroll_button.png";
						image( [ r[0] + FORTRESS_TAX_ARROW_RIGHT, r[1], 20, 20 ], right );
						image(
							[
								r[0] + FORTRESS_TAX_ARROW + Math.trunc(
									position * FORTRESS_TAX_TRAVEL / (FORTRESS_TAX_MAX - FORTRESS_TAX_MIN)
								),
								r[1] + 2,
								16,
								16
							],
							thumb
						);
						controls.push( {
							id: "fortress-tax-prev",
							label: "<",
							kind: "button",
							rect: [ r[0], r[1], 20, 20 ],
							disabled: !context
						} );
						controls.push( {
							id: "fortress-tax-next",
							label: ">",
							kind: "button",
							rect: [ r[0] + FORTRESS_TAX_ARROW_RIGHT, r[1], 20, 20 ],
							disabled: !context
						} );
						controls.push( {
							id: FORTRESS_TAX_RATE,
							label: hudCopy( "UIIT_STT_FORT_MANAGER_TAXCHANGE_RATE" ),
							kind: "range",
							rect: [
								r[0] + FORTRESS_TAX_ARROW,
								r[1],
								FORTRESS_TAX_ARROW_RIGHT - FORTRESS_TAX_ARROW,
								20
							],
							min: 0,
							max: FORTRESS_TAX_MAX - FORTRESS_TAX_MIN,
							value: String( position ),
							valueText: rate + "%",
							disabled: !context
						} );
					}
					const commander = fortressStaffView().commander;
					for (
						const [id, control] of [ [ 70, "fortress-tax-modify" ], [
							71,
							"fortress-tax-collect"
						] ] as const
					) {
						const at = node( id );
						if ( at ) {
							authoredLabeledButton(
								{ ...at, rect: [ at.rect[0], at.rect[1], 56, 24 ] },
								px,
								py,
								control,
								hudCopy( at.text ),
								!context || !commander
							);
						}
					}
					endWindow( admission, "service:" + FORTRESS_TAX_PANEL );
				}
				const fortressWar = panel === FORTRESS_WAR_PANEL ? fortressWarView() : null;
				if (
					fortressWar && hudData?.windows.iffortresswarapplywnd &&
					hudData.windows.iffortresswarapplywndslot &&
					hudData.root.GDR_FORTRESS_WAR_APPLY_WND
				) {
					// CIFFortressWarApplyWnd (660930): the dates, the column heads and
					// eight slots, the official's fortresses first (662E80, 662D00).
					const admission = beginWindow(),
						root = hudData.root.GDR_FORTRESS_WAR_APPLY_WND,
						layout = hudData.windows.iffortresswarapplywnd,
						slotLayout = hudData.windows.iffortresswarapplywndslot,
						nodes = Object.values( layout ),
						slotNodes = Object.values( slotLayout ),
						byId = ( id: number ) => nodes.find( n => n.id === id ),
						[px, py] = windowOrigin( FORTRESS_WAR_PANEL, [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] );
					nativeFrame( root, px, py, hudCopy( root.text ), "fortress-war-close" );
					// The dates, the headers and the list are drawn below.
					nativePage( layout, px, py, [ 504, 505, 511, 512, 513, 514, 515 ] );
					const start = fortressWarView()?.application?.warStart;
					if ( start ) {
						const dates = fortressWarDates( start ), warLine = byId( 505 ), applyLine = byId( 504 );
						if ( warLine ) {
							authoredText(
								warLine,
								px,
								py,
								hudCopy( "UIIT_STT_FORT_OFFICAL_TEXT1" ) + " : " +
									fortressWarFormat( hudCopy( "UIIT_STT_FORT_ETC_SCHEDULE1" ), dates.war )
							);
						}
						if ( applyLine ) {
							authoredText(
								applyLine,
								px,
								py,
								hudCopy( "UIIT_STT_FORT_OFFICAL_TEXT2" ) + " : " +
									fortressWarFormat( hudCopy( "UIIT_STT_FORT_ETC_SCHEDULE2" ), dates.apply )
							);
						}
					}
					for ( const id of [ 511, 512, 513 ] ) {
						const head = byId( id );
						if ( head ) {
							authoredLabeledButton(
								head,
								px,
								py,
								"fortress-war-head:" + id,
								hudCopy( head.text ),
								true
							);
						}
					}
					const list = byId( 515 );
					if ( list ) {
						const [lx, ly] = authoredRect( list, px, py ),
							sw = FORTRESS_WAR_APPLY_SLOT_WIDTH,
							sh = FORTRESS_WAR_APPLY_SLOT_HEIGHT;
						for ( let row = 0; row < FORTRESS_WAR_APPLY_ROWS; row++ ) {
							const sy = ly + row * sh, slot = fortressWar.slots[row];
							image( [ lx, sy, sw, sh ], ROOT + "interface/guild/gil_bar02_deselect.png" );
							if ( !slot ) continue;
							const name = slotNodes.find( n => n.id === 600 ),
								owner = slotNodes.find( n => n.id === 601 ),
								button = slotNodes.find( n => n.id === 602 );
							if ( name ) authoredText( name, lx, sy, hudCopy( slot.nameSymbol ) );
							if ( owner ) authoredText( owner, lx, sy, slot.owner );
							if ( button ) {
								authoredLabeledButton(
									button,
									lx,
									sy,
									"fortress-war-slot:" + slot.fortress,
									hudCopy( slot.caption ),
									!slot.enabled || !!fortressWarHud.question()
								);
							}
						}
					}
					endWindow( admission, "service:" + FORTRESS_WAR_PANEL );
				}
				const jobRank = panel === JOB_RANK_PANEL ? jobHud.rank() : null;
				if ( jobRank && hudData?.windows.ifjobrank && hudData.windows.ifjobcontributionrank ) {
					// CIFJobRank (648110) and CIFJobContributionRank (646FC0): the cached
					// list's page of ten slots, the spin control and, for the
					// contribution window, the viewer's own entry (job-rank.ts).
					const contribution = jobRank.kind === JOB_RANK_CONTRIBUTION,
						root = contribution ? hudData.root.GDR_JOB_CONTRIBUTION_RANK : hudData.root.GDR_JOB_RANK,
						layout = contribution ? hudData.windows.ifjobcontributionrank : hudData.windows.ifjobrank,
						slotLayout = contribution ?
							hudData.windows.ifjobcontributionrankslot :
							hudData.windows.ifjobrankslot,
						list = game?.jobRanks?.lists.find( l => l.job === jobRank.job && l.kind === jobRank.kind );
					if ( root && slotLayout && list ) {
						const admission = beginWindow(),
							nodes = Object.values( layout ),
							slotNodes = Object.values( slotLayout ),
							byId = ( id: number ) => nodes.find( n => n.id === id ),
							firstSlot = contribution ? 30 : 20,
							spinId = contribution ? 50 : 40,
							own = contribution ?
								jobContributionSelf( game?.job ?? noJob(), jobRank.job, hudCopy ) :
								null,
							page = jobRankPage( list, jobRank.page, hudData.jobExpThresholds, hudCopy ),
							[px, py] = windowOrigin( JOB_RANK_PANEL, [
								Math.max( 0, (w - root.rect[2]) / 2 ),
								Math.max( 0, (h - root.rect[3]) / 2 ),
								root.rect[2],
								root.rect[3]
							] );
						nativeFrame( root, px, py, page.title, "job-rank-close" );
						const custom = Array.from( { length: 10 }, ( _, i ) => firstSlot + i );
						custom.push( spinId );
						// 646FC0 relabels the title (20) and header (28) by job, and shows the
						// own entry (10, 13-16) or the "no entry" line (60).
						if ( contribution ) custom.push( 10, 13, 14, 15, 16, 20, 28, 60 );
						nativePage( layout, px, py, custom );
						const say = ( id: number, value: string ) => {
							const node = byId( id );
							if ( node ) authoredText( node, px, py, value );
						};
						if ( contribution ) {
							const trader = jobRank.job === 1;
							say(
								20,
								hudCopy(
									trader ? "UIIT_STT_JOBGUILD_CONTRIBUTERANK" : "UIIT_STT_JOBGUILD_CONTRIBUTERANK2"
								)
							);
							const header = byId( 28 );
							if ( header ) {
								authoredChrome(
									{ ...header, text: trader ? "UIIT_STT_DONATION" : "UIIT_STT_CONTRIBUTE" },
									px,
									py
								);
							}
							if ( own ) {
								const note = byId( 10 );
								if ( note ) {
									const r = authoredRect( note, px, py ),
										out = text.guide( guideTokens( own.note ), r, r, note.color, resources.size );
									quads.push( ...out.quads );
									paths.push( ...out.paths );
								}
								say( 13, own.label );
								say( 14, own.alias );
								say( 15, own.grade );
								say( 16, own.amount );
							} else say( 60, hudCopy( "UIIT_STT_JOBGUILD_RANKING_NOEXIST" ) );
						}
						for ( const [index, slot] of page.slots.entries() ) {
							const holder = byId( firstSlot + index );
							if ( !holder ) continue;
							const [sx, sy] = authoredRect( holder, px, py );
							nativePage( slotLayout, sx, sy, [ 10, 11, 12, 13, 14 ] );
							for ( const node of slotNodes ) {
								const value = slot[node.id];
								if ( value !== undefined ) authoredText( node, sx, sy, value );
							}
						}
						const spin = byId( spinId );
						if ( spin ) {
							const [sx, sy, sw] = authoredRect( spin, px, py );
							button( "job-rank-prev", "<", sx, sy, 20, jobRank.page === 0 );
							quads.push(
								...text.quads(
									jobRank.page + 1 + " / " + page.pages,
									[ sx + 22, sy + 2, sw - 44, 14 ],
									full,
									white,
									{ hAlign: 1, vAlign: 0 }
								)
							);
							button( "job-rank-next", ">", sx + sw - 20, sy, 20, jobRank.page + 1 >= page.pages );
						}
						endWindow( admission, "service:" + JOB_RANK_PANEL );
					}
				}
				const grant = game?.magicOption;
				if ( panel === GRANT_PANEL && grant && hudData?.windows.ifgrantmagicattributewnd ) {
					// CIFGrantMagicAttributeWnd (6EB3E0): the item slot (8), its count
					// line (9), the five option rows (0x28..0x2C) and the buttons are live.
					const admission = beginWindow(),
						root = hudData.root.GDR_GRANT_MAGIC_ATTRIBUTE!,
						layout = hudData.windows.ifgrantmagicattributewnd,
						nodes = Object.values( layout ),
						at = ( id: number ) => nodes.find( n => n.id === id ),
						[px, py] = windowOrigin( GRANT_PANEL, [
							Math.max( 0, w - 388 - root.rect[2] - 8 ),
							Math.max( 0, h - 478 ),
							root.rect[2],
							root.rect[3]
						] ),
						item = game?.inventory.find( r => r.slot === grant.item ),
						part = item ? grantableAvatarPart( item.typeFlags ) : null,
						options = grant.parts.find( p => p.part === part )?.options ?? [],
						choice = magicOptionHud.state(),
						busy = !!game?.inventoryPending || grant.phase !== "idle";
					// Frame close and authored Cancel are separate native controls with one action.
					nativeFrame( root, px, py, hudCopy( root.text ), "magic-option-close" );
					nativePage( layout, px, py, [ 5, 6, 8, 9, 40, 41, 42, 43, 44 ] );
					const slot = at( 8 );
					if ( slot ) nativeItem( "magic-option-slot", item, authoredRect( slot, px, py ), busy );
					// 6EA540: "<part> - <ADD_COUNT>: <free><UNIT>".
					const count = at( 9 ), symbol = part === null ? null : avatarPartSymbol( part );
					if ( count && item && symbol ) {
						const free = (item.tooltip?.fields.maxMagicOptions51c ?? 0) - avatarMagicOptionCount( item );
						authoredText(
							count,
							px,
							py,
							hudCopy( symbol ) + " - " + hudCopy( "UIIT_STT_AVATAR_MAGICOPTION_ADD_COUNT" ) + ": " +
								free +
								hudCopy( "UIIT_STT_UNIT" )
						);
					}
					// 6EB3E0 backs every row with gil_bar02; the chosen row is selected.
					const select = ROOT + "interface/guild/gil_bar02_select.png",
						deselect = ROOT + "interface/guild/gil_bar02_deselect.png";
					paths.push( select, deselect );
					for ( let i = 0; i < MAGIC_OPTION_LIST_ROWS; i++ ) {
						const bar = at( 0x28 + i ), option = options[choice.top + i];
						if ( !bar ) continue;
						const r = authoredRect( bar, px, py );
						if ( resources.has( select ) && resources.has( deselect ) ) {
							rect( r, white, option && option.codename === choice.codename ? select : deselect );
						}
						if ( !option ) continue;
						quads.push(
							...text.quads(
								avatarMagicOptionText( option.codename, option.value, hudCopy ),
								[ r[0] + 8, r[1], r[2] - 16, r[3] ],
								full,
								white,
								{ vAlign: 1 }
							)
						);
						controls.push( {
							id: "magic-option-row:" + option.codename,
							label: option.codename,
							rect: r,
							kind: "button",
							disabled: busy,
							selected: option.codename === choice.codename
						} );
					}
					const list = at( 32 );
					if ( list && options.length > MAGIC_OPTION_LIST_ROWS ) {
						const r = authoredRect( list, px, py ), range = options.length - MAGIC_OPTION_LIST_ROWS;
						const scroll = chatScrollbar(
							"magic-option",
							[ r[0] + r[2] - 16, r[1] + 16, 16, r[3] - 48 ],
							options.length,
							MAGIC_OPTION_LIST_ROWS,
							range - choice.top,
							resources.size,
							full,
							hover,
							pressed
						);
						paths.push( ...scroll.paths );
						quads.push( ...scroll.quads );
						controls.push( ...scroll.controls );
					}
					for (
						const [id, nodeId] of [ [ "magic-option-confirm", 5 ], [ "magic-option-cancel", 6 ] ] as const
					) {
						const node = at( nodeId );
						if ( !node ) continue;
						authoredLabeledButton(
							node,
							px,
							py,
							id,
							hudCopy( node.text ),
							id === "magic-option-confirm" && (busy || !item || !choice.codename)
						);
					}
					endWindow( admission, "service:" + GRANT_PANEL );
				}
				const skin = skinHud.state();
				if ( panel === SKIN_PANEL && skin && hudData?.windows.ifchangeplayermodel ) {
					const admission = beginWindow(),
						root = hudData.root.GDR_CHANGE_PLAYER_MODEL!,
						layout = hudData.windows.ifchangeplayermodel,
						nodes = Object.values( layout ),
						byName = ( name: string ) => nodes.find( n => n.name === name ),
						[px, py] = windowOrigin( SKIN_PANEL, [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] );
					nativeFrame( root, px, py, hudCopy( "UIIT_PAG_CHAR_SKIN_CHANGE" ), "skin-close" );
					// The sex buttons, sliders, rotate and confirm controls are live.
					nativePage( layout, px, py, [ 17, 18, 31, 32, 33, 34, 35, 41, 42, 43, 71, 72, 73, 100 ] );
					for (
						const [id, sex, name] of [ [ "skin:male", 1, "MALE" ], [ "skin:female", 0, "FEMALE" ] ] as const
					) {
						const node = byName( "GDR_CHANGE_PLAYER_MODEL_INFO_BTN_" + name );
						if ( node ) {
							authoredLabeledButton( node, px, py, id, hudCopy( node.text ), skin.draft.sex === sex );
						}
					}
					const prev = byName( "GDR_SLIDER_CTRL_BTN_PREV" ),
						next = byName( "GDR_SLIDER_CTRL_BTN_NEXT" ),
						thumb = byName( "GDR_SLIDER_CTRL_BTN_THUMB" );
					for ( const key of [ "figure", "height", "volume" ] as const satisfies readonly SkinDraftKey[] ) {
						const node = byName( "GDR_CHANGE_PLAYER_MODEL_INFO_SLI_" + key.toUpperCase() );
						if ( !node || !prev || !next || !thumb ) continue;
						const [min, max] = skinDraftRange( skin.models, skin.draft, key ),
							value = skin.draft[key],
							[sx, sy] = authoredRect( node, px, py );
						authoredImage( node, px, py );
						authoredButton( prev, sx, sy, "skin:" + key + ":prev", "", value <= min );
						authoredButton( next, sx, sy, "skin:" + key + ":next", "", value >= max );
						authoredImage(
							thumb,
							sx + (max > min ? (value - min) / (max - min) * SKIN_SLIDER_TRAVEL : 0),
							sy
						);
					}
					for (
						const [id, name] of [ [ "skin-rotate:left", "LEFT" ], [ "skin-rotate:reset", "RESET" ], [
							"skin-rotate:right",
							"RIGHT"
						] ] as const
					) {
						const node = byName( "GDR_CHANGE_PLAYER_MODEL_VIEW_BTN_" + name );
						if ( node ) authoredButton( node, px, py, id, "" );
					}
					for ( const [id, name] of [ [ "skin-confirm", "OK" ], [ "skin-cancel", "CANCEL" ] ] as const ) {
						const node = byName( "GDR_CHANGE_PLAYER_MODEL_INFO_BTN_" + name );
						if ( node ) {
							authoredLabeledButton(
								node,
								px,
								py,
								id,
								hudCopy( node.text ),
								id === "skin-confirm" && (!skinHud.changed() || !!game?.inventoryPending)
							);
						}
					}
					const view = byName( "GDR_CHANGE_PLAYER_MODEL_VIEW" ), doll = itemMall.previewGid();
					if ( view && doll !== undefined ) {
						quads.push( {
							doll: { gid: doll, yaw: skin.yaw },
							texture: "__doll",
							rect: authoredRect( view, px, py ),
							uv: [ 0, 0, 1, 1 ],
							color: white,
							clip: full
						} );
					}
					endWindow( admission, "service:" + SKIN_PANEL );
				}
				const wholeChat = globalChatHud.state();
				if ( wholeChat && hudData?.windows.ifwholechat && hudData.root.GDR_WHOLE_CHAT ) {
					// CIFWholeChat (ginterface GDR_WHOLE_CHAT, resinfo ifwholechat.txt).
					const admission = beginWindow(),
						root = hudData.root.GDR_WHOLE_CHAT,
						layout = hudData.windows.ifwholechat,
						nodes = Object.values( layout ),
						byName = ( name: string ) => nodes.find( n => n.name === name ),
						[px, py] = windowOrigin( GLOBAL_CHAT_PANEL, [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] ),
						// 6D1D90: the slot's count, and Use only while one is left.
						count = game?.inventory.find( row => row.slot === wholeChat.slot )?.quantity ?? 0;
					// The frame's close and the Cancel button are two controls with the
					// same effect; one id for both was a duplicate control identity.
					nativeFrame( root, px, py, hudCopy( "UIIT_STT_WHOLECHAT" ), "wholechat-close" );
					nativePage( layout, px, py, [ 31, 32, 35, 36 ] );
					const edit = byName( "GDR_WHOLE_CHAT_EDITBOX_INPUT" ), odd = byName( "GDR_WHOLE_CHAT_ODDITEM" );
					if ( edit ) partyEdit( edit, px, py, GLOBAL_CHAT_TEXT, wholeChat.text, GLOBAL_CHAT_MAX_LENGTH );
					if ( odd ) authoredText( odd, px, py, String( count ) );
					for ( const [id, name] of [ [ "wholechat-use", "OK" ], [ "wholechat-exit", "CANCEL" ] ] as const ) {
						const node = byName( "GDR_WHOLE_CHAT_BTN_" + name );
						if ( node ) {
							authoredLabeledButton(
								node,
								px,
								py,
								id,
								hudCopy( node.text ),
								id === "wholechat-use" && (count === 0 || !!game?.inventoryPending)
							);
						}
					}
					endWindow( admission, "service:" + GLOBAL_CHAT_PANEL );
				}
				const specialtyDeal = specialtyDealHud.state();
				if ( specialtyDeal && hudData?.windows.ifspecialtydeal && hudData.root.GDR_SPECIALTY_DEAL ) {
					// CIFSpecialtyDeal (ginterface GDR_SPECIALTY_DEAL, resinfo ifspecialtydeal.txt).
					const admission = beginWindow(),
						root = hudData.root.GDR_SPECIALTY_DEAL,
						layout = hudData.windows.ifspecialtydeal,
						nodes = Object.values( layout ),
						byId = ( id: number ) => nodes.find( n => n.id === id ),
						[px, py] = windowOrigin( SPECIALTY_DEAL_PANEL, [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] ),
						buying = specialtyDeal.mode === "buy",
						count = specialtyDealHud.count(),
						inputs = specialtyInputs( game ),
						// A sale's sum is the server's quote (the shop dialog's).
						quote = buying ?
							undefined :
							merchantQuote(
								specialtyDeal.selection,
								game?.shop,
								merchantRows( game ),
								String( count ),
								game?.progression?.gold
							),
						sums = dealSums( count, specialtyDeal.unitBuy, buying ? undefined : quote?.total ?? undefined ),
						grouped = ( value: bigint | undefined ) =>
							value === undefined ? "" : value.toLocaleString( "en-US" ),
						grey: AuthoredControl["color"] = [ 0x99 / 255, 0x99 / 255, 0x99 / 255, 1 ],
						scaleRow = tradeScaleRow( tradeScale( sums.buy, inputs.basis, inputs.speed2 ), inputs.rows );
					nativeFrame( root, px, py, hudCopy( root.text ), "specialty-deal-close" );
					// 6492C0 greys the sale and profit labels while buying; 6496E0 greys
					// the scale label while selling.
					nativePage( layout, px, py, [ 18, 22, 23, 24, 30, 31, 32, 33, 34, 40 ] );
					for ( const id of [ 22, 23, 24 ] ) {
						const node = byId( id );
						const dim = buying ? id !== 24 : id === 24;
						if ( node ) authoredText( dim ? { ...node, color: grey } : node, px, py, hudCopy( node.text ) );
					}
					const name = byId( 18 ), edit = byId( 30 );
					if ( name ) authoredText( name, px, py, specialtyDeal.name );
					if ( edit ) {
						partyEdit(
							edit,
							px,
							py,
							SPECIALTY_DEAL_COUNT,
							specialtyDeal.dealing ?
								String( specialtyDeal.dealing.target ) :
								count ?
								String( count ) :
								"",
							12
						);
					}
					const buySum = byId( 31 ), profitSum = byId( 32 ), sellSum = byId( 33 );
					if ( buySum ) authoredText( buySum, px, py, grouped( sums.buy ) );
					if ( profitSum ) authoredText( profitSum, px, py, grouped( sums.profit ) );
					if ( sellSum ) authoredText( sellSum, px, py, grouped( sums.sell ) );
					const combo = byId( 34 );
					if ( combo ) {
						const r = authoredRect( combo, px, py );
						comboBox(
							r,
							"specialty-deal-scale",
							hudCopy( "UIIT_STT_TRADE_TRADESCALE" ),
							buying ? hudCopy( "UIIT_STT_TRADE_TRADESCALE" + (scaleRow + 1) ) : "",
							!buying || !!specialtyDeal.dealing
						);
						if ( specialtyCombo && buying ) {
							const list: UiRect = [ r[0], r[1] + r[3], r[2], inputs.rows * 18 ];
							rect( list, [ 0, 0, 0, 1 ] );
							blocks.push( list );
							for ( let row = 0; row < inputs.rows; row++ ) {
								const entry: UiRect = [ list[0], list[1] + row * 18, list[2], 18 ];
								const label = hudCopy( "UIIT_STT_TRADE_TRADESCALE" + (row + 1) );
								quads.push(
									...text.quads( label, [ entry[0] + 4, entry[1], entry[2] - 8, 18 ], entry, white )
								);
								controls.push( {
									id: "specialty-deal-row:" + row,
									label,
									rect: entry,
									kind: "button"
								} );
							}
						}
					}
					const ok = byId( 40 );
					if ( ok ) {
						authoredLabeledButton(
							ok,
							px,
							py,
							"specialty-deal-ok",
							hudCopy( ok.text ),
							count === 0 || !!specialtyDeal.dealing || !!game?.inventoryPending
						);
					}
					endWindow( admission, "service:" + SPECIALTY_DEAL_PANEL );
				}
				if ( (panel === "COS inventory" || panel === "Shop" && game?.shop?.cosGid) && hudData ) {
					const admission = beginWindow(),
						root = hudData.root.GDR_COS_WND!,
						[px, py] = windowOrigin( "COS inventory", [
							panel === "Shop" ? Math.max( 0, w - 371 ) : Math.max( 0, w - 388 - 371 ),
							Math.max( 0, h - 478 ),
							root.rect[2],
							root.rect[3]
						] ),
						records = game?.cosRecords?.filter( r => !r.dead && r.hp > 0 ) ?? [];
					if ( panel === "Shop" ) {
						cosGid = game?.shop?.cosGid ?? 0;
						cosTab = 1;
					}
					if ( panel !== "Shop" && !records.some( r => r.gid === cosGid ) ) {
						cosGid = records[0]?.gid ?? 0;
						cosSlot = -1;
						cosPage = 0;
						cosDraft = records[0]?.commandMode ?? 0;
					}
					const record = records.find( r => r.gid === cosGid ), busy = !!game?.inventoryPending;
					if ( cosTab === 1 && !record?.inventory || cosTab === 2 && record?.band !== 4 ) cosTab = 0;
					nativeFrame( root, px, py, record?.name ?? "" );
					nativePage( hudData.windows.ifcos!, px, py );
					[
						"UIIT_STT_COSNEWUI_TABMENU_BASICINFO",
						"UIIT_STT_COS_INVENTORY",
						"UIIT_STT_COSNEWUI_TABMENU_TECHNOLOGY"
					].forEach( ( key, i ) =>
						nativeTab(
							"cos-tab:" + i,
							hudCopy( key ),
							[ px + 18 + i * 78, py + 44, 72, 24 ],
							cosTab === i,
							// 6A1404: companion tabs have an asymmetric left inset.
							{
								family: "com_long_tab",
								client: [ 3, 6, 6, 0 ],
								disabled: panel === "Shop" ||
									(i === 1 ? !record?.inventory : i === 2 ? record?.band !== 4 : false)
							}
						)
					);
					const ox = px + 12,
						oy = py + 66,
						page = hudData
							.windows[cosTab === 1 ? "ifcosinventory" : cosTab === 2 ? "ifcossetup" : "ifcosinfo"]!;
					// CIFCOSInfo_SetCompanion (6A69F0) shows each block by record
					// class; the gauges (35..37) fill from the record below.
					const sections = cosInfoSections( cosClass( record?.band ?? 0 ) ?? -1 );
					const hiddenInfo = [
						35,
						36,
						37,
						...(sections.hp ? [] : [ 30, 40, 41 ]),
						...(sections.rentTime ? [] : [ 50, 51, 52 ]),
						...(sections.growth ?
							[ 100 ] :
							[ 31, 32, 42, 43, 44, 45, 60, 61, 62, 63, 65, 66, 67, 68, 71, 72, 73, 76, 77, 78 ])
					];
					nativePage( page, ox, oy, cosTab === 0 ? hiddenInfo : [] );
					if ( cosTab === 1 ) {
						const pages = Math.max( 1, Math.ceil( (record?.status ?? 0) / 28 ) );
						cosPage = Math.min( cosPage, pages - 1 );
						for ( let i = 0; i < 28; i++ ) {
							const slot = cosPage * 28 + i,
								item = record?.inventory?.find( r => r.slot === slot ),
								r: UiRect = [ ox + 41 + (i % 7) * 36, oy + 59 + Math.floor( i / 7 ) * 36, 32, 32 ];
							nativeItem(
								"cos-slot:" + slot,
								item,
								r,
								busy || slot >= (record?.status ?? 0),
								slot === cosSlot
							);
						}
						authoredText(
							page.GDR_COS_INVENTORY_NAME ?? Object.values( page ).find( n => n.id === 6 )!,
							ox,
							oy,
							record?.name ?? ""
						);
						nativeSpin(
							Object.values( page ).find( n => n.id === 17 )!,
							ox,
							oy,
							"cos-prev",
							"cos-next",
							cosPage,
							pages
						);
					} else if ( cosTab === 0 && record ) {
						const reference = hudData.cosReferences.get( record.refObjId );
						const node = ( id: number ) => Object.values( page ).find( n => n.id === id );
						const texts: [number, string][] = [
							// CIFCosInfo_RefreshName (6A5320): an unnamed companion reads "No name".
							[ 27, record.name || hudCopy( "UIIT_STT_COSNEWUI_TITLE" ) ]
						];
						if ( sections.hp ) {
							const ratio = cosStatusRatios( record, reference ).hp ?? 1;
							texts.push( [ 41, cosHpText( record, reference ) ] );
							const gauge = node( 35 );
							if ( gauge ) authoredGauge( "cos-info-hp", record.gid, gauge, ox, oy, ratio );
						}
						if ( sections.rentTime ) {
							// 6A4FD0 reads the rent left on the companion's summoner item.
							const summoner = game?.inventory?.find( item => item.slot === record.inventorySlot );
							const remaining = (summoner?.summon?.remainingSeconds ?? 0) * 1000;
							texts.push( [
								52,
								cosRentText(
									remaining,
									hudCopy( "PARAM_DAY" ),
									hudCopy( "PARAM_HOUR" ),
									hudCopy( "PARAM_MINUTE" )
								)
							] );
						}
						if ( sections.growth ) {
							const satiety = record.satiety ?? 0;
							texts.push( [ 43, cosSatietyText( satiety ) ] );
							const hgp = node( 36 );
							if ( hgp ) {
								authoredGauge(
									"cos-info-hgp",
									record.gid,
									hgp,
									ox,
									oy,
									cosStatusRatios( record, reference ).hgp ?? 1
								);
							}
							const required = record.level === undefined ? undefined : levels.get( record.level )?.[0];
							if ( record.experience && required ) {
								const [low, high] = record.experience;
								const current = BigInt( high ) << 32n | BigInt( low );
								const exp = cosExperienceText( current, BigInt( required ) );
								texts.push( [ 45, exp.text ] );
								const gauge = node( 37 );
								if ( gauge ) {
									authoredGauge(
										"cos-info-exp",
										record.gid,
										gauge,
										ox,
										oy,
										Math.min( 1, exp.ratio )
									);
								}
							}
							if ( record.level !== undefined ) texts.push( [ 65, String( record.level ) ] );
							if ( reference ) {
								// 6A4600 greys the ability values at low satiety (0xFF999999).
								const ability = cosAbilities(
									record,
									reference,
									reference.skills.map( skill => {
										const attack = hudData.tooltipSkills.get( skill )?.attack;
										return attack?.present ? attack : undefined;
									} )
								);
								const values: [number, string][] = [
									[ 66, String( ability.hit ) ],
									[ 67, cosAttackText( ability.physical ) ],
									[ 68, String( ability.physicalDefence ) ],
									[ 76, String( ability.parry ) ],
									[ 77, cosAttackText( ability.magical ) ],
									[ 78, String( ability.magicalDefence ) ]
								];
								for ( const [id, value] of values ) {
									const at = node( id );
									if ( at ) {
										authoredText(
											ability.low ? { ...at, color: COS_LOW_SATIETY } : at,
											ox,
											oy,
											value
										);
									}
								}
							}
						}
						for ( const [id, value] of texts ) {
							const at = node( id );
							if ( at ) authoredText( at, ox, oy, value );
						}
					} else if ( cosTab !== 0 ) {
						for ( const node of Object.values( page ).filter( n => n.type === "CIFCheckBox" ) ) {
							const bit = 1 << (node.id - 25),
								path = node.texture.replace( "_off", "_" + (cosDraft & bit ? "on" : "off") );
							authoredImage( node, ox, oy, path );
							controls.push( {
								id: "cos-setting:" + bit,
								label: node.name,
								rect: authoredRect( node, ox, oy ),
								kind: "button",
								selected: !!(cosDraft & bit),
								disabled: busy
							} );
						}
						for ( const [id, bit] of [ [ 20, 128 ], [ 22, 64 ] ] as const ) {
							const node = Object.values( page ).find( n => n.id === id )!,
								r = authoredRect( node, ox, oy );
							for ( let i = 0; i < 2; i++ ) {
								const selected = bit === 128 ?
										!!(cosDraft & bit) === (i === 0) :
										!!(cosDraft & bit) === (i === 1),
									path = ROOT + "interface/ifcommon/com_radiobutton_" + (selected ? "on" : "off") +
										".png";
								paths.push( path );
								if ( resources.has( path ) ) rect( [ r[0] + i * 130, r[1], 16, 16 ], white, path );
								controls.push( {
									id: "cos-radio:" + bit + ":" + i,
									label: node.name,
									rect: [ r[0] + i * 130, r[1], 130, 16 ],
									kind: "button",
									selected,
									disabled: busy
								} );
								const key = bit === 128 ?
									"UIIT_STT_COSNEWUI_PICKUP_SWITCH_" + (i ? "OFF" : "ON") :
									"UIIT_STT_COSNEWUI_PICKUPITEM_ASSORTMENT_" + (i ? "ALL" : "SELF");
								quads.push(
									...text.quads(
										hudCopy( key ),
										[ r[0] + i * 130 + 20, r[1] + 2, 110, 14 ],
										full,
										white
									)
								);
							}
						}
						for ( const node of Object.values( page ).filter( n => n.type === "CIFButton" ) ) {
							const id = node.id === 40 ? "cos-save" : "cos-reset";
							authoredLabeledButton( node, ox, oy, id, hudCopy( node.text ), busy );
						}
					}
					endWindow( admission, "service:COS inventory" );
				}
				/*
				================
				exchangeWindow

				CIFExchange (ginterface GDR_EXCHANGE, resinfo\ifexchange.txt): the
				partner's twelve slots and gold above, the own below, the money
				button and the confirm button that locks and then approves (6B2280).
				================
				*/
				function exchangeWindow() {
					const state = game?.exchange, root = hudData?.root.GDR_EXCHANGE, page = hudData?.windows.ifexchange;
					if ( !state?.open || !root || !page ) {
						exchangeHud.reset();
						return;
					}
					const admission = beginWindow(),
						[px, py] = windowOrigin( "Exchange", [
							Math.max( 0, w / 2 - root.rect[2] - 8 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] ),
						at = ( id: number ) => Object.values( page ).find( n => n.id === id )!,
						partner = next.entities.find( e => e.gid === state.partner );
					nativeFrame(
						root,
						px,
						py,
						hudCopy( root.text ) + (partner ? " - " + partner.name : ""),
						"exchange-close"
					);
					nativePage( page, px, py, [ 11, 12, 15 ] );
					for ( const id of [ 13, 14 ] ) authoredText( at( id ), px, py, hudCopy( at( id ).text ) );
					for (
						const [rows, base, mine] of [ [ state.theirs, 100, false ], [ state.own, 200, true ] ] as const
					) {
						for ( let slot = 0; slot < 12; slot++ ) {
							const node = at( base + slot ),
								r = authoredRect( node, px, py ),
								row = rows.find( offer => offer.slot === slot );
							const icon = row ? iconPath( row.item.icon ) : null;
							if ( row && icon ) {
								image( r, icon );
								itemEffects( (mine ? "exchange-my:" : "exchange-their:") + slot, row.item, r );
								itemCount( row.item, r );
							}
							controls.push( {
								id: (mine ? "exchange-my:" : "exchange-their:") + slot,
								label: row?.item.name ?? "Exchange slot " + slot,
								rect: r,
								kind: "button",
								disabled: mine && state.ownLocked
							} );
						}
					}
					authoredText( { ...at( 18 ), rect: [ 71, 152, 100, 16 ] }, px, py, String( state.theirGold ) );
					partyEdit(
						{ ...at( 19 ), rect: [ 71, 296, 100, 16 ] },
						px,
						py,
						"exchange-gold",
						exchangeHud.gold() || String( state.ownGold ),
						12
					);
					authoredButton(
						at( 15 ),
						px,
						py,
						"exchange-gold-set",
						hudCopy( "UIIT_STT_GOLD" ),
						state.ownLocked
					);
					authoredLabeledButton(
						at( 11 ),
						px,
						py,
						"exchange-confirm",
						hudCopy( at( 11 ).text ),
						state.approved || state.ownLocked && !state.theirLocked
					);
					authoredLabeledButton( at( 12 ), px, py, "exchange-cancel", hudCopy( at( 12 ).text ) );
					endWindow( admission, "service:Exchange" );
				}
				exchangeWindow();
				/*
				================
				stallWindow

				CIFStall (ginterface GDR_STALL, resinfo\ifstall.txt): the title and
				greeting with their change buttons (5, 6), the trading-state button
				(4) and line (14), and ten ifstallslot cells over the display (12).
				The owner drops bag items on empty cells and drags offers back to the
				bag while the stall is being modified; a visitor buys from an open
				stall.
				================
				*/
				function stallWindow() {
					const state = game?.stall,
						root = hudData?.root.GDR_STALL,
						page = hudData?.windows.ifstall,
						cell = hudData?.windows.ifstallslot;
					if ( !state || (state.phase !== "owner" && state.phase !== "visitor") || !root || !page || !cell ) {
						return;
					}
					const admission = beginWindow(),
						[px, py] = windowOrigin( "Stall", [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] ),
						at = ( id: number ) => Object.values( page ).find( n => n.id === id )!,
						part = ( id: number ) => Object.values( cell ).find( n => n.id === id )!,
						owner = state.phase === "owner",
						keeper = next.entities.find( e => e.gid === state.owner ),
						title = owner ? state.title : keeper?.titleText ?? "",
						presentation = stallTradingPresentation( owner, state.open );
					nativeFrame( root, px, py, hudCopy( root.text ), owner ? "stall-close" : "stall-leave" );
					// 0x5A2277/0x5A23F7 replace the invalid authored icon; never request it.
					nativePage( page, px, py, [ 3, 4, 5, 6, 14, 15 ] );
					authoredImage( at( 15 ), px, py, presentation.icon );
					authoredImage( at( 14 ), px, py );
					// The chat module (3): the latest stall lines over the input row.
					const box = authoredRect( at( 3 ), px, py ),
						rows = Math.max( 0, Math.floor( box[3] / STALL_CHAT_ROW ) - 1 ),
						said = (game.chat?.lines ?? []).filter( line => line.channel === STALL_CHAT_CHANNEL ).slice(
							-rows
						);
					for ( const [i, line] of said.entries() ) {
						const row: UiRect = [ box[0] + 4, box[1] + i * STALL_CHAT_ROW, box[2] - 8, STALL_CHAT_ROW ];
						quads.push(
							...text.quads( line.name + ":" + line.text, row, box, white, { overflow: "clip" } )
						);
					}
					partyEdit(
						{
							...at( 3 ),
							rect: [
								at( 3 ).rect[0] + 4,
								at( 3 ).rect[1] + at( 3 ).rect[3] - STALL_CHAT_ROW,
								at( 3 ).rect[2] - 8,
								14
							]
						},
						px,
						py,
						STALL_CHAT_TEXT,
						stallHud.chat(),
						STALL_CHAT_LIMIT
					);
					authoredText( at( 10 ), px, py, title );
					authoredText( at( 11 ), px, py, state.greeting );
					authoredText(
						at( 14 ),
						px,
						py,
						hudCopy( presentation.status )
					);
					if ( owner ) {
						authoredLabeledButton(
							at( 4 ),
							px,
							py,
							"stall-trading",
							hudCopy( presentation.toggle )
						);
						authoredButton(
							at( 5 ),
							px,
							py,
							"stall-change-title",
							hudCopy( "UIIT_STT_INSERT_STALL_NAME" ),
							state.open
						);
						authoredButton( at( 6 ), px, py, "stall-change-greeting", hudCopy( "UIIT_STT_STALL" ) );
					}
					const display = at( 12 ).rect;
					for ( let slot = 0; slot < STALL_SLOTS; slot++ ) {
						const ox = px + display[0] + (slot % 2) * STALL_CELL_PITCH_X,
							oy = py + display[1] + Math.floor( slot / 2 ) * STALL_CELL_PITCH_Y,
							offer = state.offers.find( row => row.slot === slot ),
							r = authoredRect( part( 1 ), ox, oy );
						const background = offer ? STALL_SLOT_IMAGES.occupied : STALL_SLOT_IMAGES.empty;
						paths.push( background );
						const backgroundSize = resources.size( background );
						if ( backgroundSize ) image( [ ox, oy, backgroundSize[0], backgroundSize[1] ], background );
						nativePage( cell, ox, oy, [ 2, 3, 4, 5 ] );
						const icon = offer ? iconPath( offer.item.icon ) : null;
						if ( offer && icon ) {
							image( r, icon, [ 1, 1, 1, presentation.slotAlpha ] );
							itemEffects( "stall-slot:" + slot, offer.item, r );
							itemCount( offer.item, r );
						}
						if ( offer ) {
							authoredText( part( 2 ), ox, oy, offer.item.name ?? "" );
							authoredText( part( 3 ), ox, oy, `${offer.quantity} ${hudCopy( "UIIT_STT_UNIT" )}` );
							const price = moneyPresentation( String( offer.price ) );
							authoredText(
								{ ...part( 4 ), color: price.color },
								ox,
								oy,
								`${price.text} ${hudCopy( "UIIT_STT_GOLD" )}`
							);
						}
						controls.push( {
							id: "stall-slot:" + slot,
							label: offer?.item.name ?? "Stall slot " + slot,
							rect: r,
							kind: "button",
							disabled: owner ? state.open : !state.open || !offer,
							draggable: owner && !state.open && !!offer
						} );
						if ( offer ) {
							authoredButton(
								part( 5 ),
								ox,
								oy,
								"stall-modify:" + slot,
								hudCopy( "UIIT_STT_TOGGLE_STORE_PRICE_CHANGE" ),
								!presentation.canModify
							);
						}
					}
					endWindow( admission, "service:Stall" );
				}
				stallWindow();
				/*
				================
				stallNetworkWindow

				CIFStallNetwork (ginterface GDR_STALL_NETWORK, resinfo\ifstallnetwork
				.txt): the category combos (41..43) and search button (51), fifteen
				ifstallnetworkslot result rows (100..114) under the sort buttons
				(60..64), the page manager (80), the carried gold (55) and the
				purchase button (50).
				================
				*/
				function stallNetworkWindow() {
					const state = game?.stall,
						root = hudData?.root.GDR_STALL_NETWORK,
						page = hudData?.windows.ifstallnetwork,
						cell = hudData?.windows.ifstallnetworkslot;
					if ( !state?.network.open || !root || !page || !cell ) return;
					const network = state.network,
						draft = stallHud.network(),
						roots = stallCategories.roots(),
						large = roots?.[draft.large],
						medium = large?.children[draft.medium],
						admission = beginWindow(),
						[px, py] = windowOrigin( "Stall network", [
							Math.max( 0, (w - root.rect[2]) / 2 ),
							Math.max( 0, (h - root.rect[3]) / 2 ),
							root.rect[2],
							root.rect[3]
						] ),
						at = ( id: number ) => Object.values( page ).find( n => n.id === id )!,
						part = ( id: number ) => Object.values( cell ).find( n => n.id === id )!,
						level = ( row: StallListing ) => {
							const fields = row.item.tooltip?.fields;
							return fields?.reqLevelType1 === 1 ? fields.requiredLevel ?? 0 : 0;
						};
					nativeFrame( root, px, py, hudCopy( root.text ), "stall-net-close" );
					const rowIds = Array.from( { length: 15 }, ( _, i ) => 100 + i );
					nativePage( page, px, py, [
						41,
						42,
						43,
						44,
						45,
						46,
						47,
						50,
						51,
						52,
						53,
						55,
						60,
						61,
						62,
						63,
						64,
						65,
						80,
						...rowIds
					] );
					for ( const id of [ 44, 45, 46, 47, 52, 53, 65 ] ) {
						authoredText( at( id ), px, py, hudCopy( at( id ).text ) );
					}
					const choose = hudCopy( "UIIT_CTL_WARENETWORK_SCAN_SELECT" );
					const combos = [
						{
							id: STALL_COMBO_LARGE,
							entries: (roots ?? []).map( row => hudCopy( row.label ) ),
							value: large ? hudCopy( large.label ) : choose,
							disabled: !roots
						},
						{
							id: STALL_COMBO_MEDIUM,
							entries: (large?.children ?? []).map( row => hudCopy( row.label ) ),
							value: medium ? hudCopy( medium.label ) : choose,
							disabled: !large
						},
						{
							id: STALL_COMBO_DEGREE,
							entries: [
								hudCopy( "UIIT_CTL_WARENETWORK_SCAN_END" ),
								...Array.from( { length: medium?.degrees ?? 0 }, ( _, i ) => String( i + 1 ) )
							],
							value: draft.degree ? String( draft.degree ) : hudCopy( "UIIT_CTL_WARENETWORK_SCAN_END" ),
							disabled: !medium?.degrees
						}
					];
					for ( const combo of combos ) {
						comboBox(
							authoredRect( at( combo.id ), px, py ),
							"stall-net-combo:" + combo.id,
							hudCopy( at( combo.id - 41 + 44 ).text ),
							combo.value,
							combo.disabled
						);
					}
					authoredLabeledButton(
						at( 51 ),
						px,
						py,
						"stall-net-search",
						hudCopy( at( 51 ).text ),
						!medium || !stallHud.searchReady( uiNow )
					);
					const sorts: readonly [number, StallNetworkSort][] = [
						[ 60, "number" ],
						[ 61, "name" ],
						[ 62, "quantity" ],
						[ 63, "level" ],
						[ 64, "price" ]
					];
					for ( const [id, sort] of sorts ) {
						authoredLabeledButton( at( id ), px, py, "stall-net-sort:" + sort, hudCopy( at( id ).text ) );
					}
					const order = stallNetworkOrder( network.rows, draft, network.rows.map( level ) );
					for ( let i = 0; i < rowIds.length; i++ ) {
						const index = order[i], row = index === undefined ? undefined : network.rows[index];
						const [ox, oy] = authoredRect( at( rowIds[i]! ), px, py );
						nativePage( cell, ox, oy, [ 11, 12, 13, 14, 15, 16 ] );
						if ( !row || index === undefined ) continue;
						const r = authoredRect( part( 12 ), ox, oy ), icon = iconPath( row.item.icon );
						if ( icon ) {
							image( r, icon );
							itemEffects( "stall-net-row:" + index, row.item, r );
							itemCount( row.item, r );
						}
						authoredText( part( 11 ), ox, oy, String( network.page * 15 + index + 1 ) );
						authoredText( part( 13 ), ox, oy, row.item.name ?? "" );
						authoredText( part( 14 ), ox, oy, String( row.quantity ) );
						authoredText( part( 15 ), ox, oy, String( level( row ) || "" ) );
						authoredText( part( 16 ), ox, oy, String( row.price ) );
						controls.push( {
							id: "stall-net-row:" + index,
							label: row.item.name ?? "Stall network row " + index,
							rect: authoredRect( at( rowIds[i]! ), px, py ),
							kind: "button",
							selected: draft.row === index
						} );
					}
					// The page manager (80): previous and next around page / pages.
					const pager = authoredRect( at( 80 ), px, py ), pagerY = pager[1] + 1;
					button( "stall-net-prev", "<", pager[0], pagerY, 40, network.page <= 0 );
					button(
						"stall-net-next",
						">",
						pager[0] + pager[2] - 40,
						pagerY,
						40,
						network.page + 1 >= network.pages
					);
					quads.push(
						...text.quads(
							network.pages ? `${network.page + 1} / ${network.pages}` : "",
							[ pager[0] + 40, pager[1], pager[2] - 80, pager[3] ],
							full,
							white,
							{ hAlign: 1, vAlign: 1 }
						)
					);
					authoredText( at( 55 ), px, py, String( game?.progression?.gold ?? 0 ) );
					authoredLabeledButton(
						at( 50 ),
						px,
						py,
						"stall-net-buy",
						hudCopy( at( 50 ).text ),
						draft.row < 0 || network.buying !== null
					);
					const open = combos.find( combo => combo.id === draft.combo && !combo.disabled );
					if ( open ) {
						const r = authoredRect( at( open.id ), px, py ),
							list: UiRect = [ r[0], r[1] + r[3], r[2], open.entries.length * 18 ];
						rect( list, [ 0, 0, 0, 1 ] );
						blocks.push( list );
						for ( const [i, value] of open.entries.entries() ) {
							const entry: UiRect = [ list[0], list[1] + i * 18, list[2], 18 ];
							quads.push(
								...text.quads( value, [ entry[0] + 4, entry[1], entry[2] - 8, 18 ], entry, white )
							);
							controls.push( { id: "stall-net-choice:" + i, label: value, rect: entry, kind: "button" } );
						}
					}
					endWindow( admission, "service:Stall network" );
				}
				stallNetworkWindow();
				/*
				================
				grantPanel

				CIFGuildGrantPower (resinfo\ifguildgrantpower.txt) at its GrantPower
				section: the column titles, five member rows with their rights
				checkboxes (ifguildgrantpowerslot), confirm and cancel. Only the
				master may change a right.
				================
				*/
				function grantPanel(
					page: AuthoredLayout,
					slot: AuthoredLayout,
					ox: number,
					oy: number,
					editable: boolean
				) {
					const at = ( id: number ) => Object.values( page ).find( n => n.id === id )!,
						slotAt = ( id: number ) => Object.values( slot ).find( n => n.id === id )!,
						list = at( 6 );
					nativePage( page, ox, oy, [ 4, 5, 11 ] );
					for ( const id of [ 12, 13, 14, 15, 16 ] ) {
						authoredText( at( id ), ox, oy, hudCopy( at( id ).text ) );
					}
					authoredText( at( 11 ), ox, oy, hudCopy( at( 11 ).text ) );
					grantPowerHud.visible().forEach( ( { row, mask }, i ) => {
						const rx = ox + list.rect[0], ry = oy + list.rect[1] + i * 23;
						authoredText( slotAt( 10 ), rx, ry, row.name );
						GRANT_RIGHTS.forEach( ( right, column ) => {
							const box = slotAt( 11 + column ),
								path = box.texture.replace( "_off", mask & right ? "_on" : "_off" );
							authoredImage( box, rx, ry, path );
							controls.push( {
								id: "guild-grant:" + row.id + ":" + right,
								label: hudCopy( at( 12 + column ).text ),
								rect: authoredRect( box, rx, ry ),
								kind: "button",
								selected: !!(mask & right),
								disabled: !editable
							} );
						} );
					} );
					authoredLabeledButton( at( 4 ), ox, oy, "guild-grant-ok", hudCopy( at( 4 ).text ), !editable );
					authoredLabeledButton( at( 5 ), ox, oy, "guild-grant-cancel", hudCopy( at( 5 ).text ) );
				}
				/*
				================
				unionPage

				CIFAllianceGuild (resinfo\ifallianceguild.txt): the leading guild and
				the union's size, the selected guild's details, the union's guilds
				(ifallianceguildslot rows of 23 px, 5FB6F0) and the three commands
				CIFAllianceGuild_RefreshButtons (5F6880) arms.
				================
				*/
				function unionPage( page: AuthoredLayout, slot: AuthoredLayout, gx: number, gy: number ) {
					const social = game?.social,
						at = ( id: number ) => Object.values( page ).find( n => n.id === id )!,
						slotAt = ( id: number ) => Object.values( slot ).find( n => n.id === id )!,
						alliances = social?.alliances ?? [],
						leader = allianceLeader( social ),
						selected = alliances.find( row => row.id === socialMember ),
						armed = allianceButtons( social );
					nativePage( page, gx, gy, [ 63, 64, 81, 82, 83 ] );
					for ( const id of [ 21, 22, 23, 24, 42, 43, 44, 45 ] ) {
						authoredText( at( id ), gx, gy, hudCopy( at( id ).text ) );
					}
					if ( !leader ) authoredText( at( 30 ), gx, gy, hudCopy( "UIIT_STT_NOT_EXIST_GUILD_RESPECT_ALLY" ) );
					else {
						authoredText( at( 26 ), gx, gy, leader.name );
						authoredText( at( 28 ), gx, gy, leader.master );
						authoredText( at( 29 ), gx, gy, String( alliances.length ) );
					}
					if ( !selected ) authoredText( at( 52 ), gx, gy, hudCopy( "UIIT_STT_NOT_EXIST_GUILD" ) );
					else {
						authoredText( at( 47 ), gx, gy, selected.name );
						authoredText( { ...at( 48 ), color: gold }, gx, gy, String( selected.level ) );
						authoredText( at( 50 ), gx, gy, selected.master );
						authoredText( at( 51 ), gx, gy, String( selected.flags ) );
					}
					authoredLabeledButton( at( 63 ), gx, gy, "union-sort:name", hudCopy( at( 63 ).text ) );
					authoredLabeledButton( at( 64 ), gx, gy, "union-sort:level", hudCopy( at( 64 ).text ) );
					const list = at( 62 ), rows = unionHud.order( alliances );
					rows.slice( 0, Math.floor( list.rect[3] / 23 ) ).forEach( ( row, i ) => {
						const ox = gx + list.rect[0], oy = gy + list.rect[1] + i * 23;
						authoredText( slotAt( 11 ), ox, oy, row.name );
						authoredText( slotAt( 12 ), ox, oy, String( row.level ) );
						controls.push( {
							id: "social-member:" + row.id,
							label: row.name,
							rect: [ ox, oy, list.rect[2], 23 ],
							kind: "button",
							selected: row.id === socialMember
						} );
					} );
					for (
						const [id, action, allowed] of [
							[ 81, "guild-union-invite", armed.invite ],
							[ 82, "guild-union-exit", armed.exit ],
							[ 83, "guild-union-expel", armed.expel ]
						] as const
					) authoredLabeledButton( at( id ), gx, gy, action, hudCopy( at( id ).text ), !allowed );
				}
				/*
				================
				guildWarPage

				600120's hostile guild list and the authored war-score page share
				selection. Unknown enemy details remain native unknown labels.
				================
				*/
				function guildWarPage( scores: boolean, gx: number, gy: number ) {
					const page = hudData?.windows[scores ? "ifguildwar" : "ifhostileguild"];
					if ( !page ) return;
					guildWarHud.reconcile( game?.social );
					const state = guildWarHud.state(),
						social = game?.social,
						rows = guildWarHud.order( social?.wars ?? [] );
					const selected = rows.find( row => row.id === state.selected );
					const at = ( id: number ) => Object.values( page ).find( n => n.id === id )!;
					const put = ( id: number, value: string ) => {
						if ( at( id ) ) authoredText( at( id ), gx, gy, value );
					};
					nativePage( page, gx, gy, scores ? [ 30, 60, 61, 62 ] : [ 33, 34, 51, 52 ] );
					const count = scores ? 6 : 9, first = Math.min( state.offset, Math.max( 0, rows.length - count ) );
					for ( let i = 0; i < count; i++ ) {
						const row = rows[first + i],
							r: UiRect = scores ?
								[ gx + 11, gy + 33 + i * 23, 144, 24 ] :
								[ gx + 236, gy + 35 + i * 23, 177, 24 ];
						if ( row ) {
							if ( row.id === state.selected ) rect( r, [ .25, .3, .35, .6 ] );
							quads.push( ...text.quads( row.name, [ r[0] + 6, r[1] + 7, r[2] - 12, 14 ], r, white ) );
							controls.push( {
								id: "war-select:" + row.id,
								label: row.name,
								rect: r,
								kind: "button",
								selected: row.id === state.selected
							} );
						}
					}
					const scroll = chatScrollbar(
						"war-scroll",
						scores ? [ gx + 155, gy + 48, 16, 93 ] : [ gx + 413, gy + 51, 16, 159 ],
						rows.length,
						count,
						Math.max( 0, rows.length - count - first ),
						resources.size,
						full,
						hover,
						pressed
					);
					paths.push( ...scroll.paths );
					quads.push( ...scroll.quads );
					controls.push( ...scroll.controls );
					if ( !scores ) {
						for ( const id of selected ? [ 12, 13, 14, 15 ] : [ 12 ] ) put( id, hudCopy( at( id ).text ) );
						if ( selected ) {
							put( 17, selected.name );
							for ( const id of [ 20, 21 ] ) put( id, hudCopy( "UIIT_CTL_GUILD_DONOTKNOW" ) );
						} else put( 22, hudCopy( at( 22 ).text ) );
						for ( const id of [ 33, 34 ] ) {
							authoredLabeledButton( at( id ), gx, gy, "war-sort:" + id, hudCopy( at( id ).text ) );
						}
						const master = social?.guild?.members.find( m => m.id === social.self )?.grade === 0;
						authoredLabeledButton( at( 51 ), gx, gy, "war-declare", hudCopy( at( 51 ).text ), !master );
						authoredLabeledButton(
							at( 52 ),
							gx,
							gy,
							"war-surrender",
							hudCopy( at( 52 ).text ),
							!master || !selected || !!selected.ending
						);
						return;
					}
					for ( const id of [ 30, 60, 61, 62 ] ) {
						authoredLabeledButton(
							at( id ),
							gx,
							gy,
							id === 30 ? "war-sort" : "war-contribution-sort:" + id,
							hudCopy( at( id ).text )
						);
					}
					for ( const id of [ 90, 91, 92, 93, 100, 101, 102 ] ) put( id, hudCopy( at( id ).text ) );
					if ( selected ) {
						put( 95, selected.name );
						put( 96, String( selected.localScore ) );
						put( 97, String( selected.enemyScore ) );
						put(
							105,
							selected.type === 0 ?
								hudCopy( "UIIT_CTL_GUILDWAR_UNLIMITED" ) :
								String( warScoreLimits()[selected.type] ?? 0 )
						);
						put( 106, selected.word38.toLocaleString( "en-US" ) + " " + hudCopy( "UIIT_STT_GOLD" ) );
						put(
							107,
							selected.word3c === WAR_UNLIMITED ?
								hudCopy( "UIIT_CTL_GUILDWAR_UNLIMITED" ) :
								[
									Math.trunc( (selected.word3c | 0) / 3600 ),
									Math.trunc( ((selected.word3c | 0) % 3600) / 60 ),
									(selected.word3c | 0) % 60
								].map( n => String( n ).padStart( 2, "0" ) ).join( " : " )
						);
					} else put( 108, hudCopy( at( 108 ).text ) );
					const members = guildWarHud.members( social?.guild?.members ?? [] );
					members.slice( state.contributionOffset, state.contributionOffset + 3 ).forEach( ( row, i ) => {
						const y = gy + 211 + i * 23;
						for (
							const [value, x, width] of [ [ row.name, gx + 17, 162 ], [
								String( row.rank ),
								gx + 187,
								52
							], [ String( row.warScore ?? 0 ), gx + 244, 151 ] ] as const
						) quads.push( ...text.quads( value, [ x, y, width, 14 ], full, white ) );
					} );
					const memberScroll = chatScrollbar(
						"war-members",
						[ gx + 414, gy + 220, 16, 24 ],
						members.length,
						3,
						Math.max( 0, members.length - 3 - state.contributionOffset ),
						resources.size,
						full,
						hover,
						pressed
					);
					paths.push( ...memberScroll.paths );
					quads.push( ...memberScroll.quads );
					controls.push( ...memberScroll.controls );
				}
				if ( (panel === "Guild" || panel === "Guild tools") && hudData ) {
					/*
					================
					appendCompactGuild

					The compact community page reflows controls before text layout. Glyph
					metrics stay native; the desktop resource layouts below are untouched.
					Existing action IDs retain the social, grant and war state owners.
					================
					*/
					function appendCompactGuild() {
						if ( !hudData ) return;
						const NATIVE_WIDTH = 477, ROW_HEIGHT = 23, LINE_HEIGHT = 18, TAB_HEIGHT = 24;
						const width = Math.min( w, NATIVE_WIDTH ),
							columns = width < NATIVE_WIDTH ? 3 : 6,
							height = Math.min( h, columns === 3 ? 443 : 375 ),
							x = Math.floor( (w - width) / 2 ),
							y = Math.floor( (h - height) / 2 ),
							left = x + 12,
							inner = width - 24,
							top = y + 39 + (6 / columns) * TAB_HEIGHT + 4,
							bottom = y + height - 10,
							root = hudData.root.GDR_COMMUNITY!,
							page = hudData.windows.ifguild!,
							social = game?.social,
							guild = social?.guild,
							self = guild?.members.find( member => member.id === social?.self );
						image( [ x + 6, y + 28, width - 12, height - 34 ], at( page, 1 ).texture );
						nativeFrame( { ...root, rect: [ 0, 0, width, height ] }, x, y, hudCopy( root.text ) );
						/*
						================
						put

						Clip/ellipsize within the new cell before glyph quads are generated.
						================
						*/
						function put( value: string, r: UiRect ) {
							const cell: UiRect = [
								Math.floor( r[0] ),
								Math.floor( r[1] ),
								Math.floor( r[2] ),
								Math.floor( r[3] )
							];
							quads.push(
								...text.quads( value, cell, cell, white, { overflow: "ellipsis", vAlign: 1 } )
							);
						}
						/*
						================
						at
						================
						*/
						function at( layout: AuthoredLayout, id: number ) {
							return Object.values( layout ).find( node => node.id === id )!;
						}
						/*
						================
						command
						================
						*/
						function command( node: AuthoredControl, r: UiRect, action: string, disabled = false ) {
							const caption = hudCopy( node.text );
							authoredButton( { ...node, rect: r }, 0, 0, action, caption, disabled );
							put( caption, [ r[0] + 4, r[1], r[2] - 8, r[3] ] );
						}
						/*
						================
						field
						================
						*/
						function field( layout: AuthoredLayout, id: number, value: string, r: UiRect ) {
							put( hudCopy( at( layout, id ).text ) + " " + value, r );
						}
						/*
						================
						memberRow
						================
						*/
						function memberRow( id: string, label: string, r: UiRect, selected: boolean ) {
							image( r, ROOT + "interface/guild/gil_bar02_deselect.png" );
							if ( selected ) rect( r, [ .25, .3, .35, .6 ] );
							controls.push( { id, label, rect: r, kind: "button", selected } );
						}
						[
							"UIIT_STT_GUILD_INFO",
							"UIIT_CTL_GUILD_RESPECT",
							"UIIT_CTL_WARSIT_WARSITUATION",
							"UIIT_CTL_FRIEND",
							"UIIT_STT_BLOCKMAN_BLOCK",
							"UIIT_STT_LETTER"
						].forEach( ( key, i ) =>
							nativeTab(
								"guild-tab:" + i,
								hudCopy( key ),
								[
									left + (i % columns) * (inner / columns),
									y + 39 + Math.floor( i / columns ) * TAB_HEIGHT,
									inner / columns - 2,
									TAB_HEIGHT
								],
								guildTab === i,
								{
									family: "com_long_tab",
									client: [ 6, 5, 6, 0 ],
									disabled: ![ 0, 1, 2, 4 ].includes( i )
								}
							)
						);
						if ( guildTab === 0 ) {
							const half = Math.floor( inner / 2 ),
								leader = guild?.members.find( member => member.grade === 0 );
							put( guild?.name ?? hudCopy( "UIIT_STT_NO_GUILD" ), [ left, top, half, LINE_HEIGHT ] );
							if ( !guild ) return;
							field( page, 33, String( guild.level ), [ left + half, top, half, LINE_HEIGHT ] );
							field( page, 34, leader?.name ?? "", [ left, top + LINE_HEIGHT, inner, LINE_HEIGHT ] );
							field( page, 35, String( guild.members.length ), [
								left,
								top + LINE_HEIGHT * 2,
								half,
								LINE_HEIGHT
							] );
							field( page, 36, String( guild.gp ), [
								left + half,
								top + LINE_HEIGHT * 2,
								half,
								LINE_HEIGHT
							] );
							const noticeY = top + LINE_HEIGHT * 3;
							image( [ left, noticeY, inner, 24 ], ROOT + "interface/guild/gil_windo02_off.png" );
							put( guild.subject || hudCopy( "UIIT_MSG_GUILD_COMMON_NOTEXIST" ), [
								left + 4,
								noticeY,
								inner - 8,
								24
							] );
							const listTop = noticeY + 28;
							if (
								grantPowerHud.isOpen() && hudData.windows.ifguildgrantpower &&
								hudData.windows.ifguildgrantpowerslot
							) {
								const grants = hudData.windows.ifguildgrantpower,
									slot = hudData.windows.ifguildgrantpowerslot,
									nameWidth = Math.floor( inner * .35 ),
									cell = (inner - nameWidth) / GRANT_RIGHTS.length,
									footerY = bottom - 24,
									pagerY = footerY - 28,
									rowsY = listTop + 24,
									capacity = Math.max( 1, Math.floor( (pagerY - rowsY - 4) / ROW_HEIGHT ) ),
									visible = grantPowerHud.visible( capacity );
								GRANT_RIGHTS.forEach( ( right, column ) =>
									put( hudCopy( at( grants, 12 + column ).text ), [
										left + nameWidth + column * cell,
										listTop,
										cell - 2,
										24
									] )
								);
								visible.forEach( ( { row, mask }, i ) => {
									const ry = rowsY + i * ROW_HEIGHT;
									put( row.name, [ left, ry, nameWidth - 4, ROW_HEIGHT ] );
									GRANT_RIGHTS.forEach( ( right, column ) => {
										const node = at( slot, 11 + column ),
											r: UiRect = [ left + nameWidth + column * cell, ry, cell - 2, ROW_HEIGHT ],
											box: UiRect = [ Math.floor( r[0] + (cell - 16) / 2 ), ry + 3, 0, 0 ];
										// The selected mark is an ifcheckbox child, not a sibling of
										// com_checkbutton02_off. Keep both authored texture dimensions.
										authoredImage( { ...node, rect: box }, 0, 0 );
										if ( mask & right ) {
											image(
												[ box[0], box[1], 16, 16 ],
												ROOT + "interface/ifcommon/com_checkbutton_on.png"
											);
										}
										controls.push( {
											id: "guild-grant:" + row.id + ":" + right,
											label: hudCopy( at( grants, 12 + column ).text ),
											rect: r,
											kind: "button",
											selected: !!(mask & right),
											disabled: self?.grade !== 0
										} );
									} );
								} );
								button( "guild-grant-prev", "<", left, pagerY, 40, !grantPowerHud.canScroll( -1 ) );
								button(
									"guild-grant-next",
									">",
									left + inner - 40,
									pagerY,
									40,
									!grantPowerHud.canScroll( 1 )
								);
								command(
									at( grants, 4 ),
									[ left, footerY, half - 2, 24 ],
									"guild-grant-ok",
									self?.grade !== 0
								);
								command(
									at( grants, 5 ),
									[ left + half, footerY, half - 2, 24 ],
									"guild-grant-cancel"
								);
								return;
							}
							const actionColumns = columns === 3 ? 3 : 4,
								actionRows = Math.ceil( 7 / actionColumns ),
								actionTop = bottom - actionRows * 26,
								count = Math.max(
									1,
									Math.min( 6, Math.floor( (actionTop - listTop - 50) / ROW_HEIGHT ) )
								),
								rows = [ ...guild.members ].sort( ( a, b ) =>
									(guildSort === 122 ?
										a.level - b.level :
										guildSort === 123 ?
										a.grade - b.grade :
										guildSort === 124 ?
										a.donated - b.donated :
										a.name.localeCompare( b.name )) * (guildDescending ? -1 : 1)
								),
								cellWidths = [ inner * .39, inner * .13, inner * .22, inner * .26 ],
								cellLeft = [
									0,
									cellWidths[0]!,
									cellWidths[0]! + cellWidths[1]!,
									inner - cellWidths[3]!
								];
							[ 121, 122, 123, 124 ].forEach( ( id, i ) =>
								command(
									at( page, id ),
									[ left + cellLeft[i]!, listTop, cellWidths[i]! - 2, 24 ],
									"guild-sort:" + id
								)
							);
							socialPage = Math.min( socialPage, Math.max( 0, Math.ceil( rows.length / count ) - 1 ) );
							// The list blocks the world behind its empty area; member buttons
							// must follow it in hit order so a touch selects the painted row.
							controls.push( {
								id: "guild-list",
								label: hudCopy( "UIIT_STT_GUILD_INFO" ),
								rect: [ left, listTop + 26, inner, count * ROW_HEIGHT ],
								kind: "region"
							} );
							rows.slice( socialPage * count, socialPage * count + count ).forEach( ( row, i ) => {
								const ry = listTop + 26 + i * ROW_HEIGHT;
								memberRow(
									"social-member:" + row.id,
									row.name,
									[ left, ry, inner, ROW_HEIGHT ],
									row.id === socialMember
								);
								const role = ({
										1: "COMMANDER",
										2: "SUBCOMMANDER",
										4: "BATTLEMANAGER",
										8: "PRODUCTMANAGER",
										16: "TRAINERMANAGER",
										32: "ENGINEER"
									} as Record<number, string>)[row.role],
									caption = guildNameMode === 0 ?
										row.name :
										guildNameMode === 1 ?
										row.grant :
										role ?
										hudCopy( "UIIT_STT_FORT_GUILD_" + role ) :
										"";
								[ caption, String( row.level ), row.grant, String( row.donated ) ].forEach( (
									value,
									column
								) => put( value, [
									left + cellLeft[column]! + (column === 0 ? 31 : 3),
									ry,
									cellWidths[column]! - (column === 0 ? 34 : 6),
									ROW_HEIGHT
								] ) );
								const slot = hudData!.windows.ifguildmemberslot!;
								[ 9, 10 ].forEach( ( id, icon ) => {
									const node = at( slot, id );
									authoredImage(
										{ ...node, rect: [ left + 2 + icon * 14, ry + 4, 14, 14 ] },
										0,
										0,
										id === 9 ?
											node.texture.replace( "_off", row.offline ? "_off" : "_on" ) :
											node.texture.replace(
												"china",
												hudData!.countries[row.model] === 1 ? "europe" : "china"
											)
									);
								} );
							} );
							button( "social-prev", "<", left, actionTop - 24, 28, socialPage === 0 );
							command( at( page, 126 ), [ left + 32, actionTop - 24, 24, 24 ], "guild-sort:126" );
							put(
								hudCopy(
									[
										"UIIT_STT_GUILDSMAN",
										"UIIT_STT_TITLE",
										"UIIT_STT_GUILD_POSITION"
									][guildNameMode]!
								),
								[ left + 60, actionTop - 24, inner - 92, 24 ]
							);
							button(
								"social-next",
								">",
								left + inner - 28,
								actionTop - 24,
								28,
								(socialPage + 1) * count >= rows.length
							);
							[
								[ 101, "guild-invite" ],
								[ 102, "guild-dialog:authority" ],
								[ 103, "guild-kick" ],
								[ 105, "guild-dialog:title" ],
								[ 106, "guild-dialog:role" ],
								[ 45, "guild-dialog:donate" ],
								[ 62, "guild-dialog:notice" ]
							].forEach( ( [id, action], i ) => {
								const number = Number( id ),
									allowed = number === 45 ? !!self : number === 101 ?
										!!(self?.permissions! & 1) :
										number === 103 ?
										!!(self?.permissions! & 2) :
										number === 62 ?
										!!(self?.permissions! & 16) :
										number === 102 ?
										true :
										self?.grade === 0 && (number !== 105 || guild.level >= 4);
								command(
									at( page, number ),
									[
										left + (i % actionColumns) * (inner / actionColumns),
										actionTop + Math.floor( i / actionColumns ) * 26,
										inner / actionColumns - 2,
										24
									],
									String( action ),
									!allowed
								);
							} );
							return;
						}
						const contentTop = top + 30;
						if ( guildTab === 1 ) {
							[ "UIIT_STT_GUILD_RESPECT_ALLY", "UIIT_STT_GUILD_RESPECT_WAR" ].forEach( ( key, i ) =>
								button(
									"war-relation:" + i,
									hudCopy( key ),
									left + i * (inner / 2),
									top,
									inner / 2 - 2,
									false,
									guildWarHud.state().relation === i
								)
							);
						}
						if ( guildTab === 1 && guildWarHud.state().relation === 0 ) {
							const alliance = hudData.windows.ifallianceguild;
							if ( !alliance ) return;
							const leader = allianceLeader( social ),
								selected = social?.alliances?.find( row => row.id === socialMember ),
								armed = allianceButtons( social ),
								rows = unionHud.order( social?.alliances ?? [] ),
								half = Math.floor( inner / 2 ),
								listX = left + half,
								count = Math.max( 1, Math.floor( (bottom - contentTop - 54) / ROW_HEIGHT ) );
							[
								[ 22, leader?.name ?? "" ],
								[ 23, leader?.master ?? "" ],
								[ 24, String( rows.length ) ],
								[ 43, selected?.name ?? "" ],
								[ 64, selected ? String( selected.level ) : "" ],
								[ 44, selected?.master ?? "" ],
								[ 45, selected ? String( selected.flags ) : "" ]
							].forEach( ( [id, value], i ) =>
								field( alliance, Number( id ), String( value ), [
									left,
									contentTop + i * 28,
									half - 6,
									24
								] )
							);
							command( at( alliance, 63 ), [ listX, contentTop, half * .7 - 2, 24 ], "union-sort:name" );
							command(
								at( alliance, 64 ),
								[ listX + half * .7, contentTop, half * .3 - 2, 24 ],
								"union-sort:level"
							);
							rows.slice( 0, count ).forEach( ( row, i ) => {
								const ry = contentTop + 26 + i * ROW_HEIGHT;
								memberRow(
									"social-member:" + row.id,
									row.name,
									[ listX, ry, half, ROW_HEIGHT ],
									row.id === socialMember
								);
								put( row.name, [ listX + 3, ry, half * .7 - 6, ROW_HEIGHT ] );
								put( String( row.level ), [ listX + half * .7, ry, half * .3, ROW_HEIGHT ] );
							} );
							[ [ 81, "guild-union-invite", armed.invite ], [ 82, "guild-union-exit", armed.exit ], [
								83,
								"guild-union-expel",
								armed.expel
							] ].forEach( ( [id, action, allowed], i ) =>
								command(
									at( alliance, Number( id ) ),
									[ left + i * (inner / 3), bottom - 24, inner / 3 - 2, 24 ],
									String( action ),
									!allowed
								)
							);
							return;
						}
						const scores = guildTab === 2, war = hudData.windows[scores ? "ifguildwar" : "ifhostileguild"];
						if ( !war ) return;
						guildWarHud.reconcile( social );
						const state = guildWarHud.state(),
							rows = guildWarHud.order( social?.wars ?? [] ),
							selected = rows.find( row => row.id === state.selected ),
							count = scores ? 6 : 9,
							first = Math.min( state.offset, Math.max( 0, rows.length - count ) ),
							half = Math.floor( inner / 2 ),
							listWidth = half - 20,
							listY = scores ? top + 26 : contentTop + 26,
							pitch = Math.min( ROW_HEIGHT, Math.floor( (bottom - listY - (scores ? 98 : 28)) / count ) );
						command(
							at( war, scores ? 30 : 33 ),
							[ left, listY - 26, listWidth, 24 ],
							scores ? "war-sort" : "war-sort:33"
						);
						if ( !scores ) command( at( war, 34 ), [ left + half, listY - 26, half, 24 ], "war-sort:34" );
						rows.slice( first, first + count ).forEach( ( row, i ) => {
							const r: UiRect = [ left, listY + i * pitch, listWidth, pitch ];
							memberRow( "war-select:" + row.id, row.name, r, row.id === state.selected );
							put( row.name, [ r[0] + 3, r[1], r[2] - 6, r[3] ] );
						} );
						// Thumb travel is shared with the existing event owner (93 / 159 / 24).
						const scroll = chatScrollbar(
							"war-scroll",
							[ left + listWidth, listY + 16, 16, scores ? 93 : 159 ],
							rows.length,
							count,
							Math.max( 0, rows.length - count - first ),
							resources.size,
							full,
							hover,
							pressed
						);
						paths.push( ...scroll.paths );
						quads.push( ...scroll.quads );
						controls.push( ...scroll.controls );
						if ( !scores ) {
							put( selected?.name ?? hudCopy( at( war, 22 ).text ), [ left + half, listY, half, 24 ] );
							if ( selected ) {
								for ( const [i, id] of [ 20, 21 ].entries() ) {
									put( hudCopy( "UIIT_CTL_GUILD_DONOTKNOW" ), [
										left + half,
										listY + 26 + i * 24,
										half,
										24
									] );
								}
							}
							command(
								at( war, 51 ),
								[ left, bottom - 24, half - 2, 24 ],
								"war-declare",
								self?.grade !== 0
							);
							command(
								at( war, 52 ),
								[ left + half, bottom - 24, half - 2, 24 ],
								"war-surrender",
								self?.grade !== 0 || !selected || !!selected.ending
							);
							return;
						}
						const values = selected ?
							[
								selected.name,
								String( selected.localScore ),
								String( selected.enemyScore ),
								selected.type === 0 ?
									hudCopy( "UIIT_CTL_GUILDWAR_UNLIMITED" ) :
									String( warScoreLimits()[selected.type] ?? 0 ),
								selected.word38.toLocaleString( "en-US" ) + " " + hudCopy( "UIIT_STT_GOLD" ),
								selected.word3c === WAR_UNLIMITED ?
									hudCopy( "UIIT_CTL_GUILDWAR_UNLIMITED" ) :
									[
										Math.trunc( (selected.word3c | 0) / 3600 ),
										Math.trunc( ((selected.word3c | 0) % 3600) / 60 ),
										(selected.word3c | 0) % 60
									].map( n => String( n ).padStart( 2, "0" ) ).join( " : " )
							] :
							[];
						[ 91, 92, 93, 100, 101, 102 ].forEach( ( id, i ) =>
							field( war, id, values[i] ?? "", [ left + half, top + i * 24, half, 24 ] )
						);
						const memberY = bottom - 3 * ROW_HEIGHT,
							memberWidths = [ inner * .45, inner * .2, inner * .35 ],
							memberLeft = [ 0, inner * .45, inner * .65 ],
							members = guildWarHud.members( guild?.members ?? [] );
						[ 60, 61, 62 ].forEach( ( id, i ) =>
							command( at( war, id ), [
								left + memberLeft[i]!,
								memberY - 24,
								memberWidths[i]! - (i === 2 ? 18 : 2),
								24
							], "war-contribution-sort:" + id )
						);
						members.slice( state.contributionOffset, state.contributionOffset + 3 ).forEach( ( row, i ) =>
							[ row.name, String( row.rank ), String( row.warScore ?? 0 ) ].forEach( ( value, column ) =>
								put( value, [
									left + memberLeft[column]!,
									memberY + i * ROW_HEIGHT,
									memberWidths[column]! - (column === 2 ? 18 : 2),
									ROW_HEIGHT
								] )
							)
						);
						const memberScroll = chatScrollbar(
							"war-members",
							[ left + inner - 16, memberY + 16, 16, 24 ],
							members.length,
							3,
							Math.max( 0, members.length - 3 - state.contributionOffset ),
							resources.size,
							full,
							hover,
							pressed
						);
						paths.push( ...memberScroll.paths );
						quads.push( ...memberScroll.quads );
						controls.push( ...memberScroll.controls );
					}
					if ( compact && (w < 477 || h < 393) ) {
						const admission = beginWindow();
						appendCompactGuild();
						endWindow( admission );
					} else {
						const admission = beginWindow(),
							root = hudData.root.GDR_COMMUNITY!,
							[px, py] = windowOrigin( "Guild", [
								Math.max( 0, (w - 477) / 2 ),
								Math.max( 0, (h - 393) / 2 ),
								root.rect[2],
								root.rect[3]
							] ),
							page = hudData.windows.ifguild!,
							gx = px + 13,
							gy = py + 61,
							guild = game?.social?.guild;
						nativeFrame( root, px, py, hudCopy( root.text ) );
						[
							"UIIT_STT_GUILD_INFO",
							"UIIT_CTL_GUILD_RESPECT",
							"UIIT_CTL_WARSIT_WARSITUATION",
							"UIIT_CTL_FRIEND",
							"UIIT_STT_BLOCKMAN_BLOCK",
							"UIIT_STT_LETTER"
						].forEach( ( key, i ) =>
							nativeTab(
								"guild-tab:" + i,
								hudCopy( key ),
								[ px + 15 + i * 75, py + 39, 72, 24 ],
								guildTab === i,
								// 5DFCF0: community tab content starts five pixels down.
								{
									family: "com_long_tab",
									client: [ 6, 5, 6, 0 ],
									disabled: ![ 0, 1, 2, 4 ].includes( i )
								}
							)
						);
						authoredChrome(
							{ ...hudData.windows.ifcommunity!.GDR_COMMUNITY_GUILD!, type: "CIFFrame" },
							px,
							py
						);
						if (
							guildTab === 1 && hudData.windows.ifallianceguild && hudData.windows.ifallianceguildslot
						) {
							const relations = hudData.windows.ifguildrelations!;
							nativePage( relations, gx, gy, [ 9, 10 ] );
							for (
								const [i, symbol] of [ "UIIT_STT_GUILD_RESPECT_ALLY", "UIIT_STT_GUILD_RESPECT_WAR" ]
									.entries()
							) {
								const selected = guildWarHud.state().relation === i;
								const r: UiRect = [ gx + 12 + i * 76, gy + 5, 76, 28 ];
								image( r, ROOT + "interface/guild/gil_subj_tab_" + (selected ? "on" : "off") + ".png" );
								quads.push(
									...text.quads( hudCopy( symbol ), [ r[0], r[1] + 8, r[2], 14 ], full, white, {
										hAlign: 1
									} )
								);
								controls.push( {
									id: "war-relation:" + i,
									label: hudCopy( symbol ),
									rect: r,
									kind: "button",
									selected
								} );
							}
							if ( guildWarHud.state().relation === 0 ) {
								unionPage(
									hudData.windows.ifallianceguild,
									hudData.windows.ifallianceguildslot,
									gx + 6,
									gy + 29
								);
							} else guildWarPage( false, gx + 6, gy + 29 );
						} else if ( guildTab === 2 ) {
							nativePage( hudData.windows.ifwarstate!, gx, gy );
							guildWarPage( true, gx + 6, gy + 29 );
						} else {
							// 5EA9D0 creates Create before the subsequent resource sections. Their
							// insertion lists reverse within a section, not across constructor calls.
							for ( const id of [ 1, 2, 3 ] ) {
								authoredChrome( Object.values( page ).find( n => n.id === id )!, gx, gy );
							}
							nativePage( page, gx, gy, [ 1, 2, 3, 104 ] );
							/*
						================
						at
						================
						*/
							const at = ( id: number ) => Object.values( page ).find( n => n.id === id )!;
							const notice = at( 61 ), noticePath = ROOT + "interface/guild/gil_windo02_off.png";
							authoredImage( notice, gx, gy, noticePath );
							authoredText( at( 63 ), gx, gy, hudCopy( at( 63 ).text ) );
							for ( const id of [ 121, 122, 123, 124, 126 ] ) {
								const node = at( id ),
									caption = hudCopy(
										id === 121 ?
											[
												"UIIT_STT_GUILDSMAN",
												"UIIT_STT_TITLE",
												"UIIT_STT_GUILD_POSITION"
											][guildNameMode]! :
											node.text
									);
								authoredLabeledButton( node, gx, gy, "guild-sort:" + id, caption );
							}
							// 5E8850 creates empty 312x24 rows until six exist. Those native row
							// textures are the backing; a bare scroll-manager rectangle is transparent.
							for ( let i = 0; i < 6; i++ ) {
								const path = ROOT + "interface/guild/gil_bar02_deselect.png";
								paths.push( path );
								if ( resources.has( path ) ) {
									rect( [ gx + 17, gy + 163 + i * 23, 312, 24 ], white, path );
								}
							}
							if ( !guild ) authoredText( at( 38 ), gx, gy, hudCopy( "UIIT_STT_NO_GUILD" ) );
							if ( guild ) {
								const leader = guild.members.find( m => m.grade === 0 ),
									self = guild.members.find( m => m.id === game?.social?.self );
								for (
									const [id, value] of [
										[ 38, guild.name ],
										[ 39, String( guild.level ) ],
										[ 41, leader?.name ?? "" ],
										[ 42, String( guild.members.length ) ],
										[ 44, String( guild.gp ) ]
									] as const
								) authoredText( { ...at( id ), ...(id === 39 ? { color: gold } : {}) }, gx, gy, value );
								authoredText(
									{ ...notice, client: [ 70, 7, 0, 0 ] },
									gx,
									gy,
									guild.subject || hudCopy( "UIIT_MSG_GUILD_COMMON_NOTEXIST" )
								);
								const rows = [ ...guild.members ].sort( ( a, b ) =>
										(guildSort === 122 ?
											a.level - b.level :
											guildSort === 123 ?
											a.grade - b.grade :
											guildSort === 124 ?
											a.donated - b.donated :
											a.name.localeCompare( b.name )) * (guildDescending ? -1 : 1)
									),
									s = at( 82 ),
									slot = hudData.windows.ifguildmemberslot!;
								socialPage = Math.min( socialPage, Math.max( 0, Math.ceil( rows.length / 6 ) - 1 ) );
								if (
									grantPowerHud.isOpen() && hudData.windows.ifguildgrantpower &&
									hudData.windows.ifguildgrantpowerslot
								) {
									grantPanel(
										hudData.windows.ifguildgrantpower,
										hudData.windows.ifguildgrantpowerslot,
										gx + at( 150 ).rect[0],
										gy + at( 150 ).rect[1],
										self?.grade === 0
									);
								} else {rows.slice( socialPage * 6, socialPage * 6 + 6 ).forEach( ( row, i ) => {
										const ox = gx + s.rect[0], oy = gy + s.rect[1] + i * 23;
										nativePage( slot, ox, oy, [ 9, 10 ] );
										const roleSymbol = ({
											1: "COMMANDER",
											2: "SUBCOMMANDER",
											4: "BATTLEMANAGER",
											8: "PRODUCTMANAGER",
											16: "TRAINERMANAGER",
											32: "ENGINEER"
										} as Record<number, string>)[row.role];
										const memberCaption = guildNameMode === 0 ?
											row.name :
											guildNameMode === 1 ?
											row.grant :
											roleSymbol ?
											hudCopy( "UIIT_STT_FORT_GUILD_" + roleSymbol ) :
											"";
										for (
											const [id, value] of [ [ 11, memberCaption ], [ 12, String( row.level ) ], [
												13,
												row.grant
											], [ 14, String( row.donated ) ] ] as const
										) {
											authoredText(
												Object.values( slot ).find( n => n.id === id )!,
												ox,
												oy,
												value
											);
										}
										const online = Object.values( slot ).find( n => n.id === 9 )!;
										authoredImage(
											online,
											ox,
											oy,
											online.texture.replace( "_off", row.offline ? "_off" : "_on" )
										);
										const race = Object.values( slot ).find( n => n.id === 10 )!;
										authoredImage(
											race,
											ox,
											oy,
											race.texture.replace(
												"china",
												hudData.countries[row.model] === 1 ? "europe" : "china"
											)
										);
										controls.push( {
											id: "social-member:" + row.id,
											label: row.name,
											rect: [ ox, oy, 312, 23 ],
											kind: "button",
											selected: row.id === socialMember
										} );
									} );}
								// 5E3090 mode 3 hides the command section beneath the panel.
								if ( !grantPowerHud.isOpen() ) {
									for (
										const [id, action] of [
											[ 101, "guild-invite" ],
											[ 102, "guild-dialog:authority" ],
											[ 103, "guild-kick" ],
											[ 105, "guild-dialog:title" ],
											[ 106, "guild-dialog:role" ],
											[ 45, "guild-dialog:donate" ],
											[ 62, "guild-dialog:notice" ]
										] as const
									) {
										const node = id === 105 ?
											{ ...at( id ), rect: [ 353, 223, 0, 0 ] as UiRect } :
											at( id );
										const allowed = id === 45 ?
											!!self :
											id === 101 ?
											!!(self?.permissions! & 1) :
											id === 103 ?
											!!(self?.permissions! & 2) :
											id === 62 ?
											!!(self?.permissions! & 16) :
											id === 102 ?
											true :
											self?.grade === 0 && (id !== 105 || guild.level >= 4);
										authoredButton( node, gx, gy, action, hudCopy( node.text ), !allowed );
										if ( node.text ) authoredText( node, gx, gy, hudCopy( node.text ) );
									}
								}
								controls.push( {
									id: "guild-list",
									label: hudCopy( "UIIT_STT_GUILD_INFO" ),
									rect: authoredRect( s, gx, gy ),
									kind: "region"
								} );
							}
						}
						endWindow( admission );
					}
				}
				if ( [ "Magic Pop", "Chat" ].includes( panel ) ) {
					const admission = beginWindow();
					const px = Math.max( 12, (w - 380) / 2 ), py = Math.max( 150, (h - 440) / 2 );
					windowBox( panel, px, py, 380, 420 );
					closeButton( px + 354, py + 10 );
					if ( panel === "Magic Pop" ) {
						const active = game?.gacha,
							busy = !!game?.inventoryPending,
							prizes = gachaPrizes(),
							cards = game?.inventory.filter( item => isGachaTicket( item.typeFlags ) ) ?? [];
						prizes.forEach( ( prize, index ) =>
							button(
								"gacha-prize:" + prize.entry,
								localization.text( prize.nameSymbol, "Loading item..." ) + " x" + prize.quantity,
								px + 22 + (index % 2) * 170,
								py + 45 + Math.floor( index / 2 ) * 27,
								164,
								busy,
								prize.entry === gachaEntry
							)
						);
						gachaPage = Math.min( gachaPage, Math.max( 0, Math.ceil( cards.length / 2 ) - 1 ) );
						cards.slice( gachaPage * 2, gachaPage * 2 + 2 ).forEach( ( item, index ) =>
							button(
								"gacha-card:" + item.slot,
								(item.name ?? "Magic Pop card") + " x" + item.quantity,
								px + 22,
								py + 216 + index * 28,
								336,
								busy,
								gachaSlot === item.slot
							)
						);
						button( "gacha-prev", "Previous", px + 22, py + 276, 164, busy || gachaPage === 0 );
						button(
							"gacha-next",
							"Next",
							px + 194,
							py + 276,
							164,
							busy || (gachaPage + 1) * 2 >= cards.length
						);
						button(
							"gacha-roll",
							"Play",
							px + 22,
							py + 314,
							336,
							busy || !cards.some( item => item.slot === gachaSlot )
						);
						label(
							active?.phase === "rolling" ?
								"Rolling..." :
								active?.phase === "waiting" ?
								"Waiting for result..." :
								active?.result === "win" ?
								"Win: " + localization.text(
									prizes.find( p => p.refObjId === active.reward?.refObjId )?.nameSymbol,
									"Reward"
								) + " x" + active.reward?.quantity :
								active?.result === "lose" ?
								"Try again" :
								active?.error !== null && active?.error !== undefined ?
								"Magic Pop rejected: " + active.error :
								"Select a prize and a card.",
							px + 22,
							py + 354,
							gold
						);
					} else if ( panel === "Chat" ) {
						const channels = [ [ 1, "All" ], [ 2, "Whisper" ], [ 4, "Party" ], [ 5, "Guild" ], [
							11,
							"Union"
						] ] as const;
						channels.forEach( ( [id, name], i ) =>
							button(
								"chat-channel:" + id,
								name,
								px + 20 + i * 68,
								py + 42,
								64,
								false,
								chatChannel === id
							)
						);
						const history = game?.chat?.lines.filter( line =>
							line.channel === chatChannel ||
							(chatChannel === 1 && (line.channel === 3 || line.channel === 6))
						) ?? [];
						chatPage = Math.min( chatPage, Math.max( 0, Math.ceil( history.length / 7 ) - 1 ) );
						const end = history.length - chatPage * 7,
							visible = history.slice( Math.max( 0, end - 7 ), end );
						visible.forEach( ( line, i ) => {
							const sender = line.name || next.entities.find( e => e.gid === line.gid )?.name ||
								"Unknown speaker";
							label(
								`${line.outgoing && line.channel === 2 ? "To " : ""}${sender}: ${line.text}`,
								px + 22,
								py + 78 + i * 22,
								white,
								[ px + 20, py + 76, 340, 205 ]
							);
						} );
						if ( history.length > 7 ) {
							button( "chat-older", "Older", px + 22, py + 244, 160, end <= 7 );
							button( "chat-newer", "Newer", px + 195, py + 244, 160, chatPage === 0 );
						}
						/*
						================
						editBox
						================
						*/
						function editBox( id: string, name: string, value: string, yy: number, maxLength: number ) {
							const r: UiRect = [ px + 22, yy, 336, 26 ];
							controls.push( { id, label: name, kind: "text", value, rect: r, maxLength } );
							rect( r, [ .02, .03, .02, 1 ] );
							quads.push(
								...text.quads(
									value || name,
									[ r[0] + 6, r[1] + 2, r[2] - 6, 22 ],
									r,
									value ? white : gold,
									{ vAlign: 0, overflow: "clip" }
								)
							);
							if ( focus === id ) {
								const prefix = text.run( value.slice( 0, selection[0] ) ),
									selected = text.run( value.slice( selection[0], selection[1] ) );
								if ( selection[1]! > selection[0]! ) {
									rect(
										[ r[0] + 6 + prefix.width, r[1] + 2, selected.width, 20 ],
										[ .3, .5, .8, .35 ],
										"",
										[ 0, 0, 1, 1 ],
										r
									);
								}
								rect( [ r[0] + 6 + prefix.width, r[1] + 3, 1, 19 ], gold, "", [ 0, 0, 1, 1 ], r );
								if ( composing ) {
									rect(
										[ r[0] + 6, r[1] + 23, text.run( value ).width, 1 ],
										gold,
										"",
										[ 0, 0, 1, 1 ],
										r
									);
								}
							}
						}
						if ( chatChannel === 2 ) {
							editBox( "chat-target", "Whisper recipient", chatTarget, py + 280, 127 );
						}
						editBox( "chat-text", "Chat message", chatText, py + 310, 140 );
						button(
							"chat-send",
							"Send",
							px + 250,
							py + 345,
							108,
							!chatText.trim() || !!game?.chat?.pending || (chatChannel === 2 && !chatTarget.trim())
						);
					}
					endWindow( admission );
				}
			}
			if ( worldVisible ) {
				const worldLabelStart = quads.length;
				paths.push( ...combatGlyphPaths );
				const path = ROOT + "icon/etc/icon_rudiment.png";
				const local = next.entities.find( e => e.gid === game?.localGid );
				const overheads = new Map(
					next.entities.map(
						entity => [ entity.gid, game ? overheadLayout( entity, local, game, options, hudCopy ) : null ]
					)
				);
				// Characters whose guild line, fortress mark or quick status bars show;
				// overheadBoardVisible shows their names with them.
				const overlaid = new Set<number>();
				// Mounts carrying a rider: their ride link hides their name board.
				const ridden = new Set( next.entities.flatMap( e => e.mountedOn ? [ e.mountedOn ] : [] ) );
				const rides = new Map( next.entities.filter( e => ridden.has( e.gid ) ).map( e => [ e.gid, e ] ) );
				const boardAt = ( entity: EntityState ) =>
					riderBoardPosition( entity, entity.mountedOn ? rides.get( entity.mountedOn ) : undefined );
				if ( game && hud.data() ) {
					for ( const entity of next.entities ) {
						if (
							hiddenSilkCos( entity, options.hideSilkCos ) ||
							next.blindHeld && blindableCharacter( entity, game?.localGid )
						) continue;
						const overlay = overheads.get( entity.gid );
						if (
							!overlay ||
							entity.gid !== next.hoveredEntity && !nameInRange( boardAt( entity ), local, game.pose )
						) continue;
						if ( overlay.fortressMark || overlay.guildText || overlay.status || overlay.stallText ) {
							overlaid.add( entity.gid );
						}
						if ( overlay.stallText ) {
							const value = overlay.stallText,
								width = text.run( value ).width,
								height = text.boardHeight(),
								left = -(width >> 1),
								top = overlay.stallY - (height >> 1);
							quads.push( {
								characterAnchor: entity.gid,
								rect: [ left - 1, top - 1, width + 2, height + 2 ],
								clip: full,
								uv: [ 0, 0, 1, 1 ],
								texture: "",
								color: [ 0, 0, 0, 96 / 255 ]
							} );
							quads.push(
								...text.quads( value, [ left, top, width, height ], full, [
									0xfe / 255,
									0xb5 / 255,
									1,
									1
								], {
									vAlign: 0
								} ).map( q => ({ ...q, characterAnchor: entity.gid }) )
							);
						}
						if ( overlay.fortressMark ) {
							const mark = overlay.fortressMark;
							paths.push( mark.path );
							if ( resources.has( mark.path ) ) {
								quads.push( {
									characterAnchor: entity.gid,
									rect: [ -16, mark.y, 32, 32 ],
									clip: full,
									uv: [ 0, 0, 1, 1 ],
									texture: mark.path,
									color: white,
									alphaCutoff: 128 / 255
								} );
							}
						}
						if ( overlay.guildText ) {
							const value = overlay.guildText,
								width = text.run( value ).width,
								height = text.boardHeight(),
								left = -(width >> 1),
								top = overlay.guildY - (height >> 1);
							quads.push( {
								characterAnchor: entity.gid,
								rect: [ left - 1, top - 1, width + 2, height + 2 ],
								clip: full,
								uv: [ 0, 0, 1, 1 ],
								texture: "",
								color: [ 0, 0, 0, 64 / 255 ]
							} );
							quads.push(
								...text.quads( value, [ left, top, width, height ], full, overlay.guildColor, {
									vAlign: 0
								} ).map( q => ({ ...q, characterAnchor: entity.gid }) )
							);
							// 86B350: the fortress emblem follows the rendered guild label by five pixels.
							if ( overlay.fortressCrest ) {
								const path = overlay.fortressCrest;
								paths.push( path );
								if ( resources.has( path ) ) {
									quads.push( {
										characterAnchor: entity.gid,
										rect: [ left + width + 5, top, 16, 16 ],
										clip: full,
										uv: [ 0, 0, 1, 1 ],
										texture: path,
										color: white,
										alphaCutoff: 128 / 255
									} );
								}
							}
							if ( next.session?.crestPrefix !== undefined && next.session.marksBase ) {
								for ( const crest of entityCrestFiles( entity, game, next.session.crestPrefix ) ) {
									const path = new URL( "/marks/" + crest.file, next.session.marksBase ).href;
									paths.push( path );
									if ( resources.has( path ) ) {
										quads.push( {
											characterAnchor: entity.gid,
											rect: [ left + crest.left, top, 16, 16 ],
											clip: full,
											uv: [ 0, 0, 1, 1 ],
											texture: path,
											color: white,
											alphaCutoff: 128 / 255
										} );
									}
								}
							}
						}
						const status = overlay.status;
						if ( !status ) continue;
						const half = status.mp === undefined,
							layout = hud.data()!.windows[half ? "ifquickstatehalfwnd" : "ifquickstatewnd"]!,
							prefix = half ? "GDR_QUICK_STATE_HARF_WND_" : "GDR_QUICK_STATE_WND_",
							offset = quads.length;
						authoredImage( layout[prefix + "BACKGROUND"]!, -37, overlay.statusY );
						for ( const [kind, fraction] of [ [ "HP", status.hp ], [ "MP", status.mp ] ] as const ) {
							if ( fraction !== undefined ) {
								authoredImage(
									layout[prefix + kind]!,
									-37,
									overlay.statusY,
									ROOT + "interface/ifcommon/quick_" + kind.toLowerCase() + ".png",
									fraction
								);
							}
						}
						for ( let i = offset; i < quads.length; i++ ) {
							quads[i] = { ...quads[i]!, characterAnchor: entity.gid };
						}
					}
				}

				for ( const entity of next.entities ) {
					if ( entity.groundItem ) {
						if (
							!groundItemNameVisible(
								entity,
								game?.pose ?? local,
								entity.gid === next.hoveredEntity,
								!!next.dropNamesHeld
							)
						) continue;
						const name = groundItemName( entity, hudCopy( "UIIT_STT_GOLD" ) ),
							width = text.run( name ).width,
							height = text.boardHeight();
						// 86E8FD -> 7827A0 sets backing ARGB 40000000 after construction.
						// 7832C0 draws a one-pixel margin; 86E8EB sets tint 1.
						const color: UiQuad["color"] = entity.groundItem.tint === 1 ?
							[ 114 / 255, 191 / 255, 1, 1 ] :
							white;
						if ( name && height ) {
							quads.push( {
								characterAnchor: entity.gid,
								occlusion: "none" as const,
								rect: [ -(width >> 1) - 1, -(height >> 1) - 1, width + 2, height + 2 ],
								clip: full,
								uv: [ 0, 0, 1, 1 ],
								texture: "",
								color: [ 0, 0, 0, 64 / 255 ]
							} );
						}
						quads.push(
							...text.quads( name, [ -(width >> 1), -(height >> 1), width, height ], full, color, {
								vAlign: 0
							} ).map( q => ({ ...q, characterAnchor: entity.gid, occlusion: "none" as const }) )
						);
						continue;
					}
					if (
						![ "local-player", "player", "monster", "npc", "cos" ].includes( entity.kind ) ||
						next.blindHeld && blindableCharacter( entity, game?.localGid )
					) continue;
					// 86AB90: the dress bar under the head, 4.8 px a second, shrinking
					// 0.48 px every 100 ms, in ARGB FFFFEE1F, whether or not the name shows.
					const progress = entity.actionProgress, clock = next.simulationTimeMs ?? now;
					if ( progress && clock - progress.startedAtMs < progress.seconds * 1000 ) {
						const tenths = Math.floor( Math.max( 0, clock - progress.startedAtMs ) / 100 );
						quads.push( {
							characterAnchor: entity.gid,
							occlusion: "none" as const,
							rect: [ -24, 10, progress.seconds * 4.8 - tenths * 0.48, 2 ],
							clip: full,
							uv: [ 0, 0, 1, 1 ],
							texture: "",
							color: [ 1, 0xee / 255, 0x1f / 255, 1 ]
						} );
						dirty = true;
					}
					const hovered = entity.gid === next.hoveredEntity, selected = entity.gid === game?.target;
					// One decision for the name and every overhead icon: an icon never
					// shows without its name (name-visibility.ts header).
					const named = !hiddenSilkCos( entity, options.hideSilkCos ) &&
						overheadBoardVisible(
							boardAt( entity ),
							local,
							hovered,
							options,
							game?.pose,
							overlaid.has( entity.gid ),
							ridden.has( entity.gid )
						);
					const partyMark = named ?
						monsterPartyNameplate( entity, [
							text.run( entity.name ?? "", selected ? 2 : 0 ).width,
							text.boardHeight()
						] ) :
						null;
					if ( partyMark ) {
						paths.push( partyMark.path );
						if ( resources.has( partyMark.path ) ) {
							quads.push( {
								characterAnchor: entity.gid,
								rect: partyMark.rect,
								clip: full,
								uv: [ 0, 0, 1, 1 ],
								texture: partyMark.path,
								color: white,
								alphaCutoff: 128 / 255
							} );
						}
					}
					if ( named && beginnerMarkShown( entity, options ) ) {
						const name = entity.name ?? (entity.gid === game?.localGid ? next.session?.character : "") ??
								"",
							width = text.run( name, selected ? 2 : 0 ).width,
							height = text.boardHeight();
						paths.push( path );
						if ( resources.has( path ) && height ) {
							quads.push( {
								characterAnchor: entity.gid,
								alphaCutoff: 128 / 255,
								rect: [ -(width >> 1) - 21.5, -(height >> 1) - 1.5, 16, 16 ],
								clip: full,
								uv: [ 0, 0, 1, 1 ],
								texture: path,
								color: white
							} );
						}
					}
					if ( !named ) continue;
					const name = entity.name ?? (entity.gid === game?.localGid ? next.session?.character : "") ?? "",
						width = text.run( name, selected ? 2 : 0 ).width,
						height = text.boardHeight();
					const left = -(width >> 1), top = -(height >> 1);
					if ( name && height ) {
						quads.push( {
							characterAnchor: entity.gid,
							rect: [ left - 1, top - 1, width + 2, height + 2 ],
							clip: full,
							uv: [ 0, 0, 1, 1 ],
							texture: "",
							color: [ 0, 0, 0, 64 / 255 ]
						} );
					}
					// CICUser 858810 gives the actual [GM] name prefix priority over player
					// relation colors. This is presentation, never a privilege check.
					const nameColor: UiQuad["color"] = nameColorRgba( entity.nameColor ?? 0xffffffff );
					quads.push(
						...text.quads( name, [ left, top, width, height ], full, nameColor, {
							vAlign: 0,
							fontStyle: selected ? 2 : 0
						} ).map( q => ({ ...q, characterAnchor: entity.gid }) )
					);
					if ( hovered && width ) {
						quads.push( {
							characterAnchor: entity.gid,
							rect: [ left, top + height - 1, width, 1 ],
							clip: full,
							uv: [ 0, 0, 1, 1 ],
							texture: "",
							color: nameColor
						} );
					}
				}
				const speakers = next.entities.filter( e => e.kind === "local-player" || e.kind === "player" );
				bugReport?.chat( game?.chat?.lines ?? [] );
				for ( const [gid, row] of speech.step( game?.chat?.lines ?? [], speakers, now ) ) {
					const lines = textBoardLines( row.text, 200, value => text.run( value ).width ),
						lineHeight = text.boardHeight(),
						height = lines.length * lineHeight,
						width = Math.max( 0, ...lines.map( value => text.run( value ).width ) ),
						left = -(width >> 1),
						top = (overheads.get( gid )?.speechY ?? 0) - height - 10;
					const color: UiQuad["color"] = row.channel === 3 ?
						[ 1, 174 / 255, 195 / 255, 1 ] :
						row.channel === 6 ?
						[ 1, 1, 0, 1 ] :
						row.channel === 13 ?
						[ 219 / 255, 173 / 255, 248 / 255, 1 ] :
						white;
					if ( text.height() ) {
						quads.push( {
							characterAnchor: gid,
							rect: [ left - 1, top - 1, width + 2, height + 2 ],
							clip: full,
							uv: [ 0, 0, 1, 1 ],
							texture: "",
							color: [ 0, 0, 0, 64 / 255 ]
						} );
					}
					lines.forEach( ( value, i ) =>
						quads.push(
							...text.quads( value, [ left, top + i * lineHeight, width, lineHeight ], full, color, {
								vAlign: 0
							} ).map( q => ({ ...q, characterAnchor: gid }) )
						)
					);
				}
				// Damage text follows the nametags and speech: world UI does not
				// depth-write, so later quads cover earlier ones. The renderer draws
				// it each frame (UiScene.damageText); this only demands its glyphs.
				// World annotations precede CIF windows; they must not bleed through menus.
				quads.unshift( ...quads.splice( worldLabelStart ) );
			}
			const guideData = guideResources.data();
			const academyVisible = !!(worldVisible && game?.academy && !game.academy.member &&
				(game.progression?.level ?? roster.find( c => c.name === next.session?.character )?.level ?? 40) <
					40);
			// CIFEventGuide::UpdateAcademy (6660F0): sound only on hidden -> shown.
			if ( academyVisible && !academyWasVisible ) sound( "quest" );
			academyWasVisible = academyVisible;
			if ( worldVisible && game?.academy ) {
				const academy = game.academy,
					level = game.progression?.level ?? roster.find( c => c.name === next.session?.character )?.level;
				if ( level !== undefined && level < 40 && !academy.member ) {
					const path = ROOT + "icon/etc/wintereventguide_1.png",
						r: UiRect = [ Math.max( 0, w - 46 ), 249, 40, 40 ];
					image( r, path );
					controls.push( {
						id: "academy-open",
						label: "Academy matching",
						kind: "button",
						rect: r,
						disabled: !!academy.request
					} );
					blocks.push( r );
				}
				if ( panel === "Academy Matching" && guideData ) {
					const admission = beginWindow();
					const [ax, ay] = windowOrigin( "Academy Matching", [
							Math.max( 0, (w - 786) / 2 ),
							Math.max( 0, (h - 482) / 2 ),
							786,
							482
						] ),
						r: UiRect = [ ax, ay, 786, 482 ];
					blocks.push( r );
					quads.push(
						...frameRing( r, FRAME, PARTS.map( p => resources.size( FRAME + p + ".png" ) ), full )
					);
					quads.push(
						...text.quads(
							guideData.strings.UIIT_PAG_TC_MACHING ?? "Academy Matching",
							[ ax + 10, ay + 12, 766, 12 ],
							full,
							white,
							{ hAlign: 1, vAlign: 1 }
						)
					);
					closeButton( ax + 760, ay + 10 );
					const field: Record<string, keyof import("@/engine/foundation/gameplay/academy").AcademyListing> = {
						ID_BTN: "id",
						LEVEL_BTN: "level",
						RACE_BTN: "model",
						NAME_BTN: "name",
						GRADUATE_BTN: "graduates",
						NUM_BTN: "students",
						GRADE_BTN: "grade"
					};
					const order = field[academySort] ?? "id",
						rows = academy.rows.filter( r =>
							(!academyFilter.name ||
								r.name.toLowerCase().includes( academyFilter.name.toLowerCase() )) &&
							(academyFilter.grade === 5 || r.grade === academyFilter.grade + 1) &&
							(academyFilter.kind === 2 || r.kind === academyFilter.kind)
						).sort( ( a, b ) => {
							const av = a[order], bv = b[order];
							return (typeof av === "number" && typeof bv === "number" ?
								av - bv :
								String( av ).localeCompare( String( bv ) )) * (academyAscending ? 1 : -1);
						} );
					const projected = academyLayout(
						guideData,
						{ ...academy, rows },
						academySelection,
						ax,
						ay,
						full,
						resources.size,
						( s, r, c, color, style ) => text.quads( s, r, c, color, style ),
						hover,
						pressed,
						{ name: academyName, grade: academyGrade, kind: academyKind, open: academyCombo },
						level
					);
					quads.push( ...projected.quads );
					paths.push( ...projected.paths );
					controls.push( ...projected.controls );
					endWindow( admission );
				}
			}
			if ( worldVisible && guideData && game?.guide ) {
				if ( !guideOrigin && pose ) guideOrigin = { regionId: pose.regionId, x: pose.x, z: pose.z };
				const state = game.guide;
				if ( state.event && state.event !== guideObserved ) {
					guideObserved = state.event;
					if ( state.event === 1 || panel === "Game Guide" ) {
						guideTab = "events";
						guideEvent = state.event;
						guideScroll = 0;
						setPanel( "Game Guide" );
						dirty = true;
					} else {
						if ( !guidePending ) sound( "quest" );
						guidePending = state.event;
					}
				}
				let event: number | null = null;
				/*
				================
				available
				================
				*/
				const available = ( id: number ) => {
					const article = guideData.articles.find( a => a.id === id );
					return article && (state.country === 0 || article.european !== null);
				};
				if ( state.pending?.some( available ) ) event = state.pending.find( available )!;
				else if ( !(state.seenMask & 1) && available( 1 ) ) event = 1;
				else if ( pose && guideOrigin ) {
					const max = game?.progression?.stats?.maxHp ?? roster.find( c =>
						c.name === next.session?.character
					)?.maxHp ?? 0;
					event = automaticGuide( state.seenMask, {
						moved: pose.regionId !== guideOrigin.regionId || pose.x !== guideOrigin.x ||
							pose.z !== guideOrigin.z,
						regionId: pose.regionId,
						monster: next.entities.some( e => e.kind === "monster" ),
						hp: local?.hp ?? max,
						maxHp: max
					} );
				}
				if ( event && !(guideRequested & (1 << (event - 1))) ) {
					const article = guideData.articles.find( r => r.id === event )!;
					const tokens = state.country === 1 ? article.european : article.tokens;
					const images = (tokens ?? []).flatMap( t => t.kind === "image" ? [ t.path ] : [] );
					paths.push( ...images );
					if ( tokens && text.path() && images.every( resources.has ) ) {
						sendGameplay( { kind: "guide-event", event } );
						guideRequested |= 1 << (event - 1);
					}
				}
			}
			if ( worldVisible && options.eventGuide && guidePending && panel !== "Game Guide" ) {
				const path = ROOT + "icon/etc/eventguide_1.png", r: UiRect = [ Math.max( 0, w - 46 ), 179, 40, 40 ];
				image( r, path );
				controls.push( { id: "guide-indicator", label: "Open Event Guide", kind: "button", rect: r } );
				blocks.push( r );
			}
			if ( worldVisible && panel === "Game Guide" && guideData ) {
				const admission = beginWindow();
				const questLevel = game?.progression?.level ??
					next.session?.characters?.find( c => c.name === next.session?.character )?.level;
				const questRows = guideTab === "quests" && questLevel !== undefined ?
					guideResources.quests(
						questLevel,
						(game?.quests ?? []).map( q => q.refId ),
						game?.completedQuests ?? []
					) :
					[];
				if ( guideX === null || guideY === null ) {
					[guideX, guideY] = windowPlacement.takeRemembered( "gameGuide", w, h, [ 420, 452 ] ) ??
						[ Math.trunc( w / 2 ) - 210, Math.trunc( h / 2 ) - 226 ];
				}
				const gx = guideX,
					gy = guideY,
					nodes = guideData.layout;
				controls.push( {
					id: "guide-drag",
					label: "Move Game Guide",
					kind: "button",
					draggable: true,
					rect: [ gx + 10, gy + 5, 380, 25 ]
				} );
				const bounds: UiRect = [ gx, gy, 420, 452 ];
				blocks.push( bounds );
				quads.push(
					...frameRing( bounds, FRAME, PARTS.map( p => resources.size( FRAME + p + ".png" ) ), full )
				);
				quads.push(
					...text.quads(
						guideTab === "quests" ? guideData.strings.UIIT_STT_QUESTGUIDE2! : guideData.caption,
						[ gx + 10, gy + 12, 399, 12 ],
						full,
						white,
						{ hAlign: 1, vAlign: 1 }
					)
				);
				closeButton( gx + 399, gy + 10 );
				for (
					const name of [
						"GDR_GUIDE_FRAME_1",
						"GDR_GUIDE_TILE_1",
						"GDR_GUIDE_TILE_2",
						"GDR_GUIDE_PAPER",
						"GDR_GUIDE_DATA_SCL_BG"
					]
				) {
					const node = nodes[name]!;
					const r = authoredRect( node, gx, gy );
					if ( node.type === "CIFFrame" ) {
						const family = PARTS.map( p => node.texture + p + ".png" );
						paths.push( ...family );
						quads.push( ...frameRing( r, node.texture, family.map( p => resources.size( p ) ), full ) );
					} else if ( node.type === "CIFNormalTile" ) {
						paths.push( node.texture );
						quads.push( ...normalTile( r, node.texture, resources.size( node.texture ), full ) );
					} else authoredImage( node, gx, gy );
				}
				const sidebarX = gx - (guideSidebar ? 221 : 19),
					side = guideData.artwork.side,
					handle = guideSidebar ? guideData.artwork.expanded : guideData.artwork.collapsed;
				for (
					const [path, sx, sy] of [ [ side, sidebarX, gy + 45 ], [ handle, sidebarX + 4, gy + 183 ] ] as const
				) {
					paths.push( path );
					const size = resources.size( path );
					if ( size ) rect( [ sx, sy, ...size ], white, path );
				}
				const handleSize = resources.size( handle );
				if ( handleSize ) {
					const r: UiRect = [ sidebarX + 4, gy + 183, ...handleSize ];
					controls.push( {
						id: "guide-sidebar",
						label: guideSidebar ? "Collapse guide index" : "Expand guide index",
						kind: "button",
						rect: r
					} );
					blocks.push( r );
				}
				if ( guideSidebar ) {
					const sx = gx - 206,
						sy = gy + 45,
						menuRect: UiRect = [ sx, sy, 213, 390 ],
						family = ROOT + "interface/messagebox/msgbox_window_",
						tile = ROOT + "interface/ifcommon/bg_tile/com_bg_tile_d.png";
					const familyPaths = PARTS.map( p => family + p + ".png" );
					paths.push( ...familyPaths );
					quads.push( ...frameRing( menuRect, family, familyPaths.map( p => resources.size( p ) ), full ) );
					blocks.push( menuRect );
					for ( const node of Object.values( guideData.menu ) ) {
						const r = authoredRect( node, sx, sy );
						if ( node.type === "CIFNormalTile" ) {
							paths.push( node.texture );
							quads.push( ...normalTile( r, node.texture, resources.size( node.texture ), full ) );
						} else {
							const family = PARTS.map( p => node.texture + p + ".png" );
							paths.push( ...family );
							quads.push( ...frameRing( r, node.texture, family.map( p => resources.size( p ) ), full ) );
						}
					}
					for ( const [i, tab] of ([ "general", "events", "quests" ] as const).entries() ) {
						const skin = guideTab === tab ? guideData.artwork.tabOn : guideData.artwork.tabOff,
							r: UiRect = [ sx + 14 + i * 62, sy + 12, 60, 24 ],
							label = guideData
								.strings[[ "UIIT_STT_GAMEGUIDE", "UIIT_STT_EVENTGUIDE", "UIIT_STT_QUESTGUIDE1" ][i]!]!;
						paths.push(
							guideData.artwork.tabOn,
							guideData.artwork.tabOff
						);
						if ( resources.has( skin ) ) rect( r, white, skin );
						quads.push( ...text.quads( label, r, full, white, { hAlign: 1, vAlign: 1 } ) );
						controls.push( {
							id: "guide-tab:" + tab,
							label,
							kind: "button",
							rect: r,
							selected: guideTab === tab
						} );
					}

					const rows = guideTab === "events" ?
						guideData.articles.filter( a => !!((game?.guide?.seenMask ?? 0) & (1 << (a.id - 1))) ).map(
							a => ({ id: a.id, title: a.title, depth: 1 })
						) :
						guideTab === "general" ?
						guideData.general.filter( a => a.depth === 0 || guideGroups.has( a.parent ) ) :
						questRows.filter( a => a.depth === 0 || guideGroups.has( a.parent ) );
					guideIndexScroll = Math.min( guideIndexScroll, Math.max( 0, rows.length - 11 ) );
					rows.slice( Math.round( guideIndexScroll ), Math.round( guideIndexScroll ) + 11 ).forEach(
						( row, i ) => {
							const y = sy + 45 + i * 30,
								r: UiRect = [ sx + 17, y, 164, 28 ],
								id = (row.depth === 0 ? "guide-group:" : "guide-article:") + row.id;
							const titleX = r[0] + (row.depth === 0 ? 28 : guideTab === "events" ? 18 : 43);
							const skin = row.depth === 0 ?
								guideData.artwork.group :
								guideTab === "events" ?
								guideData.artwork.event :
								guideData.artwork.article;
							paths.push( skin );
							if ( resources.has( skin ) ) {
								rect( r, white, skin );
							}
							let title = row.title;
							if ( guideTab === "quests" && row.depth === 1 && text.run( title ).width > 90 ) {
								while ( title && text.run( title ).width > 90 ) title = title.slice( 0, -1 );
								title += "...";
							}
							const rowColor = "color" in row ? row.color as UiQuad["color"] : white;
							quads.push(
								...text.quads(
									title,
									[ titleX, y + 8, row.depth === 0 ? 131 : guideTab === "events" ? 132 : 107, 12 ],
									r,
									rowColor,
									{ hAlign: row.depth === 0 ? 1 : 0 }
								)
							);
							if ( row.depth === 1 && guideEvent === row.id ) {
								const lamp = guideData.artwork.selected;
								paths.push( lamp );
								if ( resources.has( lamp ) ) {
									rect( [ r[0] + (guideTab === "events" ? 8 : 33), y + 7, 8, 16 ], white, lamp );
								}
							}
							if ( row.depth === 0 ) {
								const path = guideGroups.has( row.id ) ?
									guideData.artwork.groupOpen :
									guideData.artwork.groupClosed;
								paths.push( path );
								if ( resources.has( path ) ) {
									rect( [ r[0] + 4, y + 4, 24, 24 ], white, path );
								}
							}
							controls.push( {
								id,
								label: row.title,
								kind: "button",
								rect: r,
								selected: guideEvent === row.id
							} );
						}
					);
					guideIndexMax = Math.max( 0, rows.length - 11 );
					const indexBar = chatScrollbar(
						"guide-index",
						[ sx + 179, sy + 61, 16, 282 ],
						rows.length,
						11,
						guideIndexMax - guideIndexScroll,
						resources.size,
						full,
						hover,
						pressed
					);
					quads.push( ...indexBar.quads );
					paths.push( ...indexBar.paths );
					controls.push(
						...indexBar.controls.map( c => ({ ...c, label: c.draggable ? "Scroll guide index" : c.label }) )
					);
				}
				const article = guideTab === "events" ?
					guideData.articles.find( a => a.id === guideEvent ) :
					guideTab === "general" ?
					guideData.general.find( a => a.id === guideEvent ) :
					questRows.find( a => a.id === guideEvent );
				if ( guideEvent === 0 ) {
					authoredImage( nodes.GDR_GUIDE_STARTPGDECO!, gx, gy );
					for ( const n of [ 1, 2 ] ) {
						const node = nodes["GDR_GUIDE_STARTPGDECO_TEXT_" + n]!, r = authoredRect( node, gx, gy );
						quads.push(
							...text.guide(
								guideTokens( guideData.strings["UIIT_STT_GAMEGUIDE_START_" + n] ?? "" ),
								r,
								r,
								node.color,
								resources.size
							).quads
						);
					}
				}
				const tokens = article ?
					(guideTab === "events" && game?.guide?.country === 1 && "european" in article ?
						(article.european ?? []) :
						article.tokens) :
					[];
				if ( guideTab === "quests" && article && "depth" in article && article.depth === 1 ) {
					const node = nodes.GDR_GUIDE_QUEST_NAME_STA!;
					quads.push(
						...text.quads( article.title, authoredRect( node, gx, gy ), full, node.color, { fontStyle: 2 } )
					);
				}
				const contentNode = nodes.GDR_GUIDE_DATA_PML!,
					clip: UiRect = guideTab === "quests" ?
						[ gx + 53, gy + 116, 290, 273 ] :
						authoredRect( contentNode, gx, gy ),
					measure = text.guide( tokens, clip, clip, contentNode.color, resources.size );
				guideScroll = Math.min( guideScroll, Math.max( 0, measure.height - clip[3] ) );
				const content = text.guide(
					tokens,
					[ clip[0], clip[1] - guideScroll, clip[2], clip[3] ],
					clip,
					contentNode.color,
					resources.size
				);
				paths.push( ...content.paths );
				quads.push( ...content.quads );
				const scroll = authoredRect( nodes.GDR_GUIDE_DATA_SCL!, gx, gy ),
					travel = contentNode.rect[3] - 48,
					overflow = Math.max( 0, measure.height - clip[3] );
				for (
					const [id, skin, sy, disabled] of [ [ "guide-up", "up", scroll[1], guideScroll === 0 ], [
						"guide-down",
						"down",
						scroll[1] + travel + 16,
						guideScroll >= overflow
					] ] as const
				) {
					const path = ROOT + "interface/guide/gd_scroll_" + skin + ".png", size = resources.size( path );
					paths.push( path );
					const r: UiRect = [ scroll[0], sy, size?.[0] ?? 16, size?.[1] ?? 16 ];
					if ( size ) rect( r, white, path );
					controls.push( {
						id,
						label: skin === "up" ? "Scroll guide up" : "Scroll guide down",
						kind: "button",
						rect: r,
						disabled
					} );
					blocks.push( r );
				}
				const thumb = ROOT + "interface/guide/gd_scroll_button.png", thumbSize = resources.size( thumb );
				paths.push( thumb );
				guideScrollMax = overflow;
				guideThumbTravel = thumbSize ? Math.max( 0, travel - thumbSize[1] ) : 0;
				if ( overflow && thumbSize ) {
					const r: UiRect = [
						scroll[0],
						scroll[1] + 16 + Math.floor( guideThumbTravel * guideScroll / overflow ),
						thumbSize[0],
						thumbSize[1]
					];
					rect( r, white, thumb );
					controls.push( {
						id: "guide-thumb",
						label: "Scroll guide",
						kind: "button",
						draggable: true,
						rect: r
					} );
				}
				endWindow( admission );
			}
			const noticeData = hud.data(), noticeLayout = noticeData?.windows.ifmallnotifywnd;
			if ( worldVisible && mallNotice === "open" && noticeData && noticeLayout ) {
				// CIFMallNotifyWnd (6CCC20): the mall_communicate frame, its two titles,
				// the notice text with TextMargin between lines, and buttons 4 and 5.
				// One window admission: text and buttons wait for the frame, so a
				// cold open after a teleport never shows them over the world.
				const admission = beginWindow();
				const [nx, ny] = mallNotifyOrigin( w, h ),
					notice = noticeData.mallNotify,
					nodes = Object.values( noticeLayout ),
					node = ( id: number ) => nodes.find( n => n.id === id );
				blocks.push( [ nx, ny, MALL_NOTIFY_WIDTH, MALL_NOTIFY_HEIGHT ] );
				image( [ nx, ny, MALL_NOTIFY_WIDTH, MALL_NOTIFY_HEIGHT ], MALL_NOTICE_FRAME );
				nativePage( noticeLayout, nx, ny, [ 3, 4, 5 ] );
				const contents = node( 3 );
				if ( contents ) {
					// CIFMallNotifyWnd_LoadText (6CC490) writes the text into static 3,
					// which keeps its authored FontColor (61,34,0) unless the file names
					// a TextColor; v1.150's mall_notify.txt names none.
					const [cx, cy, cw] = authoredRect( contents, nx, ny ),
						pitch = contents.rect[3] + notice.lineSpacing;
					for ( const [i, line] of notice.text.split( "\n" ).entries() ) {
						quads.push(
							...text.quads( line, [ cx, cy + i * pitch, cw, contents.rect[3] ], full, contents.color )
						);
					}
				}
				const enter = node( 4 ), exit = node( 5 );
				if ( enter ) {
					authoredLabeledButton(
						enter,
						nx,
						ny,
						"mall-notice-enter",
						hudCopy( "UIIT_STT_SILKMALL_DIRECT_ENTER" )
					);
				}
				if ( exit ) {
					authoredLabeledButton(
						exit,
						nx,
						ny,
						"mall-notice-close",
						hudCopy( "UIIT_CTL_LETTER_WINDOWSCLOSE" )
					);
				}
				endWindow( admission, "mall-notice" );
			}
			if (
				game?.social?.invitation && game.social.invitation.type !== 10 && worldVisible &&
				(game.social.invitation.type !== 5 && game.social.invitation.type !== 6 ||
					next.entities.some( e => e.gid === game.social!.invitation!.gid && e.guildName ))
			) {
				controls = [];
				blocks = [ full ];
				if ( focus !== "invite-accept" && focus !== "invite-refuse" ) {
					focus = null;
					composing = false;
				}
				paths.push( ...partyProposalAssets() );
				const invite = game.social.invitation,
					union = invite.type === 6,
					guild = invite.type === 5 || union,
					exchange = invite.type === 1,
					inviter = next.entities.find( e => e.gid === invite.gid ),
					layout = proposalLayout( invite.type, w, h, resources.size( PARTY_OPTION ), invitePosition ),
					// 52F460 case 0x1C titles the union box and asks with the
					// inviter's guild name on its single line.
					heading = union ?
						"UIIT_STT_GUILD_RESPECT_ALLY_JOIN" :
						guild ?
						"UIIT_STT_AGREEMENT_BOX" :
						"UIIT_STT_CONFIRM_BOX";
				controls.push( {
					id: "invite-drag",
					label: hudCopy( heading ),
					kind: "region",
					draggable: true,
					rect: layout.drag
				} );
				/*
				================
				copy
				================
				*/
				const copy = ( key: string, fallback: string ) => hudCopy( key ) || title.catalog( key ) || fallback;
				quads.push( ...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ) );
				quads.push(
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				/*
				================
				line
				================
				*/
				const line = ( value: string, r: UiRect ) =>
					quads.push( ...text.quads( value, r, full, white, { hAlign: 1, vAlign: 0 } ) );
				line( copy( heading, "Confirmation window" ), layout.title );
				line(
					union ?
						copy( "UIIT_MSG_QUESTION_GUILD_RESPECT_ALLY_JOIN", "[%s]" ).replace(
							"%s",
							() => inviter?.guildName ?? ""
						) :
						copy( guild ? "UIIT_MSG_GUILD_JOIN_REQUEST" : "UIIT_STT_PARTY_SOMEUSER", "[%s]has" ).replace(
							"%s",
							() => inviter?.name ?? ""
						),
					layout.name
				);
				if ( !union ) {
					line(
						guild ?
							copy( "UIIT_MSG_GUILD_QUESTION_JOIN", "" ).replace( "%s", () => inviter?.guildName ?? "" ) :
							copy( exchange ? "UIIT_MSG_DEAL_ASK" : "UIIT_STT_PARTY_PROPOSAL_ASK", "" ),
						layout.question
					);
				}
				if ( !exchange ) {
					layout.options.forEach( ( option, i ) => {
						rect( option.image, white, PARTY_OPTION );
						line(
							copy(
								i === 0 ?
									((invite.options ?? 0) & 1 ?
										"UIIT_STT_PARTY_EXP_SHARE" :
										"UIIT_STT_PARTY_EXP_SELF") :
									((invite.options ?? 0) & 2 ?
										"UIIT_STT_PARTY_ITEM_SHARE" :
										"UIIT_STT_PARTY_ITEM_SELF"),
								""
							),
							option.text
						);
					} );
				}
				button(
					"invite-accept",
					copy( guild ? "UIIT_CTL_YES" : "UIIT_STT_ACCEPT", "Accept" ),
					layout.accept[0],
					layout.accept[1],
					76,
					false,
					false,
					6
				);
				button(
					"invite-refuse",
					copy( guild ? "UIIT_CTL_NO" : "UIIT_STT_REFUSE", "Refuse" ),
					layout.refuse[0],
					layout.refuse[1],
					76,
					false,
					false,
					6
				);
			}
			const partyJoining = game?.partyMatching?.joining,
				joinElapsed = partyJoining ? Math.max( 0, (next.simulationTimeMs ?? now) - partyJoining.since ) : 10000;
			if ( worldVisible && partyJoining && joinElapsed < 10000 && hud.data() && !game?.partyMatching?.request ) {
				const mark = beginWindow(),
					px = Math.floor( (w - 365) / 2 ),
					py = Math.floor( (h - 149) / 2 ),
					prefix = ROOT + "interface/messagebox/msgbox2_window_",
					layout = hud.data()!.windows.ifpartyjoinprogress!;
				controls = [];
				blocks.push( full );
				paths.push( ...PARTS.map( p => prefix + p + ".png" ) );
				quads.push(
					...frameRing(
						[ px, py, 365, 149 ],
						prefix,
						PARTS.map( p => resources.size( prefix + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads(
						hudCopy( "UIIT_PAG_PARTYMATCH_JOINPROGRESS" ),
						[ px + 10, py + 11, 344, 12 ],
						full,
						white,
						{ hAlign: 1 }
					)
				);
				for ( const node of authoredPaintOrder( layout ) ) {
					if ( node.type === "CIFPML" ) {
						const r = authoredRect( node, px, py ),
							copy = hudCopy( "UIIT_STT_PARTYMATCH_JOIN_PROGRESS" ).replace(
								"%s",
								next.session?.character ?? ""
							).replace( "%s", partyJoining.name ),
							out = text.guide( guideTokens( copy ), r, r, white, resources.size );
						quads.push( ...out.quads );
						paths.push( ...out.paths );
					} else if ( node.type === "CIFGauge" ) {
						const r = authoredRect( node, px, py ),
							fraction = Math.fround( Math.floor( joinElapsed / 200 ) * 200 / 10000 );
						paths.push( node.texture );
						if ( resources.has( node.texture ) ) {
							rect( [ r[0], r[1], r[2] * fraction, r[3] ], white, node.texture, [ 0, 0, fraction, 1 ] );
						}
					} else authoredChrome( node, px, py );
				}
				endWindow( [ mark[0], 0, mark[2], mark[3] ], "modal:" + panel );
				blocks.push( full );
			}
			const partyHudData = hud.data();
			if ( worldVisible && partyHudData && (partyDialog || game?.partyMatching?.request) ) {
				const mark = beginWindow(),
					request = game?.partyMatching?.request,
					remove = partyDialog === "delete",
					automatic = partyDialog === "auto",
					width = request ? 360 : 314,
					height = request ? 315 : automatic ? 337 : 373,
					px = Math.floor( (w - width) / 2 ),
					py = Math.floor( (h - height) / 2 );
				controls = [];
				blocks.push( full );
				if ( remove ) {
					// 635300 asks through a 6888C0 simple message box, type 1 (Yes/No).
					simpleMessageBox( {
						title: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
						lines: [ hudCopy( "UIIT_STT_PARTYMATCH_DELETE_CONFIRM" ) ],
						yes: "party-form-confirm",
						no: "party-form-cancel"
					} );
				} else {
					const prefix = ROOT + "interface/messagebox/msgbox2_window_";
					paths.push( ...PARTS.map( p => prefix + p + ".png" ) );
					quads.push(
						...frameRing(
							[ px, py, width, height ],
							prefix,
							PARTS.map( p => resources.size( prefix + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads(
							hudCopy(
								request ?
									"UIIT_PAG_PARTYMATCH_JOINREQUEST" :
									automatic ?
									"UIIT_PAG_PARTYMATCH_AUTOMATCH" :
									"UIIT_PAG_PARTYMATCH_RECORD"
							),
							[ px + 10, py + 11, width - 20, 12 ],
							full,
							white,
							{ hAlign: 1 }
						)
					);
					const layout = partyHudData.windows[
						request ?
							"ifpartymatchreqjoin" :
							automatic ?
							"ifpartymatchauto" :
							"ifpartymatchregister"
					]!;
					for ( const node of authoredPaintOrder( layout ) ) {
						if ( node.type === "CIFButton" ) {
							const id = request ?
									"party-answer:" + (node.id === 45 ? 1 : 0) :
									node.id === (automatic ? 50 : 60) ?
									"party-form-confirm" :
									"party-form-cancel",
								caption = hudCopy(
									request ?
										node.text :
										node.id === (automatic ? 50 : 60) ?
										"UIIT_CTL_CONFIRM" :
										"UIIT_CTL_CANCEL"
								);
							authoredLabeledButton( node, px, py, id, caption );
						} else if ( node.type === "CIFEdit" ) {
							const key = node.id === 49 ? "title" : node.id === 42 ? "min" : "max";
							partyEdit( node, px, py, "party-form-" + key, partyForm[key], key === "title" ? 50 : 3 );
						} else if ( automatic && node.type === "CIFRadioButton" ) {
							const key = node.id === 35 ?
									"purpose" :
									node.id === 37 ?
									"race" :
									node.id === 39 ?
									"exp" :
									"item",
								items = key === "purpose" ?
									partyPurposes.slice( 0, 4 ) :
									key === "race" ?
									[
										"UIIT_CTL_PARTYMATCH_AUTOMATCH_RACE_CH",
										"UIIT_CTL_PARTYMATCH_AUTOMATCH_RACE_EU",
										"UIIT_CTL_PARTYMATCH_AUTOMATCH_RACE_ALL"
									] :
									[
										"UIIT_STT_PARTY_" + (key === "exp" ? "EXP" : "ITEM") + "_SHARE",
										"UIIT_STT_PARTY_" + (key === "exp" ? "EXP" : "ITEM") + "_SELF"
									];
							items.forEach( ( copy, i ) => {
								const vertical = key === "exp" || key === "item",
									r: UiRect = [
										px + node.rect[0] + (vertical ? 0 : i * (key === "race" ? 88 : 66)),
										py + node.rect[1] + (vertical ? i * 22 : 0),
										16,
										16
									],
									path = ROOT + "interface/ifcommon/com_radiobutton_" +
										(partyAuto[key] === i ? "on" : "off") + ".png";
								image( r, path );
								quads.push(
									...text.quads(
										hudCopy( copy ),
										[ r[0] + 22, r[1] + 2, vertical ? 95 : 60, 12 ],
										full,
										white
									)
								);
								controls.push( {
									id: "party-form-auto:" + key + ":" + i,
									label: hudCopy( copy ),
									rect: [ r[0], r[1], vertical ? 120 : 66, 16 ],
									kind: "button"
								} );
							} );
						} else if ( node.type === "CIFRadioButton" ) {
							const job = partyActiveJob( game?.inventory ?? [] );
							for ( let i = 0; i < 4; i++ ) {
								const disabled = !partyPurposeAllowed( job, i ),
									r: UiRect = [ px + 30 + i * 66, py + 77, 16, 16 ],
									path = ROOT + "interface/ifcommon/com_radiobutton_" +
										(partyForm.purpose === i ? "on" : "off") + ".png";
								paths.push( path );
								if ( resources.has( path ) ) {
									rect( r, disabled ? [ .5, .5, .5, 1 ] : white, path );
								}
								quads.push(
									...text.quads(
										hudCopy( partyPurposes[i]! ),
										[ r[0] + 22, r[1] + 2, 44, 12 ],
										full,
										white
									)
								);
								controls.push( {
									id: "party-form-purpose:" + i,
									label: hudCopy( partyPurposes[i]! ),
									rect: [ r[0], r[1], 66, 16 ],
									kind: "button",
									disabled
								} );
							}
						} else if ( request && (node.id === 34 || node.id === 35) ) {
							const mastery = partyHudData.skillUi.masteries.find( m =>
								m.id === (node.id === 34 ? request.primary : request.secondary)
							);
							if ( mastery ) {
								const path = iconPath( mastery.icon ), r = authoredRect( node, px, py );
								if ( path ) image( r, path );
								controls.push( {
									id: "party-request-mastery:" + node.id,
									label: hudCopy( mastery.name ),
									helpText: hudCopy( mastery.name ),
									kind: "region",
									rect: r
								} );
							}
						} else if ( request && [ 21, 30, 31, 32, 33 ].includes( node.id ) ) {
							const member = request.member,
								entity = next.entities.find( e => e.gid === member.id ),
								value = node.id === 21 ?
									hudCopy( "UIIT_STT_PARTYMATCH_JOIN_REQUEST" ).replace( "%s", member.name ) :
									node.id === 30 ?
									String( member.level ) :
									node.id === 31 ?
									hudCopy(
										"UIIT_CTL_PARTYMATCH_AUTOMATCH_RACE_" +
											(partyHudData.countries[member.model] === 0 ?
												"CH" :
												partyHudData.countries[member.model] === 1 ?
												"EU" :
												"")
									) :
									node.id === 32 ?
									(partyHudData
										.zones[(partyHudData.regionCodes[String( member.region )] ?? "") + "_01"] ??
										"") :
									request.flags === 4 ?
									member.guild ?? "" :
									"";
							if ( node.type === "CIFPML" ) {
								const r = authoredRect( node, px, py ),
									out = text.guide( guideTokens( value ), r, r, white, resources.size );
								quads.push( ...out.quads );
								paths.push( ...out.paths );
							} else authoredText( node, px, py, value );
						} else if ( !request && (node.id === 45 || node.id === 47) ) {
							const opts = effectivePartyOptions( game?.social, partyOptions );
							authoredText(
								node,
								px,
								py,
								hudCopy(
									node.id === 45 ?
										opts & 1 ? "UIIT_STT_PARTY_EXP_SHARE" : "UIIT_STT_PARTY_EXP_SELF" :
										opts & 2 ?
										"UIIT_STT_PARTY_ITEM_SHARE" :
										"UIIT_STT_PARTY_ITEM_SELF"
								)
							);
						} else authoredChrome( node, px, py );
					}
				}
				endWindow( [ mark[0], 0, mark[2], mark[3] ], "modal:" + panel );
				blocks.push( full );
			}
			if ( worldVisible && shopDialog && panel === "Shop" && game?.shop && hud.data() ) {
				const admission = beginWindow(),
					confirm = shopChoice?.kind !== "buy",
					layout = messageBox( w, h, 327, confirm ? 175 : 177, shopPosition ),
					[mx, my] = layout.frame,
					shop = game.shop,
					quote = merchantQuote(
						shopChoice,
						shop,
						merchantRows( game ),
						shopQuantity,
						game.progression?.gold
					),
					item = quote?.item;
				const page = confirm ?
						merchantDialogPage( hud.data()!.windows.ifmessagebox!, true ) :
						hud.data()!.windows.ifitemmallconfirmbuy!,
					locked = !!game.inventoryPending,
					quantityMode = quote?.quantityMode ?? "readonly";
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile(
						confirm ? layout.background : [ mx + 16, my + 40, 295, 128 ],
						MESSAGE_TILE,
						resources.size( MESSAGE_TILE ),
						full
					),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				controls.push( {
					id: "shop-dialog-drag",
					label: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
					rect: layout.drag,
					kind: "region",
					draggable: true
				} );
				quads.push(
					...text.quads(
						hudCopy( "UIIT_STT_CONFIRM_BOX" ),
						confirm ? layout.title : authoredRect( page.GDR_ITEMMALL_CONFIRM_BUY_TITLE!, mx, my ),
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				// Active NPC package purchases: 59A100 -> 6C0540 -> 6BF320.
				// Gold adds a 22px currency row; sale/buyback retain MsgBoxStoreConfirm.
				nativePage(
					page,
					mx,
					my,
					confirm ? [ 78, 79, 80, 81, 82, 83, 84, 85, 86, 87 ] : [ 4, 5, 11, 21, 40, 42, 50, 51, 61, 62 ]
				);
				const name = page[confirm ? "GDR_MBS_CONFIRM_EDIT_ITEM_NAME" : "GDR_ITEMMALL_CONFIRM_EDIT_ITEM_NAME"]!,
					amount = page[confirm ? "GDR_MBS_CONFIRM_EDIT_COUNT" : "GDR_ITEMMALL_CONFIRM_EDIT_COUNT"]!,
					iconNode = page[confirm ? "GDR_MBS_CONFIRM_BUY_ITEM_ICON" : "GDR_ITEMMALL_CONFIRM_BUY_ITEM_ICON"]!;
				authoredText( name, mx, my, item?.name ?? "" );
				if ( confirm ) {
					authoredText(
						page.GDR_MBS_CONFIRM_EDIT_PRICE!,
						mx,
						my,
						quote?.total == null ? "" : moneyPresentation( quote.total.toString() ).text
					);
					for (
						const key of [
							"GDR_MBS_CONFIRM_STA_PRICE",
							"GDR_MBS_CONFIRM_STA_GOLD",
							"GDR_MBS_CONFIRM_STA_COUNT"
						]
					) {
						const node = page[key]!;
						authoredText( node, mx, my, hudCopy( node.text ) );
					}
				} else {
					const row = hud.data()!.windows.ifitemmallconfirmslot!;
					nativePage( row, mx, my + 100, [ 3, 4, 5, 10 ] );
					// 6BD2FD supplies L"%d" to the currency-row editor. Inventory's grouped,
					// threshold-colored money control is a different native presentation.
					authoredText( row.GDR_ITEMMALL_CONFIRM_EDIT!, mx, my + 100, quote?.total?.toString() ?? "" );
					authoredText(
						row.GDR_ITEMMALL_CONFIRM_STA_1!,
						mx,
						my + 100,
						hudCopy( "UIIT_CTL_WARENETWORK_RESULT_PRICE" )
					);
					authoredText( row.GDR_ITEMMALL_CONFIRM_STA_2!, mx, my + 100, hudCopy( "UIIT_STT_GOLD" ) );
					authoredText( page.GDR_ITEMMALL_CONFIRM_STA_COUNT!, mx, my, hudCopy( "UIIT_STT_AMOUNT" ) );
				}
				const icon = iconPath( item?.icon );
				if ( icon ) authoredImage( { ...iconNode, texture: icon }, mx, my );
				if ( quantityMode !== "hidden" ) {
					const r = authoredClientRect( amount, mx, my ),
						quantity = shopChoice?.kind === "buyback" ? String( quote?.quantity ?? "" ) : shopQuantity;
					const editing = quantityMode === "editable" && !locked && focus === "shop-quantity";
					const textWidth = text.run( quantity ).width,
						left = r[0] +
							(amount.hAlign === 2 ?
								r[2] - textWidth :
								amount.hAlign === 1 ?
								Math.trunc( (r[2] - textWidth) / 2 ) :
								0),
						start = Math.min( ...selection, quantity.length ),
						end = Math.min( Math.max( ...selection ), quantity.length ),
						before = text.run( quantity.slice( 0, start ) ).width,
						through = text.run( quantity.slice( 0, end ) ).width;
					if ( editing && end > start ) {
						rect(
							[ left + before, r[1], through - before, r[3] ],
							[ 59 / 255, 69 / 255, 122 / 255, 1 ],
							"",
							[ 0, 0, 1, 1 ],
							r
						);
					}
					quads.push(
						...text.quads( quantity, r, r, amount.color, {
							fontIndex: amount.fontIndex,
							hAlign: amount.hAlign,
							vAlign: amount.vAlign,
							overflow: "clip"
						} )
					);
					if ( editing && caretVisible ) {
						rect( [ Math.min( r[0] + r[2] - 2, left + through ), r[1], 2, r[3] ], amount.color, "", [
							0,
							0,
							1,
							1
						], r );
					}
					if ( quantityMode === "editable" ) {
						controls.push( {
							id: "shop-quantity",
							label: hudCopy( "UIIT_STT_AMOUNT" ),
							rect: authoredRect( amount, mx, my ),
							kind: "text",
							value: shopQuantity,
							maxLength: 5,
							disabled: locked,
							textAlign: amount.hAlign === 2 ? "right" : amount.hAlign === 1 ? "center" : "left",
							textInsets: amount.client
						} );
					}
				}
				if ( quantityMode === "editable" ) {
					for (
						const [suffix, id, delta] of [ [ "UP", "shop-quantity-up", 1 ], [
							"DOWN",
							"shop-quantity-down",
							-1
						] ] as const
					) {
						const node =
							page[(confirm ? "GDR_MBS_CONFIRM_SPINBTN_" : "GDR_ITEMMALL_CONFIRM_SPINBTN_") + suffix]!;
						authoredButton(
							node,
							mx,
							my,
							id,
							"",
							locked || !quote || Number( shopQuantity ) + delta < 1 ||
								Number( shopQuantity ) + delta > quote.maximum
						);
					}
				}
				const ok = confirm ?
						page.GDR_MBS_CONFIRM_BTN_SELL! :
						{ ...page.GDR_ITEMMALL_CONFIRM_BTN_BUY!, rect: [ 82, 137, 0, 0 ] as UiRect },
					cancel = confirm ?
						page.GDR_MBS_CONFIRM_BTN_CANCEL! :
						{ ...page.GDR_ITEMMALL_CONFIRM_BTN_CANCEL!, rect: [ 170, 137, 0, 0 ] as UiRect },
					caption = hudCopy(
						shopChoice?.kind === "sell" ?
							"UIIT_STT_SELL" :
							shopChoice?.kind === "buyback" ?
							"UIIT_STT_RE_BUY_OBJECT" :
							"UIIT_STT_BUY"
					);
				authoredLabeledButton(
					ok,
					mx,
					my,
					"shop-trade",
					caption,
					locked || !quote?.valid || game.target !== shop.npc
				);
				authoredLabeledButton( cancel, mx, my, "shop-cancel", hudCopy( "UIIT_CTL_CANCEL" ), locked );
				endWindow( [ admission[0], 0, 0, admission[3] ], "modal:" + panel );
			}
			if (
				worldVisible && guildDialog && hud.data() && game &&
				(guildDialog === "academy-notice" ? game.academy?.member : game.social?.guild)
			) {
				const admission = beginWindow(),
					donate = guildDialog === "donate",
					academyNotice = guildDialog === "academy-notice",
					notice = guildDialog === "notice" || academyNotice,
					layoutName = notice ?
						"ifguildnotifywrite" :
						donate ?
						"ifguildpointup" :
						guildDialog === "role" ?
						"ifguildpositiongrant" :
						null;
				const width = notice ? 440 : donate ? 360 : guildDialog === "role" ? 286 : 308,
					height = notice ? 310 : donate ? 240 : guildDialog === "role" ? 310 : 148,
					mx = Math.max( 0, (w - width) / 2 ),
					my = Math.max( 0, (h - height) / 2 );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile(
						[ mx + 16, my + 40, width - 32, height - 56 ],
						MESSAGE_TILE,
						resources.size( MESSAGE_TILE ),
						full
					),
					...frameRing(
						[ mx, my, width, height ],
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				if ( layoutName ) {
					const page = hud.data()!.windows[layoutName]!;
					nativePage(
						academyNotice ?
							Object.fromEntries(
								Object.entries( page ).map( (
									[key, node]
								) => [ key, node.id === 53 ? { ...node, text: "UIIT_CTL_TC_COMMON" } : node ] )
							) :
							page,
						mx,
						my
					);
					for ( const node of Object.values( page ) ) {
						if ( node.type === "CIFEdit" ) {
							partyEdit(
								node,
								mx,
								my,
								notice ? (node.id === 10 ? "social-subject" : "social-contents") : "social-amount",
								notice ? (node.id === 10 ? socialSubject : socialContents) : socialAmount,
								notice ? (node.id === 10 ? 127 : 1023) : 10
							);
						}
						if ( node.type === "CIFButton" ) {
							const cancel = node.name.includes( "CANCEL" ),
								id = cancel ?
									"guild-dialog-close" :
									notice ?
									(academyNotice ? "academy-notice-submit" : "guild-notice") :
									donate ?
									"guild-donate" :
									"guild-role";
							authoredLabeledButton(
								node,
								mx,
								my,
								id,
								hudCopy( node.text ),
								!cancel && (donate ?
									!/^\d+$/.test( socialAmount ) || Number( socialAmount ) < 1 ||
									Number( socialAmount ) > (game.progression?.skillPoints ?? 0) :
									notice ?
									false :
									!socialMember)
							);
						}
						if ( donate && node.id === 611 ) authoredText( node, mx, my, game.social!.guild!.name );
						if ( donate && node.id === 608 ) {
							authoredText( node, mx, my, String( game.progression?.skillPoints ?? 0 ) );
						}
						if ( node.type === "CIFCheckBox" ) {
							const index = Object.values( page ).filter( n => n.type === "CIFCheckBox" ).sort( (
									a,
									b
								) => a.id - b.id
								).indexOf( node ),
								chosen = Number( socialAmount ) === fortressGrantRole( index );
							authoredImage( node, mx, my, node.texture.replace( "_off", chosen ? "_on" : "_off" ) );
							controls.push( {
								id: "guild-role-choice:" + index,
								helpText: hudCopy(
									[
										"UIIT_MSG_TP_FORT_GUILD_SUBCOMMANDER",
										"UIIT_MSG_TP_FORT_GUILD_BATTLEMANAGER",
										"UIIT_MSG_TP_FORT_GUILD_PRODUCTMANAGER",
										"UIIT_MSG_TP_FORT_GUILD_TRAINERMANAGER",
										"UIIT_MSG_TP_FORT_GUILD_ENGINEER"
									][index] ?? ""
								),
								label: node.name,
								rect: authoredRect( node, mx, my ),
								kind: "button",
								selected: chosen
							} );
						}
					}
				} else {
					const r: UiRect = [ mx + 28, my + 65, 252, 20 ];
					controls.push( {
						id: "social-name",
						label: hudCopy( "UIIT_CTL_GUILD_NAMEGRANT" ),
						rect: r,
						kind: "text",
						value: socialName,
						maxLength: 12
					} );
					quads.push( ...text.quads( socialName, r, r, white, { overflow: "clip" } ) );
					button(
						"guild-title",
						hudCopy( "UIIT_CTL_CONFIRM" ),
						mx + 75,
						my + 108,
						76,
						!socialName || !socialMember
					);
					button( "guild-dialog-close", hudCopy( "UIIT_CTL_CANCEL" ), mx + 155, my + 108, 76 );
				}
				endWindow( [ admission[0], 0, 0, admission[3] ], "modal:" + panel );
			}
			guildWarHud.reconcile( game?.social );
			const warDialog = guildWarHud.state(), warInvite = game?.social?.invitation?.war;
			if ( worldVisible && hud.data() && (warDialog.mode !== "closed" || warInvite) ) {
				const input = !warInvite && warDialog.mode === "input",
					surrender = !warInvite && warDialog.mode === "surrender";
				const terms = warInvite ?? warDialog.terms,
					width = surrender ? 308 : 350,
					height = surrender ? 148 : input ? 257 : 222;
				const x = Math.floor( (w - width) / 2 ), y = Math.floor( (h - height) / 2 );
				const layout = hud.data()!
					.windows[warInvite ? "ifguildwaragree" : input ? "ifguildwarrequest" : "ifguildwarconfirm"]!;
				const at = ( id: number ) => Object.values( layout ).find( n => n.id === id )!;
				const put = ( id: number, value: string ) => {
					if ( at( id ) ) authoredText( at( id ), x, y, value );
				};
				const unlimited = hudCopy( "UIIT_CTL_GUILDWAR_UNLIMITED" );
				const choice = ( id: number, value: number ) =>
					id === 23 ?
						value === 0 ? unlimited : String( warScoreLimits()[value] ) :
						value === [ 31, 24, 6 ][id - 24] ?
						unlimited :
						String( id === 26 ? value * 10 : value ) + " " +
						hudCopy( id === 24 ? "PARAM_DAY" : id === 25 ? "UIIT_STT_HOUR" : "UIIT_STT_MINUTE" );
				controls = [];
				blocks = [ full ];
				if ( surrender ) {
					const box = guildProposalLayout( w, h ),
						enemy = game?.social?.wars?.find( row => row.id === warDialog.selected )?.name ?? "";
					paths.push( ...partyProposalAssets() );
					quads.push(
						...normalTile( box.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
						...frameRing(
							box.frame,
							MESSAGE_FRAME,
							PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), box.title, full, white, { hAlign: 1 } ),
						...text.quads(
							hudCopy( "UIIT_MSG_GUILDWAR_SANCTION_ENDWAR_01" ).replace( "%s", () => enemy ),
							box.name,
							full,
							white,
							{ hAlign: 1 }
						),
						...text.quads( hudCopy( "UIIT_MSG_GUILDWAR_SANCTION_ENDWAR_02" ), box.question, full, white, {
							hAlign: 1
						} )
					);
					button(
						"war-confirm",
						hudCopy( "UIIT_CTL_YES" ),
						...box.accept.slice( 0, 3 ) as [number, number, number]
					);
					button(
						"war-cancel",
						hudCopy( "UIIT_CTL_NO" ),
						...box.refuse.slice( 0, 3 ) as [number, number, number]
					);
				} else {
					const root = hud.data()!.root.GDR_REQUEST_GUILDWAR!;
					nativeFrame(
						{
							...root,
							texture: ROOT + "interface/messagebox/msgbox2_window_",
							rect: [ 0, 0, width, height ]
						},
						x,
						y,
						hudCopy(
							warInvite ? "UIIT_STT_AGREEMENT_BOX" : input ? "UIIT_STT_INPUT_BOX" : "UIIT_STT_CONFIRM_BOX"
						),
						warInvite ? "invite-close" : "war-close"
					);
					nativePage( layout, x, y, [ 5, 6, 10, 11, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 40, 55, 56 ] );
					if ( input ) {
						for ( const id of [ 10, 29, 30, 31, 32, 56 ] ) {
							if ( at( id ) ) put( id, hudCopy( at( id ).text ) );
						}
						put( 40, String( terms.stake ) );
						partyEdit( at( 55 ), x, y, "war-name", warDialog.draft.name, 12 );
						authoredButton( at( 28 ), x, y, "war-money-open", hudCopy( "UIIT_STT_AMOUNT_MONEY" ) );
						const values = [
							warDialog.draft.scoreIndex,
							warDialog.draft.days,
							warDialog.draft.hours,
							warDialog.draft.minutes
						];
						for ( const [i, value] of values.entries() ) {
							comboBox(
								authoredRect( at( 23 + i ), x, y ),
								"war-combo:" + (23 + i),
								choice( 23 + i, value ),
								choice( 23 + i, value )
							);
						}
					} else {
						for ( const id of [ 11, 27, 28, 29, 30 ] ) put( id, hudCopy( at( id ).text ) );
						put( 10, hudCopy( at( 10 ).text ).replace( "%s", () => terms.name ) );
						for ( const id of [ 23, 24, 25, 26 ] ) authoredChrome( at( id ), x, y );
						put( 31, choice( 23, terms.scoreIndex ) );
						put( 40, String( terms.stake ) );
						put(
							32,
							terms.period >= WAR_UNLIMITED ?
								unlimited :
								[
									choice( 24, terms.period >>> 10 & 31 ),
									choice( 25, terms.period >>> 15 & 31 ),
									choice( 26, (terms.period >>> 20 & 63) / 10 )
								].join( " " )
						);
					}
					authoredLabeledButton(
						at( 5 ),
						x,
						y,
						warInvite ? "invite-accept" : "war-confirm",
						hudCopy( at( 5 ).text ),
						warInvite ?
							Number( game?.progression?.gold ?? 0 ) < terms.stake :
							input &&
							(terms.period === 0 || terms.name.length < 2 || terms.name === game?.social?.guild?.name ||
								terms.stake > WAR_MAX_STAKE)
					);
					authoredLabeledButton(
						at( 6 ),
						x,
						y,
						warInvite ? "invite-refuse" : "war-cancel",
						hudCopy( at( 6 ).text )
					);
					if ( input && warDialog.combo ) {
						const id = warDialog.combo,
							count = [ 8, 32, 25, 7 ][id - 23]!,
							visible = id === 23 ? 8 : 7,
							first = Math.min( warDialog.comboOffset, count - visible );
						const r = authoredRect( at( id ), x, y ),
							list: UiRect = [ r[0], r[1] + 20, r[2], visible * 18 ];
						rect( list, [ 0, 0, 0, 1 ] );
						blocks.push( list );
						for ( let i = 0; i < visible; i++ ) {
							const value = first + i,
								cell: UiRect = [ list[0], list[1] + i * 18, list[2] - (count > visible ? 16 : 0), 18 ];
							quads.push( ...text.quads( choice( id, value ), cell, cell, white ) );
							controls.push( {
								id: "war-choice:" + value,
								label: choice( id, value ),
								rect: cell,
								kind: "button"
							} );
						}
						if ( count > visible ) {
							const scroll = chatScrollbar(
								"war-combo",
								[ list[0] + list[2] - 16, list[1] + 16, 16, list[3] - 48 ],
								count,
								visible,
								count - visible - first,
								resources.size,
								full,
								hover,
								pressed
							);
							paths.push( ...scroll.paths );
							quads.push( ...scroll.quads );
							controls.push( ...scroll.controls );
						}
					}
				}
			}
			if ( worldVisible && warDialog.result ) {
				const result = warDialog.result,
					value = noticeText( hudCopy, { key: result.key, value: 0, arguments: result.names } ) +
						(result.additionalKey ? "\n" + hudCopy( result.additionalKey ) : "");
				const box = noticeDialog( w, h, value, value => text.run( value ).width );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( box.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						box.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push( ...text.quads( hudCopy( "UIIT_STT_EVENTGUIDE" ), box.title, full, white, { hAlign: 1 } ) );
				for ( const [i, line] of box.lines.entries() ) {
					quads.push(
						...text.quads( line, [ box.body[0], box.body[1] + i * 23, box.body[2], 23 ], full, white, {
							hAlign: 1
						} )
					);
				}
				button(
					"war-result-close",
					hudCopy( "UIIT_CTL_CONFIRM" ),
					...box.confirm.slice( 0, 3 ) as [number, number, number]
				);
			}
			const unionAsk = unionHud.question();
			if ( worldVisible && unionAsk ) {
				// 5F7670 asks through a 6888C0 simple message box (both calls,
				// 5F778A and 5F7817), not the MsgBoxINIF proposal geometry.
				const title = unionAsk.kind === "exit" ?
						"UIIT_STT_GUILD_RESPECT_ALLY_EXIT" :
						"UIIT_STT_GUILD_RESPECT_ALLY_EXPULSION",
					question = unionAsk.kind === "exit" ?
						hudCopy( "UIIT_MSG_GUILD_QUESTION_ALLY_EXIT" ) :
						hudCopy( "UIIT_MSG_QUESTION_GUILD_RESPECT_ALLY_EXPEL" ).replace( "%s", () => unionAsk.name );
				controls = [];
				blocks = [ full ];
				simpleMessageBox( {
					title: hudCopy( title ),
					lines: [ question ],
					yes: "union-ask-yes",
					no: "union-ask-no"
				} );
			}
			if ( worldVisible && recallConfirm !== null ) {
				// 5C82D0 / 52F460 type 5 retain the 308x148 MsgBoxINIF geometry.
				const layout = guildProposalLayout( w, h );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				const region = String( game?.pose?.regionId ?? 0 ),
					town = hud.data()?.zones[region] ?? hudCopy( region ),
					current = hudCopy( "UIIT_MSG_MSGBOX_SETREBIRTH_CURPOS" ).replace( "%s", town );
				quads.push(
					...text.quads( hudCopy( "UIIT_STT_AGREEMENT_BOX" ), layout.title, full, white, { hAlign: 1 } ),
					...text.quads( current, layout.name, full, white, { hAlign: 1 } ),
					...text.quads( hudCopy( "UIIT_MSG_MSGBOX_SETREBIRTH_ASKCERTIFY" ), layout.question, full, white, {
						hAlign: 1
					} )
				);
				button(
					"recall-confirm",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					"recall-cancel",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			if ( !game?.npcConversation || game.npcConversation.phase !== "menu" ) jobHud.reset();
			const productionAsk = fortressProductionHud.question(), productionBox = hud.data()?.windows.ifmessagebox;
			if ( worldVisible && productionAsk && productionBox ) {
				// 656CA0 / 65C6D0 open CIFMessageBox kind 0xC (MsgBoxMakeItem) or 0xD
				// (MsgBoxMakeItemCancel); 52C870 modes 0xA and 0xB fill them.
				const making = productionAsk.kind === "make", base = making ? 110 : 120;
				const layout = messageBox( w, h, FORTRESS_PRODUCTION_BOX[0], FORTRESS_PRODUCTION_BOX[1] ),
					[fx, fy] = layout.frame;
				const section = Object.fromEntries(
					Object.entries( productionBox ).filter( ( [, row] ) => row.id >= base && row.id <= base + 6 )
				);
				const at = ( id: number ) => Object.values( section ).find( row => row.id === id );
				const item = making ?
					productionAsk.item :
					fortressProductionItems().find( row => row.refObjId === productionAsk.order.refObjId );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				nativePage( section, fx, fy, [ base, base + 1, base + 4, base + 6 ] );
				const main = at( base + 4 );
				if ( main ) {
					authoredText(
						main,
						fx,
						fy,
						hudCopy(
							making ? "UIIT_MSG_FORT_SMITH_PRODUCT_WINDOW" : "UIIT_MSG_FORT_SMITH_PRODUCT_CANCEL_WINDOW"
						).replace( "%s", item?.name ?? "" )
					);
				}
				const edit = at( base + 6 );
				if ( edit && productionAsk.kind === "make" ) {
					partyEdit(
						edit,
						fx,
						fy,
						FORTRESS_PRODUCTION_COUNT,
						productionAsk.count,
						FORTRESS_PRODUCTION_COUNT_LENGTH
					);
				} else if ( edit && productionAsk.kind === "cancel" ) {
					// 52DC80 disables the edit and prints the order's count in it.
					authoredText( edit, fx, fy, String( productionAsk.order.count ) );
				}
				for (
					const [id, control] of [
						[ base + 1, "fortress-production-yes" ],
						[ base, "fortress-production-no" ]
					] as const
				) {
					const node = at( id );
					if ( node ) {
						authoredLabeledButton(
							{ ...node, rect: [ node.rect[0], node.rect[1], 76, 24 ] },
							fx,
							fy,
							control,
							hudCopy( node.text ),
							control === "fortress-production-yes" && productionAsk.kind === "make" &&
								!Number( productionAsk.count )
						);
					}
				}
			}
			const taxAsk = fortressTaxHud.question(), taxBox = hud.data()?.windows.ifmessagebox;
			if ( worldVisible && taxAsk && taxBox ) {
				// 665BA0 opens CIFMessageBox kind 0xA (MsgBoxTaxModify, 292x178) or
				// 0xB (MsgBoxTaxLevy, 286x151); 52C870 modes 8 and 9 fill it.
				const modify = taxAsk.kind === "rate", prefix = modify ? "GDR_MB_TAX_MODIFY_" : "GDR_MB_TAX_LEVY_";
				const layout = messageBox( w, h, modify ? 292 : 286, modify ? 178 : 151 ), [fx, fy] = layout.frame;
				const section = Object.fromEntries(
					Object.entries( taxBox ).filter( ( [name] ) => name.startsWith( prefix ) )
				);
				const at = ( id: number ) => Object.values( section ).find( row => row.id === id );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				nativePage( section, fx, fy, modify ? [ 100, 101 ] : [ 94, 95, 96 ] );
				if ( taxAsk.kind === "rate" ) {
					const context = fortressTaxHud.context();
					const row = view?.gameplay?.fortress?.fortresses.find( r => r.id === context?.fortress );
					for (
						const [id, key, value] of [
							[
								104,
								"UIIT_MSG_FORT_MANAGER_TAXCHANGE_WINDOW1",
								row?.nameStrId ? hudCopy( row.nameStrId ) : ""
							],
							[ 105, "UIIT_MSG_FORT_MANAGER_TAXCHANGE_WINDOW2", String( taxAsk.from ) ],
							[ 106, "UIIT_MSG_FORT_MANAGER_TAXCHANGE_WINDOW3", String( taxAsk.to ) ]
						] as const
					) {
						const line = at( id );
						if ( line ) {
							authoredText(
								line,
								fx,
								fy,
								noticeText( hudCopy, { key, value: 0, arguments: [ value ] } )
							);
						}
					}
				} else {
					const edit = at( 94 );
					if ( edit ) {
						partyEdit( edit, fx, fy, FORTRESS_TAX_AMOUNT, taxAsk.amount, FORTRESS_TAX_AMOUNT_LENGTH );
					}
				}
				for (
					const [id, control] of modify ?
						[ [ 101, "fortress-tax-yes" ], [ 100, "fortress-tax-no" ] ] as const :
						[ [ 96, "fortress-tax-yes" ], [ 95, "fortress-tax-no" ] ] as const
				) {
					const node = at( id );
					if ( node ) {
						authoredLabeledButton(
							{ ...node, rect: [ node.rect[0], node.rect[1], 76, 24 ] },
							fx,
							fy,
							control,
							hudCopy( node.text ),
							control === "fortress-tax-yes" && taxAsk.kind === "collect" && !taxAsk.amount
						);
					}
				}
			}
			const fortressAsk = fortressWarHud.question(), staffAsk = fortressStaffHud.question();
			if ( worldVisible && (fortressAsk || staffAsk) ) {
				// 6649C0's question boxes 0x64-0x67.
				const layout = guildProposalLayout( w, h ),
					row = view?.gameplay?.fortress?.fortresses.find( r => r.id === fortressAsk?.fortress ),
					name = row?.nameStrId ? hudCopy( row.nameStrId ) : "";
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} ),
					...text.quads(
						fortressWarFormat(
							hudCopy(
								staffAsk ?
									"UIIT_MSG_FORT_MANAGER_EMPLOY_WINDOW" :
									fortressWarQuestionKey( fortressAsk!.question )
							),
							staffAsk ?
								[
									hudCopy(
										"SN_FORTRESS_MANAGER_NPC_NAME_" +
											(staffAsk.flag === 1 ?
												"BATTLEAID" :
												staffAsk.flag === 2 ?
												"SMITH" :
												"TRAINER")
									),
									30000,
									3000
								] :
								[ name, row?.requestFee ?? 0 ]
						),
						layout.name,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				button(
					staffAsk ? "fortress-staff-yes" : "fortress-war-yes",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					staffAsk ? "fortress-staff-no" : "fortress-war-no",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			const jobAsk = jobHud.confirm(), aliasWindow = jobHud.alias();
			if ( worldVisible && (jobAsk || aliasWindow) ) {
				// 5D26F0's question boxes (types 4 and 5) and CIFJobAlias.
				const layout = guildProposalLayout( w, h );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads(
						hudCopy(
							jobAsk ?
								"UIIT_STT_CONFIRM_BOX" :
								aliasWindow?.modify ?
								"UIIT_PAG_ALIAS_MODIFY" :
								"UIIT_PAG_ALIAS_CREATE"
						),
						layout.title,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					),
					...text.quads(
						hudCopy(
							jobAsk ?
								(jobAsk.kind === "join" ?
									"UIIT_STT_JOBGUILD_JOIN_WINDOW" :
									"UIIT_STT_JOBGUILD_WITHD_WINDOW") :
								aliasWindow?.modify ?
								"UIIT_STT_ALIAS_MODIFY_WINDOW" :
								"UIIT_STT_ALIAS_CREATE_WINDOW"
						),
						layout.name,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				if ( jobAsk ) {
					button(
						"job-confirm-yes",
						hudCopy( "UIIT_CTL_YES" ),
						...layout.accept.slice( 0, 3 ) as [number, number, number]
					);
					button(
						"job-confirm-no",
						hudCopy( "UIIT_CTL_NO" ),
						...layout.refuse.slice( 0, 3 ) as [number, number, number]
					);
				} else if ( aliasWindow ) {
					const field: UiRect = [ layout.question[0], layout.question[1], layout.question[2], 16 ];
					controls.push( {
						id: "job-alias-text",
						label: hudCopy( "UIIT_STT_ALIAS_CREATE_WINDOW" ),
						kind: "text",
						value: aliasWindow.text,
						rect: field,
						maxLength: 12
					} );
					rect( field, [ 0, 0, 0, .6 ], "", [ 0, 0, 1, 1 ], full );
					quads.push(
						...text.quads( aliasWindow.text, field, field, white, {
							hAlign: 1,
							vAlign: 1,
							overflow: "clip"
						} )
					);
					if ( focus === "job-alias-text" && caretVisible ) {
						const width = text.run( aliasWindow.text ).width;
						rect(
							[ field[0] + (field[2] + width) / 2, field[1] + 1, 2, 14 ],
							white,
							"",
							[ 0, 0, 1, 1 ],
							field
						);
					}
					const [ax, ay, aw] = layout.accept, [rx, ry, rw] = layout.refuse;
					button( "job-alias-check", hudCopy( "UIIT_CTL_CHECK" ), ax!, ay!, aw! );
					button( "job-alias-ok", hudCopy( "UIIT_CTL_OK" ), rx!, ry!, rw! );
					button( "job-alias-cancel", hudCopy( "UIIT_CTL_CANCEL" ), rx! + rw! + 8, ry!, rw! );
				}
			}
			if ( !game?.npcConversation || game.npcConversation.phase !== "menu" ) guildManagerHud.reset();
			else if ( game.social?.compensation !== undefined && !guildManagerHud.question() ) {
				guildManagerHud.ask( "compensation", game.npcConversation.gid, game.social.compensation );
			}
			const guildAsk = guildManagerHud.question(),
				guildField = guildManagerHud.field(),
				guildVote = guildManagerHud.vote();
			if ( worldVisible && (guildAsk || guildField || guildVote) ) {
				// The guild manager's boxes (guild-manager-hud.ts) in the job box's frame.
				const layout = guildProposalLayout( w, h );
				const say = ( key: string, ...args: string[] ) =>
					noticeText( hudCopy, { key, value: 0, arguments: args } );
				const price = guildAsk?.kind === "level-up" ? guildLevelUpPrice( guildAsk.value ) : undefined;
				const title = guildField?.kind === "create" ?
					"UIIT_CTL_GUILD_CREATE" :
					guildField ?
					"UIIT_STT_MLEAVE_WINDOWS" :
					guildVote ?
					"UIIT_STT_MRELEASE_VOTESTATE" :
					"UIIT_STT_CONFIRM_BOX";
				const lines: Record<string, readonly [string, string]> = {
					"level-up": [
						say( "UIIT_MSG_GUILD_LEVEL_UP_CONDITION", String( (guildAsk?.value ?? 0) + 1 ) ),
						price ?
							hudCopy( "UIIT_STT_NEED_GP" ) + " : " + price.gp + "   " +
							hudCopy( "UIIT_STT_CIRCULATION_NEEDMONEY" ) + " : " + price.gold :
							hudCopy( "UIIT_MSG_ERROR_GUILD_LEVEL_UP_FULL" )
					],
					"dissolve": [
						hudCopy( "UIIT_MSG_GUILD_BREAK_CONFIRM" ),
						hudCopy( "UIIT_MSG_GUILD_BREAK_ANOTHER_EXPLAIN" )
					],
					"secede": [ hudCopy( "UIIT_MSG_GUILD_SECESSION_CONFIRM" ), "" ],
					"release": [ hudCopy( "UIIT_MSG_MRELEASE_CONFIRM" ), "" ],
					"compensation": [
						say( "UIIT_CTL_GUILDWAR_COMPENSATION_01", String( guildAsk?.value ?? 0 ) ),
						hudCopy( "UIIT_CTL_GUILDWAR_COMPENSATION_02" )
					],
					"master-leave": [ hudCopy( "UIIT_MSG_MLEAVE_INPUTID" ), "" ],
					"create": [ "", "" ],
					"vote": [
						Math.ceil( (guildVote?.remainingMs ?? 0) / 60000 ) + " " + hudCopy( "PARAM_MINUTE" ),
						""
					]
				};
				const [first, second] = lines[guildAsk?.kind ?? guildField?.kind ?? "vote"]!;
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( title ), layout.title, full, white, { hAlign: 1, vAlign: 0 } ),
					...text.quads( first, layout.name, full, white, { hAlign: 1, vAlign: 0 } )
				);
				if ( guildField ) {
					const field: UiRect = [ layout.question[0], layout.question[1], layout.question[2], 16 ];
					controls.push( {
						id: "guild-manager-text",
						label: hudCopy( title ),
						kind: "text",
						value: guildField.text,
						rect: field,
						maxLength: 12
					} );
					rect( field, [ 0, 0, 0, .6 ], "", [ 0, 0, 1, 1 ], full );
					quads.push(
						...text.quads( guildField.text, field, field, white, {
							hAlign: 1,
							vAlign: 1,
							overflow: "clip"
						} )
					);
					if ( focus === "guild-manager-text" && caretVisible ) {
						const width = text.run( guildField.text ).width;
						rect(
							[ field[0] + (field[2] + width) / 2, field[1] + 1, 2, 14 ],
							white,
							"",
							[ 0, 0, 1, 1 ],
							field
						);
					}
				} else {
					quads.push( ...text.quads( second, layout.question, full, white, { hAlign: 1 } ) );
				}
				button(
					"guild-manager-yes",
					hudCopy( guildAsk ? "UIIT_CTL_YES" : "UIIT_CTL_OK" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				if ( !guildVote ) {
					button(
						"guild-manager-no",
						hudCopy( guildAsk ? "UIIT_CTL_NO" : "UIIT_CTL_CANCEL" ),
						...layout.refuse.slice( 0, 3 ) as [number, number, number]
					);
				}
			}
			if ( panel !== "Shop" ) repairHud.reset();
			const repairCost = repairHud.confirmCost();
			if ( worldVisible && repairCost !== null ) {
				// 5B2B10 raises box 0x0C: Repair All's question and its total.
				const layout = guildProposalLayout( w, h );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} ),
					...text.quads( hudCopy( "UIIT_MSG_MSGBOX_REPAIR_ITEM" ), layout.name, full, white, {
						hAlign: 1,
						vAlign: 0
					} ),
					...text.quads( repairCost.toLocaleString( "en-US" ), layout.question, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				button(
					"repair-all-confirm",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					"repair-all-cancel",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			if ( structureRemoval && view?.gameplay?.target !== structureRemoval.gid ) structureRemoval = null;
			if ( worldVisible && structureRemoval ) {
				// 517750 raises UIIT_MSG_FORT_STRUCTURE_DELETE_WINDOW naming the target.
				const layout = guildProposalLayout( w, h );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} ),
					...text.quads(
						hudCopy( "UIIT_MSG_FORT_STRUCTURE_DELETE_WINDOW" ).replace( "%s", structureRemoval.name ),
						layout.name,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				button(
					"structure-remove-confirm",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					"structure-remove-cancel",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			if ( worldVisible && (cosHud.cleanConfirm() !== null || cosHud.targetUse() !== null) ) {
				// 6A2350 case 5 raises the type 0xD box with the two
				// UIIT_MSG_COS_CLEAN_CONFIRM lines before a transport is destroyed.
				const layout = guildProposalLayout( w, h );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} ),
					...text.quads(
						// CIFMessageBox type 0x1F (52F460) asks with the two CANCLE lines.
						hudCopy(
							cosHud.targetUse() ?
								"UIIT_MSG_QUESTION_SILKMALL_ITEM_USE_CANCLE_1" :
								"UIIT_MSG_COS_CLEAN_CONFIRM1"
						),
						layout.name,
						full,
						white,
						{
							hAlign: 1,
							vAlign: 0
						}
					),
					...text.quads(
						hudCopy(
							cosHud.targetUse() ?
								"UIIT_MSG_QUESTION_SILKMALL_ITEM_USE_CANCLE_2" :
								"UIIT_MSG_COS_CLEAN_CONFIRM2"
						),
						layout.question,
						full,
						white,
						{
							hAlign: 1,
							vAlign: 0
						}
					)
				);
				button(
					cosHud.targetUse() ? cosHud.targetUseBox() + "-confirm" : "cos-clean-confirm",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					cosHud.targetUse() ? cosHud.targetUseBox() + "-cancel" : "cos-clean-cancel",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			const teleportShown = mapTeleport.pending();
			if ( worldVisible && teleportShown ) {
				// Map teleport: the recall-appoint message box geometry.
				const layout = guildProposalLayout( w, h );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				const region = String( teleportShown.regionId ),
					place = hud.data()?.zones[region] ?? "Region " + region;
				quads.push(
					...text.quads( "Teleport", layout.title, full, white, { hAlign: 1 } ),
					...text.quads( place, layout.name, full, white, { hAlign: 1 } ),
					...text.quads( "Teleport here?", layout.question, full, white, { hAlign: 1 } )
				);
				button(
					"map-teleport-confirm",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					"map-teleport-cancel",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			if ( worldVisible && buffDismiss ) {
				const layout = guildProposalLayout( w, h ), [x, y] = layout.frame;
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				quads.push(
					...text.quads(
						hudCopy( "UIIT_MSG_QUESTION_SILKMALL_ITEM_USE_CANCLE_1" ),
						layout.name,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					),
					...text.quads(
						hudCopy( "UIIT_MSG_QUESTION_SILKMALL_ITEM_USE_CANCLE_2" ),
						layout.question,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				button(
					"buff-dismiss-confirm",
					hudCopy( "UIIT_CTL_YES" ),
					...layout.accept.slice( 0, 3 ) as [number, number, number]
				);
				button(
					"buff-dismiss-cancel",
					hudCopy( "UIIT_CTL_NO" ),
					...layout.refuse.slice( 0, 3 ) as [number, number, number]
				);
			}
			if ( worldVisible && shopWarning ) {
				controls = [];
				blocks = [ full ];
				simpleMessageBox( {
					title: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
					lines: [ hudCopy( "UIIT_MSG_CANNOT_BUYBACK_ITEM_SELL" ).replace( "%s", shopWarning.name ) ],
					yes: "shop-warning-confirm",
					no: "shop-warning-cancel",
					yesDisabled: !!game?.inventoryPending
				} );
			}
			if ( worldVisible && groundDrop ) {
				controls = [];
				blocks = [ full ];
				simpleMessageBox( {
					title: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
					lines: [ "UIIT_MSG_DROP_WARNING_1", "UIIT_MSG_DROP_WARNING_2" ].map( key => hudCopy( key ) ),
					yes: "ground-drop-confirm",
					no: "ground-drop-cancel",
					yesDisabled: !!game?.inventoryPending
				} );
			}
			const gathering = game?.questGathering;
			const delayRows = [
				...(game?.returnScroll ?
					[ {
						cast: game.returnScroll,
						name: game.returnScroll.name,
						id: "return-cancel",
						collection: false
					} ] :
					[]),
				...(gathering &&
						(gathering.durationMs === 0 || quickslotTime < gathering.startedAtMs + gathering.durationMs) ?
					[ {
						cast: gathering,
						name: guideResources.data()?.questPresentation.records[gathering.refId]?.title ?? "",
						id: "gathering-cancel",
						collection: true
					} ] :
					[])
			];
			for ( const [row, delay] of (worldVisible ? delayRows : []).entries() ) {
				const bar = returnScrollBar(
					delay.cast,
					w,
					h,
					{
						now: quickslotTime,
						row,
						collection: delay.collection,
						pressed: pressed === delay.id,
						focused: hover === delay.id
					}
				);
				blocks.push( bar.frame );
				controls.push( {
					id: delay.id,
					label: hudCopy( "UIIT_CTL_CANCEL" ),
					kind: "button",
					rect: bar.cancel
				} );
				for ( const q of bar.quads ) {
					paths.push( q.texture );
					if ( resources.has( q.texture ) ) quads.push( q );
				}
				quads.push( ...text.quads( delay.name, bar.name, full, white, { hAlign: 1, vAlign: 0 } ) );
			}
			if ( worldVisible && splitStack && [ "Inventory", "Shop", "COS inventory", "Storage" ].includes( panel ) ) {
				// Native 529E90, MsgBoxDivideCount authored 300x183; edit stays 42x24.
				const layout = messageBox( w, h, 300, 183 ),
					[x, y] = layout.frame,
					item = game?.inventory.find( i => i.slot === splitStack!.slot );
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads( hudCopy( "UIIT_STT_INPUT_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				for (
					const [file, r] of [ [ "interface/messagebox/msgbox_itemwindow.png", [ x + 18, y + 44, 48, 48 ] ], [
						"interface/messagebox/msgbox_iteminfo_3.png",
						[ x + 73, y + 44, 212, 48 ]
					], [ "interface/messagebox/msgbox_quantity.png", [ x + 223, y + 102, 42, 24 ] ] ] as const
				) {
					const path = ROOT + file;
					image( r, path );
				}
				const icon = iconPath( item?.icon );
				if ( icon ) {
					paths.push( icon );
					if ( resources.has( icon ) ) rect( [ x + 25, y + 51, 32, 32 ], white, icon );
				}
				// Retain authored geometry; shared single-line fitting contains long
				// localized captions before the neighboring quantity controls.
				for (
					const [value, r, color, vAlign] of [
						[ item?.name ?? "", [ x + 77, y + 47, 202, 19 ], [ 238 / 255, 218 / 255, 164 / 255, 1 ], 1 ],
						[ hudCopy( "UIIT_MSG_MSGBOX_DIVIDE_CURRENT_ITEM" ), [ x + 77, y + 68, 202, 19 ], white, 1 ],
						[ hudCopy( "UIIT_MSG_MSGBOX_CURRENT_COUNT" ), [ x + 34, y + 106, 64, 19 ], white, 0 ],
						[ hudCopy( "UIIT_MSG_MSGBOX_DIVIDE_COUNT" ), [ x + 153, y + 106, 64, 19 ], [
							239 / 255,
							218 / 255,
							164 / 255,
							1
						], 0 ]
					] as const
				) quads.push( ...text.quads( value, r, full, color, { hAlign: 1, vAlign } ) );
				quads.push(
					...text.quads(
						String( splitStack.quantity ),
						[ x + 105, y + 106, 41, 19 ],
						[ x + 105, y + 106, 41, 19 ],
						white,
						{ hAlign: 0, vAlign: 0 }
					)
				);
				const edit: UiRect = [ x + 230, y + 107, 28, 14 ];
				controls.push( {
					id: "split-amount",
					label: hudCopy( "UIIT_MSG_MSGBOX_DIVIDE_COUNT" ),
					kind: "text",
					value: splitAmount,
					rect: edit,
					maxLength: 5
				} );
				quads.push(
					...text.quads( splitAmount, edit, edit, white, { hAlign: 1, vAlign: 1, overflow: "clip" } )
				);
				if ( focus === "split-amount" ) {
					const left = edit[0] + (edit[2] - text.run( splitAmount ).width) / 2,
						start = Math.min( ...selection, splitAmount.length ),
						end = Math.min( Math.max( ...selection ), splitAmount.length ),
						before = text.run( splitAmount.slice( 0, start ) ).width,
						through = text.run( splitAmount.slice( 0, end ) ).width;
					if ( end > start ) {
						rect(
							[ left + before, edit[1], through - before, 14 ],
							[ .2, .4, .7, .5 ],
							"",
							[ 0, 0, 1, 1 ],
							edit
						);
					}
					if ( caretVisible ) rect( [ left + through, edit[1], 2, 14 ], white, "", [ 0, 0, 1, 1 ], edit );
				}
				button(
					"split-confirm",
					hudCopy( "UIIT_CTL_CONFIRM" ),
					x + 71,
					y + 143,
					76,
					!!game?.inventoryPending,
					false,
					6
				);
				button( "split-cancel", hudCopy( "UIIT_CTL_CANCEL" ), x + 151, y + 143, 76, false, false, 6 );
			}
			if (
				worldVisible &&
				(warDialog.money !== null || goldDialog && (panel === "Inventory" || panel === "Storage"))
			) {
				const amount = warDialog.money ?? goldAmount,
					inputId = warDialog.money !== null ? "war-money" : "gold-amount";
				const layout = messageBox( w, h, 308, 148 ), [x, y] = layout.frame;
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads( hudCopy( "UIIT_STT_CONFIRM_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				for (
					const [file, r] of [ [ "interface/messagebox/msgbox_itemwindow.png", [ x + 18, y + 44, 48, 48 ] ], [
						"icon/mini_gold_icon.png",
						[ x + 25, y + 51, 32, 32 ]
					], [ "interface/messagebox/msgbox_iteminfo.png", [ x + 73, y + 44, 212, 48 ] ] ] as const
				) {
					const path = ROOT + file;
					image( r, path );
				}
				quads.push(
					...text.quads( hudCopy( "UIIT_STT_AMOUNT_MONEY" ), [ x + 73, y + 51, 212, 12 ], full, white, {
						hAlign: 1,
						vAlign: 0
					} ),
					...text.quads( hudCopy( "UIIT_STT_GOLD" ), [ x + 255, y + 72, 23, 12 ], full, white )
				);
				const edit: UiRect = [ x + 128, y + 71, 107, 14 ];
				controls.push( {
					id: inputId,
					label: hudCopy( "UIIT_STT_AMOUNT_MONEY" ),
					kind: "text",
					value: amount,
					rect: edit,
					maxLength: 20
				} );
				quads.push(
					...text.quads( amount, [ edit[0], edit[1], edit[2] - 2, edit[3] ], edit, white, {
						hAlign: 2,
						vAlign: 0,
						overflow: "clip"
					} )
				);
				if ( focus === inputId ) {
					const width = text.run( amount ).width,
						start = Math.min( selection[0] ?? 0, selection[1] ?? 0, amount.length ),
						end = Math.min( Math.max( selection[0] ?? 0, selection[1] ?? 0 ), amount.length ),
						left = edit[0] + edit[2] - 2 - width,
						before = text.run( amount.slice( 0, start ) ).width,
						through = text.run( amount.slice( 0, end ) ).width;
					if ( end > start ) {
						rect(
							[ left + before, edit[1], through - before, 14 ],
							[ .2, .4, .7, .5 ],
							"",
							[ 0, 0, 1, 1 ],
							edit
						);
					}
					if ( caretVisible ) rect( [ left + through, edit[1], 2, 14 ], white, "", [ 0, 0, 1, 1 ], edit );
				}
				button(
					warDialog.money !== null ? "war-money-ok" : "drop-gold",
					hudCopy( "UIIT_CTL_CONFIRM" ),
					x + 123,
					y + 101,
					76,
					!!game?.inventoryPending
				);
				button(
					warDialog.money !== null ? "war-money-cancel" : "gold-cancel",
					hudCopy( "UIIT_CTL_CANCEL" ),
					x + 203,
					y + 101,
					76
				);
			}
			const stallBox = stallHud.prompt(), stallPage = hud.data()?.windows.ifstall;
			if ( worldVisible && stallBox && game?.stall && stallPage ) {
				// CIFStall's message boxes (5A1DF0, 5A1A40): one modal box at a time.
				const [boxWidth, boxHeight] = STALL_PROMPT_SIZE[stallBox.kind],
					layout = messageBox( w, h, boxWidth, boxHeight ),
					[x, y] = layout.frame,
					template = Object.values( stallPage ).find( n => n.id === 11 )!,
					line = ( value: string, dy: number ) => {
						quads.push(
							...text.quads( value, [ x + 20, y + dy, boxWidth - 40, 14 ], full, white, {
								hAlign: 1,
								vAlign: 0
							} )
						);
					},
					edit = ( id: string, value: string, r: UiRect, maxLength: number ) => {
						image(
							[ r[0] - 4, r[1] - 3, r[2] + 8, r[3] + 6 ],
							ROOT + "interface/messagebox/msgbox_quantity.png"
						);
						partyEdit( { ...template, name: id, rect: r }, 0, 0, id, value, maxLength );
					};
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads(
						hudCopy( stallBox.kind === "price" ? "UIIT_STT_INPUT_BOX" : "UIIT_STT_CONFIRM_BOX" ),
						layout.title,
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				const stall = game.stall;
				if ( stallBox.kind === "title" || stallBox.kind === "greeting" ) {
					line( hudCopy( stallBox.kind === "title" ? "UIIT_STT_INSERT_STALL_NAME" : "UIIT_STT_STALL" ), 48 );
					edit( STALL_PROMPT_TEXT, stallBox.text, [ x + 28, y + 76, boxWidth - 56, 14 ], STALL_TEXT_LIMIT );
				} else if ( stallBox.kind === "price" ) {
					const item = game.inventory.find( row => row.slot === stallBox.bagSlot );
					line( item?.name ?? "", 46 );
					quads.push(
						...text.quads(
							hudCopy( "UIIT_CTL_WARENETWORK_RESULT_FIGURE" ),
							[ x + 30, y + 72, 80, 14 ],
							full,
							white
						),
						...text.quads(
							hudCopy( "UIIT_CTL_WARENETWORK_RESULT_PRICE" ),
							[ x + 30, y + 96, 80, 14 ],
							full,
							white
						)
					);
					edit( STALL_PROMPT_QUANTITY, stallBox.quantity, [ x + 120, y + 72, 60, 14 ], 5 );
					edit( STALL_PROMPT_PRICE, stallBox.price, [ x + 120, y + 96, 150, 14 ], 10 );
				} else if ( stallBox.kind === "buy" || stallBox.kind === "network-buy" ) {
					const offer = stallBox.kind === "buy" ?
						stall.offers.find( row => row.slot === stallBox.slot ) :
						stall.network.rows[stallBox.row];
					line(
						hudCopy( "UIIT_MSG_WARENETWORK_BUY_CONFIRM" ).replace( "%s", offer?.item.name ?? "" ).replace(
							"%d",
							String( offer?.quantity ?? 0 )
						),
						56
					);
					line( String( offer?.price ?? "" ), 76 );
				} else {
					for ( const [i, key] of [ "01", "02", "03" ].entries() ) {
						line( hudCopy( "UIIT_MSG_WARENETWORK_REGIST_" + key ), 50 + i * 18 );
					}
				}
				const pending = stallBox.kind === "network-buy" && stall.network.buying !== null;
				button(
					"stall-prompt-ok",
					hudCopy( "UIIT_CTL_CONFIRM" ),
					x + boxWidth / 2 - 80,
					y + boxHeight - 40,
					76,
					pending
				);
				button(
					"stall-prompt-cancel",
					hudCopy( "UIIT_CTL_CANCEL" ),
					x + boxWidth / 2 + 4,
					y + boxHeight - 40,
					76
				);
			}
			if (
				worldVisible && carriedItem &&
				[ "Inventory", "Shop", "Alchemy", GRANT_PANEL, "COS inventory", "Storage" ].includes( panel )
			) {
				const item = carriedRow( carriedItem, game ), path = iconPath( item?.icon );
				if ( path ) {
					paths.push( path );
					if ( resources.has( path ) ) {
						rect( [ carriedItem.x - 16, carriedItem.y - 16, 32, 32 ], white, path );
					}
				}
			}
			if ( worldVisible && rebirthDue && !deathDismissed ) {
				const layout = rebirthDialog( w, h, deathPosition ),
					art = ROOT + "interface/messagebox/msgbox_rebirth.png",
					base = ROOT + "interface/messagebox/msgbox_rebirth_button.png";
				blocks.push( layout.frame );
				controls.push( {
					id: "rebirth-body",
					label: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
					kind: "region",
					rect: layout.frame
				}, {
					id: "rebirth-drag",
					label: hudCopy( "UIIT_STT_CONFIRM_BOX" ),
					kind: "region",
					draggable: true,
					rect: layout.drag
				} );
				paths.push(
					...partyProposalAssets(),
					art,
					base,
					base.replace( ".png", "_focus.png" ),
					base.replace( ".png", "_press.png" )
				);
				quads.push( ...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ) );
				quads.push(
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				for (
					const [key, r] of [ [ "UIIT_STT_CONFIRM_BOX", layout.title ], [
						"UIIT_MSG_MSGBOX_ASK_SELF_REBIRTH_2",
						layout.second
					] ] as const
				) quads.push( ...text.quads( hudCopy( key ), r, full, white, { hAlign: 1, vAlign: 0 } ) );
				const tokens = guideTokens( hudCopy( "UIIT_MSG_MSGBOX_ASK_SELF_REBIRTH_1" ) ),
					width = tokens.reduce( ( n, t ) => n + (t.kind === "text" ? text.run( t.value ).width : 0), 0 );
				let tx = layout.first[0] + Math.floor( (layout.first[2] - width) / 2 );
				for ( const token of tokens ) {
					if ( token.kind === "text" ) {
						const width = text.run( token.value ).width;
						quads.push(
							...text.quads(
								token.value,
								[ tx, layout.first[1], width, layout.first[3] ],
								layout.first,
								token.color ?? white,
								{ vAlign: 0 }
							)
						);
						tx += width;
					}
				}
				if ( resources.has( art ) ) rect( layout.art, white, art );
				const low = game?.progression?.level !== undefined && game.progression.level <= 10;
				for (
					const [id, key, r] of [ [ "rebirth-point", "UIIT_MSG_MSGBOX_REBIRTH_POINT_BUTTON", layout.point ], [
						"rebirth-alternate",
						low ? "UIIT_MSG_MSGBOX_REBIRTH_STANDING_BUTTON" : "UIIT_MSG_MSGBOX_REBIRTH_HELP_BUTTON",
						layout.alternate
					] ] as const
				) {
					const down = pressed === id && hover === id,
						path = down ?
							base.replace( ".png", "_press.png" ) :
							(hover === id || focus === id) ?
							base.replace( ".png", "_focus.png" ) :
							base;
					controls.push( { id, label: hudCopy( key ), kind: "button", rect: r } );
					if ( resources.has( path ) ) rect( r, white, path );
					quads.push(
						...text.quads(
							hudCopy( key ),
							[ r[0] + (down ? 1 : 0), r[1] + 6 + (down ? 1 : 0), r[2], 18 ],
							r,
							[ 1, 230 / 255, 176 / 255, 1 ],
							{ hAlign: 1, vAlign: 0 }
						)
					);
				}
			}
			// The resurrection question comes while the player is dead. Opening it
			// retired the death box (7644E0, see the proposal sync); a death box
			// the player reopens by selecting themselves (6813E0) and a pending
			// invitation box keep their controls. Every other open dialog loses
			// its controls until the question is answered; the question is not
			// dismissed by a revive or the server's 30 s expiry.
			if ( game?.social?.resurrection && worldVisible ) {
				/*
				================
				kept

				Controls that survive the question: the death box, a pending
				invitation box and the question itself.
				================
				*/
				const kept = ( id: string | null ) =>
					!!id &&
					(id.startsWith( "rebirth-" ) || id.startsWith( "invite-" ) || id.startsWith( "resurrection-" ));
				controls = controls.filter( c => kept( c.id ) );
				blocks = [ full ];
				if ( !kept( focus ) ) {
					focus = null;
					composing = false;
				}
				paths.push( ...partyProposalAssets() );
				const mutation = !!game.social.resurrection.mutation,
					layout = resurrectionLayout( w, h, resurrectionPrompt.position(), mutation );
				controls.push( {
					id: "resurrection-body",
					label: hudCopy( "UIIT_STT_AGREEMENT_BOX" ),
					kind: "region",
					rect: layout.frame
				}, {
					id: "resurrection-drag",
					label: hudCopy( "UIIT_STT_AGREEMENT_BOX" ),
					kind: "region",
					draggable: true,
					rect: layout.drag
				} );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					),
					// Both boxes carry the agreement caption (52F460 case 3, 7644E0 case 7).
					...text.quads( hudCopy( "UIIT_STT_AGREEMENT_BOX" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				// Kind 4's third line is the note in 0xFFFFF1D3 (52F460 case 3).
				resurrectionQuestion( mutation ).forEach( ( key, i ) =>
					quads.push(
						...text.quads(
							hudCopy( key ),
							layout.lines[i]!,
							full,
							!mutation && i === 2 ?
								resurrectionNoteColor() :
								white,
							{ vAlign: 0 }
						)
					)
				);
				button( "resurrection-accept", hudCopy( "UIIT_CTL_YES" ), layout.accept[0], layout.accept[1], 76 );
				button( "resurrection-refuse", hudCopy( "UIIT_CTL_NO" ), layout.refuse[0], layout.refuse[1], 76 );
			}
			const fatalAssetFailure = next.frontend?.error ?? next.resourceError ?? hud.error() ?? text.error() ??
				guideResources.error() ??
				minimapResources.error();
			const assetFailure = next.worldError ?? fatalAssetFailure ?? resources.error();
			let nativeLoadError = false;
			if ( assetFailure && phase !== "disconnected" ) {
				const dw = Math.min( 600, w - 20 ),
					details = resourceErrorLines( assetFailure, dw - 48, value => text.run( value ).width ),
					layout = messageBox( w, h, dw, 122 + details.length * 14 ),
					[ex, ey] = layout.frame,
					art = partyProposalAssets();
				paths.push( ...art );
				nativeLoadError = art.every( path => resources.has( path ) ) && !!text.path() &&
					resources.has( text.path()! ) && !text.error();
				controls = [];
				blocks = [ full ];
				if ( nativeLoadError ) {
					quads.push(
						...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full )
					);
					quads.push(
						...frameRing(
							layout.frame,
							MESSAGE_FRAME,
							PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
							full
						)
					);
					quads.push(
						...text.quads(
							next.worldRetrying ? "Connection interrupted" : "Unable to finish loading",
							layout.title,
							full,
							white,
							{ hAlign: 1, vAlign: 0 }
						)
					);
					quads.push(
						...text.quads(
							next.worldRetrying ? "Connection lost. Retrying automatically..." : next.worldError ?
								"World resources could not be loaded." :
								fatalAssetFailure ?
								"Unable to load resources. Reload to try again." :
								"Interface resources unavailable. Retrying...",
							[ ex + 16, ey + 48, dw - 32, 18 ],
							full,
							white,
							{ hAlign: 1, vAlign: 0 }
						)
					);
					for ( const [i, line] of details.entries() ) {
						quads.push(
							...text.quads( line, [ ex + 24, ey + 74 + i * 14, dw - 48, 14 ], full, white, {
								vAlign: 0
							} )
						);
					}
					if ( next.worldError ) {
						button(
							"world-load-retry",
							"Retry",
							ex + (dw - 76) / 2,
							ey + layout.frame[3] - 38,
							76,
							false,
							false,
							6
						);
					}
				}
			}
			if ( worldVisible && activeNoticeDialog ) {
				const layout = noticeDialog(
					w,
					h,
					activeNoticeDialog.lines.map( key => hudCopy( key ) ).join( "\n" ),
					value => text.run( value ).width,
					noticeDialogPosition
				);
				noticeDialogFrame = layout.frame;
				controls = [];
				blocks = [ full ];
				paths.push( ...partyProposalAssets() );
				controls.push( {
					id: "notice-dialog-drag",
					label: hudCopy( activeNoticeDialog.title ),
					kind: "region",
					draggable: true,
					rect: layout.drag
				} );
				quads.push(
					...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads( hudCopy( activeNoticeDialog.title ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				for ( const [i, line] of layout.lines.entries() ) {
					quads.push(
						...text.quads(
							line,
							[ layout.body[0], layout.body[1] + i * 23, layout.body[2], 23 ],
							full,
							white,
							{ hAlign: 1, vAlign: 0 }
						)
					);
				}
				button(
					"notice-dialog-confirm",
					hudCopy( "UIIT_CTL_CONFIRM" ),
					layout.confirm[0],
					layout.confirm[1],
					76,
					false,
					false,
					6
				);
			}
			if ( phase === "disconnected" ) {
				/*
				================
				copy
				================
				*/
				const layout = disconnectDialog( w, h, disconnectPosition ),
					copy = ( key: string, fallback: string ) => hudCopy( key ) || title.catalog( key ) || fallback;
				controls = [];
				blocks = [ full ];
				if ( focus !== "disconnect-confirm" ) {
					focus = null;
					composing = false;
				}
				controls.push( {
					id: "disconnect-drag",
					label: copy( "UIIT_STT_CONFIRM_BOX", "Confirmation window" ),
					kind: "region",
					draggable: true,
					rect: layout.drag
				} );
				paths.push( ...partyProposalAssets() );
				quads.push( ...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ) );
				quads.push(
					...frameRing(
						layout.frame,
						MESSAGE_FRAME,
						PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
						full
					)
				);
				quads.push(
					...text.quads( copy( "UIIT_STT_CONFIRM_BOX", "Confirmation window" ), layout.title, full, white, {
						hAlign: 1,
						vAlign: 0
					} )
				);
				quads.push(
					...text.box(
						(next.session?.disconnectMessage ||
							copy( "UIIT_MSG_MSGBOX_NETOFF", "Disconnected from the server." )) +
							(next.session?.incidentID ? `\nRef: ${next.session.incidentID}` : ""),
						[ layout.message[0] + 8, layout.message[1] - 14, layout.message[2] - 16, 54 ],
						full,
						white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
				button(
					"disconnect-confirm",
					copy( "UIIT_CTL_CONFIRM", "Confirm" ),
					layout.confirm[0],
					layout.confirm[1],
					76,
					false,
					false,
					6
				);
			}
			if ( worldVisible && game && itemMall.read().visible && hud.data() ) {
				const data = hud.data()!;
				let state = itemMall.read( game.itemMall );
				const root = data.windows.ifitemmall!;
				const frame = data.root.GDR_ITEM_MALL!;
				const ox = Math.floor( (w - frame.rect[2]) / 2 );
				const oy = Math.floor( (h - frame.rect[3]) / 2 );
				// Native mall is a modal owner; underlying windows retain state but
				// cannot receive input through its scene or child controls.
				controls = [];
				blocks = [ full ];
				const mallAdmission = beginWindow();
				paths.push( ...mallMenuArtwork() );
				if ( compact ) {
					// Port-only, owner-approved reflow. Never scale the Mall's text or
					// combine its background with a purchase dialog's fitting bounds.
					const margin = 8, touch = 40, gap = 4, cardHeight = 112, tabPitch = 28;
					const contentWidth = w - margin * 2;
					const columns = Math.max( 1, Math.min( 2, Math.floor( contentWidth / 240 ) ) );
					const tabColumns = Math.max( 1, Math.floor( contentWidth / 140 ) );
					const tabRows = Math.ceil( state.tabs.length / tabColumns );
					const productTop = 124 + tabRows * tabPitch;
					const visibleRows = Math.max( 1, Math.floor( (h - touch - margin - productTop) / cardHeight ) );
					itemMall.setPageSize( Math.min( 6, columns * visibleRows ) );
					state = itemMall.read( game.itemMall );
					const busy = !!game.inventoryPending || state.batchPending;
					/*
					================
					mallText
					================
					*/
					function mallText( value: string, r: UiRect ) {
						quads.push( ...text.box( value, r, r, white, { fontIndex: 0, hAlign: 0, vAlign: 0 } ) );
					}
					/*
					================
					mallButton
					================
					*/
					function mallButton( id: string, caption: string, r: UiRect, disabled = false ) {
						button( id, caption, r[0], r[1] + (r[3] - 24) / 2, r[2], disabled );
						controls[controls.length - 1] = { ...controls[controls.length - 1]!, rect: r };
					}
					/*
					================
					mallEdit
					================
					*/
					function mallEdit( id: string, value: number, r: UiRect, maxLength: number ) {
						rect( r, [ .02, .025, .03, 1 ] );
						mallText( String( value ), [ r[0] + 4, r[1] + 12, r[2] - 8, 16 ] );
						controls.push( {
							id,
							label: id === "item-mall-quantity" ? "Quantity" : "Points",
							kind: "text",
							rect: r,
							value: String( value ),
							maxLength,
							disabled: busy
						} );
					}
					authoredChrome( { ...mallControl( root, 21 ), rect: [ 12, 40, w - 24, h - 52 ], text: "" }, 0, 0 );
					authoredChrome( { ...frame, type: "CIFFrame", rect: full, text: "" }, 0, 0 );
					mallText( hudCopy( frame.text ), [ margin, 12, contentWidth - 72, 22 ] );
					mallText(
						`Silk ${game.itemMall?.silk ?? 0}   Gift ${game.itemMall?.giftSilk ?? 0}   Points ${
							game.itemMall?.points ?? 0
						}`,
						[ margin, 52, Math.max( 1, contentWidth - 64 ), 28 ]
					);
					if ( state.pointDialog || state.selected || state.question ) {
						paths.push( ...partyProposalAssets() );
						quads.push(
							...normalTile(
								[ margin, 80, contentWidth, h - 80 - margin ],
								MESSAGE_TILE,
								resources.size( MESSAGE_TILE ),
								full
							)
						);
						quads.push(
							...frameRing(
								[ margin, 80, contentWidth, h - 80 - margin ],
								MESSAGE_FRAME,
								PARTS.map( part => resources.size( MESSAGE_FRAME + part + ".png" ) ),
								full
							)
						);
						const dx = margin, dy = 88, dw = contentWidth;
						if ( state.pointDialog ) {
							mallText( hudCopy( "UIIT_STT_SILKMALL_USE_POINT" ), [ dx, dy, dw, 32 ] );
							mallText( `Available ${game.itemMall?.points ?? 0} / Maximum ${state.pointLimit}`, [
								dx,
								dy + 36,
								dw,
								32
							] );
							mallEdit( "item-mall-point-value", state.pointDraft, [ dx, dy + 76, dw, touch ], 10 );
							mallButton( "item-mall-points-apply", hudCopy( "UIIT_CTL_CONFIRM" ), [
								dx,
								dy + 124,
								(dw - gap) / 2,
								touch
							], busy );
							mallButton( "item-mall-points-close", hudCopy( "UIIT_CTL_CANCEL" ), [
								dx + (dw + gap) / 2,
								dy + 124,
								(dw - gap) / 2,
								touch
							] );
						} else if ( state.selected && game.itemMall ) {
							const offer = state.selected;
							mallText( hudCopy( offer.name ), [ dx, dy, dw, 32 ] );
							const price = mallCurrencyRows( offer, state.quantity, state.points ).rows.map( row =>
								`${hudCopy( row.label )}: ${row.amount}`
							).join( "   " );
							mallText( price, [ dx, dy + 36, dw, 40 ] );
							mallButton(
								"item-mall-quantity-down",
								"−",
								[ dx, dy + 80, touch, touch ],
								busy || state.quantity <= 1
							);
							mallEdit( "item-mall-quantity", state.quantity, [
								dx + touch + gap,
								dy + 80,
								dw - 2 * (touch + gap),
								touch
							], 5 );
							mallButton(
								"item-mall-quantity-up",
								"+",
								[ dx + dw - touch, dy + 80, touch, touch ],
								busy || state.quantity >= offer.purchaseLimit
							);
							if ( offer.allowsPoints ) {
								mallButton(
									"item-mall-points",
									`${hudCopy( "UIIT_STT_SILKMALL_USE_POINT" )}: ${state.points}`,
									[ dx, dy + 124, dw, touch ],
									busy
								);
							}
							mallButton( "item-mall-purchase", hudCopy( "UIIT_STT_BUY" ), [
								dx,
								h - touch - margin,
								(dw - gap) / 2,
								touch
							], busy || !itemMall.purchase( game.itemMall ) );
							mallButton( "item-mall-cancel", hudCopy( "UIIT_CTL_CANCEL" ), [
								dx + (dw + gap) / 2,
								h - touch - margin,
								(dw - gap) / 2,
								touch
							], busy );
						} else if ( state.question ) {
							const question = state.question;
							const quote = mallQuestionLayout( question.kind, question.offers.length );
							mallText( hudCopy( quote.caption ), [ dx, dy, dw, 32 ] );
							mallText(
								question.offers.length === 1 ?
									hudCopy( question.offers[0]!.name ) :
									`${question.offers.length} items`,
								[ dx, dy + 36, dw, 32 ]
							);
							if ( question.kind === "basket" || question.kind === "worn" ) {
								const silk = question.offers.reduce( ( sum, offer ) => sum + offer.silk, 0 ) -
									state.points;
								const gift = question.offers.reduce( ( sum, offer ) => sum + offer.giftSilk, 0 );
								mallText( `Silk ${silk}   Gift ${gift}`, [ dx, dy + 72, dw, 32 ] );
								if ( state.pointLimit > 0 ) {
									mallButton(
										"item-mall-points",
										`${hudCopy( "UIIT_STT_SILKMALL_USE_POINT" )}: ${state.points}`,
										[ dx, dy + 108, dw, touch ],
										busy
									);
								}
							}
							mallButton( "item-mall-question-confirm", hudCopy( quote.confirm ), [
								dx,
								h - touch - margin,
								(dw - gap) / 2,
								touch
							], busy || !state.questionReady );
							mallButton( "item-mall-question-cancel", hudCopy( "UIIT_CTL_CANCEL" ), [
								dx + (dw + gap) / 2,
								h - touch - margin,
								(dw - gap) / 2,
								touch
							] );
						}
					} else {
						const actionWidth = (contentWidth - gap * 2) / 3;
						mallButton( "item-mall-home", "Categories", [ margin, 80, actionWidth, touch ] );
						mallButton( "item-mall-view:bag", "Bag", [
							margin + actionWidth + gap,
							80,
							actionWidth,
							touch
						] );
						mallButton( "item-mall-view:preview", "Preview", [
							margin + 2 * (actionWidth + gap),
							80,
							actionWidth,
							touch
						] );
						if ( state.compactView === "shop" && state.category === -1 ) {
							const menuWidth = contentWidth;
							const categoryWidth = (menuWidth - gap) / 2;
							mallCategories().forEach( ( category, index ) => {
								const disabled = category.key !== "basket" &&
									!game.itemMall?.tabs.some( tab => tab.category === category.key );
								const node = mallMenuControl( mallControl( root, 8 ), index, false, disabled );
								const r: UiRect = [
									margin + index % 2 * (categoryWidth + gap),
									124 + Math.floor( index / 2 ) * (touch + gap),
									categoryWidth,
									touch
								];
								image( r, node.texture );
								const icon = mallCategoryIcon( node, index, disabled );
								image( [ r[0] + 6, r[1] + 10, 20, 20 ], icon.texture );
								mallText( hudCopy( node.text ), [ r[0] + 30, r[1] + 4, r[2] - 34, 32 ] );
								controls.push( {
									id: node.name,
									label: hudCopy( node.text ),
									kind: "button",
									rect: r,
									disabled
								} );
							} );
						} else if ( state.compactView === "bag" ) {
							const bag = compactMallBagLayout( {
								width: w,
								height: h,
								total: game.inventorySlotCount ?? 0,
								equipment: game.equipmentSlotCount ?? 13,
								requestedPage: state.compactBagPage
							} );
							const lattice: UiRect = [ bag.left, bag.top, bag.columns * 36, bag.rows * 36 ];
							authoredChrome(
								{
									...mallControl( data.windows.ifitemmallinventory!, 2 ),
									rect: [ lattice[0] - 3, lattice[1] - 3, lattice[2] + 6, lattice[3] + 6 ],
									text: ""
								},
								0,
								0
							);
							authoredChrome(
								{ ...mallControl( data.windows.ifitemmallinventory!, 1 ), rect: lattice, text: "" },
								0,
								0
							);
							for ( const cell of bag.slots ) {
								const item = game.inventory.find( item => item.slot === cell.slot );
								const path = cell.enabled ? item && iconPath( item.icon ) : data.popupArt.blocked;
								if ( path ) image( cell.rect, path );
								if ( cell.enabled && item ) {
									equipmentOverlay( item, cell.rect );
									itemEffects( "item-mall-slot:" + cell.slot, item, cell.rect );
									itemCount( item, cell.rect );
									controls.push( {
										id: "item-mall-slot:" + cell.slot,
										label: item.name ?? "",
										kind: "button",
										rect: cell.rect
									} );
								}
							}
							mallButton( "item-mall-bag-slice:" + (bag.page - 1), "< Bag", [
								margin,
								h - touch - margin,
								90,
								touch
							], bag.page <= 0 );
							mallButton( "item-mall-bag-slice:" + (bag.page + 1), "Bag >", [
								w - margin - 90,
								h - touch - margin,
								90,
								touch
							], bag.page + 1 >= bag.pages );
						} else if ( state.compactView === "preview" ) {
							const previewRect: UiRect = [
								margin + 8,
								128,
								contentWidth - 16,
								Math.max( 1, h - 128 - touch - margin - gap )
							];
							authoredChrome(
								{
									...mallControl( root, 2 ),
									rect: [ margin, 124, contentWidth, h - 124 - touch - margin ],
									text: ""
								},
								0,
								0
							);
							if ( game.localGid && previewRect[2] > 0 && previewRect[3] > 0 ) {
								quads.push( {
									doll: { gid: state.previewGid ?? game.localGid, yaw: 0 },
									texture: "__doll",
									rect: previewRect,
									uv: [ 0, 0, 1, 1 ],
									color: white,
									clip: full
								} );
							}
							mallButton( "item-mall-root:4", "Buy worn", [
								margin,
								h - touch - margin,
								(contentWidth - gap) / 2,
								touch
							], !state.worn.length || busy );
							mallButton( "item-mall-root:5", "Take off", [
								margin + (contentWidth + gap) / 2,
								h - touch - margin,
								(contentWidth - gap) / 2,
								touch
							], !state.worn.length || busy );
						} else {
							const tabWidth = (contentWidth - gap * (tabColumns - 1)) / tabColumns;
							state.tabs.forEach( ( tab, index ) => {
								const node = mallTabControl( mallControl( root, 8 ), index, index === state.tab );
								const r: UiRect = [
									margin + index % tabColumns * (tabWidth + gap),
									124 + Math.floor( index / tabColumns ) * tabPitch,
									tabWidth,
									tabPitch
								];
								image( r, node.texture );
								mallText( hudCopy( tab.label ), [ r[0] + 4, r[1] + 6, r[2] - 8, 18 ] );
								controls.push( {
									id: node.name,
									label: hudCopy( tab.label ),
									kind: "button",
									rect: r,
									selected: index === state.tab
								} );
							} );
							const cardWidth = (contentWidth - gap * (columns - 1)) / columns;
							state.offers.forEach( ( offer, index ) => {
								const x = margin + index % columns * (cardWidth + gap),
									y = productTop + Math.floor( index / columns ) * cardHeight;
								const rowArt = data.windows.ifitemmallshopslot!;
								image( [ x, y, cardWidth, cardHeight - gap ], mallControl( rowArt, 50 ).texture );
								image( [ x + 40, y + 4, cardWidth - 44, 36 ], mallControl( rowArt, 2 ).texture );
								const icon = iconPath( offer.icon );
								if ( icon ) image( [ x + 4, y + 4, 32, 32 ], icon );
								controls.push( {
									id: "item-mall-offer:" + offer.packageId,
									label: hudCopy( offer.name ),
									kind: "button",
									rect: [ x + 4, y + 4, 32, 32 ]
								} );
								mallText( hudCopy( offer.name ), [ x + 40, y + 4, cardWidth - 44, 36 ] );
								mallText( `Silk ${offer.silk}   Gift ${offer.giftSilk}`, [
									x + 4,
									y + 40,
									cardWidth - 8,
									24
								] );
								const bw = (cardWidth - 16) / 3;
								mallButton( "item-mall-buy:" + index, hudCopy( "UIIT_STT_BUY" ), [
									x + 4,
									y + 64,
									bw,
									touch
								], busy );
								mallButton(
									"item-mall-wear:" + index,
									"Wear",
									[ x + 8 + bw, y + 64, bw, touch ],
									busy || !game.itemMall || !itemMall.wearEnabled( offer, game.itemMall )
								);
								mallButton( "item-mall-reserve:" + index, state.category === 7 ? "Remove" : "Basket", [
									x + 12 + bw * 2,
									y + 64,
									bw,
									touch
								], busy );
							} );
							const pages = Math.ceil( state.count / state.pageSize ), footerY = h - touch - margin;
							mallButton(
								"item-mall-page:" + (state.page - 1),
								"<",
								[ margin, footerY, touch, touch ],
								state.page <= 0
							);
							mallButton( "item-mall-page:" + (state.page + 1), ">", [
								w - margin - touch,
								footerY,
								touch,
								touch
							], state.page + 1 >= pages );
							if ( state.category === 7 ) {
								mallButton( "item-mall-buy-all", "Buy basket", [
									margin + touch + gap,
									footerY,
									contentWidth - 2 * (touch + gap),
									touch
								], !state.count || busy );
							} else {mallText( `${state.page + 1} / ${Math.max( 1, pages )}`, [
									margin + touch + gap,
									footerY + 12,
									contentWidth - 2 * (touch + gap),
									20
								] );}
						}
					}
					const missing = paths.slice( mallAdmission[3] ).filter( path => !resources.has( path ) );
					windowMissing.push( ...missing );
					if ( !fontPath || !resources.has( fontPath ) || missing.length ) {
						quads.length = mallAdmission[0];
						controls = [];
					} else {
						const painted = resolveTextOverlaps( quads.slice( mallAdmission[0] ) );
						quads.length = mallAdmission[0];
						quads.push( ...painted );
					}
					mallButton( "item-mall-close", "Close", [ w - margin - 64, margin, 64, touch ] );
				} else {
					itemMall.setPageSize( 6 );
					state = itemMall.read( game.itemMall );
					nativeFrame( frame, ox, oy, hudCopy( frame.text ), "item-mall-close" );
					nativePage( root, ox, oy );
					// 6BC17E..6BC197 explicitly disable Buying List and Obtained List
					// through CIFButton_SetEnabled(false), even in the retail client.
					for ( const node of Object.values( root ) ) {
						if ( node.type !== "CIFButton" ) continue;
						// Control ids are unique: the frame X already owns "item-mall-close".
						const id = node.id === 8 ? "item-mall-close-button" : node.id === 3 ?
							"item-mall-home" :
							"item-mall-root:" + node.id;
						authoredLabeledButton(
							node,
							ox,
							oy,
							id,
							hudCopy( node.text ),
							node.id === 4 || node.id === 5 ?
								state.worn.length === 0 || state.batchPending || !!game.inventoryPending :
								node.id !== 8 && node.id !== 3
						);
					}
					for ( let index = 0; index < 8; index++ ) {
						const category = mallCategories()[index]!;
						const disabled = category.key !== "basket" &&
							!game.itemMall?.tabs.some( row => row.category === category.key );
						const node = mallMenuControl(
							mallControl( root, 8 ),
							index,
							state.category === index,
							disabled
						);
						authoredLabeledButton( node, ox, oy, node.name, hudCopy( node.text ), disabled );
						authoredImage( mallCategoryIcon( node, index, disabled ), ox, oy );
					}
					const infoOrigin = mallControl( root, 90 ).rect;
					const info = data.windows.ifitemmallmyinfo!;
					// CIFItemMallMyInfo_OnCreate (6BDA60) always hides the money sign
					// (5); on an English client (GameConfig +0x138 language 4, the
					// type.txt Language this port ships) it also hides the Point row
					// (17 box, 18 value, 19 "P", 22 caption).
					nativePage( info, ox + infoOrigin[0], oy + infoOrigin[1], [
						5,
						10,
						11,
						12,
						14,
						17,
						18,
						19,
						22
					] );
					// 6BDA60 disables Buy silk (7) and Coupon (9) only when their link
					// strings are shorter than 5; v1.150's open retail web pages.
					// Deliberate deviation, port-only, not native: there is no shop to
					// link to, so both stay disabled.
					for ( const id of [ 7, 9 ] ) {
						const node = mallControl( info, id );
						authoredLabeledButton(
							node,
							ox + infoOrigin[0],
							oy + infoOrigin[1],
							"item-mall-funding:" + id,
							hudCopy( node.text ),
							true
						);
					}
					if ( game.localGid ) {
						quads.push( {
							portraitGid: game.localGid,
							texture: "__portrait",
							rect: authoredRect( mallControl( info, 4 ), ox + infoOrigin[0], oy + infoOrigin[1] ),
							uv: [ 0, 0, 1, 1 ],
							color: white,
							clip: full
						}, {
							doll: { gid: state.previewGid ?? game.localGid, yaw: 0 },
							texture: "__doll",
							rect: (() => {
								const [x, y, width, height] = authoredRect( mallControl( root, 9 ), ox, oy );
								// 6BB5BA applies the native viewport insets before its 30-degree camera.
								return [ x + 2, y + 15, width - 2, height - 37 ];
							})(),
							uv: [ 0, 0, 1, 1 ],
							color: white,
							clip: full
						} );
					}
					for (
						const [id, value] of [
							[ 10, next.session?.character ],
							[ 11, game.progression?.level ],
							[ 12, game.itemMall?.silk ],
							[ 14, game.itemMall?.giftSilk ]
						] as const
					) {
						authoredText(
							mallControl( info, id ),
							ox + infoOrigin[0],
							oy + infoOrigin[1],
							String( value ?? "" )
						);
					}
					const trunkOrigin = mallControl( root, 102 ).rect;
					const tx = ox + trunkOrigin[0], ty = oy + trunkOrigin[1];
					nativePage( data.windows.ifitemmalltrunk!, tx, ty );
					nativePage( data.windows.ifitemmallinventory!, tx, ty );
					// 6CD535 creates 32x32 cells at (17,19), using the inventory's
					// ordinary 32-slot pages and 36-pixel pitch. Share slot admission.
					const mallBag = inventorySlots(
						tx - 1,
						ty + 6,
						game.inventorySlotCount ?? 0,
						game.equipmentSlotCount ?? 13,
						state.bagPage
					);
					for ( const cell of mallBag.slots ) {
						const item = game.inventory.find( row => row.slot === cell.slot );
						const path = cell.enabled ? item && iconPath( item.icon ) : data.popupArt.blocked;
						if ( path ) image( cell.rect, path );
						if ( cell.enabled && item ) {
							equipmentOverlay( item, cell.rect );
							itemEffects( "item-mall-slot:" + cell.slot, item, cell.rect );
							itemCount( item, cell.rect );
							controls.push( {
								id: "item-mall-slot:" + cell.slot,
								label: item.name ?? "",
								rect: cell.rect,
								kind: "button"
							} );
						}
					}
					const selector = mallControl( data.windows.ifitemmallinventory!, 12 );
					const expansion = data.windows.ifitemmalltrunkexpbar!;
					for ( let index = 0; index < 3; index++ ) {
						const position = mallControl( data.windows.ifitemmallinventory!, 51 + index ).rect;
						const count = Math.max(
							0,
							Math.min(
								32,
								(game.inventorySlotCount ?? 0) -
									(game.equipmentSlotCount ?? 13) - 32 * index
							)
						);
						const ex = tx + position[0], ey = ty + position[1];
						nativePage( expansion, ex, ey );
						authoredImage(
							{
								...mallControl( expansion, 2 ),
								texture: ROOT + "interface/mall/mall_inven_icon" + (count > 0 ? "" : "_disable") +
									".png"
							},
							ex,
							ey
						);
						authoredText(
							mallControl( expansion, 3 ),
							ex,
							ey,
							count > 0 ?
								hudCopy( "UIIT_STT_SILKMALL_REMAIN_INVENTORY" ).replace( "%d", String( count ) ) :
								hudCopy( "UIIT_STT_NONE" )
						);
					}
					const spin = data.windows.ifspincontrol!;
					const spinX = tx + selector.rect[0], spinY = ty + selector.rect[1];
					authoredText( mallControl( spin, 0 ), spinX, spinY, String( mallBag.page + 1 ) );
					for ( const [id, delta] of [ [ 1, -1 ], [ 2, 1 ] ] as const ) {
						const page = mallBag.page + delta;
						authoredButton(
							mallControl( spin, id ),
							spinX,
							spinY,
							"item-mall-bag-page:" + page,
							"",
							page < 0 || page >= mallBag.pages
						);
					}
					const shopOrigin = mallControl( root, 50 ).rect;
					const shop = data.windows.ifitemmallshop!;
					for ( const node of authoredPaintOrder( shop ) ) {
						if ( node.creationSection === 0 || state.category === -1 && node.creationSection === 2 ) {
							authoredChrome( node, ox + shopOrigin[0], oy + shopOrigin[1] );
						}
					}

					const sx = ox + shopOrigin[0], sy = oy + shopOrigin[1];
					const currentTab = state.tabs[state.tab];
					const description = state.category === -1 ?
						"UIIT_STT_SILKMALL_MAIN_INTRO" :
						state.category === 7 ?
						"UIIT_STT_SILKMALL_MAIN_ZZIM_SUB_TITLE_ZZIM" :
						currentTab ?
						mallDescription( currentTab.category, currentTab.tab ) :
						"";
					authoredChrome( { ...mallControl( shop, 41 ), text: description }, sx, sy );
					if ( state.category === -1 ) {
						authoredChrome(
							{ ...mallControl( shop, 53 ), text: "UIIT_STT_SILKMALL_MAIN_EXPLAIN" },
							sx,
							sy
						);
					}
					if ( state.category === 7 ) {
						const tab = mallTabControl( mallControl( root, 8 ), 0, true );
						authoredLabeledButton( tab, sx, sy, tab.name, hudCopy( "UIIT_STT_SILKMALL_ZZIM" ) );
						const buyAll = mallControl( shop, 42 );
						authoredLabeledButton(
							buyAll,
							sx,
							sy,
							"item-mall-buy-all",
							hudCopy( buyAll.text ),
							state.count === 0 || state.batchPending || !!game.inventoryPending
						);
					}
					if ( state.category >= 0 ) {
						for ( let index = 0; index < state.tabs.length; index++ ) {
							const tab = state.tabs[index]!;
							const control = mallTabControl( mallControl( root, 8 ), index, index === state.tab );
							authoredLabeledButton( control, sx, sy, control.name, hudCopy( tab.label ) );
						}
						const manager = mallPageLayout( mallControl( shop, 74 ).rect, state.page, state.count );
						if ( manager.count > 0 ) {
							const template = {
								...mallControl( data.windows.ifpagemanager!, 10 ),
								client: [ 0, 0, 0, 0 ] as UiRect,
								hAlign: 1,
								vAlign: 1
							};
							authoredText(
								{ ...template, rect: [ manager.x - 4, manager.y, 4, manager.height ] },
								sx,
								sy,
								"["
							);
							authoredText(
								{
									...template,
									rect: [ manager.x + manager.count * manager.width, manager.y, 4, manager.height ]
								},
								sx,
								sy,
								"]"
							);
							for ( let index = 0; index < manager.count; index++ ) {
								const page = manager.first + index;
								const control = {
									...template,
									rect: [
										manager.x + index * manager.width,
										manager.y,
										manager.width,
										manager.height
									] as UiRect
								};
								authoredText( control, sx, sy, String( page + 1 ) );
								controls.push( {
									id: "item-mall-page:" + page,
									label: String( page + 1 ),
									rect: authoredRect( control, sx, sy ),
									kind: "button"
								} );
							}
							if ( manager.previous >= 0 ) {
								const node = mallControl( data.windows.ifpagemanager!, 1 );
								authoredButton(
									{ ...node, rect: [ manager.left - node.size[0], manager.y, 0, 0 ] },
									sx,
									sy,
									"item-mall-page:" + manager.previous,
									""
								);
							}
							if ( manager.next < manager.pages ) {
								const node = mallControl( data.windows.ifpagemanager!, 2 );
								authoredButton(
									{ ...node, rect: [ manager.right, manager.y, 0, 0 ] },
									sx,
									sy,
									"item-mall-page:" + manager.next,
									""
								);
							}
						}

						const row = data.windows.ifitemmallshopslot!;
						for ( let index = 0; index < MALL_ROWS_PER_PAGE; index++ ) {
							const offer = state.offers[index];
							const origin = mallControl( shop, 61 + index ).rect;
							const rx = sx + origin[0], ry = sy + origin[1];
							if ( !offer ) {
								// CIFItemMallShopSlot_ApplyVisibilityAndButtonStates (6C9420):
								// an empty slot hides its bar, name, price, silk mark, icon and
								// buttons, but the slot and its background (50) stay.
								authoredChrome( mallControl( row, 50 ), rx, ry );
								continue;
							}
							nativePage( row, rx, ry, [ 8, 10, 11 ] );
							authoredText( mallControl( row, 11 ), rx, ry, hudCopy( offer.name ) );
							authoredText( mallControl( row, 10 ), rx, ry, String( offer.silk ) );
							authoredText( mallControl( row, 8 ), rx, ry, hudCopy( "UIIT_STT_SILKMALL_SILK" ) );
							const icon = iconPath( offer.icon );
							if ( icon ) authoredImage( { ...mallControl( row, 1 ), texture: icon }, rx, ry );
							controls.push( {
								id: "item-mall-offer:" + offer.packageId,
								label: hudCopy( offer.name ),
								rect: authoredRect( mallControl( row, 1 ), rx, ry ),
								kind: "button"
							} );
							for ( const node of Object.values( row ) ) {
								if ( node.type !== "CIFButton" ) continue;
								const action = node.id === 3 ? "item-mall-wear:" + index : node.id === 4 ?
									"item-mall-buy:" + index :
									node.id === 6 ?
									"item-mall-reserve:" + index :
									"item-mall-row:" + index + ":" + node.id;
								authoredLabeledButton(
									node,
									rx,
									ry,
									action,
									hudCopy(
										node.id === 6 && state.category === 7 ? "UIIT_STT_SILKMALL_DEL" : node.text
									),
									(node.id === 3 ?
										!game.itemMall || !itemMall.wearEnabled( offer, game.itemMall ) :
										node.id !== 4 && node.id !== 6) || !!game.inventoryPending || state.batchPending
								);
							}
						}
					}
					if ( state.selected && game.itemMall ) {
						const page = data.windows.ifitemmallconfirmbuy!;
						const row = data.windows.ifitemmallconfirmslot!;
						const currencyLayout = mallCurrencyRows( state.selected, state.quantity, state.points );
						const currencies = currencyLayout.rows;
						const dialogWidth = 327, height = currencyLayout.height;
						const mx = Math.floor( (w - dialogWidth) / 2 ), my = Math.floor( (h - height) / 2 );
						controls = [];
						const prefix = ROOT + "interface/messagebox/msgbox2_window_";
						paths.push( ...PARTS.map( part => prefix + part + ".png" ) );
						quads.push(
							...frameRing(
								[ mx, my, dialogWidth, height ],
								prefix,
								PARTS.map( part => resources.size( prefix + part + ".png" ) ),
								full
							)
						);
						const background = mallControl( page, 4 );
						authoredChrome(
							{
								...background,
								rect: [
									background.rect[0],
									background.rect[1],
									background.rect[2],
									background.rect[3] + currencyLayout.growth
								]
							},
							mx,
							my
						);
						// The fill belongs behind the authored controls.
						for ( const node of authoredPaintOrder( page ) ) {
							if ( [ 4, 11, 40, 42, 50, 51, 61, 62 ].includes( node.id ) ) continue;
							authoredChrome( node, mx, my );
							if ( node.text ) authoredText( node, mx, my, hudCopy( node.text ) );
						}
						authoredText( mallControl( page, 40 ), mx, my, hudCopy( state.selected.name ) );
						authoredText( mallControl( page, 42 ), mx, my, String( state.quantity ) );
						const icon = iconPath( state.selected.icon );
						if ( icon ) authoredImage( { ...mallControl( page, 11 ), texture: icon }, mx, my );
						for ( let index = 0; index < currencies.length; index++ ) {
							const currency = currencies[index]!, cy = my + currency.y;
							nativePage( row, mx, cy, [ 3, 4, 5, 10 ] );
							authoredText( mallControl( row, 3 ), mx, cy, String( currency.amount ) );
							authoredText( mallControl( row, 4 ), mx, cy, hudCopy( currency.label ) );
						}
						controls.push( {
							id: "item-mall-quantity",
							label: hudCopy( "UIIT_STT_AMOUNT" ),
							rect: authoredRect( mallControl( page, 42 ), mx, my ),
							kind: "text",
							value: String( state.quantity ),
							maxLength: 5,
							disabled: game.inventoryPending
						} );
						if ( state.selected.allowsPoints ) {
							authoredLabeledButton(
								mallControl( row, 10 ),
								mx,
								my + currencies.find( row => row.points )!.y,
								"item-mall-points",
								hudCopy( "UIIT_STT_SILKMALL_USE_POINT" ),
								game.inventoryPending
							);
						}

						for (
							const [id, name, delta] of [ [ 50, "item-mall-quantity-up", 1 ], [
								51,
								"item-mall-quantity-down",
								-1
							] ] as const
						) {
							authoredButton(
								mallControl( page, id ),
								mx,
								my,
								name,
								"",
								game.inventoryPending || state.quantity + delta < 1 ||
									state.quantity + delta > state.selected.purchaseLimit
							);
						}
						authoredLabeledButton(
							{ ...mallControl( page, 61 ), rect: [ 82, height - 40, 0, 0 ] },
							mx,
							my,
							"item-mall-purchase",
							hudCopy( "UIIT_STT_BUY" ),
							game.inventoryPending || !itemMall.purchase( game.itemMall )
						);
						authoredLabeledButton(
							{ ...mallControl( page, 62 ), rect: [ 170, height - 40, 0, 0 ] },
							mx,
							my,
							"item-mall-cancel",
							hudCopy( "UIIT_CTL_CANCEL" ),
							game.inventoryPending
						);
					}
					if ( state.question && game.itemMall ) {
						const question = state.question;
						const layout = mallQuestionLayout( question.kind, question.offers.length );
						const mx = Math.floor( (w - layout.width) / 2 ), my = Math.floor( (h - layout.height) / 2 );
						const page = data.windows.ifmessagebox!;
						const store = Object.fromEntries(
							Object.entries( page ).filter( ( [, node] ) => node.creationSection === 1 )
						);
						const template = mallControl( data.windows.ifitemmallconfirmbuy!, 5 );
						controls = [];
						paths.push( ...partyProposalAssets() );
						quads.push(
							...normalTile(
								[ mx + 16, my + 40, layout.width - 32, layout.height - 56 ],
								MESSAGE_TILE,
								resources.size( MESSAGE_TILE ),
								full
							),
							...frameRing(
								[ mx, my, layout.width, layout.height ],
								MESSAGE_FRAME,
								PARTS.map( part => resources.size( MESSAGE_FRAME + part + ".png" ) ),
								full
							)
						);
						authoredText(
							{ ...template, rect: [ 16, 12, layout.width - 32, 14 ] },
							mx,
							my,
							hudCopy( layout.caption )
						);
						authoredChrome(
							{ ...template, type: "CIFPML", rect: [ ...layout.messageRect ], text: layout.message },
							mx,
							my
						);
						if ( question.kind === "reserve" || question.kind === "remove" ) {
							const offer = question.offers[0]!;
							authoredImage( mallControl( store, 10 ), mx, my );
							authoredImage( mallControl( store, 1 ), mx, my );
							authoredText( mallControl( store, 1 ), mx, my, hudCopy( offer.name ) );
							const icon = iconPath( offer.icon );
							if ( icon ) authoredImage( { ...mallControl( store, 12 ), texture: icon }, mx, my );
						} else {
							const total = question.offers.reduce( ( sum, offer ) => sum + offer.silk, 0 ) -
								state.points;
							if ( question.kind === "worn" ) {
								for ( let index = 0; index < question.offers.length; index++ ) {
									const offer = question.offers[index]!,
										y = layout.rowStart + layout.rowPitch * index;
									authoredText(
										{ ...template, rect: [ 70, y, 100, 16 ], hAlign: 2 },
										mx,
										my,
										hudCopy( offer.name )
									);
									authoredText(
										{ ...template, rect: [ 165, y, 40, 16 ], hAlign: 2 },
										mx,
										my,
										`${offer.silk} ${hudCopy( "UIIT_STT_ROLL_OF_CLOTH" )}`
									);
								}
							}
							authoredText(
								{
									...mallControl( store, 8 ),
									rect: [ question.kind === "basket" ? 91 : 121, layout.totalY, 50, 14 ],
									hAlign: 2
								},
								mx,
								my,
								hudCopy( "UIIT_STT_PRICE" )
							);
							authoredText(
								{
									...mallControl( store, 2 ),
									color: tooltipColor( 0xffffcc26 ),
									rect: [
										question.kind === "basket" ? 143 : 166,
										layout.totalY,
										question.kind === "basket" ? 40 : 28,
										14
									],
									hAlign: 2
								},
								mx,
								my,
								String( total )
							);
							authoredText(
								{
									...mallControl( store, 7 ),
									rect: [ question.kind === "basket" ? 183 : 188, layout.totalY, 26, 14 ]
								},
								mx,
								my,
								hudCopy( "UIIT_STT_SILKMALL_SILK" )
							);
							if ( state.pointLimit > 0 ) {
								for ( const id of [ 50, 51 ] ) {
									const node = mallControl( store, id );
									authoredText(
										{ ...node, rect: [ node.rect[0], layout.pointY, node.rect[2], node.rect[3] ] },
										mx,
										my,
										id === 51 ? String( state.points ) : hudCopy( node.text )
									);
								}
								const button = mallControl( store, 52 );
								authoredLabeledButton(
									{
										...button,
										rect: [ button.rect[0], layout.pointY, button.rect[2], button.rect[3] ]
									},
									mx,
									my,
									"item-mall-points",
									hudCopy( button.text ),
									!!game.inventoryPending
								);
							}
						}
						authoredLabeledButton(
							{ ...mallControl( store, 215 ), rect: [ layout.buttonX, layout.buttonY, 76, 22 ] },
							mx,
							my,
							"item-mall-question-confirm",
							hudCopy( layout.confirm ),
							!!game.inventoryPending || !state.questionReady
						);
						authoredLabeledButton(
							{ ...mallControl( store, 216 ), rect: [ layout.buttonX + 80, layout.buttonY, 76, 22 ] },
							mx,
							my,
							"item-mall-question-cancel",
							hudCopy( "UIIT_CTL_CANCEL" )
						);
					}
					if ( state.pointDialog && game.itemMall ) {
						const prefix = ROOT + "interface/messagebox/msgbox2_window_";
						const pointPage = data.windows.ifitemmallusepoint!;
						const pointWidth = 233, pointHeight = 177;
						const px = Math.floor( (w - pointWidth) / 2 ), py = Math.floor( (h - pointHeight) / 2 );
						controls = [];
						quads.push(
							...frameRing(
								[ px, py, pointWidth, pointHeight ],
								prefix,
								PARTS.map( part => resources.size( prefix + part + ".png" ) ),
								full
							)
						);
						nativePage( pointPage, px, py, [ 40, 41 ] );
						authoredText( mallControl( pointPage, 40 ), px, py, String( game.itemMall.points ) );
						authoredText( mallControl( pointPage, 41 ), px, py, String( state.pointDraft ) );
						controls.push( {
							id: "item-mall-point-value",
							label: hudCopy( "UIIT_STT_SILKMALL_P_POINT" ),
							rect: authoredRect( mallControl( pointPage, 41 ), px, py ),
							kind: "text",
							value: String( state.pointDraft ),
							maxLength: 10,
							disabled: game.inventoryPending
						} );
						authoredLabeledButton(
							mallControl( pointPage, 50 ),
							px,
							py,
							"item-mall-points-apply",
							hudCopy( mallControl( pointPage, 50 ).text ),
							game.inventoryPending
						);
					}
					endWindow( mallAdmission, "item-mall" );
				}
			}
			if ( worldVisible && game && withdrawal.confirming() && hud.data() ) {
				const state = withdrawal.read( game, hud.data()?.masteryCosts ?? {}, hud.data()?.withdrawalGoldPrices );
				const page = hud.data()!.windows.ifskillremovalbox!, row = state.choice;
				const [width, height] = state.resuscitation ? RESUSCITATION_CONFIRM_SIZE : WITHDRAWAL_CONFIRM_SIZE;
				const fillHeight = state.resuscitation ?
					RESUSCITATION_CONFIRM_FILL_HEIGHT :
					WITHDRAWAL_CONFIRM_FILL_HEIGHT;
				const buttonY = state.resuscitation ? RESUSCITATION_CONFIRM_BUTTON_Y : WITHDRAWAL_CONFIRM_BUTTON_Y;
				const px = Math.floor( (w - width) / 2 ), py = Math.floor( (h - height) / 2 );
				const prefix = ROOT + "interface/messagebox/msgbox2_window_";
				controls = [];
				const admission = beginWindow();
				blocks.push( full );
				paths.push( ...PARTS.map( part => prefix + part + ".png" ) );
				quads.push(
					...frameRing(
						[ px, py, width, height ],
						prefix,
						PARTS.map( part => resources.size( prefix + part + ".png" ) ),
						full
					)
				);
				for ( const node of authoredPaintOrder( page ) ) {
					// Mode 3 hides native controls 12/13 and 32..35: gold has no role.
					if (
						node.type === "CIFButton" ||
						!state.resuscitation && (node.id === 12 || node.id === 13 || node.id >= 32 && node.id <= 35)
					) continue;
					if ( node.id === 5 ) {
						authoredChrome(
							{
								...node,
								rect: [ node.rect[0], node.rect[1], node.rect[2], fillHeight ]
							},
							px,
							py
						);
					} else authoredChrome( node, px, py );
					if ( node.text ) authoredText( node, px, py, hudCopy( node.text ) );
				}
				if ( row ) {
					const mastery = row.kind === "mastery-withdraw" ?
						hud.data()!.skillUi.masteries.find( m => m.id === row.id ) :
						undefined;
					const skill = skillMetadataById( game, row.id );
					const caption = row.kind === "mastery-withdraw" ?
						"UIIT_STT_CIRCULATION_WITHDRAW_MASTERY_WND" :
						"UIIT_STT_CIRCULATION_WITHDRAW_SKILL_WND";
					quads.push(
						...text.quads(
							hudCopy( caption ),
							messageBox( w, h, width, height, [ px, py ] ).title,
							full,
							white,
							{ hAlign: 1, vAlign: 0 }
						)
					);
					authoredText(
						page.GDR_SKLRB_SKILLNAME!,
						px,
						py,
						mastery ? hudCopy( mastery.name ) : localization.text( row.nameSymbol, row.name )
					);
					authoredText( page.GDR_SKLRB_CURRENTLEVEL!, px, py, `Lv ${row.level}` );
					authoredText( page.GDR_SKLRB_TARGETLEVEL!, px, py, `Lv ${row.rank}` );
					authoredText( page.GDR_SKLRB_WITHDRAWED_LEVEL!, px, py, String( state.amount ) );
					authoredText( page.GDR_SKLRB_TOTALPOINT!, px, py, String( row.refund ) );
					if ( state.resuscitation ) {
						authoredText(
							page.GDR_SKLRB_CURRENTMONEY!,
							px,
							py,
							BigInt( game.progression?.gold ?? "0" ).toLocaleString( "en-US" )
						);
						authoredText( page.GDR_SKLRB_NEEDMONEY!, px, py, state.gold.toLocaleString( "en-US" ) );
					}
					authoredText(
						page.GDR_SKLRB_WITHDRAW_POTION!,
						px,
						py,
						`${state.potion?.name ?? ""} ${state.quantity} ${hudCopy( "UIIT_STT_UNIT" )}`
					);
					const icon = iconPath( mastery?.icon ?? skill?.icon ), potionIcon = iconPath( state.potion?.icon );
					if ( icon ) image( authoredRect( page.GDR_SKLRB_SKILLICON!, px, py ), icon );
					if ( potionIcon ) image( authoredRect( page.GDR_SKLRB_WITHDRAWICON!, px, py ), potionIcon );
				}
				authoredButton(
					page.GDR_SKLRB_BTN_DOWNGRADE!,
					px,
					py,
					"withdrawal-decrease",
					hudCopy( "UIIT_STT_CIRCULATION_WITHDRAW_LEV" ),
					state.amount >= state.maximum
				);
				authoredButton(
					page.GDR_SKLRB_BTN_RECOVER!,
					px,
					py,
					"withdrawal-recover",
					hudCopy( "UIIT_STT_CIRCULATION_CANCEL_WITHDRAW" ),
					state.amount === 0
				);
				for (
					const [node, id] of [ [ page.GDR_SKLRB_BTN_OK!, "withdrawal-confirm" ], [
						page.GDR_SKLRB_BTN_CANCEL!,
						"withdrawal-cancel"
					] ] as const
				) {
					authoredLabeledButton(
						{ ...node, rect: [ node.rect[0], buttonY, node.rect[2], node.rect[3] ] },
						px,
						py,
						id,
						hudCopy( node.text ),
						id === "withdrawal-confirm" && !state.command
					);
				}
				endWindow( admission, "withdrawal-confirm" );
			}
			if ( worldVisible && carriedShortcut && game ) {
				const binding = quickSlotDrag( carriedShortcut.id, 0, game ),
					skill = binding?.kind === 0x49 ?
						skillMetadataById( game, binding.payload ) :
						undefined,
					action = binding?.kind === 0x4a ?
						hud.data()?.actions.find( r => r.id === (binding.payload & 0xffffff) ) :
						undefined,
					item = binding ? game.inventory.find( r => r.slot === quickSlotItemSlot( binding ) ) : undefined,
					icon = iconPath( skill?.icon ?? action?.icon ?? item?.icon );
				if ( icon ) {
					paths.push( icon );
					if ( resources.has( icon ) ) {
						rect( [ carriedShortcut.x - 16, carriedShortcut.y - 16, 32, 32 ], white, icon );
					}
				}
			}
			if ( worldVisible ) {
				paths.push(
					ROOT + "interface/ifcommon/com_tooltip_corner.png",
					ROOT + "interface/ifcommon/com_tooltip_edge.png"
				);
				const control = controls.find( c => c.id === hover ),
					key = control && hudTooltipKey( control.id, avatarView, blockTab ),
					value = control?.helpText ??
						(control &&
								(control.id.startsWith( "buff:" ) || control.id.startsWith( "abnormal:" ) ||
									control.id.startsWith( "party-buff:" ) || control.id.startsWith( "target-buff:" )) ?
							control.label :
							key && hudCopy( key ));
				const hudData = hud.data();
				let tooltip: readonly TooltipRow[] = value ? [ { value, color: 0xffffffff } ] : [];
				if (
					control && game && hudData && phase === "world" && !carriedShortcut && !carriedItem && !pressed &&
					!practice
				) {
					/*
					================
					lookup
					================
					*/
					const id = control.id,
						lookup = ( symbol: string ) =>
							!symbol || symbol === "xxx" ?
								"" :
								localization.text( symbol, "" ) || hudData.strings[symbol] || "";
					const items = tooltipItems( id, game, cosGid ), item = items[0];
					if ( id.startsWith( "item-mall-offer:" ) ) {
						const offer = game.itemMall?.offers.find( row =>
							row.packageId === Number( id.slice( "item-mall-offer:".length ) )
						);
						if ( offer ) {
							tooltip = [
								{ value: lookup( offer.name ), color: 0xffffffff, heading: true },
								...tooltipDescription( lookup( offer.description ) )
							];
						}
					}
					if ( control.helpSource ) {
						tooltip = buffTooltip(
							control.helpSource,
							game,
							next.simulationTimeMs ?? 0,
							hudData.tooltipSkills,
							lookup
						);
					}
					if ( id.startsWith( "mastery-info:" ) && game.progression ) {
						const mastery = hudData.tooltipMasteries.get( Number( id.slice( 13 ) ) );
						if ( mastery ) {
							tooltip = masteryTooltip( mastery, game.progression, hudData.masteryCosts, lookup );
						}
					}
					const action = actionTooltipKey(
						id,
						game,
						hudData.actions,
						next.entities.find( e => e.gid === game.localGid )?.movementMode
					);
					if ( action ) tooltip = [ { value: lookup( action ), color: 0xffffffff } ];
					let skill = id.startsWith( "skill:" ) ? Number( id.slice( 6 ) ) : 0;
					if ( id.startsWith( "hotbar:" ) ) {
						const binding = game.quickSlots?.find( row => row.slot === Number( id.slice( 7 ) ) );
						if ( binding?.kind === 0x49 ) skill = binding.payload;
					}
					if ( item || skill ) {
						const country = game.guide?.country,
							gender = next.session?.characters?.find( row => row.name === next.session?.character )
								?.gender,
							sex = gender === 0 ? 1 : gender === 1 ? 0 : undefined;
						if (
							tooltipMemo && tooltipMemo.item === item && tooltipMemo.skill === skill &&
							tooltipMemo.progression === game.progression && tooltipMemo.learned === game.skills &&
							tooltipMemo.catalog === hudData.tooltipSkills && tooltipMemo.country === country &&
							tooltipMemo.sex === sex
						) tooltip = tooltipMemo.rows;
						else {
							tooltip = item ?
								itemTooltip( item, game.progression ?? { masteries: [] }, lookup, { country, sex } ) :
								skillTooltip(
									{
										id: skill,
										catalog: hudData.tooltipSkills,
										learned: game.skills ?? [],
										progression: game.progression ?? { masteries: [] }
									},
									lookup,
									id => lookup( hudData.skillUi.masteries.find( m => m.id === id )?.name ?? "" )
								);
							tooltipMemo = {
								item,
								skill,
								progression: game.progression,
								learned: game.skills,
								catalog: hudData.tooltipSkills,
								country,
								sex,
								rows: tooltip
							};
						}
						if ( items.length > 1 ) {
							tooltip = items.flatMap( item =>
								itemTooltip( item, game.progression ?? { masteries: [] }, lookup, { country, sex } )
							);
						}
						// Price belongs to the current offer, outside the item-property memo.
						// A package gets one total even when several item details precede it.
						if ( item ) {
							tooltip = [
								...tooltip,
								...commerceTooltip( id, game, {
									price: lookup( "UIIT_STT_PRICE" ),
									gold: lookup( "UIIT_STT_GOLD" ),
									honor: lookup( "UIIT_STT_TC_HONOR_POINT" ),
									point: lookup( "UIIT_STT_SILKMALL_P_POINT" )
								} )
							];
						}
					}
				}
				if (
					control && tooltip.length && phase === "world" && !carriedShortcut && !carriedItem && !pressed &&
					!practice
				) {
					const bubble = tooltipBubble(
						tooltip,
						control.rect,
						full,
						( s, strong ) => text.run( s, strong ? 2 : 0 ).width
					);
					paths.push( ...bubble.paths );
					if ( bubble.paths.every( resources.has ) ) {
						quads.push( ...bubble.quads );
						for ( const row of bubble.lines ) {
							quads.push(
								...text.quads( row.value, row.rect, full, tooltipColor( row.color ), {
									fontStyle: row.strong ? 2 : 0
								} )
							);
						}
					}
				}
			}
			if ( worldVisible && consolePhase !== 0 && game?.eligibility?.gm ) {
				const r: UiRect = [ 0, consoleY, 600, 112 ];
				blocks.push( r );
				// 720090: fixed 600x112. 5086E0: black alpha C8 at left, 0 at right.
				quads.push( {
					rect: r,
					uv: [ 0, 0, 1, 1 ],
					color: [ 0, 0, 0, 200 / 255 ],
					rightColor: [ 0, 0, 0, 0 ],
					texture: "",
					clip: full
				} );
				for ( const [i, row] of consoleRows.slice( -7 ).entries() ) {
					label( row, 5, consoleY + 2 + i * 13, [ 200 / 255, 200 / 255, 200 / 255, 1 ], r );
				}
				const pink = [ 1, 128 / 255, 1, 1 ] as const, px = 5 + text.run( "C:>" ).width + 2;
				label( "C:>", 5, consoleY + 96, pink, r );
				const edit: UiRect = [ px, consoleY + 96, 600 - px, 16 ],
					start = Math.max( 0, Math.min( consoleText.length, selection[0] ?? 0 ) ),
					end = Math.max( start, Math.min( consoleText.length, selection[1] ?? start ) );
				const before = text.run( consoleText.slice( 0, start ) ).width,
					through = text.run( consoleText.slice( 0, end ) ).width,
					scroll = focus === "gm-input" ? Math.max( 0, through - edit[2] + 3 ) : 0;
				if ( focus === "gm-input" && end > start ) {
					rect( [ px + before - scroll, edit[1], through - before, 16 ], [ .2, .4, .7, .6 ], "", [
						0,
						0,
						1,
						1
					], edit );
				}
				quads.push(
					...text.quads(
						consoleText,
						[ px - scroll, edit[1], Math.max( edit[2], text.run( consoleText ).width ), 16 ],
						edit,
						pink
					)
				);
				if ( focus === "gm-input" && caretVisible ) {
					rect( [ px + through - scroll, edit[1], 2, text.height() + 2 ], pink, "", [ 0, 0, 1, 1 ], edit );
				}
				controls.push( {
					id: "gm-input",
					label: "GM command",
					kind: "text",
					value: consoleText,
					rect: [ px, consoleY + 96, 600 - px, 16 ],
					maxLength: 255
				} );
			}
			probe?.detailEnd( "ui-assembly" );
			probe?.detailBegin( "ui-finalize" );
			const semantics = {
				loadingVisible: !!loading && !assetFailure,
				// The diagnostic chip gets a separate row below the compact Map
				// button, whether the minimap is expanded or collapsed.
				hudCorner: compact ?
					[ w, itemMall.read().visible ? 52 : compactTelemetryTop, 0, 0 ] as UiRect :
					hudCorner,
				focusRequest: worldVisible && phase === "world" ? focusRequest : undefined,
				loadingProgress: loading?.progress,
				loadingStatus: loading?.status,
				loadingError: assetFailure ?? next.frontend?.error ?? undefined,
				loading: !!assetFailure ?
					!nativeLoadError :
					loading?.startup === true || next.frontend?.phase === "failed",
				title: phase === "world" ? "Silkroad game interface" : "Silkroad " + phase,
				message: assetFailure ?
					(next.worldRetrying ? "Connection lost. Retrying automatically. " : "Unable to finish loading. ") +
					(next.worldError ?
						"Retry world loading." :
						fatalAssetFailure ?
						"Reload the client to try again." :
						"Interface resources unavailable; retrying.") :
					phase === "disconnected" ?
					((next.session?.disconnectMessage || title.catalog( "UIIT_MSG_MSGBOX_NETOFF" ) ||
						"Disconnected from the server.") +
						(next.session?.incidentID ?
							` Incident ${next.session.incidentID}. Report ${
								next.session.incidentDelivery ?? "not sent"
							}.` :
							"")) :
					title.message( error ) ||
					(phase === "world" ?
						`Health ${local?.hp ?? "unknown"}; mana ${local?.mp ?? "unknown"}; target ${
							target?.name ?? "none"
						}. ${next.entities.length} nearby entities.` :
						phase),
				controls
			};
			// A completion here must rebuild texture-dependent quads on the next step.
			paths.push( ...missionLoadingAssets() );
			// Prewarm decoded authored artwork so hotkeys/sidebar changes stay immediate.
			// Renderer now derives GPU residency from committed quads; this catalogue
			// must never again be interpreted as the set of GPU-resident textures.
			paths.push( ...hud.data()?.warmPaths ?? [], ...guideResources.data()?.warmPaths ?? [] );
			paths.push( ...windowWarm.paths() );
			// An open map wants every tile a drag can reveal, even when it opened
			// before its warm build ran.
			if ( panel === "Map" ) paths.push( ...worldMapImagePaths( hud.data()?.mapIcons ) );
			for ( const item of game?.inventory ?? [] ) {
				const path = iconPath( item.icon );
				if ( path ) paths.push( path );
			}
			// Publication: the demand is final for this layout. Frozen, it lets
			// resources.step skip re-reading it on the frames until the next layout.
			Object.freeze( paths );
			dirty = resources.step( paths, now );
			if ( dirty ) layoutResourcesRevision++;
			if ( windowWarm.active() ) {
				// Keep the demand, publish nothing, and close the unseen window.
				windowWarm.end( paths );
				setPanel( "", "warm" );
				admittedWindows.clear();
				gauges.end();
				dirty = true;
				return null;
			}
			gauges.end();
			// Text runs travel to the renderer whole: the GPU packer writes one record
			// per glyph straight into its buffer (text-run.ts, device/ui.ts).
			quads = resolveTextOverlaps( quads );
			probe?.detailEnd( "ui-finalize" );
			probe?.detailBegin( "ui-compare" );
			const unchanged = lastProduct && lastProduct.width === w && lastProduct.height === h &&
				lastProduct.damageText === worldVisible && sameUiQuads( lastProduct.quads, quads ) &&
				sameUiSemantics( lastProduct.semantics, semantics );
			probe?.detailEnd( "ui-compare" );
			if ( unchanged ) return null;
			probe?.detailBegin( "ui-publish" );
			lastProduct = { width: w, height: h, damageText: worldVisible, quads, semantics };
			publish( { revision: ++revision, width: w, height: h, quads, damageText: worldVisible } );
			probe?.detailEnd( "ui-publish" );
			return semantics;
		},
		/*
		================
		dispose

		Release owned children and pending work before discarding local state.
		================
		*/
		dispose() {
			if ( !disposed ) persistWindowPositions();
			itemMall.reset();
			skillTraining.reset();
			gauges.reset();
			tooltipMemo = null;
			chatLayoutCache.reset();
			statusLayoutCache.reset();
			playerLayoutCache.reset();
			petLayoutCache.reset();
			barLayoutCache.reset();
			admittedWindows.clear();
			lastProduct = null;
			if ( disposed ) return;

			disposed = true;
			password = "";
			account = "";
			chatText = "";
			chatTarget = "";
			controls = [];
			blocks = [];
			paths = [];
			const errors: unknown[] = [];
			for (
				const owner of [
					resources,
					hud,
					guideResources,
					minimapResources,
					stallCategories,
					localization,
					title,
					text
				]
			) {
				try {
					owner.dispose();
				} catch ( error ) {
					errors.push( error );
				}
			}
			try {
				publish( null );
			} catch ( error ) {
				errors.push( error );
			}
			if ( errors.length ) throw new AggregateError( errors, "UI cleanup failed" );
		}
	};
}
