/*
===========================================================================

item-mall-chrome.test.mjs - the Item Mall's native hidden and empty controls

CIFItemMallMyInfo_OnCreate (6BDA60) hides the money sign, and on an English
client the Point row; CIFItemMallShopSlot_ApplyVisibilityAndButtonStates
(6C9420) keeps an empty slot's background. Driven through the production
HUD.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";

/*
================
textureCounts

How many quads of the last drawn scene use each texture, by file name.
================
*/
function textureCounts( f ) {
	const counts = new Map();
	for ( const quad of f.scenes.findLast( scene => scene )?.quads ?? [] ) {
		if ( typeof quad.texture !== "string" || !quad.texture ) continue;
		const name = quad.texture.split( "/" ).pop();
		counts.set( name, (counts.get( name ) ?? 0) + 1 );
	}
	return counts;
}

/*
================
settle
================
*/
function settle( f, from ) {
	for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, from + i * 50 );
}

test("My info hides the money sign and the English Point row", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 1000 );
		f.ui.event( { kind: "key", code: "F10" } );
		settle( f, 1100 );
		const counts = textureCounts( f );
		assert.equal( counts.get( "mall_moneybutton.png" ), undefined, "6BDA60 hides control 5" );
		// Silk and gift silk keep their boxes; the Point row's box (17) is hidden.
		assert.equal( counts.get( "mall_blackbox01.png" ), 2 );
	} finally {
		f.dispose();
	}
});

test("a shop page keeps all six slot backgrounds when it has no offers", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 1000 );
		f.ui.event( { kind: "key", code: "F10" } );
		settle( f, 1100 );
		f.ui.event( { kind: "activate", id: "item-mall-category:1" } );
		settle( f, 3000 );
		assert.equal( textureCounts( f ).get( "mall_bar01.png" ), 6 );
	} finally {
		f.dispose();
	}
});
