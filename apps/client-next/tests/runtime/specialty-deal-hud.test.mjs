/*
===========================================================================

specialty-deal-hud.test.mjs - the trade goods window's request loop

CIFSpecialtyDeal_RequestTradeGoodsMove (64A660): one request of at most a
stack at a time until the target has moved; a request that moves nothing,
or that the worker never takes within 5000 ms, ends the deal.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createSpecialtyDealHud, DEAL_REQUEST_TIMEOUT_MS } = await import(
	sourceFileUrl( "src/engine/runtime/ui/hud/specialty-deal-hud.ts" ).href
);

/*
================
openDeal
================
*/
function openDeal( count, stack = 10 ) {
	const hud = createSpecialtyDealHud();
	hud.open( {
		mode: "buy",
		selection: { kind: "buy", npc: 1, tab: 0, slot: 0, binding: "x", cosGid: 7 },
		name: "Silk",
		refObjId: 9,
		cosGid: 7,
		unitBuy: 100,
		stack,
		count
	} );
	return hud;
}

test("a multi-stack purchase sends one stack at a time until the target moved", () => {
	const hud = openDeal( 25 );
	assert.deepEqual( hud.confirm( 0, 0 ), { chunk: 10, progress: true } );
	assert.equal( hud.observe( false, 0, 10 ), null, "waits for the worker to take the request" );
	assert.equal( hud.observe( true, 0, 20 ), null );
	assert.equal( hud.observe( false, 10, 30 ), 10, "the first stack settled; send the second" );
	assert.equal( hud.observe( true, 10, 40 ), null );
	assert.equal( hud.observe( false, 20, 50 ), 5, "the last request is what is left" );
	assert.equal( hud.observe( true, 20, 60 ), null );
	assert.equal( hud.observe( false, 25, 70 ), 0, "the target moved: the deal closes" );
	assert.equal( hud.state(), null );
});

test("a request that moves nothing, or is never taken, ends the deal", () => {
	const refused = openDeal( 25 );
	refused.confirm( 4, 0 );
	refused.observe( true, 4, 10 );
	assert.equal( refused.observe( false, 4, 20 ), 0, "the server refused: nothing moved" );
	assert.equal( refused.state(), null );
	const stalled = openDeal( 25 );
	stalled.confirm( 0, 1000 );
	assert.equal( stalled.observe( false, 0, 1000 + DEAL_REQUEST_TIMEOUT_MS - 1 ), null );
	assert.equal( stalled.observe( false, 0, 1000 + DEAL_REQUEST_TIMEOUT_MS ), 0, "64A660's 5000 ms timer" );
});

test("a sale's stock goes down; one stack needs no progress notice", () => {
	const hud = openDeal( 8, 8 );
	assert.deepEqual( hud.confirm( 8, 0 ), { chunk: 8, progress: false } );
	hud.observe( true, 8, 1 );
	assert.equal( hud.observe( false, 0, 2 ), 0 );
});

test("the typed count is digits only, capped at the edit's limit, and nothing confirms nothing", () => {
	const hud = openDeal( 10 );
	hud.type( "12a3", 50 );
	assert.equal( hud.count(), 50 );
	hud.type( "", 50 );
	assert.equal( hud.count(), 0 );
	assert.equal( hud.confirm( 0, 0 ), null, "64A8F0: a zero count closes the window" );
	assert.equal( hud.state(), null );
});
