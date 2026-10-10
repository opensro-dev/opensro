/*
===========================================================================

stall-presentation.test.mjs - native trading-state branches and warm admission

The real HUD must replace the invalid authored icon before admission, draw
the native open/closed resource and opaque chat panes, and preserve
owner/visitor control gates.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
const { emptyStall } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { stallTradingPresentation } = await import( "../../src/engine/runtime/ui/hud/stall-hud.ts" );

const ICON_ROOT = "/assets/images/Media_extracted/interface/stall/";
const ITEM_ICON = "/assets/images/Media_extracted/icon/item/etc/hp_potion_01.png";
const PRICE_BACKGROUND = "/assets/images/Media_extracted/interface/messagebox/msgbox_iteminfo.png";
const CHAT_BACKGROUND = "/assets/images/Media_extracted/interface/ifcommon/bg_tile/com_bg_tile_e.png";
// Chat module interiors and ifstall display inside its 12px frame, relative to the stall.
const STALL_INTERIORS = [
	{ name: "message", rect: [ 34, 369, 281, 86 ] },
	{ name: "member", rect: [ 344, 369, 89, 86 ] },
	{ name: "item grid", rect: [ 34, 120, 399, 192 ] }
];
// Both retail slot DDS/PNG variants have transparent right/bottom padding.
const SLOT_IMAGE_SIZE = [ 208, 44 ];
const SLOT_OPAQUE_SIZE = [ 205, 41 ];

/*
================
glyphWindows
================
*/
function glyphWindows( value ) {
	return Array.from( value, character => {
		const glyph = fontAtlas.fonts["0"].glyphs[character.codePointAt( 0 )];
		return [
			glyph.x / fontAtlas.atlasWidth,
			glyph.y / fontAtlas.atlasHeight,
			glyph.width / fontAtlas.atlasWidth,
			glyph.height / fontAtlas.atlasHeight
		];
	} );
}

/*
================
assertStallBackgrounds

Require retail opaque coverage over both chat panes and the entire item-grid
interior. Slot padding contributes no coverage; row seams and the center gutter
must be painted by the natural slot art and the authored black divider.
================
*/
function assertStallBackgrounds( quads, origin ) {
	const backgrounds = quads.flatMap( quad => {
		if ( !quad.color.every( channel => channel === 1 ) ) return [];
		if ( quad.texture === CHAT_BACKGROUND ) return [ quad ];
		if ( quad.texture !== ICON_ROOT + "stl_slot_02.png" && quad.texture !== ICON_ROOT + "stl_slot_05.png" ) {
			return [];
		}
		assert.deepEqual( quad.rect.slice( 2 ), SLOT_IMAGE_SIZE, "slot artwork retains its native extent" );
		return [ { ...quad, rect: [ ...quad.rect.slice( 0, 2 ), ...SLOT_OPAQUE_SIZE ] } ];
	} );
	for ( const pane of STALL_INTERIORS ) {
		const [dx, dy, width, height] = pane.rect, x = origin[0] + dx, y = origin[1] + dy;
		const painted = new Uint8Array( width * height );
		for ( const quad of backgrounds ) {
			const r = quad.rect, c = quad.clip;
			const left = Math.max( x, r[0], c[0] ), top = Math.max( y, r[1], c[1] );
			const right = Math.min( x + width, r[0] + r[2], c[0] + c[2] );
			const bottom = Math.min( y + height, r[1] + r[3], c[1] + c[3] );
			if ( right <= left || bottom <= top ) continue;
			for ( let row = Math.ceil( top - y ); row < bottom - y; row++ ) {
				painted.fill( 1, row * width + Math.ceil( left - x ), row * width + Math.floor( right - x ) );
			}
		}
		assert.ok(
			painted.every( pixel => pixel === 1 ),
			`${pane.name} has complete opaque retail background coverage`
		);
	}
}

for ( const owner of [ true, false ] ) {
	test(`${owner ? "owner" : "visitor"} stall admits and updates every trading-state branch`, t => {
		const f = uiFixture();
		t.after( () => f.dispose() );
		f.state.entities.push(
			{ ...f.state.entities[0], gid: 2, name: "Keeper" },
			{ ...f.state.entities[0], gid: 3, name: "Alice" },
			{ ...f.state.entities[0], gid: 4, name: "Bob" }
		);
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		let now = 100;
		for ( const open of [ false, true, false, true ] ) {
			const stall = {
				...emptyStall(),
				phase: owner ? "owner" : "visitor",
				owner: owner ? 1 : 2,
				open,
				visitors: [ 3, 99, 4, 3, 1 ],
				offers: [ {
					slot: 0,
					bagSlot: 13,
					quantity: 2,
					price: 12345,
					item: { slot: 13, refObjId: 1, quantity: 2, name: "Potion", icon: "item/etc/hp_potion_01.ddj" }
				} ]
			};
			Object.assign( f.state.gameplay, {
				stall,
				chat: { lines: [ { channel: 9, name: "Seller", text: "X".repeat( 80 ), outgoing: false } ] }
			} );
			let frame;
			for ( let i = 0; i < 20; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
			assert.ok( frame );
			const control = id => frame.controls.find( row => row.id === id );
			assert.ok( control( owner ? "stall-close" : "stall-leave" ), "stall admits with retail artwork" );
			assert.equal( frame.controls.filter( row => row.id === "main-popup-drag" ).length, 1 );
			assert.equal( control( "stall-modify:0" )?.disabled, !owner || open );
			assert.equal( control( "stall-modify:1" ), undefined, "empty offers have no price button" );
			assert.equal( control( "stall-slot:0" )?.disabled, owner ? open : !open );
			assert.equal( control( "stall-slot:1" )?.disabled, owner ? open : true );
			assert.equal( !!control( "stall-trading" ), owner );
			assert.equal( control( "stall-change-title" )?.disabled, owner ? open : undefined );
			const quads = f.scenes.at( -1 )?.quads ?? [];
			const drag = control( "window-drag:Stall" );
			assert.ok( drag );
			assertStallBackgrounds( quads, [ drag.rect[0] - 10, drag.rect[1] ] );
			const origin = [ drag.rect[0] - 10, drag.rect[1] ];
			assert.deepEqual( control( "stall-slot:0" )?.rect, [ origin[0] + 31, origin[1] + 117, 32, 32 ] );
			assert.deepEqual( control( "stall-slot:9" )?.rect, [ origin[0] + 237, origin[1] + 281, 32, 32 ] );
			assert.deepEqual( control( "stall-chat-text" )?.rect, [ origin[0] + 31, origin[1] + 443, 270, 20 ] );
			const messageClip = [ origin[0] + 31, origin[1] + 365, 270, 75 ];
			assert.ok(
				f.products.at( -1 )?.quads.some( quad =>
					quad.run && quad.clip.every( ( value, i ) => value === messageClip[i] )
				),
				"stall messages remain inside the native left message pane"
			);
			const memberClip = [ origin[0] + 340, origin[1] + 366, 80, 90 ];
			const memberRuns = f.products.at( -1 )?.quads.filter( quad =>
				quad.run && quad.clip.every( ( value, i ) =>
					value === memberClip[i]
				)
			) ?? [];
			const memberNames = owner ? [ "Player", "Alice", "Bob" ] : [ "Keeper", "Alice", "Bob", "Player" ];
			assert.deepEqual(
				memberRuns.map( quad => quad.run?.glyphs.map( glyph => glyph.uv ) ),
				memberNames.map( glyphWindows ),
				"roster paints the owner, known visitors in packet order, and the local visitor once"
			);
			assert.equal( quads.filter( row => row.texture === ICON_ROOT + "stl_slot_02.png" ).length, 1 );
			assert.equal( quads.filter( row => row.texture === ICON_ROOT + "stl_slot_05.png" ).length, 9 );
			assert.ok( f.hasText( "12,345" ), "price uses native three-digit grouping" );
			const expected = ICON_ROOT + `stl_condition_icon_${open ? "01" : "02"}.png`;
			assert.ok( quads.some( row => row.texture === expected ), expected );
			assert.ok(
				!quads.some( row => row.texture === ICON_ROOT + `stl_condition_icon_${open ? "02" : "01"}.png` )
			);
			assert.ok(
				quads.some( row => row.texture === ITEM_ICON && row.color[3] === (!owner && !open ? 128 / 255 : 1) )
			);
			assert.ok(
				!f.requested.includes( ICON_ROOT + "stl_condition_icon_1.png" ),
				"authored placeholder is never requested"
			);
			const presentation = stallTradingPresentation( owner, open );
			assert.equal( presentation.status, open ? "UIIT_STT_TRADING_NOW" : "UIIT_STT_STALL_MODIFYING" );
			assert.equal( presentation.toggle, open ? "UIIT_STT_END_STALL" : "UIIT_STT_START_STALL" );
			stall.offers = [];
			for ( let i = 0; i < 20; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
			const emptyQuads = f.scenes.at( -1 )?.quads ?? [];
			assertStallBackgrounds( emptyQuads, origin );
			assert.equal( emptyQuads.filter( quad => quad.texture === ICON_ROOT + "stl_slot_05.png" ).length, 10 );
		}
	});
}

test("stall chat wraps native rows, scrolls retained messages, and clears on reentry", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	f.ui.step( f.state, 0 );
	const stall = { ...emptyStall(), phase: "owner", owner: 1 };
	let now = 0, frame;
	const draw = () => {
		for ( let i = 0; i < 10; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
		assert.ok( frame );
		return frame;
	};
	const gameplay = Object.assign( f.state.gameplay, {
		stall,
		chat: { lines: [ { sequence: 1, channel: 9, name: "Seller", text: "X".repeat( 80 ), outgoing: false } ] }
	} );
	draw();
	const origin = () => {
		const drag = frame.controls.find( control => control.id === "window-drag:Stall" );
		assert.ok( drag );
		return [ drag.rect[0] - 10, drag.rect[1] ];
	};
	const runs = () => {
		const [x, y] = origin(), clip = [ x + 31, y + 365, 270, 75 ];
		return f.products.at( -1 )?.quads.filter( quad =>
			quad.run && quad.clip.every( ( value, i ) => value === clip[i] )
		) ?? [];
	};
	assert.ok( runs().length > 1, "a long token wraps instead of losing its clipped suffix" );
	assert.deepEqual(
		runs().flatMap( quad => quad.run.glyphs.map( glyph => glyph.uv ) ),
		glyphWindows( "Seller:" + "X".repeat( 80 ) )
	);
	// The bitmap run has a three-pixel vertical inset within its 15px row.
	assert.equal( runs().at( -1 ).rect[1], origin()[1] + 428, "short chat lists end on the fifth 15px row" );
	assert.ok( runs().every( quad => quad.rect[2] <= 270 ) );
	gameplay.chat = {
		lines: Array.from( { length: 60 }, ( _, i ) => ({
			sequence: i + 2,
			channel: 9,
			name: "Seller",
			text: i === 10 ? "X".repeat( 80 ) : "Message" + i,
			outgoing: false
		}) )
	};
	draw();
	assert.deepEqual(
		runs().map( quad => quad.run.glyphs.map( glyph => glyph.uv ) ),
		[ 55, 56, 57, 58, 59 ].map( i => glyphWindows( "Seller:Message" + i ) )
	);
	f.ui.event( { kind: "drag", id: "stall-chat-scroll-thumb", dx: 0, dy: -10000 } );
	draw();
	const oldest = runs(), wrapped = glyphWindows( "Seller:" + "X".repeat( 80 ) );
	assert.deepEqual(
		oldest.flatMap( quad => quad.run.glyphs.map( glyph => glyph.uv ) ).slice( 0, wrapped.length ),
		wrapped,
		"50 logical messages remain before wrapping; the oldest still includes all its wrapped rows"
	);
	f.ui.event( { kind: "activate", id: "stall-chat-scroll-down" } );
	draw();
	assert.deepEqual(
		runs()[0].run.glyphs.map( glyph => glyph.uv ),
		oldest[1].run.glyphs.map( glyph => glyph.uv ),
		"the linked scroll button advances one painted row"
	);
	// Worker snapshots clone lines: sequence identity keeps unchanged history from appending again.
	gameplay.chat.lines = gameplay.chat.lines.map( line => ({ ...line }) );
	gameplay.chat.lines.push( { sequence: 62, channel: 9, name: "Seller", text: "New", outgoing: false } );
	draw();
	assert.deepEqual(
		runs().at( -1 ).run.glyphs.map( glyph => glyph.uv ),
		glyphWindows( "Seller:New" ),
		"an arriving message returns the native scrollbar to the tail"
	);
	f.ui.event( { kind: "edit", id: "stall-chat-text", value: "Unsent draft", start: 12, end: 12, composing: false } );
	const drafted = draw();
	assert.equal( drafted.controls.find( control => control.id === "stall-chat-text" )?.value, "Unsent draft" );
	gameplay.stall = emptyStall();
	draw();
	gameplay.stall = stall;
	const reopened = draw();
	assert.equal( reopened.controls.find( control => control.id === "stall-chat-text" )?.value, "" );
	assert.equal( runs().length, 0, "reopening does not replay a previous stall's channel history" );
	for ( let gid = 2; gid <= 9; gid++ ) {
		f.state.entities.push( { ...f.state.entities[0], gid, name: "Member" + gid } );
	}
	stall.visitors = Array.from( { length: 8 }, ( _, i ) => i + 2 );
	draw();
	const memberRuns = () => {
		const [x, y] = origin(), clip = [ x + 340, y + 366, 80, 90 ];
		return f.products.at( -1 )?.quads.filter( quad =>
			quad.run && quad.clip.every( ( value, i ) => value === clip[i] )
		) ?? [];
	};
	assert.deepEqual(
		memberRuns().map( quad => quad.run.glyphs.map( glyph => glyph.uv ) ),
		[ "Player", "Member2", "Member3", "Member4", "Member5", "Member6" ].map( glyphWindows ),
		"the native member textbox starts with the first six names"
	);
	f.ui.event( { kind: "activate", id: "stall-member-scroll-down" } );
	draw();
	assert.deepEqual( memberRuns()[0].run.glyphs.map( glyph => glyph.uv ), glyphWindows( "Member2" ) );
	f.state.entities.push( { ...f.state.entities[0], gid: 10, name: "Member10" } );
	stall.visitors = [ ...stall.visitors, 10 ];
	draw();
	assert.deepEqual(
		memberRuns()[0].run.glyphs.map( glyph => glyph.uv ),
		glyphWindows( "Member2" ),
		"a new visitor preserves the manually selected first row"
	);
});

test("stall prompts use native edits, item artwork and nonoverlapping confirmation buttons", t => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	t.after( () => f.dispose() );
	const item = { slot: 13, refObjId: 1, quantity: 20, name: "Potion", icon: "item/etc/hp_potion_01.ddj" };
	Object.assign( f.state.gameplay, {
		inventory: [ item ],
		stall: {
			...emptyStall(),
			phase: "owner",
			owner: 1,
			offers: [ { slot: 0, bagSlot: 13, quantity: 2, price: 12345, item: { ...item, quantity: 2 } } ]
		}
	} );
	let now = 0, frame;
	const draw = () => {
		for ( let i = 0; i < 10; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
		assert.ok( frame );
	};
	const control = id => frame.controls.find( row => row.id === id );
	draw();
	for (
		const [opener, width, input, maximum, buttons] of /** @type {const} */ ([
			[ "stall-change-title", 280, [ 18, 70, 243, 20 ], 40, [ 55, 143 ] ],
			[ "stall-change-greeting", 420, [ 16, 68, 386, 20 ], 64, [ 128, 216 ] ]
		])
	) {
		f.ui.event( { kind: "activate", id: opener } );
		draw();
		const x = (1600 - width) / 2, y = (900 - 144) / 2;
		assert.deepEqual( control( "stall-prompt-text" )?.rect, [ x + input[0], y + input[1], input[2], input[3] ] );
		assert.equal( control( "stall-prompt-text" )?.maxLength, maximum );
		assert.deepEqual( control( "stall-prompt-ok" )?.rect, [ x + buttons[0], y + 104, 76, 24 ] );
		assert.deepEqual( control( "stall-prompt-cancel" )?.rect, [ x + buttons[1], y + 104, 76, 24 ] );
		const value = "W".repeat( maximum - 5 ) + "ABCDE", clip = control( "stall-prompt-text" ).rect;
		for (
			const [start, end, direction] of /** @type {const} */ ([
				[ maximum, maximum, undefined ],
				[ maximum - 5, maximum, undefined ],
				[ 0, 0, undefined ],
				[ 0, maximum, "backward" ],
				[ 0, maximum, "forward" ]
			])
		) {
			f.ui.event( { kind: "edit", id: "stall-prompt-text", value, start, end, direction, composing: false } );
			draw();
			const quads = f.scenes.at( -1 )?.quads.filter( quad =>
				quad.clip[0] >= clip[0] && quad.clip[1] >= clip[1] &&
				quad.clip[0] + quad.clip[2] <= clip[0] + clip[2] && quad.clip[1] + quad.clip[3] <= clip[1] + clip[3]
			) ?? [];
			const caret = quads.find( quad =>
				quad.texture === "" && quad.rect[2] === 1 && quad.color.every( c => c === 1 )
			);
			assert.ok( caret, "the focused overflowing prompt paints its caret" );
			assert.ok(
				caret.rect[0] >= clip[0] && caret.rect[0] + caret.rect[2] <= clip[0] + clip[2],
				"the active caret remains inside the prompt viewport at End and Home"
			);
			const visible = quads.filter( quad =>
				quad.texture === fontAtlas.image && quad.rect[0] >= quad.clip[0] &&
				quad.rect[0] + quad.rect[2] <= quad.clip[0] + quad.clip[2]
			).map( quad => quad.uv );
			if ( direction === "backward" ? start : end ) {
				assert.deepEqual( visible.slice( -5 ), glyphWindows( "ABCDE" ), "End reveals the complete suffix" );
			} else {
				assert.deepEqual( visible[0], glyphWindows( "W" )[0], "Home restores the beginning of the text" );
				assert.equal( caret.rect[0], clip[0], "Home and backward selection follow the active beginning" );
			}
			if ( end > start ) {
				const highlight = quads.find( quad => quad.texture === "" && quad.color[3] === .6 );
				assert.ok( highlight, "the visible suffix selection is painted" );
				if ( start ) assert.ok( highlight.rect[0] >= clip[0] );
				assert.equal(
					direction === "backward" ? highlight.rect[0] : highlight.rect[0] + highlight.rect[2],
					caret.rect[0],
					"the normalized highlight range retains its active endpoint"
				);
			}
		}
		f.ui.event( { kind: "activate", id: "stall-prompt-cancel" } );
		draw();
	}
	f.ui.event( { kind: "activate", id: "stall-modify:0" } );
	draw();
	const x = (1600 - 308) / 2, y = (900 - 148) / 2;
	assert.deepEqual( control( "stall-prompt-price" )?.rect, [ x + 127, y + 68, 121, 18 ] );
	assert.deepEqual( control( "stall-prompt-quantity" )?.rect, [ x + 20, y + 102, 42, 20 ] );
	assert.deepEqual( control( "stall-prompt-ok" )?.rect, [ x + 123, y + 101, 76, 24 ] );
	assert.deepEqual( control( "stall-prompt-cancel" )?.rect, [ x + 203, y + 101, 76, 24 ] );
	assert.ok(
		f.scenes.at( -1 )?.quads.some( quad =>
			quad.texture === ITEM_ICON && quad.rect.every( ( value, i ) => value === [ x + 25, y + 51, 32, 32 ][i] )
		),
		"the price prompt displays the offered item in its native icon frame"
	);
	const priceQuads = f.products.at( -1 )?.quads ?? [];
	const background = priceQuads.findLastIndex( quad => quad.texture === PRICE_BACKGROUND );
	assert.ok( background >= 0, "the price prompt paints the native item information background" );
	for (
		const [value, dx, dy, width, height] of /** @type {const} */ ([
			[ "Price", 77, 71, 43, 12 ],
			[ "Gold", 255, 72, 23, 12 ]
		])
	) {
		// Centered "Gold" is 24px wide in the native 23px label; allow its one-pixel overhang.
		const label = priceQuads.findIndex( quad =>
			quad.run && quad.rect[0] + quad.rect[2] / 2 >= x + dx &&
			quad.rect[0] + quad.rect[2] / 2 <= x + dx + width && quad.rect[1] >= y + dy &&
			quad.rect[1] + quad.rect[3] <= y + dy + height
		);
		assert.ok( label >= 0, `${value} paints at its native label rectangle` );
		assert.deepEqual( priceQuads[label].run.glyphs.map( glyph => glyph.uv ), glyphWindows( value ) );
		assert.ok( label > background, `${value} remains visible above the opaque item information background` );
	}
	f.ui.event( { kind: "edit", id: "stall-prompt-price", value: "9999999999", start: 10, end: 10, composing: false } );
	f.ui.event( { kind: "edit", id: "stall-prompt-quantity", value: "99", start: 2, end: 2, composing: false } );
	draw();
	assert.equal( control( "stall-prompt-price" )?.value, "1000000000" );
	assert.equal( control( "stall-prompt-quantity" )?.value, "20" );
	f.ui.event( { kind: "activate", id: "stall-prompt-cancel" } );
	draw();
	assert.equal( sent.length, 0, "cancelling each prompt leaves the stall unchanged" );
});
