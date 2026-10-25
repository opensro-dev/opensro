/*
===========================================================================

debug-position.test.mjs - the /Pos word parser (690C40 case 2) and the
/GetPos flip path (690C40 case 3)

The single argument word is comma-split: a swscanf "%x" region token,
then three CRT_atof coordinates. Missing or unreadable tokens read 0,
extra words are ignored, and no word at all runs nothing. The flip path
is pinned end to end at its two testable seams: the worker core routes
the debug-body-visual command to the entity owner, and the owner writes
the flipped byte unless the server already set that value there.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { debugPosition } = await import(
	"../../src/engine/foundation/ui/debug-commands.ts"
);
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createWorldCore } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/core.ts"
);

test("a full word parses as a hex region and three coordinates", () => {
	assert.deepEqual( debugPosition( [ "61A8,100,200,300" ] ), {
		regionId: 0x61a8,
		x: 100,
		y: 200,
		z: 300
	} );
	assert.deepEqual( debugPosition( [ "0x61a8,-12.5,1.5e2,0" ] ), {
		regionId: 0x61a8,
		x: -12.5,
		y: 150,
		z: 0
	} );
});

test("the region is the destination's u16 field", () => {
	assert.equal( debugPosition( [ "1FFFF,1,2,3" ] )?.regionId, 0xffff );
	assert.equal( debugPosition( [ "-1,1,2,3" ] )?.regionId, 0xffff );
	// swscanf %x stops at the first non-hex digit.
	assert.equal( debugPosition( [ "61A8zz,1,2,3" ] )?.regionId, 0x61a8 );
});

test("missing and unreadable tokens read as zero", () => {
	assert.deepEqual( debugPosition( [ "61A8,100" ] ), { regionId: 0x61a8, x: 100, y: 0, z: 0 } );
	assert.deepEqual( debugPosition( [ "61A8" ] ), { regionId: 0x61a8, x: 0, y: 0, z: 0 } );
	assert.deepEqual( debugPosition( [ "zz,abc,," ] ), { regionId: 0, x: 0, y: 0, z: 0 } );
	assert.deepEqual( debugPosition( [ "" ] ), { regionId: 0, x: 0, y: 0, z: 0 } );
});

test("no word runs nothing, and only the first word is read", () => {
	assert.equal( debugPosition( [] ), null );
	assert.deepEqual( debugPosition( [ "61A8,1,2,3", "FFFF,9,9,9" ] ), {
		regionId: 0x61a8,
		x: 1,
		y: 2,
		z: 3
	} );
});

/*
================
localOwner
================
*/
function localOwner() {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 3914, tidWord: 0x11c6, kind: "cos" } ],
		localPlayerEntry: {
			modelRef: 1933,
			startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
		}
	} );
	owner.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	drainEntities( owner );
	return owner;
}

/*
================
drainEntities
================
*/
function drainEntities( owner ) {
	const batch = owner.take();
	if ( batch ) owner.ack( batch.sequence );
}

/*
================
bodyVisual
================
*/
function bodyVisual( owner ) {
	return defined( owner.read( 7 ) ).appearanceState?.[2] ?? 0;
}

/*
================
bodyVisualRows

The appearance rows a batch wrote for the local player, in wire order.
Batch members without an entity carry no row.
================
*/
function bodyVisualRows( batch ) {
	/** @type {(readonly number[] | undefined)[]} */
	const rows = [];
	for ( const event of batch?.events ?? [] ) {
		if ( event.kind === "state" && "entity" in event && event.entity.gid === 7 ) {
			rows.push( event.entity.appearanceState );
		}
	}
	return rows;
}

test("the /GetPos flip writes the local body visual byte on and off", () => {
	const owner = localOwner();
	try {
		assert.equal( bodyVisual( owner ), 0 );
		owner.debugBodyVisual( 7 );
		assert.equal( bodyVisual( owner ), 1 );
		owner.debugBodyVisual( 7 );
		assert.equal( bodyVisual( owner ), 0 );
		// The flip byte is global (CEAD84): an unknown gid still consumes it,
		// so the next flip on the local player turns it off, not on.
		owner.debugBodyVisual( 999 );
		owner.debugBodyVisual( 7 );
		assert.equal( bodyVisual( owner ), 0, "the unknown gid did not consume its flip" );
	} finally {
		owner.dispose();
	}
});

test("a server-set body visual byte survives the first /GetPos flip", () => {
	const owner = localOwner();
	try {
		// 0x3122 channel 4 is the server's own write of the same byte.
		owner.receive( { opcode: 0x3122, payload: Buffer.from( [ 7, 0, 0, 0, 4, 1 ] ) }, 1 );
		assert.equal( bodyVisual( owner ), 1 );
		drainEntities( owner );
		owner.debugBodyVisual( 7 );
		const batch = owner.take();
		if ( batch ) owner.ack( batch.sequence );
		assert.equal( bodyVisualRows( batch ).length, 0, "the first flip rewrote the server byte" );
		assert.equal( bodyVisual( owner ), 1, "the server byte fell on the first flip" );
		owner.debugBodyVisual( 7 );
		assert.equal( bodyVisual( owner ), 0, "the second flip did not land" );
	} finally {
		owner.dispose();
	}
});

test("/hideme and /showme write the local player's shown byte", () => {
	const owner = localOwner();
	try {
		assert.equal( defined( owner.read( 7 ) ).debugHidden ?? false, false );
		owner.debugLocalVisible( 7, false );
		assert.equal( defined( owner.read( 7 ) ).debugHidden, true );
		owner.debugLocalVisible( 7, false );
		assert.equal( defined( owner.read( 7 ) ).debugHidden, true, "a repeated hide rewrote the record" );
		owner.debugLocalVisible( 7, true );
		assert.equal( defined( owner.read( 7 ) ).debugHidden, false );
		owner.debugLocalVisible( 999, false );
		assert.equal( owner.read( 999 ), undefined, "an unknown gid gained a record" );
	} finally {
		owner.dispose();
	}
});

test("the worker core routes the body-visual command to the entity owner", () => {
	const core = createWorldCore( () => {} );
	try {
		core.bootstrap( {
			protocolVersion: 2,
			nativeResult: 1,
			refObjSnapshot: [],
			refItemSnapshot: [],
			localPlayerEntry: {
				modelRef: 1933,
				startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
			}
		} );
		core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
		core.step( 0, false );
		const boot = core.take();
		if ( boot ) core.ack( boot.sequence );
		core.command( { kind: "debug-body-visual" }, 1 );
		core.step( 1, false );
		const batch = core.take();
		if ( batch ) core.ack( batch.sequence );
		const writes = bodyVisualRows( batch );
		assert.equal( writes.length, 1, "the command never reached the entity owner" );
		assert.equal( writes[0]?.[2], 1 );
		core.command( { kind: "debug-body-visual" }, 2 );
		core.step( 2, false );
		const second = core.take();
		if ( second ) core.ack( second.sequence );
		const off = bodyVisualRows( second );
		assert.equal( off.length, 1 );
		assert.equal( off[0]?.[2], 0 );
	} finally {
		core.dispose();
	}
});

test("the worker core routes the local-visible command to the entity owner", () => {
	const core = createWorldCore( () => {} );
	try {
		core.bootstrap( {
			protocolVersion: 2,
			nativeResult: 1,
			refObjSnapshot: [],
			refItemSnapshot: [],
			localPlayerEntry: {
				modelRef: 1933,
				startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
			}
		} );
		core.receive( { opcode: 0x32a6, payload: Buffer.from( [ 7, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
		core.step( 0, false );
		const boot = core.take();
		if ( boot ) core.ack( boot.sequence );
		core.command( { kind: "debug-local-visible", visible: false }, 1 );
		core.step( 1, false );
		const batch = core.take();
		if ( batch ) core.ack( batch.sequence );
		/** @type {(boolean | undefined)[]} */
		const writes = [];
		for ( const event of batch?.events ?? [] ) {
			if ( event.kind === "state" && "entity" in event && event.entity.gid === 7 ) {
				writes.push( event.entity.debugHidden );
			}
		}
		assert.deepEqual( writes, [ true ], "the command never reached the entity owner" );
	} finally {
		core.dispose();
	}
});
