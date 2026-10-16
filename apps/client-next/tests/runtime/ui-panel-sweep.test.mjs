/*
===========================================================================

ui-panel-sweep.test.mjs - every registered window publishes unique controls

BUG-064 hid in windows no test opened: a frame close button and the window's
own Cancel shared one control id. The fixture refuses duplicate ids on every
publication, so opening each registered panel checks mounted cases. Target
controls must appear; companion Inventory or an NPC menu cannot stand in for
the requested surface. Synthetic presentation does not prove visible menu
entry, every state, or native parity.
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
import { OPENERS, SERVICE_WINDOWS } from "../helpers/ui-entry-recipes.mjs";
import { entryEvidence } from "../helpers/ui-entry-evidence.mjs";

const OFFICIAL = 17, TRADER = 18, TRANSPORT = 7, TRADE_GOODS_TYPE = 0x46c;
/** @typedef {import("../helpers/ui-entry-recipes.mjs").EntryFixture} Fixture */
const SWEEP_WINDOWS = { ...SERVICE_WINDOWS, "Shop with a transport out": tradeWithTransport };

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

for ( const panel of [ ...uiPanels(), ...Object.keys( SWEEP_WINDOWS ) ] ) {
	test(`${panel} opens with unique control ids`, async t => {
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
		const open = OPENERS[panel] ?? SWEEP_WINDOWS[panel] ??
			(() => f.ui.event( { kind: "activate", id: "open-window:" + panel } ));
		await open( f, step );
		step();
		const targets = panel === "Shop with a transport out" ? [ "Shop", "COS inventory" ] : [ panel ];
		for ( const target of targets ) {
			const evidence = entryEvidence( target, seen.presentation?.controls ?? [] );
			assert.ok( evidence.admitted, `${target} is missing its own controls: ${evidence.missing.join( ", " )}` );
		}
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

test("companion Inventory and an official menu do not admit missing target windows", async t => {
	for ( const name of [ "Magic option", "Fortress war application" ] ) {
		const f = uiFixture();
		t.after( () => f.dispose() );
		const seen = {
			/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
		};
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
		};
		step();
		if ( name === "Magic option" ) f.ui.event( { kind: "activate", id: "open-window:Magic option" } );
		else {
			await OPENERS[name]( f, step );
			delete f.state.gameplay.fortress;
		}
		step();
		const controls = seen.presentation?.controls ?? [];
		assert.ok(
			controls.some( control => control.id === (name === "Magic option" ? "inventory-gold" : "npc-close") )
		);
		assert.equal( entryEvidence( name, controls ).admitted, false );
	}
});

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
		const matching = {
			page: 0,
			pages: 1,
			rows: [],
			own: null,
			pending: null,
			result: null,
			auto: [],
			request: {
				a: 7,
				b: 42,
				native7c0: 0,
				primary: 0,
				secondary: 0,
				flags: 0,
				member: {
					id: 77,
					name: "Peer",
					model: 1907,
					level: 20,
					region: 1,
					status: 0xaa,
					x: 0,
					y: 0,
					z: 0,
					war: 0
				},
				expires: 1e9
			}
		};
		f.state.gameplay.partyMatching = matching;
		return () => {
			f.state.gameplay.partyMatching = { ...matching, request: null };
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
/** @param {(f: Fixture, check: () => Set<string>, step: () => void) => void | Promise<void>} open */
async function scenario( open ) {
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
		await open( f, check, step );
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
/** @param {Record<string, (f: Fixture, step: () => void) => unknown>} windows */
async function markersOf( windows ) {
	const opened = Object.fromEntries(
		await Promise.all(
			Object.entries( windows ).map( async ( [name, open] ) => {
				let ids = /** @type {string[]} */ ([]);
				await scenario( async ( f, check, step ) => {
					const before = check();
					await open( f, step );
					ids = [ ...check() ].filter( id => !before.has( id ) );
				} );
				return [ name, ids ];
			} )
		)
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

const overlayMarkers = await markersOf( OVERLAYS ), modalMarkers = await markersOf( MODALS );
// An exchange opens the inventory tab in place of the panel (69FBB0): each
// panel case ends with one, beside every overlay still open.
const exchangeMarkers = (await markersOf( { exchange: SERVICE_WINDOWS["Exchange"], ...OVERLAYS } )).exchange;

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

for ( const panel of [ ...uiPanels(), ...Object.keys( SWEEP_WINDOWS ) ] ) {
	test(`${panel} stands beside every overlay and under every modal with unique control ids`, async () => {
		await scenario( async ( f, check, step ) => {
			// A window is never stacked with itself (Global chat, Stall).
			const [first, ...rest] = Object.entries( OVERLAYS ).filter( ( [name] ) => name !== panel.toLowerCase() )
				.map( ( [, overlay] ) => overlay );
			check();
			if ( panel.toLowerCase() !== "global chat" ) first( f, step );
			else rest.unshift( first );
			const before = check();
			const open = OPENERS[panel] ?? SWEEP_WINDOWS[panel] ??
				(() => f.ui.event( { kind: "activate", id: "open-window:" + panel } ));
			await open( f, step );
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

test("buying trade goods opens the deal box beside the shop and its transport", async () => {
	await scenario( ( f, check, step ) => {
		check();
		tradeWithTransport( f );
		const shopState = f.state.gameplay.shop;
		assert.ok( shopState );
		f.state.gameplay.shop = {
			...shopState,
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: 2001,
				name: "Trade goods",
				price: "100",
				maxStack: 10,
				items: [ {
					slot: 0,
					refObjId: 2001,
					typeFlags: TRADE_GOODS_TYPE,
					quantity: 10,
					name: "Trade goods",
					plus: 0,
					durability: 0,
					variance: "0",
					magic: []
				} ]
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
