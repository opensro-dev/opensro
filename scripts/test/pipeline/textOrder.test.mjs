/*
===========================================================================

textOrder.test.mjs - the build's text order is pinned, not the host's

Packs and indexes list entries in compareText order, and the order is in
their bytes. The order must be en-US collation (the order every published
pack was built in), whatever the host's default locale: under tr-TR the
dotted and dotless i sort differently, and a plain codepoint sort would
move 12,996 of the 42,558 published paths (measured 11-16), repacking
every group.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { compareText, TEXT_ORDER_LOCALE } from "../../build/shared/textOrder.mjs";

// Shapes the asset paths hold: case, "_" against "-", dots, digits, accents.
const SHUFFLED = [ "z", "a.b", "B", "10", "a-b", "é", "A", "9", "a_b", "Z", "e", "a" ];
const EN_US_ORDER = [ "10", "9", "a", "A", "a_b", "a-b", "a.b", "B", "e", "é", "z", "Z" ];

test("compareText sorts in the pinned en-US collation", () => {
	assert.equal( TEXT_ORDER_LOCALE, "en-US" );
	assert.deepEqual( [ ...SHUFFLED ].sort( compareText ), EN_US_ORDER );
});

test("the pinned order is neither codepoint order nor a locale that orders i differently", () => {
	assert.notDeepEqual( [ ...SHUFFLED ].sort(), EN_US_ORDER );
	const turkish = new Intl.Collator( "tr-TR" );
	const letters = [ "ı", "I", "i", "İ" ];
	assert.notDeepEqual( [ ...letters ].sort( turkish.compare ), [ ...letters ].sort( compareText ) );
});
