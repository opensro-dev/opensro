/*
===========================================================================

notice-dialog.test.mjs - tests for notice-dialog.ts, messages.ts,
native-notice.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { noticeDialog, noticeDialogLines } = await import( "../../src/engine/foundation/ui/notice-dialog.ts" );
const { createHudMessages } = await import( "../../src/engine/runtime/ui/hud/messages.ts" );
const { resolveNativeNotice } = await import( "../../src/engine/foundation/gameplay/native-notice.ts" );
test("simple notice dialog wraps at native width and grows from minimum dimensions", () => {
	const measure = s => s.length * 10, short = noticeDialog( 1000, 800, "One\nTwo", measure );
	assert.deepEqual( short.frame, [ 320, 316, 360, 168 ] );
	assert.deepEqual( short.body, [ 350, 381, 300, 46 ] );
	assert.deepEqual( short.confirm, [ 462, 447, 76, 24 ] );
	const wrapped = noticeDialog( 1000, 800, "x".repeat( 61 ), measure );
	assert.equal( wrapped.lines.length, 2 );
	assert.equal( wrapped.frame[2], 660 );
	const dragged = noticeDialog( 1000, 800, "short", measure, [ 9999, -50 ] );
	assert.deepEqual( dragged.frame, [ 640, 0, 360, 151 ] );
});
test("dialog lines apply the notice arguments, localized ones included", () => {
	// The retail English rows, as the shipped copy resolves them.
	const copy = key => ({
		UIIT_MSG_FORT_POSITION_GRANT_COMPLETE: "[%s] position is provided to [%s].",
		UIIT_MSG_FORT_POSITION_REMOVE_COMPLETE: "Position of [%s] is removed.",
		UIIT_STT_FORT_GUILD_SUBCOMMANDER: "Deputy commander at fortress war",
		UIIT_CTL_MASTERRELEASE_NOTICE: "A master release vote is open."
	}[key] ?? key);
	// The grant line is formatted natively from (member name, title);
	// the title argument names a copy key, not text.
	assert.equal(
		noticeDialogLines( copy, {
			lines: [ "UIIT_MSG_FORT_POSITION_GRANT_COMPLETE" ],
			arguments: [ "Peer", "" ],
			localizedArguments: [ null, "UIIT_STT_FORT_GUILD_SUBCOMMANDER" ]
		} ),
		"[Peer] position is provided to [Deputy commander at fortress war]."
	);
	assert.equal(
		noticeDialogLines( copy, {
			lines: [ "UIIT_MSG_FORT_POSITION_REMOVE_COMPLETE" ],
			arguments: [ "Peer" ]
		} ),
		"Position of [Peer] is removed."
	);
	// A line without conversions passes through as plain copy.
	assert.equal(
		noticeDialogLines( copy, { lines: [ "UIIT_CTL_MASTERRELEASE_NOTICE" ] } ),
		"A master release vote is open."
	);
});
test("modal notification never leaks into status guide history", () => {
	const notice = resolveNativeNotice( 16, 67 ).notice, hud = createHudMessages( () => 0 );
	assert.deepEqual( hud.step( 0, [], 1, 0, [ { ...notice, sequence: 1 } ], () => "localized" ), [] );
});
