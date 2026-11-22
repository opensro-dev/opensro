/*
===========================================================================

character-resources-gone.test.mjs - a source the release lacks is asked once

A character source the asset owner answers absent (this release does not
publish it) or stale (the page's release was replaced) can never load in
this page. The character resources must not ask for it again, whether the
actor stays in view, leaves and comes back, or a new world loads: one tab
left open on a replaced release asked for one animation 319 times, filing
an incident each time (#4114). Any other failure still retries.

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterResources } = await import(
	sourceFileUrl( "src/engine/runtime/characters/resources/resources.ts" ).href
);

const CLIP = "/assets/anim/prim/ani/char/europe/man/skill_eu_wizard_firea_spray_a.ban";
const FRAME_SECONDS = 0.1;

/*
================
fixture

Character resources over an asset owner that answers every request with
one error result; `requests` counts what reached the owner.
================
*/
function fixture( verdict ) {
	let serial = 0;
	const results = new Map(), requests = [];
	const assets = {
		available: () => 4,
		request( url ) {
			requests.push( url );
			results.set( ++serial, { kind: "error", id: serial, error: "Asset HTTP 404", ...verdict } );
			return serial;
		},
		take( id ) {
			const result = results.get( id );
			results.delete( id );
			return result;
		},
		cancel( id ) {
			results.delete( id );
		}
	};
	const resources = createCharacterResources(
		assets,
		{ setCharacterModel() {}, setCharacterAnimation() {}, retainCharacterModels() {} },
		"http://fixture.invalid"
	);
	let now = 0;
	return {
		resources,
		requests,
		// Frames over `seconds`, wanting the clip's bytes on each while `wanted`.
		run( seconds, wanted ) {
			for ( let t = 0; t < seconds; t += FRAME_SECONDS ) {
				resources.begin( now );
				resources.poll();
				if ( wanted ) resources.manifest( CLIP );
				resources.retainWanted( [] );
				now += FRAME_SECONDS;
			}
		}
	};
}

for ( const [name, verdict] of [ [ "absent", { absent: true } ], [ "stale", { stale: true } ] ] ) {
	test(`a source answered ${name} is requested once for the page`, () => {
		const f = fixture( verdict );
		f.run( 10, true );
		assert.equal( f.requests.length, 1, "in view: no retry can find it" );
		assert.equal( f.resources.failed( CLIP ), true );
		f.run( 5, false );
		assert.equal( f.resources.failed( CLIP ), true, "out of view it still reads as failed, for the fallback" );
		f.run( 10, true );
		assert.equal( f.requests.length, 1, "back in view: still not asked again" );
		f.resources.reset();
		f.run( 10, true );
		assert.equal( f.requests.length, 1, "a new world keeps the page's release" );
	});
}

test("any other failure keeps retrying behind its deadline", () => {
	const f = fixture( {} );
	f.run( 10, true );
	assert.ok( f.requests.length >= 4, `retried after each deadline (${f.requests.length} requests in 10 s)` );
	assert.ok( f.requests.length <= 6, `but never every frame (${f.requests.length} requests in 10 s)` );
});
