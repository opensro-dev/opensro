/*
===========================================================================

persistent.ts - the durable store of verified asset bytes

One owner for durable verified bytes, bounded publication and LRU order,
kept in Cache Storage keyed by content digest. Cache failure is an
optional-storage failure, never an asset admission failure.

The store is STORE_SHARDS caches, picked by the digest's first hex digit.
One cache cannot be listed past a size limit: Chrome refuses keys() with
"Operation too large" (kErrorQueryTooLarge) once its answer grows past
what one query may return, tens of thousands of entries. The single cache
of the v1 store reached that on players who explored (2026-11-25: Jangan
downloaded again on every refresh). Its reads still hit, but the inventory
rebuild failed before every write, so nothing new was stored and nothing
was evicted. A shard holds a sixteenth of the entries. A shard that still
cannot be listed is dropped and starts empty rather than freezing the
store.

The v1 cache still holds what its player paid for, and its reads work at
any size, so it is emptied by use instead of downloaded again: a read the
shards miss is answered from it, the bytes are queued for their shard, and
the v1 copy goes once the shard holds them. Nothing is ever added to it and
it is never listed. It is deleted LEGACY_KEEP_MS after its first open by
this store, or as soon as the origin refuses a write for quota, so what
only an older release named does not stay.

The budget is half the origin quota, between MIN_BUDGET_BYTES and
MAX_BUDGET_BYTES: browsers grant an origin 60% of the disk (Chrome,
Safari) or the smaller of 10% and 10 GiB (Firefox best effort), so the
whole game fits on most machines and a player who explores never
re-downloads areas already visited. Startup entries - the packs and
members of the startup groups every session needs before the first frame -
are never evicted; the rest leave least recently used first, and a write
that cannot fit after eviction is skipped, never stored over budget.

A put the origin refuses for quota with nothing left to evict means the
quota itself cannot hold a pack (a private window, a full device): every
later put would be refused too, and a browser may keep each refused body
in memory, so the store stops writing for the session (writesStopped).

Which entries are startup entries comes from the manifest the asset worker
admitted (setStartup), never from what an earlier release stored: a warm
entry the manifest names is protected at once, and one only an older
release named becomes ordinary LRU data.

The inventory (every stored key and its size) is rebuilt once per worker
lifetime, before the first write. Writes wait behind it, and enqueue skips
whatever arrives while they wait, so the rebuild must be quick: the
admitted manifests already give the size of every digest they name
(addSizes), and only entries no admitted manifest names - an older
release's - have their stored size read. Those can never serve this
release, so they are the first evicted.

===========================================================================
*/

import type { AssetStoreFailure, AssetStoreFailureReason, AssetStoreOperation } from "@/engine/contracts/assets";
import { isResponseByteLimitError, readBytes } from "@/engine/foundation/assets/read-bytes";

// The store's caches: STORE_SHARDS of them, named STORE_PREFIX plus the hex
// digit their digests start with, and the older single cache they replace.
const STORE_PREFIX = "sro-next-verified-v2-";
const STORE_SHARDS = 16;
// Entries one shard may hold. Headless Chromium answered keys() for 17,000
// store keys and refused 18,000 ("Operation too large", measured
// 2026-11-25); 12,000 keeps a third in reserve, about 190,000 entries in all.
export const MAX_SHARD_ENTRIES = 12000;
const LEGACY_STORE = "sro-next-verified-v1";
// How long the legacy cache is kept for its entries to move, and the entry in
// it that records when that ends.
export const LEGACY_KEEP_MS = 14 * 24 * 60 * 60 * 1000;
const LEGACY_EXPIRY_KEY = "/assets/.verified/legacy-expiry", LEGACY_EXPIRY_HEADER = "x-sro-expires";
// The deadline storage() gives an operation; its rejection is a slow backend,
// not a store that cannot be listed.
const CACHE_TIMEOUT_MESSAGE = "Cache operation timed out";
// The refusals storage() and drain() raise themselves, classified by
// failureReason.
const CACHE_SUSPENDED_MESSAGE = "Cache suspended";
const CACHE_QUEUE_FULL_MESSAGE = "Cache queue full";
const INCOMPLETE_ASSET_MESSAGE = "Incomplete persistent asset";
// Chrome's refusal of keys() on a cache past its query limit
// (kErrorQueryTooLarge, #4370).
const TOO_LARGE_MESSAGE = "Operation too large";
// DOMException names the browser raises when Cache Storage itself is not
// available to this origin (a private window, blocked storage, a broken
// backend).
const UNAVAILABLE_ERRORS = new Set( [ "SecurityError", "InvalidStateError", "UnknownError", "NotAllowedError" ] );
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
// Bounds on verified bytes waiting for publication (enqueue): copies held in
// memory, so a burst beyond them skips persistence rather than growing.
const MAX_PENDING_WRITES = 64;
const MAX_QUEUED_BYTES = 32 << 20;

// The store's open caches, indexed by shard (shardOf).
type Shards = readonly Cache[];

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
failureReason

The enumerated word for a failed store operation's error. Bug reports
carry no free-form text, so the browser's message is read here and
dropped (AssetStoreFailureReason).
================
*/
export function failureReason( error: unknown ): AssetStoreFailureReason {
	const name = error instanceof Error ? error.name : "";
	const message = error instanceof Error ? error.message : "";
	if ( message === CACHE_TIMEOUT_MESSAGE ) return "timeout";
	if ( message === CACHE_SUSPENDED_MESSAGE ) return "suspended";
	if ( message === CACHE_QUEUE_FULL_MESSAGE ) return "queue-full";
	if ( name === "QuotaExceededError" ) return "quota";
	if ( message.includes( TOO_LARGE_MESSAGE ) ) return "too-large";
	if ( message === INCOMPLETE_ASSET_MESSAGE || isResponseByteLimitError( error ) ) return "corrupt";
	if ( UNAVAILABLE_ERRORS.has( name ) ) return "unavailable";
	return "other";
}

/*
================
createPersistentAssets

budgetOf turns the origin quota into this store's byte budget
(budgetFromQuota; tests inject a small one).
================
*/
export function createPersistentAssets( budgetOf: ( quota: number | undefined ) => number = budgetFromQuota ) {
	let opened: Promise<Shards | null> | null = null, tail: Promise<void> = Promise.resolve();
	// Shards dropped because they could not be listed (rebuild).
	let resets = 0;
	// The legacy cache while it has entries to move, and reads it answered.
	let legacy: Cache | null = null, legacyHits = 0;
	// Inventory entries per shard (shardOf), kept with the inventory.
	let shardEntries: number[] = new Array( STORE_SHARDS ).fill( 0 );
	let inventory: Map<string, number> | null = null, total = 0, budget = MIN_BUDGET_BYTES, estimatedAt = -Infinity;
	// Keys of the current manifest's startup packs and members (setStartup).
	let startup = new Set<string>();
	// Stored size of every key an admitted manifest names (addSizes).
	const sizes = new Map<string, number>();
	let hits = 0, misses = 0, writes = 0, evictions = 0, queuedBytes = 0, skipped = 0;
	// Failed operations by kind and the latest one (noteFailure).
	const failures: Record<AssetStoreOperation, number> = { open: 0, has: 0, read: 0, write: 0, remove: 0 };
	let lastFailure: AssetStoreFailure | null = null;
	// Set once the origin's quota cannot hold a pack (the header above).
	let writesStopped = false;
	// Queued writes by key, each settling when its write has (enqueue).
	const pending = new Map<string, Promise<void>>(), touched = new Set<string>();
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
	noteFailure

	The one place a failed store operation is counted. Callers skip it for
	their own cancellation, which is not a store failure.
	================
	*/
	function noteFailure( operation: AssetStoreOperation, error: unknown ) {
		failures[operation]++;
		lastFailure = { operation, reason: failureReason( error ), atMs: performance.now() };
	}
	/*
	================
	drain

	FIFO admission counts native work, including abandoned response cleanup.
	Suspension rejects waiting callers without starting their operations.
	================
	*/
	function drain() {
		if ( stalled > 0 ) {
			for ( const entry of queue.splice( 0 ) ) entry.fail( Error( CACHE_SUSPENDED_MESSAGE ) );
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
		if ( stalled > 0 ) return Promise.reject( Error( CACHE_SUSPENDED_MESSAGE ) );
		if ( queue.length >= MAX_CACHE_WAITERS ) return Promise.reject( Error( CACHE_QUEUE_FULL_MESSAGE ) );
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
				deadline.abort( Error( CACHE_TIMEOUT_MESSAGE ) );
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
	openLegacy

	The legacy cache, or null when there is none or its time is over (it is
	then deleted in the background). The first open starts that time.
	================
	*/
	async function openLegacy(): Promise<Cache | null> {
		if ( !await caches.has( LEGACY_STORE ) ) return null;
		const cache = await caches.open( LEGACY_STORE );
		const marker = await cache.match( LEGACY_EXPIRY_KEY );
		if ( !marker ) {
			const expires = String( Date.now() + LEGACY_KEEP_MS );
			await cache.put(
				LEGACY_EXPIRY_KEY,
				new Response( null, { headers: { [LEGACY_EXPIRY_HEADER]: expires } } )
			);
			return cache;
		}
		if ( Date.now() < Number( marker.headers.get( LEGACY_EXPIRY_HEADER ) ) ) return cache;
		void caches.delete( LEGACY_STORE ).catch( () => {} );
		return null;
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
		opened ??= storage( async () => {
			if ( typeof caches === "undefined" ) return null;
			// Without its legacy cache the store still works.
			const [shards, older] = await Promise.all( [
				Promise.all(
					Array.from( { length: STORE_SHARDS }, ( _, n ) => caches.open( STORE_PREFIX + n.toString( 16 ) ) )
				),
				openLegacy().catch( () => null )
			] );
			legacy = older;
			return shards;
		} ).catch( error => {
			noteFailure( "open", error );
			opened = null;
			return null;
		} );
		// Waiting on the shared promise is not another native storage operation.
		// Retain the waiter count until settlement, even if its caller cancels.
		signal?.throwIfAborted();
		if ( openingWaiters >= MAX_CACHE_WAITERS ) return Promise.resolve( null );
		openingWaiters++;
		const opening = opened;
		return new Promise<Shards | null>( ( resolve, reject ) => {
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
	shardOf

	The shard index of a stored key: its digest's first hex digit. A key
	that does not end in a hex digest (none does) lands in shard 0.
	================
	*/
	function shardOf( url: string ) {
		const digit = parseInt( url.charAt( url.lastIndexOf( "/" ) + 1 ), 16 );
		return Number.isNaN( digit ) ? 0 : digit % STORE_SHARDS;
	}
	/*
	================
	cacheFor

	The shard that holds a stored key. open() always opens STORE_SHARDS
	caches, so every shardOf index names one.
	================
	*/
	function cacheFor( shards: Shards, url: string ): Cache {
		return shards[shardOf( url )]!;
	}
	/*
	================
	forget

	Drops a key from the inventory and its totals, if it is listed.
	================
	*/
	function forget( url: string ) {
		if ( !inventory?.has( url ) ) return;
		total -= inventory.get( url )!;
		inventory.delete( url );
		shardEntries[shardOf( url )]!--;
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
				const shards = await open();
				if ( !shards ) return;
				await storage( () => cacheFor( shards, url ).delete( url ) );
				forget( url );
				touched.delete( url );
				const older = legacy;
				if ( older ) await storage( () => older.delete( url ) );
			} catch ( error ) {
				noteFailure( "remove", error );
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
	rebuild

	The inventory from Cache Storage, with total set to its size: one
	keys() walk per shard, sizes from the admitted manifests (addSizes). A
	shard whose keys() is refused for anything but the deadline (Chrome's
	"Operation too large") cannot be listed, so it cannot be evicted
	either: it is deleted, the store reopens on the next write and that
	shard starts empty. The write in hand is skipped. Only keys
	no manifest names have their size header read, MAX_CACHE_OPERATIONS at
	a time; one read per entry used to hold every write for minutes on a
	store of tens of thousands. Unnamed keys lead the LRU order, so
	eviction takes them first.
	================
	*/
	async function rebuild( shards: Shards ) {
		const requests: Request[] = [];
		for ( let n = 0; n < shards.length; n++ ) {
			try {
				requests.push(
					...await storage( () => shards[n]!.keys(), undefined, undefined, undefined, CACHE_SCAN_MS )
				);
			} catch ( error ) {
				if ( error instanceof Error && error.message === CACHE_TIMEOUT_MESSAGE ) throw error;
				await storage( () => caches.delete( STORE_PREFIX + n.toString( 16 ) ) );
				resets++;
				opened = null;
				throw error;
			}
		}
		// A key lives in one shard; list it once even if a backend answers
		// every shard from one store.
		const listed = new Set<string>();
		// Classify every key before the first await: the HD index may add
		// sizes during the scan, and a key it names then must not be counted
		// both as read and as named.
		const unnamed: Request[] = [], named: [string, number][] = [];
		for ( const request of requests ) {
			if ( listed.has( request.url ) ) continue;
			listed.add( request.url );
			const size = sizes.get( request.url );
			if ( size === undefined ) unnamed.push( request );
			else named.push( [ request.url, size ] );
		}
		const rebuilt = new Map<string, number>();
		let sum = 0;
		for ( let i = 0; i < unnamed.length; i += MAX_CACHE_OPERATIONS ) {
			const batch = unnamed.slice( i, i + MAX_CACHE_OPERATIONS );
			// Only the size header is read: dispose cancels the body at
			// settlement, before the slot frees (storage).
			const responses = await Promise.all(
				batch.map( request =>
					storage( () => cacheFor( shards, request.url ).match( request ), undefined, undefined, release )
				)
			);
			batch.forEach( ( request, n ) => {
				const size = Number( responses[n]?.headers.get( "content-length" ) ) || 0;
				sum += size;
				rebuilt.set( request.url, size );
			} );
		}
		for ( const [url, size] of named ) {
			sum += size;
			rebuilt.set( url, size );
		}
		// Reads served before the rebuild still count as recent use.
		for ( const url of touched ) {
			const size = rebuilt.get( url );
			if ( size === undefined ) continue;
			rebuilt.delete( url );
			rebuilt.set( url, size );
		}
		touched.clear();
		total = sum;
		shardEntries = new Array( STORE_SHARDS ).fill( 0 );
		for ( const url of rebuilt.keys() ) shardEntries[shardOf( url )]!++;
		return rebuilt;
	}
	/*
	================
	dropLegacyEntry

	Removes a key's legacy copy now that its shard holds it. A failure only
	leaves the copy for the legacy cache's own end.
	================
	*/
	async function dropLegacyEntry( url: string ) {
		const older = legacy;
		if ( !older ) return;
		try {
			await storage( () => older.delete( url ) );
		} catch ( error ) {
			noteFailure( "remove", error );
		}
	}
	/*
	================
	publish
	================
	*/
	async function publish( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
		if ( writesStopped ) {
			skipped++;
			return;
		}
		const opening = await open();
		if ( !opening ) return;
		// evict below is a closure: name the narrowed store for it.
		const shards: Shards = opening;
		try {
			if ( performance.now() - estimatedAt >= 60000 ) {
				estimatedAt = performance.now();
				try {
					const estimate = await storage( () => navigator.storage.estimate() );
					budget = budgetOf( estimate.quota );
				} catch {}
			}
			if ( bytes.length > budget ) return;
			if ( !inventory ) inventory = await rebuild( shards );
			const target = key( origin, digest );
			if ( inventory.has( target ) ) {
				touch( target );
				await dropLegacyEntry( target );
				return;
			}
			/*
			================
			evict
			================
			*/
			async function evict( shard?: number ) {
				// Least recently used first (in one shard when named), skipping
				// the startup entries.
				let victim: [string, number] | undefined;
				for ( const entry of inventory!.entries() ) {
					if ( !startup.has( entry[0] ) && (shard === undefined || shardOf( entry[0] ) === shard) ) {
						victim = entry;
						break;
					}
				}
				if ( !victim ) return false;
				const [url, size] = victim;
				await storage( () => cacheFor( shards, url ).delete( url ) );
				total -= size;
				inventory!.delete( url );
				shardEntries[shardOf( url )]!--;
				evictions++;
				return true;
			}
			while ( total + bytes.length > budget && await evict() ) {}
			// A shard past MAX_SHARD_ENTRIES could no longer be listed.
			const shard = shardOf( target );
			while ( shardEntries[shard]! >= MAX_SHARD_ENTRIES && await evict( shard ) ) {}
			if ( shardEntries[shard]! >= MAX_SHARD_ENTRIES ) {
				skipped++;
				return;
			}
			// Only startup entries are left: the write is optional, the budget is not.
			if ( total + bytes.length > budget ) {
				skipped++;
				return;
			}
			const put = () =>
				cacheFor( shards, target ).put(
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
				if ( legacy ) {
					// The legacy cache goes before any current entry. The browser may
					// free its space late; a later refusal then evicts.
					legacy = null;
					await storage( () => caches.delete( LEGACY_STORE ) );
				} else {
					const count = Math.max( 1, Math.ceil( inventory.size / 4 ) );
					let freed = 0;
					for ( let i = 0; i < count; i++ ) {
						if ( !await evict() ) break;
						freed++;
					}
					if ( !freed ) {
						writesStopped = true;
						skipped++;
						return;
					}
				}
				await storage( put );
			}
			inventory.set( target, bytes.length );
			total += bytes.length;
			shardEntries[shard]!++;
			writes++;
			await dropLegacyEntry( target );
		} catch ( error ) {
			noteFailure( "write", error );
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
		tail = operation.catch( error => {
			noteFailure( "write", error );
		} );
		return tail;
	}
	// Copy before the caller transfers/detaches its buffer. Never wait for disk on
	// the admission path; excess demand skips optional persistence, not rendering.
	/*
	================
	enqueue

	Queues verified bytes for the store. The promise settles (never
	rejects) once the write has been tried or skipped: the admission
	path ignores it, while a pack block shared across windows waits on
	it so the next window finds the bytes stored (packs.ts sharedMember).
	================
	*/
	function enqueue( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ): Promise<void> {
		const id = key( origin, digest );
		const queued = pending.get( id );
		if ( queued ) return queued;
		if (
			stalled > 0 || pending.size >= MAX_PENDING_WRITES ||
			queuedBytes + bytes.length > MAX_QUEUED_BYTES
		) {
			skipped++;
			return Promise.resolve();
		}
		const owned = bytes.slice();
		queuedBytes += owned.length;
		const operation = write( origin, digest, owned ).finally( () => {
			queuedBytes -= owned.length;
			pending.delete( id );
		} );
		pending.set( id, operation );
		return operation;
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
				const shards = await open( signal );
				if ( !shards ) return null;
				const url = key( origin, digest );
				// Presence needs no body: dispose hands it to cleanup at settlement,
				// before the slot frees (storage), whether or not this caller waits.
				const response = await storage(
					() => cacheFor( shards, url ).match( url ),
					signal,
					undefined,
					release
				);
				if ( !response ) return false;
				touch( url );
				return true;
			} catch ( error ) {
				if ( !signal?.aborted ) noteFailure( "has", error );
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
				const shards = await open( signal );
				if ( !shards ) return null;
				let response = await storage( () => cacheFor( shards, url ).match( url ), signal, release );
				// The shards hold no copy: the legacy cache may, and it moves.
				const older = response ? null : legacy;
				if ( older ) response = await storage( () => older.match( url ), signal, release );
				unclaimed = response;
				if ( !response ) {
					forget( url );
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
					throw Error( INCOMPLETE_ASSET_MESSAGE );
				}
				hits++;
				touch( url );
				if ( older ) {
					legacyHits++;
					void enqueue( origin, digest, bytes );
				}
				return bytes;
			} catch ( error ) {
				if ( !signal?.aborted ) noteFailure( "read", error );
				signal?.throwIfAborted();
				return null;
			} finally {
				release( unclaimed );
			}
		},
		remove,
		write,
		enqueue,
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
		/*
		================
		addSizes

		The stored length of each digest an admitted manifest names (a
		pack's bytes, a member's decoded length): what the inventory
		rebuild needs instead of reading each entry. Additive, since the
		HD set's index is admitted after the main one.
		================
		*/
		addSizes( origin: string, digests: Iterable<readonly [string, number]> ) {
			for ( const [digest, length] of digests ) sizes.set( key( origin, digest ), length );
		},
		flush: () => tail,
		stats: () => ({
			hits,
			misses,
			writes,
			errors: failures.open + failures.has + failures.read + failures.write + failures.remove,
			failures: { ...failures },
			lastFailure,
			evictions,
			queuedBytes,
			skipped,
			writesStopped,
			resets,
			legacyHits,
			budget,
			pinned: inventory ? [ ...inventory.keys() ].filter( url => startup.has( url ) ).length : 0
		})
	};
}
