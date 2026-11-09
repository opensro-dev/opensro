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
	countJobHelpText,
	countJobRemainingMs,
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

The English command words.
================
*/
const copy = symbol =>
	({
		UIIT_STT_PREMIUM_COMMAND_RETURN_HIGH_SPEED: "/Return",
		UIIT_STT_PREMIUM_COMMAND_REVERSE_RETURN: "/Reverse Return",
		"UIIT_STT_PREMIUM_COMMAND_RESURRECTION_100%": "/Resurrection"
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
	assert.equal( premiumCommand( "/Return", copy ), "return" );
	assert.equal( premiumCommand( " /reverse return ", copy ), "reverse-return" );
	assert.equal( premiumCommand( "/Resurrection", copy ), "resurrection" );
	assert.equal( premiumCommand( "/Return home", copy ), null );
	assert.equal( premiumCommand( "Return", copy ), null );
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

/*
================
helpCopy

The board help's words, resolved as the client's text lookup resolves
them.
================
*/
const helpCopy = symbol =>
	({
		PARAM_REMAINED_PERIOD: "Left service period",
		PARAM_DAY: "Day",
		PARAM_HOUR: "Hour",
		PARAM_MINUTE: "Minute",
		PARAM_SECOND: "Second",
		UIIT_STT_UNIT: "Unit"
	})[symbol] ?? symbol;

test("the board rows name every limited item once its reference lands", () => {
	assert.deepEqual( owner().state().map( row => row.itemName ), [ "Resurrection", "Instant return" ] );
});

test("the kind-5 help reads name, period and one line per limited row", () => {
	const rows = owner().state(), left = (27 * 86400 + 23 * 3600 + 59 * 60) * 1000;
	assert.equal(
		countJobHelpText( "Gold Time", rows, left, helpCopy ),
		"Gold Time\n \nLeft service period: 27Day 23Hour 59Minute\nResurrection: 1Unit\nInstant return: 3Unit"
	);
});

test("under a minute the period line counts seconds", () => {
	const rows = owner().state();
	assert.equal(
		countJobHelpText( "Gold Time", rows, 45000, helpCopy ).split( "\n" )[2],
		"Left service period: 45Second"
	);
	assert.equal(
		countJobHelpText( "Gold Time", rows, 59999, helpCopy ).split( "\n" )[2],
		"Left service period: 59Second"
	);
	// A whole minute is the shortest day/hour/minute line.
	assert.equal(
		countJobHelpText( "Gold Time", rows, 60000, helpCopy ).split( "\n" )[2],
		"Left service period: 0Day 0Hour 1Minute"
	);
});

test("the board slot's hover help is the composed kind-5 text", async () => {
	const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
	// One minute in, the four-week package has 27 days 23:59 left.
	const board = buffBoard(
		/** @type {any} */ ({ localGid: 1, countJobs: owner().state() }),
		60000,
		new Set(),
		undefined,
		true,
		helpCopy
	);
	const slot = board.find( row => row.id === "count-job:" + PACKAGE );
	assert.ok( slot );
	assert.equal(
		slot.helpText,
		"Gold Time\n \nLeft service period: 27Day 23Hour 59Minute\nResurrection: 1Unit\nInstant return: 3Unit"
	);
});

test("the remained period counts from the row's receipt", () => {
	const [row] = owner().state();
	assert.equal( countJobRemainingMs( row, 0 ), 2419200 * 1000 );
	assert.equal( countJobRemainingMs( row, 60000 ), 2419200 * 1000 - 60000 );
});

test("the reverse return runs from its type word", () => {
	assert.equal( commandOfType( 0x19ec ), "reverse-return" );
	assert.equal( commandOfType( 0x09ec | 3 ), "return" );
	assert.equal( commandOfType( 0x36ec ), "resurrection" );
	assert.equal( commandOfType( 0x72ec ), null );
});

/*
================
The Energy of Life buff (port-only, cap-110 Blue Berserk)
================
*/
test("the Energy of Life buff's package window button sends its use", async () => {
	const { rowAdmission } = await import( "../../src/engine/foundation/gameplay/count-job.ts" );
	const HWAN_TYPE = (3 << 2) | (3 << 5) | (1 << 7) | (8 << 11);
	assert.equal( commandOfType( HWAN_TYPE ), "hwan-charge" );
	const row = { packageRefObjId: PACKAGE, itemRefObjId: 23937, uses: 1, remainingSec: 3600, receivedAtMs: 0 };
	assert.deepEqual( rowAdmission( row, HWAN_TYPE, facts ), { row, command: "hwan-charge" } );
	// A spent row is a disabled button; a dead player is refused with the client's own notice.
	assert.equal( rowAdmission( { ...row, uses: 0 }, HWAN_TYPE, facts ), null );
	assert.deepEqual( rowAdmission( row, HWAN_TYPE, { ...facts, alive: false } ), { code: COUNT_JOB_DEAD } );
	// Another item of the same type family keeps its own meaning: the old types still resolve.
	assert.equal( commandOfType( (3 << 2) | (3 << 5) | (3 << 7) | (1 << 11) ), "return" );
});

test("gameplay sends a count-job use to the server, from the window button and the chat command", async () => {
	const { createGameplay } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
	);
	const sent = [], g = createGameplay( frame => sent.push( frame ) );
	const local = {
		gid: 1,
		countryByte9c: 1,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	g.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [],
		equipItems: [],
		character: { hp: 10, mp: 10, maxHp: 10, maxMp: 10 }
	} );
	g.worldReady();
	g.seed( local );
	g.references( [ { refObjId: 50134, typeFlags: 0x40ec, name: "Energy" }, {
		refObjId: 777,
		typeFlags: 0x72ec,
		name: "Pkg"
	} ] );
	g.receive( { opcode: 0x3021, payload: Uint8Array.of( ...u32( 777 ), ...u32( 3600 ), ...u32( 50134 ), 1 ) }, 0 );
	g.command( { kind: "count-job-use", packageRefObjId: 777, itemRefObjId: 50134 }, 10, undefined, local );
	assert.deepEqual( sent.filter( f => f.opcode === 0x76fd ).map( f => [ ...f.payload ] ), [
		[ ...u32( 777 ), ...u32( 50134 ) ]
	] );
});
