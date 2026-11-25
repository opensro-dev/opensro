/*
===========================================================================

gacha-prize-glow.test.mjs - the chosen Magic Pang prize wears the slot glow

642550 (the prize slot click) arms the clicked slot's glow through
CIFSlotWithHelp_SetRentGlow (54FAA0, selected byte +0x4FE), 641DD0
clears the others, and CIFControl_RenderIconOverlays (565850) draws it
as the looping pt_edge_effect sheet a summoned companion's item wears.
The port drew a gold outline instead (#4403, #976).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

const GLOW = "interface/pet/pt_edge_effect.png";

/*
================
pangWindow

A UI with the Magic Pang window open over twelve prizes. Returns the
fixture and a step returning the controls by id.
================
*/
function pangWindow() {
	const f = uiFixture();
	let now = 1000, seen = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) seen = f.ui.step( f.state, now += 50 ) ?? seen;
		return new Map( (seen?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	const prizes = Array.from( { length: 12 }, ( _, i ) => ({
		entry: i + 1,
		refObjId: 100 + i,
		quantity: 1,
		nameSymbol: "SN_PRIZE_" + i
	}) );
	f.state.gameplay = {
		...f.state.gameplay,
		gacha: {
			visible: true,
			phase: "idle",
			npc: 7,
			slot: null,
			entry: 0,
			started: 0,
			result: null,
			error: null,
			prizes
		}
	};
	step();
	f.ui.event( { kind: "activate", id: "open-window:Magic Pang" } );
	return { f, step };
}

/*
================
glowRects

The rects of the scene's quads that draw the glow sheet.
================
*/
function glowRects( f ) {
	return (f.scenes.at( -1 )?.quads ?? [])
		.filter( q => typeof q.texture === "string" && q.texture.endsWith( GLOW ) )
		.map( q => q.rect.join( "," ) );
}

/*
================
goldQuads

Quads in the old outline's gold, which no Magic Pang art uses.
================
*/
function goldQuads( f ) {
	return (f.scenes.at( -1 )?.quads ?? []).filter( q => q.color?.join( "," ) === "1,0.82,0.25,1" );
}

test("only the chosen prize wears the glow, and it moves with the choice (642550, 641DD0)", () => {
	const { f, step } = pangWindow();
	try {
		let controls = step();
		assert.deepEqual( glowRects( f ), [], "nothing chosen: no glow" );
		f.ui.event( { kind: "activate", id: "gacha-prize:3" } );
		controls = step();
		const third = controls.get( "gacha-prize:3" );
		assert.ok( third?.selected, "prize 3 is chosen" );
		assert.deepEqual( glowRects( f ), [ third.rect.join( "," ) ] );
		assert.deepEqual( goldQuads( f ), [], "no gold outline" );
		f.ui.event( { kind: "activate", id: "gacha-prize:7" } );
		controls = step();
		assert.deepEqual( glowRects( f ), [ controls.get( "gacha-prize:7" )?.rect.join( "," ) ] );
	} finally {
		f.dispose();
	}
});

test("the chosen prize's glow loops through the sheet's nine cells", () => {
	const { f, step } = pangWindow();
	try {
		step();
		f.ui.event( { kind: "activate", id: "gacha-prize:1" } );
		const cells = new Set();
		for ( let i = 0; i < 12; i++ ) {
			step();
			const quad = (f.scenes.at( -1 )?.quads ?? []).find( q =>
				typeof q.texture === "string" && q.texture.endsWith( GLOW )
			);
			assert.ok( quad, "glow drawn" );
			assert.ok( Math.abs( quad.uv[2] - 1 / 9 ) < 1e-6, "one ninth of the sheet" );
			cells.add( Math.round( quad.uv[0] * 9 ) );
		}
		assert.ok( cells.size > 1, "the cell advances" );
	} finally {
		f.dispose();
	}
});
