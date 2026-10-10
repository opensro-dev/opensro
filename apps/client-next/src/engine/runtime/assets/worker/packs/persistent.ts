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
// The inventory scan's keys() walks every stored entry, thousands for a player
// who has explored: background bookkeeping, so it may take this long before
// its timeout suspends the store and foreground reads start missing.
const CACHE_SCAN_MS = 30000;
// Match the bounded publication backlog; waiting callers own no native work.
const MAX_CACHE_WAITERS = 64;

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
	// Operations past their deadline that have not settled yet. While any is
	// open the store takes no new work: a stuck backend stays suspended, a
	// slow one resumes when its late operation settles. A timeout fails only
	// its own caller; later reads may still use the verified disk entries.
	let stalled = 0;
	let outstanding = 0, cleanups = 0, openingWaiters = 0;
	const queue: { start: () => void; fail: ( error: Error ) => void; }[] = [];
	const removals = new Map<string, Promise<void>>();
	/*
	================
	drain

	FIFO admission counts native work, including abandoned response cleanup.
	Suspension rejects waiting callers without starting their operations.
	================
	*/
	function drain() {
		if ( stalled > 0 ) {
			for ( const entry of queue.splice( 0 ) ) entry.fail( Error( "Cache suspended" ) );
			return;
		}
		while ( queue.length && outstanding + cleanups < MAX_CACHE_OPERATIONS ) queue.shift()!.start();
	}
	/*
	================
	storage

	A bounded FIFO gives healthy bursts a turn. Queue wait and native execution
	each have a deadline. Cancellation removes unstarted work, but native work
	keeps its slot until settlement. A timeout suspends admission until that
	work (including body cancellation) settles; it never disables the owner.

	dispose runs at settlement while the operation still holds its slot, before
	the queue drains: a result handed to cleanup there (a presence check's
	body) is counted in the same step its slot frees, so queued work can never
	run beside it past MAX_CACHE_OPERATIONS, and cleanup admission is never
	already full when it is handed over.
	================
	*/
	function storage<T>(
		operation: ( deadline: AbortSignal ) => Promise<T>,
		signal?: AbortSignal,
		abandoned?: ( value: T ) => void,
		dispose?: ( value: T ) => void,
		deadlineMs = CACHE_OPERATION_MS
	): Promise<T> {
		signal?.throwIfAborted();
		if ( stalled > 0 ) return Promise.reject( Error( "Cache suspended" ) );
		if ( queue.length >= MAX_CACHE_WAITERS ) return Promise.reject( Error( "Cache queue full" ) );
		return new Promise<T>( ( resolve, reject ) => {
			let waiting = true, overdue = false, started = false;
			const deadline = new AbortController();
			/*
			================
			fail
			================
			*/
			const fail = ( error: unknown ) => {
				waiting = false;
				signal?.removeEventListener( "abort", cancel );
				if ( !started ) {
					clearTimeout( timer );
					const index = queue.indexOf( entry );
					if ( index >= 0 ) queue.splice( index, 1 );
				}
				reject( error );
			};
			/*
			================
			cancel
			================
			*/
			const cancel = () => fail( signal!.reason );
			/*
			================
			expire
			================
			*/
			const expire = () => {
				if ( started ) {
					overdue = true;
					stalled++;
				}
				deadline.abort( Error( "Cache operation timed out" ) );
				fail( deadline.signal.reason );
				drain();
			};
			let timer = setTimeout( expire, deadlineMs );
			/*
			================
			start
			================
			*/
			const start = () => {
				started = true;
				clearTimeout( timer );
				timer = setTimeout( expire, deadlineMs );
				outstanding++;
				let work: Promise<T>;
				try {
					work = operation( deadline.signal );
				} catch ( error ) {
					work = Promise.reject( error );
				}
				/*
				================
				settle
				================
				*/
				const settle = () => {
					outstanding--;
					clearTimeout( timer );
					signal?.removeEventListener( "abort", cancel );
					if ( overdue ) stalled--;
				};
				work.then( value => {
					dispose?.( value );
					settle();
					if ( waiting ) resolve( value );
					else abandoned?.( value );
					drain();
				}, error => {
					settle();
					fail( error );
					drain();
				} );
			};
			const entry = { start, fail };
			signal?.addEventListener( "abort", cancel, { once: true } );
			queue.push( entry );
			drain();
		} );
	}
	/*
	================
	trackCleanup

	Retain cancellation admission until native settlement, even after a read fails.
	================
	*/
	function trackCleanup( completion: Promise<void>, overdue = false ) {
		cleanups++;
		if ( overdue ) stalled++;
		const timer = setTimeout( () => {
			if ( !overdue ) {
				overdue = true;
				stalled++;
			}
			drain();
		}, CACHE_OPERATION_MS );
		void completion.catch( () => {} ).finally( () => {
			cleanups--;
			clearTimeout( timer );
			if ( overdue ) stalled--;
			drain();
		} );
	}
	/*
	================
	release

	Late matches still own a body after timeout suspends new lookups. Cleanup
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
		if ( stalled > 0 ) return Promise.resolve( null );
		// A failed or late open is retried by the next caller, never kept for
		// the session.
		opened ??= storage( () =>
			typeof caches === "undefined" ?
				Promise.resolve( null ) :
				caches.open( "sro-next-verified-v1" )
		).catch( () => {
			errors++;
			opened = null;
			return null;
		} );
		// Waiting on the shared promise is not another native storage operation.
		// Retain the waiter count until settlement, even if its caller cancels.
		signal?.throwIfAborted();
		if ( openingWaiters >= MAX_CACHE_WAITERS ) return Promise.resolve( null );
		openingWaiters++;
		const opening = opened;
		return new Promise<Cache | null>( ( resolve, reject ) => {
			const cancel = () => reject( signal!.reason );
			signal?.addEventListener( "abort", cancel, { once: true } );
			void opening.then( resolve, reject ).finally( () => {
				openingWaiters--;
				signal?.removeEventListener( "abort", cancel );
			} );
		} );
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
		if ( stalled > 0 ) return Promise.resolve();
		if ( removals.has( url ) ) return removals.get( url )!;
		if ( removals.size >= MAX_CACHE_OPERATIONS ) return Promise.resolve();
		const operation = tail.then( async () => {
			try {
				const cache = await open();
				if ( !cache ) return;
				await storage( () => cache.delete( url ) );
				if ( inventory?.has( url ) ) {
					total -= inventory.get( url )!;
					inventory.delete( url );
				}
				touched.delete( url );
			} catch {
				errors++;
				// A timed-out delete may still commit before admission resumes.
				inventory = null;
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
				const requests = await storage( () => cache.keys(), undefined, undefined, undefined, CACHE_SCAN_MS );
				for ( const request of requests ) {
					// Only the size header is read: dispose cancels the body at
					// settlement, before the slot frees (storage).
					const response = await storage( () => cache.match( request ), undefined, undefined, release ),
						size = Number( response?.headers.get( "content-length" ) ) || 0;
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
		// Whether verified bytes are stored, without reading them: null when the
		// store cannot answer (suspended, saturated, failed). Absent and unknown
		// differ: the background installer must not fetch what it cannot store.
		/*
		================
		has
		================
		*/
		async has( origin: string, digest: string, signal?: AbortSignal ): Promise<boolean | null> {
			try {
				signal?.throwIfAborted();
				const cache = await open( signal );
				if ( !cache ) return null;
				// Presence needs no body: dispose hands it to cleanup at settlement,
				// before the slot frees (storage), whether or not this caller waits.
				const response = await storage(
					() => cache.match( key( origin, digest ) ),
					signal,
					undefined,
					release
				);
				if ( !response ) return false;
				touch( key( origin, digest ) );
				return true;
			} catch {
				errors++;
				signal?.throwIfAborted();
				return null;
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
				if ( !cache ) return null;
				const response = await storage( () => cache.match( url ), signal, release );
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
					deadline => {
						// Transfer body ownership only after storage admits the read.
						unclaimed = undefined;
						return readBytes( response.body!, length, {
							signal: deadline,
							// Carry the expired read into cancellation with no admission gap.
							onCancel: completion => trackCleanup( completion, deadline.aborted )
						} )
							.catch( error => {
								if (
									isResponseByteLimitError( error ) && !signal?.aborted && !deadline.aborted
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
			if ( stalled > 0 || pending.has( id ) ) return;
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
