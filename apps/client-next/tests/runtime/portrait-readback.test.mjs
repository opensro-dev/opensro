/*
===========================================================================

portrait-readback.test.mjs - a portrait still whose device dies mid-map

The staging buffer is private to one read, so a map the browser
aborts is the device being torn down under it: the still ends as a
blank frame instead of an exception (#2560). Any other rejection is
a live failure and keeps travelling.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { readPortraitPixels } = await import(
	"../../src/engine/runtime/renderer/device/portrait-readback.ts"
);

/*
================
fixture

One fake device whose staging buffer's map the test settles by hand.
================
*/
function fixture( t ) {
	const previous = /** @type {[string, PropertyDescriptor | undefined][]} */ (
		[ "GPUBufferUsage", "GPUMapMode" ].map(
			k => [ k, Object.getOwnPropertyDescriptor( globalThis, k ) ]
		)
	);
	t.after( () => {
		for ( const [k, v] of previous ) {
			if ( v ) Object.defineProperty( globalThis, k, v );
			else delete globalThis[k];
		}
	} );
	globalThis.GPUBufferUsage = /** @type {any} */ ({ MAP_READ: 1, COPY_DST: 2 });
	globalThis.GPUMapMode = /** @type {any} */ ({ READ: 1 });
	const pendingMaps = [];
	const retired = [];
	const device = /** @type {any} */ ({
		/*
		================
		createBuffer
		================
		*/
		createBuffer() {
			return {
				/*
				================
				mapAsync
				================
				*/
				mapAsync() {
					return new Promise( ( resolve, reject ) => {
						pendingMaps.push( { resolve, reject } );
					} );
				},
				/*
				================
				getMappedRange

				One padded row: the texel, then the row's padding.
				================
				*/
				getMappedRange() {
					const bytes = new Uint8Array( 256 );
					bytes.set( [ 10, 20, 30, 255 ] );
					return bytes.buffer;
				},
				/*
				================
				unmap
				================
				*/
				unmap() {}
			};
		},
		/*
		================
		createCommandEncoder
		================
		*/
		createCommandEncoder() {
			return { copyTextureToBuffer() {}, finish: () => ({}) };
		},
		queue: { submit() {} }
	});
	const source = /** @type {any} */ ({
		label: "doll",
		texture: {},
		width: 1,
		height: 1,
		format: "rgba8unorm"
	});
	return {
		retired,
		/*
		================
		read
		================
		*/
		read() {
			return readPortraitPixels( device, source, buffer => retired.push( buffer ) );
		},
		/*
		================
		pendingMap
		================
		*/
		async pendingMap() {
			for ( let i = 0; i < 100 && !pendingMaps.length; i++ ) await Promise.resolve();
			assert.equal( pendingMaps.length, 1, "the read never reached its map" );
			return pendingMaps[0];
		}
	};
}

test("a completed map unpacks the texel and retires the staging buffer", async t => {
	const f = fixture( t );
	const answer = f.read();
	(await f.pendingMap()).resolve();
	const still = await answer;
	assert.deepEqual( [ still.width, still.height ], [ 1, 1 ] );
	assert.deepEqual( [ ...still.rgba ], [ 10, 20, 30, 255 ] );
	assert.equal( f.retired.length, 1 );
});

test("a map aborted by device teardown ends as a blank still", async t => {
	const f = fixture( t );
	const answer = f.read();
	(await f.pendingMap()).reject( Object.assign( Error( "Buffer unmapped" ), { name: "AbortError" } ) );
	const still = await answer;
	assert.deepEqual( [ still.width, still.height ], [ 1, 1 ] );
	assert.deepEqual( [ ...still.rgba ], [ 0, 0, 0, 0 ] );
	assert.equal( f.retired.length, 1 );
});

test("a live map failure is reported, not blanked", async t => {
	const f = fixture( t );
	const answer = f.read();
	(await f.pendingMap()).reject( Error( "device exploded" ) );
	await assert.rejects( answer, /device exploded/ );
	assert.equal( f.retired.length, 1 );
});
