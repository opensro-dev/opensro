/*
===========================================================================

confirm-box-text.test.mjs - the item-target confirmation keeps its text inside

Grass of Life and the clock confirm through the fixed 308x148 native box
(5C82D0). Its second line is wider than the box, so it wraps inside the
frame; drawn as one row it ran past the window on both sides.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

// ETC group 1, subtype 6: Grass of Life (cos-item-use.ts isRevivalGrass).
const GRASS_FLAGS = 0x6c | (1 << 7) | (6 << 11);

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

test("the item-target confirm keeps its two lines inside the 308x148 frame", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		Object.assign( f.state.gameplay, {
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [
				{
					slot: 13,
					refObjId: 1,
					typeFlags: GRASS_FLAGS,
					quantity: 1,
					plus: 0,
					durability: 0,
					variance: "0",
					magic: [],
					name: "Grass of Life",
					icon: "item/etc/hp_potion_01.ddj"
				},
				{
					slot: 14,
					refObjId: 2,
					typeFlags: 0x6c,
					quantity: 1,
					plus: 0,
					durability: 0,
					variance: "0",
					magic: [],
					name: "Potion",
					icon: "item/etc/hp_potion_01.ddj"
				}
			]
		} );
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.step( f.state, 1300 );
		// Using the grass only arms the target cursor; the target slot confirms.
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.equal( sent.length, 0, "the grass use waits for its target" );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		const scene = defined( f.ui.step( f.state, 1400 ) );
		const confirm = scene.controls.find( c => c.id === "cos-revive-confirm" );
		assert.ok( confirm, "the revive confirm is admitted" );
		// guildProposalLayout: the buttons sit at (72, 99) inside 308x148.
		const frame = [ confirm.rect[0] - 72, confirm.rect[1] - 99, 308, 148 ];
		for (
			const value of [
				"Will you cancel the use of the selected item?",
				"If you cancel the use of the selected item, its effect will be lost."
			]
		) {
			const runs = glyphsFor( f, value );
			assert.ok( runs.length > 0, "the line is drawn: " + value );
			for ( const run of runs ) {
				for ( const glyph of run ) {
					assert.ok(
						glyph.rect[0] >= frame[0] && glyph.rect[0] + glyph.rect[2] <= frame[0] + frame[2] &&
							glyph.rect[1] >= frame[1] && glyph.rect[1] + glyph.rect[3] <= frame[1] + frame[3],
						`"${value}" stays inside the frame at ${glyph.rect}`
					);
				}
			}
		}
		f.ui.event( { kind: "activate", id: "cos-revive-confirm" } );
		assert.deepEqual(
			sent.at( -1 ),
			{ kind: "gameplay", command: { kind: "item-use", slot: 13, revivalSlot: 14 } },
			"Yes still runs the targeted use"
		);
	} finally {
		f.dispose();
	}
});
