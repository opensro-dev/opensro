/*
===========================================================================

gacha-window.test.mjs - tests for gacha-window.ts, gacha-hud.ts, npc-talk.ts

The Magic Pang window (CIFGhaCha): the pure rules of its state byte and
timers, the hud that replays the three native timers from the simulation
clock, and the talk-menu row that opens it.

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const window = await import( "../../src/engine/foundation/ui/gacha-window.ts" );
const { createGachaHud } = await import( "../../src/engine/runtime/ui/hud/gacha-hud.ts" );
const { npcTalkLayout } = await import( "../../src/engine/foundation/ui/npc-talk.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );

// 642170 starts the roll; the first timer fires one tick later.
const TICK = window.GACHA_TICK_MS;
/** @type {any} */
const TICKET = { slot: 20, quantity: 1, typeFlags: 0xf6c };
/** @type {any} */
const VOUCHER = { slot: 20, quantity: 1, typeFlags: 0xf6e };

const rolling = started => ({
	visible: true,
	phase: "rolling",
	npc: 1,
	slot: 20,
	entry: 3,
	started,
	result: null,
	error: null
});
const resolved = ( started, result ) => ({
	visible: true,
	phase: "result",
	npc: 1,
	slot: 20,
	entry: 3,
	started,
	result,
	reward: result === "win" ? { refObjId: 4104, quantity: 1 } : undefined,
	error: null
});
const idle = { visible: true, phase: "idle", npc: 1, slot: null, entry: 0, started: 0, result: null, error: null };

// A hud with prize 3 chosen and a ticket registered, ready to play.
function readyHud( items = new Map( [ [ 20, TICKET ] ] ) ) {
	const hud = createGachaHud();
	const at = /** @type {( gacha: any, now: number ) => any} */ (( gacha, now ) => ({
		gacha,
		now,
		items: [ ...items.values() ]
	}));
	hud.select( 3, false );
	const frame = hud.frame( at( idle, 0 ) );
	assert.equal( hud.drop( 20, TICKET, false, frame.state === 2 ), true );
	return { hud, at, items };
}

test("the instruction state follows the prize and the card, and only state 2 enables Play", () => {
	assert.equal( window.gachaUiState( false, false ), 0 );
	assert.equal( window.gachaUiState( false, true ), 0 );
	assert.equal( window.gachaUiState( true, false ), 1 );
	assert.equal( window.gachaUiState( true, true ), 2 );
	assert.deepEqual( [ 0, 1, 2 ].map( state => window.gachaStateText( /** @type {0 | 1 | 2} */ (state) ) ), [
		"UIIT_MSG_GACHA_CHOICE",
		"UIIT_MSG_GACHA_ENTRY",
		"UIIT_MSG_GACHA_BUTTON_CLICK"
	] );
});

test("reels step by two through frames 0..20 and wrap past 20", () => {
	let counter = 0;
	const frames = [];
	for ( let i = 0; i < 13; i++ ) {
		const step = window.gachaReelStep( counter );
		frames.push( step.frame );
		counter = step.next;
	}
	assert.deepEqual( frames, [ 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 0, 2 ] );
});

test("the effect frame holds in the two native ranges and ends on frame 19", () => {
	assert.equal( window.gachaEffectFrame( 0 ), 1 );
	assert.equal( window.gachaEffectFrame( 7 ), 8 );
	for ( let tick = 7; tick <= 16; tick++ ) assert.equal( window.gachaEffectFrame( tick ), 8, "held at tick " + tick );
	assert.equal( window.gachaEffectFrame( 17 ), 9 );
	assert.equal( window.gachaEffectFrame( window.GACHA_EFFECT_LAST_TICK ), 19 );
});

test("sheet frames sit row-major in cells of the frame size", () => {
	assert.deepEqual( window.gachaSheetFrameRect( window.gachaSheet( "prepare" ), 5, [ 256, 256 ] ), [
		0.25,
		0.25,
		0.25,
		0.25
	] );
	assert.deepEqual( window.gachaSheetFrameRect( window.gachaSheet( "success" ), 14, [ 256, 512 ] ), [
		0.5,
		243 / 512,
		0.25,
		81 / 512
	] );
});

test("prize controls 100..111 choose entries 1..12 and nothing else does", () => {
	assert.equal( window.gachaPrizeEntry( 100 ), 1 );
	assert.equal( window.gachaPrizeEntry( 111 ), 12 );
	assert.equal( window.gachaPrizeEntry( 99 ), null );
	assert.equal( window.gachaPrizeEntry( 112 ), null );
});

test("a card registers only after a prize is chosen, and Play then names the prize and the bag slot", () => {
	const hud = createGachaHud();
	const items = new Map( [ [ 20, TICKET ] ] );
	const at = /** @type {( gacha: any, now: number ) => any} */ (( gacha, now ) => ({
		gacha,
		now,
		items: [ ...items.values() ]
	}));
	assert.equal( hud.drop( 20, TICKET, false, false ), false, "no prize chosen yet" );
	assert.equal( hud.frame( at( idle, 0 ) ).state, 0 );
	assert.equal( hud.select( 3, false ), true );
	assert.equal( hud.frame( at( idle, 0 ) ).state, 1 );
	assert.equal( hud.drop( 20, VOUCHER, false, false ), false, "a voucher is not a card" );
	assert.equal( hud.drop( 5, { ...TICKET, slot: 5 }, false, false ), false, "equipment slots are not the bag" );
	assert.equal( hud.drop( 20, TICKET, true, false ), false, "refused while a roll is in flight" );
	assert.equal( hud.drop( 20, TICKET, false, false ), true );
	const frame = hud.frame( at( idle, 0 ) );
	assert.equal( frame.state, 2 );
	assert.equal( frame.playEnabled, true );
	assert.deepEqual( hud.play( at( idle, 0 ) ), { entry: 3, slot: 20 } );
});

test("Play is refused without a card, and a card that left the bag disables it", () => {
	const { hud, at, items } = readyHud();
	assert.notEqual( hud.play( at( idle, 0 ) ), null );
	items.delete( 20 );
	assert.equal( hud.frame( at( idle, 0 ) ).state, 1 );
	assert.equal( hud.play( at( idle, 0 ) ), null );
	const empty = createGachaHud();
	assert.equal( empty.play( at( idle, 0 ) ), null );
});

test("dragging the card back onto the bag empties the slot, and not during a roll", () => {
	const { hud, at } = readyHud();
	assert.equal( hud.clearCard( true ), false );
	assert.equal( hud.clearCard( false ), true );
	assert.equal( hud.frame( at( idle, 0 ) ).state, 1 );
	assert.equal( hud.clearCard( false ), false );
});

test("prize and card changes are refused while a roll is in flight", () => {
	const { hud, at } = readyHud();
	const busy = hud.frame( at( rolling( 0 ), 0 ) );
	assert.equal( busy.busy, true );
	assert.equal( busy.playEnabled, false );
	assert.equal( hud.select( 4, busy.busy ), false );
	assert.equal( hud.frame( at( { ...rolling( 0 ), phase: "waiting" }, 4500 ) ).busy, true );
});

test("reels spin every tick for 4000 ms, then show the original texture", () => {
	const { hud, at } = readyHud();
	assert.equal( hud.frame( at( rolling( 1000 ), 1000 ) ).reelFrame, null, "before the first tick" );
	assert.equal( hud.frame( at( rolling( 1000 ), 1000 + TICK ) ).reelFrame, 0 );
	assert.equal( hud.frame( at( rolling( 1000 ), 1000 + 2 * TICK ) ).reelFrame, 2 );
	assert.equal( hud.frame( at( rolling( 1000 ), 1000 + 11 * TICK ) ).reelFrame, 20 );
	assert.equal( hud.frame( at( rolling( 1000 ), 1000 + 12 * TICK ) ).reelFrame, 0, "wraps after frame 20" );
	assert.equal( hud.frame( at( rolling( 1000 ), 1000 + window.GACHA_ROLL_MS ) ).reelFrame, null );
});

test("the effect shows frame 1 then advances, holding where native holds", () => {
	const { hud, at } = readyHud();
	const started = 5000;
	assert.equal( hud.frame( at( rolling( started ), started + TICK ) ).effectFrame, 1 );
	assert.equal( hud.frame( at( rolling( started ), started + 8 * TICK ) ).effectFrame, 8 );
	assert.equal( hud.frame( at( rolling( started ), started + 9 * TICK ) ).effectFrame, 8, "held" );
	assert.equal( hud.frame( at( rolling( started ), started + 41 * TICK ) ).effectFrame, 19 );
	assert.equal( hud.frame( at( rolling( started ), started + 42 * TICK ) ).effectFrame, 1, "run ended" );
});

test("a win known before tick 41 plays the success sheet from frame 1 to 14, then the prepare sheet stops", () => {
	const { hud, at } = readyHud();
	const started = 0;
	hud.frame( at( rolling( started ), 3000 ) );
	hud.frame( at( resolved( started, "win" ), 4150 ) );
	const end = started + 42 * TICK;
	const first = hud.frame( at( resolved( started, "win" ), end ) );
	assert.equal( first.deco?.sheet, "success" );
	assert.equal( first.deco?.frame, 1 );
	const mid = hud.frame( at( resolved( started, "win" ), end + 5 * TICK ) );
	assert.equal( mid.deco?.frame, 6 );
	const done = hud.frame( at( resolved( started, "win" ), end + 14 * TICK ) );
	assert.equal( done.deco, null );
});

test("a result that arrives after tick 41 plays no success sheet", () => {
	const { hud, at } = readyHud();
	hud.frame( at( rolling( 0 ), 4100 ) );
	assert.equal( hud.frame( at( rolling( 0 ), 4200 ) ).deco, null, "run ended without a win flag" );
	const late = hud.frame( at( resolved( 0, "win" ), 4300 ) );
	assert.equal( late.deco, null );
	assert.equal( late.result, "win" );
});

test("a win flag left by one roll plays the success sheet at the end of the next roll", () => {
	const { hud, at } = readyHud();
	hud.frame( at( rolling( 0 ), 4150 ) );
	hud.frame( at( resolved( 0, "win" ), 4150 ) );
	hud.frame( at( resolved( 0, "win" ), 4200 + 14 * TICK ) );
	const second = 20000;
	// No result by tick 41 of the second roll: native reads the flag the first one left.
	hud.frame( at( rolling( second ), second + 4000 ) );
	const end = hud.frame( at( rolling( second ), second + 42 * TICK ) );
	assert.equal( end.deco?.sheet, "success" );
});

test("a loss that arrives before tick 41 clears the flag, so no success sheet plays", () => {
	const { hud, at } = readyHud();
	hud.frame( at( resolved( 0, "win" ), 4150 ) );
	hud.frame( at( resolved( 0, "win" ), 8000 ) );
	const second = 20000;
	hud.frame( at( rolling( second ), second + 4000 ) );
	hud.frame( at( resolved( second, "lose" ), second + 4150 ) );
	assert.equal( hud.frame( at( resolved( second, "lose" ), second + 4300 ) ).deco, null );
});

test("a loss at the end of a run leaves the tick counter at 1, so the next run ends a tick sooner", () => {
	const { hud, at } = readyHud();
	hud.frame( at( rolling( 0 ), 4200 ) );
	const second = 20000;
	// The first run ended on its 42nd firing; this one starts at tick 1 and ends on its 41st.
	hud.frame( at( rolling( second ), second + 40 * TICK ) );
	assert.equal( hud.frame( at( rolling( second ), second + 40 * TICK ) ).effectFrame > 1, true, "still running" );
	assert.equal( hud.frame( at( rolling( second ), second + 41 * TICK ) ).effectFrame, 1, "ended" );
});

test("the instruction line shows the result until the next click, then follows the state again", () => {
	const { hud, at, items } = readyHud();
	hud.frame( at( rolling( 0 ), 4000 ) );
	items.set( 20, VOUCHER );
	const shown = hud.frame( at( resolved( 0, "lose" ), 4100 ) );
	assert.equal( shown.text, null );
	assert.equal( shown.result, "lose" );
	assert.equal( shown.state, 1, "the voucher is not a card" );
	assert.equal( hud.select( 2, false ), true );
	const after = hud.frame( at( resolved( 0, "lose" ), 4200 ) );
	assert.equal( after.result, null );
	assert.equal( after.text, "UIIT_MSG_GACHA_ENTRY" );
});

test("a win reports the reward for the instruction line", () => {
	const { hud, at } = readyHud();
	hud.frame( at( rolling( 0 ), 4000 ) );
	const shown = hud.frame( at( resolved( 0, "win" ), 4100 ) );
	assert.deepEqual( shown.reward, { refObjId: 4104, quantity: 1 } );
});

test("a stalled frame catches the timers up without replaying unbounded ticks", () => {
	const { hud, at } = readyHud();
	hud.frame( at( rolling( 0 ), TICK ) );
	const frame = hud.frame( at( resolved( 0, "lose" ), 10 * 60 * 1000 ) );
	assert.equal( frame.reelFrame, null );
	assert.equal( frame.deco, null );
	assert.equal( frame.effectFrame, 1 );
});

test("the talk menu lists the Magic Pang row only for the machine's capability", async () => {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/if_npctalk.json", "utf8" ) )
	);
	const state = /** @type {any} */ ({ phase: "menu", gid: 7 });
	const render = canGacha =>
		npcTalkLayout( {
			state,
			layout,
			origin: [ 100, 200 ],
			copy: key => key,
			measure: () => 8,
			draw: () => [],
			size: () => [ 16, 16 ],
			hover: null,
			pressed: null,
			top: 0,
			canGacha
		} ).controls;
	const withRow = render( true ).find( control => control.id === "gacha-open" );
	assert.ok( withRow, "row present with the capability" );
	assert.match( withRow.label, /SN_TALK_CH_GACHA_MACHINE_2/ );
	assert.equal( render( false ).some( control => control.id === "gacha-open" ), false );
});

test("a result voucher is the 3.3.14.2 type word, and nothing else is", () => {
	assert.equal( window.isGachaVoucher( 0x176c ), true );
	assert.equal( window.isGachaVoucher( 0x176d ), true, "bit 0 is not tested" );
	assert.equal( window.isGachaVoucher( 0x176e ), false, "bit 1 set" );
	assert.equal( window.isGachaVoucher( 0xf6c ), false, "the 3.3.14.1 ticket" );
	assert.equal( window.isGachaVoucher( 0x2c ), false );
	assert.equal( window.isGachaVoucher( 0 ), false );
});

test("Gori's talk row appears only with his capability, and the machine's does not carry it", () => {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/if_npctalk.json", "utf8" ) )
	);
	const state = /** @type {any} */ ({ phase: "menu", gid: 7 });
	const rows =
		/** @type {( input: { canGacha?: boolean; canGachaOperator?: boolean } ) => string[]} */ (input =>
			npcTalkLayout( {
				state,
				layout,
				origin: [ 100, 200 ],
				copy: key => key,
				measure: () => 8,
				draw: () => [],
				size: () => [ 16, 16 ],
				hover: null,
				pressed: null,
				top: 0,
				...input
			} ).controls.map( control => control.id ));
	assert.equal( rows( { canGachaOperator: true } ).includes( "gacha-exchange" ), true );
	assert.equal( rows( { canGachaOperator: true } ).includes( "gacha-open" ), false );
	assert.equal( rows( { canGacha: true } ).includes( "gacha-exchange" ), false );
	assert.equal( window.GACHA_OPERATOR_FUNCTION, 0x20000 );
});

test("the withdrawal row appears only with the herbalist's capability", () => {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/if_npctalk.json", "utf8" ) )
	);
	const state = /** @type {any} */ ({ phase: "menu", gid: 7 });
	const rows = input =>
		npcTalkLayout( {
			state,
			layout,
			origin: [ 100, 200 ],
			copy: key => key,
			measure: () => 8,
			draw: () => [],
			size: () => [ 16, 16 ],
			hover: null,
			pressed: null,
			top: 0,
			...input
		} ).controls.map( control => control.id );
	assert.equal( rows( { canWithdrawSkill: true } ).includes( "npc-withdraw" ), true );
	assert.equal( rows( {} ).includes( "npc-withdraw" ), false );
});

test("the no-voucher answer belongs to one operator and clears", () => {
	const hud = createGachaHud();
	assert.equal( hud.noVoucherFor(), 0 );
	hud.noVoucher( 18 );
	assert.equal( hud.noVoucherFor(), 18 );
	hud.clearNoVoucher();
	assert.equal( hud.noVoucherFor(), 0 );
});
