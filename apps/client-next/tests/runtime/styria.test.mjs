/*
===========================================================================

styria.test.mjs - the Styria Clash feed, its shaping and its windows

Port-only, not native (#3648 slice 5). The worker lane
(gameplay/styria) decodes the server's status snapshot strictly and
latches its result; the foundation (ui/styria) shapes the words and
the conversation's rows; the hud owns the standing and its clock;
the view (runtime/ui/hud/styria-view) turns the state into plain
ops, so the registration, score and result windows are pinned here
without the renderer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { createStyria, OP_STYRIA_STATUS } = await load(
	"runtime/simulation/worker/session/world/gameplay/styria/styria.ts"
);
const { styriaClockText, styriaCountdown, styriaResultWord, styriaConversationChoices } = await load(
	"foundation/ui/styria.ts"
);
const { createStyriaHud } = await load( "runtime/ui/hud/styria-hud.ts" );
const { styriaView, styriaStripOps } = await load( "runtime/ui/hud/styria-view.ts" );

// A status frame, built the way the server wire writes it.
/**
 * @param {{ phase?: number, matchSeq?: number, nextStartMs?: number, windowOpensMs?: number,
 *   phaseEndsMs?: number, ownSide?: number, ownState?: number, hunters?: number, thieves?: number,
 *   scoreHunters?: number, scoreThieves?: number, outcome?: number, jobExp?: number,
 *   skillExp?: number, serverNowMs?: number }} [fields]
 */
function statusFrame( fields = {} ) {
	const p = new Uint8Array( 56 );
	const v = new DataView( p.buffer );
	const f = {
		phase: 0,
		matchSeq: 0,
		nextStartMs: 0,
		windowOpensMs: 0,
		phaseEndsMs: 0,
		ownSide: 255,
		ownState: 0,
		hunters: 0,
		thieves: 0,
		scoreHunters: 0,
		scoreThieves: 0,
		outcome: 0,
		jobExp: 0,
		skillExp: 0,
		serverNowMs: 0,
		...fields
	};
	p[0] = f.phase;
	v.setUint32( 1, f.matchSeq, true );
	v.setBigUint64( 5, BigInt( f.nextStartMs ), true );
	v.setBigUint64( 13, BigInt( f.windowOpensMs ), true );
	v.setBigUint64( 21, BigInt( f.phaseEndsMs ), true );
	p[29] = f.ownSide;
	p[30] = f.ownState;
	v.setUint16( 31, f.hunters, true );
	v.setUint16( 33, f.thieves, true );
	v.setUint16( 35, f.scoreHunters, true );
	v.setUint16( 37, f.scoreThieves, true );
	p[39] = f.outcome;
	v.setUint32( 40, f.jobExp, true );
	v.setUint32( 44, f.skillExp, true );
	v.setBigUint64( 48, BigInt( f.serverNowMs ), true );
	return { opcode: OP_STYRIA_STATUS, payload: p };
}

const COPY = {
	UIIT_STT_ARENA_JOB_WAR: "Styria Clash",
	UIIT_PORT_STYRIA_APPLY: "Join Styria Clash",
	UIIT_PORT_STYRIA_CANCEL: "Cancel Registration",
	UIIT_PORT_STYRIA_WAITING: "Waiting for the arena...",
	UIIT_PORT_STYRIA_DESC: "Rule line",
	UIIT_PORT_STYRIA_OPENS_IN: "Registration opens in",
	UIIT_PORT_STYRIA_STARTS_IN: "Starts in",
	UIIT_PORT_STYRIA_APPLICANTS: "Applicants",
	UIIT_PORT_STYRIA_YOUR_SIDE: "Your side",
	UIIT_PORT_STYRIA_APPLIED: "Registration completed.",
	UIIT_PORT_STYRIA_SUIT_HINT: "Wear a job suit to fight for its side.",
	UIIT_PORT_STYRIA_TIME_LEFT: "Time left",
	UIIT_PORT_STYRIA_RESULT: "Styria Clash Results",
	UIIT_PORT_STYRIA_REWARDS: "Styria Clash Rewards",
	UIIT_PORT_STYRIA_SKILL_EXP: "Skill exp : %d",
	UIIT_PORT_STYRIA_JOB_EXP: "Job EXP : %d",
	UIIT_PORT_STYRIA_WIN: "Victory",
	UIIT_PORT_STYRIA_LOSE: "Defeat",
	UIIT_PORT_STYRIA_DRAW: "Draw",
	UIIT_PORT_STYRIA_CONFIRM: "Confirm"
};
const copy = key => COPY[key] ?? "";

test("the worker decodes a snapshot and ignores every other opcode", () => {
	const styria = createStyria();
	assert.equal( styria.receive( { opcode: 0x3fc6, payload: new Uint8Array( 56 ) } ), false );
	assert.equal( styria.current(), null );
	assert.equal(
		styria.receive( statusFrame( {
			phase: 1,
			matchSeq: 3,
			phaseEndsMs: 9000,
			ownSide: 1,
			ownState: 2,
			hunters: 4,
			thieves: 5,
			scoreHunters: 2,
			scoreThieves: 6,
			serverNowMs: 8000
		} ) ),
		true
	);
	const feed = styria.current();
	assert.equal( feed.phase, "match" );
	assert.equal( feed.matchSeq, 3 );
	assert.equal( feed.ownSide, 1 );
	assert.equal( feed.ownState, 2 );
	assert.equal( feed.scoreThieves, 6 );
	assert.equal( feed.serverNowMs, 8000 );
	assert.equal( feed.revision, 1 );
});

test("a malformed snapshot is a protocol error, never a partial standing", () => {
	const styria = createStyria();
	assert.throws( () => styria.receive( { opcode: OP_STYRIA_STATUS, payload: new Uint8Array( 55 ) } ) );
	assert.throws( () => styria.receive( statusFrame( { phase: 9 } ) ) );
	assert.throws( () => styria.receive( statusFrame( { ownSide: 7 } ) ) );
	assert.throws( () => styria.receive( statusFrame( { outcome: 4 } ) ) );
	assert.equal( styria.current(), null );
});

test("a result latches across later snapshots until the world clears", () => {
	const styria = createStyria();
	styria.receive( statusFrame( { phase: 2, matchSeq: 4, outcome: 1, ownSide: 0, jobExp: 20, skillExp: 670 } ) );
	assert.equal( styria.result().matchSeq, 4 );
	styria.receive( statusFrame( { phase: 0, matchSeq: 4, serverNowMs: 10 } ) );
	assert.equal( styria.current().phase, "registration" );
	assert.equal( styria.result().outcome, 1 );
	styria.clear();
	assert.equal( styria.current(), null );
	assert.equal( styria.result(), null );
});

test("the hud anchors its clock on the server's and dismisses by match", () => {
	const hud = createStyriaHud();
	const styria = createStyria();
	styria.receive( statusFrame( { phase: 2, matchSeq: 7, outcome: 3, serverNowMs: 5000 } ) );
	hud.reconcile( styria.current(), styria.result(), 1000 );
	assert.equal( hud.state( 3000 ).serverNowMs, 7000 );
	assert.equal( hud.resultPending(), true );
	hud.markResultShown();
	assert.equal( hud.resultPending(), false );
	assert.equal( hud.state( 3000 ).result.matchSeq, 7 );
	hud.dismissResult();
	assert.equal( hud.state( 3000 ).result, null );
	// A newer match already running supersedes an old standing result.
	styria.receive( statusFrame( { phase: 2, matchSeq: 8, outcome: 1, serverNowMs: 9000 } ) );
	hud.reconcile( styria.current(), styria.result(), 4000 );
	assert.equal( hud.state( 4000 ).result.matchSeq, 8 );
	styria.receive( statusFrame( { phase: 1, matchSeq: 9, serverNowMs: 9500 } ) );
	hud.reconcile( styria.current(), styria.result(), 4500 );
	assert.equal( hud.state( 4500 ).result, null );
});

test("the foundation shapes clocks, countdowns and verdicts", () => {
	assert.equal( styriaClockText( 0 ), "0:00" );
	assert.equal( styriaClockText( 61_999 ), "1:01" );
	assert.equal( styriaClockText( 3_661_000 ), "61:01" );
	const feed = { nextStartMs: 100_000, windowOpensMs: 40_000 };
	assert.deepEqual( styriaCountdown( feed, 10_000 ), { opens: true, ms: 30_000 } );
	assert.deepEqual( styriaCountdown( feed, 50_000 ), { opens: false, ms: 50_000 } );
	assert.equal( styriaCountdown( { nextStartMs: 0, windowOpensMs: 0 }, 0 ), null );
	assert.equal( styriaResultWord( { outcome: 1, ownSide: 0 } ), "win" );
	assert.equal( styriaResultWord( { outcome: 1, ownSide: 1 } ), "lose" );
	assert.equal( styriaResultWord( { outcome: 3, ownSide: 1 } ), "draw" );
});

test("the conversation's Styria rows name the window's buttons", () => {
	const conversation = {
		phase: "ready",
		gid: 42,
		dialogueRevision: 1,
		dialogue: {
			kind: 4,
			prompt: "SN_NPC_SD_ARENA_MANAGER_BS",
			options: [
				{ choice: 5, symbol: "UIIT_PORT_STYRIA_APPLY" },
				{ choice: 6, symbol: "SN_TALK" }
			]
		}
	};
	assert.deepEqual( styriaConversationChoices( conversation ), { apply: 5 } );
	assert.deepEqual( styriaConversationChoices( { phase: "closed" } ), {} );
	assert.deepEqual( styriaConversationChoices( undefined ), {} );
});

test("the registration window offers the conversation's own Apply choice", () => {
	const styria = createStyria();
	styria.receive( statusFrame( {
		phase: 0,
		nextStartMs: 200_000,
		windowOpensMs: 100_000,
		hunters: 2,
		thieves: 1,
		serverNowMs: 150_000
	} ) );
	const hud = createStyriaHud();
	hud.reconcile( styria.current(), styria.result(), 1000 );
	const ops = styriaView( {
		origin: [ 0, 0 ],
		tile: "",
		state: hud.state( 1000 ),
		copy,
		choices: { apply: 5 }
	} );
	const texts = ops.filter( op => op.kind === "text" ).map( op => op.value );
	assert.ok( texts.some( value => value === "Starts in 0:50" ) );
	assert.ok( texts.some( value => value === "Applicants:  Hunters 2  ·  Thieves 1" ) );
	const button = ops.find( op => op.kind === "button" );
	assert.equal( button.id, "npc-choice:5" );
	assert.equal( button.label, "Join Styria Clash" );
});

test("the result window speaks the verdict and the paid figures; the strip keeps the score", () => {
	const styria = createStyria();
	styria.receive( statusFrame( {
		phase: 2,
		matchSeq: 2,
		outcome: 1,
		ownSide: 0,
		scoreHunters: 3,
		scoreThieves: 1,
		jobExp: 20,
		skillExp: 11040,
		serverNowMs: 5000
	} ) );
	const hud = createStyriaHud();
	hud.reconcile( styria.current(), styria.result(), 1000 );
	const ops = styriaView( {
		origin: [ 0, 0 ],
		tile: "",
		state: hud.state( 1000 ),
		copy,
		choices: {}
	} );
	const texts = ops.filter( op => op.kind === "text" ).map( op => op.value );
	assert.ok( texts.includes( "Victory" ) );
	assert.ok( texts.includes( "Skill exp : 11040" ) );
	assert.ok( texts.includes( "Job EXP : 20" ) );
	assert.equal( ops.find( op => op.kind === "button" ).id, "styria-dismiss" );
	assert.deepEqual( styriaStripOps( { origin: [ 0, 0 ], state: hud.state( 1000 ) } ), [] );
	styria.receive( statusFrame( {
		phase: 1,
		matchSeq: 3,
		scoreHunters: 1,
		scoreThieves: 0,
		phaseEndsMs: 65_000,
		serverNowMs: 5000
	} ) );
	hud.reconcile( styria.current(), styria.result(), 2000 );
	const strip = styriaStripOps( { origin: [ 0, 0 ], state: hud.state( 2000 ) } );
	assert.equal( strip.length, 2 );
	assert.equal( strip[1].value, "Hunters 1  -  0 Thieves   ·   1:00" );
});
