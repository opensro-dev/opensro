/*
===========================================================================

map-level-filter.ts - the world map foot's monster level filter

The v1.696 bottom box's control 31, "Display 6 lv.higher monsters from
the current character's level". It starts unchecked (E72F30). Checking it
narrows the guide drawer to the band, tints the hunting grounds of every
guide entry 0..6 levels above the player on the world page (E72300) and
moves the map onto them once (E724F0). Map tools own the guide catalogue
and hand it in; this module owns only the box and its one pending snap.

===========================================================================
*/

import {
	levelFilterOverlay,
	levelFilterTarget,
	type WorldMapGuideCatalog
} from "@/engine/foundation/ui/worldmap-guide";
import type { Pose } from "@/engine/contracts/gameplay";
import { levelBand, type LevelBand } from "@/engine/foundation/ui/hunting-guide";

/*
================
GuideState

What the filter reads of map tools' guide load: its rows once "ready",
and "absent" when they will never come (a build without the guide).
================
*/
export interface GuideState {
	readonly kind: string;
	readonly catalog?: WorldMapGuideCatalog;
}

/*
================
createLevelFilter
================
*/
export function createLevelFilter() {
	let checked = false;
	// Checking the box asks for one snap, taken once the catalogue is in.
	let snapPending = false;
	return {
		/*
		================
		checked
		================
		*/
		checked(): boolean {
			return checked;
		},
		/*
		================
		toggle
		================
		*/
		toggle(): void {
			checked = !checked;
			snapPending = checked;
		},
		/*
		================
		band

		The drawer's band for this character; undefined (every species
		shows) while the box is off or the level unknown.
		================
		*/
		band( characterLevel: number | undefined ): LevelBand | undefined {
			if ( !checked || characterLevel === undefined ) return undefined;
			return levelBand( characterLevel );
		},
		/*
		================
		ground

		The tinted hunting grounds on the world page; null while the box is
		off, the level unknown or the catalogue not in.
		================
		*/
		ground(
			guide: GuideState,
			characterLevel: number | undefined,
			frame: Parameters<typeof levelFilterOverlay>[2],
			clip: Parameters<typeof levelFilterOverlay>[3]
		): ReturnType<typeof levelFilterOverlay> | null {
			if ( !checked || characterLevel === undefined || !guide.catalog ) return null;
			return levelFilterOverlay( guide.catalog, characterLevel, frame, clip );
		},
		/*
		================
		takeSnap

		The pose the map moves to once after the box is checked, when the
		catalogue is in; null otherwise. A catalogue that will never come
		drops the snap.
		================
		*/
		takeSnap( guide: GuideState, characterLevel: number | undefined ): Pose | null {
			if ( !snapPending || characterLevel === undefined ) return null;
			if ( guide.kind === "absent" ) snapPending = false;
			if ( !guide.catalog ) return null;
			snapPending = false;
			return levelFilterTarget( guide.catalog, characterLevel );
		}
	};
}
