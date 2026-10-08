/*
===========================================================================

temple-bridge.test.mjs - both Fertility Temple entrances retain their floor

Published meshes exercise authored links in both directions, including a
finite step that ends between the source and receiving edges.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as bytes } from "../../../../scripts/lib/publishedAsset.mjs";
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { navHeight } = await import( "../../src/engine/foundation/navigation/object-navigation.ts" );
const { createNavigation } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
);

/*
================
portalStand
================
*/
function portalStand( regionId, p, edge ) {
	const cell = p.mesh.edges[edge * 6 + 2];
	let x = 0, z = 0;
	for ( let i = 0; i < 3; i++ ) {
		const at = p.mesh.cells[cell * 3 + i] * 3;
		x += p.mesh.vertices[at] / 3;
		z += p.mesh.vertices[at + 2] / 3;
	}
	const y = navHeight( p.mesh, x, z, 0, cell );
	assert.notEqual( y, null );
	return {
		regionId,
		x: Math.cos( p.yaw ) * x - Math.sin( p.yaw ) * z + p.x,
		y: y + p.y,
		z: Math.sin( p.yaw ) * x + Math.cos( p.yaw ) * z + p.z,
		angle: 0
	};
}

for ( const [regionId, bridgeIndex] of [ [ 0x6687, 0 ], [ 0x6686, 3 ] ] ) {
	const product = await createNavigationResources().resolve(
		bytes( `/assets/world/outdoor/regions/region-${regionId.toString( 16 )}.json` ),
		regionId,
		async p => bytes( p )
	);
	const bridge = product.objects[bridgeIndex];
	assert.ok( bridge.links?.length );
	const link = bridge.links[0], temple = product.objects[link.target];
	const a = portalStand( regionId, bridge, link.edge ), b = portalStand( regionId, temple, link.targetEdge );
	test(`Temple ${regionId.toString( 16 )} unresolved links still stop`, () => {
		const nav = createNavigation();
		nav.install( regionId, { ...product, objects: product.objects.map( p => ({ ...p, links: [] }) ) } );
		const query = { slide: false, status: 0 };
		const rest = nav.clip( a, b, query );
		assert.ok( !rest || query.status & 0x10000001 );
		nav.clear();
	});
	for ( const [direction, [from, to]] of [ [ a, b ], [ b, a ] ].entries() ) {
		for ( const dt of [ 16, 100 ] ) {
			test(`Temple ${regionId.toString( 16 )} direction ${direction}, ${dt}ms steps`, () => {
				const movement = createMovement( () => {} );
				movement.seed( from );
				movement.navigation( regionId, product );
				movement.speeds( 40, 100, 0 );
				movement.request( to, 0 );
				for ( let now = dt; now <= 4000; now += dt ) {
					movement.step( now );
					const pose = movement.state().pose;
					assert.ok( pose );
					assert.ok( pose.y >= Math.min( from.y, to.y ) - 1 );
				}
				const state = movement.state();
				assert.ok( state.pose );
				assert.ok(
					Math.hypot( state.pose.x - Math.trunc( to.x ), state.pose.z - Math.trunc( to.z ) ) < .5,
					JSON.stringify( state.pose )
				);
				assert.ok( state.navigationOwner );
				movement.clear();
			});
		}
	}
}
