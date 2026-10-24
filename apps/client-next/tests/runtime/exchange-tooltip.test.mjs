/*
===========================================================================

exchange-tooltip.test.mjs - hovering an item on the exchange table

Thread 1561461009862885478: check whether the exchange window shows item
tooltips, as the original's bound slots do. The resolver already resolves
both exchange prefixes; this drives the whole UI: an open exchange, a
hover on each side's slot, and the tooltip text on screen.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
offer
================
*/
function offer( slot, name, icon ) {
	return {
		slot,
		item: { slot: 14, refObjId: 1, quantity: 1, name, icon, typeFlags: 0x6c, magic: [] }
	};
}

test("hovering either side of the exchange table shows the item's tooltip", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	let now = 0;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
	};
	// The tooltip title may use the plain or the emphasized face.
	const shown = name => [ 0, 2 ].some( style => f.hasText( name, "0", style ) );
	step();
	f.state.gameplay = {
		...f.state.gameplay,
		exchange: {
			open: true,
			partner: 2,
			own: [ offer( 0, "My Sword", "item/etc/hp_potion_01.ddj" ) ],
			theirs: [ offer( 0, "Their Potion", "item/etc/hp_potion_01.ddj" ) ],
			ownGold: 0,
			theirGold: 0,
			ownLocked: false,
			theirLocked: false,
			approved: false,
			requesting: false
		}
	};
	step();
	f.ui.event( { kind: "hover", id: "exchange-my:0" } );
	step();
	assert.ok( shown( "My Sword" ), "the own item's tooltip" );
	assert.equal( shown( "Their Potion" ), false, "only the hovered item's" );
	f.ui.event( { kind: "hover", id: "exchange-their:0" } );
	step();
	assert.ok( shown( "Their Potion" ), "the partner item's tooltip" );
	assert.equal( shown( "My Sword" ), false, "only the hovered item's" );
});
