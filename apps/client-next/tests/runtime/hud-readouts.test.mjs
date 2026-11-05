/*
===========================================================================

hud-readouts.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, "src/engine", file + ".ts" ) ).href );
}
const { bootstrapProgression, progressionPacket } = await load( "foundation/gameplay/progression" );
const { experienceReadout, minimapCoordinates, minimapRotation, skillPointReadouts } = await load(
	"foundation/ui/hud-readouts"
);
const { movementHeading } = await load( "foundation/gameplay/native-movement" );
const { feedbackLevels } = await load( "foundation/gameplay/feedback-levels" );
const { worldMapQuads, worldMapPageAt, worldMapPages, decodeWorldMapLayout, nativeLayout, worldMapImagePaths } =
	await load(
		"foundation/ui/world-map"
	);
const { fortressNotice } = await load( "foundation/gameplay/system-notices" );
const { createFeedback } = await load( "runtime/simulation/worker/session/world/gameplay/feedback/feedback" );

test("bootstrap retains exact XP and zero counters; base-stat admission is atomic", () => {
	const state = bootstrapProgression( { character: { level: 1, experience: "59" } } );
	assert.equal( state.experience, "59" );
	assert.equal( state.skillPoints, 0 );
	assert.equal( state.skillExperience, 0 );
	assert.throws( () => bootstrapProgression( { character: { experience: 9007199254740992 } } ) );
	const packet = Buffer.alloc( 36 );
	packet.writeUInt32LE( 123456, 24 );
	packet.writeUInt32LE( 234567, 28 );
	packet.writeUInt16LE( 55, 32 );
	packet.writeUInt16LE( 66, 34 );
	const next = progressionPacket( state, 0x343c, packet );
	assert.equal( next.stats.maxHp, 123456 );
	assert.equal( next.stats.maxMp, 234567 );
	assert.equal( next.stats.strength, 55 );
	assert.equal( next.stats.intellect, 66 );
	for ( const length of [ 0, 35, 37 ] ) {
		assert.throws( () => progressionPacket( next, 0x343c, Buffer.alloc( length ) ) );
	}
	assert.equal( state.stats, undefined );
	assert.equal( next.stats.maxHp, 123456 );
});
test("native XP formatting caps incomplete bars and coordinates truncate negative offsets", () => {
	const levels = feedbackLevels();
	assert.equal( experienceReadout( "59", 1, levels ), "50.00 %" );
	assert.equal( experienceReadout( "118", 1, levels ), "99.99 %" );
	assert.equal( experienceReadout( "0", 255, levels ), "" );
	assert.deepEqual( minimapCoordinates( { regionId: 92 * 256 + 135, x: -19, z: 19 } ), [ "X: -1", "Y:  1" ] );
	// A cave reads from its own origin, signed, truncating toward zero.
	assert.deepEqual( minimapCoordinates( { regionId: 0x8007, x: -19, z: 8025 } ), [ "X: -1", "Y:802" ] );
	assert.ok( minimapRotation( 0 ) === 0 );
	assert.ok( Math.abs( minimapRotation( 65535 ) - minimapRotation( 0 ) + Math.PI * 2 ) < 1e-9 );
});
test("skill remainder follows unsigned native arithmetic; malformed packets do not advance it", () => {
	const owner = createFeedback();
	owner.bootstrap( { character: { level: 1, experience: 0, skillExp: 390 } } );
	const p = Buffer.alloc( 13 );
	p.writeInt32LE( 25, 8 );
	assert.throws( () => owner.receive( 0x30d2, p.subarray( 0, 12 ), 1 ) );
	const result = owner.receive( 0x30d2, p, 1 );
	assert.equal( result.skillExperience, 15 );
	assert.deepEqual( result.messages, [ { key: "UIIT_MSG_STATE_GET_SKILL_EXP", value: 25 } ] );
	p.writeInt32LE( -16, 8 );
	assert.equal( owner.receive( 0x30d2, p, 1 ).skillExperience, 0xffffffff % 400 );
});
const MAPINFO_NATIVE_ROW0 = [
	"0",
	"0",
	"world",
	"interface/worldmap/map/map_worldmap.ddj",
	"128",
	"128",
	"128",
	"128",
	"3584",
	"1024",
	"66",
	"113",
	"177",
	"82",
	"0",
	"192",
	"192",
	"0",
	"xxx"
];
const MAPINFO_ALEXANDRIA = [
	"12",
	"1",
	"alex",
	"interface/worldmap/map/map_alex.ddj",
	"1024",
	"1024",
	"878",
	"1024",
	"878",
	"1024",
	"46",
	"96",
	"51",
	"89",
	"0",
	"0",
	"192",
	"0",
	"xxx"
];

test("map pages and the overview box come from the published mapinfo rows", () => {
	const inCity = { regionId: 90 * 256 + 48, x: 960, y: 0, z: 960, angle: 0 };
	// No published rows, or none beyond the native ones: the native five pages and 28 x 8 overview.
	for ( const raw of [ null, {}, { rows: [] }, { rows: [ MAPINFO_NATIVE_ROW0 ] } ] ) {
		const layout = decodeWorldMapLayout( raw );
		assert.deepEqual( worldMapPages( layout ).map( page => page.id ), [ 1, 2, 3, 4, 5 ] );
		assert.equal( worldMapPageAt( inCity, layout ), 0 );
		assert.equal( worldMapImagePaths( [], layout ).filter( path => path.includes( "map_world_" ) ).length, 28 * 8 );
	}
	// A published town page appears; a fortress page (id 1001) does not.
	const fortress = [
		"1001",
		"1",
		"fort",
		"interface/worldmap/map/map_fort_jangan.ddj",
		...MAPINFO_ALEXANDRIA.slice( 4 )
	];
	const layout = decodeWorldMapLayout( { rows: [ MAPINFO_NATIVE_ROW0, MAPINFO_ALEXANDRIA, fortress ] } );
	assert.deepEqual( worldMapPages( layout ).map( page => page.id ), [ 1, 2, 3, 4, 5, 12 ] );
	assert.equal( worldMapPageAt( inCity, layout ), 12 );
	// A native town keeps its page.
	assert.equal( worldMapPageAt( { regionId: 105 * 256 + 79, x: 1000, y: 0, z: 1000, angle: 0 }, layout ), 5 );
	const quads = worldMapQuads( inCity, 12, [ 100, 100, 256, 256 ], [ 0, 0 ], inCity, layout );
	assert.ok( quads[0].uv[2] <= 1 && quads[0].uv[3] <= 1 );
	// A layout is data the caller passes: decoding one leaves the native default untouched.
	assert.deepEqual( worldMapPages().map( page => page.id ), [ 1, 2, 3, 4, 5 ] );
	assert.equal( worldMapPageAt( inCity ), 0 );
	assert.equal( worldMapPageAt( inCity, nativeLayout() ), 0 );
});

test("a wider overview box adds the tile columns and rows it names", () => {
	const widened = [ ...MAPINFO_NATIVE_ROW0 ];
	widened[10] = "46";
	widened[13] = "70";
	const tiles = worldMapImagePaths( [], decodeWorldMapLayout( { rows: [ widened ] } ) ).filter( path =>
		path.includes( "map_world_" )
	);
	assert.equal( tiles.length, 33 * 11 );
	assert.ok( tiles.some( path => path.endsWith( "map_world_46x113.png" ) ) );
	assert.ok( tiles.some( path => path.endsWith( "map_world_174x73.png" ) ) );
	// A box that is not a whole number of tiles is ignored.
	const ragged = [ ...MAPINFO_NATIVE_ROW0 ];
	ragged[10] = "47";
	const kept = worldMapImagePaths( [], decodeWorldMapLayout( { rows: [ ragged ] } ) );
	assert.equal( kept.filter( path => path.includes( "map_world_" ) ).length, 28 * 8 );
	// The native default is unchanged by either.
	assert.equal( worldMapImagePaths().filter( path => path.includes( "map_world_" ) ).length, 28 * 8 );
});

test("town maps stay inside published texture UVs and manual center survives player movement", () => {
	const pose = { regionId: 105 * 256 + 79, x: 1000, y: 0, z: 1000, angle: 0 };
	assert.equal( worldMapPageAt( pose ), 5 );
	assert.equal( worldMapPageAt( { ...pose, regionId: 0x8001 } ), 0 );
	for ( let id = 1; id <= 5; id++ ) {
		const q = worldMapQuads( pose, id, [ 100, 100, 256, 256 ], [ 0, 0 ] )[0];
		assert.ok( q.uv[2] <= 1 && q.uv[3] <= 1 );
	}
	const a = worldMapQuads( pose, 5, [ 100, 100, 256, 256 ], [ 0, 0 ], pose ),
		b = worldMapQuads( { ...pose, x: 1100 }, 5, [ 100, 100, 256, 256 ], [ 0, 0 ], pose );
	assert.deepEqual( a[0].rect, b[0].rect );
	assert.notDeepEqual( a.at( -1 ).rect, b.at( -1 ).rect );
});
test("fortress countdowns use native constants and leave other packet branches unclaimed", () => {
	assert.deepEqual( [ 1, 2, 3, 4, 5, 6 ].map( n => fortressNotice( 0x3887, Uint8Array.of( n ) ).value ), [
		30,
		0,
		30,
		20,
		10,
		0
	] );
	assert.equal( fortressNotice( 0x3887, Uint8Array.of( 0 ) ), null );
	assert.equal( fortressNotice( 0x3887, Uint8Array.of( 16 ) ), null );
	assert.equal( fortressNotice( 0x1234, Uint8Array.of( 1 ) ), null );
	assert.throws( () => fortressNotice( 0x3887, Uint8Array.of( 1, 0 ) ) );
});

// Check screen-space direction, not a copy of the angle formula. The published
// arrow points right; world +Z is map-up. Exercise the production wire encoder.
test("minimap and full-map arrows face movement across all quadrants and sector edges", () => {
	const from = { regionId: 25000, x: 1910, y: 0, z: 1910, angle: 0 };
	for (
		const [dx, dz] of [ [ 30, 0 ], [ 0, 30 ], [ -30, 0 ], [ 0, -30 ], [ 30, 30 ], [ -30, 30 ], [ -30, -30 ], [
			30,
			-30
		] ]
	) {
		const rx = Math.floor( (from.x + dx) / 1920 ), rz = Math.floor( (from.z + dz) / 1920 );
		const to = {
			...from,
			regionId: ((from.regionId & 255) + rx) | (((from.regionId >>> 8) + rz) << 8),
			x: from.x + dx - rx * 1920,
			z: from.z + dz - rz * 1920
		};
		const angle = movementHeading( from, to ), rotation = minimapRotation( angle ), length = Math.hypot( dx, dz );
		assert.ok( Math.abs( Math.cos( rotation ) - dx / length ) < 0.0001, `horizontal ${dx},${dz}` );
		assert.ok( Math.abs( Math.sin( rotation ) + dz / length ) < 0.0001, `vertical ${dx},${dz}` );
		const pose = { ...from, angle },
			arrow = worldMapQuads( pose, 0, [ 0, 0, 512, 512 ], [ 0, 0 ] ).find( q =>
				q.texture.endsWith( "/mm_sign_character.png" )
			);
		assert.ok( arrow, "full map admits the player arrow" );
		assert.equal( arrow.rotation, rotation );
	}
	assert.ok( Math.abs( Math.cos( minimapRotation( 65535 ) ) - 1 ) < 1e-12, "wrapped zero faces east" );
});

test("the underbar skill points stay inside GDR_STATIC_SP: native %d first, then whole units", () => {
	assert.deepEqual( skillPointReadouts( 12345678 ), [ "12345678", "12345K", "12M", "0B" ] );
	assert.deepEqual( skillPointReadouts( 98765432109 ), [ "98765432109", "98765432K", "98765M", "98B" ] );
	// The HUD draws the first that fits GDR_STATIC_SP (48 pixels, 6-pixel digits).
	const fits = value => value.length * 6 <= 48;
	assert.equal( skillPointReadouts( 12345678 ).find( fits ), "12345678" );
	assert.equal( skillPointReadouts( 123456789 ).find( fits ), "123456K" );
	assert.equal( skillPointReadouts( 98765432109 ).find( fits ), "98765M" );
});
