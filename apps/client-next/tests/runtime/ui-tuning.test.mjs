/*
===========================================================================

ui-tuning.test.mjs - the shard's client tuning, taken only within bounds

The world-entry bootstrap may carry uiTuning (port-only). A missing or
malformed value keeps today's built-in one; a tuned short notice changes
only the Experimental "Short notices" banner, never the native one; a
tuned acknowledgement deadline moves the chat's own deadline.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { defaultUiTuning, uiTuningFrom } = await import(
	sourceFileUrl( "src/engine/foundation/ui/ui-tuning.ts" ).href
);
const DEFAULT_UI_TUNING = defaultUiTuning();
const { noticeTiming, NATIVE_NOTICE_TIMING, SHORT_NOTICE_TIMING } = await import(
	sourceFileUrl( "src/engine/runtime/ui/hud/unique-banner.ts" ).href
);
const { createChat } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" ).href
);

test("an absent or malformed tuning keeps today's values", () => {
	assert.deepEqual( DEFAULT_UI_TUNING, {
		shortNoticeShowMs: 5000,
		shortNoticeFadeMs: 1000,
		chatAckTimeoutMs: 10000
	} );
	for ( const bootstrap of [ undefined, null, {}, { uiTuning: null }, { uiTuning: "5000" }, { uiTuning: [] } ] ) {
		assert.deepEqual( uiTuningFrom( bootstrap ), DEFAULT_UI_TUNING, JSON.stringify( bootstrap ) );
	}
	const bad = uiTuningFrom( {
		uiTuning: { shortNoticeShowMs: 99999, shortNoticeFadeMs: 1.5, chatAckTimeoutMs: "20000" }
	} );
	assert.deepEqual( bad, DEFAULT_UI_TUNING, "out of bounds, fractional and string values fall back" );
});

test("values inside their bounds are taken one by one", () => {
	assert.deepEqual(
		uiTuningFrom( { uiTuning: { shortNoticeShowMs: 4000, shortNoticeFadeMs: 800, chatAckTimeoutMs: 20000 } } ),
		{ shortNoticeShowMs: 4000, shortNoticeFadeMs: 800, chatAckTimeoutMs: 20000 }
	);
	assert.deepEqual(
		uiTuningFrom( { uiTuning: { chatAckTimeoutMs: 5000 } } ),
		{ ...DEFAULT_UI_TUNING, chatAckTimeoutMs: 5000 },
		"an omitted value keeps its default"
	);
	// The bounds' edges are accepted; the longest fade still ends inside the
	// shortest banner.
	assert.deepEqual(
		uiTuningFrom( { uiTuning: { shortNoticeShowMs: 3000, shortNoticeFadeMs: 2000, chatAckTimeoutMs: 30000 } } ),
		{ shortNoticeShowMs: 3000, shortNoticeFadeMs: 2000, chatAckTimeoutMs: 30000 }
	);
	assert.deepEqual(
		uiTuningFrom( { uiTuning: { shortNoticeShowMs: 2999, shortNoticeFadeMs: 2001, chatAckTimeoutMs: 4999 } } ),
		DEFAULT_UI_TUNING,
		"one past each edge falls back"
	);
});

test("a tuned short notice changes only the short banner", () => {
	const tuning = { shortNoticeShowMs: 4000, shortNoticeFadeMs: 800, chatAckTimeoutMs: 10000 };
	assert.deepEqual( noticeTiming( true, tuning ), { showMs: 4000, fadeMs: 800 } );
	assert.equal( noticeTiming( false, tuning ), NATIVE_NOTICE_TIMING, "the native banner is never tuned" );
	assert.equal( noticeTiming( true ), SHORT_NOTICE_TIMING, "no tuning keeps the built-in short timing" );
	assert.deepEqual( noticeTiming( true, DEFAULT_UI_TUNING ), SHORT_NOTICE_TIMING );
});

test("a tuned acknowledgement deadline moves the chat's own deadline", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Me" }, uiTuning: { chatAckTimeoutMs: 6000 } } );
	chat.request( 1, "hello", "", 0 );
	assert.equal( chat.step( 5999 ), false );
	assert.equal( chat.state().pending, true );
	assert.equal( chat.step( 6000 ), true, "the tuned deadline releases the slot" );
	assert.equal( chat.state().pending, false );
	// A later entry without tuning returns to the built-in deadline.
	chat.bootstrap( { character: { name: "Me" } } );
	chat.request( 1, "again", "", 7000 );
	assert.equal( chat.step( 16999 ), false );
	assert.equal( chat.step( 17000 ), true );
});
