/*
===========================================================================

pk-status.test.mjs - the local PK counters and the mini-info PK tooltip

The bootstrap seeds daily/total/penalty from the character's PK record,
0x33C4 / 0x3647 / 0x30F2 replace one each, and the HUD shows GDR_PMI_PK
with 6B5150's two lines only while one counter is non-zero.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { pkStatusBootstrap, pkStatusPacket, pkStatusTooltip, PK_DAILY_LIMIT } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/pk-status.ts" ).href
);

const TEXT = {
	UIIT_TOOLTIP_PK_DAY: "Daily PK %d / %d",
	UIIT_TOOLTIP_PK_PENALTY_TIME: "murderer level %d (%d level)"
};

/*
================
frame
================
*/
function frame( opcode, ...bytes ) {
	return { opcode, payload: Uint8Array.from( bytes ) };
}

test("the bootstrap seeds the counters, and a character without a record is all zero", () => {
	assert.deepEqual( pkStatusBootstrap( { character: { pk: { dailyCount: 3, totalCount: 7, penalty: 1200 } } } ), {
		daily: 3,
		total: 7,
		penalty: 1200
	} );
	assert.deepEqual( pkStatusBootstrap( { character: {} } ), { daily: 0, total: 0, penalty: 0 } );
	assert.throws( () => pkStatusBootstrap( { character: { pk: { dailyCount: 256 } } } ), /Invalid PK record/ );
	assert.throws( () => pkStatusBootstrap( { character: { pk: { penalty: -1 } } } ), /Invalid PK record/ );
});

test("each counter packet replaces its own counter", () => {
	const start = { daily: 1, total: 2, penalty: 3 };
	assert.deepEqual( pkStatusPacket( start, frame( 0x33c4, 9 ) ), { daily: 9, total: 2, penalty: 3 } );
	assert.deepEqual( pkStatusPacket( start, frame( 0x3647, 0x34, 0x12 ) ), { daily: 1, total: 0x1234, penalty: 3 } );
	assert.deepEqual( pkStatusPacket( start, frame( 0x30f2, 0x78, 0x56, 0x34, 0x12 ) ), {
		daily: 1,
		total: 2,
		penalty: 0x12345678
	} );
	assert.equal( pkStatusPacket( start, frame( 0x3013, 0 ) ), null );
	assert.throws( () => pkStatusPacket( start, frame( 0x3647, 1 ) ), /Invalid PK total/ );
});

test("6B5150: hidden while all zero; daily against the limit, then penalty and total", () => {
	const text = symbol => TEXT[symbol] ?? "";
	assert.equal( pkStatusTooltip( { daily: 0, total: 0, penalty: 0 }, text ), null );
	assert.equal(
		pkStatusTooltip( { daily: 2, total: 5, penalty: 300 }, text ),
		`Daily PK 2 / ${PK_DAILY_LIMIT}\nmurderer level 300 (5 level)`
	);
	// Any single non-zero counter shows the control.
	assert.ok( pkStatusTooltip( { daily: 0, total: 1, penalty: 0 }, text ) );
});

test("the mini-info shows GDR_PMI_PK with the native tooltip only while a counter is set", () => {
	const f = uiFixture( () => {} );
	try {
		f.state.gameplay = { ...f.state.gameplay, pkStatus: { daily: 2, total: 5, penalty: 300 } };
		let shown;
		for ( let time = 0; time < 1200; time += 100 ) shown = f.ui.step( f.state, time ) ?? shown;
		const pk = shown?.controls.find( c => c.id === "GDR_PMI_PK" );
		assert.ok( pk, "GDR_PMI_PK is shown" );
		assert.equal( pk.helpText, "Daily PK 2 / 15\nmurderer level 300 (5 level)" );
		// The worker publishes a new state object on every change.
		f.state = { ...f.state, gameplay: { ...f.state.gameplay, pkStatus: { daily: 0, total: 0, penalty: 0 } } };
		let hidden;
		for ( let time = 1200; time < 1500; time += 100 ) hidden = f.ui.step( f.state, time ) ?? hidden;
		assert.equal( hidden?.controls.some( c => c.id === "GDR_PMI_PK" ), false );
	} finally {
		f.dispose();
	}
});
