/*
===========================================================================

job-honor.test.mjs - the Job Honor feed, its shaping and its window

Port-only, not native (#3530). The worker lane (gameplay/job-honor)
decodes the server's snapshot strictly; the foundation re-runs the
board's composition per tab; the view (runtime/ui/hud/job-honor-view)
turns the state into plain ops, so the tabs, the My honor panel and
the empty states are pinned here without the renderer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { createJobHonor, OP_JOB_HONOR_SNAPSHOT } = await load(
	"runtime/simulation/worker/session/world/gameplay/job-honor/job-honor.ts"
);
const { honorRows, honorToday, honorCountdownText } = await load( "foundation/ui/job-honor.ts" );
const { jobHonorView, jobHonorPictures } = await load( "runtime/ui/hud/job-honor-view.ts" );

// A snapshot frame, built the way the server wire writes it.
function snapshot( { now = 1000, change = 2000, prevEnd = 500, current = [], previous = [], own = null } = {} ) {
	const bytes = [];
	const u8 = value => bytes.push( value & 0xff );
	const u16 = value => bytes.push( value & 0xff, (value >> 8) & 0xff );
	const u32 = value => {
		for ( let i = 0; i < 4; i++ ) bytes.push( (value >> (8 * i)) & 0xff );
	};
	const u64 = value => {
		let left = BigInt( value );
		for ( let i = 0; i < 8; i++ ) {
			bytes.push( Number( left & 0xffn ) );
			left >>= 8n;
		}
	};
	const row = entry => {
		u8( entry.job );
		const alias = new TextEncoder().encode( entry.alias );
		u8( alias.length );
		bytes.push( ...alias );
		u32( entry.points );
		u8( entry.tier );
		u8( entry.tonight );
	};
	u64( now );
	u64( change );
	u64( prevEnd );
	u16( current.length );
	current.forEach( row );
	u16( previous.length );
	previous.forEach( row );
	if ( !own ) {
		u8( 0 );
	} else {
		u8( 1 );
		u8( own.job );
		u16( own.place );
		u8( own.tier );
		u8( own.tonight );
		u32( own.capLeft );
		own.today.forEach( u32 );
		own.month.forEach( u32 );
	}
	return { opcode: OP_JOB_HONOR_SNAPSHOT, payload: new Uint8Array( bytes ) };
}

const ROWS = [
	{ job: 1, alias: "Alpha", points: 300, tier: 2, tonight: 3 },
	{ job: 2, alias: "Beta", points: 400, tier: 1, tonight: 1 },
	{ job: 1, alias: "Cy", points: 400, tier: 1, tonight: 2 }
];
const OWN = { job: 1, place: 2, tier: 2, tonight: 3, capLeft: 750, today: [ 40, 0, 5 ], month: [ 90, 10, 5 ] };

function feed( overrides = {} ) {
	return {
		serverNowMs: 1000,
		nextChangeMs: 1000 + 5 * 3600_000 + 12 * 60_000,
		previousEndMs: 500,
		current: ROWS,
		previous: [ { job: 2, alias: "Old", points: 90, tier: 1, tonight: 1 } ],
		own: { present: true, ...OWN },
		revision: 1,
		...overrides
	};
}

function view( state ) {
	return jobHonorView( { origin: [ 100, 50 ], tile: "tile.png", state } );
}

function texts( ops ) {
	return ops.filter( op => op.kind === "text" ).map( op => op.value );
}

test("the lane decodes a snapshot and ignores other opcodes", () => {
	const honor = createJobHonor();
	assert.equal( honor.receive( { opcode: 0x1234, payload: new Uint8Array() } ), false );
	assert.equal(
		honor.receive( snapshot( { current: ROWS, previous: [ ROWS[1] ], own: OWN } ) ),
		true
	);
	const current = honor.current();
	assert.equal( current.serverNowMs, 1000 );
	assert.equal( current.previousEndMs, 500 );
	assert.deepEqual( current.current, ROWS );
	assert.deepEqual( current.previous, [ ROWS[1] ] );
	assert.deepEqual( current.own.today, [ 40, 0, 5 ] );
	assert.equal( current.own.capLeft, 750 );
	honor.clear();
	assert.equal( honor.current(), null );
	honor.receive( snapshot( {} ) );
	assert.equal( honor.current().revision, 2, "the revision survives a clear" );
});

test("a malformed snapshot is refused whole", () => {
	const honor = createJobHonor();
	const good = snapshot( { current: ROWS, own: OWN } );
	const short = { opcode: OP_JOB_HONOR_SNAPSHOT, payload: good.payload.slice( 0, good.payload.length - 3 ) };
	assert.throws( () => honor.receive( short ), /job honor/ );
	const huge = snapshot( { current: Array.from( { length: 300 }, () => ROWS[0] ), own: OWN } );
	assert.throws( () => honor.receive( huge ), /job honor/ );
	assert.equal( honor.current(), null );
});

test("tabs re-run the board's composition over one job", () => {
	const all = honorRows( feed(), "all", "current" );
	assert.deepEqual( all, ROWS, "All keeps the server's order" );
	const traders = honorRows( feed(), "trader", "current" );
	assert.deepEqual( traders.map( row => row.alias ), [ "Cy", "Alpha" ], "points down, alias breaking ties" );
	const previous = honorRows( feed(), "thief", "previous" );
	assert.deepEqual( previous.map( row => row.alias ), [ "Old" ] );
});

test("the My honor words: today, the countdown", () => {
	assert.equal( honorToday( feed() ), 45 );
	assert.equal( honorCountdownText( feed(), 1000 ), "5:12" );
});

test("the view draws My honor, the tabs and a page of rows", () => {
	const ops = view( { feed: feed(), tab: "all", period: "current", page: 0, serverNowMs: 1000 } );
	const lines = texts( ops );
	assert.ok( lines.includes( "My honor" ) );
	assert.ok( lines.includes( "Today 45 points - daily cap left 750" ) );
	assert.ok( lines.includes( "This month: Trader 90 · Thief 10 · Hunter 5" ) );
	assert.ok( lines.includes( "Place #2 (Trader)" ) );
	assert.ok( lines.some( line => line.startsWith( "Tier Gold - King after tonight" ) && line.endsWith( "(5:12)" ) ) );
	const buttons = ops.filter( op => op.kind === "button" ).map( op => op.id );
	assert.deepEqual( buttons, [
		"job-honor-tab:all",
		"job-honor-tab:trader",
		"job-honor-tab:thief",
		"job-honor-period:current",
		"job-honor-period:previous"
	] );
	assert.ok( lines.includes( "Beta" ) && lines.includes( "400" ) && lines.includes( "Gold -> King" ) );
	assert.ok( jobHonorPictures( ops ).includes( "tile.png" ) );
});

test("the view speaks the empty states", () => {
	const waiting = view( { feed: null, tab: "all", period: "current", page: 0, serverNowMs: 0 } );
	assert.ok( texts( waiting ).includes( "Waiting for the ranking..." ) );
	const unranked = view( {
		feed: feed( { own: { present: true, ...OWN, place: 0 } } ),
		tab: "all",
		period: "current",
		page: 0,
		serverNowMs: 1000
	} );
	assert.ok( texts( unranked ).includes( "You are not in this ranking." ) );
	const noPrevious = view( {
		feed: feed( { previous: [], previousEndMs: 0 } ),
		tab: "all",
		period: "previous",
		page: 0,
		serverNowMs: 1000
	} );
	assert.ok( texts( noPrevious ).includes( "No previous period yet." ) );
	const empty = view( {
		feed: feed( { current: [] } ),
		tab: "trader",
		period: "current",
		page: 0,
		serverNowMs: 1000
	} );
	assert.ok( texts( empty ).includes( "No rankings yet." ) );
});

test("the table pages after twelve rows", () => {
	const many = Array.from( { length: 13 }, ( _, i ) => ({
		job: 1,
		alias: "R" + i,
		points: 100 - i,
		tier: 0,
		tonight: 0
	}) );
	const first = view( {
		feed: feed( { current: many } ),
		tab: "all",
		period: "current",
		page: 0,
		serverNowMs: 1000
	} );
	assert.ok( texts( first ).includes( "Page 1/2" ) );
	assert.ok( !texts( first ).includes( "R12" ) );
	const second = view( {
		feed: feed( { current: many } ),
		tab: "all",
		period: "current",
		page: 1,
		serverNowMs: 1000
	} );
	assert.ok( texts( second ).includes( "R12" ) );
	const pager = second.filter( op => op.kind === "button" ).map( op => op.id );
	assert.ok( pager.includes( "job-honor-page:-1" ) && pager.includes( "job-honor-page:1" ) );
});
