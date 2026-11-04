/*
===========================================================================

storage-gold-dialog.test.mjs - money dialogs after visiting a merchant

Drive the shipped gameplay owners through a shop visit and a warehouse open.
The retained merchant catalog must not block the inventory's deposit button;
only the visible shop panel blocks that button.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";

const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );
const { emptyStall } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { OP_STORAGE_GOLD, OP_STORAGE_LIST, STORAGE_FUNCTION, STORAGE_GOLD_DEPOSIT, STORAGE_GOLD_WITHDRAW } =
	await import(
		"../../src/engine/foundation/gameplay/storage-room.ts"
	);

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const MERCHANT = 17;
const WAREHOUSE = 7;
const SHOP_FUNCTION = 1;
const OP_SELECT_RESULT = 0xb45a;
const OP_NPC_INTERACTION = 0xb338;
const OP_NPC_RELEASE_RESULT = 0xb4b3;
const OP_SHOP_CATALOG = 11;
const SELECT_RESULT_BYTES = 11;
const SELECT_GID_OFFSET = 1;
const SELECT_CAPABILITY_OFFSET = 6;
const INTERACTION_RESULT_BYTES = 5;
const INTERACTION_MASK_OFFSET = 1;
const GOLD_BALANCE_BYTES = 8;
const INVENTORY_SLOT_COUNT = 45;
const EQUIPMENT_SLOT_COUNT = 13;
const STORAGE_CAPACITY = 150;
const CATALOG = { version: 1, npc: MERCHANT, name: "Merchant", offers: [] };

/*
================
dialogFixture

Keep artwork pending: dialog controls and submitted commands must work
without requiring the extracted retail assets for this behavioral test.
================
*/
function dialogFixture() {
	const sent = [];
	const ui = createUi(
		{ available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		message => sent.push( message ),
		() => {},
		() => {},
		"https://fixture.invalid/",
		"https://fixture.invalid/"
	);
	const state = {
		session: { phase: /** @type {const} */ ("world"), revision: 1 },
		gameplay: { localGid: 1, vitals: [], inventory: [], target: 0 },
		entities: [],
		width: 1600,
		height: 900,
		worldReady: true
	};
	return { ui, state, sent };
}

/*
================
storageAfterShopping

Close the merchant's interaction and open storage through real packets, so
the regression uses the catalog that production gameplay actually retains.
================
*/
function storageAfterShopping( raisedGoldCaps ) {
	const game = createGameplay( () => {} );
	const npc = { refObjId: 1, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0, name: "NPC" };
	let now = 0;
	/*
	================
	select
	================
	*/
	function select( gid, capability ) {
		game.command( { kind: "select", gid }, ++now, { ...npc, gid } );
		const grant = Buffer.alloc( SELECT_RESULT_BYTES );
		grant[0] = 1;
		grant.writeUInt32LE( gid, SELECT_GID_OFFSET );
		grant.writeUInt32LE( capability, SELECT_CAPABILITY_OFFSET );
		game.receive( { opcode: OP_SELECT_RESULT, payload: grant }, ++now );
	}
	/*
	================
	opened
	================
	*/
	function opened( capability ) {
		const payload = Buffer.alloc( INTERACTION_RESULT_BYTES );
		payload[0] = 1;
		payload.writeUInt32LE( capability, INTERACTION_MASK_OFFSET );
		game.receive( { opcode: OP_NPC_INTERACTION, payload }, ++now );
	}
	game.bootstrap( {
		simulationProtocolVersion: 1,
		inventorySlotCount: INVENTORY_SLOT_COUNT,
		equipmentSlotCount: EQUIPMENT_SLOT_COUNT,
		character: { gold: "5000" },
		raisedGoldCaps
	} );
	game.seed( { ...npc, gid: 1, kind: "player" } );
	select( MERCHANT, SHOP_FUNCTION );
	game.command( { kind: "shop-open", gid: MERCHANT }, ++now, { ...npc, gid: MERCHANT } );
	opened( SHOP_FUNCTION );
	game.receive( { opcode: OP_SHOP_CATALOG, payload: new TextEncoder().encode( JSON.stringify( CATALOG ) ) }, ++now );
	assert.equal( defined( game.take() ).shop?.npc, MERCHANT );
	game.command( { kind: "npc-close" }, ++now, undefined );
	game.receive( { opcode: OP_NPC_RELEASE_RESULT, payload: Uint8Array.of( 1 ) }, ++now );
	select( WAREHOUSE, STORAGE_FUNCTION );
	game.command( { kind: "storage-open", gid: WAREHOUSE }, ++now, undefined );
	const gold = Buffer.alloc( GOLD_BALANCE_BYTES );
	gold.writeBigUInt64LE( 500n );
	game.receive( { opcode: OP_STORAGE_GOLD, payload: gold }, ++now );
	game.receive( { opcode: OP_STORAGE_LIST, payload: Uint8Array.of( STORAGE_CAPACITY, 0 ) }, ++now );
	opened( STORAGE_FUNCTION );
	const state = defined( game.take() );
	assert.equal( state.storage?.phase, "open" );
	assert.equal( state.shop?.npc, MERCHANT, "the merchant catalog survives the visit to storage" );
	game.dispose();
	return state;
}

for ( const raised of [ false, true ] ) {
	test(`shopping before storage leaves deposit and withdraw usable (raised caps ${raised})`, () => {
		const f = dialogFixture(), { sent } = f;
		try {
			const state = { ...f.state, gameplay: storageAfterShopping( raised ) };
			let now = 0, scene;
			/*
			================
			click
			================
			*/
			function click( id ) {
				f.ui.event( { kind: "activate", id } );
				scene = f.ui.step( state, ++now ) ?? scene;
			}
			scene = f.ui.step( state, now );
			assert.equal( f.ui.stats().panel, "Storage" );
			for (
				const [button, type, amount] of /** @type {[string, number, number][]} */ (
					[ [ "storage-gold", STORAGE_GOLD_WITHDRAW, 500 ], [ "inventory-gold", STORAGE_GOLD_DEPOSIT, 700 ] ]
				)
			) {
				click( button );
				assert.equal(
					defined( scene ).controls.find( control => control.id === "gold-amount" )?.value,
					"0",
					`${button} opens its money dialog`
				);
				assert.equal( sent.length, 0, "opening the modal sends no request" );
				const value = String( amount );
				f.ui.event( {
					kind: "edit",
					id: "gold-amount",
					value,
					start: value.length,
					end: value.length,
					composing: false
				} );
				click( "drop-gold" );
				assert.deepEqual( sent.pop(), {
					kind: "gameplay",
					command: {
						kind: "storage-move",
						move: { type, source: 0, destination: 0, quantity: 0, gold: amount }
					}
				} );
			}
		} finally {
			f.ui.dispose();
		}
	});
}

test("the inventory money button remains blocked by an open shop, exchange, stall or NPC conversation", () => {
	for (
		const [panel, interaction] of /** @type {[string, object][]} */ ([
			[ "Shop", { shop: CATALOG } ],
			[ "Inventory", { exchange: { open: true, own: [], theirs: [] } } ],
			[ "Inventory", { stall: { ...emptyStall(), phase: "owner" } } ],
			[ "Inventory", { stall: { ...emptyStall(), phase: "visitor" } } ],
			[ "Inventory", { npcConversation: { phase: "menu", gid: WAREHOUSE } } ]
		])
	) {
		const f = dialogFixture(), { sent } = f;
		try {
			const state = {
				...f.state,
				gameplay: { ...f.state.gameplay, ...interaction, progression: { gold: "5000", masteries: [] } }
			};
			let scene = f.ui.step( state, 0 );
			f.ui.event( { kind: "activate", id: "open-window:" + panel } );
			scene = f.ui.step( state, 1 ) ?? scene;
			assert.equal( f.ui.stats().panel, panel );
			f.ui.event( { kind: "activate", id: "inventory-gold" } );
			scene = f.ui.step( state, 2 ) ?? scene;
			assert.equal( defined( scene ).controls.some( control => control.id === "gold-amount" ), false );
			assert.equal( sent.length, 0 );
		} finally {
			f.ui.dispose();
		}
	}
});
