/*
===========================================================================

navigation.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mesh, product, pose } from "../helpers/navigation-fixture.mjs";
import {
	readPublishedAssetBytesSync as assetBytes,
	readPublishedAssetJsonSync as assetJson
} from "../../../../scripts/lib/publishedAsset.mjs";
const bytes = p => assetBytes( p, "../../.generated/client-public" ),
	json = p => assetJson( p, "../../.generated/client-public" );
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { createNavigation } = await load(
		"runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
	),
	{ createNavigationResources } = await load( "runtime/assets/worker/navigation/navigation.ts" ),
	{ createNavigationStream } = await load( "runtime/navigation/navigation.ts" ),
	{ interpolateMovement, poseDistance } = await load( "foundation/gameplay/native-movement.ts" );
test("solid NVM plane bit raises the sampled ground, survives resource projection, and preserves hills", async () => {
	const nav = createNavigation(), p = product();
	p.objects = [];
	const row = p.navmesh.regions[0];
	const types = Buffer.alloc( 36 ), planes = Buffer.alloc( 144 );
	for ( let i = 0; i < 36; i++ ) planes.writeFloatLE( 800, i * 4 );
	for ( const [flag, want] of [ [ 0, 0 ], [ 1, 0 ], [ 2, 800 ], [ 3, 800 ], [ 4, 0 ] ] ) {
		types.fill( flag );
		row.planeType = types.toString( "base64" );
		row.planeHeight = planes.toString( "base64" );
		nav.install( 257, p );
		assert.equal( nav.surface( pose ).y, want, "only bit 0x02 makes the plane solid" );
	}
	types.fill( 2 );
	row.planeType = types.toString( "base64" );
	const heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let i = 0; i < 97 * 97; i++ ) heights.writeFloatLE( 900, i * 4 );
	row.heightMap = heights.toString( "base64" );
	nav.install( 257, p );
	assert.equal( nav.surface( pose ).y, 900 );
	const retail = await createNavigationResources().resolve(
		bytes( "/assets/world/outdoor/regions/region-5c81.json" ),
		0x5c81,
		async path => bytes( path )
	);
	assert.ok(
		retail.navmesh.regions.every( r => r.planeType && r.planeHeight ),
		"worker must preserve planes for every neighbor"
	);
	nav.install( 0x5c81, retail );
	const lake = { regionId: 0x5c81, x: 750, y: 690, z: 410, angle: 0 };
	assert.equal( nav.surface( lake ).y, 800 );
	const to = nav.clip( { ...lake, y: 800 }, { ...lake, x: 800, z: 430 } );
	assert.equal( to.y, 800 );
	assert.throws(
		() => nav.install( 257, { ...p, navmesh: { ...p.navmesh, regions: [ { ...row, planeHeight: undefined } ] } } ),
		/Incomplete navigation plane/
	);
	assert.equal( nav.surface( lake ).y, 800, "rejected plane admission preserves the previous navigation" );
});
test("Jangan potion-seller building blocks height-independent entry before the reported westward trap", async () => {
	const p = await createNavigationResources().resolve(
		bytes( "/assets/world/outdoor/regions/region-61a8.json" ),
		0x61a8,
		async path => bytes( path )
	);
	const nav = createNavigation();
	nav.install( p.regionId, p );
	// Displayed (6500,1099) -> (6495,1099), in the outdoor sector frame.
	const inside = { regionId: 0x61a8, x: 1640, y: 0, z: 1390, angle: 0 }, west = { ...inside, x: 1590 };
	const building = p.objects.find( o =>
		Math.abs( o.x - 1712.0404052734375 ) < .001 && Math.abs( o.z - 1459.9156494140625 ) < .001
	);
	assert.ok( building?.terrainCells?.length, "published terrain must carry the building candidate associations" );
	const outlines = Array.from( building.mesh.edges ).filter( ( _, i ) =>
		i % 6 === 4 && building.mesh.edges[i + 1] === 0
	);
	assert.equal( outlines.length, 76 );
	assert.ok( outlines.every( flags => flags === 3 ), "this is not a zero-flag outline incident" );
	for ( const z of [ 1250, 1300, 1550 ] ) {
		for ( const y of [ 0, 8.8, 40 ] ) {
			const from = { ...inside, z, y }, result = nav.clip( from, inside );
			assert.ok(
				result && Math.hypot( result.x - inside.x, result.z - inside.z ) > 40,
				"solid perimeter must stop entry regardless of chord Y"
			);
		}
	}
	const stop = nav.clip( inside, west );
	assert.ok( stop.x > 1594.7 && stop.x < 1594.9, "reported westward request hits the solid perimeter" );
	// Discriminating historical control: missing candidate associations invokes
	// the old +/-2 edge-height approximation. High walls disappear; low walls do not.
	const legacy = structuredClone( p );
	for ( const o of legacy.objects ) delete o.terrainCells;
	nav.install( p.regionId, legacy );
	assert.equal( nav.clip( { ...inside, z: 1300 }, inside ).z, inside.z );
	assert.ok( nav.clip( inside, west ).x > 1594.7 );
});
test("retained stair owner survives chord Y drift, while a terrain clip resolves its actual height", () => {
	const nav = createNavigation();
	nav.install( 257, product( 257, true ) );
	const out = { slide: false, sourceOwner: { placement: 0, cell: 1 } };
	const result = nav.clip( { ...pose, y: 16 }, { ...pose, x: 40, y: 16 }, out );
	assert.equal( result.y, 10 );
	assert.equal( result.x, 40 );
	assert.ok( out.owner );
	// Terrain stays underneath an overhead deck when no owner was acquired.
	const ground = product();
	ground.objects = [];
	const row = ground.navmesh.regions[0], heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let z = 0; z < 97; z++ ) for ( let x = 0; x < 97; x++ ) heights.writeFloatLE( x * x, 4 * (z * 97 + x) );
	row.heightMap = heights.toString( "base64" );
	const blocked = Buffer.alloc( 9216 );
	blocked[2 * 96 + 2] = 1;
	row.blockedTiles = blocked.toString( "base64" );
	nav.install( 257, ground );
	const clipped = nav.clip( { ...pose, y: .5 }, { ...pose, x: 100, y: 25 } );
	assert.ok( clipped.x < 40 && clipped.x > 39.9 );
	assert.ok( Math.abs( clipped.y - nav.surface( clipped ).y ) < 1e-9 );
});

test("Constantinople reported stair follows its cells down and up without inventing an outline entry", async () => {
	const { navLocal } = await load( "foundation/navigation/object-navigation.ts" );
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const p = await createNavigationResources().resolve(
		bytes( "/assets/world/outdoor/regions/region-6046.json" ),
		0x6046,
		async path => bytes( path )
	);
	const a = { regionId: 0x6046, x: 920, y: 1124.1568906758841, z: 610, angle: 0 },
		b = { ...a, y: 1090.5145930961428, z: 670 };
	let crossings = 0;
	for ( const object of p.objects ) {
		const from = navLocal( object, a.x, a.y, a.z ),
			to = navLocal( object, b.x, b.y, b.z ),
			{ vertices: v, edges: e } = object.mesh,
			dx = to[0] - from[0],
			dz = to[2] - from[2];
		for ( let i = 0; i < e.length; i += 6 ) {
			if ( e[i + 5] !== 0 || (e[i + 4] & 0x10) ) continue;
			const ia = e[i] * 3,
				ib = e[i + 1] * 3,
				sx = v[ib] - v[ia],
				sz = v[ib + 2] - v[ia + 2],
				den = dx * sz - dz * sx;
			if ( Math.abs( den ) < 1e-12 ) continue;
			const t = ((v[ia] - from[0]) * sz - (v[ia + 2] - from[2]) * sx) / den,
				u = ((v[ia] - from[0]) * dz - (v[ia + 2] - from[2]) * dx) / den;
			if ( t >= 0 && t <= 1 && u >= 0 && u <= 1 ) crossings++;
		}
	}
	assert.equal( crossings, 0, "428300 outline probe cannot establish ownership on this chord" );
	for ( const [from, to] of [ [ a, b ], [ b, a ] ] ) {
		const movement = createMovement( () => {} );
		movement.seed( from );
		movement.navigation( p.regionId, p );
		movement.request( to, 0 );
		let previous = from.y;
		for ( let now = 0; now <= 2000; now += 20 ) {
			movement.step( now );
			const current = movement.state().pose;
			assert.ok( Math.abs( current.y - previous ) < 3, "no surface pop" );
			previous = current.y;
		}
		assert.ok( Math.abs( movement.state().pose.z - to.z ) < .01 );
		assert.ok( Math.abs( previous - to.y ) < .01 );
		assert.ok( movement.state().navigationOwner );
		movement.clear();
	}
	const nav = createNavigation();
	nav.install( p.regionId, p );
	const clipped = nav.clip( { ...a, y: 1126.6672973632812 }, b );
	assert.ok( clipped.z < 620, "terrain-owned input does not magically become an object owner" );
	assert.ok( Math.abs( clipped.y - nav.surface( clipped ).y ) < .01 );
});
test("prediction crosses admitted outdoor seams with destination heights and canonical coordinates", () => {
	const nav = createNavigation(), p = product();
	p.objects = [];
	const row = p.navmesh.regions[0];
	const heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let i = 0; i < 97 * 97; i++ ) heights.writeFloatLE( 30, i * 4 );
	p.navmesh.regions.push( { ...row, dx: 1, heightMap: heights.toString( "base64" ) } );
	nav.install( 257, p );
	const a = { ...pose, x: 1910, y: 0 }, b = { ...pose, regionId: 258, x: 30, y: 30 };
	assert.deepEqual( nav.clip( a, b ), b );
	const back = nav.clip( b, a );
	assert.equal( back.regionId, 257 );
	assert.equal( back.x, 1910 );
	assert.equal( back.y, 0 );
	const middle = interpolateMovement( a, b, .5 );
	assert.equal( middle.regionId, 258 );
	assert.equal( middle.x, 10 );
	const blocked = Buffer.alloc( 9216 );
	blocked[2 * 96] = 1;
	p.navmesh.regions[1].blockedTiles = blocked.toString( "base64" );
	nav.install( 257, p );
	const stop = nav.clip( a, b );
	assert.equal( stop.regionId, 257 );
	assert.ok( stop.x < 1920 && stop.x > 1919.9 );
	p.navmesh.regions.pop();
	nav.install( 257, p );
	assert.equal( nav.clip( a, b ), null );
	assert.equal( nav.clip( a, { ...b, regionId: 0x8001 } ), null );
	nav.clear();
});
test("object deck overrides blocked terrain and rail clips without blocking ground below", () => {
	const nav = createNavigation();
	nav.install( 257, product( 257, true ) );
	let to = nav.clip( pose, { ...pose, x: 200 } );
	assert.equal( to.x, 99.82111358642578 );
	assert.equal( to.z, 49.91055679321289 );
	assert.equal( to.y, 10 );
	assert.equal( nav.clip( { ...pose, y: 0 }, { ...pose, x: 200, y: 0 } ), null );
	nav.install( 257, product() );
	to = nav.clip( { ...pose, y: 0 }, { ...pose, x: 200, y: 0 } );
	assert.equal( to.x, 200 );
	assert.equal( to.y, 0 );
	const bad = product();
	bad.objects[0].mesh.cells[0] = 99;
	assert.throws( () => nav.install( 257, bad ) );
	assert.equal( nav.clip( { ...pose, y: 0 }, { ...pose, x: 200, y: 0 } ).x, 200 );
	nav.clear();
	assert.equal( nav.clip( pose, { ...pose, x: 20 } ), null );
});
test("dungeon prediction preserves coordinates and cannot bridge disconnected islands", () => {
	const nav = createNavigation(), p = product( 0x8001 );
	p.objects[0].mesh.edges = new Uint32Array();
	p.objects.push( { ...p.objects[0], x: 200 } );
	nav.install( 0x8001, p );
	const a = { ...pose, regionId: 0x8001 }, to = nav.clip( a, { ...a, x: 250 } );
	assert.ok( to.x > 99.9 && to.x < 100 );
	assert.equal( to.regionId, 0x8001 );
	assert.deepEqual( interpolateMovement( { ...a, x: -2000 }, { ...a, x: 4000 }, .5 ), { ...a, x: 1000 } );
	assert.equal( poseDistance( { ...a, x: -2000 }, { ...a, x: 4000 } ), 6000 );
	assert.throws( () => poseDistance( a, { ...a, regionId: 0x8003 } ), /teleport/ );
});
test("published outdoor neighborhood and every dungeon decode and admit", async () => {
	const resources = createNavigationResources(), nav = createNavigation();
	for (
		const [path, id] of [
			[ "/assets/world/outdoor/regions/region-60a8.json", 0x60a8 ],
			...json( "/assets/world/dungeon/dungeon-resources.json" ).entries.map(
				e => [ "/assets/world/dungeon/dungeon-resources.json", e.sectorId ]
			)
		]
	) {
		const p = await resources.resolve( bytes( path ), id, async path => bytes( path ) );
		assert.ok( p.objects.length > 0, path + id );
		assert.doesNotThrow( () => nav.install( p.regionId, p ), path + id );
	}
});
test("stream cancels stale completions and reset requires fresh collision admission", () => {
	let id = 0;
	const pending = new Map(), sent = [], cancelled = [], requests = [];
	const assets = {
		available: () => 4,
		/*
================
request
================
		*/
		request( url, limit, decode ) {
			requests.push( { url, decode } );
			pending.set( ++id, null );
			return id;
		},
		/*
================
take
================
		*/
		take( id ) {
			const p = pending.get( id );
			if ( p ) pending.delete( id );
			return p;
		},
		/*
================
cancel
================
		*/
		cancel( id ) {
			cancelled.push( id );
			pending.delete( id );
		}
	};
	const stream = createNavigationStream( assets, c => sent.push( c ), "https://fixture.test" );
	stream.step( pose );
	pending.set( 1, {
		kind: "bytes",
		buffer: Buffer.from(
			JSON.stringify( {
				regionsById: {
					"0x0101": [ { bundlePublicPath: "/assets/a.json" } ],
					"0x0102": [ { bundlePublicPath: "/assets/b.json" } ]
				}
			} )
		)
	} );
	stream.step( pose );
	assert.equal( requests[1].decode, "navigation" );
	stream.step( { ...pose, regionId: 258 } );
	assert.deepEqual( cancelled, [ 2 ] );
	pending.set( 2, { kind: "navigation", product: product() } );
	pending.set( 3, { kind: "navigation", product: product( 258 ) } );
	stream.step( { ...pose, regionId: 258 } );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].regionId, 258 );
	assert.equal( stream.phase(), "admitting" );
	stream.step( { ...pose, regionId: 258 }, 257 );
	assert.equal( stream.phase(), "admitting" );
	stream.step( { ...pose, regionId: 258 }, 258, undefined, sent[0].requestId );
	assert.equal( stream.phase(), "ready" );
	stream.reset();
	stream.step( { ...pose, regionId: 258 } );
	assert.equal( requests.length, 4 );
	stream.dispose();
	assert.ok( cancelled.includes( 4 ) );
	assert.equal( stream.error(), null );
});

test("asset worker admits and publishes the navigation lane within its request budget", async t => {
	const { createLoader } = await load( "runtime/assets/worker/loader.ts" );
	const results = [];
	t.mock.method(
		globalThis,
		"fetch",
		async () => new Response( bytes( "/assets/world/dungeon/dungeon-resources.json" ) )
	);
	const loader = createLoader( r => {
		if ( r.kind !== "progress" ) results.push( r );
	} );
	t.after( () => loader.dispose() );
	loader.receive( {
		kind: "load",
		id: 1,
		url: "http://fixture.test/dungeon.json#32769",
		limit: 128 << 20,
		decode: "navigation"
	} );
	for ( let i = 0; i < 100 && !results.length; i++ ) await new Promise( resolve => setTimeout( resolve, 10 ) );
	assert.equal( results[0]?.kind, "navigation", results[0]?.error );
	assert.equal( results[0].product.regionId, 0x8001 );
	assert.ok( results[0].product.objects.length > 0 );
});

const { dungeonLinks, portalCongruent, objectLinks } = await load( "foundation/navigation/topology.ts" );
/*
================
linkedRooms
================
*/
function linkedRooms() {
	const a = mesh(), b = mesh();
	a.edges = Uint32Array.from( [ 1, 2, 0, 65535, 8, 0 ] );
	b.edges = Uint32Array.from( [ 3, 0, 1, 65535, 8, 0 ] );
	return [ { x: 0, y: 0, z: 0, yaw: 0, block: 0, mesh: a }, { x: 100, y: 0, z: 0, yaw: 0, block: 1, mesh: b } ];
}
test("declared dungeon portals permit forward and reverse traversal; absent links retain collision", () => {
	const nav = createNavigation(),
		raw = linkedRooms(),
		objects = dungeonLinks( raw, [ [ 1 ], [ 0 ] ] ),
		p = { regionId: 0x8001, complete: true, objects };
	assert.equal( objects[0].links[0].target, 1 );
	assert.equal( objects[1].links[0].targetEdge, 0 );
	nav.install( p.regionId, p );
	const a = { ...pose, regionId: p.regionId, x: 50 }, b = { ...a, x: 150 };
	assert.equal( nav.clip( a, b ).x, 150 );
	assert.equal( nav.clip( b, a ).x, 50 );
	nav.install( p.regionId, { ...p, objects: dungeonLinks( raw, [ [], [] ] ) } );
	assert.ok( nav.clip( a, b ).x < 100 );
	const bad = structuredClone( p );
	bad.objects[0].links[0].target = 99;
	assert.throws( () => nav.install( p.regionId, bad ) );
	assert.ok( nav.clip( a, b ).x < 100 );
	nav.clear();
	assert.equal( nav.clip( a, b ), null );
});
test("portal endpoint tolerance is strict, three-dimensional, orientation aware and adjacency scoped", () => {
	const [a, b] = linkedRooms();
	assert.equal( portalCongruent( a, 0, b, 0 ), true );
	assert.equal( portalCongruent( a, 0, { ...b, y: 4.99 }, 0 ), true );
	assert.equal( portalCongruent( a, 0, { ...b, y: 5 }, 0 ), false );
	assert.equal( dungeonLinks( [ a, b ], [ [], [] ] )[0].links.length, 0 );
	assert.throws( () => dungeonLinks( [ a, b ], [ [ 9 ], [] ] ) );
	const bytes = Buffer.from( [ 2, 0, 3, 0, 4, 0 ] );
	assert.deepEqual( objectLinks( 1, bytes.toString( "base64" ) ), [ { target: 2, targetEdge: 3, edge: 4 } ] );
	assert.throws( () => objectLinks( 2, bytes.toString( "base64" ) ) );
});

test("portal matching agrees with original 45d180 execution at tolerance boundaries", async () => {
	const fs = await import( "node:fs" ),
		oracle = JSON.parse( fs.readFileSync( "tests/fixtures/native/native-portal-reference.json", "utf8" ) );
	assert.equal( oracle.rows.length, 42 );
	const edge = vertices => ({
		x: 0,
		y: 0,
		z: 0,
		yaw: 0,
		mesh: { vertices: Float32Array.from( vertices ), edges: Uint32Array.from( [ 0, 1, 0, 65535, 8, 0 ] ) }
	});
	for ( const row of oracle.rows ) {
		assert.equal( portalCongruent( edge( row.a ), 0, edge( row.b ), 0 ), row.matches, JSON.stringify( row ) );
	}
});

test("authored dungeon collision circles clip movement and permit escape from an occupied circle", () => {
	const nav = createNavigation(), p = product( 0x8001 );
	p.objects[0].mesh.edges = new Uint32Array();
	p.objects[0].obstacles = [ { x: 60, y: 10, z: 50, radiusSquared: 625 } ];
	nav.install( p.regionId, p );
	const a = { ...pose, regionId: p.regionId, x: 10 }, b = { ...a, x: 90 };
	const hit = nav.clip( a, b );
	assert.ok( hit.x > 34.9 && hit.x < 35 );
	assert.equal( nav.clip( { ...a, x: 65 }, { ...a, x: 60 } ).x, 65 );
	assert.equal( nav.clip( { ...a, x: 65 }, b ).x, 90 );
});

test("dungeon destinations outside admitted triangles reach collision before endpoint admission", () => {
	const nav = createNavigation(), regionId = 0x8001;
	const m = {
		vertices: Float32Array.of( 0, 0, 0, 0, 0, 100, 100, 0, 0 ),
		cells: Uint16Array.of( 0, 1, 2 ),
		edges: Uint32Array.of( 0, 1, 0, 65535, 2, 0, 1, 2, 0, 65535, 2, 0, 2, 0, 0, 65535, 2, 0 ),
		bounds: [ 0, 0, 0, 100, 0, 100 ],
		passThrough: false
	};
	nav.install( regionId, { regionId, complete: true, objects: [ { x: 0, y: 0, z: 0, yaw: 0, mesh: m } ] } );
	const from = { regionId, x: 10, y: 0, z: 10, angle: 0 };
	const hit = nav.clip( from, { ...from, x: 80, z: 80 } );
	assert.ok( hit, "native 453fa0 returns a clipped contact, not absent coverage" );
	assert.ok( hit.x > 49 && hit.x < 50 );
	assert.ok( hit.z > 49 && hit.z < 50 );
	assert.equal( nav.clip( { ...from, x: 80, z: 80 }, from ), null, "unadmitted starts still refuse prediction" );
	nav.clear();
	assert.equal( nav.clip( from, { ...from, x: 80, z: 80 } ), null );
});

/*
================
portalInterior
================
*/
function portalInterior( p, e, regionId ) {
	const m = p.mesh,
		v = m.vertices,
		at = m.edges[e * 6] * 3,
		bt = m.edges[e * 6 + 1] * 3,
		cell = m.edges[e * 6 + 2] * 3;
	let x = (v[at] + v[bt]) / 2, y = (v[at + 1] + v[bt + 1]) / 2, z = (v[at + 2] + v[bt + 2]) / 2, cx = 0, cz = 0;
	for ( let k = 0; k < 3; k++ ) {
		cx += v[m.cells[cell + k] * 3] / 3;
		cz += v[m.cells[cell + k] * 3 + 2] / 3;
	}
	const d = Math.hypot( cx - x, cz - z );
	x += (cx - x) / d * .5;
	z += (cz - z) / d * .5;
	return {
		regionId,
		x: Math.cos( p.yaw ) * x - Math.sin( p.yaw ) * z + p.x,
		y: y + p.y,
		z: Math.sin( p.yaw ) * x + Math.cos( p.yaw ) * z + p.z,
		angle: 0
	};
}
test("every published dungeon portal crosses its linked seam through the movement navigation owner", async () => {
	const nav = createNavigation(), resources = createNavigationResources(), failures = [];
	let total = 0;
	for ( const e of json( "/assets/world/dungeon/dungeon-resources.json" ).entries ) {
		const p = await resources.resolve(
			bytes( "/assets/world/dungeon/dungeon-resources.json" ),
			e.sectorId,
			async p => bytes( p )
		);
		nav.install( p.regionId, p );
		for ( let i = 0; i < p.objects.length; i++ ) {
			for ( const link of p.objects[i].links ?? [] ) {
				const a = portalInterior( p.objects[i], link.edge, p.regionId ),
					b = portalInterior( p.objects[link.target], link.targetEdge, p.regionId ),
					q = nav.clip( a, b );
				total++;
				if ( !q || Math.hypot( q.x - b.x, q.z - b.z ) > .1 ) {
					failures.push( { region: p.regionId, source: i, link, a, b, q } );
				}
			}
		}
	}
	(await import( "node:fs" )).writeFileSync(
		"temp/artifacts/topology-corpus-failures.json",
		JSON.stringify( failures )
	);
	assert.equal( total, 308 );
	assert.deepEqual( failures, [] );
});

test("declared portal gaps are bounded and a later wall remains blocking after a portal", () => {
	const nav = createNavigation(), rooms = linkedRooms();
	rooms[1].x = 100.4;
	const p = { regionId: 0x8001, complete: true, objects: dungeonLinks( rooms, [ [ 1 ], [ 0 ] ] ) };
	nav.install( p.regionId, p );
	const a = { ...pose, regionId: p.regionId, x: 50 }, b = { ...a, x: 150 };
	assert.equal( nav.clip( a, b ).x, 150 );
	const m = rooms[1].mesh;
	m.vertices = Float32Array.from( [ ...m.vertices, 50, 10, 0, 50, 10, 100 ] );
	m.cells = Uint16Array.from( [ 0, 4, 5, 0, 5, 3, 4, 1, 2, 4, 2, 5 ] );
	m.edges = Uint32Array.from( [ ...m.edges, 4, 5, 0, 3, 7, 1 ] );
	nav.install( p.regionId, { ...p, objects: dungeonLinks( rooms, [ [ 1 ], [ 0 ] ] ) } );
	const stop = nav.clip( a, { ...b, x: 190 } );
	assert.ok( stop.x > 150.2 && stop.x < 150.3 );
	assert.ok( stop.z > 49.8 && stop.z < 50 );
	rooms[1].x = 106;
	nav.install( p.regionId, { ...p, objects: dungeonLinks( rooms, [ [ 1 ], [ 0 ] ] ) } );
	assert.ok( nav.clip( a, b ).x < 100 );
});

test("Jangan X6434 Y991 acquires the stair surface from terrain and retains it to the deck", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const product = await createNavigationResources().resolve(
		bytes( "/assets/world/outdoor/regions/region-61a8.json" ),
		0x61a8,
		async p => bytes( p )
	);
	const movement = createMovement( () => {} ),
		from = { regionId: 0x61a8, x: 980, y: -32.60888284444809, z: 480, angle: 0 };
	movement.seed( from );
	movement.navigation( from.regionId, product );
	movement.request( { ...from, z: 310, y: 1.4093971252441406 }, 0 );
	let previous = from.y;
	for ( let t = 0; t <= 4000; t += 20 ) {
		movement.step( t );
		const y = movement.state().pose.y;
		assert.ok( Math.abs( y - previous ) < 1.2, `stair discontinuity at ${t}: ${previous} -> ${y}` );
		previous = y;
	}
	assert.ok(
		Math.abs( movement.state().pose.y - 1.4093971252441406 ) < .001,
		JSON.stringify( movement.state().pose )
	);
	assert.ok( movement.state().navigationOwner, "stair owner survives arrival" );
});

test("spatial acceleration preserves exact owner paths, ties and rotated stacked floors", async () => {
	const { createNavigationIndex } = await load( "foundation/navigation/spatial-index.ts" );
	const { terrainOwnerPath, dungeonOwnerPath } = await load( "foundation/navigation/dungeon-ownership.ts" );
	const objects = [];
	for ( let z = -4; z <= 4; z++ ) {
		for ( let x = -4; x <= 4; x++ ) {
			for ( const y of [ 0, 20 ] ) {
				objects.push( { x: x * 160, y, z: z * 160, yaw: (x - z) * .17, mesh: mesh() } );
			}
		}
	}
	objects.push( { ...objects[80] } ); // Equal height/contact retains authored ordering.
	const index = createNavigationIndex( objects );
	let seed = 321;
	const random = () => ((seed = (Math.imul( seed, 1664525 ) + 1013904223) >>> 0) / 4294967296);
	for ( let i = 0; i < 600; i++ ) {
		const from = [ random() * 1500 - 750, random() < .5 ? 10 : 30, random() * 1500 - 750 ],
			to = [ from[0] + random() * 200 - 100, from[1], from[2] + random() * 200 - 100 ];
		for ( const walk of [ terrainOwnerPath, dungeonOwnerPath ] ) {
			const expected = walk( objects, from, to );
			assert.deepEqual(
				walk( objects, from, to, undefined, index ),
				expected,
				JSON.stringify( { i, from, to } )
			);
			if ( expected.owner ) {
				assert.deepEqual(
					walk( objects, from, to, expected.owner, index ),
					walk( objects, from, to, expected.owner )
				);
			}
		}
	}
	const before = index.stats();
	index.placements( [ 0, 10, 0 ], [ 10, 10, 10 ] );
	const after = index.stats();
	assert.ok( after.candidates - before.candidates < 10 );
	assert.ok( after.visited - before.visited < objects.length / 2 );
});

test("malformed retained outline grid cannot replace an admitted navigation product", () => {
	const nav = createNavigation(), p = product();
	nav.install( 257, p );
	for (
		const grid of [
			{ x: 0, z: 0, nx: 1, nz: 1, offsets: new Uint32Array( [ 0, 1 ] ), edgeIds: new Uint16Array( [ 99 ] ) },
			{ x: 0, z: 0, nx: 1, nz: 1, offsets: new Uint32Array( [ 0, 1 ] ), edgeIds: new Uint16Array( [ 1 ] ) },
			{ x: 0, z: 0, nx: 2, nz: 1, offsets: new Uint32Array( [ 0, 2, 1 ] ), edgeIds: new Uint16Array( [ 0 ] ) }
		]
	) {
		const bad = product();
		bad.objects[0].mesh.outlineGrid = grid;
		assert.throws( () => nav.install( 257, bad ) );
		assert.deepEqual( nav.clip( pose, { ...pose, x: 20 } ), { ...pose, x: 20 } );
	}
});

test("stationary spawn resolves its surface in either navigation/spawn arrival order", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	for ( const navFirst of [ false, true ] ) {
		const movement = createMovement( () => {} ), p = product();
		p.objects = [];
		const from = { ...pose, y: -8 };
		if ( navFirst ) movement.navigation( 257, p );
		movement.seed( from );
		if ( !navFirst ) {
			assert.equal( movement.state().pose.y, -8 );
			movement.navigation( 257, p );
		}
		assert.equal( movement.state().pose.y, 0, "grounded before a movement request" );
		assert.equal( movement.state().authoritativePose.y, 0 );
		assert.equal( movement.step( 100 ), false );
		assert.equal( movement.state().moving, false );
		movement.request( { ...from, x: 30, y: 0 }, 100 );
		movement.step( 101 );
		assert.equal( movement.state().pose.y, 0, "first movement cannot cause a grounding pop" );
		movement.clear();
	}
});

test("stationary spawn chooses the nearby authored deck, not terrain underneath it", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} );
	movement.seed( { ...pose, y: 9 } );
	movement.navigation( 257, product() );
	assert.equal( movement.state().pose.y, 10 );
	assert.equal( movement.state().moving, false );
	movement.clear();
	movement.navigation( 257, product() );
	movement.seed( { ...pose, y: 0 } );
	assert.equal( movement.state().pose.y, 0, "a player underneath the deck stays underneath" );
	movement.clear();
});

test("navigation arrival publishes corrected stationary gameplay without waiting for input", async () => {
	const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
	const game = createGameplay( () => {} ), p = product();
	p.objects = [];
	game.seed( { ...pose, y: -8, gid: 7, heading: 0 } );
	assert.equal( game.take().pose.y, -8 );
	assert.equal( game.take(), null );
	game.command( { kind: "navigation", regionId: 257, bundle: p }, 0 );
	const published = game.take();
	assert.equal( published.pose.y, 0 );
	assert.equal( published.moving, false );
	assert.equal( game.take(), null );
	game.dispose();
});

test("authored terrain associations block outside entry independent of guessed endpoint height", async () => {
	const { navContactDetail } = await load( "foundation/navigation/object-navigation.ts" );
	const p = { x: 0, y: 0, z: 0, yaw: 0, mesh: mesh(), terrainCells: [ [ 0, 0, 200, 100 ] ] };
	for ( const y of [ 0, 8, 10, 13, 80 ] ) {
		assert.ok(
			navContactDetail( p, [ 110, y, 50 ], [ 90, y, 50 ] ),
			"authored obstacle remains blocking at Y=" + y
		);
	}
	assert.equal(
		navContactDetail( { ...p, terrainCells: [] }, [ 110, 10, 50 ], [ 90, 10, 50 ] ),
		null,
		"unassociated overhead object is not a terrain candidate"
	);
	assert.equal(
		navContactDetail( { ...p, terrainCells: [ [ 0, 200, 200, 300 ] ] }, [ 110, 10, 50 ], [ 90, 10, 50 ] ),
		null
	);
});
test("reported city building entry keeps collision at terrain and stale respawn heights", async () => {
	const p = await createNavigationResources().resolve(
		bytes( "/assets/world/outdoor/regions/region-6b4f.json" ),
		0x6b4f,
		async path => bytes( path )
	);
	assert.ok( p.objects.some( o => o.terrainCells?.length ) );
	const nav = createNavigation();
	nav.install( p.regionId, p );
	for ( const y of [ 80, 82, 120 ] ) {
		const from = { regionId: 0x6b4f, x: 1205, y, z: 396, angle: 0 }, to = { ...from, x: 1394, y: 82, z: 398 };
		const hit = nav.clip( from, to );
		assert.ok( hit && hit.x < 1390, "cannot enter building with stale source height " + y );
	}
});

test("terrain object association admission rejects malformed bounds without replacing resident collision", () => {
	const nav = createNavigation(), good = product();
	nav.install( 257, good );
	const before = nav.clip( pose, { ...pose, x: 120 } );
	for ( const cells of [ [ [ 0, 0, NaN, 10 ] ], [ [ 10, 0, 0, 10 ] ], [ [ 0, 0, 10 ] ] ] ) {
		const bad = product();
		bad.objects[0].terrainCells = cells;
		assert.throws( () => nav.install( 257, bad ), /terrain object cells/ );
		assert.deepEqual( nav.clip( pose, { ...pose, x: 120 } ), before );
	}
});

// A NavOwner indexes the installed product, whose placements are cloned on
// every install; re-centring the product on the next region shifts indices.
// The native cell pointer survives region borders, so the owner is carried by
// the placement's world geometry (anchor -> relocate), never re-guessed.
test("navigation re-centring relocates the retained owner to the same placed object", () => {
	const nav = createNavigation();
	nav.install( 257, product( 257, true ) );
	const anchor = nav.anchor( { placement: 0, cell: 1 } );
	const next = product( 258, true );
	next.navmesh.regions[0].dx = -1;
	next.objects = [ { x: 500, y: 0, z: 500, yaw: 0, mesh: mesh() }, { x: -1920, y: 0, z: 0, yaw: 0, mesh: mesh() } ];
	nav.install( 258, next );
	assert.deepEqual( nav.relocate( anchor ), { placement: 1, cell: 1 } );
	next.objects = [ { x: 500, y: 0, z: 500, yaw: 0, mesh: mesh() } ];
	nav.install( 258, next );
	assert.equal( nav.relocate( anchor ), undefined, "a vanished object is not guessed" );
});
test("the movement owner survives a navigation re-centre instead of being re-guessed", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} );
	movement.seed( pose );
	movement.navigation( 257, product( 257, true ) );
	movement.request( { ...pose, x: 60 }, 0 );
	for ( let t = 0; t <= 3000; t += 20 ) movement.step( t );
	const owned = movement.state().navigationOwner;
	assert.ok( owned, "deck owner acquired" );
	const next = product( 258, true );
	next.navmesh.regions[0].dx = -1;
	next.objects = [ { x: 500, y: 0, z: 500, yaw: 0, mesh: mesh() }, { x: -1920, y: 0, z: 0, yaw: 0, mesh: mesh() } ];
	movement.navigation( 258, next );
	assert.deepEqual( movement.state().navigationOwner, { placement: 1, cell: owned.cell } );
});

test("owner relocation rejects same-count replacement geometry and ambiguous placements", () => {
	const nav = createNavigation();
	nav.install( 257, product() );
	const anchor = nav.anchor( { placement: 0, cell: 0 } );
	const changed = product();
	changed.objects[0].mesh.vertices[1] += 1;
	nav.install( 257, changed );
	assert.equal( nav.relocate( anchor ), undefined, "equal array lengths do not establish cell identity" );
	const duplicated = product();
	duplicated.objects.push( structuredClone( duplicated.objects[0] ) );
	nav.install( 257, duplicated );
	assert.equal( nav.relocate( anchor ), undefined, "an ambiguous owner must not pick the first duplicate" );
});

test("city bundles are projected to the requested neighborhood before simulation admission", async () => {
	const loader = createNavigationResources();
	for ( const region of [ 0x694e, 0x694f, 0x684d ] ) {
		const p = await loader.resolve(
			bytes( "/assets/world/constantinople/region-694e.json" ),
			region,
			async path => bytes( path )
		);
		assert.equal( p.regionId, region );
		assert.ok( p.navmesh.regions.length <= 9 );
		assert.ok( p.navmesh.regions.some( r => r.dx === 0 && r.dz === 0 ) );
		assert.ok( p.navmesh.regions.every( r => Math.abs( r.dx ) <= 1 && Math.abs( r.dz ) <= 1 ) );
		createNavigation().install( region, p );
	}
});
test("admission rejection fails immediately; retry ignores stale acknowledgements and missing replies time out", () => {
	const pending = new Map(), sent = [];
	let id = 0;
	const assets = {
		available: () => 4,
		request: () => ++id,
		cancel: () => {},
		take: i => {
			const r = pending.get( i );
			pending.delete( i );
			return r;
		}
	};
	const stream = createNavigationStream( assets, c => sent.push( c ), "https://fixture.test" );
	const step = ( region, fail, ack, now = 0 ) => stream.step( pose, region, fail, ack, now );
	step();
	pending.set( 1, {
		kind: "bytes",
		buffer: Buffer.from(
			JSON.stringify( { regionsById: { "0x0101": [ { bundlePublicPath: "/assets/a.json" } ] } } )
		)
	} );
	step();
	pending.set( 2, { kind: "navigation", product: product() } );
	step();
	const failure = { region: 257, requestId: sent[0].requestId, error: "Invalid navigation product" };
	step( undefined, failure );
	assert.equal( stream.phase(), "failed" );
	assert.equal( stream.error(), failure.error );
	stream.retry();
	step();
	pending.set( 3, { kind: "navigation", product: product() } );
	step();
	step( 257, failure, sent[0].requestId );
	assert.equal( stream.phase(), "admitting" );
	step( 257, undefined, sent[1].requestId );
	assert.equal( stream.phase(), "ready" );
	stream.reset();
	step();
	pending.set( 4, { kind: "navigation", product: product() } );
	step();
	step( undefined, undefined, undefined, 15000 );
	assert.equal( stream.phase(), "failed" );
	assert.match( stream.error(), /admitting timed out/ );
	stream.retry();
	step();
	step( undefined, undefined, undefined, 60000 );
	assert.match( stream.error(), /loading timed out/ );
});

test("every published city bundle admits its center using the production decoder", async () => {
	const catalog = json( "/assets/world/world-region-catalog.json" ),
		paths = new Set(
			Object.values( catalog.regionsById ).flatMap( rows =>
				rows.filter( r => r.area !== "outdoor" ).map( r => r.bundlePublicPath )
			)
		);
	for ( const path of paths ) {
		const b = json( path ), region = b.source.sectorX | (b.source.sectorY << 8);
		const p = await createNavigationResources().resolve( bytes( path ), region, async p => bytes( p ) );
		assert.equal( p.regionId, region, path );
		createNavigation().install( region, p );
	}
});
