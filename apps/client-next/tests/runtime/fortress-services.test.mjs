/*
===========================================================================

fortress-services.test.mjs - native fortress staff packet layouts

703130 request fields and 754A40 response fields are pinned independently.
Every known action and each optional production/registration body is checked.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";

const { fortressServiceRequest, fortressServiceReply } = await import(
	"../../src/engine/foundation/gameplay/fortress-services.ts"
);
const { fortressBootstrap, fortressPacket } = await import( "../../src/engine/foundation/gameplay/fortress.ts" );

/*
================
reply
================
*/
function reply( hex ) {
	return { opcode: 0xb1e1, payload: Uint8Array.from( Buffer.from( hex, "hex" ) ) };
}

test("all v1.150 staff request branches preserve native field order", () => {
	const expected = [
		"443322110008070605",
		"443322110108070605ecff",
		"443322110208070605ffffffffffffffff",
		"443322110308070605",
		"44332211040807060503",
		"443322110508070605",
		"4433221106",
		"44332211070807060503",
		"44332211080807060503",
		"4433221109",
		"0a0807060540302010",
		"443322110b080706054030201003",
		"443322110c0807060540302010",
		"443322110d08070605",
		"443322110e08070605403020100500",
		"443322110f0807060540302010",
		"443322111008070605403020100500",
		"443322111108070605",
		"443322111208070605403020100500",
		"44332211130807060540302010",
		"443322111408070605403020100500",
		"4433221115080706050500",
		"443322111608070605",
		"443322111708070605",
		"443322111808070605"
	];
	for ( let action = 0; action < expected.length; action++ ) {
		const frame = fortressServiceRequest( {
			action,
			target: 0x11223344,
			fortress: 0x05060708,
			reference: 0x10203040,
			word: action === 1 ? -20 : 5,
			stackLimit: 20,
			flag: 3,
			gold: "-1"
		} );
		assert.equal( frame.opcode, 0x71e1 );
		assert.equal( Buffer.from( frame.payload ).toString( "hex" ), expected[action], `action ${action}` );
	}
	assert.throws( () => fortressServiceRequest( { action: 25 } ) );
	assert.throws( () => fortressServiceRequest( { action: 0, fortress: 1 } ) );
	for ( const word of [ -21, 21, 1.5, NaN ] ) {
		assert.throws( () => fortressServiceRequest( { action: 1, target: 1, fortress: 1, word } ) );
	}
	for ( const gold of [ "9223372036854775808", "-9223372036854775809", "1.2", "", "no" ] ) {
		assert.throws( () => fortressServiceRequest( { action: 2, target: 1, fortress: 1, gold } ) );
	}
});

test("all native response bodies decode without borrowing previous fields", () => {
	/** @type {Array<[string, Record<string, unknown>]>} */
	const rows = [
		[ "000108070605ecffffffffffffffff7f", { fortress: 0x05060708, taxRate: -20, gold: "9223372036854775807" } ],
		[ "01011400", { taxRate: 20 } ],
		[ "02010100000000000000", { gold: "1" } ],
		[ "030107", { flags: 7 } ],
		[ "040101", { flags: 1 } ],
		[ "0501" + "0000".repeat( 16 ) + "010100410301", {
			schedules: [ Array( 8 ).fill( 0 ), Array( 8 ).fill( 0 ) ],
			applicants: [ { name: "A", level: 3, side: 1 } ]
		} ],
		[ "0601" + "0000".repeat( 8 ) + "00", { schedules: [ Array( 8 ).fill( 0 ) ], registered: false } ],
		[ "0601" + "0000".repeat( 8 ) + "010807060501", {
			schedules: [ Array( 8 ).fill( 0 ) ],
			registered: true,
			fortress: 0x05060708,
			side: 1
		} ],
		[ "07010807060500", { registered: true, fortress: 0x05060708, side: 0 } ],
		[ "08010807060501", { registered: false, fortress: 0x05060708, side: 1 } ],
		[ "0901", {} ],
		[ "0a010807060540302010", { fortress: 0x05060708, reference: 0x10203040 } ],
		[ "0b01080706054030201003", { fortress: 0x05060708, reference: 0x10203040, level: 3 } ],
		[ "0c01080706054030201064000000", { fortress: 0x05060708, reference: 0x10203040, hp: 100 } ],
		[ "0d010807060500", { fortress: 0x05060708, producing: false } ],
		[ "0d010807060501403020100500010100000000000000", {
			fortress: 0x05060708,
			producing: true,
			reference: 0x10203040,
			quantity: 5,
			ready: 1,
			productionTime: "1"
		} ],
		[ "0e01080706054030201005000100000000000000", {
			fortress: 0x05060708,
			reference: 0x10203040,
			quantity: 5,
			productionTime: "1"
		} ],
		[ "0f010807060540302010", { fortress: 0x05060708, reference: 0x10203040 } ],
		[ "100108070605403020100500", { fortress: 0x05060708, reference: 0x10203040, quantity: 5 } ],
		[ "11010807060500", { fortress: 0x05060708, producing: false } ],
		[ "11010807060501403020100500000100000000000000", {
			fortress: 0x05060708,
			producing: true,
			reference: 0x10203040,
			quantity: 5,
			ready: 0,
			productionTime: "1"
		} ],
		[ "1201080706054030201005000100000000000000", {
			fortress: 0x05060708,
			reference: 0x10203040,
			quantity: 5,
			productionTime: "1"
		} ],
		[ "13010807060540302010", { fortress: 0x05060708, reference: 0x10203040 } ],
		[ "140108070605403020100500", { fortress: 0x05060708, reference: 0x10203040, quantity: 5 } ],
		[ "150108070605403020100200", { fortress: 0x05060708, reference: 0x10203040, gate: 2 } ],
		[ "1601", {} ],
		[ "170140302010", { reference: 0x10203040 } ],
		[ "180108070605014030201064000000", {
			fortress: 0x05060708,
			structures: [ { reference: 0x10203040, remainingMinutes: 100 } ]
		} ]
	];
	for ( const [hex, fields] of rows ) {
		const frame = reply( hex ), action = frame.payload[0];
		assert.deepEqual( fortressServiceReply( frame ), { action, result: 1, ...fields }, `action ${action}` );
		for ( let end = 0; end < frame.payload.length; end++ ) {
			assert.throws( () => fortressServiceReply( { ...frame, payload: frame.payload.slice( 0, end ) } ) );
		}
		assert.throws( () => fortressServiceReply( reply( hex + "00" ) ) );
	}
});

test("smith and trainer collection clamp at the native item stack capacity", () => {
	for ( const action of [ 0x10, 0x14 ] ) {
		for ( const [word, stackLimit, expected] of [ [ 2, 3, 2 ], [ 3, 3, 3 ], [ 4, 3, 3 ], [ 65535, 1, 1 ] ] ) {
			const frame = fortressServiceRequest( { action, target: 1, fortress: 1, reference: 1, word, stackLimit } );
			assert.equal( new DataView( frame.payload.buffer ).getUint16( 13, true ), expected );
		}
		assert.throws( () => fortressServiceRequest( { action, target: 1, fortress: 1, reference: 1, word: 5 } ) );
	}
});

test("every refusal uses one byte and malformed replies leave the state unchanged", () => {
	let state = fortressBootstrap( {} );
	for ( let action = 0; action <= 24; action++ ) {
		const frame = { opcode: 0xb1e1, payload: Uint8Array.of( action, 2, 21 ) };
		const next = fortressPacket( state, frame );
		assert.ok( next );
		state = next;
		assert.deepEqual( state.service, { action, result: 2, error: 21 } );
		assert.throws( () => fortressPacket( state, { ...frame, payload: Uint8Array.of( action, 2, 21, 40 ) } ) );
		assert.deepEqual( state.service, { action, result: 2, error: 21 } );
	}
	assert.throws( () => fortressServiceReply( reply( "0000" ) ) );
	assert.throws( () => fortressServiceReply( reply( "1901" ) ) );
	assert.equal( fortressServiceReply( { opcode: 1, payload: new Uint8Array() } ), null );
});
