/*
===========================================================================
extended.ts - explicit admission seeds for UI outside the panel registry

Each recipe names its production entry and its evidence. These snapshots never
replace production rendering, modal ownership, validation or request reducers.
===========================================================================
*/
import type { EntryFixture } from "./entry-recipes.mjs";
import type { Platform } from "../../src/engine/contracts/runtime";
import type { createBugReportDialog } from "../../src/engine/runtime/bug-report/dialog";
import type { Journey } from "./scenarios";
import type { UiSemantics } from "../../src/engine/contracts/ui";
import { commerceRecipes } from "./recipes-commerce";
import { socialRecipes } from "./recipes-social";
import { reachabilityRecipes } from "./recipes-reachability";
import { surfaceRecipes } from "./recipes-surfaces";
import { completionRecipes } from "./recipes-completion";
import { fixtureGuild, guildMember } from "./recipe-input";
import { experimentalOptions } from "../../src/engine/foundation/ui/experimental-options";
import { npcGid } from "../../src/engine/foundation/gameplay/npc-dialogue";
import type { FixtureState } from "./fixture-state";

export interface RecipeContext extends EntryFixture {
	state: FixtureState;
	step(): Promise<void>;
	advance( milliseconds: number ): void;
	platform: Platform;
	report: ReturnType<typeof createBugReportDialog>;
	frontend( mode: string ): void;
	stage( mode: boolean | string ): Promise<void>;
	actor(): Promise<void>;
	loading( error: boolean ): void;
	controls(): UiSemantics["controls"];
}
export interface ExtendedRecipe {
	id: string;
	title: string;
	family: Journey["family"];
	source: string;
	resources?: readonly string[];
	control?: string;
	controls?: readonly string[];
	selectedControls?: readonly string[];
	selector?: string;
	texture?: string;
	anchor?: number;
	unsupported?: string;
	limits?: string;
	open( context: RecipeContext ): void | Promise<void>;
}

/*
================
activate
================
*/
function activate( context: RecipeContext, id: string ) {
	context.ui.event( { kind: "activate", id } );
}

/*
================
useItem

Publish the item first, then use its actual bag slot after the owner observed it.
================
*/
async function useItem( context: RecipeContext, refObjId: number, typeFlags: number, name: string ) {
	context.state.gameplay.inventory = [ {
		slot: 13,
		refObjId,
		typeFlags,
		name,
		quantity: 3,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		icon: "item/etc/hp_potion_01.ddj"
	} ];
	activate( context, "open-window:Inventory" );
	await context.step();
	context.ui.event( { kind: "double-activate", id: "slot:13" } );
}

/*
================
guildManager
================
*/
async function guildManager( context: RecipeContext, row: string ) {
	const game = context.state.gameplay;
	game.target = 99;
	game.targetCapabilities = 0x4000;
	game.npcConversation = { phase: "menu", gid: npcGid( 99 ), dialogueRevision: 1 };
	if ( row === "create" ) game.social = { ...game.social, guild: null };
	else {
		game.social = { ...game.social, guild: { ...fixtureGuild( context ), level: 2 } };
		if ( row === "secede" || row === "vote" ) guildMember( context ).grade = 1;
		if ( row === "vote" ) {
			// Candidates are what opens the election window; without them the
			// UI waits for a vote instead (ui.ts awaitVote path).
			fixtureGuild( context ).votes = [
				{ id: 7, kind: 0, remainingMs: 60000, candidates: [ { id: 102, ballots: 1 } ] }
			];
		}
	}
	await context.step();
	activate( context, "npc-guild:" + row );
}

/*
================
extendedRecipes

Variants are separate review contexts, even where one production owner draws them.
================
*/
export const extendedRecipes: readonly ExtendedRecipe[] = [
	...surfaceRecipes,
	...completionRecipes,
	...reachabilityRecipes,
	...commerceRecipes,
	...socialRecipes,
	...([ [ "party", 2 ], [ "exchange", 1 ], [ "guild", 5 ], [ "union", 6 ] ] as const).map( ( [name, type] ) => ({
		id: `dialog:invite-${name}`,
		title: `${name} invitation`,
		family: "Dialogs" as const,
		source: "tests/runtime/ui-behavior.test.mjs:1832",
		control: "invite-accept",
		open( context: RecipeContext ) {
			context.state.entities = context.state.entities.map( ( row ) =>
				row.gid === 3 ? { ...row, guildName: "Visitor Guild" } : row
			);
			context.state.gameplay.social = { ...context.state.gameplay.social, invitation: { type, gid: 3 } };
		}
	}) ),
	...([ "create", "level-up", "dissolve", "secede", "release", "vote" ] as const).map( row => ({
		id: `dialog:guild-${row}`,
		title: `Guild manager · ${row}`,
		family: "Dialogs" as const,
		source: "runtime/ui/ui.ts npc-guild / runtime/ui/hud/guild-manager-hud.ts",
		// ui.ts selects these pages in the corresponding manager draw branch.
		resources: row === "level-up" ?
			[ "ifguildlevelup" ] :
			row === "vote" ?
			[ "ifguildmasterelection" ] :
			[ "ifmessagebox" ],
		control: row === "create" ?
			"guild-manager-text" :
			row === "vote" ?
			"guild-election-remaining" :
			"guild-manager-yes",
		open: ( context: RecipeContext ) => guildManager( context, row )
	}) ),
	{
		id: "dialog:reverse-return",
		title: "Reverse Return destination",
		family: "Dialogs",
		source: "runtime/ui/hud/reverse-return-hud.ts / runtime/ui/ui.ts item-use",
		controls: [ "reverse-scroll-cancel", "reverse-scroll:2" ],
		open: context => useItem( context, 4002, 0x19ec, "Reverse Return Scroll" )
	},
	{
		id: "dialog:restoration",
		title: "Skill Restoration Potion",
		family: "Dialogs",
		source: "tests/runtime/withdrawal.test.mjs / runtime/ui/hud/withdrawal.ts",
		resources: [ "ifskillwithdrawal" ],
		control: "withdrawal-close",
		open: context => useItem( context, 3828, 0x6ec, "Skill Restoration Potion" )
	},
	{
		id: "dialog:resuscitation",
		title: "NPC skill resuscitation",
		family: "Dialogs",
		source: "runtime/ui/ui.ts restorationRevision / tests/runtime/withdrawal.test.mjs",
		resources: [ "ifskillwithdrawal" ],
		control: "withdrawal-close",
		open( context ) {
			context.state.gameplay.restorationRevision = 1;
		}
	},
	{
		id: "dialog:item-mall",
		title: "Item Mall catalogue and purchase dialogs",
		family: "Dialogs",
		source: "tests/runtime/item-mall.test.mjs / runtime/ui/hud/item-mall.ts",
		// ui.ts mall catalogue and buy-confirmation draw paths. Related resources
		// are navigational links, not an assertion that every section was reviewed.
		resources: [
			"ifitemmall",
			"ifitemmallshop",
			"ifitemmallshopslot",
			"ifitemmallconfirmbuy",
			"ifitemmallconfirmslot",
			"ifitemmallusepoint"
		],
		control: "item-mall-close",
		async open( context ) {
			context.state.gameplay.itemMall = {
				tabs: [ { shop: 2, tab: 0, category: "MALL_CONSUME", label: "Consumables" } ],
				offers: Array.from( { length: 8 }, ( _, slot ) => ({
					group: 852,
					shop: 2,
					tab: 0,
					slot,
					packageId: 20000 + slot,
					name: "Review package " + (slot + 1),
					description: "Synthetic offer; purchases are not sent.",
					icon: "item/etc/hp_potion_01.ddj",
					silk: 20,
					giftSilk: 0,
					currencyMask: 22,
					allowsPoints: true,
					purchaseLimit: 5,
					itemIds: [ 3630 ]
				}) ),
				silk: 100,
				giftSilk: 0,
				points: 15,
				pending: false,
				revision: 1
			};
			await context.step();
			activate( context, "item-mall" );
		}
	},
	{
		id: "dialog:mall-notice",
		title: "First-entry Item Mall notice",
		family: "Dialogs",
		source: "runtime/ui/ui.ts mallNotice session-entry owner",
		resources: [ "ifmallnotifywnd" ],
		control: "mall-notice-close",
		async open( context ) {
			context.state.session = { phase: "signed-out", revision: 2 };
			await context.step();
			context.state.session = { phase: "world", revision: 3, character: "Reviewer" };
		}
	},
	...([ false, true ] as const).map( mutation => ({
		id: `dialog:resurrection-${mutation ? "mutation" : "normal"}`,
		title: `Resurrection · ${mutation ? "mutation" : "normal"}`,
		family: "Dialogs" as const,
		source: "tests/runtime/ui-behavior.test.mjs:1491",
		control: "resurrection-accept",
		async open( context: RecipeContext ) {
			context.state.gameplay.vitals = [ { gid: 1, hp: 0, mp: 0, deathState: true } ];
			context.state.entities = context.state.entities.map( ( row ) =>
				row.gid === 1 ? { ...row, appearanceState: [ 2, 0, 0 ] } : row
			);
			await context.step();
			context.state.gameplay.social = { ...context.state.gameplay.social, resurrection: { gid: 3, mutation } };
		}
	}) ),
	...([ 10, 40 ] as const).map( level => ({
		id: `dialog:death-${level}`,
		title: `Death and rebirth · level ${level}`,
		family: "Dialogs" as const,
		source: "tests/runtime/ui-behavior.test.mjs:1304",
		control: "rebirth-point",
		async open( context: RecipeContext ) {
			context.state.gameplay.progression = { ...context.state.gameplay.progression, level };
			context.state.gameplay.vitals = [ { gid: 1, hp: 0, mp: 0, deathState: true } ];
			context.state.entities = context.state.entities.map( ( row ) =>
				row.gid === 1 ? { ...row, appearanceState: [ 2, 0, 0 ] } : row
			);
			await context.step();
			context.advance( 3100 );
		}
	}) ),
	{
		id: "dialog:disconnect",
		title: "Disconnected with retained HUD",
		family: "Dialogs",
		source: "tests/runtime/ui-behavior.test.mjs:2686",
		control: "disconnect-confirm",
		open( context ) {
			context.state.session = {
				...context.state.session,
				phase: "disconnected",
				disconnectMessage: "Synthetic connection loss"
			};
		}
	},
	{
		id: "dialog:world-error",
		title: "World resource failure and retry",
		family: "Dialogs",
		source: "runtime/ui/ui.ts worldError / world-load-retry",
		control: "world-load-retry",
		open( context ) {
			context.state.worldError = "Synthetic local fixture: world asset could not be loaded";
		}
	},
	...([
		"login",
		"servers-empty",
		"authenticating",
		"dock-empty",
		"dock-selected",
		"delete",
		"restore",
		"customize-europe",
		"customize-china"
	] as const).map( mode => ({
		id: `frontend:${mode}`,
		title: `Frontend · ${mode}`,
		family: "Frontend" as const,
		control: mode.startsWith( "customize-" ) ?
			"create:name" :
			mode === "delete" || mode === "restore" ?
			"dock:warning-accept" :
			mode === "dock-empty" ?
			"frontend:create" :
			mode === "dock-selected" ?
			"enter" :
			"account",
		source: "runtime/ui/title/title.ts / runtime/frontend/creation/creation.ts / runtime/frontend/dialog/dialog.ts",
		open: ( context: RecipeContext ) => context.frontend( mode )
	}) ),
	...([ false, true ] as const).map( failed => ({
		id: `dom:loading-${failed ? "failed" : "progress"}`,
		title: `DOM loading · ${failed ? "failure" : "transfer progress"}`,
		family: "DOM" as const,
		source: "index.html / runtime/platform/platform.ts presentUi and presentLoading",
		selector: "#startup-loading[data-active='true']",
		open: ( context: RecipeContext ) => context.loading( failed )
	}) ),
	{
		id: "dom:update",
		title: "Release update notice and input block",
		family: "DOM",
		source: "index.html / runtime/platform/platform.ts presentUpdate",
		selector: "#update-notice:not([hidden])",
		open( context ) {
			context.platform.presentUpdate( true );
		}
	},
	{
		id: "dom:report-archive",
		title: "Report archive and unavailable delivery",
		family: "DOM",
		source: "runtime/bug-report/dialog.ts savedList",
		selector: ".sro-bug-report",
		open( context ) {
			context.report.open( {
				text: "Local fixture report with synthetic archive entries.",
				replay: null,
				maxBytes: 1000000,
				replayState: "unsupported",
				replayError: "No recorder or delivery in this fixture"
			} );
		}
	},
	{
		id: "dom:report-offer",
		title: "Requested report download toast",
		family: "DOM",
		source: "runtime/bug-report/dialog.ts offer",
		selector: ".sro-bug-request",
		open( context ) {
			context.report.offer( {
				id: "QA-SYNTHETIC",
				createdAt: "2026-01-01T12:00:00Z",
				description: "Synthetic report",
				bytes: 2000000,
				delivered: false
			}, "Local fixture" );
		}
	},
	{
		id: "transient:unique",
		title: "Unique monster announcement",
		family: "Transients",
		source: "tests/runtime/ui-behavior.test.mjs:922 / runtime/ui/hud/unique-banner.ts",
		texture: "/com_warning_",
		open( context ) {
			context.state.gameplay.notices = [ {
				sequence: 1,
				key: "UIIT_MSG_ANYONE_DEAD_UNIC",
				value: 0,
				arguments: [ "Reviewer", "Cerberus" ],
				banner: true
			} ];
		}
	},
	{
		id: "transient:seal-star",
		title: "Seal of Star drop: Event tab and notice",
		family: "Transients",
		source: "#1976 / runtime/ui/hud/unique-banner.ts + foundation/ui/chat-layout.ts",
		open( context ) {
			sealDropFixture( context, "Looter has dropped D6 Star Blade (Seal Of Star) from Tiger Girl", false );
		}
	},
	{
		id: "transient:seal-moon",
		title: "Seal of Moon drop: Event tab and notice",
		family: "Transients",
		source: "#1976 / runtime/ui/hud/unique-banner.ts + foundation/ui/chat-layout.ts",
		open( context ) {
			sealDropFixture( context, "Looter has dropped D6 Moon Blade (Seal Of Moon) from Tiger Girl", false );
		}
	},
	{
		id: "transient:seal-sun",
		title: "Seal of Sun drop: Event tab and notice",
		family: "Transients",
		source: "#1976 / runtime/ui/hud/unique-banner.ts + foundation/ui/chat-layout.ts",
		open( context ) {
			sealDropFixture( context, "SEAL OF SUN: Looter has dropped D6 Sun Blade from Tiger Girl", true );
		}
	}
];

/*
================
sealDropFixture

One seal drop on both of its surfaces (#1976): the tier-coloured
top-centre notice, and its channel-7 chat line beside an ordinary All
line and a Party line, so the Event tab (Experimental row on) and the
All tab can be compared. The wording is the server's own
(action/announce.go).
================
*/
function sealDropFixture( context: RecipeContext, text: string, sunDrop: boolean ) {
	// The fixture document retires saved preferences, so the Event tab's
	// Experimental row is restored through the production event instead.
	context.ui.event( {
		kind: "experimental-preferences",
		value: experimentalOptions( { eventChatTab: true } )
	} );
	context.state.gameplay.notices = [ {
		sequence: 1,
		key: "",
		text,
		value: 0,
		notificationBanner: true,
		bannerOnly: true,
		dropPlus: true,
		...(sunDrop ? { sunDrop: true as const } : {})
	} ];
	context.state.gameplay.chat = {
		pending: false,
		error: null,
		lines: [
			{ channel: 1, name: "Reviewer", text: "An ordinary line in All.", outgoing: false, sequence: 1 },
			{ channel: 7, name: "", text, outgoing: false, sequence: 2 },
			{ channel: 4, name: "Reviewer", text: "A party line.", outgoing: false, sequence: 3 }
		]
	};
}

export const extendedJourneys: readonly Journey[] = extendedRecipes.map( recipe => ({
	id: recipe.id,
	family: recipe.family,
	title: recipe.title,
	resources: recipe.resources,
	description: `Production opening recipe: ${recipe.source}`,
	instructions:
		"Use the real controls. Reset restores this exact context. Advance time tests expiry/fades. Unsupported commands are recorded without a server response.",
	limits: recipe.limits ?? recipe.unsupported ?? (recipe.family === "Frontend" ?
		"Production title UI and creation/delete dialog owners. The 3D dock, stage renderer, camera navigation and live session are not mounted; unsupported navigation is logged." :
		"Synthetic prerequisites and real production owners. Entry accessibility is not exhaustive state coverage or native parity. No game/report request leaves this fixture."),
	actions: [ [ "open-extended", "Open recipe again" ], [ "advance", "Advance clock 1 second" ], [
		"escape",
		"Escape / close"
	], [ "play", "Play / pause fixture clock" ] ]
}) );
