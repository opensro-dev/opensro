/*
===========================================================================

asyncLimiter.test.mjs - one budget shared by work started from many places

The pack builder starts every group at once and bounds the packs in flight
with one createLimiter: never more than the limit running, every task run,
results and failures delivered to their own caller.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { createLimiter } from "../../build/shared/asyncUtils.mjs";

test("no more than the limit runs at once, and every task runs", async () => {
	const run = createLimiter( 3 );
	let running = 0, peak = 0;
	const results = await Promise.all( Array.from( { length: 20 }, ( _, i ) =>
		run( async () => {
			running++;
			peak = Math.max( peak, running );
			await new Promise( resolve => setTimeout( resolve, 2 ) );
			running--;
			return i;
		} ) ) );
	assert.equal( peak, 3 );
	assert.deepEqual( results, Array.from( { length: 20 }, ( _, i ) => i ) );
});

test("a failing task rejects only its own caller and frees its slot", async () => {
	const run = createLimiter( 1 );
	const failed = run( async () => {
		throw new Error( "pack failed" );
	} );
	const next = run( async () => "next" );
	await assert.rejects( failed, /pack failed/ );
	assert.equal( await next, "next" );
});
