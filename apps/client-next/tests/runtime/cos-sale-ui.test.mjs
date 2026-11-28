/*
===========================================================================

cos-sale-ui.test.mjs - the active transport's portrait dropped on a store

Drives the production HUD: the COS status portrait carried onto the open
store runs 5B6CA0's refusals, a stable keeper opens the sale box (6A9F50),
and its Sell sends the cos-sell command the worker turns into 0x71DC.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readServedBytes, uiFixture } from "../helpers/ui-fixture.mjs";

const CATALOG = "/assets/data/cosPresentation.json";
// COS_T_DONKEY in the published catalog; its SellPrice is supplied here
// so the test does not wait for the shared tree to rebuild.
const DONKEY = 2183, PRICE = 240, NPC = 42, COS = 9;
const STABLE = 0x400 | 1;

/*
================
saleFixture

A world with an open store at NPC and one active transport COS.
================
*/
function saleFixture( capabilities ) {
	const catalog = JSON.parse( readServedBytes( CATALOG ).toString( "utf-8" ) );
	const published = { [CATALOG]: Buffer.from( JSON.stringify( { ...catalog, sale: { [DONKEY]: PRICE } } ) ) };
	const commands = [],
		f = uiFixture(
			c => commands.push( c ),
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			undefined,
			{},
			published
		);
	const record = { gid: COS, refObjId: DONKEY, band: 2, hp: 100, mp: 0, status: 0, dead: false, inventory: [] };
	const shop = { npc: NPC, name: "Stable", offers: [], buyback: [] };
	let now = 0;
	const step = () => f.ui.step( f.state, now += 50 );
	const set = patch => {
		f.state.gameplay = { ...f.state.gameplay, ...patch };
		return step();
	};
	set( {
		target: NPC,
		targetCapabilities: capabilities,
		inventorySlotCount: 45,
		cosRecords: [ record ],
		activeCos: { gid: COS, refObjId: DONKEY, hp: 100, mp: 0, status: 0, dead: false }
	} );
	f.ui.event( { kind: "activate", id: "shop-open" } );
	set( { shop, shopCompletionRevision: 1 } );
	return { f, commands, step };
}

/*
================
dropPortrait
================
*/
function dropPortrait( fixture ) {
	const controls = fixture.f.presented()?.controls ?? [];
	const shopControl = controls.find( c => c.id.startsWith( "shop-" ) );
	assert.ok( controls.some( c => c.id === "cos-status:" + COS ), "the transport's portrait is shown" );
	assert.ok( shopControl, "the store is open" );
	fixture.commands.length = 0;
	fixture.f.ui.event( { kind: "drag", id: "cos-status:" + COS, dx: 50, dy: 0 } );
	fixture.f.ui.event( {
		kind: "drag-end",
		id: "cos-status:" + COS,
		x: shopControl.rect[0] + 2,
		y: shopControl.rect[1] + 2
	} );
	return fixture.step();
}

test("a stable keeper's store opens the sale box and its Sell sends cos-sell", () => {
	const fixture = saleFixture( STABLE );
	try {
		dropPortrait( fixture );
		const sell = fixture.f.presented()?.controls.find( c => c.id === "cos-sale-trade" );
		assert.ok( sell, "the sale box shows its Sell button" );
		assert.ok( fixture.f.hasText( "240" ), "the box prints the row's SellPrice" );
		fixture.f.ui.event( { kind: "activate", id: "cos-sale-trade" } );
		fixture.step();
		assert.deepEqual( fixture.commands.filter( c => c.kind === "gameplay" ).map( c => c.command ), [ {
			kind: "cos-sell",
			npc: NPC
		} ] );
		assert.equal( fixture.f.presented()?.controls.some( c => c.id === "cos-sale-trade" ), false );
	} finally {
		fixture.f.dispose();
	}
});

test("a store without the transport-dealer bit refuses with DEALER_DONT_TREAT_THIS_ITEM", () => {
	const fixture = saleFixture( 1 );
	try {
		dropPortrait( fixture );
		const sent = fixture.commands.filter( c => c.kind === "gameplay" ).map( c => c.command );
		assert.equal( sent.length, 1 );
		assert.equal( sent[0].kind, "local-notice" );
		assert.equal( fixture.f.presented()?.controls.some( c => c.id === "cos-sale-trade" ), false );
	} finally {
		fixture.f.dispose();
	}
});
