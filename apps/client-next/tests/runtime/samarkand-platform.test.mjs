/*
===========================================================================

samarkand-platform.test.mjs - an object's navigation comes from its BSR's
one navigation mesh

441E40 loads an object's navigation from the mesh its BSR header names at
offset slot 7 (primaryMeshPath), never from its render meshes. The
Samarkand centre platform (samarkand_center_01.bsr) lists ten render
meshes, and samarkand_center_02 carries a navigation tail whose outline
edge is blocked where center_01's is open: built from every render mesh,
the walk up onto the platform stopped at its foot while the server walked
the player up ("going up the platform sticks on the client").

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { readPublishedAssetBytesSync as bytes } from "../../../../scripts/lib/publishedAsset.mjs";
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);

const REGION = 0x6b6c;
// The reported walk: from the ground east of the platform onto its deck.
const FROM = { regionId: REGION, x: 458.8226318359375, y: 180, z: 107.57698059082031, angle: 32840 };
const TO = { regionId: REGION, x: 237, y: 192, z: 105, angle: 32840 };
// The deck stands above the ground the walk starts on.
const DECK_HEIGHT = 190;

const product = await createNavigationResources().resolve(
	bytes( `/assets/world/outdoor/regions/region-${REGION.toString( 16 )}.json` ),
	REGION,
	async p => bytes( p )
);

test("the walk from the ground climbs onto the Samarkand centre platform", () => {
	const movement = createMovement( () => {} );
	movement.seed( FROM );
	movement.navigation( REGION, product );
	movement.speeds( 40, 100, 0 );
	movement.request( TO, 0 );
	for ( let now = 100; now <= 5000; now += 100 ) movement.step( now );
	const pose = defined( movement.state().pose );
	assert.ok( Math.abs( pose.x - TO.x ) < 1 && Math.abs( pose.z - TO.z ) < 1, `reached (${pose.x}, ${pose.z})` );
	assert.ok( pose.y > DECK_HEIGHT, `on the deck (y ${pose.y})` );
});
