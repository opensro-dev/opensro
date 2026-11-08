/*
===========================================================================

quest-info.ts - the level-up quest notice: which quests a level opened

Pure model of the native CIFQuestInfo chain (issue #2310, claims checked
in SRO_Client.exe assembly): on a level gain the 0x30D2 handler calls
CIFQuestInfo_Str_QuestInfoGuide (69CCA0) with 1, which walks the quest
table (0x7e5fa0) collecting the quests the new level opened. This file
owns the walk's filter, the icon's placement and blink, and the window's
size rule; the HUD owner (runtime/ui/hud/quest-info.ts) owns the state,
and ui.ts only draws what this model decides.

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
// 62D260 sizes the window SetSize(0x13B, 0x8E) for one row and
// SetSize(0x13B, 0x154) for the list; the width never changes.
export const QUEST_INFO_WINDOW_WIDTH = 0x13b;
export const QUEST_INFO_WINDOW_HEIGHT_ONE = 0x8e;
export const QUEST_INFO_WINDOW_HEIGHT_LIST = 0x154;

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

One row takes the short window; two or more take the list window. Zero
rows never opens a window (the icon section is destroyed instead).
================
*/
export function questInfoWindowHeight( count: number ): number {
	return count <= 1 ? QUEST_INFO_WINDOW_HEIGHT_ONE : QUEST_INFO_WINDOW_HEIGHT_LIST;
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
