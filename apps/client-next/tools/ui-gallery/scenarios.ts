/*
===========================================================================
scenarios.ts - explicit synthetic checkpoints, using production empty owners

These are presentation seeds, not native captures or a fake game server.
Stall transitions are driven separately through its production reducer.
===========================================================================
*/
import { emptySocial } from "../../src/engine/foundation/gameplay/social";
import { emptyStall, stallRequest } from "../../src/engine/foundation/gameplay/stall";
import { uiPanels } from "../../src/engine/foundation/ui/panels";
import { SERVICE_WINDOWS } from "./entry-recipes.mjs";
import { extendedJourneys } from "./extended";

import type { FixtureState } from "./fixture-state";
import { npcGid } from "../../src/engine/foundation/gameplay/npc-dialogue";

export const SCENARIO_VERSION = 2;

export interface Journey {
	id: string;
	family: "HUD" | "Guild" | "Stall" | "Panels" | "Services" | "Dialogs" | "Frontend" | "DOM" | "Transients";
	title: string;
	description: string;
	instructions: string;
	limits: string;
	actions: readonly (readonly [string, string])[];
	resources?: readonly string[];
}

const GUILD_LIMIT =
	"Production UI over synthetic social/NPC snapshots. Guild command responses and server permissions are unsupported; captured commands are never auto-succeeded.";
const STALL_LIMIT =
	"Production stall request/reply reducers for naming and bounded refusal/close responses. Inventory settlement, worker transport and multi-client trading remain unsupported.";
const HUD_LIMIT =
	"Real renderer, input bridge and report dialog. Recording is a labeled presentation state; capture, delivery, world rendering and live telemetry are unsupported.";

const focusedJourneys: readonly Journey[] = [
	{
		id: "hud-tools",
		family: "HUD",
		title: "Tools and availability",
		description: "Inspect the actual F, Record and report controls, then hide or restore reporting.",
		instructions:
			"Click all three tools. Reporting opens the production dialog; Record changes a simulated presentation state only.",
		limits: HUD_LIMIT,
		actions: [ [ "tools", "Toggle reporting tools" ], [ "record", "Toggle recording presentation" ], [
			"report",
			"Open report dialog"
		] ]
	},
	{
		id: "hud-companions",
		family: "HUD",
		title: "Target and companion overlap",
		description: "Review how the DOM tools move around companion and target chrome.",
		instructions:
			"Cycle companion modes and target visibility. Inspect the top edge and click-through space between tools.",
		limits: HUD_LIMIT,
		actions: [ [ "pets", "Cycle companions" ], [ "target", "Toggle target" ], [
			"record",
			"Toggle recording presentation"
		] ]
	},
	{
		id: "hud-compact",
		family: "HUD",
		title: "Compact viewport and minimap",
		description: "Resize the real UI without scaling a screenshot.",
		instructions:
			"Choose 375 × 667, expand the map and companions using their game controls, then return to desktop.",
		limits: HUD_LIMIT,
		actions: [ [ "pets", "Cycle companions" ], [ "target", "Toggle target" ] ]
	},
	{
		id: "hud-modal",
		family: "HUD",
		title: "Modal focus and window overlap",
		description: "Exercise the report dialog over a production inventory window.",
		instructions:
			"Open the report dialog; type, select text and dismiss it. Open Inventory again and test pointer gaps around the tool row.",
		limits: HUD_LIMIT,
		actions: [ [ "inventory", "Open Inventory" ], [ "report", "Open report dialog" ] ]
	},
	{
		id: "guild-empty",
		family: "Guild",
		title: "No guild and empty relations",
		description: "Start with no guild, then inspect a guild whose alliance and hostility lists are empty.",
		instructions: "Try Guild admission without membership; add the seeded guild and inspect its relation tabs.",
		limits: GUILD_LIMIT,
		actions: [ [ "guild-toggle", "Toggle guild membership snapshot" ], [ "guild", "Open Guild" ], [
			"relations",
			"Open relations tab"
		] ]
	},
	{
		id: "guild-roster",
		family: "Guild",
		title: "Roster, selection and overflow",
		description: "A 24-member roster exposes scrolling and selected-row state.",
		instructions:
			"Select a member in the game window and scroll. Remove the fixture's first nonmaster while selected; inspect authority cancel and reopen.",
		limits: GUILD_LIMIT,
		actions: [ [ "guild", "Open Guild" ], [ "remove-member", "Remove member 102 snapshot" ], [
			"authority",
			"Open authority dialog"
		] ]
	},
	{
		id: "guild-transfer",
		family: "Guild",
		title: "Unpartied master transfer",
		description:
			"The local character is guild master while party identity is zero. Guild member IDs differ from entity IDs.",
		instructions:
			"Select a successor, continue to confirmation, cancel and reopen. Downgrade the local member and inspect permission admission. Submission is logged, not sent.",
		limits: GUILD_LIMIT,
		actions: [ [ "transfer", "Open manager transfer" ], [ "permission", "Toggle master/member snapshot" ], [
			"guild",
			"Open Guild"
		] ]
	},
	{
		id: "guild-notice",
		family: "Guild",
		title: "Notice reading, editing and permissions",
		description: "Long multiline notice content exposes the production reader, edit fields and scroll owner.",
		instructions:
			"Read and scroll the notice, close/reopen, edit with text selection, then change the local permission snapshot.",
		limits: GUILD_LIMIT,
		actions: [ [ "notice", "Read notice" ], [ "notice-edit", "Edit notice" ], [
			"permission",
			"Toggle master/member snapshot"
		] ]
	},
	{
		id: "stall-owner",
		family: "Stall",
		title: "Owner board and price edits",
		description: "Inspect the ten-slot production board with empty and populated offers.",
		instructions:
			"Click an offer and modify its price. Compare dark, light and contrast backgrounds. Commands queue until explicitly applied.",
		limits: STALL_LIMIT,
		actions: [ [ "offers", "Toggle empty/full snapshot" ], [ "apply", "Apply queued requests" ], [
			"title",
			"Edit title"
		] ]
	},
	{
		id: "stall-visitor",
		family: "Stall",
		title: "Visitor open and closed",
		description: "A visitor board exposes native open/closed presentation and purchase confirmation.",
		instructions:
			"Toggle trading state, select an offer and confirm a purchase. Its request can be held and refused; successful inventory settlement is unsupported.",
		limits: STALL_LIMIT,
		actions: [ [ "trading", "Toggle open/closed snapshot" ], [ "offers", "Toggle empty/full snapshot" ], [
			"apply",
			"Apply queued requests"
		], [ "refuse", "Refuse pending request" ] ]
	},
	{
		id: "stall-pending",
		family: "Stall",
		title: "Naming, pending and refusal",
		description: "Creation uses the production request and reply paths with explicit response timing.",
		instructions:
			"Enter a title and confirm. The prompt should remain dismissed while queued. Apply requests, then accept or refuse creation.",
		limits: STALL_LIMIT,
		actions: [ [ "apply", "Apply queued requests" ], [ "accept", "Accept creation / close" ], [
			"refuse",
			"Refuse pending request"
		] ]
	},
	{
		id: "stall-reentry",
		family: "Stall",
		title: "Cancel and immediate naming reentry",
		description: "Replay the coalesced publication that previously reopened stale confirmations.",
		instructions:
			"Use the checkpoint to send real Cancel and Stall events, then apply both queued requests in one publication. Repeat and inspect namingSequence.",
		limits: STALL_LIMIT,
		actions: [ [ "reentry", "Cancel → Stall (queue both)" ], [ "apply", "Publish queued requests once" ], [
			"accept",
			"Accept creation / close"
		] ]
	}
];

/*
================
journeys

Registry probes use production names directly, without another window list.
Service-dependent admission remains explicit until a suitable seed is added.
================
*/
export const journeys: readonly Journey[] = [
	...focusedJourneys,
	...extendedJourneys,
	...Object.keys( SERVICE_WINDOWS ).map( name => ({
		id: `service:${name}`,
		family: "Services" as const,
		title: `Service · ${name}`,
		description: "Shared admission recipe from the production runtime sweep.",
		instructions: "Use the real controls; reset to restore the seeded service. Commands are recorded locally.",
		limits: "Synthetic service snapshot. No command transport, settlement or native parity claim.",
		actions: [ [ "open-entry", "Open entry" ], [ "escape", "Escape / close" ] ] as const
	}) ),
	...uiPanels().map( name => ({
		id: `surface:${name}`,
		family: "Panels" as const,
		title: `Panel · ${name}`,
		description: `Mount the production ${name} presentation using explicit synthetic prerequisites.`,
		instructions:
			"Use the real window controls. Escape closes its current dialog/window; Open entry uses the shared production sweep recipe, including explicit service/item prerequisite snapshots.",
		limits:
			"Target-specific controls witness only the mounted presentation. Direct activation does not prove visible menu reachability. Further states, server behavior and native parity remain unverified. World and 3D portraits are not mounted.",
		actions: [ [ "open-entry", "Open entry" ], [ "escape", "Escape / close" ] ] as const
	}) )
];

/*
================
guildSnapshot

Party identity deliberately remains zero; names resolve guild identity.
================
*/
export function guildSnapshot() {
	return {
		...emptySocial( "Reviewer" ),
		guild: {
			id: 7,
			name: "Review Guild",
			level: 5,
			gp: 500,
			crest: 0,
			subject: "A notice for the review team",
			contents: Array.from(
				{ length: 16 },
				( _, i ) => `Line ${i + 1}: inspect scrolling, selection and reopen behavior.`
			).join( "\n" ),
			members: Array.from( { length: 24 }, ( _, i ) => ({
				id: 101 + i,
				name: i === 0 ? "Reviewer" : `Member ${i + 1}`,
				grade: i === 0 ? 0 : 1,
				level: 40,
				permissions: i === 0 ? 31 : 0,
				grant: "",
				donated: 0,
				model: 1907,
				role: 0,
				offline: i > 0 && i % 3 === 0 ? 1 : 0
			}) )
		}
	};
}

/*
================
stallOffers
================
*/
export function stallOffers() {
	return Array.from( { length: 10 }, ( _, slot ) => ({
		slot,
		bagSlot: 13 + slot,
		quantity: slot + 1,
		price: 12345 + slot,
		item: {
			slot: 13 + slot,
			refObjId: 3630,
			typeFlags: 0x8ec,
			quantity: slot + 1,
			plus: 0,
			durability: 0,
			variance: "0",
			name: "HP recovery herb",
			icon: "item/etc/hp_potion_01.ddj",
			magic: []
		}
	}) );
}

/*
================
seedJourney

The production UiView boundary is cast once in frame.ts: these intentionally
bounded snapshots do not pretend to construct a complete gameplay session.
================
*/
export function seedJourney( id: string ): FixtureState {
	const player = {
		gid: 1,
		refObjId: 0,
		regionId: 0x6a48,
		x: 1000,
		y: 0,
		z: 1000,
		heading: 0,
		kind: "player",
		name: "Reviewer",
		mountedOn: 0
	};
	const pet = {
		gid: 9,
		refObjId: 7493,
		band: 2,
		hp: 500,
		mp: 0,
		status: 0,
		dead: false,
		level: 12,
		satiety: 5000,
		name: "Companion"
	};
	let stall = emptyStall();
	if ( id === "stall-owner" || id === "stall-visitor" ) {
		stall = {
			...stall,
			phase: id === "stall-owner" ? "owner" : "visitor",
			owner: id === "stall-owner" ? 1 : 3,
			title: "Review stall",
			greeting: "Welcome. Inspect the actual board and input controls.",
			open: id === "stall-visitor",
			offers: stallOffers(),
			visitors: [ 3, 4 ]
		};
	} else if ( id.startsWith( "stall-" ) ) stall = stallRequest( stall, { kind: "stall-name" } ).state;
	return {
		frontend: { phase: "world" },
		session: { phase: "world", revision: 1, character: "Reviewer" },
		gameplay: {
			revision: 1,
			localGid: 1,
			pose: { ...player, angle: 0 },
			authoritativePose: null,
			pendingMoves: 0,
			acknowledgedMove: 0,
			targetPending: 0,
			error: null,
			target: id.startsWith( "hud-" ) ? 8 : id === "guild-transfer" ? 99 : 0,
			targetCapabilities: id === "guild-transfer" ? 0x4000 : 0,
			// Absent when inactive: a "closed" object is truthy and reads as
			// an open conversation to the UI guards (ui.ts inventory-gold).
			npcConversation: id === "guild-transfer" ?
				{ phase: "menu", gid: npcGid( 99 ), dialogueRevision: 0 } :
				undefined,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventoryPending: false,
			inventory: stallOffers().map( offer => offer.item ),
			vitals: [],
			casts: [],
			skills: [],
			progression: { level: 40, gold: "1234567", experience: "1234", skillPoints: 200, masteries: [] },
			cosRecords: id.startsWith( "hud-" ) ? [ pet ] : [],
			social: id === "guild-empty" ? emptySocial( "Reviewer" ) : guildSnapshot(),
			stall,
			chat: {
				pending: false,
				error: null,
				lines: Array.from(
					{ length: 18 },
					( _, i ) => ({
						channel: 9,
						name: "Reviewer",
						text: `Stall message ${i + 1}`,
						outgoing: false,
						sequence: i + 1
					})
				)
			}
		},
		entities: [ player, { ...player, gid: 8, kind: "monster", name: "Hyungno Ghost", level: 24, maxHp: 1200 }, {
			...player,
			gid: 99,
			kind: "npc",
			name: "Guild manager"
		}, { ...player, gid: 3, name: "Seller" } ],
		worldReady: true
	};
}
