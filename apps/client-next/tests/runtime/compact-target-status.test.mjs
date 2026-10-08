/*
===========================================================================

compact-target-status.test.mjs - native target branches in a compact slot

Asset-independent authored nodes exercise selection and projection together.
The tests preserve native meanings, font metadata and immutable input while
checking actual text client rectangles, including narrow metadata fallback.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { targetStatus, compactTargetStatus } = await import(
	"../../src/engine/foundation/ui/target-status.ts"
);
const { authoredClientRect } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );

/*
================
layout
Nonzero client insets ensure the projection replaces old frame padding.
================
*/
function layout( names ) {
	return Object.fromEntries(
		names.split( " " ).map( ( name, id ) => [ name, {
			name,
			id,
			type: "CIFStatic",
			rect: [ 5, 9, 168, 16 ],
			client: [ 3, 2, 4, 1 ],
			uv: [ .1, .2, .3, .4 ],
			texture: name + ".png",
			size: [ 168, 16 ],
			text: "",
			color: [ .2, .4, .6, 1 ],
			fontIndex: 2,
			hAlign: 0,
			vAlign: 0
		} ] )
	);
}

const layouts = {
	iftargetwindow: layout(
		"GDR_TW_COMMONENEMY GDR_TW_SPECIALMOBWND GDR_TW_JOB_PLAYERWND GDR_TW_PLAYERWND GDR_TW_CLOSE"
	),
	iftw_specialmob: layout(
		"GDR_TWSM_GEM GDR_TWSM_TEXT_ID GDR_TWSM_LEVEL GDR_TWSM_ICON GDR_TWSM_TEXT_LEV GDR_TWSM_GAUGE_HPGAUGE"
	),
	iftw_commonenemy: layout( "GDR_TWCE_GEM GDR_TWCE_TEXT_ID GDR_TWCE_GAUGE_HPGAUGE" ),
	iftw_player: layout( "GDR_TW_KINDRED_MARK GDR_TWP_TEXT_NAME" ),
	iftw_jobplayer_trijob2: layout(
		"GDR_TWJP_KINDRED_MARK GDR_TWJP_JOB_ALIAS GDR_TWJP_JOB_ICON GDR_TWJP_JOB_GRADENAME GDR_TWJP_JOB_GRADE"
	)
};

/*
================
render
================
*/
function render( fields = {}, hp = 50 ) {
	const entity = {
		gid: 1,
		refObjId: 1,
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		kind: "monster",
		name: "Long target name",
		level: 10,
		maxHp: 100,
		rarity: 0,
		...fields
	};
	const output = targetStatus( layouts, entity, 10, hp, key => key, value => value.length * 7, "retained-grade.png" );
	assert.ok( output );
	return { entity, output };
}

test("compact monster keeps native health/color/art while reflowing the 151px slot", () => {
	const { output } = render( { rarity: 4, rarityAuxIcon: 1 } );
	const before = structuredClone( output );
	const result = compactTargetStatus( output, "monster", 151 );
	assert.ok( result );
	assert.equal( result.width, 151 );
	assert.equal( result.height, 54 );
	assert.deepEqual( result.images[0].node.rect, [ 0, 0, 151, 54 ] );
	assert.deepEqual( result.texts.map( row => row.node.rect ), [ [ 24, 7, 103, 14 ], [ 8, 35, 40, 12 ], [
		65,
		35,
		81,
		12
	] ] );
	assert.deepEqual( result.close.rect, [ 131, 7, 16, 16 ] );
	assert.deepEqual( result.images.find( image => image.fraction !== undefined )?.node.rect, [ 8, 27, 135, 4 ] );
	assert.equal( result.images.find( image => image.fraction !== undefined )?.fraction, 50 / 20000 );
	assert.deepEqual( result.texts[0].node.color, output.texts[0].node.color );
	assert.deepEqual(
		result.images.map( image => [ image.node.texture, image.node.uv ] ),
		output.images.map( image => [ image.node.texture, image.node.uv ] )
	);
	assert.deepEqual( output, before );
});

test("compact projection retains every native target family and keeps visible fields separated", () => {
	const cases = [
		...[ 0, 1, 2, 3, 4, 5, 6 ].map( rarity => ({ rarity, rarityAuxIcon: 1 }) ),
		{ kind: "npc" },
		{ kind: "cos", tidWord: 0x9c6 },
		...[ 3, 4, 5 ].map( band => ({ kind: "cos", tidWord: 0x1c6 | band << 11 }) ),
		...[ 0, 1, 2 ].flatMap( countryByte9c =>
			[ 0, 1, 2, 3, 4 ].map( jobType => ({ kind: "player", jobType, countryByte9c, jobGrade: 2 }) )
		)
	];
	for ( const fields of cases ) {
		const { output, entity } = render( fields );
		const before = structuredClone( output );
		for ( const width of [ 80, 96, 119, 120, 151, 196, 250 ] ) {
			const result = compactTargetStatus( output, entity.kind, width );
			assert.ok( result );
			assert.equal( result.width, Math.min( 196, width ) );
			assert.equal( result.gradeIcon, output.gradeIcon );
			assert.deepEqual( result.texts.map( row => row.value ), output.texts.map( row => row.value ) );
			assert.deepEqual(
				result.images.map( row => row.node.texture ),
				output.images.map( row => row.node.texture )
			);
			assert.ok( result.height <= 78 );
			assert.deepEqual(
				result.images.filter( row => row.fraction !== undefined ).map( row => row.fraction ),
				output.images.filter( row => row.fraction !== undefined ).map( row => row.fraction )
			);
			for ( const row of output.texts ) assert.ok( result.helpText.includes( row.value ) );
			for ( const row of result.texts ) {
				assert.equal(
					row.node.fontIndex,
					output.texts.find( original => original.node.name === row.node.name )?.node.fontIndex
				);
				assert.deepEqual( authoredClientRect( row.node, 0, 0 ), row.node.rect );
			}
			const rectangles = [
				...result.images.map( row => row.node.rect ),
				...result.texts.map( row => row.node.rect ),
				result.close.rect
			];
			for ( const [x, y, w, h] of rectangles ) {
				assert.ok( x >= 0 && y >= 0 && w > 0 && h > 0 && x + w <= result.width && y + h <= result.height );
			}
			const fields = [
				...result.texts.map( row => row.node.rect ),
				result.close.rect,
				...result.images.slice( 1 ).map( row => row.node.rect )
			];
			for ( let i = 0; i < fields.length; i++ ) {
				for ( const b of fields.slice( i + 1 ) ) {
					const a = fields[i];
					assert.ok(
						a[0] + a[2] <= b[0] || b[0] + b[2] <= a[0] || a[1] + a[3] <= b[1] || b[1] + b[3] <= a[1],
						JSON.stringify( { entity, width, a, b } )
					);
				}
			}
		}
		assert.deepEqual( output, before );
	}
});

test("missing health never invents a gauge and unusable width does not overflow", () => {
	const { output } = render( { maxHp: undefined } );
	const result = compactTargetStatus( output, "monster", 151.9 );
	assert.ok( result );
	assert.equal( result.width, 151 );
	assert.ok( !result.images.some( image => image.fraction !== undefined ) );
	for ( const width of [ -1, 0, 79, NaN, Infinity ] ) {
		assert.equal( compactTargetStatus( output, "monster", width ), null );
	}
});
