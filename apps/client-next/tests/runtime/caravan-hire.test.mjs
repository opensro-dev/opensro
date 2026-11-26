/*
===========================================================================

caravan-hire.test.mjs - the AI caravan hire window's frames (port-only 0x3FCF / 0x7FCF)

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCaravanHire, CARAVAN_HIRE_OPCODE, CARAVAN_HIRE_REQUEST_OPCODE, CARAVAN_LEAD_OPCODE } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/caravan-hire.ts" ).href
);

const json = body => new TextEncoder().encode( JSON.stringify( body ) );
const window = {
	version: 1,
	title: "Hunters and Traders for Hire - Jangan",
	fee: "15,000",
	message: "",
	rows: [
		{
			name: "[AI]Hunter_Kwan",
			display: "Kwan",
			role: "Hunter",
			town: "Jangan",
			available: true,
			status: "Available"
		},
		{
			name: "[AI]Trader_Mei",
			display: "Mei",
			role: "Trader",
			town: "Donwhang",
			available: false,
			status: "Waiting in Donwhang"
		}
	],
	stars: [ { stars: 1, goods: "100,000", total: "115,000" } ]
};

test("a window frame publishes its rows and star prices", () => {
	const hire = createCaravanHire();
	assert.equal( hire.receive( { opcode: CARAVAN_HIRE_OPCODE, payload: json( window ) } ), true );
	const state = hire.state();
	assert.equal( state.rows.length, 2 );
	assert.equal( state.rows[0].available, true );
	assert.equal( state.rows[1].status, "Waiting in Donwhang" );
	assert.equal( state.stars[0].total, "115,000" );
	hire.clear();
	assert.equal( hire.state(), null );
});

test("other opcodes are not consumed; a malformed body is a protocol error", () => {
	const hire = createCaravanHire();
	assert.equal( hire.receive( { opcode: 0x3fc0, payload: json( window ) } ), false );
	assert.throws(
		() => hire.receive( { opcode: CARAVAN_HIRE_OPCODE, payload: json( { ...window, version: 2 } ) } ),
		/Invalid caravan hire/
	);
	assert.throws(
		() =>
			hire.receive( {
				opcode: CARAVAN_HIRE_OPCODE,
				payload: json( { ...window, stars: [ { stars: 9, goods: "", total: "" } ] } )
			} ),
		/Invalid caravan hire/
	);
	assert.equal( hire.state(), null );
});

test("a hire request carries the operation, stars and name", () => {
	const frame = createCaravanHire().request( 1, 4, "[AI]Trader_Jin" );
	assert.equal( frame.opcode, CARAVAN_HIRE_REQUEST_OPCODE );
	assert.deepEqual( [ ...frame.payload.slice( 0, 2 ) ], [ 1, 4 ] );
	assert.equal( new TextDecoder().decode( frame.payload.slice( 2 ) ), "[AI]Trader_Jin" );
	assert.throws( () => createCaravanHire().request( 1, 6, "x" ), /Invalid caravan hire request/ );
});

test("the lead state names the leader and who may lead; an empty list clears it", () => {
	const hire = createCaravanHire();
	const lead = {
		version: 1,
		leader: "[AI]Trader_Jin",
		candidates: [ { name: "Boss", display: "Boss" }, { name: "[AI]Trader_Jin", display: "Jin" } ]
	};
	assert.equal( hire.receive( { opcode: CARAVAN_LEAD_OPCODE, payload: json( lead ) } ), true );
	assert.equal( hire.lead().leader, "[AI]Trader_Jin" );
	assert.equal( hire.lead().candidates[1].display, "Jin" );
	hire.clear();
	assert.notEqual( hire.lead(), null, "closing the hire window keeps the lead state" );
	hire.receive( { opcode: CARAVAN_LEAD_OPCODE, payload: json( { version: 1, leader: "", candidates: [] } ) } );
	assert.equal( hire.lead(), null );
	assert.throws(
		() => hire.receive( { opcode: CARAVAN_LEAD_OPCODE, payload: json( { ...lead, version: 2 } ) } ),
		/Invalid caravan lead/
	);
	const frame = hire.request( 2, 0, "[AI]Trader_Jin" );
	assert.deepEqual( [ ...frame.payload.slice( 0, 2 ) ], [ 2, 0 ] );
});

test("an NPC select can carry the hire row its menu lists first", async () => {
	const { CARAVAN_HIRE_NPC_OPCODE } = await import(
		sourceFileUrl( "src/engine/foundation/gameplay/caravan-hire.ts" ).href
	);
	const hire = createCaravanHire();
	const label = new TextEncoder().encode( "Hire Hunters and Traders" );
	const payload = new Uint8Array( 4 + label.length );
	new DataView( payload.buffer ).setUint32( 0, 0x1234, true );
	payload.set( label, 4 );
	assert.equal( hire.receive( { opcode: CARAVAN_HIRE_NPC_OPCODE, payload } ), true );
	assert.deepEqual( hire.hireNpc(), { gid: 0x1234, label: "Hire Hunters and Traders" } );
	assert.throws(
		() => hire.receive( { opcode: CARAVAN_HIRE_NPC_OPCODE, payload: Uint8Array.of( 1, 2 ) } ),
		/Invalid caravan hire NPC/
	);
	hire.reset();
	assert.equal( hire.hireNpc(), null );
});

test("the lead mark toggles: a member takes the lead, the leader's mark hands it back", async () => {
	const { caravanLeadToggle } = await import(
		sourceFileUrl( "src/engine/foundation/gameplay/caravan-hire.ts" ).href
	);
	const candidates = [ { name: "Boss", display: "Boss" }, { name: "[AI]Trader_Jin", display: "Jin" }, {
		name: "[AI]Trader_Bao",
		display: "Bao"
	} ];
	const traderLeads = { leader: "[AI]Trader_Jin", candidates };
	assert.equal( caravanLeadToggle( traderLeads, "[AI]Trader_Bao" ), "[AI]Trader_Bao" );
	assert.equal( caravanLeadToggle( traderLeads, "Boss" ), "Boss" );
	assert.equal(
		caravanLeadToggle( traderLeads, "[AI]Trader_Jin" ),
		"Boss",
		"a Trader's lit mark hands the lead to the hirer"
	);
	assert.equal(
		caravanLeadToggle( { leader: "Boss", candidates }, "boss" ),
		"[AI]Trader_Jin",
		"the hirer's lit mark hands it to the first Trader"
	);
	assert.equal( caravanLeadToggle( traderLeads, "Stranger" ), "" );
});

const hireHud = () => import( sourceFileUrl( "src/engine/runtime/ui/hud/caravan-hire-hud.ts" ).href );
const hireWindow = () => import( sourceFileUrl( "src/engine/runtime/ui/hud/caravan-hire-window.ts" ).href );
const hireList = {
	...window,
	rows: [
		...window.rows,
		{
			name: "[AI]Thief_Ox",
			display: "Ox",
			role: "Thief",
			town: "",
			available: false,
			extend: true,
			status: "Yours for 20 min"
		}
	],
	stars: [ 1, 2, 3, 4, 5 ].map( stars => ({ stars, goods: String( stars * 100 ), total: String( stars * 115 ) }) )
};

test("the hire window confirms first; a Trader's stars stay inside the list's choices", async () => {
	const { createCaravanHireHud } = await hireHud();
	const hud = createCaravanHireHud(), sent = [];
	const click = id => hud.activate( id, hireList, command => sent.push( command ) );
	assert.equal( hud.activate( "caravan-hire-row:[AI]Hunter_Kwan", undefined, () => {} ), false, "no window open" );
	assert.equal( click( "market-close" ), false, "another window's control" );
	assert.equal( click( "caravan-hire-row:[AI]Trader_Mei" ), true );
	assert.equal( hud.pick(), null, "an unavailable row asks nothing" );
	hireList.rows[1] = { ...hireList.rows[1], available: true };
	click( "caravan-hire-row:[AI]Trader_Mei" );
	assert.equal( hud.pick().stars, 3, "a Trader's pick starts at three stars" );
	for ( let i = 0; i < 4; i++ ) click( "caravan-hire-star-more" );
	assert.equal( hud.pick().stars, 5 );
	for ( let i = 0; i < 9; i++ ) click( "caravan-hire-star-less" );
	assert.equal( hud.pick().stars, 1 );
	assert.deepEqual( sent, [], "nothing is sent before the confirm" );
	click( "caravan-hire-star-ok" );
	assert.deepEqual( sent, [ { kind: "caravan-hire", operation: 1, stars: 1, name: "[AI]Trader_Mei" } ] );
	assert.equal( hud.pick(), null );
	click( "caravan-hire-row:[AI]Hunter_Kwan" );
	assert.equal( hud.pick().stars, 0, "a Hunter carries no goods" );
	click( "caravan-hire-star-more" );
	assert.equal( hud.pick().stars, 0 );
	click( "caravan-hire-star-cancel" );
	click( "caravan-hire-row:[AI]Thief_Ox" );
	click( "caravan-hire-star-ok" );
	assert.deepEqual(
		sent.at( -1 ),
		{ kind: "caravan-hire", operation: 3, stars: 0, name: "[AI]Thief_Ox" },
		"own agent: extend"
	);
	click( "caravan-hire-refresh" );
	click( "caravan-hire-close" );
	assert.deepEqual( sent.slice( -2 ), [ { kind: "caravan-hire", operation: 0, stars: 0, name: "" }, {
		kind: "caravan-hire-close"
	} ] );
	hireList.rows[1] = { ...hireList.rows[1], available: false };
});

test("the hire window's page follows the filter and never runs past the list", async () => {
	const { createCaravanHireHud } = await hireHud();
	const hud = createCaravanHireHud(), click = id => hud.activate( id, hireList, () => {} );
	click( "caravan-hire-prev" );
	assert.equal( hud.page( 3 ), 0 );
	click( "caravan-hire-next" );
	click( "caravan-hire-next" );
	assert.equal( hud.page( 3 ), 2 );
	click( "caravan-hire-next" );
	assert.equal( hud.page( 3 ), 2, "clamped to the last page" );
	assert.equal( hud.page( 1 ), 0, "a shorter list pulls the page back" );
	click( "caravan-hire-next" );
	click( "caravan-hire-filter" );
	assert.equal( hud.onlyAvailable(), true );
	assert.equal( hud.page( 3 ), 0, "the filter starts at the first page" );
});

test("the hire window draws a control only for the rows a click can take", async () => {
	const { createCaravanHireHud } = await hireHud();
	const { drawCaravanHire } = await hireWindow();
	const frame = {
		quads: [],
		controls: [],
		blocks: [],
		paths: [],
		w: 1280,
		h: 720,
		full: [ 0, 0, 1280, 720 ],
		white: [ 1, 1, 1, 1 ],
		chrome: { frame: "frame/", parts: [ "a" ] },
		resources: { size: () => [ 16, 16 ] },
		text: { quads: value => [ { value } ] }
	};
	const buttons = [];
	const paint = {
		beginWindow: () => [ 0, 0, 0, 0 ],
		endWindow: () => {},
		windowBox: () => {},
		closeButton: () => {},
		button: ( _, spec ) => buttons.push( spec ),
		rect: () => {},
		image: () => {},
		label: () => {}
	};
	drawCaravanHire( frame, paint, { hire: hireList, hud: createCaravanHireHud() } );
	assert.deepEqual(
		frame.controls.map( c => c.id ),
		[ "caravan-hire-filter", "caravan-hire-row:[AI]Hunter_Kwan", "caravan-hire-row:[AI]Thief_Ox" ]
	);
	assert.equal( frame.controls[2].label, "Extend Ox's hire" );
	const pager = Object.fromEntries( buttons.map( b => [ b.id, !!b.disabled ] ) );
	assert.deepEqual( pager, { "caravan-hire-prev": true, "caravan-hire-next": true, "caravan-hire-refresh": false } );
	assert.ok( frame.quads.some( q => q.value === "Waiting in Donwhang" ), "a refused row still shows its reason" );
});

test("the lead badge says what a click does", async () => {
	const { caravanLeadCaption } = await hireWindow();
	assert.deepEqual( caravanLeadCaption( true, true, "You" ), {
		label: "You lead the caravan",
		help: "You lead the caravan. Click to let your first Trader lead."
	} );
	assert.equal( caravanLeadCaption( true, false, "Jin" ).help, "Jin leads the caravan. Click to lead it yourself." );
	assert.equal( caravanLeadCaption( false, false, "Jin" ).label, "Let Jin lead the caravan" );
	assert.equal( caravanLeadCaption( false, true, "You" ).label, "Lead the caravan yourself" );
});
