/*
===========================================================================

shared-blocks.ts - one pack block download for every window of the origin

Game windows of one site share the persistent store but not their
in-flight reads, so before #4268 five windows teleporting together each
downloaded the same 1 MiB block. Each block has a cross-window Web Lock:
a window that holds it fetches, and before it lets go stores the block's
other members for a window queued behind it, which then reads them from
the store.

A lone window pays nothing for this (#4324): it holds each block once
for all the members it wants from it, takes a free lock without waiting,
and stores only what it demanded. Queuing behind its own lock, reading
the store first and storing every neighbour doubled a world entry.

packs.ts owns the bytes (fetch, verify, store); this module owns only the
lock and the hold.

===========================================================================
*/
import type { PackEntry } from "./internal/pack-contract";

type Bytes = Uint8Array<ArrayBuffer>;

/*
================
SharedBlockReader

What the pack owner does for one block: fetch and verify a member, read a
member's verified bytes from the store (or null), and store the block's
other members given one served member.
================
*/
export type SharedBlockReader = {
	fetched: ( entry: PackEntry ) => Promise<Bytes>;
	stored: ( entry: PackEntry ) => Promise<Bytes | null>;
	neighbours: ( entry: PackEntry ) => Promise<void>;
};

type BlockWant = { entry: PackEntry; resolve: ( bytes: Bytes ) => void; reject: ( error: unknown ) => void; };

// This window's hold of one block's lock: the members it still wants.
type BlockHold = {
	reader: SharedBlockReader;
	queued: BlockWant[];
	served: PackEntry[];
	active: number;
	// Null until the lock is granted; then whether this window had to wait
	// behind another one for it.
	waited: boolean | null;
	// Settles when the last member in flight finishes (serve).
	idle: () => void;
};

/*
================
sharedLocks

The origin's Web Locks, shared by every window and worker of the site,
or null where the browser has none (tests, old engines).
================
*/
export function sharedLocks(): LockManager | null {
	return typeof navigator !== "undefined" && navigator.locks ? navigator.locks : null;
}

/*
================
otherWindowWaits

True when a request for the lock is queued, which while this window holds
it can only be another window's (each block is held once per window).
False where the browser cannot say.
================
*/
async function otherWindowWaits( locks: LockManager, name: string ): Promise<boolean> {
	try {
		return (await locks.query()).pending?.some( request => request.name === name ) ?? false;
	} catch {
		return false;
	}
}

/*
================
serve

One member of a held block: from the store when this window waited behind
another (that window stored the block before letting go), else fetched.
================
*/
async function serve( hold: BlockHold, want: BlockWant ) {
	// Counted before the first await, so the hold sees it at once.
	hold.active++;
	try {
		const saved = hold.waited ? await hold.reader.stored( want.entry ) : null;
		want.resolve( saved ?? await hold.reader.fetched( want.entry ) );
		hold.served.push( want.entry );
	} catch ( error ) {
		want.reject( error );
	} finally {
		if ( --hold.active === 0 ) hold.idle();
	}
}

/*
================
createSharedBlocks

One per window. signal ends the window's lock waits.
================
*/
export function createSharedBlocks( signal: AbortSignal ) {
	const holds = new Map<string, BlockHold>();
	/*
	================
	held

	The lock is this window's: serve every member wanted until none is in
	flight. The hold stays registered until the lock is let go, so a member
	asked for meanwhile, even while the neighbours are stored, is served
	under it, never queued behind this window's own lock.
	================
	*/
	async function held( locks: LockManager, name: string, hold: BlockHold, waited: boolean ) {
		hold.waited = waited;
		do {
			const idle = new Promise<void>( settle => {
				hold.idle = settle;
			} );
			for ( const queued of hold.queued.splice( 0 ) ) void serve( hold, queued );
			if ( hold.active ) await idle;
			// Another window queued for this block reads its member from the
			// store, so the block's other members are stored before the lock
			// goes. A lone window stores only what it demanded.
			const first = hold.served[0];
			if ( first && await otherWindowWaits( locks, name ) ) await hold.reader.neighbours( first );
		} while ( hold.active || hold.queued.length );
		holds.delete( name );
	}
	/*
	================
	abandon

	The lock request failed (the window is disposed): every member still
	queued on the hold fails with it.
	================
	*/
	function abandon( name: string, hold: BlockHold, error: unknown ) {
		holds.delete( name );
		for ( const queued of hold.queued.splice( 0 ) ) queued.reject( error );
	}
	return {
		/*
		================
		read

		A member of the block the lock name stands for. A member wanted from
		a block this window holds or awaits joins that hold.
		================
		*/
		read( locks: LockManager, name: string, entry: PackEntry, reader: SharedBlockReader ): Promise<Bytes> {
			return new Promise<Bytes>( ( resolve, reject ) => {
				const want = { entry, resolve, reject };
				const joined = holds.get( name );
				if ( joined ) {
					if ( joined.waited === null ) joined.queued.push( want );
					else void serve( joined, want );
					return;
				}
				const hold: BlockHold = {
					reader,
					queued: [ want ],
					served: [],
					active: 0,
					waited: null,
					idle: () => {}
				};
				holds.set( name, hold );
				// Taken at once when free; only a window that waits behind
				// another one reads the store first.
				locks.request( name, { ifAvailable: true }, lock =>
					lock ?
						held( locks, name, hold, false ) :
						locks.request( name, { signal }, () => held( locks, name, hold, true ) ) ).catch( error =>
						abandon( name, hold, error ) );
			} );
		}
	};
}
