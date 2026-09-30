/*
===========================================================================

convertImagesRunner.test.mjs - convert_images.py call serialization

Runs the runner with a fake conversion: calls never overlap, a successful
unfiltered pass makes later filtered passes redundant, and a failed or
filtered pass does not.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { createConvertImagesRunner } from "../../build/shared/convertImagesRunner.mjs";

/*
================
fakeConversion

Records each spawned pass and whether two passes ever overlapped.
================
*/
function fakeConversion( status = () => 0 ) {
	const passes = [];
	let running = 0;
	let overlapped = false;
	const spawn = async ( args ) => {
		running++;
		if ( running > 1 ) overlapped = true;
		passes.push( args );
		await new Promise( ( resolve ) => setTimeout( resolve, 5 ) );
		running--;
		return { status: status( args ) };
	};
	return { passes, spawn, overlapped: () => overlapped };
}

test("concurrent calls run one at a time, in call order", async () => {
	const conversion = fakeConversion();
	const run = createConvertImagesRunner( conversion.spawn );
	await Promise.all( [ run( [ "a" ] ), run( [ "b" ] ), run( [ "c" ] ) ] );
	assert.deepEqual( conversion.passes, [ [ "a" ], [ "b" ], [ "c" ] ] );
	assert.equal( conversion.overlapped(), false );
});

test("a successful unfiltered pass makes later filtered passes redundant", async () => {
	const conversion = fakeConversion();
	const run = createConvertImagesRunner( conversion.spawn );
	assert.deepEqual( await run( [] ), { status: 0 } );
	assert.deepEqual( await run( [ "Map_extracted" ] ), { status: 0 } );
	assert.deepEqual( conversion.passes, [ [] ] );
});

test("a failed unfiltered pass does not suppress filtered passes", async () => {
	const conversion = fakeConversion( ( args ) => args.length === 0 ? 1 : 0 );
	const run = createConvertImagesRunner( conversion.spawn );
	assert.deepEqual( await run( [] ), { status: 1 } );
	await run( [ "Map_extracted" ] );
	assert.deepEqual( conversion.passes, [ [], [ "Map_extracted" ] ] );
});

test("a filtered pass never counts as the full pass", async () => {
	const conversion = fakeConversion();
	const run = createConvertImagesRunner( conversion.spawn );
	await run( [ "Media_extracted" ] );
	await run( [ "Map_extracted" ] );
	assert.deepEqual( conversion.passes, [ [ "Media_extracted" ], [ "Map_extracted" ] ] );
});
