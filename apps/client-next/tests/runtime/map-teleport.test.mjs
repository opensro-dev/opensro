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
});
