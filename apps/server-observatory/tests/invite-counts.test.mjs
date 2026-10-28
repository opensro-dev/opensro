/*
===========================================================================
invite-counts.test.mjs - the readings of the website's filter counts

The website counts each status class under every filter except status,
and the total under every filter. Choosing Unused over 13 keys (3 of
them) must still label All with 13 - opensro-review caught the label
using the total (3). These pins hold the readings to the contract.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { allStatusCount, openCount } from "../public/invite-counts.js";

// The website's answer while Unused is chosen over 13 keys.
const COUNTS = {
	total: 3,
	status: { unused: 3, partial: 2, used: 5, revoked: 2, expired: 1 },
	sources: [ { source: "reddit", count: 8 } ]
};

test("All counts the keys without the status filter, not the filtered total", () => {
	assert.equal( allStatusCount( COUNTS ), 13 );
});

test("open keys follow the chosen status class", () => {
	assert.equal( openCount( COUNTS, "" ), 5 );
	assert.equal( openCount( COUNTS, "unused" ), 3 );
	assert.equal( openCount( COUNTS, "partial" ), 3 );
	for ( const closed of [ "used", "revoked", "expired" ] ) {
		assert.equal( openCount( COUNTS, closed ), 0, closed );
	}
});
