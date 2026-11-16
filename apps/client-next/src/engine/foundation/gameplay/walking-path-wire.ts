/*
===========================================================================

walking-path-wire.ts - a walking path crosses the worker port as its change

A moving character's state carries its walking path (motion.ts): the
connected ground samples behind it and the admitted ones just ahead, about
250 poses. The simulation republishes that state on every step, and one
batch is one postMessage: its structured clone copied every pose of every
moving character, every time. With 56 monsters around the player a batch
was about 700 KB, 98% of it walking paths, 35 to 49 times a second; reading
them took 124 to 190 ms of every main-thread second and left the copies
behind as garbage (bench bundle, 2026-11-14).

The path is a window that slides. Between two publications of one character
its head loses a few poses and its tail gains a few; every other pose is one
already sent (99.4% of them in that scene). So for a path that continues the
one last sent for its character, the worker's port sends the change alone:
how many poses left the head, how many stay, and the new tail. The host
rebuilds the chain from the one it holds: the same poses in the same order.

Both ends remember the last path of each character and read the same events
in the same order: the world journal delivers every batch once and in
sequence (host.ts refuses anything else). They forget a character on its
despawn or on a state without a path, and every character on a reset, so the
memory is bounded by the characters that walk. A change whose base the host
does not hold is a broken stream, and restoring it throws.

The change lives only on the port. The simulation, and every reader behind
the host, see whole paths as before.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { WorldBatch, WorldEvent } from "@/engine/contracts/world";

// The share of a path's poses its base must supply for the change to be
// sent. Below it (a replaced or re-anchored chain) the path goes whole.
const MIN_KEPT_SHARE = 0.5;
// A pose can repeat inside a chain, so the path's head can match its base
// in more than one place. This many matches are tried; the longest run wins.
const MAX_BASE_STARTS = 4;

/*
================
WalkingPathChange

One walking path of a batch, as its change from the path last sent for the
same character: the base loses `drop` poses at its head, its next `keep`
poses stay, and `tail` follows them.
================
*/
export interface WalkingPathChange {
	/** Index of the batch event (a state or a spawn) this path belongs to. */
	readonly event: number;
	readonly drop: number;
	readonly keep: number;
	readonly tail: readonly Pose[];
}

/*
================
WalkingPathMemory

The last walking path of each character, by gid, as one end of the port
holds it.
================
*/
export type WalkingPathMemory = Map<number, readonly Pose[]>;

/*
================
createWalkingPathMemory
================
*/
export function createWalkingPathMemory(): WalkingPathMemory {
	return new Map();
}

/*
================
samePose

Equal by value, negative zero apart from zero: the rebuilt chain must be
the one a whole clone would have delivered.
================
*/
function samePose( a: Pose, b: Pose ): boolean {
	return a === b ||
		Object.is( a.regionId, b.regionId ) && Object.is( a.x, b.x ) && Object.is( a.y, b.y ) &&
			Object.is( a.z, b.z ) && Object.is( a.angle, b.angle );
}

/*
================
pathChange

The change that turns base into path, or null when base supplies too few of
its poses.
================
*/
function pathChange( event: number, base: readonly Pose[], path: readonly Pose[] ): WalkingPathChange | null {
	if ( !path.length ) return null;
	const head = path[0]!;
	let drop = -1, keep = 0;
	for ( let start = 0, starts = 0; start < base.length && starts < MAX_BASE_STARTS; start++ ) {
		if ( !samePose( base[start]!, head ) ) continue;
		starts++;
		const limit = Math.min( base.length - start, path.length );
		let run = 1;
		while ( run < limit && samePose( base[start + run]!, path[run]! ) ) run++;
		if ( run > keep ) {
			drop = start;
			keep = run;
		}
		if ( run === limit ) break;
	}
	if ( drop < 0 || keep < path.length * MIN_KEPT_SHARE ) return null;
	return { event, drop, keep, tail: path.slice( keep ) };
}

/*
================
packWalkingPaths

The worker's side. Returns the batch to post and, when a path of it
continues its character's last one, those paths' changes: their events are
copies without the path. The given batch and its events are not written.
================
*/
export function packWalkingPaths(
	batch: WorldBatch,
	memory: WalkingPathMemory
): { readonly batch: WorldBatch; readonly walkingPaths?: readonly WalkingPathChange[]; } {
	let events: WorldEvent[] | null = null;
	const changes: WalkingPathChange[] = [];
	for ( let index = 0; index < batch.events.length; index++ ) {
		const event = batch.events[index]!;
		if ( event.kind === "reset" ) memory.clear();
		else if ( event.kind === "despawn" ) memory.delete( event.gid );
		if ( event.kind !== "state" && event.kind !== "spawn" ) continue;
		const gid = event.entity.gid, movement = event.entity.movementPath, path = movement?.walkingPath;
		if ( !movement || !path ) {
			memory.delete( gid );
			continue;
		}
		const base = memory.get( gid );
		memory.set( gid, path );
		const change = base ? pathChange( index, base, path ) : null;
		if ( !change ) continue;
		changes.push( change );
		events ??= batch.events.slice();
		// The key stays, so the restored movement lists its fields in the
		// order the whole one does.
		events[index] = {
			...event,
			entity: { ...event.entity, movementPath: { ...movement, walkingPath: undefined } }
		};
	}
	return events ? { batch: { ...batch, events }, walkingPaths: changes } : { batch };
}

/*
================
restoreWalkingPaths

The host's side. Writes every changed path back into the batch it was just
handed (a structured clone nobody else holds yet) and remembers each
character's path for the next batch. A path that did not change is the very
array the host already holds.
================
*/
export function restoreWalkingPaths(
	batch: WorldBatch,
	changes: readonly WalkingPathChange[] | undefined,
	memory: WalkingPathMemory
): void {
	let next = 0;
	for ( let index = 0; index < batch.events.length; index++ ) {
		const event = batch.events[index]!;
		const change = changes && changes[next]?.event === index ? changes[next++]! : undefined;
		if ( event.kind === "reset" ) memory.clear();
		else if ( event.kind === "despawn" ) memory.delete( event.gid );
		if ( event.kind !== "state" && event.kind !== "spawn" ) {
			if ( change ) throw Error( "Walking path change on an event without a character" );
			continue;
		}
		const gid = event.entity.gid, movement = event.entity.movementPath;
		if ( !change ) {
			if ( movement?.walkingPath ) memory.set( gid, movement.walkingPath );
			else memory.delete( gid );
			continue;
		}
		const base = memory.get( gid );
		if ( !movement || !base || change.drop + change.keep > base.length ) {
			throw Error( "Walking path change without its base" );
		}
		let path = base;
		if ( change.drop || change.keep !== base.length || change.tail.length ) {
			const rebuilt = base.slice( change.drop, change.drop + change.keep );
			for ( const pose of change.tail ) rebuilt.push( pose );
			path = rebuilt;
		}
		(movement as { walkingPath?: readonly Pose[]; }).walkingPath = path;
		memory.set( gid, path );
	}
	if ( changes && next !== changes.length ) throw Error( "Walking path change without its event" );
}
