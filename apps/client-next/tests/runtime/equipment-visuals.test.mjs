/*
===========================================================================

equipment-visuals.test.mjs - tests for equipment-visuals.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { selectEquipmentVisuals, selectDefaultWear } = await import(
	sourceFileUrl( "src/engine/foundation/animation/equipment-visuals.ts" ).href
);
const rules = {
	1: { slot: 0, visualMask: 1, visualPriority: 150 },
	2: { slot: 1, visualMask: 2, visualPriority: 150 },
	3: { slot: 8, visualMask: 3, visualPriority: 50 },
	4: { slot: null, avatarSlot: 0, visualMask: 1, visualPriority: 70 },
	5: { slot: null, avatarSlot: 2, visualMask: 3, visualPriority: 90 },
	6: { slot: 6, visualMask: 0, visualPriority: 150 },
	7: { slot: 7, visualMask: 0, visualPriority: 150 },
	8: { slot: null, avatarSlot: 1, visualMask: 3, visualPriority: 70 }
};
const item = id => ({ refObjId: id, slot: rules[id].slot ?? -1, avatar: rules[id].avatarSlot !== undefined });
const select = ( ids, missing = [], hair = false, mounted = false ) =>
	selectEquipmentVisuals(
		ids.map( item ),
		rules,
		new Set( ids.filter( id => !missing.includes( id ) ) ),
		hair,
		mounted
	).map( i => i.refObjId );
test("job mask applies even if its own resource cannot attach", () => {
	assert.deepEqual( select( [ 1, 2, 3 ] ), [ 3 ] );
	assert.deepEqual( select( [ 1, 2, 3 ], [ 3 ] ), [] );
});
test("only admitted higher priority avatars suppress lower priority pieces", () => {
	assert.deepEqual( select( [ 1, 2, 4, 5 ] ), [ 2, 4 ] );
	assert.deepEqual( select( [ 1, 2, 4, 5 ], [ 4 ] ), [ 5 ] );
	assert.deepEqual( select( [ 1, 2, 4, 5 ], [ 4, 5 ] ), [ 1, 2 ] );
});
test("same-priority avatar references coexist; traversal order is preserved", () => {
	assert.deepEqual( select( [ 8, 4 ] ), [ 8, 4 ] );
	assert.deepEqual( select( [ 4, 8 ] ), [ 4, 8 ] );
});
test("Hwan hair excludes normal heads and overlapping avatars before mask evaluation", () => {
	assert.deepEqual( select( [ 1, 2, 4, 5 ], [], true ), [ 2 ] );
	assert.deepEqual( select( [ 1, 2, 3 ], [], true ), [ 3 ] );
});
test("mount state excludes both weapon sockets and dismount restores them", () => {
	assert.deepEqual( select( [ 1, 6, 7 ], [], false, true ), [ 1 ] );
	assert.deepEqual( select( [ 1, 6, 7 ] ), [ 1, 6, 7 ] );
});
test("fortress excludes armor, job and avatars but keeps both weapon handles eligible", () => {
	const all = [ 1, 2, 3, 4, 5, 6, 7, 8 ];
	for ( const mounted of [ false, true ] ) {
		assert.deepEqual(
			selectEquipmentVisuals( all.map( item ), rules, new Set( all ), false, mounted, true ).map( i =>
				i.refObjId
			),
			mounted ? [] : [ 6, 7 ]
		);
	}
	assert.deepEqual( select( all ), [ 3, 6, 7 ] );
});
test("ownerless Chinese and European previews fill missing body and legs", () => {
	// Ownerless previews (dock, creation) pass the player gate for both races.
	const r = {
		11: { armorClass: 3, thiefSuit: false, visualMask: 0 },
		12: { armorClass: 3, thiefSuit: false, visualMask: 0 },
		13: { armorClass: 1, thiefSuit: false, visualMask: 0 }
	};
	assert.deepEqual( selectDefaultWear( [], [], r, true ), [ "clothes_BA", "clothes_LA" ] );
	assert.deepEqual( selectDefaultWear( [ { refObjId: 11, slot: 2 } ], [], r, true ), [ "light_BA", "light_LA" ] );
	assert.deepEqual( selectDefaultWear( [ { refObjId: 11, slot: 1 }, { refObjId: 12, slot: 4 } ], [], r, true ), [] );
	assert.deepEqual(
		selectDefaultWear( [ { refObjId: 11, slot: 2 }, { refObjId: 13, slot: 0 } ], [], r, true ),
		[ "clothes_BA", "clothes_LA" ],
		"the first armor socket decides the family"
	);
});

test("default clothing uses raw occupancy, first armor socket, CH gate, thief suit and accepted avatars", () => {
	const r = {
		1: { armorClass: 1, thiefSuit: false, visualMask: 1 },
		2: { armorClass: 3, thiefSuit: false, visualMask: 4 },
		3: { armorClass: 0, thiefSuit: true, visualMask: 0 },
		4: { armorClass: 0, thiefSuit: false, visualMask: 18 }
	};
	const head = { slot: 0, refObjId: 1 },
		shoulder = { slot: 2, refObjId: 2 },
		thief = { slot: 8, refObjId: 3 },
		avatar = { slot: -1, refObjId: 4, avatar: true };
	assert.deepEqual( selectDefaultWear( [], [], r, false ), [] );
	for ( let cycle = 0; cycle < 3; cycle++ ) {
		assert.deepEqual( selectDefaultWear( [], [], r, true ), [ "clothes_BA", "clothes_LA" ] );
		assert.deepEqual( selectDefaultWear( [ shoulder ], [], r, true ), [ "light_BA", "light_LA" ] );
		assert.deepEqual( selectDefaultWear( [ shoulder, head ], [], r, true ), [ "clothes_BA", "clothes_LA" ] );
		// No model was admitted, but raw body/leg references still suppress defaults.
		assert.deepEqual(
			selectDefaultWear( [ { slot: 1, refObjId: 1 }, { slot: 4, refObjId: 2 } ], [], r, true ),
			[]
		);
		assert.deepEqual( selectDefaultWear( [ thief ], [], r, true ), [] );
		assert.deepEqual( selectDefaultWear( [ avatar ], [ avatar ], r, true ), [] );
		assert.deepEqual( selectDefaultWear( [ avatar ], [], r, true ), [ "clothes_BA", "clothes_LA" ] );
	}
});
