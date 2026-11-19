/*
===========================================================================

market-notices.test.mjs - a consignment market event, everywhere at once

Port-only (#3529): a sale, an offer or an answer to one's offer pushed by
the server is the centre notice (banner), a green line in the
bottom-right message list and a (Notice) line in every chat tab.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { MARKET_CONTROL } = await import( "../../src/engine/foundation/gameplay/commerce-controls.ts" );
const { MARKET_NOTICE_ARGB } = await import( "../../src/engine/foundation/gameplay/market.ts" );
const { chatTabShows } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );

// The four native chat tabs (All, Party, Guild, Ally).
const NATIVE_TABS = [ 0, 1, 2, 3 ];

/*
================
eventFrame

A pushed market answer carrying events (action market.go marketEventFrame).
================
*/
function eventFrame( events ) {
	const json = JSON.stringify( { version: 2, ok: true, op: "event", events } );
	return { opcode: MARKET_CONTROL, payload: new TextEncoder().encode( json ) };
}

test("a market offer and a sale each raise the centre notice, a coloured list line and a chat notice", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {} );
	g.seed( { gid: 1, refObjId: 1, kind: "player", name: "Me", regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	g.take();
	const offer = { kind: "offer", item: "Drako Sword", currency: "gold", price: 900, who: "Lulu" };
	const sold = { kind: "sold", item: "Strom Bow", currency: "silk", price: 50, paid: 50, who: "" };
	assert.equal( g.receive( eventFrame( [ offer, sold ] ), 0 ), true );
	const state = defined( g.take() );
	const raised = defined( state.notices ).slice( -2 );
	assert.deepEqual( raised.map( notice => notice.text ), [
		"[Market] Lulu offers 900 Gold for your Drako Sword.",
		"[Market] Strom Bow sold for 50 Silk. You received 50 Silk after the fee."
	] );
	for ( const notice of raised ) {
		assert.equal( notice.banner, true, "the centre notice" );
		assert.equal( notice.colorArgb, MARKET_NOTICE_ARGB, "its own colour in the message list" );
	}
	const lines = defined( state.chat ).lines.filter( line => line.channel === 7 );
	assert.deepEqual( lines.map( line => line.text ), raised.map( notice => notice.text ) );
	for ( const line of lines ) {
		for ( const tab of NATIVE_TABS ) {
			assert.ok( chatTabShows( line.channel, tab, false, line.event, line.everyTab ), `tab ${tab}` );
		}
	}
});

test("a native notice stays on All; only a market notice is on every tab", () => {
	for ( const tab of NATIVE_TABS.slice( 1 ) ) {
		assert.ok( !chatTabShows( 7, tab, false, false ), `a native notice off tab ${tab}` );
		assert.ok( chatTabShows( 7, tab, false, false, true ), `a market notice on tab ${tab}` );
	}
	assert.ok( chatTabShows( 7, 4, true, false, true ), "the Event tab lists it too" );
});
