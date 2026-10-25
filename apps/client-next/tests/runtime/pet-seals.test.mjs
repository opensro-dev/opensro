/*
===========================================================================
pet-seals.test.mjs - sealed pet white stats, yellow names and native slot shine
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { petGear } = await import( "../../src/engine/foundation/ui/pet-gear.ts" );
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { itemSlotOverlays } = await import( "../../src/engine/foundation/ui/item-slot-effects.ts" );

test("all seals keep the pet footer, scale whites and use yellow rare headings and shine", () => {
	for ( let seal = 1; seal <= 3; seal++ ) {
		const gear = petGear( 30109 + seal * 10 );
		const base = petGear( 30109 );
		assert.ok( gear );
		assert.ok( base );
		assert.equal(
			gear.item.baseStats.maxHPPercent,
			base.item.baseStats.maxHPPercent * (1 + seal * .5)
		);
		const item = {
			name: "Collar",
			refObjId: 30109 + seal * 10,
			typeFlags: 0x4ec,
			slot: 13,
			quantity: 1,
			plus: 0,
			magic: [],
			variance: "0",
			durability: 0,
			tooltip: { fields: { rarity: 2, itemClass: 24 + seal } }
		};
		const rows = itemTooltip( item, { masteries: [] }, key => key );
		assert.equal( rows[0].color, 0xffffd953 );
		assert.equal( rows[2].value, [ "", "PARAM_RARE_FIRST", "PARAM_RARE_SECOND", "PARAM_RARE_THIRD" ][seal] );
		assert.equal( rows.at( -1 )?.value, "Pet" );
		assert.ok(
			itemSlotOverlays( item, [ 0, 0, 32, 32 ], 0, 0 ).some( overlay =>
				overlay.path.endsWith( "icon_edge_rare.png" )
			)
		);
	}
	assert.equal( petGear( 30149 ), undefined );
});

test("equipped sealed pet gear draws the same animated rare shine as inventory", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		cosRecords: [ { gid: 7, refObjId: 100, band: 3, hp: 100, mp: 0, status: 0, dead: false } ],
		petTraining: {
			gid: 7,
			points: 0,
			attackPercent: 100,
			defensePercent: 100,
			rows: [],
			slotted: [ 0, 0, 0 ],
			skills: [],
			receivedAtMs: 0,
			gear: [
				{
					slot: 0,
					refObjId: 30132,
					name: "Studded Collar",
					typeFlags: 0x4ec,
					quantity: 1,
					plus: 0,
					magic: [],
					variance: "0",
					durability: 0,
					icon: "item\\pet\\collar_02.ddj",
					tooltip: { fields: { rarity: 2, itemClass: 6 } }
				},
				null,
				null
			]
		}
	} );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { attackPetTraining: true } ) } );
	f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
	f.ui.step( f.state, 0 );
	f.ui.event( { kind: "activate", id: "cos-tab:1" } );
	for ( let i = 1; i <= 12; i++ ) f.ui.step( f.state, i * 100 );
	assert.equal( f.ui.stats().error, null );
	assert.ok(
		f.scenes.at( -1 ).quads.some( q => String( q.texture ).includes( "icon_edge_rare" ) ),
		"equipped seal shine"
	);
});
