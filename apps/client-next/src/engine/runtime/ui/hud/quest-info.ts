/*
===========================================================================

quest-info.ts - the level-up quest notice's lifetime: icon, then window

Owns the state the native CIFQuestInfoGuide (icon, child 0x2E) and
CIFQuestInfo (window, child 0x2D) keep between them (issue #2310): which
quests the last level gain opened, whether the blinking icon shows, and
whether the window is open. The filter, geometry and blink live in the
pure model (foundation/ui/quest-info.ts); ui.ts feeds the current view
facts to observe() every frame, draws what shows, and routes the two
clicks. Nothing here sends: opening the window is presentation only.

Native lifecycle copied: the icon appears only on a level gain with at
least one newly opened quest; with none, the section is destroyed
(69CCA0). Clicking the icon opens the window and hides the icon
(62DFB0); closing the window retires the notice until the next gain
(62C420 destroys the icon once its window is gone).

===========================================================================
*/
import {
	questInfoEntries,
	questInfoIconFrame,
	type QuestInfoEntry,
	type QuestInfoRecord
} from "@/engine/foundation/ui/quest-info";

export const questInfoIconPaths = [ 1, 2 ].map( n => `/assets/images/Media_extracted/icon/etc/qstinfoic_${n}.png` );

/*
================
createQuestInfo
================
*/
export function createQuestInfo() {
	// The last level observed; undefined until the first in-world view
	// sets the baseline, so entering at a level never shows the icon.
	let lastLevel: number | undefined;
	// A gain whose catalog had not loaded yet: the walk runs on the
	// first observe that brings records, at this level.
	let pendingLevel: number | undefined;
	let entries: readonly QuestInfoEntry[] = [];
	let open = false;
	// The blink phase as of the last observe (questInfoIconFrame).
	let frame: 0 | 1 = 0;
	return {
		/*
================
observe

Feed the current level, the local player's country byte, the held quest
ids and the quest table (undefined while any of them is unknown). A
level increase runs the walk; a decrease silently re-baselines; an
unchanged level only settles a pending gain. Returns true when what
the HUD shows may have changed.
================
		*/
		observe(
			level: number | undefined,
			country: number | undefined,
			held: readonly number[] | undefined,
			records: Readonly<Record<number, QuestInfoRecord>> | undefined,
			now: number
		): boolean {
			const nextFrame = questInfoIconFrame( now );
			if ( nextFrame !== frame ) {
				frame = nextFrame;
				if ( !open && entries.length ) return true;
			}
			if ( level === undefined ) return false;
			if ( lastLevel === undefined ) {
				lastLevel = level;
				return false;
			}
			let changed = false;
			if ( level !== lastLevel ) {
				pendingLevel = level > lastLevel ? level : undefined;
				lastLevel = level;
				entries = [];
				open = false;
				changed = true;
			}
			if ( pendingLevel !== undefined && records !== undefined && country !== undefined ) {
				entries = questInfoEntries( records, pendingLevel, country, new Set( held ?? [] ) );
				pendingLevel = undefined;
				if ( !entries.length ) open = false;
				changed = true;
			}
			return changed;
		},
		/*
================
iconVisible

The icon shows only while its window has never been opened.
================
		*/
		iconVisible(): boolean {
			return !open && entries.length > 0;
		},
		/*
================
iconPath

The current blink frame's image (observe keeps the phase current).
================
		*/
		iconPath(): string {
			return questInfoIconPaths[frame]!;
		},
		/*
================
openWindow

62DFB0: the click shows the window and hides the icon.
================
		*/
		openWindow(): void {
			if ( entries.length ) open = true;
		},
		/*
================
closeWindow

62C420: once the window is gone the notice is retired - the icon does
not come back until the next level gain.
================
		*/
		closeWindow(): void {
			open = false;
			entries = [];
		},
		windowOpen(): boolean {
			return open;
		},
		entries(): readonly QuestInfoEntry[] {
			return entries;
		},
		/*
================
reset

World exit: the native sections die with the interface.
================
		*/
		reset(): void {
			lastLevel = undefined;
			pendingLevel = undefined;
			entries = [];
			open = false;
			frame = 0;
		}
	};
}
