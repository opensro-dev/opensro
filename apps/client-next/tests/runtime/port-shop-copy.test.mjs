/*
===========================================================================

port-shop-copy.test.mjs - tests for port-shop-copy.ts

Jangan's degree 10 stones tab (server simulation/npcjanganshop.go) names
its label by a symbol no v1.150 text row carries; the port copy spells
it, and every other symbol falls through to the text tables. Loads the
TypeScript sources through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { portShopTabCopy } = await import( "../../src/engine/foundation/ui/port-shop-copy.ts" );

test("the degree 10 stones tab reads 10th degree", () => {
	assert.equal( portShopTabCopy( "UIIT_PORT_ALCHEMY_D10_STONES" ), "10th degree" );
});

test("other symbols fall through to the text tables", () => {
	for ( const symbol of [ "SN_TAB_ALCHEMY", "UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE", "", "toString", "__proto__" ] ) {
		assert.equal( portShopTabCopy( symbol ), "", symbol );
	}
});
