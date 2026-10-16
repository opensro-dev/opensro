/*
===========================================================================
prototype-scope.ts - finite human QA admission checklist at the inspected revision

This is a browsing map, not an exhaustive native parity or state certification.
Production registry membership, probes and explicit human reviews remain distinct.
===========================================================================
*/
import { uiPanels } from "../../src/engine/foundation/ui/panels";

const populated: Record<string, string> = {
	Academy: "breadth:academy-master",
	"Academy Matching": "inspect:academy-matching",
	Alchemy: "inspect:alchemy",
	Blocking: "inspect:blocking-list",
	"COS inventory": "breadth:cos-inventory",
	Guild: "guild-roster",
	Inventory: "inspect:inventory-actor",
	"Magic Pop": "inspect:magic-pop-card",
	Party: "reach:party-leader",
	Quests: "reach:quest-objectives",
	Shop: "breadth:merchant",
	Skills: "inspect:skills-list",
	"Skin change": "inspect:skin-actor",
	"Stall network": "inspect:stall-network",
	Storage: "inspect:storage",
	"Fortress production": "inspect:fortress-make"
};

export const prototypeScope = {
	goal:
		"Access each inventoried implemented UI layout and its essential local controls for human review. A mount is not a native-correctness claim.",
	A: {
		meaning: "Unique implemented visual surfaces still inaccessible",
		remaining: [] as { id: string; blocker: string; source: string; }[],
		panels: uiPanels().map( name => ({
			name,
			entry: "surface:" + name,
			inspect: populated[name] ?? "surface:" + name,
			browse:
				"Use the production tabs, lists, selection, scroll and local dialogs. Server submission is captured.",
			source: "foundation/ui/panels.ts; tests/helpers/ui-entry-recipes.mjs; runtime/ui/ui.ts"
		}) ),
		additionalLayouts: [
			[
				"Frontend",
				"frontend:stage-login",
				"Actual title, empty/populated dock, selected/delete/restore, both race actors, customization and creation confirmation"
			],
			[
				"Guild services",
				"guild-transfer",
				"Authority, notice/editor, transfer, election, union, war input/confirmation/agreement/surrender/result, soldiers, warehouse and compensation"
			],
			[
				"Stall",
				"stall-owner",
				"Owner, visitor, naming, greeting/title, price, purchase and pending/refusal presentation"
			],
			[
				"Items and commerce",
				"inspect:inventory-gold",
				"Gold, split/drop, storage gold, exchange, skill learning, restoration, repair, premium and Mall quote/points/basket/actor"
			],
			[
				"Social/NPC",
				"inspect:npc-choices",
				"Invitations, party registration/approval/wait, quest conversation/acceptance/reward/abandon, job and fortress service questions"
			],
			[
				"HUD",
				"inspect:overhead-actor",
				"Tools, target/companions, player/target buffs, buff dismissal, quest/region/unique alerts, low health, tooltip, GM console, name/speech boards"
			],
			[
				"DOM and session",
				"dom:recording",
				"Report/archive/offer, recording phases, loading/progress/failure/update, death/resurrection/disconnect/world error"
			]
		]
	},
	B: {
		meaning: "Essential local operation",
		remaining: [],
		available: [
			"Fresh iframe reset and deterministic clock with explicit play/pause",
			"Real keyboard/text/selection/drag/scroll/modal cancellation",
			"Production stage camera, picking, actor assembly and readiness",
			"Source/asset fingerprint, separate visual/interaction/integration review axes, explicit save and portable export"
		],
		boundary:
			"No successful server response is invented to make a button appear functional. Unsupported submissions are visible in the trace. Specialty quantity, Shop and COS inventory mount with distinct close identities; closing the transport ends the local trade presentation. The preceding visible menu/request/reply flow and live settlement remain unverified."
	},
	C: {
		meaning: "Outside prototype completion",
		items: [
			"Exhaustive permissions, fullness, timing, stale selections and every server refusal/success combination",
			"Cross-client transactions, authoritative permissions, login authentication and character creation/deletion settlement",
			"Real report capture/upload/archive operations and live telemetry",
			"Every world scene, lighting, occlusion, viewport, overlap and font/content permutation",
			"Native screenshot/gameplay parity and version reconciliation for reverse-engineering annotations",
			"Native resource trees without implemented browser owners, including disabled Guild Friends/Letter tabs"
		]
	}
};
