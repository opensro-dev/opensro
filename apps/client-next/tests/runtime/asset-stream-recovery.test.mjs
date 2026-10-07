/*
===========================================================================

asset-stream-recovery.test.mjs - bounded retry clocks for failed streams

Only typed transport failures opt into recovery. Frames, not background
timers, advance this clock so disposal and explicit reset remain immediate.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { assetFailure, createAssetRecovery, transientAssetFailure } = await import(
	"../../src/engine/foundation/assets/asset-recovery.ts"
);

test("recovery backs off and stops after eight attempts", () => {
	const recovery = createAssetRecovery();
	let now = 123;
	for ( const delay of [ 2000, 5000, 10000, 30000, 30000, 30000, 30000, 30000 ] ) {
		recovery.failed( assetFailure( "network", true ), now );
		assert.equal( recovery.reconnecting(), true );
		assert.equal( recovery.due( now + delay - 1 ), false );
		now += delay;
		assert.equal( recovery.due( now ), true );
	}
	recovery.failed( assetFailure( "network", true ), now );
	assert.equal( recovery.reconnecting(), false );
	assert.equal( recovery.transient(), true );
	assert.equal( recovery.due( now + 1000000 ), false );
	recovery.reset();
	assert.equal( recovery.transient(), false );
	recovery.failed( assetFailure( "network", true ), now );
	assert.equal( recovery.due( now + 1999 ), false );
	assert.equal( recovery.due( now + 2000 ), true );
});

test("permanent validation and untagged failures never opt into recovery", () => {
	for (
		const error of [
			new TypeError( "decode" ),
			new Error( "HTTP 404" ),
			assetFailure( "hash mismatch" ),
			"network"
		]
	) {
		const recovery = createAssetRecovery();
		recovery.failed( error, 0 );
		assert.equal( transientAssetFailure( error ), false );
		assert.equal( recovery.reconnecting(), false );
		assert.equal( recovery.due( 1000000 ), false );
	}
});
