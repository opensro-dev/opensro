/*
===========================================================================

animation-timelines.test.mjs - tests for animation-timelines.ts, character-render-plan.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createAnimationTimelines, TIMELINE_CURSOR_BYTES } = await import(
	sourceFileUrl( "src/engine/foundation/animation/animation-timelines.ts" ).href
);
const { characterPoseBytes } = await import(
	sourceFileUrl( "src/engine/foundation/animation/character-budget.ts" ).href
);
const { createCharacterRenderPlan } = await import(
	sourceFileUrl( "src/engine/foundation/animation/character-render-plan.ts" ).href
);
/*
================
cursor

The cursor channel c of a pose's timelines reads.
================
*/
const cursor = ( timelines, c ) => timelines.cursors[timelines.index[c]];

test("shared clocks match a stateless linear-search oracle through seeks, duplicate keys and endpoints", () => {
	const arrays = [ [ .1, .3, .3, .9, 1.8 ], [ .1, .3, .3, .9, 1.8 ], [ 0, .2, .7, 2 ], [ 1 ], [ 0, 1 ], [ -0, 1 ] ]
		.map( values => Float32Array.from( values ) );
	const plan = createAnimationTimelines( { channels: arrays.map( times => ({ times }) ) } );
	assert.equal( cursor( plan, 0 ), cursor( plan, 1 ) );
	assert.notEqual( cursor( plan, 4 ), cursor( plan, 5 ) );
	let seed = 17;
	for (
		const time of [
			...arrays.flatMap( values => [ ...values ] ),
			0,
			10,
			...Array.from( { length: 2000 }, () => {
				seed = (Math.imul( seed, 1664525 ) + 1013904223) >>> 0;
				return seed / 2 ** 32 * 3;
			} )
		]
	) {
		plan.sample( time );
		for ( let i = 0; i < arrays.length; i++ ) {
			const times = arrays[i];
			let low = 0;
			for ( let key = 0; key < times.length; key++ ) if ( times[key] <= time ) low = key;
			const next = Math.min( low + 1, times.length - 1 ),
				span = times[next] - times[low],
				fraction = span ? Math.max( 0, Math.min( 1, (time - times[low]) / span ) ) : 0;
			const { low: l, next: n, span: s, fraction: f } = cursor( plan, i );
			assert.deepEqual( [ l, n, s, f ], [ low, next, span, fraction ] );
		}
	}
});

test("pose owners keep independent clocks even when they share immutable clip data", () => {
	const clip = { channels: [ { times: Float32Array.of( 0, 1, 2 ) } ] },
		a = createAnimationTimelines( clip ),
		b = createAnimationTimelines( clip );
	a.sample( .25 );
	b.sample( 1.75 );
	assert.equal( cursor( a, 0 ).fraction, .25 );
	assert.equal( cursor( b, 0 ).fraction, .75 );
});

test("a timestamp hash collision never aliases different authored clocks", () => {
	const a = Float32Array.of( 0, 1 ), b = new Float32Array( Uint32Array.of( 1, 1048577683 ).buffer );
	const hash = times => {
		let h = 2166136261;
		for ( const word of new Uint32Array( times.buffer ) ) h = Math.imul( h ^ word, 16777619 );
		return h;
	};
	assert.equal( hash( a ), hash( b ) );
	const plan = createAnimationTimelines( { channels: [ { times: a }, { times: b } ] } );
	assert.notEqual( cursor( plan, 0 ), cursor( plan, 1 ) );
	plan.sample( .125 );
	assert.notEqual( cursor( plan, 0 ).fraction, cursor( plan, 1 ).fraction );
});

test("a pose keeps one cursor per timeline of a clip, not one record per channel", () => {
	// A native clip keys every channel at the same times.
	const times = Float32Array.of( 0, .5, 1 );
	const clip = { channels: Array.from( { length: 73 }, () => ({ times }) ) };
	const a = createAnimationTimelines( clip ), b = createAnimationTimelines( clip );
	assert.equal( a.cursors.length, 1 );
	assert.deepEqual( [ ...new Set( a.index ) ], [ 0 ] );
	assert.notEqual( a.cursors[0], b.cursors[0] );
});

test("a pose is charged one cursor per timeline of a clip, not one per channel", () => {
	const node = i => ({
		name: "n" + i,
		parent: i ? 0 : -1,
		translation: [ 0, 0, 0 ],
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	});
	const body = channelTimes => ({
		nodes: Array.from( { length: 30 }, ( _, i ) => node( i ) ),
		primitives: [],
		images: [],
		clips: [ {
			name: "stand",
			duration: 1,
			channels: Array.from( { length: 90 }, ( _, c ) => ({
				node: c % 30,
				path: [ "translation", "rotation", "scale" ][c % 3],
				interpolation: "LINEAR",
				times: channelTimes( c ),
				values: new Float32Array( 8 )
			}) )
		} ]
	});
	const shared = Float32Array.of( 0, 1 );
	const native = body( () => shared ), apart = body( c => Float32Array.of( 0, 1 + c ) );
	assert.equal( characterPoseBytes( apart ) - characterPoseBytes( native ), 89 * TIMELINE_CURSOR_BYTES );
});

/*
================
catalog

A body's clip catalog as the decoder admits it: native clips key every
channel at the same times, others key some channels apart, and two
timelines differ only in the sign of a zero.
================
*/
function catalog() {
	const node = i => ({
		name: "n" + i,
		parent: i ? 0 : -1,
		translation: [ 0, 0, 0 ],
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	});
	const clip = ( name, channelTimes ) => ({
		name,
		duration: 2,
		channels: Array.from( { length: 60 }, ( _, c ) => ({
			node: c % 20,
			path: [ "translation", "rotation", "scale" ][c % 3],
			interpolation: "LINEAR",
			times: channelTimes( c ),
			values: new Float32Array( 8 )
		}) )
	});
	const native = Float32Array.from( { length: 41 }, ( _, k ) => k / 20 );
	return {
		nodes: Array.from( { length: 20 }, ( _, i ) => node( i ) ),
		primitives: [],
		images: [],
		clips: [
			clip( "stand", () => native ),
			clip( "run", c => c % 7 ? native : Float32Array.of( 0, 1 + c ) ),
			clip( "sit", c => c % 2 ? Float32Array.of( 0, 1 ) : Float32Array.of( -0, 1 ) )
		]
	};
}
/*
================
countedTimelines

A renderer's timeline cache, and how many clips it was asked to store:
each store is one clip whose key times were hashed.
================
*/
function countedTimelines() {
	const timelines = new WeakMap(), store = timelines.set.bind( timelines );
	let walks = 0;
	timelines.set = ( clip, bytes ) => {
		walks++;
		return store( clip, bytes );
	};
	return { timelines, walks: () => walks };
}

test("re-planning a body hashes only the key times of the clip it newly admitted, for the same charge", () => {
	const model = catalog(), { timelines, walks } = countedTimelines(), caches = { timelines, bindings: new WeakMap() };
	createCharacterRenderPlan( model, caches );
	assert.equal( walks(), 3 );
	// The second plan reads every clip's charge from the cache.
	assert.equal( createCharacterRenderPlan( model, caches ).poseBytes, characterPoseBytes( model ) );
	assert.equal( walks(), 3 );
	// An admitted clip appends to the catalog every assembly of the body shares.
	const [added] = catalog().clips, extended = { ...model, clips: [ ...model.clips, { ...added, name: "walk" } ] };
	assert.equal( createCharacterRenderPlan( extended, caches ).poseBytes, characterPoseBytes( extended ) );
	assert.equal( walks(), 4 );
});
