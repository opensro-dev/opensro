/*
===========================================================================

debug-commands.test.mjs - config\command.txt in the GM console

The published table parses as 68D9C0 reads it, a console line is looked up
as 68DD10 does, and the console family (690C40 cases 0, 1 and 4) runs in
the production HUD while every other line still goes to the server.

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
