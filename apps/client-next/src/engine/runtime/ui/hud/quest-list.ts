/*
===========================================================================

quest-list.ts - the native quest list window and mini tracker lifetime

Owns the quest list window's state (which quests are tracked, at most
three; which tracked quests the tracker folds; the list's scroll) and
what the two res_ui windows show (foundation/ui/quest-list holds the
pure row model; this owner feeds it from the session and the catalogue
and draws the layout instances). Clicks arrive as the control ids the
draw emitted; actions the UI must service are reported back
synchronously: a locate names the quest whose objective the world map
should centre on, a content view asks for the quest-detail window,
and the tracker-full refusal surfaces the helper copy.

Quest data comes from the session's QuestRecord list and the quest
presentation catalogue (records + text), never from outside the asset
pipeline. Titles resolve through the same hudCopy the other quest
windows use, and the remaining-time line comes from the quest timers
owner the coordinator feeds.
===========================================================================
*/

import type { QuestRecord } from "@/engine/contracts/gameplay";
import type { QuestListRow, QuestTrackerEntry } from "@/engine/foundation/ui/quest-list";
import {
	QUEST_TRACKED_MAX,
	questContentFace,
	questContentFaceTexture,
	questListRows,
	questListTitle,
	questToggleTracked,
	questTrackerEntries
} from "@/engine/foundation/ui/quest-list";
import type { QuestPresentation } from "@/engine/foundation/ui/quest-presentation";
import { createResLayoutWindow, type ResLayoutWindow } from "./../res-layout/res-layout";
import type { ResLayout } from "@/engine/foundation/ui/res-layout";
import type { UiFrame } from "./../frame/frame-contract";

// The layout's own counts: ten CNIFQuestSlot rows, three
// CNIFMiniQuestSlot rows. The importer publishes their slots in
// layout order (ascending y), so placement below walks them in
// record order.
export const QUEST_LIST_SLOTS = 10;
export const QUEST_TRACKER_SLOTS = 3;

// The list window's scrollbar control (questlist.2dt id 6).
export const QUEST_LIST_SCROLL_KEY = "6";

// The list window's caption ("Quest list" in textuisystem).
export const QUEST_LIST_CAPTION = "UIIT_STT_QUEST_LIST";

// Control ids the draw binds (the layout children each slot carries).
export const QUEST_TRACK_ID = 8;
export const QUEST_TITLE_ID = 16;
export const QUEST_STATUS_ID = 11;
export const QUEST_TIMER_ID = 75;
export const QUEST_LOCATE_ID = 18;
export const QUEST_CONTENT_ID = 19;
export const QUEST_TRACKER_FOLD_ID = 56;
export const QUEST_TRACKER_TITLE_ID = 57;

// Free-standing objective lines drawn under a tracker slot's title.
export const QUEST_TRACKER_LINE_X = 10;
export const QUEST_TRACKER_LINE_Y = 44;

/*
================
QuestListInput

One frame's facts: the session's quests, the catalogue facts (records
by refId, the objective text map, the player's level), and the two
layout windows' resource bundles.
================
*/
export type QuestListInput = {
	readonly quests: readonly QuestRecord[];
	readonly presentation: QuestPresentation | undefined;
	readonly playerLevel: number;
	readonly timers: { text( q: QuestRecord, copy: ( key: string ) => string ): string; };
};

/*
================
QuestListAction

The synchronous answers a click or state change asks of the caller:
a locate to service on the world map, a content view for the detail
window, or the helper-full copy to surface.
================
*/
export type QuestListAction = { kind: "locate"; refId: number; } | { kind: "content"; refId: number; } | {
	kind: "helper-full";
};

/*
================
createQuestList
================
*/
export function createQuestList( questList: ResLayout, questMiniList: ResLayout ) {
	const listWindow: ResLayoutWindow = createResLayoutWindow( questList ),
		trackerWindow: ResLayoutWindow = createResLayoutWindow( questMiniList );
	let quests: readonly QuestRecord[] = [],
		tracked: readonly number[] = [],
		folded = new Set<number>(),
		scrollOffset = 0,
		dragRest = 0,
		playerLevel = 0;
	let presentation: QuestPresentation | undefined;
	let actions: QuestListAction[] = [];
	// The last quest the player located (world-map marking); 0 clears.
	let located = 0;
	let levelFace = new Map<number, "blue" | "normal" | "red">();
	/*
================
	queueAction
================
	*/
	const queueAction = ( action: QuestListAction ) => {
		if ( action.kind ) actions.push( action );
	};
	/*
================
	trackedQuest
================
	*/
	const trackedList = (): readonly number[] => tracked;
	/*
================
	questRows

	The list window's visible flat rows after the scroll offset.
================
	*/
	const questRows = (): readonly QuestListRow[] => {
		const entries = presentation?.text ?? {};
		return questListRows( quests, entries );
	};
	return {
		/*
================
		observe

		Feed a frame's quests, catalogue facts and player level, and
		reclip the scroll offset to the row count.
		*/
		observe( input: QuestListInput ) {
			quests = input.quests;
			presentation = input.presentation;
			playerLevel = input.playerLevel;
			levelFace = new Map();
			for ( const quest of quests ) {
				const meta = presentation?.records[quest.refId];
				if ( meta ) levelFace.set( quest.refId, questContentFace( meta.level, playerLevel ) );
			}
			const rows = questRows().length;
			scrollOffset = Math.min( scrollOffset, Math.max( 0, rows - QUEST_LIST_SLOTS ) );
		},
		/*
================
		tracked

		The tracked quests in track order (the newest last), for the
		world map's quest marker pass.
================
		*/
		tracked: trackedList,
		/*
================
		lastLocated

		The located quest's id, or undefined when none was ever
		marked; the world map draws its quest icon while it stands.
		*/
		lastLocated(): number | undefined {
			return quests.some( q => q.refId === located ) ? located : undefined;
		},
		/*
================
		located

		Members the quest the player last located; the marker lasts
		while the quest is held.
		*/
		locateQuest( refId: number ) {
			located = refId;
		},
		/*
================
		trackedCount
================
		*/
		trackedCount(): number {
			return tracked.length;
		},
		/*
================
		toggleTracked

		Toggles a quest's track box; the refusal surfaces the helper
		copy. Returns the outcome so the caller can answer it.
		*/
		toggleTracked( refId: number ): { readonly status: "added" | "removed" | "full"; } {
			const result = questToggleTracked( tracked, refId );
			tracked = result.tracked;
			if ( result.status === "full" ) queueAction( { kind: "helper-full" } );
			return { status: result.status };
		},
		/*
================
		toggleFolded

		Folds or unfolds one tracked quest in the tracker.
		*/
		toggleFolded( refId: number ) {
			if ( folded.has( refId ) ) folded.delete( refId );
			else folded.add( refId );
		},
		/*
================
		scrollBy

		Scrolls the list by a row delta (wheel ticks arrive positive
		for down), clamped to the row count.
		*/
		scrollBy( delta: number ) {
			const rows = questRows().length;
			scrollOffset = Math.min(
				Math.max( 0, rows - QUEST_LIST_SLOTS ),
				Math.max( 0, scrollOffset + delta )
			);
		},
		/*
================
		dragScroll

		Scrollbar thumb drags arrive as fractional row deltas; the rest
		carries between events so a slow drag still steps a row.
================
		*/
		dragScroll( rows: number ) {
			dragRest += rows;
			const step = Math.trunc( dragRest );
			if ( !step ) return;
			dragRest -= step;
			this.scrollBy( step );
		},
		/*
================
		scroll
================
		*/
		scroll(): number {
			return scrollOffset;
		},
		/*
================
		maxScroll
================
		*/
		maxScroll(): number {
			return Math.max( 0, questRows().length - QUEST_LIST_SLOTS );
		},
		/*
================
		scrollRect

		The list's scrollbar control (layout key 6, type 13) placed at
		the window origin. The layout authors it without art, so the
		caller draws the shared scrollbar over this rect.
================
		*/
		scrollRect( ox: number, oy: number ): readonly [number, number, number, number] {
			const control = listWindow.control( QUEST_LIST_SCROLL_KEY ), root = listWindow.layout.root.rect;
			if ( !control ) return [ ox, oy, 0, 0 ];
			return [ ox + control.rect[0] - root[0], oy + control.rect[1] - root[1], control.rect[2], control.rect[3] ];
		},
		/*
================
		actions
================
		*/
		actions(): readonly QuestListAction[] {
			const drained = actions;
			actions = [];
			return drained;
		},
		/*
================
		trackerEntries
================
		*/
		trackerEntries( copy: ( key: string ) => string ): readonly QuestTrackerEntry[] {
			if ( !presentation ) return [];
			const records = presentation.records as Readonly<Record<number, { level: number; title: string; }>>;
			return questTrackerEntries( quests, tracked, folded, records, presentation.text, copy );
		},
		/*
================
		handleControl

		Acts on one drawn control id; returns whether the owner
		consumed it so the caller can leave the rest unrouted.
		*/
		handleControl( id: string ): boolean {
			if ( id.startsWith( "quest-list-track:" ) ) {
				const refId = Number( id.slice( "quest-list-track:".length ) ),
					result = questToggleTracked( tracked, refId );
				tracked = result.tracked;
				if ( result.status === "full" ) queueAction( { kind: "helper-full" } );
				return true;
			}
			if ( id.startsWith( "quest-list-content:" ) ) {
				queueAction( { kind: "content", refId: Number( id.slice( "quest-list-content:".length ) ) } );
				return true;
			}
			if ( id.startsWith( "quest-list-locate:" ) ) {
				queueAction( { kind: "locate", refId: Number( id.slice( "quest-list-locate:".length ) ) } );
				return true;
			}
			if ( id.startsWith( "quest-tracker-fold:" ) ) {
				const refId = Number( id.slice( "quest-tracker-fold:".length ) );
				if ( folded.has( refId ) ) folded.delete( refId );
				else folded.add( refId );
				return true;
			}
			return false;
		},
		/*
================
		drawList

		Draws the list window at a caller-chosen origin after filling
		its slots with the visible rows. Slot fill happens here, not
		in step time, so the layout state never goes stale.
		*/
		drawList(
			frame: UiFrame,
			ox: number,
			oy: number,
			copy: ( key: string ) => string,
			timers: QuestListInput["timers"]
		) {
			const window = listWindow, rows = questRows();
			const slotKeys = questListSlotKeys( window );
			slotKeys.forEach( ( slotKey, index ) => {
				const row = rows[scrollOffset + index];
				if ( !row ) {
					window.show( slotKey, false );
					return;
				}
				window.show( slotKey, true );
				if ( row.content < 0 ) {
					// A quest row: title plus progress, the track box,
					// the remaining-time line, locate and content view.
					const meta = presentation?.records[row.quest.refId];
					window.show( msgKey( slotKey, QUEST_TITLE_ID ) );
					window.show( msgKey( slotKey, QUEST_TRACK_ID ) );
					window.show( msgKey( slotKey, QUEST_TIMER_ID ) );
					window.show( msgKey( slotKey, QUEST_LOCATE_ID ) );
					window.show( msgKey( slotKey, QUEST_CONTENT_ID ) );
					window.hide( msgKey( slotKey, QUEST_STATUS_ID ) );
					window.setText(
						msgKey( slotKey, QUEST_TITLE_ID ),
						meta ? questListTitle( meta, row.quest, copy ) : ""
					);
					const time = timers.text( row.quest, copy );
					if ( time ) window.setText( msgKey( slotKey, QUEST_TIMER_ID ), time );
					window.show( msgKey( slotKey, QUEST_TIMER_ID ), !!time );
					window.hide( msgKey( slotKey, QUEST_STATUS_ID ) );
					window.setChecked( msgKey( slotKey, QUEST_TRACK_ID ), tracked.includes( row.quest.refId ) );
					window.setId(
						msgKey( slotKey, QUEST_TRACK_ID ),
						"quest-list-track:" + row.quest.refId
					);
					window.setId( msgKey( slotKey, QUEST_LOCATE_ID ), "quest-list-locate:" + row.quest.refId );
					window.setImage(
						msgKey( slotKey, QUEST_CONTENT_ID ),
						questContentFaceTexture( levelFace.get( row.quest.refId ) ?? "normal" )
					);
					window.setId( msgKey( slotKey, QUEST_CONTENT_ID ), "quest-list-content:" + row.quest.refId );
				} else if ( row.objective ) {
					// An objective row: the small face with its
					// description and status, no buttons of its own.
					window.show( msgKey( slotKey, QUEST_TITLE_ID ) );
					window.show( msgKey( slotKey, QUEST_STATUS_ID ) );
					window.hide( msgKey( slotKey, QUEST_TRACK_ID ) );
					window.hide( msgKey( slotKey, QUEST_TIMER_ID ) );
					window.hide( msgKey( slotKey, QUEST_LOCATE_ID ) );
					window.hide( msgKey( slotKey, QUEST_CONTENT_ID ) );
					window.setText( msgKey( slotKey, QUEST_TITLE_ID ), row.objective.description );
					window.setTint( msgKey( slotKey, QUEST_TITLE_ID ), row.objective.color );
					window.setText( msgKey( slotKey, QUEST_STATUS_ID ), copy( row.objective.statusKey ) );
					window.setTint( msgKey( slotKey, QUEST_STATUS_ID ), row.objective.color );
				}
			} );
			// The archive's caption is Korean (" 퀘스트 목록", quest list);
			// the window shows the same words from the text table.
			window.setText( window.layout.root.key, copy( QUEST_LIST_CAPTION ) );
			window.draw( frame, ox, oy );
		},
		/*
================
		drawTracker

		Draws the mini tracker at the placed origin: each tracked
		quest's slot row plus its free-standing objective lines.
		*/
		drawTracker(
			frame: UiFrame,
			ox: number,
			oy: number,
			copy: ( key: string ) => string
		) {
			const entries = this.trackerEntries( copy );
			const window = trackerWindow;
			const slots = trackerSlotKeys( window );
			slots.forEach( ( slotKey, index ) => {
				const entry = entries[index];
				if ( !entry ) {
					window.show( slotKey, false );
					return;
				}
				window.show( slotKey, true );
				window.setText( msgKey( slotKey, QUEST_TRACKER_TITLE_ID ), entry.title );
				window.setLabel(
					msgKey( slotKey, QUEST_TRACKER_FOLD_ID ),
					"quest-tracker-fold:" + entry.refId
				);
				window.setId( msgKey( slotKey, QUEST_TRACKER_FOLD_ID ), "quest-tracker-fold:" + entry.refId );
				void QUEST_TRACKER_SLOTS;
			} );
			window.draw( frame, ox, oy );
			// Objective lines under each slot's title, drawn free-
			// standing (the later client composes them from text
			// rows, not layout children); x follows the title's own
			// authored inset so the lines keep the slot's margin.
			let y = oy;
			for ( const entry of entries ) {
				y += 30;
				for ( const line of entry.lines ) {
					const rect = [ ox + QUEST_TRACKER_LINE_X, y, 304, 16 ] as const;
					frame.quads.push(
						...frame.text.quads(
							line.text,
							[ rect[0], rect[1], rect[2], rect[3] ],
							frame.full,
							line.color,
							{
								vAlign: 0
							}
						)
					);
					y += 16;
				}
			}
		},
		/*
================
		reset
================
		*/
		reset() {
			quests = [];
			tracked = [];
			folded = new Set();
			scrollOffset = 0;
			dragRest = 0;
			actions = [];
			located = 0;
		}
	};
}

/*
================
msgKey
================
*/
function msgKey( slotKey: string, id: number ): string {
	return slotKey + "/" + id;
}

/*
================
questListSlotKeys

The quest list's CNIFQuestSlot rows, in the layout's record order
(already ascending by authored y).
================
*/
export function questListSlotKeys( window: ResLayoutWindow ): readonly string[] {
	return window.layout.order.filter( key => window.layout.controls[key]?.className === "CNIFQuestSlot" );
}

/*
================
trackerSlotKeys

The mini tracker's CNIFMiniQuestSlot rows, in the layout's record
order.
================
*/
export function trackerSlotKeys( window: ResLayoutWindow ): readonly string[] {
	return window.layout.order.filter( key => window.layout.controls[key]?.className === "CNIFMiniQuestSlot" );
}

// QUEST_TRACKED_MAX is re-exported for callers that bind the rule.
export { QUEST_TRACKED_MAX };
