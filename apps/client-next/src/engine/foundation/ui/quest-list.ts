/*
===========================================================================

quest-list.ts - the native quest list window's rows and tracker rules

Pure model of the later client's quest list window (issue #4245): one
quest row, then one objective row per content the quest carries, is the
flat list the CNIFQuestSlot rows show; the HUD mini tracker carries at
most three tracked quests, each with a foldable title row and one line
per objective. The layout runtime (runtime/ui/res-layout) only renders
what this model computes. Data comes from the quest presentation
catalogue (records + text) and the session's QuestRecord list, never
from the fork's exported data.

===========================================================================
*/

import type { QuestRecord } from "@/engine/contracts/gameplay";
import { questObjectivePresentation } from "@/engine/foundation/ui/quest-presentation";

// The later client's tracker keeps at most three tracked quests; the
// quest list then recommends no more track state.
export const QUEST_TRACKED_MAX = 3;

// The mini tracker's start line: the layout authoring places the
// CNIFMiniQuestWnd root at y=267, and the plate starts at the screen's
// right edge. The quest list layouts author coordinates for a
// 1024-wide virtual canvas (the questminilist root is 324 wide at
// x=702); here the tracker starts at the right edge of the actual
// screen, y=267, then steps around the windows it would overlap.
export const QUEST_TRACKER_Y = 267;

// Free-standing text lines the tracker draws under a slot's title:
// one line per objective, at this line pitch (native screen point).
export const QUEST_TRACKER_OBJECTIVE_LINE_HEIGHT = 16;

/*
================
QuestListMeta

A quest's catalogue facts the window reads: its level and title, as
decodeQuestPresentation resolves them.
================
*/
export interface QuestListMeta {
	readonly level: number;
	readonly title: string;
}

/*
================
QuestObjectiveRow

One objective line: the resolved description, the status copy and the
objective colour (quest-presentation's rule). Used by the list's
objective rows and the tracker's objective lines alike.
================
*/
export interface QuestObjectiveRow {
	readonly description: string;
	readonly statusKey: string;
	readonly color: readonly [number, number, number, number];
}

/*
================
QuestListRow

One visible list row: a quest header (content < 0) or one of its
objectives. Quest rows carry the track state and the catalogue facts;
objective rows carry the resolved line.
================
*/
export interface QuestListRow {
	readonly quest: QuestRecord;
	readonly content: number;
	readonly objective?: QuestObjectiveRow;
}

/*
================
questListRows

The window's flat row list: each quest first, then one row per content
its record carries, in catalogue order. The list window shows no fold
of its own; the tracker folds instead.
================
*/
export function questListRows(
	quests: readonly QuestRecord[],
	entries: Readonly<Record<string, string>>
): readonly QuestListRow[] {
	const rows: QuestListRow[] = [];
	for ( const quest of quests ) {
		rows.push( { quest, content: -1 } );
		quest.contents.forEach( ( content, index ) => {
			rows.push( {
				quest,
				content: index,
				objective: questObjectivePresentation( content, entries )
			} );
		} );
	}
	return rows;
}

/*
================
questProgressCounts

The quest row's progress pair: the record's packed u08 keeps the
finished objectives in its low nibble and the objective count in the
high nibble, each clamped at 15.
================
*/
export function questProgressCounts( quest: QuestRecord ): readonly [number, number] {
	return [ quest.u08 & 15, Math.min( 15, quest.u08 >>> 4 ) ];
}

/*
================
questListTitle

The quest row's title: "Lv. <level> <title> (<done>/<total>)".
UIIT_STT_LEVEL_LV is the system copy for the "Lv." it abbreviates; a
catalogue without the key drops the level part.
================
*/
export function questListTitle(
	meta: QuestListMeta | undefined,
	quest: QuestRecord,
	copy: ( key: string ) => string
): string {
	const levelCopy = copy( "UIIT_STT_LEVEL_LV" );
	const [done, total] = questProgressCounts( quest );
	const count = ` (${done}/${total})`;
	if ( !meta ) return count.trim() ? count.slice( 1 ) : "";
	const prefix = levelCopy ? `Lv. ${meta.level} ` : "";
	return prefix + meta.title + count;
}

/*
================
questContentFace

The content view button's face for this quest.
CIFQuestList_SetQuestContentFace (research isro1696 sro_client.exe
0x00B4A820; the mini list's twin CIFQuestMiniList_SetQuestContentFace
0x00B4CB10) takes the player's level (byte +0x9BC of the player at
0x17D6160) less the quest's (byte +0x1C): ten or more picks the blue
face (a quest far below the player), zero or more the plain face, and
a quest above the player the red face.
================
*/
export function questContentFace( questLevel: number, playerLevel: number ): "blue" | "normal" | "red" {
	const diff = playerLevel - questLevel;
	return diff >= 10 ? "blue" : diff >= 0 ? "normal" : "red";
}

/*
================
questToggleTracked

Toggles one quest's track state. The tracker keeps at most
QUEST_TRACKED_MAX quests and never reorders: removing and re-adding a
quest appends it. The result names the refusal when the tracker is
full, so the caller can answer it (UIIT_CTL_QUEST_HELPER_FULL).
================
*/
export function questToggleTracked(
	tracked: readonly number[],
	refId: number
): { readonly tracked: readonly number[]; readonly status: "added" | "removed" | "full"; } {
	if ( tracked.includes( refId ) ) {
		return { tracked: tracked.filter( id => id !== refId ), status: "removed" };
	}
	if ( tracked.length >= QUEST_TRACKED_MAX ) return { tracked, status: "full" };
	return { tracked: [ ...tracked, refId ], status: "added" };
}

/*
================
QuestTrackerLine

One objective line of the tracker: "[<status>] <description>",
coloured by the objective's own rule.
================
*/
export interface QuestTrackerLine {
	readonly text: string;
	readonly color: readonly [number, number, number, number];
}

/*
================
QuestTrackerEntry

One tracked quest as the tracker draws it: its title (with the
party/guild prefix the quests' own relation byte asks for), its
objective lines, and whether the player folded it to its title row.
================
*/
export interface QuestTrackerEntry {
	readonly refId: number;
	readonly title: string;
	readonly folded: boolean;
	readonly lines: readonly QuestTrackerLine[];
}

/*
================
questTrackerEntries

The tracked quests in track order. The title keeps the relation
prefix the quest window uses ([PARTY] for a party quest, u09 1;
[GUILD] for a guild quest, u09 2) through the system copies; a folded
quest keeps its title and drops its lines.
================
*/
export function questTrackerEntries(
	quests: readonly QuestRecord[],
	tracked: readonly number[],
	folded: ReadonlySet<number>,
	records: Readonly<Record<number, QuestListMeta>>,
	entries: Readonly<Record<string, string>>,
	copy: ( key: string ) => string
): readonly QuestTrackerEntry[] {
	const byRef = new Map( quests.map( q => [ q.refId, q ] ) );
	const result: QuestTrackerEntry[] = [];
	for ( const refId of tracked ) {
		const quest = byRef.get( refId );
		if ( !quest ) continue;
		const meta = records[refId];
		const title = questTrackerTitle( quest, meta?.title ?? "", copy );
		const isFolded = folded.has( refId );
		const lines: QuestTrackerLine[] = [];
		if ( !isFolded ) {
			for ( const content of quest.contents ) {
				const objective = questObjectivePresentation( content, entries );
				lines.push( {
					text: `[${copy( objective.statusKey )}] ${objective.description}`,
					color: objective.color
				} );
			}
		}
		result.push( { refId, title, folded: isFolded, lines } );
	}
	return result;
}

/*
================
questTrackerTitle
================
*/
function questTrackerTitle( quest: QuestRecord, title: string, copy: ( key: string ) => string ): string {
	if ( quest.u09 === 1 ) return `[${copy( "UIIT_STT_PARTY" )}] ${title}`;
	if ( quest.u09 === 2 ) return `[${copy( "UIIT_STT_GUILDVIEW" )}] ${title}`;
	return title;
}

/*
================
QuestTrackerPlacementInput

The facts the tracker's placement needs: the actual screen and
window size, and the windows the composition owns this frame - the
"beside" rects grow the window list leftwards, the "above" rects
push the tracker down, mirroring the fork's own rule.
================
*/
export interface QuestTrackerPlacementInput {
	readonly screenWidth: number;
	readonly screenHeight: number;
	readonly width: number;
	readonly height: number;
	readonly beside: readonly (readonly [number, number, number, number])[];
	readonly above: readonly (readonly [number, number, number, number])[];
}

/*
================
questTrackerPlacement

Places the tracker: it starts at the screen's right edge on its
authored line (y = 267), then steps around the windows it would
overlap: a "beside" window moves it left of that window, an "above"
window moves it below that window, at most sixteen steps, and clamps
to the screen at the end. Returns the tracker's top-left corner.
================
*/
export function questTrackerPlacement(
	input: QuestTrackerPlacementInput
): readonly [number, number] {
	let x = input.screenWidth - input.width, y = QUEST_TRACKER_Y;
	const collides = ( r: readonly [number, number, number, number] ) => (
		x < r[0] + r[2] && x + input.width > r[0] && y < r[1] + r[3] && y + input.height > r[1]
	);
	for ( let step = 0; step < 16; step++ ) {
		const beside = input.beside.find( collides );
		if ( beside ) {
			x = beside[0] - input.width;
			continue;
		}
		const above = input.above.find( collides );
		if ( above ) {
			y = above[1] + above[3];
			continue;
		}
		break;
	}
	x = Math.max( 0, Math.min( input.screenWidth - input.width, x ) );
	y = Math.max( 0, Math.min( input.screenHeight - input.height, y ) );
	return [ x, y ];
}

/*
================
questTrackerSize

The tracker's skin size: the native plate (324 wide, 30 high per
slot) plus each shown quest's objective lines at their pitch.
================
*/
export function questTrackerSize(
	entries: readonly QuestTrackerEntry[],
	slotHeight: number
): readonly [number, number] {
	let height = 0;
	for ( const entry of entries ) {
		height += slotHeight;
		height += entry.lines.length * QUEST_TRACKER_OBJECTIVE_LINE_HEIGHT;
	}
	return [ 324, height ];
}
