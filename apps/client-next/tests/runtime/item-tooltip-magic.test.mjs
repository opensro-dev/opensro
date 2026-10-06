/*
===========================================================================

item-tooltip-magic.test.mjs - native magic-attribute branch and data census

Expected text follows 55B540 and its 553980 avatar formatter. The licensed
catalog census fails when an authored option family lacks an audited case.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import path from "node:path";
import { readTextDataRowsSync } from "../../../../scripts/build/shared/textDataIo.mjs";
import { serverGameDataRoot } from "../../../../scripts/build/world/paths.mjs";
const { itemTooltipMagic } = await import( "../../src/engine/foundation/ui/item-tooltip-magic.ts" );
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);

const EXPECTED = {
	MATTR_STR: "PARAM_STR 5 PARAM_INCREASE (+50%)",
	MATTR_INT: "PARAM_INT 5 PARAM_INCREASE (+50%)",
	MATTR_HP: "PARAM_HP 5 PARAM_INCREASE (+50%)",
	MATTR_MP: "PARAM_MP 5 PARAM_INCREASE (+50%)",
	MATTR_DUR: "PARAM_DUR 5% PARAM_INCREASE (+50%)",
	MATTR_HR: "PARAM_HR 5% PARAM_INCREASE (+50%)",
	MATTR_ER: "PARAM_ER 5% PARAM_INCREASE (+50%)",
	MATTR_EVADE_BLOCK: "PARAM_IGNORE_BLOCKING 5 (+50%)",
	MATTR_EVADE_CRITICAL: "PARAM_EVADE_CRITICAL 5 (+50%)",
	MATTR_RESIST_FROSTBITE: "PARAM_FZ,PARAM_FBPARAM_HOUR 5% PARAM_DECREASE (+50%)",
	MATTR_RESIST_ESHOCK: "PARAM_ESPARAM_HOUR 5% PARAM_DECREASE (+50%)",
	MATTR_RESIST_BURN: "PARAM_BUPARAM_HOUR 5% PARAM_DECREASE (+50%)",
	MATTR_RESIST_POISON: "PARAM_PSPARAM_HOUR 5% PARAM_DECREASE (+50%)",
	MATTR_RESIST_ZOMBIE: "PARAM_ZBPARAM_HOUR 5% PARAM_DECREASE (+50%)",
	MATTR_ATHANASIA: "PARAM_ATHANASIA(5UIIT_STT_COUNT)",
	MATTR_SOLID: "PARAM_SOLID(5UIIT_STT_COUNT)",
	MATTR_LUCK: "PARAM_LUCK(5UIIT_STT_COUNT)",
	MATTR_ASTRAL: "PARAM_ASTRAL 5 UIIT_STT_COUNT",
	MATTR_REPAIR: "PARAM_REPAIR (4UIIT_STT_COUNT)",
	MATTR_STR_3JOB: "PARAM_STR 5 PARAM_INCREASE",
	MATTR_INT_3JOB: "PARAM_INT 5 PARAM_INCREASE",
	MATTR_STR_AVATAR: "PARAM_STR 5 PARAM_INCREASE",
	MATTR_INT_AVATAR: "PARAM_INT 5 PARAM_INCREASE",
	MATTR_AVATAR_STR: "PARAM_STR 5 PARAM_INCREASE",
	MATTR_AVATAR_INT: "PARAM_INT 5 PARAM_INCREASE",
	MATTR_AVATAR_HP: "PARAM_HP 5 PARAM_INCREASE",
	MATTR_AVATAR_MP: "PARAM_MP 5 PARAM_INCREASE",
	MATTR_AVATAR_ER: "PARAM_ER 5% PARAM_INCREASE",
	MATTR_AVATAR_HR: "PARAM_HR 5% PARAM_INCREASE",
	MATTR_AVATAR_DRUA: "PARAM_AVATAR_DRUA 5% PARAM_INCREASE",
	MATTR_AVATAR_DARA: "PARAM_AVATAR_DARA 5% PARAM_INCREASE",
	MATTR_AVATAR_HPRG: "PARAM_AVATAR_HPRG 5% PARAM_INCREASE",
	MATTR_AVATAR_MPRG: "PARAM_AVATAR_MPRG 5% PARAM_INCREASE",
	MATTR_AVATAR_MDIA: "PARAM_AVATAR_MDIA 5% UIIT_STT_PROBABILITY",
	MATTR_DEC_MAXDUR: "PARAM_MAX_DURABILITY 5% PARAM_DECREASE",
	MATTR_NOT_REPARABLE: "PARAM_NOT_REPAIRABLE (PARAM_MAX_DURABILITY 0% PARAM_INCREASE)",
	MATTR_REINFORCE_ITEM: "+0 PARAM_REINFORCE(0PARAM_SECOND)"
};

/*
================
optionItem

The 1..9 packed bracket makes the instance magnitude five exactly 50%.
================
*/
/** @returns {import('../../src/engine/contracts/gameplay').InventoryItem} */
function optionItem( optionName, amount = 5 ) {
	return {
		slot: 13,
		refObjId: 4161,
		typeFlags: 13100,
		quantity: 1,
		plus: 0,
		durability: 53,
		variance: "0",
		magic: [ String( BigInt( amount ) << 32n | 9n ) ],
		tooltip: { fields: { itemClass: 1 } },
		magicReferences: [ {
			paramId: 9,
			optionName,
			degree: 1,
			paramName: [ "MATTR_DEC_MAXDUR", "MATTR_NOT_REPARABLE" ].includes( optionName ) ? "-" : "+",
			rangeWords: [ 65545, 0, 0 ]
		} ]
	};
}

test("every native blue and red attribute family has exact text, color and bold weight", () => {
	for ( const [name, value] of Object.entries( EXPECTED ) ) {
		const item = optionItem( name );
		assert.deepEqual( itemTooltipMagic( item, s => s ), [ {
			value,
			color: name === "MATTR_DEC_MAXDUR" || name === "MATTR_NOT_REPARABLE" ? 0xffff4a4a : 0xff00eaff,
			strong: true
		} ], name );
	}
});

test("the licensed magic-option census has no silently omitted authored family or degree", () => {
	const source = readTextDataRowsSync( path.join( serverGameDataRoot, "textdata", "magicoption.txt" ) );
	const rows = source.filter( row => row[0] === "1" );
	assert.deepEqual( [ ...new Set( rows.map( row => row[2] ) ) ].sort(), Object.keys( EXPECTED ).sort() );
	for ( const row of rows ) {
		const item = optionItem( row[2] );
		const degree = Number( row[4] );
		const reference = {
			paramId: Number( row[1] ),
			optionName: row[2],
			paramName: row[3],
			degree,
			rangeWords: /** @type {[number,number,number]} */ (row.slice( 8, 11 ).map( Number ))
		};
		const instance = {
			...item,
			magic: [ String( 5n << 32n | BigInt( reference.paramId ) ) ],
			tooltip: { fields: { itemClass: (degree - 1) * 3 + 1 } },
			magicReferences: [ reference ]
		};
		const result = itemTooltipMagic( instance, s => s );
		assert.equal( result.length, 1, `${reference.optionName} degree ${degree}` );
		assert.equal( result[0].strong, true );
	}
	assert.equal( rows.length, 255 );
});

test("older avatar STR and INT use the definition degree for a zero magnitude without percentages", () => {
	for ( const name of [ "MATTR_STR_AVATAR", "MATTR_INT_AVATAR" ] ) {
		assert.equal(
			itemTooltipMagic( optionItem( name, 0 ), s => s )[0].value,
			`${name.includes( "STR" ) ? "PARAM_STR" : "PARAM_INT"} 1 PARAM_INCREASE`
		);
	}
});

test("temporary reinforcement formats seconds and minutes; repair charges saturate at zero", () => {
	const cases = /** @type {[number,string][]} */ ([
		[ 0, "0PARAM_SECOND" ],
		[ 59999, "59PARAM_SECOND" ],
		[ 60000, "1PARAM_MINUTE 0PARAM_SECOND" ],
		[ 125999, "2PARAM_MINUTE 5PARAM_SECOND" ]
	]);
	for ( const [amount, duration] of cases ) {
		const source = optionItem( "MATTR_REINFORCE_ITEM", amount );
		const reference = source.magicReferences?.[0];
		assert.ok( reference );
		const item = {
			...source,
			magicReferences: [ {
				...reference,
				rangeWords: /** @type {[number,number,number]} */ ([ 1, 2, 2 ])
			} ]
		};
		assert.equal( itemTooltipMagic( item, s => s )[0].value, `+2 PARAM_REINFORCE(${duration})` );
	}
	for ( const amount of [ 0, 1, 2 ] ) {
		assert.equal(
			itemTooltipMagic( optionItem( "MATTR_REPAIR", amount ), s => s )[0].value,
			`PARAM_REPAIR (${amount === 2 ? 1 : 0}UIIT_STT_COUNT)`
		);
	}
});

test("accessories omit durability penalties and unknown options produce no invented rows", () => {
	for ( const family of [ 5, 12 ] ) {
		const source = optionItem( "MATTR_DEC_MAXDUR" );
		assert.deepEqual( itemTooltipMagic( { ...source, typeFlags: 44 | family << 7 | 1 << 11 }, s => s ), [] );
	}
	assert.deepEqual( itemTooltipMagic( optionItem( "MATTR_UNRECOGNIZED" ), s => s ), [] );
});

test("missing degree definitions suppress positive rows while class zero resolves degree one", () => {
	const source = optionItem( "MATTR_STR" );
	assert.deepEqual( itemTooltipMagic( { ...source, tooltip: { fields: { itemClass: 4 } } }, s => s ), [] );
	assert.equal(
		itemTooltipMagic( { ...source, tooltip: { fields: { itemClass: 0 } } }, s => s )[0].value,
		EXPECTED.MATTR_STR
	);
	const inventory = createInventory( () => {} );
	const reference = source.magicReferences?.[0];
	assert.ok( reference );
	inventory.bootstrap( {
		refItemSnapshot: [ { refObjId: 4161, typeFlags: 13100, nativeFields: { itemClass: 0 } } ],
		magicOptionSnapshot: [ { ...reference, degree: 2 }, { ...reference, paramId: 10 } ]
	} );
	const presented = inventory.present( source );
	assert.deepEqual( presented.magicReferences?.map( r => r.paramId ), [ 9, 10 ] );
	assert.equal( itemTooltipMagic( presented, s => s )[0].value, EXPECTED.MATTR_STR );
	inventory.clear();
});
