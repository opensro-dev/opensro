/*
===========================================================================

processTreeBound.test.mjs - a probe browser's tree cannot climb above its bound

Chrome raises its own GPU process above normal, which no inherited class
prevents. These pin the bound's decision apart from the live machine and,
on Windows, prove a bounded child that raises itself stays bounded.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { boundChildren, browserImage, childTreeBound } from "../../lib/processPriority.mjs";

// Win32 BELOW_NORMAL_PRIORITY_CLASS, as the job reports its limit.
const BELOW_NORMAL_PRIORITY_CLASS = 0x4000;

/*
================
fakePriority

A node:os stand-in whose getPriority answers value, or throws when value
is null.
================
*/
function fakePriority( value ) {
	return {
		constants: os.constants,
		getPriority: () => {
			if ( value === null ) throw Error( "no access" );
			return value;
		}
	};
}

test("the bound never lifts the caller's class and caps the CPU at half by default", () => {
	const { PRIORITY_LOW, PRIORITY_BELOW_NORMAL, PRIORITY_NORMAL, PRIORITY_HIGH } = os.constants.priority;
	assert.deepEqual( childTreeBound( {}, fakePriority( PRIORITY_LOW ) ), { priorityClass: "Idle", cpuPercent: 50 } );
	assert.deepEqual( childTreeBound( {}, fakePriority( PRIORITY_BELOW_NORMAL ) ), {
		priorityClass: "BelowNormal",
		cpuPercent: 50
	} );
	assert.deepEqual( childTreeBound( {}, fakePriority( PRIORITY_NORMAL ) ), {
		priorityClass: "BelowNormal",
		cpuPercent: 50
	} );
	assert.deepEqual( childTreeBound( {}, fakePriority( PRIORITY_HIGH ) ), {
		priorityClass: "BelowNormal",
		cpuPercent: 50
	} );
	assert.deepEqual( childTreeBound( {}, fakePriority( null ) ), { priorityClass: "BelowNormal", cpuPercent: 50 } );
});

test("SRO_PROBE_CPU_PERCENT names the cap, and only a whole 1 to 100 counts", () => {
	const normal = fakePriority( os.constants.priority.PRIORITY_NORMAL );
	assert.equal( childTreeBound( { SRO_PROBE_CPU_PERCENT: "25" }, normal ).cpuPercent, 25 );
	assert.equal( childTreeBound( { SRO_PROBE_CPU_PERCENT: " 100 " }, normal ).cpuPercent, 100 );
	for ( const bad of [ "0", "101", "12.5", "-3", "half", "" ] ) {
		assert.equal( childTreeBound( { SRO_PROBE_CPU_PERCENT: bad }, normal ).cpuPercent, 50, bad );
	}
});

test("SRO_CHECK_PRIORITY=normal lifts the priority limit but keeps the CPU cap", () => {
	const bound = childTreeBound( { SRO_CHECK_PRIORITY: "Normal", SRO_PROBE_CPU_PERCENT: "40" }, fakePriority( 0 ) );
	assert.deepEqual( bound, { priorityClass: "None", cpuPercent: 40 } );
});

test("the bounded image is the named executable's, or Chrome's", () => {
	assert.equal( browserImage( undefined ), "chrome.exe" );
	assert.equal(
		browserImage( path.join( "C:", "tools", "chrome-headless-shell.exe" ) ),
		"chrome-headless-shell.exe"
	);
});

test("off Windows nothing is spawned: inheritance already holds there", async () => {
	assert.equal(
		await boundChildren( "chrome.exe", { priorityClass: "BelowNormal", cpuPercent: 50 }, "linux" ),
		null
	);
});

/*
================
raisedChild

Starts a node child that, once told, raises itself to above normal and
reports the priority it then holds. bound runs between start and raise.
================
*/
async function raisedChild( bound ) {
	const child = spawn( process.execPath, [
		"-e",
		[
			"const os = require( 'node:os' );",
			"process.stdout.write( 'ready\\n' );",
			"process.stdin.once( 'data', () => {",
			"	try { os.setPriority( os.constants.priority.PRIORITY_ABOVE_NORMAL ); } catch {}",
			"	process.stdout.write( String( os.getPriority() ) + '\\n' );",
			"	process.exit( 0 );",
			"} );"
		].join( "\n" )
	], { stdio: [ "pipe", "pipe", "inherit" ] } );
	const lines = [];
	let wake = () => {};
	child.stdout.setEncoding( "utf8" );
	child.stdout.on( "data", chunk => {
		lines.push( ...chunk.split( "\n" ).filter( line => line.length > 0 ) );
		wake();
	} );
	const next = async () => {
		while ( lines.length === 0 ) await new Promise( resolve => wake = resolve );
		return lines.shift();
	};
	assert.equal( await next(), "ready" );
	const report = await bound();
	child.stdin.write( "go\n" );
	return { report, raised: Number( await next() ) };
}

test( "a bounded child that raises itself stays at its bound", {
	skip: process.platform !== "win32" && "Windows job objects"
}, async () => {
	const { PRIORITY_ABOVE_NORMAL, PRIORITY_BELOW_NORMAL } = os.constants.priority;
	// The control: unbounded, the raise takes, so the bound is what stops it.
	const free = await raisedChild( async () => null );
	assert.equal( free.raised, PRIORITY_ABOVE_NORMAL );
	const held = await raisedChild( () =>
		boundChildren( "node.exe", { priorityClass: "BelowNormal", cpuPercent: 50 } )
	);
	assert.ok( held.report && held.report.joined >= 1, JSON.stringify( held.report ) );
	assert.equal( held.raised, PRIORITY_BELOW_NORMAL );
	// The job reports the limits it holds, read back from the system.
	assert.equal( held.report.priorityClass, BELOW_NORMAL_PRIORITY_CLASS );
	assert.equal( held.report.cpuPercent, 50 );
} );
