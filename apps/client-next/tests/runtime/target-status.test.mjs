/*
===========================================================================

target-status.test.mjs - tests for target-status.ts, authored-layout.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
const { targetStatus, targetDifficulty, monsterMaximumHp, fortressTargetKind } = await import(
		"../../src/engine/foundation/ui/target-status.ts"
	),
	{ decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const layouts = Object.fromEntries(
	await Promise.all(
		[
			"iftargetwindow",
			"iftw_specialmob",
			"iftw_commonenemy",
			"iftw_player",
			"iftw_jobplayer_trijob2",
			"iftw_fortressstructure"
		].map(
			async name => [
				name,
				decodeAuthoredLayout(
					JSON.parse(
						await readFile( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/" + name + ".json", "utf8" )
					)
				)
			]
		)
	)
);
const entity = { gid: 1, kind: "monster", name: "Mangyang", level: 10, maxHp: 100, rarity: 0 };
const render = e => targetStatus( layouts, e, 10, 50, k => k, s => s.length * 6, "remembered.png" );
test("retail difficulty thresholds and monster HP grade/party multipliers", () => {
	assert.deepEqual( [ -8, -7, -6, -4, -3, 0, 1, 5, 6 ].map( d => targetDifficulty( 10 + d, 10 ) ), [
		0,
		0,
		1,
		1,
		2,
		2,
		3,
		3,
		4
	] );
	for ( const [rarity, multiple] of [ [ 0, 1 ], [ 1, 2 ], [ 3, 1 ], [ 4, 20 ], [ 5, 100 ], [ 6, 4 ], [ 7, 30 ] ] ) {
		for ( const party of [ 0, 1 ] ) {
			assert.equal(
				monsterMaximumHp( { ...entity, rarity, rarityAuxIcon: party } ),
				100 * multiple * (party ? 10 : 1)
			);
		}
	}
	assert.equal( monsterMaximumHp( { ...entity, rarity: 2 } ), undefined );
});
test("grade 8 (imported uniques) gets the unique title and a gauge; unlisted grades still get none", () => {
	const apis = { ...entity, name: "Apis", level: 103, maxHp: 21068995, rarity: 8 };
	assert.equal( monsterMaximumHp( apis ), 21068995 );
	for ( const [hp, expected] of [ [ apis.maxHp, 1 ], [ apis.maxHp / 2, .5 ], [ 0, 0 ] ] ) {
		const output = defined( targetStatus( layouts, apis, 110, hp, k => k, s => s.length * 6, "" ) );
		const gauge = defined( output.images.find( image => image.node.name.includes( "HPGAUGE" ) ) );
		assert.equal( gauge.fraction, expected );
		assert.ok( output.gradeIcon.endsWith( "tw_icon_unique.png" ) );
	}
	assert.equal( monsterMaximumHp( { ...entity, rarity: 2 } ), undefined );
});
test("grade 7 (the summon scroll's and Pandora's waves) gets its title, the elite icon and a full gauge", () => {
	const strong = { ...entity, name: "Yeti", level: 59, maxHp: 7059, rarity: 7 };
	assert.equal( monsterMaximumHp( strong ), 7059 * 30 );
	const output = defined( targetStatus( layouts, strong, 60, 7059 * 30, k => k, s => s.length * 6, "" ) );
	const gauge = defined( output.images.find( image => image.node.name.includes( "HPGAUGE" ) ) );
	assert.equal( gauge.fraction, 1 );
	assert.ok( output.gradeIcon.endsWith( "tw_icon_elite.png" ) );
	assert.ok( output.texts.some( text => JSON.stringify( text ).includes( "UIIT_STT_MOB_STRONG" ) ) );
});
test("all supported target families use exclusive authored layouts and native width changes", () => {
	const monster = render( entity );
	assert.equal( defined( monster ).height, 78 );
	assert.equal( defined( monster ).width, 196 );
	assert.equal( defined( defined( monster ).images.find( i => i.node.name.includes( "HPGAUGE" ) ) ).fraction, .5 );
	assert.equal( defined( render( { ...entity, kind: "npc" } ) ).width, 236 );
	assert.equal(
		defined( defined( render( { ...entity, kind: "npc" } ) ).images.find( i => i.node.name.includes( "HPGAUGE" ) ) )
			.node.rect[2],
		208
	);
	for ( const band of [ 3, 4, 5 ] ) {
		const cos = render( { ...entity, kind: "cos", tidWord: 0x1c6 | (band << 11) } );
		assert.equal( defined( cos ).height, 36 );
		assert.ok( !defined( cos ).images.some( i => i.node.name.includes( "HPGAUGE" ) ) );
	}
	assert.equal( defined( render( { ...entity, kind: "cos", tidWord: 0x9c6 } ) ).height, 51 );
	assert.equal( defined( render( { ...entity, kind: "player", jobType: 4 } ) ).height, 36 );
	for ( const jobType of [ 1, 2, 3 ] ) {
		assert.equal(
			defined( render( { ...entity, kind: "player", jobType, countryByte9c: 1, jobGrade: 2 } ) ).height,
			58
		);
	}
	assert.equal( render( { ...entity, kind: "ground-item" } ), null );
	assert.equal( layouts.iftw_commonenemy.GDR_TWCE_GAUGE_HPGAUGE.rect[2], 168, "authored data is never mutated" );
});
test("unhandled grade retains the native dynamic icon while clearing its title", () => {
	const result = render( { ...entity, rarity: 2 } );
	assert.equal( defined( result ).gradeIcon, "remembered.png" );
	assert.equal( defined( defined( result ).texts.find( row => row.node.name === "GDR_TWSM_TEXT_LEV" ) ).value, "" );
});
test("fortress structures and guards take the native fortress or wide NPC window", () => {
	// TypeID words: barricade 1/2/5/6, fort stone 1/2/5/1, war object 1/2/4/3, guard 1/2/4/4.
	const barricade = { ...entity, kind: "structure", tidWord: 0x2c4 | (6 << 11) };
	const stone = { ...entity, kind: "structure", tidWord: 0x2c4 | (1 << 11) };
	const gem = output => defined( output.images.find( i => i.node.name.endsWith( "_GEM" ) ) ).node.texture;
	const gauge = output => defined( output.images.find( i => i.node.name.includes( "HPGAUGE" ) ) );
	assert.deepEqual(
		[ barricade, { ...entity, kind: "structure", tidWord: 0x244 | (3 << 11) }, stone ].map( fortressTargetKind ),
		[ 2, 3, 0 ]
	);
	assert.deepEqual(
		[ 4, 1, 3, 2 ].map( band => fortressTargetKind( { kind: "npc", tidWord: 0x244 | (band << 11) } ) ),
		[ 1, 1, 3, 0 ]
	);
	// 516BC0: the fortress-structure child at 236x51, HP 195, name 177.
	const fortress = defined( render( barricade ) );
	assert.equal( fortress.images[0].node.name, "GDR_TW_FORTRESSSTRUCTER" );
	assert.deepEqual( [ fortress.width, fortress.height ], [ 236, 51 ] );
	assert.equal( gauge( fortress ).node.rect[2], 195 );
	assert.equal( gauge( fortress ).fraction, .5 );
	assert.equal( defined( fortress.texts.find( t => t.node.name === "GDR_TWFS_TEXT_ID" ) ).node.rect[2], 177 );
	assert.ok( gem( fortress ).endsWith( "tw_gem_normal.png" ) );
	assert.ok(
		gem( defined( render( { ...entity, kind: "npc", tidWord: 0x244 | (4 << 11) } ) ) ).endsWith(
			"tw_gem_player.png"
		)
	);
	// Any other structure is a CICNonuser, not a CICNPC: 5823B0's standard
	// frame (196x51, HP 168, name 137) with the normal gem.
	const standard = defined( render( stone ) );
	assert.deepEqual( [ standard.width, standard.height ], [ 196, 51 ] );
	assert.equal( gauge( standard ).node.rect[2], 168 );
	assert.equal( defined( standard.texts.find( t => t.node.name === "GDR_TWCE_TEXT_ID" ) ).node.rect[2], 137 );
	assert.ok( gem( standard ).endsWith( "tw_gem_normal.png" ) );
	assert.equal( layouts.iftw_fortressstructure.GDR_TWFS_GAUGE_HPGAUGE.rect[2], 50, "authored data is never mutated" );
});
