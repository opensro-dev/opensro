/*
===========================================================================

stale-draws.ts - no released geometry reaches a submit, and the owner that
still listed it is named

A released draw's buffers are destroyed when the releasing frame closes
(device/retirement.ts). An owner that keeps the draw in a list it hands to
a later frame makes WebGPU fail the whole submit ("[Buffer
"geometry-instances"] used in submit while destroyed") and the renderer
stops. That has shipped three times from different owners, each found by
guessing.

Every list the frame draws passes through here first. A released draw is
dropped (it can never be valid to draw) and reported once per list and
release site: which list still held it, where in the list, how long after
its release, and the stack that released it. The report is the bug report.

===========================================================================
*/
import type { DrawRelease, GeometryDraw } from "../internal/gpu-contract";

// One report per list and releasing stack: a stale draw recurs every frame.
const MAX_REPORTED_SITES = 32;

/*
================
StaleDrawReport
================
*/
export interface StaleDrawReport {
	readonly list: string;
	readonly index: number;
	readonly listLength: number;
	readonly releasedMsAgo: number;
	readonly releaseStack: string;
}

/*
================
createStaleDrawGuard

lookup is the geometry owner's release record; report receives each new
stale site.
================
*/
export function createStaleDrawGuard(
	lookup: ( draw: GeometryDraw ) => DrawRelease | undefined,
	report: ( stale: StaleDrawReport ) => void
) {
	const reported = new Set<string>();
	return {
		/*
		================
		live

		The list without released draws; the same array when none is stale,
		so retained bundles keyed on list identity stay valid.
		================
		*/
		live<T extends GeometryDraw>( list: string, draws: readonly T[] ): readonly T[] {
			let kept: T[] | null = null;
			for ( let i = 0; i < draws.length; i++ ) {
				const draw = draws[i]!, release = lookup( draw );
				if ( !release ) {
					kept?.push( draw );
					continue;
				}
				kept ??= draws.slice( 0, i );
				const key = list + "\n" + release.stack;
				if ( !reported.has( key ) && reported.size < MAX_REPORTED_SITES ) {
					reported.add( key );
					report( {
						list,
						index: i,
						listLength: draws.length,
						releasedMsAgo: Math.round( performance.now() - release.atMs ),
						releaseStack: release.stack
					} );
				}
			}
			return kept ?? draws;
		},
		/*
		================
		single

		One optional draw through the same rule.
		================
		*/
		single<T extends GeometryDraw>( list: string, draw: T | undefined ): T | undefined {
			return draw && this.live( list, [ draw ] ).length ? draw : undefined;
		}
	};
}
