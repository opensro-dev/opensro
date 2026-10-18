/*
===========================================================================

guild-rank.test.mjs - the guild grade column shows rank, never the grant

Native CIFGuildMemberSlot_PopulateFromMember (0x5F2ED0) renders control 13
from the member's grade byte: 0 -> UIIT_STT_LEADER, 5 -> UIIT_STT_GUILD_STAFF,
10 -> UIIT_STT_GUILDSMAN, anything else gets no text. The grant name belongs
only to the name column's grant mode. The same function colours the whole
row: gold for the master (bright online, dark offline), white online and
grey offline for every other grade.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import test from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { guildRankSymbol, guildMemberTextColor } = await load(
	"src/engine/foundation/gameplay/social-roster.ts"
);
const { emptySocial } = await load( "src/engine/foundation/gameplay/social.ts" );

test("grade bytes map to the native rank symbols, other grades to no text", () => {
	assert.equal( guildRankSymbol( 0 ), "UIIT_STT_LEADER" );
	assert.equal( guildRankSymbol( 5 ), "UIIT_STT_GUILD_STAFF" );
	assert.equal( guildRankSymbol( 10 ), "UIIT_STT_GUILDSMAN" );
	for ( const grade of [ 1, 2, 4, 6, 9, 11, 255 ] ) {
		assert.equal( guildRankSymbol( grade ), null, `grade ${grade} has no rank text` );
	}
});

test("row colours follow the native master gold and member white/grey", () => {
	// Master: 0xFFFFB541 online, 0xFFA17233 offline. Other grades:
	// 0xFFFFFFFF online (mode 5, render state 0), 0xFF808080 offline
	// (mode 3 slot, render state 3).
	assert.deepEqual( guildMemberTextColor( 0, 0 ), [ 1, 0xb5 / 255, 0x41 / 255, 1 ] );
	assert.deepEqual( guildMemberTextColor( 0, 1 ), [ 0xa1 / 255, 0x72 / 255, 0x33 / 255, 1 ] );
	assert.deepEqual( guildMemberTextColor( 5, 0 ), [ 1, 1, 1, 1 ] );
	assert.deepEqual( guildMemberTextColor( 1, 0 ), [ 1, 1, 1, 1 ] );
	assert.deepEqual( guildMemberTextColor( 10, 1 ), [ 0x80 / 255, 0x80 / 255, 0x80 / 255, 1 ] );
	assert.deepEqual( guildMemberTextColor( 5, 1 ), [ 0x80 / 255, 0x80 / 255, 0x80 / 255, 1 ] );
});

/*
================
guildState

One member per rank case, all carrying a distinctive grant name.
================
*/
function guildState() {
	const members = [
		{
			id: 1,
			name: "Alpha",
			grade: 0,
			level: 90,
			permissions: 31,
			donated: 100,
			grant: "ZZGRANT",
			model: 1907,
			role: 1,
			offline: 0
		},
		{
			id: 2,
			name: "Bravo",
			grade: 5,
			level: 80,
			permissions: 31,
			donated: 90,
			grant: "ZZGRANT",
			model: 1907,
			role: 1,
			offline: 0
		},
		{
			id: 3,
			name: "Carol",
			grade: 10,
			level: 70,
			permissions: 0,
			donated: 80,
			grant: "ZZGRANT",
			model: 1907,
			role: 0,
			offline: 1
		},
		{
			id: 4,
			name: "Delta",
			grade: 1,
			level: 60,
			permissions: 0,
			donated: 70,
			grant: "ZZGRANT",
			model: 1907,
			role: 0,
			offline: 0
		}
	];
	return {
		...emptySocial( members[0].name ),
		self: 1,
		guild: { id: 7, name: "Native Guild", level: 5, gp: 12345, subject: "", contents: "", crest: 0, members },
		alliances: [],
		wars: []
	};
}

/*
================
visibleText

Decode the painted glyph runs whose quads land inside the viewport. (The
title dialog's edit prefill parks the current grant off-screen at negative
coordinates; parked text is not shown and does not count.)
================
*/
function visibleText( fixture, width, height ) {
	const face = fontAtlas.fonts["0"];
	const byKey = new Map();
	for ( const [cp, g] of Object.entries( face.glyphs ) ) {
		byKey.set(
			[
				g.x / fontAtlas.atlasWidth,
				g.y / fontAtlas.atlasHeight,
				g.width / fontAtlas.atlasWidth,
				g.height / fontAtlas.atlasHeight
			]
				.join( "," ),
			String.fromCodePoint( Number( cp ) )
		);
	}
	const glyphs = defined( fixture.scenes.at( -1 ) ).quads.filter( q =>
		q.texture === fontAtlas.image && q.rect[0] >= 0 && q.rect[1] >= 0 && q.rect[0] < width && q.rect[1] < height
	);
	let text = "", lastY = null, lastX = null;
	for ( const q of glyphs ) {
		if ( lastY === null || Math.abs( q.rect[1] - lastY ) > 2 || (lastX !== null && q.rect[0] < lastX - 1) ) {
			text += "\n";
		}
		text += byKey.get( q.uv.join( "," ) ) ?? "?";
		lastY = q.rect[1];
		lastX = q.rect[0];
	}
	return text;
}

/*
================
glyphColors

The colours of one painted word's glyphs inside a rect, in paint order.
(The word also appears in headers and parked text; the row's own cell is
the one inside the member row's rect.)
================
*/
function glyphColors( fixture, value, rect ) {
	const face = fontAtlas.fonts["0"];
	const pattern = Array.from( value, c => {
		const g = face.glyphs[c.codePointAt( 0 )] ?? face.glyphs["63"];
		return [
			g.x / fontAtlas.atlasWidth,
			g.y / fontAtlas.atlasHeight,
			g.width / fontAtlas.atlasWidth,
			g.height / fontAtlas.atlasHeight
		]
			.join( "," );
	} );
	const quads = defined( fixture.scenes.at( -1 ) ).quads.filter( q =>
		q.texture === fontAtlas.image &&
		q.rect[0] >= rect[0] && q.rect[0] < rect[0] + rect[2] &&
		q.rect[1] >= rect[1] && q.rect[1] < rect[1] + rect[3]
	);
	const keys = quads.map( q => q.uv.join( "," ) );
	for ( let start = 0; start + pattern.length <= keys.length; start++ ) {
		if ( pattern.every( ( uv, i ) => keys[start + i] === uv ) ) {
			return quads.slice( start, start + pattern.length ).map( q => q.color );
		}
	}
	return [];
}

for ( const [width, height] of [ [ 1600, 900 ], [ 375, 667 ] ] ) {
	test(`the member list paints rank text, not grant names, at ${width}x${height}`, () => {
		const fixture = uiFixture();
		let time = 0, published;
		/*
		================
		draw
		================
		*/
		function draw() {
			published = fixture.ui.step( fixture.state, ++time * 100 ) ?? published;
			assert.equal( fixture.ui.stats().error, null, "Guild draw must not fail" );
			return defined( published );
		}
		try {
			Object.assign( fixture.state.gameplay, { social: guildState() } );
			fixture.state.width = width;
			fixture.state.height = height;
			draw();
			fixture.ui.event( { kind: "activate", id: "mall-notice-close" } );
			draw();
			fixture.ui.event( { kind: "activate", id: "open-window:Guild" } );
			draw();
			fixture.ui.event( { kind: "activate", id: "guild-tab:0" } );
			const output = draw();
			assert.ok( fixture.hasText( "Master" ), "grade 0 paints its rank" );
			assert.ok( fixture.hasText( "Vice master" ), "grade 5 paints its rank" );
			assert.ok( fixture.hasText( "Member" ), "grade 10 paints its rank" );
			assert.ok(
				!visibleText( fixture, width, height ).includes( "ZZGRANT" ),
				"the grant name is never shown in the member list"
			);
			const rowOf = id => defined( output.controls.find( c => c.id === "social-member:" + id ) ).rect;
			const master = glyphColors( fixture, "Master", rowOf( 1 ) );
			assert.ok( master.length > 0, "master rank glyphs painted" );
			for ( const color of master ) {
				assert.deepEqual( color, [ 1, 0xb5 / 255, 0x41 / 255, 1 ], "master row is native gold" );
			}
			const vice = glyphColors( fixture, "Vice master", rowOf( 2 ) );
			assert.ok( vice.length > 0, "vice master rank glyphs painted" );
			for ( const color of vice ) {
				assert.deepEqual( color, [ 1, 1, 1, 1 ], "online vice master row is native white" );
			}
			const member = glyphColors( fixture, "Member", rowOf( 3 ) );
			assert.ok( member.length > 0, "member rank glyphs painted" );
			for ( const color of member ) {
				assert.deepEqual(
					color,
					[ 0x80 / 255, 0x80 / 255, 0x80 / 255, 1 ],
					"offline member row is native grey"
				);
			}
		} finally {
			fixture.dispose();
		}
	});
}
