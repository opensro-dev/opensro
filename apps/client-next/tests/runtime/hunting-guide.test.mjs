/*
===========================================================================

hunting-guide.test.mjs - catalogue, portrait decluttering and request-lifetime risks

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
const { decodeHuntingGuide, decodeHuntingPortraits, projectHuntingGuide, HUNTING_PORTRAITS } = await import(
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

test("portraits group nearby species honestly and avoid reserved native landmarks", () => {
	const data = decodeHuntingGuide( guide );
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 30, 40, 400, 200 ];
	const frame = worldMapFrame( 0, clip, [ 0, 0 ], pose );
	const image = "/assets/npc/hunting-portraits/" + "a".repeat( 64 ) + ".png";
	const result = projectHuntingGuide( data, new Map( [ [ 1933, image ] ] ), frame, clip );
	assert.ok(
		result.controls.some( c =>
			c.helpText?.includes( "Mangyang - Lv. 1" ) && c.helpText?.includes( "Water Ghost - Lv. 10" )
		)
	);
	assert.ok( result.labels.some( r => r.value === "Lv 1-10" ) );
	assert.ok( result.paths.includes( image ) );
	assert.ok(
		result.controls.every( c =>
			c.draggable && c.rect[0] >= clip[0] && c.rect[1] >= clip[1] && c.rect[0] + c.rect[2] <= clip[0] + clip[2] &&
			c.rect[1] + c.rect[3] <= clip[1] + clip[3]
		)
	);
	const blocked = projectHuntingGuide( data, new Map(), frame, clip, [ clip ] );
	assert.equal( blocked.controls.length, 0 );
	const crowded = decodeHuntingGuide( {
		...guide,
		rows: [ {
			...guide.rows[0],
			points: Array.from(
				{ length: 200 },
				( _, i ) => ({ regionId: pose.regionId + (i % 8), x: (i % 12) * 150, z: Math.floor( i / 12 ) * 100 })
			)
		} ]
	} );
	assert.ok( projectHuntingGuide( crowded, new Map(), frame, clip ).controls.length <= 18 );
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const small = [ 30, 40, 256, 256 ];
	const little = projectHuntingGuide( crowded, new Map(), worldMapFrame( 0, small, [ 0, 0 ], pose ), small );
	assert.ok( little.controls.length <= 4 );
});

test("decluttering keeps every local species reachable, including a group displaced by landmarks", () => {
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 640, 384 ];
	const frame = worldMapFrame( 0, clip, [ 0, 0 ], pose );
	const rows = Array.from( { length: 12 }, ( _, i ) => ({
		refObjId: i + 1,
		name: `Species ${i + 1}`,
		nameKey: "",
		level: i + 1,
		points: [ { regionId: pose.regionId + (i < 6 ? -6 : 6), x: 960, z: 960 } ]
	}) );
	const data = decodeHuntingGuide( { ...guide, rows } );
	const open = projectHuntingGuide( data, new Map(), frame, clip );
	assert.equal( open.controls.length, 2 );
	// Block every placement around the left group. Its detail must move into
	// the neighbouring area rather than disappearing behind a marker limit.
	const result = projectHuntingGuide( data, new Map(), frame, clip, [ [ 0, 0, 230, 384 ] ] );
	assert.equal( result.controls.length, 1 );
	const detail = result.controls[0].helpText;
	assert.ok( detail );
	for ( const row of rows ) assert.ok( detail.includes( `${row.name} - Lv. ${row.level}` ) );
	assert.ok( result.labels.some( label => label.value === "Lv 1-12" ) );
	assert.ok( result.controls.every( control => control.rect[0] > 230 ) );
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
