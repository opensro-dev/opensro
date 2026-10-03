/*
===========================================================================

skill-tooltip-catalog.ts - the published skilldata rows as tooltip views

Decodes skillData.json (buildSkillDataAsset.mjs) into the per-skill view the
skill pane and buff help read: the projected skilldata columns plus the
native parameter blocks that sub_84b2f0 installs into CSkillData.

The catalogue holds 27,835 rows and is decoded on the main thread when the
HUD admits it, so the decode is straight-line: each row is scanned once,
character by character, validating and parsing its scalar cells into a
reused Float64Array; only the four symbol cells become strings. The
previous split + regex + Number() form froze a frame for 250 ms.

===========================================================================
*/

import type { SkillTooltipRowView, TooltipSkillCatalog } from "./skill-tooltip-data";

// Native 84B2F0 parameter cursor and 7F9310 published column projection.
type SkillPaneParamDisposition =
	| "display"
	| "runtime"
	| "get-value"
	| "set-value"
	| "requirement"
	| "marker"
	| "terminal";

interface SkillPaneParamSpec {
	readonly arity: number;
	readonly offset: number | null;
	readonly disposition: SkillPaneParamDisposition;
}

type SkillPaneParamBlock = { offset: number; tag: number; values: number[]; };

type SkillPaneArea = SkillTooltipRowView["directTooltipParams"]["areas"][number];

// Published column order: the skilldata source column of each projected cell.
const SKILL_COLUMN_COUNT = 79;
// Projected cells 29..77 are the native 49-dword parameter tail [Param2, Param50].
const PARAM_TAIL_FIRST = 29;
const PARAM_TAIL_END = 78;
const MAX_SKILL_ROWS = 65536;
const MAX_SKILL_ROW_LENGTH = 16384;
// A decimal string of at most this many digits is always a safe integer.
const SAFE_DECIMAL_DIGITS = 15;
// CSkillData offsets the decoder projects (sub_84b2f0 destinations).
const SET_VALUE_FIRST_OFFSET = 0x15c;
const SET_VALUE_LAST_OFFSET = 0x16c;
const REQUIREMENT_FIRST_OFFSET = 0x174;
const EFFECT_RANGE_FIRST_OFFSET = 0x60;
const MAX_SET_VALUES = 5;
const MAX_REQUIREMENTS = 5;
// sub_84b2f0's `msch` block: the reference appearance type and cap.
const APPEARANCE_REFERENCE_OFFSET = 0x268;

/*
================
SkillPane_PublishedColumns

The skilldata source column behind each published cell, in order.
================
*/
function SkillPane_PublishedColumns(): number[] {
	const columns = [
		1,
		2,
		7,
		8,
		34,
		36,
		38,
		39,
		40,
		41,
		42,
		43,
		44,
		45,
		46,
		57,
		59,
		60,
		61,
		62,
		9,
		50,
		51,
		52,
		53,
		54,
		55,
		64,
		65
	];
	for ( let i = 0; i < 49; i++ ) columns.push( 69 + i );
	columns.push( 22 );
	return columns;
}

/*
================
SkillPane_TextCell

Cells 18, 19, 27 and 28 are symbols; every other cell is a decimal scalar.
================
*/
function SkillPane_TextCell( index: number ): boolean {
	return index === 18 || index === 19 || index === 27 || index === 28;
}

// One scanned row: scalar cells as numbers, symbol cells as strings.
interface SkillPaneRow {
	readonly numbers: Float64Array;
	readonly text: string[];
}

/*
================
SkillPane_ScanRow

Splits one tab-separated row into row.numbers / row.text and validates it.
A scalar cell must match /^-?\d+$/ and be a safe integer; it is parsed in
the same pass. Throws on any other shape.
================
*/
function SkillPane_ScanRow( line: unknown, row: SkillPaneRow ): void {
	if ( typeof line !== "string" || line.length > MAX_SKILL_ROW_LENGTH ) throw Error( "Invalid tooltip skill row" );
	let cell = 0, start = 0;
	for ( let end = 0; end <= line.length; end++ ) {
		if ( end < line.length && line.charCodeAt( end ) !== 9 ) continue;
		if ( cell >= SKILL_COLUMN_COUNT ) throw Error( "Invalid tooltip skill scalar" );
		if ( SkillPane_TextCell( cell ) ) row.text[cell] = line.slice( start, end );
		else row.numbers[cell] = SkillPane_ScanScalar( line, start, end );
		cell++;
		start = end + 1;
	}
	if ( cell !== SKILL_COLUMN_COUNT ) throw Error( "Invalid tooltip skill scalar" );
}

/*
================
SkillPane_ScanScalar

The decimal integer in line[start, end), or a throw. At most
SAFE_DECIMAL_DIGITS digits accumulate exactly below 2^53; a longer cell
goes through Number() and must still be a safe integer.
================
*/
function SkillPane_ScanScalar( line: string, start: number, end: number ): number {
	const negative = start < end && line.charCodeAt( start ) === 45, first = negative ? start + 1 : start;
	if ( first === end ) throw Error( "Invalid tooltip skill scalar" );
	let value = 0;
	for ( let i = first; i < end; i++ ) {
		const digit = line.charCodeAt( i ) - 48;
		if ( digit < 0 || digit > 9 ) throw Error( "Invalid tooltip skill scalar" );
		value = value * 10 + digit;
	}
	if ( end - first > SAFE_DECIMAL_DIGITS ) {
		value = Number( line.slice( first, end ) );
		if ( !Number.isSafeInteger( value ) ) throw Error( "Invalid tooltip skill scalar" );
	}
	return negative ? -value : value;
}

/*
================
SkillPane_CreateRow
================
*/
function SkillPane_CreateRow(): SkillPaneRow {
	return {
		numbers: new Float64Array( SKILL_COLUMN_COUNT ),
		text: new Array<string>( SKILL_COLUMN_COUNT ).fill( "" )
	};
}

/*
================
SkillPane_ParamTag

The four-character tag as the big-endian dword the native stream stores.
================
*/
function SkillPane_ParamTag( value: string ): number {
	let tag = 0;
	for ( let i = 0; i < value.length; i++ ) tag = ((tag << 8) | value.charCodeAt( i )) >>> 0;
	return tag;
}

/*
================
SkillPane_ParamSpecs

Native sub_84b2f0's authored-tag frontier. Keeping arity and destination
together is important: walking with indexOf made a numeric value which
happened to equal a tag steal ownership from its real enclosing block.
Runtime-only entries are retained so the cursor still advances exactly.
================
*/
function SkillPane_ParamSpecs(): ReadonlyMap<number, SkillPaneParamSpec> {
	const specs = new Map<number, SkillPaneParamSpec>();
	/*
	================
	spec
	================
	*/
	function spec(
		name: string,
		arity: number,
		offset: number | null,
		disposition: SkillPaneParamDisposition = "display"
	) {
		specs.set( SkillPane_ParamTag( name ), { arity, offset, disposition } );
	}
	spec( "att", 5, 0x004 );
	spec( "pdmg", 1, 0x008 );
	// sub_84b2f0 @0x84c5d4 installs one value at +0x0c; the complete
	// sub_7f9bd0 direct-tooltip body has no corresponding read.
	spec( "pdm2", 1, 0x00c, "runtime" );
	spec( "ko", 2, 0x010 );
	spec( "da", 1, 0x014 );
	spec( "cr", 2, 0x018 );
	// `gdr` is parsed to CSkillData+0x20 but sub_7f9bd0 has no read/string
	// branch for that pointer; the authored long description owns its UI.
	spec( "ck", 1, 0x01c );
	spec( "gdr", 1, 0x020, "runtime" );
	spec( "hr", 2, 0x024 );
	spec( "ru", 1, 0x028 );
	spec( "kb", 2, 0x02c );
	spec( "saps", 2, 0x030 );
	spec( "defp", 3, 0x034 );
	spec( "defr", 3, 0x038 );
	spec( "ar", 2, 0x03c );
	spec( "dar", 2, 0x040 );
	spec( "odar", 2, 0x044 );
	spec( "br", 2, 0x04c );
	spec( "er", 2, 0x050 );
	spec( "dura", 1, 0x054 );
	spec( "onff", 2, 0x058 );
	spec( "mc", 2, 0x05c );
	// efr chooses +0x60/+0x64/+0x68 from values[0]; resolved by the walker.
	spec( "efr", 6, null );
	spec( "eshp", 0, 0x06c, "marker" );
	spec( "stns", 3, 0x070 );
	spec( "esht", 2, 0x074 );
	spec( "fitp", 2, 0x078 );
	spec( "cnsm", 3, 0x07c, "runtime" );
	spec( "rhru", 2, 0x080 );
	spec( "dcmp", 1, 0x084 );
	spec( "hpi", 2, 0x088 );
	spec( "mpi", 2, 0x08c );
	spec( "pw", 4, 0x090 );
	spec( "hste", 1, 0x094 );
	spec( "hst2", 1, 0x098 );
	spec( "hst3", 1, 0x09c );
	spec( "irgc", 2, 0x0ac );
	spec( "apru", 2, 0x0b0 );
	spec( "apau", 2, 0x0b4 );
	spec( "terd", 1, 0x0b8 );
	spec( "thrd", 1, 0x0bc );
	spec( "tpdd", 1, 0x0c0 );
	spec( "tpad", 1, 0x0c4 );
	spec( "tele", 2, 0x0c8 );
	spec( "tel2", 2, 0x0cc );
	spec( "tel3", 2, 0x0d0, "runtime" );
	spec( "reat", 2, 0x0d4 );
	spec( "bgra", 2, 0x0d8 );
	spec( "real", 3, 0x0dc );
	spec( "pola", 2, 0x0e0 );
	spec( "summ", 5, 0x0e4 );
	spec( "fz", 2, 0x0e8 );
	spec( "fb", 2, 0x0ec );
	spec( "es", 3, 0x0f0 );
	spec( "bu", 3, 0x0f4 );
	spec( "ps", 3, 0x0f8 );
	spec( "zb", 2, 0x0fc );
	spec( "heal", 4, 0x100 );
	spec( "resu", 2, 0x104 );
	spec( "cura", 1, 0x108 );
	spec( "pmhp", 4, 0x10c );
	spec( "pmmp", 4, 0x110 );
	spec( "pmdp", 4, 0x114 );
	spec( "pmdg", 4, 0x118 );
	spec( "pao", 1, 0x124 );
	// sub_84b2f0 @0x84b906: cbuf consumes only its tag and stores the next
	// stream pointer at +0x12c, parallel to nbuf/bbuf.
	spec( "cbuf", 0, 0x12c, "marker" );
	spec( "nbuf", 0, 0x130, "marker" );
	spec( "bbuf", 0, 0x134, "marker" );
	spec( "pcdu", 1, 0x138 );
	spec( "chcr", 1, 0x13c );
	spec( "cmcr", 1, 0x140 );
	spec( "lnks", 4, 0x144 );
	spec( "lks2", 0, 0x148, "marker" );
	spec( "lkcp", 1, 0x14c );
	spec( "ovl2", 1, 0x150, "runtime" );
	spec( "scls", 1, 0x154, "runtime" );
	spec( "puls", 1, 0x158, "runtime" );
	spec( "reqc", 1, 0x170 );
	spec( "reqi", 2, 0x174, "requirement" );
	spec( "reqn", 0, 0x188, "marker" );
	spec( "atca", 2, 0x190, "runtime" );
	spec( "tant", 2, 0x194, "runtime" );
	spec( "tnt2", 2, 0x198 );
	spec( "dtnt", 2, 0x19c );
	spec( "dmgt", 2, 0x1a0 );
	spec( "dru", 2, 0x1a4 );
	spec( "dru2", 2, 0x1a8 );
	spec( "stri", 2, 0x1ac );
	// sub_84b2f0 @0x84bbec consumes tag + two values for +0x1b4. The direct
	// tooltip formatter does not read it; growth-potion prose is authored.
	spec( "spda", 2, 0x1b0 );
	spec( "expi", 2, 0x1b4, "runtime" );
	spec( "inti", 2, 0x1b8 );
	spec( "abir", 1, 0x1bc );
	spec( "tkss", 1, 0x1c0 );
	spec( "lfst", 1, 0x1c4 );
	spec( "tran", 3, 0x1c8 );
	spec( "curt", 2, 0x1cc );
	spec( "curl", 3, 0x1d0 );
	spec( "rcur", 1, 0x1d4, "runtime" );
	spec( "dmgr", 4, 0x1d8 );
	spec( "dgmp", 1, 0x1dc );
	// +0x1e0 is gameplay-only: the complete sub_7f9bd0 body has no read of
	// this pointer (TC crown-buff text comes from its authored description).
	spec( "hwir", 1, 0x1e0, "runtime" );
	spec( "abnb", 1, 0x1e4 );
	spec( "dtt", 2, 0x1e8 );
	spec( "dttp", 2, 0x1ec );
	spec( "hide", 3, 0x1f0 );
	spec( "dcri", 1, 0x1f4, "runtime" );
	spec( "se", 3, 0x1f8 );
	spec( "rt", 3, 0x1fc );
	spec( "sl", 3, 0x200 );
	spec( "fe", 3, 0x204 );
	spec( "my", 4, 0x208 );
	spec( "bl", 5, 0x20c );
	spec( "dn", 4, 0x210 );
	spec( "st", 3, 0x214 );
	spec( "ds", 4, 0x218 );
	spec( "ca", 3, 0x21c );
	spec( "cssr", 4, 0x220 );
	spec( "csit", 4, 0x224 );
	spec( "cspd", 4, 0x228 );
	spec( "csmd", 4, 0x22c );
	spec( "cshp", 6, 0x230 );
	spec( "csmp", 6, 0x234 );
	spec( "tb", 4, 0x238 );
	spec( "lkdr", 3, 0x23c );
	spec( "lkag", 2, 0x240 );
	spec( "lkdd", 1, 0x244 );
	spec( "thld", 1, 0x248 );
	spec( "tcmd", 2, 0x24c );
	spec( "skc", 3, 0x254, "runtime" );
	spec( "pchr", 1, 0x25c );
	// sub_84b2f0 @0x84c75e consumes tag + four values and stores +0x260;
	// sub_7f9bd0 never reads it, so it remains an explicit runtime family.
	spec( "qest", 4, 0x260, "runtime" );
	spec( "msch", 2, 0x268 );
	spec( "mcap", 2, 0x26c );
	spec( "rmut", 1, 0x270, "runtime" );
	spec( "alcu", 1, 0x278 );
	spec( "luck", 1, 0x27c );
	spec( "pwtt", 1, 0x280 );
	spec( "mwtt", 1, 0x284 );
	spec( "pwdt", 1, 0x288 );
	spec( "mwdt", 1, 0x28c );
	spec( "mwhh", 1, 0x290 );
	spec( "mwmh", 1, 0x294 );
	spec( "mwhs", 1, 0x298 );
	spec( "mstc", 1, 0x29c );
	spec( "mscc", 1, 0x2a0 );
	spec( "lkdh", 3, 0x2a4 );
	spec( "hitm", 0, 0x0a4, "marker" );
	spec( "hntp", 0, 0x250, "marker" );
	spec( "trap", 0, 0x264, "marker" );
	spec( "efta", 0, 0x274, "marker" );
	// Pointer markers: sub_84b2f0 consumes tag only, then stores the following
	// stream address (+0x48 @0x84b39d for `ao`, +0xa8 @0x84c844 for `rpkt`).
	spec( "ao", 0, 0x048, "marker" );
	spec( "rpkt", 0, 0x0a8, "marker" );
	// sub_84b2f0 @0x84c9a8: `ssou` breaks the parameter loop immediately;
	// following dwords are not tags and must never be independently decoded.
	spec( "ssou", 0, null, "terminal" );
	spec( "getv", 1, null, "get-value" );
	spec( "setv", 3, null, "set-value" );
	return specs;
}

/*
================
SkillPane_DecodeNativeParamBlocks

Walks the 49-dword parameter tail of one scanned row the way sub_84b2f0's
cursor does: a zero dword is skipped, an unrecognised token (or a block
that would run past the tail) advances one dword, `ssou` ends the walk.
================
*/
function SkillPane_DecodeNativeParamBlocks(
	cells: Float64Array,
	specs: ReadonlyMap<number, SkillPaneParamSpec>,
	efrTag: number
): SkillPaneParamBlock[] {
	const blocks: SkillPaneParamBlock[] = [], length = PARAM_TAIL_END - PARAM_TAIL_FIRST;
	let requirementSlot = 0, setValueSlot = 0;
	for ( let index = 0; index < length; ) {
		const tag = (cells[PARAM_TAIL_FIRST + index]! | 0) >>> 0;
		if ( tag === 0 ) {
			index += 1;
			continue;
		}
		const spec = specs.get( tag );
		if ( !spec || index + spec.arity >= length ) {
			// Native advances one dword on an unrecognized token.
			index += 1;
			continue;
		}
		if ( spec.disposition === "terminal" ) break;
		const values: number[] = [];
		for ( let i = 1; i <= spec.arity; i++ ) values.push( cells[PARAM_TAIL_FIRST + index + i]! | 0 );
		let offset = spec.offset;
		if ( tag === efrTag ) {
			const selector = values[0] ?? 0;
			offset = selector >= 1 && selector <= 3 ? EFFECT_RANGE_FIRST_OFFSET + (selector - 1) * 4 : null;
		} else if ( spec.disposition === "set-value" ) {
			offset = setValueSlot < MAX_SET_VALUES ? SET_VALUE_FIRST_OFFSET + setValueSlot++ * 4 : null;
		} else if ( spec.disposition === "requirement" ) {
			offset = requirementSlot < MAX_REQUIREMENTS ? REQUIREMENT_FIRST_OFFSET + requirementSlot++ * 4 : null;
		}
		if ( offset !== null ) blocks.push( { offset, tag, values } );
		index += 1 + spec.arity;
	}
	return blocks;
}

/*
================
SkillPane_ReadNativeBlock

The last block installed at a CSkillData offset: a later block overwrites
the field, as the native install does.
================
*/
function SkillPane_ReadNativeBlock( blocks: readonly SkillPaneParamBlock[], offset: number ): number[] | null {
	for ( let index = blocks.length - 1; index >= 0; index-- ) {
		if ( blocks[index]!.offset === offset ) return blocks[index]!.values;
	}
	return null;
}

/*
================
SkillPane_DecodeArea
================
*/
function SkillPane_DecodeArea( block: readonly number[] ): SkillPaneArea {
	return {
		gate: block[0] ?? 0,
		kind: block[1] ?? 0,
		radiusDeci: block[2] ?? 0,
		targetCount: block[3] ?? 0,
		pierceDecrease: block[4] ?? 0,
		reserved: block[5] ?? 0
	};
}

/*
================
SkillPane_Symbol

A symbol cell; the "xxx" placeholder decodes to "".
================
*/
function SkillPane_Symbol( cell: string ): string {
	const symbol = cell.trim();
	return symbol === "xxx" ? "" : symbol;
}

/*
================
SkillPane_DecodeSkillRow

One projected skilldata row (buildSkillDataAsset.mjs column order) -> the
pane view. The icon column is icon-root relative and the native parse
lowercases it (sub_811890 flag 1 @0x7f97e2), so the decode roots and
lowercases it the same way.
================
*/
function SkillPane_DecodeSkillRow(
	row: SkillPaneRow,
	specs: ReadonlyMap<number, SkillPaneParamSpec>,
	efrTag: number
): SkillTooltipRowView {
	const cols = row.numbers, text = row.text;
	// v8 appends target-required after the unchanged 49-dword native tail;
	// keep the decoder's [Param2, Param50] boundary half-open and exact.
	const nativeParamBlocks = SkillPane_DecodeNativeParamBlocks( cols, specs, efrTag );
	const attackBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x004 );
	const durationBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x054 );
	const multiBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x05c );
	const downAttackBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x014 );
	const criticalBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x018 );
	// sub_7f9bd0 reads tnt2/+0x198. `tant` decodes to the distinct runtime
	// slot +0x194 and must never manufacture a visible taunt row.
	const tauntBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x198 );
	const knockoutBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x010 );
	const rangeIncreaseBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x028 );
	const knockbackBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x02c );
	const defenseBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x034 );
	const recoveryBlock = SkillPane_ReadNativeBlock( nativeParamBlocks, 0x100 );
	const areas: SkillPaneArea[] = [];
	let area: SkillPaneArea | null = null;
	for ( let slot = 0; slot < 3; slot++ ) {
		const block = SkillPane_ReadNativeBlock( nativeParamBlocks, EFFECT_RANGE_FIRST_OFFSET + slot * 4 );
		if ( !block ) continue;
		const decoded = SkillPane_DecodeArea( block );
		areas.push( decoded );
		if ( !area && decoded.gate === 1 ) area = decoded;
	}
	const setValueBlocks: SkillPaneParamBlock[] = [];
	for ( const block of nativeParamBlocks ) {
		if ( block.offset >= SET_VALUE_FIRST_OFFSET && block.offset <= SET_VALUE_LAST_OFFSET ) {
			setValueBlocks.push( block );
		}
	}
	if ( setValueBlocks.length > 1 ) setValueBlocks.sort( ( left, right ) => left.offset - right.offset );
	const setValues: { code: number; value: number; }[] = [];
	for ( let i = 0; i < setValueBlocks.length && i < MAX_SET_VALUES; i++ ) {
		const block = setValueBlocks[i]!.values;
		setValues.push( { code: block[0] ?? 0, value: block[1] ?? 0 } );
	}
	return {
		id: cols[0]! >>> 0,
		groupId: cols[1]! >>> 0,
		basicLevel: cols[2]! >>> 0,
		basicActivity: cols[3]! >>> 0,
		masteryId: cols[4]! >>> 0,
		reqMasteryLevel: cols[5]! >>> 0,
		reqStr: cols[6]! >>> 0,
		reqInt: cols[7]! >>> 0,
		reqGroups: [
			{ groupId: cols[8]! >>> 0, level: cols[11]! >>> 0 },
			{ groupId: cols[9]! >>> 0, level: cols[12]! >>> 0 },
			{ groupId: cols[10]! >>> 0, level: cols[13]! >>> 0 }
		],
		reqLearnSp: cols[14]! >>> 0,
		nameSymbol: text[19]!.trim(),
		chainNextSkillId: cols[20]! >>> 0,
		requiredWeaponKinds: [ cols[21]! >>> 0, cols[22]! >>> 0 ],
		requiredHp: cols[23]! | 0,
		requiredMp: cols[24]! | 0,
		requiredHpRatio: cols[25]! & 0xffff,
		requiredMpRatio: cols[26]! & 0xffff,
		tooltipDescriptionSymbol: SkillPane_Symbol( text[27]! ),
		studySymbol: SkillPane_Symbol( text[28]! ),
		directTooltipParams: {
			nativeParamBlocks,
			durationMs: durationBlock ? durationBlock[0]! >>> 0 : null,
			// sub_84b2f0's `mc` block is kind 2; the V1.150 server-side table
			// scan independently pins the same [tag, 2, count] shape.
			multiCount: multiBlock?.[0] === 2 && (multiBlock[1] ?? 0) > 0 ? multiBlock[1]! >>> 0 : null,
			downAttackRatio: downAttackBlock ? downAttackBlock[0]! | 0 : null,
			criticalFlat: criticalBlock?.[0] ?? 0,
			criticalRatio: criticalBlock?.[1] ?? 0,
			tauntFlat: tauntBlock?.[0] ?? 0,
			tauntRatio: tauntBlock?.[1] ?? 0,
			knockout: knockoutBlock ? { level: knockoutBlock[0] ?? 0, chance: knockoutBlock[1] ?? 0 } : null,
			rangeIncreaseDeci: rangeIncreaseBlock ? rangeIncreaseBlock[0] ?? 0 : null,
			knockback: knockbackBlock ? { chance: knockbackBlock[0] ?? 0, distance: knockbackBlock[1] ?? 0 } : null,
			defense: defenseBlock ?
				{ physical: defenseBlock[0] ?? 0, magical: defenseBlock[1] ?? 0, applyLimit: defenseBlock[2] ?? 0 } :
				null,
			recovery: recoveryBlock ?
				{
					hpFlat: recoveryBlock[0] ?? 0,
					hpRatio: recoveryBlock[1] ?? 0,
					mpFlat: recoveryBlock[2] ?? 0,
					mpRatio: recoveryBlock[3] ?? 0
				} :
				null,
			setValues,
			area,
			areas
		},
		attack: {
			present: attackBlock !== null,
			flags: (attackBlock?.[0] ?? 0) >>> 0,
			percent: attackBlock?.[1] ?? 0,
			minimum: attackBlock?.[2] ?? 0,
			maximum: attackBlock?.[3] ?? 0,
			value5: attackBlock?.[4] ?? 0
		}
	};
}

/*
================
SkillPane_ValidRows

The admitted rows of a skilldata catalogue, or a throw: the format, the
published column order and the row bound are part of the contract.
================
*/
function SkillPane_ValidRows( value: unknown ): readonly unknown[] {
	const raw = value as { format?: unknown; columns?: unknown; rows?: unknown; };
	if ( raw?.format !== "sro-skilldata" || !Array.isArray( raw.rows ) || raw.rows.length > MAX_SKILL_ROWS ) {
		throw Error( "Invalid tooltip skill catalogue" );
	}
	const columns = SkillPane_PublishedColumns();
	if ( !Array.isArray( raw.columns ) || raw.columns.length !== columns.length ) {
		throw Error( "Invalid tooltip skill catalogue" );
	}
	for ( let i = 0; i < columns.length; i++ ) {
		if ( raw.columns[i] !== columns[i] ) throw Error( "Invalid tooltip skill catalogue" );
	}
	return raw.rows;
}

/*
================
createTooltipSkillDecoder

An incremental decode of the catalogue. Each step(rows) decodes at most
that many rows and returns the finished catalogue once every row is in,
null before. The HUD steps it once per frame so 27,835 rows never land in
one frame; every admission check still runs before the catalogue exists.
================
*/
export function createTooltipSkillDecoder( value: unknown ): { step( rows: number ): TooltipSkillCatalog | null; } {
	const rows = SkillPane_ValidRows( value ), specs = SkillPane_ParamSpecs(), efrTag = SkillPane_ParamTag( "efr" );
	const result = new Map<number, SkillTooltipRowView>(), scanned = SkillPane_CreateRow();
	let next = 0, done: TooltipSkillCatalog | null = null;
	return {
		/*
		================
		step
		================
		*/
		step( count ) {
			if ( done ) return done;
			const end = Math.min( rows.length, next + Math.max( 1, count ) );
			for ( ; next < end; next++ ) {
				SkillPane_ScanRow( rows[next], scanned );
				const row = SkillPane_DecodeSkillRow( scanned, specs, efrTag );
				if ( !row.id || result.has( row.id ) ) throw Error( "Duplicate tooltip skill" );
				result.set( row.id, row );
			}
			if ( next < rows.length ) return null;
			done = SkillPane_FinishCatalog( result );
			return done;
		}
	};
}

/*
================
SkillPane_FinishCatalog

Checks chain references and builds `groups`: for each group:basicLevel,
the first row that is no other row's chain successor.
================
*/
function SkillPane_FinishCatalog( result: Map<number, SkillTooltipRowView> ): TooltipSkillCatalog {
	const children = new Set<number>();
	for ( const row of result.values() ) {
		if ( row.chainNextSkillId && !result.has( row.chainNextSkillId ) ) {
			throw Error( "Missing tooltip chain reference" );
		}
		children.add( row.chainNextSkillId );
	}
	const groups = new Map<string, SkillTooltipRowView>();
	for ( const row of result.values() ) {
		const key = row.groupId + ":" + row.basicLevel;
		if ( !children.has( row.id ) && !groups.has( key ) ) groups.set( key, row );
	}
	return Object.assign( result, { groups } );
}

/*
================
decodeTooltipSkills

The whole catalogue at once, keyed by skill id, plus `groups`.
================
*/
export function decodeTooltipSkills( value: unknown ): TooltipSkillCatalog {
	return createTooltipSkillDecoder( value ).step( MAX_SKILL_ROWS )!;
}

/*
================
tooltipAppearanceReferences

Per skill id, the last `msch` block (CSkillData+0x268): the reference
appearance type and its cap. The asset build (buildSkillDataAsset.mjs) runs
this once and publishes the result in characterActionData.json, so the
character owner never parses the catalogue; the build and the HUD share this
one parameter walk. The catalogue is admitted by the same checks as
decodeTooltipSkills: scalar cells, unique ids and resolvable chain references.
================
*/
export function tooltipAppearanceReferences( value: unknown ): Map<number, { type: number; cap: number; }> {
	const rows = SkillPane_ValidRows( value ), specs = SkillPane_ParamSpecs(), efrTag = SkillPane_ParamTag( "efr" );
	const references = new Map<number, { type: number; cap: number; }>(),
		ids = new Set<number>(),
		chains: number[] = [];
	const scanned = SkillPane_CreateRow();
	for ( const line of rows ) {
		SkillPane_ScanRow( line, scanned );
		const id = scanned.numbers[0]! >>> 0;
		if ( !id || ids.has( id ) ) throw Error( "Duplicate tooltip skill" );
		ids.add( id );
		chains.push( scanned.numbers[20]! >>> 0 );
		const block = SkillPane_ReadNativeBlock(
			SkillPane_DecodeNativeParamBlocks( scanned.numbers, specs, efrTag ),
			APPEARANCE_REFERENCE_OFFSET
		);
		if ( block ) references.set( id, { type: block[0]!, cap: block[1]! } );
	}
	for ( const chain of chains ) {
		if ( chain && !ids.has( chain ) ) throw Error( "Missing tooltip chain reference" );
	}
	return references;
}
