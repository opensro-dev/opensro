/*
===========================================================================

frame.ts - draw primitives and window admission for one UI frame

UiFrame borrows the current frame's arrays and its UI resource owners.
The authored widgets (statics, labels, buttons and chrome placed by a node
of an if*.txt layout, and the simple message box) draw through the wider
UiAuthoredFrame.
The coordinator retains input and window lifetime; these functions append
draw commands and controls, and admit complete windows as one unit.

===========================================================================
*/
import type { UiRect, UiQuad } from "@/engine/contracts/ui";
import type { BuffViewerIcon } from "@/engine/foundation/ui/buff-viewer";
import type { CloseControlId, UiAuthoredFrame, UiFrame } from "./frame-contract";
import { closedPanel } from "@/engine/foundation/ui/panels";
import { resolveTextOverlaps } from "@/engine/foundation/rendering/ui-glyphs";
import { DESKTOP_HEIGHT, DESKTOP_WIDTH, fitUiGroup } from "@/engine/foundation/ui/layout";
import { frameRing } from "@/engine/foundation/ui/frame-ring";
import { comboBoxChrome } from "@/engine/foundation/ui/combo-box";
import { buttonAccess, buttonTextColor } from "@/engine/foundation/ui/button-state";
import {
	authoredClientRect,
	authoredPaintOrder,
	authoredRect,
	type AuthoredControl,
	type AuthoredLayout
} from "@/engine/foundation/ui/authored-layout";
import { barChrome } from "@/engine/foundation/ui/bar";
import { guideTokens, hasSml2Markup } from "@/engine/foundation/ui/guide-content";
import { skillPointReadouts } from "@/engine/foundation/ui/hud-readouts";
import { latticeCells } from "@/engine/foundation/ui/inventory-layout";
import { normalTile } from "@/engine/foundation/ui/normal-tile";
import { MESSAGE_FRAME, MESSAGE_TILE, partyProposalAssets } from "@/engine/foundation/ui/party-proposal";
import { stretchRing } from "@/engine/foundation/ui/stretch-ring";
import { captionWords, textLines } from "@/engine/foundation/ui/text-lines";
import { messageBoxLines, textMessageBoxLayout } from "@/engine/foundation/ui/text-message-box";

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

// ============================================================================

// A label closer than this to its window's left edge has left its window
// (the frame's border is about this wide; the Experimental "Fit window labels").
const WINDOW_LABEL_MARGIN = 6;
// A fitted button caption keeps this far inside each side of its plate.
const CAPTION_MARGIN = 4;
// The lattice cell of CIFLattice art (36 x 36 inventory squares).
const LATTICE_CELL = 36;
// The Yes/No pair's buttons are this wide (52E720).
const MESSAGE_BUTTON_WIDTH = 76;

/*
================
authoredImage
================
*/
export function authoredImage(
	frame: UiAuthoredFrame,
	{ node, ox, oy, path = node.texture, fraction = 1, tint = frame.white }: {
		node: AuthoredControl;
		ox: number;
		oy: number;
		path?: string;
		fraction?: number;
		tint?: UiQuad["color"];
	}
) {
	if ( !path ) return;
	const r = authoredRect( node, ox, oy );
	image( frame, {
		r: [ r[0], r[1], r[2] * fraction, r[3] ],
		path,
		color: tint,
		uv: [ node.uv[0], node.uv[1], node.uv[2] * fraction, node.uv[3] ]
	} );
}

/*
================
authoredText

A static's text in its client rect. clipToControl keeps it inside
that rect: true ends a long line with an ellipsis, "clip" cuts it
at the edge.
================
*/
export function authoredText(
	frame: UiAuthoredFrame,
	{ node, ox, oy, value, clipToControl = false, fontStyle = 0 }: {
		node: AuthoredControl;
		ox: number;
		oy: number;
		value: string;
		clipToControl?: boolean | "clip";
		fontStyle?: number;
	}
) {
	const quads = frame.quads, paths = frame.paths, text = frame.text, full = frame.full;
	const box = authoredClientRect( node, ox, oy );
	// SML2 markup (843100) renders through the text board's layout, never
	// as plain characters: the Item Mall subtitles and alchemy texts.
	if ( hasSml2Markup( value ) ) {
		// Markup the guide layout cannot read falls back to plain text.
		let tokens: ReturnType<typeof guideTokens> | null = null;
		try {
			tokens = guideTokens( value );
		} catch {
			tokens = null;
		}
		if ( tokens ) {
			const out = text.guide( tokens, box, clipToControl ? box : full, node.color, frame.resources.size );
			quads.push( ...out.quads );
			paths.push( ...out.paths );
			return;
		}
	}
	// Port-only (Experimental "Fit window labels", off is the original):
	// an authored label (the static carries a text key; value fields
	// with an empty authored text keep the native fitting) wider than
	// its box (the trade window's "Purchase amount", right-aligned in
	// 63 pixels, with a wider text face, or the consignment window's
	// "Large category", right-aligned at x = 74 in a 43-pixel box)
	// wraps onto two lines instead of running past its window's left
	// edge or into the next control. A right-aligned label's lines may
	// use the room to the window's left margin; the wrap width splits
	// the label near its middle (never under its longest word) so a
	// label whose room is the rest of the window still breaks in two
	// instead of ellipsizing.
	if ( !clipToControl && frame.fitWindowLabels() && node.text !== "" && value.includes( " " ) ) {
		const measure = ( line: string ) => text.run( line, 0, node.fontIndex ).width;
		const width = measure( value );
		if ( width > box[2] ) {
			const room = node.hAlign === 2 ?
					Math.max( box[2], box[0] + box[2] - (ox + WINDOW_LABEL_MARGIN) ) :
					box[2],
				balanced = Math.max(
					box[2],
					Math.ceil( width / 2 ),
					...value.split( " " ).map( measure )
				);
			// The balanced split first (its widest line stays farthest
			// from the window's edge), the room-wide split after it.
			for ( const wrapWidth of balanced < room ? [ balanced, room ] : [ room ] ) {
				const lines = textLines( value, wrapWidth, measure );
				if ( lines.length !== 2 || !lines.every( line => measure( line ) <= room ) ) continue;
				const lineHeight = text.extentHeight( node.fontIndex ?? 0 ),
					top = box[1] + (box[3] - 2 * lineHeight) / 2;
				lines.forEach( ( line, index ) =>
					quads.push(
						...text.quads(
							line,
							[ box[0], top + index * lineHeight, box[2], lineHeight ],
							full,
							node.color,
							{
								fontIndex: node.fontIndex,
								fontStyle,
								hAlign: node.hAlign,
								vAlign: 1
							}
						)
					)
				);
				return;
			}
		}
	}
	quads.push(
		...text.quads( value, box, clipToControl ? box : full, node.color, {
			fontIndex: node.fontIndex,
			fontStyle,
			hAlign: node.hAlign,
			vAlign: node.vAlign,
			...(clipToControl ?
				{ overflow: clipToControl === "clip" ? "clip" as const : "ellipsis" as const } :
				{})
		} )
	);
}

/*
================
authoredSkillPoints

A skill point count in its static: the first of skillPointReadouts
that fits the static's width (port-only shortening, hud-readouts.ts).
The underbar's GDR_STATIC_SP (48) and the restoration page's
GDR_SKILL_TEXT_SP_NUM (45) overflow at beta counts.
================
*/
export function authoredSkillPoints(
	frame: UiAuthoredFrame,
	{ node, ox, oy, points }: { node: AuthoredControl; ox: number; oy: number; points: number; }
) {
	const room = authoredClientRect( node, ox, oy )[2], readouts = skillPointReadouts( points );
	authoredText( frame, {
		node,
		ox,
		oy,
		value: readouts.find( value => frame.text.run( value, 0, node.fontIndex ).width <= room ) ??
			readouts.at( -1 )!
	} );
}

/*
================
authoredButton
================
*/
export function authoredButton(
	frame: UiAuthoredFrame,
	{ node, ox, oy, id, label, disabled = false }: {
		node: AuthoredControl;
		ox: number;
		oy: number;
		id: string;
		label: string;
		disabled?: boolean;
	}
) {
	const pressed = frame.pressed, hover = frame.hover, focus = frame.focus;
	const r = authoredRect( node, ox, oy ), family = frame.buttonFamily( node.texture );
	frame.paths.push( ...family );
	frame.controls.push( { id, label, rect: r, kind: "button", disabled } );
	frame.blocks.push( r );
	const state = disabled ?
		3 :
		pressed === id && hover === id ?
		2 :
		pressed === null && (hover === id || focus === id) ?
		1 :
		0;
	if ( family.every( frame.resources.has ) ) rect( frame, { r, color: frame.white, textureId: family[state]! } );
}

/*
================
authoredLabeledButton
================
*/
export function authoredLabeledButton(
	frame: UiAuthoredFrame,
	{ node, ox, oy, id, caption, disabled = false }: {
		node: AuthoredControl;
		ox: number;
		oy: number;
		id: string;
		caption: string;
		disabled?: boolean;
	}
) {
	// Port-only, not native (Experimental "Fit window labels", off is
	// the original): a button caption wider than its plate keeps the
	// leading words that fit ("Delete message" -> "Delete", "Check
	// for duplicate entries" -> "Check").
	const shown = frame.fitWindowLabels() ?
		captionWords(
			caption,
			authoredRect( node, ox, oy )[2] - 2 * CAPTION_MARGIN,
			line => frame.text.run( line, 0, node.fontIndex ).width
		) :
		caption;
	authoredButton( frame, { node, ox, oy, id, label: shown, disabled } );
	authoredText( frame, { node, ox, oy, value: shown } );
}

/*
================
authoredChrome

One node's own art. Resource sections are reverse-insertion lists
(9BD240), so a page's native child paint order is the reverse of
controlsByName (authoredPaintOrder), not numeric control ID.
================
*/
export function authoredChrome(
	frame: UiAuthoredFrame,
	{ node, ox, oy }: { node: AuthoredControl; ox: number; oy: number; }
) {
	const quads = frame.quads, paths = frame.paths, resources = frame.resources, full = frame.full;
	const PARTS = frame.chrome.parts;
	// CIFScrollManager inherits CIFFrame (6F3830), but some resources omit its skin.
	if ( node.type === "CIFScrollManager" && !node.texture ) return;
	const r = authoredRect( node, ox, oy );
	if (
		node.type === "CIFFrame" || node.type === "CIFSubFrame" || node.type === "CIF_NPCTalk" ||
		node.type === "CIFScrollManager"
	) {
		paths.push( ...PARTS.map( p => node.texture + p + ".png" ) );
		quads.push(
			...frameRing(
				r,
				node.texture,
				PARTS.map( p => resources.size( node.texture + p + ".png" ) ),
				full
			)
		);
		if ( node.text ) {
			quads.push(
				...frame.text.quads(
					frame.copy( node.text ),
					[ r[0] + 10, r[1] + 7, r[2] - 20, 12 ],
					full,
					node.color,
					{ hAlign: 1, vAlign: 0 }
				)
			);
		}
	} else if ( node.type === "CIFBarWnd" ) {
		const bar = barChrome( r, node.texture, resources.size, full );
		paths.push( ...bar.paths );
		quads.push( ...bar.quads );
		if ( node.text ) authoredText( frame, { node, ox, oy, value: frame.copy( node.text ) } );
	} else if ( node.type === "CIFStretchWnd" ) {
		const ring = stretchRing( r, node.texture, resources.size, full );
		paths.push( ...ring.paths );
		quads.push( ...ring.quads );
	} else if ( node.type === "CIFLattice" ) {
		for (
			const cell of latticeCells(
				r[0],
				r[1],
				Math.floor( r[2] / LATTICE_CELL ),
				Math.floor( r[3] / LATTICE_CELL )
			)
		) {
			const path = node.texture + cell.part + ".png";
			paths.push( path );
			if ( resources.has( path ) ) {
				rect( frame, { r: cell.rect, color: frame.white, textureId: path } );
			}
		}
	} else if ( node.type === "CIFNormalTile" ) {
		paths.push( node.texture );
		quads.push( ...normalTile( r, node.texture, resources.size( node.texture ), full ) );
	} else if ( node.type === "CIFStatic" || node.type === "CIFPartySlot" ) {
		if ( node.texture ) authoredImage( frame, { node, ox, oy } );
		if ( node.type === "CIFStatic" && node.text ) {
			authoredText( frame, { node, ox, oy, value: frame.copy( node.text ) } );
		}
	} else if ( node.type === "CIFPML" ) {
		const out = frame.text.guide(
			guideTokens( frame.copy( node.text ) ),
			r,
			r,
			node.color,
			resources.size,
			{
				align: node.hAlign === 1 ? "center" : node.hAlign === 2 ? "right" : "left",
				vAlign: node.vAlign
			}
		);
		quads.push( ...out.quads );
		paths.push( ...out.paths );
	}
}

/*
================
nativePage

Every node of a layout in native paint order, except the ids listed.
================
*/
export function nativePage(
	frame: UiAuthoredFrame,
	{ layout, px, py, exclude = [] }: { layout: AuthoredLayout; px: number; py: number; exclude?: readonly number[]; }
) {
	for ( const node of authoredPaintOrder( layout ) ) {
		if ( !exclude.includes( node.id ) ) authoredChrome( frame, { node, ox: px, oy: py } );
	}
}

/*
================
MessageBox

A simple message box's caption, body lines and buttons. single picks
CIFSimpleMessageBox type 0 (Confirm, the yes id) or 2 (Cancel, the no
id); without it the box is type 1 with Yes and No.
================
*/
export interface MessageBox {
	readonly title: string;
	readonly lines: readonly string[];
	readonly yes: string;
	readonly no: string;
	readonly yesDisabled?: boolean;
	readonly single?: "confirm" | "cancel";
}

/*
================
simpleMessageBox

The CIFSimpleMessageBox every confirmation or notice on the HUD
opens: the tiled CIFMessageBox client under its frame ring, the
caption, the body lines left at (30,65) and the centred Yes/No pair
(52E720), sized by its lines (textMessageBoxLayout). Its sisters draw
through this one owner so they share one look. The caller owns the
blocks and controls around it.
================
*/
export function simpleMessageBox( frame: UiAuthoredFrame, box: MessageBox ) {
	const quads = frame.quads, text = frame.text, resources = frame.resources, full = frame.full;
	const white: UiQuad["color"] = frame.white, PARTS = frame.chrome.parts;
	const lines = messageBoxLines( box.lines, value => text.run( value ).width );
	const layout = textMessageBoxLayout( frame.w, frame.h, lines.map( line => text.run( line ).width ) );
	frame.paths.push( ...partyProposalAssets() );
	quads.push(
		...normalTile( layout.background, MESSAGE_TILE, resources.size( MESSAGE_TILE ), full ),
		...frameRing(
			layout.frame,
			MESSAGE_FRAME,
			PARTS.map( p => resources.size( MESSAGE_FRAME + p + ".png" ) ),
			full
		),
		...text.quads( box.title, layout.title, full, white, { hAlign: 1, vAlign: 0 } )
	);
	for ( const [i, line] of lines.entries() ) {
		quads.push( ...text.quads( line, layout.lines[i]!, full, white, { vAlign: 0 } ) );
	}
	if ( box.single === "confirm" ) {
		button( frame, {
			id: box.yes,
			value: frame.copy( "UIIT_CTL_CONFIRM" ),
			x: layout.single[0],
			y: layout.single[1],
			width: MESSAGE_BUTTON_WIDTH,
			disabled: !!box.yesDisabled
		} );
	} else if ( box.single === "cancel" ) {
		button( frame, {
			id: box.no,
			value: frame.copy( "UIIT_CTL_CANCEL" ),
			x: layout.single[0],
			y: layout.single[1],
			width: MESSAGE_BUTTON_WIDTH
		} );
	} else {
		button( frame, {
			id: box.yes,
			value: frame.copy( "UIIT_CTL_YES" ),
			x: layout.accept[0],
			y: layout.accept[1],
			width: MESSAGE_BUTTON_WIDTH,
			disabled: !!box.yesDisabled
		} );
		button( frame, {
			id: box.no,
			value: frame.copy( "UIIT_CTL_NO" ),
			x: layout.refuse[0],
			y: layout.refuse[1],
			width: MESSAGE_BUTTON_WIDTH
		} );
	}
	return layout;
}
