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
import { defined } from "../helpers/defined.mjs";
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
	for ( let time = 0; time < 300; time += 100 ) f.ui.step( f.state, time );
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
		// No party: the party row is not offered (#4391).
		assert.deepEqual( boxIds( shown ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-user",
			"reverse-scroll-cancel"
		] );
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

/*
================
buttonArt

The return button art painted over a control's whole rect: "", "_focus"
or "_press", or null when another image is there.
================
*/
function buttonArt( f, result, id ) {
	const rect = result.controls.find( c => c.id === id ).rect.join( "," );
	const quad = f.scenes.at( -1 ).quads.find( q =>
		q.rect.join( "," ) === rect && q.texture?.includes( "/interface/ifcommon/com_return_button" )
	);
	return quad ? defined( /com_return_button(_focus|_press)?\.png/.exec( quad.texture ) )[1] ?? "" : null;
}

test("the box stacks its return buttons; the one under the pointer shows its focus art", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		const idle = defined( f.ui.step( f.state, 1300 ) );
		// 52F460 types 0x21/0x24: 252 x 24 buttons at x 25, y 76, then one
		// row pitch (31) per button under it.
		const frame = defined( idle.controls.find( c => c.id === "reverse-scroll:2" ) ).rect;
		assert.deepEqual(
			boxIds( idle ).map( id => defined( idle.controls.find( c => c.id === id ) ).rect ),
			[ 0, 31, 62, 93 ].map( dy => [ frame[0], frame[1] + dy, 252, 24 ] )
		);
		for ( const id of boxIds( idle ) ) assert.equal( buttonArt( f, idle, id ), "", id + " at rest" );
		f.ui.event( { kind: "hover", id: "reverse-scroll:3" } );
		f.ui.step( f.state, 1400 );
		const hovered = f.presented();
		assert.equal( buttonArt( f, hovered, "reverse-scroll:3" ), "_focus" );
		assert.equal( buttonArt( f, hovered, "reverse-scroll:2" ), "" );
		f.ui.event( { kind: "press", id: "reverse-scroll:3" } );
		f.ui.step( f.state, 1500 );
		assert.equal( buttonArt( f, f.presented(), "reverse-scroll:3" ), "_press" );
		f.ui.event( { kind: "press", id: null } );
		f.ui.event( { kind: "hover", id: "reverse-scroll-cancel" } );
		f.ui.step( f.state, 1600 );
		const cancel = f.presented();
		assert.equal( buttonArt( f, cancel, "reverse-scroll-cancel" ), "_focus" );
		assert.equal( buttonArt( f, cancel, "reverse-scroll:3" ), "", "the focus stayed after the pointer left" );
		assert.equal( sent.filter( row => row.kind === "gameplay" ).length, 0 );
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
		assert.equal( boxIds( f.ui.step( f.state, 1300 ) ).length, 4 );
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

for ( const change of [ "replacement", "travel", "character" ] ) {
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
// A busy bag is one round trip, not a reason to drop the choice
// ============================================================================

test("the box survives a busy inventory", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		const before = f.ui.step( f.state, 1300 );
		assert.equal( boxIds( defined( before ) ).length, 4, "the box is up before the bag is busy" );
		/** @type {any} */ (f.state.gameplay).inventoryPending = true;
		const busy = f.ui.step( f.state, 1400 );
		/** @type {any} */ (f.state.gameplay).inventoryPending = false;
		// A frame that changed nothing repaints nothing: the last scene stands.
		const shown = defined( f.ui.step( f.state, 1500 ) ?? busy ?? before );
		assert.deepEqual( boxIds( shown ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-user",
			"reverse-scroll-cancel"
		] );
		f.ui.event( { kind: "activate", id: "reverse-scroll:3" } );
		assert.deepEqual( sent.filter( row => row.kind === "gameplay" ), [
			{ kind: "gameplay", command: { kind: "item-use", slot: 13, reverseChoice: 3 } }
		] );
	} finally {
		f.dispose();
	}
});

test("an answer during a busy inventory is held, not lost", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		const before = f.ui.step( f.state, 1300 );
		/** @type {any} */ (f.state.gameplay).inventoryPending = true;
		f.ui.step( f.state, 1400 );
		f.ui.event( { kind: "activate", id: "reverse-scroll:3" } );
		const held = f.ui.step( f.state, 1500 ) ?? before;
		assert.equal( sent.filter( row => row.kind === "gameplay" ).length, 0, "the answer waited for the bag" );
		assert.equal( boxIds( defined( held ) ).length, 4, "the box stayed up" );
		/** @type {any} */ (f.state.gameplay).inventoryPending = false;
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "activate", id: "reverse-scroll:3" } );
		assert.deepEqual( sent.filter( row => row.kind === "gameplay" ), [
			{ kind: "gameplay", command: { kind: "item-use", slot: 13, reverseChoice: 3 } }
		] );
	} finally {
		f.dispose();
	}
});

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

test("with no published table the box keeps its two native points", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1300 ) ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-user",
			"reverse-scroll-cancel"
		] );
		// The map row is not offered, so naming it leaves the box as it was.
		f.ui.event( { kind: "activate", id: "reverse-scroll-map" } );
		const after = f.ui.step( f.state, 1400 );
		assert.ok( !after || boxIds( after ).length === 4, "the box stays up" );
		assert.ok( !after?.controls.some( c => c.id.startsWith( "reverse-map-point:" ) ), "no map points" );
	} finally {
		f.dispose();
	}
});

test("with a published table, the map row picks a point and sends choice 7 with its id", () => {
	const sent = [], f = withMapTable( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1300 ) ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-map",
			"reverse-scroll-user",
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

test("the map choice survives a busy inventory", () => {
	const sent = [], f = withMapTable( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-map" } );
		let scene = f.ui.step( f.state, 1400 );
		for (
			let time = 1500;
			time < 3000 && !scene?.controls.some( c => c.id === "reverse-map-point:1" );
			time += 100
		) {
			scene = f.ui.step( f.state, time ) ?? scene;
		}
		assert.ok( scene?.controls.some( c => c.id === "reverse-map-point:1" ) );
		/** @type {any} */ (f.state.gameplay).inventoryPending = true;
		const busy = f.ui.step( f.state, 3100 );
		/** @type {any} */ (f.state.gameplay).inventoryPending = false;
		const after = defined( f.ui.step( f.state, 3200 ) ?? busy ?? scene );
		assert.ok(
			after.controls.some( c => c.id === "reverse-map-point:1" ),
			"the map still draws the reverse point after the bag went busy"
		);
		f.ui.event( { kind: "activate", id: "reverse-map-point:1" } );
		f.ui.step( f.state, 3300 );
		f.ui.event( { kind: "activate", id: "map-teleport-confirm" } );
		assert.deepEqual( sent.filter( row => row.kind === "gameplay" ), [
			{ kind: "gameplay", command: { kind: "item-use", slot: 13, reverseChoice: 7, reverseMapPoint: 1 } }
		] );
	} finally {
		f.dispose();
	}
});

test("a reverse confirm waits for a busy inventory", () => {
	const sent = [], f = withMapTable( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-map" } );
		let scene = f.ui.step( f.state, 1400 );
		for (
			let time = 1500;
			time < 3000 && !scene?.controls.some( c => c.id === "reverse-map-point:1" );
			time += 100
		) {
			scene = f.ui.step( f.state, time ) ?? scene;
		}
		f.ui.event( { kind: "activate", id: "reverse-map-point:1" } );
		const asked = defined( f.ui.step( f.state, 3100 ) ?? scene );
		assert.ok( asked.controls.some( c => c.id === "map-teleport-confirm" ), "the confirm is up" );
		/** @type {any} */ (f.state.gameplay).inventoryPending = true;
		const busy = f.ui.step( f.state, 3200 );
		f.ui.event( { kind: "activate", id: "map-teleport-confirm" } );
		// The confirmation owns the control list while it is up, so the answer
		// must keep it, not throw the point away. The held confirm is proven by
		// the second answer below sending the reverse item-use, not a warp.
		const held = defined( f.ui.step( f.state, 3300 ) ?? busy ?? asked );
		assert.equal( sent.filter( row => row.kind === "gameplay" ).length, 0, "the confirm waited for the bag" );
		assert.ok( held.controls.some( c => c.id === "map-teleport-confirm" ), "the confirm stayed up" );
		assert.ok( f.hasText( "Move here?" ), "the reverse confirm is still the one asked" );
		/** @type {any} */ (f.state.gameplay).inventoryPending = false;
		f.ui.step( f.state, 3400 );
		f.ui.event( { kind: "activate", id: "map-teleport-confirm" } );
		assert.deepEqual( sent.filter( row => row.kind === "gameplay" ), [
			{ kind: "gameplay", command: { kind: "item-use", slot: 13, reverseChoice: 7, reverseMapPoint: 1 } }
		] );
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

// ============================================================================
// Later-native (#3903): the party and user destinations
// ============================================================================

const {
	decodeReverseUserPoint,
	reversePartyTail,
	reverseReturnFailureKey
} = await import( "../../src/engine/foundation/gameplay/reverse-user-point.ts" );

/*
================
withParty

The open fixture in a party with two other members.
================
*/
function withParty( f ) {
	f.state.gameplay = /** @type {any} */ ({
		...f.state.gameplay,
		social: {
			...(f.state.gameplay.social ?? {}),
			members: [ { id: 42, name: "Mate" }, { id: 43, name: "Other" } ]
		}
	});
	return f;
}

test("the party row lists the members and a member sends choice 4 with its gid", () => {
	const sent = [], f = withParty( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-party" } );
		const shown = defined( f.ui.step( f.state, 1400 ) );
		assert.deepEqual(
			shown.controls.map( c => c.id ).filter( id => id.startsWith( "reverse-party:" ) ),
			[ "reverse-party:42", "reverse-party:43" ]
		);
		assert.equal( sent.length, 0, "no use until a member is picked" );
		f.ui.event( { kind: "activate", id: "reverse-party:43" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 4, reversePartyGid: 43 }
		} );
	} finally {
		f.dispose();
	}
});

test("with no party the box offers no party row, and naming it opens nothing (#4391)", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		const shown = defined( f.ui.step( f.state, 1300 ) );
		assert.ok( !boxIds( shown ).includes( "reverse-scroll-party" ), "no party row without a party" );
		// Even named directly, the party window never opens onto an
		// empty member list: nothing repaints, the box stands as it was.
		f.ui.event( { kind: "activate", id: "reverse-scroll-party" } );
		const after = f.ui.step( f.state, 1400 ) ?? shown;
		assert.deepEqual( boxIds( after ), boxIds( shown ) );
		assert.ok( !after.controls.some( c => c.id.startsWith( "reverse-party:" ) ), "no member buttons" );
		assert.equal( sent.length, 0, "no use sent" );
	} finally {
		f.dispose();
	}
});

test("a party of only the player offers no party row (#4391)", () => {
	const sent = [], f = open( sent );
	try {
		const gid = f.state.gameplay.localGid;
		f.state.gameplay = /** @type {any} */ ({
			...f.state.gameplay,
			social: { ...(f.state.gameplay.social ?? {}), members: [ { id: gid, name: "Self" } ] }
		});
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		const shown = defined( f.ui.step( f.state, 1300 ) );
		assert.ok( !boxIds( shown ).includes( "reverse-scroll-party" ), "no party row with nobody to move to" );
	} finally {
		f.dispose();
	}
});

test("the party window returns to the box when the last member leaves (#4391)", () => {
	const sent = [], f = withParty( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-party" } );
		f.ui.step( f.state, 1400 );
		f.state.gameplay = /** @type {any} */ ({ ...f.state.gameplay, social: { members: [] } });
		const shown = defined( f.ui.step( f.state, 1500 ) );
		assert.ok( boxIds( shown ).includes( "reverse-scroll:2" ), "back at the box" );
		assert.ok( !shown.controls.some( c => c.id.startsWith( "reverse-party:" ) ), "no member buttons" );
		assert.equal( sent.length, 0, "no use sent" );
	} finally {
		f.dispose();
	}
});

test("the party window's cancel returns to the box, and Escape leaves the window first", () => {
	const sent = [], f = withParty( open( sent ) );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-party" } );
		f.ui.step( f.state, 1400 );
		f.ui.event( { kind: "activate", id: "reverse-window-close" } );
		assert.deepEqual( boxIds( defined( f.ui.step( f.state, 1500 ) ) ), [
			"reverse-scroll:2",
			"reverse-scroll:3",
			"reverse-scroll-party",
			"reverse-scroll-user",
			"reverse-scroll-cancel"
		] );
		f.ui.event( { kind: "activate", id: "reverse-scroll-user" } );
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.equal( boxIds( defined( f.ui.step( f.state, 1700 ) ) ).length, 5, "Escape returned to the box" );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( boxIds( f.ui.step( f.state, 1800 ) ), [], "a second Escape closed the box" );
		assert.equal( sent.filter( row => row.kind === "gameplay" ).length, 0 );
	} finally {
		f.dispose();
	}
});

test("the user window enrolls without a location, and asks before it overwrites one", () => {
	const sent = [], f = open( sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-user" } );
		const shown = defined( f.ui.step( f.state, 1400 ) );
		assert.ok( shown.controls.some( c => c.id === "reverse-user-enroll" ), "the enroll button is up" );
		assert.ok( !shown.controls.some( c => c.id === "reverse-user-move" ), "no move button without a location" );
		f.ui.event( { kind: "activate", id: "reverse-user-enroll" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 6 }
		} );
		// With a location told, the enroll asks first, then sends.
		f.state.gameplay = /** @type {any} */ ({
			...f.state.gameplay,
			reverseUserPoint: { regionId: 25000, x: 10, y: 2, z: 30 }
		});
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1500 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-user" } );
		const saved = defined( f.ui.step( f.state, 1600 ) );
		assert.ok( saved.controls.some( c => c.id === "reverse-user-move" ), "the move button is up" );
		const sentBefore = sent.length;
		f.ui.event( { kind: "activate", id: "reverse-user-enroll" } );
		f.ui.step( f.state, 1700 );
		assert.equal( sent.length, sentBefore, "the enroll asked before overwriting" );
		f.ui.event( { kind: "activate", id: "reverse-user-enroll" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 6 }
		} );
	} finally {
		f.dispose();
	}
});

test("the user window's move button sends choice 5", () => {
	const sent = [], f = open( sent );
	try {
		f.state.gameplay = /** @type {any} */ ({
			...f.state.gameplay,
			reverseUserPoint: { regionId: 25000, x: 10, y: 2, z: 30 }
		});
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "reverse-scroll-user" } );
		f.ui.step( f.state, 1400 );
		f.ui.event( { kind: "activate", id: "reverse-user-move" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, reverseChoice: 5 }
		} );
	} finally {
		f.dispose();
	}
});

test("the worker sends the party, user and enroll choices as their tails", () => {
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
	const word = [ REVERSE_SCROLL & 255, REVERSE_SCROLL >>> 8 ];
	gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 4, reversePartyGid: 0x01020304 }, 1, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x75bd, payload: Uint8Array.of( 21, ...word, 4, 4, 3, 2, 1 ) } );
	// The party flight's refusal byte reads as the left-party line; the
	// same byte with no flight is the native mapper's business.
	gameplay.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xf6 ) }, 1 );
	assert.equal(
		defined( defined( gameplay.take() ).notices ).at( -1 )?.key,
		"UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE"
	);
	gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 5 }, 1, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x75bd, payload: Uint8Array.of( 21, ...word, 5 ) } );
	gameplay.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 0xfa ) }, 1 );
	assert.equal(
		defined( defined( gameplay.take() ).notices ).at( -1 )?.key,
		"UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_2"
	);
	gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 6 }, 1, undefined );
	assert.deepEqual( sent.at( -1 ), { opcode: 0x75bd, payload: Uint8Array.of( 21, ...word, 6 ) } );
	// An enroll's success answer is the set line (and spends nothing).
	gameplay.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 1, 21, 2, 0, ...word ) }, 1 );
	assert.equal(
		defined( defined( gameplay.take() ).notices ).at( -1 )?.key,
		"UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_APPOINT_SELECTION_END"
	);
	assert.throws( () => gameplay.command( { kind: "item-use", slot: 21, reverseChoice: 4 }, 1, undefined ) );
	gameplay.dispose();
});

test("the saved location body decodes whole or not at all", () => {
	assert.equal( decodeReverseUserPoint( Uint8Array.of( 0 ) ), null );
	const body = new Uint8Array( 15 );
	const view = new DataView( body.buffer );
	body[0] = 1;
	view.setUint16( 1, 25000, true );
	view.setFloat32( 3, 900.5, true );
	view.setFloat32( 7, 12, true );
	view.setFloat32( 11, 640.25, true );
	assert.deepEqual( decodeReverseUserPoint( body ), { regionId: 25000, x: 900.5, y: 12, z: 640.25 } );
	assert.throws( () => decodeReverseUserPoint( Uint8Array.of( 1, 2 ) ) );
	assert.throws( () => decodeReverseUserPoint( Uint8Array.of( 2 ) ) );
	assert.throws( () => decodeReverseUserPoint( new Uint8Array( 0 ) ) );
	assert.deepEqual( [ ...reversePartyTail( 0x01020304 ) ], [ 4, 4, 3, 2, 1 ] );
	assert.throws( () => reversePartyTail( 0 ) );
});

test("the refusal bytes name the later client's lines", () => {
	assert.equal( reverseReturnFailureKey( 0xf6 ), "UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE" );
	assert.equal( reverseReturnFailureKey( 0xf8 ), "UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE_1" );
	assert.equal( reverseReturnFailureKey( 0xfa ), "UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_2" );
	assert.equal( reverseReturnFailureKey( 0xfb ), "UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_3" );
	assert.equal( reverseReturnFailureKey( 0x85 ), undefined );
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
