/*
===========================================================================

server-notification.test.mjs - tests for gameplay.ts, system-notices.ts,
unique-banner.ts, chat-presentation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { serverNotification } = await import( "../../src/engine/foundation/gameplay/system-notices.ts" );
const {
	createNoticeBanner,
	notificationBannerPaths,
	SUN_DROP_BANNER_COLOR,
	SUN_DROP_BANNER_TEXT_RGB
} = await import( "../../src/engine/runtime/ui/hud/unique-banner.ts" );
const { chatLineText, chatLineColor } = await import( "../../src/engine/foundation/ui/chat-presentation.ts" );
test("server refusal reaches the independent native red notice and pink chat log", () => {
	const text = "This skill is not implemented yet.",
		bytes = Buffer.from( text, "utf16le" ),
		payload = Buffer.concat( [ Buffer.from( [ 7, text.length, 0 ] ), bytes ] );
	const game = createGameplay( () => {} );
	game.receive( { opcode: 0x3667, payload }, 100 );
	const state = game.take(),
		notice = defined( defined( state ).notices ).at( -1 ),
		line = defined( defined( state ).chat ).lines.at( -1 );
	assert.equal( defined( notice ).notificationBanner, true );
	assert.equal( defined( notice ).questBanner, undefined );
	assert.equal( defined( line ).text, text );
	assert.equal( chatLineText( line, () => "Notify" ), "(Notify):" + text );
	assert.deepEqual( chatLineColor( defined( line ).channel ), [ 1, 174 / 255, 195 / 255, 1 ] );
	const banner = createNoticeBanner( "notificationBanner" ), warning = createNoticeBanner();
	banner.step( defined( state ).notices, 100, true );
	warning.step( defined( state ).notices, 100, true );
	assert.equal( banner.value( () => "" ), text );
	assert.equal( banner.alpha(), 1 );
	assert.equal( warning.alpha(), 0 );
	assert.ok( notificationBannerPaths.every( p => p.includes( "/com_notice_" ) ) );
	banner.step( [], 6100, true );
	assert.equal( banner.alpha(), 127 / 255 );
	banner.step( [], 7100, true );
	assert.equal( banner.alpha(), 0 );
	for ( let n = 1; n < payload.length; n++ ) {
		assert.throws( () => serverNotification( 0x3667, payload.subarray( 0, n ) ) );
	}
	assert.throws( () => serverNotification( 0x3667, Buffer.concat( [ payload, Buffer.from( [ 0 ] ) ] ) ) );
});

test("a job-kill notice (port-only channel 0x80) shows like channel 7 unless the Experimental row is off", () => {
	const text = "[leChiffre] killed [ES_Tip]",
		payload = Buffer.concat( [ Buffer.from( [ 0x80, text.length, 0 ] ), Buffer.from( text, "utf16le" ) ] );
	const notice = defined( serverNotification( 0x3667, payload ) );
	assert.equal( notice.jobKill, true );
	assert.equal( notice.text, text );
	assert.equal(
		serverNotification( 0x3667, Buffer.concat( [ Buffer.from( [ 7 ] ), payload.subarray( 1 ) ] ) )?.jobKill,
		undefined
	);
	// On by default: the banner notice and a (Notify): chat line, colored as channel 7.
	const shown = createGameplay( () => {} );
	shown.receive( { opcode: 0x3667, payload }, 100 );
	const state = defined( shown.take() ),
		line = defined( defined( state.chat ).lines.at( -1 ) );
	assert.equal( defined( defined( state.notices ).at( -1 ) ).text, text );
	assert.equal( line.channel, 7 );
	assert.equal( chatLineText( line, () => "Notify" ), "(Notify):" + text );
	// Off: neither the banner nor the chat log sees it; a plain notice still shows.
	const hidden = createGameplay( () => {} );
	hidden.experimental( {
		predictedRunUp: false,
		jobKillNotices: false,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true
	} );
	hidden.receive( { opcode: 0x3667, payload }, 100 );
	const quiet = hidden.take();
	assert.equal( quiet?.notices?.some( n => n.text === text ) ?? false, false );
	assert.equal( quiet?.chat?.lines.some( l => l.text === text ) ?? false, false );
	const plain = Buffer.concat( [ Buffer.from( [ 7, 5, 0 ] ), Buffer.from( "Hello", "utf16le" ) ] );
	hidden.receive( { opcode: 0x3667, payload: plain }, 200 );
	assert.equal( defined( defined( hidden.take() ).notices ).at( -1 )?.text, "Hello" );
});

test("a drop or plus notice (port-only channel 0x81) shows like channel 7 unless its own row is off", () => {
	const text = "Looter advanced Blade to +9",
		payload = Buffer.concat( [ Buffer.from( [ 0x81, text.length, 0 ] ), Buffer.from( text, "utf16le" ) ] );
	const notice = defined( serverNotification( 0x3667, payload ) );
	assert.equal( notice.dropPlus, true );
	assert.equal( notice.jobKill, undefined );
	// On by default: the banner notice and a (Notify): chat line, colored as channel 7.
	const shown = createGameplay( () => {} );
	shown.receive( { opcode: 0x3667, payload }, 100 );
	const state = defined( shown.take() ),
		line = defined( defined( state.chat ).lines.at( -1 ) );
	assert.equal( defined( defined( state.notices ).at( -1 ) ).text, text );
	assert.equal( line.channel, 7 );
	assert.equal( chatLineText( line, () => "Notify" ), "(Notify):" + text );
	// Off: neither the banner nor the chat log sees it; a job-kill notice still shows.
	const hidden = createGameplay( () => {} );
	hidden.experimental( {
		predictedRunUp: false,
		jobKillNotices: true,
		dropPlusNotices: false,
		plusSevenNotices: true,
		breakNotices: true
	} );
	hidden.receive( { opcode: 0x3667, payload }, 100 );
	const quiet = hidden.take();
	assert.equal( quiet?.notices?.some( n => n.text === text ) ?? false, false );
	assert.equal( quiet?.chat?.lines.some( l => l.text === text ) ?? false, false );
	const kill = Buffer.concat( [ Buffer.from( [ 0x80, 5, 0 ] ), Buffer.from( "Hello", "utf16le" ) ] );
	hidden.receive( { opcode: 0x3667, payload: kill }, 200 );
	assert.equal( defined( defined( hidden.take() ).notices ).at( -1 )?.text, "Hello" );
});

test("a plus notice for a +7 item (channel 0x81, the body's plus byte) answers to its own row only", () => {
	// The announced item's body as ItemBody.Encode writes it: RefObjID,
	// the plus byte, then the rest of the fixed 18-byte header.
	const body = ( plus, bytes = 18 ) => {
			const b = Buffer.alloc( bytes );
			b.writeUInt32LE( 1234, 0 );
			if ( bytes > 4 ) b[4] = plus;
			return b;
		},
		linked = ( text, bodyBytes ) =>
			Buffer.concat( [
				Buffer.from( [ 0x81, text.length, 0 ] ),
				Buffer.from( text, "utf16le" ),
				Buffer.from( [ 2, 0 ] ),
				Buffer.from( "Ab", "ascii" ),
				Buffer.from( [ bodyBytes.length, 0 ] ),
				bodyBytes
			] );
	const text = "Looter advanced Blade to +7",
		payload = linked( text, body( 7 ) );
	const notice = defined( serverNotification( 0x3667, payload ) );
	assert.equal( notice.plusSeven, true );
	assert.equal( notice.dropPlus, undefined );
	assert.equal( notice.text, text );
	// On by default: the banner notice and a (Notify): chat line, colored as channel 7.
	const shown = createGameplay( () => {} );
	shown.receive( { opcode: 0x3667, payload }, 100 );
	const state = defined( shown.take() ),
		line = defined( defined( state.chat ).lines.at( -1 ) );
	assert.equal( defined( defined( state.notices ).at( -1 ) ).text, text );
	assert.equal( line.channel, 7 );
	assert.equal( chatLineText( line, () => "Notify" ), "(Notify):" + text );
	// Its own row off: neither the banner nor the chat log sees it.
	const hidden = createGameplay( () => {} );
	hidden.experimental( {
		predictedRunUp: false,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: false,
		breakNotices: true
	} );
	hidden.receive( { opcode: 0x3667, payload }, 100 );
	const quiet = hidden.take();
	assert.equal( quiet?.notices?.some( n => n.text === text ) ?? false, false );
	assert.equal( quiet?.chat?.lines.some( l => l.text === text ) ?? false, false );
	// The drop/plus row off never widens or narrows this one.
	const stillShown = createGameplay( () => {} );
	stillShown.experimental( {
		predictedRunUp: false,
		jobKillNotices: true,
		dropPlusNotices: false,
		plusSevenNotices: true,
		breakNotices: true
	} );
	stillShown.receive( { opcode: 0x3667, payload }, 100 );
	assert.equal( defined( defined( stillShown.take() ).notices ).at( -1 )?.text, text );
	// +8 and above stay with drop/plus, and a body too short to name a
	// plus does too.
	const nine = defined( serverNotification( 0x3667, linked( "Looter advanced Blade to +9", body( 9 ) ) ) );
	assert.equal( nine.dropPlus, true );
	assert.equal( nine.plusSeven, undefined );
	const short = defined( serverNotification( 0x3667, linked( text, body( 7, 10 ) ) ) );
	assert.equal( short.dropPlus, true );
	assert.equal( short.plusSeven, undefined );
	// No further channel was introduced: an 0x84 frame is not a notice
	// (0x82 is the item-break channel, #1738; 0x83 the rename channel, #1689).
	assert.equal(
		serverNotification(
			0x3667,
			Buffer.concat( [ Buffer.from( [ 0x84, 5, 0 ] ), Buffer.from( "Hello", "utf16le" ) ] )
		),
		null
	);
});

test("an item break notice (port-only channel 0x82) shows like channel 7 unless the Experimental row is off", () => {
	const text = "leChiffre broke Blade (Seal Of Sun)",
		payload = Buffer.concat( [ Buffer.from( [ 0x82, text.length, 0 ] ), Buffer.from( text, "utf16le" ) ] );
	const notice = defined( serverNotification( 0x3667, payload ) );
	assert.equal( notice.itemBreak, true );
	assert.equal( notice.jobKill, undefined );
	assert.equal( notice.dropPlus, undefined );
	assert.equal( notice.text, text );
	// On by default: the banner notice and a (Notify): chat line, colored as channel 7.
	const shown = createGameplay( () => {} );
	shown.receive( { opcode: 0x3667, payload }, 100 );
	const state = defined( shown.take() );
	assert.equal( defined( defined( state.notices ).at( -1 ) ).text, text );
	assert.equal( defined( defined( state.chat ).lines.at( -1 ) ).text, text );
	// Off: neither the banner nor the chat log sees it; the other notice rows are untouched.
	const hidden = createGameplay( () => {} );
	hidden.experimental( {
		predictedRunUp: false,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: false
	} );
	hidden.receive( { opcode: 0x3667, payload }, 100 );
	const quiet = hidden.take();
	assert.equal( quiet?.notices?.some( n => n.text === text ) ?? false, false );
	assert.equal( quiet?.chat?.lines.some( l => l.text === text ) ?? false, false );
	// The job-kill row off never widens or narrows this one.
	const stillShown = createGameplay( () => {} );
	stillShown.experimental( {
		predictedRunUp: false,
		jobKillNotices: false,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true
	} );
	stillShown.receive( { opcode: 0x3667, payload }, 100 );
	assert.equal( defined( defined( stillShown.take() ).notices ).at( -1 )?.text, text );
});

test("a plus notice above +11 (channel 0x81) renders its text verbatim; the client caps no plus", () => {
	const text = "Looter advanced Blade to +13",
		payload = Buffer.concat( [ Buffer.from( [ 0x81, text.length, 0 ] ), Buffer.from( text, "utf16le" ) ] );
	assert.equal( defined( serverNotification( 0x3667, payload ) ).text, text );
	const shown = createGameplay( () => {} );
	shown.receive( { opcode: 0x3667, payload }, 100 );
	const state = defined( shown.take() );
	assert.equal( defined( defined( state.notices ).at( -1 ) ).text, text );
	assert.equal( defined( defined( state.chat ).lines.at( -1 ) ).text, text );
});

test("a tiered plus notice (channel 0x81, #1739) keeps the plain drop/plus rendering and the 100-unit budget", () => {
	// The announced item's body as ItemBody.Encode writes it: RefObjID,
	// the plus byte, then the rest of the fixed 18-byte header.
	const body = plus => {
			const b = Buffer.alloc( 18 );
			b.writeUInt32LE( 1234, 0 );
			b[4] = plus;
			return b;
		},
		linked = ( text, plus ) =>
			Buffer.concat( [
				Buffer.from( [ 0x81, text.length, 0 ] ),
				Buffer.from( text, "utf16le" ),
				Buffer.from( [ 2, 0 ] ),
				Buffer.from( "Ab", "ascii" ),
				Buffer.from( [ 18, 0 ] ),
				body( plus )
			] );
	// The tier is wording only: the line answers to the drop/plus row,
	// its colour still rides the linked item's seal tier, and no Sun
	// highlight is claimed (the text never says "has dropped").
	for (
		const [text, plus] of [
			[ "Great success! Looter advanced Blade (Seal Of Sun) to +10", 10 ],
			[ "LEGENDARY! Looter advanced Blade (Seal Of Sun) to +11", 11 ]
		]
	) {
		const notice = defined( serverNotification( 0x3667, linked( text, plus ) ) );
		assert.equal( notice.dropPlus, true );
		assert.equal( notice.plusSeven, undefined );
		assert.equal( notice.sunDrop, undefined );
		assert.equal( notice.text, text );
	}
	const shown = createGameplay( () => {} ),
		text = "LEGENDARY! Looter advanced Blade to +11";
	shown.receive( { opcode: 0x3667, payload: linked( text, 11 ) }, 100 );
	assert.equal( defined( defined( shown.take() ).notices ).at( -1 )?.text, text );
	// The banner's budget is the wire's: a tiered line at exactly 100
	// UTF-16 units decodes whole.
	const longest = "LEGENDARY! " + "A".repeat( 89 );
	assert.equal( longest.length, 100 );
	assert.equal( defined( serverNotification( 0x3667, linked( longest, 11 ) ) ).text, longest );
});

test("a Seal of Sun drop notice (channel 0x81) flags sunDrop on the banner while Star, Moon and plus stay unchanged", () => {
	const frame = ( channel, text ) =>
		Buffer.concat( [ Buffer.from( [ channel, text.length, 0 ] ), Buffer.from( text, "utf16le" ) ] );
	const sunText = "Looter has dropped Heaven's Cloud Spear (Seal Of Sun) from Tiger Girl",
		starText = "Looter has dropped Heaven's Cloud Spear (Seal Of Star) from Tiger Girl",
		moonText = "Looter has dropped Heaven's Cloud Spear (Seal Of Moon) from Tiger Girl",
		plusText = "Looter advanced Heaven's Cloud Spear (Seal Of Sun) to +8";
	const sunNotice = defined( serverNotification( 0x3667, frame( 0x81, sunText ) ) );
	assert.equal( sunNotice.dropPlus, true );
	assert.equal( sunNotice.sunDrop, true );
	for ( const other of [ starText, moonText, plusText ] ) {
		const notice = defined( serverNotification( 0x3667, frame( 0x81, other ) ) );
		assert.equal( notice.dropPlus, true );
		assert.equal( notice.sunDrop, undefined, other );
	}
	// Channel 7 never sets sunDrop even if its text mentions (Seal Of Sun).
	assert.equal( defined( serverNotification( 0x3667, frame( 7, sunText ) ) ).sunDrop, undefined );
	const banner = createNoticeBanner( "notificationBanner" );
	banner.step( [ { ...sunNotice, sequence: 1 } ], 100, true );
	assert.equal( banner.sunDrop(), true );
	assert.deepEqual( SUN_DROP_BANNER_COLOR, [ 180, 120, 16 ] );
	assert.deepEqual( SUN_DROP_BANNER_TEXT_RGB, [ 1, 215 / 255, 80 / 255 ] );
	const starNotice = defined( serverNotification( 0x3667, frame( 0x81, starText ) ) );
	banner.step( [ { ...starNotice, sequence: 2 } ], 200, true );
	assert.equal( banner.sunDrop(), false );
});
