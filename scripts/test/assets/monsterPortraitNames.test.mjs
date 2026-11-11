/*
===========================================================================

monsterPortraitNames.test.mjs - a portrait's public name is its bytes

A still used to be published under sha256(renderer fingerprint + refObjId),
and the fingerprint covers every engine source file: any client edit
renamed all 464 stills, replaced the image packs, and stranded open tabs
on pack files the new release no longer served (#3104). These tests pin
the published name to the PNG bytes alone - same bytes, same name on
every build; the fingerprint now only keys the render cache.

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { monsterPortraitCacheKey, monsterPortraitPublishedPath } from "../../build/char/buildMonsterPortraitAssets.mjs";

const STILL_A = Buffer.from( "portrait bytes A" );
const STILL_B = Buffer.from( "portrait bytes B" );

/*
================
same bytes, same name
================
*/
test("the same still bytes publish under the same name on every build", () => {
	const first = monsterPortraitPublishedPath( STILL_A );
	const second = monsterPortraitPublishedPath( Buffer.from( STILL_A ) );
	assert.equal( first, second );
	assert.equal(
		first,
		"/assets/npc/hunting-portraits/" + createHash( "sha256" ).update( STILL_A ).digest( "hex" ) + ".png"
	);
});

/*
================
the name ignores the renderer fingerprint
================
*/
test("a moved renderer fingerprint renames nothing that renders the same", () => {
	// Two builds whose engine sources differ draw the identical still:
	// the cache keys differ (each build re-draws), the publication does not.
	assert.notEqual(
		monsterPortraitCacheKey( "fingerprint-one", 14838 ),
		monsterPortraitCacheKey( "fingerprint-two", 14838 )
	);
	assert.equal( monsterPortraitPublishedPath( STILL_A ), monsterPortraitPublishedPath( STILL_A ) );
});

/*
================
changed bytes, new name
================
*/
test("a still that renders differently publishes under a new name", () => {
	assert.notEqual( monsterPortraitPublishedPath( STILL_A ), monsterPortraitPublishedPath( STILL_B ) );
});
