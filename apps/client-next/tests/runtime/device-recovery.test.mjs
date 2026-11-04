/*
===========================================================================

device-recovery.test.mjs - the paced device recovery budget

Pins device-recovery.ts on its own: eight paced attempts, then the budget
renewed after a stable minute of running. Split from lifecycle.test.mjs,
which drives the same owner through the renderer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createDeviceRecovery } = await load( "src/engine/foundation/rendering/device-recovery.ts" );
test("device recovery paces eight attempts and renews after a stable minute", () => {
	const recovery = createDeviceRecovery();
	// The first attempt is due at once; each later one waits out its
	// rung of the ladder, counted from the frame that saw the failure.
	assert.equal( recovery.due( 1000 ), true );
	recovery.began();
	assert.equal( recovery.due( 2000 ), false, "the second attempt waits 1 s" );
	assert.equal( recovery.due( 2999 ), false );
	assert.equal( recovery.due( 3000 ), true );
	recovery.began();
	for ( const wait of [ 2000, 5000, 10000, 30000, 30000, 30000 ] ) {
		const failedAt = 100000;
		assert.equal( recovery.due( failedAt ), false );
		assert.equal( recovery.due( failedAt + wait - 1 ), false );
		assert.equal( recovery.due( failedAt + wait ), true );
		recovery.began();
	}
	assert.equal( recovery.pending(), false, "eight attempts bound the budget" );
	assert.equal( recovery.due( 999999 ), false );

	const renewed = createDeviceRecovery();
	assert.equal( renewed.due( 0 ), true );
	renewed.began();
	renewed.running( 10000 );
	renewed.running( 69999 );
	assert.equal( renewed.due( 70000 ), false, "59.999 s stable has not renewed the budget yet" );
	renewed.running( 70001 );
	renewed.running( 130001 );
	assert.equal( renewed.due( 140000 ), true, "a stable minute restores the immediate attempt" );
});
