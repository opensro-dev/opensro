/*
===========================================================================

frame.ts - the frame builder: one frame's draw primitives

step() in ui.ts once defined its draw helpers inside itself, closing
over the frame's arrays, so no window code could leave that file. The
builder lives here instead: a plain record, UiFrame, holding the arrays,
the viewport, the HUD data and the text owner, plus the module functions
that draw into it. step() creates one frame per call and forwards its
old helpers here; window owners call the module directly.

The record's array fields are live: step() re-seeds the arrays as it
walks its sections, and the primitives must always draw into the
arrays step() currently holds.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { BuffViewerIcon } from "@/engine/foundation/ui/buff-viewer";
import { resolveTextOverlaps, type GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import { buttonAccess, buttonTextColor } from "@/engine/foundation/ui/button-state";
import { comboBoxChrome } from "@/engine/foundation/ui/combo-box";
import { frameRing } from "@/engine/foundation/ui/frame-ring";
import { fitUiGroup } from "@/engine/foundation/ui/layout";
import { closedPanel } from "@/engine/foundation/ui/panels";

const white = [ 1, 1, 1, 1 ] as const;

/*
================
UiAdmittedWindow

One window's last fully admitted paint, replayed while its replacement
is still loading (see uiEndWindow).
================
*/
export interface UiAdmittedWindow {
	readonly key: string;
	readonly quads: UiQuad[];
	readonly controls: UiControl[];
	readonly blocks: UiRect[];
	readonly paths: string[];
}

/*
================
UiFrameChrome

The window chrome art the primitives draw with: the frame ring's base
path and parts, the button family's four states and the close family's
three.
================
*/
export interface UiFrameChrome {
	readonly frame: string;
	readonly frameParts: readonly string[];
	readonly button: string;
	readonly buttonFocus: string;
	readonly buttonPress: string;
	readonly buttonDisable: string;
	readonly close: string;
	readonly closePress: string;
	readonly closeFocus: string;
}

/*
================
UiFrameText

The text owner's surface the primitives use. Declared here, not
imported from the owner: only ui.ts imports its own modules, and the
builder receives their surfaces through the record (as every HUD
presenter already does).
================
*/
export interface UiFrameText {
	quads(
		value: string,
		rect: UiRect,
		clip: UiRect,
		color: UiQuad["color"],
		style?: GlyphStyle
	): readonly UiQuad[];
}

/*
================
UiFrameResources

The resource owner's surface the primitives use: what is loaded, and
the natural size of what is.
================
*/
export interface UiFrameResources {
	has( path: string ): boolean;
	size( path: string ): readonly [number, number] | undefined;
}

/*
================
UiFramePlacement

The placement owner's surface the primitives use: a window's frame
inside the viewport.
================
*/
export interface UiFramePlacement {
	frame(
		id: string,
		initial: UiRect,
		viewport: readonly [number, number],
		restoreExtent?: readonly [number, number]
	): UiRect;
}

/*
================
UiFrameCompact

The compact layout's surface uiEndWindow's fit uses.
================
*/
export interface UiFrameCompact {
	readonly top: number;
	readonly bounds: UiRect;
}

/*
================
UiFrame

Everything one frame's primitives draw with: the quad, control, block
and path arrays, the viewport and its full rect, the resource and text
owners, the placement owner, the window admission state and the
interaction state the button faces read.
================
*/
export interface UiFrame {
	quads: UiQuad[];
	controls: UiControl[];
	blocks: UiRect[];
	paths: string[];
	readonly full: UiRect;
	readonly width: number;
	readonly height: number;
	readonly text: UiFrameText;
	readonly resources: UiFrameResources;
	readonly placement: UiFramePlacement;
	readonly contentImages: ReadonlySet<string>;
	readonly windowMissing: string[];
	readonly admittedWindows: Map<string, UiAdmittedWindow>;
	readonly windowPaths: readonly string[];
	readonly fontPath: string | null;
	readonly phase: string;
	readonly compact: UiFrameCompact | null;
	readonly compactCloseControl: ( control: UiControl, screen: UiRect ) => UiControl;
	readonly compactWindowDrag: ( control: UiControl ) => boolean;
	readonly chrome: UiFrameChrome;
	pressed: string | null;
	hover: string | null;
	focus: string | null;
}

/*
================
UiWindowMark

The array lengths uiBeginWindow records, so uiEndWindow can withhold
or cache exactly what one window drew.
================
*/
export type UiWindowMark = readonly [number, number, number, number];

/*
================
uiWindowOrigin
================
*/
export function uiWindowOrigin(
	frame: UiFrame,
	name: string,
	initial: UiRect,
	id = "window-drag:" + name,
	options: { drag?: UiRect; nativeExtent?: readonly [number, number]; } = {}
): UiRect {
	const r = frame.placement.frame( id, initial, [ frame.width, frame.height ], options.nativeExtent );
	const drag = options.drag;
	frame.controls.push( {
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
uiRect
================
*/
export function uiRect(
	frame: UiFrame,
	r: UiRect,
	color: readonly [number, number, number, number],
	textureId = "",
	uv: UiRect = [ 0, 0, 1, 1 ],
	clip: UiRect = frame.full
): void {
	if ( r[2] > 0 && r[3] > 0 ) frame.quads.push( { rect: r, color, texture: textureId, uv, clip } );
}

// CIFStateSlot children render after the icon: the admittance overlay
// (6E8410), then the gauge at {0, cell, cell, 4} (6E7FA0).
/*
================
uiBuffViewer
================
*/
export function uiBuffViewer(
	frame: UiFrame,
	prefix: string,
	icons: readonly BuffViewerIcon[],
	ox: number,
	oy: number
): void {
	for ( const icon of icons ) {
		const r: UiRect = [ ox + icon.x, oy + icon.y, icon.size, icon.size ],
			gauge: UiRect = [ r[0], r[1] + icon.size, icon.size, 4 ];
		for ( const layer of [ icon.path, ...icon.overlay ] ) {
			if ( layer ) {
				frame.paths.push( layer );
				if ( frame.resources.has( layer ) ) uiRect( frame, r, white, layer );
			}
		}
		for ( const layer of icon.gauge ) {
			frame.paths.push( layer );
			if ( frame.resources.has( layer ) ) uiRect( frame, gauge, white, layer );
		}
		frame.controls.push( {
			id: prefix + icon.id,
			label: icon.label,
			helpSource: icon.helpSource,
			kind: "region",
			rect: r
		} );
		frame.blocks.push( r );
	}
}

/*
================
uiImage
================
*/
export function uiImage(
	frame: UiFrame,
	r: UiRect,
	path: string,
	color: UiQuad["color"] = white,
	uv: UiRect = [ 0, 0, 1, 1 ],
	clip: UiRect = frame.full
): void {
	frame.paths.push( path );
	if ( frame.resources.has( path ) ) uiRect( frame, r, color, path, uv, clip );
}

/*
================
uiAdditiveImage

An effect sheet without alpha, drawn additively: black adds nothing. INFERENCE: the
native effect statics (ga_effect_*, alcm_effect_prepare) are opaque DXT1 art, so the
original can only show them by adding them to what is behind.
================
*/
export function uiAdditiveImage(
	frame: UiFrame,
	r: UiRect,
	path: string,
	uv: UiRect = [ 0, 0, 1, 1 ]
): void {
	frame.paths.push( path );
	if ( frame.resources.has( path ) ) {
		frame.quads.push( { rect: r, color: white, texture: path, uv, clip: frame.full, additive: true } );
	}
}

/*
================
uiLabel
================
*/
export function uiLabel(
	frame: UiFrame,
	value: string,
	x: number,
	y: number,
	color: readonly [number, number, number, number] = white,
	clip: UiRect = frame.full
): void {
	frame.quads.push(
		...frame.text.quads( value, [ x, y, Math.max( 0, clip[0] + clip[2] - x ), 22 ], clip, color, {
			vAlign: 0
		} )
	);
}

// A window is one resource admission. Keep demand while withholding BOTH
// pixels and hit areas, so a cold open cannot expose an empty clickable shell.
/*
================
uiBeginWindow
================
*/
export function uiBeginWindow( frame: UiFrame ): UiWindowMark {
	const mark = [
		frame.quads.length,
		frame.controls.length,
		frame.blocks.length,
		frame.paths.length
	] as const;
	frame.paths.push( ...frame.windowPaths );
	return mark;
}

/*
================
uiEndWindow
================
*/
export function uiEndWindow(
	frame: UiFrame,
	mark: UiWindowMark,
	owner = "primary",
	// The window's own rect, when its quads paint past it through a
	// clip (the world map's page tiles): the fit measures this
	// instead of every quad's raw extent.
	source?: UiRect
): void {
	const admittedWindow = frame.admittedWindows.get( owner );
	// Everything still loading is reported (tests wait on it before a raster);
	// only the window's own art holds its admission.
	const unloaded = frame.paths.slice( mark[3] ).filter( path => !frame.resources.has( path ) );
	frame.windowMissing.push( ...unloaded );
	const gaps = unloaded.filter( path => !frame.contentImages.has( path ) );
	const key = frame.phase + ":" + frame.width + ":" + frame.height;
	if ( !frame.fontPath || !frame.resources.has( frame.fontPath ) || gaps.length ) {
		frame.quads.length = mark[0];
		frame.controls.length = mark[1];
		frame.blocks.length = mark[2];
		// Keep a fully admitted popup until its replacement is ready. Old image
		// demand stays alive; stale content controls cannot dispatch new-tab actions.
		if ( admittedWindow?.key === key ) {
			frame.quads.push( ...admittedWindow.quads );
			frame.blocks.push( ...admittedWindow.blocks );
			frame.paths.push( ...admittedWindow.paths );
			frame.controls.push(
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
		if ( frame.compact ) {
			const windowBounds = owner === "item-mall" || owner === "compact-guild" || frame.compact.top < 200 ?
				frame.full :
				frame.compact.bounds;
			fitUiGroup( frame.quads, frame.controls, mark[0], mark[1], windowBounds, frame.full, {
				blocks: frame.blocks,
				firstBlock: mark[2],
				disableDrag: frame.compactWindowDrag,
				...(source ? { sourceBounds: source } : {})
			} );
			for ( let i = mark[1]; i < frame.controls.length; i++ ) {
				frame.controls[i] = frame.compactCloseControl( frame.controls[i]!, windowBounds );
			}
		}
		const painted = resolveTextOverlaps( frame.quads.slice( mark[0] ) );
		frame.quads.length = mark[0];
		frame.quads.push( ...painted );
		frame.admittedWindows.set( owner, {
			key,
			quads: painted,
			controls: frame.controls.slice( mark[1] ),
			blocks: frame.blocks.slice( mark[2] ),
			paths: frame.paths.slice( mark[3] )
		} );
	}
}

/*
================
uiWindowBox
================
*/
export function uiWindowBox(
	frame: UiFrame,
	title: string,
	x: number,
	y: number,
	width: number,
	height: number
): void {
	const r: UiRect = [ x, y, width, height ];
	frame.blocks.push( r );
	uiRect( frame, r, [ .06, .075, .065, .96 ] );
	frame.quads.push(
		...frameRing(
			r,
			frame.chrome.frame,
			frame.chrome.frameParts.map( part => frame.resources.size( frame.chrome.frame + part + ".png" ) ),
			frame.full
		)
	);
	frame.quads.push(
		...frame.text.quads( title, [ x + 10, y + 12, width - 21, 12 ], frame.full, white, {
			hAlign: 1,
			vAlign: 1
		} )
	);
}

/*
================
uiButton
================
*/
export function uiButton(
	frame: UiFrame,
	id: string,
	value: string,
	x: number,
	y: number,
	width = 120,
	disabled = false,
	selected = false,
	clientTop?: number
): void {
	const r: UiRect = [ x, y, width, 24 ];
	frame.blocks.push( r );
	frame.controls.push( { id, label: value, rect: r, kind: "button", disabled, selected } );
	const down = !disabled && ((frame.pressed === id && frame.hover === id) || selected),
		chrome = frame.chrome,
		path = disabled ?
			chrome.buttonDisable :
			down ?
			chrome.buttonPress :
			((frame.pressed === null && frame.hover === id) || frame.focus === id) ?
			chrome.buttonFocus :
			chrome.button;
	if (
		[ chrome.button, chrome.buttonFocus, chrome.buttonPress, chrome.buttonDisable ]
			.every( frame.resources.has )
	) {
		uiRect( frame, r, white, path );
	}
	frame.quads.push(
		...frame.text.quads(
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
uiCloseButton
================
*/
export function uiCloseButton( frame: UiFrame, x: number, y: number, id: string ): void {
	const r: UiRect = [ x, y, 16, 16 ];
	frame.blocks.push( r );
	frame.controls.push( { id, label: "Close", rect: r, kind: "button" } );
	const chrome = frame.chrome;
	if (
		frame.resources.has( chrome.close ) && frame.resources.has( chrome.closePress ) &&
		frame.resources.has( chrome.closeFocus )
	) {
		uiRect(
			frame,
			r,
			white,
			frame.pressed === id ? chrome.closePress : frame.hover === id ? chrome.closeFocus : chrome.close
		);
	}
}

/*
================
uiComboBox
================
*/
export function uiComboBox(
	frame: UiFrame,
	r: UiRect,
	id: string,
	label: string,
	value: string,
	disabled = false
): void {
	const chrome = comboBoxChrome(
		r,
		frame.resources.size,
		frame.full,
		!disabled && frame.pressed === id ? 2 : !disabled && frame.hover === id ? 1 : 0
	);
	frame.paths.push( ...chrome.paths );
	frame.quads.push( ...chrome.quads );
	frame.quads.push( ...frame.text.quads( value, chrome.textRect, r, white, { hAlign: 1, vAlign: 1 } ) );
	frame.controls.push( { id, label, rect: r, kind: "button", disabled } );
}
