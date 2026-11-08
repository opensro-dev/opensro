/*
===========================================================================

companion-potions.test.mjs - shared companion recovery and T dialog tests

Uses the production selector and UI. No characters or inventories are modified.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { companionAutoPotion, playerVigorAutoPotion, transportPotionBlocked } = await import(
	"../../src/engine/foundation/gameplay/transport-auto-potion.ts"
);
const { createCompanionPotions, defaultCompanionPotions } = await import(
	"../../src/engine/runtime/ui/hud/companion-potions.ts"
);
const { hotbarSlot } = await import( "../../src/engine/foundation/gameplay/quickslots.ts" );
const { autoPotionWord, autoPotionEntry } = await import( "../../src/engine/foundation/gameplay/auto-potion.ts" );
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const HP = 0x6c | 1 << 7 | 4 << 11, CURE = 0x6c | 2 << 7 | 7 << 11;
/*
================
item
================
*/
function item( slot, flags ) {
	return { slot, typeFlags: flags, quantity: 5, refObjId: slot, plus: 0, durability: 0, variance: "0", magic: [] };
}
/*
================
fixture
================
*/
function fixture( change = {} ) {
	const settings = defaultCompanionPotions();
	return {
		records: [ { gid: 41, refObjId: 1, band: 2, hp: 400, mp: 0, status: 0, dead: false }, {
			gid: 42,
			refObjId: 2,
			band: 3,
			hp: 200,
			mp: 0,
			status: 0,
			dead: false
		} ],
		maxima: [ { gid: 41, maxHp: 1000 }, { gid: 42, maxHp: 1000 } ],
		vitals: [],
		items: [ item( 20, HP ), item( 21, CURE ) ],
		cooldowns: [],
		now: 10000,
		blocked: false,
		quickSlots: [ { slot: 1, kind: 0x46, payload: 7 }, { slot: 2, kind: 0x46, payload: 8 } ],
		settings: {
			...settings,
			bindings: {
				...settings.bindings,
				hp: autoPotionWord( { enabled: true, percent: 50, slot: 1 } ),
				cure: autoPotionWord( { enabled: true, percent: 0, slot: 2 } )
			}
		},
		...change
	};
}
test("separate HP thresholds share one bound potion and target their own companion", () => {
	const f = fixture();
	assert.deepEqual( companionAutoPotion( f ), { slot: 20, companionGid: 41 } );
	f.settings = { ...f.settings, transport: autoPotionWord( { enabled: true, percent: 30, slot: 0 } ) };
	assert.deepEqual( companionAutoPotion( f ), { slot: 20, companionGid: 42 } );
	f.settings = { ...f.settings, attack: autoPotionWord( { enabled: false, percent: 50, slot: 0 } ) };
	assert.equal( companionAutoPotion( f ), null );
});
test("abnormal status uses the shared pet pill on the afflicted companion only", () => {
	const f = fixture( { vitals: [ { gid: 42, abnormal: 8 } ] } );
	assert.deepEqual( companionAutoPotion( f ), { slot: 21, companionGid: 42 } );
	f.settings = { ...f.settings, bindings: { ...f.settings.bindings, cure: 0 } };
	assert.deepEqual( companionAutoPotion( f ), { slot: 20, companionGid: 41 } );
});
test("any negative status triggers a pill for healthy transports and attack pets", () => {
	const base = fixture();
	for ( const band of [ 1, 2, 3 ] ) {
		const records = [ { ...base.records[0], band, hp: 1000 } ];
		for ( const abnormal of [ 1, 2, 4, 8, 16, 32, 0x80, 0x100, 0x4000 ] ) {
			const f = fixture( { records, vitals: [ { gid: 41, abnormal } ] } );
			assert.deepEqual( companionAutoPotion( f ), { slot: 21, companionGid: 41 } );
			assert.equal( companionAutoPotion( { ...f, blocked: true } ), null );
			assert.equal( companionAutoPotion( { ...f, lastSentMs: 9500 } ), null );
		}
		assert.equal( companionAutoPotion( fixture( { records } ) ), null );
		assert.equal( companionAutoPotion( fixture( { records, vitals: [ { gid: 41, abnormal: 0 } ] } ) ), null );
	}
});
test("missing bindings, player potions, empty stacks and cooldowns never consume items", () => {
	assert.equal( companionAutoPotion( fixture( { quickSlots: [] } ) ), null );
	assert.equal( companionAutoPotion( fixture( { items: [ item( 20, 0x6c | 1 << 7 | 1 << 11 ) ] } ) ), null );
	assert.equal( companionAutoPotion( fixture( { items: [ { ...item( 20, HP ), quantity: 0 } ] } ) ), null );
	assert.equal( companionAutoPotion( fixture( { blocked: true } ) ), null );
	assert.equal( companionAutoPotion( fixture( { lastSentMs: 9500 } ) ), null );
});
test("healthy, dead, pickup and unknown-max companions do not drink", () => {
	const f = fixture();
	assert.equal( companionAutoPotion( { ...f, maxima: [] } ), null );
	assert.equal( companionAutoPotion( { ...f, records: f.records.map( r => ({ ...r, hp: 1000 }) ) } ), null );
	assert.equal( companionAutoPotion( { ...f, records: f.records.map( r => ({ ...r, dead: true }) ) } ), null );
	assert.equal( companionAutoPotion( { ...f, records: f.records.map( r => ({ ...r, band: 4 }) ) } ), null );
});
test("attack pet uses the effective maximum instead of a native reference maximum", () => {
	const f = fixture();
	f.settings = { ...f.settings, transport: 0 };
	assert.equal( companionAutoPotion( { ...f, maxima: [ { gid: 42, maxHp: 300 } ] } ), null );
	assert.deepEqual( companionAutoPotion( { ...f, maxima: [ { gid: 42, maxHp: 1000 } ] } ), {
		slot: 20,
		companionGid: 42
	} );
});
test("OK persists per character; cancel and malformed preferences cannot enable arbitrary targets", () => {
	const values = new Map(),
		storage = { getItem: key => values.get( key ) ?? null, setItem: ( key, value ) => values.set( key, value ) };
	const owner = createCompanionPotions( storage );
	owner.sync( "server|A" );
	owner.begin();
	owner.percent( "attack", 80 );
	owner.begin();
	assert.equal( autoPotionEntry( owner.state().draft.attack ).percent, 50 );
	owner.percent( "attack", 80 );
	owner.choice( "hp", "page", 2 );
	owner.save();
	owner.sent( 50 );
	owner.sync( "server|B" );
	assert.equal( autoPotionEntry( owner.state().saved.attack ).percent, 50 );
	assert.equal( owner.state().lastSentMs, undefined );
	owner.sync( "server|A" );
	assert.equal( autoPotionEntry( owner.state().saved.attack ).percent, 80 );
	assert.equal( autoPotionEntry( owner.state().saved.bindings.hp ).slot, 21 );
	values.set( "sro.companion-potions.v1:bad", JSON.stringify( { transport: -1 } ) );
	owner.sync( "bad" );
	assert.equal( autoPotionEntry( owner.state().saved.attack ).percent, 50 );
});
/*
================
openDialog
================
*/
function openDialog( t, enabled = true, viewport = {} ) {
	const sent = [],
		f = uiFixture( message => {
			if ( message.kind === "gameplay" ) sent.push( message.command );
		} );
	t.after( () => f.dispose() );
	Object.assign( f.state, viewport );
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { transportAutoPotion: enabled } ) } );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "activate", id: "open-window:Auto Potion" } );
	if ( enabled ) f.ui.event( { kind: "activate", id: "potion-tab:companion" } );
	const dialog = f.ui.step( f.state, 100 );
	assert.ok( dialog );
	return { f, sent, dialog };
}
test("T dialog retains player controls, adds exactly two HP thresholds and shared bindings", t => {
	const { f, dialog } = openDialog( t );
	assert.ok( !dialog.controls.some( c => c.id === "potion-percent:hp" ) );
	const controls = dialog.controls;
	for (
		const id of [
			"companion-potion-percent:transport",
			"companion-potion-percent:attack",
			"potion-combo:companionHp:page",
			"potion-combo:companionCure:page"
		]
	) assert.ok( controls.some( c => c.id === id ), id );
	f.ui.event( {
		kind: "edit",
		id: "companion-potion-percent:attack",
		value: "75",
		start: 0,
		end: 2,
		composing: false
	} );
	const changed = f.ui.step( f.state, 200 );
	assert.ok( changed );
	const slider = changed.controls.find( c => c.id === "companion-potion-percent:attack" );
	assert.ok( slider );
	assert.equal( slider.value, "75" );
});
test("opt-out preserves the original dialog without companion controls", t => {
	const { f, dialog } = openDialog( t, false );
	assert.ok( !dialog.controls.some( c => c.id.startsWith( "companion-potion" ) ) );
});

test("riding mounts use the transport threshold, without admitting pickup pets", () => {
	const f = fixture();
	assert.deepEqual( companionAutoPotion( { ...f, records: [ { ...f.records[0], band: 1 } ] } ), {
		slot: 20,
		companionGid: 41
	} );
	assert.equal( companionAutoPotion( { ...f, records: [ { ...f.records[0], band: 4 } ] } ), null );
});
test("a shared potion cooldown suppresses both companion targets", () => {
	assert.equal(
		companionAutoPotion( fixture( { cooldowns: [ { category: 4, startedAtMs: 9900, durationMs: 1000 } ] } ) ),
		null
	);
});

test("the shared pill stays enabled independently of the HP slider checkboxes", () => {
	const f = fixture( { vitals: [ { gid: 42, abnormal: 8 } ] } );
	f.settings = { ...f.settings, transport: 0, attack: 0 };
	assert.deepEqual( companionAutoPotion( f ), { slot: 21, companionGid: 42 } );
});
test("inventory and interaction locks block both shared recovery channels", () => {
	for (
		const game of [ { inventoryPending: true }, { storage: {} }, { stall: { phase: "owner" } }, {
			stall: { phase: "visitor" }
		}, { npcConversation: { phase: "menu" } } ]
	) assert.equal( transportPotionBlocked( game ), true );
	assert.equal( transportPotionBlocked( {} ), false );
	assert.equal( transportPotionBlocked( { npcConversation: { phase: "closed" } } ), false );
});

test("companion slider arrows work and Cancel discards the draft", t => {
	const { f } = openDialog( t );
	f.ui.event( { kind: "activate", id: "companion-potion-step:transport:1" } );
	const changed = defined( f.ui.step( f.state, 200 ) );
	assert.equal( changed.controls.find( row => row.id === "companion-potion-percent:transport" )?.value, "51" );
	f.ui.event( { kind: "activate", id: "potion-cancel" } );
	f.ui.step( f.state, 300 );
	f.ui.event( { kind: "activate", id: "open-window:Auto Potion" } );
	f.ui.event( { kind: "activate", id: "potion-tab:companion" } );
	const reopened = defined( f.ui.step( f.state, 400 ) );
	assert.equal( reopened.controls.find( row => row.id === "companion-potion-percent:transport" )?.value, "50" );
});

test("tab switching preserves drafts, closes dropdowns and matches the approved pixel grid", t => {
	const { f, dialog } = openDialog( t );
	const slider = defined( dialog.controls.find( c => c.id === "companion-potion-percent:transport" ) );
	const ox = slider.rect[0] - 49, oy = slider.rect[1] - 239;
	/** @type {Array<[string, number[]]>} */
	const grid = [
		[ "potion-tab:character", [ 24, 40, 72, 24 ] ],
		[ "potion-tab:companion", [ 98, 40, 72, 24 ] ],
		[ "companion-potion-enable:transport", [ 30, 213, 16, 16 ] ],
		[ "companion-potion-percent:attack", [ 49, 325, 314, 16 ] ],
		[ "potion-combo:companionHp:page", [ 208, 381, 57, 20 ] ],
		[ "potion-combo:companionCure:key", [ 326, 419, 57, 20 ] ]
	];
	for ( const [id, expected] of grid ) {
		const control = defined( dialog.controls.find( c => c.id === id ) );
		assert.deepEqual( control.rect, [ ox + expected[0], oy + expected[1], expected[2], expected[3] ], id );
	}
	f.ui.event( { kind: "activate", id: "companion-potion-step:transport:1" } );
	f.ui.event( { kind: "activate", id: "potion-combo:companionHp:page" } );
	f.ui.event( { kind: "activate", id: "potion-tab:character" } );
	const character = defined( f.ui.step( f.state, 200 ) );
	assert.ok( character.controls.some( c => c.id === "potion-percent:hp" ) );
	assert.ok( !character.controls.some( c => c.id.startsWith( "companion-potion" ) ) );
	assert.ok( !character.controls.some( c => c.id.startsWith( "potion-choice:" ) ) );
	f.ui.event( { kind: "activate", id: "potion-tab:companion" } );
	assert.equal(
		defined( f.ui.step( f.state, 300 ) ).controls.find( c => c.id === "companion-potion-percent:transport" )?.value,
		"51"
	);
});
test("shared recovery waits for the authoritative potion lane and scheduling margin", () => {
	assert.equal( companionAutoPotion( fixture( { lastSentMs: 8801 } ) ), null );
	assert.deepEqual( companionAutoPotion( fixture( { lastSentMs: 8800 } ) ), { slot: 20, companionGid: 41 } );
});

test("odd viewport sizes keep the Auto Potion bitmap tiles on whole pixels", t => {
	const { dialog } = openDialog( t, true, { width: 1601, height: 901 } );
	const tab = defined( dialog.controls.find( c => c.id === "potion-tab:character" ) );
	assert.deepEqual( tab.rect, [ 618, 154, 72, 24 ] );
	for (
		const c of dialog.controls.filter( c => c.id.startsWith( "companion-potion" ) || c.id.startsWith( "potion-" ) )
	) {
		assert.equal( Number.isInteger( c.rect[0] ) && Number.isInteger( c.rect[1] ), true, c.id );
	}
});

/*
================
vigorFixture
================
*/
function vigorFixture() {
	const base = fixture();
	return {
		...base,
		settings: {
			...base.settings,
			vigorHp: autoPotionWord( { enabled: true, percent: 55, slot: 1 } ),
			vigorMp: autoPotionWord( { enabled: true, percent: 35, slot: 1 } )
		},
		items: [ item( 20, 0x18ec ) ],
		vital: { hp: 550, mp: 1000, maxHp: 1000, maxMp: 1000 }
	};
}
test("vigor HP and MP thresholds use the bound vigor once and independently", () => {
	const f = vigorFixture();
	assert.deepEqual( playerVigorAutoPotion( f ), { slot: 20 } );
	assert.equal( playerVigorAutoPotion( { ...f, vital: { ...f.vital, hp: 551 } } ), null );
	assert.deepEqual( playerVigorAutoPotion( { ...f, vital: { ...f.vital, hp: 1000, mp: 350 } } ), { slot: 20 } );
	assert.equal( playerVigorAutoPotion( { ...f, vital: { ...f.vital, hp: 1000, mp: 351 } } ), null );
	assert.deepEqual( playerVigorAutoPotion( { ...f, vital: { ...f.vital, mp: 1 } } ), { slot: 20 } );
});
test("vigor rejects the wrong potion family, empty stacks, death, locks and cooldowns", () => {
	const f = vigorFixture();
	for ( const flags of [ 0x8ec, 0x10ec, 0x20ec, 0x48ec ] ) {
		assert.equal( playerVigorAutoPotion( { ...f, items: [ item( 20, flags ) ] } ), null );
	}
	assert.equal( playerVigorAutoPotion( { ...f, items: [ { ...item( 20, 0x18ec ), quantity: 0 } ] } ), null );
	assert.equal( playerVigorAutoPotion( { ...f, blocked: true } ), null );
	assert.equal( playerVigorAutoPotion( { ...f, lastSentMs: 9500 } ), null );
	assert.equal( playerVigorAutoPotion( { ...f, vital: { ...f.vital, hp: 0, mp: 0 } } ), null );
	assert.equal( playerVigorAutoPotion( { ...f, quickSlots: [] } ), null );
	assert.equal( playerVigorAutoPotion( { ...f, vital: { ...f.vital, abnormal: 0x20 } } ), null );
});
test("vigor rows use the approved grid and tabs keep their top edge", t => {
	const { f, dialog } = openDialog( t );
	const comp = defined( dialog.controls.find( c => c.id === "potion-tab:companion" ) );
	const ox = comp.rect[0] - 98, oy = comp.rect[1] - 40;
	assert.deepEqual( defined( dialog.controls.find( c => c.id === "potion-save" ) ).rect, [
		ox + 125,
		oy + 464,
		76,
		24
	] );
	f.ui.event( { kind: "activate", id: "potion-tab:character" } );
	const character = defined( f.ui.step( f.state, 200 ) );
	assert.deepEqual( defined( character.controls.find( c => c.id === "potion-tab:companion" ) ).rect, comp.rect );
	for ( const [key, y] of [ [ "hp", 235 ], [ "mp", 315 ], [ "vigorHp", 395 ], [ "vigorMp", 475 ] ] ) {
		assert.deepEqual( defined( character.controls.find( c => c.id === "potion-percent:" + key ) ).rect, [
			ox + 49,
			oy + Number( y ),
			314,
			16
		] );
	}
	assert.deepEqual( defined( character.controls.find( c => c.id === "potion-combo:vigorHp:key" ) ).rect, [
		ox + 326,
		oy + 367,
		57,
		20
	] );
	assert.deepEqual( defined( character.controls.find( c => c.id === "potion-save" ) ).rect, [
		ox + 125,
		oy + 614,
		76,
		24
	] );
	f.ui.event( { kind: "activate", id: "potion-enable:vigorHp" } );
	f.ui.step( f.state, 250 );
	f.ui.event( { kind: "activate", id: "potion-step:vigorHp:1" } );
	assert.equal(
		defined( f.ui.step( f.state, 300 ) ).controls.find( c => c.id === "potion-percent:vigorHp" )?.value,
		"51"
	);
	f.ui.event( { kind: "activate", id: "potion-tab:companion" } );
	f.ui.step( f.state, 400 );
	f.ui.event( { kind: "activate", id: "potion-tab:character" } );
	assert.equal(
		defined( f.ui.step( f.state, 500 ) ).controls.find( c => c.id === "potion-percent:vigorHp" )?.value,
		"51"
	);
});
test("real UI vigor use waits for Confirm and leaves native save bytes unchanged", t => {
	const { f, sent } = openDialog( t );
	Object.assign( f.state.gameplay, {
		inventory: [ ...f.state.gameplay.inventory, item( 20, 0x18ec ) ],
		quickSlots: [ { slot: 1, kind: 0x46, payload: 7 } ],
		vitals: [ { gid: 1, hp: 20, mp: 20, maxHp: 100, maxMp: 100 } ]
	} );
	f.ui.event( { kind: "activate", id: "potion-tab:character" } );
	f.ui.event( { kind: "activate", id: "potion-enable:vigorHp" } );
	f.ui.event( { kind: "activate", id: "potion-enable:vigorMp" } );
	f.ui.step( f.state, 200 );
	assert.ok( !sent.some( c => c.kind === "item-use" ) );
	f.ui.event( { kind: "activate", id: "potion-save" } );
	f.ui.step( f.state, 300 );
	assert.deepEqual( sent.filter( c => c.kind === "item-use" ), [ { kind: "item-use", slot: 20 } ] );
	assert.deepEqual( Object.keys( defined( sent.find( c => c.kind === "auto-potion-save" ) ).settings ).sort(), [
		"cure",
		"cureKey",
		"curePage",
		"hp",
		"mp",
		"timing"
	] );
});

test("old companion preferences migrate with both vigor rows disabled; new vigor bindings persist", () => {
	const old = defaultCompanionPotions();
	/** @type {Record<string,string>} */
	const data = {
		"sro.companion-potions.v1:test": JSON.stringify( {
			transport: old.transport,
			attack: old.attack,
			bindings: old.bindings
		} )
	};
	const owner = createCompanionPotions( {
		getItem: key => data[key] ?? null,
		setItem: ( key, value ) => {
			data[key] = value;
		}
	} );
	owner.sync( "test" );
	assert.equal( autoPotionEntry( owner.state().saved.vigorHp ).enabled, false );
	owner.begin();
	owner.toggle( "vigorHp" );
	owner.percent( "vigorHp", 55 );
	owner.choice( "vigorHp", "page", 3 );
	owner.choice( "vigorHp", "key", 7 );
	owner.save();
	owner.sync( "other" );
	owner.sync( "test" );
	assert.deepEqual( autoPotionEntry( owner.state().saved.vigorHp ), { enabled: true, percent: 55, slot: 37 } );
	owner.begin();
	owner.percent( "vigorHp", 80 );
	owner.begin();
	assert.equal( autoPotionEntry( owner.state().draft.vigorHp ).percent, 55 );
});

test("expanded recovery keeps its title visible after restoring negative native window coordinates", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { transportAutoPotion: true } ) } );
	f.ui.event( {
		kind: "window-positions",
		value: { width: 1600, height: 900, windows: { gameGuide: [ -25, -51 ] } }
	} );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "activate", id: "open-window:Auto Potion" } );
	const result = defined( f.ui.step( f.state, 100 ) );
	const tab = defined( result.controls.find( c => c.id === "potion-tab:character" ) );
	assert.equal( tab.rect[0] >= 24, true );
	assert.equal( tab.rect[1] >= 40, true );
});

test("vigor uses the selected native numbered slot, never the previous slot or common M slot", () => {
	for ( let page = 0; page < 4; page++ ) {
		for ( let key = 1; key <= 10; key++ ) {
			const slot = hotbarSlot( page, key ), f = vigorFixture();
			f.settings = { ...f.settings, vigorHp: autoPotionWord( { enabled: true, percent: 55, slot } ) };
			f.items = [ item( 20, 0x18ec ), item( 21, 0x18ec ) ];
			f.quickSlots = [ { slot: slot - 1, kind: 0x46, payload: 8 }, { slot, kind: 0x46, payload: 7 } ];
			assert.deepEqual( playerVigorAutoPotion( f ), { slot: 20 }, "F" + (page + 1) + "/" + (key % 10) );
		}
	}
});
test("companion HP and pill bindings use native slot numbers too", () => {
	const f = fixture( { vitals: [ { gid: 41, abnormal: 16 } ] } ), slot = hotbarSlot( 3, 3 );
	f.settings = {
		...f.settings,
		bindings: {
			...f.settings.bindings,
			hp: autoPotionWord( { enabled: true, percent: 50, slot } ),
			cure: autoPotionWord( { enabled: true, percent: 0, slot } )
		}
	};
	f.quickSlots = [ { slot: slot - 1, kind: 0x46, payload: 8 }, { slot, kind: 0x46, payload: 7 } ];
	f.items = [ item( 20, HP ), item( 21, HP ) ];
	assert.deepEqual( companionAutoPotion( f ), { slot: 20, companionGid: 41 } );
	f.items = [ item( 20, CURE ), item( 21, CURE ) ];
	assert.deepEqual( companionAutoPotion( f ), { slot: 20, companionGid: 41 } );
});
