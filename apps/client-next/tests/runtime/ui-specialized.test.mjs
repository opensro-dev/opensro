/*
===========================================================================

ui-specialized.test.mjs - HUD commands and native inventory page boundaries

Reuse the production HUD fixture and its retail layout delivery. Chat IME
input belongs to browser/chat-enter-ime.test.mjs; browser/quest-details.test.mjs
covers actual confirmation clicks. These checks preserve command routing and
server-owned snapshots without relying on the removed sidebar menu.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const HUD_ADMISSION_FRAMES = 12;
const FRAME_INTERVAL_MS = 100;
const EQUIPMENT_SLOTS = 13;
const BAG_PAGE_SLOTS = 32;
const FIRST_PAGE_CAPACITY = EQUIPMENT_SLOTS + BAG_PAGE_SLOTS;
const PARTIAL_PAGE_SLOTS = 13;

/*
================
slotIds

Compare membership independently of the HUD's paint order.
================
*/
function slotIds( controls, disabled = false ) {
	return controls.filter( control => control.id.startsWith( "slot:" ) && !!control.disabled === disabled )
		.map( control => Number( control.id.slice( 5 ) ) ).sort( ( a, b ) => a - b );
}

/*
================
hudControls

Retain unchanged publications and activate only controls the HUD admitted.
================
*/
function hudControls( fixture, state ) {
	let now = 0;
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */
	let semantics = null;
	/*
	================
	draw
	================
	*/
	function draw() {
		semantics = fixture.ui.step( state, now ) ?? semantics;
		now += FRAME_INTERVAL_MS;
		return defined( semantics, "HUD publication" );
	}
	for ( let frame = 0; frame < HUD_ADMISSION_FRAMES; frame++ ) {
		semantics = fixture.ui.step( state, now ) ?? semantics;
		now += FRAME_INTERVAL_MS;
	}
	return {
		draw,
		/*
		================
		activate
		================
		*/
		activate( id ) {
			const control = defined( draw().controls.find( control => control.id === id ), id );
			assert.equal( !!control.disabled, false, id + " must be usable" );
			fixture.ui.event( { kind: "activate", id } );
			return draw();
		}
	};
}

test("HUD chat submission routes one command without adding an authoritative line", () => {
	const commands = [], fixture = uiFixture( command => commands.push( command ) );
	const state = {
		...fixture.state,
		gameplay: { ...fixture.state.gameplay, chat: { lines: [], pending: false, error: null } }
	};
	try {
		const hud = hudControls( fixture, state );
		assert.ok( hud.draw().controls.some( control => control.id === "chat-text" ) );
		fixture.ui.event( { kind: "focus", id: "chat-text" } );
		fixture.ui.event( { kind: "edit", id: "chat-text", value: "hello", start: 5, end: 5, composing: false } );
		hud.draw();
		// This is the submit event emitted by the browser's Enter/IME bridge.
		fixture.ui.event( { kind: "activate", id: "submit" } );
		hud.draw();
		assert.deepEqual( commands.filter( value => value.command?.kind === "chat" ), [ {
			kind: "gameplay",
			command: { kind: "chat", channel: 1, text: "hello", target: "" }
		} ] );
		assert.deepEqual( state.gameplay.chat.lines, [], "sending must wait for the server's chat line" );
	} finally {
		fixture.dispose();
	}
});

test("quest abandonment sends only confirmation and preserves the authoritative quest", () => {
	const commands = [], fixture = uiFixture( command => commands.push( command ) );
	const quest = {
		refId: 7,
		u08: 0,
		u09: 0,
		flags: 0,
		// A completed quest (u10 = 2) exposes Reward instead of Abandon.
		u10: 1,
		contents: [ {
			tag: 1,
			kind: 1,
			description: "Find the relic",
			objectiveSentinel: false,
			objectiveValues: [ 1 ]
		} ],
		targetIds: []
	};
	const state = { ...fixture.state, gameplay: { ...fixture.state.gameplay, quests: [ quest ] } };
	try {
		const hud = hudControls( fixture, state );
		fixture.ui.event( { kind: "key", code: "KeyQ" } );
		hud.activate( "quest:7" );
		hud.activate( "quest-abandon" );
		assert.ok( hud.draw().controls.some( control => control.id === "quest-abandon-yes" ) );
		assert.deepEqual( commands.filter( value => value.command?.kind === "quest-abandon" ), [] );
		hud.activate( "quest-abandon-no" );
		assert.equal( hud.draw().controls.some( control => control.id === "quest-abandon-yes" ), false );
		assert.deepEqual( commands.filter( value => value.command?.kind === "quest-abandon" ), [] );
		hud.activate( "quest-abandon" );
		hud.activate( "quest-abandon-yes" );
		assert.deepEqual( commands.filter( value => value.command?.kind === "quest-abandon" ), [ {
			kind: "gameplay",
			command: { kind: "quest-abandon", refId: 7 }
		} ] );
		assert.deepEqual( state.gameplay.quests, [ quest ], "confirmation must wait for the server's removal" );
		assert.equal( hud.draw().controls.some( control => control.id === "quest-abandon-yes" ), false );
	} finally {
		fixture.dispose();
	}
});

test("native inventory tabs keep equipment and disable bag cells beyond capacity", () => {
	for ( const capacity of [ FIRST_PAGE_CAPACITY, FIRST_PAGE_CAPACITY + PARTIAL_PAGE_SLOTS ] ) {
		const fixture = uiFixture();
		const state = {
			...fixture.state,
			gameplay: { ...fixture.state.gameplay, inventorySlotCount: capacity, equipmentSlotCount: EQUIPMENT_SLOTS }
		};
		try {
			const hud = hudControls( fixture, state );
			fixture.ui.event( { kind: "key", code: "KeyI" } );
			let controls = hud.draw().controls;
			assert.deepEqual(
				controls.filter( control => control.id.startsWith( "inventory-page:" ) ).map( control => control.id ),
				capacity === FIRST_PAGE_CAPACITY ? [ "inventory-page:0" ] : [ "inventory-page:0", "inventory-page:1" ]
			);
			// Port-only, owner-approved 2026-10-15, always on: the tabs read their page number.
			assert.deepEqual(
				controls.filter( control => control.id.startsWith( "inventory-page:" ) ).map( control =>
					control.label
				),
				capacity === FIRST_PAGE_CAPACITY ? [ "1" ] : [ "1", "2" ]
			);
			assert.deepEqual(
				slotIds( controls ),
				Array.from( { length: FIRST_PAGE_CAPACITY }, ( _, slot ) => slot )
			);
			assert.equal( controls.some( control => control.id === "slot:" + FIRST_PAGE_CAPACITY ), false );
			if ( capacity === FIRST_PAGE_CAPACITY ) continue;
			controls = hud.activate( "inventory-page:1" ).controls;
			assert.deepEqual(
				slotIds( controls ),
				[
					...Array.from( { length: EQUIPMENT_SLOTS }, ( _, slot ) => slot ),
					...Array.from( { length: PARTIAL_PAGE_SLOTS }, ( _, slot ) => FIRST_PAGE_CAPACITY + slot )
				]
			);
			assert.deepEqual(
				slotIds( controls, true ),
				Array.from( { length: BAG_PAGE_SLOTS - PARTIAL_PAGE_SLOTS }, ( _, slot ) => capacity + slot )
			);
			controls = hud.activate( "inventory-page:0" ).controls;
			assert.deepEqual(
				slotIds( controls ),
				Array.from( { length: FIRST_PAGE_CAPACITY }, ( _, slot ) => slot )
			);
		} finally {
			fixture.dispose();
		}
	}
});
