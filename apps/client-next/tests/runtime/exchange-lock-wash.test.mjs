/*
===========================================================================

exchange-lock-wash.test.mjs - a locked exchange side turns light blue

Native CIFExchange raises CIFSlotWithHelp's +0x4FD flag (54FC60) on all
twelve slots of a side once it locks: 6B21B0 on the own side when 0xB095
accepts the confirm, 6B2120 on the partner's side on 0x37CF. While it
stands, 565850 lays com_item_exchange.ddj over each slot, empty or not.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

const WASH = "/interface/ifcommon/com_item_exchange.png";
const SLOTS = 12;

/*
================
exchangeFixture

An open exchange with one item on each side.
================
*/
function exchangeFixture( t ) {
	const f = uiFixture();
	t.after( () => f.dispose() );
	const offer = name => ({
		slot: 0,
		item: {
			slot: 14,
			refObjId: 1,
			quantity: 1,
			name,
			icon: "item/etc/hp_potion_01.ddj",
			typeFlags: 0x6c,
			magic: []
		}
	});
	Object.assign( f.state.gameplay, {
		inventory: [],
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventoryPending: false,
		exchange: {
			open: true,
			partner: 2,
			own: [ offer( "Mine" ) ],
			theirs: [ offer( "Theirs" ) ],
			ownGold: 0,
			theirGold: 0,
			ownLocked: false,
			theirLocked: false,
			approved: false,
			requesting: false
		}
	} );
	let now = 1000;
	/*
	================
	washed

	The slot rects of one side ("exchange-my" or "exchange-their") that the
	last scene washes.
	================
	*/
	const washed = side => {
		for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, now += 50 );
		const quads = defined( f.scenes.at( -1 ) ).quads.filter( q => q.texture.endsWith( WASH ) );
		const controls = defined( f.presented() ).controls;
		return Array.from(
			{ length: SLOTS },
			( _, slot ) => defined( controls.find( c => c.id === side + ":" + slot ) )
		)
			.filter( control => quads.some( q => q.rect.join( "," ) === control.rect.join( "," ) ) );
	};
	return { f, washed };
}

test("an unlocked exchange washes no slot", t => {
	const { washed } = exchangeFixture( t );
	assert.equal( washed( "exchange-my" ).length, 0 );
	assert.equal( washed( "exchange-their" ).length, 0 );
});

test("the own lock washes all twelve own slots and only those", t => {
	const { f, washed } = exchangeFixture( t );
	f.state.gameplay.exchange.ownLocked = true;
	assert.equal( washed( "exchange-my" ).length, SLOTS, "empty slots too" );
	assert.equal( washed( "exchange-their" ).length, 0 );
});

test("the partner's lock washes the partner's twelve slots", t => {
	const { f, washed } = exchangeFixture( t );
	f.state.gameplay.exchange.theirLocked = true;
	assert.equal( washed( "exchange-their" ).length, SLOTS );
	assert.equal( washed( "exchange-my" ).length, 0 );
	f.state.gameplay.exchange.ownLocked = true;
	assert.equal( washed( "exchange-my" ).length, SLOTS );
});
