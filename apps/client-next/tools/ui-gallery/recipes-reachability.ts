/*
===========================================================================
recipes-reachability.ts - remaining social, targeted and frontend owner entries

Inputs are synthetic publications. Visible input and modal ownership remain
production behavior; commands never receive invented successful settlements.
===========================================================================
*/
import type { ExtendedRecipe, RecipeContext } from "./extended";
import { socialPacket } from "../../src/engine/foundation/gameplay/social";
import { fortressBootstrap } from "../../src/engine/foundation/gameplay/fortress";
import { jobOutcomeAnswer } from "../../src/engine/foundation/gameplay/job-guild";
import { OPENERS } from "./entry-recipes.mjs";
import { fixtureGuild, guildMember, herb, npc, openPanel, press } from "./recipe-input";

/*
================
party
================
*/
async function party( context: RecipeContext, leader: boolean ) {
	Object.assign( context.state.gameplay.social, {
		self: 11,
		leader: leader ? 11 : 12,
		options: 3,
		members: [ 11, 12, 13, 14, 15, 16, 17, 18 ].map( ( id, index ) => ({
			id,
			name: index ? "Party peer " + index : "Reviewer",
			model: 1907,
			level: 20 + index,
			status: 0xaa,
			region: 25000,
			x: index,
			y: 0,
			z: 0,
			war: 0,
			guild: "Review Guild"
		}) )
	} );
	await openPanel( context, "Party" );
	const controls = context.controls();
	if ( !!controls.find( row => row.id === "party-member-kick:12" ) !== leader ) {
		throw Error( "Party kick permission mismatch" );
	}
	if ( !!controls.find( row => row.id === "party-disband" )?.disabled === leader ) {
		throw Error( "Party disband permission mismatch" );
	}
}

/*
================
fortressTarget
================
*/
async function fortressTarget( context: RecipeContext, allowed: boolean ) {
	const game = context.state.gameplay;
	guildMember( context ).role = allowed ? 2 : 8;
	game.target = 31;
	game.targetCapabilities = 0;
	game.npcConversation = { phase: "closed" };
	game.fortress = {
		...fortressBootstrap( {
			gameWorldData: [ { gameWorldId: 7, warName: "FORTRESS_JANGAN" } ],
			siegeFortressData: [ { fortressId: 1, codeName: "FORTRESS_JANGAN", nameStrId: "SN_FORTRESS_JANGAN" } ]
		} ),
		worldId: 7,
		wars: [ { id: 1, name: fixtureGuild( context ).name, flags: 1 } ]
	};
	const player = context.state.entities[0];
	if ( !player ) throw Error( "Fortress recipe requires its player snapshot" );
	context.state.entities.push( {
		...player,
		gid: 31,
		refObjId: 19565,
		kind: "structure",
		tidWord: 0x2c4 | (6 << 11),
		name: "Barricade",
		maxHp: 80000
	} );
	await context.step();
	if ( !allowed && context.controls().some( row => row.id === "target-structure-remove" ) ) {
		throw Error( "Unauthorized structure delete is visible" );
	}
}

/*
================
reachabilityRecipes
================
*/
export const reachabilityRecipes: readonly ExtendedRecipe[] = [
	...([ "objectives", "reward", "abandon" ] as const).map( mode => ({
		id: "reach:quest-" + mode,
		title: "Quests - " + mode,
		family: "Dialogs" as const,
		source: "tests/runtime/ui-behavior.test.mjs quest objectives (683); runtime/ui/ui.ts quest details",
		resources: [ "ifquest", "ifquestreward", "ifquestslotsub" ],
		controls: mode === "objectives" ?
			[ "quest-expand:2" ] :
			mode === "reward" ?
			[ "quest-reward", "quest-details-close" ] :
			[ "quest-abandon-yes", "quest-abandon-no" ],
		async open( context: RecipeContext ) {
			context.state.gameplay.quests = [ {
				refId: 2,
				u08: 0,
				u09: 0,
				flags: 24,
				u10: mode === "reward" ? 2 : 1,
				contents: [ {
					tag: 1,
					kind: 1,
					description: "SN_CON_QNO_CH_SOLDIER_EA1_1",
					objectiveSentinel: false,
					objectiveValues: [ 7 ]
				} ],
				targetIds: []
			} ];
			await openPanel( context, "Quests" );
			if ( mode === "objectives" ) await press( context, "quest-expand:2" );
			else {
				await press( context, "quest:2" );
				if ( mode === "abandon" ) await press( context, "quest-abandon" );
			}
		}
	}) ),
	...([ "modify", "collect" ] as const).map( action => ({
		id: "reach:fortress-tax-" + action,
		title: "Fortress tax - " + action + " confirmation",
		family: "Services" as const,
		source: "tools/ui-gallery/entry-recipes.mjs Fortress tax; runtime/ui/ui.ts fortress-tax-modify/collect",
		controls: [ "fortress-tax-yes", "fortress-tax-no" ],
		async open( context: RecipeContext ) {
			guildMember( context ).role = 1;
			await OPENERS["Fortress tax"]!( context, context.step );
			await context.step();
			await press( context, "fortress-tax-" + action );
		}
	}) ),
	{
		id: "reach:job-outcome",
		title: "Job guild - outcome collection",
		family: "Services",
		source: "foundation/gameplay/job-guild.ts jobOutcomeAnswer; runtime/ui/ui.ts NPC outcome page",
		controls: [ "npc-job-collect:3" ],
		async open( context ) {
			npc( context, 0x200000, "Hunter official" );
			context.state.gameplay.job = { type: 3, grade: 1, exp: 0, alias: "Hunter", contribution: 0 };
			await context.step();
			context.state.gameplay.jobOutcome =
				jobOutcomeAnswer( { opcode: 0xb7be, payload: Uint8Array.of( 1, 0, 0xe8, 3, 0, 0 ) }, null, 99 )!
					.outcome;
		}
	},
	{
		id: "reach:npc-recall",
		title: "NPC - designate recall confirmation",
		family: "Services",
		source: "tests/runtime/ui-behavior.test.mjs recall menu (5053)",
		controls: [ "recall-confirm", "recall-cancel" ],
		async open( context ) {
			npc( context, 0x40, "Recall official" );
			await press( context, "npc-recall-designate" );
		}
	},
	...[ false, true ].map( race => ({
		id: "frontend:stage-" + (race ? "race" : "dock"),
		title: "Production 3D stage - " + (race ? "race selection" : "empty dock"),
		family: "Frontend" as const,
		source: "runtime/frontend/frontend.ts; runtime/frontend/stage/stage.ts; runtime/frontend/camera/camera.ts",
		limits:
			"Production frontend, stage, camera, character presentation and readiness. Session is synthetic and outgoing operations are captured. Advance the clock for transitions and animation.",
		controls: race ? [ "frontend:back" ] : [ "frontend:create", "frontend:leave" ],
		open: ( context: RecipeContext ) => context.stage( race )
	}) ),
	...[ "populated", "selected", "delete", "restore", "europe", "china" ].map( mode => ({
		id: "frontend:stage-" + mode,
		title: "Production 3D characters - " + mode,
		family: "Frontend" as const,
		source: "runtime/frontend/frontend.ts; runtime/characters/characters.ts; runtime/characters/dock-preview.ts",
		limits:
			"Synthetic roster, actual production character assemblies and readiness. Use clock advance for animation. Commands are captured; no server settlement.",
		controls: mode === "populated" ?
			[ "frontend:create", "frontend:leave" ] :
			mode === "selected" ?
			[ "enter", "dock:delete", "dock:back" ] :
			mode === "delete" || mode === "restore" ?
			[ "dock:warning-accept", "dock:warning-cancel" ] :
			[ "create:name", "create:male", "create:female" ],
		open: ( context: RecipeContext ) => context.stage( mode )
	}) ),
	...[ true, false ].map( leader => ({
		id: "reach:party-" + (leader ? "leader" : "member"),
		title: "Party - full roster, " + (leader ? "leader" : "member") + " permissions",
		family: "Dialogs" as const,
		source: "runtime/ui/ui.ts Party branch; foundation/gameplay/social-roster.ts",
		resources: [ "ifparty", "ifpartyslot" ],
		controls: [ leader ? "party-member-kick:12" : "party-leave", "party-disband" ],
		open: ( context: RecipeContext ) => party( context, leader )
	}) ),
	{
		id: "reach:party-approval",
		title: "Party matching - incoming approval",
		family: "Dialogs",
		source: "tests/runtime/ui-behavior.test.mjs party matching approval (3268)",
		resources: [ "ifmessagebox" ],
		controls: [ "party-answer:1", "party-answer:0" ],
		open( context ) {
			context.state.gameplay.partyMatching = {
				page: 0,
				pages: 1,
				rows: [],
				own: null,
				pending: null,
				result: null,
				auto: [],
				request: {
					a: 7,
					b: 42,
					native7c0: 0,
					primary: 0,
					secondary: 0,
					flags: 0,
					member: {
						id: 77,
						name: "Peer",
						model: 1907,
						level: 20,
						region: 1,
						status: 0xaa,
						x: 0,
						y: 0,
						z: 0,
						war: 0
					},
					expires: 20000
				}
			};
		}
	},
	{
		id: "reach:party-joining",
		title: "Party matching - joining progress and timeout",
		family: "Dialogs",
		source: "tests/runtime/ui-behavior.test.mjs native party join progress (3297)",
		texture: "pt_progress.png",
		open( context ) {
			context.state.gameplay.partyMatching = {
				page: 0,
				pages: 1,
				rows: [],
				own: null,
				request: null,
				pending: "join",
				result: null,
				auto: [],
				joining: { name: "Peer", since: 1000 }
			};
			context.advance( 200 );
		}
	},
	{
		id: "reach:guild-war-agreement",
		title: "Guild - incoming war agreement",
		family: "Guild",
		source: "foundation/gameplay/guild-war.ts; runtime/ui/ui.ts guild war consent",
		resources: [ "ifrequestguildwar" ],
		controls: [ "invite-accept", "invite-refuse" ],
		open( context ) {
			context.state.gameplay.social.invitation = {
				type: 10,
				gid: 3,
				war: { name: "Blue", mode: 0, period: 0xffffffff, scoreIndex: 7, stake: 400 }
			};
		}
	},
	{
		id: "reach:guild-war-result",
		title: "Guild - war result",
		family: "Guild",
		source: "tests/runtime/guild-war.test.mjs ended result (214)",
		controls: [ "war-result-close" ],
		open( context ) {
			context.state.gameplay.social.warResult = {
				key: "UIIT_MSG_GUILDWAR_WINERGUILD",
				names: [ "Review Guild" ],
				sequence: 1
			};
		}
	},
	{
		id: "reach:guild-compensation",
		title: "Guild manager - compensation answer",
		family: "Guild",
		source: "runtime/ui/ui.ts social.compensation to guildManagerHud question",
		controls: [ "guild-manager-yes", "guild-manager-no" ],
		open( context ) {
			npc( context, 0x4000 );
			context.state.gameplay.social.compensation = 70000;
		}
	},
	{
		id: "reach:guild-soldiers",
		title: "Guild manager - soldier attributes",
		family: "Guild",
		source: "tests/runtime/guild-manager.test.mjs soldier attribute visible-menu lifetime (202)",
		controls: [ "npc-guild-soldier:1", "npc-guild-soldier:2", "npc-guild-soldier:4", "npc-guild-soldier:8" ],
		async open( context ) {
			npc( context, 0x4000 );
			await context.step();
			context.state.gameplay.social = socialPacket( context.state.gameplay.social, {
				opcode: 0xb322,
				payload: Uint8Array.of( 1, 8 )
			} )!;
		}
	},
	{
		id: "reach:guild-warehouse",
		title: "Guild manager - populated warehouse",
		family: "Guild",
		source: "runtime/ui/ui.ts npc-guild:warehouse; foundation/gameplay/storage-room.ts",
		resources: [ "ifstorageroom" ],
		controls: [ "storage-slot:0" ],
		async open( context ) {
			npc( context, 0x4000 );
			context.state.gameplay.storage = {
				phase: "function",
				npc: 99,
				guild: true,
				capacity: 30,
				gold: "0",
				items: []
			};
			await press( context, "npc-guild:warehouse" );
			context.state.gameplay.storage = {
				phase: "open",
				npc: 99,
				guild: true,
				capacity: 30,
				gold: "10000",
				items: [ { ...herb, slot: 0 } ]
			};
		}
	},
	{
		id: "reach:fortress-remove",
		title: "Fortress - barricade removal confirmation",
		family: "Services",
		source: "tests/runtime/fortress-structure-delete.test.mjs barricadeFixture",
		controls: [ "structure-remove-confirm", "structure-remove-cancel" ],
		async open( context ) {
			await fortressTarget( context, true );
			await press( context, "target-structure-remove" );
		}
	},
	{
		id: "reach:fortress-remove-denied",
		title: "Fortress - barricade without removal permission",
		family: "Services",
		source: "tests/runtime/fortress-structure-delete.test.mjs role 8 denial",
		controls: [ "clear-target" ],
		open: context => fortressTarget( context, false )
	},
	{
		id: "reach:npc-portal",
		title: "NPC - portal destination menu",
		family: "Services",
		source: "tests/runtime/ui-behavior.test.mjs NPC portal (3912); published HUD teleport links",
		controls: [ "npc-close" ],
		async open( context ) {
			npc( context, 0x80, "Ferry" );
			await press( context, "npc-portal-open" );
			if ( !context.controls().some( row => row.id.startsWith( "npc-portal:" ) ) ) {
				throw Error( "No published destinations for this portal reference" );
			}
		}
	}
];
