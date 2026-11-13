/*
===========================================================================

frame.ts - draw primitives and window admission for one UI frame

UiFrame borrows the current frame's arrays and its UI resource owners.
The coordinator retains input and window lifetime; these functions append
draw commands and controls, and admit complete windows as one unit.

===========================================================================
*/
import type { UiRect, UiQuad } from "@/engine/contracts/ui";
import type { BuffViewerIcon } from "@/engine/foundation/ui/buff-viewer";
import type { CloseControlId, UiFrame } from "./frame-contract";
import { closedPanel } from "@/engine/foundation/ui/panels";
import { resolveTextOverlaps } from "@/engine/foundation/rendering/ui-glyphs";
import { DESKTOP_HEIGHT, DESKTOP_WIDTH, fitUiGroup } from "@/engine/foundation/ui/layout";
import { frameRing } from "@/engine/foundation/ui/frame-ring";
import { comboBoxChrome } from "@/engine/foundation/ui/combo-box";
import { buttonAccess, buttonTextColor } from "@/engine/foundation/ui/button-state";

/*
================
windowOrigin
================
*/
export function windowOrigin(
	frame: UiFrame,
	{ name, initial, id = "window-drag:" + name, options = {} }: {
		name: string;
		initial: UiRect;
		id?: string;
		options?: { drag?: UiRect; nativeExtent?: readonly [number, number]; };
	}
) {
	const windowPlacement = frame.windowPlacement, w = frame.w, h = frame.h, controls = frame.controls;
	const r = windowPlacement.frame( id, initial, [ w, h ], options.nativeExtent );
	const drag = options.drag;
	controls.push( {
		id,
		label: name,
		kind: "region",
		draggable: true,
		rect: drag ?
			[ r[0] + drag[0], r[1] + drag[1], drag[2], drag[3] ] :
			[ r[0] + 10, r[1], r[2] - 21, 34 ]
	} );
	return r;
}

/*
================
rect
================
*/
export function rect(
	frame: UiFrame,
	{ r, color, textureId = "", uv = [ 0, 0, 1, 1 ], clip = frame.full }: {
		r: UiRect;
		color: UiQuad["color"];
		textureId?: string;
		uv?: UiRect;
		clip?: UiRect;
	}
) {
	const quads = frame.quads;
	if ( r[2] > 0 && r[3] > 0 ) quads.push( { rect: r, color, texture: textureId, uv, clip } );
}

/*
================
buffViewer
================
*/
export function buffViewer(
	frame: UiFrame,
	{ prefix, icons, ox, oy }: { prefix: string; icons: readonly BuffViewerIcon[]; ox: number; oy: number; }
) {
	const resources = frame.resources,
		paths = frame.paths,
		controls = frame.controls,
		blocks = frame.blocks,
		white = frame.white;
	for ( const icon of icons ) {
		const r: UiRect = [ ox + icon.x, oy + icon.y, icon.size, icon.size ],
			gauge: UiRect = [ r[0], r[1] + icon.size, icon.size, 4 ];
		for ( const layer of [ icon.path, ...icon.overlay ] ) {
			if ( layer ) {
				paths.push( layer );
				if ( resources.has( layer ) ) rect( frame, { r: r, color: white, textureId: layer } );
			}
		}
		for ( const layer of icon.gauge ) {
			paths.push( layer );
			if ( resources.has( layer ) ) rect( frame, { r: gauge, color: white, textureId: layer } );
		}
		controls.push( {
			id: prefix + icon.id,
			label: icon.label,
			helpSource: icon.helpSource,
			kind: "region",
			rect: r
		} );
		blocks.push( r );
	}
}

/*
================
image
================
*/
export function image(
	frame: UiFrame,
	{ r, path, color = frame.white, uv = [ 0, 0, 1, 1 ], clip = frame.full }: {
		r: UiRect;
		path: string;
		color?: UiQuad["color"];
		uv?: UiRect;
		clip?: UiRect;
	}
) {
	const paths = frame.paths, resources = frame.resources;
	paths.push( path );
	if ( resources.has( path ) ) rect( frame, { r: r, color: color, textureId: path, uv: uv, clip: clip } );
}

/*
================
additiveImage
================
*/
export function additiveImage( frame: UiFrame, r: UiRect, path: string, uv: UiRect = [ 0, 0, 1, 1 ] ) {
	const paths = frame.paths, resources = frame.resources, quads = frame.quads, white = frame.white, full = frame.full;
	paths.push( path );
	if ( resources.has( path ) ) {
		quads.push( { rect: r, color: white, texture: path, uv, clip: full, additive: true } );
	}
}

/*
================
label
================
*/
export function label(
	frame: UiFrame,
	{ value, x, y, color = frame.white, clip = frame.full }: {
		value: string;
		x: number;
		y: number;
		color?: UiQuad["color"];
		clip?: UiRect;
	}
) {
	const quads = frame.quads, text = frame.text;
	quads.push(
		...text.quads( value, [ x, y, Math.max( 0, clip[0] + clip[2] - x ), 22 ], clip, color, {
			vAlign: 0
		} )
	);
}

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
	// An absent image is not loading: it draws nothing, as a native static
	// with no texture does, and holding the window for it kept the M window
	// shut forever on a build without its chrome art (#3407).
	const unloaded = paths.slice( mark[3] ).filter( path => !resources.has( path ) && !resources.absent?.( path ) );
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

/*
================
windowBox
================
*/
export function windowBox(
	frame: UiFrame,
	{ title, x, y, width, height }: { title: string; x: number; y: number; width: number; height: number; }
) {
	const blocks = frame.blocks,
		quads = frame.quads,
		resources = frame.resources,
		full = frame.full,
		text = frame.text,
		white = frame.white;
	const FRAME = frame.chrome.frame, PARTS = frame.chrome.parts;
	const r: UiRect = [ x, y, width, height ];
	blocks.push( r );
	rect( frame, { r: r, color: [ .06, .075, .065, .96 ] } );
	quads.push(
		...frameRing( r, FRAME, PARTS.map( part => resources.size( FRAME + part + ".png" ) ), full )
	);
	quads.push(
		...text.quads( title, [ x + 10, y + 12, width - 21, 12 ], full, white, { hAlign: 1, vAlign: 1 } )
	);
}

/*
================
button
================
*/
export function button(
	frame: UiFrame,
	{ id, value, x, y, width = 120, disabled = false, selected = false, clientTop }: {
		id: string;
		value: string;
		x: number;
		y: number;
		width?: number;
		disabled?: boolean;
		selected?: boolean;
		clientTop?: number;
	}
) {
	const blocks = frame.blocks,
		controls = frame.controls,
		pressed = frame.pressed,
		hover = frame.hover,
		focus = frame.focus,
		resources = frame.resources,
		quads = frame.quads,
		text = frame.text,
		white = frame.white;
	const BUTTON = frame.chrome.button,
		BUTTON_FOCUS = frame.chrome.buttonFocus,
		BUTTON_PRESS = frame.chrome.buttonPress,
		BUTTON_DISABLE = frame.chrome.buttonDisable;
	const r: UiRect = [ x, y, width, 24 ];
	blocks.push( r );
	controls.push( { id, label: value, rect: r, kind: "button", disabled, selected } );
	const down = !disabled && ((pressed === id && hover === id) || selected),
		path = disabled ?
			BUTTON_DISABLE :
			down ?
			BUTTON_PRESS :
			((pressed === null && hover === id) || focus === id) ?
			BUTTON_FOCUS :
			BUTTON;
	if ( [ BUTTON, BUTTON_FOCUS, BUTTON_PRESS, BUTTON_DISABLE ].every( resources.has ) ) {
		rect( frame, { r: r, color: white, textureId: path } );
	}
	quads.push(
		...text.quads(
			value,
			[ x + (down ? 1 : 0), y + (down ? 1 : 0) + (clientTop ?? 0), width, 24 - (clientTop ?? 0) ],
			r,
			buttonTextColor( [ 254 / 255, 251 / 255, 216 / 255, 1 ], buttonAccess( false, disabled ) ),
			{ hAlign: 1, vAlign: clientTop === undefined ? 1 : 0 }
		)
	);
}

/*
================
closeButton
================
*/
export function closeButton( frame: UiFrame, x: number, y: number, id: CloseControlId ) {
	const blocks = frame.blocks,
		controls = frame.controls,
		resources = frame.resources,
		pressed = frame.pressed,
		hover = frame.hover,
		white = frame.white;
	const CLOSE = frame.chrome.close, CLOSE_PRESS = frame.chrome.closePress, CLOSE_FOCUS = frame.chrome.closeFocus;
	const r: UiRect = [ x, y, 16, 16 ];
	blocks.push( r );
	controls.push( { id, label: "Close", rect: r, kind: "button" } );
	if ( resources.has( CLOSE ) && resources.has( CLOSE_PRESS ) && resources.has( CLOSE_FOCUS ) ) {
		rect( frame, {
			r: r,
			color: white,
			textureId: pressed === id ? CLOSE_PRESS : hover === id ? CLOSE_FOCUS : CLOSE
		} );
	}
}

/*
================
comboBox
================
*/
export function comboBox(
	frame: UiFrame,
	{ r, id, label, value, disabled = false }: {
		r: UiRect;
		id: string;
		label: string;
		value: string;
		disabled?: boolean;
	}
) {
	const resources = frame.resources,
		full = frame.full,
		pressed = frame.pressed,
		hover = frame.hover,
		paths = frame.paths,
		quads = frame.quads,
		text = frame.text,
		white = frame.white,
		controls = frame.controls;
	const chrome = comboBoxChrome(
		r,
		resources.size,
		full,
		!disabled && pressed === id ? 2 : !disabled && hover === id ? 1 : 0
	);
	paths.push( ...chrome.paths );
	quads.push( ...chrome.quads );
	quads.push( ...text.quads( value, chrome.textRect, r, white, { hAlign: 1, vAlign: 1 } ) );
	controls.push( { id, label, rect: r, kind: "button", disabled } );
}
