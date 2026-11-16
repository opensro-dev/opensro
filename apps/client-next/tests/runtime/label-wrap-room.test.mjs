/*
===========================================================================

label-wrap-room.test.mjs - a wrapped label stays clear of the rows beside it

The port-only Experimental "Fit window labels" wraps an authored label
wider than its box onto two lines. Two lines at the font's full extent
did not fit a box sized for one: centred on it they ran 8 pixels into the
row above and the row below, and where both neighbours wrapped their
lines crossed ("Blacksmith's workshop" over "Defending tool shop" in the
Fortress tax window, "Purchase amount" over "Profit amount" in the
special goods trade window, #3673). Each label now keeps to half the gap
to its neighbours (authored-layout.ts labelRoom) and stacks its lines as
tight as that needs (frame.ts wrappedLabelBand). The check is the
picture's: no two glyphs of the window cross, with the row on; off, the
labels keep the original one-line overflow.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const OFFICIAL = 17;
const TRADER = 18;
const TRANSPORT = 7;
// A trade goods type word (specialty-deal.ts isTradeGoods: TID 3/3/8).
const TRADE_GOODS_TYPE = 0x46c;
// Lines may share an edge; a crossing deeper than this is ink on ink.
const CROSS_SLACK = .5;
// Glyphs on one baseline further apart than this are separate texts (a
// space is about 3 pixels in font 0; two columns are tens apart).
const LINE_GAP = 8;
// Each glyph's height above its baseline, by its cell in the atlas: two
// glyphs of one line share a baseline, whatever their cells' tops.
const ORIGIN_Y = new Map(
	Object.values( fontAtlas.fonts ).flatMap( font =>
		Object.values( font.glyphs ).map( glyph => [ glyph.x + "," + glyph.y, glyph.originY ] )
	)
);

/*
================
openWindow

A fixture with the row set, opened by recipe; the last scene and the
published controls.
================
*/
function openWindow( fit, recipe ) {
	const f = uiFixture();
	let now = 0;
	/** @type {any} */
	let seen = null;
	const step = () => {
		for ( let i = 0; i < 8; i++ ) seen = f.ui.step( f.state, now += 200 ) ?? seen;
	};
	f.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: { fitWindowLabels: fit } }) );
	recipe( f, step );
	step();
	assert.equal( f.ui.stats().error, null );
	const scene = defined( f.scenes.at( -1 ) ), controls = seen?.controls ?? [];
	f.dispose();
	return { scene, controls };
}

/*
================
fortressTax

The manager's tax row with its answer in (5D8930 action 0x33 row 1).
================
*/
function fortressTax( f, step ) {
	f.state.entities.push( { ...f.state.entities[0], gid: OFFICIAL, refObjId: 2011, kind: "npc", name: "Official" } );
	Object.assign( f.state.gameplay, {
		target: OFFICIAL,
		npcConversation: { phase: "menu", gid: OFFICIAL },
		fortress: {
			...f.state.gameplay.fortress,
			worldId: 7,
			worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
			fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME", taxTargets: 63 } ],
			wars: [],
			registered: [],
			serviceSequence: 0
		}
	} );
	step();
	f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
	f.state.gameplay.fortress = {
		...f.state.gameplay.fortress,
		serviceSequence: 1,
		service: { action: 0, result: 1, fortress: 1, taxRate: 10, gold: "1234" }
	};
}

/*
================
specialtyDeal

A trade goods offer's deal box, with the transport's bag beside it.
================
*/
function specialtyDeal( f, step ) {
	f.state.entities.push( { ...f.state.entities[0], gid: TRADER, refObjId: 2011, kind: "npc", name: "Trader" } );
	f.state.gameplay.cosRecords = [
		{ gid: TRANSPORT, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false, inventory: [] }
	];
	Object.assign( f.state.gameplay, {
		target: TRADER,
		progression: { level: 40, gold: "10000000" },
		shop: {
			npc: TRADER,
			name: "Trader",
			cosGid: TRANSPORT,
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: 2001,
				name: "Trade goods",
				price: "100",
				maxStack: 10,
				items: [ { refObjId: 2001, typeFlags: TRADE_GOODS_TYPE, quantity: 10, name: "Trade goods" } ]
			} ]
		}
	} );
	step();
	f.ui.event( { kind: "activate", id: "open-window:Shop" } );
	step();
	f.ui.event( { kind: "activate", id: "shop-offer:0" } );
}

/*
================
baseline

A font quad's baseline: its top plus its glyph's height above it.
Undefined for a quad whose cell starts on no glyph's: a glyph cut by its
control's clip, which keeps inside that control and so crosses nothing.
================
*/
function baseline( quad ) {
	const x = Math.round( quad.uv[0] * fontAtlas.atlasWidth ), y = Math.round( quad.uv[1] * fontAtlas.atlasHeight );
	const originY = ORIGIN_Y.get( x + "," + y );
	return originY === undefined ? undefined : quad.rect[1] + originY;
}

/*
================
textLines

The drawn lines of text inside rect: glyphs on one baseline that follow
each other with no wider gap than LINE_GAP, as [left, top, right, bottom]
of their ink.
================
*/
function textLines( scene, rect ) {
	const glyphs = scene.quads.filter( q =>
		q.texture === fontAtlas.image && q.rect[0] >= rect[0] && q.rect[1] >= rect[1] &&
		q.rect[0] + q.rect[2] <= rect[0] + rect[2] && q.rect[1] + q.rect[3] <= rect[1] + rect[3]
	).map( q => ({ rect: q.rect, baseline: baseline( q ) }) ).filter( glyph => glyph.baseline !== undefined ).sort( (
		a,
		b
	) => a.baseline - b.baseline || a.rect[0] - b.rect[0] );
	const lines = [];
	let line = null, previous = null;
	for ( const glyph of glyphs ) {
		const [x, y, w, h] = glyph.rect;
		if ( !line || glyph.baseline !== previous.baseline || x - (previous.rect[0] + previous.rect[2]) > LINE_GAP ) {
			line = [ x, y, x + w, y + h ];
			lines.push( line );
		} else {
			line[1] = Math.min( line[1], y );
			line[2] = Math.max( line[2], x + w );
			line[3] = Math.max( line[3], y + h );
		}
		previous = glyph;
	}
	return lines;
}

/*
================
crossings

The pairs of text lines inside rect that share columns and whose ink
overlaps: one line's descent in the next line's letters, or one label
over another.
================
*/
function crossings( scene, rect ) {
	const lines = textLines( scene, rect ), out = [];
	for ( let i = 0; i < lines.length; i++ ) {
		for ( let j = i + 1; j < lines.length; j++ ) {
			const a = lines[i], b = lines[j];
			const w = Math.min( a[2], b[2] ) - Math.max( a[0], b[0] );
			const h = Math.min( a[3], b[3] ) - Math.max( a[1], b[1] );
			if ( w > CROSS_SLACK && h > CROSS_SLACK ) out.push( [ a, b ] );
		}
	}
	return out;
}

/*
================
windowRect

A window's area from its drag bar's left, top and width, down to height.
================
*/
function windowRect( controls, panel, height ) {
	const bar = defined( controls.find( c => c.id === "window-drag:" + panel ) );
	return [ bar.rect[0], bar.rect[1], bar.rect[2], height ];
}

// Each window's height from its drag bar down: iftaxmanagement.txt,
// ifspecialtydeal.txt and ifcosinventory.txt roots, rounded up.
const WINDOWS = [
	{ recipe: fortressTax, panels: [ [ "Fortress tax", 470 ] ] },
	{ recipe: specialtyDeal, panels: [ [ "Specialty deal", 300 ], [ "COS inventory", 380 ] ] }
];

for ( const { recipe, panels } of WINDOWS ) {
	for ( const [panel, height] of panels ) {
		test(`${panel}: no wrapped label crosses another label`, () => {
			const { scene, controls } = openWindow( true, recipe );
			const rect = windowRect( controls, panel, height );
			const crossed = crossings( scene, rect );
			assert.deepEqual( crossed.slice( 0, 4 ), [], `${crossed.length} line crossings in ${rect}` );
		});
	}
}

// ifspecialtydeal.txt ids 21-24: the four right-aligned labels, 63 pixels
// wide at x 13, from y 121 (Purchase amount) to y 230 (Trade scale's
// bottom), each two lines under the row.
const DEAL_LABELS = { left: 13, right: 76, top: 110, bottom: 240, count: 4 };

test("the trade window's wrapped labels read as pairs, each apart from the next", () => {
	for ( const fit of [ true, false ] ) {
		const { scene, controls } = openWindow( fit, specialtyDeal );
		const [x, y] = windowRect( controls, "Specialty deal", 0 );
		const column = textLines( scene, [
			x + DEAL_LABELS.left - 12,
			y + DEAL_LABELS.top,
			DEAL_LABELS.right - DEAL_LABELS.left + 12,
			DEAL_LABELS.bottom - DEAL_LABELS.top
		] ).sort( ( a, b ) => a[1] - b[1] );
		if ( !fit ) {
			// Off, the original: each label one line, past its box's left edge.
			assert.ok( column.length < 2 * DEAL_LABELS.count, "the row off wraps nothing" );
			continue;
		}
		assert.equal( column.length, 2 * DEAL_LABELS.count, "four labels, two lines each" );
		const pitches = column.slice( 1 ).map( ( line, index ) => line[1] - column[index][1] );
		for ( let pair = 0; pair < DEAL_LABELS.count; pair++ ) {
			const inside = pitches[2 * pair];
			for ( const between of [ pitches[2 * pair - 1], pitches[2 * pair + 1] ] ) {
				if ( between !== undefined ) assert.ok( inside < between, `label ${pair}: ${pitches}` );
			}
		}
	}
});
