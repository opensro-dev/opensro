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
crashed with a frame close and Cancel sharing "wholechat-exit"), and the
pairs a game state opens together (a trade-goods shop beside its transport
bag, whose frames both published "close").

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { uiPanels, panelCloseId, closedPanel } = await import( "../../src/engine/foundation/ui/panels.ts" );
const { emptyStall, stallRequest } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { fortressPacket } = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const { fortressProductionQuery } = await import( "../../src/engine/foundation/gameplay/fortress-production.ts" );

const OFFICIAL = 17;
// A type word isGlobalChatItem accepts (global-chat.ts): TID 3/3/5/2.
const GLOBAL_CHAT_TYPE = 0x29ec;
const BAG_SLOT = 13;
// A trade goods type word (specialty-deal.ts isTradeGoods: TID 3/3/8).
const TRADE_GOODS_TYPE = 0x46c;
// A trader beside the player and the transport the trade goods load into.
const TRADER = 18;
const TRANSPORT = 7;

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
		f.state.gameplay.fortress.production = fortressProductionQuery( undefined, "trainer", 1, 1 );
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
	},
	"Shop with a transport out"( f ) {
		tradeWithTransport( f );
	}
};

/*
================
tradeWithTransport

A trade-goods shop opened with a transport out: the shop names the
transport, and its bag opens beside the shop.
================
*/
/** @param {Fixture} f */
function tradeWithTransport( f ) {
	f.state.entities.push( { ...f.state.entities[0], gid: TRADER, refObjId: 2011, kind: "npc", name: "Trader" } );
	f.state.gameplay.cosRecords = [
		{ gid: TRANSPORT, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false, inventory: [] }
	];
	Object.assign( f.state.gameplay, {
		target: TRADER,
		shop: { npc: TRADER, name: "Trader", offers: [], cosGid: TRANSPORT }
	} );
	f.ui.event( { kind: "activate", id: "open-window:Shop" } );
}

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
		// Each frame's close is named after its own window: a shared "close"
		// was unique only while one window showed (BUG-064, #499, #627, #791).
		const ids = seen.presentation?.controls.map( control => control.id ) ?? [];
		assert.ok( !ids.includes( "close" ), `${panel} publishes a bare "close"` );
		const closes = ids.filter( id => closedPanel( id ) !== undefined );
		if ( closes.length === 0 ) return;
		f.ui.event( { kind: "activate", id: closes[0] } );
		step();
		assert.equal( f.ui.stats().panel, "", `${panel}: ${closes[0]} closes its window` );
	});
}

test("the transport bag beside a shop closes the trade through its own control", t => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	t.after( () => f.dispose() );
	let now = 0;
	// A property, not a local, as in the sweep above: a closure-assigned
	// local narrows to never at the assertions.
	const seen = {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
	};
	const step = () => {
		for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
	};
	step();
	tradeWithTransport( f );
	step();
	const ids = seen.presentation?.controls.map( control => control.id ) ?? [];
	assert.ok( ids.includes( panelCloseId( "Shop" ) ), "the shop has its close" );
	assert.ok( ids.includes( panelCloseId( "COS inventory" ) ), "the transport bag beside it has its own" );
	f.ui.event( { kind: "activate", id: panelCloseId( "COS inventory" ) } );
	step();
	assert.ok(
		sent.some( command => command.kind === "gameplay" && command.command.kind === "npc-close" ),
		"closing the bag ends the trade, as the shop's close does"
	);
	assert.ok(
		!seen.presentation?.controls.some( control => closedPanel( control.id ) !== undefined ),
		"both windows closed"
	);
});

/*
================
OVERLAYS

The windows game state opens over whatever panel is open: ui.ts draws each
outside the panel switch, so any of them can stand beside any panel.
	global chat      the Global Chatting item used from the bag, opened
	                 first: in a service window a bag double-click is that
	                 service's action, not the item's use
	talk menu        gameplay.npcConversation (hidden beside its own Shop)
	stall            gameplay.stall, owner or visitor
	stall network    gameplay.stall.network.open
	pet status       gameplay.cosRecords (the pet bars)
An exchange is no overlay: it opens the inventory tab in place of any panel
(69FBB0), so it stands only beside the inventory (VARIANTS).
================
*/
/** @type {Record<string, (f: Fixture, step: () => void) => void>} */
const OVERLAYS = {
	"global chat"( f, step ) {
		SERVICE_WINDOWS["Global chat"]( f, step );
	},
	"talk menu"( f ) {
		talkTo( f );
	},
	"stall"( f ) {
		// Merged into the stall state, so a network window already open stays.
		f.state.gameplay.stall = { ...(f.state.gameplay.stall ?? emptyStall()), phase: "owner", title: "Stall" };
	},
	"stall network"( f ) {
		f.state.gameplay.stall = stallRequest( f.state.gameplay.stall ?? emptyStall(), {
			kind: "stall-network-open",
			open: true
		} ).state;
	},
	"pet status"( f ) {
		f.state.gameplay.cosRecords ??= [
			{ gid: TRANSPORT, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false, inventory: [] }
		];
	}
};

/*
================
MODALS

The boxes that own the screen while they stand (ui.ts clears every other
control for them), raised over a panel and its overlays one at a time.
	party request    gameplay.partyMatching.request
	reverse return   gameplay.reverseReturnChoice
Each returns the undo that lowers it again.
================
*/
/** @type {Record<string, (f: Fixture) => () => void>} */
const MODALS = {
	"party request"( f ) {
		f.state.gameplay.partyMatching = {
			page: 0,
			pages: 1,
			rows: [],
			own: null,
			pending: null,
			result: null,
			auto: [],
			request: { a: 7, b: 42, member: { id: 77, name: "Peer", model: 1907, level: 20, region: 1 }, expires: 1e9 }
		};
		return () => {
			f.state.gameplay.partyMatching = { ...f.state.gameplay.partyMatching, request: null };
		};
	},
	"reverse return"( f ) {
		f.state.gameplay.reverseReturnChoice = true;
		return () => {
			f.state.gameplay.reverseReturnChoice = false;
		};
	}
};

/*
================
scenario

Runs open on a production HUD. open gets the fixture, a check that steps
the HUD and returns the ids it publishes, and the bare step. The fixture refuses
duplicate ids on every publication.
================
*/
/** @param {(f: Fixture, check: () => Set<string>, step: () => void) => void} open */
function scenario( open ) {
	const f = uiFixture();
	try {
		let now = 0;
		const seen = {
			/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
		};
		const step = () => {
			for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
		};
		const check = () => {
			step();
			return new Set( seen.presentation?.controls.map( control => control.id ) ?? [] );
		};
		open( f, check, step );
	} finally {
		f.dispose();
	}
}

/*
================
markersOf

Each window's markers: the ids it publishes opened alone on a bare HUD that
no other window in the same set publishes, so a marker present proves that
window is open.
================
*/
/** @param {Record<string, (f: any, step: () => void) => unknown>} windows */
function markersOf( windows ) {
	const opened = Object.fromEntries(
		Object.entries( windows ).map( ( [name, open] ) => {
			let ids = /** @type {string[]} */ ([]);
			scenario( ( f, check, step ) => {
				const before = check();
				open( f, step );
				ids = [ ...check() ].filter( id => !before.has( id ) );
			} );
			return [ name, ids ];
		} )
	);
	return Object.fromEntries(
		Object.entries( opened ).map( ( [name, ids] ) => [
			name,
			ids.filter( id =>
				!Object.entries( opened ).some( ( [other, others] ) => other !== name && others.includes( id ) )
			)
		] )
	);
}

const overlayMarkers = markersOf( OVERLAYS ), modalMarkers = markersOf( MODALS );
// An exchange opens the inventory tab in place of the panel (69FBB0): each
// panel case ends with one, beside every overlay still open.
const exchangeMarkers = markersOf( { exchange: SERVICE_WINDOWS["Exchange"], ...OVERLAYS } ).exchange;

test("every overlay and modal has controls of its own to prove it opened", () => {
	for ( const [name, markers] of Object.entries( { ...overlayMarkers, ...modalMarkers } ) ) {
		assert.ok( markers.length, `${name} publishes no control of its own` );
	}
});

/*
================
absent

The windows of markers none of whose marker ids ids holds.
================
*/
/** @param {Record<string, string[]>} markers @param {Set<string>} ids */
function absent( markers, ids ) {
	return Object.entries( markers ).filter( ( [, own] ) => !own.some( id => ids.has( id ) ) ).map( ( [name] ) =>
		name
	);
}

for ( const panel of [ ...uiPanels(), ...Object.keys( SERVICE_WINDOWS ) ] ) {
	test(`${panel} stands beside every overlay and under every modal with unique control ids`, () => {
		scenario( ( f, check, step ) => {
			// A window is never stacked with itself (Global chat, Stall).
			const [first, ...rest] = Object.entries( OVERLAYS ).filter( ( [name] ) => name !== panel.toLowerCase() )
				.map( ( [, overlay] ) => overlay );
			check();
			if ( panel.toLowerCase() !== "global chat" ) first( f, step );
			else rest.unshift( first );
			const before = check();
			const open = OPENERS[panel] ?? SERVICE_WINDOWS[panel] ??
				(() => f.ui.event( { kind: "activate", id: "open-window:" + panel } ));
			open( f, step );
			const own = [ ...check() ].filter( id => !before.has( id ) );
			assert.ok( own.length, `${panel} did not open` );
			for ( const overlay of rest ) overlay( f, step );
			const together = check();
			const others = Object.fromEntries(
				Object.entries( overlayMarkers ).filter( ( [name] ) => name !== panel.toLowerCase() )
			);
			assert.deepEqual( absent( others, together ), [], `${panel}: overlays that did not open beside it` );
			assert.ok( own.some( id => together.has( id ) ), `${panel} closed when the overlays opened` );
			for ( const [name, raise] of Object.entries( MODALS ) ) {
				const lower = raise( f );
				assert.ok( modalMarkers[name].some( id => check().has( id ) ), `${name} did not open over ${panel}` );
				lower();
			}
			SERVICE_WINDOWS["Exchange"]( f, step );
			assert.ok( exchangeMarkers.some( id => check().has( id ) ), `the exchange did not open over ${panel}` );
		} );
	});
}

test("buying trade goods opens the deal box beside the shop and its transport", () => {
	scenario( ( f, check, step ) => {
		check();
		tradeWithTransport( f );
		f.state.gameplay.shop = {
			...f.state.gameplay.shop,
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: 2001,
				name: "Trade goods",
				price: "100",
				maxStack: 10,
				items: [ { refObjId: 2001, typeFlags: TRADE_GOODS_TYPE, quantity: 10, name: "Trade goods" } ]
			} ]
		};
		const shop = check();
		assert.ok(
			shop.has( panelCloseId( "COS inventory" ) ) && shop.has( panelCloseId( "Shop" ) ),
			"the shop and its transport bag are open"
		);
		f.ui.event( { kind: "activate", id: "shop-offer:0" } );
		const deal = check();
		assert.ok( [ ...deal ].some( id => id.startsWith( "specialty-deal" ) ), "the deal box opened" );
		assert.ok(
			deal.has( panelCloseId( "COS inventory" ) ) && deal.has( panelCloseId( "Shop" ) ),
			"beside the shop and its transport bag"
		);
	} );
});
