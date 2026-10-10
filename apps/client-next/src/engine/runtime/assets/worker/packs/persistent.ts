/*
===========================================================================

persistent.ts - the durable store of verified asset bytes

One owner for durable verified bytes, bounded publication and LRU order,
kept in Cache Storage keyed by content digest. Cache failure is an
optional-storage failure, never an asset admission failure.

The budget is half the origin quota, between MIN_BUDGET_BYTES and
MAX_BUDGET_BYTES: browsers grant an origin 60% of the disk (Chrome,
Safari) or the smaller of 10% and 10 GiB (Firefox best effort), so the
whole game fits on most machines and a player who explores never
re-downloads areas already visited. Startup entries - the packs and
members of the startup groups every session needs before the first frame -
are never evicted; the rest leave least recently used first, and a write
that cannot fit after eviction is skipped, never stored over budget.

Which entries are startup entries comes from the manifest the asset worker
admitted (setStartup), never from what an earlier release stored: a warm
entry the manifest names is protected at once, and one only an older
release named becomes ordinary LRU data.

===========================================================================
*/

import { isResponseByteLimitError, readBytes } from "@/engine/foundation/assets/read-bytes";

const MIN_BUDGET_BYTES = 512 * 1024 * 1024;
const MAX_BUDGET_BYTES = 4 * 1024 * 1024 * 1024;
// Share of the origin quota this store may fill; the rest stays for the
// browser's own caches and other storage.
const QUOTA_SHARE = 0.5;
// Optional disk reads must yield well before the network's 15-second stall window.
const CACHE_OPERATION_MS = 2000;
const MAX_CACHE_OPERATIONS = 8;

/*
================
budgetFromQuota
================
*/
export function budgetFromQuota( quota: number | undefined ) {
	if ( !quota ) return MIN_BUDGET_BYTES;
	return Math.min( MAX_BUDGET_BYTES, Math.max( MIN_BUDGET_BYTES, Math.floor( quota * QUOTA_SHARE ) ) );
}

/*
================
createPersistentAssets

budgetOf turns the origin quota into this store's byte budget
(budgetFromQuota; tests inject a small one).
================
*/
export function createPersistentAssets( budgetOf: ( quota: number | undefined ) => number = budgetFromQuota ) {
	let opened: Promise<Cache | null> | null = null, tail: Promise<void> = Promise.resolve();
	let inventory: Map<string, number> | null = null, total = 0, budget = MIN_BUDGET_BYTES, estimatedAt = -Infinity;
	// Keys of the current manifest's startup packs and members (setStartup).
	let startup = new Set<string>();
	let hits = 0, misses = 0, writes = 0, errors = 0, evictions = 0, queuedBytes = 0, skipped = 0;
	const pending = new Set<string>(), touched = new Set<string>();
	const disabled = new AbortController();
	let outstanding = 0, cleanups = 0;
	const removals = new Map<string, Promise<void>>();
	/*
	================
	storage

	Cache Storage cannot abort its native operations. Cancellation releases only
	this caller; its native work retains its slot and deadline until settlement.
	Saturation bypasses optional storage. Only a real timeout disables this owner,
	preventing a stuck backend from accumulating abandoned native operations.
	================
	*/
	function storage<T>(
		operation: () => Promise<T>,
		signal?: AbortSignal,
		abandoned?: ( value: T ) => void
	): Promise<T> {
		signal?.throwIfAborted();
		if ( disabled.signal.aborted ) return Promise.reject( disabled.signal.reason );
		if ( outstanding + cleanups >= MAX_CACHE_OPERATIONS ) {
			return Promise.reject( Error( "Cache busy" ) );
		}
		return new Promise<T>( ( resolve, reject ) => {
			let waiting = true;
			const timer = setTimeout( () => disabled.abort(), CACHE_OPERATION_MS );
			const clearWaiter = () => {
				clearTimeout( timer );
				disabled.signal.removeEventListener( "abort", abort );
				signal?.removeEventListener( "abort", cancel );
			};
			const cancel = () => {
				waiting = false;
				signal?.removeEventListener( "abort", cancel );
				reject( signal!.reason );
			};
			const abort = () => {
				waiting = false;
				clearWaiter();
				reject( disabled.signal.reason );
			};
			disabled.signal.addEventListener( "abort", abort, { once: true } );
			signal?.addEventListener( "abort", cancel, { once: true } );
			outstanding++;
			let work: Promise<T>;
			try {
				work = operation();
			} catch ( error ) {
				work = Promise.reject( error );
			}
			work.then( value => {
				outstanding--;
				clearWaiter();
				if ( waiting ) resolve( value );
				else abandoned?.( value );
			}, error => {
				outstanding--;
				clearWaiter();
				reject( error );
			} );
		} );
	}
	/*
	================
	trackCleanup

	Retain cancellation admission until native settlement, even after a read fails.
	================
	*/
	function trackCleanup( completion: Promise<void> ) {
		cleanups++;
		const timer = setTimeout( () => disabled.abort(), CACHE_OPERATION_MS );
		void completion.catch( () => {} ).finally( () => {
			cleanups--;
			clearTimeout( timer );
		} );
	}
	/*
	================
	release

	Late matches still own a body after timeout disables new lookups. Cleanup
	keeps finite admission and never delays the foreground caller.
	================
	*/
	function release( response: Response | undefined ) {
		if ( !response?.body || cleanups >= MAX_CACHE_OPERATIONS ) return;
		trackCleanup( response.body.cancel() );
	}

	/*
	================
	open
	================
	*/
	function open( signal?: AbortSignal ) {
		if ( disabled.signal.aborted ) return Promise.resolve( null );
		opened ??= storage( () =>
			typeof caches === "undefined" ?
				Promise.resolve( null ) :
				caches.open( "sro-next-verified-v1" )
		).catch( () => {
			errors++;
			return null;
		} );
		// Each caller owns cancellation even while sharing the initial open.
		return storage( () => opened!, signal );
	}

	/*
	================
	key
	================
	*/
	function key( origin: string, digest: string ) {
		return origin + "/assets/.verified/" + digest;
	}
	/*
	================
	touch
	================
	*/
	function touch( url: string ) {
		if ( inventory?.has( url ) ) {
			const size = inventory.get( url )!;
			inventory.delete( url );
			inventory.set( url, size );
		} else {
			touched.delete( url );
			touched.add( url );
			if ( touched.size > 65536 ) touched.delete( touched.values().next().value! );
		}
	}
	/*
	================
	remove

	Serialize invalidation before publication so a late delete cannot erase its
	verified replacement. Foreground callers do not wait for this queue.
	================
	*/
	function remove( origin: string, digest: string ) {
		const url = key( origin, digest );
		if ( disabled.signal.aborted ) return Promise.resolve();
		if ( removals.has( url ) ) return removals.get( url )!;
		if ( removals.size >= MAX_CACHE_OPERATIONS ) return Promise.resolve();
		const operation = tail.then( async () => {
			try {
				const cache = await open();
				if ( cache ) await storage( () => cache.delete( url ) );
				if ( inventory?.has( url ) ) {
					total -= inventory.get( url )!;
					inventory.delete( url );
				}
				touched.delete( url );
			} catch {
				errors++;
			}
		} ).finally( () => removals.delete( url ) );
		removals.set( url, operation );
		tail = operation;
		return operation;
	}
	/*
	================
	publish
	================
	*/
	async function publish( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
		const cache = await open();
		if ( !cache ) return;
		try {
			if ( performance.now() - estimatedAt >= 60000 ) {
				estimatedAt = performance.now();
				try {
					const estimate = await storage( () => navigator.storage.estimate() );
					budget = budgetOf( estimate.quota );
				} catch {}
			}
			if ( bytes.length > budget ) return;
			if ( !inventory ) {
				inventory = new Map();
				total = 0;
				for ( const request of await storage( () => cache.keys() ) ) {
					const response = await storage( () => cache.match( request ), undefined, release ),
						size = Number( response?.headers.get( "content-length" ) ) || 0;
					release( response );
					total += size;
					inventory.set( request.url, size );
				}
				const recent = [ ...touched ];
				touched.clear();
				for ( const url of recent ) if ( inventory.has( url ) ) touch( url );
			}
			const target = key( origin, digest );
			if ( inventory.has( target ) ) {
				touch( target );
				return;
			}
			/*
			================
			evict
			================
			*/
			async function evict() {
				// Least recently used first, skipping the startup entries.
				let victim: [string, number] | undefined;
				for ( const entry of inventory!.entries() ) {
					if ( !startup.has( entry[0] ) ) {
						victim = entry;
						break;
					}
				}
				if ( !victim ) return false;
				const [url, size] = victim;
				await storage( () => cache!.delete( url ) );
				total -= size;
				inventory!.delete( url );
				evictions++;
				return true;
			}
			while ( total + bytes.length > budget && await evict() ) {}
			// Only startup entries are left: the write is optional, the budget is not.
			if ( total + bytes.length > budget ) {
				skipped++;
				return;
			}
			const put = () =>
				cache.put(
					target,
					new Response( bytes, {
						headers: {
							"content-length": String( bytes.length ),
							"content-type": "application/octet-stream"
						}
					} )
				);
			try {
				await storage( put );
			} catch ( error ) {
				if ( !(error instanceof DOMException) || error.name !== "QuotaExceededError" ) throw error;
				const count = Math.max( 1, Math.ceil( inventory.size / 4 ) );
				for ( let i = 0; i < count; i++ ) if ( !await evict() ) break;
				await storage( put );
			}
			inventory.set( target, bytes.length );
			total += bytes.length;
			writes++;
		} catch {
			errors++;
			inventory = null;
		}
	}
	/*
	================
	write
	================
	*/
	function write( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
		const operation = tail.then( () => publish( origin, digest, bytes ) );
		tail = operation.catch( () => {
			errors++;
		} );
		return tail;
	}
	return {
		// Whether verified bytes are stored, without reading them. A storage
		// failure reads as absent: the caller simply fetches again.
		/*
		================
		has
		================
		*/
		async has( origin: string, digest: string, signal?: AbortSignal ) {
			try {
				signal?.throwIfAborted();
				const cache = await open( signal );
				const response = cache ?
					await storage( () => cache.match( key( origin, digest ) ), signal, release ) :
					undefined;
				if ( !response ) return false;
				release( response );
				touch( key( origin, digest ) );
				return true;
			} catch {
				errors++;
				signal?.throwIfAborted();
				return false;
			}
		},
		/*
		================
		read
		================
		*/
		async read( origin: string, digest: string, length: number, signal?: AbortSignal ) {
			let unclaimed: Response | undefined;
			try {
				signal?.throwIfAborted();
				const url = key( origin, digest );
				if ( removals.has( url ) ) return null;
				const cache = await open( signal );
				const response = cache ? await storage( () => cache.match( url ), signal, release ) : undefined;
				unclaimed = response;
				if ( !response ) {
					if ( inventory?.has( url ) ) {
						total -= inventory.get( url )!;
						inventory.delete( url );
					}
					touched.delete( url );
					misses++;
					return null;
				}
				if ( Number( response.headers.get( "content-length" ) ) !== length || !response.body ) {
					void remove( origin, digest );
					misses++;
					return null;
				}
				// A backend read rejection is not evidence that the stored entry is
				// corrupt. Only observed metadata or completed bytes justify removal.
				const bytes = await storage(
					() => {
						// Transfer body ownership only after storage admits the read.
						unclaimed = undefined;
						return readBytes( response.body!, length, { signal: disabled.signal, onCancel: trackCleanup } )
							.catch( error => {
								if (
									isResponseByteLimitError( error ) && !signal?.aborted && !disabled.signal.aborted
								) {
									void remove( origin, digest );
								}
								throw error;
							} );
					},
					signal
				);
				signal?.throwIfAborted();
				if ( bytes.length !== length ) {
					void remove( origin, digest );
					throw Error( "Incomplete persistent asset" );
				}
				hits++;
				touch( url );
				return bytes;
			} catch {
				errors++;
				signal?.throwIfAborted();
				return null;
			} finally {
				release( unclaimed );
			}
		},
		remove,
		write,
		// Copy before the caller transfers/detaches its buffer. Never wait for disk on
		// the admission path; excess demand skips optional persistence, not rendering.
		/*
		================
		enqueue
		================
		*/
		enqueue( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
			const id = key( origin, digest );
			if ( disabled.signal.aborted || pending.has( id ) ) return;
			if ( pending.size >= 64 || queuedBytes + bytes.length > (32 << 20) ) {
				skipped++;
				return;
			}
			const owned = bytes.slice();
			queuedBytes += owned.length;
			pending.add( id );
			void write( origin, digest, owned ).finally( () => {
				queuedBytes -= owned.length;
				pending.delete( id );
			} );
		},
		// The admitted manifest's startup packs and members: the entries eviction
		// keeps. Replacing the set releases whatever only an older release named.
		/*
		================
		setStartup
		================
		*/
		setStartup( origin: string, digests: Iterable<string> ) {
			startup = new Set( Array.from( digests, digest => key( origin, digest ) ) );
		},
		flush: () => tail,
		stats: () => ({
			hits,
			misses,
			writes,
			errors,
			evictions,
			queuedBytes,
			skipped,
			budget,
			pinned: inventory ? [ ...inventory.keys() ].filter( url => startup.has( url ) ).length : 0
		})
	};
}
