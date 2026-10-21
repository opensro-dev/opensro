/*
===========================================================================
recipes-completion.ts - remaining authored layouts and populated primary views

Each seed supplies data to a production owner and checks a specific target.
Server responses are never synthesized as consequences of submitted commands.
===========================================================================
*/
import type { ExtendedRecipe, RecipeContext } from "./extended";
import { OPENERS } from "./entry-recipes.mjs";
import { fixtureGuild, guildMember, herb, openPanel, press } from "./recipe-input";
import { defaultGameOptions } from "../../src/engine/foundation/gameplay/game-options";

/*
================
edit
================
*/
function edit( context: RecipeContext, id: string, value: string ) {
	if ( !context.controls().some( row => row.id === id && !row.disabled ) ) throw Error( "Missing edit: " + id );
	context.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
}

/*
================
completionRecipes
================
*/
export const completionRecipes: readonly ExtendedRecipe[] = [
	{
		id: "inspect:overhead-actor",
		title: "HUD - projected name and speech boards",
		family: "HUD",
		source: "runtime/ui/ui.ts worldLabelStart; runtime/ui/hud/speech.ts; renderer characterAnchor",
		anchor: 1,
		async open( context ) {
			await context.actor();
			context.ui.event( { kind: "preferences", value: defaultGameOptions() } );
			context.state.gameplay.chat = {
				...context.state.gameplay.chat,
				lines: [ {
					channel: 1,
					gid: 1,
					name: "Reviewer",
					text: "Synthetic speech board for human review",
					sequence: 100,
					outgoing: false
				} ]
			};
			await context.step();
		}
	},
	...[ "relations", "scores", "surrender" ].map( mode => ({
		id: "inspect:guild-war-" + mode,
		title: "Guild war - populated " + mode,
		family: "Guild" as const,
		source: "runtime/ui/hud/guild-war-hud.ts; runtime/ui/ui.ts ifhostileguild/ifguildwar",
		control: mode === "surrender" ?
			"war-confirm" :
			mode === "scores" ?
			"war-contribution-sort:60" :
			"war-select:77",
		async open( context: RecipeContext ) {
			context.state.gameplay.social.wars = [ {
				id: 77,
				enemyId: 8,
				name: "Blue",
				type: 0,
				localScore: 100,
				enemyScore: 50,
				word38: 0,
				word3c: 0
			} ];
			await openPanel( context, "Guild" );
			await press( context, mode === "scores" ? "guild-tab:2" : "guild-tab:1" );
			if ( mode !== "scores" ) {
				await press( context, "war-relation:1" );
				await press( context, "war-select:77" );
				if ( mode === "surrender" ) await press( context, "war-surrender" );
			}
		}
	}) ),
	{
		id: "frontend:stage-login",
		title: "Login - production title stage",
		family: "Frontend",
		source: "runtime/frontend/frontend.ts; runtime/ui/title/login.ts",
		limits:
			"Actual production title stage, camera, renderer and login controls. Session is synthetic; authentication and server replies are unsupported.",
		controls: [ "account", "password" ],
		async open( context ) {
			await context.stage( "login" );
		}
	},
	{
		id: "inspect:mall-actor",
		title: "Item Mall - production preview actor",
		family: "Panels",
		source: "runtime/characters/mall-preview.ts; runtime/ui/ui.ts mallPreview",
		control: "item-mall-close",
		async open( context ) {
			await context.actor();
			const { extendedRecipes } = await import( "./extended" );
			await extendedRecipes.find( row => row.id === "dialog:item-mall" )!.open( context );
		}
	},
	{
		id: "inspect:magic-pop-card",
		title: "Magic Pang - populated card inventory",
		family: "Panels",
		source: "runtime/ui/ui.ts Magic Pang; foundation/gameplay/gacha.ts",
		controls: [ "gacha-card", "gacha-play" ],
		async open( context ) {
			context.state.gameplay.inventory = [ {
				...herb,
				slot: 13,
				typeFlags: 0xf6c,
				name: "Synthetic Magic Pang card"
			} ];
			await openPanel( context, "Magic Pang" );
		}
	},
	{
		id: "inspect:gm-console",
		title: "GM console - local privileged input",
		family: "HUD",
		source: "runtime/ui/ui.ts Shift Backquote handler; runtime/ui/hud/gm-console.ts",
		control: "gm-input",
		async open( context ) {
			context.state.gameplay.eligibility = {
				pcRoomEvent: false,
				...context.state.gameplay.eligibility,
				gm: true
			};
			await context.step();
			context.ui.event( { kind: "key", code: "Backquote", shift: true } );
			context.advance( 1000 );
			await context.step();
		}
	},
	{
		id: "inspect:item-tooltip",
		title: "Inventory - production item tooltip",
		family: "HUD",
		source: "foundation/ui/helper-bubble.ts; runtime/ui/ui.ts inventory hover",
		texture: "/com_tooltip_corner.png",
		async open( context ) {
			context.state.gameplay.inventory = [ { ...herb, slot: 14 } ];
			await openPanel( context, "Inventory" );
			context.ui.event( { kind: "hover", id: "slot:14" } );
			context.advance( 1000 );
			await context.step();
		}
	},
	{
		id: "inspect:inventory-actor",
		title: "Inventory - production character paperdoll",
		family: "Panels",
		source: "runtime/characters/characters.ts; runtime/ui/ui.ts __doll; renderer character assembly",
		controls: [ "doll-left", "doll-right", "equipment-view" ],
		async open( context ) {
			await context.actor();
			await openPanel( context, "Inventory" );
		}
	},
	{
		id: "inspect:skin-actor",
		title: "Skin change - production preview actor",
		family: "Panels",
		source: "runtime/characters/mall-preview.ts; runtime/ui/ui.ts skinPreview",
		controls: [ "skin:male", "skin:female", "skin-close" ],
		async open( context ) {
			await context.actor();
			await OPENERS["Skin change"]!( context, context.step );
		}
	},
	{
		id: "inspect:alchemy",
		title: "Alchemy - selected equipment",
		family: "Panels",
		source: "tests/runtime/alchemy-ctrl-click.test.mjs; runtime/ui/ui.ts Alchemy",
		controls: [ "alchemy-slot:13", "alchemy-start" ],
		async open( context ) {
			context.state.gameplay.inventory = [ {
				...herb,
				slot: 13,
				refObjId: 3633,
				typeFlags: 0x132c,
				quantity: 1,
				name: "Sword",
				icon: "item/china/weapon/sword_01.ddj"
			} ];
			await openPanel( context, "Alchemy" );
			await press( context, "slot:13", "double-activate" );
		}
	},
	{
		id: "inspect:stall-network",
		title: "Stall network - populated search result",
		family: "Stall",
		source: "foundation/gameplay/stall.ts StallListing; runtime/ui/ui.ts Stall network",
		controls: [ "stall-net-row:0", "stall-net-search" ],
		async open( context ) {
			await OPENERS["Stall network"]!( context, context.step );
			const stall = context.state.gameplay.stall;
			context.state.gameplay.stall = {
				...stall,
				network: {
					...stall.network,
					pages: 1,
					rows: [ { item: { ...herb }, owner: 3, slot: 0, quantity: 10, price: 500n, serial: 1n } ]
				}
			};
		}
	},
	...[ "list", "add" ].map( mode => ({
		id: "inspect:blocking-" + mode,
		title: "Blocking - " + mode,
		family: "Dialogs" as const,
		source: "runtime/ui/ui.ts Blocking and blocking-add",
		controls: mode === "list" ?
			[ "blocking-row:0", "blocking-remove" ] :
			[ "blocking-name", "blocking-ok", "blocking-cancel" ],
		async open( context: RecipeContext ) {
			context.state.gameplay.chat = { ...context.state.gameplay.chat, blocked: [ "MutedPeer", "AnotherPeer" ] };
			await openPanel( context, "Blocking" );
			if ( mode === "add" ) await press( context, "blocking-add" );
		}
	}) ),
	...[ "declaration", "confirmation" ].map( mode => ({
		id: "inspect:guild-war-" + mode,
		title: "Guild war - " + mode,
		family: "Guild" as const,
		source: "runtime/ui/hud/guild-war-hud.ts; runtime/ui/ui.ts ifguildwarrequest/ifguildwarconfirm",
		controls: mode === "declaration" ?
			[ "war-name", "war-confirm", "war-cancel" ] :
			[ "war-confirm", "war-cancel" ],
		async open( context: RecipeContext ) {
			await openPanel( context, "Guild" );
			await press( context, "guild-tab:1" );
			await press( context, "war-relation:1" );
			await press( context, "war-declare" );
			edit( context, "war-name", "Blue" );
			await context.step();
			if ( mode === "confirmation" ) await press( context, "war-confirm" );
		}
	}) ),
	{
		id: "inspect:fortress-make",
		title: "Fortress production - quantity question",
		family: "Dialogs",
		source: "runtime/ui/hud/fortress-production-hud.ts; tests/runtime/fortress-production.test.mjs",
		controls: [ "fortress-production-count", "fortress-production-yes", "fortress-production-no" ],
		async open( context ) {
			await OPENERS["Fortress production"]!( context, context.step );
			const game = context.state.gameplay;
			guildMember( context ).role = 1;
			if ( !game.fortress ) throw Error( "Production recipe requires its fortress publication" );
			game.fortress = { ...game.fortress, wars: [ { id: 1, name: fixtureGuild( context ).name, flags: 0 } ] };
			await context.step();
			await press( context, "fortress-production-make:9001" );
		}
	},
	{
		id: "frontend:stage-confirm",
		title: "Creation - production confirmation overlay",
		family: "Frontend",
		source: "runtime/frontend/creation/creation.ts; runtime/ui/title/creation.ts",
		controls: [ "create:confirm", "create:confirm-cancel" ],
		limits:
			"Actual production creation actor and local validation. Confirmation submission is captured without server settlement.",
		async open( context ) {
			await context.stage( "europe" );
			edit( context, "create:name", "Reviewer" );
			await press( context, "create:weapon:next" );
			await press( context, "create:ok" );
			context.advance( 1000 );
			await context.step();
		}
	},
	{
		id: "inspect:hud-low-health",
		title: "HUD - low health caution",
		family: "HUD",
		source: "runtime/ui/ui.ts caution atlas; tests/runtime/ui-behavior.test.mjs",
		texture: "caution",
		open( context ) {
			context.ui.event( { kind: "preferences", value: defaultGameOptions() } );
			context.state.gameplay.vitals = [ { gid: 1, hp: 10, mp: 100, maxHp: 100, maxMp: 100 } ];
			context.advance( 1000 );
		}
	},
	{
		id: "inspect:hud-region",
		title: "HUD - native region banner",
		family: "HUD",
		source: "runtime/ui/hud/region-banner.ts; foundation/ui/region-banner.ts",
		texture: "/area_deco.png",
		async open( context ) {
			context.advance( 1000 );
			await context.step();
		}
	},
	...([ "starting", "finishing" ] as const).map( phase => ({
		id: "dom:record-" + phase,
		title: "Report recording - " + phase,
		family: "DOM" as const,
		source: "runtime/bug-report/dialog.ts createRecordControl",
		selector: ".sro-bug-record[data-phase='" + phase + "']:not([hidden])",
		open( context: RecipeContext ) {
			context.report.showRecording( phase, 12, 30 );
		}
	}) )
];
