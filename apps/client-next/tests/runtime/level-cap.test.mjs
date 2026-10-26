/*
===========================================================================

level-cap.test.mjs - the published level cap the client bounds windows by

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { NATIVE_LEVEL_CAP, EXPANSION_LEVEL_CAP, publishedLevelCap } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/level-cap.ts" ).href
);

test("an unpublished cap is the native one; the server's two values pass through", () => {
	assert.equal( NATIVE_LEVEL_CAP, 90 );
	assert.equal( EXPANSION_LEVEL_CAP, 110 );
	assert.equal( publishedLevelCap( undefined ), 90 );
	assert.equal( publishedLevelCap( 90 ), 90 );
	assert.equal( publishedLevelCap( 110 ), 110 );
});

test("any other published value is refused", () => {
	for ( const value of [ null, 0, 91, 109, 111, "110", 110.5, Number.NaN ] ) {
		assert.throws( () => publishedLevelCap( value ), /Invalid published level cap/ );
	}
});
