/*
===========================================================================

walking-path-wire.test.mjs - a walking path sent as its change arrives whole

The worker's port sends a walking path that continues its character's last
one as a change; the host rebuilds it. Whatever the journal publishes, the
batch the host retains must equal the one a whole structured clone would
have delivered, and a sliding window must cost its new poses, not the chain.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { createWalkingPathMemory, packWalkingPaths, restoreWalkingPaths } = await import(
	sourceFileUrl( path.join( root, "src/engine/foundation/gameplay/walking-path-wire.ts" ) ).href
);

/*
================
pose
================
*/
function pose( step, regionId = 25511 ) {
	return Object.freeze( {
		regionId,
		x: 1000 + step * 0.75,
		y: 64 + (step % 7) * 0.125,
		z: 1800 - step * 0.5,
		angle: 1.5
	} );
}

/*
================
state

One published entity state, as entities.ts publishes it: frozen, with the
walking path inside its movement path when it walks.
================
*/
/** @returns {any} */
function state( gid, walkingPath, kind = "state" ) {
	/** @type {any} */
	const entity = { gid, kind: "monster", regionId: 25511, x: 1, y: 2, z: 3, heading: 0, moving: !!walkingPath };
	if ( walkingPath ) {
		entity.movementPath = { from: walkingPath[0], to: walkingPath.at( -1 ), walkingPath, durationMs: 250 };
	}
	return Object.freeze( { kind, entity: Object.freeze( entity ) } );
}

/*
================
createPort

The two ends of the worker port. deliver() sends one batch the way entry.ts
and host.ts do, with structuredClone standing for postMessage, and returns
what the host retains, what a whole clone would have delivered, and the
poses that crossed.
================
*/
function createPort() {
	const posted = createWalkingPathMemory(), received = createWalkingPathMemory();
	let sequence = 0;
	return {
		received,
		deliver( events ) {
			const batch = { sequence: ++sequence, events, spanMs: 16 };
			const message = structuredClone( { kind: "world", ...packWalkingPaths( batch, posted ) } );
			let crossed = 0;
			for ( const event of message.batch.events ) crossed += event.entity?.movementPath?.walkingPath?.length ?? 0;
			for ( const change of message.walkingPaths ?? [] ) crossed += change.tail.length;
			restoreWalkingPaths( message.batch, message.walkingPaths, received );
			return { retained: message.batch, whole: structuredClone( batch ), crossed };
		}
	};
}

test("a sliding window arrives whole and costs only its new poses", () => {
	const port = createPort();
	let window = Array.from( { length: 240 }, ( _, step ) => pose( step ) );
	const first = port.deliver( [ state( 7, window ) ] );
	assert.deepStrictEqual( first.retained, first.whole );
	assert.equal( first.crossed, 240, "a path without a base goes whole" );
	for ( let step = 240; step < 300; step++ ) {
		// The head loses 0 to 2 poses and the tail gains one or two, as measured.
		const added = step % 5 === 0 ? [ pose( step ), pose( step + 0.5 ) ] : [ pose( step ) ];
		window = [ ...window.slice( step % 3 ), ...added ];
		const result = port.deliver( [ state( 7, window ) ] );
		assert.deepStrictEqual( result.retained, result.whole );
		assert.equal( result.crossed, added.length, "only the new tail crosses" );
	}
});

test("the field order of a restored movement path is the whole one's", () => {
	const port = createPort();
	const window = Array.from( { length: 8 }, ( _, step ) => pose( step ) );
	port.deliver( [ state( 3, window ) ] );
	const result = port.deliver( [ state( 3, [ ...window.slice( 1 ), pose( 8 ) ] ) ] );
	assert.deepStrictEqual(
		Object.keys( result.retained.events[0].entity.movementPath ),
		Object.keys( result.whole.events[0].entity.movementPath )
	);
});

test("an unchanged path is the array the host already holds", () => {
	const port = createPort();
	const window = Array.from( { length: 16 }, ( _, step ) => pose( step ) );
	const first = port.deliver( [ state( 5, window ) ] );
	// Two states of one character in a batch share the path (an HP change
	// beside its pose), and the next batch republishes it.
	const second = port.deliver( [ state( 5, window ), state( 5, window ) ] );
	assert.deepStrictEqual( second.retained, second.whole );
	assert.equal( second.crossed, 0 );
	const held = first.retained.events[0].entity.movementPath.walkingPath;
	assert.equal( second.retained.events[0].entity.movementPath.walkingPath, held );
	assert.equal( second.retained.events[1].entity.movementPath.walkingPath, held );
});

test("a replaced path, a stop, a despawn and a reset all arrive as published", () => {
	const port = createPort();
	const walk = Array.from( { length: 32 }, ( _, step ) => pose( step ) );
	const elsewhere = Array.from( { length: 32 }, ( _, step ) => pose( step + 500, 25512 ) );
	const batches = [
		[ state( 1, walk, "spawn" ), state( 2, walk, "spawn" ) ],
		// A path that shares nothing with its base goes whole.
		[ state( 1, elsewhere ) ],
		// A state without a path forgets it; the next walk starts whole.
		[ state( 1, undefined ) ],
		[ state( 1, [ ...elsewhere.slice( 1 ), pose( 532, 25512 ) ] ) ],
		// Order inside a batch: the despawn, then the same gid spawned again.
		[ { kind: "despawn", gid: 2 }, state( 2, [ ...walk.slice( 1 ), pose( 32 ) ], "spawn" ) ],
		[ state( 2, [ ...walk.slice( 2 ), pose( 32 ), pose( 33 ) ] ) ],
		[ { kind: "reset", epoch: 2, resync: true }, state( 1, walk, "spawn" ), state( 2, walk, "spawn" ) ],
		[ state( 1, [ ...walk.slice( 1 ), pose( 32 ) ] ), { kind: "despawn", gid: 2 } ]
	];
	const crossed = [];
	for ( const events of batches ) {
		const result = port.deliver( events );
		assert.deepStrictEqual( result.retained, result.whole );
		crossed.push( result.crossed );
	}
	// Each first path counts whole (two characters in the spawn batches).
	assert.deepStrictEqual( crossed, [ 64, 32, 0, 32, 32, 1, 64, 1 ] );
	assert.deepStrictEqual( [ ...port.received.keys() ], [ 1 ], "a despawned character's path is forgotten" );
});

test("repeated poses and negative zero keep their place", () => {
	const port = createPort();
	const still = pose( 4 );
	const window = [ pose( 1 ), still, still, still, pose( 5 ), pose( 6 ), pose( 7 ), pose( 8 ) ];
	port.deliver( [ state( 9, window ) ] );
	// The head repeats inside the chain: the run that keeps the most wins.
	const slid = port.deliver( [
		state( 9, [ still, still, pose( 5 ), pose( 6 ), pose( 7 ), pose( 8 ), pose( 9 ) ] )
	] );
	assert.deepStrictEqual( slid.retained, slid.whole );
	assert.equal( slid.crossed, 1 );
	// Zero and negative zero are different poses to a clone.
	const zero = Object.freeze( { ...pose( 20 ), y: 0 } ), negative = Object.freeze( { ...pose( 20 ), y: -0 } );
	const tail = [ pose( 21 ), pose( 22 ), pose( 23 ) ];
	port.deliver( [ state( 10, [ zero, ...tail ] ) ] );
	const signed = port.deliver( [ state( 10, [ negative, ...tail ] ) ] );
	assert.ok( Object.is( signed.retained.events[0].entity.movementPath.walkingPath[0].y, -0 ) );
	assert.deepStrictEqual( signed.retained, signed.whole );
});

test("random journals of many characters arrive as published", () => {
	// A fixed generator: the same journal on every run.
	let seed = 0x2f6e2b1;
	const random = bound => {
		seed = Math.imul( seed, 1664525 ) + 1013904223 >>> 0;
		return seed % bound;
	};
	const port = createPort(), paths = new Map();
	let nextStep = 0, whole = 0, crossed = 0;
	for ( let batch = 0; batch < 400; batch++ ) {
		/** @type {any[]} */
		const events = [];
		if ( random( 97 ) === 0 ) {
			events.push( { kind: "reset", epoch: batch, resync: true } );
			// A resync republishes every character it still holds.
			for ( const [gid, walk] of paths ) events.push( state( gid, walk, "spawn" ) );
		}
		for ( let count = 1 + random( 6 ); count > 0; count-- ) {
			const gid = 1 + random( 12 ), walk = paths.get( gid ), roll = random( 20 );
			if ( roll === 0 ) {
				paths.delete( gid );
				events.push( { kind: "despawn", gid } );
			} else if ( roll === 1 ) {
				paths.delete( gid );
				events.push( state( gid, undefined ) );
			} else if ( !walk || roll === 2 ) {
				const fresh = Array.from( { length: 1 + random( 60 ) }, () => pose( nextStep++ ) );
				paths.set( gid, fresh );
				events.push( state( gid, fresh, walk ? "state" : "spawn" ) );
			} else {
				const slid = [
					...walk.slice( random( 4 ) ),
					...Array.from( { length: random( 4 ) }, () => pose( nextStep++ ) )
				];
				if ( slid.length ) paths.set( gid, slid );
				events.push( state( gid, slid.length ? slid : walk ) );
			}
		}
		const result = port.deliver( events );
		assert.deepStrictEqual( result.retained, result.whole, `batch ${batch}` );
		for ( const event of events ) whole += event.entity?.movementPath?.walkingPath?.length ?? 0;
		crossed += result.crossed;
	}
	assert.ok( crossed * 3 < whole, `${crossed} of ${whole} poses crossed` );
});

test("a change without its base is a broken stream", () => {
	const window = Array.from( { length: 8 }, ( _, step ) => pose( step ) );
	const posted = createWalkingPathMemory();
	packWalkingPaths( { sequence: 1, events: [ state( 4, window ) ] }, posted );
	const message = structuredClone(
		packWalkingPaths( { sequence: 2, events: [ state( 4, [ ...window.slice( 1 ), pose( 8 ) ] ) ] }, posted )
	);
	assert.equal( message.walkingPaths.length, 1 );
	// A host that never received the first batch holds no base.
	assert.throws(
		() => restoreWalkingPaths( message.batch, message.walkingPaths, createWalkingPathMemory() ),
		/without its base/
	);
	// A base shorter than the change reaches is refused too.
	const short = createWalkingPathMemory();
	short.set( 4, window.slice( 0, 3 ) );
	assert.throws(
		() => restoreWalkingPaths( structuredClone( message.batch ), message.walkingPaths, short ),
		/without its base/
	);
	// A change must name an event of the batch.
	const held = createWalkingPathMemory();
	held.set( 4, window );
	assert.throws(
		() =>
			restoreWalkingPaths( structuredClone( message.batch ), [ { ...message.walkingPaths[0], event: 5 } ], held ),
		/without its event/
	);
});

test("the published batch is not written", () => {
	const window = Array.from( { length: 8 }, ( _, step ) => pose( step ) );
	const posted = createWalkingPathMemory();
	packWalkingPaths( { sequence: 1, events: [ state( 4, window ) ] }, posted );
	const next = [ ...window.slice( 1 ), pose( 8 ) ];
	const batch = Object.freeze( { sequence: 2, events: Object.freeze( [ state( 4, next ) ] ) } );
	const packed = packWalkingPaths( batch, posted );
	assert.equal( batch.events[0].entity.movementPath.walkingPath, next );
	assert.notEqual( packed.batch, batch );
	assert.equal( packed.batch.events[0].entity.movementPath.walkingPath, undefined );
});
