/*
===========================================================================

quest-info.ts - the level-up quest notice: which quests a level opened

Pure model of the native CIFQuestInfo chain (issue #2310, claims checked
in SRO_Client.exe assembly): on a level gain the 0x30D2 handler calls
CIFQuestInfo_Str_QuestInfoGuide (69CCA0) with 1, which walks the quest
table (0x7e5fa0) collecting the quests the new level opened. This file
owns the walk's filter, the icon's placement and blink, and the window's
size, row and divider rules; the HUD owner (runtime/ui/hud/quest-info.ts)
owns the state, and ui.ts only draws what this model decides.

===========================================================================
*/

// A quest the gained level opened, with its window row already resolved
// through the text manager (62DE20 inserts record+0x24/+0x78/+0x94).
export interface QuestInfoEntry {
	readonly id: number;
	readonly title: string;
	readonly notify: string;
	readonly condition: string;
}

// The quest table as the walk reads it: the quest-presentation records
// (level +0x20, title +0x24, notify +0x78, condition +0x94, country
// +0xb0). Extra record fields are ignored.
export interface QuestInfoRecord {
	readonly level: number;
	readonly title: string;
	readonly notify: string;
	readonly condition: string;
	readonly country: number;
}

// questcontentsdata country byte meaning "both countries" (0x7e6047).
export const QUEST_INFO_COUNTRY_BOTH = 3;
// Past this level byte the walk skips the country check entirely
// (0x7e603a ja): a level-41+ player is shown every country's quests.
export const QUEST_INFO_COUNTRY_CHECK_MAX_LEVEL = 0x28;
// The walk's out-list capacity (69CCA0 passes threshold 0x28).
export const QUEST_INFO_MAX_ENTRIES = 0x28;
// CIFQuestInfoGuide child 0x2E: x = screen width - 0x57, y = 0xB3 (69CCA0).
export const QUEST_INFO_ICON_X_INSET = 0x57;
export const QUEST_INFO_ICON_Y = 0xB3;
// qstinfoic_1/_2 animated sprite interval (62E090): 0x1F4 ms per frame.
export const QUEST_INFO_BLINK_MS = 0x1f4;
// 62D260 sizes the window by row count: SetSize(0x13B, 0x8E) for one
// row, (0x13B, 0xF1) for two and (0x13B, 0x154) for three or more; the
// width never changes.
export const QUEST_INFO_WINDOW_WIDTH = 0x13b;
export const QUEST_INFO_WINDOW_HEIGHT_ONE = 0x8e;
export const QUEST_INFO_WINDOW_HEIGHT_TWO = 0xf1;
export const QUEST_INFO_WINDOW_HEIGHT_LIST = 0x154;
// The scroll manager shows at most three rows (6F35F0 is passed 1, 2
// or 3); more scroll.
export const QUEST_INFO_VISIBLE_ROWS_MAX = 3;
// Row pitch (6F36C0): 0x60 for the single row, 0x63 once there are
// two or more, the 96-pixel slot plus a 3-pixel divider.
export const QUEST_INFO_ROW_PITCH_ONE = 0x60;
export const QUEST_INFO_ROW_PITCH = 0x63;
// The dividers 62D260 creates between rows: one tile control (runtime
// class 0xCE92B0) per row after the first, rect (0x28, 0x85 + i * 0x63,
// 0xEB, 3) in the window, its texture set to
// interface/ifcommon/bg_tile/com_bg_tile_a.ddj.
export const QUEST_INFO_DIVIDER_X = 0x28;
export const QUEST_INFO_DIVIDER_Y = 0x85;
export const QUEST_INFO_DIVIDER_WIDTH = 0xeb;
export const QUEST_INFO_DIVIDER_HEIGHT = 3;
export const QUEST_INFO_DIVIDER_TEXTURE = "interface/ifcommon/bg_tile/com_bg_tile_a.png";

/*
================
questInfoEntries

The 0x7e5fa0 walk: a quest qualifies when its level byte equals the
gained level, its country byte is 3 (both) or the player's country byte
(the check skipped past level 40), and the quest is not already held
(0x7e6070-0x7e6080). At most QUEST_INFO_MAX_ENTRIES rows are collected.
Iteration follows the record map's key order, as the native by-id walk.
================
*/
export function questInfoEntries(
	records: Readonly<Record<number, QuestInfoRecord>>,
	level: number,
	country: number,
	held: ReadonlySet<number>
): readonly QuestInfoEntry[] {
	const entries: QuestInfoEntry[] = [];
	for ( const key of Object.keys( records ) ) {
		const id = Number( key ), record = records[id]!;
		if ( record.level !== level || held.has( id ) ) continue;
		if (
			level <= QUEST_INFO_COUNTRY_CHECK_MAX_LEVEL && record.country !== QUEST_INFO_COUNTRY_BOTH &&
			record.country !== country
		) continue;
		entries.push( { id, title: record.title, notify: record.notify, condition: record.condition } );
		if ( entries.length >= QUEST_INFO_MAX_ENTRIES ) break;
	}
	return entries;
}

/*
================
questInfoWindowHeight

62D260's three sizes: one row, two rows, and three or more (the list,
which scrolls). Zero rows never opens a window (the icon section is
destroyed instead).
================
*/
export function questInfoWindowHeight( count: number ): number {
	if ( count <= 1 ) return QUEST_INFO_WINDOW_HEIGHT_ONE;
	return count === 2 ? QUEST_INFO_WINDOW_HEIGHT_TWO : QUEST_INFO_WINDOW_HEIGHT_LIST;
}

/*
================
questInfoRows

The rows the window shows and their pitch: up to three, 0x60 apart
for one row and 0x63 (slot plus divider) for more.
================
*/
export function questInfoRows( count: number ): { visible: number; pitch: number; } {
	const visible = Math.min( Math.max( count, 1 ), QUEST_INFO_VISIBLE_ROWS_MAX );
	return { visible, pitch: visible === 1 ? QUEST_INFO_ROW_PITCH_ONE : QUEST_INFO_ROW_PITCH };
}

/*
================
questInfoDividers

The divider rects, relative to the window: one under each shown row
but the last (62D260 creates one for two rows, two for the list).
================
*/
export function questInfoDividers( count: number ): readonly (readonly [number, number, number, number])[] {
	const { visible } = questInfoRows( count );
	return Array.from( { length: visible - 1 }, ( _, i ) =>
		[
			QUEST_INFO_DIVIDER_X,
			QUEST_INFO_DIVIDER_Y + i * QUEST_INFO_ROW_PITCH,
			QUEST_INFO_DIVIDER_WIDTH,
			QUEST_INFO_DIVIDER_HEIGHT
		] as const );
}

/*
================
questInfoIconFrame

The two blink frames swap every QUEST_INFO_BLINK_MS (62E090).
================
*/
export function questInfoIconFrame( nowMs: number ): 0 | 1 {
	return Math.floor( nowMs / QUEST_INFO_BLINK_MS ) % 2 === 0 ? 0 : 1;
}
