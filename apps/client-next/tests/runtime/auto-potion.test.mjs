/*
===========================================================================

auto-potion.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const p = await load( "src/engine/foundation/gameplay/auto-potion.ts" );
const { createGameplay } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
test("native potion defaults, packed bits and exact save bytes", () => {
	assert.deepEqual( p.admittedAutoPotion( { hp: 65535, mp: 65535, cure: 65535, timing: 0 } ), p.defaultAutoPotion() );
	assert.equal( p.admittedAutoPotion( { hp: 0, mp: 0, cure: 0, timing: 1 } ).hp, 0x3211 );
	assert.deepEqual( p.autoPotionEntry( 0xb211 ), { enabled: true, percent: 50, slot: 1 } );
	assert.equal( p.autoPotionEntry( 1 ).slot, 40 );
	for ( let slot = 0; slot <= 40; slot++ ) {
		for ( const enabled of [ false, true ] ) {
			for ( const percent of [ 0, 50, 100, 127 ] ) {
				assert.deepEqual( p.autoPotionEntry( p.autoPotionWord( { slot, enabled, percent } ) ), {
					slot,
					enabled,
					percent
				} );
			}
		}
	}
	const frame = p.autoPotionSave( { hp: 0xb211, mp: 0xb212, cure: 0x8013, timing: 0x8a } );
	assert.equal( frame.opcode, 0x7541 );
	assert.deepEqual( [ ...frame.payload ], [ 2, 17, 178, 18, 178, 19, 128, 138 ] );
	assert.equal( p.autoPotionDelay( { ...p.defaultAutoPotion(), timing: 127 } ), 500 );
	assert.equal( p.autoPotionDelay( { ...p.defaultAutoPotion(), timing: 255 } ), 12700 );
	for ( const value of [ -1, 65536, NaN, 1.5 ] ) {
		assert.throws( () => p.autoPotionSave( { hp: value, mp: 0, cure: 0, timing: 1 } ) );
	}
});
test("native HP, MP and cure condition branches remain independent", () => {
	const entry = { enabled: true, percent: 50, slot: 1 },
		facts = { alive: true, hp: 50, mp: 50, maxHp: 100, maxMp: 100, abnormal: 0 };
	assert.equal( p.autoPotionEligible( 0, entry, facts ), true );
	assert.equal( p.autoPotionEligible( 0, entry, { ...facts, hp: 51 } ), false );
	assert.equal( p.autoPotionEligible( 0, entry, { ...facts, abnormal: 32 } ), false );
	assert.equal( p.autoPotionEligible( 1, entry, { ...facts, abnormal: 32 } ), true );
	assert.equal( p.autoPotionEligible( 2, entry, facts ), false );
	assert.equal( p.autoPotionEligible( 2, entry, { ...facts, abnormal: 1 } ), true );
	assert.equal( p.autoPotionEligible( 2, entry, { ...facts, abnormal: 0x4001 } ), false );
	for ( const kind of [ 0, 1, 2 ] ) {
		assert.equal( p.autoPotionEligible( kind, entry, { ...facts, alive: false, abnormal: 1 } ), false );
	}
});
test("settings commit only after successful enqueue; bootstrap replaces configuration", () => {
	let reject = true;
	const frames = [],
		g = createGameplay( f => {
			if ( reject ) throw Error( "closed" );
			frames.push( f );
		} );
	g.bootstrap( {} );
	g.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	const settings = { hp: 0xb211, mp: 0x3212, cure: 0x13, timing: 0x8a };
	assert.throws( () => g.command( { kind: "auto-potion-save", settings }, 0 ), /closed/ );
	assert.deepEqual( g.take().autoPotion, p.defaultAutoPotion() );
	reject = false;
	g.command( { kind: "auto-potion-save", settings }, 1 );
	assert.deepEqual( g.take().autoPotion, settings );
	assert.equal( frames.length, 1 );
	g.bootstrap( { character: { autoPotion: { hp: 0, mp: 0, cure: 0, timing: 0 } } } );
	assert.deepEqual( g.take().autoPotion, p.defaultAutoPotion() );
	g.dispose();
});

/*
================
automatic
================
*/
function automatic( options = {} ) {
	const sent = [],
		g = createGameplay( f => sent.push( f ) ),
		local = { gid: 1, countryByte9c: 0, regionId: 257, x: 0, y: 0, z: 0, heading: 0, appearanceState: [ 1, 0, 0 ] };
	g.bootstrap( {
		character: {
			hp: 40,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			autoPotion: { hp: 0xb211, mp: 0x3212, cure: 0x13, timing: options.timing ?? 0x8a },
			quickSlots: [ { slot: 1, kind: 0x46, payload: 0 } ]
		},
		refItemSnapshot: [ {
			refObjId: 1,
			typeFlags: options.typeFlags ?? 0xec,
			nativeFields: { useCooldownDuration528: 1500 }
		} ],
		equipItems: [ { slot: 13, refObjId: 1, body: [ 1, 0, 0, 0, 10, 0 ] } ]
	} );
	g.seed( local );
	return { sent, g, local };
}
/*
================
abnormal
================
*/
function abnormal( mask ) {
	const payload = new Uint8Array( 11 ), v = new DataView( payload.buffer );
	v.setUint32( 0, 1, true );
	payload[6] = 4;
	v.setUint32( 7, mask, true );
	return { opcode: 0x33a6, payload };
}
test("automatic use respects the inventory transaction, timer, death and teardown", () => {
	const { sent, g, local } = automatic();
	g.receive( vitals( 40 ), 0 );
	g.step( 0, local );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].opcode, 0x75bd );
	assert.deepEqual( [ ...sent[0].payload ], [ 13, 0xec, 0 ] );
	g.step( 999, local );
	g.step( 1000, local );
	assert.equal( sent.length, 1, "pending use prevents duplicate requests" );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 1 ) }, 1001 );
	g.step( 1999, local );
	assert.equal( sent.length, 1 );
	g.step( 2000, local );
	assert.equal( sent.length, 2 );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 1 ) }, 2001 );
	g.step( 3000, { ...local, appearanceState: [ 2, 0, 0 ] } );
	assert.equal( sent.length, 2 );
	g.step( 3001, local );
	g.receive( vitals( 40 ), 3001 );
	assert.equal( sent.length, 3 );
	g.resetWorld();
	g.step( 10000 );
	assert.equal( sent.length, 3 );
	g.dispose();
	g.step( 20000, local );
	assert.equal( sent.length, 3 );
});
test("normal abnormal-state updates block HP without disarming the native retry timer", () => {
	const { sent, g, local } = automatic();
	g.receive( abnormal( 0x20 ), 0 );
	g.receive( vitals( 40 ), 0 );
	g.step( 0, local );
	assert.equal( sent.length, 0 );
	g.receive( abnormal( 0 ), 100 );
	g.step( 100, local );
	assert.equal( sent.length, 0, "clearing block does not bypass the armed timer" );
	g.step( 1000, local );
	assert.equal( sent.length, 1 );
	g.dispose();
});
test("automatic binding cannot invoke a skill, equipment, or unrelated consumable family", () => {
	for ( const kind of [ 0, 0x49, 0x4a, 0x25 ] ) {
		assert.equal( p.autoPotionItemSlot( { kind, slot: 1, payload: 0 }, [ { slot: 13, typeFlags: 0xec } ] ), null );
	}
	for ( const tid of [ 0x2c, 0x6c, 0x1ec, 0x2ec ] ) {
		assert.equal(
			p.autoPotionItemSlot( { kind: 0x46, slot: 1, payload: 0 }, [ { slot: 13, typeFlags: tid } ] ),
			null
		);
	}
	for ( const tid of [ 0xec, 0x16c ] ) {
		assert.equal(
			p.autoPotionItemSlot( { kind: 0x46, slot: 1, payload: 0 }, [ { slot: 13, typeFlags: tid } ] ),
			13
		);
	}
});

/*
================
vitals
================
*/
function vitals( hp ) {
	const payload = new Uint8Array( 11 ), view = new DataView( payload.buffer );
	view.setUint32( 0, 1, true );
	payload[6] = 1;
	view.setUint32( 7, hp, true );
	return { opcode: 0x33a6, payload };
}

test("armed retry survives recovery and damage after the item cooldown expires", () => {
	const { sent, g, local } = automatic( { timing: 0x9e, typeFlags: 0x8ec } );
	g.receive( vitals( 40 ), 0 );
	g.step( 0, local );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 1, 13, 9, 0, 0xec, 8 ) }, 1 );
	assert.equal( g.take().inventory.find( row => row.slot === 13 ).quantity, 9 );
	g.receive( vitals( 80 ), 1400 );
	g.step( 1400, local );
	g.receive( vitals( 40 ), 1500 );
	g.step( 1500, local );
	assert.equal( sent.length, 1, "threshold crossing must not bypass the armed retry even after cooldown" );
	g.step( 2999, local );
	assert.equal( sent.length, 1 );
	g.step( 3000, local );
	assert.equal( sent.length, 2, "original timer still retries below threshold" );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 1 ) }, 3001 );
	g.receive( vitals( 80 ), 4500 );
	g.step( 6000, local );
	g.receive( vitals( 40 ), 6001 );
	g.step( 6001, local );
	assert.equal( sent.length, 3, "a callback that saw recovery disarmed the timer" );
	g.dispose();
});

test("threshold uses the native float32 percentage and unsigned result", () => {
	for ( const kind of [ 0, 1 ] ) {
		const facts = { alive: true, hp: 53, mp: 53, maxHp: 100, maxMp: 100, abnormal: 0 };
		const entry = { enabled: true, percent: 53, slot: 1 };
		assert.equal( p.autoPotionActive( kind, entry, facts ), false, "53% is stored below 0.53 and truncates to 52" );
		assert.equal( p.autoPotionActive( kind, entry, { ...facts, hp: 52, mp: 52 } ), true );
	}
	assert.equal( p.autoPotionDelay( { ...p.defaultAutoPotion(), timing: 0x80 } ), 1 );
});

test("inactive channels require their own vitals notification, not a frame or configuration save", () => {
	const { g, local, sent } = automatic();
	g.step( 0, local );
	g.step( 5000, local );
	assert.equal( sent.length, 0 );
	g.receive( abnormal( 1 ), 5001 );
	assert.equal( sent.length, 0, "abnormal updates cannot start HP" );
	const settings = { hp: 0xb211, mp: 0x3212, cure: 0x13, timing: 0x8a };
	g.command( { kind: "auto-potion-save", settings }, 5002 );
	assert.equal( sent.length, 0, "unchanged OK does not send or restart channels" );
	g.receive( vitals( 40 ), 5003 );
	assert.equal( sent.length, 1 );
	g.dispose();
});

test("channel flag, timer registration, and configured delay have independent lifetimes", () => {
	const facts = { alive: true, hp: 10, mp: 10, maxHp: 100, maxMp: 100, abnormal: 1 };
	for ( const kind of [ 0, 1, 2 ] ) {
		let settings = { hp: 0xb211, mp: 0xb212, cure: 0x8013, timing: 0x8a };
		let timer = p.emptyAutoPotionTimer();
		const run = ( event, now ) => {
			const r = p.checkAutoPotionTimer( timer, { kind, settings, facts, now, event } );
			timer = r.timer;
			return r.use;
		};
		assert.equal( run( "timer", 0 ), false );
		assert.equal( run( "vitals", 10 ), true );
		assert.deepEqual( timer, { active: true, due: 1010, period: 1000 } );
		settings = { ...settings, timing: 0x94 };
		assert.equal( run( "vitals", 500 ), false );
		assert.equal( run( "timer", 1010 ), true );
		assert.equal( timer.due, 2010, "delay edit preserves registered period" );
		const key = [ "hp", "mp", "cure" ][kind];
		settings = { ...settings, [key]: settings[key] & 0x7fff };
		timer = { ...timer, active: false };
		assert.equal( run( "timer", 2010 ), false );
		assert.equal( timer.due, 3010, "disabled callback keeps registration" );
		settings = { ...settings, [key]: settings[key] | 0x8000 };
		assert.equal( run( "vitals", 2200 ), true );
		assert.equal( timer.due, 3010, "re-enable cannot replace existing timer" );
		const r = p.checkAutoPotionTimer( timer, {
			kind,
			settings,
			facts: { ...facts, alive: false },
			now: 3010,
			event: "timer"
		} );
		timer = r.timer;
		assert.equal( timer.due, null );
		assert.equal( run( "vitals", 3100 ), true );
		assert.equal( timer.period, 2000, "a new registration uses the edited delay" );
	}
});

test("configuration draft clamps controls without changing committed packed settings", () => {
	const settings = { hp: 0x7f00, mp: 0, cure: 0, timing: 0xff };
	const draft = p.autoPotionDraft( settings );
	assert.deepEqual( p.autoPotionEntry( draft.hp ), { enabled: false, percent: 100, slot: 1 } );
	assert.deepEqual( p.autoPotionEntry( draft.mp ), { enabled: false, percent: 1, slot: 1 } );
	assert.equal( p.autoPotionEntry( draft.cure ).slot, 40 );
	assert.equal( draft.timing, 0xdf );
	assert.deepEqual( settings, { hp: 0x7f00, mp: 0, cure: 0, timing: 0xff } );
	assert.equal( p.autoPotionDraft( { ...settings, timing: 0x80 } ).timing, 0x85 );
});
test("automatic companion items without a target never abort a frame or vitals receipt", () => {
	for ( const typeFlags of [ 0x20ec, 0x28ec, 0x30ec, 0x38ec, 0x48ec, 0x396c ] ) {
		const { g, local, sent } = automatic( { typeFlags } );
		assert.doesNotThrow( () => g.receive( vitals( 40 ), 0 ) );
		assert.doesNotThrow( () => g.step( 1000, local ) );
		assert.equal( sent.length, 0 );
		g.dispose();
	}
});

test("original x86 corpus covers every packed word and filter word plus check branch partitions", () => {
	const corpus = JSON.parse(
		readFileSync( new URL( "../fixtures/native/auto-potion-core.json", import.meta.url ), "utf8" )
	);
	const filter = [], packed = [], checks = [];
	for ( let word = 0; word < corpus.filter.words; word++ ) {
		filter.push(
			Number(
				p.autoPotionItemSlot( { slot: 1, kind: 0x46, payload: 0 }, [ { slot: 13, typeFlags: word } ] ) !== null
			)
		);
		const entry = p.autoPotionEntry( word ), timing = word & 255, delay = (timing & 127) * 100;
		packed.push( Number( entry.enabled ), entry.percent, entry.slot, timing >>> 7, delay & 255, delay >>> 8 );
	}
	const d = corpus.checks.domains;
	for ( const kind of d.kind ) {
		for ( const enabled of d.enabled ) {
			for ( const active of d.active ) {
				for ( const life of d.life ) {
					for ( const current of d.current ) {
						for ( const abnormal of d.abnormal ) {
							for ( const timing of d.timing ) {
								const word = p.autoPotionWord( { enabled: !!enabled, percent: 50, slot: 1 } );
								const r = p.checkAutoPotionTimer( {
									active: !!active,
									due: active ? 0 : null,
									period: active ? 1000 : 0
								}, {
									kind,
									settings: { hp: word, mp: word, cure: word, timing },
									facts: {
										alive: !!(life & 1),
										hp: current,
										mp: current,
										maxHp: 100,
										maxMp: 100,
										abnormal
									},
									now: 0,
									event: active ? "timer" : "vitals"
								} );
								checks.push( Number( r.timer.active ), Number( r.use ) );
							}
						}
					}
				}
			}
		}
	}
	assert.equal( createHash( "sha256" ).update( Uint8Array.from( filter ) ).digest( "hex" ), corpus.filter.sha256 );
	assert.equal( createHash( "sha256" ).update( Uint8Array.from( packed ) ).digest( "hex" ), corpus.packed.sha256 );
	assert.equal( checks.length, corpus.checks.cases * 2 );
	assert.equal(
		createHash( "sha256" ).update( Uint8Array.from( checks ) ).digest( "hex" ),
		corpus.checks.activeAndUseSha256
	);
});

test("empty cure combos retain independent native selections until apply", () => {
	const draft = p.autoPotionDraft( { hp: 0, mp: 0, cure: 0, timing: 0x8a } );
	assert.equal( draft.curePage, -1 );
	assert.equal( draft.cureKey, -1 );
	assert.equal( p.autoPotionEntry( draft.cure ).slot, 40 );
	const page = p.autoPotionDraftChoice( draft, "cure", "page", 0 );
	assert.equal( page.curePage, 0 );
	assert.equal( page.cureKey, -1 );
	assert.equal( p.autoPotionEntry( page.cure ).slot, 0 );
	const key = p.autoPotionDraftChoice( draft, "cure", "key", 1 );
	assert.equal( key.curePage, -1 );
	assert.equal( key.cureKey, 0 );
	assert.equal( p.autoPotionEntry( key.cure ).slot, 40 );
	assert.equal( p.autoPotionEntry( p.autoPotionDraftChoice( key, "cure", "page", 2 ).cure ).slot, 21 );
});

test("a lost item receipt leaves automatic use blocked through subsequent frames", () => {
	const { g, local, sent } = automatic();
	g.receive( vitals( 40 ), 0 );
	assert.equal( sent.length, 1 );
	assert.throws( () => g.step( 10001, local ), /timed out/ );
	assert.throws( () => g.step( 11001, local ), /timed out/ );
	assert.equal( g.take().inventoryPending, true );
	assert.equal( sent.length, 1 );
	g.dispose();
});

test("drag admission changes preserve the armed retry", () => {
	const { g, local, sent } = automatic();
	g.command( { kind: "auto-potion-input", blocked: true }, 0 );
	g.receive( vitals( 40 ), 0 );
	assert.equal( sent.length, 0 );
	g.command( { kind: "auto-potion-input", blocked: false }, 100 );
	g.step( 100, local );
	assert.equal( sent.length, 0 );
	g.step( 1000, local );
	assert.equal( sent.length, 1 );
	g.receive( { opcode: 0xb5bd, payload: Uint8Array.of( 2, 1 ) }, 1001 );
	g.command( { kind: "auto-potion-input", blocked: true }, 1002 );
	g.step( 2000, local );
	assert.equal( sent.length, 1 );
	g.command( { kind: "auto-potion-input", blocked: false }, 2001 );
	g.step( 2999, local );
	assert.equal( sent.length, 1 );
	g.step( 3000, local );
	assert.equal( sent.length, 2 );
	g.dispose();
});

test("Item Mall refusal publishes the native interaction notice while dragging stays silent", () => {
	const { g, local, sent } = automatic();
	g.command( { kind: "auto-potion-input", blocked: false, itemMallOpen: true }, 0 );
	g.receive( vitals( 40 ), 0 );
	const first = g.take();
	assert.equal( first.notices.at( -1 )?.key, "UIIT_MSG_STRGERR_CANT_USEITEM_WHILE_INTERACT" );
	assert.equal( sent.length, 0 );
	g.command( { kind: "auto-potion-input", blocked: true, itemMallOpen: true }, 1 );
	g.step( 1000, local );
	assert.deepEqual( g.take()?.notices ?? first.notices, first.notices );
	g.command( { kind: "auto-potion-input", blocked: false, itemMallOpen: false }, 1001 );
	g.step( 1999, local );
	assert.equal( sent.length, 0 );
	g.step( 2000, local );
	assert.equal( sent.length, 1 );
	g.dispose();
});

for ( const visitor of [ false, true ] ) {
	test(`visible ${visitor ? "visitor" : "owner"} stall refuses automatic use until its close receipt`, () => {
		const { g, local, sent } = automatic();
		const opened = visitor ?
			{ opcode: 0xb61f, payload: Uint8Array.of( 1, 2, 0, 0, 0, 0, 0, 1, 0, 255, 0 ) } :
			{ opcode: 0xb049, payload: Uint8Array.of( 1 ) };
		g.receive( opened, 0 );
		g.receive( vitals( 40 ), 0 );
		assert.equal( sent.length, 0 );
		assert.equal( g.take().notices.at( -1 )?.key, "UIIT_MSG_STRGERR_CANT_USEITEM_WHILE_INTERACT" );
		g.step( 1000, local );
		assert.equal( sent.length, 0 );
		g.receive(
			visitor ?
				{ opcode: 0xb6e7, payload: Uint8Array.of( 1 ) } :
				{ opcode: 0xb42c, payload: Uint8Array.of( 1 ) },
			1001
		);
		g.step( 1999, local );
		assert.equal( sent.length, 0 );
		g.step( 2000, local );
		assert.equal( sent.length, 1 );
		assert.equal( sent[0].opcode, 0x75bd );
		g.dispose();
	});
}

test("the separate stall network does not refuse automatic potion use", () => {
	const { g, sent } = automatic();
	g.command( { kind: "stall-network-open", open: true }, 0 );
	g.receive( vitals( 40 ), 0 );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].opcode, 0x75bd );
	g.dispose();
});
