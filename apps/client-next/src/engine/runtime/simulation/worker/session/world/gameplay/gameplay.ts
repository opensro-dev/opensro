/*
===========================================================================

gameplay.ts - worker gameplay command and packet ownership

Movement, combat, inventory and social lanes commit wire state here before
publishing it to presentation. UI controls and quickslots submit the same
commands and cannot bypass actor eligibility.

===========================================================================
*/
import { createParamJobs } from "@/engine/foundation/gameplay/param-job";
import { createStorageRoom, isWarehouseTicket } from "@/engine/foundation/gameplay/storage-room";
import type { PlayerModel } from "@/engine/foundation/gameplay/skin-change";
import {
	jobAliasRequest,
	jobDressSeconds,
	jobGuildAnswer,
	jobJoinRequest,
	jobWithdrawRequest,
	noJob,
	type LocalJob
} from "@/engine/foundation/gameplay/job-guild";
import { recallAppointmentRequest, recallAppointmentNotice } from "@/engine/foundation/gameplay/recall-appointment";
import { createPickup } from "./pickup";
import { createActionSession } from "./action-session";
import { createCosPickup } from "./cos-pickup";
import {
	withdrawalRequest,
	withdrawalSkillBindings,
	SKILL_WITHDRAWAL_RESPONSE,
	MASTERY_WITHDRAWAL_RESPONSE
} from "@/engine/foundation/gameplay/withdrawal";
import { positionSkillRequest } from "@/engine/foundation/gameplay/position-skill";
import { repairNotice } from "@/engine/foundation/gameplay/repair";
import { portalNotice } from "@/engine/foundation/gameplay/portal";
import {
	interactionApproach,
	interactionApproachTransition,
	type InteractionApproachState
} from "@/engine/foundation/gameplay/interaction-approach";
import { targetNotice } from "@/engine/foundation/gameplay/target-notices";
import { constantNativeNotice } from "@/engine/foundation/gameplay/native-notice";
import { skillNotice } from "@/engine/foundation/gameplay/skill-notices";
import { returnScrollCast, type ReturnScrollCast } from "@/engine/foundation/gameplay/return-scroll";
import { fortressActive } from "@/engine/foundation/gameplay/fortress";
import { skillCooldown } from "@/engine/foundation/gameplay/skill-cooldowns";
import { uniqueNotice, uniqueReferences } from "@/engine/foundation/gameplay/unique-notices";
import { inventoryNotice } from "@/engine/foundation/gameplay/inventory-notices";
import {
	emptyPartyMatching,
	partyMatchPacket,
	partyMatchRequest,
	partyActiveJob,
	partyPurposeAllowed
} from "@/engine/foundation/gameplay/party-matching";
import { gmRequest, gmReply, gmItemReferences, type GmReply } from "@/engine/foundation/gameplay/gm-command";
import {
	fortressBootstrap,
	fortressPacket,
	fortressMusicActive,
	fortressMusicMode
} from "@/engine/foundation/gameplay/fortress";
import {
	cosTimerPacket,
	cosTimerReference,
	type CosItemWindowReference,
	type CosItemWindow
} from "@/engine/foundation/gameplay/cos-timer";
import { vitalWarning } from "@/engine/foundation/ui/vital-warning";
import { gameOptions, initialGameOptions, type GameOptions } from "@/engine/foundation/gameplay/game-options";
import {
	autoPotionBootstrap,
	autoPotionSettings,
	autoPotionSave,
	autoPotionEntry,
	autoPotionDelay,
	autoPotionEligible,
	autoPotionActive,
	autoPotionItemSlot,
	type AutoPotionFacts
} from "@/engine/foundation/gameplay/auto-potion";
import { playerStats } from "@/engine/foundation/gameplay/player-stats";
import {
	fortressNotice,
	serverNotification,
	restrictionNotice,
	type SystemNotice
} from "@/engine/foundation/gameplay/system-notices";
import {
	academyBootstrap,
	academyRequest,
	academyPacket,
	academyNoticeRequest
} from "@/engine/foundation/gameplay/academy";
import { academyAcknowledgment } from "@/engine/foundation/gameplay/academy-notices";
import {
	guideBootstrap,
	revealGuide,
	queueGuide,
	guideLevelEvents,
	guideInventoryEvents,
	guideAbnormalEvent
} from "@/engine/foundation/gameplay/guide";
import {
	parseAbnormalSnapshot,
	abnormalSnapshotNotices,
	type AbnormalRecord
} from "@/engine/foundation/gameplay/abnormal-snapshot";
import { entryEnvironment, environmentPacket } from "@/engine/foundation/gameplay/event-environment";
import {
	cosContainerRequest,
	cosContainerResult,
	cosGroundRequest,
	cosGroundResult
} from "@/engine/foundation/gameplay/cos-container";
import { cosBehaviorResult, applyCosBehavior, cosBehaviorRequest } from "@/engine/foundation/gameplay/cos-behavior";
import { createFeedback } from "./feedback/feedback";
import { saleResult, soldInventory, commerceJson, commerceInteger } from "@/engine/foundation/gameplay/commerce";
import { actionEmote, quickSlotItemSlot, quickSlot, TRACE_ACTION_ID } from "@/engine/foundation/gameplay/quickslots";
import { reconcileQuickslotInventory } from "@/engine/foundation/gameplay/quickslot-inventory";
import { createTraining } from "./training/training";
import {
	emptySocial,
	socialPacket,
	socialRequest,
	withoutResurrection,
	type SocialCommand
} from "@/engine/foundation/gameplay/social";
import { partyLootNotice } from "@/engine/foundation/gameplay/party-loot";
import {
	skillCatalog,
	skillMpCost,
	skillTrainingReason,
	trainingRequest,
	type SkillMetadata
} from "@/engine/foundation/gameplay/skill-catalog";
import { createCastMotionLock } from "@/engine/foundation/gameplay/cast-motion-lock";
import { createSkillPressQueue, decidePress } from "@/engine/foundation/gameplay/skill-queue";
import { movementHeading } from "@/engine/foundation/gameplay/native-movement";
import { bootstrapProgression, progressionPacket, type Progression } from "@/engine/foundation/gameplay/progression";
import { skillBindings, quickSlotPacket } from "@/engine/foundation/gameplay/quickslots";
import { decodeCosRecord } from "@/engine/foundation/gameplay/cos-record";
import { decodeWorldClock } from "@/engine/foundation/gameplay/world-clock";
import { createQuests } from "./quests/quests";
import { createNpcConversation } from "./npc/npc";
import { createChat } from "./chat/chat";
import { createMovement } from "./movement/movement";
import { logoutCancelRequest, worldPointAction } from "@/engine/foundation/gameplay/direction-movement";
import { createInventory } from "./inventory/inventory";
import { createCombat } from "./combat/combat";
import { createTargeting } from "./targeting/targeting";
import { createMoveReservation } from "./reservation/reservation";
import { createBetaPlayerMap } from "./beta-map/beta-map";
import type { GameplayCommand, GameplayState } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import type { WireFrame } from "@/engine/contracts/network";
/*
================
unlimitedItemIds

The bootstrap's optional unlimitedItems list: positive integer RefObjIDs.
A malformed list is a server defect, not something to guess around.
================
*/
function unlimitedItemIds( value: unknown ): readonly number[] {
	const list = (value as { unlimitedItems?: unknown; }).unlimitedItems;
	if ( list === undefined ) return [];
	if ( !Array.isArray( list ) || list.some( id => !Number.isInteger( id ) || id < 1 || id > 0xffffffff ) ) {
		throw new Error( "Invalid bootstrap unlimited item list" );
	}
	return list as number[];
}

/*
================
WorldReferences

The world catalog lookups gameplay reads but does not own (entities owns
the catalog): a character reference's country, a country's player models
and an item reference.
================
*/
interface WorldReferences {
	readonly country: ( refObjId: number ) => number | undefined;
	readonly playerModels: ( country: number ) => readonly PlayerModel[];
	readonly item: ( refObjId: number ) => { readonly typeFlags: number; readonly name: string; } | undefined;
}

// A dress answer may trail its bar by a server tick and the round trip.
const JOB_DRESS_ANSWER_GRACE_MS = 5000;

// grounditem.ExecuteRange: a pickup closer than this is granted in place;
// farther away the server walks the player to the item first.
const PICKUP_EXECUTE_RANGE = 10;
// How long past two round trips a sent skill press's cooldown stand-in waits
// for its answer.
const SKILL_ANSWER_SLACK_MS = 500;
// B2CD kind 1 admits a command (75BAA0); count 2 queues it behind an open one.
const ACTION_STATE_ARM = 1;
const QUEUED_COMMANDS = 2;

/*
================
skillPressAnswer

What a frame says about the local player's newest skill press. B2CD is the
local interface's command count: kind 1 (arm) admits the press, count 2
queues it. B245 refuses it ([2, code]) or opens a cast; only a cast of the
local caster (+6) answers a press. Monster and peer casts nearby, and the
B2CD releases of earlier commands, answer nothing: timing the round trip
by them made it short.
================
*/
function skillPressAnswer( frame: WireFrame, localGid: number ): "answered" | "queued" | null {
	const p = frame.payload;
	if ( frame.opcode === 0xb2cd ) {
		if ( p[0] !== ACTION_STATE_ARM ) return null;
		return p[1] === QUEUED_COMMANDS ? "queued" : "answered";
	}
	if ( frame.opcode !== 0xb245 || !localGid ) return null;
	if ( p[0] !== 1 ) return "answered";
	if ( p.length < 10 ) return null;
	return ((p[6]! | p[7]! << 8 | p[8]! << 16 | p[9]! << 24) >>> 0) === localGid ? "answered" : null;
}

/*
================
createGameplay

Compose gameplay owners at the worker boundary. Commands and packets commit
synchronously before take publishes one coherent frame snapshot.
================
*/
export function createGameplay(
	send: ( frame: WireFrame ) => void,
	play: ( handle: import("@/engine/foundation/ui/sound-catalog").UiSoundHandle, at: number ) => void = () => {},
	publishFeedback: (
		event:
			| import("@/engine/contracts/orb").VisualFeedback
			| import("@/engine/contracts/effective-hp").CombatPresentationEvent
	) => void = () => {},
	readEntity: ( gid: number ) => import("@/engine/contracts/world").EntityState | undefined = () => undefined,
	playItem: ( cue: import("@/engine/contracts/audio").ItemSoundRequest, at: number ) => void = () => {}
) {
	let entryVitals: Partial<
		Pick<import("@/engine/contracts/gameplay").VitalState, "hp" | "mp" | "maxHp" | "maxMp" | "abnormal">
	> = {};
	let partyMatching = emptyPartyMatching();
	let gmItems = gmItemReferences( {} );
	let warnings = [ false, false ];
	let options = initialGameOptions();
	let eligibility = { gm: false, pcRoomEvent: false };
	let autoPotion = autoPotionBootstrap( {} );
	let potionFacts: AutoPotionFacts = { alive: false, hp: 0, mp: 0, maxHp: 0, maxMp: 0, abnormal: 0 };
	const potionDue: [number | null, number | null, number | null] = [ null, null, null ];
	const guideSummons = new Map<number, number>();
	let guide: GameplayState["guide"];
	let academy: GameplayState["academy"];
	let selectionDecal: GameplayState["selectionDecal"] = null;
	// The bootstrap's unlimited-item ids (beta starter kit); empty without one.
	let unlimitedItems: readonly number[] = [];
	let soundClock = 0;
	const feedback = createFeedback();
	const pickup = createPickup();
	const actionSession = createActionSession();
	const cosPickup = createCosPickup();
	const training = createTraining( send );
	// The held skill press, the newest denial and the round-trip estimate
	// (skill-queue.ts).
	const skillPress = createSkillPressQueue<GameplayCommand & { kind: "skill"; }>();
	const movement = createMovement( send ),
		inventory = createInventory( send, handle => play( handle, soundClock ), cue => playItem( cue, soundClock ) ),
		combat = createCombat( readEntity, publishFeedback, () => skillPress.oneWayMs() ),
		targeting = createTargeting( send ),
		moveReservation = createMoveReservation(),
		betaMap = createBetaPlayerMap();
	/*
================
sendFrame
================
	*/
	function sendFrame( frame: WireFrame ): WireFrame {
		send( frame );
		pickup.sent( frame );
		if ( frame.opcode === 0x72cd && frame.payload[0] === 1 ) {
			moveReservation.clear();
			// sendSkillPress names the skill right after.
			skillPress.commandSent();
		}
		return frame;
	}
	/*
================
sendSkillPress

A skill press leaves: its answer times the round trip. When the server will
start the cast as the press arrives (immediate: no target, or one within the
skill's reach), its cooldown stands in from then until that answer. A press
the server first walks the caster for starts nothing on arrival: a stand-in
there showed a cooldown that vanished when it lapsed mid-run and came back
when the cast finally started.
================
	*/
	function sendSkillPress(
		frame: WireFrame,
		skillId: number,
		now: number,
		immediate: boolean,
		target = 0
	): WireFrame {
		const oneWay = skillPress.oneWayMs();
		sendFrame( frame );
		skillPress.sent( now, skillId );
		if ( immediate && affordable( catalog.find( row => row.id === skillId ) ) ) {
			combat.pressed( skillId, now + oneWay, now + 4 * oneWay + SKILL_ANSWER_SLACK_MS, now );
		} // The HUD shows it as next while the server runs the caster there.
		else skillPress.approach( skillId, target, now );
		return frame;
	}
	/*
================
affordable

Whether the caster's MP covers the skill's authored cost. A press it does
not cover is still sent (a consumption rate the server alone knows may
lower the cost), but the server all but surely refuses it (0x3004), so it
neither stands a cooldown in nor starts its cast: either showed a cooldown,
and an animation, for a cast that never came. Unknown vitals count as paid.
================
	*/
	function affordable( metadata: SkillMetadata | undefined ): boolean {
		if ( !metadata || !potionFacts.maxMp ) return true;
		return skillMpCost( metadata, potionFacts.maxMp ) <= potionFacts.mp;
	}
	/*
================
withinReach

Whether target stands within the skill's authored range of the caster's
pose. The server's reach adds both bodies, so within it the server never
walks the caster first. A skill whose reach is the weapon's has no authored
range: unknown, so not within.
================
	*/
	function withinReach( metadata: SkillMetadata | undefined, target: EntityState ): boolean {
		const pose = movement.state().pose;
		if ( !pose || !metadata?.range ) return false;
		if ( (target.regionId | pose.regionId) & 0x8000 && target.regionId !== pose.regionId ) return false;
		const dx = target.x - pose.x + ((target.regionId & 255) - (pose.regionId & 255)) * 1920,
			dz = target.z - pose.z + ((target.regionId >>> 8) - (pose.regionId >>> 8)) * 1920;
		return Math.hypot( dx, dz ) <= metadata.range;
	}
	/*
================
predictCast

Start the press's cast animation now when the server will all but surely
start the cast at once (cast-prediction.ts): an action skill, a living
caster standing on foot (a walk the server leads goes on until the server's
stop: an action there would slide) with no cast in flight, and no target or
a living one within the skill's authored range (the server's reach adds
both bodies to it). The caster turns to the target at once, as the cast
would turn it.
================
	*/
	function predictCast(
		metadata: SkillMetadata | undefined,
		target: EntityState | undefined,
		local: EntityState | undefined,
		now: number
	) {
		const walking = movement.state(), pose = walking.pose;
		if (
			!metadata?.actionMs || !affordable( metadata ) || !pose || walking.moving || !local || local.mountedOn ||
			local.appearanceState?.[0] === 2 || localCastHolds( now ) || combat.predicting() ||
			combat.guidedActive( localGid, now )
		) return;
		if ( target && target.gid !== localGid ) {
			if ( target.kind === "monster" && target.appearanceState?.[0] === 2 || !withinReach( metadata, target ) ) {
				return;
			}
			movement.heading( movementHeading( pose, { ...target, angle: target.heading } ) );
		}
		const oneWay = skillPress.oneWayMs();
		combat.predict( metadata.id, target?.gid ?? 0, now, now + 4 * oneWay + SKILL_ANSWER_SLACK_MS );
	}
	/*
================
cancelActionForMovement

Cancel continuation before waiting on presentation. Otherwise a new basic
attack can arrive in the same batch as the previous close and starve the walk.
================
	*/
	function cancelActionForMovement() {
		// A ground click also drops a skill press held for its cooldown, and
		// the run-up to a pressed skill's target.
		if ( skillPress.cancel() ) dirty = true;
		if ( skillPress.approachEnded() ) dirty = true;
		const cancel = actionSession.cancelForMovement();
		if ( !cancel ) return;
		sendFrame( cancel );
		actionSession.sentCancellation();
	}
	const cosItemRefs = new Map<number, number>(), cosItemCaps = new Map<number, number>();
	const cosRefs = new Map<number, number>(),
		cosRecords = new Map<number, import("@/engine/contracts/gameplay").CosRecord>();
	let uniqueRefs = uniqueReferences( {} );
	let gmReplies: readonly (GmReply & { sequence: number; })[] = [];
	let gmSequence = 0;
	let noticeSequence = 0;
	let notices: readonly SystemNotice[] = [];
	let progression: Progression = { masteries: [] };
	const skillGroups = new Map<number, { group: number; level: number; }>();
	let fortress = fortressBootstrap( {} ), musicMode = 0;
	let social = emptySocial();
	// The world catalog's lookups, bound by the composition root (core).
	let worldReferences: WorldReferences = { country: () => undefined, playerModels: () => [], item: () => undefined };
	// The session's catalog is fixed; one list per country keeps the
	// published state's identity stable.
	let playerModels: { readonly country: number; readonly models: readonly PlayerModel[]; } | null = null;
	// The local player's job guild membership (job-guild.ts).
	let job: LocalJob = noJob();
	/*
	================
	localPlayerModels
	================
	*/
	function localPlayerModels(): readonly PlayerModel[] {
		if ( localCountry === undefined ) return [];
		if ( playerModels?.country !== localCountry || !playerModels.models.length ) {
			playerModels = { country: localCountry, models: worldReferences.playerModels( localCountry ) };
		}
		return playerModels.models;
	}
	let bindings = skillBindings( {} );
	let catalog: readonly SkillMetadata[] = [];
	// The local cast's action-state-2 window (CIDecoSkill 8E0A23 / 877240).
	const castMotion = createCastMotionLock();
	const bindingRepairs = new Map<number, import("@/engine/foundation/gameplay/quickslots").QuickSlot>();
	/*
================
flushBindingRepairs

Inventory has already committed. Retain repaired references if transport
fails; retry persistence without restoring stale slot occupancy.
================
	*/
	function flushBindingRepairs() {
		for ( const [slot, row] of bindingRepairs ) {
			try {
				send( quickSlotPacket( row ) );
			} catch {
				return;
			}
			bindingRepairs.delete( slot );
		}
	}
	let worldClock: GameplayState["worldClock"];
	let environment = entryEnvironment( {} ).state;
	let cosError: string | null = null;
	let approach: InteractionApproachState = { phase: "idle" };
	let returnScroll: ReturnScrollCast | undefined, teleportMode = 0;
	let activeCos: GameplayState["activeCos"], cosResult: GameplayState["cosResult"];
	// 6E6150 keys a kind-3 row by its item id, so distinct items stack.
	let cosWindows: readonly CosItemWindow[] = [];
	const cosItemRefs2 = new Map<number, CosItemWindowReference>();
	// Kind-4 board rows of the EXP/SP scroll jobs (param-job.ts).
	const paramJobs = createParamJobs();
	let abnormalRecords: readonly AbnormalRecord[] = [];
	let abnormalMask = 0;
	const chat = createChat( send ), quests = createQuests( send );
	const npcConversation = createNpcConversation( send );
	// The NPC warehouse (storage-room.ts).
	const storage = createStorageRoom( send );
	let previousLockedQuestNotice = "";
	let localGid = 0, revision = 0, dirty = false, protocol = 0, localCountry: number | undefined;
	/*
================
localCastHolds

The local player's own cast is live (not cancelled) and its action still
runs: the server's attack lock (4AAB40) drops ground commands until it
releases. A persistent cast (a wall, an aura) stays in the cast table for
the object's whole life, but its action ends with the authored action
time (actionMs); counted for its whole life it blocked the predicted cast
and the pickup run-up for minutes. A cast whose action time is unknown
holds until it ends, as before.
================
	*/
	function localCastHolds( now: number ): boolean {
		return combat.state().casts.some( c => {
			if ( c.caster !== localGid || c.cancelledAtMs !== undefined ) return false;
			const actionMs = catalog.find( row => row.id === c.skill )?.actionMs;
			return !actionMs || c.receivedAtMs === undefined || now - c.receivedAtMs < actionMs;
		} );
	}
	/*
================
withMemberCountries

Stamp each party member with its model's country once per party packet,
so the race marks never depend on the member being in view.
================
	*/
	function withMemberCountries<
		T extends { readonly members: readonly import("@/engine/foundation/gameplay/social").PartyMember[]; }
	>( value: T ): T {
		if ( !value.members.some( member => member.country === undefined ) ) return value;
		return {
			...value,
			members: value.members.map( member => {
				const country = member.country ?? worldReferences.country( member.model );
				return country === undefined ? member : { ...member, country };
			} )
		};
	}
	/*
================
predictPickupRunUp

A pickup beyond reach is a walk the server drives: it answers 0x72CD with
a movement acknowledgement toward the item (grounditem.PlanApproach). Start
that walk now so the click answers at once; the acknowledgement adopts it
and a refusal walks it back (movement.predictApproach).
================
	*/
	function predictPickupRunUp( entity: EntityState, local: EntityState | undefined, now: number ) {
		const pose = movement.state().pose;
		if ( !pose || localCastHolds( now ) || local?.mountedOn || local?.appearanceState?.[0] === 2 ) return;
		const dx = entity.x - pose.x + ((entity.regionId & 255) - (pose.regionId & 255)) * 1920,
			dz = entity.z - pose.z + ((entity.regionId >>> 8) - (pose.regionId >>> 8)) * 1920;
		if ( (entity.regionId | pose.regionId) & 0x8000 && entity.regionId !== pose.regionId ) return;
		if ( Math.hypot( dx, dz ) <= PICKUP_EXECUTE_RANGE ) return;
		movement.predictApproach(
			{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: pose.angle },
			now
		);
	}
	/*
================
selectEntity

Request selection in reach or after approach. A coalesced click has no reply
coming, so retain its menu, dialogue and lock until a new request.
================
	*/
	function selectEntity( entity: EntityState, now: number ) {
		const frame = targeting.select( entity.gid, now, entity.kind, {
			fortress: !!entity.teleport?.fortressId,
			reopen: npcConversation.state().phase === "closed"
		} );
		if ( frame ) npcConversation.clear();
		selectionDecal = {
			kind: "target",
			gid: entity.gid,
			slot: entity.kind === "monster" || entity.kind === "cos" ? 3 : entity.kind === "player" ? 2 : 1
		};
		return frame;
	}
	/*
================
clearState

Retire session facts together so a later character cannot inherit bindings,
selected entities, cooldowns or world-entry state.
================
	*/
	function clearState() {
		actionSession.clear();
		moveReservation.clear();
		betaMap.clear();
		partyMatching = emptyPartyMatching();
		entryVitals = {};
		warnings = [ false, false ];
		eligibility = { gm: false, pcRoomEvent: false };
		autoPotion = autoPotionBootstrap( {} );
		potionFacts = { alive: false, hp: 0, mp: 0, maxHp: 0, maxMp: 0, abnormal: 0 };
		potionDue.fill( null );
		selectionDecal = null;
		uniqueRefs = uniqueReferences( {} );
		gmItems.clear();
		gmReplies = [];
		notices = [];
		guide = undefined;
		guideSummons.clear();
		academy = undefined;
		feedback.reset();
		training.reset();
		social = emptySocial();
		fortress = fortressBootstrap( {} );
		musicMode = 0;
		bindings = skillBindings( {} );
		catalog = [];
		castMotion.clear();
		progression = { masteries: [] };
		skillGroups.clear();
		movement.clear();
		chat.clear();
		pickup.clear();
		cosPickup.clear();
		quests.clear();
		npcConversation.clear();
		previousLockedQuestNotice = "";
		approach = interactionApproachTransition( approach, { kind: "cancel" } );
		returnScroll = undefined;
		teleportMode = 0;
		inventory.clear();
		bindingRepairs.clear();
		inventory.takeBindingMoves();
		targeting.clear();
		combat.clear();
		skillPress.clear();
		localGid = 0;
		localCountry = undefined;
		job = noJob();
		cosRecords.clear();
		cosRefs.clear();
		cosItemRefs.clear();
		cosItemCaps.clear();
		cosItemRefs2.clear();
		cosWindows = [];
		paramJobs.reset();
		storage.reset();
		abnormalRecords = [];
		abnormalMask = 0;
		worldClock = undefined;
		environment = entryEnvironment( {} ).state;
		activeCos = undefined;
		cosResult = undefined;
		cosError = null;
	}
	const api = {
		/*
================
enterMusic
================
		*/
		enterMusic() {
			musicMode = fortressMusicActive( fortress ) ? 3 : 0;
			dirty = true;
		},
		/*
================
notice
================
		*/
		notice( value: SystemNotice ) {
			notices = [ ...notices.slice( -99 ), { ...value, sequence: ++noticeSequence } ];
			dirty = true;
		},
		/*
================
chatBlocks
================
		*/
		chatBlocks( value: readonly string[] ) {
			chat.chatBlocks( value );
			dirty = true;
		},
		/*
================
options
================
		*/
		options( value: GameOptions ) {
			options = gameOptions( value );
			chat.options( options.whispers );
			dirty = true;
		},
		/*
================
bootstrap

Validate authoritative entry data before exposing character facts. Live
packets own subsequent mutations; bootstrap owns only initial state.
================
		*/
		bootstrap( value: unknown ) {
			pickup.clear();
			cosPickup.clear();
			approach = interactionApproachTransition( approach, { kind: "cancel" } );
			returnScroll = undefined;
			teleportMode = 0;
			partyMatching = emptyPartyMatching();
			bindingRepairs.clear();
			inventory.takeBindingMoves();
			gmItems = gmItemReferences( value );
			unlimitedItems = unlimitedItemIds( value );
			const entryEvents = entryEnvironment( value );
			warnings = [ false, false ];
			autoPotion = autoPotionBootstrap( value );
			const identity = (value as { character?: { gmPrivilege?: unknown; pcRoomEvent?: unknown; }; }).character;
			for ( const flag of [ identity?.gmPrivilege, identity?.pcRoomEvent ] ) {
				if ( flag !== undefined && typeof flag !== "boolean" ) throw Error( "Invalid entry eligibility" );
			}
			eligibility = { gm: identity?.gmPrivilege === true, pcRoomEvent: identity?.pcRoomEvent === true };
			potionDue.fill( null );
			const pc =
				(value as { character?: { hp?: number; mp?: number; maxHp?: number; maxMp?: number; }; }).character;
			entryVitals = { hp: pc?.hp, mp: pc?.mp, maxHp: pc?.maxHp, maxMp: pc?.maxMp };
			for ( const value of Object.values( entryVitals ) ) {
				if ( value !== undefined && (!Number.isInteger( value ) || value < 0 || value > 0xffffffff) ) {
					throw Error( "Invalid entry vitals" );
				}
			}
			potionFacts = {
				alive: false,
				hp: pc?.hp ?? 0,
				mp: pc?.mp ?? 0,
				maxHp: pc?.maxHp ?? 0,
				maxMp: pc?.maxMp ?? 0,
				abnormal: 0
			};
			gmReplies = [];
			notices = entryEvents.notices.map( key => ({ key, value: 0, sequence: ++noticeSequence }) );
			feedback.bootstrap( value );
			const b = value as {
				character?: {
					gold?: number;
					skillPoints?: number;
					statPoints?: number;
					masteries?: { id: number; level: number; }[];
				};
				refSkillSnapshot?: {
					id: number;
					group: number;
					level: number;
					status: boolean;
					effectRider: boolean;
				}[];
			};
			uniqueRefs = uniqueReferences( value );
			const nextProgression = bootstrapProgression( value ),
				nextCatalog = skillCatalog( value ),
				nextGuide = guideBootstrap( value );
			guide = nextGuide;
			guideSummons.clear();
			for (
				const row
					of (value as { refItemSnapshot?: { refObjId: number; summonedCharacterTypeFlags?: number; }[]; })
						.refItemSnapshot ?? []
			) {
				if ( row.summonedCharacterTypeFlags !== undefined ) {
					if (
						!Number.isInteger( row.summonedCharacterTypeFlags ) || row.summonedCharacterTypeFlags < 0 ||
						row.summonedCharacterTypeFlags > 65535
					) throw Error( "Invalid guide summon reference" );
					guideSummons.set( row.refObjId, row.summonedCharacterTypeFlags );
				}
			}
			academy = academyBootstrap( value );
			const nextGroups = new Map<number, { group: number; level: number; }>();
			for ( const row of b.refSkillSnapshot ?? [] ) {
				if (
					!Number.isInteger( row.id ) || row.id <= 0 || row.id > 0xffffffff ||
					!Number.isInteger( row.group ) || row.group < 0 || row.group > 0xffffffff ||
					!Number.isInteger( row.level ) || row.level < 0 || row.level > 255 || nextGroups.has( row.id )
				) throw new Error( "Invalid skill group reference" );
				nextGroups.set( row.id, { group: row.group, level: row.level } );
			}
			progression = nextProgression;
			skillGroups.clear();
			for ( const [id, ref] of nextGroups ) skillGroups.set( id, ref );
			training.reset();
			training.bootstrap( value );
			fortress = fortressBootstrap( value );
			musicMode = 0;
			social = emptySocial( (value as { character?: { name?: string; }; }).character?.name ?? "" );
			bindings = skillBindings( value );
			catalog = nextCatalog;
			castMotion.catalog( nextCatalog );
			worldClock = undefined;
			environment = entryEvents.state;
			chat.bootstrap( value );
			chat.options( options.whispers );
			quests.bootstrap( value );
			npcConversation.clear();
			activeCos = undefined;
			cosResult = undefined;
			cosError = null;
			cosRecords.clear();
			cosRefs.clear();
			cosItemRefs.clear();
			cosItemCaps.clear();
			for (
				const row
					of (value as { refItemSnapshot?: { refObjId: number; typeFlags: number; }[]; }).refItemSnapshot ??
						[]
			) cosItemRefs.set( row.refObjId, row.typeFlags );
			for (
				const row of (value as { refObjSnapshot?: { refObjId: number; kind: string; tidWord: number; }[]; })
					.refObjSnapshot ?? []
			) if ( row.kind === "cos" ) cosRefs.set( row.refObjId, row.tidWord );
			for (
				const row
					of (value as { refItemSnapshot?: { refObjId: number; nativeFields?: { maxStack?: number; }; }[]; })
						.refItemSnapshot ?? []
			) {
				const cap = row.nativeFields?.maxStack;
				if ( cap !== undefined ) cosItemCaps.set( row.refObjId, cap );
			}
			cosItemRefs2.clear();
			cosWindows = [];
			paramJobs.reset();
			for (
				const row of (value as {
					refItemSnapshot?: ({ refObjId: number; } & Parameters<typeof cosTimerReference>[0])[];
				}).refItemSnapshot ?? []
			) {
				const reference = cosTimerReference( row );
				if ( reference ) cosItemRefs2.set( row.refObjId, reference );
				paramJobs.reference( row );
			}
			protocol = (value as {
				simulationProtocolVersion?: number;
			}).simulationProtocolVersion ?? 0;
			movement.clear();
			movement.mode(
				(value as {
					character?: {
						world?: {
							movementMode?: number;
						};
					};
				}).character?.world?.movementMode ?? 3
			);
			targeting.clear();
			combat.clear();
			combat.references( b.refSkillSnapshot ?? [] );
			inventory.bootstrap( value );
			guide = queueGuide( guide, [
				...guideInventoryEvents(
					inventory.state().inventory,
					inventory.state().equipmentSlotCount ?? 0,
					guideSummons
				),
				1
			] );
			localGid = 0;
			localCountry = undefined;
			dirty = true;
		},
		/*
================
speeds
================
		*/
		speeds( entity: EntityState, now: number ) {
			if ( entity.walkSpeed !== undefined && entity.runSpeed !== undefined ) {
				movement.speeds( entity.walkSpeed, entity.runSpeed, now );
				dirty = true;
			}
		},
		/*
================
seed

Bind the admitted local actor and initialize its authoritative movement.
================
		*/
		seed( entity: EntityState ) {
			localCountry = entity.countryByte9c;
			job = entity.localJob ?? noJob();
			if ( localGid !== entity.gid ) {
				localGid = entity.gid;
				publishFeedback( { kind: "orb-gauge", value: feedback.gauge() } );
				publishFeedback( { kind: "orb-clear" } );
				combat.seed( localGid, entryVitals );
				combat.cooldownReferences( localGid, catalog );
			}
			movement.seed( { ...entity, angle: entity.heading } );
			if ( entity.appearanceState?.[0] === 2 ) movement.life( 2, 0 );
			if ( entity.walkSpeed !== undefined && entity.runSpeed !== undefined ) {
				movement.speeds( entity.walkSpeed, entity.runSpeed, 0 );
			}
			dirty = true;
		},
		/*
================
correct
================
		*/
		correct( entity: EntityState, now?: number ) {
			if ( entity.gid !== localGid ) throw Error( "Correction references non-local entity" );
			movement.correct( { ...entity, angle: entity.heading }, now );
			dirty = true;
		},
		/*
================
nameInputs
================
		*/
		nameInputs( now = soundClock ) {
			return { social, fortress, attackedName: combat.nameAttack( now ), localItem: inventory.nameItem() };
		},
		/*
================
localIdentity
================
		*/
		localIdentity() {
			return localGid;
		},
		/*
================
entityLifecycle

Entity removal retires targeting and combat references in the same frame.
================
		*/
		entityLifecycle(
			event: Extract<import("@/engine/contracts/world").WorldEvent, { kind: "spawn" | "despawn"; }>
		) {
			cosPickup.track( event );
			if ( event.kind === "spawn" ) {
				combat.seedEffects( event.entity.gid, event.entity.spawnSkills ?? [], soundClock );
			} else {
				approach = interactionApproachTransition( approach, { kind: "despawn", gid: event.gid } );
				combat.remove( event.gid, soundClock );
				pickup.remove( event.gid );
				targeting.remove( event.gid );
				if ( skillPress.queued()?.command.gid === event.gid && skillPress.cancel() ) dirty = true;
				if ( skillPress.targetGone( event.gid ) ) dirty = true;
				const conversation = npcConversation.state();
				if ( conversation.phase !== "closed" && conversation.gid === event.gid ) npcConversation.clear();
			}
			dirty = true;
		},
		/*
================
itemUseCooldown
================
		*/
		itemUseCooldown( slot: number ) {
			return inventory.useCooldown( slot );
		},
		/*
================
itemUseType
================
		*/
		itemUseType( slot: number ) {
			return inventory.useType( slot );
		},
		/*
================
skillTarget

The object a skill press aims at, or undefined for none: the newest
selection intent (see targeting.selectionIntent). This owner is always
newer than the target the UI snapshot carried, which trails a click by one
grant round trip and a deselection by one publish.
================
		*/
		skillTarget() {
			return targeting.selectionIntent() || undefined;
		},
		/*
================
command

UI and quickslots share this dispatcher. Validate current actor and target
state here before a command can claim a native wire conversation.
================
		*/
		command(
			command: GameplayCommand,
			now: number,
			entity: EntityState | undefined,
			local?: EntityState
		): WireFrame | null {
			dirty = true;
			soundClock = now;
			if ( command.kind === "berserk" ) {
				return localGid && feedback.gauge() === 5 ?
					sendFrame( { opcode: 0x7341, payload: Uint8Array.of( 1 ) } ) :
					null;
			}
			if ( command.kind === "effect-cancel" ) {
				const { skillId, token } = command;
				if (
					!Number.isInteger( skillId ) || skillId <= 0 || skillId > 0xffffffff ||
					!Number.isInteger( token ) || token < 0 || token > 0xffffffff
				) throw Error( "Invalid effect cancellation identity" );
				// Expiry/teardown can win the worker queue race after a UI click.
				if (
					!combat.state().attachedEffects.some( e =>
						e.gid === localGid && e.skill === skillId && (!token || e.token === token)
					)
				) return null;
				// 6FD710: skill identity, optional nonzero instance, zero tail.
				const payload = new Uint8Array( token ? 11 : 7 ), v = new DataView( payload.buffer );
				payload[0] = 1;
				payload[1] = 5;
				v.setUint32( 2, skillId, true );
				if ( token ) v.setUint32( 6, token, true );
				return sendFrame( { opcode: 0x72cd, payload } );
			}
			if ( command.kind === "minimap-floors" ) {
				movement.minimapFloors( command.poses );
				return null;
			}
			if ( command.kind === "return-cancel" ) {
				return localGid && returnScroll ?
					sendFrame( { opcode: 0x72dd, payload: new Uint8Array( 0 ) } ) :
					null;
			}
			if ( command.kind === "gathering-cancel" ) {
				quests.cancelGathering();
				return null;
			}
			if (
				combat.guidedActive( localGid, now ) &&
				[ "move", "ground-move", "attack", "skill", "pickup" ].includes( command.kind )
			) return null;
			if (
				teleportMode === 1 && [ "move", "ground-move", "attack", "skill", "pickup" ].includes( command.kind )
			) return null;
			if (
				[ "move", "ground-move", "select", "npc-close", "attack", "skill", "pickup" ].includes( command.kind )
			) approach = interactionApproachTransition( approach, { kind: "cancel" } );
			if ( command.kind === "recall-appoint" ) {
				const target = targeting.state(), conversation = npcConversation.state();
				if (
					!localGid || local?.appearanceState?.[0] === 2 || target.targetPending ||
					conversation.phase !== "menu" || conversation.gid !== command.gid ||
					target.target !== command.gid || !((target.targetCapabilities ?? 0) & 0x40)
				) return null;
				return sendFrame( recallAppointmentRequest( command.gid ) );
			}
			if ( command.kind === "npc-close" ) {
				const frame = targeting.release( now );
				npcConversation.clear();
				storage.close();
				return frame;
			}
			if ( command.kind === "job-join" || command.kind === "job-withdraw" || command.kind === "job-alias" ) {
				// The job menu exists only on the selected guild NPC (5D79E0).
				if ( !localGid || targeting.state().target !== command.gid ) throw Error( "Select a job guild NPC" );
				return sendFrame(
					command.kind === "job-join" ?
						jobJoinRequest( command.gid, command.job ) :
						command.kind === "job-withdraw" ?
						jobWithdrawRequest( command.gid ) :
						jobAliasRequest( command.gid, command.mode, command.alias )
				);
			}
			if ( command.kind === "storage-open" ) {
				const target = targeting.state();
				// The storage row exists only on a selected warehouse NPC (0x4).
				if ( !localGid || target.target !== command.gid || !((target.targetCapabilities ?? 0) & 4) ) {
					throw Error( "Select a warehouse NPC" );
				}
				storage.open( command.gid );
				dirty = true;
				return null;
			}
			if ( command.kind === "storage-close" ) {
				storage.close();
				dirty = true;
				return null;
			}
			if ( command.kind === "storage-move" ) {
				const room = storage.state();
				if ( !room ) throw Error( "Storage room is not open" );
				return inventory.storageMove( room, command.move, now, cosItemCaps );
			}
			if ( command.kind === "npc-talk" || command.kind === "npc-choice" ) {
				const target = targeting.state(), conversation = npcConversation.state();
				if (
					!localGid || target.targetPending || conversation.phase === "closed" ||
					conversation.gid !== target.target || !((target.targetCapabilities ?? 0) & 2)
				) throw Error( "Select an available talking NPC" );
				if ( command.kind === "npc-talk" ) npcConversation.talk( now );
				else npcConversation.choose( command.choice, now );
				return null;
			}

			if ( command.kind === "gm-command" ) {
				if ( !eligibility.gm || !localGid ) return null;
				const frame = gmRequest( command.line, gmItems, local?.heading ?? 0 );
				return frame ? sendFrame( frame ) : null;
			}
			if ( command.kind === "rebirth" ) {
				if ( local?.gid !== localGid || local?.appearanceState?.[0] !== 2 ) return null;
				if ( command.choice !== 1 && command.choice !== 2 ) throw Error( "Invalid rebirth choice" );
				if ( command.choice === 2 && (progression.level === undefined || progression.level > 10) ) return null;
				// Native 697215 sends 32DC without an opcode-group lock: CC9054 only
				// registers 7338/B338. Silent refusals must leave later clicks usable.
				// The server serializes revival and rejects already-living actors.
				const frame = { opcode: 0x32dc, payload: Uint8Array.of( command.choice ) };
				send( frame );
				return frame;
			}
			if (
				local?.appearanceState?.[0] === 2 &&
				[ "move", "ground-move", "attack", "skill", "pickup", "cos-attack" ].includes( command.kind )
			) return null;
			// 6932D7..69338D: a ground click cancels a running action and a
			// logout countdown before the seated and navigation checks.
			if ( command.kind === "ground-move" || command.kind === "move" ) {
				cancelActionForMovement();
				if ( command.kind === "ground-move" && command.departing ) sendFrame( logoutCancelRequest() );
			}
			// 85C590 freeze leaves action state 9, which disables navigation.
			// Sleep and stun do not. The mask is the same word 0x36C7 and 0x33A6 write.
			if (
				(combat.state().vitals.find( v => v.gid === localGid )?.abnormal ?? 0) & 1 &&
				[ "move", "ground-move" ].includes( command.kind )
			) return null;
			// 6933a6 ground click / 692d19 entity click: seated interaction
			// requests stand and RETURNS. It must not also predict travel.
			if (
				local?.movementMode === 4 &&
				[ "move", "ground-move", "select", "attack", "pickup" ].includes( command.kind )
			) return sendFrame( { opcode: 0x7017, payload: Uint8Array.of( 4 ) } );

			if (
				command.kind === "party-match-page" || command.kind === "party-match-join" ||
				command.kind === "party-match-register" || command.kind === "party-match-modify" ||
				command.kind === "party-match-delete" || command.kind === "party-match-answer" ||
				command.kind === "party-match-auto" || command.kind === "party-match-auto-stop"
			) {
				if (
					(command.kind === "party-match-register" || command.kind === "party-match-modify") &&
					!partyPurposeAllowed( partyActiveJob( inventory.state().inventory ), command.registration.purpose )
				) return null;
				// Expiry or a replacement request may win the worker queue race.
				if (
					command.kind === "party-match-answer" &&
					(!partyMatching.request || partyMatching.request.a !== command.a ||
						partyMatching.request.b !== command.b)
				) return null;
				const next = partyMatchRequest( partyMatching, command, now );
				if ( next.frame ) send( next.frame );
				partyMatching = next.state;
				return next.frame;
			}
			if ( command.kind === "academy-notice" ) {
				if ( !academy || !localGid ) throw Error( "Academy session is not initialized" );
				const frame = academyNoticeRequest( command.subject, command.contents );
				if ( frame ) return sendFrame( frame );
				const notice = constantNativeNotice( 0x1d, 0x17 );
				if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
				dirty = true;
				return null;
			}
			if ( command.kind === "academy-page" || command.kind === "academy-join" ) {
				if ( !academy || !localGid ) throw Error( "Academy session is not initialized" );
				const next = academyRequest( academy, command, progression.level );
				send( next.frame );
				academy = next.state;
				return next.frame;
			}
			if ( command.kind === "stat-increase" ) {
				if ( command.stat !== "str" && command.stat !== "int" ) throw Error( "Invalid stat allocation" );
				if ( !localGid || !local || (progression.statPoints ?? 0) < 1 ) return null;
				return sendFrame( { opcode: command.stat === "str" ? 0x727a : 0x7552, payload: new Uint8Array( 0 ) } );
			}
			if ( command.kind === "beginner-mark" ) {
				if ( !local || !localGid || typeof command.enabled !== "boolean" ) {
					throw Error( "Missing beginner mark authority" );
				}
				if ( (progression.maxLevel ?? progression.level ?? 255) > 19 && command.enabled ) return null;
				return sendFrame( {
					opcode: 0x7683,
					payload: Uint8Array.of( ((local.visualFlags ?? 0) & 2) | (command.enabled ? 1 : 0) )
				} );
			}
			if ( command.kind === "auto-potion-save" ) {
				const next = autoPotionSettings( command.settings ), frame = autoPotionSave( next );
				send( frame );
				autoPotion = next;
				potionDue.fill( null );
				return frame;
			}
			if ( command.kind === "guide-event" ) {
				if ( !guide || !localGid ) throw Error( "Guide session is not initialized" );
				const next = revealGuide( guide, command.event );
				if ( !next ) return null;
				send( next.frame );
				guide = next.state;
				return next.frame;
			}
			if ( command.kind.startsWith( "alchemy-" ) || command.kind.startsWith( "gacha-" ) ) {
				if ( !localGid ) throw Error( "Local player is not initialized" );
				if (
					command.kind === "gacha-open" &&
					(!entity || entity.kind !== "npc" || entity.refObjId !== 9251 ||
						targeting.state().target !== command.gid || targeting.state().targetPending)
				) throw Error( "Select the Magic Pop NPC" );
				return inventory.process(
					command as import("@/engine/contracts/item-process").ItemProcessCommand,
					now
				);
			}
			if (
				[
					"inventory-move",
					"item-use",
					"skill-withdraw",
					"mastery-withdraw",
					"item-drop",
					"gold-drop",
					"shop-open",
					"shop-buy",
					"shop-sell",
					"shop-buyback"
				].includes( command.kind ) && inventory.state().inventoryPending
			) throw Error( "Inventory is busy" );
			if ( command.kind === "navigation" ) {
				movement.navigation( command.regionId, command.bundle, command.requestId );
				dirty = true; // Publish stationary grounding when navigation arrives.
				return null;
			}
			if ( !localGid ) {
				throw new Error( "Local player is not initialized" );
			}
			if ( command.kind === "cos-shop-buy" || command.kind === "cos-shop-sell" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || entity?.gid !== record.gid || entity.kind !== "cos" || entity.ownerGid !== localGid ||
					entity.refObjId !== record.refObjId
				) throw Error( "No owned COS shop authority" );
				if ( inventory.state().shop?.npc !== targeting.state().target ) {
					throw Error( "Merchant selection changed" );
				}
				return inventory.cosTrade(
					record,
					command.kind === "cos-shop-buy",
					command.slot,
					command.quantity,
					command.kind === "cos-shop-buy" ? command.tab : 0,
					now
				);
			}
			if ( command.kind === "cos-transfer" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || entity?.gid !== record.gid || entity.kind !== "cos" || entity.ownerGid !== localGid ||
					entity.refObjId !== record.refObjId
				) throw Error( "No owned COS container authority" );
				return inventory.transferCos(
					record,
					command.toCos,
					command.source,
					command.destination,
					now,
					cosItemCaps
				);
			}
			if ( command.kind === "cos-pickup" || command.kind === "cos-drop" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || entity?.gid !== record.gid || entity.kind !== "cos" || entity.ownerGid !== localGid ||
					entity.refObjId !== record.refObjId
				) throw Error( "No owned COS ground authority" );
				return inventory.cosGround(
					cosGroundRequest(
						record,
						command.kind === "cos-drop" ? "drop" : "pickup",
						command.kind === "cos-drop" ? command.slot : command.target
					),
					now
				);
			}
			if ( command.kind === "cos-inventory-move" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || entity?.gid !== record.gid || entity.kind !== "cos" || entity.ownerGid !== localGid ||
					entity.refObjId !== record.refObjId
				) throw Error( "No owned COS container authority" );
				const frame = cosContainerRequest( record, command.source, command.destination, command.quantity );
				return inventory.cosMove( frame, now );
			}
			if ( command.kind === "cos-ride" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || record.dead || record.hp === 0 || ![ 1, 2 ].includes( record.band ) ||
					entity?.gid !== record.gid || entity.kind !== "cos" ||
					(record.band !== 1 && entity.ownerGid !== localGid) ||
					entity.refObjId !== record.refObjId || command.mounted === (local?.mountedOn === record.gid)
				) throw Error( "Invalid owned COS ride transition" );
				const payload = new Uint8Array( 5 );
				payload[0] = Number( command.mounted );
				new DataView( payload.buffer ).setUint32( 1, record.gid, true );
				return sendFrame( { opcode: 0x74b5, payload } );
			}
			if ( command.kind === "cos-follow" || command.kind === "cos-cancel" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || (command.kind === "cos-follow" && (record.dead || record.hp === 0)) ||
					![ 3, 4 ].includes( record.band ) ||
					entity?.gid !== record.gid || entity.kind !== "cos" || entity.ownerGid !== localGid ||
					entity.refObjId !== record.refObjId || local?.mountedOn === record.gid
				) throw Error( "No eligible owned companion" );
				if ( command.kind === "cos-cancel" ) {
					// 6FF8C0 sends only the owned GID. Acknowledgement does not
					// remove the actor: the ordinary despawn owns that transition.
					const payload = new Uint8Array( 4 );
					new DataView( payload.buffer ).setUint32( 0, record.gid, true );
					return sendFrame( { opcode: 0x756c, payload } );
				}
				// CosEntryPanel_SyncActiveState 6A2777 sends no target after tag 9.
				const payload = new Uint8Array( 5 );
				new DataView( payload.buffer ).setUint32( 0, record.gid, true );
				payload[4] = 9;
				return sendFrame( { opcode: 0x769e, payload } );
			}
			if ( command.kind === "cos-pet-attack" ) {
				// 6A2350 case 2: an attack pet (class 3) attacks the player's target
				// with 0x769E [u32 pet][u8 2][u32 target] and remembers it at
				// +0xAB2C. The client admits monster targets only, like its own
				// basic attack.
				// command.gid names the target; the record set holds only owned pets.
				const record = cosRecords.get( command.pet );
				if ( !record || record.band !== 3 || record.dead || record.hp === 0 || entity?.kind !== "monster" ) {
					throw Error( "No attack pet or attackable target" );
				}
				const payload = new Uint8Array( 9 ), v = new DataView( payload.buffer );
				v.setUint32( 0, record.gid, true );
				payload[4] = 2;
				v.setUint32( 5, entity.gid, true );
				return sendFrame( { opcode: 0x769e, payload } );
			}
			if ( command.kind === "cos-clean" ) {
				// CICCos_ExecuteActionCommand (6A2350) case 5: a riding mount or a
				// transport (record class 0/1, bands 1/2) is retired with 0x7618
				// [u32 gid] (6FF800); a guild soldier (class 4, band 5) with an
				// empty 0x7458 (6FE850). Pets leave through cancellation instead.
				const record = cosRecords.get( command.gid );
				// A riding mount spawns without an owner GID (as cos-ride allows);
				// the owner's own record set proves it is ours.
				if (
					!record || ![ 1, 2, 5 ].includes( record.band ) || entity?.gid !== record.gid ||
					entity.kind !== "cos" || (record.band !== 1 && entity.ownerGid !== localGid) ||
					entity.refObjId !== record.refObjId
				) throw Error( "No owned COS to clean" );
				if ( record.band === 5 ) return sendFrame( { opcode: 0x7458, payload: new Uint8Array( 0 ) } );
				const payload = new Uint8Array( 4 );
				new DataView( payload.buffer ).setUint32( 0, record.gid, true );
				return sendFrame( { opcode: 0x7618, payload } );
			}
			if ( command.kind === "cos-behavior" ) {
				const record = cosRecords.get( command.gid );
				if (
					!record || entity?.gid !== record.gid || entity.kind !== "cos" || entity.ownerGid !== localGid ||
					entity.refObjId !== record.refObjId
				) throw Error( "No owned COS behavior authority" );
				return sendFrame( cosBehaviorRequest( record, command.mode ) );
			}
			if (
				command.kind.startsWith( "party-" ) || command.kind.startsWith( "guild-" ) ||
				command.kind === "social-consent" || command.kind === "resurrection-consent"
			) {
				// CIFCommunityNotice_SubmitText 5F46F0 checks subject first,
				// then contents, before composing the guild notice request.
				if ( command.kind === "guild-notice" && (!command.subject || !command.contents) ) {
					const notice = constantNativeNotice( 0x10, !command.subject ? 0x22 : 0x23 );
					if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					dirty = true;
					return null;
				}
				// The worker may retire a prompt before its queued UI click arrives.
				if ( command.kind === "social-consent" && !social.invitation ) return null;
				if ( command.kind === "resurrection-consent" && !social.resurrection ) return null;
				const request = socialRequest( social, command as SocialCommand );
				send( request );
				if ( command.kind === "social-consent" ) social = { ...social, invitation: null };
				if ( command.kind === "resurrection-consent" ) social = withoutResurrection( social );
				return request;
			}
			if ( command.kind === "skill-withdraw" || command.kind === "mastery-withdraw" ) {
				const { id, rank } = command;
				let receiptID = command.id;
				if ( command.kind === "skill-withdraw" && command.rank > 0 ) {
					const current = catalog.find( row => row.id === id );
					const lower = catalog.find( row =>
						row.group === current?.group && row.level === rank && row.trainable
					);
					if ( !lower ) throw Error( "Restoration rank is unavailable" );
					receiptID = lower.id;
				}
				return training.request( withdrawalRequest( command ), receiptID, now );
			}
			if ( command.kind === "skill-train" || command.kind === "mastery-train" ) {
				const id = command.id;
				if ( command.kind === "skill-train" ) {
					const row = catalog.find( r => r.id === id );
					if ( !row ) throw Error( "Skill metadata is unavailable" );
					const reason = skillTrainingReason( row, bindings.skills, catalog, progression );
					if ( reason ) throw Error( reason );
				} else if ( !progression.masteries.some( r => r.id === id ) ) throw Error( "Mastery is not available" );
				return training.request( trainingRequest( command.kind, command.id ), id, now );
			}
			if ( command.kind === "quickslot-bind" || command.kind === "quickslot-set" ) {
				const row = command.kind === "quickslot-set" ?
					quickSlot( command.binding ) :
					{ slot: command.slot, kind: command.skillId === 0 ? 0 : 0x49, payload: command.skillId };
				const frame = quickSlotPacket( row );
				if ( row.kind === 0x49 && !bindings.skills.includes( row.payload ) ) {
					throw Error( "Skill is not learned" );
				}
				const itemSlot = quickSlotItemSlot( row );
				if ( itemSlot !== null && !inventory.state().inventory.some( item => item.slot === itemSlot ) ) {
					throw Error( "Bound item is absent" );
				}

				send( frame );
				bindingRepairs.delete( row.slot );
				bindings = {
					...bindings,
					quickSlots: [
						...bindings.quickSlots.filter( old => old.slot !== row.slot ),
						...(row.kind ? [ row ] : [])
					].sort( ( a, b ) => a.slot - b.slot )
				};
				return frame;
			}
			if ( command.kind === "quest-abandon" || command.kind === "quest-reward" ) {
				quests.request( command.refId, command.kind === "quest-reward" );
				return null;
			}
			if ( command.kind === "whisper-block" ) {
				chat.block( command.name, command.blocked, now );
				return null;
			}
			if ( command.kind === "chat" ) {
				chat.request(
					command.channel === 1 && eligibility.gm ? 3 : command.channel,
					command.text,
					command.target ?? "",
					now
				);
				return null;
			}
			if ( command.kind === "action-command" ) {
				if ( command.id === TRACE_ACTION_ID ) {
					const selection = targeting.state();
					const target = readEntity( selection.target ?? 0 );
					if (
						!localGid || !local || local.mountedOn || local.appearanceState?.[0] === 2 ||
						teleportMode === 1 ||
						combat.guidedActive( localGid, now ) || selection.targetPending || !target ||
						target.kind !== "player" ||
						target.gid === localGid || target.appearanceState?.[0] === 2 ||
						combat.state().casts.some( c => c.caster === localGid && c.cancelledAtMs === undefined )
					) return null;
					// 695420 action 1003 is Trace, never the basic-attack family.
					const payload = Uint8Array.of( 1, 3, 1, 0, 0, 0, 0 );
					new DataView( payload.buffer ).setUint32( 3, target.gid, true );
					const frame = sendFrame( { opcode: 0x72cd, payload } );
					approach = interactionApproachTransition( approach, { kind: "cancel" } );
					return frame;
				}
				if ( command.id === 1001 ) {
					return sendFrame( { opcode: 0x7017, payload: Uint8Array.of( local?.movementMode === 2 ? 3 : 2 ) } );
				}
				if ( command.id === 5000 ) {
					const pet = [ ...cosRecords.values() ].find( c => c.band === 4 && !c.dead && c.hp > 0 );
					if ( !pet ) return null;
					const payload = new Uint8Array( 5 );
					new DataView( payload.buffer ).setUint32( 0, pet.gid, true );
					payload[4] = 11;
					return sendFrame( { opcode: 0x769e, payload } );
				}
				if ( command.id === 1000 ) {
					if (
						local?.mountedOn || local?.appearanceState?.[0] === 2 ||
						combat.state().casts.some( c => c.caster === localGid && c.cancelledAtMs === undefined )
					) return null;
					return sendFrame( { opcode: 0x7017, payload: Uint8Array.of( 4 ) } );
				}
				const emote = actionEmote( command.id );
				if ( emote === null ) throw Error( "Action is unavailable" );
				return sendFrame( { opcode: 0x324b, payload: Uint8Array.of( emote ) } );
			}
			if ( command.kind === "ground-move" || command.kind === "move" ) {
				if ( protocol !== 1 ) {
					throw new Error( "Server does not support simulation protocol 1" );
				}
				if ( castMotion.locked( combat.state().casts, localGid, now, !actionSession.released() ) ) {
					// Resolve the click now, not when the cast releases: its ray belongs
					// to the camera at click time, and the destination marker appears
					// when the player clicks. A direction walk keeps its query.
					let held: typeof command = command;
					if ( command.kind === "ground-move" ) {
						const state = movement.state();
						if ( !state.pose ) return null;
						const action = worldPointAction(
							state.pose,
							movement.pick( command.query ),
							command.query,
							false
						);
						if ( action.kind === "none" ) return null;
						if ( action.kind === "walk-to" ) held = { kind: "move", destination: action.destination };
					}
					moveReservation.hold( held );
					selectionDecal = held.kind === "move" ? { kind: "ground", pose: { ...held.destination } } : null;
					dirty = true;
					return null;
				}
				if ( local?.mountedOn && (!activeCos || activeCos.gid !== local.mountedOn || activeCos.dead) ) {
					throw Error( "Mounted COS authority is unavailable" );
				}
				const cosGid = local?.mountedOn || undefined;
				let destination = command.kind === "move" ? command.destination : undefined;
				if ( command.kind === "ground-move" ) {
					// CGInterface_MoveToWorldPoint: a miss walks the ray's direction.
					const state = movement.state();
					if ( !state.pose ) return null;
					const walking = state.moving && state.directionWalk !== undefined;
					const action = worldPointAction(
						state.pose,
						movement.pick( command.query ),
						command.query,
						walking
					);
					if ( action.kind === "none" ) return null;
					if ( action.kind === "walk-direction" ) {
						const frame = movement.direct( action.heading, now, cosGid );
						pickup.clear();
						selectionDecal = null;
						return frame;
					}
					destination = action.destination;
				}
				if ( !destination ) return null;
				const frame = movement.request( destination, now, cosGid );
				pickup.clear();
				selectionDecal = { kind: "ground", pose: { ...destination } };
				return frame;
			}
			if ( command.kind === "avatar-move" ) {
				return inventory.avatarMove( command.equip, command.source, command.destination, now );
			}
			if ( command.kind === "inventory-move" ) {
				return inventory.move( command.source, command.destination, command.quantity, now );
			}
			if ( command.kind === "mall-open" ) return inventory.openMall( now );
			if ( command.kind === "mall-buy" ) return inventory.purchaseMall( command.request, now );
			if ( command.kind === "shop-open" ) {
				if (
					entity?.kind !== "npc" || targeting.state().target !== command.gid ||
					!((targeting.state().targetCapabilities ?? 0) & 1)
				) throw Error( "Select a merchant first" );
				return inventory.openShop( command.gid, now );
			}
			if ( command.kind === "shop-repair" ) {
				if ( inventory.state().shop?.npc !== targeting.state().target ) {
					throw Error( "Merchant selection changed" );
				}
				return inventory.repair( command.mode, command.slot, now );
			}
			if ( command.kind === "shop-buyback" ) {
				if ( inventory.state().shop?.npc !== targeting.state().target ) {
					throw Error( "Merchant selection changed" );
				}
				return inventory.buyback( command.id, now );
			}
			if ( command.kind === "shop-buy" || command.kind === "shop-sell" ) {
				if ( inventory.state().shop?.npc !== targeting.state().target ) {
					throw Error( "Merchant selection changed" );
				}
				return inventory.trade(
					command.kind === "shop-buy",
					command.slot,
					command.quantity,
					command.kind === "shop-buy" ? command.tab : 0,
					now
				);
			}
			if ( command.kind === "item-drop" ) return inventory.drop( command.slot, now );
			if ( command.kind === "gold-drop" ) {
				if (
					progression.gold === undefined || !Number.isSafeInteger( command.amount ) || command.amount < 1 ||
					BigInt( command.amount ) > BigInt( progression.gold )
				) throw Error( "Insufficient gold" );
				return inventory.dropGold( command.amount, now );
			}
			if ( command.kind === "item-use" ) {
				return inventory.use( command.slot, now, {
					records: [ ...cosRecords.values() ],
					selectedGid: command.companionGid,
					revivalSlot: command.revivalSlot,
					summonerSlot: command.summonerSlot,
					skin: command.skin,
					targetSlot: command.targetSlot
				} );
			}
			if ( command.kind === "release-target" ) {
				if ( skillPress.cancel() ) dirty = true;
				const frame = targeting.release( now );
				npcConversation.clear();
				return frame;
			}
			if ( command.kind === "skill" ) {
				const skillId = command.skillId;
				if ( !bindings.skills.includes( command.skillId ) ) throw Error( "Skill is not learned" );
				const metadata = catalog.find( row => row.id === skillId );
				// skill-queue.ts: never send a press the server would refuse for
				// its cooldown; hold one that is nearly ready, deny the rest.
				const decision = decidePress(
					skillCooldown( combat.state().skillCooldowns, skillId, metadata?.cooldownGroup ?? 0, now )
						?.remainingMs,
					skillPress.oneWayMs(),
					now
				);
				if ( decision.kind === "queue" ) {
					skillPress.queue( { skill: skillId, command, fireAtMs: decision.fireAtMs }, now );
					dirty = true;
					return null;
				}
				if ( decision.kind === "deny" ) {
					skillPress.deny( { skill: skillId, atMs: now, remainingMs: decision.remainingMs } );
					dirty = true;
					return null;
				}
				// A press sent now supersedes any held one.
				if ( skillPress.cancel() ) dirty = true;
				if ( metadata?.groundTarget ) {
					const from = movement.state().pose;
					if ( !from || !command.query ) throw Error( "Point into the world to cast this skill" );
					if ( local?.mountedOn ) throw Error( "Cannot cast while mounted" );
					const to = movement.groundSkillGoal( command.query, now );
					if ( !to ) return null;
					const frame = positionSkillRequest( skillId, to );
					if ( metadata.haltsWalk ) movement.holdForCast( now );
					return sendSkillPress( frame, skillId, now, true );
				}
				if ( metadata && !metadata.targetRequired ) command = { kind: "skill", skillId: command.skillId };
				else if ( metadata?.targetRequired && !command.gid ) {
					// A row that admits its caster (targetSelf) aims at the caster when
					// nothing is selected, as the server resolves the same row.
					if ( !metadata.targetSelf || !localGid ) throw Error( "This skill requires a target" );
					const frame = combat.skill( skillId, localGid );
					movement.holdForCast( now );
					predictCast( metadata, undefined, local, now );
					return sendSkillPress( frame, skillId, now, true );
				}
			}
			if ( command.kind === "skill" && command.gid === undefined ) {
				// An ordinary cast stops the server's walk where the command finds
				// it (InitiateSkillCast 59B5F6): end the local walk at the press,
				// as a targeted command does (movement.holdForCast).
				const frame = combat.skill( command.skillId );
				const skillId = command.skillId, metadata = catalog.find( row => row.id === skillId );
				if ( metadata?.haltsWalk ) movement.holdForCast( now );
				predictCast( metadata, undefined, local, now );
				return sendSkillPress( frame, skillId, now, true );
			}
			if ( !entity || (entity.gid === localGid && command.kind !== "skill") ) {
				throw new Error( "Target is absent or local player" );
			}
			if ( command.kind === "mount" ) {
				if (
					entity.kind !== "cos" || entity.ownerGid !== localGid || activeCos?.gid !== entity.gid ||
					activeCos.refObjId !== entity.refObjId || activeCos.dead || combat.state().vitals.find( v =>
							v.gid === entity.gid
						)?.hp === 0
				) throw new Error( "No living owned active COS" );
				const payload = new Uint8Array( 5 );
				new DataView( payload.buffer ).setUint32( 0, entity.gid, true );
				payload[4] = 0x0b;
				return sendFrame( { opcode: 0x769e, payload } );
			}
			if ( command.kind === "cos-attack" ) {
				if ( !activeCos || activeCos.dead || entity.kind !== "monster" || local?.mountedOn !== activeCos.gid ) {
					throw Error( "No living active COS or attackable target" );
				}
				const payload = new Uint8Array( 9 ), v = new DataView( payload.buffer );
				v.setUint32( 0, activeCos.gid, true );
				payload[4] = 2;
				v.setUint32( 5, entity.gid, true );
				return sendFrame( { opcode: 0x769e, payload } );
			}
			if ( command.kind === "pickup" ) {
				if ( entity.kind !== "ground-item" ) throw new Error( "Target is not a ground item" );
				const frame = pickup.request( entity.gid );
				if ( !frame ) return null;
				predictPickupRunUp( entity, local, now );
				return sendFrame( frame );
			}
			if ( command.kind === "select" ) {
				const pose = movement.state().pose;
				// 698740: an NPC or gate out of reach is walked to first; the
				// select follows when the walk ends (the arrival block below).
				if ( pose ) {
					const destination = interactionApproach( pose, entity );
					if ( destination ) {
						const frame = movement.request( destination, now, local?.mountedOn || undefined );
						pickup.clear();
						approach = interactionApproachTransition( approach, { kind: "begin", target: entity } );
						npcConversation.clear();
						return frame;
					}
				}
				return selectEntity( entity, now );
			}
			if ( command.kind === "attack" && entity.kind !== "monster" ) {
				throw new Error( "Target is not attackable" );
			}
			// A targeted command settles the server's walk where it finds it: stop the
			// local walk at the same point (movement.holdForCast).
			if ( command.kind === "attack" ) {
				const frame = combat.attack( entity.gid );
				movement.holdForCast( now );
				return sendFrame( frame );
			}
			if ( command.kind !== "skill" ) throw Error( "Unsupported gameplay command" );
			const frame = combat.skill( command.skillId, entity.gid );
			movement.holdForCast( now );
			const pressedSkill = command.skillId;
			const pressedMetadata = catalog.find( row => row.id === pressedSkill );
			predictCast( pressedMetadata, entity, local, now );
			return sendSkillPress(
				frame,
				command.skillId,
				now,
				entity.gid === localGid || withinReach( pressedMetadata, entity ),
				entity.gid
			);
		},
		/*
================
references
================
		*/
		references( rows: readonly import("@/engine/foundation/gameplay/commerce").CommerceItemReference[] ) {
			inventory.references( rows );
			for ( const row of rows ) {
				cosItemRefs.set( row.refObjId, row.typeFlags );
				if ( row.maxStack !== undefined ) cosItemCaps.set( row.refObjId, row.maxStack );
				paramJobs.reference(
					row as typeof row & { readonly nativeFields?: { readonly itemParam1_29c?: number; }; }
				);
			}
		},
		surface: movement.surface,
		/*
================
heading
================
		*/
		heading( angle: number ) {
			movement.heading( angle );
			dirty = true;
		},
		/*
================
receive

Route each native reply to its owner before scheduling presentation.
Packet handling must not depend on which HUD panel is currently open.
================
		*/
		receive( frame: WireFrame, now: number, chatSender?: EntityState ) {
			const inventoryBefore = inventory.state().inventory;
			// Every skill press is answered at once by B245 or B2CD.
			const answer = skillPressAnswer( frame, localGid );
			if ( answer ) skillPress.answered( now );
			// A local cast opened or a press was refused: no run-up waits.
			if ( answer && frame.opcode === 0xb245 && skillPress.approachEnded() ) dirty = true;
			if ( frame.opcode === 0xb2cd && frame.payload.length >= 2 ) {
				if ( skillPress.commandCount( frame.payload[0]!, frame.payload[1]!, now ) ) dirty = true;
			}
			if ( answer === "queued" ) {
				// Its cast, and with it its cooldown, starts only when the open
				// command ends.
				if ( combat.pressQueued( now ) ) dirty = true;
				// The server queued the command instead of acting on it: it never
				// stopped its walk, so a walk held at the press follows it again
				// (held, the player fell behind by the whole queue time and the
				// next correction pulled it 40 to 70 units forward).
				movement.castRefused( now );
			}
			try {
				if ( betaMap.receive( frame ) ) {
					dirty = true;
					return true;
				}
				if ( actionSession.receive( frame ) ) {
					pickup.receive( frame );
					// B2CD releases the action queue, not a movement prediction. The
					// movement acknowledgement owns acceptance/refusal of the walk.
					// 75BAA0: kind 3 is the generic action notice; pickup's
					// inventory refusals still arrive separately on B06D.
					const notice = frame.payload[0] === 3 ? constantNativeNotice( 0x19, frame.payload[2]! ) : null;
					if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					// A refused command leaves the server's walk running.
					if ( frame.payload[0] === 3 ) movement.castRefused( now );
					dirty = true;
					return true;
				}
				const academyAck = academyAcknowledgment( frame );
				if ( academyAck ) {
					if ( academyAck.notice ) {
						notices = [ ...notices.slice( -99 ), { ...academyAck.notice, sequence: ++noticeSequence } ];
						dirty = true;
					}
					return true;
				}
				if ( frame.opcode === 0x317d ) {
					const notice = partyLootNotice( frame.payload, {
						item: worldReferences.item,
						memberName: gid => social.members.find( member => member.id === gid )?.name,
						localGid
					} );
					notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					dirty = true;
					return true;
				}
				if ( storage.receive( frame, cosItemRefs ) && frame.opcode !== 0xb338 ) {
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb338 ) {
					npcConversation.interaction( frame.payload, targeting.state().targetCapabilities ?? 0 );
					// 75AE50 kind 2 -> 689420 category 13: a refused NPC function shows
					// its reason (code 4, UIIT_MSG_INTERACTION_FAIL_TOO_FAR).
					const refusal = frame.payload[0] === 2 ? constantNativeNotice( 13, frame.payload[1]! ) : null;
					if ( refusal ) notices = [ ...notices.slice( -99 ), { ...refusal, sequence: ++noticeSequence } ];
					dirty = true;
				}
				if ( frame.opcode === 0xb5b6 ) {
					const reply = gmReply( frame.payload );
					if ( reply ) {
						gmReplies = [ ...gmReplies.slice( -99 ), { ...reply, sequence: ++gmSequence } ];
						dirty = true;
					}
					return true;
				}
				soundClock = now;
				if ( frame.opcode === 0x343c ) {
					const stats = playerStats( frame.payload );
					potionFacts = { ...potionFacts, maxHp: stats.maxHp, maxMp: stats.maxMp };
					if ( localGid ) combat.seed( localGid, { maxHp: stats.maxHp, maxMp: stats.maxMp } );
				}
				if (
					frame.opcode === 0x3122 && frame.payload.length === 6 && frame.payload[4] === 0 &&
					frame.payload[5] === 1 &&
					new DataView( frame.payload.buffer, frame.payload.byteOffset, 6 ).getUint32( 0, true ) === localGid
				) {
					dirty = true;
				}
				const matched = partyMatchPacket( partyMatching, frame, social.localName, now, {
					race: localCountry ?? 0,
					members: Math.max( 1, social.members.length )
				} );
				if ( matched ) {
					const flag = frame.payload[0],
						detail = frame.payload[1],
						key = flag === 2 ?
							null :
							frame.opcode === 0xb5bf ?
							detail === 1 ?
								"UIIT_MSG_PARTYMATCH_JOIN_COMPLETE_MASTER" :
								detail === 0 ?
								"UIIT_MSG_PARTYERR_CREATE_PARTY_REFUSED" :
								"UIIT_MSG_PARTYMATCH_JOIN_NOREPLY" :
							null;
					if ( flag === 2 ) {
						const notice = constantNativeNotice( 2, detail! );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					} else if ( key ) {
						notices = [ ...notices.slice( -99 ), { sequence: ++noticeSequence, key, value: 0 } ];
					}
					if ( frame.opcode === 0xb5bf && !matched.pending && matched.auto.length ) {
						const [id, ...auto] = matched.auto;
						const continuation = partyMatchRequest( { ...matched, auto }, {
							kind: "party-match-join",
							id: id!
						}, now );
						if ( continuation.frame ) send( continuation.frame );
						partyMatching = continuation.state;
					} else partyMatching = matched;
					dirty = true;
					return true;
				}
				if ( academy ) {
					const next = academyPacket( academy, frame );
					if ( next ) {
						// 774C64/69: an authoritative notice update also shows child 23.
						if ( frame.opcode === 0x3ac5 && frame.payload[0] === 7 ) {
							notices = [ ...notices.slice( -99 ), {
								key: "UIIT_MSG_TC_COMMON_KNOW_REMIND_UPDATE",
								value: 0,
								notificationBanner: true,
								bannerOnly: true,
								sequence: ++noticeSequence
							} ];
						}
						// 769C6B / 769DA3: matching failures use academy category 1D.
						if ( (frame.opcode === 0xb701 || frame.opcode === 0xb592) && frame.payload[0] === 2 ) {
							const notice = constantNativeNotice( 0x1d, frame.payload[1]! );
							if ( notice ) {
								notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
							}
						}
						// 774D6E: membership-seed refusal uses the same academy dispatcher.
						if ( frame.opcode === 0x3ac5 && frame.payload[0] === 10 && frame.payload[1] === 2 ) {
							const notice = constantNativeNotice( 0x1d, frame.payload[2]! );
							if ( notice ) {
								notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
							}
						}
						academy = next;
						dirty = true;
						return true;
					}
				}
				if ( frame.opcode === 0x36c7 ) {
					const nextRecords = parseAbnormalSnapshot( frame.payload, now );
					for ( const notice of abnormalSnapshotNotices( abnormalMask, nextRecords ) ) {
						notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					}
					abnormalRecords = nextRecords;
					abnormalMask = nextRecords.reduce( ( bits, record ) => bits | (2 ** record.bit), 0 ) >>> 0;
					guide = queueGuide( guide, guideAbnormalEvent( frame.payload ) );
					potionFacts = { ...potionFacts, abnormal: abnormalMask };
					entryVitals = { ...entryVitals, abnormal: abnormalMask };
					if ( localGid ) combat.seed( localGid, { abnormal: abnormalMask } );
					dirty = true;
					return false;
				}
				if ( paramJobs.receive( frame, now ) ) {
					dirty = true;
					return true;
				}
				const windowUpdate = cosTimerPacket( frame, now );
				if ( windowUpdate ) {
					const id = windowUpdate.kind === "remove" ?
						windowUpdate.itemRefObjId :
						windowUpdate.timer.itemRefObjId;
					const kept = cosWindows.filter( row => row.itemRefObjId !== id );
					if ( windowUpdate.kind === "remove" ) cosWindows = kept;
					else if ( cosItemRefs2.has( id ) ) {
						if ( kept.length >= 8 ) throw Error( "COS window capacity" );
						cosWindows = [ ...kept, windowUpdate.timer ];
					}
					dirty = true;
					return true;
				}
				const fortressNext = fortressPacket( fortress, frame );
				if ( fortressNext ) {
					musicMode = fortressMusicMode( musicMode, fortress, fortressNext, frame.payload[0]! );
					fortress = fortressNext;
					dirty = true;
				}
				const notice = restrictionNotice( frame.opcode, frame.payload ) ??
					uniqueNotice( frame.opcode, frame.payload, uniqueRefs ) ??
					fortressNotice( frame.opcode, frame.payload ) ?? serverNotification( frame.opcode, frame.payload );
				if ( notice ) {
					notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					if ( frame.opcode === 0x3667 && frame.payload[0] === 7 ) chat.receive( frame, localGid );
					dirty = true;
					return false;
				}
				if ( fortressNext ) return true;
				if ( frame.opcode === 0x3508 && frame.payload[4] === 4 ) {
					const p = frame.payload;
					if ( p.length !== 7 ) throw Error( "Invalid COS satiety update" );
					const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
					const gid = v.getUint32( 0, true ), satiety = v.getUint16( 5, true );
					if ( !gid || satiety > 10000 ) throw Error( "Invalid COS satiety value" );
					const record = cosRecords.get( gid );
					if ( record?.band === 3 ) {
						cosRecords.set( gid, { ...record, satiety } );
						dirty = true;
					}
					return true;
				}
				const feedbackResult = feedback.receive(
					frame.opcode,
					frame.payload,
					localGid,
					frame.opcode === 0x3508 && frame.payload.length >= 4 ?
						cosRecords.get(
							new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength )
								.getUint32( 0, true )
						) :
						undefined
				);
				if ( feedbackResult ) {
					if ( feedbackResult.messages?.length ) {
						notices = [
							...notices,
							...feedbackResult.messages.map( row => ({ ...row, sequence: ++noticeSequence }) )
						].slice( -100 );
					}
					if ( feedbackResult.level !== undefined ) {
						guide = queueGuide( guide, guideLevelEvents( progression.level, feedbackResult.level ) );
					}
					if ( feedbackResult.level !== undefined ) {
						progression = {
							...progression,
							level: feedbackResult.level,
							maxLevel: Math.max( progression.maxLevel ?? progression.level ?? 1, feedbackResult.level ),
							experience: feedbackResult.experience,
							skillExperience: feedbackResult.skillExperience,
							...(feedbackResult.statPoints !== undefined ?
								{ statPoints: feedbackResult.statPoints } :
								{})
						};
					}
					if ( feedbackResult.cos ) {
						const previous = cosRecords.get( feedbackResult.cos.gid );
						if ( previous ) cosRecords.set( previous.gid, { ...previous, ...feedbackResult.cos } );
					}
					for ( const event of feedbackResult.events ) publishFeedback( event );
					dirty = true;
					return frame.opcode === 0x30b3;
				}
				if ( frame.opcode === 0xb75d ) {
					const gathering = quests.state().questGathering;
					quests.receive( frame, now );
					// 766950 reports failure only when the cancelled identity owns the row.
					const key = frame.payload[0] === 2 ?
						"UIIT_STT_ERR_COMMON_NOT_ACCEPT" :
						gathering && !quests.state().questGathering ?
						"UIIT_MSG_QUEST_GET_ITEM_FAILURE" :
						undefined;
					if ( key ) {
						notices = [ ...notices.slice( -99 ), { key, value: 0, sequence: ++noticeSequence } ];
					}
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb29a || frame.opcode === 0xb1eb ) {
					quests.receive( frame ); // Decode and settle the transaction before presentation.
					const p = frame.payload;
					if ( frame.opcode === 0xb29a && p[0] === 1 ) play( "SND_QUEST", now );
					// Native 75c3d0 displays only refusal 4; all other refusal codes are silent.
					if ( frame.opcode === 0xb1eb && p[0] === 2 && p[1] === 4 ) {
						notices = [ ...notices.slice( -99 ), {
							key: "UIIT_MSG_SR_ABORT_QUEST_ERROR_NOT_ALLOWED",
							value: 0,
							sequence: ++noticeSequence
						} ];
					}
					dirty = true;
					return true;
				}
				// Audio is consumed here; retain the packet for notice/VFX consumers.
				if ( frame.opcode === 0x36bf ) {
					const p = frame.payload;
					if ( p.length < 2 ) throw Error( "Invalid talk notice" );
					const length = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 0, true );
					if ( p.length !== length + 2 ) throw Error( "Invalid talk notice length" );
					const key = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 2 ) );
					if ( npcConversation.interactionLocked() ) {
						if ( key === previousLockedQuestNotice ) return true;
						previousLockedQuestNotice = key;
					}
					if ( key === "SN_TALK_COMMON_END" ) play( "SND_QUEST_END", now );
					notices = [ ...notices.slice( -99 ), {
						key,
						value: 0,
						sequence: ++noticeSequence,
						questBanner: true,
						bannerOnly: true
					} ];
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0x36b0 ) {
					const p = frame.payload;
					if ( p.length !== 4 ) throw Error( "Invalid level-up effect" );
					const gid = new DataView( p.buffer, p.byteOffset, 4 ).getUint32( 0, true );
					if ( gid !== 0 ) {
						publishFeedback( { kind: "level-up", gid } );
						if ( gid === localGid || cosRecords.has( gid ) ) play( "SND_LEVUP", now );
					}
					return true;
				}
				const nextSocial = socialPacket( social, frame, { country: localCountry } );
				if ( nextSocial ) {
					if ( nextSocial.members.length && !social.members.length ) guide = queueGuide( guide, [ 10 ] );
					if ( nextSocial.notice ) {
						notices = [ ...notices.slice( -99 ), { ...nextSocial.notice, sequence: ++noticeSequence } ];
					}
					social = withMemberCountries(
						nextSocial.notice ? { ...nextSocial, notice: undefined } : nextSocial
					);
					if (
						social.invitation &&
						((social.invitation.type === 1 && !options.exchangeRequests) ||
							([ 2, 3 ].includes( social.invitation.type ) && !options.partyInvites))
					) {
						send( socialRequest( social, { kind: "social-consent", accept: false, automatic: true } ) );
						social = { ...social, invitation: null };
					}
					dirty = true;
					return true;
				}
				if ( frame.opcode === SKILL_WITHDRAWAL_RESPONSE || frame.opcode === MASTERY_WITHDRAWAL_RESPONSE ) {
					if ( !training.accepts( frame.opcode ) ) return true;
					const payload = frame.payload;
					if ( payload[0] === 2 && payload.length === 2 ) {
						training.receipt( frame.opcode );
						const notice = constantNativeNotice( 8, payload[1]! );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					} else {
						const mastery = frame.opcode === MASTERY_WITHDRAWAL_RESPONSE;
						if ( payload[0] !== 1 || payload.length !== (mastery ? 6 : 5) ) {
							throw Error( "Invalid restoration receipt" );
						}
						const id = new DataView( payload.buffer, payload.byteOffset, payload.byteLength ).getUint32(
							1,
							true
						);
						if ( !training.accepts( frame.opcode, id ) ) throw Error( "Mismatched restoration receipt" );
						if ( mastery ) {
							if ( !progression.masteries.some( row => row.id === id && row.level > payload[5]! ) ) {
								throw Error( "Invalid restored mastery" );
							}
							progression = {
								...progression,
								masteries: progression.masteries.map( row =>
									row.id === id ? { ...row, level: payload[5]! } : row
								)
							};
						} else {
							const nextBindings = withdrawalSkillBindings( bindings, catalog, id );
							for ( const slot of nextBindings.quickSlots ) {
								const previous = bindings.quickSlots.find( row => row.slot === slot.slot );
								if ( previous?.kind !== slot.kind || previous.payload !== slot.payload ) {
									send( quickSlotPacket( slot ) );
								}
							}
							bindings = nextBindings;
						}
						training.receipt( frame.opcode );
					}
					dirty = true;
					return true;
				}
				const nextProgression = progressionPacket( progression, frame.opcode, frame.payload );
				// 30B3 type 1 with its notify byte set prints the gain before it
				// stores the balance (CPSMission_OnPointUpdate30B3 case 0).
				if (
					nextProgression && frame.opcode === 0x30b3 && frame.payload[0] === 1 && frame.payload[9] !== 0 &&
					progression.gold !== undefined && nextProgression.gold !== undefined
				) {
					const gain = BigInt( nextProgression.gold ) - BigInt( progression.gold );
					if ( gain > 0n ) {
						notices = [ ...notices.slice( -99 ), {
							key: "UIIT_MSG_STATE_GAIN_GOLD",
							value: Number( gain > 0x7fffffffn ? 0x7fffffffn : gain ),
							nativeType: 1,
							sequence: ++noticeSequence
						} ];
					}
				}
				if ( nextProgression ) {
					if ( frame.opcode === 0xb165 && frame.payload[0] === 2 ) {
						const notice = constantNativeNotice( 7, frame.payload[1]! );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					}
					if ( frame.opcode === 0xb165 ) {
						training.receipt(
							frame.opcode,
							frame.payload[0] === 1 ?
								new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength )
									.getUint32( 1, true ) :
								undefined
						);
					}
					progression = nextProgression;
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb2cb ) {
					const p = frame.payload;
					if ( p[0] === 2 && p.length === 2 ) {
						training.receipt( frame.opcode );
						const notice = constantNativeNotice( 5, p[1]! );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
						progression = { ...progression, error: undefined };
						dirty = true;
						return true;
					}
					if ( p[0] !== 1 || p.length !== 5 ) throw new Error( "Invalid skill learn response" );
					const id = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true ),
						group = skillGroups.get( id );
					if ( group === undefined ) throw new Error( "Missing learned skill reference authority" );
					const replaced = new Set( bindings.skills.filter( old => {
						const ref = skillGroups.get( old );
						return ref !== undefined && ref.group === group.group && ref.level + 1 === group.level;
					} ) );
					// 75BB20 -> 67AD00 -> 5731F0 -> 572E00: upgrades save every
					// affected slot, including the extended bar, through 7541.
					for ( const row of bindings.quickSlots ) {
						if ( row.kind === 0x49 && replaced.has( row.payload ) ) {
							send( quickSlotPacket( { ...row, payload: id } ) );
						}
					}
					bindings = {
						skills: [ ...bindings.skills.filter( old => old !== id && !replaced.has( old ) ), id ],
						quickSlots: bindings.quickSlots.map( row =>
							row.kind === 0x49 && replaced.has( row.payload ) ? { ...row, payload: id } : row
						)
					};
					training.receipt( frame.opcode, id );
					progression = { ...progression, error: undefined };
					dirty = true;
					return true;
				}
				const env = environmentPacket( environment, frame.opcode, frame.payload );
				if ( env ) {
					environment = env.state;
					if ( env.notice ) {
						notices = [ ...notices.slice( -99 ), {
							key: env.notice,
							value: 0,
							sequence: ++noticeSequence
						} ];
					}
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0x31ad || frame.opcode === 0x32a6 ) {
					worldClock = decodeWorldClock( frame.payload, frame.opcode === 0x32a6 ? 4 : 0, now );
					dirty = true;
					if ( frame.opcode === 0x31ad ) return true;
				}
				if (
					npcConversation.receive( frame ) || quests.receive( frame, now ) ||
					chat.receive( frame, localGid, chatSender?.name )
				) {
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0x3158 ) {
					const record = decodeCosRecord( frame.payload, cosRefs, cosItemRefs );
					if ( !record ) return false;
					if ( !cosRecords.has( record.gid ) && cosRecords.size >= 64 ) {
						throw new Error( "COS record capacity exceeded" );
					}
					inventory.bindCompanion( record );
					cosRecords.set( record.gid, record );
					if ( record.band === 1 || record.band === 2 ) activeCos = record;
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb4b5 ) {
					const payload = frame.payload;
					if ( payload[0] === 1 ) {
						if ( payload.length !== 10 || payload[5]! > 1 ) throw Error( "Invalid COS ride result" );
						return false;
					}
					if ( payload[0] !== 2 || payload.length !== 2 ) throw Error( "Invalid COS ride result" );
					const notice = constantNativeNotice( 14, payload[1]! );
					if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb56c ) {
					const payload = frame.payload;
					if (
						(payload[0] !== 1 && payload[0] !== 2) ||
						payload.length !== (payload[0] === 2 ? 2 : 1)
					) throw Error( "Invalid COS cancellation result" );
					if ( payload[0] === 2 ) {
						const notice = constantNativeNotice( 12, payload[1]! );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					}
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb618 ) {
					// CPSMission_OnCosCleanupResponseB618 (7782A0): 1 is success, and
					// the despawn retires the actor; otherwise one notice byte in
					// category 12.
					const payload = frame.payload;
					if (
						(payload[0] !== 1 && payload[0] !== 2) ||
						payload.length !== (payload[0] === 2 ? 2 : 1)
					) throw Error( "Invalid COS cleanup result" );
					if ( payload[0] === 2 ) {
						const notice = constantNativeNotice( 12, payload[1]! );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					}
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb05b ) {
					const result = cosBehaviorResult( frame.payload );
					if ( result.kind === "rejected" ) {
						const notice = constantNativeNotice( 12, result.code );
						if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
						cosError = null;
						dirty = true;
						return true;
					}
					const record = cosRecords.get( result.gid );
					if ( record ) {
						const next = applyCosBehavior( record, result );
						cosRecords.set( result.gid, next );
						cosError = null;
						dirty = true;
					}
					return true;
				}
				if ( frame.opcode === 0xb69e ) {
					const p = frame.payload;
					if ( p.length < 2 ) throw new Error( "Truncated COS result" );
					const at = p[0] === 2 ? 3 : 2;
					if ( p.length < at + 4 ) throw new Error( "Truncated COS result identity" );
					// 74FD2E..74FD40: zero failure code discards the remaining
					// packet before selector-specific reads (including pickup GID).
					if ( p[0] === 2 && p[2] === 0 ) return true;
					if ( p.length !== at + 4 + (p[1] === 8 ? 4 : 0) ) throw new Error( "Invalid COS result" );
					const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
					cosResult = {
						subtype: p[0]!,
						selector: p[1]!,
						result: p[0] === 2 ? p[2] : undefined,
						gid: v.getUint32( at, true ),
						itemGid: p[1] === 8 ? v.getUint32( at + 4, true ) : undefined
					};
					if ( cosPickup.result( cosResult, now ) ) {
						const record = cosRecords.get( cosResult.gid );
						if ( record && !record.dead && record.hp && record.commandMode !== undefined ) {
							sendFrame( cosBehaviorRequest( record, record.commandMode & ~0x80 ) );
						}
					}
					dirty = true;
					return true;
				}
				if (
					frame.opcode === 0x3122 && frame.payload.length === 6 && frame.payload[4] === 0 &&
					(frame.payload[5] === 1 || frame.payload[5] === 2) &&
					new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true ) === localGid
				) {
					movement.life( frame.payload[5] as 1 | 2, now );
					dirty = true;
				}
				if (
					frame.opcode === 0x3122 && frame.payload.length === 6 && frame.payload[4] === 1 &&
					new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true ) === localGid
				) {
					movement.mode( frame.payload[5]!, now );
					dirty = true;
				}
				if (
					frame.opcode === 0xb738 && frame.payload.length >= 4 &&
					new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true ) === localGid
				) {
					movement.native( frame.payload, now, localGid );
					if ( frame.payload[4] === 0 ) selectionDecal = null;
					dirty = true;
					return true;
				}
				if ( frame.opcode === 10 ) {
					movement.receive( frame.payload, now, localGid );
					dirty = true;
					return true;
				}
				// A despawn leaves kind-3 windows alone: retail erases one only on the
				// 0x3691 zero pair or the 0x3369/0x366A reset sweep (6E6270).
				if ( frame.opcode === 0x36ab && frame.payload.length === 4 ) {
					const gid = new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true );
					cosRecords.delete( gid );
					if ( selectionDecal?.kind === "target" && selectionDecal.gid === gid ) selectionDecal = null;
					if ( activeCos?.gid === gid ) {
						activeCos = undefined;
						cosResult = undefined;
						cosError = null;
					}
					targeting.remove( gid );
					combat.remove( gid );
					if ( skillPress.queued()?.command.gid === gid ) skillPress.cancel();
					skillPress.targetGone( gid );
					dirty = true;
					return false;
				}
				if (
					frame.opcode === 0xb06d && frame.payload[0] === 1 &&
					(frame.payload[1] === 0x1a || frame.payload[1] === 0x1b)
				) {
					if ( frame.payload.length !== 8 ) throw Error( "Invalid COS transfer result" );
					const gid = new DataView( frame.payload.buffer, frame.payload.byteOffset, 8 ).getUint32( 2, true ),
						record = cosRecords.get( gid );
					if ( !record ) throw Error( "Absent COS transfer owner" );
					const next = inventory.cosTransferred( record, frame.payload, cosItemCaps );
					cosRecords.set( gid, next );
					dirty = true;
					return true;
				}
				if (
					frame.opcode === 0xb06d && frame.payload[0] === 1 &&
					(frame.payload[1] === 0x11 || frame.payload[1] === 0x12)
				) {
					if ( frame.payload.length < 7 ) throw Error( "Invalid COS ground result" );
					const gid = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength )
							.getUint32( 2, true ),
						record = cosRecords.get( gid );
					if ( !record ) throw Error( "Absent COS ground owner" );
					const next = cosGroundResult( record, frame.payload, cosItemRefs );
					if ( frame.payload[1] !== 0x11 || !cosPickup.receipt( gid ) ) {
						inventory.cosGrounded( frame.payload );
					}
					cosRecords.set( gid, next );
					dirty = true;
					return true;
				}
				if ( frame.opcode === 0xb06d && frame.payload[0] === 1 && frame.payload[1] === 0x10 ) {
					if ( frame.payload.length !== 10 ) throw Error( "Invalid COS container result" );
					const gid = new DataView( frame.payload.buffer, frame.payload.byteOffset, 10 ).getUint32( 2, true ),
						record = cosRecords.get( gid );
					if ( !record ) throw Error( "COS container result references absent record" );
					const next = cosContainerResult( record, frame.payload, cosItemCaps );
					inventory.cosMoved( frame.payload );
					cosRecords.set( gid, next );
					dirty = true;
					return true;
				}
				if ( frame.opcode === 12 ) {
					const r = commerceJson( frame.payload );
					if ( r.cosGid !== undefined ) {
						const record = cosRecords.get( commerceInteger( r.cosGid, 0xffffffff, 1 ) );
						if ( !record ) throw Error( "COS shop snapshot references absent record" );
						const next = inventory.cosShopSnapshot( record, frame.payload );
						cosRecords.set( record.gid, next );
						dirty = true;
						return true;
					}
				}
				if ( frame.opcode === 0xb06d && frame.payload[0] === 1 && frame.payload[1] === 19 ) {
					inventory.cosPurchase( frame.payload );
					dirty = true;
					return true;
				}
				const sale = saleResult( frame.opcode, frame.payload );
				if ( sale?.cosGid !== undefined ) {
					const cos = cosRecords.get( sale.cosGid );
					if ( !cos?.inventory ) throw Error( "Sale result references absent COS inventory" );
					const next = soldInventory( cos.inventory, sale.slot, sale.quantity );
					inventory.cosSold( cos.gid, sale.slot, sale.quantity, sale.context );
					cosRecords.set( cos.gid, { ...cos, inventory: next } );
					dirty = true;
					return true;
				}
				// 7641D0 tells the local player its skin changed (chat kind 5); the
				// entity owner applies the skin itself.
				if (
					frame.opcode === 0x323a && frame.payload.length >= 8 &&
					new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true ) === localGid
				) {
					notices = [ ...notices.slice( -99 ), {
						key: "UIIT_MSG_CHAR_SKIN_CHANGE_SUCCESS",
						value: 0,
						sequence: ++noticeSequence
					} ];
					dirty = true;
				}
				if ( frame.opcode === 0xb2dd ) {
					const p = frame.payload;
					if ( p[0] === 1 && p.length === 1 ) {
						notices = [ ...notices.slice( -99 ), {
							key: "UIIT_MSG_TRANSITION_CANCEL_RESULT",
							value: 0,
							sequence: ++noticeSequence
						} ];
						dirty = true;
						return true;
					}
					if ( p[0] === 2 && p.length === 2 ) return true;
					throw Error( "Invalid return cancellation response" );
				}
				const previousTarget = targeting.state();
				const used = frame.opcode === 0xb5bd && frame.payload[0] === 1 ?
					inventory.state().inventory.find( i => i.slot === frame.payload[1] ) :
					undefined;
				const cast = used ? returnScrollCast( used, now ) : undefined;
				const mallRequest = inventory.state().itemMall?.pending === true;
				const room = storage.state();
				if ( frame.opcode === 0xb06d && room ) {
					const next = inventory.storageSettle( room, frame.payload, cosItemCaps );
					if ( next ) {
						storage.apply( next );
						dirty = true;
						return true;
					}
				}
				const item = inventory.receive( frame.opcode, frame.payload, now, {
						country: localCountry,
						abnormal: potionFacts.abnormal
					} ),
					target = targeting.receive( frame.opcode, frame.payload ),
					fight = combat.receive( frame.opcode, frame.payload, now );
				// B245 [2, code]: the server refused the targeted command at the
				// press, before touching movement (an empty MP pool is 0x3004).
				if ( frame.opcode === 0xb245 && frame.payload[0] === 2 ) movement.castRefused( now );
				if ( item && cast ) returnScroll = cast;
				// A spent warehouse ticket opens the room on the player's own gid.
				if ( item && used && localGid && isWarehouseTicket( used.typeFlags ) ) {
					storage.open( localGid );
					dirty = true;
				}
				// A pickup into the gold slot (0xFE) prints the whole heap: pickup types
				// 6/0x1C resolve to window 0x46 with slot 0xFE (7653D0), and that branch
				// of CPSMission_ApplyInventoryOperation reads the u32 and prints
				// UIIT_MSG_STATE_GAIN_GOLD (7571E1). The balance itself rides 0x30B3.
				if (
					item && frame.opcode === 0xb06d && frame.payload.length === 7 && frame.payload[0] === 1 &&
					frame.payload[1] === 6 && frame.payload[2] === 254
				) {
					notices = [ ...notices.slice( -99 ), {
						key: "UIIT_MSG_STATE_GAIN_GOLD",
						value: new DataView( frame.payload.buffer, frame.payload.byteOffset, 7 ).getUint32( 3, true ),
						nativeType: 1,
						sequence: ++noticeSequence
					} ];
				}
				if (
					frame.opcode === 0x3122 && frame.payload.length === 6 && frame.payload[4] === 11 &&
					new DataView( frame.payload.buffer, frame.payload.byteOffset, 6 ).getUint32( 0, true ) === localGid
				) {
					teleportMode = frame.payload[5]!;
					if ( teleportMode === 0 ) {
						if ( returnScroll ) {
							notices = [ ...notices.slice( -99 ), {
								key: "UIIT_MSG_TRANSITION_CANCEL_RESULT",
								value: 0,
								sequence: ++noticeSequence
							} ];
						}
						returnScroll = undefined;
					}
					dirty = true;
				}
				const answer = jobGuildAnswer( frame, job );
				if ( answer ) {
					job = answer.job;
					if ( answer.notice ) {
						notices = [ ...notices.slice( -99 ), { ...answer.notice, sequence: ++noticeSequence } ];
					}
					dirty = true;
					return true;
				}
				// The suit move's answer waits for the dress bar (jobdress.go).
				const dress = jobDressSeconds( frame, localGid );
				if ( dress !== null ) inventory.holdForDress( now + dress * 1000 + JOB_DRESS_ANSWER_GRACE_MS );
				if ( frame.opcode === 0xb341 ) {
					const p = frame.payload;
					if ( !p.length || p.length !== (p[0] === 1 ? 1 : 2) ) throw Error( "Invalid Berserk response" );
					const notice = p[0] === 1 ? null : constantNativeNotice( 26, p[1]! );
					if ( notice ) notices = [ ...notices.slice( -99 ), { ...notice, sequence: ++noticeSequence } ];
					dirty = true;
					return true;
				}
				const refusal = recallAppointmentNotice( frame.opcode, frame.payload ) ??
					targetNotice( frame.opcode, frame.payload ) ?? portalNotice( frame.opcode, frame.payload ) ??
					repairNotice( frame.opcode, frame.payload ) ??
					inventoryNotice( frame.opcode, frame.payload, localCountry, mallRequest ) ??
					skillNotice( frame.opcode, frame.payload, localCountry, fortressActive( fortress ) );
				if ( refusal ) notices = [ ...notices.slice( -99 ), { ...refusal, sequence: ++noticeSequence } ];
				if ( target ) {
					const selection = targeting.state();
					if (
						frame.opcode === 0xb45a && frame.payload[0] === 1 && previousTarget.targetPending &&
						!selection.targetPending && ((selection.targetCapabilities ?? 0) & 0xc3)
					) npcConversation.select( selection.target );
					else if ( !selection.target ) npcConversation.clear();
				}
				if ( item ) {
					guide = queueGuide(
						guide,
						guideInventoryEvents(
							inventory.state().inventory,
							inventory.state().equipmentSlotCount ?? 0,
							guideSummons
						)
					);
				}
				if ( frame.opcode === 0x30e3 ) dirty = true;
				if ( item || target || fight || frame.opcode === 0xb495 || frame.opcode === 0xb20d ) {
					dirty = true;
					return true;
				}
				return false;
			} finally {
				const inventoryAfter = inventory.state().inventory, moves = inventory.takeBindingMoves();
				if ( inventoryBefore !== inventoryAfter || moves.length ) {
					const next = reconcileQuickslotInventory(
						bindings.quickSlots,
						inventoryBefore,
						inventoryAfter,
						moves,
						{ country: localCountry, progression, maxHp: potionFacts.maxHp, maxMp: potionFacts.maxMp },
						frame.opcode === 0xb5bd && frame.payload[0] === 1
					);
					for ( let i = 0; i < next.length; i++ ) {
						if ( next[i] !== bindings.quickSlots[i] ) {
							bindingRepairs.set( next[i]!.slot, next[i]! );
							dirty = true;
						}
					}
					bindings = { ...bindings, quickSlots: next.filter( row => row.kind !== 0 ) };
					flushBindingRepairs();
				}
			}
		},
		/*
================
step

Advance deadlines on the worker clock after queued packets commit and
before take assembles the presentation snapshot.
================
		*/
		step( now: number, local?: EntityState ) {
			flushBindingRepairs();
			if ( moveReservation.holding() && (!local || local.appearanceState?.[0] === 2) ) {
				moveReservation.clear();
				if ( selectionDecal?.kind === "ground" ) selectionDecal = null;
				dirty = true;
			}
			if ( moveReservation.holding() && local && local.appearanceState?.[0] !== 2 ) {
				cancelActionForMovement();
			}
			const sharedOwners = new Set( (social.options & 2) ? social.members.map( member => member.id ) : [] );
			for (
				const frame of cosPickup.step( {
					now,
					local,
					records: cosRecords.values(),
					sharedOwners,
					read: readEntity
				} )
			) {
				sendFrame( frame );
			}
			// A click held through the cast walks as soon as the caster leaves
			// action state 2 (the skill's action window ends or it is cancelled);
			// death forfeits it.
			if (
				moveReservation.holding() &&
				!castMotion.locked( combat.state().casts, localGid, now, !actionSession.released() )
			) {
				const held = moveReservation.take()!;
				if ( !local || local.appearanceState?.[0] === 2 ) {
					// A forfeited click takes its marker with it.
					if ( held.kind === "move" && selectionDecal?.kind === "ground" ) selectionDecal = null;
					dirty = true;
				} else {
					try {
						api.command( held, now, undefined, local );
					} catch ( error ) {
						moveReservation.fail( String( error ) );
						dirty = true;
					}
				}
			}
			// A held skill press goes out when due, through the same command
			// path as a fresh one: aimed at the newest selection, as core.ts
			// aims a fresh press. Death forfeits it, and so does a monster
			// that died while it was held.
			const duePress = skillPress.due( now );
			if ( duePress ) {
				dirty = true;
				const { gid: _held, ...press } = duePress.command, gid = targeting.selectionIntent() || undefined;
				const command = gid ? { ...press, gid } : press;
				const target = gid === undefined ? undefined : readEntity( gid );
				if (
					local && local.appearanceState?.[0] !== 2 &&
					!(target?.kind === "monster" && target.appearanceState?.[0] === 2)
				) {
					try {
						api.command( command, now, target, local );
					} catch {
						// The press lost its target or skill while held: it lapses.
					}
				}
			}
			if ( quests.step( now ) ) dirty = true;
			if ( approach.phase === "moving" ) {
				const approachingTarget = approach.target;
				const m = movement.state();
				if ( !local || local.appearanceState?.[0] === 2 ) {
					approach = interactionApproachTransition( approach, { kind: "cancel" } );
				} else if (
					!targeting.state().targetPending &&
					(m.pose && !interactionApproach( m.pose, approachingTarget ) || !m.moving && !m.pendingMoves)
				) {
					// 693AD0 via CNavigationDeadreckon_OnTick: the pending select is
					// dispatched once in reach or when the walk ends, wherever that is;
					// the server's 4A8E10 range check decides.
					selectEntity( approachingTarget, now );
					approach = interactionApproachTransition( approach, { kind: "arrived" } );
					dirty = true;
				}
			}
			if ( npcConversation.step( now ) ) dirty = true;
			const conversation = npcConversation.state();
			if ( conversation.phase !== "closed" && targeting.state().target !== conversation.gid ) {
				npcConversation.clear();
				dirty = true;
			}
			if ( partyMatching.request && now >= partyMatching.request.expires ) {
				const { a, b } = partyMatching.request,
					next = partyMatchRequest( partyMatching, { kind: "party-match-answer", a, b, answer: 2 } );
				if ( next.frame ) send( next.frame );
				partyMatching = next.state;
				dirty = true;
			}

			const vital = combat.state().vitals.find( v => v.gid === localGid );
			potionFacts = {
				...potionFacts,
				alive: !!local && local.appearanceState?.[0] !== 2 && (vital?.hp ?? potionFacts.hp) > 0,
				hp: vital?.hp ?? potionFacts.hp,
				mp: vital?.mp ?? potionFacts.mp,
				abnormal: vital?.abnormal ?? potionFacts.abnormal
			};
			const low = [
				vitalWarning( potionFacts.hp, potionFacts.maxHp ),
				vitalWarning( potionFacts.mp, potionFacts.maxMp )
			];
			for ( const kind of [ 0, 1 ] as const ) {
				if (
					local && low[kind] && !warnings[kind] && options.warningSound &&
					(kind === 0 ? options.hpWarning : options.mpWarning)
				) play( "SND_ALARM", now );
			}
			warnings = local ? low : [ false, false ];
			for ( const kind of [ 0, 1, 2 ] as const ) {
				const entry = autoPotionEntry( [ autoPotion.hp, autoPotion.mp, autoPotion.cure ][kind]! );
				if ( !autoPotionActive( kind, entry, potionFacts ) ) {
					potionDue[kind] = null;
					continue;
				}
				if ( potionDue[kind] !== null && now < potionDue[kind]! ) continue;
				potionDue[kind] = now + autoPotionDelay( autoPotion );
				if ( !autoPotionEligible( kind, entry, potionFacts ) ) continue;
				const binding = bindings.quickSlots.find( row => row.slot === entry.slot ),
					slot = binding ? autoPotionItemSlot( binding, inventory.state().inventory ) : null;
				if (
					slot === null || inventory.state().inventoryPending ||
					!inventory.state().inventory.some( row => row.slot === slot )
				) continue;
				inventory.use( slot, now, { records: [ ...cosRecords.values() ] } );
				dirty = true;
			}
			const combatChanged = combat.step( now ), inventoryChanged = inventory.step( now );
			const trained = training.step( now ), moved = movement.step( now ), targeted = targeting.step( now );
			dirty = combatChanged || inventoryChanged || trained || chat.step( now ) || moved || targeted || dirty;
		},
		/*
================
die
================
		*/
		die( gid: number, now: number ) {
			combat.cancelGuided( gid, now );
			if ( gid === localGid ) {
				movement.life( 2, now );
				selectionDecal = null;
				dirty = true;
			}
		},
		takeDisplacements: combat.takeDisplacements,
		takeCancellations: combat.takeCancellations,
		/*
================
cancelCast
================
		*/
		cancelCast( token: number, now: number ) {
			movement.cancelCast( token, now );
			dirty = true;
		},
		// The local player's live pose is owned by movement, not by its entity
		// row (which only follows displacements). Clipping a local dash from
		// the entity row sent Ghost Walk back toward a stale position.
		/*
================
constrainDisplacement
================
		*/
		constrainDisplacement(
			command: import("@/engine/contracts/gameplay").CastDisplacement,
			entityPose: import("@/engine/contracts/gameplay").Pose,
			now: number
		) {
			const from = command.gid === localGid ? movement.current( now ) ?? entityPose : entityPose;
			return movement.constrainDisplacement( command, from, command.gid === localGid, now );
		},
		guidedArrival: combat.guidedArrival,
		/*
================
displace
================
		*/
		displace( command: import("@/engine/contracts/gameplay").CastDisplacement, now: number ) {
			if ( command.gid === localGid ) {
				const arrival = movement.displace( command, now );
				dirty = true;
				return arrival;
			}
		},
		/*
================
take

Publish only after mutation. Clear the dirty flag once all owner snapshots
have been collected so consumers never observe half of a packet update.
================
		*/
		/*
================
bindReferences

The composition root hands over the world catalog's lookups.
================
		*/
		bindReferences( references: WorldReferences ) {
			worldReferences = references;
			social = withMemberCountries( social );
		},
		/*
================
take

The published plane when something changed since the last take, else null.
================
		*/
		take(): GameplayState | null {
			if ( !dirty ) {
				return null;
			}
			dirty = false;
			const m = movement.state(), i = inventory.state(), c = combat.state();
			const npcState = {
				npcConversation: npcConversation.state(),
				restorationRevision: npcConversation.restorationRevision()
			};
			const cosVital = activeCos ? c.vitals.find( v => v.gid === activeCos!.gid ) : undefined;
			const cos = activeCos ?
				{
					...activeCos,
					hp: cosVital?.hp ?? activeCos.hp,
					mp: cosVital?.mp ?? activeCos.mp,
					dead: (cosVital?.hp ?? activeCos.hp) === 0 || activeCos.dead
				} :
				undefined;
			return {
				...npcState,
				abnormalRecords,
				returnScroll,
				musicMode,
				gmReplies,
				eligibility,
				autoPotion,
				selectionDecal,
				notices,
				partyMatching,
				academy,
				guide,
				paramJobs: paramJobs.state(),
				storage: storage.state(),
				playerModels: localPlayerModels(),
				job,
				cosWindows: cosWindows.filter( row => cosItemRefs2.has( row.itemRefObjId ) ).map( row => ({
					...row,
					reference: cosItemRefs2.get( row.itemRefObjId )!
				}) ),
				...bindings,
				...training.state(),
				social,
				fortress,
				skillCatalog: catalog,
				progression,
				cosRecords: [ ...cosRecords.values() ].map( record => {
					const v = c.vitals.find( row => row.gid === record.gid );
					return {
						...record,
						inventory: record.inventory?.map( inventory.present ),
						hp: v?.hp ?? record.hp,
						mp: v?.mp ?? record.mp,
						satiety: v?.satiety ?? record.satiety
					};
				} ),
				worldClock,
				weather: environment.effective,
				eventGroups: environment.groups,
				...quests.state(),
				chat: chat.state(),
				activeCos: cos,
				cosResult,
				revision: ++revision,
				localGid,
				...m,
				...i,
				...c,
				...targeting.state(),
				...skillPress.state(),
				betaPlayers: betaMap.players(),
				unlimitedItems,
				error: m.error ?? i.error ?? c.error ?? targeting.error() ?? progression.error ??
					training.state().trainingError ?? social.error ?? cosError ?? moveReservation.error() ?? null
			};
		},
		// 0x3369 answers 0x36DD and runs the 0x366A handler (74B880 -> 74B250),
		// whose 685400 -> 6E6270(board, 0) empties every board row, kind 3
		// included; the server re-raises what is still live after the reset.
		// References stay: a re-entry does not resend the item snapshot.
		/*
================
resetWorld

World transfer retires spatial work while retaining character/session data.
================
		*/
		resetWorld() {
			actionSession.clear();
			pickup.clear();
			cosPickup.clear();
			moveReservation.clear();
			quests.clearGathering();
			approach = interactionApproachTransition( approach, { kind: "cancel" } );
			returnScroll = undefined;
			teleportMode = 0;
			npcConversation.clear();
			storage.close();
			cosWindows = [];
			paramJobs.clear();
			const vital = combat.state().vitals.find( row => row.gid === localGid );
			if ( vital ) {
				entryVitals = {
					hp: vital.hp,
					mp: vital.mp,
					maxHp: vital.maxHp,
					maxMp: vital.maxMp,
					abnormal: vital.abnormal
				};
			}
			warnings = [ false, false ];
			potionDue.fill( null );
			potionFacts = { ...potionFacts, alive: false };
			selectionDecal = null;
			social = withoutResurrection( { ...social, invitation: null } );
			movement.clear();
			targeting.clear();
			combat.clear( true );
			skillPress.clear();
			localGid = 0;
			worldClock = undefined;
			environment = entryEnvironment( {} ).state;
			activeCos = undefined;
			cosResult = undefined;
			cosError = null;
			cosRecords.clear();
			dirty = true;
		},
		/*
================
reset
================
		*/
		reset() {
			clearState();
			dirty = true;
		},
		/*
================
dispose
================
		*/
		dispose() {
			clearState();
			training.dispose();
			cosRefs.clear();
			dirty = false;
		}
	};
	return api;
}
