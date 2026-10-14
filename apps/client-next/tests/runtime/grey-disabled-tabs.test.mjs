/*
===========================================================================

grey-disabled-tabs.test.mjs - a busy tab never takes the grey look

The Experimental row Grey disabled tabs dims a tab that is unavailable
(port-only). A tab that only waits for the server, the shop's tabs while a
buy or sell is pending, refuses input and keeps the original look: the
request owner sets that reason, so no timing can turn it grey.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

/*
================
shopFixture

An armor merchant with one branch of three tabs, opened through its menu.
================
*/
function shopFixture() {
	const f = uiFixture( () => {} );
	const game = f.state.gameplay;
	game.target = 17;
	game.targetCapabilities = 3;
	game.inventorySlotCount = 109;
	game.equipmentSlotCount = 13;
	game.progression = { gold: "5000", masteries: [] };
	game.npcConversation = { phase: "menu", gid: 17 };
	f.state.entities.push( {
		...f.state.entities[0],
		gid: 17,
		kind: "npc",
		name: "Armor merchant",
		merchantBranches: [ { id: 850, labelSymbol: "SN_STORE_ARMOR_GROUP1", tabs: [ 0, 1, 2 ] } ]
	} );
	game.shop = {
		npc: 17,
		name: "Armor merchant",
		tabs: Array.from( { length: 3 }, ( _, i ) => ({ index: i, labelSymbol: "SN_TAB_HEAVYARMOR" }) ),
		offers: [ { tab: 0, slot: 0, refObjId: 3630, name: "Item", price: "60", maxStack: 50 } ]
	};
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
	let now = 100;
	const settle = () => {
		for ( let i = 0; i < 16; i++ ) semantics = f.ui.step( f.state, now += 100 ) ?? semantics;
		return defined( semantics );
	};
	return { f, game, settle };
}

/*
================
tabLook

The plate alpha and label colour drawn inside one tab control.
================
*/
function tabLook( f, semantics, id ) {
	const tab = semantics.controls.find( c => c.id === id );
	assert.ok( tab, id );
	const inside = q =>
		q.rect[0] >= tab.rect[0] && q.rect[1] >= tab.rect[1] && q.rect[0] < tab.rect[0] + tab.rect[2] &&
		q.rect[1] < tab.rect[1] + tab.rect[3];
	const quads = f.scenes.at( -1 ).quads;
	const plate = quads.find( q => q.texture.includes( "/com_short_tab_" ) && inside( q ) );
	const label = quads.find( q => q.texture.endsWith( "/native-ui-font-atlas.png" ) && inside( q ) );
	assert.ok( plate && label, id + " plate and label" );
	return { disabled: !!tab.disabled, plate: plate.color, label: label.color };
}

/*
================
pending trade keeps the native tab look
================
*/
test("a pending trade locks the shop tabs without greying them, with the row on", () => {
	const { f, game, settle } = shopFixture();
	try {
		f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { greyDisabledTabs: true } ) } );
		settle();
		f.ui.event( { kind: "activate", id: "shop-group:850" } );
		game.shopCompletionRevision = 1;
		const idle = tabLook( f, settle(), "shop-tab:1" );
		assert.equal( idle.disabled, false );
		// A new gameplay snapshot, as the worker publishes one.
		f.state.gameplay = { ...game, inventoryPending: true, revision: (game.revision ?? 0) + 1 };
		const busy = tabLook( f, settle(), "shop-tab:1" );
		assert.equal( busy.disabled, true, "a pending trade refuses tab clicks" );
		assert.deepEqual( busy.plate, idle.plate, "the busy plate is the original's" );
		assert.deepEqual( busy.label, idle.label, "the busy label is the original's" );
		assert.equal( busy.plate[3], 1 );
	} finally {
		f.dispose();
	}
});
