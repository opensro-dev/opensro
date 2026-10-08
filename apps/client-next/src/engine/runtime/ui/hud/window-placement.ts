/*
===========================================================================

window-placement.ts - where draggable windows sit, and where they reopen

UI-owned placement only. Visibility, modal admission and close side
effects remain with each window's existing lifecycle owner.

It also keeps the remembered positions (window-positions.ts): enter()
hands a session the positions saved for its screen size, as
CGInterface_LoadWindowPositions (6A06B0) does when the interface is
created, and leave() gathers them again at logout, as
CGInterface_SaveWindowPositions (6A01B0) does.

===========================================================================
*/

import type { UiRect } from "@/engine/contracts/ui";
import {
	positionsAfterLayout,
	positionsForViewport,
	rememberedWindows,
	type RememberedWindow,
	type WindowPositions
} from "@/engine/foundation/ui/window-positions";

type PlacedWindows = WindowPositions["windows"];

// The remembered windows this owner frames, by their drag-region id. The
// main popup, world map and game guide keep their own origins in the UI.
const PLACED_WINDOWS: readonly (readonly [RememberedWindow, string])[] = [
	[ "store", "window-drag:Shop" ],
	[ "storageRoom", "window-drag:Storage" ],
	[ "exchange", "window-drag:Exchange" ],
	[ "cosWindow", "window-drag:COS inventory" ],
	[ "alchemyBox", "window-drag:Alchemy" ],
	[ "autoPotion", "window-drag:Auto Potion" ]
];

/*
================
createWindowPlacement
================
*/
export function createWindowPlacement() {
	const frames = new Map<string, { rect: UiRect; viewport: readonly [number, number]; }>();
	const consumedOrigins = new Set<RememberedWindow>();
	let saved: WindowPositions | null = null;
	let session: { width: number; height: number; windows: PlacedWindows; } | null = null;
	let initialSave = false;
	let rejectedRecord = false;

	/*
	================
	snapshot

	6A01B0 gathers current origins without changing interface lifetime.
	================
	*/
	function snapshot( width: number, height: number, own: PlacedWindows ): WindowPositions | null {
		if ( !session ) return null;
		// 6A02CE -> 69C290 retains remembered coordinates for an absent window,
		// even when the active screen dimensions changed during this session.
		const windows: Partial<Record<RememberedWindow, readonly [number, number]>> = { ...session.windows };
		for ( const [key, id] of PLACED_WINDOWS ) {
			const rect = frames.get( id )?.rect;
			// A seeded frame keeps its zero extent until the window is drawn.
			if ( rect && rect[2] > 0 ) windows[key] = [ rect[0], rect[1] ];
		}
		Object.assign( windows, own );
		// 6A028F initializes one coordinate pair before the ordered write loop.
		// A missing control and missing map entry leave that pair unchanged.
		let previous: readonly [number, number] = [ 0, 0 ];
		for ( const { key } of rememberedWindows() ) {
			previous = windows[key] ?? previous;
			windows[key] = previous;
		}
		saved = { width, height, windows };
		initialSave = false;
		rejectedRecord = false;
		return saved;
	}

	/*
	================
	clamp
	================
	*/
	function clamp( r: UiRect, w: number, h: number ): UiRect {
		return [
			Math.max( 0, Math.min( Math.max( 0, w - r[2] ), r[0] ) ),
			Math.max( 0, Math.min( Math.max( 0, h - r[3] ), r[1] ) ),
			r[2],
			r[3]
		];
	}

	return {
		snapshot,
		/*
		================
		needsInitialSave

		6A0BA0 immediately replaces a record rejected by the screen-size gate.
		================
		*/
		needsInitialSave() {
			return initialSave;
		},
		/*
		================
		takeRemembered

		69C290 restores a lazy control once its real extent is known. Manual
		UI owners then retain their actual origin, including centered defaults.
		================
		*/
		takeRemembered(
			key: RememberedWindow,
			width: number,
			height: number,
			extent: readonly [number, number]
		): readonly [number, number] | null {
			if ( !session || consumedOrigins.has( key ) ) return null;
			consumedOrigins.add( key );
			const position = positionsAfterLayout( session.windows )[key];
			if ( !position ) return null;
			return [ Math.min( width - extent[0], position[0] ), Math.min( height - extent[1], position[1] ) ];
		},
		/*
		================
		frame
		================
		*/
		frame( id: string, initial: UiRect, w: number, h: number ): UiRect {
			const old = frames.get( id );
			let rect: UiRect = old ? [ old.rect[0], old.rect[1], initial[2], initial[3] ] : initial;
			// 69C2D7..69C2F6 checks a lazy window's remembered origin only when
			// its real extent is known; negative origins remain valid natively.
			if ( old && old.rect[2] === 0 ) {
				rect = [ Math.min( w - rect[2], rect[0] ), Math.min( h - rect[3], rect[1] ), rect[2], rect[3] ];
			} else if ( !old || old.viewport[0] !== w || old.viewport[1] !== h ) {
				// Native tab reflow changes extent without recentering the owner.
				rect = clamp( rect, w, h );
			}
			frames.set( id, { rect, viewport: [ w, h ] } );
			return rect;
		},
		/*
		================
		drag
		================
		*/
		drag( id: string, dx: number, dy: number ) {
			const entry = frames.get( id );
			if ( !entry ) return false;
			entry.rect = clamp(
				[ entry.rect[0] + dx, entry.rect[1] + dy, entry.rect[2], entry.rect[3] ],
				...entry.viewport
			);
			return true;
		},
		/*
		================
		read
		================
		*/
		read( id: string ) {
			return frames.get( id )?.rect;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			frames.clear();
			consumedOrigins.clear();
		},
		/*
		================
		load

		The stored record Platform restored.
		================
		*/
		load( value: WindowPositions | null ) {
			saved = value;
			// No load event means no file; null means an existing record failed validation.
			rejectedRecord = value === null;
		},
		/*
		================
		enter

		World entry (6A06B0): the saved positions apply only at the size they
		were saved at. Initial layout overrides eagerly created windows; seeds
		and returns the remaining origins for their owners. Null while a session
		is already open, so a teleport never undoes this session's drags.
		================
		*/
		enter( width: number, height: number ): PlacedWindows | null {
			if ( session ) return null;
			consumedOrigins.clear();
			initialSave = rejectedRecord || saved !== null && (saved.width !== width || saved.height !== height);
			const eligible = positionsForViewport( saved, width, height );
			session = { width, height, windows: eligible };
			const windows = positionsAfterLayout( eligible );
			for ( const [key, id] of PLACED_WINDOWS ) {
				const position = windows[key];
				if ( position ) {
					frames.set( id, { rect: [ position[0], position[1], 0, 0 ], viewport: [ width, height ] } );
				}
			}
			return windows;
		},
		/*
		================
		leave

		Logout (6A01B0): every remembered window's position. A window not
		opened this session keeps the position it entered with, as 6A01B0
		reads the remembered one for a window not created. Null when no
		session entered.
		================
		*/
		leave( width: number, height: number, own: PlacedWindows ): WindowPositions | null {
			const result = snapshot( width, height, own );
			session = null;
			return result;
		}
	};
}
