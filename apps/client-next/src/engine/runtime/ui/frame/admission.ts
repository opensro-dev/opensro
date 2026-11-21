/*
===========================================================================

admission.ts - a window is published whole or not at all

beginWindow marks where a window starts in the frame's arrays; endWindow
publishes everything drawn since as one unit once its own art and the font
are resident, fitting it to a compact or small viewport first, and keeps
the last admitted copy on screen while a replacement is still loading.
An internal of frame.ts, which re-exports it.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import type { UiFrame } from "./frame-contract";
import { closedPanel } from "@/engine/foundation/ui/panels";
import { resolveTextOverlaps } from "@/engine/foundation/rendering/ui-glyphs";
import { DESKTOP_HEIGHT, DESKTOP_WIDTH, fitUiGroup } from "@/engine/foundation/ui/layout";

/*
================
beginWindow
================
*/
export function beginWindow( frame: UiFrame ) {
	const quads = frame.quads,
		controls = frame.controls,
		blocks = frame.blocks,
		paths = frame.paths,
		commonWindowPaths = frame.commonWindowPaths;
	const mark = [ quads.length, controls.length, blocks.length, paths.length ] as const;
	paths.push( ...commonWindowPaths );
	return mark;
}

/*
================
unloadedImages

The images a window drew from `from` on that are still loading: neither
resident nor known absent. Native never holds a window for its art:
CTextBoard_SetTexturePath (53FF00) keeps whatever the texture load
returned, null for a missing file, and CIFWnd_RenderTexturedFace (5405DF)
draws on the path flag, not the texture. So an absent image never holds a
window's admission; it held the M window shut forever on a build without
its chrome art (#3407). Every window admission asks this one rule.
================
*/
export function unloadedImages( resources: UiFrame["resources"], paths: readonly string[], from: number ): string[] {
	return paths.slice( from ).filter( path => !resources.has( path ) && !resources.absent?.( path ) );
}

/*
================
endWindow
================
*/
export function endWindow(
	frame: UiFrame,
	mark: readonly [number, number, number, number],
	owner = "primary",
	source?: UiRect
) {
	const admittedWindows = frame.admittedWindows,
		paths = frame.paths,
		resources = frame.resources,
		windowMissing = frame.windowMissing,
		contentImages = frame.contentImages,
		phase = frame.phase,
		w = frame.w,
		h = frame.h,
		fontPath = frame.fontPath,
		quads = frame.quads,
		controls = frame.controls,
		blocks = frame.blocks,
		compact = frame.compact,
		full = frame.full;
	const compactWindowDrag = frame.compactPolicy.windowDrag, compactCloseControl = frame.compactPolicy.closeControl;
	const admittedWindow = admittedWindows.get( owner );
	// Everything still loading is reported (tests wait on it before a raster);
	// only the window's own art holds its admission.
	const unloaded = unloadedImages( resources, paths, mark[3] );
	windowMissing.push( ...unloaded );
	const gaps = unloaded.filter( path => !contentImages.has( path ) );
	const key = phase + ":" + w + ":" + h;
	if ( !fontPath || !resources.has( fontPath ) || gaps.length ) {
		quads.length = mark[0];
		controls.length = mark[1];
		blocks.length = mark[2];
		// Keep a fully admitted popup until its replacement is ready. Old image
		// demand stays alive; stale content controls cannot dispatch new-tab actions.
		if ( admittedWindow?.key === key ) {
			quads.push( ...admittedWindow.quads );
			blocks.push( ...admittedWindow.blocks );
			paths.push( ...admittedWindow.paths );
			controls.push(
				...admittedWindow.controls.map( c => ({
					...c,
					disabled: closedPanel( c.id ) !== undefined || c.id.startsWith( "select-window:" ) ?
						c.disabled :
						true
				}) )
			);
		}
	} else {
		// Fit within this window before caching/publication; text behind a modal
		// must not become a neighboring column of that modal.
		if ( compact ) {
			const windowBounds = owner === "item-mall" || owner === "compact-guild" || compact.top < 200 ?
				full :
				compact.bounds;
			fitUiGroup( quads, controls, mark[0], mark[1], windowBounds, full, {
				blocks,
				firstBlock: mark[2],
				disableDrag: compactWindowDrag,
				...(source ? { sourceBounds: source } : {})
			} );
			for ( let i = mark[1]; i < controls.length; i++ ) {
				controls[i] = compactCloseControl( controls[i]!, windowBounds );
			}
		} else if ( w < DESKTOP_WIDTH || h < DESKTOP_HEIGHT ) {
			// Port-only, not native: a fine pointer below the original's
			// smallest screen mode keeps the desktop windows; one that
			// cannot hold the viewport is scaled into it, and a window
			// that already fits keeps the place the player dragged it to.
			// A scaled window drops its window drags like the compact
			// fit does: a drag would write fitted coordinates back as
			// the remembered native position.
			fitUiGroup( quads, controls, mark[0], mark[1], full, full, {
				blocks,
				firstBlock: mark[2],
				disableDrag: compactWindowDrag,
				leaveWhenFits: true,
				...(source ? { sourceBounds: source } : {})
			} );
		}
		const painted = resolveTextOverlaps( quads.slice( mark[0] ) );
		quads.length = mark[0];
		quads.push( ...painted );
		admittedWindows.set( owner, {
			key,
			quads: painted,
			controls: controls.slice( mark[1] ),
			blocks: blocks.slice( mark[2] ),
			paths: paths.slice( mark[3] )
		} );
	}
}
