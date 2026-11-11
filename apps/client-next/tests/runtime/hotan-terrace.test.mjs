/*
===========================================================================

hotan-terrace.test.mjs - a retained terrain stand boards the deck it
stands in, a fresh point does not (#3006)

The Hotan terrace lip in region 0x5E87: the gate structure's flat deck
cell (y 243.85) covers the stand, and its ramp descends to the street in
0x5D87 (y 146). The terrain under the deck samples 242.68, so a fresh
point there arbitrates to the terrain (FindNavCell, terrain winning by
|deltaY|) and a town-ward click clips at the first blocked terrain tile
under the ramp. A mover that walked to the lip keeps its terrain owner,
and that retained stand is resident on the deck cell: the walk boards
the deck and descends the ramp, as the server's retained seat does
(server navowner.go residentObjectStand).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { readPublishedAssetBytesSync as bytes } from "../../../../scripts/lib/publishedAsset.mjs";
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const { createNavigation } = await import( "../../src/engine/foundation/navigation/clipper.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);

const REGION = 0x5e87;
// The report's stand: terrain-owned at the lip, inside the deck cell.
const LIP = { regionId: REGION, x: 1138.18, y: 242.68, z: 160.01, angle: 0 };
// The clicked street point below, across the region seam.
const STREET = { regionId: 0x5d87, x: 1159, y: 146, z: 1832, angle: 0 };
// A plaza point the player walks from first, as in the report's replay.
const PLAZA = { regionId: REGION, x: 1186, y: 243, z: 288, angle: 0 };

const product = await createNavigationResources().resolve(
	bytes( `/assets/world/outdoor/regions/region-${REGION.toString( 16 )}.json` ),
	REGION,
	async p => bytes( p )
);

/*
================
reachedStreet
================
*/
function reachedStreet( pose ) {
	return pose.regionId === STREET.regionId && Math.abs( pose.x - STREET.x ) < .5 &&
		Math.abs( pose.z - STREET.z ) < .5 && Math.abs( pose.y - STREET.y ) < .5;
}

test("a fresh point at the lip still clips at the ramp, a retained terrain stand descends", () => {
	const navigation = createNavigation();
	navigation.install( REGION, product );
	const fresh = { slide: false, sourceTerrainRetained: false, status: 0 };
	const freshRest = navigation.clip( LIP, STREET, fresh );
	assert.ok( freshRest );
	assert.ok( !reachedStreet( freshRest ), `fresh point reached the street: ${JSON.stringify( freshRest )}` );
	const retained = { slide: false, sourceTerrainRetained: true, status: 0 };
	const retainedRest = navigation.clip( LIP, STREET, retained );
	assert.ok( retainedRest );
	assert.ok(
		reachedStreet( retainedRest ),
		`retained stand did not reach the street: ${JSON.stringify( retainedRest )}`
	);
	navigation.clear();
});

test("the walked player boards the deck at the lip and walks down to the street", () => {
	const movement = createMovement( () => {} );
	movement.seed( PLAZA );
	movement.navigation( REGION, product );
	movement.speeds( 40, 100, 0 );
	movement.request( { ...LIP, y: 243 }, 0 );
	for ( let now = 100; now <= 2500; now += 100 ) movement.step( now );
	const lip = defined( movement.state().pose );
	assert.ok(
		Math.abs( lip.x - Math.trunc( LIP.x ) ) < 1 && Math.abs( lip.z - Math.trunc( LIP.z ) ) < 1,
		`did not reach the lip: ${JSON.stringify( lip )}`
	);
	movement.request( STREET, 2500 );
	for ( let now = 2600; now <= 9000; now += 100 ) movement.step( now );
	const pose = defined( movement.state().pose );
	assert.ok( reachedStreet( pose ), `did not reach the street: ${JSON.stringify( pose )}` );
	movement.clear();
});
