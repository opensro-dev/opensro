/*
===========================================================================

structure-visuals.ts - the shown stage and damage level of each structure

Owns, per fortress structure gid, the CICATStruct fields +0x768 (stage
shown) and +0x76C (damage level shown) and the one-second state timer that
re-evaluates them (structure-stage.ts holds the rules). The character
presenter steps it once a frame, reads the stage model and effects it
resolved, and plays the stage sounds it reports.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import {
	STRUCTURE_STAGE_DESTROYED,
	STRUCTURE_VISUAL_PERIOD_MS,
	structureEffectTransition,
	structureLevelParticles,
	type StructureStageModel,
	structureVisualTarget,
	type StructureVisuals
} from "@/engine/foundation/rendering/structure-stage";

/*
================
StructureStageEvent

A stage reached by rising damage: its sound and camera shake (4F78A0).
================
*/
export interface StructureStageEvent {
	readonly gid: number;
	readonly handle?: string;
	readonly shake: boolean;
}

/*
================
StructureVisualRow

One staged structure this frame: its entity, its atstructeffect record and
its effective hit points (undefined before any vitals: the spawn's).
================
*/
export interface StructureVisualRow {
	readonly entity: EntityState;
	readonly visuals: StructureVisuals;
	readonly hp: number | undefined;
}

interface Shown {
	stage: number;
	level: number;
	dueMs: number;
	model: StructureStageModel | undefined;
	particles: readonly ModelParticle[];
	appearance: StructureAppearance | undefined;
}

/*
================
StructureAppearance

The model and effects to draw: identity-stable while nothing changes, so
model emission keeps its running effect clocks.
================
*/
export interface StructureAppearance {
	readonly base: readonly ModelParticle[];
	readonly glb: string;
	readonly particles: readonly ModelParticle[];
}

const NO_PARTICLES: readonly ModelParticle[] = [];

/*
================
createStructureVisuals
================
*/
export function createStructureVisuals() {
	const shown = new Map<number, Shown>();

	/*
	================
	showStage

	4F78A0: clamps to 0..2; a new stage loads its model (or the record's
	own, kept by an undefined model) and, when it exceeds a shown stage,
	plays its sound and shakes the camera. Returns whether it changed.
	================
	*/
	function showStage(
		row: Shown,
		gid: number,
		visuals: StructureVisuals,
		stage: number,
		out: StructureStageEvent[]
	) {
		const next = Math.min( Math.max( stage, 0 ), STRUCTURE_STAGE_DESTROYED );
		if ( next === row.stage ) return false;
		if ( row.stage >= 0 && next > row.stage ) {
			const sound = visuals.sounds[next];
			if ( sound ) out.push( { gid, handle: sound.handle, shake: sound.shake } );
		}
		row.model = visuals.stages[next];
		row.stage = next;
		row.appearance = undefined;
		return true;
	}

	/*
	================
	showLevel

	4F79A0: a forced call (the stage changed) or a new level clears the
	shown effects, then shows the level's effects if the record has it.
	================
	*/
	function showLevel( row: Shown, visuals: StructureVisuals, level: number, forced: boolean ) {
		if ( !forced && level === row.level ) return;
		row.particles = NO_PARTICLES;
		row.appearance = undefined;
		const particles = structureLevelParticles( visuals, level, structureEffectTransition( row.level, level ) );
		if ( !particles ) return;
		row.particles = particles;
		row.level = level;
	}

	return {
		/*
		================
		step
		================
		*/
		step( rows: readonly StructureVisualRow[], nowMs: number ): readonly StructureStageEvent[] {
			const events: StructureStageEvent[] = [], present = new Set<number>();
			for ( const { entity, visuals, hp: effectiveHp } of rows ) {
				present.add( entity.gid );
				let row = shown.get( entity.gid );
				if ( !row ) {
					row = {
						stage: -1,
						level: -1,
						dueMs: nowMs,
						model: undefined,
						particles: NO_PARTICLES,
						appearance: undefined
					};
					shown.set( entity.gid, row );
				}
				if ( nowMs < row.dueMs ) continue;
				row.dueMs = nowMs + STRUCTURE_VISUAL_PERIOD_MS;
				const hp = effectiveHp ?? entity.structureHp ?? 0;
				const target = structureVisualTarget( hp, entity.maxHp ?? 0, entity.structureState ?? 0 );
				showLevel( row, visuals, target.level, showStage( row, entity.gid, visuals, target.stage, events ) );
			}
			for ( const gid of shown.keys() ) if ( !present.has( gid ) ) shown.delete( gid );
			return events;
		},
		/*
		================
		appearance

		What gid shows over its record's own model and particles, or
		undefined when it is not a staged structure. A stage without its own
		model keeps the record's (4F78A0's fallback).
		================
		*/
		appearance( gid: number, glb: string, particles: readonly ModelParticle[] ): StructureAppearance | undefined {
			const row = shown.get( gid );
			if ( !row || row.stage < 0 ) return undefined;
			const base = row.model?.particles ?? particles;
			if ( !row.appearance || row.appearance.base !== base || (!row.model && row.appearance.glb !== glb) ) {
				row.appearance = {
					base,
					glb: row.model?.glb ?? glb,
					particles: row.particles.length ? [ ...base, ...row.particles ] : base
				};
			}
			return row.appearance;
		},
		reset() {
			shown.clear();
		}
	};
}
