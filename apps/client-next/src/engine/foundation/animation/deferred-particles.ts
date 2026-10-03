/*
===========================================================================

deferred-particles.ts - occlusion-queried EFP emitters and their clock

Light effects behind an occlusion query (AAEA80/AEC0D0/AEC180) fade by
visibility and tick only while drawn. The renderer owns this frame clock;
visibility gates actual 50 ms EFP ticks, not just fragment opacity.

===========================================================================
*/

import { createModifierDelta } from "@/engine/foundation/rendering/modifier-delta";
import { CHARACTER_ACTORS } from "./character-budget";
import type { CharacterActor } from "@/engine/contracts/character";
/*
================
createDeferredParticles
================
*/
// A2E980 / AAEA80 / AEC0D0 / AEC180. The renderer owns this frame clock;
// visibility gates actual 50-ms EFP ticks, not just fragment opacity.
export function createDeferredParticles() {
	let previousSeconds: number | null = null;
	let deltaFor = createModifierDelta(), frame = 0, delta = 0, tickRemainder = 0, ticks = 0, queryRemainder = 0;
	const rows = new Map<
		number,
		{
			model: string;
			sourceTime: number;
			alpha: number;
			instanceAlpha: number;
			deferred: boolean;
			visible: boolean;
			last: number;
			time: number;
			draw: boolean;
		}
	>();
	let queued: readonly number[] = [], eligible: readonly number[] = [], due = false;
	return {
		begin( seconds: number, actors: readonly CharacterActor[], enabled = true, night = true ) {
			if ( previousSeconds !== null && seconds < previousSeconds ) tickRemainder = 0;
			previousSeconds = seconds;
			delta = deltaFor( seconds );
			frame = (frame + 1) >>> 0;
			tickRemainder += delta;
			ticks = Math.trunc( tickRemainder / 50 );
			tickRemainder -= ticks * 50;
			if ( !actors.length && !rows.size ) return;
			const keep = new Set<number>();
			for ( const actor of actors ) {
				if ( !actor.deferredParticle ) continue;
				keep.add( actor.gid );
				let row = rows.get( actor.gid );
				if ( !row || row.model !== actor.model || actor.time < row.sourceTime ) {
					row = {
						model: actor.model,
						sourceTime: actor.time,
						alpha: 0,
						instanceAlpha: 255,
						deferred: true,
						visible: false,
						last: frame,
						time: 0,
						draw: false
					};
					rows.set( actor.gid, row );
				} else row.sourceTime = actor.time;
				const route = actor.deferredParticle.lodHidden ?
					"hidden" :
					particleRenderRoute(
						actor.deferredParticle.offset,
						true,
						enabled,
						!!actor.deferredParticle.nightOnly,
						night
					);
				row.deferred = route === "deferred";
				row.draw = route === "immediate";
				// AEC4B0 -> AEC040: disabled queries use ordinary depth rendering and
				// still tick even if a previous query left instance alpha at zero.
				if ( row.draw ) row.time += ticks / 20;
			}
			if ( keep.size > CHARACTER_ACTORS ) throw Error( "Deferred particle capacity exceeded" );
			for ( const gid of rows.keys() ) if ( !keep.has( gid ) ) rows.delete( gid );
			queued = [];
			eligible = [];
			due = false;
		},
		plan( ids: readonly number[], enabled: ReadonlySet<number> ) {
			if (
				ids.length > CHARACTER_ACTORS || new Set( ids ).size !== ids.length || ids.some( id => !rows.has( id ) )
			) throw Error( "Invalid deferred particle queue" );
			queued = ids;
			eligible = ids.filter( id => enabled.has( id ) );
			if ( !ids.length ) return false;
			queryRemainder += delta;
			due = queryRemainder > 500;
			if ( due ) queryRemainder %= 500;
			return due && eligible.length > 0;
		},
		eligible: () => eligible,
		complete( results?: readonly boolean[] ) {
			if ( results && (results.length !== eligible.length || !due) ) {
				throw Error( "Invalid particle query results" );
			}
			if ( due && eligible.length && !results ) throw Error( "Missing particle query results" );
			const enabled = new Set( eligible ),
				visibility = new Map( eligible.map( ( id, i ) => [ id, results?.[i] ] ) );
			for ( const id of queued ) {
				const row = rows.get( id )!,
					allowed = enabled.has( id ),
					visible = allowed ? (due ? visibility.get( id )! : row.visible) : false;
				if ( !advanceDeferredAlpha( row, visible, frame, delta ) ) {
					row.draw = false;
					continue;
				}
				row.instanceAlpha = row.alpha;
				row.draw = (allowed || due) && row.alpha > 0;
				if ( row.draw ) row.time += ticks / 20;
			}
		},
		sample: ( gid: number ) => rows.get( gid ),
		// Presentation only: how far into the next 50 ms tick this frame is, in
		// seconds (particle-presentation.ts smoothing deviation). Below one tick,
		// so adding it to a drawn row's time never changes the tick it samples.
		pendingSeconds: () => tickRemainder / 1000,
		reset() {
			previousSeconds = null;
			rows.clear();
			queued = [];
			eligible = [];
			deltaFor = createModifierDelta();
			frame =
				delta =
				tickRemainder =
				ticks =
				queryRemainder =
					0;
		}
	};
}
/*
================
particleQueryPoint
================
*/
// AAEA80 offsets the query point towards the camera by byte90 - 1.
export function particleQueryPoint(
	position: readonly number[],
	camera: readonly number[],
	offset: number
): readonly number[] {
	if (
		position.length !== 3 || camera.length !== 3 || !position.every( Number.isFinite ) ||
		!camera.every( Number.isFinite ) || !Number.isInteger( offset ) || offset < 1 || offset > 255
	) throw Error( "Invalid particle query point" );
	const f = Math.fround,
		d = position.map( ( v, i ) => f( camera[i]! - v ) ),
		length = f( Math.sqrt( f( d.reduce( ( n, v ) => n + v * v, 0 ) ) ) ),
		inverse = f( length > 0 ? 1 / length : 0 );
	return position.map( ( v, i ) => f( v + f( f( d[i]! * inverse ) * (offset - 1) ) ) );
}

export interface DeferredVisibility {
	alpha: number;
	visible: boolean;
	last: number;
}
/** AEC0D0: a missing previous-frame stamp clears alpha without stamping. */
/*
================
advanceDeferredAlpha
================
*/
export function advanceDeferredAlpha(
	row: DeferredVisibility,
	visible: boolean,
	frame: number,
	delta: number
): boolean {
	row.visible = visible;
	if ( visible ) row.alpha = Math.min( 255, row.alpha + Math.trunc( delta * 1.5 ) );
	else if ( row.last !== ((frame - 1) >>> 0) ) {
		row.alpha = 0;
		return false;
	} else row.alpha = Math.max( 0, row.alpha - Math.trunc( delta * 1.5 ) );
	row.last = frame;
	return true;
}

/** AEC4B0: active instance, backend present, no pending start delay. */
/*
================
particleRenderRoute
================
*/
export function particleRenderRoute(
	offset: number,
	queriesSupported: boolean,
	lightEffect: boolean,
	nightOnly: boolean,
	night: boolean
): "hidden" | "deferred" | "immediate" {
	if ( nightOnly && !night ) return "hidden";
	return offset > 0 && queriesSupported && lightEffect ? "deferred" : "immediate";
}
