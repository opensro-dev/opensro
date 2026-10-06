/*
===========================================================================

travel.test.mjs - tests for travel.ts, mission-loading.ts, core.ts,
presentation.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { travelMode, resetTravelRegion, gateRequest } = await import( "../../src/engine/foundation/gameplay/travel.ts" );
const { travelLoadingQuads } = await import( "../../src/engine/foundation/ui/mission-loading.ts" );
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );
const { decodePortalCatalog, portalMenu, portalNotice } = await import(
	"../../src/engine/foundation/gameplay/portal.ts"
);
const { interactionApproach, interactionApproachTransition } = await import(
	"../../src/engine/foundation/gameplay/interaction-approach.ts"
);
test("primary portal menu resolves selected reference and authored destination IDs", () => {
	const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/teleportData.json", "utf8" ) ),
		catalog = decodePortalCatalog( raw );
	assert.equal( catalog.links.length, 85 );
	const text = {
		"UIIT_CTL_TELEPORT_RESULT": "Teleport to [%s]area. [%d]gold",
		"UIIT_CTL_TELEPORT_FREE_RESULT": "Teleport to [%s]area.",
		"SN_ZONE_21004": "Western ferry"
	};
	const menu = portalMenu( catalog, 2011, key => text[key] ?? key );
	assert.ok(
		menu.some( row => row.id === "npc-portal:9" && row.label === "Teleport to [Western ferry]area. [500]gold" )
	);
	assert.deepEqual( portalMenu( catalog, 200001, key => key ), [], "runtime GID cannot stand in for RefObjID" );
	assert.throws(
		() => decodePortalCatalog( { ...raw, linkRows: [ ...raw.linkRows, raw.linkRows[0] ] } ),
		/duplicate/
	);
	assert.throws(
		() => decodePortalCatalog( { ...raw, linkRows: [ { sourceId: 1, destinationId: 0xffffff, fee: 0 } ] } ),
		/Unresolved/
	);
});
test("portal refusal uses native quest/transport notices and strict reply framing", () => {
	assert.equal(
		defined( portalNotice( 0xb495, Uint8Array.of( 2, 0x17 ) ) ).key,
		"UIIT_MSG_INTERACTION_FAIL__REQUESTED_JOB_BLOCKED_BY_QUEST"
	);
	assert.equal(
		defined( portalNotice( 0xb495, Uint8Array.of( 2, 0x10 ) ) ).key,
		"UIIT_MSG_INTERACTION_FAIL_CANT_USE_TELEPORT_WITH_TRADECART"
	);
	assert.equal( defined( portalNotice( 0xb495, Uint8Array.of( 2, 4 ) ) ).banner, undefined );
	for ( const code of [ 4, 6, 7, 11, 16, 17, 18, 19, 20, 21, 22, 23, 24, 26, 27, 28, 29, 30, 32, 33 ] ) {
		assert.equal( defined( portalNotice( 0xb495, Uint8Array.of( 2, code ) ) ).nativeType, 0 );
	}
	assert.equal( portalNotice( 0xb495, Uint8Array.of( 1 ) ), null );
	assert.equal( portalNotice( 0xb495, Uint8Array.of( 2, 2 ) ), null );
	for ( const p of [ [], [ 1, 0 ], [ 2 ], [ 2, 4, 0 ] ] ) {
		assert.throws( () => portalNotice( 0xb495, Uint8Array.from( p ) ) );
	}
});
const bootstrap = {
	protocolVersion: 2,
	nativeResult: 1,
	refObjSnapshot: [],
	localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x694f, x: 10, y: 20, z: 30, angle: 0 } }
};
/*
================
state
================
*/
const state = ( gid, channel, value ) => {
	const p = Buffer.alloc( 6 );
	p.writeUInt32LE( gid );
	p[4] = channel;
	p[5] = value;
	return { opcode: 0x3122, payload: p };
};
/*
================
item
================
*/
const item = ( word, result = 1 ) => {
	const p = Buffer.alloc( result === 1 ? 6 : 2 );
	p[0] = result;
	if ( result === 1 ) p.writeUInt16LE( word, 4 );
	return { opcode: 0xb5bd, payload: p };
};
/*
================
drain
================
*/
function drain( owner ) {
	const batch = owner.take();
	if ( batch ) owner.ack( batch.sequence );
	return batch;
}
test("only local death selects rebirth; revival, movement and remote deaths do not", () => {
	assert.equal( travelMode( state( 7, 0, 2 ), 7 ), 1 );
	for ( const row of [ state( 8, 0, 2 ), state( 7, 0, 1 ), state( 7, 1, 2 ), state( 7, 11, 1 ) ] ) {
		assert.equal( travelMode( row, 7 ), null );
	}
	assert.throws( () => travelMode( { opcode: 0x3122, payload: Buffer.alloc( 5 ) }, 7 ) );
});
test("accepted return/reverse scroll and appearance-change replies preserve native type guards", () => {
	for ( const word of [ 0x09ec, 0x19ec ] ) assert.equal( travelMode( item( word ), 7 ), 2 );
	assert.equal( travelMode( item( 0x4eec ), 7 ), 6 );
	assert.equal( travelMode( item( 0x4eec ), 7, 1000 ), null );
	for ( const word of [ 0x09ee, 0x19cc, 0x11ec, 0x4e6c ] ) assert.equal( travelMode( item( word ), 7 ), null );
	assert.equal( travelMode( item( 0x09ec, 2 ), 7 ), null );
});
test("both reset opcodes require exactly one u16 region; unrelated packets are not resets", () => {
	for ( const opcode of [ 0x3369, 0x366a ] ) {
		assert.equal( resetTravelRegion( { opcode, payload: Uint8Array.of( 0x4f, 0x69 ) } ), 0x694f );
		for ( const length of [ 0, 1, 3 ] ) {
			assert.throws( () => resetTravelRegion( { opcode, payload: new Uint8Array( length ) } ) );
		}
	}
	assert.equal( resetTravelRegion( { opcode: 0x3122, payload: new Uint8Array() } ), null );
});
test("gate request branches encode native target widths instead of guessing a common payload", () => {
	assert.deepEqual( [ ...gateRequest( { kind: "travel-gate", gid: 7, type: 2, target: 0x12345678 } ).payload ], [
		7,
		0,
		0,
		0,
		2,
		0x78,
		0x56,
		0x34,
		0x12
	] );
	for ( const [kind, type] of [ [ "travel-gate", 5 ], [ "travel-instance", 3 ] ] ) {
		assert.deepEqual( [ ...gateRequest( { kind, gid: 7, type, target: 9 } ).payload ], [ 7, 0, 0, 0, type, 9 ] );
	}
	assert.throws( () => gateRequest( { kind: "travel-instance", gid: 7, target: 256 } ) );
});
test("same-region reset publishes its mode after reset, survives rebootstrap, and never inherits remote death", () => {
	for ( const opcode of [ 0x3369, 0x366a ] ) {
		const core = createWorldCore( () => {} ), presentation = createPresentation();
		core.bootstrap( bootstrap );
		presentation.apply( drain( core ) );
		const latch = Buffer.alloc( 8 );
		latch.writeUInt32LE( 7 );
		core.receive( { opcode: 0x32a6, payload: latch }, 0 );
		core.step( 0 );
		presentation.apply( drain( core ) );
		core.receive( state( 8, 0, 2 ), 0 );
		core.receive( state( 7, 0, 2 ), 0 );
		drain( core );
		core.receive( { opcode, payload: Uint8Array.of( 0x4f, 0x69 ) }, 0 );
		const batch = drain( core );
		assert.deepEqual( batch.events.filter( e => e.kind === "travel" ), [ {
			kind: "travel",
			travel: { mode: 1, region: 0x694f, revision: 1 }
		} ] );
		// Use a fresh projection sequence because native state packets were drained above.
		const p = createPresentation();
		p.apply( { sequence: 1, events: batch.events } );
		assert.deepEqual( p.travel(), { mode: 1, region: 0x694f, revision: 1 } );
		assert.equal( p.gameplay(), null );
		core.bootstrap( bootstrap );
		assert.equal( drain( core ).events.at( -1 ).travel.mode, 1 );
		core.travelReady();
		core.clear();
		core.bootstrap( bootstrap );
		assert.equal( drain( core ).events.some( e => e.kind === "travel" ), false );
		core.dispose();
	}
});
test("loading mode dispatch includes rebirth, destinations, thief, legacy customization and mode 6 without invented artwork", () => {
	for (
		const [mode, name] of [ [ 0, "europe_2" ], [ 1, "rebirth" ], [ 2, "constantinople" ], [ 3, "constantinople" ], [
			4,
			"thief2"
		], [ 5, "charactercustom" ] ]
	) {
		assert.ok(
			travelLoadingQuads( 1024, 768, { mode, region: 0x694f }, 2, .5 )[1].texture.endsWith(
				"loading_" + name + ".png"
			)
		);
	}
	const q = travelLoadingQuads( 1024, 768, { mode: 6, region: 0x694f }, 1, .5 );
	assert.equal( q.length, 4 );
	assert.ok( q[1].texture.endsWith( "loading_form.png" ) );
});

test("inventory admission precedes mode changes and nonzero appearance cooldown suppresses mode 6", () => {
	for (
		const [word, cooldown, mode] of [ [ 0x09ec, 0, 2 ], [ 0x19ec, 0, 2 ], [ 0x4eec, 0, 6 ], [ 0x4eec, 1000, 0 ] ]
	) {
		const body = Buffer.alloc( 6 );
		body.writeUInt32LE( 3829 );
		body.writeUInt16LE( 1, 4 );
		const core = createWorldCore( () => {} );
		core.bootstrap( {
			...bootstrap,
			refItemSnapshot: [ {
				refObjId: 3829,
				typeFlags: word,
				nativeFields: { useCooldownDuration528: cooldown, itemParam1_29c: 30 }
			} ],
			equipItems: [ { slot: 13, refObjId: 3829, body: [ ...body ] } ]
		} );
		drain( core );
		const frame = item( word );
		frame.payload[1] = 13;
		core.receive( frame, 0 );
		core.receive( { opcode: 0x366a, payload: Uint8Array.of( 0x4f, 0x69 ) }, 0 );
		assert.equal( drain( core ).events.find( e => e.kind === "travel" ).travel.mode, mode );
		core.dispose();
	}
	const core = createWorldCore( () => {} );
	core.bootstrap( bootstrap );
	drain( core );
	assert.throws( () => core.receive( item( 0x09ec ), 0 ), /Stale item/ );
	core.receive( { opcode: 0x366a, payload: Uint8Array.of( 0x4f, 0x69 ) }, 0 );
	assert.equal( drain( core ).events.find( e => e.kind === "travel" ).travel.mode, 0 );
	core.dispose();
});

test("outbound return-scroll activation selects mode before a later rejection or reset", () => {
	const body = Buffer.alloc( 6 );
	body.writeUInt32LE( 3829 );
	body.writeUInt16LE( 1, 4 );
	const sent = [], core = createWorldCore( frame => sent.push( frame ) );
	core.bootstrap( {
		...bootstrap,
		refItemSnapshot: [ { refObjId: 3829, typeFlags: 0x09ec } ],
		equipItems: [ { slot: 13, refObjId: 3829, body: [ ...body ] } ]
	} );
	drain( core );
	const latch = Buffer.alloc( 8 );
	latch.writeUInt32LE( 7 );
	core.receive( { opcode: 0x32a6, payload: latch }, 0 );
	drain( core );
	core.command( { kind: "item-use", slot: 13 }, 0 );
	assert.equal( sent[0].opcode, 0x75bd );
	core.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 1 ) }, 0 );
	core.receive( { opcode: 0x366a, payload: Uint8Array.of( 0x4f, 0x69 ) }, 0 );
	assert.equal( drain( core ).events.find( e => e.kind === "travel" ).travel.mode, 2 );
	core.dispose();
});

test("loading completion restores destination mode after initial entry, rebirth and appearance change", () => {
	for ( const mode of [ 0, 1, 6 ] ) {
		const body = Buffer.alloc( 6 );
		body.writeUInt32LE( 3829 );
		body.writeUInt16LE( 1, 4 );
		const core = createWorldCore( () => {} );
		core.bootstrap( {
			...bootstrap,
			refItemSnapshot: [ { refObjId: 3829, typeFlags: 0x4eec } ],
			equipItems: [ { slot: 13, refObjId: 3829, body: [ ...body ] } ]
		} );
		drain( core );
		const latch = Buffer.alloc( 8 );
		latch.writeUInt32LE( 7 );
		core.receive( { opcode: 0x32a6, payload: latch }, 0 );
		drain( core );
		if ( mode === 1 ) core.receive( state( 7, 0, 2 ), 0 );
		if ( mode === 6 ) {
			const frame = item( 0x4eec );
			frame.payload[1] = 13;
			core.receive( frame, 0 );
		}
		const reset = { opcode: 0x366a, payload: Uint8Array.of( 0x4f, 0x69 ) };
		if ( mode !== 0 ) {
			core.receive( reset, 0 );
			assert.equal( drain( core ).events.find( e => e.kind === "travel" ).travel.mode, mode );
			core.bootstrap( bootstrap );
			assert.equal( drain( core ).events.at( -1 ).travel.mode, mode );
		}
		core.travelReady();
		assert.equal( core.readyRevision(), 0 );
		core.receive( reset, 0 );
		const travel = drain( core ).events.find( e => e.kind === "travel" ).travel;
		assert.equal( travel.mode, 2 );
		assert.ok( travelLoadingQuads( 1024, 768, travel, 1, .5 )[1].texture.endsWith( "loading_constantinople.png" ) );
		core.dispose();
	}
});

const { createTargeting } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/targeting/targeting.ts"
);
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
test("city gates decode native 24-byte structures, retain authored bounds, and retire by identity", () => {
	const e = createEntities(),
		gate = {
			refObjId: 2094,
			kind: "teleport",
			name: "Jangan",
			teleport: { radius: 10, height: 25, fortressId: 1 }
		};
	e.bootstrap( { ...bootstrap, refObjSnapshot: [ gate ] } );
	drain( e );
	const row = Buffer.alloc( 24 );
	row.writeUInt32LE( 2094 );
	row.writeUInt32LE( 252094, 4 );
	row.writeUInt16LE( 25000, 8 );
	row.writeFloatLE( 1254, 10 );
	row.writeFloatLE( -6, 14 );
	row.writeFloatLE( 1374, 18 );
	e.receive( { opcode: 0x30d7, payload: row }, 100 );
	const entity = drain( e ).events.find( e => e.kind === "spawn" ).entity;
	assert.equal( entity.kind, "teleport" );
	assert.deepEqual( entity.teleport, gate.teleport );
	assert.equal( entity.z, 1374 );
	e.receive( { opcode: 0x36ab, payload: row.subarray( 4, 8 ) }, 101 );
	assert.ok( drain( e ).events.some( e => e.kind === "despawn" && e.gid === 252094 ) );
	e.dispose();
});
test("gate selection consumes capabilities without NPC mask and reads tax only for fortress-bound references", () => {
	for ( const fortress of [ false, true ] ) {
		const t = createTargeting( () => {} );
		t.select( 252094, 0, "teleport", { fortress } );
		const p = Buffer.alloc( fortress ? 11 : 9 );
		p[0] = 1;
		p.writeUInt32LE( 252094, 1 );
		p.writeUInt32LE( 128, 5 );
		if ( fortress ) p.writeInt16LE( -10, 9 );
		assert.equal( t.receive( 0xb45a, p ), true );
		assert.equal( t.state().targetCapabilities, 128 );
		assert.equal( t.state().targetTaxRate, fortress ? -10 : 0 );
		t.clear();
		assert.equal( t.state().targetTaxRate, 0 );
	}
	const t = createTargeting( () => {} );
	t.select( 1, 0, "teleport", { fortress: true } );
	assert.throws( () => t.receive( 0xb45a, Buffer.from( [ 1, 1, 0, 0, 0, 128, 0, 0, 0 ] ) ), /teleport grant/ );
});
test("gate approach uses native 800 range and 640 stopping distance across region boundaries", () => {
	const gate = { kind: "teleport", regionId: 25000, x: 1254, y: -6, z: 1374 };
	const pose = { regionId: 25256, x: 960, y: 20, z: 458, angle: 0 };
	const next = interactionApproach( pose, gate );
	assert.ok( next );
	assert.ok(
		Math.abs(
			Math.hypot( next.x - gate.x, next.z - gate.z + (next.regionId - gate.regionId) / 256 * 1920 ) - 640
		) < 1e-8
	);
	assert.equal( interactionApproach( next, gate ), null );
	assert.equal( interactionApproach( { ...gate, angle: 0, x: gate.x + 800 }, gate ), null );
});
test("an NPC is selected inside 240 units and approached to 240 from farther away", () => {
	const npc = { kind: "npc", regionId: 25000, x: 1000, y: 0, z: 1000 };
	assert.equal( interactionApproach( { regionId: 25000, x: 1240, y: 0, z: 1000, angle: 0 }, npc ), null );
	const next = interactionApproach( { regionId: 25000, x: 1600, y: 0, z: 1000, angle: 0 }, npc );
	assert.ok( next );
	assert.ok( Math.abs( Math.hypot( next.x - npc.x, next.z - npc.z ) - 240 ) < 1e-8 );
	// Monsters and players are selected wherever they stand.
	assert.equal(
		interactionApproach( { regionId: 25000, x: 1900, y: 0, z: 1000, angle: 0 }, { ...npc, kind: "monster" } ),
		null
	);
});
test("native I64 fee strings resolve the full integer format and shared tax calculation", () => {
	const catalog = decodePortalCatalog(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/teleportData.json", "utf8" ) )
	);
	const rows = portalMenu( catalog, 2011, key => key === "UIIT_CTL_TELEPORT_RESULT" ? "%s %I64d" : key, 10 );
	assert.ok( rows.some( row => row.label.endsWith( " 550" ) ) );
	assert.ok( rows.every( row => !row.label.includes( "%" ) ) );
});

test("an interaction approach retires on cancellation, arrival or matching despawn, never another entity", () => {
	const idle = { phase: "idle" }, target = { gid: 252094 };
	const moving = interactionApproachTransition( idle, { kind: "begin", target } );
	assert.equal( interactionApproachTransition( moving, { kind: "despawn", gid: 9 } ), moving );
	for ( const event of [ { kind: "cancel" }, { kind: "arrived" }, { kind: "despawn", gid: 252094 } ] ) {
		assert.deepEqual( interactionApproachTransition( moving, event ), idle );
	}
});
