/*
===========================================================================

npc-storage-window.test.mjs - warehouse actions hide the NPC window itself

Exercise the shipped dispatcher and HUD: v1.150 hides the parent before the
storage request, keeps the NPC selected, and does not restore the menu when
another panel opens. Selecting the NPC again owns showing its menu.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createNpcConversation } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/npc/npc.ts"
);
const { createNpcPanel } = await import( "../../src/engine/runtime/ui/hud/npc-panel.ts" );

const NPC = { gid: 7, refObjId: 1, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0, name: "Warehouse" };
const STORAGE_LIST_REQUEST = 0x72c3;
const NPC_ACTION = 0x7338;
const NPC_RELEASE = 0x74b3;
const TARGET_REQUEST = 0x745a;
const PERSONAL_STORAGE = 4;
const GUILD_STORAGE = 0x4000;

/*
================
storageFixture

Read the published state inside send(), before the request leaves the owner.
This checks ordering as well as the snapshot eventually received by the HUD.
================
*/
function storageFixture( guild = false ) {
	const frames = [], requests = [];
	let latest, now = 0;
	const game = createGameplay( frame => {
		frames.push( frame );
		if ( frame.opcode === STORAGE_LIST_REQUEST || frame.opcode === NPC_ACTION ) {
			latest = defined( game.take() );
			requests.push( { frame, state: latest } );
		}
	} );
	game.bootstrap( { simulationProtocolVersion: 1, inventorySlotCount: 45, equipmentSlotCount: 13 } );
	game.seed( { ...NPC, gid: 1, kind: "player" } );
	/*
	================
	command
	================
	*/
	function command( value ) {
		game.command( value, ++now, NPC );
	}
	/*
	================
	select
	================
	*/
	function select() {
		command( { kind: "select", gid: NPC.gid } );
		const grant = Buffer.alloc( 11 );
		grant[0] = 1;
		grant.writeUInt32LE( NPC.gid, 1 );
		grant.writeUInt32LE( (guild ? GUILD_STORAGE : PERSONAL_STORAGE) | 2, 6 );
		game.receive( { opcode: 0xb45a, payload: grant }, ++now );
	}
	/*
	================
	read
	================
	*/
	function read() {
		latest = game.take() ?? latest;
		return defined( latest );
	}
	/*
	================
	openPersonalRoom
	================
	*/
	function openPersonalRoom() {
		game.receive( { opcode: 0x3126, payload: new Uint8Array( 8 ) }, ++now );
		game.receive( { opcode: 0x321a, payload: Uint8Array.of( 150, 0 ) }, ++now );
		game.receive( { opcode: 0xb338, payload: Uint8Array.of( 1, PERSONAL_STORAGE, 0, 0, 0 ) }, ++now );
	}
	select();
	read();
	return { game, frames, requests, command, select, read, openPersonalRoom };
}

test("personal storage hides the NPC window before both uncached and cached requests without releasing it", () => {
	const f = storageFixture();
	try {
		assert.equal( f.read().npcWindow.hidden, false );
		f.command( { kind: "storage-open", gid: NPC.gid } );
		const first = defined( f.requests[0] );
		assert.equal( first.frame.opcode, STORAGE_LIST_REQUEST );
		assert.equal( first.state.npcWindow.hidden, true, "hidden before the first list request" );
		assert.equal( first.state.target, NPC.gid );
		assert.equal( first.state.npcConversation.phase, "menu" );
		assert.equal( first.state.storage.phase, "listing" );
		f.openPersonalRoom();
		assert.equal( f.read().storage.phase, "open" );
		assert.equal( f.read().npcWindow.hidden, true );
		f.command( { kind: "storage-close" } );
		assert.equal( f.read().npcWindow.hidden, true, "closing only the room does not show the NPC window" );
		f.command( { kind: "storage-open", gid: NPC.gid } );
		const cached = defined( f.requests.at( -1 ) );
		assert.equal( cached.frame.opcode, NPC_ACTION );
		assert.deepEqual( [ ...cached.frame.payload ], [ NPC.gid, 0, 0, 0, PERSONAL_STORAGE, 0, 0, 0 ] );
		assert.equal( cached.state.npcWindow.hidden, true );
		assert.equal( cached.state.storage.phase, "opening" );
		assert.equal( f.frames.filter( frame => frame.opcode === NPC_RELEASE ).length, 0 );
	} finally {
		f.game.dispose();
	}
});

test("guild storage hides the NPC window before its function request", () => {
	const f = storageFixture( true );
	try {
		f.command( { kind: "storage-open-guild", gid: NPC.gid } );
		const request = defined( f.requests[0] );
		assert.equal( request.frame.opcode, NPC_ACTION );
		assert.deepEqual( [ ...request.frame.payload ], [ NPC.gid, 0, 0, 0, 0, 0x40, 0, 0 ] );
		assert.equal( request.state.npcWindow.hidden, true );
		assert.equal( request.state.target, NPC.gid );
		assert.equal( request.state.npcConversation.phase, "menu" );
		assert.equal( f.frames.filter( frame => frame.opcode === NPC_RELEASE ).length, 0 );
	} finally {
		f.game.dispose();
	}
});

test("hidden NPC menus survive panel changes and coalesced reselection shows the same NPC again", () => {
	const f = storageFixture(), pending = [], hud = uiFixture( message => pending.push( message.command ) );
	let semantics, now = 0;
	hud.state.entities.push( { ...hud.state.entities[0], ...NPC } );
	/*
	================
	draw
	================
	*/
	function draw() {
		const view = { ...hud.state, gameplay: f.read() };
		for ( let i = 0; i < 50; i++ ) semantics = hud.ui.step( view, ++now ) ?? semantics;
		return defined( semantics ).controls;
	}
	try {
		assert.ok( draw().some( control => control.id === "storage-open" ) );
		hud.ui.event( { kind: "activate", id: "storage-open" } );
		assert.ok(
			!draw().some( control => control.id === "npc-close" ),
			"hidden before the worker processes the click"
		);
		assert.deepEqual( pending, [ { kind: "storage-open", gid: NPC.gid } ] );
		f.command( pending.shift() );
		assert.ok( !draw().some( control => control.id === "npc-close" ), "worker receipt keeps the window hidden" );
		f.openPersonalRoom();
		assert.ok( draw().some( control => control.id === "storage-gold" ) );
		for ( const panel of [ "Inventory", "Character", "Inventory" ] ) {
			hud.ui.event( { kind: "activate", id: "open-window:" + panel } );
			const controls = draw();
			assert.ok( !controls.some( control => control.id === "storage-gold" ), "the primary panel changed" );
			assert.ok(
				!controls.some( control =>
					[ "npc-close", "npc-talk", "npc-talkend", "storage-open" ].includes( control.id )
				),
				"another panel must not restore the hidden NPC window"
			);
		}
		assert.equal( f.frames.filter( frame => frame.opcode === NPC_RELEASE ).length, 0 );
		f.command( { kind: "npc-close" } );
		f.game.receive( { opcode: 0xb4b3, payload: Uint8Array.of( 1 ) }, ++now );
		f.select();
		assert.equal( f.read().npcWindow.hidden, false );
		assert.ok( draw().some( control => control.id === "storage-open" ), "new selection shows the menu" );
	} finally {
		hud.dispose();
		f.game.dispose();
	}
});

test("clicking the same NPC again requests selection only while its window is hidden", () => {
	const f = storageFixture();
	/*
	================
	selectRequests
	================
	*/
	const selectRequests = () => f.frames.filter( frame => frame.opcode === TARGET_REQUEST ).length;
	try {
		const initial = selectRequests();
		f.command( { kind: "select", gid: NPC.gid } );
		assert.equal( selectRequests(), initial, "a visible menu suppresses the repeated click" );
		f.command( { kind: "storage-open", gid: NPC.gid } );
		f.openPersonalRoom();
		f.command( { kind: "storage-close" } );
		assert.equal( f.read().npcWindow.hidden, true );
		f.select();
		assert.equal( selectRequests(), initial + 1, "a hidden menu lets the click select again" );
		assert.equal( f.read().npcWindow.hidden, false );
		assert.equal( f.read().npcConversation.phase, "menu" );
	} finally {
		f.game.dispose();
	}
});

test("hiding a window preserves the NPC lock and clear or selection resets its visibility", () => {
	const npc = createNpcConversation( () => {} );
	npc.select( NPC.gid );
	npc.interaction( Uint8Array.of( 1, PERSONAL_STORAGE, 0, 0, 0 ), PERSONAL_STORAGE );
	npc.hideWindow();
	assert.equal( npc.window().hidden, true );
	assert.equal( npc.interactionLocked(), true );
	assert.equal( npc.state().phase, "menu" );
	npc.select( NPC.gid );
	assert.equal( npc.window().hidden, false, "even a new selection of the same NPC shows its window" );
	npc.hideWindow();
	npc.clear();
	assert.equal( npc.window().hidden, false );
	assert.equal( npc.interactionLocked(), false );
	assert.equal( npc.state().phase, "closed" );
});

test("stale window receipts cannot undo a click or let a hidden NPC consume scroll input", () => {
	const npc = createNpcConversation( () => {} ), panel = createNpcPanel();
	npc.select( NPC.gid );
	panel.observe( npc.state(), npc.window() );
	panel.geometry( { range: 30, travel: 272, bounds: [ 23, 23, 302, 343 ] } );
	panel.event( { kind: "scroll", x: 30, y: 30, delta: 1 } );
	assert.equal( panel.top(), 3 );
	panel.hide();
	assert.equal( panel.observe( npc.state(), npc.window() ), false );
	assert.equal( panel.hidden(), true );
	assert.equal( panel.event( { kind: "scroll", x: 30, y: 30, delta: 1 } ), false );
	assert.equal( panel.top(), 3 );
	npc.hideWindow();
	assert.equal( panel.observe( npc.state(), npc.window() ), true );
	assert.equal( panel.hidden(), true );
	npc.clear();
	npc.select( NPC.gid );
	assert.equal( panel.observe( npc.state(), npc.window() ), true );
	assert.equal( panel.hidden(), false, "a new selection shows even when the HUD missed the closed state" );
	assert.equal( panel.top(), 0, "the new selection rebuilds the menu even when its text is identical" );
});
