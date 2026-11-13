/*
===========================================================================

map-teleport.test.mjs - world map pick inverts the projection

The local-player marker is centred on the projected point, so picking the
marker centre must return the same region and region-local x/z.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { worldMapPresentation, worldMapPoint } = await import( "../../src/engine/foundation/ui/world-map.ts" );
const { createMapTeleport } = await import( "../../src/engine/runtime/ui/hud/map-teleport.ts" );

/** @type {import("../../src/engine/contracts/ui").UiRect} */
const clip = [ 100, 80, 600, 420 ];
/** @type {[number, import("../../src/engine/contracts/gameplay").Pose, [number, number]][]} */
const CASES = [
	[ 0, { regionId: 24744, x: 980, y: 0, z: 1100, angle: 0 }, [ 0, 0 ] ],
	[ 0, { regionId: 25000, x: 12, y: 0, z: 1900, angle: 0 }, [ 37, -15 ] ],
	[ 1, { regionId: 25000, x: 400, y: 0, z: 300, angle: 0 }, [ -20, 10 ] ]
];
for ( const [page, pose, pan] of CASES ) {
	test(`map pick round-trips page ${page} region ${pose.regionId}`, () => {
		const marker = worldMapPresentation( pose, page, clip, pan, pose ).markers.at( -1 );
		assert.ok( marker, "local player marker painted" );
		const [x, y, width, height] = marker.rect;
		const picked = worldMapPoint( page, clip, pan, pose, x + width / 2, y + height / 2 );
		assert.ok( picked );
		assert.equal( picked.regionId, pose.regionId );
		assert.ok( Math.abs( picked.x - pose.x ) < 1e-6 && Math.abs( picked.z - pose.z ) < 1e-6 );
	});
}

test("map pick outside the page is rejected", () => {
	const pose = { regionId: 24744, x: 980, y: 0, z: 1100, angle: 0 };
	const marker = worldMapPresentation( pose, 0, clip, [ 0, 0 ], pose ).markers.at( -1 );
	assert.ok( marker );
	assert.equal( worldMapPoint( 0, clip, [ 0, 0 ], pose, -1e6, -1e6 ), null );
});

test("confirmed target becomes a terrain-snapped GM warp line", () => {
	const teleport = createMapTeleport(), pose = { regionId: 24744, x: 980, y: 0, z: 1100, angle: 0 };
	teleport.view( 0, clip, [ 0, 0 ], pose );
	const marker = worldMapPresentation( pose, 0, clip, [ 0, 0 ], pose ).markers.at( -1 );
	assert.ok( marker );
	const [x, y, width, height] = marker.rect;
	const target = teleport.pick( x + width / 2, y + height / 2 );
	assert.ok( target );
	assert.deepEqual( teleport.pending(), target );
	assert.equal( teleport.command( target ), "/warp 24744 980.0 -10000 1100.0" );
	teleport.clear();
	assert.equal( teleport.pending(), null );
	const edge = { regionId: 24744, x: 1919.96, y: 0, z: 1100, angle: 0 };
	teleport.view( 0, clip, [ 0, 0 ], edge );
	const at = worldMapPresentation( edge, 0, clip, [ 0, 0 ], edge ).markers.at( -1 );
	assert.ok( at );
	const picked = teleport.pick( at.rect[0] + at.rect[2] / 2, at.rect[1] + at.rect[3] / 2 );
	assert.ok( picked );
	assert.match(
		teleport.command( picked ),
		/^\/warp 24744 1919\.9 -10000 /,
		"a region-edge pick stays inside its region"
	);
});

const { caveMapView } = await import( "../../src/engine/foundation/ui/cave-map.ts" );

/*
================
cave page fixture

One tomb floor page: 4 x 3 tiles of 128 pixels from tile column 120,
row 130; a tile is one 1920-unit region of the floor's coordinates.
================
*/
const CAVE_PAGE = {
	regionId: 0x8007,
	floorIndex: 0,
	layer: 0,
	floorLabel: "B1",
	columns: 4,
	rows: 3,
	width: 512,
	height: 384,
	left: 120,
	top: 130,
	image: "jinsi1",
	caption: "Tomb B1",
	labels: [],
	signs: []
};
const CAVE_CATALOG = { imageRoot: "/cave/", signIcon: "sign", monsterLevelNote: "", maps: [ CAVE_PAGE ] };

test("a double-click on a dungeon page warps to that floor, not the last world page", () => {
	const teleport = createMapTeleport(), field = { regionId: 24744, x: 980, y: 0, z: 1100, angle: 0 };
	teleport.view( 0, clip, [ 0, 0 ], field );
	const pose = { regionId: 0x8007, x: -10560, y: 0, z: 2880, angle: 0 };
	const view = caveMapView( {
		pose,
		center: pose,
		page: CAVE_PAGE,
		catalog: CAVE_CATALOG,
		clip,
		pan: [ 0, 0 ],
		local: true
	} );
	teleport.viewCave( view.point );
	const marker = view.quads.at( -1 );
	assert.ok( marker );
	const [x, y, width, height] = marker.rect;
	const target = teleport.pick( x + width / 2, y + height / 2 );
	assert.ok( target );
	assert.equal( target.regionId, 0x8007 );
	assert.ok( Math.abs( target.x - pose.x ) < 1e-6 && Math.abs( target.z - pose.z ) < 1e-6 );
	assert.equal( teleport.command( target ), "/warp 32775 -10560.0 -10000 2880.0", "a floor point is not clamped" );
	assert.equal( teleport.pick( clip[0] - 5, clip[1] - 5 ), null, "off the page" );
	teleport.hide();
	assert.equal( teleport.pick( x + width / 2, y + height / 2 ), null, "no page painted, no pick" );
});
