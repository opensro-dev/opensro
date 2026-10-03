/*
===========================================================================

resources.ts - UI image demand, loading and residency

Loads the images the interface asks for, retries failures with backoff,
and keeps recent inactive images below 48 MiB / 480 entries (LRU). Runs
every frame while demand is unsettled, so it allocates nothing per entry.

===========================================================================
*/

import type { AssetOwner } from "@/engine/contracts/assets";

/*
================
createUiAssets

Retry history belongs to current demand and disappears when a screen releases it.
================
*/
export function createUiAssets(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	publish: ( id: string, image: ImageBitmap | null ) => void,
	base: string,
	report: (
		event: { kind: "failed" | "recovered" | "released"; path: string; attempts: number; message: string; }
	) => void = event => {
		if ( event.kind === "failed" ) console.warn( "[ui-assets]", event );
		else console.info( "[ui-assets]", event );
	}
) {
	const pending = new Map<string, number>();
	const loaded = new Map<string, readonly [number, number]>();
	const failures = new Map<string, { attempts: number; retryAt: number; message: string; }>();
	const missingCrests = new Set<string>();
	const crest = ( path: string ) => /\/marks\/[GA][0-9]{1,10}_[0-9]{1,10}_[0-9]{1,10}\.crb$/.test( path );
	let disposed = false;
	let wanted = new Set<string>();
	let previousPaths: readonly string[] = [], settled = false;
	// Counts removals from `loaded`. Between two removals the resident set only
	// grows, so a list found fully resident stays so until the count moves.
	let evictions = 0;
	const residentLists = new WeakMap<readonly string[], number>();

	/*
	================
	trim

	UI lifetime owns decoded readiness. Keep recent inactive images below
	48 MiB / 480 entries, evicting LRU
	inactive entries before publication. Paths are immutable for this owner;
	disposal releases everything and a new owner reconstructs from assets.
	Renderer derives GPU residency from committed quads; prewarming a hidden
	window must not allocate one GPU descriptor per downloaded sprite.
	================
	*/
	function trim( incomingBytes = 0, incomingCount = 0 ) {
		let bytes = incomingBytes;
		for ( const size of loaded.values() ) bytes += size[0] * size[1] * 4;
		// Keys, not entries: when every resident image is wanted this walks the
		// whole map each frame, and entry pairs were a million objects a minute.
		for ( const path of loaded.keys() ) {
			if ( bytes <= 48 * 1024 * 1024 && loaded.size + incomingCount <= 480 ) break;
			if ( wanted.has( path ) ) continue;
			const size = loaded.get( path )!;
			publish( path, null );
			loaded.delete( path );
			evictions++;
			bytes -= size[0] * size[1] * 4;
		}
	}

	/*
	================
	fail
	================
	*/
	function fail( path: string, message: string, now: number ) {
		const previous = failures.get( path );
		const attempts = Math.min( 6, (previous?.attempts ?? 0) + 1 );
		failures.set( path, { attempts, retryAt: now + Math.min( 30_000, 1000 * 2 ** (attempts - 1) ), message } );
		// Report each distinct failure, not every frame or repeated retry.
		if ( !previous || previous.message !== message ) report( { kind: "failed", path, attempts, message } );
	}

	/*
	================
	clearFailure
	================
	*/
	function clearFailure( path: string, kind: "recovered" | "released" ) {
		const failure = failures.get( path );
		if ( !failure ) return false;
		failures.delete( path );
		report( { kind, path, attempts: failure.attempts, message: failure.message } );
		return true;
	}

	return {
		/*
		================
		step
		================
		*/
		step( paths: readonly string[], now: number ) {
			if ( disposed ) return false;
			let demandChanged = paths.length !== previousPaths.length;
			if ( !demandChanged ) {
				for ( let i = 0; i < paths.length; i++ ) {
					if ( paths[i] !== previousPaths[i] ) {
						demandChanged = true;
						break;
					}
				}
			}
			if ( !demandChanged && settled ) return false;
			if ( demandChanged ) {
				previousPaths = [ ...paths ];
				wanted = new Set( paths );
				settled = false;
			}
			for ( const [path, id] of pending ) {
				if ( !wanted.has( path ) ) {
					assets.cancel( id );
					pending.delete( path );
				}
			}
			let changed = false;
			for ( const path of wanted ) {
				const size = loaded.get( path );
				if ( size ) {
					loaded.delete( path );
					loaded.set( path, size );
				}
			}
			trim();
			for ( const path of missingCrests ) {
				if ( !wanted.has( path ) && missingCrests.size > 480 ) missingCrests.delete( path );
			}
			for ( const path of failures.keys() ) {
				if ( !wanted.has( path ) ) {
					clearFailure( path, "released" );
					changed = true;
				}
			}
			for ( const [path, id] of pending ) {
				const result = assets.take( id );
				if ( !result ) continue;
				pending.delete( path );
				if ( result.kind === "image" ) {
					const size = [ result.image.width, result.image.height ] as const;
					trim( size[0] * size[1] * 4, 1 );
					publish( path, result.image );
					loaded.set( path, size );
					clearFailure( path, "recovered" );
					changed = true;
				} else if ( crest( path ) ) {
					missingCrests.add( path );
					changed = true;
				} else {
					fail( path, result.kind === "error" ? result.error : "Expected UI image", now );
					changed = true;
				}
			}
			for ( const path of wanted ) {
				if (
					missingCrests.has( path ) || loaded.has( path ) || pending.has( path ) ||
					(failures.get( path )?.retryAt ?? -Infinity) > now
				) continue;
				if ( assets.available() === 0 ) break;
				try {
					pending.set(
						path,
						assets.request(
							new URL( path, base ).href,
							crest( path ) ? 256 : 4 * 1024 * 1024,
							crest( path ) ? "crest" : "png"
						)
					);
				} catch ( error ) {
					fail( path, String( error ), now );
					changed = true;
				}
			}
			settled = pending.size === 0 && failures.size === 0;
			if ( settled ) {
				for ( const path of wanted ) {
					if ( !loaded.has( path ) && !missingCrests.has( path ) ) {
						settled = false;
						break;
					}
				}
			}
			return changed;
		},
		/*
		================
		stats

		Counts in place: wanted holds every HUD image path, and copying it to
		count the pending ones allocated a large array on every report.
		================
		*/
		stats() {
			let pending = 0;
			for ( const path of wanted ) {
				if ( !loaded.has( path ) && !failures.has( path ) && !missingCrests.has( path ) ) pending++;
			}
			return { pending, failed: [ ...failures.keys() ] };
		},
		has: ( path: string ) => loaded.has( path ),
		/*
		================
		residentAll

		True when every path of an immutable list is resident. A list found
		resident is remembered until the next eviction, so a baseline checked
		every frame is walked only while it loads.
		================
		*/
		residentAll( paths: readonly string[] ): boolean {
			if ( residentLists.get( paths ) === evictions ) return true;
			for ( const path of paths ) if ( !loaded.has( path ) ) return false;
			residentLists.set( paths, evictions );
			return true;
		},
		size: ( path: string ) => loaded.get( path ),
		error: () => {
			const failure = failures.entries().next().value;
			return failure ? `UI image unavailable; retrying: ${failure[0]}: ${failure[1].message}` : null;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			previousPaths = [];
			settled = false;
			const errors: unknown[] = [];
			for ( const id of pending.values() ) {
				try {
					assets.cancel( id );
				} catch ( error ) {
					errors.push( error );
				}
			}
			for ( const path of loaded.keys() ) {
				try {
					publish( path, null );
				} catch ( error ) {
					errors.push( error );
				}
			}
			pending.clear();
			loaded.clear();
			evictions++;
			failures.clear();
			missingCrests.clear();
			wanted.clear();
			if ( errors.length ) throw new AggregateError( errors, "UI image cleanup failed" );
		}
	};
}
