/*
===========================================================================

trade-progress.test.mjs - the caravan's published distance to the next star

Port-only (#3163): under SRO_TRADE_TIERS the server publishes the
cargo's stars and the gold short of the next star with the star
answer; the worker keeps the latest answer for the bag and the
specialty deal window. No frame ever arrives with the flag off.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createTradeProgress, OP_TRADE_PROGRESS } = await import(
	sourceFileUrl(
		"src/engine/runtime/simulation/worker/session/world/gameplay/trade-progress/trade-progress.ts"
	).href
);

function body( stars, gap ) {
	const payload = new Uint8Array( 5 );
	payload[0] = stars;
	new DataView( payload.buffer ).setUint32( 1, gap, true );
	return payload;
}

test("a progress frame publishes its stars and gap", () => {
	const progress = createTradeProgress();
	assert.equal( progress.state(), null );
	assert.equal( progress.receive( { opcode: OP_TRADE_PROGRESS, payload: body( 3, 2424 ) } ), true );
	assert.deepEqual( progress.state(), { stars: 3, gap: 2424 } );
	// The next answer replaces the last, in the same step as its star.
	assert.equal( progress.receive( { opcode: OP_TRADE_PROGRESS, payload: body( 5, 0 ) } ), true );
	assert.deepEqual( progress.state(), { stars: 5, gap: 0 } );
});

test("other opcodes are not consumed and move nothing", () => {
	const progress = createTradeProgress();
	assert.equal( progress.receive( { opcode: 0x352d, payload: new Uint8Array( [ 3 ] ) } ), false );
	assert.equal( progress.receive( { opcode: 0x3fbf, payload: body( 3, 1 ) } ), false );
	assert.equal( progress.state(), null );
});

test("a malformed body is a protocol error and keeps the last answer", () => {
	const progress = createTradeProgress();
	progress.receive( { opcode: OP_TRADE_PROGRESS, payload: body( 2, 7 ) } );
	for ( const payload of [ new Uint8Array( 0 ), new Uint8Array( 4 ), new Uint8Array( 6 ) ] ) {
		assert.throws( () => progress.receive( { opcode: OP_TRADE_PROGRESS, payload } ), /Invalid trade progress/ );
	}
	assert.deepEqual( progress.state(), { stars: 2, gap: 7 } );
});

test("clear drops the answer until the next publish", () => {
	const progress = createTradeProgress();
	progress.receive( { opcode: OP_TRADE_PROGRESS, payload: body( 1, 100 ) } );
	progress.clear();
	assert.equal( progress.state(), null );
});
