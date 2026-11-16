/*
===========================================================================

scenery-emission.ts - the effects authored on placed scenery, as actors

Every placed object's authored effects become emitter actors of the
character renderer, one per placement/compound branch/entry. An emitter's
lifetime follows its placement, not the camera or its resources: its clock
starts once its effect is resident and survives scene-origin changes.

Each actor carries whether its placement was in the last view
(holderVisible): the renderer stops the clock of an emitter whose holder is
out of view, as the original ticks an object attached to a compound only on
a frame the compound is visible (A9EFB0).

===========================================================================
*/
import { createPresentationIds } from "./presentation-ids";
import type { SceneryPresentation } from "@/engine/contracts/scenery";
import type { CharacterActor } from "@/engine/contracts/character";

/*
================
createSceneryEmission
================
*/
export function createSceneryEmission( allocate: () => number = createPresentationIds() ) {
	const rows = new Map<
		string,
		{ source: string; started: number | null; attempted: boolean; actor: CharacterActor; }
	>();
	return {
		/*
		================
		step
		================
		*/
		step(
			scene: SceneryPresentation | null,
			seconds: number,
			ready: ( path: string ) => boolean,
			capacity: number
		): readonly CharacterActor[] {
			if ( !Number.isFinite( seconds ) ) throw Error( "Invalid scenery clock" );
			const keep = new Set<string>(), out: CharacterActor[] = [];
			for ( const source of scene?.emitters ?? [] ) {
				keep.add( source.id );
				let row = rows.get( source.id );
				if ( !row || row.source !== source.model || row.started !== null && seconds < row.started ) {
					row = {
						source: source.model,
						started: null,
						attempted: false,
						actor: {
							gid: allocate(),
							deferredParticle: source.renderPriority || source.nightOnly ?
								{ offset: source.renderPriority, nightOnly: source.nightOnly } :
								undefined,
							model: source.model,
							pose: source.pose,
							effectBasis: source.basis,
							clip: "effect",
							time: 0,
							loop: true,
							scale: 1,
							pickable: false
						}
					};
					rows.set( source.id, row );
				}
				if ( out.length >= capacity || !ready( source.model ) ) continue;
				// Begin the EFP clock on admission, not before its bytes exist.
				// Subsequent scene-origin changes preserve identity and do not
				// restart the emitter. AEC3B0 refuses a night-only start during
				// daylight. A2E4D0 only writes the night flag; it does not retry
				// activation. Existing instances survive daylight and AEC4B0
				// pauses their rendering/ticks until night returns.
				if ( !row.attempted ) {
					row.attempted = true;
					if ( !source.nightOnly || scene!.night ) row.started = seconds;
				}
				if ( row.started === null ) continue;
				out.push( {
					...row.actor,
					pose: source.pose,
					effectBasis: source.basis,
					time: seconds - row.started,
					holderVisible: scene!.holderVisible?.( source ) ?? true
				} );
			}
			for ( const id of rows.keys() ) if ( !keep.has( id ) ) rows.delete( id );
			return out;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rows.clear();
		}
	};
}
