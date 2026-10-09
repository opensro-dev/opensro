/*
===========================================================================

cast-facing.test.mjs - tests for cast-facing.ts

A caster keeps turning toward a target that moves during the cast, at the
native 10 radians per second of CIDecoSkill_Update (8DC440), and keeps the
turned yaw after the cast until movement or a new server heading.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { castFacing } = await import( sourceFileUrl( "src/engine/foundation/gameplay/cast-facing.ts" ).href );
const { movementHeading } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/native-movement.ts" ).href
);

const FRAME_SECONDS = 0.05;
const WORDS_PER_RADIAN = 65536 / (Math.PI * 2);
const caster = { regionId: 1, x: 100, y: 0, z: 100, angle: 0 };
const startTarget = { regionId: 1, x: 200, y: 0, z: 100, angle: 0 };
const movedTarget = { regionId: 1, x: 100, y: 0, z: 200, angle: 0 };

/*
================
frame
================
*/
function frame( held, seconds, overrides = {} ) {
	return castFacing( held, {
		caster,
		target: movedTarget,
		tracking: true,
		moving: false,
		revision: 1,
		seconds,
		...overrides
	} );
}

test("a caster turns toward the target's current position at ten radians per second", () => {
	let held = frame( undefined, 0, { target: startTarget } );
	assert.equal( held.angle, 0, "The cast starts on the server's heading" );
	held = frame( held, FRAME_SECONDS );
	assert.ok(
		Math.abs( held.angle - 10 * FRAME_SECONDS * WORDS_PER_RADIAN ) < 1,
		`One frame turns half a radian, got ${held.angle}`
	);
	for ( let i = 2; i < 20; i++ ) held = frame( held, i * FRAME_SECONDS );
	assert.ok(
		Math.abs( held.angle - movementHeading( caster, movedTarget ) ) < 1,
		"The caster ends facing where the target walked to, not where it stood"
	);
});

test("the turned yaw outlives the cast until movement or a new server heading", () => {
	let held = frame( undefined, 0 );
	for ( let i = 1; i < 20; i++ ) held = frame( held, i * FRAME_SECONDS );
	const after = frame( held, 2, { tracking: false, target: undefined } );
	assert.equal( after?.angle, held.angle, "Cast end keeps the yaw 8DC440 wrote" );
	assert.equal( frame( after, 3, { tracking: false, revision: 2 } ), undefined, "A new movement revision" );
	assert.equal(
		frame( after, 3, { tracking: false, caster: { ...caster, angle: 100 } } ),
		undefined,
		"A new server heading"
	);
	assert.equal( frame( after, 3, { moving: true } ), undefined, "Movement" );
});

test("an actor that never tracked keeps its logical heading", () => {
	assert.equal( frame( undefined, 0, { tracking: false } ), undefined );
});
