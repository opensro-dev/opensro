/*
===========================================================================

hud-movement-regressions.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { createPosePresentation } = await load( "runtime/characters/pose-presentation.ts" );
const { createHudMessages } = await load( "runtime/ui/hud/messages.ts" );
const { helperBubble } = await load( "foundation/ui/helper-bubble.ts" );
const { textLines } = await load( "foundation/ui/text-lines.ts" );
const { terrainOwnerPath } = await load( "foundation/navigation/dungeon-ownership.ts" );
test("visual pose interpolates worker steps, then settles; teleports and retired entities snap", () => {
	const presentation = createPosePresentation(),
		a = { regionId: 257, x: 10, y: 0, z: 10, angle: 0 },
		b = { ...a, x: 11, y: 1 };
	assert.deepEqual( presentation.pose( 1, a, 0 ), a );
	assert.deepEqual( presentation.pose( 1, b, .016 ), a );
	const half = presentation.pose( 1, b, .024 );
	assert.ok( Math.abs( half.y - .5 ) < 1e-6 );
	assert.ok( Math.abs( half.x - 10.5 ) < 1e-6 );
	assert.deepEqual( presentation.pose( 1, b, .04 ), b );
	const teleport = { ...b, x: 500 };
	assert.deepEqual( presentation.pose( 1, teleport, .05 ), teleport );
	presentation.retain( new Set() );
	assert.deepEqual( presentation.pose( 1, a, .06 ), a );
	presentation.reset();
	assert.deepEqual( presentation.pose( 1, b, .07 ), b );
});
test("message tips use inclusive native eligibility, 60 second cadence and share ordered notice history", () => {
	let draws = 0;
	const messages = createHudMessages( count => {
		draws++;
		assert.equal( count, 1 );
		return 0;
	} );
	const tips = [ { id: 1, type: 0, minLevel: 1, maxLevel: 20, text: "Chinese" }, {
		id: 2,
		type: 1,
		minLevel: 1,
		maxLevel: 20,
		text: "European"
	}, { id: 3, type: 3, minLevel: 21, maxLevel: 100, text: "Universal" } ];
	const step = ( now, level = 20, notices = [] ) => messages.step( now, tips, level, 1, notices, key => key + " %d" );
	assert.deepEqual( step( 1000 ), [] );
	assert.deepEqual( step( 60999 ), [] );
	assert.equal( step( 61000 )[0].value, "European" );
	assert.equal( draws, 1 );
	const notice = { sequence: 1, key: "UIIT_MSG_STATE_GAIN_EXP_NEW", value: 4 };
	assert.deepEqual( step( 62000, 20, [ notice ] ).map( row => row.value ), [
		"European",
		"UIIT_MSG_STATE_GAIN_EXP_NEW 4"
	] );
	assert.equal( step( 63000, 20, [ notice ] ).length, 2, "repeated snapshots cannot duplicate notices" );
	assert.equal( step( 300000, 21 ).at( -1 ).value, "Universal" );
	assert.equal( draws, 2, "suspension does not burst old messages" );
	messages.reset();
	assert.deepEqual( step( 300001 ), [] );
});
test("helper bubbles wrap long tokens, use native chrome and stay above the bottom margin", () => {
	const measure = s => s.length * 7,
		bubble = helperBubble( "See/hide major ability", [ 200, 570, 16, 16 ], [ 0, 0, 800, 600 ], measure );
	assert.equal( bubble.quads.length, 9 );
	assert.ok( bubble.quads.slice( 1 ).every( q => q.texture.includes( "com_tooltip_" ) ) );
	assert.equal( bubble.lines[0].rect[0], 224 );
	assert.ok( bubble.lines.at( -1 ).rect[1] + bubble.lines.at( -1 ).rect[3] <= 585 );
	const right = helperBubble( "Ability", [ 780, 0, 16, 16 ], [ 0, 0, 800, 600 ], measure );
	assert.ok( right.lines[0].rect[0] < 780 );
	const rows = textLines( "abcdefghijk\nTwo words", 28, measure );
	assert.ok( rows.every( row => measure( row ) <= 28 ) );
	assert.equal( rows.join( "" ).replaceAll( " ", "" ), "abcdefghijkTwowords" );
});
test("terrain admits a ramp through its open outline, not from the XZ footprint of an elevated deck", () => {
	const mesh = {
		vertices: Float32Array.of( 0, 0, 0, 100, 20, 0, 100, 20, 100, 0, 0, 100 ),
		cells: Uint16Array.of( 0, 1, 2, 0, 2, 3 ),
		edges: Uint32Array.of( 3, 0, 1, 65535, 0, 0 ),
		bounds: [ 0, 0, 0, 100, 20, 100 ]
	};
	const objects = [ { x: 0, y: 0, z: 0, yaw: 0, mesh } ];
	const path = terrainOwnerPath( objects, [ -10, 0, 50 ], [ 90, 0, 50 ] );
	assert.equal( path.stop, 1 );
	assert.ok( path.owner );
	assert.ok( Math.abs( path.spans[0].from - .1 ) < 1e-6 );
	assert.equal(
		terrainOwnerPath( objects, [ 50, 0, 50 ], [ 90, 0, 50 ] ).owner,
		null,
		"a walker beneath an overlapping surface must not pop up without entry"
	);
	mesh.edges[4] = 0x10;
	assert.equal(
		terrainOwnerPath( objects, [ -10, 0, 50 ], [ 90, 0, 50 ] ).owner,
		null,
		"disabled outline does not acquire ownership"
	);
});

test("80ms worker batches keep the body moving between deliveries at 30, 60 and 150 FPS", () => {
	for ( const fps of [ 30, 60, 150 ] ) {
		const p = createPosePresentation(), start = { regionId: 257, x: 10, y: 0, z: 10, angle: 0 };
		let last = 10, stalls = 0, steps = 0;
		p.pose( 1, start, 0 );
		for ( let frame = 1; frame <= fps; frame++ ) {
			const now = frame / fps, target = { ...start, x: 10 + Math.floor( (now + 1e-8) / .08 ) * 4 };
			const sample = p.pose( 1, target, now );
			if ( now > .24 ) {
				steps++;
				if ( sample.x - last < 1e-6 ) stalls++;
			}
			assert.ok( sample.x >= last - 1e-8 );
			assert.ok( sample.x <= target.x + 1e-8 );
			last = sample.x;
		}
		assert.ok( stalls / steps < .1, `${fps} FPS stalled ${stalls}/${steps} frames` );
		p.pose( 1, { ...start, x: 70 }, 1.1 );
		p.pose( 1, { ...start, x: 70 }, 1.3 );
		assert.equal( p.moving( 1 ), false );
	}
});
test("visual heading takes the native shortest arc and limits angular velocity independent of FPS", () => {
	for ( const fps of [ 30, 60, 150 ] ) {
		const p = createPosePresentation(), a = { regionId: 257, x: 10, y: 0, z: 10, angle: 0 };
		p.pose( 1, a, 0 );
		const turned = p.pose( 1, { ...a, angle: 16384 }, 1 / fps );
		assert.ok( Math.abs( turned.angle - 98304 / fps ) <= .5 );
		let final;
		for ( let i = 2; i <= fps / 2; i++ ) final = p.pose( 1, { ...a, angle: 16384 }, i / fps );
		assert.equal( final.angle, 16384 );
		p.reset();
		p.pose( 1, { ...a, angle: 65500 }, 0 );
		assert.equal( p.pose( 1, { ...a, angle: 20 }, .01 ).angle, 20 );
	}
});
test("teleports across dungeon and outdoor coordinate spaces bypass distance interpolation", () => {
	const p = createPosePresentation(), a = { regionId: 0x8001, x: 10, y: 2, z: 10, angle: 0 };
	p.pose( 1, a, 0 );
	for ( const regionId of [ 0x8002, 257, 0x8001 ] ) {
		const next = { ...a, regionId };
		assert.deepEqual( p.pose( 1, next, .01 ), next );
	}
});
test("distant navigation placements are rejected before reading their triangle or edge columns", () => {
	const mesh = {
		bounds: [ 1000, 0, 1000, 1100, 0, 1100 ],
		get cells() {
			throw Error( "distant triangle scan" );
		},
		get edges() {
			throw Error( "distant outline scan" );
		},
		vertices: new Float32Array()
	};
	assert.equal( terrainOwnerPath( [ { x: 0, y: 0, z: 0, yaw: 0, mesh } ], [ 0, 0, 0 ], [ 1, 0, 1 ] ).owner, null );
});

test("native message type and explicit color survive retention and wrapping", async () => {
	const { systemMessageLayout } = await load( "foundation/ui/system-message-layout.ts" );
	const { readFileSync } = await import( "node:fs" );
	const { decodeAuthoredLayout } = await load( "foundation/ui/authored-layout.ts" );
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifsystemmessage.json", "utf8" ) )
	);
	const owner = createHudMessages( () => 0 ),
		notices = [ { sequence: 1, key: "guide", value: 0, nativeType: 6 }, {
			sequence: 2,
			key: "fight",
			value: 0,
			nativeType: 2,
			colorArgb: 0xff123456
		} ];
	const rows = owner.step( 0, [], 1, 0, notices, key => key );
	assert.equal( rows[0].category, "" );
	assert.equal( rows[1].category, "fight" );
	const painted = [];
	systemMessageLayout(
		layout,
		1024,
		768,
		2,
		rows,
		() => [ 16, 16 ],
		( value, r, clip, color ) => {
			painted.push( { value, color } );
			return [];
		},
		null,
		null
	);
	assert.deepEqual( painted.find( r => r.value === "guide" ).color, [ 186 / 255, 207 / 255, 242 / 255, 1 ] );
	assert.deepEqual( painted.find( r => r.value === "fight" ).color, [ 18 / 255, 52 / 255, 86 / 255, 1 ] );
	assert.deepEqual(
		owner.step( 1, [], 1, 0, notices, key => key ),
		rows,
		"unchanged publication cannot duplicate a message or lose its color"
	);
});

test("chat hover draws native text-width highlight and clears on exit", async () => {
	const { chatLayout } = await load( "foundation/ui/chat-layout.ts" );
	const { readFileSync } = await import( "node:fs" );
	const { decodeAuthoredLayout } = await load( "foundation/ui/authored-layout.ts" );
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
	);
	const draw = hover =>
		chatLayout( {
			layout,
			width: 1024,
			height: 768,
			rows: 2,
			tab: 0,
			input: "",
			lines: [],
			welcome: "Welcome",
			copy: x => x,
			size: () => [ 16, 16 ],
			text: () => [],
			hover,
			pressed: null,
			measure: s => s.length * 7
		} );
	const cold = draw( null ), hot = draw( "chat-line:0" );
	assert.ok( cold.controls.some( c => c.id === "chat-line:0" ) );
	const highlight = hot.quads.find( q => q.texture === "" && q.color[3] === 102 / 255 );
	assert.ok( highlight );
	assert.equal( highlight.rect[2], 49 );
	assert.equal( highlight.rect[3], 15 );
	assert.equal( draw( null ).quads.filter( q => q.color[3] === 102 / 255 ).length, 0 );
});
