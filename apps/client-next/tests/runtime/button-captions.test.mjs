/*
===========================================================================

button-captions.test.mjs - button captions stay inside their plates

The party and academy windows' "Dismiss" ran past both sides of its
44-pixel plate, the blocking window's "Block Registration" wrapped out of
its 24-pixel plate, and the Item Mall's "Buy Item Worn" and "Bundle goods"
wrapped out of theirs (#3675). Under the Experimental "Fit window labels"
(on by default) each caption is drawn whole inside its plate, in the
ui-small face or on two lines; with the row off every caption is drawn as
the original draws it.

The windows are the production ones, opened by the scene scripts the
golden suite plays, so this suite also needs the generated asset tree.

===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import test from "node:test";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { SCENE_SCRIPTS } from "../helpers/scene-scripts.mjs";

const ASSETS_HERE = existsSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json" );
// A glyph whose centre is this far outside a plate still belongs to its
// caption: "Dismiss" overflows its plate by two pixels a side, and the
// blocking window's two plates are five pixels apart.
const CAPTION_REACH = 4;

const CASES = [
	{ scene: "panel-party", id: "party-disband", caption: "Dismiss" },
	{ scene: "panel-academy", id: "academy-dismiss", caption: "Dismiss" },
	{ scene: "panel-blocking", id: "blocking-add", caption: "Block Registration" },
	{ scene: "panel-blocking", id: "blocking-remove", caption: "Block Removal" },
	{ scene: "item-mall", id: "item-mall-root:4", caption: "Buy Item Worn" },
	{ scene: "item-mall", id: "item-mall-category:4", caption: "Bundle goods" }
];

/*
================
openScene

Play one scene script against a fresh HUD with Fit window labels set to
fit, and return the last scene, its controls and the font atlas.
================
*/
async function openScene( name, fit ) {
	const { uiFixture, fontAtlas } = await import( "../helpers/ui-fixture.mjs" );
	const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
	const script = SCENE_SCRIPTS.find( entry => entry.name === name );
	assert.ok( script, name );
	const fx = uiFixture();
	const ctx = { fx, state: fx.state, now: 0, scene: undefined, semantics: undefined, event: e => fx.ui.event( e ) };
	const settle = ( ticks = 16 ) => {
		for ( let i = 0; i < ticks; i++ ) {
			const semantics = fx.ui.step( ctx.state, ctx.now += 100 );
			if ( semantics ) ctx.semantics = semantics;
		}
		ctx.scene = fx.scenes.at( -1 );
	};
	try {
		fx.ui.event( { kind: "experimental-preferences", value: { fitWindowLabels: fit } } );
		settle();
		for ( const beat of script.beats ) {
			beat.run( ctx );
			settle( beat.ticks );
		}
		const glyphs = expandTextRuns( ctx.scene.quads ).filter( q => q.texture === fontAtlas.image );
		return { glyphs, controls: ctx.semantics?.controls ?? [] };
	} finally {
		fx.dispose?.();
	}
}

/*
================
captionInk

The glyphs a control's caption drew: those centred on its rows and
within CAPTION_REACH of its sides, and the bounds of their ink. A line
wrapped out of the plate is centred off its rows, so the caller's letter
count misses it.
================
*/
function captionInk( glyphs, rect ) {
	const [x, y, w, h] = rect;
	const ink = glyphs.filter( g => {
		const cx = g.rect[0] + g.rect[2] / 2, cy = g.rect[1] + g.rect[3] / 2;
		return cx > x - CAPTION_REACH && cx < x + w + CAPTION_REACH && cy >= y && cy < y + h;
	} );
	return {
		count: ink.length,
		left: Math.min( ...ink.map( g => g.rect[0] ) ),
		right: Math.max( ...ink.map( g => g.rect[0] + g.rect[2] ) ),
		top: Math.min( ...ink.map( g => g.rect[1] ) ),
		bottom: Math.max( ...ink.map( g => g.rect[1] + g.rect[3] ) )
	};
}

test( "with Fit window labels every reported caption is whole and inside its plate", {
	skip: ASSETS_HERE ? false : "needs the generated asset tree"
}, async () => {
	for ( const scene of new Set( CASES.map( c => c.scene ) ) ) {
		const { glyphs, controls } = await openScene( scene, true );
		for ( const { id, caption } of CASES.filter( c => c.scene === scene ) ) {
			const control = controls.find( c => c.id === id );
			assert.ok( control, `${scene}: ${id} is drawn` );
			assert.equal( control.label, caption, `${id} keeps every word` );
			const [x, y, w, h] = control.rect, ink = captionInk( glyphs, control.rect );
			// One glyph per character (a space draws a blank one): nothing
			// was dropped. A wrapped caption's break space is not drawn.
			assert.ok(
				ink.count === caption.length || ink.count === caption.length - 1,
				`${id}: every letter drawn (${ink.count})`
			);
			assert.ok( ink.left >= x && ink.right <= x + w, `${id}: ink ${ink.left}..${ink.right} in ${x}..${x + w}` );
			assert.ok( ink.top >= y && ink.bottom <= y + h, `${id}: ink ${ink.top}..${ink.bottom} in ${y}..${y + h}` );
		}
	}
} );

test( "without Fit window labels the party and blocking captions are the original's", {
	skip: ASSETS_HERE ? false : "needs the generated asset tree"
}, async () => {
	// The original draws "Dismiss" whole in its own face past both sides
	// of its plate, and wraps "Block Registration" out of its plate's
	// bottom only through the port's label wrap, which is off too: one
	// line, wider than the plate.
	for ( const { scene, id } of CASES.filter( c => c.scene !== "item-mall" && c.id !== "blocking-remove" ) ) {
		const { glyphs, controls } = await openScene( scene, false );
		const control = controls.find( c => c.id === id );
		assert.ok( control, `${scene}: ${id} is drawn` );
		const [x, , w] = control.rect, ink = captionInk( glyphs, control.rect );
		assert.ok( ink.left < x || ink.right > x + w, `${id}: the original overflows (${ink.left}..${ink.right})` );
	}
} );
