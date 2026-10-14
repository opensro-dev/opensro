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

test("SRO_JOB_SUIT_HIDES_ARMOR: a trader or hunter suit hides armour only with the flag", () => {
	// The shipped masks: a trader/hunter suit 0x700, a thief suit 0x73F;
	// armour pieces carry 1 << socket at priority 150.
	const suits = {
		...rules,
		20: { slot: 8, visualMask: 0x700, visualPriority: 50 },
		21: { slot: 8, visualMask: 0x73f, visualPriority: 50 },
		22: { slot: 4, visualMask: 0x10, visualPriority: 150 }
	};
	const worn = ids => ids.map( id => ({ refObjId: id, slot: suits[id].slot }) );
	const pick = ( ids, flag ) =>
		selectEquipmentVisuals( worn( ids ), suits, new Set( ids ), false, false, false, flag ).map( i => i.refObjId );
	// Native: the trader suit is drawn over the chest and legs; the thief hides them.
	assert.deepEqual( pick( [ 2, 22, 20 ], false ), [ 2, 22, 20 ] );
	assert.deepEqual( pick( [ 2, 22, 21 ], false ), [ 21 ] );
	// Flag on: the trader suit hides them as the thief suit does; weapons stay.
	assert.deepEqual( pick( [ 2, 22, 20 ], true ), [ 20 ] );
	assert.deepEqual( pick( [ 2, 6, 20 ], true ), [ 6, 20 ] );
	// Without a job suit the flag changes nothing.
	assert.deepEqual( pick( [ 2, 22 ], true ), [ 2, 22 ] );
	// Default wear: the suit is only a back prop, so the hidden armour counts
	// as unworn and the body gets an unarmoured character's garments instead
	// of showing bare. Natively the worn light chest and legs block them.
	const wear = {
		20: { armorClass: 0, thiefSuit: false, visualMask: 0x700 },
		21: { armorClass: 0, thiefSuit: true, visualMask: 0x73f },
		30: { armorClass: 2, thiefSuit: false, visualMask: 0x2 },
		31: { armorClass: 2, thiefSuit: false, visualMask: 0x10 }
	};
	const armoured = [ { refObjId: 30, slot: 1 }, { refObjId: 31, slot: 4 }, { refObjId: 20, slot: 8 } ];
	assert.deepEqual( selectDefaultWear( armoured, armoured, wear, true, false ), [] );
	assert.deepEqual( selectDefaultWear( armoured, armoured, wear, true, true ), [ "clothes_BA", "clothes_LA" ] );
	// Without a job suit the flag keeps the native occupancy.
	assert.deepEqual( selectDefaultWear( armoured.slice( 0, 2 ), armoured.slice( 0, 2 ), wear, true, true ), [] );
	// A thief suit still clears them, flag or not; EU bodies never get them.
	const thief = [ ...armoured.slice( 0, 2 ), { refObjId: 21, slot: 8 } ];
	assert.deepEqual( selectDefaultWear( thief, thief, wear, true, true ), [] );
	assert.deepEqual( selectDefaultWear( armoured, armoured, wear, false, true ), [] );
});
