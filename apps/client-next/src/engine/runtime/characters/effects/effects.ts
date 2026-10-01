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
import { castResultAt } from "@/engine/foundation/gameplay/cast-results";
import { questMarkerEffect } from "@/engine/foundation/gameplay/quest";
import { createPresentationIds } from "@/engine/foundation/animation/presentation-ids";
import { itemEffectClock, type ItemEffectClock } from "@/engine/foundation/animation/item-effect-clock";
import { createHitLights } from "@/engine/foundation/animation/hit-light";
import { advanceProjectileCurve } from "@/engine/foundation/animation/projectile-curve";
import { modelAmbientParticles as ambientModelParticles } from "@/engine/foundation/animation/model-emission";
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
import { projectileSpace, sampleProjectile } from "@/engine/foundation/animation/projectile-time";
import {
	projectileBasis,
	movingFade,
	movingTargets,
	radialDestination,
	stepMoving
} from "@/engine/foundation/animation/moving-stage";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { SoundEvent } from "@/engine/contracts/audio";
import type { EffectCatalog, EffectVisual, EffectTrigger } from "@/engine/contracts/effects";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import { nativeHeadingYaw, radians } from "@/engine/foundation/math/angles";
import { hawkInitial, hawkEvent, hawkAnimation, stepHawk, type HawkState } from "@/engine/foundation/animation/hawk";
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
					x: (owner.pose.regionId & 255) * 1920 + owner.pose.x,
					y: owner.pose.y,
					z: (owner.pose.regionId >>> 8) * 1920 + owner.pose.z,
					expires: now + .25
				} );
			}
			if ( !stage.resource ) continue;
			const model = modelFor( stage.resource );
			if ( !model ) {
				unsupported.add( stage.resource );
				continue;
			}
			const target = stage.action === "AT_TARGET" || stage.action === "AT_TARGET_F",
				bone = (target ? stage.targetBone : stage.bone) ?? "",
				offset = target ? (stage.targetOffset ?? [ 0, 0, 0 ]) : stage.offset,
				overhead = bone === "*";
			if ( overhead && owner.height === undefined ) throw Error( "Missing attached effect anchor height" );
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
					const position = {
						x: owner.pose.x + offset[0],
						y: owner.pose.y + offset[1],
						z: owner.pose.z + offset[2]
					};
					const state = hawkInitial( position, Math.fround( Math.PI - owner.pose.yaw ) );
					result.push( {
						actor: {
							gid: allocate(),
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
					continue;
				}
				const actor: CharacterActor = {
					gid: allocate(),
					model: model.glb,
					pose: owner.pose,
					clip: model.clips[0] ?? "",
					time: 0,
					loop: stage.action === "AT_LOOP",
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
						gid: owner.gid,
						bone: overhead ? "" : bone,
						root: overhead || !bone,
						offset: [ offset[0], offset[1] + (overhead ? owner.height! : 0), -offset[2] ]
					}
				};
				const control = {
					life: stage.life,
					keep: stage.action === "AT_LOOP" && !stop,
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
								attachment: {
									gid: actor.gid,
									bone: particle.bone,
									root: particle.root,
									offset: particle.offset
								}
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
		item
		Queues an external item effect (a used item's visual) for the next step.
		================
		*/
		item( event: import("@/engine/contracts/orb").ItemEffectFeedback ) {
			if ( external.size >= 128 ) throw Error( "External item effect backlog" );
			external.set( ++externalSerial, { event, clock: { phase: "pending" }, hostGid: allocate(), children: [] } );
		},
		/*
		================
		system
		Queues a system effect record (a level-up or a similar event) on a character.
		================
		*/
		system( gid: number, skill: number ) {
			if ( system.size >= 128 ) throw Error( "System effect backlog" );
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
			gameplay: GameplayState | null,
			now: number,
			resourceReady: ( path: string ) => boolean,
			duration: ( path: string, clip: string ) => number,
			triggers: readonly EffectTrigger[] = [],
			socket?: (
				gid: number,
				bone: string,
				offset: readonly [number, number, number],
				trigger: EffectTrigger
			) => EffectVisual["actor"]["pose"] | null,
			presented: readonly CharacterActor[] = [],
			effectDetail = 2,
			bloodEnabled = true
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
				const castStates = new Map( gameplay?.casts.map( cast => [ cast.token, cast ] ) ?? [] );
				const current = new Set( castStates.keys() );
				const byGid = new Map( entities.map( entity => [ entity.gid, entity ] ) );
				// The local movement owner advances gameplay.pose; the entity row
				// retains its wire/spawn position. All effect anchors (including
				// bone-less victims, returns and chain hops) need the live pose.
				const local = gameplay?.localGid ? byGid.get( gameplay.localGid ) : undefined;
				if ( local && gameplay?.pose ) {
					byGid.set( local.gid, { ...local, ...gameplay.pose, heading: gameplay.pose.angle } );
				}
				hitLights.step( now, new Set( byGid.keys() ) );
				const wantedPersistent = new Set<string>(),
					presentation = new Map( presented.map( actor => [ actor.gid, actor ] ) );
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
					if ( view.tint ) {
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
					} else if ( view.tint === null ) materials.delete( entity.gid );
					const enabled = entity.appearanceState?.[2] === 1;
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
									const overhead = stage.bone === "*";
									if ( overhead && owner.height === undefined ) {
										throw Error( "Missing native effect anchor height" );
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
										attachment: {
											gid: owner.gid,
											bone: overhead ? "" : stage.bone ?? "",
											root: overhead || !stage.bone,
											basis: stageAttachmentBasis( stage.resource ),
											offset: [
												stage.offset[0],
												stage.offset[1] + (overhead ? owner.height! : 0),
												stage.offset[2]
											]
										}
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
											attachment: {
												gid,
												bone: particle.bone,
												root: particle.root,
												offset: particle.offset
											}
										} );
									}
								}
								if (
									actors.length + active.size +
											[ ...persistent.values() ].reduce(
												( n, row ) => n + row.actors.length,
												0
											) > 128
								) throw Error( "Effect population exceeds budget" );
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
						if ( pendingTriggers.size >= 128 ) throw Error( "Effect callback population exceeds budget" );
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
							const targetLocal = stage.action === "AT_TARGET";
							const radial = stage.action === "AT_MOV_OPTION",
								chain = stage.action === "AT_MOV_SPLASH" && stage.move !== "MOV_HWAN";
							const distributed = stage.action === "AT_MOV_1TAR" || stage.action === "AT_MOV_SPLASH";
							const flying = distributed || radial || returning ||
								targetLocal && stage.move !== "MOV_NONE";
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
								const pose = entity.gid === gameplay?.localGid && gameplay.pose ?
									gameplay.pose :
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
										x: (pose.regionId & 255) * 1920 + pose.x,
										y: pose.y,
										z: (pose.regionId >>> 8) * 1920 + pose.z,
										expires: trigger.at + 0.25
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
									(caster.gid === gameplay?.localGid && gameplay?.pose ?
										gameplay.pose.angle :
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
								const effectRotation = rotation?.rotation ?? commandRotation;
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
									socket?.( entity.gid, stage.bone, [
										stage.offset[0],
										stage.offset[1],
										-stage.offset[2]
									], trigger ) :
									undefined;
								const targetOffset = stage.targetOffset ?? [ 0, 0, 0 ];
								const targetSocket = flying && !radial && !targetLocal && target && stage.targetBone ?
									socket?.( target.gid, stage.targetBone, [
										targetOffset[0],
										targetOffset[1],
										-targetOffset[2]
									], { ...trigger, sampleCurrent: true } ) :
									undefined;
								const supportedFlight = flying && target &&
									(targetLocal || !stage.bone || sourceSocket) &&
									(radial || targetLocal || !stage.targetBone || targetSocket) &&
									(curved || stage.move === "MOV_STRAIGHT" || stage.move === "MOV_ROUND" ||
										[ "MOV_UP", "MOV_UPR" ].includes( stage.move ) &&
											stage.parameters !== undefined) &&
									motion && (curved || motion.startSpeed > 0) &&
									projectileSpace( pose.regionId, target.regionId ) &&
									(!radial || stage.move === "MOV_STRAIGHT" && stage.native?.actionOptions.enabled);
								if (
									!model ||
									(!supportedFlight &&
										(![ "AT_DMG_POS", "AT_ONE_FOLLOW", "AT_LOOP", "AT_STOP", "AT_TARGET" ].includes(
											stage.action
										) || stage.move !== "MOV_NONE" ||
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
								const instances = distributed || radial ? 1 : stage.count;
								if ( active.size + instances > 128 ) {
									failure = "Effect population exceeds budget";
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
									let source = sourceSocket ??
										{
											regionId: pose.regionId,
											x: pose.x + stage.offset[0],
											y: pose.y + stage.offset[1],
											z: pose.z + stage.offset[2],
											yaw: victimFacing ?? nativeHeadingYaw( pose.angle )
										};
									if ( targetLocal && !flying ) {
										const anchor = stage.targetBone ?
											socket?.( entity.gid, stage.targetBone, [
												targetOffset[0],
												targetOffset[1],
												-targetOffset[2]
											], trigger ) :
											{
												regionId: pose.regionId,
												x: pose.x + targetOffset[0],
												y: pose.y + targetOffset[1],
												z: pose.z + targetOffset[2],
												yaw: nativeHeadingYaw( pose.angle )
											};
										if ( !anchor ) {
											unsupported.add( `target-socket:${entity.refObjId}/${stage.targetBone}` );
											continue;
										}
										source = anchor;
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
										}
										if ( entity.mountedOn !== undefined ) {
											unsupported.add( `mounted-hit-anchor:${entity.refObjId}` );
											continue;
										}
										if ( !projectileSpace( entity.regionId, caster.regionId ) ) {
											unsupported.add( "hit-anchor:region-space" );
											continue;
										}
										const dungeon = !!(entity.regionId & 0x8000),
											dx = caster.x - entity.x + (dungeon ?
												0 :
												((caster.regionId & 255) - (entity.regionId & 255)) * 1920),
											dy = caster.y - entity.y,
											dz = caster.z - entity.z +
												(dungeon ?
													0 :
													((caster.regionId >>> 8) - (entity.regionId >>> 8)) * 1920),
											length = Math.hypot( dx, dy, dz ),
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
										targetSocket ??
											(target ?
												{
													regionId: target.regionId,
													x: target.x + targetOffset[0],
													y: target.y + targetOffset[1],
													z: target.z + targetOffset[2],
													yaw: nativeHeadingYaw( target.heading )
												} :
												source);
									if ( radial ) source = { ...source, yaw: destination.yaw };
									const dungeon = !!(source.regionId & 0x8000);
									const curveResult = supportedFlight && curved ?
										random.curve( [ source.x, source.y, source.z ], [
											destination.x + (dungeon ?
												0 :
												((destination.regionId & 255) - (source.regionId & 255)) * 1920),
											destination.y,
											destination.z + (dungeon ?
												0 :
												((destination.regionId >>> 8) - (source.regionId >>> 8)) * 1920)
										] ) :
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
											atTarget: stage.action === "AT_TARGET",
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
													attachment: resource?.endsWith( ".bsr" ) ?
														{
															gid,
															bone: "Bone01",
															offset: [ 0, 0, 0 ],
															rootIfMissing: true
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
										independent: !!flight,
										impact,
										flight,
										owner: entity.gid,
										token: cast.token,
										life: route ?
											0 :
											targetLocal && !flight ?
											(stage.movement?.delayMs ?? 0) / 1000 :
											stage.life,
										attachment: [ "AT_ONE_FOLLOW", "AT_LOOP" ].includes( stage.action ) ?
											{
												kind: "entity",
												offset: [ ...stage.offset ],
												...(victimFacing !== undefined ? { facing: victimFacing } : {})
											} :
											{ kind: "world" },
										actor: {
											gid,
											effectBasis: flight && resource?.endsWith( ".bsr" ) ?
												projectileBasis( source, destination ) :
												undefined,
											effectRotation,
											pickable: false,
											attachment: (stage.bone ||
													[ "AT_ONE_FOLLOW", "AT_LOOP" ].includes( stage.action )) &&
													!supportedFlight && !targetLocal ?
												{
													gid: entity.gid,
													bone: stage.bone ?? "",
													root: !stage.bone,
													basis: stageAttachmentBasis( stage.resource ),
													offset: [ stage.offset[0], stage.offset[1], stage.offset[2] ],
													...(victimFacing !== undefined && !stage.bone ?
														{ facing: victimFacing } :
														{})
												} :
												undefined,
											model: traded?.actor.model ?? model.glb,
											pose: source,
											clip: traded?.actor.clip ?? model.clips[0] ?? "",
											time: trigger.at,
											loop: traded?.actor.loop ??
												(stage.action === "AT_LOOP" || !!flight && model.clipLoop),
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
									x: (pose.regionId & 255) * 1920 + pose.x,
									y: pose.y,
									z: (pose.regionId >>> 8) * 1920 + pose.z,
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
						for ( const visual of child.actors ) result.push( { ...visual.actor, time: now - visual.at } );
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
							time: now - row.clock.started,
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
					const age = now - event.started;
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
													const dungeon = !!(target.regionId & 0x8000),
														dx = hawk.state.position.x - target.x + (dungeon ?
															0 :
															((hawk.region & 255) - (target.regionId & 255)) * 1920),
														dy = hawk.state.position.y - target.y,
														dz = hawk.state.position.z - target.z +
															(dungeon ?
																0 :
																((hawk.region >>> 8) - (target.regionId >>> 8)) * 1920),
														length = Math.hypot( dx, dy, dz ),
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
									const holder = byGid.get( row.effect.gid )!,
										target = byGid.get( hawk.state.target ),
										targetActor = target ? presentation.get( target.gid ) : undefined;
									const region = owner.pose.regionId;
									if ( !projectileSpace( hawk.region, region ) ) continue;
									const point = ( p: { regionId: number; x: number; y: number; z: number; } ) => ({
										x: p.x + (region & 0x8000 ? 0 : ((p.regionId & 255) - (region & 255)) * 1920),
										y: p.y,
										z: p.z + (region & 0x8000 ? 0 : ((p.regionId >>> 8) - (region >>> 8)) * 1920)
									});
									hawk.state = {
										...hawk.state,
										position: point( { ...hawk.state.position, regionId: hawk.region } )
									};
									hawk.region = region;
									if ( target && targetActor?.heightFactor === undefined ) continue;
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
									time: now - visual.started - cycle,
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
					if ( result.length + active.size > 2048 ) {
						throw Error( "Attached effect population exceeds budget" );
					}
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
					let elapsed = now - visual.actor.time;
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
								dungeon = !!(start.regionId & 0x8000);
							const rx = dungeon ? 0 : Math.floor( x / 1920 ), rz = dungeon ? 0 : Math.floor( z / 1920 );
							sample = {
								phase: "travel",
								pose: {
									regionId: dungeon ?
										start.regionId :
										((start.regionId & 255) + rx) | (((start.regionId >>> 8) + rz) << 8),
									x: x - rx * 1920,
									y,
									z: z - rz * 1920,
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
								x: (pose.regionId & 255) * 1920 + pose.x,
								y: pose.y,
								z: (pose.regionId >>> 8) * 1920 + pose.z,
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
											const offset = route.offset;
											const endpoint = entity ?
												(route.bone ?
													socket?.(
														target,
														route.bone,
														[ offset[0], offset[1], -offset[2] ],
														{ ...route.trigger, at: now, sampleCurrent: true }
													) :
													{
														regionId: entity.regionId,
														x: entity.x + offset[0],
														y: entity.y + offset[1],
														z: entity.z + offset[2],
														yaw: sample.pose.yaw
													}) :
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
												x: (sample.pose.regionId & 255) * 1920 + sample.pose.x,
												y: sample.pose.y,
												z: (sample.pose.regionId >>> 8) * 1920 + sample.pose.z,
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
										x: (sample.pose.regionId & 255) * 1920 + sample.pose.x,
										y: sample.pose.y,
										z: (sample.pose.regionId >>> 8) * 1920 + sample.pose.z,
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
							dungeon = !!(previous.regionId & 0x8000);
						const dx = previous.x - current.x +
								(dungeon ? 0 : ((previous.regionId & 255) - (current.regionId & 255)) * 1920),
							dy = previous.y - current.y,
							dz = previous.z - current.z +
								(dungeon ? 0 : ((previous.regionId >>> 8) - (current.regionId >>> 8)) * 1920);
						if ( dx * dx + dy * dy + dz * dz > 1 ) {
							visual = {
								...visual,
								actor: { ...visual.actor, effectBasis: projectileBasis( previous, current, false ) }
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
						continue;
					}
					// Asset admission starts a one-shot's visual clock. Projectile
					// travel/result timing remains on the action callback clock.
					if ( !visual.flight ) {
						if ( !visualStarts.has( gid ) ) visualStarts.set( gid, now );
						elapsed = now - visualStarts.get( gid )!;
					}
					const life = visual.life || duration( visual.actor.model, visual.actor.clip );
					if (
						!visual.flight && !visual.landed && !command?.loop && command?.released === undefined &&
						(!life || elapsed >= life)
					) {
						active.delete( gid );
						continue;
					}
					let pose = flightPose ?? visual.actor.pose;
					if ( visual.attachment.kind === "entity" && entity ) {
						const anchor = entity.gid === gameplay?.localGid && gameplay.pose ?
							gameplay.pose :
							{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
						const offset = visual.attachment.offset;
						pose = {
							regionId: anchor.regionId,
							x: anchor.x + offset[0],
							y: anchor.y + offset[1],
							z: anchor.z + offset[2],
							yaw: visual.attachment.facing ?? nativeHeadingYaw( anchor.angle )
						};
					}
					if ( visual.visualStarted !== undefined ) elapsed = now - visual.visualStarted;
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
						time: elapsed - cycle,
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
							time: now - auxiliary.started - cycle,
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
						result.push( { ...actor, pose: owner.pose, time: now - row.started } );
					}
				}
				hiddenWeapons.clear();
				for ( const gid of weaponOwners.values() ) hiddenWeapons.add( gid );
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
			return {
				...(hitLights.get( gid ) ? { pointLight: hitLights.get( gid ) } : {}),
				weaponHidden: hiddenWeapons.has( gid ),
				materialTint: materials.get( gid )?.color,
				scale: hwan.get( gid )?.value ?? 1
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
			externalSerial = 0;
			hitLights.reset();
			activations.length = 0;
			hawkImpacts.length = 0;
			weaponOwners.clear();
			hiddenWeapons.clear();
			materials.clear();
			materialOwners.clear();
			hwan.clear();
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
			hitLights.reset();
			activations.length = 0;
			hawkImpacts.length = 0;
			weaponOwners.clear();
			hiddenWeapons.clear();
			materials.clear();
			materialOwners.clear();
			hwan.clear();
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
