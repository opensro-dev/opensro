/*
===========================================================================

hunting-guide.test.mjs - catalogue, portrait decluttering and request-lifetime risks

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
const { decodeHuntingGuide, decodeHuntingPortraits, projectHuntingGuide, huntingGuideDetails, HUNTING_PORTRAITS } =
	await import(
		"../../src/engine/foundation/ui/hunting-guide.ts"
	);
const { worldMapFrame } = await import( "../../src/engine/foundation/ui/world-map.ts" );
const { createHuntingGuideHud } = await import( "../../src/engine/runtime/ui/hud/hunting-guide.ts" );
const { huntingGuideSource } = await import( "../../src/engine/runtime/simulation/worker/session/http/http.ts" );

const pose = { regionId: 0x61a0, x: 960, y: 0, z: 960, angle: 0 };
const guide = {
	format: "sro-hunting-guide",
	version: 1,
	rows: [
		{
			refObjId: 1933,
			name: "Mangyang",
			nameKey: "SN_MOB_CH_MANGNYANG",
			level: 1,
			points: [ { regionId: pose.regionId, x: 960, z: 960 }, { regionId: pose.regionId + 1, x: 960, z: 960 } ]
		},
		{
			refObjId: 1934,
			name: "Water Ghost",
			nameKey: "SN_MOB_CH_WATERGHOST",
			level: 10,
			points: [ { regionId: pose.regionId, x: 970, z: 960 } ]
		}
	]
};

test("atlas retains all species anchors and rejects dungeon/nonfinite positions", () => {
	const data = decodeHuntingGuide( guide );
	assert.equal( data.regions.size, 2 );
	assert.equal( data.regions.get( pose.regionId )?.length, 2 );
	for ( const point of [ { regionId: 0x8001, x: 0, z: 0 }, { regionId: pose.regionId, x: NaN, z: 0 } ] ) {
		assert.throws( () => decodeHuntingGuide( { ...guide, rows: [ { ...guide.rows[0], points: [ point ] } ] } ) );
	}
});

test("sections keep their world geometry and complete membership across pan and resize", () => {
	const data = decodeHuntingGuide( guide );
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 30, 40, 400, 200 ];
	const frame = worldMapFrame( 0, clip, [ 0, 0 ], pose );
	const initial = projectHuntingGuide( data, frame, clip );
	const movedFrame = worldMapFrame( 0, clip, [ 17, 9 ], pose );
	const moved = projectHuntingGuide( data, movedFrame, clip );
	assert.ok( initial.controls.length );
	for ( const a of initial.controls ) {
		const b = moved.controls.find( row => row.id === a.id );
		assert.ok( b && a.hitPolygon && b.hitPolygon );
		assert.equal( b.label, a.label );
		assert.equal( b.hitPolygon.length, a.hitPolygon.length );
		for ( let i = 0; i < a.hitPolygon.length; i++ ) {
			assert.ok( Math.abs( b.hitPolygon[i][0] - a.hitPolygon[i][0] - (movedFrame.ox - frame.ox) ) < 1e-8 );
			assert.ok( Math.abs( b.hitPolygon[i][1] - a.hitPolygon[i][1] - (movedFrame.oy - frame.oy) ) < 1e-8 );
		}
	}
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const small = [ 30, 40, 256, 256 ];
	const little = projectHuntingGuide( data, worldMapFrame( 0, small, [ 0, 0 ], pose ), small );
	assert.deepEqual(
		little.controls.map( row => [ row.id, row.label ] ),
		initial.controls.map( row => [ row.id, row.label ] )
	);
	assert.ok(
		little.controls.every( control =>
			control.draggable &&
			control.hitPolygon?.every( ( [x, y] ) =>
				x >= small[0] && x <= small[0] + small[2] && y >= small[1] && y <= small[1] + small[3]
			)
		)
	);
	assert.equal( initial.paths.length, 0, "Unhovered sections do not demand portrait art" );
});

test("section hover reveals every local species with portraits and levels in a bounded card", () => {
	const rows = Array.from( { length: 23 }, ( _, i ) => ({
		...guide.rows[0],
		refObjId: i + 1,
		name: `Species ${i + 1}`,
		level: i + 1,
		points: [ { regionId: pose.regionId, x: 960, z: 960 } ]
	}) );
	const data = decodeHuntingGuide( { ...guide, rows } );
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 640, 384 ];
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const viewport = [ 0, 0, 800, 600 ];
	const projection = projectHuntingGuide( data, worldMapFrame( 0, clip, [ 0, 0 ], pose ), clip );
	assert.equal( projection.controls.length, 1 );
	const image = "/assets/npc/hunting-portraits/" + "a".repeat( 64 ) + ".png";
	const detail = huntingGuideDetails( projection, new Map( [ [ 1, image ] ] ), projection.controls[0].id, viewport );
	assert.ok( detail );
	assert.equal( detail.rows.length, rows.length );
	assert.ok( detail.title.includes( "Lv. 1-23" ) );
	assert.ok( detail.paths.includes( image ) );
	assert.deepEqual( detail.rows.map( row => row.monster.refObjId ), rows.map( row => row.refObjId ) );
	assert.ok(
		detail.rect[0] >= 0 && detail.rect[1] >= 0 && detail.rect[0] + detail.rect[2] <= viewport[2] &&
			detail.rect[1] + detail.rect[3] <= viewport[3]
	);
	assert.equal( huntingGuideDetails( projection, new Map(), null, viewport ), null );
});

test("authored town pages suppress sections while world return restores the same areas", () => {
	const data = decodeHuntingGuide( guide );
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 640, 384 ];
	const world = projectHuntingGuide( data, worldMapFrame( 0, clip, [ 0, 0 ], pose ), clip );
	assert.ok( world.areas.length );
	for ( const page of [ 1, 2, 3, 4, 5 ] ) {
		const town = projectHuntingGuide(
			data,
			worldMapFrame( page, clip, [ 0, 0 ], pose ),
			clip,
			world.controls[0].id
		);
		assert.equal( town.quads.length, 0 );
		assert.equal( town.controls.length, 0 );
		assert.equal( huntingGuideDetails( town, new Map(), world.controls[0].id, clip ), null );
	}
	assert.deepEqual( projectHuntingGuide( data, worldMapFrame( 0, clip, [ 0, 0 ], pose ), clip ), world );
});

test("optional portrait index rejects duplicate identities and external or traversing images", () => {
	const path = "/assets/npc/hunting-portraits/" + "a".repeat( 64 ) + ".png";
	const data = { format: "sro-hunting-portraits", version: 1, rows: [ [ 1933, path ] ] };
	assert.equal( decodeHuntingPortraits( data ).get( 1933 ), path );
	for (
		const rows of [ [ [ 1933, "https://other.invalid/image.png" ] ], [ [ 1933, path ], [ 1933, path ] ], [ [
			1933,
			"/assets/npc/hunting-portraits/../private.png"
		] ] ]
	) assert.throws( () => decodeHuntingPortraits( { ...data, rows } ) );
});

test("HUD requests on demand, caches both resources and cancels replaced or disposed requests", () => {
	const guideBytes = new TextEncoder().encode( JSON.stringify( guide ) ),
		artBytes = new TextEncoder().encode(
			JSON.stringify( { format: "sro-hunting-portraits", version: 1, rows: [] } )
		);
	const artBase = "https://assets.fixture.invalid", artUrl = new URL( HUNTING_PORTRAITS, artBase ).href;
	const requests = [], cancels = [];
	let delivered = true;
	/** @type {Pick<import("../../src/engine/contracts/assets.ts").AssetOwner, "available" | "request" | "take" | "cancel">} */
	const assets = {
		available: () => 2,
		request: ( url, limit ) => {
			assert.doesNotThrow( () => new URL( url ), "The production worker requires absolute URLs" );
			requests.push( { url, limit } );
			return requests.length;
		},
		take: id =>
			delivered ?
				{
					kind: "bytes",
					id,
					buffer: (requests[id - 1].url === artUrl ? artBytes : guideBytes).buffer
				} :
				null,
		cancel: id => cancels.push( id )
	};
	const hud = createHuntingGuideHud( assets, artBase ),
		source = { url: "http://fixture.invalid/guide.json", bytes: guideBytes.length };
	hud.step( source, false );
	assert.equal( requests.length, 0 );
	hud.step( source, true );
	assert.equal( requests.length, 2 );
	assert.equal( requests[1].url, artUrl );
	const result = hud.present( 0, [ 0, 0, 400, 200 ], [ 0, 0 ], pose );
	assert.strictEqual( hud.present( 0, [ 0, 0, 400, 200 ], [ 0, 0 ], { ...pose } ), result );
	hud.step( source, false );
	hud.step( source, true );
	assert.equal( requests.length, 2 );
	delivered = false;
	hud.step( { ...source, url: source.url + "?new" }, true );
	hud.step( { ...source, url: source.url + "?other" }, false );
	assert.deepEqual( cancels, [ 3 ] );
	hud.step( { ...source, url: source.url + "?other" }, true );
	hud.dispose();
	assert.deepEqual( cancels, [ 3, 4 ] );
	const pending = createHuntingGuideHud( assets, artBase );
	pending.step( source, true );
	pending.dispose();
	assert.deepEqual( cancels, [ 3, 4, 5, 6 ] );
});

test("optional atlas identity stays on the selected transport and cannot break legacy admission", () => {
	const hash = "a".repeat( 64 ), base = "https://example.invalid/shards/global-official";
	const source = huntingGuideSource( { path: `/transport/references/${hash}.json`, sha256: hash, bytes: 100 }, base );
	assert.ok( source );
	assert.equal( source.url, base + `/transport/references/${hash}.json` );
	assert.equal( huntingGuideSource( undefined, base ), undefined );
	assert.equal(
		huntingGuideSource( { path: "https://other.invalid/private", sha256: hash, bytes: 100 }, base ),
		undefined
	);
	assert.equal(
		huntingGuideSource( { path: `/transport/references/${hash}.json`, sha256: hash, bytes: 3 << 20 }, base ),
		undefined
	);
});
