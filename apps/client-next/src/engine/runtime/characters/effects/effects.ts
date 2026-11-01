/*
===========================================================================

effects.ts - skill, buff, item and status visual effects on characters

Owns every effect actor drawn on or around a character: cast stage effects
(CIDecoSkill 8DDDE0 stage command lists), damage effects on victims
(8DB770 -> 8D5440), attached buff visuals, persistent status effects,
external item effects, projectile flights and the hawk. It loads the effect
catalogs through the asset owner, turns gameplay triggers into actors each
frame, and publishes impacts, camera scripts and activations to its owner.

This file predates the formatter; its step() still holds several
lifecycles that belong in separate owners (see the size ledger).

===========================================================================
*/
import {
	createPenguinBolts,
	penguinEffectActors,
	PENGUIN_EFFECT_SOUND
} from "@/engine/foundation/animation/penguin-effects";
import {
	PET_BOND_TRANSFER_SOUND,
	PET_BOND_REVIVE_SOUND,
	PET_ROAR_SOUND,
	PET_SHIELD_SOUND,
	PET_SHIELD_BURST_SOUND
} from "@/engine/foundation/animation/sound-selectors";
import { trimToEffectBudget } from "@/engine/foundation/animation/effect-room";
import {
	WOLF_EFFECT_MAX_PARTS,
	WOLF_EFFECT_SECONDS,
	wolfEffectSound,
	wolfEffectKind,
	wolfEffectParts
} from "@/engine/foundation/animation/wolf-effects";
import { petRoarRadius } from "@/engine/foundation/ui/pet-training";
import { castResultAt } from "@/engine/foundation/gameplay/cast-results";
import { questMarkerEffect } from "@/engine/foundation/gameplay/quest";
import { createPresentationIds } from "@/engine/foundation/animation/presentation-ids";
import { itemEffectClock, type ItemEffectClock } from "@/engine/foundation/animation/item-effect-clock";
import { createHitLights } from "@/engine/foundation/animation/hit-light";
import { advanceProjectileCurve } from "@/engine/foundation/animation/projectile-curve";
import {
	modelAmbientParticles as ambientModelParticles,
	modelParticleAttachment
} from "@/engine/foundation/animation/model-emission";
import { modelAnimationParticles } from "@/engine/foundation/animation/animation-emission";
import { modelAnimationBindings, modelModifierSets } from "@/engine/foundation/animation/model-animation";
import {
	effectScript,
	hitRotation,
	localHitFlash,
	projectNativeCommandRotation
} from "@/engine/foundation/animation/effect-script";
import {
	stepMaterial,
	stageEffectScale,
	stepHwanScale,
	type MaterialScript
} from "@/engine/foundation/animation/effect-material";
import {
	STATUS_DECORATION_MASK,
	createStatusOwner,
	type StatusView
} from "@/engine/foundation/animation/status-presentation";
import type { CharacterActor } from "@/engine/contracts/character";
import { impactSource } from "@/engine/foundation/animation/impact-source";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import { projectileSpace, sampleProjectile, stepHomingProjectile } from "@/engine/foundation/animation/projectile-time";
import {
	projectileBasis,
	movingFade,
	movingTargets,
	radialDestination,
	stepMoving
} from "@/engine/foundation/animation/moving-stage";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { SoundEvent } from "@/engine/contracts/audio";
import type { EffectCatalog, EffectStage, EffectVisual, EffectTrigger } from "@/engine/contracts/effects";
import { importedModelBasis, nativeRootOffset } from "@/engine/foundation/rendering/world-math";
import { SPINE_AIM_BONE } from "@/engine/foundation/animation/spine-aim";
import { createSpineAims } from "./spine-aims";
import type { GameplayState, Pose } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import { characterHeadingYaw, nativeHeadingYaw, radians, type Radians } from "@/engine/foundation/math/angles";
import { hawkInitial, hawkEvent, hawkAnimation, stepHawk, type HawkState } from "@/engine/foundation/animation/hawk";
import { hypot3 } from "@/engine/foundation/math/hypot";
import {
	normalizeRegionXZ,
	regionRelativeXZ,
	worldAudioPosition,
	worldDelta
} from "@/engine/foundation/world/world-space";
import { SYSTEM_CAPTURE_MARK } from "@/engine/contracts/orb";

/*
================
audioPosition

Projects a regional pose into the shared 3D audio listener coordinate frame.
================
*/
function audioPosition( pose: {
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
} ): { readonly x: number; readonly y: number; readonly z: number; } {
	const [x, y, z] = worldAudioPosition( pose );
	return { x, y, z };
}
/*
================
EffectGameplay

Effects consume these state channels. A drawn local pose or predicted cast
does not require copying unrelated inventory windows, guild or quest state.
================
*/
type EffectGameplay = Pick<
	GameplayState,
	| "localGid"
	| "pose"
	| "casts"
	| "vitals"
	| "questMarkers"
	| "inventory"
	| "attachedEffects"
	| "homingProjectiles"
	| "petTraining"
>;
// TID4 6 in the COS type word: the captured quest monster.
const CAPTURED_MONSTER_TYPE = 6 << 11;
// The effect actors drawn at once: cast visuals, state and attached
// effects together. The original keeps each character's decorations in an
// unbounded list (CICharactor_AppendOwnedDecoration 85C020), so this is a
// port budget, sized above a crowd's real load. Reaching it never empties
// the frame: the farthest attached effects give way first
// (trimToEffectBudget); only a cast arriving with every slot taken by
// other casts skips its visuals.
const EFFECT_ACTOR_BUDGET = 2048;
const CROW_EFFECT_LIMIT = 64;
// A one-shot whose model is still not resident this long after its spawn is dropped.
const UNREADY_ONE_SHOT_SECONDS = 15;
// 8DE74C loads [0xC14E6C], -pi/2 as a float, as the fYaw of every AT_TARGET
// mover it hands to CIDecoSkillEffectEntity_Initialize (8D8E30). 8DE6C0 first
// sets the mover's rotation axis byte +0xD4 to 0 (CIDecoStopObj's default,
// 8E1054, is 1: yaw), so CIDecoMoveObj_OnTransformEvent (8E0E20, SetYaw's
// event 3) turns an .efp compound by that angle about X: a pitch that stands
// a shard's long axis upright, not a yaw.
const TARGET_MOVER_PITCH = { axis: "x", angle: Math.fround( -Math.PI / 2 ) } as const;
// The same -pi/2 float is the spawn fYaw only the AT_TARGET caller
// (8DE74C) hands to Initialize; the other stage callers pass the source
// object's yaw ([obj+0x88]). A zero-length mover keeps whichever it was
// given (8D922D; see importedProjectileBasis).
const TARGET_MOVER_SPAWN_YAW = Math.fround( -Math.PI / 2 );
/*
================
targetMoverPoint

One end of an AT_TARGET mover (CIDecoSkill_ExecuteStageCommandList case 3,
8DE6C7..8DE748): the target's position plus the authored offset, added in
world axes with float stores. Unlike a static stage (8D6330), native neither
turns nor scales the offset by the target's root, and samples no bone or
'*' height. The point carries no yaw: the mover is turned only by
TARGET_MOVER_PITCH, whatever the target faces.
================
*/
function targetMoverPoint(
	pose: { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; },
	offset: readonly [number, number, number]
): CharacterActor["pose"] {
	return {
		regionId: pose.regionId,
		x: Math.fround( pose.x + offset[0] ),
		y: Math.fround( pose.y + offset[1] ),
		z: Math.fround( pose.z + offset[2] ),
		yaw: radians( 0 )
	};
}
/*
================
importedProjectileBasis

A flying BSR's world basis: projectileBasis is the native object basis
(right, up, back along the flight), carried into the imported mesh's space.
A zero-length flight has no travel basis; native keeps the spawn yaw.
================
*/
function importedProjectileBasis(
	start: Parameters<typeof projectileBasis>[0],
	end: Parameters<typeof projectileBasis>[1],
	verticalFallback = true,
	spawnYaw?: number
): CharacterActor["effectBasis"] {
	const basis = projectileBasis( start, end, verticalFallback );
	if ( basis ) return importedModelBasis( basis );
	if ( spawnYaw === undefined ) return undefined;
	// 8D8E30 Initialize: 879250's path length is fcomp'ed against 0.0 at
	// 8D921D, and a zero-length path returns at 8D922D before the travel
	// basis write (8D9233), so the mover keeps its caller's fYaw. Only
	// the AT_TARGET caller (8DE74C) gives -pi/2; the other stage callers
	// give the source object's yaw ([obj+0x88]). Build the kept facing's
	// rows with projectileBasis's convention: back = (-sin, 0, cos).
	const f = Math.fround,
		x = f( -Math.sin( spawnYaw ) ),
		z = f( Math.cos( spawnYaw ) ),
		length = f( Math.sqrt( f( x * x + z * z ) ) ),
		inverse = length ? f( 1 / length ) : 0,
		backX = f( x * inverse ),
		backZ = f( z * inverse );
	return importedModelBasis( [ backZ, 0, f( -backX ), 0, 1, 0, backX, 0, backZ ] );
}
/** One decoded stage binding; see stageBinding. */
type StageBinding = {
	readonly bone: string | null;
	readonly keepRotation: boolean;
	readonly addHeight: boolean;
	readonly offset: readonly [number, number, number];
};
/*
================
stageAttachmentBasis

CIDecoSkill_ComputeSocketTransformMatrix 8D9EC0 resolves every attached
stage object, .efp program or .bsr mesh (arrows, weapons), through the same
8D6880 holder matrix, then applies the authored rotation in object space.
Only the asset space differs: compiled BSR vertices are Z-flipped.
================
*/
function stageAttachmentBasis( resource: string ): "native" | "native-bsr" {
	return resource.endsWith( ".efp" ) ? "native" : "native-bsr";
}
/*
================
isCapturedMonster

CICCos_EquipOwnerReferenceAppearance (8554E0): a COS whose type word is band
6 (a quest's captured monster) spawns with SYSTEM_CAPTURE_MARK attached.
================
*/
function isCapturedMonster( entity: import("@/engine/contracts/world").EntityState ): boolean {
	const tid = entity.tidWord ?? 0;
	return entity.kind === "cos" && (tid & 0x7fe) === 0x1c6 && (tid & 0xf800) === CAPTURED_MONSTER_TYPE;
}
/*
================
stageBinding

One of a stage's two 0x18-byte attachment bindings (+0x3C start, +0x54
target): the bone CSkillEffectManager_ParseSkillEffectRecord decoded from its token, the +0x08 / +0x09
flags, and the authored native model-space offset.
================
*/
function stageBinding( stage: EffectStage, target: boolean ): StageBinding {
	return target ?
		{
			bone: stage.targetBone ?? null,
			keepRotation: stage.targetKeepRotation ?? true,
			addHeight: stage.targetAddHeight ?? false,
			offset: stage.targetOffset ?? [ 0, 0, 0 ]
		} :
		{
			bone: stage.bone,
			keepRotation: stage.keepRotation ?? true,
			addHeight: stage.addHeight ?? false,
			offset: stage.offset
		};
}
/*
================
stageAttachment

The renderer attachment for one binding on a holder, resolved as 8D6880
does: a named bone or the holder root, the native offset turned by the
root (characters.ts transformFor), the '*' height added to that offset so
it scales with the holder like the native +0x220 height, and the +0x08
rotation flag.
================
*/
function stageAttachment(
	holder: { readonly gid: number; readonly height?: number; },
	binding: StageBinding,
	resource: string
) {
	if ( binding.addHeight && holder.height === undefined ) throw Error( "Missing native effect anchor height" );
	const [x, y, z] = binding.offset;
	return {
		gid: holder.gid,
		bone: binding.bone ?? "",
		root: !binding.bone,
		basis: stageAttachmentBasis( resource ),
		offset: [ x, y + (binding.addHeight ? holder.height! : 0), z ] as const,
		...(binding.keepRotation ? {} : { keepRotation: false })
	};
}
// A trade bandit's type word without the service bit (861B00, as
// appearance-lookup.ts); 861720 attaches its skill's imbue as a lasting deco.
const TRADE_THIEF_WORD = 0x10c6, TRADE_HUNTER_WORD = 0x18c6;
// The client-side token that keys a bandit's imbue deco (server tokens are
// positive; -1 marks the status and hwan rows).
const TRADE_IMBUE_TOKEN = -3;

/*
================
createCharacterEffects
Creates the effect owner. The caller supplies the asset owner, sound
sink, presentation randomness and item reference lookup.
================
*/
export function createCharacterEffects(
	assets: AssetOwner,
	origin: string,
	play: ( event: SoundEvent ) => void,
	random: PresentationRandom,
	itemReference?: (
		id: number
	) => {
		codename: string;
		dropModelPath?: string;
		model?: { glb: string; clips: readonly string[]; clipLoop: boolean; };
	} | undefined,
	allocate: () => number = createPresentationIds(),
	status = createStatusOwner()
) {
	const hitLights = createHitLights();
	let namedCatalog: EffectCatalog | null = null;
	const external = new Map<
		number,
		{
			event: import("@/engine/contracts/orb").ItemEffectFeedback;
			clock: ItemEffectClock;
			hostGid: number;
			children: {
				serial: number;
				at: number;
				stages: Set<number>;
				actors: { actor: CharacterActor; at: number; life: number; }[];
			}[];
		}
	>();
	let externalSerial = 0;
	const crowEffects: {
		pose: CharacterActor["pose"];
		gids: number[];
		cue: number;
		book: number;
		started?: number;
	}[] = [];
	let catalog: EffectCatalog | null = null,
		models:
			| Record<string, {
				glb: string;
				clips: string[];
				clipLoop: boolean;
				particleModifiers?: unknown;
				states?: Readonly<Record<number, import("@/engine/contracts/effects").HawkAnimation>>;
			}>
			| null = null;
	const animationModels = new Map<
		string,
		{
			bindings: ReturnType<typeof modelAnimationBindings>;
			selectors: ReturnType<typeof modelModifierSets>;
			particles: ReturnType<typeof modelAnimationParticles>;
			paths: string[];
		}
	>();
	let weapons: Record<string, string> = {};
	const weaponOwners = new Map<number, number>(), hiddenWeapons = new Set<number>();
	const spineAims = createSpineAims();
	let job: {
			id: number;
			kind: "records" | "models" | "named";
		} | null = null,
		failure: string | null = null,
		disposed = false;
	let retryAt = 0, attempts = 0, loadFailure: string | null = null;
	const impactEvents: import("@/engine/contracts/effects").EffectImpactEvent[] = [];
	const hawkImpacts: import("@/engine/contracts/effects").HawkImpact[] = [];
	const cameraEvents: import("@/engine/contracts/camera-script").CameraScript[] = [];
	const activations: {
		gid: CharacterActor["gid"];
		skill: import("@/engine/foundation/gameplay/attached-effects").AttachedEffect["skill"];
		at: number;
	}[] = [];
	/*
	================
	cameraArrival
	Queues a flight's arrival camera script unless the camera's target is
	another player.
	================
	*/
	function cameraArrival( visual: EffectVisual, at: number, entities: ReadonlyMap<number, EntityState> ) {
		if ( visual.camera && entities.get( visual.camera.target )?.kind !== "player" ) {
			cameraEvents.push( { ...visual.camera.script, atMs: Math.trunc( at * 1000 ) } );
		}
	}
	const impactIndexes = new Map<number, ReadonlyMap<string, number>>();
	const seen = new Set<string>(), active = new Map<number, EffectVisual>(), unsupported = new Set<string>();
	const pendingTriggers = new Map<string, EffectTrigger>();
	// Effects the budget turned away since the last takeBudget, and the
	// actors the last step drew: the frame probe's evidence that a crowd
	// stays under EFFECT_ACTOR_BUDGET.
	let budgetGaveWay = 0, budgetActors = 0;
	/*
	================
	adoptCast

	The server's cast takes over its press's prediction: the windup visuals,
	callbacks and weapon hiding the prediction started continue under the
	server's token instead of retiring with the prediction.
	================
	*/
	function adoptCast( from: number, cast: import("@/engine/contracts/gameplay").CastState ) {
		const prefix = `${from}:`;
		for ( const [gid, visual] of active ) {
			if ( visual.token === from ) active.set( gid, { ...visual, token: cast.token } );
		}
		for ( const key of [ ...seen ] ) {
			if ( key.startsWith( prefix ) ) {
				seen.delete( key );
				seen.add( `${cast.token}:${key.slice( prefix.length )}` );
			}
		}
		for ( const [key, trigger] of [ ...pendingTriggers ] ) {
			if ( key.startsWith( prefix ) ) {
				pendingTriggers.delete( key );
				pendingTriggers.set( `${cast.token}:${key.slice( prefix.length )}`, { ...trigger, cast } );
			}
		}
		const weaponOwner = weaponOwners.get( from );
		if ( weaponOwner !== undefined ) {
			weaponOwners.delete( from );
			weaponOwners.set( cast.token, weaponOwner );
		}
	}
	const visualStarts = new Map<number, number>();
	const system = new Map<
		number,
		{ gid: number; skill: number; actors: AttachedVisual[] | null; started: number | null; }
	>();
	let systemSerial = 0;
	type AttachedVisual = import("@/engine/contracts/effects").EffectRelease & {
		actor: CharacterActor;
		life: number;
		keep: boolean;
		started: number;
		slot: number;
		hawk?: {
			state: HawkState;
			previous: number;
			revision: number;
			region: number;
			states: Readonly<Record<number, import("@/engine/contracts/effects").HawkAnimation>>;
		};
	};
	type Attachment = import("@/engine/foundation/gameplay/attached-effects").AttachedEffect & { recordName?: string; };
	const statuses = new Map<number, { view: StatusView; revisions: Map<string, number>; }>();
	const recordFor = ( effect: Attachment ) =>
		effect.recordName ? namedCatalog?.[effect.recordName] : catalog?.[String( effect.skill )];
	const attached = new Map<
		string,
		{ effect: Attachment; visuals: AttachedVisual[]; stopped: boolean; started: number; stoppedAt?: number; }
	>();
	const materials = new Map<
			number,
			{
				script: MaterialScript;
				elapsed: number;
				forward: boolean;
				color: readonly [number, number, number];
				previous: number;
			}
		>(),
		materialOwners = new Map<string, number>();
	const hwan = new Map<
		number,
		{ active: boolean; from: number; value: number; progress: number; previous: number; revision: number; }
	>();
	const roars = new Map<
		number,
		{
			revision: number;
			action: number;
			radiusMeters: number;
			startedAt: number;
			released: boolean;
			originPose: CharacterActor["pose"];
			auraBodyGid: number;
			auraHeadGid: number;
			shockPrimaryGid: number;
			shockSecondaryGid: number;
			shockCoreGid: number;
		}
	>();
	const penguinBolts = createPenguinBolts( allocate );
	// Wolf Hunting Howl (actions 1..3) and Savage Rend (4..6) on the wolf's GID.
	const wolfSkills = new Map<
		number,
		{
			revision: number;
			action: number;
			startedAt: number;
			sounded: boolean;
			pose: CharacterActor["pose"];
			gids: readonly number[];
		}
	>();
	const shields = new Map<
		number,
		{
			revision: number;
			gid: number;
			innerGid: number;
			ringGid: number;
			at: number;
			until: number;
			burst: boolean;
			shook: boolean;
		}
	>();
	const spiritRings = new Map<number, number>();
	const bondPulses = new Map<number, { revision: number; gid: number; started: number; sounded: number; }>();
	const counterPulses = new Map<
		number,
		{ revision: number; gid: number; started: number; pose: CharacterActor["pose"]; }
	>();
	const roarAppearances = new Map<
		number,
		{
			pointLight?: NonNullable<CharacterActor["pointLight"]>;
			materialTint?: readonly [number, number, number];
			scale?: number;
		}
	>();
	/*
	================
	releaseMaterial
	Ends the material script an attached effect started on its owner.
	================
	*/
	function releaseMaterial( key: string ) {
		const gid = materialOwners.get( key );
		if ( gid !== undefined ) {
			materialOwners.delete( key );
			materials.delete( gid );
		}
	}
	/*
	================
	releaseCommand
	Marks a command released at now and starts its fade from full opacity.
	A second release keeps the first release time.
	================
	*/
	function releaseCommand( command: import("@/engine/contracts/effects").EffectRelease, now: number ) {
		if ( command.released !== undefined ) return;
		command.released = now;
		command.previous = now;
		command.progress = 0;
		command.opacity = 1;
	}
	/*
	================
	stepRelease
	Advances a released command's fade-out.
	================
	*/
	function stepRelease( command: import("@/engine/contracts/effects").EffectRelease, now: number ) {
		if ( command.released === undefined || !command.fade ) return;
		const fade = movingFade( command.progress ?? 0, Math.max( 0, now - command.previous! ), command.fade, true );
		command.previous = now;
		command.progress = fade.progress;
		command.opacity = fade.opacity;
	}
	/*
	================
	releaseAttached
	Stops an attached visual from looping and fades it out.
	================
	*/
	function releaseAttached( visual: AttachedVisual, now: number ) {
		visual.keep = false;
		releaseCommand( visual, now );
	}
	/*
	================
	attachedKey
	The identity of one attached effect instance: record, owner, token,
	skill, phase and the time it was received.
	================
	*/
	function attachedKey( effect: Attachment ) {
		return `${effect.recordName ?? "skill"}:${effect.gid}:${effect.token}:${effect.skill}:${effect.phase}:${
			effect.receivedAtMs ?? "entry"
		}`;
	}
	const persistent = new Map<string, { started: number; actors: CharacterActor[]; }>();
	/*
	================
	impactIndex
	The ordinal of a skill's damage stage among its damage events, in
	native phase then event order, or -1 when the stage deals no damage.
	================
	*/
	function impactIndex( skill: number, phase: string, event: number ) {
		let indexes = impactIndexes.get( skill );
		if ( !indexes ) {
			const record = catalog?.[String( skill )];
			if ( !record ) return -1;
			const phases = [
				"READY",
				"WAIT",
				"SHOT",
				"ACT_OS",
				"ACT_OL",
				"ACT_OE",
				"ACT_S",
				"ACT_L",
				"DEACT",
				"S_RETURN"
			];
			const groups = new Map<string, { phase: string; event: number; }>();
			for ( const stage of record.stages ) {
				if ( stage.damageEvent ) {
					const phase = stage.phase ?? "SHOT";
					groups.set( `${phase}:${stage.startEvent}`, { phase, event: stage.startEvent } );
				}
			}
			indexes = new Map(
				[ ...groups.values() ].sort( ( a, b ) =>
					phases.indexOf( a.phase ) - phases.indexOf( b.phase ) || a.event - b.event
				).map( ( group, index ) => [ `${group.phase}:${group.event}`, index ] )
			);
			impactIndexes.set( skill, indexes );
		}
		return indexes.get( `${phase}:${event}` ) ?? -1;
	}
	// Loading is a recoverable transaction. Presentation diagnostics never gate it.
	/*
	================
	load
	Advances the catalog and model loading transaction, retrying failures
	with a capped backoff.
	================
	*/
	function load( now: number, wanted: boolean, namedWanted: boolean ) {
		try {
			if ( job ) {
				const result = assets.take( job.id );
				if ( result ) {
					const kind = job.kind;
					job = null;
					if ( result.kind === "error" ) {
						throw new Error( result.error );
					}
					if ( kind === "named" && result.kind === "effects" ) namedCatalog = result.catalog;
					else if ( kind === "records" && result.kind === "effects" ) {
						catalog = result.catalog;
						impactIndexes.clear();
					} else if ( kind === "models" && result.kind === "bytes" ) {
						const value = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) );
						if (
							value?.format !== "sro-skill-stage-models" || !value.models ||
							typeof value.models !== "object" || Array.isArray( value.models )
						) {
							throw new Error( "Invalid effect model manifest" );
						}
						for (
							const model of Object.values( value.models ) as {
								glb?: unknown;
								clips?: unknown;
								clipLoop?: unknown;
								states?: Readonly<Record<number, import("@/engine/contracts/effects").HawkAnimation>>;
							}[]
						) {
							if (
								!model || typeof model.glb !== "string" || !model.glb.startsWith( "/assets/" ) ||
								model.glb.includes( ".." ) || !Array.isArray( model.clips ) ||
								!model.clips.every( clip => typeof clip === "string" ) ||
								typeof model.clipLoop !== "boolean"
							) {
								throw new Error( "Invalid effect model entry" );
							}
							if ( model.states !== undefined ) {
								if (
									!model.states || typeof model.states !== "object" ||
									Array.isArray( model.states ) || Object.keys( model.states ).join( "," ) !== "0,2,7"
								) throw Error( "Invalid hawk animation table" );
								for ( const state of Object.values( model.states ) ) {
									if (
										!state || !model.clips.includes( state.clip ) ||
										!Number.isInteger( state.durationMs ) || state.durationMs <= 0 ||
										typeof state.loop !== "boolean" || !Array.isArray( state.trackEvents ) ||
										state.trackEvents.length > 512 || state.trackEvents.some( event =>
											!Number.isInteger( event.cursorMs ) || event.cursorMs < 0 ||
											event.cursorMs > state.durationMs || !Number.isInteger( event.eventCode )
										)
									) {
										throw Error( "Invalid hawk animation definition" );
									}
								}
							}
						}
						models = value.models;
						animationModels.clear();
						for (
							const model of Object.values( value.models ) as {
								glb: string;
								animationBindings?: unknown;
								modifierSets?: unknown;
								particleModifiers?: unknown;
							}[]
						) {
							const particles = modelAnimationParticles( model.particleModifiers );
							animationModels.set( model.glb, {
								bindings: modelAnimationBindings( model.animationBindings ),
								selectors: modelModifierSets( model.modifierSets ),
								particles,
								paths: [
									...new Set(
										particles.flatMap( set =>
											set.particles.map( p =>
												"/assets/effects/programs.json#" + encodeURIComponent( p.effectPath )
											)
										)
									)
								]
							} );
						}
						weapons = value.weapons ?? {};
						if (
							!weapons || typeof weapons !== "object" || Array.isArray( weapons ) ||
							Object.values( weapons ).some( path => typeof path !== "string" || !models?.[path] )
						) throw Error( "Invalid thrown weapon resource mapping" );
					} else {
						throw new Error( "Effect catalog result mismatch" );
					}
					loadFailure = null;
					attempts = 0;
					retryAt = 0;
				}
			}
			if ( wanted && !job && now >= retryAt && assets.available() > 0 ) {
				if ( !catalog ) {
					job = {
						id: assets.request(
							new URL( "/assets/skill/effectRecords.json", origin ).href,
							32 << 20,
							"effects"
						),
						kind: "records"
					};
				} else if ( (external.size || namedWanted) && !namedCatalog ) {
					job = {
						id: assets.request(
							new URL( "/assets/skill/namedEffectRecords.json", origin ).href,
							32 << 20,
							"effects"
						),
						kind: "named"
					};
				} else if ( !models ) {
					job = {
						id: assets.request( new URL( "/assets/skillfx/manifest.json", origin ).href ),
						kind: "models"
					};
				}
			}
		} catch ( error ) {
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			loadFailure = String( error );
			retryAt = now + Math.min( 8, 0.5 * 2 ** Math.min( attempts++, 4 ) );
		}
	}
	/*
	================
	modelFor
	The drawable for an effect resource: an .efp names a program in the
	shared effect program bundle; anything else is a loaded model.
	================
	*/
	const modelFor = ( path: string ) =>
		path.endsWith( ".efp" ) ?
			{
				glb: "/assets/effects/programs.json#" + encodeURIComponent( path ),
				clips: [ "effect" ],
				clipLoop: false
			} :
			models?.[path];
	/*
	================
	spawnAt
	Leaves a world-placed copy of a moving visual (or of another model) at a
	pose, for effects that stay where a flight passed or landed.
	================
	*/
	function spawnAt( visual: EffectVisual, pose: CharacterActor["pose"], at: number, path?: string ) {
		const model = path ? modelFor( path ) : undefined;
		if ( path && !model ) {
			unsupported.add( path );
			return;
		}
		if ( active.size >= 2048 ) throw Error( "Moving effect residual population exceeds budget" );
		const gid = allocate();
		active.set( gid, {
			...visual,
			camera: undefined,
			impact: undefined,
			flight: undefined,
			auxiliary: undefined,
			landed: undefined,
			family: path ? undefined : visual.family,
			life: 0,
			attachment: { kind: "world" },
			actor: {
				...visual.actor,
				gid,
				attachment: undefined,
				pose,
				time: at,
				loop: false,
				...(model ?
					{
						model: model.glb,
						clip: model.clips[0] ?? "",
						effectBasis: undefined,
						effectRotation: undefined,
						drawGeometry: undefined,
						scale: 1,
						absoluteEffectScale: false
					} :
					{})
			}
		} );
	}
	// CIDecoSkill 8DD6F0 / 8DD110 directly play event-zero vectors. These
	// instances never borrow a combat cast token or a projectile result owner.
	/*
	================
	attachedVisuals
	================
	*/
	function attachedVisuals(
		effect: Attachment,
		owner: CharacterActor,
		now: number,
		stop: boolean,
		result: AttachedVisual[] = []
	): AttachedVisual[] {
		const record = recordFor( effect );
		if ( !record ) return [];
		// 776450 converts the wire phase to (phase == 1) before 8DD6F0.
		const phases = effect.phase === 1 || (stop && record.overlap) ?
			[ "ACT_OS", "ACT_OL", "ACT_OE" ] :
			[ "ACT_S", "ACT_L", "DEACT" ];
		const wanted = stop ? [ phases[2] ] : effect.restored ? [ phases[1] ] : phases.slice( 0, 2 );
		for ( const stage of record.stages ) {
			if ( !wanted.includes( stage.phase ) || stage.startEvent !== 0 ) continue;
			if ( stage.native?.kill ) {
				for ( const visual of result ) {
					if ( visual.slot === stage.native.kill ) releaseAttached( visual, now );
				}
			}
			// 8DCA40 has no SCT_RUT consumer; rotation is specific to the
			// AT_DMG_POS branch in 8DDDE0, not a generic attachment transform.
			const script = stage.script ?? effectScript( stage.scripts );
			if ( script.kind === "material" ) {
				materialOwners.set( attachedKey( effect ), owner.gid );
				materials.set( owner.gid, {
					script,
					elapsed: 0,
					forward: true,
					color: script.from,
					previous: Math.trunc( now * 1000 )
				} );
			}
			if (
				stage.move !== "MOV_NONE" || script.kind === "unsupported" ||
				![ "AT_LOOP", "AT_ONE_FOLLOW", "AT_SOURCE", "AT_TARGET", "AT_TARGET_F" ].includes( stage.action )
			) {
				unsupported.add(
					`attached:${effect.skill}/${stage.phase}/${stage.startEvent}: ${stage.action}/${stage.move}/${
						script.kind === "unsupported" ? script.operation : script.kind
					} (${stage.resource})`
				);
				continue;
			}
			if ( stage.sound ) {
				play( {
					id: `attached:${effect.gid}:${effect.token}:${effect.receivedAtMs ?? 0}:${stop}:${result.length}`,
					path: stage.sound,
					gain: 1,
					...audioPosition( owner.pose ),
					expires: now + .25
				} );
			}
			if ( !stage.resource ) continue;
			const model = modelFor( stage.resource );
			if ( !model ) {
				unsupported.add( stage.resource );
				continue;
			}
			const binding = stageBinding( stage, stage.action === "AT_TARGET" || stage.action === "AT_TARGET_F" ),
				offset = binding.offset;
			if ( !Number.isInteger( stage.count ) || stage.count < 1 || stage.count > 128 ) {
				throw Error( "Invalid attached effect instance count" );
			}
			for ( let i = 0; i < stage.count; i++ ) {
				if ( script.kind === "mover" ) {
					if (
						stage.native?.attach || stage.action !== "AT_ONE_FOLLOW" || stage.move !== "MOV_NONE" ||
						stage.phase !== "ACT_L" || stop
					) throw Error( "Unbound SCT_MOVER registration branch" );
					if ( owner.heightFactor === undefined ) throw Error( "Missing hawk holder height factor" );
					const states = "states" in model ? model.states : undefined;
					if (
						!states || ![ 0, 2, 7 ].every( id => states[id] && model.clips.includes( states[id]!.clip ) )
					) throw Error( "Missing hawk animation states" );
					const turned = nativeRootOffset( owner.pose.yaw, owner.scale, offset ),
						position = {
							x: owner.pose.x + turned[0],
							y: owner.pose.y + turned[1],
							z: owner.pose.z + turned[2]
						};
					const state = hawkInitial( position, Math.fround( Math.PI - owner.pose.yaw ) ),
						hawkGid = allocate();
					result.push( {
						actor: {
							gid: hawkGid,
							model: model.glb,
							pose: { ...owner.pose, ...position },
							clip: states[0]!.clip,
							time: 0,
							loop: true,
							scale: 1,
							pickable: false
						},
						life: 0,
						keep: true,
						started: now,
						slot: stage.native?.slot ?? 0,
						fade: 0,
						hawk: {
							state,
							previous: Math.trunc( now * 1000 ),
							revision: 0,
							region: owner.pose.regionId,
							states
						}
					} );
					// The hawk's own ambient effects ride on it: the Lightning and
					// Frost Hawks are the Black and White Hawks' mesh and material
					// with wing effects on Bip01 L/R Hand (lighthawk.bsr,
					// coldhawk.bsr), so without them each looks like its base hawk.
					if ( "particleModifiers" in model ) {
						for ( const particle of ambientModelParticles( model.particleModifiers ) ) {
							result.push( {
								actor: {
									gid: allocate(),
									model: "/assets/effects/programs.json#" + encodeURIComponent( particle.effectPath ),
									pose: owner.pose,
									clip: "effect",
									time: 0,
									loop: true,
									scale: 1,
									pickable: false,
									attachment: modelParticleAttachment( hawkGid, particle, particle.scale ?? 1 )
								},
								life: 0,
								keep: true,
								started: now,
								slot: stage.native?.slot ?? 0,
								fade: 0
							} );
						}
					}
					continue;
				}
				// A loop stage, and an active-phase stage with no authored life (the
				// White and Blue Hawks' follower: AT_ONE_FOLLOW whitehawk.bsr with no
				// SCT_MOVER), live as long as their attachment, looping their clip
				// until its deactivation. Inferred from the data: no other ACT_L
				// stage is unlooped, and a one-shot follower vanished one clip after
				// the summon while its buff ran on for minutes.
				const persists = stage.action === "AT_LOOP" || stage.phase === "ACT_L" && !stage.life;
				const actor: CharacterActor = {
					gid: allocate(),
					model: model.glb,
					pose: owner.pose,
					clip: model.clips[0] ?? "",
					time: 0,
					loop: persists,
					scale: stage.resource.endsWith( ".efp" ) ?
						stageEffectScale(
							stage.native?.scale,
							owner.effectBaseScale ?? owner.scale ?? 1,
							owner.heightFactor
						) :
						1,
					absoluteEffectScale: stage.resource.endsWith( ".efp" ),
					pickable: false,
					attachment: {
						...stageAttachment( owner, binding, stage.resource ),
						// A root effect (a speed buff's ring) stands on the ground: under
						// a rider that is the ride's position, as retail draws it.
						...(!binding.addHeight && !binding.bone ? { ground: true } : {})
					}
				};
				const control = {
					life: stage.life,
					keep: persists && !stop,
					started: now,
					slot: stage.native?.slot ?? 0,
					fade: (stage.native?.fadeInMs ?? 0) / 1000
				};
				result.push( { actor, ...control } );
				if ( "particleModifiers" in model ) {
					for ( const particle of ambientModelParticles( model.particleModifiers ) ) {
						result.push( {
							actor: {
								gid: allocate(),
								model: "/assets/effects/programs.json#" + encodeURIComponent( particle.effectPath ),
								pose: owner.pose,
								clip: "effect",
								time: 0,
								loop: actor.loop,
								scale: 1,
								pickable: false,
								attachment: modelParticleAttachment(
									actor.gid,
									particle,
									particle.scale ?? actor.scale
								)
							},
							...control
						} );
					}
				}
			}
		}
		return result;
	}
	return {
		/*
		================
		crowEffect
		A bounded, fixed-origin gust outlives the despawned clone.
		================
		*/
		crowEffect(
			source: Extract<import("@/engine/contracts/orb").VisualFeedback, { kind: "pet-crow-effect"; }>["source"],
			cue = 0,
			book = 1
		) {
			if ( crowEffects.length >= CROW_EFFECT_LIMIT ) return;
			crowEffects.push( {
				pose: { ...source, yaw: nativeHeadingYaw( 0 ) },
				cue,
				book,
				gids: Array.from( { length: cue === 1 || cue === 4 ? 12 : 7 }, allocate )
			} );
		},
		/*
		================
		item
		================
		*/
		item( event: import("@/engine/contracts/orb").ItemEffectFeedback ) {
			// A full queue skips the newest visual; it never fails the frame.
			if ( external.size >= EFFECT_ACTOR_BUDGET ) {
				budgetGaveWay++;
				return;
			}
			external.set( ++externalSerial, { event, clock: { phase: "pending" }, hostGid: allocate(), children: [] } );
		},
		/*
		================
		system
		Queues a system effect record (a level-up or a similar event) on a character.
		================
		*/
		system( gid: number, skill: number ) {
			if ( system.size >= EFFECT_ACTOR_BUDGET ) {
				budgetGaveWay++;
				return;
			}
			system.set( ++systemSerial, { gid, skill, actors: null, started: null } );
		},
		/*
		================
		animationModel
		The animation bindings, modifier sets and particles loaded for a model.
		================
		*/
		animationModel: ( path: string ) => animationModels.get( path ),
		/*
		================
		step
		Turns this frame's gameplay (casts, triggers, attached effects, status,
		items) into effect actors, advancing flights, releases and loads.
		Returns the actors to draw; failures are held for error().
		================
		*/
		step(
			entities: readonly EntityState[],
			gameplay: EffectGameplay | null,
			now: number,
			resourceReady: ( path: string ) => boolean,
			duration: ( path: string, clip: string ) => number,
			triggers: readonly EffectTrigger[] = [],
			socket?: (
				gid: number,
				bone: string | null,
				offset: readonly [number, number, number],
				trigger: EffectTrigger
			) => EffectVisual["actor"]["pose"] | null,
			presented: readonly CharacterActor[] = [],
			effectDetail = 2,
			bloodEnabled = true,
			// The trade bandits' published dress by RefObjID (861720).
			banditTable: ReadonlyMap<number, { readonly imbue: number; }> = new Map(),
			// The frame's drawn poses (presentation-poses.ts). The local
			// player's anchors sit on the body as drawn; absent in direct
			// test drives, which keep anchoring on gameplay.pose as before.
			drawnPoses?: ReadonlyMap<number, Pose>
		) {
			/*
			================
			ready
			True when a model and every model it depends on are resident.
			================
			*/
			function ready( path: string ) {
				let admitted = resourceReady( path );
				for ( const dependency of animationModels.get( path )?.paths ?? [] ) {
					if ( !resourceReady( dependency ) ) admitted = false;
				}
				return admitted;
			}
			// Launch sockets the hold below sampled this step, keyed by callback,
			// stage and source. The launch reuses them: one sample per launch.
			const launchSockets = new Map<string, EffectVisual["actor"]["pose"] | null>();
			/*
			================
			launchKey
			================
			*/
			function launchKey( trigger: EffectTrigger, index: number, gid: number ) {
				return `${trigger.cast.token}:${trigger.phase}:${trigger.event}:${index}:${gid}`;
			}
			/*
			================
			moverSourceCold

			True when a mover stage this callback starts launches from a bone on a
			resident actor that cannot be posed yet: its model, or the mount it
			rides, is still loading. The admission below would otherwise drop the
			mover as unsupported. A returning mover leaves its victims; every other
			mover leaves the caster. Target sockets are not probed here: they are
			sampled live at launch and again at each retarget.
			================
			*/
			function moverSourceCold(
				record: import("@/engine/contracts/effects").EffectRecord,
				trigger: EffectTrigger,
				byGid: ReadonlyMap<number, EntityState>
			) {
				if ( !socket ) return false;
				const cast = trigger.cast;
				let cold = false;
				for ( let index = 0; index < record.stages.length; index++ ) {
					const stage = record.stages[index]!;
					if ( (stage.phase ?? "SHOT") !== trigger.phase || stage.startEvent !== trigger.event ) continue;
					if ( !stage.bone || stage.action === "AT_TARGET" || stage.action === "AT_TARGET_F" ) continue;
					const returning = stage.action === "AT_SOURCE" && stage.move !== "MOV_NONE";
					const mover = returning || stage.action === "AT_MOV_1TAR" || stage.action === "AT_MOV_SPLASH" ||
						stage.action === "AT_MOV_OPTION";
					if ( !mover ) continue;
					const sources = returning ?
						[ ...new Set( [ cast.target, ...(cast.results?.map( row => row.target ) ?? []) ] ) ] :
						[ cast.caster ];
					for ( const gid of sources ) {
						if ( !byGid.has( gid ) ) continue;
						const pose = socket( gid, stage.bone, stage.offset, trigger );
						launchSockets.set( launchKey( trigger, index, gid ), pose );
						if ( !pose ) cold = true;
					}
				}
				return cold;
			}
			if ( disposed ) {
				return [];
			}
			load(
				now,
				entities.length > 0 || external.size > 0,
				gameplay?.vitals?.some( v => !!((v.abnormal ?? 0) & STATUS_DECORATION_MASK) ) ?? false
			);
			failure = null;
			try {
				for ( const cast of gameplay?.casts ?? [] ) {
					if ( cast.predictedToken !== undefined && cast.predictedToken !== cast.token ) {
						adoptCast( cast.predictedToken, cast );
					}
				}
				const castStates = new Map( gameplay?.casts.map( cast => [ cast.token, cast ] ) ?? [] );
				const current = new Set( castStates.keys() );
				const byGid = new Map( entities.map( entity => [ entity.gid, entity ] ) );
				// All effect anchors sit on the body as drawn (#1442): the local
				// player's anchor is the frame's drawn pose when the frame
				// resolved one, else the movement owner's logical pose. The map
				// supplies only the position: the heading stays the movement
				// owner's, as the composed map's local row is built the same
				// way (presentation-events' drawnLocal). The hawk's mechanical
				// holder below is the one exception: it is game logic, not a
				// drawn anchor, and keeps the logical pose.
				const localLogical = gameplay?.localGid !== undefined ? gameplay.pose ?? undefined : undefined;
				const localDrawn = gameplay?.localGid !== undefined ?
					drawnPoses?.get( gameplay.localGid ) :
					undefined;
				const localPose = localDrawn && localLogical ?
					{ ...localDrawn, angle: localLogical.angle } :
					localDrawn ?? localLogical;
				// The local movement owner advances gameplay.pose; the entity row
				// retains its wire/spawn position. All effect anchors (including
				// bone-less victims, returns and chain hops) need the live pose.
				const local = gameplay?.localGid ? byGid.get( gameplay.localGid ) : undefined;
				if ( local && localPose ) {
					// The movement snapshot may retain old entity metadata; only its coordinates own this anchor.
					const pose = localPose;
					byGid.set( local.gid, {
						...local,
						regionId: pose.regionId,
						x: pose.x,
						y: pose.y,
						z: pose.z,
						heading: pose.angle
					} );
				}
				hitLights.step( now, new Set( byGid.keys() ) );
				const wantedPersistent = new Set<string>(),
					presentation = new Map( presented.map( actor => [ actor.gid, actor ] ) );
				/*
				================
				stageAnchor

				The world point one authored binding names on a holder entity,
				resolved as 8D6880 does: the named bone (a missing bone falls back
				to the root inside the socket query), or for no bone the holder's
				own root, which is its live entity pose; plus the native offset
				turned by the root, plus the holder's height for the '*' token. A
				named bone on a model that is not resident yet answers from the
				root by the same rule. Null while a '*' holder's height is
				unknown. The caller supplies the yaw: only the position is
				resolved here.
				================
				*/
				function stageAnchor(
					holder: EntityState,
					binding: Omit<StageBinding, "keepRotation">,
					trigger: EffectTrigger,
					yaw: Radians
				): CharacterActor["pose"] | null {
					const actor = presentation.get( holder.gid );
					if ( binding.addHeight && actor?.height === undefined ) return null;
					const [x, y, z] = binding.offset,
						offset = [ x, y + (binding.addHeight ? actor!.height! : 0), z ] as const;
					const sampled = binding.bone ? socket?.( holder.gid, binding.bone, offset, trigger ) : null;
					if ( sampled ) return { ...sampled, yaw };
					const turned = nativeRootOffset(
						characterHeadingYaw( holder.heading ),
						actor?.scale ?? 1,
						offset
					);
					return {
						regionId: holder.regionId,
						x: holder.x + turned[0],
						y: holder.y + turned[1],
						z: holder.z + turned[2],
						yaw
					};
				}
				const clock = Math.trunc( now * 1000 );
				for ( const [token, gid] of weaponOwners ) {
					if (
						!current.has( token ) || !byGid.has( gid ) ||
						castStates.get( token )?.cancelledAtMs !== undefined
					) weaponOwners.delete( token );
				}
				for ( const cast of gameplay?.casts ?? [] ) {
					if (
						!cast.resultOnly && cast.cancelledAtMs === undefined &&
						catalog?.[String( cast.skill )]?.hideWeapon === 1
					) weaponOwners.set( cast.token, cast.caster );
				}
				spineAims.step( { casts: gameplay?.casts ?? [], catalog, triggers, presented, now }, socket );
				for ( const [gid, row] of materials ) {
					if ( !byGid.has( gid ) ) {
						materials.delete( gid );
						continue;
					}
					const sample = stepMaterial( row.script, row, Math.max( 0, clock - row.previous ) );
					Object.assign( row, sample, { previous: clock } );
				}
				// 85C590 owns named PARAM_ decorations and one material register.
				// Dead appearance and zero hp do not gate it.
				for ( const entity of entities ) {
					const mask = (gameplay?.vitals ?? []).find( v => v.gid === entity.gid )?.abnormal ?? 0;
					const prior = statuses.get( entity.gid );
					const view = status.view( entity.gid, mask, entity.appearanceState?.[2] ?? 0 ); // +0x2B6 (85EC00): 1 = berserk skips the material calls
					const revisions = new Map( prior?.revisions );
					for ( const name of view.names ) if ( !revisions.has( name ) ) revisions.set( name, clock );
					for ( const name of revisions.keys() ) if ( !view.names.includes( name ) ) revisions.delete( name );
					statuses.set( entity.gid, { view, revisions } );
					// 85C590 skips every material call while +0x2B6 is 1 (berserk):
					// a status view cached from before berserk must not repaint or
					// clear the hwan material register either.
					const enabled = entity.appearanceState?.[2] === 1;
					if ( !enabled && view.tint ) {
						const script: MaterialScript = {
							kind: "material",
							from: view.tint.from,
							to: view.tint.to,
							durationMs: 1000
						};
						const row = materials.get( entity.gid );
						if ( !row || row.script.kind !== "material" || row.script.from !== view.tint.from ) {
							materials.set( entity.gid, {
								script,
								elapsed: 0,
								forward: true,
								color: script.from,
								previous: clock
							} );
						}
					} else if ( !enabled && view.tint === null ) materials.delete( entity.gid );
					let row = hwan.get( entity.gid );
					if ( !row && !enabled ) continue;
					if ( !row ) {
						row = { active: enabled, from: 1, value: 1, progress: 0, previous: now, revision: clock };
						hwan.set( entity.gid, row );
					}
					if ( row.active !== enabled ) {
						row.active = enabled;
						row.from = row.value;
						row.progress = 0;
						row.revision = clock;
						if ( !enabled ) materials.delete( entity.gid );
					}
					const sample = stepHwanScale(
						row.from,
						enabled ? Math.fround( 1.1 ) : 1,
						row.progress,
						Math.max( 0, now - row.previous ),
						enabled
					);
					row.value = sample.value;
					row.progress = sample.progress;
					row.previous = now;
				}
				for ( const gid of hwan.keys() ) if ( !byGid.has( gid ) ) hwan.delete( gid );
				for ( const gid of statuses.keys() ) {
					if ( !byGid.has( gid ) ) {
						statuses.delete( gid );
						materials.delete( gid );
						status.drop( gid );
					}
				}
				for ( const [key, gid] of materialOwners ) if ( !byGid.has( gid ) ) materialOwners.delete( key );
				// 868420 toggles 8000001E; 85ECD0/85C570 own this state effect
				// independently of action tokens. Late asset admission must not
				// permanently consume the appearance transition.
				const stateEffects = new Map<number, number[]>();
				for ( const entity of entities ) {
					if ( (entity.visualFlags ?? 0) & 2 ) stateEffects.set( entity.gid, [ 0x8000001e ] );
					else if ( entity.captureMark || isCapturedMonster( entity ) ) {
						stateEffects.set( entity.gid, [ SYSTEM_CAPTURE_MARK ] );
					}
				}
				// 787DB0 builds the NPC index from flag 2 / optional gid, taking
				// the first record in ascending registry-key order for each NPC.
				const marked = new Set<number>();
				for ( const marker of [ ...gameplay?.questMarkers ?? [] ].sort( ( a, b ) => a.refId - b.refId ) ) {
					const gid = marker.optional;
					if ( !(marker.flags & 2) || !gid || marked.has( gid ) ) continue;
					marked.add( gid );
					stateEffects.set( gid, [ ...stateEffects.get( gid ) ?? [], questMarkerEffect( marker.valueA ) ] );
				}
				for ( const entity of entities ) {
					for ( const effectId of stateEffects.get( entity.gid ) ?? [] ) {
						try {
							const key = `${entity.gid}:${entity.refObjId}:${entity.bodyShape ?? 0}:${effectId}`;
							wantedPersistent.add( key );
							const owner = presentation.get( entity.gid );
							if ( !owner || !catalog || !models ) continue;
							if ( !persistent.has( key ) ) {
								const record = catalog[String( effectId | 0 )] ?? catalog[String( effectId )];
								if ( !record ) {
									unsupported.add( "state:" + effectId.toString( 16 ) );
									continue;
								}
								const actors: CharacterActor[] = [];
								for ( const stage of record.stages ) {
									if ( stage.phase !== "ACT_L" || stage.startEvent !== 0 ) continue;
									if (
										stage.action !== "AT_LOOP" || stage.move !== "MOV_NONE" ||
										stage.scripts.length || stage.count !== 1 || !stage.resource || stage.life !== 0
									) throw Error( "Unsupported persistent effect stage" );
									const model = models[stage.resource];
									if ( !model ) throw Error( "Missing persistent effect model" );
									if ( !model.clips.length && model.particleModifiers === undefined ) {
										throw Error( "Stale effect manifest lacks particle attachments" );
									}
									const gid = allocate();
									actors.push( {
										gid,
										model: model.glb,
										pose: owner.pose,
										clip: model.clips[0] ?? "",
										time: 0,
										loop: true,
										scale: 1,
										pickable: false,
										// 8D6880: missing bone retries on the mount, else root
										// orientation. The offset is still applied.
										attachment: stageAttachment(
											owner,
											stageBinding( stage, false ),
											stage.resource
										)
									} );
									for ( const particle of ambientModelParticles( model.particleModifiers ) ) {
										actors.push( {
											gid: allocate(),
											model: "/assets/effects/programs.json#" +
												encodeURIComponent( particle.effectPath ),
											pose: owner.pose,
											clip: "effect",
											time: 0,
											loop: true,
											scale: 1,
											pickable: false,
											// Same 8D6880 miss: the particle stays on the effect root.
											attachment: modelParticleAttachment( gid, particle, particle.scale ?? 1 )
										} );
									}
								}
								if (
									actors.length + active.size +
											[ ...persistent.values() ].reduce(
												( n, row ) => n + row.actors.length,
												0
											) > EFFECT_ACTOR_BUDGET
								) {
									// A full budget skips this state effect; the frame draws.
									budgetGaveWay += actors.length;
									continue;
								}
								// Begin the visual clock only once the entire attachment
								// family is admitted; partial families never flash at origin.
								let admitted = true;
								for ( const actor of actors ) if ( !ready( actor.model ) ) admitted = false;
								if ( admitted && actors.length ) persistent.set( key, { started: now, actors } );
							}
						} catch ( error ) {
							failure = `State effect ${
								effectId >>> 0
							}, actor ${entity.gid}, reference ${entity.refObjId}: ${String( error )}`;
						}
					}
				}
				for ( const key of persistent.keys() ) if ( !wantedPersistent.has( key ) ) persistent.delete( key );
				// Only the action owner produces phase callbacks. B245 contains deferred
				// result rows; its arrival is not itself an impact event.
				for ( const trigger of triggers ) {
					const key = `${trigger.cast.token}:${trigger.phase}:${trigger.event}`;
					if ( !seen.has( key ) && !pendingTriggers.has( key ) ) {
						// A crowd's casts can arrive together; past the budget the
						// newest trigger waits out, the frame still draws.
						if ( pendingTriggers.size >= EFFECT_ACTOR_BUDGET ) {
							budgetGaveWay++;
							continue;
						}
						pendingTriggers.set( key, trigger );
					}
				}
				for ( const [pendingKey, trigger] of pendingTriggers ) {
					const cast = trigger.cast, key = `${cast.token}:${trigger.phase}:${trigger.event}`;
					if ( !byGid.has( cast.caster ) && !byGid.has( cast.target ) ) {
						pendingTriggers.delete( pendingKey );
						continue;
					}
					if ( !catalog || !models ) continue;
					// Do not consume a callback while its native characterInfo
					// anchor is still waiting for the asset owner to publish it.
					const pendingRecord = catalog[String( cast.skill )];
					if (
						pendingRecord?.stages.some( s =>
							(s.phase ?? "SHOT") === trigger.phase && s.startEvent === trigger.event &&
							s.resource?.endsWith( ".efp" ) && s.native?.scale === "CHAR_BASE"
						) && presentation.get( cast.caster )?.heightFactor === undefined
					) continue;
					if (
						pendingRecord?.stages.some( s =>
							(s.phase ?? "SHOT") === trigger.phase && s.startEvent === trigger.event &&
							s.action === "AT_DMG_POS" && (s.script ?? effectScript( s.scripts )).kind === "rotation"
						) && (cast.results?.length ?
							cast.results.map( r => r.target ) :
							[ cast.target ]).some( gid => byGid.has( gid ) && !presentation.get( gid )?.effectAnchor )
					) continue;
					// Native compounds load synchronously, so 8D6330 always finds a
					// mover's launch bone. Here a resident caster (or its mount) may
					// still be loading; hold the callback until its launch bone poses,
					// but not past the cast itself, so a failed model still reports.
					if (
						pendingRecord && castStates.has( cast.token ) &&
						moverSourceCold( pendingRecord, trigger, byGid )
					) continue;
					pendingTriggers.delete( pendingKey );
					if ( !seen.has( key ) ) {
						seen.add( key );
						const record = catalog[String( cast.skill )];
						if ( !record ) {
							unsupported.add( `skill:${cast.skill}` );
							continue;
						}
						for ( let index = 0; index < record.stages.length; index++ ) {
							const stage = record.stages[index]!;
							if ( (stage.phase ?? "SHOT") !== trigger.phase || stage.startEvent !== trigger.event ) {
								continue;
							}
							const script = stage.script ?? effectScript( stage.scripts );
							// 8DBE60 releases every matching owned command. Slot
							// zero is not a kill; launched movers are independent.
							if ( stage.native?.kill ) {
								for ( const visual of active.values() ) {
									if (
										visual.token === cast.token && visual.command?.slot === stage.native.kill
									) releaseCommand( visual.command, now );
								}
							}
							// 8DEF4A common tail runs once per stage, including a
							// script-only stage. Only exact remote CICUser is exempt.
							if (
								script.kind === "camera" && !script.arrival &&
								byGid.get( cast.caster )?.kind !== "player"
							) cameraEvents.push( { ...script, atMs: Math.trunc( now * 1000 ) } );
							const returning = stage.action === "AT_SOURCE" && stage.move !== "MOV_NONE";
							// 8DE7A4 gives AT_TARGET_F's EFP to the target's decoration
							// owner. It ignores mover fields and survives caster cleanup.
							const targetFollow = stage.action === "AT_TARGET_F";
							if ( targetFollow && !stage.resource?.endsWith( ".efp" ) ) continue;
							const targetLocal = stage.action === "AT_TARGET" || stage.action === "AT_TARGET_F";
							const radial = stage.action === "AT_MOV_OPTION",
								chain = stage.action === "AT_MOV_SPLASH" && stage.move !== "MOV_HWAN";
							const distributed = stage.action === "AT_MOV_1TAR" || stage.action === "AT_MOV_SPLASH";
							// 8DE635: AT_TARGET builds a mover only for a non-zero start
							// speed (+0x12); a MOV_STRAIGHT row at speed 0 is a static
							// effect on the target, as MOV_NONE is.
							const flying = distributed || radial || returning ||
								!targetFollow && targetLocal && (stage.movement?.startSpeed ?? 0) !== 0;
							const staticTarget = targetLocal && !targetFollow && !flying;
							const resultStage = impactIndex( cast.skill, trigger.phase, trigger.event );
							const resultTargets = cast.results?.filter( row =>
								resultStage < 0 || castResultAt( row.impacts, resultStage )
							).map( row =>
								row.target
							) ?? (cast.impacts?.length ? [ cast.target ] : []);
							const targets = distributed ?
								movingTargets( stage.count, cast.target, resultTargets ) :
								radial ?
								[ { target: cast.caster, owns: false, all: false } ] :
								(!targetLocal && (stage.damageEvent || flying) ?
									[ ...new Set( resultTargets.length ? resultTargets : [ cast.target ] ) ] :
									[ cast.target ]).map( target => ({ target, owns: true, all: targetLocal }) );
							for ( const dispatch of targets ) {
								const targetGid = dispatch.target;
								const entity = byGid.get(
									returning || targetLocal ?
										targetGid :
										flying || !stage.damageEvent ?
										cast.caster :
										targetGid
								);
								if ( !entity ) {
									continue;
								}
								const pose = entity.gid === gameplay?.localGid && localPose ?
									localPose :
									{
										regionId: entity.regionId,
										x: entity.x,
										y: entity.y,
										z: entity.z,
										angle: entity.heading
									};
								if ( stage.sound && !flying ) {
									play( {
										id: `effect:${key}:${index}:${targetGid}`,
										path: stage.sound,
										gain: 1,
										...audioPosition( pose ),
										expires: (trigger.adopted ? now : trigger.at) + 0.25
									} );
								}
								if ( !stage.resource ) {
									continue;
								}
								const caster = byGid.get( cast.caster );
								// 8DB770 -> 8D5440: an effect a result spawns on a victim copies the
								// caster's model matrix (+0x98 vfunc +0x18) at that moment; the victim
								// only places it, so the victim's heading never turns the effect.
								const casterAngle = caster ?
									(caster.gid === gameplay?.localGid && localPose ?
										localPose.angle :
										caster.heading) :
									undefined;
								const victimFacing = !returning && !targetLocal && !flying && stage.damageEvent &&
										casterAngle !== undefined && entity.gid !== cast.caster ?
									nativeHeadingYaw( casterAngle ) :
									undefined;
								const rotation = script.kind === "rotation" && stage.action === "AT_DMG_POS" ?
									hitRotation(
										script.radians,
										(caster?.gid === gameplay?.localGid ? gameplay?.inventory : caster?.equipment)
											?.find( item => item.slot === 6 )?.typeFlags,
										trigger.attackKind ?? 0,
										Math.max( 0, impactIndex( cast.skill, trigger.phase, trigger.event ) )
									) :
									undefined;
								const commandRotation = stage.native?.rotation ?
									projectNativeCommandRotation( stage.native.rotation ) :
									undefined;
								// A flying .bsr keeps the 8D9233 travel basis (effectBasis).
								const effectRotation = flying && targetLocal && stage.resource?.endsWith( ".efp" ) ?
									TARGET_MOVER_PITCH :
									rotation?.rotation ?? commandRotation;
								const weapon = stage.resource === "weapon" ?
									(caster?.gid === gameplay?.localGid ? gameplay?.inventory : caster?.equipment)
										?.find( item => item.slot === 6 ) :
									undefined;
								const resource = stage.resource === "weapon" ?
									(weapon ? weapons[String( weapon.refObjId )] : undefined) :
									rotation?.pierce ?
									"hiteffect/hit_1_pierce_critical.efp" :
									stage.resource;
								// Native kind 5 with an empty slot creates no model; it never borrows a different weapon.
								if ( stage.resource === "weapon" && !weapon ) continue;
								const model = resource ? modelFor( resource ) : undefined;
								const target = byGid.get( returning ? cast.caster : targetGid ) ??
										(distributed && stage.count === 1 ? byGid.get( cast.caster ) : undefined),
									motion = stage.movement;
								if ( distributed && stage.count > 1 && !target ) continue;
								const curved = stage.move === "MOV_HWAN";
								// 8DE6C7..8DE786: AT_TARGET movers start/end at
								// target-local offsets; this lane does not sample bones.
								const sourceSocket = flying && !targetLocal && stage.bone ?
									launchSockets.get( launchKey( trigger, index, entity.gid ) ) ??
										socket?.( entity.gid, stage.bone, stage.offset, trigger ) :
									undefined;
								const targetOffset = stage.targetOffset ?? [ 0, 0, 0 ];
								const targetSocket = flying && !radial && !targetLocal && target && stage.targetBone ?
									socket?.( target.gid, stage.targetBone, targetOffset, {
										...trigger,
										sampleCurrent: true
									} ) :
									undefined;
								const supportedFlight = flying && target &&
									(targetLocal || !stage.bone || sourceSocket) &&
									(radial || targetLocal || !stage.targetBone || targetSocket) &&
									(curved || stage.move === "MOV_STRAIGHT" || stage.move === "MOV_ROUND" ||
										[ "MOV_UP", "MOV_UPR" ].includes( stage.move ) &&
											stage.parameters !== undefined) &&
									// 8DE6C7: no speed gate. Navigation_StepTowards (879650)
									// arrives a speed-0 mover whose start is its end at once.
									motion && (curved || motion.startSpeed >= 0 && motion.endSpeed >= 0) &&
									projectileSpace( pose.regionId, target.regionId ) &&
									(!radial || stage.move === "MOV_STRAIGHT" && stage.native?.actionOptions.enabled);
								if (
									!model ||
									(!supportedFlight &&
										(![
											"AT_DMG_POS",
											"AT_ONE_FOLLOW",
											"AT_LOOP",
											"AT_STOP",
											"AT_TARGET",
											"AT_TARGET_F"
										].includes(
											stage.action
										) || !targetFollow && !staticTarget && stage.move !== "MOV_NONE" ||
											(stage.bone &&
												![ "AT_ONE_FOLLOW", "AT_LOOP", "AT_STOP" ].includes( stage.action ) &&
												!targetLocal))) ||
									script.kind === "unsupported" ||
									(!Number.isInteger( stage.count ) || stage.count < 1 || stage.count > 128 ||
										(!distributed && !radial && !returning && !targetLocal && stage.count !== 1))
								) {
									// Keep resource support separate from runtime endpoint failures.
									// A report must identify the failing actor, not just the skill.
									const details = [
										!model ? `missing-model:${resource}` : null,
										flying && !targetLocal && stage.bone && !sourceSocket ?
											`source-socket:${entity.gid}/${stage.bone}` :
											null,
										flying && !radial && !targetLocal && stage.targetBone && !targetSocket ?
											`target-socket:${target?.gid ?? targetGid}/${stage.targetBone}` :
											null,
										flying && !target ? `missing-target:${targetGid}` : null,
										flying && target && !projectileSpace( pose.regionId, target.regionId ) ?
											`region-space:${pose.regionId}/${target.regionId}` :
											null
									].filter( value => value !== null );
									unsupported.add(
										`${cast.skill}/${trigger.phase}/${trigger.event}/${index}: ${stage.action}/${stage.move}/${
											script.kind === "unsupported" ? script.operation : script.kind
										} (${stage.resource})${details.length ? ` [${details.join( ", " )}]` : ""}`
									);
									continue;
								}
								const instances = distributed || radial || targetFollow ? 1 : stage.count;
								if ( active.size + instances > EFFECT_ACTOR_BUDGET ) {
									budgetGaveWay += instances;
									break;
								}
								for ( let instance = 0; instance < instances; instance++ ) {
									// 8DC3B0 transfers the first command payload only for a
									// single distributed mover. Resource and visual clock survive.
									const traded =
										supportedFlight && distributed && stage.count === 1 && stage.native?.trade ?
											[ ...active.values() ].find( v =>
												v.token === cast.token && v.command?.slot === stage.native!.trade
											) :
											undefined;
									const gid = traded?.actor.gid ?? allocate();
									if ( traded ) active.delete( gid );
									// Effect world poses carry the native heading yaw; only the
									// position comes from the binding (stageAnchor).
									let source = flying && targetLocal ?
										targetMoverPoint( pose, stage.offset ) :
										sourceSocket ??
											stageAnchor(
												entity,
												stageBinding( stage, false ),
												trigger,
												victimFacing ?? nativeHeadingYaw( pose.angle )
											);
									if ( !source ) {
										unsupported.add( `anchor-height:${entity.refObjId}` );
										continue;
									}
									if ( staticTarget ) {
										// 8DE67A..8DE69E: the static effect sits on the target's
										// binding but takes the caster's m_fYaw (+0x88). The caster
										// always exists natively; a port without its row keeps the
										// target's heading.
										const anchor = stageAnchor(
											entity,
											stageBinding( stage, true ),
											trigger,
											nativeHeadingYaw( casterAngle ?? pose.angle )
										);
										if ( !anchor ) {
											unsupported.add( `anchor-height:${entity.refObjId}` );
											continue;
										}
										source = anchor;
									}
									// The renderer attachment's holder; a '*' binding needs its height.
									const holder = { gid: entity.gid, height: presentation.get( entity.gid )?.height };
									if (
										holder.height === undefined &&
										(targetFollow ? stage.targetAddHeight : stage.addHeight)
									) {
										unsupported.add( `anchor-height:${entity.refObjId}` );
										continue;
									}
									if ( rotation && caster ) {
										const anchor = presentation.get( entity.gid )?.effectAnchor;
										if ( !anchor ) {
											unsupported.add( `hit-anchor:${entity.refObjId}` );
											continue;
										}
										let [ax, ay, az] = anchor.offset;
										if ( anchor.bone ) {
											const joint = socket?.( entity.gid, anchor.bone, [ 0, 0, 0 ], trigger );
											if ( !joint ) {
												unsupported.add( `hit-socket:${entity.refObjId}/${anchor.bone}` );
												continue;
											}
											ax += joint.x - entity.x;
											ay += joint.y - entity.y;
											az += joint.z - entity.z;
											const actor = presentation.get( entity.gid );
											if ( entity.mountedOn !== undefined && actor?.mountedOn === undefined ) {
												const mount = byGid.get( entity.mountedOn ) ?? entity;
												const saddle = socket?.(
													entity.mountedOn,
													"saddle",
													[ 0, 0, 0 ],
													trigger
												);
												if ( saddle ) {
													ax += saddle.x - mount.x;
													ay += saddle.y - mount.y;
													az += saddle.z - mount.z;
												}
											}
										}
										if ( !projectileSpace( entity.regionId, caster.regionId ) ) {
											unsupported.add( "hit-anchor:region-space" );
											continue;
										}
										const [dx, dz] = worldDelta( entity, caster ),
											dy = caster.y - entity.y,
											length = hypot3( dx, dy, dz ),
											k = length ? -az / length : 0;
										source = {
											regionId: pose.regionId,
											x: pose.x + dx * k,
											y: pose.y + dy * k + ay,
											z: pose.z + dz * k,
											yaw: radians( -nativeHeadingYaw( caster.heading ) )
										};
									}
									const destination = radial ?
										radialDestination(
											{
												regionId: pose.regionId,
												x: pose.x,
												y: pose.y,
												z: pose.z,
												yaw: nativeHeadingYaw( pose.angle )
											},
											stage.native!.actionOptions.direction,
											stage.native!.actionOptions.distance
										) :
										flying && targetLocal ?
										targetMoverPoint( pose, targetOffset ) :
										targetSocket ??
											(target ?
												stageAnchor(
													target,
													stageBinding( stage, true ),
													trigger,
													nativeHeadingYaw( target.heading )
												) ?? source :
												source);
									// The fYaw this mover's Initialize caller gave it:
									// kept verbatim if the path has zero length. Read
									// before the radial lane re-aims the source pose.
									const spawnYaw = targetLocal ? TARGET_MOVER_SPAWN_YAW : source.yaw;
									if ( radial ) source = { ...source, yaw: destination.yaw };
									const [destX, destZ] = regionRelativeXZ( source.regionId, destination );
									const curveResult = supportedFlight && curved ?
										random.curve(
											[ source.x, source.y, source.z ],
											[ destX, destination.y, destZ ]
										) :
										null;
									const launch = supportedFlight ?
										random.range( motion.startSpeed, motion.endSpeed ) :
										null;
									const spin = supportedFlight && stage.move === "MOV_UPR" ?
										random.range( stage.parameters![1], stage.parameters![2] ) :
										null;
									// 8d8490 zeroes both arc coefficients; 8d8e30 fills them
									// only for MOV_UPR. MOV_UP's computed envelope therefore
									// contributes no offset through 8d5f20 on this path.
									const arc = spin !== null ?
										{
											amplitudePermille: stage.parameters![0],
											rotationRadians: Math.fround( spin * 0.01745329238474369 )
										} :
										undefined;
									const route:
										| NonNullable<
											import("@/engine/contracts/effects").EffectFlight["moving"]
										>["route"]
										| undefined = radial ?
											{ kind: "radial", spacing: stage.native!.actionOptions.residualDistance } :
											chain ?
											{
												kind: "chain",
												targets:
													(dispatch.all ? resultTargets : dispatch.owns ? [ targetGid ] : [])
														.filter( gid => byGid.has( gid ) ),
												cursor: 0,
												pending: null,
												trigger,
												bone: stage.targetBone ?? null,
												addHeight: stage.targetAddHeight ?? false,
												offset: targetOffset
											} :
											undefined;
									const flight: import("@/engine/contracts/effects").EffectFlight | undefined =
										supportedFlight ?
											{
												curve: curveResult ? { state: curveResult, previousMs: 0 } : undefined,
												arc: chain ? undefined : arc,
												destination,
												speed: launch!,
												delay: curved ? 0 : motion.delayMs / 1000,
												arrivalResource: stage.arrivalResource ?? null,
												// Port-only, not native: when the Experimental homing
												// projectiles option is on, a straight shot at one other
												// actor follows it (stepHomingProjectile); natively
												// CProjectileTrace_Process (8D8580) keeps its captured
												// launch start (+0x58) and end (+0x64) vectors.
												homing: gameplay?.homingProjectiles === true && !curved && !arc &&
														!radial && !chain &&
														!targetLocal && targetGid !== gameplay?.localGid &&
														stage.action === "AT_MOV_1TAR" ?
													{
														state: {
															position: source,
															previous: trigger.at + motion.delayMs / 1000
														},
														target: targetGid,
														bone: stage.targetBone ?? null,
														addHeight: stage.targetAddHeight ?? false,
														offset: targetOffset,
														trigger
													} :
													undefined,
												soundEnd: stage.soundEnd ?? null,
												soundBegin: curved && motion.delayMs !== 0 ? null : stage.sound ?? null,
												moving: route ?
													{
														pose: source,
														previous: trigger.at,
														delay: motion.delayMs / 1000,
														travelled: 0,
														age: 0,
														route
													} :
													undefined
											} :
											undefined;

									const resultIndex = impactIndex( cast.skill, trigger.phase, trigger.event );
									const impact = supportedFlight && dispatch.owns && resultIndex >= 0 ?
										{
											cast,
											target: targetGid,
											index: resultIndex,
											allTargets: dispatch.all,
											secondary: record.secondaryEffect ?? false,
											atTarget: targetLocal,
											soundSkill:
												impactSource( cast.caster, 0, gameplay?.attachedEffects ?? [], skill =>
													catalog?.[String( skill )]?.attachedAction, cast ).skill
										} :
										undefined;
									if ( flight && arc?.rotationRadians && resource?.endsWith( ".bsr" ) ) {
										flight.orientation = source;
									}
									if ( impact && impact.index >= 0 ) {
										impactEvents.push( { kind: "launch", flight: gid, ...impact, at: trigger.at } );
									}
									// 8D95FF -> 8D8104 uses source GID +168, not
									// destination +16C, for the exact CICUser test.
									const auxiliary = traded?.auxiliary ?? [];
									if ( script.kind === "arrow" && effectDetail > 1 ) {
										for ( const [index, path] of (record.arrowEffects ?? []).entries() ) {
											if ( !path ) {
												continue;
											}
											const child = modelFor( path );
											if ( !child ) {
												unsupported.add( path );
												continue;
											}
											const fade = !flight && index === 0 ? 3 : 0;
											auxiliary.push( {
												actor: {
													gid: allocate(),
													model: child.glb,
													clip: child.clips[0] ?? "",
													time: trigger.at,
													loop: true,
													scale: 1,
													pickable: false,
													pose: resource?.endsWith( ".bsr" ) ?
														source :
														{ ...source, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
													// 8D4020: bone vfunc +0x44 null keeps the owner world
													// matrix and still adds the offset. bKeepRotation (+8)
													// == 0 resets the rotation. Only this .bsr Bone01 path.
													// Its socket desc 0xCCC8B8 is {"Bone01", offset 0,
													// keep 1}, and the program is an .efp: the same 8D6880
													// native basis as every stage .efp on a named bone, so
													// it draws particle x Bone01 x arrow in native space.
													attachment: resource?.endsWith( ".bsr" ) ?
														{
															gid,
															bone: "Bone01",
															offset: [ 0, 0, 0 ],
															rootIfMissing: true,
															basis: stageAttachmentBasis( path )
														} :
														undefined
												},
												fade,
												previous: trigger.at,
												progress: 0,
												opacity: fade ? 0 : 1,
												expired: false
											} );
										}
									}
									if (
										stage.resource === "weapon" && current.has( cast.token ) &&
										castStates.get( cast.token )?.cancelledAtMs === undefined
									) {
										weaponOwners.set( cast.token, cast.caster );
									}
									active.set( gid, {
										auxiliary,
										visualStarted: traded ?
											(visualStarts.get( gid ) ?? traded.actor.time) :
											undefined,
										command: !flight &&
												[ "AT_STOP", "AT_ONE_FOLLOW", "AT_LOOP" ].includes( stage.action ) ?
											{
												slot: stage.native?.slot ?? 0,
												fade: (stage.native?.fadeInMs ?? 0) / 1000,
												loop: stage.action === "AT_LOOP"
											} :
											undefined,
										family: route ?
											{
												fadeIn: (stage.native?.fadeInMs ?? 0) / 1000,
												fadeOut: (stage.native?.fadeOutMs ?? -1) / 1000,
												stopEmission: chain || stage.native?.fadeOutMs === 0,
												end: null,
												previous: trigger.at,
												progress: 0,
												opacity: stage.native?.fadeInMs ? 0 : 1
											} :
											undefined,
										camera: flight && script.kind === "camera" && script.arrival ?
											{ target: cast.caster, script } :
											undefined,
										independent: !!flight || targetFollow,
										impact,
										flight,
										owner: entity.gid,
										token: cast.token,
										life: route || targetFollow ?
											0 :
											targetLocal && !flight ?
											(stage.movement?.delayMs ?? 0) / 1000 :
											stage.life,
										attachment:
											targetFollow || [ "AT_ONE_FOLLOW", "AT_LOOP" ].includes( stage.action ) ?
												{
													kind: "entity",
													offset: [ ...(targetFollow ? targetOffset : stage.offset) ],
													...(victimFacing !== undefined ? { facing: victimFacing } : {})
												} :
												{ kind: "world" },
										actor: {
											gid,
											effectBasis: flight && resource?.endsWith( ".bsr" ) ?
												importedProjectileBasis( source, destination, true, spawnYaw ) :
												undefined,
											effectRotation,
											pickable: false,
											attachment: targetFollow ?
												stageAttachment( holder, stageBinding( stage, true ), stage.resource ) :
												(stage.bone ||
														[ "AT_ONE_FOLLOW", "AT_LOOP" ].includes( stage.action )) &&
													!supportedFlight && !targetLocal ?
												{
													...stageAttachment(
														holder,
														stageBinding( stage, false ),
														stage.resource
													),
													...(victimFacing !== undefined && !stage.bone ?
														{ facing: victimFacing } :
														{})
												} :
												undefined,
											model: traded?.actor.model ?? model.glb,
											pose: source,
											clip: traded?.actor.clip ?? model.clips[0] ?? "",
											time: trigger.at,
											// A flight's object lives until its arrival removes it
											// (8D8AD0). Inferred: its effect runs as long, so a
											// one-shot .efp shorter than the flight (Cold Wave
											// Arrest's 10-frame shot, about 0.33 s against 0.5 to
											// 0.8 s of flight) loops instead of vanishing mid-air
											// before the impact.
											loop: traded?.actor.loop ??
												(stage.action === "AT_LOOP" ||
													!!flight && (model.clipLoop || stage.resource.endsWith( ".efp" ))),
											scale: traded?.actor.scale ??
												(stage.resource.endsWith( ".efp" ) ?
													stageEffectScale(
														stage.native?.scale,
														presentation.get( cast.caster )?.effectBaseScale ??
															presentation.get( cast.caster )?.scale ?? 1,
														presentation.get( cast.caster )?.heightFactor
													) :
													flight && stage.resource !== "weapon" ?
													(presentation.get( cast.caster )?.effectBaseScale ?? 1) :
													1),
											absoluteEffectScale: stage.resource.endsWith( ".efp" )
										}
									} );
								}
							}
						}
					}
				}
				for ( const token of seen ) {
					if ( !current.has( Number( token.split( ":", 1 )[0] ) ) ) {
						seen.delete( token );
					}
				}
				const result: CharacterActor[] = [];
				for ( let i = crowEffects.length - 1; i >= 0; i-- ) {
					const burst = crowEffects[i]!;
					burst.started ??= now;
					const age = now - burst.started;
					if ( age >= 1.2 ) {
						crowEffects.splice( i, 1 );
						continue;
					}
					if ( burst.cue > 0 ) {
						// Native unlit materials bypass instance tint; recovery colors live in the textures.
						const model = burst.cue === 1 ?
							"/assets/effects/crow-heal-wisp" :
							burst.cue === 4 ?
							"/assets/effects/crow-mana-wisp" :
							"/assets/effects/crow-feather";
						if ( !ready( model ) ) continue;
						const tint: readonly [number, number, number] = burst.cue === 2 ?
							[ .8, .65, .4 ] :
							burst.cue >= 6 && burst.cue <= 7 ?
							[ 1, .83, .4 ] :
							[ .8, .9, .83 ];
						const recovery = burst.cue === 1 || burst.cue === 4;
						const u = age / 1.2, count = (3 + burst.book) * (recovery ? 2 : 1);
						for ( let feather = 0; feather < count; feather++ ) {
							const mote = recovery && feather % 2 === 1;
							if ( recovery ) {
								const phase = (u + feather * .61803398875) % 1;
								const mana = burst.cue === 4, angle = feather * 2.39996323 + phase * (mana ? 2.4 : .8);
								const radius = mana ? 1 + 9 * (1 - phase) : 3 + Math.sin( feather * 1.7 ) * 1.4;
								result.push( {
									gid: burst.gids[feather]!,
									model,
									clip: "effect",
									time: age,
									loop: false,
									scale: mote ? .6 : 1.1 + burst.book * .1,
									absoluteEffectScale: true,
									effectEntity: true,
									pickable: false,
									opacity: Math.sin( Math.PI * u ) * Math.sin( Math.PI * phase ) * (mote ? .3 : .55),
									pose: {
										...burst.pose,
										x: burst.pose.x + Math.cos( angle ) * radius,
										y: burst.pose.y + (mana ? 7 + phase * 7 : 2 + phase * 23),
										z: burst.pose.z + Math.sin( angle ) * radius,
										yaw: radians( -angle )
									}
								} );
								continue;
							}
							const angle = age * (burst.cue === 5 ? 7 : 4) + feather * Math.PI * 2 / count;
							const radius = burst.cue === 2 ?
								8 + 2 * u :
								burst.cue === 6 ?
								10 :
								burst.cue === 8 ?
								8 + 30 * u :
								burst.cue === 3 ?
								6 + 22 * u :
								5 + 11 * u;
							const height = burst.cue === 2 ?
								1 + Math.sin( u * Math.PI ) * 2 :
								burst.cue === 5 ?
								2 + Math.sin( u * Math.PI ) * 5 :
								burst.cue === 6 ?
								24 + Math.sin( u * Math.PI ) * 3 :
								burst.cue === 8 ?
								5 :
								5 + u * 22;
							result.push( {
								gid: burst.gids[feather]!,
								model,
								clip: "effect",
								time: age,
								loop: false,
								scale: burst.cue === 7 ? 4 : 2.2 + burst.book * .3,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: .65 * Math.sin( Math.PI * u ),
								materialTint: tint,
								pose: {
									...burst.pose,
									x: burst.pose.x + Math.cos( angle ) * radius,
									y: burst.pose.y + height,
									z: burst.pose.z + Math.sin( angle ) * radius,
									yaw: radians( -angle )
								}
							} );
						}
						continue;
					}
					if ( !ready( "/assets/effects/crow-wind-burst" ) ) continue;
					for ( let band = 0; band < 3; band++ ) {
						const u = (age - band * .1) / .9;
						if ( u < 0 || u >= 1 ) continue;
						result.push( {
							gid: burst.gids[band]!,
							model: "/assets/effects/crow-wind-burst",
							clip: "effect",
							time: age,
							loop: false,
							scale: 8 + 72 * u,
							absoluteEffectScale: true,
							effectEntity: true,
							pickable: false,
							opacity: .24 * Math.sin( Math.PI * u ),
							materialTint: [ .8, .9, .86 ],
							pose: {
								...burst.pose,
								y: burst.pose.y + 1 + band * 2,
								yaw: radians( age * 5 + band * 2 )
							}
						} );
					}
				}
				const penguin = gameplay?.petTraining?.skills.some( skill => skill.id >= 31 && skill.id <= 45 ) ?
					gameplay.petTraining.gid :
					0;
				for (
					const actor of penguinBolts.step(
						gameplay?.casts ?? [],
						penguin,
						byGid,
						presentation,
						now,
						gameplay?.petTraining?.skills.find( skill => skill.queued )?.id ?? 0
					)
				) {
					if ( ready( actor.model ) ) result.push( actor );
				}
				// Port-only: quiet Resolve ring follows the bear; never changes its action.
				for ( const entity of byGid.values() ) {
					const resolve = entity.petResolve;
					if ( !resolve?.stacks || resolve.untilMs <= now * 1000 ) continue;
					let gid = spiritRings.get( entity.gid );
					if ( gid === undefined ) {
						gid = allocate();
						spiritRings.set( entity.gid, gid );
					}
					const model = "/assets/effects/pet-counter-ripple";
					if ( !ready( model ) ) continue;
					const pose = presentation.get( entity.gid )?.pose ??
						{
							regionId: entity.regionId,
							x: entity.x,
							y: entity.y,
							z: entity.z,
							yaw: nativeHeadingYaw( entity.heading )
						};
					result.push( {
						gid,
						model,
						clip: "effect",
						time: now,
						loop: true,
						pose,
						scale: 16 + resolve.stacks * .4,
						absoluteEffectScale: true,
						effectEntity: true,
						pickable: false,
						opacity: .035 + resolve.stacks * .005 +
							(resolve.stacks === 10 ? .015 * Math.sin( now * 3 ) : 0),
						materialTint: [ 1, .72, .32 ]
					} );
				}
				for ( const gid of spiritRings.keys() ) if ( !byGid.has( gid ) ) spiritRings.delete( gid );
				// Port-only: gold recovery radiance follows a stationary or moving recipient immediately.
				for ( const entity of byGid.values() ) {
					const cue = entity.petBondPulse;
					if ( !cue ) continue;
					let pulse = bondPulses.get( entity.gid );
					if ( !pulse || pulse.revision !== cue.revision ) {
						const sounded = pulse?.sounded ?? -Infinity;
						pulse = { revision: cue.revision, gid: allocate(), started: now, sounded };
						bondPulses.set( entity.gid, pulse );
						// Damage ticks must not produce a wall of repeated healing sounds.
						if ( cue.kind === 2 || now - sounded >= 1 ) {
							pulse.sounded = now;
							play( {
								id: `pet-bond:${entity.gid}:${cue.revision}:${cue.atMs}`,
								path: cue.kind === 2 ? PET_BOND_REVIVE_SOUND : PET_BOND_TRANSFER_SOUND,
								gain: cue.kind === 2 ? .65 : .2,
								...audioPosition( entity ),
								expires: now + .25
							} );
						}
					}
					const age = now - pulse.started, u = age / (cue.kind === 2 ? 1.5 : .4);
					const model = cue.kind === 2 ?
						"/assets/effects/iron-hide-shield" :
						"/assets/effects/pet-counter-ripple";
					if ( u >= 0 && u < 1 && ready( model ) ) {
						const pose = presentation.get( entity.gid )?.pose ?? {
							regionId: entity.regionId,
							x: entity.x,
							y: entity.y,
							z: entity.z,
							yaw: nativeHeadingYaw( entity.heading )
						};
						result.push( {
							gid: pulse.gid,
							model,
							clip: "effect",
							time: age,
							loop: false,
							pose: { ...pose, y: pose.y + (cue.kind === 2 ? 20 * u : 0) },
							scale: cue.kind === 2 ? 16 + 12 * u : 4 + 16 * u,
							absoluteEffectScale: true,
							effectEntity: true,
							pickable: false,
							opacity: (cue.kind === 2 ? .35 : .2) * Math.sin( Math.PI * u ),
							materialTint: [ 1, .85, .45 ]
						} );
					}
				}
				for ( const gid of bondPulses.keys() ) if ( !byGid.has( gid ) ) bondPulses.delete( gid );
				// Port-only: a quiet, fixed-origin 8 m ripple when the server releases retaliation III.
				for ( const entity of byGid.values() ) {
					if ( !entity.petCounterPulse ) continue;
					let pulse = counterPulses.get( entity.gid );
					if ( !pulse || pulse.revision !== entity.petCounterPulse.revision ) {
						pulse = {
							revision: entity.petCounterPulse.revision,
							gid: allocate(),
							started: now,
							pose: {
								...(presentation.get( entity.gid )?.pose ??
									{
										regionId: entity.regionId,
										x: entity.x,
										y: entity.y,
										z: entity.z,
										yaw: nativeHeadingYaw( entity.heading )
									})
							}
						};
						counterPulses.set( entity.gid, pulse );
					}
					const age = now - pulse.started, u = Math.min( 1, age / .55 );
					if ( age >= 0 && u < 1 && ready( "/assets/effects/pet-counter-ripple" ) ) {
						result.push( {
							gid: pulse.gid,
							model: "/assets/effects/pet-counter-ripple",
							clip: "effect",
							time: age,
							loop: false,
							pose: pulse.pose,
							scale: 2 + 78 * (1 - Math.pow( 1 - u, 2 )),
							absoluteEffectScale: true,
							effectEntity: true,
							pickable: false,
							opacity: .22 * Math.sin( Math.PI * u ),
							materialTint: [ .85, .85, .82 ]
						} );
					}
				}
				for ( const gid of counterPulses.keys() ) if ( !byGid.has( gid ) ) counterPulses.delete( gid );
				// Port-only world-size shells follow each recipient pose, without inheriting model/root transforms.
				for ( const entity of byGid.values() ) {
					const shield = entity.shield;
					if ( !shield ) continue;
					let visual = shields.get( entity.gid );
					const newWard = !visual || visual.burst || now >= visual.until;
					if (
						newWard && (!visual || visual.revision !== shield.revision) && entity.kind === "cos" &&
						shield.amount > 0 && shield.untilMs - shield.atMs >= 4750
					) {
						play( {
							id: `pet-shield:${entity.gid}:${shield.revision}:${shield.atMs}`,
							path: PET_SHIELD_SOUND,
							gain: .65,
							...audioPosition( entity ),
							expires: now + .25
						} );
					}
					const nextUntil = now + (shield.untilMs - shield.atMs) / 1000;
					if ( !visual ) {
						visual = {
							revision: shield.revision,
							gid: allocate(),
							innerGid: allocate(),
							ringGid: allocate(),
							at: now,
							until: nextUntil,
							burst: shield.burst,
							shook: false
						};
						shields.set( entity.gid, visual );
					} else if ( visual.revision !== shield.revision ) {
						visual.revision = shield.revision;
						if ( shield.burst ) {
							if ( !visual.burst ) {
								visual.at = now;
								visual.shook = false;
							}
							visual.burst = true;
							visual.until = nextUntil;
						} else if ( !visual.burst && shield.amount > 0 && now < visual.until ) {
							// Periodic 500ms server sync or partial absorption: keep visual.at so the
							// barrier rotation and pulse continue smoothly without restarting.
							visual.until = nextUntil;
						} else {
							visual.at = now;
							visual.until = nextUntil;
							visual.burst = false;
							visual.shook = false;
						}
					}
					const age = Math.max( 0, now - visual.at );
					const burst = visual.burst && age < 0.75;
					const activeShield = shield.amount > 0 && now < visual.until;
					if ( !burst && !activeShield ) continue;
					const pose = presentation.get( entity.gid )?.pose ?? {
						regionId: entity.regionId,
						x: entity.x,
						y: entity.y,
						z: entity.z,
						yaw: nativeHeadingYaw( entity.heading )
					};
					const baseScale = entity.kind === "cos" ? 26 : 15;
					if ( burst ) {
						if ( !visual.shook ) {
							visual.shook = true;
							play( {
								id: `pet-shield-burst:${entity.gid}:${shield.revision}:${shield.atMs}`,
								path: PET_SHIELD_BURST_SOUND,
								gain: .35,
								...audioPosition( pose ),
								expires: now + .25
							} );
							cameraEvents.push( {
								atMs: Math.trunc( now * 1000 ),
								amplitude: 50,
								durationMs: 500,
								periodMs: 20
							} );
						}
						const u = Math.min( 1, age / 0.75 );
						const ease = 1 - Math.pow( 1 - u, 2.2 );
						if ( ready( "/assets/effects/challenging-roar-shockwave" ) ) {
							result.push( {
								gid: visual.gid,
								model: "/assets/effects/challenging-roar-shockwave",
								clip: "effect",
								time: age,
								loop: false,
								pose,
								scale: 6 + 74 * ease,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: (1 - Math.pow( u, 1.5 )) * 0.20,
								materialTint: [ 0.62, 0.92, 1.0 ],
								effectRotation: { axis: "y", angle: radians( age * 2.8 ) }
							} );
							if ( age >= 0.06 ) {
								const u2 = Math.min( 1, (age - 0.06) / 0.69 );
								const ease2 = 1 - Math.pow( 1 - u2, 2.0 );
								result.push( {
									gid: visual.ringGid,
									model: "/assets/effects/challenging-roar-shockwave",
									clip: "effect",
									time: age - 0.06,
									loop: false,
									pose,
									scale: 4 + 76 * ease2,
									absoluteEffectScale: true,
									effectEntity: true,
									pickable: false,
									opacity: (1 - Math.pow( u2, 1.4 )) * 0.12,
									materialTint: [ 1.0, 0.88, 0.52 ],
									effectRotation: { axis: "y", angle: radians( -age * 2.2 ) }
								} );
							}
						}
						if ( age < 0.45 && ready( "/assets/effects/iron-hide-shield" ) ) {
							const su = age / 0.45;
							result.push( {
								gid: visual.innerGid,
								model: "/assets/effects/iron-hide-shield",
								clip: "effect",
								time: age,
								loop: false,
								pose,
								scale: baseScale + (80 - baseScale) * (1 - Math.pow( 1 - su, 2.0 )),
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: (1 - su) * 0.16,
								materialTint: [ 0.78, 0.95, 1.0 ],
								effectRotation: { axis: "y", angle: radians( age * 4.5 ) }
							} );
						}
					} else if ( ready( "/assets/effects/iron-hide-shield" ) ) {
						const flash = Math.max( 0, 1 - age / 0.45 );
						const pulse = 1 + 0.04 * Math.sin( age * 6 );
						result.push( {
							gid: visual.gid,
							model: "/assets/effects/iron-hide-shield",
							clip: "effect",
							time: age,
							loop: true,
							pose,
							scale: baseScale * pulse,
							absoluteEffectScale: true,
							effectEntity: true,
							pickable: false,
							opacity: 0.12 + 0.08 * flash,
							materialTint: [ 0.58 + 0.32 * flash, 0.86 + 0.14 * flash, 1.0 ],
							effectRotation: { axis: "y", angle: radians( age * 1.6 ) }
						} );
						result.push( {
							gid: visual.innerGid,
							model: "/assets/effects/iron-hide-shield",
							clip: "effect",
							time: age,
							loop: true,
							pose,
							scale: baseScale * 0.88 * (2 - pulse),
							absoluteEffectScale: true,
							effectEntity: true,
							pickable: false,
							opacity: 0.06 + 0.04 * flash,
							materialTint: [ 0.85, 0.96, 1.0 ],
							effectRotation: { axis: "y", angle: radians( -age * 2.3 ) }
						} );
					}
				}
				for ( const gid of shields.keys() ) {
					if ( !byGid.has( gid ) ) shields.delete( gid );
				}
				// 74F540 / 854410: external item producers retain their own
				// captured host; they never borrow a combat cast or its lifetime.
				for ( const [id, row] of external ) {
					const { event } = row, reference = itemReference?.( event.item );
					if ( !catalog || !namedCatalog || !models || !reference ) continue;
					const flags = event.typeFlags,
						consumable = !(flags & 2) && (flags & 0x1c) === 0xc && (flags & 0x60) === 0x60,
						category = flags & 0x780,
						subtype = flags >>> 11;
					const firework = consumable && category === 0x300;
					let record: import("@/engine/contracts/effects").EffectRecord | undefined;
					if ( consumable ) {
						const potion = category === 0x180 ?
							4 :
							category === 0x80 ?
							({ 1: 1, 2: 2, 3: 3, 4: 35, 5: 36, 9: 37 } as Record<number, number>)[subtype] :
							undefined;
						if ( potion !== undefined ) record = catalog[String( (0x80000000 + potion) | 0 )];
						else if ( category === 0x100 ) {
							record = namedCatalog[
								subtype === 1 || subtype === 6 ?
									"PARAM_CURE_ALL" :
									subtype === 7 ?
									"STATUS_CURE_COS" :
									""
							];
						}
					}
					record ??= namedCatalog[reference.codename];
					if ( !record ) {
						if ( firework ) {
							unsupported.add( `item:${reference.codename}: missing retail named effect record` );
						}
						external.delete( id );
						continue;
					}
					const stages = record.stages.filter( s => s.phase === "ACT_S" && s.startEvent === 0 );
					const count = firework ? (stages[0]?.count ?? 0) : 1;
					if ( !Number.isInteger( count ) || count < 0 || count > 255 ) {
						throw Error( "Invalid external effect repetition count" );
					}
					const currentOwner = presentation.get( event.source.gid );
					if ( !firework && !byGid.has( event.source.gid ) ) {
						external.delete( id );
						continue;
					}
					if ( !firework && !currentOwner ) continue;
					const host = firework ? reference.model : undefined;
					if ( firework && !host ) continue;
					const base = firework ?
						{
							regionId: event.source.regionId,
							x: event.source.x,
							y: event.source.y,
							z: event.source.z,
							yaw: nativeHeadingYaw( event.source.heading )
						} :
						currentOwner!.pose;
					let admitted = !host || ready( host.glb );
					for ( const stage of stages ) {
						if ( stage.resource ) {
							const model = modelFor( stage.resource );
							if ( !model ) {
								unsupported.add( stage.resource );
								admitted = false;
							} else if ( !ready( model.glb ) ) admitted = false;
						}
					}
					if ( !admitted ) continue;
					const tick = itemEffectClock( row.clock, { type: "admit", now, count } );
					row.clock = tick.state;
					if ( row.clock.phase === "pending" ) continue;
					// One due emission per frame; a stall does not collapse all
					// missed repetitions into a single burst (85442C).
					if ( tick.emit ) {
						row.children.push( { serial: row.clock.emitted, at: now, stages: new Set(), actors: [] } );
					}
					for ( const child of row.children ) {
						for ( let index = 0; index < stages.length; index++ ) {
							const stage = stages[index]!;
							if ( child.stages.has( index ) || now - child.at < (stage.movement?.delayMs ?? 0) / 1000 ) {
								continue;
							}
							if (
								stage.move !== "MOV_NONE" ||
								![ "AT_STOP", "AT_ONE_FOLLOW", "AT_SOURCE", "AT_TARGET", "AT_TARGET_F", "AT_LOOP" ]
									.includes( stage.action ) ||
								stage.scripts.length
							) throw Error( "Unbound external item stage " + reference.codename );
							if ( stage.native?.scale === "CHAR_BASE" && currentOwner?.heightFactor === undefined ) {
								continue;
							}
							const pose = { ...base, y: base.y + (firework ? 8 : 0) };
							if ( stage.sound ) {
								play( {
									id: `item:${id}:${child.serial}:${index}`,
									path: stage.sound,
									gain: 1,
									...audioPosition( pose ),
									expires: now + .25
								} );
							}
							child.stages.add( index );
							if ( !stage.resource || stage.action === "AT_STOP" && effectDetail <= 1 ) continue;
							const model = modelFor( stage.resource )!,
								target = [ "AT_TARGET", "AT_TARGET_F" ].includes( stage.action ),
								offset = target ? (stage.targetOffset ?? [ 0, 0, 0 ]) : stage.offset,
								bone = (target ? stage.targetBone : stage.bone) ?? "",
								follow = stage.action !== "AT_STOP";
							const actor: CharacterActor = {
								gid: allocate(),
								model: model.glb,
								pose: { ...pose, x: pose.x + offset[0], y: pose.y + offset[1], z: pose.z - offset[2] },
								clip: model.clips[0] ?? "",
								time: 0,
								loop: false,
								pickable: false,
								scale: stage.resource.endsWith( ".efp" ) ?
									stageEffectScale(
										stage.native?.scale,
										currentOwner?.effectBaseScale ?? 1,
										currentOwner?.heightFactor
									) :
									1,
								absoluteEffectScale: stage.resource.endsWith( ".efp" ),
								...(follow && !firework ?
									{
										attachment: {
											gid: event.source.gid,
											bone,
											root: !bone,
											basis: stageAttachmentBasis( stage.resource ),
											offset: [ offset[0], offset[1], offset[2] ] as const
										}
									} :
									{})
							};
							child.actors.push( {
								actor,
								at: now,
								life: stage.life || duration( actor.model, actor.clip )
							} );
						}
						child.actors = child.actors.filter( v => now - v.at < v.life );
						for ( const visual of child.actors ) {
							result.push( { ...visual.actor, time: Math.max( 0, now - visual.at ) } );
						}
					}
					row.children = row.children.filter( c => c.stages.size < stages.length || c.actors.length );
					if ( row.clock.phase === "draining" && !row.children.length ) {
						external.delete( id );
						continue;
					}
					if ( host ) {
						result.push( {
							gid: row.hostGid,
							model: host.glb,
							clip: host.clips[0] ?? "",
							time: Math.max( 0, now - row.clock.started ),
							loop: host.clipLoop,
							pose: base,
							pickable: false,
							scale: 1
						} );
					}
				}
				// 777670 -> 8df220: transient system records share authored
				// stage construction, but do not manufacture combat casts.
				for ( const [id, event] of system ) {
					if ( !byGid.has( event.gid ) ) {
						releaseMaterial( attachedKey( { gid: event.gid, skill: event.skill, token: id, phase: 0 } ) );
						system.delete( id );
						continue;
					}
					const owner = presentation.get( event.gid );
					if ( !owner || !catalog || !models ) continue;
					if (
						catalog[String( event.skill )]?.stages.some( s =>
							s.resource?.endsWith( ".efp" ) && s.native?.scale === "CHAR_BASE"
						) && owner.heightFactor === undefined
					) continue;
					if ( !catalog[String( event.skill )] ) {
						system.delete( id );
						continue;
					}
					if ( !event.actors ) {
						event.actors = attachedVisuals(
							{ gid: event.gid, skill: event.skill, token: id, phase: 0 },
							owner,
							now,
							false
						);
						activations.push( { gid: event.gid, skill: event.skill, at: now } );
					}
					if ( !event.actors.length ) {
						releaseMaterial( attachedKey( { gid: event.gid, skill: event.skill, token: id, phase: 0 } ) );
						system.delete( id );
						continue;
					}
					let admitted = true;
					for ( const visual of event.actors ) if ( !ready( visual.actor.model ) ) admitted = false;
					if ( !admitted ) continue;
					event.started ??= now;
					const age = Math.max( 0, now - event.started );
					const remaining = event.actors.filter( v =>
						age < (v.life > 0 ? v.life : duration( v.actor.model, v.actor.clip ))
					);
					if ( !remaining.length ) {
						releaseMaterial( attachedKey( { gid: event.gid, skill: event.skill, token: id, phase: 0 } ) );
						system.delete( id );
						continue;
					}
					for ( const visual of remaining ) {
						result.push( { ...visual.actor, pose: owner.pose, time: age, loop: false } );
					}
				}
				if ( catalog && models ) {
					const wanted = new Set<string>();
					const stateEffects: Attachment[] = [ ...hwan ].filter( ( [, row] ) => row.active ).map( (
						[gid, row]
					) => ({ gid, skill: -2147483648, token: -1, phase: 0, receivedAtMs: row.revision }) );
					for ( const [gid, row] of statuses ) {
						for ( const [recordName, revision] of row.revisions ) {
							stateEffects.push( {
								gid,
								recordName,
								skill: 0,
								token: -1,
								phase: 0,
								receivedAtMs: revision
							} );
						}
					}
					for ( const entity of entities ) {
						const word = (entity.tidWord ?? 0) & 0xfffe;
						if ( entity.kind !== "monster" || entity.tradeVariant === undefined ) continue;
						if ( word !== TRADE_THIEF_WORD && word !== TRADE_HUNTER_WORD ) continue;
						const skill = banditTable.get( entity.refObjId )?.imbue ?? 0;
						if ( skill > 0 ) {
							stateEffects.push( { gid: entity.gid, skill, token: TRADE_IMBUE_TOKEN, phase: 0 } );
						}
					}
					const registrations: Attachment[] = [ ...(gameplay?.attachedEffects ?? []), ...stateEffects ];
					for ( const effect of registrations ) {
						try {
							const key = attachedKey( effect );
							wanted.add( key );
							const owner = presentation.get( effect.gid );
							if ( !owner || attached.has( key ) ) continue;
							// Readiness belongs to the complete stage family. Keep the
							// packet pending until every requested model can be admitted.
							const record = recordFor( effect );
							if ( !record ) continue;
							if (
								record.stages.some( s =>
									s.resource?.endsWith( ".efp" ) && s.native?.scale === "CHAR_BASE" ||
									(s.script ?? effectScript( s.scripts )).kind === "mover"
								) && owner.heightFactor === undefined
							) continue;
							const phases = effect.phase === 1 ?
								[ "ACT_OS", "ACT_OL", "ACT_OE" ] :
								[ "ACT_S", "ACT_L", "DEACT" ];
							let admitted = true;
							for ( const stage of record.stages ) {
								if (
									(phases.includes( stage.phase ?? "" ) ||
										(record.overlap && stage.phase === "ACT_OE")) && stage.resource
								) {
									const model = modelFor( stage.resource );
									if ( !model || !ready( model.glb ) ) admitted = false;
								}
							}
							if ( !admitted ) continue;
							const visuals = attachedVisuals( effect, owner, now, false );
							const stopped = effect.token === 0 && !effect.restored;
							if ( stopped ) {
								attachedVisuals( effect, owner, now, true, visuals );
								for ( const visual of visuals ) releaseAttached( visual, now );
							}
							attached.set( key, {
								effect,
								visuals,
								stopped,
								started: now,
								...(stopped ? { stoppedAt: now } : {})
							} );
							if ( !effect.recordName ) {
								activations.push( { gid: effect.gid, skill: effect.skill, at: now } );
							}
						} catch ( error ) {
							failure = `Attached effect ${effect.skill}, actor ${effect.gid}, token ${effect.token}: ${
								String( error )
							}`;
						}
					}
					for ( const [key, row] of attached ) {
						const owner = presentation.get( row.effect.gid );
						if ( !byGid.has( row.effect.gid ) ) {
							releaseMaterial( key );
							attached.delete( key );
							continue;
						}
						if ( !row.stopped && !wanted.has( key ) ) {
							row.stopped = true;
							row.stoppedAt = now;
							if ( owner ) attachedVisuals( row.effect, owner, now, true, row.visuals );
							for ( const visual of row.visuals ) releaseAttached( visual, now );
						}
						// Active attachments must renew the resource plan on every frame,
						// including their release tail. Assembly retention alone does not
						// retain the resource owner's duration metadata or budget charge.
						const resident = new Set<string>();
						for ( const visual of row.visuals ) {
							if ( ready( visual.actor.model ) ) resident.add( visual.actor.model );
						}
						row.visuals = row.visuals.filter( v =>
							v.released !== undefined ?
								v.actor.model.includes( "/effects/programs.json#" ) &&
								now - v.released < (v.fade || duration( v.actor.model, v.actor.clip )) :
								v.keep || now - v.started < (v.life || duration( v.actor.model, v.actor.clip ))
						);
						if ( owner ) {
							for ( const visual of row.visuals ) {
								if ( visual.hawk ) {
									const hawk = visual.hawk,
										command = gameplay?.attachedEffects?.find( e => attachedKey( e ) === key )
											?.hawk;
									if ( command && command.revision !== hawk.revision ) {
										hawk.state = hawkEvent( hawk.state, { type: "command", ...command } );
										hawk.revision = command.revision;
									}
									const delta = Math.max( 0, clock - hawk.previous );
									hawk.previous = clock;
									const definition = hawk.states[hawkAnimation( hawk.state )]!,
										previous = hawk.state.animationMs,
										cursor = previous + delta;
									if ( hawk.state.phase === "attack" ) {
										if (
											definition.trackEvents.some( event =>
												event.eventCode === 1 && event.cursorMs >= previous &&
												event.cursorMs < cursor
											) && byGid.has( hawk.state.target )
										) {
											hawkImpacts.push( {
												resultKey: `hawk:${row.effect.token}:${hawk.revision}`,
												id: visual.actor.gid,
												holder: row.effect.gid,
												skill: row.effect.skill,
												target: hawk.state.target,
												damage: hawk.state.damage,
												at: now
											} );
											// 8E3800 supplies the normal damage resource; its u16
											// high bit is fatal. 8D5440 preserves the hawk basis.
											const resource = catalog[String( row.effect.skill )]?.damageEffect,
												target = byGid.get( hawk.state.target )!,
												anchor = presentation.get( target.gid )?.effectAnchor;
											if (
												effectDetail && resource && anchor &&
												projectileSpace( hawk.region, target.regionId )
											) {
												const cast = {
													token: visual.actor.gid,
													caster: row.effect.gid,
													target: target.gid,
													skill: row.effect.skill,
													damage: hawk.state.damage & 0x7fff,
													fatal: false
												};
												const joint = anchor.bone ?
													socket?.( target.gid, anchor.bone, [ 0, 0, 0 ], {
														cast,
														phase: "SHOT",
														event: 0,
														at: now,
														sampleCurrent: true
													} ) :
													undefined;
												if ( !anchor.bone || joint ) {
													let [, ay, az] = anchor.offset;
													if ( joint ) {
														ay += joint.y - target.y;
														az += joint.z - target.z;
													}
													const [dx, dz] = worldDelta( target, {
															regionId: hawk.region,
															x: hawk.state.position.x,
															z: hawk.state.position.z
														} ),
														dy = hawk.state.position.y - target.y,
														length = hypot3( dx, dy, dz ),
														k = length ? -az / length : 0;
													const blood = presentation.get( target.gid )?.bloodEffects
														?.[bloodEnabled ? 0 : 1];
													for ( const path of [ resource, ...(blood ? [ blood ] : []) ] ) {
														const gid = allocate();
														active.set( gid, {
															actor: {
																gid,
																model: modelFor( path )!.glb,
																clip: "effect",
																loop: false,
																time: now,
																scale: 1,
																pickable: false,
																pose: {
																	regionId: target.regionId,
																	x: target.x + dx * k,
																	y: target.y + ay + dy * k,
																	z: target.z + dz * k,
																	yaw: radians( Math.PI - hawk.state.yaw )
																}
															},
															owner: row.effect.gid,
															token: row.effect.token,
															independent: true,
															life: 0,
															attachment: { kind: "world" }
														} );
													}
												}
											}
											hawk.state = hawkEvent( hawk.state, {
												type: "impact",
												targetExists: true
											} );
										} else if (
											definition.trackEvents.some( event =>
												event.eventCode === 1 && event.cursorMs >= previous &&
												event.cursorMs < cursor
											)
										) {
											hawk.state = hawkEvent( hawk.state, {
												type: "impact",
												targetExists: false
											} );
										}
										hawk.state = { ...hawk.state, animationMs: cursor };
										if ( cursor >= definition.durationMs ) {
											hawk.state = hawkEvent( hawk.state, { type: "animation-end" } );
										}
									} else {hawk.state = {
											...hawk.state,
											animationMs: definition.loop ?
												cursor % definition.durationMs :
												Math.min( cursor, definition.durationMs )
										};}
									// A target the presenter cannot place (dead and
									// faded, culled, or unloaded while its entity
									// lingers) is no target: the hawk returns to its
									// holder (stepHawk's rule for a lost target).
									// Skipping the frame instead froze the hawk unseen
									// for as long as that entity stayed listed.
									const holder = byGid.get( row.effect.gid )!,
										listed = byGid.get( hawk.state.target ),
										placed = listed ? presentation.get( listed.gid ) : undefined,
										target = placed?.heightFactor !== undefined ? listed : undefined,
										targetActor = target ? placed : undefined;
									const region = owner.pose.regionId;
									if ( !projectileSpace( hawk.region, region ) ) continue;
									const point = ( p: { regionId: number; x: number; y: number; z: number; } ) => {
										const [x, z] = regionRelativeXZ( region, p );
										return { x, y: p.y, z };
									};
									hawk.state = {
										...hawk.state,
										position: point( { ...hawk.state.position, regionId: hawk.region } )
									};
									hawk.region = region;
									// The hawk steers by its mechanical holder: game
									// logic, so it keeps the movement owner's logical
									// pose; only its visual rides the drawn holder.
									const mechanical = holder.gid === gameplay?.localGid && gameplay.pose ?
										gameplay.pose :
										holder;
									hawk.state = stepHawk( hawk.state, {
										deltaMs: delta,
										holder: point( mechanical ),
										displayedHolder: owner.pose,
										holderYaw: Math.fround( Math.PI - owner.pose.yaw ),
										holderHeightFactor: owner.heightFactor!,
										target: target && projectileSpace( region, target.regionId ) ?
											{
												...point( target ),
												heightFactor: targetActor!.heightFactor!,
												dead: target.appearanceState?.[0] === 2
											} :
											undefined
									} );
									const animation = hawk.states[hawkAnimation( hawk.state )]!;
									if ( resident.has( visual.actor.model ) ) {
										result.push( {
											...visual.actor,
											pose: {
												...hawk.state.position,
												regionId: region,
												yaw: radians( Math.PI - hawk.state.yaw )
											},
											clip: animation.clip,
											time: hawk.state.animationMs / 1000,
											loop: animation.loop
										} );
									}
									continue;
								}
								stepRelease( visual, now );
								if ( !resident.has( visual.actor.model ) ) continue;
								const period = duration( visual.actor.model, visual.actor.clip ),
									cycle = !visual.actor.model.includes( "/effects/programs.json#" ) &&
											visual.released !== undefined && visual.actor.loop && period ?
										Math.floor( (visual.released - visual.started) / period ) * period :
										0;
								result.push( {
									...visual.actor,
									pose: owner.pose,
									time: Math.max( 0, now - visual.started - cycle ),
									loop: visual.released === undefined && visual.actor.loop,
									emissionEnd: visual.released === undefined || !visual.actor.loop ?
										undefined :
										visual.released - visual.started - cycle,
									opacity: visual.opacity
								} );
							}
						}
						if ( row.stopped && !row.visuals.length ) {
							releaseMaterial( key );
							if ( !wanted.has( key ) && now - (row.stoppedAt ?? now) >= .2 ) attached.delete( key );
						}
					}
					// Past the budget the farthest attached effects give way; the
					// frame and every other effect still draw.
					budgetGaveWay += trimToEffectBudget(
						result,
						EFFECT_ACTOR_BUDGET - active.size,
						localPose
					);
				}
				for ( const [gid, original] of active ) {
					let visual = original;
					const entity = byGid.get( visual.owner );
					if ( visual.attachment.kind === "entity" && !entity ) {
						if ( visual.impact ) {
							impactEvents.push( { kind: "discard", flight: gid, ...visual.impact, at: now } );
						}
						active.delete( gid );
						continue;
					}
					if (
						visual.landed && (visual.auxiliary ?? []).every( a =>
							a.expired ||
							a.started !== undefined &&
								now - visual.landed!.at >= duration( a.actor.model, a.actor.clip )
						)
					) {
						active.delete( gid );
						continue;
					}
					let elapsed = Math.max( 0, now - visual.actor.time );
					let flightPose: import("@/engine/contracts/character").CharacterActor["pose"] | undefined;
					const command = visual.command;
					if ( command ) {
						if (
							!current.has( visual.token ) ||
							castStates.get( visual.token )?.cancelledAtMs !== undefined || !entity
						) releaseCommand( command, now );
						stepRelease( command, now );
					}
					if (
						command?.released !== undefined &&
						(!visual.actor.model.includes( "/effects/programs.json#" ) ||
							now - command.released >=
								(command.fade || duration( visual.actor.model, visual.actor.clip )))
					) {
						active.delete( gid );
						continue;
					}
					if (
						visual.family?.end &&
						(visual.family.end.remove ||
							visual.family.fadeIn > 0 && now - visual.family.end.at >= visual.family.fadeIn)
					) {
						active.delete( gid );
						continue;
					}
					if ( visual.flight ) {
						const flight = visual.flight;
						let sample: ReturnType<typeof sampleProjectile>;
						if ( flight.moving ) {
							const moving = flight.moving, delta = Math.max( 0, now - moving.previous );
							moving.previous = now;
							moving.delay -= delta;
							if ( moving.delay > 0 ) sample = { phase: "delay" };
							else {
								// 8D9360 executes the entire frame step on delay expiry;
								// neither residual births nor chain hops catch up in a loop.
								const distance = Math.fround( flight.speed * Math.fround( delta ) );
								moving.age += delta;
								moving.travelled = Math.fround( moving.travelled + distance );
								const next = stepMoving( moving.pose, flight.destination, distance );
								moving.pose = next.pose;
								sample = next.arrived ?
									{ phase: "arrived", at: elapsed, pose: next.pose } :
									{ phase: "travel", pose: next.pose };
								if (
									!next.arrived && moving.route.kind === "radial" &&
									moving.travelled > moving.route.spacing
								) {
									moving.travelled = 0;
									spawnAt( visual, next.pose, now );
								}
							}
						} else if ( flight.curve ) {
							const clock = flight.curve, ms = Math.max( clock.previousMs, Math.trunc( elapsed * 1000 ) );
							const live = advanceProjectileCurve( clock.state, ms - clock.previousMs );
							clock.previousMs = ms;
							const [x, y, z] = clock.state.position,
								start = visual.actor.pose,
								next = normalizeRegionXZ( start.regionId, x, z );
							sample = {
								phase: "travel",
								pose: {
									regionId: next.regionId,
									x: next.x,
									y,
									z: next.z,
									yaw: start.yaw
								}
							};
							if ( !live ) {
								cameraArrival( visual, now, byGid );
								if ( visual.impact ) {
									impactEvents.push( {
										kind: visual.impact.allTargets || byGid.has( visual.impact.target ) ?
											"arrival" :
											"discard",
										flight: gid,
										...visual.impact,
										at: now,
										position: sample.pose
									} );
								}
								active.delete( gid );
								continue;
							}
						} else if ( flight.homing && elapsed >= flight.delay ) {
							// Re-aim at the target's live socket; a target that left
							// keeps the last one.
							const homing = flight.homing, entity = byGid.get( homing.target );
							const live = !entity ?
								undefined :
								stageAnchor(
									entity,
									homing,
									{ ...homing.trigger, at: now, sampleCurrent: true },
									flight.destination.yaw
								);
							if ( live && projectileSpace( homing.state.position.regionId, live.regionId ) ) {
								flight.destination = live;
							}
							const step = stepHomingProjectile( homing.state, flight.destination, flight.speed, now );
							sample = step.phase === "arrived" ? { ...step, at: step.at - visual.actor.time } : step;
						} else {sample = sampleProjectile(
								visual.actor.pose,
								flight.destination,
								flight.speed,
								flight.delay,
								elapsed,
								flight.arc
							);}
						if ( sample.phase !== "delay" && flight.soundBegin ) {
							const pose = visual.actor.pose;
							play( {
								id: `launch:${gid}`,
								path: flight.soundBegin,
								gain: 1,
								...audioPosition( pose ),
								expires: visual.actor.time + flight.delay + 0.25
							} );
							flight.soundBegin = null;
						}
						switch ( sample.phase ) {
							case "delay":
								if ( visual.family ) visual.family.previous = now;
								ready( visual.actor.model );
								continue;
							case "travel":
								flightPose = sample.pose;
								elapsed = flight.moving?.age ?? elapsed - flight.delay;
								break;
							case "arrived": {
								const at = visual.actor.time + sample.at;
								if ( flight.moving ) {
									const moving = flight.moving, route = moving.route;
									if ( flight.arrivalResource ) {
										spawnAt( visual, sample.pose, at, flight.arrivalResource );
									}
									if ( route.kind === "chain" ) {
										// 8D8AD0's initial arrival has no pending result.
										// Mark each next entry before resolving its live socket.
										if ( route.pending !== null && visual.impact ) {
											impactEvents.push( {
												...visual.impact,
												kind: byGid.has( route.pending ) ? "hop" : "skip",
												flight: gid,
												target: route.pending,
												at,
												position: moving.pose
											} );
										}
										route.pending = null;
										while ( route.cursor < route.targets.length ) {
											const target = route.targets[route.cursor++]!, entity = byGid.get( target );
											const endpoint = entity ?
												stageAnchor(
													entity,
													route,
													{ ...route.trigger, at: now, sampleCurrent: true },
													sample.pose.yaw
												) :
												null;
											if (
												!endpoint || !projectileSpace( sample.pose.regionId, endpoint.regionId )
											) {
												if ( entity && !endpoint ) {
													unsupported.add( `chain-socket:${entity.refObjId}/${route.bone}` );
												}
												if ( visual.impact ) {
													impactEvents.push( {
														...visual.impact,
														kind: "skip",
														flight: gid,
														target,
														at
													} );
												}
												continue;
											}
											route.pending = target;
											flight.destination = endpoint;
											break;
										}
										if ( route.pending !== null ) {
											flightPose = sample.pose;
											elapsed = moving.age;
											break;
										}
										if ( visual.impact ) {
											impactEvents.push( { ...visual.impact, kind: "discard", flight: gid, at } );
										}
									} else {
										cameraArrival( visual, now, byGid );
										if ( flight.soundEnd ) {
											play( {
												id: `arrival:${gid}`,
												path: flight.soundEnd,
												gain: 1,
												...audioPosition( sample.pose ),
												expires: at + .25
											} );
										}
									}
									visual.family!.end = { at, remove: visual.family!.fadeOut < 0 };
									visual.family!.previous = now;
									visual.family!.progress = 0;
									visual.family!.opacity = 1;
									visual = {
										...visual,
										impact: undefined,
										camera: undefined,
										flight: undefined,
										actor: { ...visual.actor, pose: sample.pose, loop: false }
									};
									active.set( gid, visual );
									// Retain the original EFP clock while fading its family.
									if ( !visualStarts.has( gid ) ) visualStarts.set( gid, now - moving.age );
									if ( visual.family!.end!.remove ) {
										active.delete( gid );
										continue;
									}
									break;
								}
								cameraArrival( visual, now, byGid );
								if ( visual.impact ) {
									impactEvents.push( {
										kind: visual.impact.allTargets || byGid.has( visual.impact.target ) ?
											"arrival" :
											"discard",
										flight: gid,
										...visual.impact,
										at,
										position: sample.phase === "arrived" ? sample.pose : undefined
									} );
								}
								if ( flight.soundEnd ) {
									play( {
										id: `arrival:${gid}`,
										path: flight.soundEnd,
										gain: 1,
										...audioPosition( sample.pose ),
										expires: at + 0.25
									} );
								}
								if ( visual.auxiliary?.length ) {
									if ( flight.arrivalResource ) {
										spawnAt( visual, sample.pose, at, flight.arrivalResource );
									}
									visual = {
										...visual,
										impact: undefined,
										camera: undefined,
										flight: undefined,
										landed: { at },
										actor: { ...visual.actor, pose: sample.pose, drawGeometry: false }
									};
									active.set( gid, visual );
									break;
								}
								const arrival = flight.arrivalResource ? modelFor( flight.arrivalResource ) : undefined;
								if ( !arrival?.clips.length ) {
									if ( flight.arrivalResource ) unsupported.add( flight.arrivalResource );
									active.delete( gid );
									continue;
								}
								visual = {
									...visual,
									auxiliary: undefined,
									visualStarted: undefined,
									impact: undefined,
									flight: undefined,
									life: 0,
									actor: {
										...visual.actor,
										model: arrival.glb,
										pose: sample.pose,
										time: at,
										clip: arrival.clips[0]!,
										loop: arrival.clipLoop
									}
								};
								visualStarts.delete( gid );
								if ( ready( arrival.glb ) ) visualStarts.set( gid, at );
								active.set( gid, visual );
								elapsed = now - at;
								break;
							}
						}
					}
					if ( visual.flight?.orientation && flightPose ) {
						const previous = visual.flight.orientation,
							current = flightPose,
							[dx, dz] = worldDelta( current, previous ),
							dy = previous.y - current.y;
						if ( dx * dx + dy * dy + dz * dz > 1 ) {
							visual = {
								...visual,
								actor: {
									...visual.actor,
									effectBasis: importedProjectileBasis( previous, current, false )
								}
							};
							active.set( gid, visual );
						}
						visual.flight!.orientation = current;
					}
					const family = visual.family;
					if ( family && now > family.previous ) {
						const fade = movingFade( family.progress, now - family.previous, family.fadeIn, !!family.end );
						family.previous = now;
						family.progress = fade.progress;
						family.opacity = fade.opacity;
					}
					for ( const auxiliary of visual.auxiliary ?? [] ) {
						if ( !auxiliary.expired ) {
							ready( auxiliary.actor.model );
							const fade = movingFade(
								auxiliary.progress,
								Math.max( 0, now - auxiliary.previous ),
								auxiliary.fade,
								false
							);
							auxiliary.previous = now;
							auxiliary.progress = fade.progress;
							auxiliary.opacity = fade.opacity;
							// 8D48A0 releases a timer-controlled attachment when the timer settles,
							// even for the held-arrow 0 -> 255 timer. Zero duration starts settled.
							if ( auxiliary.fade && fade.progress >= 1 ) auxiliary.expired = true;
						}
					}
					if ( !ready( visual.actor.model ) ) {
						// A one-shot whose model never becomes resident (failed or evicted
						// asset) would hold its slot forever and fill the damage pool.
						if (
							visual.independent && !visual.flight && !command &&
							elapsed >= UNREADY_ONE_SHOT_SECONDS
						) active.delete( gid );
						continue;
					}
					// Asset admission starts a one-shot's visual clock. Projectile
					// travel/result timing remains on the action callback clock.
					if ( !visual.flight ) {
						if ( !visualStarts.has( gid ) ) visualStarts.set( gid, now );
						elapsed = Math.max( 0, now - visualStarts.get( gid )! );
					}
					const life = visual.life || duration( visual.actor.model, visual.actor.clip );
					// 8D8580: a mover that cannot arrive (speed 0 short of its end) keeps
					// ticking its visual; once that visual ends nothing of it is drawn.
					const stationary = !!visual.flight && !visual.flight.moving && !visual.flight.curve &&
						visual.flight.speed === 0;
					if (
						(!visual.flight || stationary) && !visual.landed && !command?.loop &&
						command?.released === undefined && (!life || elapsed >= life)
					) {
						active.delete( gid );
						continue;
					}
					let pose = flightPose ?? visual.actor.pose;
					if ( visual.attachment.kind === "entity" && entity ) {
						const anchor = entity.gid === gameplay?.localGid && localPose ?
							localPose :
							{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
						const offset = nativeRootOffset(
							characterHeadingYaw( anchor.angle ),
							presentation.get( entity.gid )?.scale ?? 1,
							visual.attachment.offset
						);
						pose = {
							regionId: anchor.regionId,
							x: anchor.x + offset[0],
							y: anchor.y + offset[1],
							z: anchor.z + offset[2],
							yaw: visual.attachment.facing ?? nativeHeadingYaw( anchor.angle )
						};
					}
					if ( visual.visualStarted !== undefined ) elapsed = Math.max( 0, now - visual.visualStarted );
					const period = duration( visual.actor.model, visual.actor.clip ),
						cycle = !visual.actor.model.includes( "/effects/programs.json#" ) &&
								command?.released !== undefined && command.loop && period ?
							Math.floor(
								(command.released - (visualStarts.get( gid ) ?? visual.actor.time)) / period
							) * period :
							0;
					const opacity = command?.opacity ?? family?.opacity ?? visual.actor.opacity;
					result.push( {
						...visual.actor,
						pose,
						time: Math.max( 0, elapsed - cycle ),
						loop: command?.released !== undefined ? false : visual.actor.loop,
						opacity,
						emissionEnd: command?.released !== undefined && command.loop ?
							Math.max( 0, command.released - (visualStarts.get( gid ) ?? visual.actor.time) - cycle ) :
							family?.end && family.stopEmission ?
							Math.max( 0, family.end.at - (visualStarts.get( gid ) ?? visual.actor.time) ) :
							undefined
					} );
					for ( const auxiliary of visual.auxiliary ?? [] ) {
						if ( auxiliary.expired || !ready( auxiliary.actor.model ) ) continue;
						auxiliary.started ??= now;
						const end = visual.landed?.at ?? visual.family?.end?.at,
							period = duration( auxiliary.actor.model, auxiliary.actor.clip );
						const cycle =
							!auxiliary.actor.model.includes( "/effects/programs.json#" ) && end !== undefined &&
								period ?
								Math.floor( Math.max( 0, end - auxiliary.started ) / period ) * period :
								0;
						result.push( {
							...auxiliary.actor,
							time: Math.max( 0, now - auxiliary.started - cycle ),
							loop: end === undefined,
							emissionEnd: end !== undefined ? Math.max( 0, end - auxiliary.started - cycle ) : undefined,
							opacity: auxiliary.opacity
						} );
					}
				}
				for ( const gid of visualStarts.keys() ) if ( !active.has( gid ) ) visualStarts.delete( gid );
				for ( const row of persistent.values() ) {
					const owner = presentation.get( row.actors[0]!.attachment!.gid );
					if ( !owner ) continue;
					let admitted = true;
					for ( const actor of row.actors ) if ( !ready( actor.model ) ) admitted = false;
					if ( !admitted ) continue;
					for ( const actor of row.actors ) {
						result.push( { ...actor, pose: owner.pose, time: Math.max( 0, now - row.started ) } );
					}
				}
				// Attack pet Challenging Roar I / II / III (action 16 / 17 / 18 on COS):
				// 0.0s -> 1.0s: progressively brightening golden-crimson aura glow and
				// Dynamic point light follows the dedicated pet-roar buildup.
				// After 1s the ring expands to the tier's radius at its effective skill level.
				// Only the server decides taunt victims.
				roarAppearances.clear();
				for ( const entity of entities ) {
					if (
						entity.kind === "cos" && entity.emote &&
						(entity.emote.action === 16 || entity.emote.action === 17 || entity.emote.action === 18)
					) {
						const existing = roars.get( entity.gid );
						if ( !existing || existing.revision !== entity.emote.revision ) {
							const hostPose = presentation.get( entity.gid )?.pose ?? {
								regionId: entity.regionId,
								x: entity.x,
								y: entity.y,
								z: entity.z,
								yaw: nativeHeadingYaw( entity.heading )
							};
							roars.set( entity.gid, {
								revision: entity.emote.revision,
								action: entity.emote.action,
								radiusMeters: petRoarRadius(
									entity.emote.action - 15,
									gameplay?.petTraining?.gid === entity.gid ?
										gameplay.petTraining.skills.find( skill => skill.id === entity.emote!.action )
											?.effectiveLevel ?? 1 :
										1
								),
								startedAt: now,
								released: false,
								originPose: { ...hostPose },
								auraBodyGid: allocate(),
								auraHeadGid: allocate(),
								shockPrimaryGid: allocate(),
								shockSecondaryGid: allocate(),
								shockCoreGid: allocate()
							} );
						}
					}
				}
				const auraModel = "/assets/effects/challenging-roar-aura",
					shockwaveModel = "/assets/effects/challenging-roar-shockwave";
				for ( const [gid, roar] of [ ...roars ] ) {
					const host = presentation.get( gid );
					const entity = byGid.get( gid );
					if ( !entity || entity.emote?.revision !== roar.revision ) {
						roars.delete( gid );
						continue;
					}
					const bearPose = host?.pose ?? (entity ?
						{
							regionId: entity.regionId,
							x: entity.x,
							y: entity.y,
							z: entity.z,
							yaw: nativeHeadingYaw( entity.heading )
						} :
						roar.originPose);
					const elapsed = Math.max( 0, now - roar.startedAt );
					if ( elapsed >= 2.25 ) {
						// Keep the consumed revision until the entity leaves or a new emote arrives.
						continue;
					}
					const targetRadiusMeters = roar.radiusMeters;
					const targetRadiusUnits = targetRadiusMeters * 10;
					if ( elapsed < 1.0 ) {
						// Phase 1 (0.0s -> 1.0s): glow gets progressively brighter over 1 second.
						const p = Math.min( 1, elapsed / 1.0 );
						const glow = p * (0.25 + 0.75 * p);
						roar.originPose = { ...bearPose };
						roarAppearances.set( gid, {
							pointLight: {
								pose: {
									regionId: bearPose.regionId,
									x: bearPose.x,
									y: bearPose.y + 18,
									z: bearPose.z
								},
								ambient: [ 1.95 * glow, 1.22 * glow, 0.38 * glow ],
								diffuse: [ 2.85 * glow, 1.78 * glow, 0.56 * glow ],
								range: 45 + 205 * glow,
								attenuation: 0.35
							},
							materialTint: [
								1.0,
								1.0 - 0.08 * glow,
								1.0 - 0.32 * glow
							],
							scale: 1.0 + 0.08 * Math.sin( p * Math.PI * 0.65 )
						} );
						if ( ready( auraModel ) ) {
							result.push( {
								gid: roar.auraBodyGid,
								model: auraModel,
								clip: "effect",
								time: elapsed,
								loop: true,
								scale: 0.55 + 0.90 * glow,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: 0.03 + 0.22 * glow,
								materialTint: [
									0.85 + 0.15 * glow,
									0.42 + 0.53 * glow,
									0.12 + 0.66 * glow
								],
								effectRotation: { axis: "y", angle: radians( elapsed * 3.4 ) },
								pose: bearPose,
								attachment: { gid, bone: "", root: true, offset: [ 0, 0, 0 ], ground: true }
							} );
							result.push( {
								gid: roar.auraHeadGid,
								model: auraModel,
								clip: "effect",
								time: elapsed,
								loop: true,
								scale: 0.30 + 0.65 * glow,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: 0.02 + 0.12 * p * p,
								materialTint: [ 1.0, 0.88 + 0.12 * p, 0.55 + 0.40 * p ],
								effectRotation: { axis: "y", angle: radians( -elapsed * 5.2 ) },
								pose: bearPose,
								attachment: { gid, bone: "Bip01 Head", offset: [ 0, 0, 0 ], rootIfMissing: true }
							} );
						}
					} else {
						// Phase 2 (1.0s -> 2.25s): release the 25m (250-unit) shockwave!
						if ( !roar.released ) {
							roar.released = true;
							roar.originPose = { ...bearPose };
							if ( elapsed - 1 < .25 ) {
								play( {
									id: `pet-roar:${gid}:${roar.revision}`,
									path: PET_ROAR_SOUND,
									gain: .8,
									...audioPosition( bearPose ),
									expires: now + .25
								} );
							}
							cameraEvents.push( {
								atMs: Math.trunc( now * 1000 ),
								amplitude: roar.action === 18 ? 200 : 50,
								durationMs: 500,
								periodMs: roar.action === 18 ? 25 : 20
							} );
						}
						const shockElapsed = elapsed - 1.0;
						if ( shockElapsed < 0.65 ) {
							const decay = Math.pow( 1 - shockElapsed / 0.65, 1.8 );
							roarAppearances.set( gid, {
								pointLight: {
									pose: {
										regionId: roar.originPose.regionId,
										x: roar.originPose.x,
										y: roar.originPose.y + 16,
										z: roar.originPose.z
									},
									ambient: [ 2.60 * decay, 1.75 * decay, 0.68 * decay ],
									diffuse: [ 3.40 * decay, 2.25 * decay, 0.90 * decay ],
									range: 250 + 60 * (1 - decay),
									attenuation: 0.30
								},
								materialTint: [
									1.0,
									1.0 - 0.06 * decay,
									1.0 - 0.24 * decay
								],
								scale: 1.0 + 0.05 * decay
							} );
						}
						if ( shockElapsed < 0.40 && ready( auraModel ) ) {
							const burstU = shockElapsed / 0.40;
							result.push( {
								gid: roar.auraBodyGid,
								model: auraModel,
								clip: "effect",
								time: elapsed,
								loop: false,
								scale: 1.45 + 0.85 * burstU,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: 0.12 * Math.pow( 1 - burstU, 1.6 ),
								materialTint: [ 1.0, 0.92, 0.72 ],
								effectRotation: { axis: "y", angle: radians( elapsed * 4.2 ) },
								pose: roar.originPose
							} );
						}
						if ( shockElapsed < 0.90 && ready( shockwaveModel ) ) {
							const u1 = Math.min( 1, shockElapsed / 0.90 );
							const ease1 = 1 - Math.pow( 1 - u1, 2.3 );
							const primaryRadiusUnits = 6 + (targetRadiusUnits - 6) * ease1;
							result.push( {
								gid: roar.shockPrimaryGid,
								model: shockwaveModel,
								clip: "effect",
								time: shockElapsed,
								loop: false,
								scale: primaryRadiusUnits,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: Math.max( 0, (1 - Math.pow( u1, 1.65 )) * 0.45 ),
								materialTint: [
									1.0,
									0.96 - 0.22 * u1,
									0.82 - 0.48 * u1
								],
								effectRotation: { axis: "y", angle: radians( shockElapsed * 2.2 ) },
								pose: roar.originPose
							} );
						}
						if ( shockElapsed >= 0.08 && shockElapsed < 1.03 && ready( shockwaveModel ) ) {
							const u2 = Math.min( 1, (shockElapsed - 0.08) / 0.95 );
							const ease2 = 1 - Math.pow( 1 - u2, 2.1 );
							const secondaryRadiusUnits = 4 + (targetRadiusUnits - 4) * ease2;
							result.push( {
								gid: roar.shockSecondaryGid,
								model: shockwaveModel,
								clip: "effect",
								time: shockElapsed - 0.08,
								loop: false,
								scale: secondaryRadiusUnits,
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: Math.max( 0, (1 - Math.pow( u2, 1.45 )) * 0.30 ),
								materialTint: [ 1.0, 0.62 - 0.18 * u2, 0.24 ],
								effectRotation: { axis: "y", angle: radians( -shockElapsed * 1.8 ) },
								pose: {
									...roar.originPose,
									y: roar.originPose.y + 0.6
								}
							} );
						}
						if ( shockElapsed < 0.45 && ready( shockwaveModel ) ) {
							const uCore = shockElapsed / 0.45;
							result.push( {
								gid: roar.shockCoreGid,
								model: shockwaveModel,
								clip: "effect",
								time: shockElapsed,
								loop: false,
								scale: 8 + 97 * (1 - Math.pow( 1 - uCore, 2.0 )),
								absoluteEffectScale: true,
								effectEntity: true,
								pickable: false,
								opacity: (1 - uCore) * 0.25,
								materialTint: [ 1.0, 0.95, 0.80 ],
								pose: {
									...roar.originPose,
									y: roar.originPose.y + 1.1
								}
							} );
						}
					}
				}
				// Port-only wolf skill signals (server petwolfkit.go / petwolfvisual.go),
				// each a 0x324B emote on the object the effect belongs to. The wolf's
				// look is its own: retail beast-howl, wolf-strike, blood and war-cry
				// effect programs (WOLF_EFFECT_PARTS), never the bear's roar models.
				// 1..3 Hunting Howl (wolf, with the pet-howl clip), 4..6 Savage Rend
				// (wolf, sent at the bite's windup with pet-rend), 0xF0 executed
				// (victim), 0xF1 Execute III sweep (wolf or owner), 0xF2 Blood Hunt
				// consumed (prey), 0xF3 Blood Hunt III burst (bleeding enemy), 0xF4
				// Bloodscent full (wolf or owner). The server owns every outcome.
				for ( const entity of entities ) {
					const action = entity.emote?.action ?? 0;
					if (
						!(entity.kind === "cos" && action >= 1 && action <= 6) && !(action >= 0xf0 && action <= 0xf4) &&
						!(action >= 0xd0 && action <= 0xd6)
					) {
						continue;
					}
					const existing = wolfSkills.get( entity.gid );
					if ( existing?.revision === entity.emote!.revision ) continue;
					const pose = presentation.get( entity.gid )?.pose ?? {
						regionId: entity.regionId,
						x: entity.x,
						y: entity.y,
						z: entity.z,
						yaw: nativeHeadingYaw( entity.heading )
					};
					wolfSkills.set( entity.gid, {
						revision: entity.emote!.revision,
						action,
						startedAt: now,
						sounded: false,
						pose: { ...pose },
						gids: existing?.gids ?? Array.from( { length: WOLF_EFFECT_MAX_PARTS }, () => allocate() )
					} );
				}
				for ( const [gid, skill] of [ ...wolfSkills ] ) {
					const entity = byGid.get( gid );
					const elapsed = Math.max( 0, now - skill.startedAt );
					const frost = skill.action >= 0xd0 && skill.action <= 0xd6;
					// Ground frost outlives its victim; retain the latched pose for its bounded lifetime.
					if (
						!frost && (!entity || entity.emote?.revision !== skill.revision) ||
						!entity && elapsed >= WOLF_EFFECT_SECONDS
					) {
						wolfSkills.delete( gid );
						continue;
					}
					const parts = frost ? [] : wolfEffectParts( skill.action );
					const kind = skill.action >= 0xd0 && skill.action <= 0xd6 ?
						"victim" :
						wolfEffectKind( skill.action );
					// Keep the consumed revision until the entity leaves or a new emote arrives.
					if ( elapsed >= WOLF_EFFECT_SECONDS ) continue;
					const live = presentation.get( gid )?.pose ?? skill.pose;
					// Victims keep where they were struck; a dead monster may already be gone.
					const pose = kind === "victim" ? skill.pose : live;
					const owner = entity?.ownerGid !== undefined ?
						presentation.get( entity.ownerGid )?.pose :
						undefined;
					const cue = PENGUIN_EFFECT_SOUND[skill.action] ?? wolfEffectSound( skill.action );
					if ( !skill.sounded && cue && elapsed >= cue.at ) {
						skill.sounded = true;
						if ( elapsed - cue.at < .25 ) {
							play( {
								id: `pet-wolf:${gid}:${skill.revision}`,
								path: cue.path,
								gain: cue.gain,
								...audioPosition( pose ),
								expires: now + .25
							} );
						}
						if ( cue.shake ) {
							cameraEvents.push( {
								atMs: Math.trunc( now * 1000 ),
								amplitude: 50,
								durationMs: 500,
								periodMs: 20
							} );
						}
					}
					if ( frost ) {
						for ( const actor of penguinEffectActors( skill.action, elapsed, pose, skill.gids ) ) {
							if ( ready( actor.model ) ) result.push( actor );
						}
						continue;
					}
					// A short blood-red flare at the strike, a slower one for the howl.
					const glow = kind === "howl" ?
						Math.max( 0, Math.sin( Math.PI * Math.min( 1, elapsed / 1.4 ) ) ) :
						Math.max( 0, 1 - Math.abs( elapsed - (parts[0]?.at ?? 0) ) / .35 );
					if ( glow > 0 && kind !== "victim" ) {
						roarAppearances.set( gid, {
							pointLight: {
								pose: { regionId: live.regionId, x: live.x, y: live.y + 12, z: live.z },
								ambient: [ 1.1 * glow, 0.05 * glow, 0.04 * glow ],
								diffuse: [ 1.9 * glow, 0.08 * glow, 0.06 * glow ],
								range: 70 + 50 * glow,
								attenuation: 0.4
							},
							materialTint: [ 1.0, 1.0 - 0.28 * glow, 1.0 - 0.28 * glow ]
						} );
					}
					for ( const [index, part] of parts.entries() ) {
						const age = elapsed - part.at,
							model = "/assets/effects/programs.json#" + encodeURIComponent( part.path );
						if ( age < 0 || !ready( model ) ) continue;
						const life = duration( model, "effect" ) || 2;
						if ( age >= life ) continue;
						const at = part.anchor === "owner" ? owner ?? live : part.anchor === "follow" ? live : pose;
						const reach = part.anchor === "front" ? 12 : 0;
						result.push( {
							gid: skill.gids[index]!,
							model,
							clip: "effect",
							time: age,
							loop: false,
							scale: part.scale ?? 1,
							pickable: false,
							pose: {
								regionId: at.regionId,
								x: at.x + Math.sin( at.yaw ) * reach + (part.offset?.[0] ?? 0),
								y: at.y + (part.lift ?? 0),
								z: at.z + Math.cos( at.yaw ) * reach + (part.offset?.[1] ?? 0),
								yaw: at.yaw
							}
						} );
					}
				}
				for ( const [gid, visual] of shields ) {
					if ( roarAppearances.has( gid ) ) continue;
					const entity = byGid.get( gid );
					if ( !entity?.shield ) continue;
					const age = Math.max( 0, now - visual.at );
					const burst = visual.burst && age < 0.75;
					const activeShield = entity.shield.amount > 0 && now < visual.until;
					if ( !burst && !activeShield ) continue;
					const pose = presentation.get( gid )?.pose ?? {
						regionId: entity.regionId,
						x: entity.x,
						y: entity.y,
						z: entity.z,
						yaw: nativeHeadingYaw( entity.heading )
					};
					if ( burst ) {
						const decay = Math.pow( Math.max( 0, 1 - age / 0.75 ), 1.6 );
						roarAppearances.set( gid, {
							pointLight: {
								pose: { regionId: pose.regionId, x: pose.x, y: pose.y + 15, z: pose.z },
								ambient: [ 0.12 * decay, 0.22 * decay, 0.28 * decay ],
								diffuse: [ 0.18 * decay, 0.30 * decay, 0.36 * decay ],
								range: 140 + 80 * (1 - decay),
								attenuation: 0.32
							},
							materialTint: [ 1.0 - 0.12 * decay, 1.0, 1.0 ],
							scale: 1.0 + 0.04 * decay
						} );
					} else {
						const pulse = 0.85 + 0.15 * Math.sin( age * 6 );
						roarAppearances.set( gid, {
							pointLight: {
								pose: { regionId: pose.regionId, x: pose.x, y: pose.y + 15, z: pose.z },
								ambient: [ 0.04 * pulse, 0.10 * pulse, 0.14 * pulse ],
								diffuse: [ 0.07 * pulse, 0.15 * pulse, 0.20 * pulse ],
								range: 95,
								attenuation: 0.38
							},
							materialTint: [ 0.90, 0.98, 1.0 ],
							scale: 1.0
						} );
					}
				}
				hiddenWeapons.clear();
				for ( const gid of weaponOwners.values() ) hiddenWeapons.add( gid );
				budgetActors = result.length;
				return result;
			} catch ( error ) {
				failure = String( error );
				return [];
			}
		},
		/*
		================
		damage
		The damage effect actors for one hit on a victim (8D5440): the record's
		damage or defense effect plus its tint and secondary children.
		================
		*/
		damage(
			gid: number,
			caster: number,
			kind: number,
			skill: number,
			defensive: boolean,
			projectile: boolean,
			secondary: boolean,
			pose: CharacterActor["pose"],
			basis: NonNullable<CharacterActor["effectBasis"]>,
			blood: readonly [string | null, string | null] | undefined,
			bloodEnabled: boolean,
			now: number,
			ready?: ( path: string ) => boolean
		) {
			const result: CharacterActor[] = [];
			const record = catalog?.[String( skill )],
				resource = defensive ? record?.attachedAction?.defense : record?.damageEffect;
			// Exact 8D5440 nests both tint and secondary underneath primary handle.
			if ( !resource ) return result;
			/*
			================
			spawn
			Adds one damage effect actor for a resource path.
			================
			*/
			function spawn( path: string ) {
				const model = modelFor( path );
				if ( !model ) return;
				if ( active.size >= 2048 ) throw Error( "Damage effect capacity exceeded" );
				const id = allocate();
				active.set( id, {
					actor: {
						gid: id,
						model: model.glb,
						clip: model.clips[0] ?? "effect",
						time: now,
						loop: false,
						scale: 1,
						pickable: false,
						pose: { ...pose },
						effectBasis: basis
					},
					owner: gid,
					token: 0,
					life: 0,
					independent: true,
					attachment: { kind: "world" }
				} );
				if ( ready?.( model.glb ) ) {
					visualStarts.set( id, now );
					result.push( { ...active.get( id )!.actor, time: 0 } );
				}
			}
			spawn( resource );
			if ( !projectile && !defensive && record?.hitLight ) {
				hitLights.start( gid, record.hitLight, pose, now );
				hitLights.start( caster, record.hitLight, pose, now );
			}
			if ( secondary && kind !== 7 ) {
				const path = blood?.[bloodEnabled ? 0 : 1];
				if ( path ) spawn( path );
			}
			return result;
		},
		impactIndex,
		/*
		================
		appearance
		What effects change on a character's own model: hit light, hidden
		weapon, material tint and status decoration.
		================
		*/
		appearance( gid: number ) {
			const spineRotation = spineAims.rotation( gid );
			const roar = roarAppearances.get( gid );
			const pointLight = roar?.pointLight ?? hitLights.get( gid );
			return {
				...(pointLight ? { pointLight } : {}),
				weaponHidden: hiddenWeapons.has( gid ),
				...(spineRotation ? { boneRotation: { bone: SPINE_AIM_BONE, rotation: spineRotation } } : {}),
				materialTint: roar?.materialTint ?? materials.get( gid )?.color,
				scale: (hwan.get( gid )?.value ?? 1) * (roar?.scale ?? 1)
			};
		},
		/*
		================
		takeImpacts
		Hands over the impact events produced since the last call.
		================
		*/
		takeImpacts() {
			return impactEvents.splice( 0 );
		},
		/*
		================
		takeHawkImpacts
		Hands over the hawk impacts produced since the last call.
		================
		*/
		takeHawkImpacts() {
			return hawkImpacts.splice( 0 );
		},
		/*
		================
		hitFlash
		Queues the local hit flash camera script when the hit qualifies.
		================
		*/
		hitFlash( localCaster: boolean, hwan: boolean, flags: number, atMs: number ) {
			const event = localHitFlash( localCaster, hwan, flags, atMs );
			if ( event ) cameraEvents.push( event );
		},
		/*
		================
		structureShake

		CICATStruct_SetVisualStage (4F78A0) shakes the camera on a stage its
		atstructeffect sound marks: CGInterface_TriggerCameraShake (68F6D0)
		mode 0, amplitude 50, 500 ms, a 20 ms period.
		================
		*/
		structureShake( atMs: number ) {
			cameraEvents.push( { atMs: Math.trunc( atMs ), amplitude: 50, durationMs: 500, periodMs: 20 } );
		},
		/*
		================
		takeCameraScripts
		Hands over the queued camera scripts.
		================
		*/
		takeCameraScripts() {
			return cameraEvents.splice( 0 );
		},
		/*
		================
		attachedInstances
		The live attached effects, for diagnostics and probes.
		================
		*/
		attachedInstances() {
			return [ ...attached ].map( ( [key, row] ) => ({
				key,
				gid: row.effect.gid,
				skill: row.effect.skill,
				stopped: row.stopped
			}) );
		},
		/*
		================
		hostMotions
		The motions attached effects impose on their host character.
		================
		*/
		hostMotions( gid: number ) {
			return [ ...attached.values() ].filter( row =>
				row.effect.gid === gid && (row.effect.phase === 1 || catalog?.[String( row.effect.skill )]?.overlap)
			).flatMap( row => {
				const motion = catalog?.[String( row.effect.skill )]?.attachedMotion;
				return motion ?
					[ { ...motion, key: attachedKey( row.effect ), started: row.started, stoppedAt: row.stoppedAt } ] :
					[];
			} );
		},
		/*
		================
		takeActivations
		Hands over the effect activations since the last call, for sounds.
		================
		*/
		takeActivations() {
			return activations.splice( 0 );
		},
		/*
		================
		impactSource
		Which skill's impact sound a hit plays, given the attached effects.
		================
		*/
		impactSource(
			caster: number | undefined,
			target: number,
			attached: readonly import("@/engine/foundation/gameplay/attached-effects").AttachedEffect[],
			cast: import("@/engine/contracts/gameplay").CastState
		) {
			return impactSource( caster, target, attached, skill => catalog?.[String( skill )]?.attachedAction, cast );
		},
		/*
		================
		clip
		The first action clip a skill's effect record names.
		================
		*/
		clip( skill: number ) {
			return catalog?.[String( skill )]?.clips[0];
		},
		loaded: () => catalog !== null,
		/*
		================
		phases
		A skill record's ready, wait and shot clip lists.
		================
		*/
		phases( skill: number ) {
			const record = catalog?.[String( skill )];
			return record ? (record.phaseClips?.slice( 0, 3 ) ?? [ [], [], record.clips ]) : undefined;
		},
		/*
		================
		takeBudget
		The actors the last step drew and the effects the budget turned away
		since the last call (each call consumes them), for the frame probe.
		================
		*/
		takeBudget() {
			const taken = { actors: budgetActors, gaveWay: budgetGaveWay };
			budgetGaveWay = 0;
			return taken;
		},
		error: () =>
			loadFailure ?? failure ??
				(unsupported.size ?
					`Unsupported native effect resources/operations: ${[ ...unsupported ].join( ", " )}` :
					null),
		/*
		================
		reset
		Forgets every effect and queued event; the catalogs stay loaded.
		================
		*/
		reset() {
			external.clear();
			crowEffects.length = 0;
			externalSerial = 0;
			hitLights.reset();
			activations.length = 0;
			hawkImpacts.length = 0;
			weaponOwners.clear();
			hiddenWeapons.clear();
			spineAims.reset();
			materials.clear();
			materialOwners.clear();
			hwan.clear();
			roars.clear();
			wolfSkills.clear();
			penguinBolts.reset();
			roarAppearances.clear();
			shields.clear();
			spiritRings.clear();
			bondPulses.clear();
			counterPulses.clear();
			statuses.clear();
			cameraEvents.length = 0;
			system.clear();
			systemSerial = 0;
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			retryAt = 0;
			attempts = 0;
			loadFailure = null;
			impactEvents.length = 0;
			seen.clear();
			pendingTriggers.clear();
			visualStarts.clear();
			active.clear();
			attached.clear();
			persistent.clear();
			unsupported.clear();
			failure = null;
		},
		/*
		================
		dispose
		Releases everything, including the loading job.
		================
		*/
		dispose() {
			external.clear();
			crowEffects.length = 0;
			hitLights.reset();
			activations.length = 0;
			hawkImpacts.length = 0;
			weaponOwners.clear();
			hiddenWeapons.clear();
			spineAims.reset();
			materials.clear();
			materialOwners.clear();
			hwan.clear();
			roars.clear();
			wolfSkills.clear();
			roarAppearances.clear();
			penguinBolts.reset();
			shields.clear();
			statuses.clear();
			cameraEvents.length = 0;
			system.clear();
			disposed = true;
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			seen.clear();
			pendingTriggers.clear();
			visualStarts.clear();
			active.clear();
			attached.clear();
			persistent.clear();
			impactIndexes.clear();
			catalog = null;
			models = null;
		}
	};
}
