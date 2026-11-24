/*
===========================================================================

monsterPortraitFallback.test.mjs - the portrait family without a GPU

A machine whose browser cannot draw (no WebGPU adapter, #4015) must not
stop the full build: the viewer starts only for a still the cache cannot
serve, and when it cannot draw at all the row reuses the still the
previous manifest published, or joins the missing models. A failure
after a drawn still keeps the old contract - it names its row and stops.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { collectPortraitRows, portraitManifestIndex } from "../../build/char/buildMonsterPortraitAssets.mjs";

const PNG = Buffer.from( "still bytes, long enough to be a portrait still" );

/*
================
row

One drawable reference, refObjId 1933 first because the build walks the
references in id order.
================
*/
function row( refObjId ) {
	return { refObjId, glb: "/assets/npc/char/china/mangnyang.glb" };
}

/*
================
ports

The three still sources as counters; each test overrides the one it
exercises. drawn answers PNG, cached and publishedBytes answer null.
================
*/
function ports( overrides = {} ) {
	const calls = { cached: 0, draw: 0, publishedBytes: 0 };
	return {
		calls,
		async cached() {
			calls.cached++;
			return overrides.cached ? overrides.cached() : null;
		},
		async draw() {
			calls.draw++;
			if ( overrides.draw ) return overrides.draw( calls.draw );
			return PNG;
		},
		async publishedBytes() {
			calls.publishedBytes++;
			return overrides.publishedBytes ? overrides.publishedBytes() : null;
		}
	};
}

test("a cached still never asks the viewer or the published set", async () => {
	const p = ports( { cached: () => PNG } );
	const got = await collectPortraitRows( [ row( 1933 ) ], p );
	assert.deepEqual( p.calls, { cached: 1, draw: 0, publishedBytes: 0 } );
	assert.equal( got.outcomes[0].source, "cache" );
	assert.equal( got.drawn, 0 );
	assert.equal( got.down, null );
});

test("the first draw failure falls back to the published still and latches", async () => {
	const p = ports( {
		draw() {
			throw Error( "No available adapters" );
		},
		publishedBytes: () => PNG
	} );
	const got = await collectPortraitRows( [ row( 1933 ), row( 1934 ) ], p );
	assert.equal( p.calls.draw, 1, "the viewer is not asked again once it cannot draw" );
	assert.deepEqual(
		got.outcomes.map( outcome => outcome.source ),
		[ "published", "published" ]
	);
	assert.equal( got.reused, 2 );
	assert.equal( got.down.message, "No available adapters" );
	assert.deepEqual( got.missing, [] );
});

test("a row with no published still joins the missing models", async () => {
	const p = ports( {
		draw() {
			throw Error( "browser launch failed" );
		}
	} );
	const got = await collectPortraitRows( [ row( 1933 ), { refObjId: 2000, glb: undefined } ], p );
	assert.deepEqual( got.outcomes, [] );
	assert.deepEqual( got.missing, [ 1933, 2000 ] );
	assert.equal( got.reused, 0 );
});

test("a draw failure after a drawn still stays fatal", async () => {
	const failure = Error( "Monster portrait 1934 failed: asset-load" );
	const p = ports( {
		draw( call ) {
			if ( call === 2 ) throw failure;
			return PNG;
		},
		publishedBytes: () => PNG
	} );
	await assert.rejects( () => collectPortraitRows( [ row( 1933 ), row( 1934 ) ], p ), error => error === failure );
	assert.equal( p.calls.publishedBytes, 0, "a working viewer never falls back" );
});

test("the published index keeps only the family's own well-formed rows", () => {
	const index = portraitManifestIndex( {
		rows: [
			[ 1933, "/assets/npc/hunting-portraits/aaa.png" ],
			[ "1934", "/assets/npc/hunting-portraits/bbb.png" ],
			[ 1935, "/assets/npc/other/ccc.png" ],
			[ 1936, "hunting-portraits/ddd.png" ],
			[ 1937 ],
			"junk",
			[ 1933, "/assets/npc/hunting-portraits/zzz.png" ]
		]
	} );
	assert.deepEqual( [ ...index.entries() ], [ [ 1933, "/assets/npc/hunting-portraits/aaa.png" ] ] );
	assert.deepEqual( [ ...portraitManifestIndex( null ).entries() ], [] );
	assert.deepEqual( [ ...portraitManifestIndex( {} ).entries() ], [] );
});
