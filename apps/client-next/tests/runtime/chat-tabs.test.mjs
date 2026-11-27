/*
===========================================================================

chat-tabs.test.mjs - which chat lines each chat tab files

The native viewer files a whisper, a GM line and the type 10 and 0xff
lines into the Party, Guild and Ally lists beside All (6ad280), so
switching tabs never hides them (#3356, #3370). Each channel tab
otherwise shows its own channel alone, and the port-only Event tab
stays notice-only. Each tab keeps its own last 100 lines (6ac81e), so
lines one tab files never push another tab's lines out of the shared
log. The rule reads no generated file.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { CHAT_TAB_LINES, chatLogKept, chatTabShows } = await import(
	"../../src/engine/foundation/gameplay/chat-tabs.ts"
);
const { chatHistoryLines, storedChatLines } = await import( "../../src/engine/foundation/gameplay/chat-history.ts" );
const { STALL_CHAT_CHANNEL } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const { JOB_KILL_NOTICE_CHANNEL } = await import( "../../src/engine/foundation/gameplay/system-notices.ts" );

const NATIVE_TABS = [ 0, 1, 2, 3 ];
const EVERY_TAB = [
	{ name: "whisper", channel: 2 },
	{ name: "GM chat", channel: 3 },
	{ name: "type 10", channel: 0xa },
	{ name: "type 0xff", channel: 0xff }
];
// The channel the Party, Guild and Ally tabs each file as their own.
const OWN = [ { tab: 1, channel: 4 }, { tab: 2, channel: 5 }, { tab: 3, channel: 11 } ];

test("whisper, GM chat and the type 10 and 0xff lines show on every native tab", () => {
	for ( const eventTab of [ false, true ] ) {
		for ( const { name, channel } of EVERY_TAB ) {
			for ( const tab of NATIVE_TABS ) {
				assert.ok( chatTabShows( channel, tab, eventTab ), `${name} on tab ${tab}, event tab ${eventTab}` );
			}
			if ( eventTab ) assert.ok( !chatTabShows( channel, 4, true ), `${name} stays off the Event tab` );
		}
	}
});

test("a channel tab shows its own channel and no other tab's", () => {
	for ( const eventTab of [ false, true ] ) {
		for ( const { tab, channel } of OWN ) {
			for ( const other of OWN ) {
				assert.equal(
					chatTabShows( other.channel, tab, eventTab ),
					other.tab === tab,
					`${other.channel} on tab ${tab}`
				);
			}
			assert.ok( !chatTabShows( 1, tab, eventTab ), `general chat stays off tab ${tab}` );
			assert.ok( !chatTabShows( 6, tab, eventTab ), `global chat stays off tab ${tab}` );
			assert.ok( chatTabShows( channel, 0, eventTab ), `All shows channel ${channel}` );
		}
	}
});

test("the Event tab holds the notices, All skips the event notices only when it is offered", () => {
	assert.ok( chatTabShows( 7, 0, false, true ) );
	assert.ok( !chatTabShows( 7, 0, true, true ) );
	assert.ok( chatTabShows( 7, 4, true, true ) );
	for ( const channel of [ 1, 4, 5, 6, 11 ] ) assert.ok( !chatTabShows( channel, 4, true ), `${channel} on Event` );
});

test("a native server notice (the restart warning) shows in All and on the Event tab", () => {
	for ( const eventTab of [ false, true ] ) assert.ok( chatTabShows( 7, 0, eventTab ), `All, event tab ${eventTab}` );
	assert.ok( chatTabShows( 7, 4, true ) );
	for ( const tab of [ 1, 2, 3 ] ) assert.ok( !chatTabShows( 7, tab, true ), `tab ${tab}` );
});

/*
================
noticeFrame

A 0x3667 server notice on the given wire channel: {u8 channel, u16
wcharCount, UTF-16 text}.
================
*/
function noticeFrame( channel, text ) {
	const message = Buffer.from( text, "utf16le" ), payload = Buffer.alloc( 3 + message.length );
	payload[0] = channel;
	payload.writeUInt16LE( text.length, 1 );
	message.copy( payload, 3 );
	return { opcode: 0x3667, payload };
}

test("the chat owner marks a port-only notice channel as an event and a native notice as none", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	chat.receive( noticeFrame( 7, "Server restart in two minutes. Please find a safe place." ), 1 );
	chat.receive( noticeFrame( JOB_KILL_NOTICE_CHANNEL, "A hunter was killed." ), 1 );
	const [restart, kill] = chat.state().lines;
	assert.equal( restart.channel, 7 );
	assert.equal( restart.event, undefined );
	assert.equal( kill.channel, 7 );
	assert.equal( kill.event, true );
	assert.ok( chatTabShows( restart.channel, 0, true, !!restart.event ), "the restart warning shows in All" );
	assert.ok( !chatTabShows( kill.channel, 0, true, !!kill.event ), "the job kill leaves All" );
});

test("stall lines stay in the stall window's chat box", () => {
	for ( const eventTab of [ false, true ] ) {
		for ( const tab of [ 0, 1, 2, 3, 4 ] ) assert.ok( !chatTabShows( STALL_CHAT_CHANNEL, tab, eventTab ) );
	}
});

/*
================
namedFrame

A named 0x3667 line: {u8 channel, u16 sender length, sender, u16
wcharCount, UTF-16 text}.
================
*/
function namedFrame( channel, name, text ) {
	const sender = Buffer.from( name, "utf8" ), message = Buffer.from( text, "utf16le" );
	const payload = Buffer.alloc( 5 + sender.length + message.length );
	payload[0] = channel;
	payload.writeUInt16LE( sender.length, 1 );
	sender.copy( payload, 3 );
	payload.writeUInt16LE( text.length, 3 + sender.length );
	message.copy( payload, 5 + sender.length );
	return { opcode: 0x3667, payload };
}

/*
================
shown

The texts one tab draws: the lines it files, its own last CHAT_TAB_LINES.
================
*/
function shown( lines, tab, eventTab ) {
	return lines.filter( line => chatTabShows( line.channel, tab, eventTab, !!line.event, !!line.everyTab ) )
		.slice( -CHAT_TAB_LINES )
		.map( line => line.text );
}

/*
================
chatWith

A chat owner that has heard the given frames.
================
*/
function chatWith( frames ) {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	for ( const frame of frames ) chat.receive( frame, 1 );
	return chat;
}

const DROP_NOTICE_CHANNEL = 0x81;

test("event notices on the Event tab never push the All tab's lines out", () => {
	const notices = Array.from(
		{ length: CHAT_TAB_LINES + 50 },
		( _, i ) => noticeFrame( DROP_NOTICE_CHANNEL, "drop " + i )
	);
	const chat = chatWith( [
		namedFrame( 6, "Peer", "said before the AFK" ),
		namedFrame( 2, "Friend", "are you there?" ),
		...notices
	] );
	const lines = chat.state().lines;
	assert.deepEqual( shown( lines, 0, true ), [ "said before the AFK", "are you there?" ], "All keeps its own lines" );
	assert.deepEqual( shown( lines, 1, true ), [ "are you there?" ], "the whisper stays on Party" );
	const event = shown( lines, 4, true );
	assert.equal( event.length, CHAT_TAB_LINES, "the Event tab keeps its own last lines" );
	assert.equal( event[0], "drop 50" );
	// Without the Event tab the notices are All's own lines, as in the
	// native viewer: there they do take All's oldest rows.
	const all = shown( lines, 0, false );
	assert.equal( all.length, CHAT_TAB_LINES );
	assert.equal( all[0], "drop 50" );
	assert.deepEqual( shown( lines, 2, false ), [ "are you there?" ], "the whisper stays on Guild" );
});

test("each native tab keeps its own last lines; a line no tab keeps leaves the log", () => {
	const chatter = Array.from( { length: CHAT_TAB_LINES + 1 }, ( _, i ) => namedFrame( 6, "Peer", "global " + i ) );
	const chat = chatWith( [
		namedFrame( 4, "Mate", "party line" ),
		namedFrame( 6, "Peer", "oldest global" ),
		...chatter
	] );
	const lines = chat.state().lines;
	assert.deepEqual( shown( lines, 1, true ), [ "party line" ], "Party keeps its line past All's hundred" );
	assert.equal( shown( lines, 0, true )[0], "global 1", "All draws its own last hundred" );
	assert.ok( !lines.some( line => line.text === "oldest global" ), "a line no tab keeps is dropped" );
	assert.ok( !lines.some( line => line.text === "global 0" ) );
	assert.equal( lines.length, CHAT_TAB_LINES + 1, "All's hundred and the Party line" );
});

test("stall lines keep their own last lines and never push the tabs' lines out", () => {
	const stall = Array.from(
		{ length: CHAT_TAB_LINES + 10 },
		( _, i ) => ({ channel: STALL_CHAT_CHANNEL, text: "stall " + i })
	);
	const kept = chatLogKept( [ { channel: 6, text: "global" }, ...stall ] );
	assert.equal( kept[0].text, "global" );
	assert.equal( kept.filter( line => line.channel === STALL_CHAT_CHANNEL ).length, CHAT_TAB_LINES );
	assert.equal( kept[1].text, "stall 10" );
	const unchanged = [ { channel: 6, text: "a" } ];
	assert.equal( chatLogKept( unchanged ), unchanged, "a log nothing leaves is returned as it is" );
});

test("the browser's record keeps every tab's lines and the event mark across a reload", () => {
	const notices = Array.from(
		{ length: CHAT_TAB_LINES + 5 },
		( _, i ) => noticeFrame( DROP_NOTICE_CHANNEL, "drop " + i )
	);
	const chat = chatWith( [ namedFrame( 6, "Peer", "said before the AFK" ), ...notices ] );
	const record = chatHistoryLines( JSON.parse( JSON.stringify( storedChatLines( chat.state().lines ) ) ) );
	assert.deepEqual( shown( record, 0, true ), [ "said before the AFK" ], "a reload still shows All's line" );
	assert.equal( shown( record, 4, true ).length, CHAT_TAB_LINES );
	assert.ok( record.filter( line => line.text.startsWith( "drop" ) ).every( line => line.event === true ) );
});
