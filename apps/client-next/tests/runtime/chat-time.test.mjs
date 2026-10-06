/*
===========================================================================

chat-time.test.mjs - the hover time of a chat line

A received line is stamped by the chat owner, formatted in the viewer's time
zone, and published as the hover text of its row. A line without a stamp shows
nothing.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test, mock } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const { chatLineTime } = await import( "../../src/engine/foundation/ui/chat-time.ts" );
const { chatLayout } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const NOON_UTC = Date.UTC( 2026, 0, 15, 12, 0, 5 );

/*
================
publicFrame

A channel 6 broadcast: the simplest named line the chat owner accepts.
================
*/
function publicFrame( name, text ) {
	const sender = Buffer.from( name, "utf8" );
	const message = Buffer.from( text, "utf16le" );
	const payload = Buffer.alloc( 5 + sender.length + message.length );
	payload[0] = 6;
	payload.writeUInt16LE( sender.length, 1 );
	sender.copy( payload, 3 );
	payload.writeUInt16LE( text.length, 3 + sender.length );
	message.copy( payload, 5 + sender.length );
	return { opcode: 0x3667, payload };
}

test("the chat owner stamps each received line with the wall clock", () => {
	let now = NOON_UTC;
	mock.method( Date, "now", () => now );
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	chat.receive( publicFrame( "Peer", "first" ), 1 );
	now += 90_000;
	chat.receive( publicFrame( "Peer", "second" ), 1 );
	assert.deepEqual( chat.state().lines.map( line => line.sentAt ), [ NOON_UTC, NOON_UTC + 90_000 ] );
	mock.restoreAll();
});

test("the time is shown in the viewer's time zone to the second", () => {
	mock.method( Date, "now", () => NOON_UTC + 60_000 );
	assert.equal( chatLineTime( NOON_UTC, { locale: "en-GB", timeZone: "UTC" } ), "12:00:05" );
	assert.equal( chatLineTime( NOON_UTC, { locale: "en-GB", timeZone: "America/New_York" } ), "07:00:05" );
	assert.equal( chatLineTime( undefined ), "" );
	assert.equal( chatLineTime( Number.NaN ), "" );
	mock.restoreAll();
});

test("a line from an earlier day also names its date; one from today does not", () => {
	const tenPastNine = Date.UTC( 2026, 0, 16, 9, 10, 0 );
	mock.method( Date, "now", () => tenPastNine );
	// The day before in UTC: the date comes first.
	assert.equal( chatLineTime( NOON_UTC, { locale: "en-GB", timeZone: "UTC" } ), "15/01/2026, 12:00:05" );
	// Earlier the same day: time only.
	assert.equal( chatLineTime( tenPastNine - 3_600_000, { locale: "en-GB", timeZone: "UTC" } ), "08:10:00" );
	// "Today" is the viewer's day: 04:10 in New York is still the 16th, and
	// 07:00 on the 15th there is the day before.
	assert.equal(
		chatLineTime( NOON_UTC, { locale: "en-GB", timeZone: "America/New_York" } ),
		"15/01/2026, 07:00:05"
	);
	assert.equal( chatLineTime( tenPastNine - 60_000, { locale: "en-GB", timeZone: "America/New_York" } ), "04:09:00" );
	mock.restoreAll();
});

test("a chat row publishes its line's time as hover text, and a line without one publishes none", () => {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
	);
	const draw = ( lines, chatTimestamps = false ) =>
		chatLayout( {
			layout,
			width: 1024,
			height: 768,
			rows: 2,
			tab: 0,
			input: "",
			lines,
			chatTimestamps,
			welcome: "Welcome",
			copy: key => key,
			size: () => [ 16, 16 ],
			text: () => [],
			hover: null,
			pressed: null,
			measure: value => value.length * 7
		} );
	const line = { channel: 6, name: "Peer", text: "hello", outgoing: false };
	const rows = draw( [ { ...line, sentAt: NOON_UTC }, { ...line, text: "no stamp" } ], true ).controls;
	const stamped = rows.find( control => control.label === "Peer:hello" );
	const unstamped = rows.find( control => control.label === "Peer:no stamp" );
	assert.ok( stamped && unstamped );
	const nativeRow = draw( [ { ...line, sentAt: NOON_UTC } ] ).controls.find( c => c.label === "Peer:hello" );
	assert.ok( nativeRow );
	assert.equal(
		nativeRow.helpText,
		undefined,
		"native chat has no timestamp tooltip until opted in"
	);
	assert.equal( stamped.helpText, chatLineTime( NOON_UTC ) );
	assert.equal( unstamped.helpText, undefined );
	const welcome = rows.find( control => control.label === "Welcome" );
	assert.ok( welcome );
	assert.equal( welcome.helpText, undefined );
});

test("calendar day follows the viewer across midnight", t => {
	const received = Date.UTC( 2026, 0, 15, 23, 30 );
	t.mock.method( Date, "now", () => Date.UTC( 2026, 0, 16, 0, 30 ) );
	assert.equal( chatLineTime( received, { locale: "en-GB", timeZone: "UTC" } ), "15/01/2026, 23:30:00" );
	assert.equal( chatLineTime( received, { locale: "en-GB", timeZone: "America/New_York" } ), "18:30:00" );
});
