/*
===========================================================================

cos-distance-notice.test.mjs - the COS tether notice (0x342F)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { cosDistanceNotice } = await import( "../../src/engine/foundation/gameplay/cos-distance-notice.ts" );
const { constantNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );

/*
================
trade transport reason
================
*/
test("reason 1 reads as the native too-far-from-the-trade-cart error", () => {
	assert.deepEqual( cosDistanceNotice( Uint8Array.of( 1 ) ), constantNativeNotice( 12, 8 ) );
});

/*
================
capture monster reason
================
*/
test("reason 2 names the capture-quest monster at 30 m", () => {
	const notice = cosDistanceNotice( Uint8Array.of( 2 ) );
	assert.deepEqual( { key: notice?.key, arguments: notice?.arguments, banner: notice?.banner }, {
		key: "UIIT_MSG_QUEST_ERR_TOO_FAR_FROM_MONSTER",
		arguments: [ "30" ],
		banner: true
	} );
});

/*
================
other reasons
================
*/
test("other reasons are silent and a malformed body throws", () => {
	assert.equal( cosDistanceNotice( Uint8Array.of( 3 ) ), null );
	assert.throws( () => cosDistanceNotice( Uint8Array.of( 1, 0 ) ) );
});
