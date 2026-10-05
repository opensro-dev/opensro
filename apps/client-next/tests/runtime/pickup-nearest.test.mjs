/*
===========================================================================

pickup-nearest.test.mjs - tests for pickup-nearest.ts: the pickup
shortcut takes the nearest item the player may take, within 500 units

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { nearestPickable } = await import( "../../src/engine/foundation/gameplay/pickup-nearest.ts" );

const local = { gid: 7, regionId: 0x6a8c, x: 100, z: 100 };
/** @param {number} gid @param {number} x @param {object} [item] */
const drop = ( gid, x, item = {} ) => ({
	gid,
	kind: "ground-item",
	name: "",
	refObjId: 1,
	regionId: 0x6a8c,
	x,
	y: 0,
	z: 100,
	heading: 0,
	groundItem: { typeFlags: 0, goldAmount: 0, tint: 0, ...item }
});

test("the nearest own or unowned item wins", () => {
	const picked = nearestPickable(
		[ drop( 1, 300 ), drop( 2, 120, { ownerJid: 7 } ), drop( 3, 150 ) ],
		local,
		new Set()
	);
	assert.equal( picked?.gid, 2 );
});

test("another player's drop, a claimed drop and a far drop are skipped", () => {
	const items = [
		drop( 1, 110, { ownerJid: 9 } ),
		drop( 2, 115, { claimantGid: 9 } ),
		drop( 3, 700 ),
		drop( 4, 160 )
	];
	assert.equal( nearestPickable( items, local, new Set() )?.gid, 4 );
	assert.equal( nearestPickable( items, local, new Set( [ 9 ] ) )?.gid, 1, "a party member's drop may share" );
	assert.equal( nearestPickable( [ drop( 3, 700 ) ], local, new Set() ), undefined );
});
