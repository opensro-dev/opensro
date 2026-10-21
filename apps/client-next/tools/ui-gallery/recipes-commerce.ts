/*
===========================================================================
recipes-commerce.ts - populated commerce, training and companion review inputs

All drawing, quantities, costs, confirmation lifetimes and requests belong to
production owners. Seeded server publications are explicit; no sale is settled.
===========================================================================
*/
import type { ExtendedRecipe, RecipeContext } from "./extended";
import { decodeTooltipSkills } from "../../src/engine/foundation/ui/skill-tooltip-catalog";
import { createCountJobs } from "../../src/engine/foundation/gameplay/count-job";
import { decodeSkillUi } from "../../src/engine/foundation/ui/skill-layout";
import { decodeCosReferences } from "../../src/engine/foundation/ui/cos-command";
import { herb, npc, openPanel, press, published } from "./recipe-input";

/*
================
restoration

Use published IDs, ranks, prices, requirements and icons, decoded by production.
The learned ranks and inventory are synthetic character state.
================
*/
async function restoration( context: RecipeContext, npcMode: boolean, confirmation: boolean, pending = false ) {
	const layout = decodeSkillUi( await published( "skillUi" ) );
	const skills = decodeTooltipSkills( await published( "skillData" ) );
	const entry = layout.skills.find( row => row.mastery === 257 && row.row === 0 && row.column === 0 );
	if ( !entry ) throw Error( "Published first Bicheon skill is unavailable" );
	const ranks = [ ...skills.values() ].filter( row => row.groupId === entry.group && row.basicLevel <= 3 );
	const learned = ranks.reduce( ( a, b ) => a.basicLevel > b.basicLevel ? a : b );
	const game = context.state.gameplay;
	game.skillCatalog = ranks.map( row => ({
		id: row.id,
		group: row.groupId,
		level: row.basicLevel,
		name: row.nameSymbol,
		nameSymbol: row.nameSymbol,
		icon: String( entry.icon ).replace( /^icon[\\/]/i, "" ),
		spCost: row.reqLearnSp,
		trainable: true,
		targetRequired: false,
		cooldownMs: 0,
		masteries: [ { ID: row.masteryId, Level: row.reqMasteryLevel }, { ID: 0, Level: 0 } ],
		prerequisites: Array.from(
			{ length: 3 },
			( _, i ) => ({ ID: row.reqGroups[i]?.groupId ?? 0, Level: row.reqGroups[i]?.level ?? 0 })
		)
	}) );
	game.skills = [ learned.id ];
	game.progression = { ...game.progression, level: 40, gold: "100000000", masteries: [ { id: 257, level: 40 } ] };
	game.inventory = [ {
		...herb,
		slot: 13,
		refObjId: npcMode ? 3673 : 3828,
		typeFlags: 0x6ec,
		quantity: 5,
		name: npcMode ? "Curst Heart restoration potion" : "Skill Restoration Potion"
	} ];
	if ( npcMode ) game.restorationRevision = 1;
	else {
		await openPanel( context, "Inventory" );
		await press( context, "slot:13", "double-activate" );
	}
	await context.step();
	if ( confirmation ) {
		await press( context, "withdrawal-choice:skill-withdraw:" + learned.id );
		await press( context, "withdrawal-decrease" );
		if ( pending ) {
			context.state.gameplay.trainingPending = true;
			await context.step();
			if ( !context.controls().find( row => row.id === "withdrawal-confirm" )?.disabled ) {
				throw Error( "Pending training publication did not disable confirmation" );
			}
		}
	}
}

/*
================
merchant
================
*/
export async function merchant( context: RecipeContext ) {
	// 1 | 8: a targeted merchant with the repair service bit, which the shop
	// repair buttons require (ui.ts gates them on targetCapabilities & 8).
	npc( context, 9, "Merchant" );
	const bow = {
		...herb,
		slot: 15,
		refObjId: 4161,
		typeFlags: 13100,
		quantity: 1,
		name: "Bronz Bow",
		icon: "item/china/weapon/bow_02.ddj",
		durability: 40,
		variance: "31",
		tooltip: {
			fields: {
				varianceIntMin1c0: 40,
				varianceIntMax1c4: 100,
				canRepair: 1,
				repairCostB4: 200,
				reviveCostB8: 50,
				rarity: 2
			}
		}
	};
	Object.assign( context.state.gameplay, {
		inventory: [ herb, bow ],
		shop: {
			npc: 99,
			name: "Merchant",
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: herb.refObjId,
				name: herb.name,
				icon: herb.icon,
				price: "10",
				maxStack: 50,
				items: [ herb ]
			} ],
			buyback: [ {
				id: 5,
				index: 0,
				refObjId: herb.refObjId,
				name: herb.name,
				icon: herb.icon,
				quantity: 2,
				price: "20",
				plus: 0
			} ],
			saleQuotes: [ { slot: 14, refObjId: herb.refObjId, quantity: 10, price: "20" }, {
				slot: 15,
				refObjId: 4161,
				quantity: 1,
				price: "200"
			} ]
		}
	} );
	await openPanel( context, "Shop" );
}

/*
================
companion
================
*/
async function companion( context: RecipeContext, tab: number ) {
	const references = decodeCosReferences( await published( "cosPresentation" ) );
	const refObjId = [ ...references ].find( ( [, reference] ) => /rabbit/i.test( reference.icon ) )?.[0];
	if ( !refObjId ) throw Error( "Published pickup rabbit reference unavailable" );
	context.state.gameplay.cosRecords = [ {
		gid: 7,
		refObjId,
		band: 4,
		hp: 100,
		mp: 0,
		status: 28,
		dead: false,
		name: "Review companion",
		commandMode: 199,
		level: 12,
		satiety: 10000,
		inventory: [ { ...herb, slot: 0 } ]
	} ];
	await openPanel( context, "COS inventory" );
	await press( context, "cos-tab:" + tab );
}

/*
================
commerceRecipes
================
*/
export const commerceRecipes: readonly ExtendedRecipe[] = [
	...([ [ "restoration-list", false, false ], [ "restoration-confirm", false, true ], [
		"resuscitation-confirm",
		true,
		true
	], [ "restoration-pending", false, true ] ] as const).map( ( [name, npcMode, confirm] ) => ({
		id: "breadth:" + name,
		title: "Training - " + name,
		family: "Dialogs" as const,
		source:
			"tests/runtime/withdrawal.test.mjs; foundation/ui/skill-tooltip-catalog.ts; assets/data/skillUi.json and skillData.json",
		resources: [ "ifskillwithdrawal", "ifskillremovalbox" ],
		controls: confirm ?
			[ "withdrawal-decrease", "withdrawal-confirm", "withdrawal-cancel" ] :
			[ "withdrawal-close", "withdrawal-choice:mastery-withdraw:257" ],
		open: ( context: RecipeContext ) => restoration( context, npcMode, confirm, name === "restoration-pending" )
	}) ),
	...([
		[ "merchant", "", "shop-offer:0" ],
		[ "merchant-buy", "shop-offer:0", "shop-quantity" ],
		[ "merchant-sell", "slot:14", "shop-quantity" ],
		[ "merchant-buyback", "shop-buyback:0", "shop-trade" ],
		[ "merchant-rare-sale", "slot:15", "shop-trade" ],
		[ "merchant-repair-all", "shop-repair:GDR_STORE_BTN_REPAIRALL", "repair-all-confirm" ]
	] as const).map( ( [name, target, witness] ) => ({
		id: "breadth:" + name,
		title: "Commerce - " + name,
		family: "Services" as const,
		source:
			"tests/browser/native-service-windows.test.mjs; tests/browser/repair-hammer.test.mjs; tests/runtime/ui-behavior.test.mjs merchant transactions",
		resources: [ "ifstore", "ifmessagebox" ],
		controls: [ witness ],
		async open( context: RecipeContext ) {
			await merchant( context );
			if ( target ) await press( context, target, target.startsWith( "slot:" ) ? "double-activate" : "activate" );
		}
	}) ),
	...([ [ "cos-info", 0 ], [ "cos-inventory", 1 ], [ "cos-setup", 2 ] ] as const).map( ( [name, tab] ) => ({
		id: "breadth:" + name,
		title: "Companion - " + name,
		selectedControls: [ "cos-tab:" + tab ],
		family: "Services" as const,
		source:
			"runtime/ui/ui.ts COS tabs; assets/data/cosPresentation.json; tests/browser/native-service-windows.test.mjs",
		resources: [ tab === 0 ? "ifcosinfo" : tab === 1 ? "ifcosinventory" : "ifcossetup" ],
		controls: [
			"cos-tab:" + tab,
			...(tab === 1 ? [ "cos-slot:0" ] : tab === 2 ? [ "cos-save", "cos-reset" ] : [])
		],
		open: ( context: RecipeContext ) => companion( context, tab )
	}) ),
	...([ "revive", "renew" ] as const).map( mode => ({
		id: "breadth:cos-" + mode,
		title: "Companion - " + mode + " target confirmation",
		family: "Dialogs" as const,
		source: "tests/runtime/ui-behavior.test.mjs grass and clock confirmations (5581)",
		resources: [ "ifmessagebox" ],
		controls: [ "cos-" + mode + "-confirm", "cos-" + mode + "-cancel" ],
		async open( context: RecipeContext ) {
			context.state.gameplay.inventory = [
				{
					...herb,
					slot: 13,
					refObjId: mode === "revive" ? 2128 : 8985,
					typeFlags: mode === "revive" ? 0x30ec : 0x66ec
				},
				{ ...herb, slot: 14, refObjId: 901, typeFlags: 0x08cc, quantity: 1, summon: { state: 4, rentals: [] } }
			];
			await openPanel( context, "Inventory" );
			await press( context, "slot:13", "right-activate" );
			await press( context, "slot:14" );
		}
	}) ),
	{
		id: "breadth:premium",
		title: "Premium package - remaining and exhausted uses",
		family: "Dialogs",
		source: "tests/runtime/count-job.test.mjs; foundation/gameplay/count-job.ts",
		resources: [ "ifcompositeitemwnd" ],
		controls: [ "composite-item:4001", "composite-item:4002", "composite-item-cancel" ],
		async open( context ) {
			const owner = createCountJobs();
			owner.reference( {
				refObjId: 10130,
				typeFlags: 0x72ec,
				nativeFields: { itemParam1_29c: 2419200 },
				name: "Synthetic package (placeholder icon)",
				icon: herb.icon
			} );
			for (
				const [id, typeFlags, uses, name] of [ [ 4001, 0x09ec, 3, "Instant return" ], [
					4002,
					0x19ec,
					0,
					"Reverse return"
				] ] as const
			) {
				owner.reference( { refObjId: id, typeFlags, name } );
				const payload = new Uint8Array( 13 ), view = new DataView( payload.buffer );
				view.setUint32( 0, 10130, true );
				view.setUint32( 4, 2419200, true );
				view.setUint32( 8, id, true );
				payload[12] = uses;
				owner.receive( { opcode: 0x3021, payload }, 1000 );
			}
			context.state.gameplay.countJobs = owner.state();
			await context.step();
			await press( context, "count-job:10130" );
		}
	},
	{
		id: "breadth:specialty-buy",
		title: "Specialty trade - transport purchase quantity",
		family: "Services",
		source: "runtime/ui/ui.ts openSpecialtyPurchase; tests/runtime/specialty-deal-hud.test.mjs",
		resources: [ "ifspecialtydeal" ],
		controls: [ "specialty-deal-close", "specialty-deal-count" ],
		async open( context ) {
			await merchant( context );
			const shop = context.state.gameplay.shop;
			if ( !shop || !shop.offers[0] ) throw Error( "Specialty recipe requires the merchant offer" );
			context.state.gameplay.shop = {
				...shop,
				cosGid: 7,
				offers: [ {
					...shop.offers[0],
					name: "Synthetic specialty goods",
					items: [ { ...herb, typeFlags: 0x46c, name: "Synthetic specialty goods" } ]
				} ]
			};
			context.state.gameplay.cosRecords = [ {
				gid: 7,
				refObjId: 2183,
				band: 2,
				hp: 100,
				mp: 0,
				status: 28,
				dead: false,
				inventory: []
			} ];
			await press( context, "shop-offer:0" );
		}
	}
];
