/*
===========================================================================

native-menu-presentation.test.mjs - tests for stretch-ring.ts,
main-popup.ts, money-presentation.ts, authored-layout.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { stretchRing } = await import( "../../src/engine/foundation/ui/stretch-ring.ts" );
const { mainPopupPresentation } = await import( "../../src/engine/foundation/ui/main-popup.ts" );
const { moneyPresentation } = await import( "../../src/engine/foundation/ui/money-presentation.ts" );
const { decodeAuthoredLayout, authoredPaintOrder } = await import(
	"../../src/engine/foundation/ui/authored-layout.ts"
);
const oracle = JSON.parse( readFileSync( "tests/fixtures/native/native-menu-geometry.json", "utf8" ) );

test("later native section creation paints above earlier frames, preserving each section order", () => {
	const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifskill.json", "utf8" ) );
	const layout = decodeAuthoredLayout( raw, [ "Create", "MainSkillWnd" ] ),
		order = authoredPaintOrder( layout ).map( n => n.name );
	assert.deepEqual(
		order,
		[ ...raw.sections.find( s => s.name === "Create" ).nodes ].reverse().map( n => n.name ).concat(
			[ ...raw.sections.find( s => s.name === "MainSkillWnd" ).nodes ].reverse().map( n => n.name )
		)
	);
	assert.ok( order.indexOf( "GDR_SKILL_FRAME" ) < order.indexOf( "GDR_SKILL_BOTTOM_BOX" ) );
	const guild = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifguild.json", "utf8" ) ),
		[ "Create", "GuildInfo", "NotifySubBox", "MemberView", "Command", "SortBtn" ]
	);
	const groups = authoredPaintOrder( guild ).map( n => n.creationSection );
	assert.deepEqual(
		groups,
		[ ...groups ].sort( ( a, b ) => defined( a ) - defined( b ) ),
		"Guild uses the same creation contract"
	);
	const unselected = decodeAuthoredLayout( raw );
	assert.deepEqual(
		authoredPaintOrder( unselected ),
		Object.values( unselected ).reverse(),
		"unreviewed section admission is unchanged"
	);
});
test("every published stretch window matches native texture slots, UVs and bounds, including asymmetric corners", () => {
	assert.equal( oracle.function, "0x006F9950" );
	assert.equal( oracle.slot, "0xA0" );
	assert.ok( oracle.cases.length > 100 );
	const parts = oracle.textureInstaller.parts;
	for ( const row of oracle.cases ) {
		const sizes = new Map( parts.map( ( p, i ) => [ row.prefix + p + ".png", row.sizes[i] ] ) );
		const quads = stretchRing( row.rect, row.prefix, p => sizes.get( p ), [ -1000, -1000, 4000, 4000 ] ).quads;
		assert.deepEqual( quads.map( q => q.rect ), row.quads.filter( r => r[2] > 0 && r[3] > 0 ), row.id );
		const visible = row.quads.map( ( r, i ) => r[2] > 0 && r[3] > 0 ? i : -1 ).filter( i => i >= 0 );
		assert.deepEqual(
			quads.map( q => q.texture ),
			visible.map( i => row.prefix + parts[i] + ".png" ),
			row.id + " texture slots"
		);
		assert.deepEqual(
			quads.map( q =>
				q.uvTurn === 1 ? [ [ 0, 1 ], [ 0, 0 ], [ 1, 0 ], [ 1, 1 ] ] : [ [ 0, 0 ], [ 1, 0 ], [ 1, 1 ], [ 0, 1 ] ]
			),
			visible.map( i => oracle.uvs[i] ),
			row.id + " native UV stores"
		);
	}
});
test("switching pages restores native authored origins and shared backing visibility", () => {
	const raw = JSON.parse(
			readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifmainpopup.json", "utf8" )
		),
		layout = decodeAuthoredLayout( raw );
	const rows = [ "Inventory", "Actions", "Skills", "Character", "Party", "Quests", "Inventory" ].map( p =>
		mainPopupPresentation( p, layout, 100, 200 )
	);
	assert.deepEqual( defined( rows[0].backing ).rect, [ 189, 68, 9, 292 ] );
	assert.deepEqual( defined( rows[1].backing ).rect, [ 40, 154, 308, 124 ] );
	assert.deepEqual( rows[1].pane, [ 113, 238, 364, 357 ] );
	assert.equal( defined( rows[0].backing ).type, "CIFStatic" );
	for ( const row of rows.slice( 2, 6 ) ) assert.equal( row.backing, null );
	assert.deepEqual( rows[0], rows.at( -1 ) );
	assert.deepEqual(
		layout.GDR_MAINPOPUP_BG_TILE.rect,
		[ 40, 154, 308, 124 ],
		"switching must not mutate the resource product"
	);
});
test("native gold keeps all 64-bit digits, grouping and each threshold color", () => {
	const limits = [ 10000n, 100000n, 1000000n, 10000000n, 100000000n, 1000000000n, 10000000000n, 100000000000n ];
	const colors = [ 0xffffff, 0xfffa85, 0xffd348, 0xffad5c, 0xff9aa1, 0xeba1ff, 0xb8bbff, 0x95deff, 0x8bffe5 ];
	for ( const [i, limit] of limits.entries() ) {
		for ( const [n, c] of [ [ limit - 1n, colors[i] ], [ limit, colors[i + 1] ] ] ) {
			const p = moneyPresentation( String( n ) );
			assert.deepEqual( p.color, [ (c >>> 16 & 255) / 255, (c >>> 8 & 255) / 255, (c & 255) / 255, 1 ] );
			assert.equal( p.text.replaceAll( ",", "" ), String( n ) );
		}
	}
	assert.equal( moneyPresentation( "9223372036854775807" ).text, "9,223,372,036,854,775,807" );
	assert.equal(
		moneyPresentation( "18446744073709551615" ).text,
		"-1",
		"retail formatting interprets the two wire words as signed"
	);
	assert.equal( moneyPresentation( "0" ).text, "0" );
});
