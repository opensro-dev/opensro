/*
===========================================================================

actor-values.test.mjs - a rebuilt actor keeps its value objects while they hold

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { carriedLod, carriedPose, carriedTint } = await import(
	"../../src/engine/runtime/characters/actor-values.ts"
);

test("a pose is carried while every coordinate and the yaw hold, and replaced when any moves", () => {
	const last = { regionId: 25000, x: 1, y: 2, z: 3, yaw: .5 };
	assert.equal( carriedPose( last, { ...last } ), last );
	assert.equal( carriedPose( undefined, last ), last );
	for ( const key of [ "regionId", "x", "y", "z", "yaw" ] ) {
		const moved = { ...last, [key]: last[key] + 1 };
		assert.equal( carriedPose( last, moved ), moved, key );
	}
});

test("a detail level is carried while its three terms hold", () => {
	const last = { fraction: .25, crowded: false, optional: true };
	assert.equal( carriedLod( last, { ...last } ), last );
	assert.equal( carriedLod( undefined, last ), last );
	for ( const changed of [ { fraction: .5 }, { crowded: true }, { optional: false } ] ) {
		const next = { ...last, ...changed };
		assert.equal( carriedLod( last, next ), next, Object.keys( changed )[0] );
	}
});

test("a tint is carried while its colour holds, replaced when it changes, and dropped when it ends", () => {
	const last = [ 1, .72, .32 ];
	assert.equal( carriedTint( last, [ 1, .72, .32 ] ), last );
	for ( let i = 0; i < 3; i++ ) {
		const next = [ ...last ];
		next[i] = 0;
		assert.equal( carriedTint( last, next ), next, `channel ${i}` );
	}
	assert.equal( carriedTint( last, undefined ), undefined );
	assert.equal( carriedTint( undefined, last ), last );
});
