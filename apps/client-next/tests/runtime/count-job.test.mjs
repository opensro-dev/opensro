/*
===========================================================================

count-job.test.mjs - a premium package's limited uses

Expectations follow 76F820/76F920/770820 (the three packets) and 6AD990
(the chat commands' own checks) and 6AFB40 (the package window's button);
they are worked from those, not read back.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	createCountJobs,
	commandOfType,
	countJobFraction,
	countJobUseRequest,
	premiumCommand,
	COUNT_JOB_ALIVE,
	COUNT_JOB_DEAD,
	COUNT_JOB_NONE,
	COUNT_JOB_PVP,
	COUNT_JOB_TRANSPORT,
	COUNT_JOB_USED_UP
} = await import( "../../src/engine/foundation/gameplay/count-job.ts" );

const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const PACKAGE = 10130, RESURRECTION = 4000, RETURN = 4001;
const facts = { alive: true, transportOut: false, pvpState: 0 };

/*
================
copy

The localized command words (stub phrases; the one-word command is a
prefix of the reverse one, as natively, so match order matters).
================
*/
const copy = symbol =>
	({
		UIIT_STT_PREMIUM_COMMAND_RETURN_HIGH_SPEED: "/Warp",
		UIIT_STT_PREMIUM_COMMAND_REVERSE_RETURN: "/Warp Reverse",
		"UIIT_STT_PREMIUM_COMMAND_RESURRECTION_100%": "/Revive"
	})[symbol] ?? symbol;

/*
================
owner

A count-job owner holding the Gold Time package's resurrection (one use)
and instant return (three uses).
================
*/
function owner() {
	const jobs = createCountJobs();
	jobs.reference( {
		refObjId: PACKAGE,
		typeFlags: 0x72ec,
		nativeFields: { itemParam1_29c: 2419200 },
		icon: "p.ddj",
		name: "Gold Time"
	} );
	jobs.reference( { refObjId: RESURRECTION, typeFlags: 0x36ec, name: "Resurrection" } );
	jobs.reference( { refObjId: RETURN, typeFlags: 0x09ec, name: "Instant return" } );
	jobs.receive( {
		opcode: 0x3021,
		payload: Uint8Array.of( ...u32( PACKAGE ), ...u32( 2419200 ), ...u32( RESURRECTION ), 1 )
	}, 0 );
	jobs.receive( {
		opcode: 0x3021,
		payload: Uint8Array.of( ...u32( PACKAGE ), ...u32( 2419200 ), ...u32( RETURN ), 3 )
	}, 0 );
	return jobs;
}

test("the commands are the whole localized line", () => {
	assert.equal( premiumCommand( "/Warp", copy ), "return" );
	assert.equal( premiumCommand( " /warp reverse ", copy ), "reverse-return" );
	assert.equal( premiumCommand( "/Revive", copy ), "resurrection" );
	assert.equal( premiumCommand( "/Warp home", copy ), null );
	assert.equal( premiumCommand( "Warp", copy ), null );
});

test("6AD990 checks in order and sends the package and item", () => {
	const jobs = owner();
	assert.deepEqual( jobs.admit( "resurrection", facts ), { code: COUNT_JOB_ALIVE } );
	assert.deepEqual( jobs.admit( "return", { ...facts, alive: false } ), { code: COUNT_JOB_DEAD } );
	assert.deepEqual( jobs.admit( "return", { ...facts, transportOut: true } ), { code: COUNT_JOB_TRANSPORT } );
	assert.deepEqual( jobs.admit( "return", { ...facts, pvpState: 2 } ), { code: COUNT_JOB_PVP } );
	assert.deepEqual( jobs.admit( "reverse-return", facts ), { code: COUNT_JOB_NONE } );
	const admitted = jobs.admit( "return", facts );
	assert.ok( "row" in admitted );
	assert.deepEqual( countJobUseRequest( admitted.row ), {
		opcode: 0x76fd,
		payload: Uint8Array.of( ...u32( PACKAGE ), ...u32( RETURN ) )
	} );
	assert.equal( countJobUseRequest( admitted.row, 3 ).payload[8], 3 );
	assert.deepEqual( createCountJobs().admit( "return", facts ), { code: COUNT_JOB_NONE } );
});

test("a spent use counts down and a used-up row refuses", () => {
	const jobs = owner();
	const spent = jobs.receive( {
		opcode: 0xb6fd,
		payload: Uint8Array.of( 1, ...u32( PACKAGE ), ...u32( RESURRECTION ) )
	}, 0 );
	assert.equal( spent?.kind, "spent" );
	assert.deepEqual( jobs.admit( "resurrection", { ...facts, alive: false } ), { code: COUNT_JOB_USED_UP } );
	assert.deepEqual( jobs.receive( { opcode: 0xb6fd, payload: Uint8Array.of( 2, 0xc6 ) }, 0 ), {
		kind: "refused",
		code: 0xc6
	} );
	jobs.receive( { opcode: 0x36fc, payload: Uint8Array.of( ...u32( PACKAGE ), ...u32( RESURRECTION ) ) }, 0 );
	assert.deepEqual( jobs.state().map( row => row.itemRefObjId ), [ RETURN ] );
});

test("the board bar counts the package's period down", () => {
	const [row] = owner().state();
	assert.equal( row.reference.periodSec, 2419200 );
	assert.equal( countJobFraction( row, 2419200, 0 ), 1 );
	assert.equal( countJobFraction( row, 2419200, 1209600 * 1000 ), 0.5 );
});

test("6AFB40: a package window button checks its own row", () => {
	const jobs = owner(), REVERSE = 4002;
	jobs.reference( { refObjId: REVERSE, typeFlags: 0x19ec, name: "Reverse return" } );
	jobs.receive( {
		opcode: 0x3021,
		payload: Uint8Array.of( ...u32( PACKAGE ), ...u32( 2419200 ), ...u32( REVERSE ), 0 )
	}, 0 );
	const admitted = jobs.admitRow( PACKAGE, RETURN, facts );
	assert.ok( admitted && "row" in admitted );
	assert.equal( admitted.command, "return" );
	assert.equal( admitted.row.itemRefObjId, RETURN );
	assert.deepEqual( jobs.admitRow( PACKAGE, RETURN, { ...facts, alive: false } ), { code: COUNT_JOB_DEAD } );
	assert.deepEqual( jobs.admitRow( PACKAGE, RETURN, { ...facts, transportOut: true } ), {
		code: COUNT_JOB_TRANSPORT
	} );
	assert.deepEqual( jobs.admitRow( PACKAGE, RETURN, { ...facts, pvpState: 2 } ), { code: COUNT_JOB_PVP } );
	assert.deepEqual( jobs.admitRow( PACKAGE, RESURRECTION, facts ), { code: COUNT_JOB_ALIVE } );
	const revived = jobs.admitRow( PACKAGE, RESURRECTION, { ...facts, alive: false } );
	assert.ok( revived && "row" in revived );
	assert.equal( revived.command, "resurrection" );
	// A used-up row is a disabled button: nothing is sent and nothing raised.
	assert.equal( jobs.admitRow( PACKAGE, REVERSE, facts ), null );
	// A row of another package, or one that ended, is gone.
	assert.equal( jobs.admitRow( PACKAGE + 1, RETURN, facts ), null );
	jobs.receive( { opcode: 0x36fc, payload: Uint8Array.of( ...u32( PACKAGE ), ...u32( RETURN ) ) }, 0 );
	assert.equal( jobs.admitRow( PACKAGE, RETURN, facts ), null );
});

test("the reverse return runs from its type word", () => {
	assert.equal( commandOfType( 0x19ec ), "reverse-return" );
	assert.equal( commandOfType( 0x09ec | 3 ), "return" );
	assert.equal( commandOfType( 0x36ec ), "resurrection" );
	assert.equal( commandOfType( 0x72ec ), null );
});
