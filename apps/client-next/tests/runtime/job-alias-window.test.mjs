/*
===========================================================================

job-alias-window.test.mjs - the job alias window's native check flow

CIFJobAlias (645E90) opens with Check enabled and OK disabled. Check
(6461C0) refuses a name the filter blocks with notice 0x18:0x23, ignores a
length outside 2..12 and sends 0x7620 mode 0 otherwise. The server's "free"
answer (0xB620 [1][0][alias]) marks that name checked (646110): Check off,
OK on. Any edit away from it clears the check (646340). OK raises the
create or modify confirm (646470), and only its Yes sends the checked name
with mode 1 and closes the window (645BA0).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	jobAliasCheck,
	jobGuildAnswer,
	JOB_ALIAS_CHECK,
	JOB_ALIAS_CREATE
} = await import( "../../src/engine/foundation/gameplay/job-guild.ts" );
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

// A name table that allows letters, digits and the underscore, as the
// server's [A-Za-z0-9_], and blocks "gm".
const RULES = {
	allowed: new Set(
		Array.from( "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", c => c.charCodeAt( 0 ) )
	),
	forbidden: [ "gm" ],
	wholeWords: []
};

const ascii = value => [ value.length & 255, value.length >> 8, ...Array.from( value, c => c.charCodeAt( 0 ) ) ];

test("Check sends only a new name of 2 to 12 allowed characters (6461C0)", () => {
	assert.deepEqual( jobAliasCheck( "Hawk", "", RULES ), { send: true, notice: null } );
	assert.deepEqual( jobAliasCheck( "Hawk", "Hawk", RULES ), { send: false, notice: null }, "the checked name" );
	assert.deepEqual( jobAliasCheck( "H", "", RULES ), { send: false, notice: null }, "too short: silent" );
	assert.deepEqual( jobAliasCheck( "H".repeat( 13 ), "", RULES ), { send: false, notice: null }, "too long: silent" );
	const blocked = jobAliasCheck( "TheGm", "", RULES );
	assert.equal( blocked.send, false );
	assert.ok( blocked.notice, "a filtered name raises the 0x18:0x23 notice" );
});

test("0xB620 [1][0][alias] reports the name as checked; [1][1] sets the alias", () => {
	const job = { type: 3, grade: 1, exp: 0, contribution: 0, alias: "" };
	const checked = jobGuildAnswer( {
		opcode: 0xb620,
		payload: Uint8Array.from( [ 1, JOB_ALIAS_CHECK, ...ascii( "Hawk" ) ] )
	}, job );
	assert.equal( checked?.aliasChecked, "Hawk" );
	assert.equal( checked?.job.alias, "", "a check leaves the alias alone" );
	const set = jobGuildAnswer( {
		opcode: 0xb620,
		payload: Uint8Array.from( [ 1, JOB_ALIAS_CREATE, ...ascii( "Hawk" ) ] )
	}, job );
	assert.equal( set?.aliasChecked, undefined );
	assert.equal( set?.job.alias, "Hawk" );
});

/*
================
aliasWindow

The alias window opened from a hunter guild NPC's menu, with the commands
it sends and a step that returns the published controls by id.
================
*/
function aliasWindow( alias = "" ) {
	const commands = [], f = uiFixture( command => commands.push( command ) );
	let now = 1000, seen = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) seen = f.ui.step( f.state, now += 50 ) ?? seen;
		return new Map( (seen?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	f.state.gameplay = {
		...f.state.gameplay,
		npcConversation: { gid: 77, phase: "menu" },
		job: { type: 3, alias },
		progression: { ...f.state.gameplay.progression, level: 40 }
	};
	step();
	f.ui.event( { kind: "activate", id: "npc-job-alias:3" } );
	return { f, commands, step };
}

/*
================
answerChecked

The server's "name is free" answer reaching the UI through the worker's
published state.
================
*/
function answerChecked( f, alias, sequence ) {
	f.state.gameplay = { ...f.state.gameplay, jobAliasChecked: { alias, sequence } };
}

const sentAliases = commands =>
	commands.filter( c => c.command?.kind === "job-alias" ).map( c => [ c.command.mode, c.command.alias ] );

test("OK waits for a checked name; an edit away from it re-arms Check (645E90, 646110, 646340)", () => {
	const { f, commands, step } = aliasWindow();
	try {
		let controls = step();
		assert.equal( controls.get( "job-alias-check" )?.disabled, false, "Check starts enabled" );
		assert.equal( controls.get( "job-alias-ok" )?.disabled, true, "OK starts disabled" );
		f.ui.event( { kind: "edit", id: "job-alias-text", value: "Hawk", start: 4, end: 4, composing: false } );
		f.ui.event( { kind: "activate", id: "job-alias-check" } );
		step();
		assert.deepEqual( sentAliases( commands ), [ [ JOB_ALIAS_CHECK, "Hawk" ] ] );
		answerChecked( f, "Hawk", 1 );
		controls = step();
		assert.equal( controls.get( "job-alias-check" )?.disabled, true, "Check off once checked" );
		assert.equal( controls.get( "job-alias-ok" )?.disabled, false, "OK on once checked" );
		f.ui.event( { kind: "edit", id: "job-alias-text", value: "Hawks", start: 5, end: 5, composing: false } );
		controls = step();
		assert.equal( controls.get( "job-alias-ok" )?.disabled, true, "an edit clears the check" );
		assert.equal( controls.get( "job-alias-check" )?.disabled, false );
		f.ui.event( { kind: "edit", id: "job-alias-text", value: "Hawk", start: 4, end: 4, composing: false } );
		controls = step();
		assert.equal( controls.get( "job-alias-ok" )?.disabled, true, "typing it back does not restore the check" );
		f.ui.event( { kind: "activate", id: "job-alias-ok" } );
		step();
		assert.ok( !f.hasText( "Create [" ), "a disabled OK raises no confirm" );
	} finally {
		f.dispose();
	}
});

test("Check refuses a filtered or wrong-length name before sending (6461C0)", () => {
	const { f, commands, step } = aliasWindow();
	try {
		step();
		for ( const value of [ "H", "TheGm" ] ) {
			f.ui.event( {
				kind: "edit",
				id: "job-alias-text",
				value,
				start: value.length,
				end: value.length,
				composing: false
			} );
			f.ui.event( { kind: "activate", id: "job-alias-check" } );
			step();
		}
		assert.deepEqual( sentAliases( commands ), [], "neither is sent" );
	} finally {
		f.dispose();
	}
});

test("OK raises the confirm with the checked name; only Yes sends mode 1 and closes (646470, 645BA0)", () => {
	const { f, commands, step } = aliasWindow();
	try {
		step();
		f.ui.event( { kind: "edit", id: "job-alias-text", value: "Hawk", start: 4, end: 4, composing: false } );
		f.ui.event( { kind: "activate", id: "job-alias-check" } );
		answerChecked( f, "Hawk", 1 );
		step();
		f.ui.event( { kind: "activate", id: "job-alias-ok" } );
		let controls = step();
		assert.ok( controls.has( "job-alias-yes" ) && controls.has( "job-alias-no" ), "the confirm is up" );
		assert.ok( f.hasText( "Hawk" ), "the confirm names the checked alias" );
		f.ui.event( { kind: "activate", id: "job-alias-no" } );
		controls = step();
		assert.ok( !controls.has( "job-alias-yes" ), "No closes the confirm" );
		assert.ok( controls.has( "job-alias-ok" ), "the window stays" );
		assert.deepEqual( sentAliases( commands ), [ [ JOB_ALIAS_CHECK, "Hawk" ] ], "No sends nothing" );
		f.ui.event( { kind: "activate", id: "job-alias-ok" } );
		step();
		f.ui.event( { kind: "activate", id: "job-alias-yes" } );
		controls = step();
		assert.deepEqual( sentAliases( commands ).at( -1 ), [ JOB_ALIAS_CREATE, "Hawk" ], "Yes sends mode 1" );
		assert.ok( !controls.has( "job-alias-ok" ) && !controls.has( "job-alias-yes" ), "Yes closes the window" );
	} finally {
		f.dispose();
	}
});

test("an answer for another name never checks a window opened after it", () => {
	const { f, step } = aliasWindow();
	try {
		answerChecked( f, "Old", 1 );
		step();
		f.ui.event( { kind: "activate", id: "job-alias-cancel" } );
		step();
		f.ui.event( { kind: "activate", id: "npc-job-alias:3" } );
		const controls = step();
		assert.equal( controls.get( "job-alias-ok" )?.disabled, true, "a fresh window starts unchecked" );
	} finally {
		f.dispose();
	}
});

test("the modify window shows the level x 50,000 cost row (645E90 mode 1)", () => {
	const { f, step } = aliasWindow( "Hawk" );
	try {
		const controls = step();
		// The cost belongs to the window's AliasModify row, not to a confirm:
		// nothing is checked yet, so OK is off and no Yes/No box is up.
		assert.equal( controls.get( "job-alias-ok" )?.disabled, true );
		assert.ok( !controls.has( "job-alias-yes" ) );
		assert.ok( f.hasText( "2,000,000" ), "level 40 x 50,000" );
	} finally {
		f.dispose();
	}
});

for ( const fit of [ true, false ] ) {
	test(`the Check caption ${fit ? "fits its plate" : "stays native with the row off"} (port-only Fit window labels)`, () => {
		const { f, step } = aliasWindow();
		try {
			f.ui.event( /** @type {any} */ ({ kind: "experimental-preferences", value: { fitWindowLabels: fit } }) );
			const check = step().get( "job-alias-check" );
			assert.equal( check?.label, fit ? "Check" : "Check for duplicate entries" );
		} finally {
			f.dispose();
		}
	});
}
