/*
===========================================================================

pose-order.test.mjs - the order the character renderer evaluates poses in

The pose path depends on this sequence: pose sharing, level-of-detail stamps
and the cosmetic frame budget all follow it. The renderer reads each sort key
once into scratch instead of two map lookups per comparison; the sequence must
stay exactly what the original comparator gave over the whole frame.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createPoseOrder } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );

/*
================
random

Seeded mulberry32, so a failing crowd can be replayed.
================
*/
function random( seed ) {
	let state = seed >>> 0;
	return () => {
		state = (state + 0x6d2b79f5) >>> 0;
		let t = state;
		t = Math.imul( t ^ (t >>> 15), t | 1 );
		t ^= t + Math.imul( t ^ (t >>> 7), t | 61 );
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

/*
================
crowd

A frame with ties: few distinct samples, unsampled and unowned actors, three
optional states, a repeated gid now and then, and a needed subset.
================
*/
function crowd( next ) {
	const count = Math.floor( next() * 600 );
	const samples = new Map();
	/** @type {any[]} */
	const actors = [];
	const needed = new Set();
	for ( let i = 0; i < count; i++ ) {
		const gid = next() < .05 && i ? actors[Math.floor( next() * i )].gid : 1000 + i;
		const lod = next();
		actors.push( {
			gid,
			animationLod: lod < .3 ? undefined : { fraction: .8, crowded: true, optional: lod < .65 }
		} );
		const owned = next();
		if ( owned < .7 ) samples.set( gid, owned < .1 ? undefined : Math.floor( next() * 4 ) * .05 );
		if ( next() < .6 ) needed.add( gid );
	}
	return { actors, samples, needed };
}

/*
================
referenceOrder

The renderer's order before keyed scratch: every frame actor copied, sorted
by the original comparator with its two lookups, and filtered while poses
are evaluated.
================
*/
function referenceOrder( actors, samples, needed, budgeted ) {
	const order = [ ...actors ];
	if ( budgeted ) {
		order.sort( ( a, b ) =>
			Number( !!a.animationLod?.optional ) - Number( !!b.animationLod?.optional ) ||
			(samples.get( a.gid ) ?? -1) - (samples.get( b.gid ) ?? -1)
		);
	}
	return order.filter( actor => needed.has( actor.gid ) );
}

test("keyed pose order equals the original comparator over random crowds", () => {
	const order = createPoseOrder();
	let ties = 0;
	for ( let seed = 1; seed <= 400; seed++ ) {
		const next = random( seed );
		const { actors, samples, needed } = crowd( next );
		const budgeted = seed % 4 !== 0;
		const expected = referenceOrder( actors, samples, needed, budgeted );
		const sample = budgeted ? gid => samples.get( gid ) : undefined;
		for ( let i = 1; i < expected.length && budgeted; i++ ) {
			const a = expected[i - 1], b = expected[i];
			if (
				a.gid !== b.gid && !!a.animationLod?.optional === !!b.animationLod?.optional &&
				(samples.get( a.gid ) ?? -1) === (samples.get( b.gid ) ?? -1)
			) ties++;
		}
		// Evaluating a pose stamps its sample, as poseFor does: an order that
		// read a key after an earlier evaluation would move.
		const visited = [];
		for ( const actor of order( actors, needed, sample ) ) {
			visited.push( actor );
			samples.set( actor.gid, 1 + visited.length );
		}
		assert.equal( visited.length, expected.length, `seed ${seed}` );
		for ( let i = 0; i < expected.length; i++ ) {
			assert.equal( visited[i], expected[i], `seed ${seed}, position ${i}` );
		}
	}
	assert.ok( ties > 1000, "the crowds hold ties between distinct actors" );
});
