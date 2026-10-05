/*
===========================================================================

structure-stage.ts - a fortress structure's model stage and damage effects

CICATStruct shows its hit points twice: a model stage (0 standing, 1
damaged, 2 destroyed; CICATStruct_SetVisualStage 4F78A0) and a damage level
0..5 whose atstructeffect effects burn on it (CICATStruct_ApplyDamageVisualLevel
4F79A0). CICATStruct_UpdateDamageVisualStage (4F7B30) derives both from the
hit points once a second. The bake publishes the atstructeffect rows on the
structure's manifest entry; this module reads them and owns the rules.

===========================================================================
*/
import type { ModelParticle } from "../animation/model-particles";

// 4F7B30: 0.600000024f (float 0.6), compared after the ratio is stored as a float.
export const STRUCTURE_DAMAGED_RATIO = 0.6000000238418579;
export const STRUCTURE_DAMAGE_LEVELS = 5;
export const STRUCTURE_STAGE_DAMAGED = 1;
export const STRUCTURE_STAGE_DESTROYED = 2;
// Spawn state word (+0x764) bit 0 shows the destroyed stage whatever the hit points.
export const STRUCTURE_STATE_DESTROYED = 1;
// LoadVisual (4F8200) arms state timer 1 at 1000 ms; OnStateTimer (4F7CC0) re-evaluates.
export const STRUCTURE_VISUAL_PERIOD_MS = 1000;
// 4F79A0: an effect's particle type plays always (0), only as damage rises
// (1, the "downgrade" column) or only as it falls (2, "upgrade"); a
// structure's first level is transition 3, which only type 0 matches.
export const STRUCTURE_TRANSITION_RISE = 1;
export const STRUCTURE_TRANSITION_FALL = 2;
export const STRUCTURE_TRANSITION_FIRST = 3;

const STAGE_COUNT = 3;
const LEVEL_COUNT = 6;

/*
================
StructureStageModel
================
*/
export interface StructureStageModel {
	readonly glb: string;
	readonly particles: readonly ModelParticle[];
}

/*
================
StructureEffect
================
*/
export interface StructureEffect {
	readonly particle: ModelParticle;
	readonly transition: number;
}

/*
================
StructureVisuals

One structure's atstructeffect record: stage models, stage sounds (with
the camera shake flag) and each damage level's effects.
================
*/
export interface StructureVisuals {
	readonly stages: readonly (StructureStageModel | undefined)[];
	readonly sounds: readonly ({ readonly handle: string; readonly shake: boolean; } | undefined)[];
	readonly levels: readonly (readonly StructureEffect[] | undefined)[];
}

/*
================
indexed

A manifest object keyed "0".."n-1", read into a dense array.
================
*/
function indexed<T>( value: unknown, count: number, read: ( row: unknown ) => T ): (T | undefined)[] {
	if ( value === undefined ) return [];
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) {
		throw Error( "Invalid structure visual table" );
	}
	const out: (T | undefined)[] = [];
	for ( const [key, row] of Object.entries( value ) ) {
		const index = Number( key );
		if ( !Number.isInteger( index ) || index < 0 || index >= count ) {
			throw Error( "Invalid structure visual index" );
		}
		out[index] = read( row );
	}
	return out;
}

/*
================
effectPath
================
*/
function effectPath( value: unknown ): string {
	if (
		typeof value !== "string" || !value.endsWith( ".efp" ) || value.startsWith( "/" ) || value.includes( ".." ) ||
		value.includes( ":" )
	) throw Error( "Invalid structure effect path" );
	return value;
}

/*
================
readStructureVisuals

The structureStages / structureSounds / structureDamageEffects fields of a
structure's manifest entry. `stageParticles` reads a stage model's own
particle modifiers (the shared ambient reader). Offsets are BSR-space like
a model's own particles, so z flips with the compiled mesh.
================
*/
export function readStructureVisuals(
	row: { structureStages?: unknown; structureSounds?: unknown; structureDamageEffects?: unknown; },
	stageParticles: ( modifiers: unknown ) => readonly ModelParticle[]
): StructureVisuals | undefined {
	if (
		row.structureStages === undefined && row.structureSounds === undefined &&
		row.structureDamageEffects === undefined
	) return undefined;
	const stages = indexed( row.structureStages, STAGE_COUNT, value => {
		const stage = value as { glb?: unknown; particleModifiers?: unknown; };
		if (
			!stage || typeof stage.glb !== "string" || !stage.glb.startsWith( "/assets/npc/" ) ||
			stage.glb.includes( ".." ) || !stage.glb.endsWith( ".glb" )
		) throw Error( "Invalid structure stage model" );
		return { glb: stage.glb, particles: stageParticles( stage.particleModifiers ) };
	} );
	const sounds = indexed( row.structureSounds, STAGE_COUNT, value => {
		const sound = value as { handle?: unknown; shake?: unknown; };
		if ( !sound || typeof sound.handle !== "string" || !sound.handle || typeof sound.shake !== "boolean" ) {
			throw Error( "Invalid structure stage sound" );
		}
		return { handle: sound.handle, shake: sound.shake };
	} );
	const levels = indexed( row.structureDamageEffects, LEVEL_COUNT, value => {
		if ( !Array.isArray( value ) || value.length > 64 ) throw Error( "Invalid structure damage effects" );
		return value.map( ( candidate ): StructureEffect => {
			const effect = candidate as { effectPath?: unknown; offset?: unknown; loop?: unknown; particle?: unknown; };
			const offset = effect?.offset;
			if (
				!Array.isArray( offset ) || offset.length !== 3 || !offset.every( Number.isFinite ) ||
				typeof effect.loop !== "boolean" || ![ 0, 1, 2 ].includes( effect.particle as number )
			) throw Error( "Invalid structure damage effect" );
			return {
				particle: {
					effectPath: effectPath( effect.effectPath ),
					bone: "",
					root: true,
					offset: [ offset[0], offset[1], -offset[2] ],
					...(effect.loop ? {} : { loop: false })
				},
				transition: effect.particle as number
			};
		} );
	} );
	return { stages, sounds, levels };
}

/*
================
structureVisualTarget

4F7B30: no hit points or the destroyed state bit show stage 2 at level 5;
full hit points stage 0 at level -1 (no decal); otherwise stage 1 at 60%
or less, and the level counts lost fifths, truncated.
================
*/
export function structureVisualTarget( hp: number, maxHp: number, state: number ): { stage: number; level: number; } {
	if ( !maxHp || state & STRUCTURE_STATE_DESTROYED ) {
		return { stage: STRUCTURE_STAGE_DESTROYED, level: STRUCTURE_DAMAGE_LEVELS };
	}
	if ( hp === maxHp ) return { stage: 0, level: -1 };
	const ratio = Math.fround( hp / maxHp );
	return {
		stage: ratio <= STRUCTURE_DAMAGED_RATIO ? STRUCTURE_STAGE_DAMAGED : 0,
		level: Math.trunc( STRUCTURE_DAMAGE_LEVELS - ratio * STRUCTURE_DAMAGE_LEVELS )
	};
}

/*
================
structureEffectTransition

4F79A0's transition from the level last shown to the next one.
================
*/
export function structureEffectTransition( previous: number, next: number ): number {
	if ( previous < 0 ) return STRUCTURE_TRANSITION_FIRST;
	return next < previous ? STRUCTURE_TRANSITION_FALL : STRUCTURE_TRANSITION_RISE;
}

/*
================
structureLevelParticles

The effects of `level` that the transition admits, or undefined when the
record has no such level (4F7420 returns null: the stored level stays).
================
*/
export function structureLevelParticles(
	visuals: StructureVisuals,
	level: number,
	transition: number
): readonly ModelParticle[] | undefined {
	if ( level < 0 || level >= LEVEL_COUNT ) return undefined;
	const effects = visuals.levels[level] ?? [];
	return effects.filter( effect => effect.transition === 0 || effect.transition === transition ).map( effect =>
		effect.particle
	);
}
