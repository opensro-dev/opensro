/*
===========================================================================
ui-entry-recipes.mjs - shared synthetic admission recipes for production UI

Used by the runtime panel sweep and local browser QA. No rendering, permission
logic or service transport lives here. Async steps allow real assets to settle.
===========================================================================
*/
const { emptyStall, stallRequest } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { fortressPacket } = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const { fortressProductionQuery } = await import( "../../src/engine/foundation/gameplay/fortress-production.ts" );
const { npcGid } = await import( "../../src/engine/foundation/gameplay/npc-dialogue.ts" );
const { createMagicOptionGrant } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/magic-option/magic-option.ts"
);

const OFFICIAL = 17;
// A type word isGlobalChatItem accepts (global-chat.ts): TID 3/3/5/2.
const GLOBAL_CHAT_TYPE = 0x29ec;
const BAG_SLOT = 13;

/** @typedef {import("./ui-entry-recipes.mjs").EntryFixture} Fixture */

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
	Object.assign( f.state.gameplay, {
		target: OFFICIAL,
		npcConversation: { phase: "menu", gid: npcGid( OFFICIAL ), dialogueRevision: 0 }
	} );
}

/*
================
OPENERS

How a window opens when its window id alone does not open it. Each returns
after the event that opens the window; the sweep then settles the HUD.
================
*/
/** @type {Record<string, (f: Fixture, step: () => void | Promise<void>) => void | Promise<void>>} */
export const OPENERS = {
	"Magic option"( f ) {
		// avatar-magic-option.test.mjs: the real owner admits only its B338 lock.
		// This seeds a completed lock; it does not claim a visible NPC entry flow.
		const grant = createMagicOptionGrant();
		/** @type {Map<number, import("../../src/engine/foundation/gameplay/item-tooltip-reference.ts").ItemMagicReference>} */
		const references = new Map( [ [ 246, {
			paramId: 246,
			optionName: "MATTR_AVATAR_STR",
			paramName: "+",
			degree: 1,
			rangeWords: [ 1, 1, 1 ]
		} ] ] );
		grant.bootstrap( [ { part: 1, options: [ "MATTR_AVATAR_STR" ] } ], references );
		grant.open( OFFICIAL );
		grant.opened( Uint8Array.of( 1, 0, 0, 0, 128 ) );
		const hat = {
			slot: 14,
			refObjId: 9001,
			typeFlags: 3 << 2 | 1 << 5 | 13 << 7 | 1 << 11,
			quantity: 1,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: [],
			magicReferences: [ ...references.values() ],
			tooltip: { fields: { maxMagicOptions51c: 2 } }
		};
		grant.take( hat );
		f.state.gameplay = { ...f.state.gameplay, inventory: [ hat ], magicOption: grant.state() };
	},
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
		f.state.gameplay.academy = { member: false, rows: [], request: null, page: 0, pages: 0, result: null };
		f.ui.event( { kind: "activate", id: "open-window:Academy Matching" } );
	},
	"Academy honor"( f ) {
		// The window opens on a new honorRanks publication, as in the panel sweep.
		f.state.gameplay.academy = {
			member: false,
			rows: [],
			request: null,
			page: 0,
			pages: 0,
			result: null,
			honorRanks: {
				rows: [ { ranking: 1, rank: 1, name: "Mentor", level: 60, maxLevel: 80, graduates: 3, guild: "" } ],
				sequence: 1
			}
		};
	},
	"Shop"( f ) {
		// Repair buttons publish only for a repair-capable target (capability 8).
		f.state.gameplay.targetCapabilities = 8;
		f.ui.event( { kind: "activate", id: "open-window:Shop" } );
	},
	"Magic Pang"( f ) {
		// The prize slots publish only from the seeded prize rows (entries 1..12).
		f.state.gameplay.gacha = {
			visible: true,
			phase: "idle",
			npc: 0,
			slot: null,
			entry: 0,
			started: 0,
			result: null,
			error: null,
			prizes: [
				{
					entry: 1,
					refObjId: 3630,
					quantity: 1,
					nameSymbol: "SN_GACHA_PRIZE_1",
					icon: "item/etc/hp_potion_01.ddj"
				},
				{
					entry: 2,
					refObjId: 3630,
					quantity: 2,
					nameSymbol: "SN_GACHA_PRIZE_2",
					icon: "item/etc/hp_potion_01.ddj"
				}
			]
		};
		f.ui.event( { kind: "activate", id: "open-window:Magic Pang" } );
	},
	"Stall network"( f ) {
		f.state.gameplay.stall = stallRequest( emptyStall(), { kind: "stall-network-open", open: true } ).state;
		f.ui.event( { kind: "activate", id: "open-window:Stall network" } );
	},
	async "Skin change"( f, step ) {
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
		await step();
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
	},
	async "Fortress war application"( f, step ) {
		talkTo( f );
		Object.assign( f.state.gameplay, {
			targetCapabilities: 0x800000,
			fortress: {
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ {
					id: 1,
					code: "FORTRESS_JANGAN",
					nameStrId: "SN_FORTRESS_JANGAN",
					officialRefObjId: 2011,
					requestFee: 1000000
				} ],
				wars: [ { id: 1, name: "Other Guild", flags: 0 } ],
				registered: [],
				listId: 0
			}
		} );
		await step();
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
	async "Fortress war schedule"( f, step ) {
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
		await step();
		f.ui.event( { kind: "activate", id: "npc-fortress-schedule" } );
		const fortress = f.state.gameplay.fortress;
		if ( !fortress ) throw Error( "Schedule recipe requires its fortress publication" );
		f.state.gameplay.fortress = {
			...fortress,
			serviceSequence: 1,
			service: { action: 5, result: 1, applicants: [] }
		};
	},
	async "Fortress tax"( f, step ) {
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
		await step();
		f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
		const fortress = f.state.gameplay.fortress;
		if ( !fortress ) throw Error( "Tax recipe requires its fortress publication" );
		f.state.gameplay.fortress = {
			...fortress,
			serviceSequence: 1,
			service: { action: 0, result: 1, fortress: 1, taxRate: 10, gold: "1234" }
		};
	},
	async "Fortress production"( f, step ) {
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
		await step();
		f.ui.event( { kind: "activate", id: "npc-fortress-production:trainer" } );
		const fortress = f.state.gameplay.fortress;
		if ( !fortress ) throw Error( "Production recipe requires its fortress publication" );
		fortress.production = fortressProductionQuery( undefined, "trainer", 1, 1 );
		const answer = fortressPacket( fortress, {
			opcode: 0xb1e1,
			payload: Uint8Array.from( [ 0x11, 1, 1, 0, 0, 0, 0 ] )
		} );
		if ( !answer ) throw Error( "Production recipe answer was not handled" );
		f.state.gameplay.fortress = answer;
	}
};

/*
================
SERVICE_WINDOWS

Windows drawn outside the panel registry, each opened by its game flow.
================
*/
/** @type {Record<string, (f: Fixture, step: () => void | Promise<void>) => void | Promise<void>>} */
export const SERVICE_WINDOWS = {
	"Global chat"( f ) {
		// 69D4F0: using the Global Chatting item opens CIFWholeChat on its slot.
		Object.assign( f.state.gameplay, {
			inventorySlotCount: 45,
			inventory: [ {
				slot: BAG_SLOT,
				refObjId: 24600,
				typeFlags: GLOBAL_CHAT_TYPE,
				quantity: 3,
				plus: 0,
				durability: 0,
				variance: "0",
				magic: []
			} ]
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
