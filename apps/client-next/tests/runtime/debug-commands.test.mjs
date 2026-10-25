/*
===========================================================================

debug-commands.test.mjs - config\command.txt in the GM console

The published table parses as 68D9C0 reads it, a console line is looked up
as 68DD10 does, and the console family (690C40 cases 0, 1 and 4) runs in
the production HUD while every other line still goes to the server. The
weather rows (ids 99/105-107) go to the worker as a debug-weather command
and the time rows (ids 100-109) drive the local clock (debug-clock.ts).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const commands = await import( "../../src/engine/foundation/ui/debug-commands.ts" );

/*
================
publishedTable
================
*/
function publishedTable() {
	const bytes = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/config/command.txt" );
	return commands.decodeCommandTable( bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength ) );
}

test("the published table keys each row by its pattern's first word", () => {
	const table = publishedTable();
	assert.equal( table.get( "/PlayerCount" ), 0 );
	assert.equal( table.get( "/Debug" ), 1 );
	assert.equal( table.get( "/Pos" ), 3 );
	assert.equal( table.get( "/setfov" ), 500 );
	assert.equal( table.get( "/snd" ), 603 );
	assert.equal( table.size, 47 );
});

test("a console line names a row exactly and carries its words", () => {
	const table = publishedTable();
	assert.deepEqual( commands.debugCommand( table, "/time 18" ), { id: 102, args: [ "18" ] } );
	assert.deepEqual( commands.debugCommand( table, "/Pos\t1,2,3,4" ), { id: 3, args: [ "1,2,3,4" ] } );
	assert.equal( commands.debugCommand( table, "/debug" ), null, "the native set is case-sensitive" );
	assert.equal( commands.debugCommand( table, "Debug" ), null );
	assert.equal( commands.debugCommand( table, "/warp 1 2 3 4" ), null );
	assert.throws( () => commands.parseCommandTable( '1 : "/a"\n2 : "/a %d"' ) );
});

test("the GM console runs the console family and sends everything else", () => {
	/** @type {any[]} */
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	try {
		f.state.gameplay.eligibility = { gm: true };
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		};
		const type = line => {
			f.ui.event( {
				kind: "edit",
				id: "gm-input",
				value: line,
				start: line.length,
				end: line.length,
				composing: false
			} );
			f.ui.event( { kind: "activate", id: "submit" } );
			step();
		};
		step();
		f.ui.event( { kind: "key", code: "Backquote", shift: true } );
		step();
		type( "/Debug" );
		assert.ok( f.hasText( "DebugMsg On" ) );
		type( "/Debug" );
		assert.ok( f.hasText( "DebugMsg Off" ) );
		type( "/MsgClear" );
		assert.ok( !f.hasText( "DebugMsg On" ) && !f.hasText( "/MsgClear" ), "the console was not cleared" );
		type( "/null" );
		assert.deepEqual( sent, [], "a table command reached the server" );
		type( "/warp 1 2 3 4" );
		assert.deepEqual( sent, [ { kind: "gm-command", line: "/warp 1 2 3 4" } ] );
	} finally {
		f.dispose();
	}
});

test("/Item uses the named bag slot as a double click does", () => {
	/** @type {any[]} */
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	try {
		Object.assign( f.state.gameplay, {
			eligibility: { gm: true },
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ {
				slot: 13,
				refObjId: 1,
				typeFlags: (3 << 2) | (3 << 5) | (1 << 7) | (1 << 11),
				quantity: 5,
				name: "HP Recovery Potion",
				icon: "item/etc/hp_potion_01.ddj"
			} ]
		} );
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		};
		const type = line => {
			f.ui.event( {
				kind: "edit",
				id: "gm-input",
				value: line,
				start: line.length,
				end: line.length,
				composing: false
			} );
			f.ui.event( { kind: "activate", id: "submit" } );
			step();
		};
		step();
		f.ui.event( { kind: "key", code: "Backquote", shift: true } );
		step();
		type( "/Item 13 14" );
		assert.equal( sent.length, 0, "two words are not one slot" );
		type( "/Item 13" );
		assert.equal( sent.length, 1 );
		assert.equal( sent[0].kind, "item-use" );
		assert.equal( sent[0].slot, 13 );
	} finally {
		f.dispose();
	}
});

test("command words read as wcstol reads them", () => {
	assert.equal( commands.commandInteger( "13" ), 13 );
	assert.equal( commands.commandInteger( "-4x" ), -4 );
	assert.equal( commands.commandInteger( "slot" ), 0 );
	assert.equal( commands.commandFloat( "18.5" ), 18.5 );
	assert.equal( commands.commandFloat( "hour" ), 0 );
});

/*
================
consoleFixture

The GM console open over a fixture whose clock seed is day 10, 06:30, or
over no clock at all when worldClock is null.
================
*/
function consoleFixture( worldClock ) {
	/** @type {any[]} */
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	Object.assign( f.state.gameplay, {
		eligibility: { gm: true },
		worldClock: worldClock === null ? undefined : { day: 10, hour: 6, minute: 30, receivedAtMs: 0 }
	} );
	let now = 0;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
	};
	const type = line => {
		f.ui.event( {
			kind: "edit",
			id: "gm-input",
			value: line,
			start: line.length,
			end: line.length,
			composing: false
		} );
		f.ui.event( { kind: "activate", id: "submit" } );
		step();
	};
	step();
	f.ui.event( { kind: "key", code: "Backquote", shift: true } );
	step();
	return { f, sent, type, now: () => now };
}

test("the GM console sends the weather rows to the worker", () => {
	const { f, sent, type } = consoleFixture();
	try {
		type( "/rain 200" );
		assert.deepEqual( sent, [ { kind: "debug-weather", mode: 2, amount: 200 } ] );
		type( "/snow 5" );
		assert.deepEqual( sent[1], { kind: "debug-weather", mode: 3, amount: 5 } );
		type( "/power 42" );
		assert.deepEqual( sent[2], { kind: "debug-weather", amount: 42 }, "power keeps the mode" );
		type( "/clear" );
		assert.deepEqual( sent[3], { kind: "debug-weather", mode: 1, amount: 255 } );
		type( "/rain" );
		type( "/rain 1 2" );
		assert.equal( sent.length, 4, "a weather row without exactly one word ran" );
	} finally {
		f.dispose();
	}
});

test("the GM console sends /Pos as one move and /GetPos as the body-visual flip", () => {
	const { f, sent, type } = consoleFixture();
	try {
		type( "/Pos 61A8,1,2,3" );
		assert.deepEqual( sent, [
			{ kind: "move", destination: { regionId: 0x61a8, x: 1, y: 2, z: 3, angle: 0 } }
		] );
		type( "/Pos" );
		assert.equal( sent.length, 1, "/Pos without its word sent a move" );
		type( "/GetPos" );
		assert.deepEqual( sent[1], { kind: "debug-body-visual" } );
		assert.equal( sent.length, 2, "an extra line reached the worker" );
	} finally {
		f.dispose();
	}
});

test("the GM console runs /hideme, /showme, /uioff and /uion", () => {
	const { f, sent, type, now } = consoleFixture();
	try {
		type( "/hideme" );
		assert.deepEqual( sent, [ { kind: "debug-local-visible", visible: false } ] );
		type( "/showme" );
		assert.deepEqual( sent[1], { kind: "debug-local-visible", visible: true } );
		type( "/Debug" );
		assert.ok( f.hasText( "DebugMsg On" ) );
		const hudResult = f.ui.step( f.state, now() + 100 );
		assert.ok( hudResult, "the HUD step produced nothing" );
		const hudQuads = f.scenes.at( -1 ).quads.length,
			hudControls = hudResult.controls.map( c => c.id );
		assert.ok(
			hudControls.some( id => !id.startsWith( "gm-" ) ),
			"the fixture HUD never came up: " + hudControls.join( "," )
		);
		type( "/uioff" );
		assert.equal( sent.length, 2, "a UI row reached the worker" );
		assert.ok( f.hasText( "DebugMsg On" ), "the console hid with the UI" );
		const offResult = f.ui.step( f.state, now() + 100 );
		assert.ok( offResult, "the /uioff step produced nothing" );
		assert.ok( f.scenes.at( -1 ).quads.length < hudQuads, "the HUD stayed up under /uioff" );
		assert.ok(
			offResult.controls.length > 0 && offResult.controls.every( c => c.id.startsWith( "gm-" ) ),
			"controls besides the console's survived /uioff: " + offResult.controls.map( c => c.id ).join( "," )
		);
		type( "/uion" );
		assert.ok( f.scenes.at( -1 ).quads.length >= hudQuads, "the HUD did not return under /uion" );
	} finally {
		f.dispose();
	}
});

test("timeoff before the first clock lands the hold when the clock arrives", async () => {
	const { sampleWorldClock } = await import(
		"../../src/engine/foundation/gameplay/world-clock.ts"
	);
	const { f, type } = consoleFixture( null );
	try {
		type( "/timeoff" );
		const seed = { day: 10, hour: 6, minute: 30, receivedAtMs: 0 };
		const sampled = at => sampleWorldClock( f.ui.debugClockSeed( seed, at ), at );
		const early = sampled( 100000 );
		assert.deepEqual( sampled( 900000 ), early, "the hold did not land on the first clock" );
	} finally {
		f.dispose();
	}
});

test("the GM console drives the local clock", async () => {
	const { sampleWorldClock, worldClockWallTime } = await import(
		"../../src/engine/foundation/gameplay/world-clock.ts"
	);
	const { f, sent, type, now } = consoleFixture();
	try {
		const seed = { day: 10, hour: 6, minute: 30, receivedAtMs: 0 };
		const sampled = at => sampleWorldClock( f.ui.debugClockSeed( seed, at ), at );
		const wall = at => {
			const value = worldClockWallTime( f.ui.debugClockSeed( seed, at ), at );
			assert.ok( value );
			return value;
		};
		assert.notDeepEqual( sampled( 900000 ), sampled( 100000 ), "the live clock did not run" );
		type( "/timeoff" );
		assert.deepEqual( sampled( 900000 ), sampled( 100000 ), "timeoff did not hold the clock" );
		assert.deepEqual( sent, [], "a time row reached the server" );
		// "/+" steps a held clock back 72 minutes, "/-" forward again.
		const heldAt = wall( 100000 );
		type( "/+" );
		const stepped = wall( 100000 );
		assert.equal(
			(stepped.day * 1440 + stepped.hour * 60 + stepped.minute) -
				(heldAt.day * 1440 + heldAt.hour * 60 + heldAt.minute),
			-72
		);
		type( "/-" );
		assert.deepEqual( wall( 100000 ), heldAt );
		type( "/timeon" );
		assert.notDeepEqual( sampled( 900000 ), sampled( 100000 ), "timeon did not resume the clock" );
		// The setters re-seed at the fixture's current step time; read the
		// wall time there (a minute of game time later at most).
		const wallNow = () => wall( now() );
		type( "/time 18" );
		assert.equal( wallNow().hour, 18 );
		type( "/day" );
		assert.equal( wallNow().hour, 12 );
		type( "/night" );
		assert.equal( wallNow().hour, 0 );
	} finally {
		f.dispose();
	}
});
