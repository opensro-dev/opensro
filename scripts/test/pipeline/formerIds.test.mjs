/*
===========================================================================

formerIds.test.mjs - a moved monster's row names the ids it left

r203r moved the Arabia uniques off 50327-50331, the ids five pet scrolls
also shipped on (#3994). Their rows must name the old ids so the site and
the host's catalogue gate still find them.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";

import { fillFormerIds } from "../../build/catalogue/formerIds.mjs";

test("a moved monster names its former id and the others are untouched", () => {
	const monsters = [ { id: 51444, codename: "MOB_ARABIA_MUSTAFA" }, { id: 7, codename: "MOB_OTHER" } ];
	fillFormerIds( monsters, { MOB_ARABIA_MUSTAFA: [ 50331 ], MOB_GONE: [ 9 ] } );
	assert.deepEqual( monsters[0].formerIds, [ 50331 ] );
	assert.equal( "formerIds" in monsters[1], false );
});

test("a former id that is a current id, or that two monsters name, refuses", () => {
	assert.throws(
		() => fillFormerIds( [ { id: 50331, codename: "A" }, { id: 2, codename: "B" } ], { B: [ 50331 ] } ),
		/current monster id/
	);
	assert.throws(
		() => fillFormerIds( [ { id: 1, codename: "A" }, { id: 2, codename: "B" } ], { A: [ 9 ], B: [ 9 ] } ),
		/named by A and B/
	);
});
