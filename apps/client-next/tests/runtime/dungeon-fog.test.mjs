/*
===========================================================================

dungeon-fog.test.mjs - the height term of a dungeon block's shader fog

A5A680 loads c22 as ( center, band, 1 / divisor, rise ). These pin how the
.dof fields become the shader's three terms, including its two guards.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { dungeonHeightFog } = await import( "../../src/engine/foundation/rendering/dungeon-fog.ts" );

const FOG = { color: 0, nearPlane: -200, farPlane: 1000, intensity: .0003 };

test("a block's height fog divides its rise by the divisor", () => {
	const heightFog = { unk3: 700, unk4: 200, unk5: 1000, unk6: .5 };
	assert.deepEqual( dungeonHeightFog( { ...FOG, heightFog } ), [ 700, 200, .0005 ] );
});

test("a divisor of zero counts as one", () => {
	const heightFog = { unk3: 700, unk4: 200, unk5: 0, unk6: .5 };
	assert.deepEqual( dungeonHeightFog( { ...FOG, heightFog } ), [ 700, 200, .5 ] );
});

test("a block without a positive rise has no height term", () => {
	assert.deepEqual( dungeonHeightFog( FOG ), [ 0, 0, 0 ] );
	assert.deepEqual( dungeonHeightFog( { ...FOG, heightFog: null } ), [ 0, 0, 0 ] );
	const heightFog = { unk3: 700, unk4: 200, unk5: 1000, unk6: 0 };
	assert.deepEqual( dungeonHeightFog( { ...FOG, heightFog } ), [ 0, 0, 0 ] );
});
