/*
===========================================================================

bsrAuthoredBox.test.mjs - the authored resource box every pick starts from

CResObject_LoadFromArchive (client A4FF00) seeks to BSR header offset[7],
reads the counted primary-mesh path, then box1 (+0x280) and box2 (+0x298).
box1 of the base resource is the native pick box. Pins the parse on retail
bodies and a weapon, against values read from the files by hand.

Needs the extracted client data.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { parseJmxResourceBsr } from "../../build/world/objects/formats.mjs";
import { loadDataAsset } from "../../build/shared/jmxAssetIO.mjs";

const BOXES = [
	[ "res/char/china/chinaman_adventurer.bsr", 0, [ -9.128, -0.005, -1.970, 9.128, 18.119, 1.199 ] ],
	[ "res/item/china/weapon/sword_05.bsr", 5, [ -0.899, -0.287, -9.600, 0.614, 0.129, 1.437 ] ]
];

/*
================
rounded
================
*/
function rounded( box ) {
	return box.map( ( v ) => Math.round( v * 1000 ) / 1000 );
}

test("box1 and the resource kind come from the authored BSR", async () => {
	for ( const [path, kind, box] of BOXES ) {
		const bsr = parseJmxResourceBsr( await loadDataAsset( path ), path );
		assert.equal( bsr.metadata.kind0, kind, path );
		assert.deepEqual( rounded( bsr.aggregateBox ), box, path );
	}
});
