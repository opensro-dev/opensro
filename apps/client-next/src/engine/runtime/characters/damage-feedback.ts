/*
===========================================================================

damage-feedback.ts - ordered cast result consumption and flight handoff

The action owner advances callbacks; this owner consumes each target/result
once, including 8DCF40's cancellation flush of the remaining result rows.

===========================================================================
*/
import { castResultAt, castResultIndex, castResultStageCount } from "@/engine/foundation/gameplay/cast-results";
import type { CastState } from "@/engine/contracts/gameplay";
import type { EffectTrigger, EffectImpactEvent, ImpactFeedback } from "@/engine/contracts/effects";
export type { ImpactFeedback } from "@/engine/contracts/effects";
/*
================
createDamageFeedback
================
*/
export function createDamageFeedback( release: ( key: string, at: number ) => void = () => {} ) {
	const consumed = new Map<number, Set<string>>();
	const passed = new Map<number, Set<number>>();
	const flights = new Map<number, ImpactFeedback[]>();
	return {
		/*
  ================
  take
  ================
  */
		take(
			casts: readonly CastState[],
			triggers: readonly EffectTrigger[],
			index: ( skill: number, phase: string, event: number ) => number,
			now: number,
			simulationMs: number | undefined,
			transfers: readonly EffectImpactEvent[] = []
		): readonly ImpactFeedback[] {
			const active = new Set( casts.map( c => c.token ) );
			for ( const token of consumed.keys() ) if ( !active.has( token ) ) consumed.delete( token );
			for ( const token of passed.keys() ) if ( !active.has( token ) ) passed.delete( token );
			const output: ImpactFeedback[] = [];
			/*
   ================
   admit
   ================
   */
			function admit(
				cast: CastState,
				n: number,
				at: number,
				source: ImpactFeedback["source"],
				destination = output,
				target?: number
			) {
				let seen = consumed.get( cast.token );
				if ( !seen ) {
					seen = new Set();
					consumed.set( cast.token, seen );
				}
				for ( const row of cast.results ?? [ { target: cast.target, impacts: cast.impacts ?? [] } ] ) {
					if ( target !== undefined && row.target !== target ) continue;
					const key = `${row.target}:${n}`, impact = castResultAt( row.impacts, n );
					if ( !impact || seen.has( key ) ) continue;
					seen.add( key );
					if ( cast.discardPendingResults ) {
						release( `${cast.token}:${key}`, simulationMs ?? at * 1000 );
						continue;
					}
					destination.push( { cast, target: row.target, impact, key: `${cast.token}:${key}`, at, source } );
				}
			}
			// 8DDDE0 transfers a result vector out of the cast; 8D7FD0 applies
			// the retained result at arrival. Cancellation cannot reclaim it.
			for ( const event of transfers ) {
				if ( event.kind === "launch" ) {
					const held: ImpactFeedback[] = [];
					admit(
						event.cast,
						event.index,
						event.at,
						"cast",
						held,
						event.allTargets ? undefined : event.target
					);
					if ( held.length ) {
						flights.set( event.flight, held.map( hit => ({ ...hit, soundSkill: event.soundSkill ?? 0 }) ) );
					}
				} else {
					const held = flights.get( event.flight );
					if ( !held ) continue;
					if ( event.kind === "hop" || event.kind === "skip" ) {
						const remaining = held.filter( hit => hit.target !== event.target );
						if ( remaining.length ) flights.set( event.flight, remaining );
						else flights.delete( event.flight );
						if ( event.kind === "skip" ) {
							for ( const hit of held ) {
								if ( hit.target === event.target ) release( hit.key, simulationMs ?? now * 1000 );
							}
						}
						if ( event.kind === "hop" ) {
							for ( const hit of held ) {
								if ( hit.target === event.target ) {
									output.push( {
										...hit,
										at: event.at,
										position: event.atTarget || hit.target === event.target ?
											event.position :
											undefined,
										secondary: event.secondary
									} );
								}
							}
						}
					} else {
						flights.delete( event.flight );
						if ( event.kind !== "arrival" ) {
							for ( const hit of held ) release( hit.key, simulationMs ?? now * 1000 );
						}
						if ( event.kind === "arrival" ) {
							for ( const hit of held ) {
								output.push( {
									...hit,
									at: event.at,
									position: event.atTarget || hit.target === event.target ?
										event.position :
										undefined,
									secondary: event.secondary
								} );
							}
						}
					}
				}
			}
			// 8E0190: results arriving behind the callback cursor are applied now.
			// Remember the callback even when its target/result packet has not arrived.
			for ( const cast of casts ) {
				for ( const n of passed.get( cast.token ) ?? [] ) admit( cast, n, now, "flush" );
			}
			for ( const trigger of triggers ) {
				const n = index( trigger.cast.skill, trigger.phase, trigger.event );
				if ( n >= 0 ) {
					let markers = passed.get( trigger.cast.token );
					if ( !markers ) {
						markers = new Set();
						passed.set( trigger.cast.token, markers );
					}
					markers.add( n );
					admit( trigger.cast, n, trigger.at, "flush" );
				}
			}
			for ( const cast of casts ) {
				if (
					cast.cancelledAtMs !== undefined &&
					(simulationMs === undefined || cast.cancelledAtMs <= simulationMs)
				) {
					const at = simulationMs === undefined ? now : now + (cast.cancelledAtMs - simulationMs) / 1000;
					const count = castResultStageCount( cast );
					for ( let n = 0; n < count; n++ ) admit( cast, n, at, "flush" );
				}
			}
			return output;
		},
		/*
  ================
  pendingDeaths
  ================
  */
		pendingDeaths(
			casts: readonly CastState[],
			currentResult?: ( gid: number, key: string ) => boolean
		): ReadonlySet<number> {
			const pending = new Set<number>();
			for ( const cast of casts ) {
				for ( const row of cast.results ?? [ { target: cast.target, impacts: cast.impacts ?? [] } ] ) {
					for ( const [i, hit] of row.impacts.entries() ) {
						const stage = castResultIndex( hit, i );
						if (
							!cast.discardPendingResults && hit.fatal &&
							!consumed.get( cast.token )?.has( `${row.target}:${stage}` ) &&
							(!currentResult || currentResult( row.target, `${cast.token}:${row.target}:${stage}` ))
						) {
							pending.add( row.target );
						}
					}
				}
			}
			for ( const held of flights.values() ) {
				for ( const hit of held ) {
					if ( hit.impact.fatal && (!currentResult || currentResult( hit.target, hit.key )) ) {
						pending.add( hit.target );
					}
				}
			}
			return pending;
		},
		/*
  ================
  reset
  ================
  */
		reset() {
			consumed.clear();
			passed.clear();
			flights.clear();
		}
	};
}
