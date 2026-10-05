/*
===========================================================================

chat-time.test.mjs - the hover time of a chat line

A received line is stamped by the chat owner, formatted in the viewer's time
zone, and published as the hover text of its row. A line without a stamp shows
nothing.

===========================================================================
*/
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
	assert.equal( chatLineTime( NOON_UTC, { locale: "en-GB", timeZone: "UTC" } ), "12:00:05" );
	assert.equal( chatLineTime( NOON_UTC, { locale: "en-GB", timeZone: "America/New_York" } ), "07:00:05" );
	assert.equal( chatLineTime( undefined ), "" );
	assert.equal( chatLineTime( Number.NaN ), "" );
});

test("a chat row publishes its line's time as hover text, and a line without one publishes none", () => {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( "../../.generated/client-public/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
	);
	const draw = lines =>
		chatLayout( {
			layout,
			width: 1024,
			height: 768,
			rows: 2,
			tab: 0,
			input: "",
			lines,
			welcome: "Welcome",
			copy: key => key,
			size: () => [ 16, 16 ],
			text: () => [],
			hover: null,
			pressed: null,
			measure: value => value.length * 7
		} );
	const line = { channel: 6, name: "Peer", text: "hello", outgoing: false };
	const rows = draw( [ { ...line, sentAt: NOON_UTC }, { ...line, text: "no stamp" } ] ).controls;
	const stamped = rows.find( control => control.label === "Peer:hello" );
	const unstamped = rows.find( control => control.label === "Peer:no stamp" );
	assert.ok( stamped && unstamped );
	assert.equal( stamped.helpText, chatLineTime( NOON_UTC ) );
	assert.equal( unstamped.helpText, undefined );
	const welcome = rows.find( control => control.label === "Welcome" );
	assert.ok( welcome );
	assert.equal( welcome.helpText, undefined );
});
