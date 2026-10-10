/*
===========================================================================

reverse-return-scroll.test.mjs - the reverse return scroll's choice box

A use of ITEM_MALL_REVERSE_RETURN_SCROLL (3/3/3/3) from the bag opens
message box 0x1E; CGInterface_OnMsgBoxResult (6971B0) runs the use with
the picked point as one byte after the type word. Driven through the
production HUD and the worker's gameplay owner.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

// An etc item (3/3) of type 3, type 4 = 3.
const REVERSE_SCROLL = 3 << 2 | 3 << 5 | 3 << 7 | 3 << 11;

/*
================
open

A fixture holding a reverse return scroll in bag slot 13, inventory open.
================
*/
function open( sent ) {
	const f = uiFixture( command => sent.push( command ) );
	// The fixture's state types its inventory empty; this one holds a row.
	f.state.gameplay = /** @type {any} */ ({
		...f.state.gameplay,
		inventorySlotCount: 58,
		equipmentSlotCount: 13,
		inventory: [ {
			slot: 13,
			refObjId: 3795,
			typeFlags: REVERSE_SCROLL,
			quantity: 2,
			name: "Reverse return scroll",
			icon: "item/etc/hp_potion_01.ddj"
		} ]
	});
	for ( let time = 0; time < 1200; time += 100 ) f.ui.step( f.state, time );
	f.ui.event( { kind: "key", code: "KeyI" } );
	f.ui.step( f.state, 1200 );
	return f;
}

/*
================
boxIds
================
*/
function boxIds( result ) {
	return result.controls.map( c => c.id ).filter( id => id.startsWith( "reverse-scroll" ) );
}

test("a bag use asks for the point, and a row sends the use with it", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.equal( sent.length, 0, "the use waited for its point" );
		const shown = f.ui.step( f.state, 1300 );
		assert.deepEqual( boxIds( shown ), [ "reverse-scroll:2", "reverse-scroll:3", "reverse-scroll-cancel" ] );
		assert.ok( f.hasText( "Select the location used by the reverse return scroll." ) );
		f.ui.event( { kind: "activate", id: "reverse-scroll:3" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 3 }
		} );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1400 ) ), [] );
	} finally {
		f.dispose();
	}
});

test("cancel and Escape close the box without a use", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-cancel" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1400 ) ), [] );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1500 );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1600 ) ), [] );
		assert.equal( sent.filter( row => row.kind === "gameplay" && row.command.kind === "item-use" ).length, 0 );
	} finally {
		f.dispose();
	}
});

test("the worker sends the point as the byte after the type word", () => {
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( {
		refItemSnapshot: [ { refObjId: 3795, typeFlags: REVERSE_SCROLL } ],
		equipItems: [ { refObjId: 3795, slot: 21, body: [ 0xd3, 0x0e, 0, 0, 2, 0 ] } ]
	} );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Author"
	} );
	gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 2 }, 1, undefined );
	assert.deepEqual( sent, [ {
		opcode: 0x75bd,
		payload: Uint8Array.of( 21, REVERSE_SCROLL & 255, REVERSE_SCROLL >>> 8, 2 )
	} ] );
	gameplay.dispose();
});

test("right-click opens a modal choice without toggling the inventory behind it", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		assert.equal( boxIds( f.ui.step( f.state, 1300 ) ).length, 3 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.event( { kind: "activate", id: "reverse-scroll-cancel" } );
		const restored = f.ui.step( f.state, 1400 );
		assert.ok( restored );
		assert.ok( restored.controls.some( c => c.id === "slot:13" ) );
		assert.equal( sent.filter( row => row.kind === "gameplay" && row.command.kind === "item-use" ).length, 0 );
	} finally {
		f.dispose();
	}
});

test("a choice cannot survive leaving the world and reopening the same inventory", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		const session = f.state.session;
		f.state.session = { ...session, phase: "signed-out" };
		f.ui.step( f.state, 1400 );
		f.state.session = session;
		assert.deepEqual( boxIds( f.ui.step( f.state, 1500 ) ), [] );
		assert.equal( sent.filter( row => row.kind === "gameplay" && row.command.kind === "item-use" ).length, 0 );
	} finally {
		f.dispose();
	}
});

for ( const change of [ "replacement", "travel", "character", "pending" ] ) {
	test(`the pending choice is discarded on ${change}`, () => {
		const sent = [], f = open( sent );
		try {
			f.ui.event( { kind: "right-activate", id: "slot:13" } );
			f.ui.step( f.state, 1300 );
			/** @type {import("../../src/engine/contracts/ui").UiView} */
			let state = /** @type {any} */ (f.state);
			const game = state.gameplay;
			assert.ok( game );
			if ( change === "replacement" ) {
				state = {
					...state,
					gameplay: {
						...game,
						inventory: game.inventory.map( row => ({ ...row, refObjId: row.refObjId + 1 }) )
					}
				};
			}
			if ( change === "character" ) {
				state = { ...state, gameplay: { ...game, localGid: (game.localGid ?? 0) + 1 } };
			}
			if ( change === "pending" ) state = { ...state, gameplay: { ...game, inventoryPending: true } };
			if ( change === "travel" ) state = { ...state, travel: { mode: 1, region: 257 } };
			const cleared = f.ui.step( state, 1400 );
			state = { ...state, travel: null, gameplay: game };
			// A null result keeps the last published scene; it is not a new empty scene.
			assert.deepEqual( boxIds( f.ui.step( state, 1500 ) ?? cleared ), [] );
			assert.equal( sent.filter( row => row.kind === "gameplay" && row.command.kind === "item-use" ).length, 0 );
		} finally {
			f.dispose();
		}
	});
}

// ============================================================================
// Port-only, not native: the map choice (reverse-return-map.ts)
// ============================================================================

const { decodeReverseMapPoints } = await import( "../../src/engine/foundation/gameplay/reverse-return-map.ts" );

/*
================
withMapTable

The open fixture with a published reverse return map table: one point at
the local player's own position, so the map centred there shows it.
================
*/
function withMapTable( f ) {
	const pose = { regionId: 0x62a8, x: 900, y: 0, z: 900, angle: 0 };
	f.state.gameplay = /** @type {any} */ ({
		...f.state.gameplay,
		pose,
		reverseMapPoints: [ { id: 1, name: "Jangan", regionId: pose.regionId, x: pose.x, y: pose.y, z: pose.z } ]
	});
	return f;
}

test("with the Experimental row off the box keeps its two native points", () => {
	const sent = [], f = withMapTable( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1300 ) ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-cancel"
		] );
		f.ui.event( { kind: "activate", id: "reverse-scroll-map" } );
		assert.notEqual( f.ui.step( f.state, 1400 )?.panel, "Map" );
	} finally {
		f.dispose();
	}
});

test("with the row on, the map row picks a published point and sends choice 7 with its id", () => {
	const sent = [], f = withMapTable( open( sent ) );
	try {
		f.ui.event( { kind: "experimental-preferences", value: { reverseReturnMap: true } } );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1300 ) ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-map",
			"reverse-scroll-cancel"
		] );
		f.ui.event( { kind: "activate", id: "reverse-scroll-map" } );
		let scene = f.ui.step( f.state, 1400 );
		for (
			let time = 1500;
			time < 3000 && !scene?.controls.some( c => c.id === "reverse-map-point:1" );
			time += 100
		) {
			scene = f.ui.step( f.state, time ) ?? scene;
		}
		assert.ok( scene?.controls.some( c => c.id === "reverse-map-point:1" ), "the map shows the published point" );
		f.ui.event( { kind: "activate", id: "reverse-map-point:1" } );
		f.ui.step( f.state, 3100 );
		assert.ok( f.hasText( "Jangan" ) && f.hasText( "Move here?" ) );
		assert.equal( sent.filter( row => row.kind === "gameplay" && row.command.kind === "item-use" ).length, 0 );
		f.ui.event( { kind: "activate", id: "map-teleport-confirm" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 7, reverseMapPoint: 1 }
		} );
	} finally {
		f.dispose();
	}
});

test("the worker sends choice 7 and the point id as a u32 after the type word", () => {
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( {
		refItemSnapshot: [ { refObjId: 3795, typeFlags: REVERSE_SCROLL } ],
		equipItems: [ { refObjId: 3795, slot: 21, body: [ 0xd3, 0x0e, 0, 0, 2, 0 ] } ],
		reverseMapPoints: [ { id: 1, name: "Jangan", regionId: 25000, x: 900, y: 0, z: 900 } ]
	} );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Author"
	} );
	gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 7, reverseMapPoint: 0x01020304 }, 1, undefined );
	assert.deepEqual( sent, [ {
		opcode: 0x75bd,
		payload: Uint8Array.of( 21, REVERSE_SCROLL & 255, REVERSE_SCROLL >>> 8, 7, 4, 3, 2, 1 )
	} ] );
	gameplay.dispose();
});

test("the published table is checked: ids in order, field regions, local coordinates", () => {
	assert.deepEqual( decodeReverseMapPoints( undefined ), [] );
	const good = { id: 1, name: "Jangan", regionId: 25000, x: 900, y: 0, z: 900 };
	assert.equal( decodeReverseMapPoints( [ good ] ).length, 1 );
	for (
		const bad of [
			{ ...good, id: 2 },
			{ ...good, regionId: 0x8000 | 25000 },
			{ ...good, x: 1920 },
			{ ...good, z: -1 },
			{ ...good, y: Infinity },
			{ ...good, name: 5 }
		]
	) assert.throws( () => decodeReverseMapPoints( [ bad ] ) );
	assert.throws( () => decodeReverseMapPoints( "points" ) );
});
