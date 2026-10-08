/*
===========================================================================

system-message-layout.test.mjs - compact message geometry and native defaults

Exercises the production owner with authored chrome and measured bitmap text.
No generated assets are needed to verify wrapping, controls or scroll admission.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";

const { systemMessageLayout } = await import( "../../src/engine/foundation/ui/system-message-layout.ts" );

/*
================
authoredControl
================
*/
/** @returns {import('../../src/engine/foundation/ui/authored-layout.ts').AuthoredControl} */
function authoredControl( name ) {
	return {
		name,
		id: 0,
		type: "image",
		rect: [ 0, 0, 16, 16 ],
		client: [ 0, 0, 16, 16 ],
		uv: [ 0, 0, 1, 1 ],
		texture: `/fixture/${name}.png`,
		size: [ 16, 16 ],
		text: "",
		color: [ 1, 1, 1, 1 ],
		fontIndex: 0,
		hAlign: 0,
		vAlign: 0
	};
}

const layout = Object.fromEntries(
	[ "BG_UP", "BG_MID", "BG_DOWN", "CHATOPTION_BTN", "SIZE_BTN" ].map( suffix => {
		const name = `GDR_SYETEM_MESSAGE_${suffix}`;
		return [ name, authoredControl( name ) ];
	} )
);

/*
================
draw
================
*/
function draw( { width = 1024, rows = 2, lines = [ "Notice" ], offset = 0, options = {} } = {} ) {
	/** @type {{value:string,rect:readonly number[],clip:readonly number[],color:readonly number[]}[]} */
	const painted = [];
	const output = systemMessageLayout(
		layout,
		width,
		768,
		rows,
		lines,
		() => [ 16, 16 ],
		( value, rect, clip, color ) => {
			painted.push( { value, rect, clip, color } );
			return [];
		},
		null,
		null,
		offset,
		value => value.length * 7,
		true,
		options
	);
	return { ...output, painted };
}

/*
================
nativeDefaults
================
*/
test("omitted compact width preserves native geometry and post-wrap indentation", () => {
	const output = draw();
	assert.deepEqual( output.blocks, [ [ 667, 561, 353, 117 ] ] );
	assert.deepEqual( output.painted, [ {
		value: "Notice",
		rect: [ 677, 646, 316, 14 ],
		clip: [ 677, 576, 316, 84 ],
		color: [ 219 / 255, 201 / 255, 155 / 255, 1 ]
	} ] );
	assert.deepEqual( output.quads.slice( 0, 3 ).map( quad => quad.rect ), [
		[ 667, 561, 335, 4 ],
		[ 667, 565, 335, 109 ],
		[ 667, 674, 335, 4 ]
	] );
	assert.deepEqual( output.controls.find( control => control.id === "status-filter" )?.rect, [ 1004, 561, 16, 20 ] );
	assert.deepEqual( output.controls.find( control => control.id === "status-size" )?.rect, [ 1004, 658, 16, 20 ] );
	assert.deepEqual( output.scrolling, { range: 0, travel: 27, bounds: [ 667, 561, 353, 117 ] } );
	assert.deepEqual( draw( { lines: [ "X".repeat( 90 ) ] } ).painted.map( row => row.value ), [
		"X".repeat( 45 ),
		"  " + "X".repeat( 45 )
	] );
	assert.deepEqual( draw( { options: { width: undefined } } ), output );
});

for ( const width of [ 320, 375 ] ) {
	/*
	================
	compactWidth
	================
	*/
	test(`compact messages at ${width}px retain all text and fit chrome without glyph scaling`, () => {
		const panelWidth = Math.min( 353, width - 8 );
		const input = { width, rows: 1, lines: [ "X".repeat( 240 ) ], options: { width: panelWidth } };
		const bottom = draw( input );
		const innerWidth = panelWidth - 37;
		const perLine = Math.floor( (innerWidth - 14) / 7 );
		const expected = Array.from(
			{ length: Math.ceil( 240 / perLine ) },
			( _, i ) => (i ? "  " : "") + "X".repeat( Math.min( perLine, 240 - i * perLine ) )
		);
		assert.equal( bottom.scrolling.range, expected.length - 3 );
		const allRows = [];
		for ( let offset = bottom.scrolling.range; offset >= 0; offset-- ) {
			const result = draw( { ...input, offset } );
			assert.deepEqual(
				result.painted.map( row => row.value ),
				expected.slice( expected.length - offset - 3, expected.length - offset )
			);
			if ( offset === bottom.scrolling.range ) allRows.push( ...result.painted.map( row => row.value ) );
			else allRows.push( result.painted.at( -1 )?.value );
			for ( const row of result.painted ) {
				assert.ok( row.value.length * 7 <= row.clip[2], "indent and glyph advances fit the text clip" );
				assert.equal( row.rect[3], 14, "native line height is not scaled" );
				assert.equal( row.rect[2], innerWidth );
			}
			for (
				const rect of [
					...result.controls.map( c => c.rect ),
					...result.quads.map( q => q.rect ),
					...result.blocks
				]
			) {
				assert.ok(
					rect[0] >= 4 && rect[0] + rect[2] <= width - 4,
					"chrome and controls stay within viewport margins"
				);
			}
		}
		assert.equal(
			allRows.join( "" ).replaceAll( " ", "" ),
			input.lines[0],
			"scrolling exposes every source glyph"
		);
		assert.deepEqual( draw( { ...input, offset: -100 } ), bottom );
		assert.deepEqual( draw( { ...input, offset: 1000 } ), draw( { ...input, offset: bottom.scrolling.range } ) );
		assert.equal( bottom.quads[0].rect[2], panelWidth - 18 );
		assert.equal( bottom.controls.find( c => c.id === "status-filter" )?.rect[2], 16 );
	});
}

/*
================
compactWords
================
*/
test("compact word wrapping and explicit line breaks retain readable rows", () => {
	const output = draw( {
		width: 320,
		rows: 4,
		options: { width: 312 },
		lines: [
			"A notice with several words that must wrap to fit the narrow message panel.\\nNext notice."
		]
	} );
	assert.ok( output.painted.length > 2 );
	assert.equal( output.painted.at( -1 )?.value, "Next notice." );
	for ( const row of output.painted ) assert.ok( row.value.length * 7 <= row.clip[2] );
});
