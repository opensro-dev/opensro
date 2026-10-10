/*
===========================================================================

stack-rollback.test.mjs - retained quantities after operator cap rollback

The client must reconstruct the same split and cross-container counts as
the server, including cap-one elixirs and the maximum wire quantity.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { planContainerMove, planWholeTransfer } = await import(
	"../../src/engine/foundation/gameplay/container-transfer.ts"
);

/*
================
item
================
*/
function item( slot, quantity, typeFlags = 0x8ec ) {
	return { slot, quantity, typeFlags, refObjId: 1, plus: 0, durability: 0, variance: "0", magic: [] };
}

for (
	const [source, destination, cap, wantSource, wantDestination] of [
		[ 100, 10, 50, 60, 50 ],
		[ 10, 100, 50, 100, 10 ],
		[ 65535, 65535, 50, 65535, 65535 ],
		[ 65535, 49, 50, 65534, 50 ],
		[ 50, 1, 1, 1, 50 ]
	]
) {
	test(`retained stacks transfer ${source}/${destination} at cap ${cap}`, () => {
		const flags = cap === 1 ? 0xd6c : 0x8ec;
		for ( const [from, to] of [ [ 13, 0 ], [ 0, 13 ] ] ) {
			const result = planWholeTransfer(
				[ item( from, source, flags ) ],
				[ item( to, destination, flags ) ],
				from,
				to,
				new Map( [ [ 1, cap ] ] )
			);
			assert.equal( result.from[0].quantity, wantSource );
			assert.equal( result.to[0].quantity, wantDestination );
			assert.equal( result.from[0].quantity + result.to[0].quantity, source + destination );
		}
	});
}

test("retained elixirs split at cap one; native singles still swap", () => {
	const caps = new Map( [ [ 1, 1 ] ] );
	const original = item( 13, 50, 0xd6c );
	for ( const quantity of [ 1, 49, 50 ] ) {
		const result = planContainerMove( [ original ], { source: 13, destination: 14, quantity }, caps, "bag" );
		const destination = result.find( row => row.slot === 14 );
		assert.ok( destination );
		assert.equal( destination.quantity, quantity );
		assert.equal( result.reduce( ( count, row ) => count + row.quantity, 0 ), 50 );
	}
	for ( const quantity of [ 0, 51 ] ) {
		assert.throws(
			() => planContainerMove( [ original ], { source: 13, destination: 14, quantity }, caps, "bag" ),
			/Invalid bag split quantity/
		);
	}
	const a = { ...item( 13, 1, 0xd6c ), name: "first" }, b = { ...item( 14, 1, 0xd6c ), name: "second" };
	assert.deepEqual( planContainerMove( [ a, b ], { source: 13, destination: 14, quantity: 1 }, caps, "bag" ), [ {
		...b,
		slot: 13
	}, { ...a, slot: 14 } ] );
});

test("all configurable families transfer retained stacks", () => {
	for ( const flags of [ 0x8ec, 0x10ec, 0x18ec, 0x20ec, 0x48ec, 0xd6c, 0x156c, 0xdec, 0x15ec, 0x3dec ] ) {
		const result = planWholeTransfer(
			[ item( 13, 100, flags ) ],
			[ item( 0, 10, flags ) ],
			13,
			0,
			new Map( [ [ 1, 50 ] ] )
		);
		assert.deepEqual( [ result.from[0].quantity, result.to[0].quantity ], [ 60, 50 ] );
	}
});

test("rollback eligibility does not admit unrelated oversized rows", () => {
	for (
		const row of [ item( 13, 50, 0x1dec ), item( 13, 50, 0x46c ), item( 13, 50 ), {
			...item( 13, 50, 0xd6c ),
			plus: 1
		}, item( 13, 65536, 0xd6c ) ]
	) {
		assert.throws(
			() => planWholeTransfer( [ row ], [ { ...row, slot: 0, quantity: 1 } ], 13, 0, new Map( [ [ 1, 1 ] ] ) ),
			/Invalid transfer stack limit\/count/
		);
	}
});

test("retained stone stacks split and transfer without changing assimilation", () => {
	for ( const flags of [ 0xdec, 0x15ec, 0x3dec ] ) {
		const plus = flags === 0x3dec ? 0 : 90;
		const source = { ...item( 13, 50, flags ), plus };
		const split = planContainerMove(
			[ source ],
			{ source: 13, destination: 14, quantity: 3 },
			new Map( [ [ 1, 1 ] ] ),
			"bag"
		);
		assert.deepEqual( split.map( row => [ row.slot, row.quantity, row.plus ] ), [ [ 13, 47, plus ], [
			14,
			3,
			plus
		] ] );
		for ( const cap of [ 1, 20 ] ) {
			const target = { ...source, slot: 0, quantity: 1 };
			const moved = planWholeTransfer( [ source ], [ target ], 13, 0, new Map( [ [ 1, cap ] ] ) );
			assert.deepEqual( [ moved.from[0].quantity, moved.to[0].quantity ], cap === 1 ? [ 1, 50 ] : [ 31, 20 ] );
			assert.equal( moved.from[0].plus, plus );
			assert.equal( moved.to[0].plus, plus );
		}
	}
});

test("native stone singles retain count-only cross-container behavior", () => {
	for ( const flags of [ 0xdec, 0x15ec ] ) {
		const a = { ...item( 13, 1, flags ), plus: 40 }, b = { ...item( 0, 1, flags ), plus: 90 };
		assert.deepEqual( planWholeTransfer( [ a ], [ b ], 13, 0, new Map( [ [ 1, 1 ] ] ) ), {
			from: [ a ],
			to: [ b ]
		} );
		assert.deepEqual( planWholeTransfer( [ a ], [ b ], 13, 0, new Map( [ [ 1, 50 ] ] ) ), {
			from: [ { ...b, slot: 13 } ],
			to: [ { ...a, slot: 0 } ]
		} );
		const retained = { ...a, quantity: 10 };
		assert.deepEqual( planWholeTransfer( [ retained ], [ b ], 13, 0, new Map( [ [ 1, 1 ] ] ) ), {
			from: [ { ...b, slot: 13 } ],
			to: [ { ...retained, slot: 0 } ]
		} );
	}
});
