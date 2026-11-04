/*
===========================================================================

mall-use.test.mjs - the mall use confirmation and the gacha win banner

Two of #2292's remaining rows. 52F460 kind 0x1F (opened by the bag
activation 561D50 at 562177) asks UIIT_MSG_QUESTION_SILKMALL_ITEM_USE
before a gated item is used - the gate is the native rule (type 3/3/13,
CanUse & 0x81 == 0x81, TID4 not 8/12/15) - Yes runs the use, No or
Escape runs nothing - and the 768530 frame banners every Magic Pang win
it receives (UIIT_MSG_GACHA_ANNOUNCE with the prize name and its
quantity).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const { isMallUseConfirmItem } = await import( "../../src/engine/foundation/gameplay/mall-use.ts" );
const { createMallUseHud } = await import( "../../src/engine/runtime/ui/hud/mall-use-hud.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
glyphsFor

The glyphs of one string in the last scene, by atlas uv chain (the
ui-fixture hasText pattern, keeping the quads).
================
*/
function glyphsFor( fixture, value ) {
	const face = fontAtlas.fonts["0"],
		pattern = Array.from( value, c => {
			const g = face.glyphs[c.codePointAt( 0 )] ?? face.glyphs["63"];
			return [
				g.x / fontAtlas.atlasWidth,
				g.y / fontAtlas.atlasHeight,
				g.width / fontAtlas.atlasWidth,
				g.height / fontAtlas.atlasHeight
			].join( "," );
		} ),
		glyphs = fixture.scenes.at( -1 )?.quads.filter( q => q.texture === fontAtlas.image ) ?? [],
		uvs = glyphs.map( q => q.uv.join( "," ) ),
		found = [];
	for ( let start = 0; start + pattern.length <= uvs.length; start++ ) {
		if ( pattern.every( ( uv, i ) => uvs[start + i] === uv ) ) {
			found.push( glyphs.slice( start, start + pattern.length ) );
		}
	}
	return found;
}

/*
================
words

The packed type words of the gate's rows: TID1 3 (bits 2-4), TID2 3
(bits 5-6), TID3 13 (bits 7-10), then the TID4 under test.
================
*/
function words( tid4 ) {
	return 0x6ec | tid4 << 11;
}

/*
================
mallBag

The confirmation flow's bag: a gold-time ticket on slot 13 (3/3/13/4,
CanUse 129, so it asks) and a plain potion beside it on slot 14.
================
*/
function mallBag() {
	return {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventory: [
			{
				slot: 13,
				refObjId: 1,
				typeFlags: words( 4 ),
				nativeFields: { canUse: 129 },
				quantity: 1,
				plus: 0,
				durability: 0,
				variance: "0",
				magic: [],
				name: "Gold Time",
				icon: "item/etc/hp_potion_01.ddj"
			},
			{
				slot: 14,
				refObjId: 2,
				typeFlags: 0x6c,
				quantity: 3,
				plus: 0,
				durability: 0,
				variance: "0",
				magic: [],
				name: "Potion",
				icon: "item/etc/hp_potion_01.ddj"
			}
		]
	};
}

/*
================
openBag

Warms the fixture and opens its inventory window.
================
*/
function openBag( fixture ) {
	for ( let t = 0; t < 1200; t += 100 ) fixture.ui.step( fixture.state, t );
	fixture.ui.event( { kind: "key", code: "KeyI" } );
	fixture.ui.step( fixture.state, 1300 );
}

test("the mall gate is the native rule (3/3/13, CanUse 129, no picker kinds)", () => {
	const [ticket, potion] = mallBag().inventory;
	// A gold-time ticket asks.
	assert.equal( isMallUseConfirmItem( defined( ticket ) ), true );
	// ITEM_MALL_HP_INC_430_POTION (3/3/13/1, CanUse 1) does not: the low
	// CanUse bit alone is a plain, immediate use.
	assert.equal(
		isMallUseConfirmItem( { ...defined( ticket ), typeFlags: words( 1 ), nativeFields: { canUse: 1 } } ),
		false
	);
	// ITEM_MALL_EQUIP_TRANSGENDER_1 (TID4 8) does not either: that kind
	// goes to the target picker, whatever CanUse says.
	for ( const tid4 of [ 8, 12, 15 ] ) {
		assert.equal(
			isMallUseConfirmItem( { ...defined( ticket ), typeFlags: words( tid4 ) } ),
			false,
			`TID4 ${tid4} is a picker kind`
		);
	}
	// An ITEM_QNO_ row with CanUse 129 and TID4 7 asks: the ask was
	// never about the mall family.
	assert.equal( isMallUseConfirmItem( { ...defined( ticket ), typeFlags: words( 7 ) } ), true );
	// Off type or missing flags: no ask.
	assert.equal( isMallUseConfirmItem( { ...defined( ticket ), typeFlags: words( 4 ) - 0x80 } ), false );
	assert.equal(
		isMallUseConfirmItem( { ...defined( ticket ), nativeFields: { canUse: 128 } } ),
		false
	);
	assert.equal( isMallUseConfirmItem( { ...defined( ticket ), nativeFields: {} } ), false );
	assert.equal( isMallUseConfirmItem( defined( potion ) ), false );
});

test("using a mall item from the bag asks first and Yes runs the use", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		Object.assign( f.state.gameplay, mallBag() );
		openBag( f );
		// A plain potion sends at once, anywhere near no confirmation.
		f.ui.event( { kind: "double-activate", id: "slot:14" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "item-use", slot: 14 } } ] );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.equal( sent.length, 1, "the mall use waits for confirmation" );
		const scene = defined( f.ui.step( f.state, 1400 ) );
		assert.ok(
			scene.controls.some( c => c.id === "mall-use-confirm" ),
			"the kind 0x1F confirmation is admitted"
		);
		assert.ok(
			scene.controls.some( c => c.id === "mall-use-cancel" ),
			"the kind 0x1F cancellation is admitted"
		);
		assert.ok(
			glyphsFor( f, "Will you use the selected item?" ).length > 0,
			"the question line is drawn"
		);
		// No hides the box and sends nothing.
		f.ui.event( { kind: "activate", id: "mall-use-cancel" } );
		assert.equal( sent.length, 1, "No sends nothing" );
		assert.ok(
			!defined( f.ui.step( f.state, 1500 ) ).controls.some( c => c.id === "mall-use-confirm" ),
			"the box is gone"
		);
		// Asked again, Yes runs exactly the use.
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "activate", id: "mall-use-confirm" } );
		assert.deepEqual(
			sent.at( -1 ),
			{ kind: "gameplay", command: { kind: "item-use", slot: 13 } },
			"Yes runs the use"
		);
	} finally {
		f.dispose();
	}
});

test("Escape dismisses the mall confirmation without using", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		Object.assign( f.state.gameplay, mallBag() );
		openBag( f );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.ok(
			defined( f.ui.step( f.state, 1400 ) ).controls.some( c => c.id === "mall-use-confirm" ),
			"the confirmation is up"
		);
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.equal( sent.length, 0, "Escape sends nothing" );
		assert.ok(
			!defined( f.ui.step( f.state, 1500 ) ).controls.some( c => c.id === "mall-use-confirm" ),
			"the box is gone"
		);
	} finally {
		f.dispose();
	}
});

test("Enter confirms only when the chat box does not own the key", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		Object.assign( f.state.gameplay, mallBag() );
		openBag( f );
		// The player is mid-composition in the chat line when the bag
		// press raises the box; the composition's Enter belongs to the
		// chat box, not to the confirmation.
		f.ui.event( { kind: "edit", id: "chat-text", value: "x", start: 1, end: 1, composing: true } );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.ok(
			defined( f.ui.step( f.state, 1400 ) ).controls.some( c => c.id === "mall-use-confirm" ),
			"the confirmation is up"
		);
		f.ui.event( { kind: "key", code: "Enter" } );
		assert.equal( sent.length, 0, "Enter while composing sends nothing" );
		// The pointer path is unaffected by the composition, and its
		// success here proves the blocked Enter left the box up.
		f.ui.event( { kind: "activate", id: "mall-use-confirm" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "item-use", slot: 13 } } ] );
		// Asked again with the composition over, Enter runs the use.
		f.ui.event( { kind: "edit", id: "chat-text", value: "x", start: 1, end: 1, composing: false } );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "key", code: "Enter" } );
		assert.equal( sent.length, 2, "Enter after composition runs the use" );
		assert.deepEqual( sent.at( -1 ), { kind: "gameplay", command: { kind: "item-use", slot: 13 } } );
	} finally {
		f.dispose();
	}
});

test("the pending ask reconciles away with its row or the world", () => {
	const hud = createMallUseHud(), row = defined( mallBag().inventory[0] );
	hud.ask( row );
	assert.equal( hud.reconcile( [ row ], true ), false, "an unchanged row keeps the ask" );
	assert.ok( hud.pending() !== null );
	assert.equal( hud.reconcile( [ { ...row, refObjId: 2 } ], true ), true, "a changed row drops it" );
	assert.equal( hud.pending(), null );
	hud.ask( row );
	assert.equal( hud.reconcile( [], true ), true, "a row that left the bag drops it" );
	hud.ask( row );
	assert.equal( hud.reconcile( [ row ], false ), true, "leaving the world drops it" );
	assert.equal( hud.pending(), null );
});

test("a gacha winner frame banners the prize name and count", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {
		refItemSnapshot: [ {
			refObjId: 3884,
			typeFlags: 0x6c,
			name: "Gold Time",
			codename: "ITEM_MALL_GOLD_TIME_SERVICE_TICKET_4W",
			icon: "item/etc/hp_potion_01.ddj"
		} ]
	} );
	g.seed( { gid: 1, refObjId: 1, kind: "player", name: "Tester", regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	g.take();
	// 768530 reads {u32 prizeRef, u16 quantity}: prize 3884, two of it.
	const payload = Uint8Array.of( 0x2c, 0x0f, 0, 0, 2, 0 );
	g.receive( { opcode: 0x30c0, payload }, 0 );
	assert.deepEqual( { ...defined( defined( g.take() ).notices ).at( -1 ), sequence: undefined }, {
		key: "UIIT_MSG_GACHA_ANNOUNCE",
		value: 2,
		arguments: [ "Gold Time" ],
		formatKinds: [ "s", "d" ],
		notificationBanner: true,
		bannerOnly: true,
		sequence: undefined
	} );
	assert.throws( () => g.receive( { opcode: 0x30c0, payload: payload.slice( 0, 5 ) }, 0 ) );
	// An unknown prize still banners, with an empty name.
	g.receive( { opcode: 0x30c0, payload: Uint8Array.of( 9, 9, 9, 9, 1, 0 ) }, 0 );
	assert.equal( defined( defined( g.take() ).notices ).at( -1 )?.arguments?.[0], "" );
});
