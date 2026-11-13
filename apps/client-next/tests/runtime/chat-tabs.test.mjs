/*
===========================================================================

chat-tabs.test.mjs - which chat lines each chat tab files

The native viewer files a whisper, a GM line and the type 10 and 0xff
lines into the Party, Guild and Ally lists beside All (6ad280), so
switching tabs never hides them (#3356, #3370). Each channel tab
otherwise shows its own channel alone, and the port-only Event tab
stays notice-only. The rule reads no generated file.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { chatTabShows } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );
const { STALL_CHAT_CHANNEL } = await import( "../../src/engine/foundation/gameplay/stall.ts" );

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

test("the Event tab holds the notices, All skips them only when it is offered", () => {
	assert.ok( chatTabShows( 7, 0, false ) );
	assert.ok( !chatTabShows( 7, 0, true ) );
	assert.ok( chatTabShows( 7, 4, true ) );
	for ( const channel of [ 1, 4, 5, 6, 11 ] ) assert.ok( !chatTabShows( channel, 4, true ), `${channel} on Event` );
});

test("stall lines stay in the stall window's chat box", () => {
	for ( const eventTab of [ false, true ] ) {
		for ( const tab of [ 0, 1, 2, 3, 4 ] ) assert.ok( !chatTabShows( STALL_CHAT_CHANNEL, tab, eventTab ) );
	}
});
