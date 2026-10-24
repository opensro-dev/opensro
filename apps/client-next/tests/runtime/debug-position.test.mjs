/*
===========================================================================

debug-position.test.mjs - the /Pos word parser (690C40 case 2)

The single argument word is comma-split: a swscanf "%x" region token,
then three CRT_atof coordinates. Missing or unreadable tokens read 0,
extra words are ignored, and no word at all runs nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { debugPosition } = await import(
	"../../src/engine/foundation/ui/debug-commands.ts"
);

test("a full word parses as a hex region and three coordinates", () => {
	assert.deepEqual( debugPosition( [ "61A8,100,200,300" ] ), {
		regionId: 0x61a8,
		x: 100,
		y: 200,
		z: 300
	} );
	assert.deepEqual( debugPosition( [ "0x61a8,-12.5,1.5e2,0" ] ), {
		regionId: 0x61a8,
		x: -12.5,
		y: 150,
		z: 0
	} );
});

test("the region is the destination's u16 field", () => {
	assert.equal( debugPosition( [ "1FFFF,1,2,3" ] )?.regionId, 0xffff );
	assert.equal( debugPosition( [ "-1,1,2,3" ] )?.regionId, 0xffff );
	// swscanf %x stops at the first non-hex digit.
	assert.equal( debugPosition( [ "61A8zz,1,2,3" ] )?.regionId, 0x61a8 );
});

test("missing and unreadable tokens read as zero", () => {
	assert.deepEqual( debugPosition( [ "61A8,100" ] ), { regionId: 0x61a8, x: 100, y: 0, z: 0 } );
	assert.deepEqual( debugPosition( [ "61A8" ] ), { regionId: 0x61a8, x: 0, y: 0, z: 0 } );
	assert.deepEqual( debugPosition( [ "zz,abc,," ] ), { regionId: 0, x: 0, y: 0, z: 0 } );
	assert.deepEqual( debugPosition( [ "" ] ), { regionId: 0, x: 0, y: 0, z: 0 } );
});

test("no word runs nothing, and only the first word is read", () => {
	assert.equal( debugPosition( [] ), null );
	assert.deepEqual( debugPosition( [ "61A8,1,2,3", "FFFF,9,9,9" ] ), {
		regionId: 0x61a8,
		x: 1,
		y: 2,
		z: 3
	} );
});
