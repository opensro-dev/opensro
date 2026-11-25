/*
===========================================================================

caravan-hire.test.mjs - the AI caravan hire window's frames (port-only 0x3FC3 / 0x7FC3)

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCaravanHire, CARAVAN_HIRE_OPCODE, CARAVAN_HIRE_REQUEST_OPCODE, CARAVAN_LEAD_OPCODE } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/caravan-hire.ts" ).href
);

const json = body => new TextEncoder().encode( JSON.stringify( body ) );
const window = {
	version: 1,
	title: "Hunters and Traders for Hire - Jangan",
	fee: "15,000",
	message: "",
	rows: [
		{
			name: "[AI]Hunter_Kwan",
			display: "Kwan",
			role: "Hunter",
			town: "Jangan",
			available: true,
			status: "Available"
		},
		{
			name: "[AI]Trader_Mei",
			display: "Mei",
			role: "Trader",
			town: "Donwhang",
			available: false,
			status: "Waiting in Donwhang"
		}
	],
	stars: [ { stars: 1, goods: "100,000", total: "115,000" } ]
};

test("a window frame publishes its rows and star prices", () => {
	const hire = createCaravanHire();
	assert.equal( hire.receive( { opcode: CARAVAN_HIRE_OPCODE, payload: json( window ) } ), true );
	const state = hire.state();
	assert.equal( state.rows.length, 2 );
	assert.equal( state.rows[0].available, true );
	assert.equal( state.rows[1].status, "Waiting in Donwhang" );
	assert.equal( state.stars[0].total, "115,000" );
	hire.clear();
	assert.equal( hire.state(), null );
});

test("other opcodes are not consumed; a malformed body is a protocol error", () => {
	const hire = createCaravanHire();
	assert.equal( hire.receive( { opcode: 0x3fc0, payload: json( window ) } ), false );
	assert.throws(
		() => hire.receive( { opcode: CARAVAN_HIRE_OPCODE, payload: json( { ...window, version: 2 } ) } ),
		/Invalid caravan hire/
	);
	assert.throws(
		() =>
			hire.receive( {
				opcode: CARAVAN_HIRE_OPCODE,
				payload: json( { ...window, stars: [ { stars: 9, goods: "", total: "" } ] } )
			} ),
		/Invalid caravan hire/
	);
	assert.equal( hire.state(), null );
});

test("a hire request carries the operation, stars and name", () => {
	const frame = createCaravanHire().request( 1, 4, "[AI]Trader_Jin" );
	assert.equal( frame.opcode, CARAVAN_HIRE_REQUEST_OPCODE );
	assert.deepEqual( [ ...frame.payload.slice( 0, 2 ) ], [ 1, 4 ] );
	assert.equal( new TextDecoder().decode( frame.payload.slice( 2 ) ), "[AI]Trader_Jin" );
	assert.throws( () => createCaravanHire().request( 1, 6, "x" ), /Invalid caravan hire request/ );
});

test("the lead state names the leader and who may lead; an empty list clears it", () => {
	const hire = createCaravanHire();
	const lead = {
		version: 1,
		leader: "[AI]Trader_Jin",
		candidates: [ { name: "Boss", display: "Boss" }, { name: "[AI]Trader_Jin", display: "Jin" } ]
	};
	assert.equal( hire.receive( { opcode: CARAVAN_LEAD_OPCODE, payload: json( lead ) } ), true );
	assert.equal( hire.lead().leader, "[AI]Trader_Jin" );
	assert.equal( hire.lead().candidates[1].display, "Jin" );
	hire.clear();
	assert.notEqual( hire.lead(), null, "closing the hire window keeps the lead state" );
	hire.receive( { opcode: CARAVAN_LEAD_OPCODE, payload: json( { version: 1, leader: "", candidates: [] } ) } );
	assert.equal( hire.lead(), null );
	assert.throws(
		() => hire.receive( { opcode: CARAVAN_LEAD_OPCODE, payload: json( { ...lead, version: 2 } ) } ),
		/Invalid caravan lead/
	);
	const frame = hire.request( 2, 0, "[AI]Trader_Jin" );
	assert.deepEqual( [ ...frame.payload.slice( 0, 2 ) ], [ 2, 0 ] );
});
