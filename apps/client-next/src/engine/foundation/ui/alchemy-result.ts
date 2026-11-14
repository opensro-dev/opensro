/*
===========================================================================

alchemy-result.ts - what the alchemy window shows when a reinforcement ends

CIFAlchemyReinforce's result arm (62B0B0, from the 0xB373 and 0xB651
answers) swaps its effect sprite to the success or failure atlas, plays it
once and writes the outcome to the chat. The worker records the outcome
(AlchemyOutcome); the window draws and words it from here.

A successful magic or attribute stone is worded by what it changed, not by
the native enhancement-level line (stoneResultLines): port-only, not
native; owner-approved deviation (2026-10-13). Beside that wording the
native per-attribute lines print as well (stoneNativeLines, 625980):
owner decision #2377, Option 1.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { AlchemyOutcome } from "@/engine/contracts/item-process";
import { itemTooltipMagic, resistSymbol } from "./item-tooltip-magic";
import { itemMaxDurability, itemTooltipStats } from "./item-tooltip-stats";

// 62B0B0's flag bits: 0x10 success, 0x20 failure (with 1 the enhancement
// level fell and 2 the durability fell), 0x40 the item was destroyed, 0x80
// a stone's roll failed (0xB651 [2, 0x23]; the stone is spent).
export const ALCHEMY_SUCCESS = 0x10;
export const ALCHEMY_FAILURE = 0x20;
export const ALCHEMY_DESTROYED = 0x40;
export const ALCHEMY_STONE_FAILED = 0x80;
const FAILURE_LEVEL = 0x01;
const FAILURE_DURABILITY = 0x02;
const FAILURE_DETAIL = 0x0f;
// CIFAlchemyReinforce_OnCreate (625600): the effect is 64 px cells, sixteen
// of them (a 4x4 atlas), drawn into control 50; its state timer 0xA steps
// one cell every 50 ms and hides the sprite once it wraps (62C350).
export const ALCHEMY_EFFECT_CELL = 64;
export const ALCHEMY_EFFECT_FRAMES = 16;
export const ALCHEMY_EFFECT_FRAME_MS = 50;
const ALCHEMY_EFFECT_COLUMNS = 4;

/*
================
alchemyEffectTexture

The atlas 62B0B0 loads: success for 0x10, the failure atlas otherwise.
================
*/
export function alchemyEffectTexture( flags: number ): string {
	return flags & ALCHEMY_SUCCESS ? "interface/alchemy/alcm_effect_success" : "interface/alchemy/alcm_effect_fail_1";
}

/*
================
alchemyEffectAdditive

Whether the atlas is drawn additively. alcm_effect_success is opaque art
(DXT3, alpha 255 in every texel: white light on black), so blended it paints
a black square over the medallion (#3441); added, its black adds nothing.
alcm_effect_fail_1 carries its own alpha and blends as authored.
INFERENCE: the original can only show the opaque atlas by adding it to what
is behind, as the port already draws the gacha's opaque ga_effect_* sheets.
================
*/
export function alchemyEffectAdditive( flags: number ): boolean {
	return (flags & ALCHEMY_SUCCESS) !== 0;
}

/*
================
alchemyEffectCell

The atlas cell shown elapsedMs after the result, as UV [u, v, w, h], or
null once the single play has ended.
================
*/
export function alchemyEffectCell( elapsedMs: number ): [number, number, number, number] | null {
	if ( !(elapsedMs >= 0) ) return null;
	const frame = Math.floor( elapsedMs / ALCHEMY_EFFECT_FRAME_MS );
	if ( frame >= ALCHEMY_EFFECT_FRAMES ) return null;
	const size = 1 / ALCHEMY_EFFECT_COLUMNS;
	return [ (frame % ALCHEMY_EFFECT_COLUMNS) * size, Math.floor( frame / ALCHEMY_EFFECT_COLUMNS ) * size, size, size ];
}

/*
================
alchemyResultLines

62B0B0's chat lines, in its order. Success names the new enhancement
level. A failure without detail says it failed; a fallen level names the
new level (or says it is gone) and the drop; a fallen durability names the
new value and the drop. A destroyed item says so, and a stone whose roll
failed says the alchemy failed (its 0x80 arm).
INFERENCE: native reports the item's maximum durability; the port's item
rows carry the current durability, which a failed reinforcement lowers
with it, so the drop is read from that.
================
*/
export function alchemyResultLines( outcome: AlchemyOutcome, copy: ( key: string ) => string ): string[] {
	const format = ( key: string, ...values: number[] ) => {
		let text = copy( key );
		for ( const value of values ) text = text.replace( "%d", String( value ) );
		return text;
	};
	const { flags } = outcome;
	if ( flags & ALCHEMY_SUCCESS ) {
		if ( !outcome.stone ) return [ format( "UIIT_MSG_REINFORCERR_SUCCESS", outcome.plus ) ];
		// The deviation's wording first (it stands where 62B0B0's
		// enhancement line stands), then 625980's own lines (#2377).
		const lines = [
			...stoneResultLines( outcome.stone.before, outcome.stone.after, copy ),
			...stoneNativeLines( outcome.stone, copy )
		];
		return lines.length ? lines : [ format( "UIIT_MSG_REINFORCERR_SUCCESS", outcome.plus ) ];
	}
	if ( flags & ALCHEMY_DESTROYED ) return [ copy( "UIIT_MSG_REINFORCERR_BREAKDOWN" ) ];
	if ( flags & ALCHEMY_STONE_FAILED ) return [ copy( "UIIT_MSG_REINFORCERR_FAIL" ) ];
	if ( !(flags & ALCHEMY_FAILURE) ) return [];
	if ( !(flags & FAILURE_DETAIL) ) return [ copy( "UIIT_MSG_REINFORCERR_FAIL" ) ];
	const lines: string[] = [];
	if ( flags & FAILURE_LEVEL ) {
		lines.push(
			outcome.plus === 0 ?
				copy( "UIIT_MSG_REINFORCERR_FAIL_RESULT_OPTLV_ZERO" ) :
				format(
					"UIIT_MSG_REINFORCERR_FAIL_RESULT_OPTLV_DOWN",
					outcome.plus,
					outcome.previousPlus - outcome.plus
				)
		);
	}
	if ( flags & FAILURE_DURABILITY ) {
		lines.push(
			format(
				"UIIT_MSG_REINFORCERR_FAILDOWN_DURABILITY",
				outcome.durability,
				outcome.previousDurability - outcome.durability
			)
		);
	}
	return lines;
}

/*
================
stoneResultLines

Port-only, not native; owner-approved deviation (2026-10-13). Natively
62B0B0 answers every successful reinforcement with the enhancement-level
line, which for a stone names the item's plus rather than what the stone
did. A magic or attribute stone instead names each stat it changed, from
the old value to the new, in the item tooltip's own words: the white stats
(55B540's variance rows) and the magic options are compared separately,
as both lists can name the same stat. A stat the item did not have before
changed from 0.
================
*/
export function stoneResultLines(
	before: InventoryItem,
	after: InventoryItem,
	copy: ( key: string ) => string
): string[] {
	// A tooltip row is the stat's name, then its value: the value starts at
	// the first number or bracket ("Str 3 Increase (+50%)", "Luck(2 times)").
	const statRow = /^(.*?)\s*((?:[+-]?\d|\().*)$/;
	const lines: string[] = [];
	for ( const rows of [ itemTooltipStats, itemTooltipMagic ] ) {
		const previous = new Map<string, string>();
		for ( const row of rows( before, copy ) ) {
			const match = statRow.exec( row.value );
			if ( match ) previous.set( match[1]!, match[2]! );
		}
		for ( const row of rows( after, copy ) ) {
			const match = statRow.exec( row.value );
			if ( !match ) continue;
			const old = previous.get( match[1]! ) ?? "0";
			if ( old !== match[2] ) lines.push( `${match[1]} changed from ${old} to ${match[2]}` );
		}
	}
	return lines;
}

// ============================================================================

/*
================
nativeWhiteRows

625980's white-stat arms in their native order: the tooltip PARAM_* key
whose row carries the stat, the shape its values print in (the arm's own
swprintf format), the NATTR_* name of the stone that designates the
change, and - for durability, hit and evade rating - the MATTR_* twin
whose own arm takes the report over. Durability has no stone: its arm
only ever assimilates. Traced arm by arm in the client database
(625980-627F00); the two specialize labels each serve a range arm
(weapons) and a single-value arm (armour), split here by shape.
================
*/
const nativeWhiteRows = [
	{ key: "PARAM_DUR", shape: "int", designated: "NATTR_DUR", twin: "MATTR_DUR" },
	{ key: "PARAM_PA", shape: "range", designated: "NATTR_PA", twin: "" },
	{ key: "PARAM_PHYSICAL_SPECIALIZE", shape: "specRange", designated: "NATTR_PASTR", twin: "" },
	{ key: "PARAM_MA", shape: "range", designated: "NATTR_MA", twin: "" },
	{ key: "PARAM_MAGICAL_SPECIALIZE", shape: "specRange", designated: "NATTR_MAINT", twin: "" },
	{ key: "PARAM_CRITICAL", shape: "int", designated: "NATTR_CRITICAL", twin: "" },
	{ key: "PARAM_HR", shape: "int", designated: "NATTR_HR", twin: "MATTR_HR" },
	{ key: "PARAM_PD", shape: "float", designated: "NATTR_PD", twin: "" },
	{ key: "PARAM_PHYSICAL_SPECIALIZE", shape: "specSingle", designated: "NATTR_PDSTR", twin: "" },
	{ key: "PARAM_MD", shape: "float", designated: "NATTR_MD", twin: "" },
	{ key: "PARAM_MAGICAL_SPECIALIZE", shape: "specSingle", designated: "NATTR_MDINT", twin: "" },
	{ key: "PARAM_ER", shape: "int", designated: "NATTR_ER", twin: "MATTR_ER" },
	{ key: "PARAM_BLOCKING", shape: "int", designated: "NATTR_BR", twin: "" },
	{ key: "PARAM_PR", shape: "float", designated: "NATTR_PAR", twin: "" },
	{ key: "PARAM_MR", shape: "float", designated: "NATTR_MAR", twin: "" }
] as const;

/*
================
nativeOptionKinds

625980's magic-option arms in their native order (the assimilation
pass at 6280F8 and the append/change pass at 6295DB walk the kinds in
this same order).
================
*/
const nativeOptionKinds = [
	"MATTR_STR",
	"MATTR_INT",
	"MATTR_DUR",
	"MATTR_HR",
	"MATTR_EVADE_BLOCK",
	"MATTR_EVADE_CRITICAL",
	"MATTR_ER",
	"MATTR_HP",
	"MATTR_MP",
	"MATTR_RESIST_FROSTBITE",
	"MATTR_RESIST_ESHOCK",
	"MATTR_RESIST_BURN",
	"MATTR_RESIST_POISON",
	"MATTR_RESIST_ZOMBIE",
	"MATTR_ATHANASIA",
	"MATTR_SOLID",
	"MATTR_LUCK",
	"MATTR_ASTRAL",
	"MATTR_REPAIR"
] as const;

/*
================
nativeTemplate

Fill a native result template's %s slots in order, as the arms'
swprintf fills them.
================
*/
function nativeTemplate(
	copy: ( key: string ) => string,
	key: string,
	parts: readonly string[]
): string {
	let text = copy( key );
	for ( const part of parts ) text = text.replace( "%s", part );
	return text;
}

/*
================
nativeWhiteZero

A stat the item does not carry reads as its shape's zero (625980 reads
the stat record whether the tooltip lists the row or not).
================
*/
function nativeWhiteZero( shape: string ): string {
	if ( shape === "range" ) return "0 ~ 0";
	if ( shape === "specRange" ) return "0.0 % ~ 0.0 %";
	if ( shape === "specSingle" || shape === "float" ) return "0.0";
	return "0";
}

/*
================
nativeWhiteShape

Classify a tooltip stat value (its (+N%) grade stripped) into the shape
its 625980 arm prints, with the value as the arm renders it: a single
specialize loses its percent sign (the arm prints %.1f bare), the rest
keep the tooltip's own digits.
================
*/
function nativeWhiteShape( text: string ): { readonly shape: string; readonly value: string; } | undefined {
	if ( /^\d+ ~ \d+$/.test( text ) ) return { shape: "range", value: text };
	if ( /^[\d.]+ % ~ [\d.]+ %$/.test( text ) ) return { shape: "specRange", value: text };
	if ( /^[\d.]+ %$/.test( text ) ) return { shape: "specSingle", value: text.slice( 0, -2 ) };
	if ( /^\d+$/.test( text ) ) return { shape: "int", value: text };
	if ( /^[\d.]+$/.test( text ) ) return { shape: "float", value: text };
	return undefined;
}

/*
================
nativeWhiteTexts

One item's white stats as 625980 compares them, keyed "PARAM|shape":
the tooltip rows carry the native values (their grade is not part of
the value); durability is the plotted maximum the first arm reads.
================
*/
function nativeWhiteTexts( item: InventoryItem, copy: ( key: string ) => string ): Map<string, string> {
	const texts = new Map<string, string>();
	const keys = [ ...new Set( nativeWhiteRows.map( row => row.key ) ) ]
		.map( key => ({ key, label: copy( key ) }) )
		.filter( entry => entry.label )
		.sort( ( a, b ) => b.label.length - a.label.length );
	for ( const row of itemTooltipStats( item, copy ) ) {
		for ( const { key, label } of keys ) {
			if ( !row.value.startsWith( `${label} ` ) ) continue;
			const found = nativeWhiteShape( row.value.slice( label.length + 1 ).replace( / \(\+\d+%\)$/, "" ) );
			if ( found ) texts.set( `${key}|${found.shape}`, found.value );
			break;
		}
	}
	texts.set( "PARAM_DUR|int", String( itemMaxDurability( item ) ) );
	return texts;
}

/*
================
nativeBracket

The change text inside a line's [%s]: ranges print in parentheses,
everything else bare (the arms' "%d -> %d" / "%.1f -> %.1f" shapes).
================
*/
function nativeBracket( shape: string, oldText: string, newText: string ): string {
	return shape === "range" || shape === "specRange" ?
		`(${oldText}) -> (${newText})` :
		`${oldText} -> ${newText}`;
}

/*
================
nativeOptionAmounts

One item's magic options by option name, with their magnitudes (the
packed high dword, as the tooltip reads it).
================
*/
function nativeOptionAmounts( item: InventoryItem ): Map<string, number> {
	const amounts = new Map<string, number>();
	for ( const encoded of item.magic ) {
		const bits = BigInt( encoded );
		const reference = (item.magicReferences ?? []).find( entry => entry.paramId === Number( bits & 65535n ) );
		if ( reference && !amounts.has( reference.optionName ) ) {
			amounts.set( reference.optionName, Number( bits >> 32n ) );
		}
	}
	return amounts;
}

/*
================
nativeOptionParamKey

An option's PARAM_* label key: MATTR_* names its PARAM_* twin, except
the block evade, whose label is the ignore-blocking key (628653).
================
*/
function nativeOptionParamKey( optionName: string ): string {
	if ( optionName === "MATTR_EVADE_BLOCK" ) return "PARAM_IGNORE_BLOCKING";
	return `PARAM_${optionName.slice( 6 )}`;
}

/*
================
nativeResistLabel

A disease option's composite label (the "%s,%s%s %s" the resist arms
build: the disease pair, the hour word, the reduce word), as the
tooltip (55B540) words the same option.
================
*/
function nativeResistLabel( optionName: string, copy: ( key: string ) => string ): string {
	const symbol = resistSymbol( optionName.slice( 13 ) );
	if ( !symbol ) return "";
	return `${copy( symbol )}${optionName === "MATTR_RESIST_FROSTBITE" ? `,${copy( "PARAM_FB" )}` : ""}${
		copy( "PARAM_HOUR" )
	} ${copy( "PARAM_DECREASE" )}`;
}

/*
================
nativeBlueLabel

The [%s] a blue line names: the bare PARAM_* text for a change
(62972F); for an assimilation the "%s %s" composite with the increase
word (6283D5) - except the evade and count options, whose arms name
the bare label, and the disease options, which name their composite.
================
*/
function nativeBlueLabel(
	optionName: string,
	copy: ( key: string ) => string,
	assimilation: boolean
): string {
	if ( optionName.startsWith( "MATTR_RESIST_" ) ) return nativeResistLabel( optionName, copy );
	const label = copy( nativeOptionParamKey( optionName ) );
	if (
		assimilation &&
		[ "MATTR_STR", "MATTR_INT", "MATTR_DUR", "MATTR_HR", "MATTR_ER", "MATTR_HP", "MATTR_MP" ]
			.includes( optionName )
	) return `${label} ${copy( "PARAM_INCREASE" )}`;
	return label;
}

/*
================
nativeBlueBracket

A blue change's bracket text: durability and hit rating print their
percents spaced ("%d %% -> %d %%", 6283EE), evade rating tight
("%d%% -> %d%%", 62A133), the disease options tighter still
("%d %%-> %d %%", 62A5AB); the rest print plain numbers.
================
*/
function nativeBlueBracket( optionName: string, oldAmount: number, newAmount: number ): string {
	if ( optionName === "MATTR_DUR" || optionName === "MATTR_HR" ) return `${oldAmount} % -> ${newAmount} %`;
	if ( optionName === "MATTR_ER" ) return `${oldAmount}% -> ${newAmount}%`;
	if ( optionName.startsWith( "MATTR_RESIST_" ) ) return `${oldAmount}%-> ${newAmount}%`;
	return `${oldAmount} -> ${newAmount}`;
}

/*
================
nativeBlueGrant

The [%s] a newly granted option is announced with (62964D and kin):
the label, the granted amount and the increase word - percents ride
the amount, the evade options drop the word. The count options name
the bare label (62A91B and kin: the arm swprintf's the PARAM_* text
with a plain "%s" into APPEND_ATTR; the amount only gates whether
the line prints, and the presenter never reads UIIT_STT_COUNT - the
count composites are the tooltip's wording, not the presenter's).
Repair speaks only when the granted count exceeds one (62AC3F).
================
*/
function nativeBlueGrant(
	optionName: string,
	amount: number,
	copy: ( key: string ) => string
): string {
	if ( optionName.startsWith( "MATTR_RESIST_" ) ) {
		const symbol = resistSymbol( optionName.slice( 13 ) );
		if ( !symbol ) return "";
		return `${copy( symbol )}${optionName === "MATTR_RESIST_FROSTBITE" ? `,${copy( "PARAM_FB" )}` : ""}${
			copy( "PARAM_HOUR" )
		} ${amount}% ${copy( "PARAM_DECREASE" )}`;
	}
	const label = copy( nativeOptionParamKey( optionName ) );
	if ( [ "MATTR_DUR", "MATTR_HR", "MATTR_ER" ].includes( optionName ) ) {
		return `${label} ${amount}% ${copy( "PARAM_INCREASE" )}`;
	}
	if ( optionName === "MATTR_EVADE_BLOCK" || optionName === "MATTR_EVADE_CRITICAL" ) {
		return `${label} ${amount}`;
	}
	if ( [ "MATTR_ATHANASIA", "MATTR_SOLID", "MATTR_LUCK", "MATTR_ASTRAL" ].includes( optionName ) ) {
		return label;
	}
	if ( optionName === "MATTR_REPAIR" ) return amount > 1 ? label : "";
	return `${label} ${amount} ${copy( "PARAM_INCREASE" )}`;
}

/*
================
stoneNativeLines

625980's own per-attribute lines for a successful stone (owner
decision #2377, Option 1), printed after the deviation's wording.
Every white stat or magic option that changed prints one line, and
the designated attribute prints even on an equal reroll: the change
the stone designates reads UIIT_MSG_ALCHEMY_CHANGE_CATTR, a newly
granted option UIIT_MSG_ALCHEMY_APPEND_ATTR, and any other
UIIT_MSG_ALCHEMY_ATTR_ASSIMILATION. The presented before/after items
carry the values, as the saved copy (CEA528) and the created item
carry them natively; a fortress weapon's attack row carries a fort
label the arms do not name, so only the deviation reports it.
================
*/
function stoneNativeLines( stone: NonNullable<AlchemyOutcome["stone"]>, copy: ( key: string ) => string ): string[] {
	const { before, after } = stone;
	const item = after.name ?? "";
	const lines: string[] = [];
	const whiteBefore = nativeWhiteTexts( before, copy );
	const whiteAfter = nativeWhiteTexts( after, copy );
	const blueBefore = nativeOptionAmounts( before );
	const blueAfter = nativeOptionAmounts( after );
	for ( const row of nativeWhiteRows ) {
		const oldText = whiteBefore.get( `${row.key}|${row.shape}` ) ?? nativeWhiteZero( row.shape );
		const newText = whiteAfter.get( `${row.key}|${row.shape}` ) ?? nativeWhiteZero( row.shape );
		const label = copy( row.key ) || row.key;
		if ( stone.designated === row.designated ) {
			// The designated arm formats its line without comparing the
			// values first (625A68, 625D2F): it prints even when the
			// reroll lands where it started.
			lines.push(
				nativeTemplate( copy, "UIIT_MSG_ALCHEMY_CHANGE_CATTR", [
					label,
					item,
					nativeBracket( row.shape, oldText, newText )
				] )
			);
			continue;
		}
		if ( oldText === newText ) continue;
		// The durability, hit and evade arms stay silent when the stone
		// designates their magic-option twin and the created item
		// carries it: the twin's own arm owns the report (625B46).
		if ( row.twin && stone.designated === row.twin && blueAfter.has( row.twin ) ) continue;
		lines.push(
			nativeTemplate( copy, "UIIT_MSG_ALCHEMY_ATTR_ASSIMILATION", [
				label,
				item,
				nativeBracket( row.shape, oldText, newText )
			] )
		);
	}
	// The blue assimilation pass precedes the designated option's own
	// append/change pass, as in the native walk.
	for ( const kind of nativeOptionKinds ) {
		if ( kind === stone.designated ) continue;
		const oldAmount = blueBefore.get( kind ), newAmount = blueAfter.get( kind );
		if ( oldAmount === undefined || newAmount === undefined || oldAmount === newAmount ) continue;
		lines.push(
			nativeTemplate( copy, "UIIT_MSG_ALCHEMY_ATTR_ASSIMILATION", [
				nativeBlueLabel( kind, copy, true ),
				item,
				nativeBlueBracket( kind, oldAmount, newAmount )
			] )
		);
	}
	for ( const kind of nativeOptionKinds ) {
		const newAmount = blueAfter.get( kind ), oldAmount = blueBefore.get( kind );
		if ( newAmount === undefined ) continue;
		if ( oldAmount === undefined ) {
			// A newly granted option is the stone's own kind (6295DB);
			// with no stone staged, the grant still names it.
			if ( kind !== stone.designated && stone.designated ) continue;
			const granted = nativeBlueGrant( kind, newAmount, copy );
			if ( granted ) {
				lines.push( nativeTemplate( copy, "UIIT_MSG_ALCHEMY_APPEND_ATTR", [ granted, item ] ) );
			}
		} else if ( kind === stone.designated ) {
			// The designated arm formats "%d -> %d" whenever the kind
			// stands on both items (62970B, entered from 6295DB): an
			// equal reroll still prints. Its only amount gate is a
			// zero amount (62970B), which a carried option never has.
			// Repair's arm (62AC3F) is gated harder: the old count
			// must exceed one, and both counts print decremented.
			if ( kind === "MATTR_REPAIR" && oldAmount <= 1 ) continue;
			const bracket = kind === "MATTR_REPAIR" ?
				nativeBlueBracket( kind, oldAmount - 1, newAmount - 1 ) :
				nativeBlueBracket( kind, oldAmount, newAmount );
			lines.push(
				nativeTemplate( copy, "UIIT_MSG_ALCHEMY_CHANGE_CATTR", [
					nativeBlueLabel( kind, copy, false ),
					item,
					bracket
				] )
			);
		}
	}
	return lines;
}
