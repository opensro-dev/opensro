/*
===========================================================================

sro-macro.test.mjs - the SRO Macro codec and the worker's settings lane

Port-only, not native (#3907): the v1.696 macro's 0x3555 replay, 0x7554
switch and save and 0xB554 answer. The frames below are the bytes the
server's macro package (apps/server/internal/game/macro) writes and
reads, so a drift on either side breaks here.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const codec = await import( "../../src/engine/foundation/gameplay/sro-macro.ts" );
const { createSroMacro } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/sro-macro/sro-macro.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createSroMacroHud, macroAcquiredSkills, scrubMacroSkillLists } = await import(
	"../../src/engine/runtime/ui/hud/sro-macro-hud.ts"
);
import { macroAnswer as answer, macroReplay as replay } from "../helpers/sro-macro-frames.mjs";
const {
	OP_MACRO_CONFIG,
	OP_MACRO_REQUEST,
	MACRO_POTION,
	MACRO_SKILL,
	MACRO_HUNT,
	MACRO_POTION_BYTES,
	MACRO_HUNT_BYTES
} = codec;

/*
================
viewOf

The lane's live settings, which every caller here expects present.
================
*/
/** @param {ReturnType<typeof createSroMacro>} lane */
function viewOf( lane ) {
	const settings = lane.view();
	assert.ok( settings );
	return settings;
}

/*
================
saved

An encoded save, which every caller here expects present.
================
*/
/** @param {Uint8Array | null} payload */
function saved( payload ) {
	assert.ok( payload );
	return Array.from( payload );
}

/*
================
noticeText
================
*/
/** @param {ReturnType<typeof createSroMacro>} lane */
function noticeText( lane ) {
	const notice = lane.takeNotice();
	assert.ok( notice );
	return notice.text;
}

/*
================
draftOf
================
*/
/** @param {ReturnType<typeof createSroMacroHud>} hud */
function draftOf( hud ) {
	const draft = hud.state().draft;
	assert.ok( draft );
	return draft;
}

test("the Experimental row is on by default and only an explicit false turns it off", () => {
	// Owner, 2026-11-21: the SRO Macro ships on in the next release.
	// Like the other default-on rows, a stored non-boolean never turns
	// it off; only an explicit false does.
	assert.equal( experimentalOptions().sroMacro, true );
	assert.equal( experimentalOptions( { sroMacro: "yes" } ).sroMacro, true );
	assert.equal( experimentalOptions( { sroMacro: true } ).sroMacro, true );
	assert.equal( experimentalOptions( { sroMacro: false } ).sroMacro, false );
});

test("a full replay decodes every section and unsaved blocks load the defaults", () => {
	const potion = Array.from( { length: MACRO_POTION_BYTES }, ( _, i ) => i );
	const settings = codec.decodeMacroConfig(
		replay( { potion, skillOn: 1, attack: [ { slot: 2, skill: 0x1234 } ] } ).payload,
		codec.defaultMacroSettings()
	);
	assert.equal( settings.available, 7 );
	assert.equal( settings.potionOn, false );
	assert.equal( settings.skillOn, true );
	assert.deepEqual( settings.potion, potion );
	assert.deepEqual( settings.hunt, codec.defaultHuntBlock() );
	assert.deepEqual( settings.skillFlags, [ 1, 0, 0, 1 ] );
	assert.deepEqual( settings.attack, [ { slot: 2, skill: 0x1234 } ] );
	assert.deepEqual( settings.buff, [] );
});

test("an availability update switches off the kinds it removes", () => {
	const on = { ...codec.defaultMacroSettings(), available: 7, potionOn: true, huntOn: true };
	const next = codec.decodeMacroConfig( Uint8Array.of( 2, 1 ), on );
	assert.equal( next.available, 1 );
	assert.equal( next.potionOn, true );
	assert.equal( next.huntOn, false );
});

test("malformed replays and answers are protocol errors", () => {
	const base = codec.defaultMacroSettings();
	assert.throws( () => codec.decodeMacroConfig( Uint8Array.of( 0, 7, 1, MACRO_POTION, 0, 3 ), base ), /SRO Macro/ );
	assert.throws( () => codec.decodeMacroConfig( Uint8Array.of( 1 ), base ), /SRO Macro/ );
	assert.throws( () => codec.decodeMacroAnswer( Uint8Array.of( 0, 0, 1, 0 ) ), /SRO Macro/ );
	assert.throws( () => codec.decodeMacroAnswer( Uint8Array.of( 3, 0, 1, 0 ) ), /SRO Macro/ );
});

test("saves encode the server's layout and send only what changed", () => {
	const base = codec.defaultMacroSettings();
	const potion = codec.withPotionSlot( base.potion, 1, { item: 0x01020304, a: 50, b: 1 } );
	const frame = saved( codec.encodeMacroSave( MACRO_POTION, { ...base, potion }, base ) );
	assert.deepEqual( frame.slice( 0, 5 ), [ 1, MACRO_POTION, 3, 0, 0 ] );
	assert.equal( frame.length, 5 + MACRO_POTION_BYTES );
	assert.deepEqual( codec.potionSlot( frame.slice( 5 ), 1 ), { item: 0x01020304, a: 50, b: 1 } );
	assert.equal( codec.encodeMacroSave( MACRO_POTION, base, base ), null );

	const hunt = codec.withHuntByte( base.hunt, 3, 1 );
	assert.deepEqual( saved( codec.encodeMacroSave( MACRO_HUNT, { ...base, hunt }, base ) ), [
		1,
		MACRO_HUNT,
		2,
		0,
		hunt[1],
		...hunt
	] );

	const skills = { ...base, skillFlags: [ 0, 1, 0, 0 ], buff: [ { slot: 0, skill: 0x0a0b0c0d } ] };
	assert.deepEqual(
		saved( codec.encodeMacroSave( MACRO_SKILL, skills, base ) ),
		[ 1, MACRO_SKILL, 2, 0, 1, 1, 5, 5, 0, 0, 0x0d, 0x0c, 0x0b, 0x0a ]
	);
	assert.deepEqual( saved( codec.encodeMacroSave( MACRO_SKILL, base, skills ) ), [
		1,
		MACRO_SKILL,
		2,
		0,
		1,
		0,
		5,
		1,
		0,
		0
	] );
});

test("the lane is silent until the server replays its settings", () => {
	const lane = createSroMacro();
	assert.equal( lane.view(), undefined );
	assert.equal( lane.switchFrame( MACRO_POTION, true ), null );
	assert.equal( lane.saveFrame( MACRO_HUNT, codec.defaultMacroSettings() ), null );
	assert.equal( lane.receive( { opcode: 0x3fc1, payload: new Uint8Array( 0 ) } ), false );
});

test("a switch is live only once the server answers it", () => {
	const lane = createSroMacro();
	assert.equal( lane.receive( replay() ), true );
	const frame = lane.switchFrame( MACRO_HUNT, true );
	assert.deepEqual( frame, { opcode: OP_MACRO_REQUEST, payload: Uint8Array.of( 0, MACRO_HUNT, 1 ) } );
	assert.equal( viewOf( lane ).huntOn, false );
	lane.receive( answer( 0, MACRO_HUNT, 1, 0, 1 ) );
	assert.equal( viewOf( lane ).huntOn, true );
	assert.equal( lane.switchFrame( MACRO_HUNT, true ), null );
	assert.equal( lane.takeNotice(), undefined );
});

test("a refused switch prints the native ON/OFF error and changes nothing", () => {
	const lane = createSroMacro();
	lane.receive( replay() );
	lane.receive( answer( 0, MACRO_POTION, 0x06, 0x98 ) );
	assert.equal( viewOf( lane ).potionOn, false );
	assert.equal( noticeText( lane ), "ON/OFF Error" );
	assert.equal( lane.takeNotice(), undefined );
});

test("a save commits its pending copy on success and keeps the old one on failure", () => {
	const lane = createSroMacro();
	lane.receive( replay() );
	const base = viewOf( lane );
	const hunt = codec.withHuntByte( base.hunt, 0, 2 );
	assert.notEqual( lane.saveFrame( MACRO_HUNT, { ...base, hunt } ), null );
	assert.deepEqual( viewOf( lane ).hunt, base.hunt );
	lane.receive( answer( 1, MACRO_HUNT, 1, 0 ) );
	assert.deepEqual( viewOf( lane ).hunt, hunt );

	const later = codec.withHuntByte( hunt, 0, 3 );
	lane.saveFrame( MACRO_HUNT, { ...viewOf( lane ), hunt: later } );
	lane.receive( answer( 1, MACRO_HUNT, 2, 0 ) );
	assert.deepEqual( viewOf( lane ).hunt, hunt );
	assert.equal( noticeText( lane ), "Save Error" );
});

/*
================
liveSettings
================
*/
function liveSettings() {
	const lane = createSroMacro();
	lane.receive( replay() );
	return viewOf( lane );
}

test("the menu opens only with the row on and the server's replay", () => {
	const hud = createSroMacroHud();
	assert.equal( hud.toggleMenu( undefined, true ), false );
	assert.equal( hud.toggleMenu( liveSettings(), false ), false );
	assert.equal( hud.state().window, "none" );
	assert.equal( hud.toggleMenu( liveSettings(), true ), true );
	assert.equal( hud.state().window, "menu" );
	hud.reconcile( liveSettings(), false );
	assert.equal( hud.state().window, "none" );
});

test("the under-minimap guide icon toggles the menu", async () => {
	const {
		MACRO_GUIDE_ID,
		MACRO_GUIDE_ICON_PATH,
		MACRO_GUIDE_ICON_X_INSET,
		MACRO_GUIDE_ICON_Y
	} = await import( "../../src/engine/runtime/ui/hud/sro-macro-hud.ts" );
	assert.equal( MACRO_GUIDE_ID, "sro-macro-icon" );
	assert.match( MACRO_GUIDE_ICON_PATH, /mecro_0\.png$/ );
	assert.equal( MACRO_GUIDE_ICON_X_INSET, 0x57 );
	assert.equal( MACRO_GUIDE_ICON_Y, 0xb3 );
	const hud = createSroMacroHud(), live = liveSettings();
	assert.equal( hud.toggleMenu( live, true ), true );
	assert.equal( hud.state().window, "menu" );
	assert.equal( hud.toggleMenu( live, true ), true );
	assert.equal( hud.state().window, "none" );
});

test("the menu's ON/OFF asks the worker to flip the live state", () => {
	const hud = createSroMacroHud(), live = liveSettings();
	hud.toggleMenu( live, true );
	assert.deepEqual( hud.click( "sro-macro:switch:2", live ), [ {
		kind: "macro-switch",
		macro: MACRO_HUNT,
		on: true
	} ] );
	assert.deepEqual(
		hud.click( "sro-macro:switch:0", { ...live, potionOn: true } ),
		[ { kind: "macro-switch", macro: MACRO_POTION, on: false } ]
	);
	assert.deepEqual( hud.click( "sro-macro:switch:1", { ...live, available: 1 } ), [] );
});

test("the options window edits a draft that only Save sends", () => {
	const hud = createSroMacroHud(), live = liveSettings();
	hud.toggleMenu( live, true );
	hud.click( "sro-macro:options:0", live );
	assert.equal( hud.state().window, "options" );
	hud.click( "sro-macro:check:0", live );
	hud.click( "sro-macro:percent:0:1", live );
	hud.click( "sro-macro:tab:2", live );
	hud.click( "sro-macro:hunt-group:0", live );
	hud.click( "sro-macro:hunt-check:8", live );
	const draft = draftOf( hud );
	// A gauge row is on by b and fires at a (00692e00); on starts at 50%.
	assert.deepEqual( codec.potionSlot( draft.potion, 0 ), { item: 0, a: 55, b: 1 } );
	assert.equal( draft.hunt[0], 1 );
	assert.equal( draft.hunt[8], 1 );
	assert.deepEqual( live.potion, codec.defaultMacroSettings().potion );

	const commands = hud.click( "sro-macro:save", live );
	const saves = commands.flatMap( c => (c.kind === "macro-save" ? [ c ] : []) );
	assert.equal( saves.length, commands.length );
	assert.deepEqual( saves.map( c => c.macro ), [ MACRO_POTION, MACRO_SKILL, MACRO_HUNT ] );
	assert.ok( saves.every( c => c.settings === draft ) );
	assert.equal( hud.state().window, "none" );

	hud.toggleMenu( live, true );
	hud.click( "sro-macro:options:0", live );
	hud.click( "sro-macro:check:1", live );
	assert.deepEqual( hud.click( "sro-macro:cancel", live ), [] );
	assert.equal( hud.state().draft, null );
});

test("the skill tab assigns acquired skills to the active Attack / Buff page", () => {
	// Only the fields the skill tab reads; the rest of SkillMetadata is the catalog's.
	const index =
		/** @type {ReadonlyMap<number, import("../../src/engine/foundation/gameplay/skill-catalog.ts").SkillMetadata>} */ (
			/** @type {unknown} */ (new Map( [
				[ 10, {
					id: 10,
					group: 1,
					name: "Strike",
					targetRequired: true,
					actionMs: 900,
					cooldownMs: 0,
					admit: {}
				} ],
				[ 11, {
					id: 11,
					group: 2,
					name: "Guard",
					targetRequired: false,
					actionMs: 600,
					cooldownMs: 0,
					admit: {}
				} ],
				// Instant self-buff (Grass Walk): no actionMs, but a cooldown.
				[ 113, {
					id: 113,
					group: 260,
					name: "Grass Walk",
					targetRequired: false,
					cooldownMs: 1000,
					admit: {}
				} ],
				// Castle Shield: actionMs 0, 60s cool — must stay in the acquired list.
				[
					27,
					{
						id: 27,
						group: 182,
						name: "Castle shield",
						nameSymbol: "SN_SKILL_CH_SWORD_SHIELD_A",
						targetRequired: false,
						cooldownMs: 60000,
						admit: { reqi: { pairs: [ [ 4, 1 ] ] } }
					}
				],
				// Lightning-style passive: admit is published for reqi, but it is
				// never pressed — must stay out of the acquired / Buff lists.
				[ 12, {
					id: 12,
					group: 3,
					name: "Lightning Passive",
					targetRequired: false,
					cooldownMs: 0,
					admit: {},
					passive: true
				} ],
				[ 13, {
					id: 13,
					group: 4,
					name: "Punch",
					targetRequired: true,
					actionMs: 500,
					basicAttack: true,
					cooldownMs: 0
				} ],
				[ 114, {
					id: 114,
					group: 261,
					name: "Ghost Walk",
					targetRequired: true,
					groundTarget: true,
					cooldownMs: 5000
				} ],
				// Sibling rank of Guard — same skilldata group.
				[ 1111, {
					id: 1111,
					group: 2,
					name: "Guard II",
					targetRequired: false,
					actionMs: 600,
					cooldownMs: 0,
					admit: {}
				} ]
			] ))
		);
	const board = [
		{ id: 27, mastery: 257, row: 2, column: 0, icon: "a", name: "SN_SKILL_CH_SWORD_SHIELD_A" },
		{ id: 10, mastery: 257, row: 0, column: 0, icon: "b", name: "Strike" },
		{ id: 11, mastery: 257, row: 1, column: 0, icon: "c", name: "Guard" },
		{ id: 113, mastery: 274, row: 2, column: 0, icon: "d", name: "Grass" },
		{ id: 1111, mastery: 257, row: 1, column: 1, icon: "e", name: "Guard II" }
	];
	const acquired = macroAcquiredSkills( [ 10, 11, 113, 12, 13, 114, 99, 10, 1111, 27 ], index, board );
	assert.deepEqual( acquired.map( s => s.id ), [ 27, 10, 11, 113, 1111 ] );
	// A saved Lightning passive is scrubbed off Buff even though admit was set.
	assert.deepEqual(
		scrubMacroSkillLists( {
			...liveSettings(),
			buff: [ { slot: 0, skill: 12 }, { slot: 1, skill: 113 } ]
		}, index ).buff,
		[ { slot: 0, skill: 113 } ]
	);
	const hud = createSroMacroHud(), live = liveSettings();
	hud.toggleMenu( live, true );
	hud.click( "sro-macro:options:1", live, acquired, [], index );
	hud.click( "sro-macro:skill-add:10", live, acquired, [], index );
	hud.click( "sro-macro:skill-add:10", live, acquired, [], index );
	// Grass Walk is a self-buff — refused on the Attack tab.
	hud.click( "sro-macro:skill-add:113", live, acquired, [], index );
	assert.deepEqual( draftOf( hud ).attack, [ { slot: 0, skill: 10 } ] );
	assert.deepEqual( draftOf( hud ).buff, [] );
	hud.click( "sro-macro:skill-page:buff", live, acquired, [], index );
	hud.click( "sro-macro:skill-add:11", live, acquired, [], index );
	hud.click( "sro-macro:skill-add:11", live, acquired, [], index );
	hud.click( "sro-macro:skill-add:1111", live, acquired, [], index );
	hud.click( "sro-macro:skill-add:113", live, acquired, [], index );
	// A target-required strike does not belong on Buff.
	hud.click( "sro-macro:skill-add:10", live, acquired, [], index );
	assert.deepEqual( draftOf( hud ).attack, [ { slot: 0, skill: 10 } ] );
	assert.deepEqual( draftOf( hud ).buff, [ { slot: 0, skill: 11 }, { slot: 1, skill: 113 } ] );
	hud.click( "sro-macro:skill-remove:attack:0", live, acquired, [], index );
	assert.deepEqual( draftOf( hud ).attack, [] );
});

test("an unavailable kind sends nothing and clear forgets the session", () => {
	const lane = createSroMacro();
	lane.receive( replay() );
	lane.receive( { opcode: OP_MACRO_CONFIG, payload: Uint8Array.of( 2, 0b011 ) } );
	assert.equal( lane.switchFrame( MACRO_HUNT, true ), null );
	assert.notEqual( lane.switchFrame( MACRO_SKILL, true ), null );
	lane.clear();
	assert.equal( lane.view(), undefined );
});
