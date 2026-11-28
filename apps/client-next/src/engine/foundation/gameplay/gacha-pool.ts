/*
===========================================================================

gacha-pool.ts - the v1.696 Magic POP prize pool and category tree

The server publishes `gachaPool` in the world references only while its
v1.696 Magic POP pool is active (SRO_MAGIC_POP_V1696). Absent, the window
is the v1.150 twelve-prize window (gacha-catalog.ts) and nothing here runs.

The rules follow the v1.696 client (research/findings/
magic-pop-v1696-client.md): CIFGhachaSelectWnd's three combo boxes over
refgachatreeforclientuidata.txt (0x7D43C0, boxes 0xA75740 / 0xA74F70 /
0xA75440, Start 0xA75A30), CIFGhaCha's grid rebuild (0xA71220) with its
level-cap, type and degree filters, and the page spinner (0xA715FC).
The wire stays v1.150 (owner decision): a roll still names one entry id.

This module owns validation and arithmetic only. The window's state is in
runtime/ui/hud/gacha-hud.ts.

===========================================================================
*/

// The grid shows twelve prizes a page (ifghacha.txt slots 100..111).
export const GACHA_POOL_PAGE_SLOTS = 12;
// CIFGachaFreeParam (0x5EFD30) keeps at most five tokens a row.
const MAX_OPTIONS = 5;
// Bounds the published tables (the server's maxPublicReferenceRows).
const MAX_ENTRIES = 65536;
const MAX_TREE_ROWS = 4096;
const MAX_LABEL = 128;
const MAX_U32 = 0xffffffff;
// 0xA75740 compares the label's first fourteen characters.
const RACE_PREFIX_LENGTH = 14;
const CHINESE_PREFIX = "UIIT_CTL_WK_CH";
const EUROPEAN_PREFIX = "UIIT_CTL_WK_EU";
// The v1.150 item type word: TID1 bits 2..4, TID2 bits 5..6, TID3 bits 7..10,
// TID4 bits 11..15 (gacha-window.ts isGachaVoucher reads the same fields).
const TID2_EQUIPMENT = 1;
const CLASSES_PER_DEGREE = 3;

/*
================
GachaPoolOption

One <M:name,a,b> token of a prize's bracket string: a blue magic-option line
with value b. paramId is the server's resolution of the four-letter code to
a magicoption row (the code is server-only on the port); a token without it
draws no line.
================
*/
export interface GachaPoolOption {
	readonly name: string;
	readonly value: number;
	readonly paramId?: number;
}

/*
================
GachaPoolEntry

One prize of the NPC's display set (gachaitemset.txt row): the entry id the
roll names, the reward, its quantity, column 16's minimum level cap and the
bracket string's <O:n> plus and <M:...> options.
================
*/
export interface GachaPoolEntry {
	readonly entry: number;
	readonly refObjId: number;
	readonly quantity: number;
	readonly minLevelCap: number;
	readonly plus: number;
	readonly options: readonly GachaPoolOption[];
}

/*
================
GachaTreeRow

One refgachatreeforclientuidata.txt row: the type the leaf filters on
(TID1..TID4), its group and subgroup with their UIIT labels, and column 10,
the number of degrees the third box offers (0 = none).
================
*/
export interface GachaTreeRow {
	readonly tid: readonly [number, number, number, number];
	readonly group: number;
	readonly groupLabel: string;
	readonly sub: number;
	readonly subLabel: string;
	readonly degrees: number;
}

/*
================
GachaPool
================
*/
export interface GachaPool {
	readonly npc: number;
	readonly visibleSet: number;
	readonly levelCap: number;
	readonly entries: readonly GachaPoolEntry[];
	readonly tree: readonly GachaTreeRow[];
}

/*
================
decodeGachaPool

The references' gachaPool: undefined is the v1.150 window. Anything else
must be the whole published shape; a malformed table throws rather than
offer prizes the server never authorized.
================
*/
export function decodeGachaPool( value: unknown ): GachaPool | undefined {
	if ( value === undefined ) return undefined;
	const record = ( v: unknown ): v is Record<string, unknown> =>
		typeof v === "object" && v !== null && !Array.isArray( v );
	const only = ( v: Record<string, unknown>, keys: readonly string[], what: string ) => {
		if ( Object.keys( v ).some( key => !keys.includes( key ) ) ) throw Error( "Invalid Magic POP " + what );
	};
	const int = ( v: unknown, min: number, max: number, what: string ) => {
		if ( !Number.isInteger( v ) || (v as number) < min || (v as number) > max ) {
			throw Error( "Invalid Magic POP " + what );
		}
		return v as number;
	};
	const label = ( v: unknown ) => {
		if ( typeof v !== "string" || v.length === 0 || v.length > MAX_LABEL ) {
			throw Error( "Invalid Magic POP tree label" );
		}
		return v;
	};
	if ( !record( value ) ) throw Error( "Invalid Magic POP pool" );
	only( value, [ "npc", "visibleSet", "levelCap", "entries", "tree" ], "pool" );
	if ( !Array.isArray( value.entries ) || value.entries.length > MAX_ENTRIES ) {
		throw Error( "Invalid Magic POP entries" );
	}
	if ( !Array.isArray( value.tree ) || value.tree.length > MAX_TREE_ROWS ) throw Error( "Invalid Magic POP tree" );
	const seen = new Set<number>();
	const entries = value.entries.map( ( raw ): GachaPoolEntry => {
		if ( !record( raw ) ) throw Error( "Invalid Magic POP entry" );
		only( raw, [ "entry", "refObjId", "quantity", "minLevelCap", "plus", "options" ], "entry" );
		const entry = int( raw.entry, 1, MAX_U32, "entry id" );
		if ( seen.has( entry ) ) throw Error( "Magic POP entry repeats " + entry );
		seen.add( entry );
		const options = raw.options ?? [];
		if ( !Array.isArray( options ) || options.length > MAX_OPTIONS ) throw Error( "Invalid Magic POP options" );
		return {
			entry,
			refObjId: int( raw.refObjId, 1, MAX_U32, "reward" ),
			quantity: int( raw.quantity, 1, MAX_U32, "quantity" ),
			minLevelCap: int( raw.minLevelCap ?? 1, 0, 255, "level cap" ),
			plus: int( raw.plus ?? 0, 0, 255, "plus" ),
			options: options.map( ( option ): GachaPoolOption => {
				if ( !record( option ) ) throw Error( "Invalid Magic POP option" );
				only( option, [ "name", "value", "paramId" ], "option" );
				if ( typeof option.name !== "string" || !/^[a-z0-9]{1,4}$/i.test( option.name ) ) {
					throw Error( "Invalid Magic POP option name" );
				}
				return {
					name: option.name,
					value: int( option.value, 0, MAX_U32, "option value" ),
					...(option.paramId === undefined ?
						{} :
						{ paramId: int( option.paramId, 1, 0xffff, "option param" ) })
				};
			} )
		};
	} );
	const tree = value.tree.map( ( raw ): GachaTreeRow => {
		if ( !record( raw ) ) throw Error( "Invalid Magic POP tree row" );
		only( raw, [ "tid", "group", "groupLabel", "sub", "subLabel", "degrees" ], "tree row" );
		if ( !Array.isArray( raw.tid ) || raw.tid.length !== 4 ) throw Error( "Invalid Magic POP tree type" );
		const tid = raw.tid.map( n => int( n, 0, 255, "tree type" ) ) as [number, number, number, number];
		return {
			tid,
			group: int( raw.group, 0, MAX_U32, "tree group" ),
			groupLabel: label( raw.groupLabel ),
			sub: int( raw.sub, 0, MAX_U32, "tree subgroup" ),
			subLabel: label( raw.subLabel ),
			degrees: int( raw.degrees, 0, 255, "tree degrees" )
		};
	} );
	return {
		npc: int( value.npc, 1, MAX_U32, "machine" ),
		visibleSet: int( value.visibleSet, 0, 255, "display set" ),
		levelCap: int( value.levelCap, 1, 255, "level cap" ),
		entries,
		tree
	};
}

/*
================
GachaTreeSub / GachaTreeGroup

0x7D43C0's tree: groups in an ordered map by group id, each with an
ordered sub-map by subgroup id. A node keeps the type word of the row that
created it.
================
*/
export interface GachaTreeSub {
	readonly id: number;
	readonly label: string;
	readonly tid: readonly [number, number, number, number];
	readonly degrees: number;
}
export interface GachaTreeGroup {
	readonly id: number;
	readonly label: string;
	readonly tid: readonly [number, number, number, number];
	readonly subs: readonly GachaTreeSub[];
}

/*
================
gachaTreeGroups

INFERENCE: a repeated group or subgroup id keeps its first row, as an
ordered-map insert does; the shipped table repeats no (group, subgroup).
================
*/
export function gachaTreeGroups( tree: readonly GachaTreeRow[] ): GachaTreeGroup[] {
	const groups = new Map<
		number,
		{ id: number; label: string; tid: GachaTreeRow["tid"]; subs: Map<number, GachaTreeSub>; }
	>();
	for ( const row of tree ) {
		let group = groups.get( row.group );
		if ( !group ) {
			group = { id: row.group, label: row.groupLabel, tid: row.tid, subs: new Map() };
			groups.set( row.group, group );
		}
		if ( !group.subs.has( row.sub ) ) {
			group.subs.set( row.sub, { id: row.sub, label: row.subLabel, tid: row.tid, degrees: row.degrees } );
		}
	}
	return [ ...groups.values() ].sort( ( a, b ) => a.id - b.id ).map( group => ({
		id: group.id,
		label: group.label,
		tid: group.tid,
		subs: [ ...group.subs.values() ].sort( ( a, b ) => a.id - b.id )
	}) );
}

/*
================
gachaLabelHidden

0xA75740 / 0xA74F70: a label whose first fourteen characters are
UIIT_CTL_WK_EU is skipped when the country byte is 0, UIIT_CTL_WK_CH when it
is 1. INFERENCE: the byte (0x17B4F58+0x10C, default 3) is the character's
country, 0 Chinese and 1 European; any other value hides nothing.
================
*/
export function gachaLabelHidden( label: string, country: number | undefined ): boolean {
	const prefix = label.slice( 0, RACE_PREFIX_LENGTH );
	return country === 0 && prefix === EUROPEAN_PREFIX || country === 1 && prefix === CHINESE_PREFIX;
}

/*
================
gachaVisibleGroups / gachaVisibleSubs

Box 1 lists the groups and box 2 the chosen group's subgroups, each without
the other country's labels.
================
*/
export function gachaVisibleGroups( groups: readonly GachaTreeGroup[], country: number | undefined ): GachaTreeGroup[] {
	return groups.filter( group => !gachaLabelHidden( group.label, country ) );
}
export function gachaVisibleSubs( group: GachaTreeGroup | undefined, country: number | undefined ): GachaTreeSub[] {
	return (group?.subs ?? []).filter( sub => !gachaLabelHidden( sub.label, country ) );
}

/*
================
GachaSelectChoice

The three combo boxes' indexes (ids 41..43). Index 0 of boxes 1 and 2 is
UIIT_CTL_WARENETWORK_SCAN_SELECT ("Choose"); box 3's index is the degree,
0 for "Choose" or the single "None" entry. INFERENCE: box 1 also opens on a
"Choose" entry; Start (0xA75A30) needs both boxes at 1 or above, which only
a placeholder at 0 explains.
================
*/
export interface GachaSelectChoice {
	readonly group: number;
	readonly sub: number;
	readonly degree: number;
}

/*
================
GachaSelectBoxes

What the select window offers for a choice: box 1's groups and box 2's
subgroups (each after the "Choose" entry), box 3's degree count (the leaf's
column 10), and the filter Start hands SetFilter, null while Start refuses.
================
*/
export interface GachaSelectBoxes {
	readonly groups: readonly GachaTreeGroup[];
	readonly subs: readonly GachaTreeSub[];
	readonly degrees: number;
	readonly filter: GachaFilter | null;
}

/*
================
gachaSelectBoxes

0xA75A30 filters on the leaf's type word and, when the leaf offers degrees,
box 3's index; a leaf with column 10 = 0 filters on any degree. The native
filter also falls back to the group's own type word for a group with no
sub-map, but Start needs box 2 at 1 or above, so that branch cannot be
reached from the window.
================
*/
export function gachaSelectBoxes(
	groups: readonly GachaTreeGroup[],
	country: number | undefined,
	choice: GachaSelectChoice
): GachaSelectBoxes {
	const visible = gachaVisibleGroups( groups, country );
	const group = choice.group >= 1 ? visible[choice.group - 1] : undefined;
	const subs = gachaVisibleSubs( group, country );
	const leaf = group && choice.sub >= 1 ? subs[choice.sub - 1] : undefined;
	const degrees = leaf?.degrees ?? 0;
	return {
		groups: visible,
		subs,
		degrees,
		filter: leaf ? { tid: leaf.tid, degree: degrees ? Math.min( choice.degree, degrees ) : 0 } : null
	};
}

/*
================
gachaTypeIds

The four type ids of a v1.150 item type word.
================
*/
export function gachaTypeIds( typeFlags: number ): [number, number, number, number] {
	return [ typeFlags >>> 2 & 7, typeFlags >>> 5 & 3, typeFlags >>> 7 & 15, typeFlags >>> 11 & 31 ];
}

/*
================
gachaItemDegree

0xA714DA..0xA71526: equipment (TID2 = 1) reads (ItemClass - 1) / 3 + 1;
anything else reads the raw class byte. INFERENCE: ref +0x1A8 is ItemClass.
================
*/
export function gachaItemDegree( typeFlags: number, itemClass: number | undefined ): number {
	const itemClassValue = itemClass ?? 0;
	if ( gachaTypeIds( typeFlags )[1] !== TID2_EQUIPMENT ) return itemClassValue & 0xff;
	return itemClassValue > 0 ? Math.trunc( (itemClassValue - 1) / CLASSES_PER_DEGREE ) + 1 : 0;
}

/*
================
GachaFilter

What Start hands CIFGhaCha::SetFilter (0xA73A60): the leaf's type and the
third box's degree, 0 for any degree.
================
*/
export interface GachaFilter {
	readonly tid: readonly [number, number, number, number];
	readonly degree: number;
}

/*
================
GachaFilterable

A published prize row with what the filter reads: its reference's type
word and item class, and column 16.
================
*/
export interface GachaFilterable {
	readonly entry: number;
	readonly typeFlags?: number;
	readonly itemClass?: number;
	readonly minLevelCap?: number;
}

/*
================
gachaPrizeVisible

0xA71220 per element: the level cap byte must reach column 16 (prizes above
the server's cap stay hidden), the item's four type ids must equal the
filter's, and a chosen degree must match. A prize whose reference has not
arrived is not shown.
================
*/
export function gachaPrizeVisible( row: GachaFilterable, filter: GachaFilter, levelCap: number ): boolean {
	if ( levelCap < (row.minLevelCap ?? 1) || row.typeFlags === undefined ) return false;
	const tid = gachaTypeIds( row.typeFlags );
	if ( tid.some( ( value, index ) => value !== filter.tid[index] ) ) return false;
	return filter.degree === 0 || gachaItemDegree( row.typeFlags, row.itemClass ) === filter.degree;
}

/*
================
gachaFilterPrizes

The survivors, in list order (appended to +0x8E0).
================
*/
export function gachaFilterPrizes<T extends GachaFilterable>(
	rows: readonly T[],
	filter: GachaFilter,
	levelCap: number
): T[] {
	return rows.filter( row => gachaPrizeVisible( row, filter, levelCap ) );
}

/*
================
gachaLastPage

0xA715FC..0xA7168F: n / 12 when n is not a multiple of twelve, else n / 12 - 1
(0 below twelve, and 0 for an empty list).
================
*/
export function gachaLastPage( count: number ): number {
	const pages = Math.floor( count / GACHA_POOL_PAGE_SLOTS );
	return count % GACHA_POOL_PAGE_SLOTS ? pages : Math.max( 0, pages - 1 );
}

/*
================
gachaPageRows

0xA716A2..0xA716F5: slot i shows vec[page * 12 + i].
================
*/
export function gachaPageRows<T>( rows: readonly T[], page: number ): T[] {
	const first = Math.max( 0, page ) * GACHA_POOL_PAGE_SLOTS;
	return rows.slice( first, first + GACHA_POOL_PAGE_SLOTS );
}

/*
================
gachaOptionMagic

A prize option as a presented item's encoded magic value: the param id in
the low word, the value in the high dword (inventory-item.ts reads the
same layout off the wire). Options without a resolved param id give none.
================
*/
export function gachaOptionMagic( options: readonly GachaPoolOption[] ): string[] {
	return options.flatMap( option =>
		option.paramId === undefined ? [] : [ (BigInt( option.value ) << 32n | BigInt( option.paramId )).toString() ]
	);
}
