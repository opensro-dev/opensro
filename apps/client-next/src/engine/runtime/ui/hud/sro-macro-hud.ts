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

A potion row is a RefItem ID and its on and percent bytes, read from the
v1.696 auto-potion readers (sro-macro.ts potionRowOn). The hunt bytes are
still layout-derived hypotheses (research note sro-macro.md): 0, 2, 4, 5,
7 option groups and 3, 6, 8..11 checkboxes.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import {
	MACRO_HUNT,
	MACRO_POTION,
	MACRO_SKILL,
	macroAvailable,
	type MacroGearKind,
	type MacroKind,
	macroGearBagSlot,
	macroOn,
	type MacroSettings,
	MACRO_POTION_SLOTS,
	MACRO_SKILLS_PER_LIST,
	type MacroSkill,
	potionRowOn,
	potionSlot,
	withHuntByte,
	withMacroGearSlot,
	withPotionRowItem,
	withPotionRowOn,
	withPotionRowPercent
} from "@/engine/foundation/gameplay/sro-macro";
import { macroPotionAdmits, scrubMacroPotion } from "@/engine/foundation/gameplay/sro-macro-potion";
import { macroGearAdmits, scrubMacroGear } from "@/engine/foundation/gameplay/sro-macro-skill";

export const MACRO_ID = "sro-macro:";
// CIFMacroSystemGuide click (0xab9080) owns toggleMenu; the control id is
// the under-minimap button's.
export const MACRO_GUIDE_ID = "sro-macro-icon";
// OnCreate (0xab8fe0) loads icon\etc\mecro_0.ddj; Media.pk2 has no mecro_1.
export const MACRO_GUIDE_ICON_PATH = "/assets/images/Media_extracted/icon/etc/mecro_0.png";
// INFERENCE: the ginterface child's screen rect is not read. Place with
// the other under-minimap guide icons (CIFQuestInfoGuide 69CCA0:
// x = width - 0x57, y = 0xB3); the texture is 32x32.
export const MACRO_GUIDE_ICON_X_INSET = 0x57;
export const MACRO_GUIDE_ICON_Y = 0xb3;

// The gauge rows' percent stepper. No native step is read; 5 matches the
// v1.150 auto-potion window's slider granularity in practice.
export const MACRO_PERCENT_STEP = 5;
export const MACRO_PERCENT_MIN = 5;
export const MACRO_PERCENT_MAX = 95;
export const MACRO_PERCENT_DEFAULT = 50;
// Skills-to-use capacity matches the wire (MACRO_SKILLS_PER_LIST). The
// skill tab draws a scrollable subset of that grid (macro_2.2dt).
export const MACRO_SKILL_GRID = MACRO_SKILLS_PER_LIST;

export type MacroWindow = "none" | "menu" | "options";
export type MacroPotionPage = "character" | "pet";
// Attack / Buff sub-tabs of CNIFMacroAutoSkillWnd (Party Buffs is not
// on the v1.150 / our wire skill section).
export type MacroSkillPage = "attack" | "buff";

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

// Slot order from macro_1.2dt (hypothesis, sro-macro.md). Captions match the
// later official Auto-Potion rows (HP/MP/Abnormal status/Moving speed): the
// readers are 00692e00, 00692d20 and 006930a0 (buff-item / speed scroll).
export const MACRO_POTION_ROWS: Readonly<Record<MacroPotionPage, readonly MacroPotionRow[]>> = {
	character: [
		{ slot: 0, caption: "HP", gauge: true },
		{ slot: 1, caption: "MP", gauge: true },
		{ slot: 2, caption: "Abnormal status", gauge: false },
		{ slot: 3, caption: "Moving speed", gauge: false }
	],
	// Official Pet page (macro_1.2dt): two gauges, then Cure / Resurrect /
	// Summon Trigger share one row of three. Captions match the later UI.
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
MacroHuntChoice

One radio of a hunt option group: the label and the byte value native
stores (not always the radio index — range and return-timer encode
specially; see sro-macro.md).
================
*/
export interface MacroHuntChoice {
	readonly value: number;
	readonly label: string;
}

/*
================
MacroHuntGroup

One pick-one option group of the hunt block: its byte and its choices.
================
*/
export interface MacroHuntGroup {
	readonly byte: number;
	readonly caption: string;
	readonly choices: readonly MacroHuntChoice[];
}

// Group order from macro_3.2dt / 00da8bb0; values from 0068e700 (range),
// 00687130 (return hours), 006862d0 / 00686b50 (resurrect / repair).
export const MACRO_HUNT_GROUPS: readonly MacroHuntGroup[] = [
	{
		byte: 0,
		caption: "Configure Hunting Range",
		choices: [
			{ value: 0, label: "In place" },
			{ value: 1, label: "30 m" },
			{ value: 2, label: "50 m" },
			{ value: 3, label: "70 m" },
			{ value: 4, label: "100 m" }
		]
	},
	{
		byte: 2,
		caption: "Configure Berserk Mode",
		choices: [ { value: 0, label: "All" }, { value: 1, label: "Not use" } ]
	},
	{
		byte: 4,
		caption: "Configure Auto-Resurrect",
		choices: [
			{ value: 0, label: "Use Resurrection Scroll" },
			{ value: 1, label: "Resurrect in town" },
			{ value: 2, label: "Wait for resurrection" }
		]
	},
	{
		byte: 5,
		caption: "Configure Auto-Return",
		choices: [
			{ value: 0, label: "Not use" },
			{ value: 1, label: "In 1 hour" },
			{ value: 3, label: "In 3 hours" },
			{ value: 5, label: "In 5 hours" },
			{ value: 10, label: "In 10 hours" }
		]
	},
	{
		byte: 7,
		caption: "Configure Auto-Repair",
		choices: [ { value: 0, label: "Use Repair Hammer" }, { value: 1, label: "Return to town" } ]
	}
];

export const MACRO_HUNT_CHECKS: readonly { readonly byte: number; readonly caption: string; }[] = [
	{ byte: 3, caption: "Use Energy of Life" },
	{ byte: 6, caption: "Auto - Return when HP/MP potion remain less than 10" },
	{ byte: 8, caption: "Auto-Pickup" },
	// Engine indices: pillar = hunt[10] (00686720 → 68c3d0), VIP return =
	// hunt[11] (006872d0). Byte 9 has no native engine reader; the port wakes
	// a dormant Spirit there (SRO_NASRUN). Native UI captions swap TIME_FIN
	// onto byte 9 and Spirit onto byte 11 — labels here follow the engine.
	{ byte: 9, caption: "Use Devil's Spirit, Angel's Spirit" },
	{ byte: 10, caption: "Do not attack Dimension pillar" },
	{ byte: 11, caption: "Whether to return to the town when the usage time ends" }
];

export interface MacroHudState {
	readonly window: MacroWindow;
	readonly tab: MacroKind;
	readonly potionPage: MacroPotionPage;
	readonly skillPage: MacroSkillPage;
	readonly acquiredScroll: number;
	readonly skillScroll: number;
	readonly draft: MacroSettings | null;
}

/*
================
MacroAcquiredBoardSkill

One skill-board cell (skillUi.json) used to order the acquired list the
way the Skills window lays out mastery rows.
================
*/
export interface MacroAcquiredBoardSkill {
	readonly id: number;
	readonly mastery: number;
	readonly row: number;
	readonly column: number;
	readonly icon: string;
	readonly name: string;
}

/*
================
macroSkillCastable

Whether Auto-Skills may offer or keep this row. Passives publish admit for
reqi (Lightning PASSIVE_*) but are never pressed — filter on passive, and
as a fallback drop non-target rows with no cast lifetime and no cooldown
(the same envelope passives show before the catalogue flag arrives).
================
*/
export function macroSkillCastable( skill: SkillMetadata ): boolean {
	if ( skill.basicAttack || skill.groundTarget || skill.passive ) return false;
	if ( !skill.targetRequired && (skill.actionMs ?? 0) <= 0 && skill.cooldownMs <= 0 ) return false;
	return true;
}

/*
================
macroAcquiredSkills

The learned skills the skill tab offers. INFERENCE: the v1.696 acquired
list (CNIFMacroAutoSkillWnd) is not read. Exclude racial base attacks,
ground-target rows (Ghost Walk), and passives — the macro cast path skips
all three. Instant self-buffs (Grass Walk, Castle Shield: actionMs omitted)
stay on their cooldown. Optional board rows sort the list in Skills-window
order so shield buffs are not buried by learn order.
================
*/
export function macroAcquiredSkills(
	learned: readonly number[],
	index: ReadonlyMap<number, SkillMetadata> | undefined,
	board: readonly MacroAcquiredBoardSkill[] = []
): SkillMetadata[] {
	const out: SkillMetadata[] = [];
	const seen = new Set<number>();
	for ( const id of learned ) {
		if ( seen.has( id ) ) continue;
		seen.add( id );
		const skill = index?.get( id );
		if ( !skill || !macroSkillCastable( skill ) ) continue;
		out.push( skill );
	}
	if ( board.length ) {
		const order = new Map( board.map( ( row, i ) => [ row.id, i ] as const ) );
		out.sort( ( a, b ) => (order.get( a.id ) ?? 1e9) - (order.get( b.id ) ?? 1e9) );
	}
	return out;
}

/*
================
macroSkillFitsPage

Attack takes target-required skills; Buff takes the rest (Grass Walk,
Castle Shield). A self-buff on the Attack tab is refused.
================
*/
export function macroSkillFitsPage( skill: SkillMetadata, page: MacroSkillPage ): boolean {
	return page === "attack" ? skill.targetRequired : !skill.targetRequired;
}

/*
================
macroSkillBound

True when this skill id, its skilldata group, or its SkillUiReplace group
is already on either Auto-Skills list — one Grass Walk rank blocks its
siblings.
================
*/
export function macroSkillBound(
	settings: MacroSettings,
	skill: SkillMetadata,
	index: ReadonlyMap<number, SkillMetadata> | undefined
): boolean {
	const bound = [ ...settings.attack, ...settings.buff ];
	if ( bound.some( entry => entry.skill === skill.id ) ) return true;
	for ( const entry of bound ) {
		const other = index?.get( entry.skill );
		if ( !other ) continue;
		if ( other.group && other.group === skill.group ) return true;
		const a = skill.admit?.replace, b = other.admit?.replace;
		if ( a && b && !a.cbuf && !b.cbuf && a.category === b.category && a.group === b.group ) {
			return true;
		}
	}
	return false;
}

/*
================
scrubMacroSkillLists

Drop duplicate id / group / replace entries that a draft or a live
replay may already hold, keeping the first of each (attack before buff).
================
*/
export function scrubMacroSkillLists(
	settings: MacroSettings,
	index: ReadonlyMap<number, SkillMetadata> | undefined
): MacroSettings {
	const taken: MacroSkill[] = [];
	const accept = ( entry: MacroSkill, page: MacroSkillPage ): boolean => {
		const skill = index?.get( entry.skill );
		const probe: SkillMetadata = skill ?? {
			id: entry.skill,
			group: 0,
			level: 0,
			name: "",
			spCost: 0,
			trainable: false,
			// Unknown rows keep their current list; only catalogued skills are
			// re-typed (Grass Walk off Attack, a strike off Buff).
			targetRequired: page === "attack",
			cooldownMs: 0,
			masteries: [],
			prerequisites: []
		};
		if ( skill && !macroSkillCastable( skill ) ) return false;
		if ( skill && !macroSkillFitsPage( skill, page ) ) return false;
		if ( macroSkillBound( { ...settings, attack: taken, buff: [] }, probe, index ) ) return false;
		taken.push( entry );
		return true;
	};
	const attack = renumber( settings.attack.filter( entry => accept( entry, "attack" ) ) );
	const buff = renumber( settings.buff.filter( entry => accept( entry, "buff" ) ) );
	const same = ( a: readonly MacroSkill[], b: readonly MacroSkill[] ) =>
		a.length === b.length && a.every( ( entry, i ) => entry.skill === b[i]!.skill && entry.slot === b[i]!.slot );
	if ( same( attack, settings.attack ) && same( buff, settings.buff ) ) return settings;
	return { ...settings, attack, buff };
}

/*
================
isPotionRow
================
*/
function isPotionRow( slot: number ): boolean {
	return Number.isInteger( slot ) && slot >= 0 && slot < MACRO_POTION_SLOTS;
}

/*
================
gearKindOf

The Weapon / Shield control id's kind, or null when the id is not one.
================
*/
function gearKindOf( id: string ): MacroGearKind | null {
	const prefix = MACRO_ID + "gear:";
	if ( !id.startsWith( prefix ) ) return null;
	const kind = id.slice( prefix.length );
	return kind === "weapon" || kind === "shield" ? kind : null;
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
	let skillPage: MacroSkillPage = "attack";
	let acquiredScroll = 0;
	let skillScroll = 0;
	let draft: MacroSettings | null = null;

	const close = () => {
		window = "none";
		draft = null;
	};
	const editDraft = ( change: ( settings: MacroSettings ) => MacroSettings ) => {
		if ( draft ) draft = change( draft );
	};
	const clampScroll = ( value: number, max: number ) => Math.min( Math.max( 0, max ), Math.max( 0, value ) );
	return {
		/*
================
state
================
		*/
		state(): MacroHudState {
			return { window, tab, potionPage, skillPage, acquiredScroll, skillScroll, draft };
		},
		/*
================
toggleMenu

The under-minimap CIFMacroSystemGuide button (0xab9080): opens the menu,
or closes whatever is open. False when the macro cannot open (row off or
no replay yet).
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
skill-page:<attack|buff>, check:<slot>, percent:<slot>:<+1|-1>,
item:<slot> (clears the row's item), gear:<weapon|shield> (clears the
Weapon / Shield bag bind), hunt-group:<byte>, hunt-check:<byte>,
skill-add:<id>, skill-remove:<attack|buff>:<index>,
acquired-scroll:<+1|-1>, skill-scroll:<+1|-1>, save, cancel, close.
================
		*/
		click(
			id: string,
			live: MacroSettings | undefined,
			acquired: readonly SkillMetadata[] = [],
			inventory: readonly { readonly slot: number; readonly refObjId: number; readonly typeFlags: number; }[] =
				[],
			skillIndex?: ReadonlyMap<number, SkillMetadata>
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
				if ( window !== "options" ) {
					draft = scrubMacroSkillLists(
						scrubMacroGear( scrubMacroPotion( live, inventory ), inventory ),
						skillIndex
					);
				}
				window = "options";
				tab = kind;
				return [];
			}
			if ( verb === "save" ) {
				const saved = draft ?
					scrubMacroSkillLists(
						scrubMacroGear( scrubMacroPotion( draft, inventory ), inventory ),
						skillIndex
					) :
					null;
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
			if ( verb === "skill-page" && (first === "attack" || first === "buff") ) {
				skillPage = first;
				skillScroll = 0;
				return [];
			}
			const slot = Number( first );
			if ( !isPotionRow( slot ) && (verb === "check" || verb === "percent" || verb === "item") ) return [];
			if ( verb === "check" ) {
				editDraft( settings => ({
					...settings,
					potion: withPotionRowOn(
						settings.potion,
						slot,
						!potionRowOn( settings.potion, slot ),
						MACRO_PERCENT_DEFAULT
					)
				}) );
			} else if ( verb === "percent" ) {
				editDraft( settings => {
					const current = potionSlot( settings.potion, slot ).a || MACRO_PERCENT_DEFAULT;
					const percent = Math.min(
						MACRO_PERCENT_MAX,
						Math.max( MACRO_PERCENT_MIN, current + Math.sign( Number( second ) ) * MACRO_PERCENT_STEP )
					);
					return { ...settings, potion: withPotionRowPercent( settings.potion, slot, percent ) };
				} );
			} else if ( verb === "item" ) {
				editDraft( settings => ({ ...settings, potion: withPotionRowItem( settings.potion, slot, 0 ) }) );
			} else if ( verb === "gear" && (first === "weapon" || first === "shield") ) {
				editDraft( settings => ({
					...settings,
					skillFlags: withMacroGearSlot( settings.skillFlags, first, 0 )
				}) );
			} else if ( verb === "hunt-group" ) {
				const group = MACRO_HUNT_GROUPS.find( g => g.byte === slot );
				if ( group ) {
					editDraft( settings => {
						const current = settings.hunt[slot] ?? 0;
						const at = group.choices.findIndex( choice => choice.value === current );
						const next = group.choices[(at < 0 ? 0 : at + 1) % group.choices.length]!;
						return { ...settings, hunt: withHuntByte( settings.hunt, slot, next.value ) };
					} );
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
					// Attack = target-required; Buff = self skills (Grass Walk).
					if ( !macroSkillFitsPage( skill, skillPage ) ) return settings;
					// Exact id, skilldata group, or replace group — one Grass Walk
					// rank must not sit twice, and not on both lists.
					if ( macroSkillBound( settings, skill, skillIndex ) ) return settings;
					if ( settings[skillPage].length >= MACRO_SKILL_GRID ) return settings;
					return {
						...settings,
						[skillPage]: renumber( [ ...settings[skillPage], { slot: 0, skill: slot } ] )
					};
				} );
			} else if ( verb === "skill-remove" && (first === "attack" || first === "buff") ) {
				const index = Number( second );
				editDraft( settings => ({
					...settings,
					[first]: renumber( settings[first].filter( ( _, i ) => i !== index ) )
				}) );
			} else if ( verb === "acquired-scroll" ) {
				acquiredScroll = clampScroll( acquiredScroll + Math.sign( slot ), Number.MAX_SAFE_INTEGER );
			} else if ( verb === "skill-scroll" ) {
				skillScroll = clampScroll( skillScroll + Math.sign( slot ), Number.MAX_SAFE_INTEGER );
			}
			return [];
		},
		/*
================
scroll

Wheel over an acquired or skills-to-use zone. The skill tab clamps the
first-row index when it draws, so an overshoot just sticks on the last
page.
================
		*/
		scroll( zone: "acquired" | "use", delta: number ) {
			if ( window !== "options" || tab !== MACRO_SKILL || !delta ) return false;
			if ( zone === "acquired" ) {
				acquiredScroll = Math.max( 0, acquiredScroll + Math.sign( delta ) );
			} else {
				skillScroll = Math.max( 0, skillScroll + Math.sign( delta ) );
			}
			return true;
		},
		/*
================
bindItem

A bag item dropped on a potion row's slot (sro-macro:item:<row>) binds
its RefItem ID. Native CNIFMacroAutoPotionSlot drop (0xdafff0) takes only
quickslot kind 0x46, then per-row TypeId checks (0x690b70..); weapons and
wrong-row potions return false and leave the row alone. Admission is
macroPotionAdmits.
================
		*/
		bindItem( id: string, item: { readonly refObjId: number; readonly typeFlags: number; } ): boolean {
			const prefix = MACRO_ID + "item:";
			if ( window !== "options" || tab !== MACRO_POTION || !id.startsWith( prefix ) ) return false;
			const slot = Number( id.slice( prefix.length ) );
			if ( !isPotionRow( slot ) || !macroPotionAdmits( slot, item.typeFlags ) ) return false;
			editDraft( settings => ({
				...settings,
				potion: withPotionRowItem( settings.potion, slot, item.refObjId )
			}) );
			return true;
		},
		/*
================
bindGear

A bag item dropped on Weapon or Shield (sro-macro:gear:<kind>) stores the
absolute bag slot (0x17b37de / 0x17b37df). Only bag slots in 0x0d..0x6f
with the matching CIFEquipment socket bind; other classes return false.
================
		*/
		bindGear(
			id: string,
			item: { readonly slot: number; readonly typeFlags: number; }
		): boolean {
			const kind = gearKindOf( id );
			if ( window !== "options" || tab !== MACRO_SKILL || !kind ) return false;
			if ( item.slot < 0x0d || item.slot >= 0x70 ) return false;
			if ( !macroGearAdmits( kind, item.typeFlags ) ) return false;
			editDraft( settings => ({
				...settings,
				skillFlags: withMacroGearSlot( settings.skillFlags, kind, item.slot )
			}) );
			return true;
		},
		/*
================
isPotionItemSlot

True for a potion row's item control (bind or clear).
================
		*/
		isPotionItemSlot( id: string ): boolean {
			const prefix = MACRO_ID + "item:";
			if ( window !== "options" || tab !== MACRO_POTION || !id.startsWith( prefix ) ) return false;
			return isPotionRow( Number( id.slice( prefix.length ) ) );
		},
		/*
================
isGearItemSlot

True for the Weapon or Shield control on the Auto-Skills tab.
================
		*/
		isGearItemSlot( id: string ): boolean {
			return window === "options" && tab === MACRO_SKILL && gearKindOf( id ) !== null;
		},
		/*
================
potionItemRef

The draft's RefItem ID on a potion row's item control, or 0.
================
		*/
		potionItemRef( id: string ): number {
			if ( !this.isPotionItemSlot( id ) || !draft ) return 0;
			const slot = Number( id.slice( (MACRO_ID + "item:").length ) );
			return potionSlot( draft.potion, slot ).item;
		},
		/*
================
gearBagSlot

The draft's absolute bag slot on Weapon or Shield, or 0.
================
		*/
		gearBagSlot( id: string ): number {
			const kind = gearKindOf( id );
			if ( !kind || !draft ) return 0;
			return macroGearBagSlot( draft.skillFlags, kind );
		},
		/*
================
clearItem

Drag-out of a bound potion row (CIFSlot release away from the cell).
True when the row was cleared.
================
		*/
		clearItem( id: string ): boolean {
			if ( !this.isPotionItemSlot( id ) || !draft ) return false;
			const slot = Number( id.slice( (MACRO_ID + "item:").length ) );
			if ( !potionSlot( draft.potion, slot ).item ) return false;
			editDraft( settings => ({ ...settings, potion: withPotionRowItem( settings.potion, slot, 0 ) }) );
			return true;
		},
		/*
================
clearGear

Drag-out of a bound Weapon / Shield slot. True when the bind cleared.
================
		*/
		clearGear( id: string ): boolean {
			const kind = gearKindOf( id );
			if ( !kind || !draft ) return false;
			if ( !macroGearBagSlot( draft.skillFlags, kind ) ) return false;
			editDraft( settings => ({
				...settings,
				skillFlags: withMacroGearSlot( settings.skillFlags, kind, 0 )
			}) );
			return true;
		},
		/*
================
transferItem

Move a bound RefItem between potion rows without a TypeId re-check (used
when the stack is gone from the bag so typeFlags cannot be recovered).
================
		*/
		transferItem( fromId: string, toId: string ): boolean {
			if ( !this.isPotionItemSlot( fromId ) || !this.isPotionItemSlot( toId ) || fromId === toId || !draft ) {
				return false;
			}
			const from = Number( fromId.slice( (MACRO_ID + "item:").length ) );
			const to = Number( toId.slice( (MACRO_ID + "item:").length ) );
			const ref = potionSlot( draft.potion, from ).item;
			if ( !ref ) return false;
			editDraft( settings => ({
				...settings,
				potion: withPotionRowItem( withPotionRowItem( settings.potion, from, 0 ), to, ref )
			}) );
			return true;
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
			skillPage = "attack";
			acquiredScroll = 0;
			skillScroll = 0;
		}
	};
}
