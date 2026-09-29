/*
===========================================================================

reference-appearance.test.mjs - tests for reference-appearance.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const { chooseReferenceAppearance, createReferenceAppearances, referenceAppearanceItems } = await import(
	sourceFileUrl( "src/engine/foundation/animation/reference-appearance.ts" ).href
);
test("reference type-3 appearance uses native ordered CRT draws, cap fallback and independent head selection", () => {
	const values = [ 2, 3, 2, 8, 141, 1 ], used = [];
	const choice = chooseReferenceAppearance( 0, [ [ 100 ], [ 200, 201 ] ], () => {
		const v = values[used.length];
		used.push( v );
		return v;
	} );
	assert.deepEqual( choice, { model: 201, race: 1, armor: 1, weapon: 10, level: 1, head: "HA" } );
	assert.equal( used.length, 6 );
});
test("reference model choices are stable, stop restores original without resuming older transforms, immediate stop still rolls", () => {
	let draws = 0;
	const owner = createReferenceAppearances( () => {
		draws++;
		return 0;
	} );
	owner.setReferences( new Map( [ [ 7, { type: 3, cap: 50 } ], [ 8, { type: 2, cap: 60 } ] ] ), [ [ 1 ], [ 2 ] ] );
	const a = { key: "a", gid: 1, skill: 7, stopped: false }, b = { ...a, key: "b" };
	owner.step( [ a ] );
	assert.equal( owner.get( 1 ).model, 2 );
	owner.step( [ a ] );
	assert.equal( draws, 6 );
	owner.step( [ a, b ] );
	assert.equal( draws, 12 );
	owner.step( [ a, { ...b, stopped: true } ] );
	assert.equal( owner.get( 1 ), undefined );
	owner.step( [ a ] );
	assert.equal( owner.get( 1 ), undefined );
	owner.reset();
	owner.step( [ { ...a, stopped: true } ] );
	assert.equal( draws, 18 );
	assert.equal( owner.get( 1 ), undefined );
	owner.reset();
});
test("appearance equipment walks degrees per part and selects only the rolled head", () => {
	// Level 30 is degree 4; each piece takes the highest degree the catalog holds.
	const itemIds = new Map( [
		[ "ITEM_CH_BLADE_01_A", 101 ],
		[ "ITEM_CH_BLADE_03_A", 103 ],
		[ "ITEM_CH_M_HEAVY_01_HA_A", 201 ],
		[ "ITEM_CH_M_HEAVY_02_BA_A", 202 ],
		[ "ITEM_CH_SHIELD_01_A", 301 ],
		[ "ITEM_CH_M_HEAVY_05_LA_A", 405 ]
	] );
	const base = { race: 0, model: 1, level: 30, armor: 3, weapon: 3, head: "CA" };
	assert.deepEqual( referenceAppearanceItems( base, true, itemIds ), [
		{ slot: 6, refObjId: 103, plus: 0 },
		{ slot: 1, refObjId: 202, plus: 0 },
		{ slot: 7, refObjId: 301, plus: 0 }
	], "a degree above the level's (05 legs) is never taken" );
	assert.deepEqual(
		referenceAppearanceItems( { ...base, head: "HA" }, true, itemIds ).map( i => i.slot ),
		[ 6, 0, 1, 7 ]
	);
	assert.deepEqual(
		referenceAppearanceItems(
			{ ...base, race: 1, weapon: 10, armor: 1 },
			false,
			new Map( [ [ "ITEM_EU_DARKSTAFF_02_A", 9 ] ] )
		),
		[ { slot: 6, refObjId: 9, plus: 0 } ],
		"weapon class 10 is the darkstaff; it takes no shield"
	);
});
test("8DD131: a mask skin shows until an msch instance on its gid ends or stops; a new application shows again", () => {
	const owner = createReferenceAppearances( () => 0 );
	owner.setReferences( new Map( [ [ 7126, { type: 1, cap: 0 } ] ] ), [ [ 1 ], [ 2 ] ] );
	const skin = { refObjId: 1933, revision: 1 },
		mask = { key: "m", gid: 5, skill: 7126, stopped: false },
		other = { key: "x", gid: 5, skill: 1, stopped: false };
	owner.step( [ mask ] );
	assert.equal( owner.skin( 5, skin ), 1933 );
	assert.equal( owner.get( 5 ), undefined, "mode 1 rolls no disguise" );
	owner.step( [ mask, other ] );
	owner.step( [ mask ] );
	assert.equal( owner.skin( 5, skin ), 1933, "a non-msch end keeps the skin" );
	owner.step( [] );
	assert.equal( owner.skin( 5, skin ), undefined, "the mask ended" );
	assert.equal( owner.skin( 5, { ...skin, revision: 2 } ), 1933, "a fresh 0x323A applies again" );
	owner.step( [ { ...mask, key: "n" } ] );
	owner.step( [ { ...mask, key: "n", stopped: true } ] );
	assert.equal( owner.skin( 5, { ...skin, revision: 2 } ), undefined, "stop restores as well" );
	assert.equal( owner.skin( 6, skin ), 1933, "ends are per gid" );
	assert.equal( owner.skin( 6, undefined ), undefined );
});
