/*
===========================================================================

fortress-structure-notice.test.mjs - construction and repair reply notices

The native reply names an event zone. Its structure name comes from the
character catalog, including when no corresponding actor is visible. The
real world composition must deliver the formatted line to chat and banner.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";

const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const { fortressBootstrap, fortressStructureNotice } = await import(
	"../../src/engine/foundation/gameplay/fortress.ts"
);
const { createHudMessages, noticeText } = await import( "../../src/engine/runtime/ui/hud/messages.ts" );

const FORTRESS = 1;
const ZONE = 109;
const STRUCTURE = 19565;
const CONSTRUCT = 0x0a;
const REPAIR = 0x0c;
const CONSTRUCTION_KEY = "UIIT_MSG_FORT_BATTLEAIDE_CONSTRUCTION_COMPLETE";
const REPAIR_KEY = "UIIT_MSG_FORT_BATTLEAIDE_REPAIR_COMPLETE";
const STRUCTURE_NAME = "Jangan barricade";

/*
================
u32
================
*/
function u32( value ) {
	const bytes = Buffer.alloc( 4 );
	bytes.writeUInt32LE( value );
	return [ ...bytes ];
}

/*
================
reply
================
*/
function reply( action, zone = ZONE, hp = 80000 ) {
	return {
		opcode: 0xb1e1,
		payload: Uint8Array.from( [
			action,
			1,
			...u32( FORTRESS ),
			...u32( zone ),
			...(action === REPAIR ? u32( hp ) : [])
		] )
	};
}

/*
================
roster
================
*/
function roster( ref = STRUCTURE ) {
	return {
		opcode: 0x3887,
		payload: Uint8Array.from( [
			0x0f,
			...u32( FORTRESS ),
			1,
			...u32( ZONE ),
			...u32( ref ),
			...u32( 80000 ),
			0,
			0,
			0
		] )
	};
}

/*
================
bootstrap
================
*/
function bootstrap( name = STRUCTURE_NAME, kind = "structure" ) {
	return {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ {
			refObjId: STRUCTURE,
			tidWord: 0x32c4,
			codename: "STRUCTURE_TEST",
			nameStrId: "SN_STRUCTURE_TEST",
			kind,
			name
		} ],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	};
}

/*
================
flush
================
*/
function flush( core ) {
	core.step( 0, false );
	const batch = core.take();
	if ( batch ) core.ack( batch.sequence );
	return batch?.events ?? [];
}

/*
================
gameplay
================
*/
function gameplay( events ) {
	const event = events.find( row => row.kind === "gameplay" );
	assert.ok( event?.kind === "gameplay", "the world publishes gameplay" );
	return event.state;
}

/*
================
world
================
*/
function world( t, name = STRUCTURE_NAME, kind = "structure" ) {
	const core = createWorldCore( () => {} );
	t.after( () => core.dispose() );
	core.bootstrap( bootstrap( name, kind ) );
	flush( core );
	core.receive( roster(), 0 );
	flush( core );
	return core;
}

/*
================
The two direct success arms reach both native display channels.
================
*/
test("construction and repair name an out-of-view structure in chat and banner", t => {
	const core = world( t );
	const copy = key =>
		({ [CONSTRUCTION_KEY]: "[%s] has been constructed.", [REPAIR_KEY]: "[%s] has been repaired." })[key] ?? key;
	const messages = createHudMessages( () => 0 );
	for (
		const [action, key, verb] of [ [ CONSTRUCT, CONSTRUCTION_KEY, "constructed" ], [
			REPAIR,
			REPAIR_KEY,
			"repaired"
		] ]
	) {
		core.receive( reply( action ), 1 );
		const events = flush( core );
		assert.ok( !events.some( event => event.kind === "spawn" ), "no visible actor is needed" );
		const notices = defined( gameplay( events ).notices );
		const notice = defined( notices.at( -1 ) );
		assert.equal( notice.key, key );
		assert.equal( notice.nativeType, 0 );
		assert.equal( notice.banner, true );
		assert.equal( notice.bannerOnly, undefined );
		assert.deepEqual( notice.arguments, [ STRUCTURE_NAME ] );
		const expected = `[${STRUCTURE_NAME}] has been ${verb}.`;
		assert.equal( noticeText( copy, notice, "banner" ), expected );
		assert.equal( messages.step( 1, [], 1, 0, notices, copy ).at( -1 )?.value, expected );
		const count = messages.step( 2, [], 1, 0, notices, copy ).length;
		assert.equal(
			messages.step( 3, [], 1, 0, notices, copy ).length,
			count,
			"a repeated snapshot does not replay"
		);
	}
});

/*
================
The helper does not turn another reply or a refusal into success.
================
*/
test("only the direct construction and repair success arms emit these lines", () => {
	const state = fortressBootstrap( {} );
	assert.equal( fortressStructureNotice( state, STRUCTURE_NAME ), null );
	for ( let action = 0; action <= 0x18; action++ ) {
		assert.equal(
			fortressStructureNotice( { ...state, service: { action, result: 2, error: 3 } }, STRUCTURE_NAME ),
			null
		);
		const notice = fortressStructureNotice( { ...state, service: { action, result: 1 } }, STRUCTURE_NAME );
		assert.equal( notice !== null, action === CONSTRUCT || action === REPAIR, `action ${action}` );
	}
});

/*
================
Zone identifiers must not be interpreted as character reference identifiers.
================
*/
test("missing zones, empty zones and non-structure references cannot name a success", t => {
	for ( const kind of [ "structure", "monster" ] ) {
		const core = world( t, STRUCTURE_NAME, kind );
		core.receive( reply( CONSTRUCT, STRUCTURE ), 1 );
		assert.equal( gameplay( flush( core ) ).notices?.length ?? 0, 0 );
		core.receive( roster( 0 ), 2 );
		flush( core );
		core.receive( reply( CONSTRUCT ), 3 );
		assert.equal( gameplay( flush( core ) ).notices?.length ?? 0, 0 );
		if ( kind === "monster" ) {
			core.receive( roster(), 4 );
			flush( core );
			core.receive( reply( REPAIR ), 5 );
			assert.equal( gameplay( flush( core ) ).notices?.length ?? 0, 0 );
		}
	}
});

/*
================
A later roster or unrelated service answer must not replay the old outcome.
================
*/
test("successes stay sequenced while refusals, roster refreshes and other replies do not replay them", t => {
	const core = world( t );
	core.receive( reply( REPAIR, ZONE, 0xffffffff ), 1 );
	const first = defined( gameplay( flush( core ) ).notices );
	assert.equal( first.length, 1 );
	core.receive( roster(), 2 );
	assert.deepEqual( gameplay( flush( core ) ).notices, first, "the roster does not add a notice" );
	core.receive( { opcode: 0xb1e1, payload: Uint8Array.of( 9, 1 ) }, 3 );
	assert.deepEqual( gameplay( flush( core ) ).notices, first );
	core.receive( { opcode: 0xb1e1, payload: Uint8Array.of( REPAIR, 2, 0x30 ) }, 4 );
	const refused = defined( gameplay( flush( core ) ).notices );
	assert.equal( refused.filter( notice => notice.key === REPAIR_KEY ).length, 1 );
	assert.equal( refused.at( -1 )?.key, "UIIT_MSG_FORT_BATTLEAIDE_REPAIR_FAIL" );
	core.receive( reply( CONSTRUCT ), 5 );
	assert.ok( defined( defined( gameplay( flush( core ) ).notices ).at( -1 ) ).sequence > first[0].sequence );
});

/*
================
Parse failures are atomic with respect to the published notice queue.
================
*/
test("truncated and trailing-byte successes publish no partial notice", t => {
	const core = world( t );
	for ( const action of [ CONSTRUCT, REPAIR ] ) {
		const frame = reply( action );
		for ( let end = 0; end < frame.payload.length; end++ ) {
			assert.throws( () => core.receive( { ...frame, payload: frame.payload.subarray( 0, end ) }, 1 ) );
			assert.equal( flush( core ).length, 0 );
		}
		assert.throws(
			() => core.receive( { ...frame, payload: Uint8Array.of( ...frame.payload, 0 ) }, 1 ),
			/Trailing/
		);
		assert.equal( flush( core ).length, 0 );
	}
});

/*
================
Re-entry replaces the name catalog and drops the prior zone roster.
================
*/
test("a new world entry cannot reuse the old zone or structure name", t => {
	const core = world( t );
	core.receive( reply( REPAIR ), 1 );
	const previous = defined( defined( gameplay( flush( core ) ).notices ).at( -1 ) );
	core.bootstrap( bootstrap( "Replacement barricade" ) );
	flush( core );
	core.receive( reply( REPAIR ), 2 );
	assert.equal( gameplay( flush( core ) ).notices?.length ?? 0, 0 );
	core.receive( roster(), 3 );
	flush( core );
	core.receive( reply( REPAIR ), 4 );
	const next = defined( defined( gameplay( flush( core ) ).notices ).at( -1 ) );
	assert.deepEqual( next.arguments, [ "Replacement barricade" ] );
	assert.ok( next.sequence > previous.sequence );
});
