/*
===========================================================================
recipes-social.ts - populated social publications through production entry owners

Membership and permissions are synthetic publications. Commands are recorded as
unsupported integration; no server action or permission grant is simulated.
===========================================================================
*/
import type { ExtendedRecipe, RecipeContext } from "./extended";
import { fixtureGuild, guildMember, npc, openPanel, press } from "./recipe-input";

/*
================
academy
================
*/
async function academy( context: RecipeContext, master: boolean ) {
	context.state.gameplay.academy = {
		member: true,
		localMemberId: master ? 7 : 8,
		members: [ {
			id: 7,
			kind: 0,
			name: "Reviewer",
			level: 80,
			entryLevel: 80,
			model: 1907,
			offline: false,
			honor: 0,
			location: "Jangan"
		}, {
			id: 8,
			kind: 1,
			name: "Apprentice",
			level: 25,
			entryLevel: 1,
			model: 1907,
			offline: false,
			honor: 0,
			location: "Jangan"
		} ],
		subject: "Academy review",
		contents: "Synthetic populated notice for layout review.",
		request: null,
		rows: [],
		page: 0,
		pages: 0,
		result: null
	};
	await openPanel( context, "Academy" );
	const notice = context.controls().find( row => row.id === "academy-notice" );
	if ( !notice || Boolean( notice.disabled ) === master ) throw Error( "Academy notice permission mismatch" );
}

/*
================
union
================
*/
async function union( context: RecipeContext ) {
	Object.assign( context.state.gameplay.social, {
		allianceMaster: 7,
		alliances: [ { id: 7, name: "ReviewGuild", level: 5, master: "Reviewer", model: 1907, flags: 1 }, {
			id: 8,
			name: "Bravo",
			level: 3,
			master: "Fox",
			model: 1907,
			flags: 4
		} ]
	} );
	await openPanel( context, "Guild" );
	await press( context, "guild-tab:1" );
}

/*
================
socialRecipes
================
*/
export const socialRecipes: readonly ExtendedRecipe[] = [
	...([ 1, 2, 4 ] as const).map( flag => ({
		id: "breadth:fortress-hire-" + flag,
		title: "Fortress - hire " + ({ 1: "battle aide", 2: "smith", 4: "trainer" })[flag],
		family: "Services" as const,
		source: "runtime/ui/ui.ts fortressStaffView and npc-fortress-staff; runtime/ui/ui.ts fortressStaffHud",
		resources: [ "ifmessagebox" ],
		controls: [ "fortress-staff-yes", "fortress-staff-no" ],
		async open( context: RecipeContext ) {
			npc( context, 0x400000, "Fortress manager" );
			const game = context.state.gameplay;
			guildMember( context ).role = 1;
			game.fortress = {
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ {
					id: 1,
					code: "FORTRESS_JANGAN",
					nameStrId: "SN_FORTRESS_JANGAN",
					officialRefObjId: 2011
				} ],
				wars: [ { id: 1, name: fixtureGuild( context ).name, flags: 1 } ],
				registered: [],
				listId: 1,
				staffFlags: 0
			};
			await press( context, "npc-fortress-staff" );
			await press( context, "npc-fortress-hire:" + flag );
		}
	}) ),
	...[ true, false ].map( master => ({
		id: "breadth:academy-" + (master ? "master" : "member"),
		title: "Academy - " + (master ? "master" : "member") + " permission",
		family: "Services" as const,
		source: "tests/runtime/ui-behavior.test.mjs academy notice permission (5002)",
		resources: [ "ifacademy" ],
		controls: [ "academy-notice" ],
		open: ( context: RecipeContext ) => academy( context, master )
	}) ),
	{
		id: "breadth:academy-editor",
		title: "Academy - notice editor",
		family: "Dialogs",
		source: "tests/runtime/ui-behavior.test.mjs academy notice editor (5002)",
		resources: [ "ifguildnotifywrite" ],
		controls: [ "social-subject", "social-contents", "academy-notice-submit" ],
		async open( context ) {
			await academy( context, true );
			await press( context, "academy-notice" );
		}
	},
	{
		id: "breadth:party-registration",
		title: "Party matching - populated listing and registration",
		family: "Dialogs",
		source: "tests/runtime/ui-behavior.test.mjs party matching registration (3220)",
		resources: [ "ifpartyregister" ],
		controls: [ "party-form-title", "party-form-confirm" ],
		async open( context ) {
			Object.assign( context.state.gameplay.social, { self: 1, leader: 0, options: 3, members: [] } );
			context.state.gameplay.partyMatching = {
				page: 0,
				pages: 1,
				rows: [ {
					id: 42,
					party: 0,
					name: "Peer",
					race: 0,
					members: 1,
					type: 3,
					purpose: 0,
					min: 1,
					max: 90,
					title: "Hunting"
				} ],
				own: null,
				request: null,
				pending: null,
				result: null,
				auto: []
			};
			await openPanel( context, "Party Matching" );
			await press( context, "party-match:18" );
		}
	},
	{
		id: "breadth:guild-union",
		title: "Guild - populated union",
		family: "Guild",
		source: "runtime/ui/ui.ts guild relation list; runtime/ui/hud/union-hud.ts",
		resources: [ "ifguild" ],
		controls: [ "guild-union-invite", "guild-union-exit", "guild-union-expel" ],
		open: union
	},
	{
		id: "breadth:guild-union-exit",
		title: "Guild - union exit confirmation",
		family: "Guild",
		source: "runtime/ui/ui.ts guild-union-exit and union-ask",
		resources: [ "ifmessagebox" ],
		controls: [ "union-ask-yes", "union-ask-no" ],
		async open( context ) {
			await union( context );
			await press( context, "guild-union-exit" );
		}
	},
	...([ "join", "withdraw", "alias" ] as const).map( action => ({
		id: "breadth:job-" + action,
		title: "Job guild - " + action,
		family: "Services" as const,
		source: "runtime/ui/ui.ts NPC job rows and confirmations; foundation/gameplay/job-guild.ts",
		resources: [ "ifmessagebox" ],
		controls: action === "alias" ?
			[ "job-alias-text", "job-alias-check", "job-alias-cancel" ] :
			[ "job-confirm-yes", "job-confirm-no" ],
		async open( context: RecipeContext ) {
			npc( context, 0x80000, "Trader guild official" );
			context.state.gameplay.job = {
				type: action === "join" ? 0 : 1,
				grade: 1,
				exp: 0,
				alias: "",
				contribution: 0
			};
			await context.step();
			await press( context, "npc-job-" + action + ":1" );
		}
	}) )
];
