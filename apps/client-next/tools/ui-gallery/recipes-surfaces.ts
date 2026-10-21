/*
===========================================================================
recipes-surfaces.ts - finite missing layouts, not exhaustive state permutations

Synthetic publications populate the actual UI owners. Shared modal templates
need representative operation, not every possible server outcome.
===========================================================================
*/
import type { ExtendedRecipe, RecipeContext } from "./extended";
import { herb, npc, openPanel, press } from "./recipe-input";
import { OPENERS, SERVICE_WINDOWS } from "./entry-recipes.mjs";
import { npcGid } from "../../src/engine/foundation/gameplay/npc-dialogue";

/*
================
existing

Reuse a registered seed only after module initialization, without duplicating it.
================
*/
async function existing( context: RecipeContext, id: string ) {
	const { extendedRecipes } = await import( "./extended" );
	const recipe = extendedRecipes.find( row => row.id === id );
	if ( !recipe ) throw Error( "Missing source recipe: " + id );
	await recipe.open( context );
	await context.step();
}

/*
================
inventory
================
*/
async function inventory( context: RecipeContext ) {
	context.state.gameplay.inventory = [ { ...herb } ];
	await openPanel( context, "Inventory" );
}

/*
================
buffs
================
*/
function buffs( context: RecipeContext ) {
	const game = context.state.gameplay;
	const metadata = {
		group: 1,
		level: 1,
		spCost: 0,
		trainable: false,
		targetRequired: false,
		cooldownMs: 0,
		masteries: [],
		prerequisites: []
	};
	const skills = [
		{ ...metadata, id: 7, name: "Buff", icon: herb.icon, buffCancel: "direct" as const },
		{ ...metadata, id: 8, name: "Scroll", icon: herb.icon, buffCancel: "confirm" as const, buffSecondary: true }
	];
	game.skillCatalog = skills;
	game.buffSlots = skills.map( ( row, index ) => ({
		state: "active",
		serial: index + 1,
		secondary: false,
		effect: { gid: 1, skill: row.id, token: 100 + index, phase: 1 }
	}) );
	game.target = 8;
	game.vitals = [ { gid: 8, hp: 600, maxHp: 1200, abnormal: 8 } ];
	game.attachedEffects = [ { gid: 8, skill: 7, token: 20, phase: 1 }, { gid: 8, skill: 8, token: 21, phase: 2 } ];
}

/*
================
surfaceRecipes
================
*/
export const surfaceRecipes: readonly ExtendedRecipe[] = [
	{
		id: "inspect:inventory-gold",
		title: "Inventory - gold amount dialog",
		family: "Dialogs",
		source: "runtime/ui/ui.ts inventory-gold; tests/runtime/ui-behavior.test.mjs",
		controls: [ "gold-amount", "drop-gold", "gold-cancel" ],
		async open( context ) {
			await inventory( context );
			await press( context, "inventory-gold" );
		}
	},
	{
		id: "inspect:inventory-split",
		title: "Inventory - split stack dialog",
		family: "Dialogs",
		source: "runtime/ui/ui.ts Shift slot activation; tests/runtime/ui-behavior.test.mjs",
		controls: [ "split-amount", "split-confirm", "split-cancel" ],
		async open( context ) {
			await inventory( context );
			if ( !context.controls().some( row => row.id === "slot:14" ) ) throw Error( "Missing seeded stack" );
			context.ui.event( { kind: "activate", id: "slot:14", shift: true } );
		}
	},
	{
		id: "inspect:inventory-drop",
		title: "Inventory - ground drop confirmation",
		family: "Dialogs",
		source: "runtime/ui/ui.ts drop-item; tests/runtime/ui-behavior.test.mjs",
		controls: [ "ground-drop-confirm", "ground-drop-cancel" ],
		async open( context ) {
			await inventory( context );
			if ( !context.controls().some( row => row.id === "slot:14" ) ) throw Error( "Missing draggable item" );
			context.ui.event( { kind: "drag", id: "slot:14", dx: 100, dy: 0 } );
			context.ui.event( { kind: "drag-end", id: "slot:14", x: 50, y: 350 } );
		}
	},
	{
		id: "inspect:storage",
		title: "Storage - populated room and gold",
		family: "Services",
		source: "runtime/ui/ui.ts Storage; tools/ui-gallery/entry-recipes.mjs",
		controls: [ "storage-slot:0", "storage-gold" ],
		async open( context ) {
			await OPENERS.Storage!( context, context.step );
			const storage = context.state.gameplay.storage;
			if ( !storage ) throw Error( "Storage recipe requires an open room" );
			context.state.gameplay.storage = {
				...storage,
				gold: "20000",
				items: [ { ...herb, slot: 0 } ]
			};
		}
	},
	{
		id: "inspect:storage-gold",
		title: "Storage - gold transfer dialog",
		family: "Dialogs",
		source: "runtime/ui/ui.ts storage-gold",
		controls: [ "gold-amount", "drop-gold", "gold-cancel" ],
		async open( context ) {
			await existing( context, "inspect:storage" );
			await press( context, "storage-gold" );
		}
	},
	{
		id: "inspect:exchange",
		title: "Exchange - populated offers",
		family: "Services",
		source: "runtime/ui/ui.ts Exchange; foundation/gameplay/exchange.ts",
		controls: [ "exchange-my:0", "exchange-their:0", "exchange-close" ],
		async open( context ) {
			await SERVICE_WINDOWS.Exchange!( context, context.step );
			const exchange = context.state.gameplay.exchange;
			if ( !exchange ) throw Error( "Exchange recipe requires its publication" );
			context.state.gameplay.exchange = {
				...exchange,
				partner: 3,
				own: [ { slot: 0, bagSlot: 14, item: { ...herb } } ],
				theirs: [ { slot: 0, item: { ...herb } } ],
				ownGold: 100,
				theirGold: 200
			};
		}
	},
	{
		id: "inspect:academy-matching",
		title: "Academy matching - populated listings",
		family: "Services",
		source: "foundation/gameplay/academy.ts AcademyListing; runtime/ui/ui.ts matching board",
		controls: [ "academy-row:7", "academy-search" ],
		async open( context ) {
			await OPENERS["Academy Matching"]!( context, context.step );
			context.state.gameplay.academy = {
				member: false,
				page: 0,
				pages: 1,
				request: null,
				result: null,
				rows: [ {
					id: 7,
					kind: 0,
					detail: "Review academy - new students welcome",
					level: 80,
					model: 1907,
					name: "Teacher",
					students: 4,
					grade: 1,
					graduates: 12
				} ]
			};
		}
	},
	...[ "list", "confirm" ].map( mode => ({
		id: "inspect:skills-" + mode,
		title: "Skills - populated " + mode,
		family: "Dialogs" as const,
		source: "runtime/ui/hud/skill-training.ts; runtime/ui/ui.ts skill-learn",
		controls: mode === "list" ? [ "skill-learn:291" ] : [ "skill-confirm-ok", "skill-confirm-cancel" ],
		async open( context: RecipeContext ) {
			const base = {
				id: 3,
				group: 174,
				level: 1,
				name: "SKILL_CH_SWORD_SMASH_A",
				nameSymbol: "SN_SKILL_CH_SWORD_SMASH_A",
				icon: "skill/china/sword_smash_a.ddj",
				spCost: 1,
				trainable: true,
				targetRequired: true,
				cooldownMs: 3000,
				masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
				prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
			};
			Object.assign( context.state.gameplay, {
				skills: [ 3 ],
				skillCatalog: [ base, { ...base, id: 291, level: 2, spCost: 5 } ],
				progression: {
					...context.state.gameplay.progression,
					level: 40,
					skillPoints: 200,
					masteries: [ { id: 257, level: 7 } ]
				}
			} );
			await openPanel( context, "Skills" );
			if ( mode === "confirm" ) await press( context, "skill-learn:291" );
		}
	}) ),
	...[ "choices", "accept" ].map( mode => ({
		id: "inspect:npc-" + mode,
		title: "NPC conversation - " + mode,
		family: "Dialogs" as const,
		source: "foundation/gameplay/npc-dialogue.ts; runtime/ui/hud/npc-panel.ts",
		controls: [ mode === "choices" ? "npc-choice:5" : "npc-choice:2", "npc-close" ],
		async open( context: RecipeContext ) {
			npc( context, 2, "General Sonhyeon" );
			context.state.gameplay.npcConversation = {
				phase: "ready",
				gid: npcGid( 99 ),
				dialogueRevision: 2,
				dialogue: {
					kind: mode === "choices" ? 4 : 3,
					prompt: mode === "choices" ? "SN_NPC_CH_SOLDIER_EA1_BS" : "SN_TALK_QNO_CH_SOLDIER_EA1_1_01",
					options: mode === "choices" ?
						[ { choice: 5, symbol: "SN_QNO_CH_SOLDIER_EA1_1" } ] :
						[ { choice: 2, symbol: "UIIT_STT_YES" }, { choice: 3, symbol: "UIIT_STT_NO" } ]
				}
			};
			await context.step();
			for (
				let i = 0;
				mode === "accept" && i < 40 && !context.controls().some( row => row.id === "npc-choice:2" );
				i++
			) {
				await press( context, "npc-scroll-down" );
			}
		}
	}) ),
	{
		id: "inspect:hud-buffs",
		title: "HUD - player buffs and target abnormal states",
		family: "HUD",
		source: "runtime/ui/ui.ts CIFBuffViewer; tests/runtime/ui-behavior.test.mjs",
		controls: [ "buff:100:7", "buff:101:8", "target-buff:0:buff:7", "target-buff:2:abnormal:3" ],
		open: buffs
	},
	{
		id: "inspect:hud-buff-dismiss",
		title: "HUD - buff dismissal confirmation",
		family: "Dialogs",
		source: "runtime/ui/ui.ts buff-dismiss",
		controls: [ "buff-dismiss-confirm", "buff-dismiss-cancel" ],
		async open( context ) {
			buffs( context );
			await press( context, "buff:101:8", "right-activate" );
		}
	},
	{
		id: "inspect:hud-quest-banner",
		title: "HUD - quest progress banner",
		family: "HUD",
		source: "runtime/ui/hud/quest-banner.ts authored questBanner publication",
		texture: "/com_quest_",
		open( context ) {
			context.state.gameplay.notices = [ {
				sequence: 1,
				key: "",
				value: 0,
				text: "Synthetic quest objective: 7 / 20",
				questBanner: true
			} ];
		}
	},
	...[ "quote", "points", "question" ].map( mode => ({
		id: "inspect:mall-" + mode,
		title: "Item Mall - " + mode + " dialog",
		family: "Dialogs" as const,
		source: "runtime/ui/hud/item-mall.ts; runtime/ui/ui.ts mall purchase draw paths",
		controls: mode === "points" ?
			[ "item-mall-points-apply", "item-mall-point-value" ] :
			mode === "question" ?
			[ "item-mall-question-confirm" ] :
			[ "item-mall-purchase", "item-mall-quantity" ],
		async open( context: RecipeContext ) {
			await existing( context, "dialog:item-mall" );
			await press( context, "item-mall-category:1" );
			if ( mode === "question" ) {
				await press( context, "item-mall-reserve:0" );
				return;
			}
			await press( context, "item-mall-buy:0" );
			if ( mode === "points" ) await press( context, "item-mall-points" );
		}
	}) ),
	{
		id: "dom:recording",
		title: "Report tools - recording progress",
		family: "DOM",
		source: "runtime/bug-report/dialog.ts createRecordControl via showRecording",
		selector: ".sro-bug-record[data-phase='recording']:not([hidden])",
		open( context ) {
			context.report.showRecording( "recording", 12, 30 );
		}
	}
];
