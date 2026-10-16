/*
===========================================================================
coverage-ledger.ts - source-backed family inventory, not a completion score

Recipe existence, observed presentation and human review are separate facts.
Remaining transitions stay visible even after a fixture has been reviewed.
===========================================================================
*/
import { journeys } from "./scenarios";
import { extendedRecipes } from "./extended";
import { prototypeScope } from "./prototype-scope";

const families = [
	[
		"training",
		"Restoration and resuscitation",
		/restoration|resuscitation|withdrawal/i,
		"runtime/ui/hud/withdrawal.ts",
		"Mastery costs, insufficient SP/gold/items, server refusal, replacement and reopen."
	],
	[
		"merchant",
		"Merchant and repair",
		/merchant|surface:Shop/i,
		"foundation/ui/merchant.ts",
		"Fresh quote publication, quantity limits, stale selection, busy/refused settlement, no-buyback warning."
	],
	[
		"academy",
		"Academy",
		/academy/i,
		"runtime/ui/ui.ts Academy branch",
		"Matching pages, join approval, graduation, master replacement, long roster and notice scrolling."
	],
	[
		"party",
		"Party and matching",
		/party/i,
		"runtime/ui/ui.ts Party and Party Matching branches",
		"Leader replacement, member churn while selected, approval expiry/refusal, pending/refused registration and listing refresh. Full roster, permission split, incoming approval and joining timeout have recipes."
	],
	[
		"guild",
		"Guild, union and services",
		/guild/i,
		"runtime/ui/ui.ts Guild branch; foundation/gameplay/guild-manager.ts",
		"War declaration/hostility/surrender and terms edits, soldier selection refusal, warehouse contention/transfers, compensation settlement and live permission changes. Agreement/result, soldier rows, populated warehouse and compensation now have recipes."
	],
	[
		"fortress",
		"Fortress services",
		/fortress/i,
		"runtime/ui/ui.ts fortressStaffView; runtime/ui/hud/fortress-war-hud.ts",
		"Tax settlement/refusal, production start/cancel/collect, occupied staff and noncommander staff denial, application confirmations. Tax questions and structure removal/denial have recipes."
	],
	[
		"job",
		"Job services",
		/job/i,
		"foundation/gameplay/job-guild.ts; runtime/ui/ui.ts NPC job rows",
		"Outcome collection settlement/refusal, alias pending/duplicate/refusal, withdrawal cooldown and rank refresh. Outcome collection menu is reachable."
	],
	[
		"cos",
		"Companions",
		/cos-|COS inventory|companions/i,
		"runtime/ui/ui.ts COS inventory; foundation/gameplay/cos-command.ts",
		"Transport cleanup, attack/riding variants, dead state, fullness, selection persistence and stale target rejection."
	],
	[
		"premium",
		"Premium and specialty trade",
		/premium|specialty/i,
		"foundation/gameplay/count-job.ts; runtime/ui/hud/specialty-deal-hud.ts",
		"Real package references/icons, expiration, spent reply and reverse return point. Specialty quantity, Shop and COS inventory now mount with distinct production close identities. Live trade settlement and visible menu/request/reply coverage remain unverified."
	],
	[
		"stall",
		"Stalls",
		/stall/i,
		"foundation/gameplay/stall.ts; runtime/ui/ui.ts Stall branch",
		"Full inventory, prices, contention, visitor purchase refusal and cross-client settlement."
	],
	[
		"npc",
		"NPC, quest and portal",
		/npc|quest|portal|recall/i,
		"runtime/ui/ui.ts NPC, Quests and teleport branches",
		"NPC quest conversation/acceptance chains, objective updates and long-list scrolling, reward choice/settlement, conditional portal destinations, stale target rejection and async replacements. Quest objectives/reward/abandon, portal menu and recall question are reachable."
	],
	[
		"frontend",
		"Frontend",
		/frontend:/i,
		"runtime/frontend/creation/creation.ts; tools/ui-qa/frontend-fixture.ts",
		"Optional C: account/shard refusal combinations and authoritative session/character transitions. Actual title, populated dock, both-race customization actors and production readiness are mounted."
	],
	[
		"hud",
		"HUD and DOM",
		/hud-|dom:|transient:/i,
		"runtime/ui/ui.ts; runtime/bug-report/dialog.ts",
		"Optional C: real capture/delivery, exhaustive focus/overlap and world backgrounds. Buffs, alerts, tooltips, recording phases, GM console and projected overhead boards have explicit recipes."
	],
	[
		"panels",
		"Other registered panels and dialogs",
		/.*/,
		"foundation/ui/panels.ts; tests/helpers/ui-entry-recipes.mjs",
		"Each panel has entry evidence only until populated states, permission/selection/scroll/modal/async transitions are reviewed."
	]
] as const;

/*
================
coverageRows

First match assigns each recipe once; no family count implies exhaustive UI.
================
*/
export function coverageRows() {
	return families.map( ( [id, title, , source, remaining] ) => ({
		id,
		title,
		source,
		remaining,
		prototype: {
			A: id === "premium" ? prototypeScope.A.remaining : [],
			B: prototypeScope.B.remaining,
			C: remaining
		},
		states: journeys.filter( journey => families.find( row => row[2].test( journey.id ) )?.[0] === id ).map(
			journey => {
				const recipe = extendedRecipes.find( row => row.id === journey.id );
				return {
					id: journey.id,
					title: journey.title,
					source: recipe?.source ?? source,
					requiredControls: recipe?.controls ?? (recipe?.control ? [ recipe.control ] : []),
					verification:
						"Recipe registered; consult fingerprinted probe evidence and human review separately.",
					integration:
						"Unsupported unless the scenario explicitly names a bounded production reducer response."
				};
			}
		)
	}) );
}
