/*
===========================================================================

pet-gear-tooltip.test.mjs - pet equipment has an equipment-style footer

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { PET_DESIGN } = await import( "../../src/engine/foundation/ui/pet-design-data.ts" );

test("all pet gear puts its level and Pet footer last without stack rows", () => {
	for ( const degree of PET_DESIGN.gear ) {
		for ( const gear of degree.items ) {
			const item = {
				slot: 13,
				name: `${gear.name} (Degree ${degree.degree})`,
				refObjId: gear.id,
				typeFlags: 0x4ec,
				quantity: 1,
				plus: 0,
				magic: [],
				variance: "0",
				durability: 0,
				tooltip: { fields: { maxStack: 1 }, descriptionSymbol: "description" }
			};
			const rows = itemTooltip(
				item,
				{ masteries: [] },
				symbol =>
					symbol === "description" ?
						`Base effect. Needs pet level ${degree.minPetLevel}. Up to ${degree.maxBlues} blues.` :
						symbol
			);
			assert.equal( rows[0].value, gear.name );
			assert.equal( rows[0].heading, true );
			assert.deepEqual( rows.slice( 2, 4 ).map( row => row.value ), [
				`UIIT_STT_ARMOR_POSITION: ${gear.slot}`,
				`Degree: ${degree.degree} degrees`
			] );
			assert.deepEqual( rows.slice( -2 ).map( row => row.value ), [
				`Level: Pet level ${degree.minPetLevel} required`,
				"Pet"
			] );
			assert.ok( !rows.some( row => /Base effect|Drag it|cooldowns|damage \+/.test( row.value ) ) );
			const health = rows.find( row =>
				row.value === `Maximum HP +${Number( gear.baseStats.maxHPPercent.toFixed( 1 ) )}%`
			);
			assert.equal( health?.color, 0xffffffff );
			for (
				const [key, value] of [
					[ "PARAM_PA", gear.baseStats.physicalAttack ],
					[ "PARAM_MA", gear.baseStats.magicalAttack ],
					[ "PARAM_PD", gear.baseStats.physicalDefense ],
					[ "PARAM_MD", gear.baseStats.magicalDefense ],
					[ "PARAM_HR", gear.baseStats.hitRatio ],
					[ "PARAM_ER", gear.baseStats.parryRatio ]
				]
			) {
				const row = rows.find( row => row.value === `${key} ${value}` );
				assert.equal( !!row, Number( value ) > 0 );
				if ( row ) assert.equal( row.color, 0xffffffff );
			}
			assert.ok( !rows.some( row => /Needs pet level|PARAM_MAX_CONTAIN|UIIT_STT_AMOUNT/.test( row.value ) ) );
			const ordinary = itemTooltip( { ...item, refObjId: 1 }, { masteries: [] }, symbol => symbol );
			assert.equal( ordinary[0].value, item.name );
			assert.ok( ordinary.some( row => row.value === "PARAM_MAX_CONTAIN 1" ) );
			assert.ok( ordinary.some( row => row.value === "UIIT_STT_AMOUNT 1" ) );
		}
	}
});
