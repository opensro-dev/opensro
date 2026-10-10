/*
===========================================================================

ui-panel-sweep.test.mjs - every registered window publishes unique controls

BUG-064 hid in windows no test opened: a frame close button and the window's
own Cancel shared one control id. The fixture refuses duplicate ids on every
publication, so opening each registered panel through the production HUD
proves the whole class. A window must actually open (new controls appear).
Most open from their window id; service windows open through the flow that
opens them in game (a storage room, an official's answer, a used scroll).
SERVICE_WINDOWS adds the windows no registered panel opens (Global chat
crashed with a frame close and Cancel sharing "wholechat-exit").

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { uiPanels } = await import( "../../src/engine/foundation/ui/panels.ts" );
const { emptyStall, stallRequest } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { fortressPacket } = await import( "../../src/engine/foundation/gameplay/fortress.ts" );

const OFFICIAL = 17;
// A type word isGlobalChatItem accepts (global-chat.ts): TID 3/3/5/2.
const GLOBAL_CHAT_TYPE = 0x29ec;
const BAG_SLOT = 13;

/** @typedef {{ state: any, ui: { event: ( event: any ) => void } }} Fixture */

/*
================
talkTo

An open talk menu with a fortress official, the state every fortress
service window is requested from.
================
*/
/** @param {Fixture} f */
function talkTo( f ) {
	f.state.entities.push( { ...f.state.entities[0], gid: OFFICIAL, refObjId: 2011, kind: "npc", name: "Official" } );
	Object.assign( f.state.gameplay, { target: OFFICIAL, npcConversation: { phase: "menu", gid: OFFICIAL } } );
}

/*
================
OPENERS

How a window opens when its window id alone does not open it. Each returns
after the event that opens the window; the sweep then settles the HUD.
================
*/
/** @type {Record<string, (f: Fixture, step: () => void) => void>} */
const OPENERS = {
	"COS inventory"( f ) {
		f.state.gameplay.cosRecords = [ { gid: 7, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false } ];
		f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
	},
	"Storage"( f ) {
		// A storage room the worker opened (a warehouse ticket or the talk menu).
		// A new gameplay object: the HUD opens the room on its arrival.
		f.state.gameplay = {
			...f.state.gameplay,
			storage: { npc: OFFICIAL, phase: "open", capacity: 150, gold: "0", items: [] }
		};
		f.ui.event( { kind: "activate", id: "open-window:Storage" } );
	},
	"Academy Matching"( f ) {
		f.state.gameplay.academy = { member: false, rows: [], request: null, page: 0 };
		f.ui.event( { kind: "activate", id: "open-window:Academy Matching" } );
	},
	"Stall network"( f ) {
		f.state.gameplay.stall = stallRequest( emptyStall(), { kind: "stall-network-open", open: true } ).state;
		f.ui.event( { kind: "activate", id: "open-window:Stall network" } );
	},
	"Skin change"( f, step ) {
		Object.assign( f.state.entities[0], { refObjId: 1907, bodyShape: 0x22 } );
		Object.assign( f.state.gameplay, {
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			playerModels: [ { refObjId: 1907, sex: 1 }, { refObjId: 1920, sex: 0 } ],
			inventory: [ {
				slot: 13,
				refObjId: 1,
				typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (9 << 11),
				quantity: 1,
				name: "Skin scroll",
				icon: "item/etc/hp_potion_01.ddj"
			} ]
		} );
		f.ui.event( { kind: "key", code: "KeyI" } );
		step();
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
	},
	"Fortress war application"( f, step ) {
		talkTo( f );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-war" } );
		f.state.gameplay.fortressApplication = { warStart: null, applied: null, sequence: 1 };
	},
	"Job ranking"( f ) {
		// A 0xB37E answer opens its window (job-hud.ts observe).
		f.state.gameplay.jobRanks = {
			lists: [ { job: 1, kind: 0, rows: [ { rank: 1, alias: "Trader", grade: 2, value: 40 } ] } ],
			opened: { job: 1, kind: 0, sequence: 1 }
		};
	},
	"Fortress war schedule"( f, step ) {
		talkTo( f );
		Object.assign( f.state.gameplay, {
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME" } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-schedule" } );
		f.state.gameplay.fortress = {
			...f.state.gameplay.fortress,
			serviceSequence: 1,
			service: { action: 5, result: 1, applicants: [] }
		};
	},
	"Fortress tax"( f, step ) {
		// The manager's tax row shows the window at once (5D8930 action 0x33 row 1).
		talkTo( f );
		Object.assign( f.state.gameplay, {
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME", taxTargets: 63 } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
		f.state.gameplay.fortress = {
			...f.state.gameplay.fortress,
			serviceSequence: 1,
			service: { action: 0, result: 1, fortress: 1, taxRate: 10, gold: "1234" }
		};
	},
	"Fortress production"( f, step ) {
		// The trainer's row queries; the answer opens the window (754A40 0x11).
		talkTo( f );
		Object.assign( f.state.gameplay, {
			targetCapabilities: 0x4000000,
			fortressForge: [ { refObjId: 9001, gold: 1, gp: 1, minutes: 1, staff: "trainer", name: "Cart" } ],
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME", taxTargets: 63 } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-production:trainer" } );
		f.state.gameplay.fortress = fortressPacket( f.state.gameplay.fortress, {
			opcode: 0xb1e1,
			payload: Uint8Array.from( [ 0x11, 1, 1, 0, 0, 0, 0 ] )
		} );
	}
};

/*
================
SERVICE_WINDOWS

Windows drawn outside the panel registry, each opened by its game flow.
================
*/
/** @type {Record<string, (f: Fixture, step: () => void) => void>} */
const SERVICE_WINDOWS = {
	"Global chat"( f ) {
		// 69D4F0: using the Global Chatting item opens CIFWholeChat on its slot.
		Object.assign( f.state.gameplay, {
			inventorySlotCount: 45,
			inventory: [ { slot: BAG_SLOT, refObjId: 24600, typeFlags: GLOBAL_CHAT_TYPE, quantity: 3 } ]
		} );
		f.ui.event( { kind: "double-activate", id: "slot:" + BAG_SLOT } );
	},
	"Exchange"( f ) {
		f.state.gameplay.exchange = {
			open: true,
			partner: 2,
			own: [],
			theirs: [],
			ownGold: 0,
			theirGold: 0,
			ownLocked: false,
			theirLocked: false,
			approved: false,
			requesting: false
		};
	},
	"Stall"( f ) {
		f.state.gameplay.stall = { ...emptyStall(), phase: "owner", title: "Stall" };
	}
};

for ( const panel of [ ...uiPanels(), ...Object.keys( SERVICE_WINDOWS ) ] ) {
	test(`${panel} opens with unique control ids`, t => {
		const f = uiFixture();
		t.after( () => f.dispose() );
		let now = 0;
		// A property, not a local: the closure assigns it, and a narrowed local
		// would read as never at the assertions.
		const seen = {
			/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
		};
		const step = () => {
			for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
		};
		step();
		const before = new Set( seen.presentation?.controls.map( control => control.id ) ?? [] );
		const open = OPENERS[panel] ?? SERVICE_WINDOWS[panel] ??
			(() => f.ui.event( { kind: "activate", id: "open-window:" + panel } ));
		open( f, step );
		step();
		assert.ok(
			seen.presentation?.controls.some( control => !before.has( control.id ) ),
			`${panel} published no new controls: the window did not open`
		);
	});
}
