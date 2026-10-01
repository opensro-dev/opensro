/*
===========================================================================

beta-features.test.mjs - beta map roster, window warm and screen size

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createBetaPlayerMap, OP_BETA_PLAYER_MAP } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/beta-map/beta-map.ts"
);
const { createWindowWarm, skillWindowIcons } = await import( "../../src/engine/runtime/ui/warm/window-warm.ts" );
const { videoOptions, defaultVideoOptions, displaySizes } = await import(
	"../../src/engine/foundation/rendering/video-options.ts"
);

/*
================
roster
================
*/
function roster( rows ) {
	const out = [ rows.length & 255, rows.length >> 8 ];
	for ( const row of rows ) {
		const b = Buffer.alloc( 15 );
		b.writeUInt32LE( row.gid, 0 );
		b.writeUInt16LE( row.regionId, 4 );
		b.writeFloatLE( row.x, 6 );
		b.writeFloatLE( row.z, 10 );
		const name = Buffer.from( row.name );
		b[14] = name.length;
		out.push( ...b, ...name );
	}
	return Uint8Array.from( out );
}

test("the beta roster decodes every row and rejects malformed bodies", () => {
	const map = createBetaPlayerMap();
	assert.equal( map.receive( { opcode: 0x3122, payload: new Uint8Array( 6 ) } ), false );
	const rows = [ { gid: 7, regionId: 0x694f, x: 1040, z: 160.5, name: "Ann" }, {
		gid: 8,
		regionId: 0x61a8,
		x: 1,
		z: 2,
		name: ""
	} ];
	assert.equal( map.receive( { opcode: OP_BETA_PLAYER_MAP, payload: roster( rows ) } ), true );
	assert.deepEqual( map.players(), rows );
	const truncated = roster( rows ).subarray( 0, 20 );
	assert.throws( () => map.receive( { opcode: OP_BETA_PLAYER_MAP, payload: truncated } ), /Truncated|Invalid/ );
	assert.deepEqual( map.players(), rows, "a malformed frame never replaces the last roster" );
	map.clear();
	assert.deepEqual( map.players(), [] );
});

test("window warm builds each window once and keeps its demand", () => {
	const warm = createWindowWarm( [ "Inventory", "Skills" ] );
	assert.equal( warm.begin( false ), null, "never while a window is open" );
	assert.equal( warm.begin( true ), "Inventory" );
	assert.throws( () => warm.begin( true ), /already open/ );
	warm.end( [ "/a.png", "/b.png" ] );
	assert.equal( warm.begin( true ), "Skills" );
	warm.add( [ "/c.png" ] );
	warm.end( [ "/a.png" ] );
	assert.equal( warm.begin( true ), null, "every window is warm" );
	assert.deepEqual( [ ...warm.paths() ].sort(), [ "/a.png", "/b.png", "/c.png" ] );
	warm.reset();
	assert.equal( warm.begin( true ), "Inventory", "a new session warms again" );
});

test("the skill window warm covers every owned mastery, not only the open tab", () => {
	const mastery = ( id, icon ) => ({ id, icon, name: "", count: 0, tab: 0, tabName: "" }),
		group = ( id, icon ) => ({ mastery: id, row: 0, name: "", icon }),
		skill = ( id, icon ) => ({
			id: 0,
			group: 0,
			level: 1,
			mastery: id,
			row: 0,
			column: 0,
			icon,
			name: "",
			study: ""
		});
	const catalog = {
		slots: {},
		masteries: [ mastery( 1, "m1" ), mastery( 2, "m2" ), mastery( 3, "m3" ) ],
		groups: [ group( 1, "g1" ), group( 2, "g2" ) ],
		skills: [ skill( 1, "s1" ), skill( 2, "s2" ), skill( 3, "s3" ) ]
	};
	const { icons, groups } = skillWindowIcons( catalog, [ 1, 2 ] );
	assert.deepEqual( icons.sort(), [ "m1", "m2", "s1", "s2" ] );
	assert.deepEqual( groups.sort(), [ "g1", "g2" ] );
});

test("screen size accepts only the offered modes, round-trips and drops the retired height", () => {
	const base = defaultVideoOptions();
	for ( const size of displaySizes().slice( 1 ) ) {
		assert.deepEqual( videoOptions( { ...base, displaySize: size } ).displaySize, size );
	}
	assert.throws( () => videoOptions( { ...base, displaySize: [ 1234, 567 ] } ), /display size/ );
	// The stretched height-only setting is dropped instead of failing a load.
	assert.equal( videoOptions( { ...base, displayHeight: 1080 } ).displaySize, undefined );
});
