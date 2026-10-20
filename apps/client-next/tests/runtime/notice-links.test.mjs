/*
===========================================================================

notice-links.test.mjs - the player and item a drop or plus notice links

The 0x3667 channel 0x81 tail against the server's pinned vectors
(apps/server/internal/game/item/wire/notification_test.go), the lenient
decode that never fails the session, the chat-log spans the Experimental
"Links in notices" row turns on, and the tooltip a linked item resolves to.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const { serverNotification } = await import( "../../src/engine/foundation/gameplay/system-notices.ts" );
const { decodeNoticeLinks, noticeRowLinks, noticeWrappedLinks } = await import(
	"../../src/engine/foundation/gameplay/notice-links.ts"
);
const { decodeInventoryItem } = await import( "../../src/engine/foundation/gameplay/inventory-item.ts" );
const { chatLayout, sunDropChatColor, isSunDropChatLine } = await import(
	"../../src/engine/foundation/ui/chat-layout.ts"
);
const { tooltipItems } = await import( "../../src/engine/foundation/ui/tooltip-target.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );

const bytes = hex => Uint8Array.from( hex.split( " " ).map( b => parseInt( b, 16 ) ) );
// The server's pinned vectors: text "A", then no player and no item, or
// Looter and an ETC potion (TID 3.3.1.1, ref 0x01020304, quantity 3).
const BARE = bytes( "81 01 00 41 00 00 00 00 00" );
const LINKED = bytes( "81 01 00 41 00 06 00 4C 6F 6F 74 65 72 06 00 04 03 02 01 03 00" );
const POTION_BODY = bytes( "04 03 02 01 03 00" );
// TID 3.3.1.1 as the client packs it: kind 3 (0x0C), band 3 (0x60), group 1, sub 1.
const POTION_TYPE = 0x0c | 0x60 | 1 << 7 | 1 << 11;
const POTION = {
	slot: 0,
	refObjId: 0x01020304,
	typeFlags: POTION_TYPE,
	quantity: 3,
	plus: 0,
	durability: 0,
	variance: "0",
	magic: [],
	name: "Potion"
};

/*
================
receive

One 0x3667 frame through a chat owner whose item resolver records the body.
================
*/
function receive( payload ) {
	const bodies = [];
	const chat = createChat( () => {}, body => {
		bodies.push( Array.from( body ) );
		return POTION;
	} );
	chat.bootstrap( { character: { name: "Local" } } );
	assert.equal( chat.receive( { opcode: 0x3667, payload }, 1 ), true );
	const line = chat.state().lines.at( -1 );
	assert.ok( line );
	return { line, bodies };
}

test("the notice tail decodes the server's pinned vectors", () => {
	const bare = receive( BARE );
	assert.equal( bare.line.text, "A" );
	assert.equal( bare.line.notice, undefined, "no player and no item links nothing" );
	assert.deepEqual( bare.bodies, [] );
	const linked = receive( LINKED );
	assert.equal( linked.line.text, "A" );
	assert.deepEqual( linked.line.notice, { name: "Looter", item: POTION } );
	assert.deepEqual( linked.bodies, [ Array.from( POTION_BODY ) ] );
	// The banner shows the text alone.
	for ( const payload of [ BARE, LINKED ] ) {
		const notice = serverNotification( 0x3667, payload );
		assert.ok( notice );
		assert.equal( notice.text, "A" );
		assert.equal( notice.dropPlus, true );
	}
	// The body is the bag's own item record.
	const decoded = decodeInventoryItem( POTION_BODY, 0, new Map( [ [ 0x01020304, POTION_TYPE ] ] ) );
	assert.equal( decoded.item?.quantity, 3 );
	assert.equal( decoded.next, POTION_BODY.length );
});

test("a malformed tail leaves a plain line and never fails the session", () => {
	const text = "81 01 00 41 00";
	for (
		const tail of [
			"06",
			"06 00 4C 6F",
			"0D 00 41 41 41 41 41 41 41 41 41 41 41 41 41 00 00",
			"02 00 4C 20 00 00",
			"06 00 4C 6F 6F 74 65 72 09 00 04 03 02 01",
			"06 00 4C 6F 6F 74 65 72 00 00 FF"
		]
	) {
		const { line } = receive( bytes( text + " " + tail ) );
		assert.equal( line.text, "A", tail );
		assert.equal( line.notice, undefined, tail );
	}
	assert.equal( decodeNoticeLinks( bytes( "06" ), 0 ), null );
	// Only the drop/plus channel may carry a tail.
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	assert.throws( () => chat.receive( { opcode: 0x3667, payload: bytes( "07 01 00 41 00 00 00 00 00" ) }, 1 ) );
});

test("an undecodable item still links the player", () => {
	const chat = createChat( () => {}, () => undefined );
	chat.bootstrap( { character: { name: "Local" } } );
	chat.receive( { opcode: 0x3667, payload: LINKED }, 1 );
	assert.deepEqual( chat.state().lines.at( -1 )?.notice, { name: "Looter" } );
});

const layout = decodeAuthoredLayout(
	JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
);
const NOTICE_LINE = {
	channel: 7,
	name: "",
	text: "Looter has dropped Potion (Seal Of Star) from Tiger Girl",
	outgoing: false,
	sequence: 5,
	notice: { name: "Looter", item: POTION }
};

/*
================
layoutWith
================
*/
function layoutWith( overrides ) {
	return chatLayout( {
		layout,
		width: 1024,
		height: 768,
		rows: 2,
		tab: 0,
		input: "",
		lines: [ NOTICE_LINE ],
		welcome: "",
		copy: key => key,
		size: () => [ 16, 16 ],
		text: () => [],
		hover: null,
		pressed: null,
		measure: value => value.length * 7,
		...overrides
	} );
}

test("a notice row links the player and the item only with the row on", () => {
	const off = layoutWith( { noticeLinks: false } );
	assert.ok( !off.controls.some( c => c.id.startsWith( "notice-" ) ) );
	const on = layoutWith( { noticeLinks: true } );
	// The row holding the player (the line may wrap onto a second row).
	const row = on.controls.find( c => c.id.startsWith( "chat-line:" ) && c.label.includes( "Looter" ) );
	const name = on.controls.find( c => c.id === "notice-name:5" );
	const item = on.controls.find( c => c.id === "notice-item:5" );
	assert.ok( row && name && item );
	assert.equal( name.whisperTarget, "Looter" );
	assert.equal( name.kind, "button" );
	assert.deepEqual( name.rect, [ row.rect[0] + row.label.indexOf( "Looter" ) * 7, row.rect[1], 6 * 7, 14 ] );
	assert.equal( item.whisperTarget, undefined );
	assert.deepEqual( item.rect, [ row.rect[0] + row.label.indexOf( "Potion" ) * 7, row.rect[1], 6 * 7, 14 ] );
	// The hovered link underlines; nothing else does.
	const plainInk = on.quads.filter( q => q.texture === "" ).length;
	const hovered = layoutWith( { noticeLinks: true, hover: "notice-item:5" } );
	const underline = hovered.quads.filter( q => q.texture === "" );
	assert.equal( underline.length, plainInk + 1 );
	assert.deepEqual( underline.at( -1 )?.rect, [ item.rect[0], item.rect[1] + 13, item.rect[2], 1 ] );
});

test("the linked spans follow the player first, then the item", () => {
	assert.deepEqual(
		noticeRowLinks( "(N):Looter advanced Potion to +9", { name: "Looter", item: { name: "Potion" } } ),
		[
			{ kind: "name", at: 4, value: "Looter" },
			{ kind: "item", at: 20, value: "Potion" }
		]
	);
	assert.deepEqual( noticeRowLinks( "from Tiger Girl", { name: "Looter", item: { name: "Potion" } } ), [] );
	assert.deepEqual( noticeRowLinks( "(N):Looter has dropped Sword", { name: "Looter", item: { name: "Potion" } } ), [
		{ kind: "name", at: 4, value: "Looter" }
	] );
});

test("a linked item shows the bag's own tooltip", () => {
	// Only the two fields tooltipItems reads for a notice.
	const game = /** @type {any} */ ({ inventory: [], chat: { lines: [ NOTICE_LINE ] } });
	assert.deepEqual( tooltipItems( "notice-item:5", game, 0 ), [ POTION ] );
	assert.deepEqual( tooltipItems( "notice-item:5:1", game, 0 ), [ POTION ] );
	assert.deepEqual( tooltipItems( "notice-item:6", game, 0 ), [] );
});

test("a multi-word item wrapped across two rows links and underlines both rows", () => {
	const spear = { ...POTION, name: "Heaven's Cloud Spear" };
	const wrappedLine = {
		channel: 7,
		name: "",
		text: "Star has dropped Heaven's Cloud Spear (Seal Of Star) from Tiger Girl",
		outgoing: false,
		sequence: 9,
		notice: { name: "Star", item: spear }
	};
	const on = layoutWith( { noticeLinks: true, lines: [ wrappedLine ] } );
	const names = on.controls.filter( c => c.id.startsWith( "notice-name:9" ) );
	const items = on.controls.filter( c => c.id.startsWith( "notice-item:9" ) );
	assert.equal( names.length, 1, "player name 'Star' is not matched again inside '(Seal Of Star)' on row 2" );
	assert.deepEqual( items.map( c => [ c.id, c.label ] ), [
		[ "notice-item:9", "Heaven's Cloud" ],
		[ "notice-item:9:1", "Spear" ]
	] );
	const plainInk = on.quads.filter( q => q.texture === "" ).length;
	for ( const hover of [ "notice-item:9", "notice-item:9:1" ] ) {
		const hovered = layoutWith( { noticeLinks: true, lines: [ wrappedLine ], hover } );
		assert.equal( hovered.quads.filter( q => q.texture === "" ).length, plainInk + 2 );
	}
	assert.deepEqual(
		noticeWrappedLinks(
			"(N):Star has dropped Heaven's Cloud Spear (Seal Of Star)",
			[ "(N):Star has dropped Heaven's", "Cloud Spear (Seal Of Star)" ],
			{ name: "Star", item: spear }
		),
		[
			[ { kind: "name", at: 4, value: "Star" }, { kind: "item", at: 21, value: "Heaven's" } ],
			[ { kind: "item", at: 0, value: "Cloud Spear", part: 1 } ]
		]
	);
});

test("links in notices are on unless explicitly false", () => {
	for ( const value of [ undefined, null, {}, { noticeLinks: 1 }, { noticeLinks: "false" } ] ) {
		assert.equal( experimentalOptions( value ).noticeLinks, true );
	}
	assert.equal( experimentalOptions( { noticeLinks: false } ).noticeLinks, false );
});

test("Seal of Sun drop chat lines render in bold gold while Star, Moon and plus lines stay pink and regular", () => {
	const withRarity = ( rarity, itemClass ) => ({
		...POTION,
		tooltip: { descriptionSymbol: "", fields: { rarity, itemClass } }
	});
	const sunItem = withRarity( 2, 3 ),
		starItem = withRarity( 2, 1 ),
		moonItem = withRarity( 2, 2 );
	const sunLine = {
			channel: 7,
			name: "",
			text: "Looter has dropped Potion (Seal Of Sun) from Tiger Girl",
			outgoing: false,
			sequence: 10,
			notice: { name: "Looter", item: sunItem }
		},
		sunFallbackLine = {
			channel: 7,
			name: "",
			text: "Looter has dropped Potion (Seal Of Sun) from Tiger Girl",
			outgoing: false,
			sequence: 11
		},
		starLine = {
			...NOTICE_LINE,
			sequence: 12,
			notice: { name: "Looter", item: starItem }
		},
		moonLine = {
			...NOTICE_LINE,
			text: "Looter has dropped Potion (Seal Of Moon) from Tiger Girl",
			sequence: 13,
			notice: { name: "Looter", item: moonItem }
		},
		plusLine = {
			channel: 7,
			name: "",
			text: "Looter advanced Potion (Seal Of Sun) to +8",
			outgoing: false,
			sequence: 14,
			notice: { name: "Looter", item: { ...sunItem, plus: 8 } }
		};
	assert.equal( isSunDropChatLine( sunLine ), true );
	assert.equal( isSunDropChatLine( sunFallbackLine ), true );
	assert.equal( isSunDropChatLine( starLine ), false );
	assert.equal( isSunDropChatLine( moonLine ), false );
	assert.equal( isSunDropChatLine( plusLine ), false );
	assert.equal( isSunDropChatLine( { ...sunFallbackLine, channel: 1 } ), false );
	const calls = [], measuredStyles = [];
	const out = layoutWith( {
		noticeLinks: true,
		lines: [ sunLine, starLine ],
		measure: ( value, style ) => {
			measuredStyles.push( style?.fontStyle ?? 0 );
			return value.length * (style?.fontStyle === 2 ? 8 : 7);
		},
		text: ( value, r, clip, color, style ) => {
			if ( value.includes( "Looter" ) || value.includes( "Tiger" ) ) {
				calls.push( { value, color, style } );
			}
			return [];
		}
	} );
	const sunCalls = calls.filter( c => c.value.includes( "(Seal Of Sun)" ) || c.color[1] === sunDropChatColor()[1] );
	const starCalls = calls.filter( c => c.color[1] !== sunDropChatColor()[1] );
	assert.ok( sunCalls.length > 0 );
	for ( const call of sunCalls ) {
		assert.deepEqual( call.color, [ 1, 215 / 255, 80 / 255, 1 ] );
		assert.deepEqual( call.style, { fontStyle: 2 } );
	}
	assert.ok( starCalls.length > 0 );
	for ( const call of starCalls ) {
		assert.deepEqual( call.color, [ 1, 174 / 255, 195 / 255, 1 ] );
		assert.equal( call.style, undefined );
	}
	assert.ok( measuredStyles.includes( 2 ) );
	const sunItemCtrl = out.controls.find( c => c.id === "notice-item:10" );
	const starItemCtrl = out.controls.find( c => c.id === "notice-item:12" );
	assert.equal( sunItemCtrl?.rect[2], 6 * 8 );
	assert.equal( starItemCtrl?.rect[2], 6 * 7 );
});
