/*
===========================================================================

sro-macro-hud.ts - the SRO Macro windows' state (port-only, not native)

Port-only, not native (#3907): the v1.696 macro menu (CNIFMacroMenuWnd,
macro_start.2dt) and its options window (CNIFMacroWnd, macro_1.2dt with
the potion, skill and hunt tabs). v1.150 has neither window and no
layout for them, so sro-macro-window.ts draws them from the client's own
chrome; this module owns what they show and turns clicks into gameplay
commands. It is reachable only with the Experimental "SRO Macro" row on
and once the server (SRO_AUTO_MACRO) has replayed the settings.

The options window edits a draft of the live settings. Save sends one
0x7554 save per changed section and closes; the worker makes each live
only when 0xB554 accepts it. Cancel drops the draft.

Field meanings inside the opaque blocks are layout-derived hypotheses
(research note sro-macro.md): potion rows {a: checkbox, b: percent},
hunt bytes 0, 2, 4, 5, 7 option groups and 3, 6, 8..11 checkboxes.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import {
	MACRO_HUNT,
	MACRO_POTION,
	MACRO_SKILL,
	macroAvailable,
	type MacroKind,
	macroOn,
	type MacroSettings,
	type MacroSkill,
	potionSlot,
	withHuntByte,
	withPotionSlot
} from "@/engine/foundation/gameplay/sro-macro";

export const MACRO_ID = "sro-macro:";

// The gauge rows' percent stepper. No native step is read; 5 matches the
// v1.150 auto-potion window's slider granularity in practice.
export const MACRO_PERCENT_STEP = 5;
export const MACRO_PERCENT_MIN = 5;
export const MACRO_PERCENT_MAX = 95;
export const MACRO_PERCENT_DEFAULT = 50;
// macro_2.2dt's slot grid is not in the v1.150 data; eight per list is
// the inferred grid (two rows of four).
export const MACRO_SKILL_GRID = 8;
export const MACRO_ACQUIRED_ROWS = 8;

export type MacroWindow = "none" | "menu" | "options";
export type MacroPotionPage = "character" | "pet";

/*
================
MacroPotionRow

One potion row: its slot in the block and whether it is a gauge (a
checkbox and a percent) or a checkbox only.
================
*/
export interface MacroPotionRow {
	readonly slot: number;
	readonly caption: string;
	readonly gauge: boolean;
}

// Slot order from macro_1.2dt (hypothesis, sro-macro.md); captions are the
// v1.696 rows UIIT_STT_MACROPOTION_HP/MP, UIIT_STT_SRO_MACRO_*.
export const MACRO_POTION_ROWS: Readonly<Record<MacroPotionPage, readonly MacroPotionRow[]>> = {
	character: [
		{ slot: 0, caption: "HP", gauge: true },
		{ slot: 1, caption: "MP", gauge: true },
		{ slot: 2, caption: "Chinese Status Anomaly", gauge: false },
		{ slot: 3, caption: "European Status Anomaly", gauge: false }
	],
	pet: [
		{ slot: 4, caption: "HP", gauge: true },
		{ slot: 5, caption: "Hunger", gauge: true },
		{ slot: 6, caption: "Cure Status Anomaly", gauge: false },
		{ slot: 7, caption: "Resurrect", gauge: false },
		{ slot: 8, caption: "Summon Trigger", gauge: false }
	]
};

/*
================
MacroHuntGroup

One pick-one option group of the hunt block: its byte and its choices,
the byte holding the chosen index.
================
*/
export interface MacroHuntGroup {
	readonly byte: number;
	readonly caption: string;
	readonly choices: readonly string[];
}

// Group order from macro_3.2dt (hypothesis, sro-macro.md); captions and
// choices are the v1.696 UIIT_STT_SRO_MACRO_AUTO_HUNTING_* rows.
export const MACRO_HUNT_GROUPS: readonly MacroHuntGroup[] = [
	{ byte: 0, caption: "Configure Hunting Range", choices: [ "30 m", "50 m", "70 m", "100 m", "In place" ] },
	{ byte: 2, caption: "Configure Berserk Mode", choices: [ "All", "Not use" ] },
	{
		byte: 4,
		caption: "Configure Auto-Resurrect",
		choices: [ "Use Resurrection Scroll", "Resurrect in town", "Wait for resurrection" ]
	},
	{
		byte: 5,
		caption: "Configure Auto-Return",
		choices: [ "Not use", "In 1 hour", "In 3 hours", "In 5 hours", "In 10 hours" ]
	},
	{ byte: 7, caption: "Configure Auto-Repair", choices: [ "Use Repair Hammer", "Return to town" ] }
];

export const MACRO_HUNT_CHECKS: readonly { readonly byte: number; readonly caption: string; }[] = [
	{ byte: 3, caption: "Use Energy of Life" },
	{ byte: 6, caption: "Auto - Return when HP/MP potion remain less than 10" },
	{ byte: 8, caption: "Auto-Pickup" },
	{ byte: 9, caption: "Do not attack Dimension pillar" },
	{ byte: 10, caption: "Use Devil's Spirit, Angel's Spirit" },
	{ byte: 11, caption: "Whether to return to the town when the usage time ends" }
];

export interface MacroHudState {
	readonly window: MacroWindow;
	readonly tab: MacroKind;
	readonly potionPage: MacroPotionPage;
	readonly acquiredPage: number;
	readonly draft: MacroSettings | null;
}

/*
================
macroAcquiredSkills

The learned skills the skill tab offers: castable rows (an action time)
that are not the racial base attacks. INFERENCE: the v1.696 acquired
list (CNIFMacroAutoSkillWnd) is not read; passive rows have no cast, so
they cannot be automated.
================
*/
export function macroAcquiredSkills(
	learned: readonly number[],
	index: ReadonlyMap<number, SkillMetadata> | undefined
): SkillMetadata[] {
	const out: SkillMetadata[] = [];
	for ( const id of learned ) {
		const skill = index?.get( id );
		if ( skill && !skill.basicAttack && (skill.actionMs ?? 0) > 0 ) out.push( skill );
	}
	return out;
}

/*
================
renumber

A list's slot bytes are its grid positions. INFERENCE: the record's u8
is read as the slot index the grid places the skill in.
================
*/
function renumber( list: readonly MacroSkill[] ): MacroSkill[] {
	return list.map( ( skill, slot ) => ({ slot, skill: skill.skill }) );
}

/*
================
createSroMacroHud
================
*/
export function createSroMacroHud() {
	let window: MacroWindow = "none";
	let tab: MacroKind = MACRO_POTION;
	let potionPage: MacroPotionPage = "character";
	let acquiredPage = 0;
	let draft: MacroSettings | null = null;

	const close = () => {
		window = "none";
		draft = null;
	};
	const editDraft = ( change: ( settings: MacroSettings ) => MacroSettings ) => {
		if ( draft ) draft = change( draft );
	};
	return {
		/*
================
state
================
		*/
		state(): MacroHudState {
			return { window, tab, potionPage, acquiredPage, draft };
		},
		/*
================
toggleMenu

The /macro command: opens the menu, or closes whatever is open. False
when the macro cannot open (row off or no replay yet).
================
		*/
		toggleMenu( live: MacroSettings | undefined, enabled: boolean ): boolean {
			if ( window !== "none" ) {
				close();
				return true;
			}
			if ( !enabled || !live ) return false;
			window = "menu";
			return true;
		},
		/*
================
reconcile

Turning the row off, or a session without the server's replay, closes
the windows.
================
		*/
		reconcile( live: MacroSettings | undefined, enabled: boolean ) {
			if ( window !== "none" && (!enabled || !live) ) close();
		},
		close,
		/*
================
click

Handles one sro-macro: control and returns the commands to send. Ids:
switch:<kind>, options:<kind>, tab:<kind>, page:<character|pet>,
check:<slot>, percent:<slot>:<+1|-1>, hunt-group:<byte>, hunt-check:<byte>,
skill-add:<id>, skill-remove:<attack|buff>:<index>, acquired:<+1|-1>,
save, cancel, close.
================
		*/
		click(
			id: string,
			live: MacroSettings | undefined,
			acquired: readonly SkillMetadata[] = []
		): GameplayCommand[] {
			if ( !id.startsWith( MACRO_ID ) ) return [];
			const [verb = "", first = "", second = ""] = id.slice( MACRO_ID.length ).split( ":" );
			if ( verb === "close" || verb === "cancel" ) {
				close();
				return [];
			}
			if ( !live ) {
				close();
				return [];
			}
			const kind = Number( first ) as MacroKind;
			if ( verb === "switch" ) {
				if ( !macroAvailable( live, kind ) ) return [];
				return [ { kind: "macro-switch", macro: kind, on: !macroOn( live, kind ) } ];
			}
			if ( verb === "options" || verb === "tab" ) {
				if ( kind !== MACRO_POTION && kind !== MACRO_SKILL && kind !== MACRO_HUNT ) return [];
				if ( window !== "options" ) draft = live;
				window = "options";
				tab = kind;
				return [];
			}
			if ( verb === "save" ) {
				const saved = draft;
				close();
				if ( !saved ) return [];
				return ([ MACRO_POTION, MACRO_SKILL, MACRO_HUNT ] as const)
					.filter( section => macroAvailable( live, section ) )
					.map( section => ({ kind: "macro-save", macro: section, settings: saved }) );
			}
			if ( verb === "page" && (first === "character" || first === "pet") ) {
				potionPage = first;
				return [];
			}
			const slot = Number( first );
			if ( verb === "check" ) {
				editDraft( settings => {
					const row = potionSlot( settings.potion, slot );
					const gauge = Object.values( MACRO_POTION_ROWS ).flat().find( r => r.slot === slot )?.gauge;
					const b = gauge && row.b === 0 ? MACRO_PERCENT_DEFAULT : row.b;
					return {
						...settings,
						potion: withPotionSlot( settings.potion, slot, { ...row, a: row.a ? 0 : 1, b } )
					};
				} );
			} else if ( verb === "percent" ) {
				editDraft( settings => {
					const row = potionSlot( settings.potion, slot );
					const current = row.b || MACRO_PERCENT_DEFAULT;
					const b = Math.min(
						MACRO_PERCENT_MAX,
						Math.max( MACRO_PERCENT_MIN, current + Math.sign( Number( second ) ) * MACRO_PERCENT_STEP )
					);
					return { ...settings, potion: withPotionSlot( settings.potion, slot, { ...row, b } ) };
				} );
			} else if ( verb === "hunt-group" ) {
				const group = MACRO_HUNT_GROUPS.find( g => g.byte === slot );
				if ( group ) {
					editDraft( settings => ({
						...settings,
						hunt: withHuntByte(
							settings.hunt,
							slot,
							((settings.hunt[slot] ?? 0) + 1) % group.choices.length
						)
					}) );
				}
			} else if ( verb === "hunt-check" ) {
				if ( MACRO_HUNT_CHECKS.some( c => c.byte === slot ) ) {
					editDraft( settings => ({
						...settings,
						hunt: withHuntByte( settings.hunt, slot, settings.hunt[slot] ? 0 : 1 )
					}) );
				}
			} else if ( verb === "skill-add" ) {
				const skill = acquired.find( s => s.id === slot );
				editDraft( settings => {
					if ( !skill ) return settings;
					if (
						settings.attack.some( s => s.skill === slot ) || settings.buff.some( s => s.skill === slot )
					) {
						return settings;
					}
					const list = skill.targetRequired ? "attack" : "buff";
					if ( settings[list].length >= MACRO_SKILL_GRID ) return settings;
					return { ...settings, [list]: renumber( [ ...settings[list], { slot: 0, skill: slot } ] ) };
				} );
			} else if ( verb === "skill-remove" && (first === "attack" || first === "buff") ) {
				const index = Number( second );
				editDraft( settings => ({
					...settings,
					[first]: renumber( settings[first].filter( ( _, i ) => i !== index ) )
				}) );
			} else if ( verb === "acquired" ) {
				const pages = Math.max( 1, Math.ceil( acquired.length / MACRO_ACQUIRED_ROWS ) );
				acquiredPage = Math.min( pages - 1, Math.max( 0, acquiredPage + Math.sign( slot ) ) );
			}
			return [];
		},
		/*
================
reset

Leaving the world closes the windows.
================
		*/
		reset() {
			close();
			tab = MACRO_POTION;
			potionPage = "character";
			acquiredPage = 0;
		}
	};
}
