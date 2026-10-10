/*
===========================================================================

hunting-guide.test.mjs - catalogue, filter/projection and request-lifetime risks

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
const { decodeHuntingGuide, projectHuntingGuide } = await import( "../../src/engine/foundation/ui/hunting-guide.ts" );
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

test("map cells aggregate nearby species, cull to clip and apply both level bounds and names", () => {
	const data = decodeHuntingGuide( guide );
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 30, 40, 400, 200 ];
	const frame = worldMapFrame( 0, clip, [ 0, 0 ], pose );
	const all = projectHuntingGuide( data, { search: "", min: 1, max: 90 }, frame, clip );
	assert.equal( all.matches, 2 );
	assert.ok(
		all.controls.some( c =>
			c.helpText?.includes( "Mangyang · Lv. 1" ) && c.helpText?.includes( "Water Ghost · Lv. 10" )
		)
	);
	assert.ok( all.controls.every( c =>
		c.draggable && c.rect[0] >= clip[0] && c.rect[1] >= clip[1] &&
		c.rect[0] + c.rect[2] <= clip[0] + clip[2] && c.rect[1] + c.rect[3] <= clip[1] + clip[3]
	) );
	const named = projectHuntingGuide( data, { search: "mang", min: 1, max: 1 }, frame, clip );
	assert.equal( named.matches, 1 );
	assert.equal( named.controls.length, 2 );
	assert.ok( named.controls.every( c => !c.helpText?.includes( "Water Ghost" ) ) );
	assert.equal( projectHuntingGuide( data, { search: "", min: 11, max: 90 }, frame, clip ).controls.length, 0 );
	assert.equal( projectHuntingGuide( data, { search: "", min: 10, max: 1 }, frame, clip ).controls.length, 0 );
});

test("HUD requests only on demand, retains projection and cancels replaced/disposed requests", () => {
	const bytes = new TextEncoder().encode( JSON.stringify( guide ) ), requests = [], cancels = [];
	let delivered = true;
	/** @type {Pick<import("../../src/engine/contracts/assets.ts").AssetOwner, "available" | "request" | "take" | "cancel">} */
	const assets = {
		available: () => 1,
		request: ( url, limit ) => {
			requests.push( { url, limit } );
			return requests.length;
		},
		take: id => delivered ? { kind: "bytes", id, buffer: bytes.buffer } : null,
		cancel: id => cancels.push( id )
	};
	const hud = createHuntingGuideHud( assets ),
		source = { url: "http://fixture.invalid/guide.json", bytes: bytes.length };
	hud.step( source, false );
	assert.equal( requests.length, 0 );
	hud.step( source, true );
	const result = hud.present( 0, [ 0, 0, 400, 200 ], [ 0, 0 ], pose );
	assert.equal( requests.length, 1 );
	assert.strictEqual( hud.present( 0, [ 0, 0, 400, 200 ], [ 0, 0 ], { ...pose } ), result );
	hud.type( "map-hunting-search", "missing" );
	assert.equal( hud.present( 0, [ 0, 0, 400, 200 ], [ 0, 0 ], pose ).controls.length, 0 );
	hud.reset();
	delivered = false;
	hud.step( { ...source, url: source.url + "?new" }, true );
	hud.step( { ...source, url: source.url + "?other" }, false );
	assert.deepEqual( cancels, [ 2 ] );
	hud.step( { ...source, url: source.url + "?other" }, true );
	hud.dispose();
	assert.deepEqual( cancels, [ 2, 3 ] );
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
