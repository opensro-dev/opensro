/*
===========================================================================

spine-aims.ts - each caster's Spine_Base aim while its skill runs

The CIDecoSkill side of spine-aim.ts. A cast whose animation set names a
rotation axis arms its caster's aim when it starts (8E07D7), measuring the
height between its stage bindings once (+0xF8). Every frame it retargets the
rotator at the target's current position (8DC440) and steps it (A9ADD0). The
final stage's trigger or the cast's end releases it; it eases home and is
dropped. One aim per caster: a new cast retargets the same rotator, as
A9C370 updates the bone's existing entry.

===========================================================================
*/
import type { CastState } from "@/engine/contracts/gameplay";
import type { CharacterActor } from "@/engine/contracts/character";
import type { EffectCatalog, EffectTrigger, EffectVisual, SpineAim, SpineAimBinding } from "@/engine/contracts/effects";
import {
	spineAimBearing,
	spineAimRotation,
	spineAimTargets,
	stepSpineAim,
	type SpineAimRotator
} from "@/engine/foundation/animation/spine-aim";

/*
================
SpineAimFrame

One presentation frame's inputs: the live casts, the catalog naming each
skill's aim, the stage triggers that fired, the presented actors and the
socket probe the effects owner already samples launches with.
================
*/
export interface SpineAimFrame {
	readonly casts: readonly CastState[];
	readonly catalog: EffectCatalog | null;
	readonly triggers: readonly EffectTrigger[];
	readonly presented: readonly CharacterActor[];
	readonly now: number;
}

/*
================
SpineAimSocket

The effects owner's socket probe: a bone's world pose on a holder.
================
*/
export type SpineAimSocket = (
	gid: number,
	bone: string | null,
	offset: readonly [number, number, number],
	trigger: EffectTrigger
) => EffectVisual["actor"]["pose"] | null;

/*
================
ArmedAim

A caster's aim: the cast that armed it, its record, the measured height and
the rotator it drives.
================
*/
interface ArmedAim {
	token: number;
	aim: SpineAim;
	target: number;
	height: number;
	readonly rotator: SpineAimRotator;
	lastMs: number;
}

/*
================
createSpineAims
================
*/
export function createSpineAims() {
	const aims = new Map<number, ArmedAim>();
	return {
		/*
		================
		step
		================
		*/
		step( frame: SpineAimFrame, socket?: SpineAimSocket ) {
			/*
			================
			bindingHeight

			A binding's height above its holder's root, as 8E083E measures it: the
			bone socket (with its offset), plus the holder's height for a '*' binding.
			================
			*/
			function bindingHeight(
				actor: CharacterActor | undefined,
				binding: SpineAimBinding | undefined,
				trigger: EffectTrigger
			) {
				if ( !actor || !binding || !socket ) return 0;
				const pose = socket( actor.gid, binding.bone, [ 0, binding.offsetY, 0 ], trigger );
				if ( !pose ) return 0;
				return pose.y - actor.pose.y + (binding.addHeight ? actor.height ?? 0 : 0);
			}
			const nowMs = Math.trunc( frame.now * 1000 );
			let presented: Map<number, CharacterActor> | null = null;
			const actors = () => presented ??= new Map( frame.presented.map( actor => [ actor.gid, actor ] ) );
			const live = new Set<number>();
			for ( const cast of frame.casts ) {
				if ( cast.resultOnly || cast.cancelledAtMs !== undefined ) continue;
				const aim = frame.catalog?.[String( cast.skill )]?.spineAim;
				if ( !aim || !cast.target || cast.target === cast.caster ) continue;
				live.add( cast.token );
				const armed = aims.get( cast.caster );
				if ( armed?.token === cast.token ) continue;
				const trigger: EffectTrigger = { cast, phase: "READY", event: 0, at: frame.now, sampleCurrent: true };
				const height = aim.start || aim.target ?
					bindingHeight( actors().get( cast.target ), aim.target, trigger ) -
					bindingHeight( actors().get( cast.caster ), aim.start, trigger ) :
					0;
				const rotator = armed?.rotator ?? { current: [ 0, 0, 0 ], target: [ 0, 0, 0 ], releasing: false };
				rotator.releasing = false;
				aims.set( cast.caster, {
					token: cast.token,
					aim,
					target: cast.target,
					height,
					rotator,
					lastMs: armed?.lastMs ?? nowMs
				} );
			}
			for ( const trigger of frame.triggers ) {
				const armed = aims.get( trigger.cast.caster ), release = armed?.aim.release;
				if (
					armed && release && armed.token === trigger.cast.token && trigger.phase === release.phase &&
					trigger.event === release.event
				) armed.rotator.releasing = true;
			}
			for ( const [caster, armed] of aims ) {
				const rotator = armed.rotator;
				if ( !live.has( armed.token ) ) rotator.releasing = true;
				if ( rotator.releasing ) rotator.target.fill( 0 );
				else {
					const from = actors().get( caster )?.pose, to = actors().get( armed.target )?.pose;
					if ( from && to ) {
						const angle = spineAimBearing( from, to, armed.height );
						rotator.target.splice( 0, 3, ...spineAimTargets( armed.aim.axis, angle ) );
					}
				}
				const elapsed = Math.max( 0, nowMs - armed.lastMs );
				armed.lastMs = nowMs;
				if ( !stepSpineAim( rotator, elapsed ) ) aims.delete( caster );
			}
		},
		/*
		================
		rotation

		The caster's Spine_Base rotation in model space, while a rotator exists.
		================
		*/
		rotation( gid: number ): [number, number, number, number] | undefined {
			const armed = aims.get( gid );
			return armed ? spineAimRotation( armed.rotator ) : undefined;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			aims.clear();
		}
	};
}
