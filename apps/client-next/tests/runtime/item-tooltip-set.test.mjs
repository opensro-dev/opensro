/*
===========================================================================

item-tooltip-set.test.mjs - the set bonus block of an equipment tooltip

The block lists the item's set, how many pieces are worn and each tier,
marking the one in force. A shard that publishes no sets, or an item that
names none, shows nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { itemTooltipSet, tooltipSetViewer, wornSetPieces } = await import(
	"../../src/engine/foundation/ui/item-tooltip-set.ts"
);
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { publishedEquipmentSets } = await import( "../../src/engine/foundation/gameplay/equipment-sets.ts" );

const SETS = publishedEquipmentSets( [ {
	id: 13,
	name: "SN_ITEM_11_SET_A_DEFENSE",
	tiers: [
		{ pieces: 2, options: [ { option: 389, value: 1 } ] },
		{ pieces: 4, options: [ { option: 389, value: 2 }, { option: 390, value: 10 } ] },
		{ pieces: 6, options: [ { option: 389, value: 4 } ] }
	]
} ] );
const text = symbol => symbol;

/*
================
piece

A worn or carried item as the tooltip reads it: its slot and its set id.
================
*/
/** @returns {any} */
function piece( slot, setId ) {
	return { slot, tooltip: { fields: { setId } } };
}

test("worn pieces count by set and ignore the bag", () => {
	const counts = wornSetPieces( [ piece( 1, 13 ), piece( 2, 13 ), piece( 3, 0 ), piece( 13, 13 ), piece( 4, 14 ) ] );
	assert.equal( counts.get( 13 ), 2 );
	assert.equal( counts.get( 14 ), 1 );
	assert.equal( counts.has( 0 ), false );
});

test("the tier the worn count reaches is in force, higher tiers wait", () => {
	const rows = itemTooltipSet( piece( 1, 13 ), SETS, new Map( [ [ 13, 4 ] ] ), text );
	assert.equal( rows[0].value, "Ruin Set (4/6)" );
	const active = rows.filter( row => row.color === 0xff6ce675 ).map( row => row.value.trim() );
	assert.deepEqual( active, [ "Reinforce +2 on each worn piece", "Abnormal state resistance +10" ] );
	const waiting = rows.filter( row => row.color === 0xff8a8a8a ).map( row => row.value.trim() );
	assert.ok( waiting.includes( "Reinforce +4 on each worn piece" ) );
	assert.ok( waiting.includes( "Reinforce +1 on each worn piece" ) );
});

test("below the first tier nothing is in force", () => {
	const rows = itemTooltipSet( piece( 1, 13 ), SETS, new Map( [ [ 13, 1 ] ] ), text );
	assert.equal( rows[0].value, "Ruin Set (1/6)" );
	assert.equal( rows.some( row => row.color === 0xff6ce675 ), false );
});

test("an item without a published set shows nothing", () => {
	assert.deepEqual( itemTooltipSet( piece( 1, 0 ), SETS, new Map(), text ), [] );
	assert.deepEqual( itemTooltipSet( piece( 1, 99 ), SETS, new Map(), text ), [] );
	assert.deepEqual( itemTooltipSet( piece( 1, 13 ), [], new Map(), text ), [] );
});

test("a malformed set list is refused whole and an absent one is empty", () => {
	assert.deepEqual( publishedEquipmentSets( undefined ), [] );
	assert.throws( () => publishedEquipmentSets( [ { id: -1, tiers: [] } ] ) );
	assert.throws( () =>
		publishedEquipmentSets( [ { id: 1, tiers: [ { pieces: 2, options: [ { option: 389 } ] } ] } ] )
	);
	assert.throws( () => publishedEquipmentSets( {} ) );
});

test("the Experimental row and a published set list decide whether the tooltip has a set block", () => {
	const bag = [ piece( 1, 13 ) ];
	assert.equal( tooltipSetViewer( false, SETS, bag ), undefined, "row off" );
	assert.equal( tooltipSetViewer( true, [], bag ), undefined, "a shard that publishes no sets" );
	assert.equal( tooltipSetViewer( true, undefined, bag ), undefined, "no list at all" );
	const viewer = tooltipSetViewer( true, SETS, bag );
	assert.equal( viewer?.groups, SETS, "the groups are the published ones, for memoising" );
	assert.equal( viewer?.equipment, bag );
	assert.deepEqual( tooltipSetViewer( true, SETS, undefined )?.equipment, [] );
});

test("an armor tooltip carries the set block only when the viewer gives sets", () => {
	// Every stat column reads as 1, so the armor rows build; only the set rows are under test.
	const fields = new Proxy( { setId: 13, rarity: 0, itemClass: 1 }, {
		get: ( o, k ) => (k in o ? o[k] : 1),
		has: () => true
	} );
	/** @type {any} */ const item = {
		name: "Piece",
		slot: 1,
		typeFlags: 0x2c | (3 << 7) | (1 << 11),
		quantity: 1,
		plus: 0,
		variance: 0,
		magic: [],
		tooltip: { fields }
	};
	const rowsOf = viewer => itemTooltip( item, { masteries: [] }, symbol => symbol, viewer ).map( row => row.value );
	const plain = rowsOf( undefined );
	assert.equal( plain.some( value => value.includes( "Ruin Set" ) ), false, "no viewer, no block" );
	const shown = rowsOf( { sets: tooltipSetViewer( true, SETS, [ item ] ) } );
	assert.ok( shown.some( value => value.includes( "Ruin Set (1/6)" ) ), "the block heads the set rows" );
	assert.deepEqual(
		rowsOf( { sets: tooltipSetViewer( false, SETS, [ item ] ) } ),
		plain,
		"row off leaves the native rows"
	);
});
