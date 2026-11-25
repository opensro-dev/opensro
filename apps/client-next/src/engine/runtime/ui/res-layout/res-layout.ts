/*
===========================================================================

res-layout.ts - one res_ui layout instance, drawn and clicked

The layout runtime (issue #4245, its stage 2): loads one layout the
asset step published (foundation decodes it), keeps the per-control
state the owner drives - show/hide, text, tint, image, button states,
checked, interaction id, label, shift, ellipsis, rect - and draws the
whole window into the coordinator's frame. Radio semantics are the
later client's own: a control draws its authored art until the owner
replaces it, and a hidden control hides its whole subtree.

Nine-piece frames draw through foundation's frame ring (its own
parts order); plain textures draw as one image clamped 1:1 at their
authored rect; tiles repeat through CIFNormalTile's rule. Buttons
draw their authored base art with whichever _focus/_press faces the
owner armed with setStates, and emit a click control when the owner
gave them an interaction id; checkboxes draw the com_checkbutton
pair's on/off texture set by setChecked; text steps draw with the
authored colour until setTint replaces it.
===========================================================================
*/

import type { UiQuad, UiRect, UiControl } from "@/engine/contracts/ui";
import { frameParts, frameRing } from "@/engine/foundation/ui/frame-ring";
import { normalTile } from "@/engine/foundation/ui/normal-tile";
import {
	resLayoutFrameParts,
	resLayoutFramePrefix,
	resLayoutImagePath,
	type ResLayout,
	type ResLayoutControl
} from "@/engine/foundation/ui/res-layout";
import type { UiFrame } from "./../frame/frame-contract";

// The layout's own image step (the frame contract's plain image rule,
// inline so this owner does not import frame internals: name the path
// for loading, draw only while resident).
function layoutImage(
	frame: UiFrame,
	r: UiRect,
	path: string,
	tint: readonly [number, number, number, number] | null | undefined
) {
	if ( !path ) return;
	frame.paths.push( path );
	if ( frame.resources.has( path ) ) {
		frame.quads.push( {
			rect: r,
			texture: path,
			uv: [ 0, 0, 1, 1 ],
			clip: frame.full,
			color: tint ? [ tint[0] ?? 1, tint[1] ?? 1, tint[2] ?? 1, tint[3] ?? 1 ] : frame.white
		} );
	}
}

// Later-client control types (the .2dt type word, the import's own
// vocabulary): 0 window, 1 framed window, 2 tile, 4 button, 5 text,
// 8 named slot, 11 checkbox, 13 scrollbar. Every control still draws
// by these rules; unknown types draw as images.
export const RES_LAYOUT_TYPE_WINDOW = 0;
export const RES_LAYOUT_TYPE_FRAME = 1;
export const RES_LAYOUT_TYPE_TILE = 2;
export const RES_LAYOUT_TYPE_BUTTON = 4;
export const RES_LAYOUT_TYPE_TEXT = 5;
export const RES_LAYOUT_TYPE_SLOT = 8;
export const RES_LAYOUT_TYPE_CHECKBOX = 11;
export const RES_LAYOUT_TYPE_SCROLLBAR = 13;

// A framed root window's flat body, the colour frame.ts windowChrome
// lays under every authored window's ring.
export const RES_LAYOUT_WINDOW_BODY: readonly [number, number, number, number] = [ .06, .075, .065, .96 ];

/*
================
ResLayoutWindow

One live layout instance and its owners' view state: which controls
show, and the texts, images, tints, checked flags, interaction ids
and labels the owner armed them with.
================
*/
export interface ResLayoutWindow {
	readonly layout: ResLayout;
	show( key: string, visible?: boolean ): void;
	hide( key: string ): void;
	setText( key: string, text: string | null ): void;
	setTint( key: string, color: readonly [number, number, number, number] | null ): void;
	setImage( key: string, texture: string ): void;
	setStates( key: string, states: { readonly focus?: boolean; readonly press?: boolean; } | null ): void;
	setChecked( key: string, checked: boolean ): void;
	setId( key: string, id: string ): void;
	setLabel( key: string, label: string | null ): void;
	setShift( key: string, dx: number, dy: number ): void;
	setEllipsis( key: string, ellipsis: boolean ): void;
	setRect( key: string, rect: readonly [number, number, number, number] ): void;
	control( key: string ): ResLayoutControl | undefined;
	draw( frame: UiFrame, ox: number, oy: number ): void;
}

/*
================
createResLayoutWindow

State lives on the instance, never shared between windows: two
windows bound to the same layout draw independently.
================
*/
export function createResLayoutWindow( layout: ResLayout ): ResLayoutWindow {
	const hidden = new Map<string, boolean>();
	const shifts = new Map<string, readonly [number, number]>();
	const rects = new Map<string, readonly [number, number, number, number]>();
	const texts = new Map<string, string | null>();
	const tints = new Map<string, readonly [number, number, number, number] | null>();
	const images = new Map<string, string>();
	const stateOverlay = new Map<string, { readonly focus?: boolean; readonly press?: boolean; } | null>();
	const checkedState = new Map<string, boolean>();
	const ids = new Map<string, string>();
	const labels = new Map<string, string | null>();
	const ellipsis = new Set<string>();

	/*
================
	visible

	A control is visible unless the owner hid it or it sits under a
	hidden container: hides inherit down the tree.
	================
	*/
	const visible = ( control: ResLayoutControl ): boolean => {
		let node: ResLayoutControl | undefined = control;
		while ( node ) {
			if ( hidden.get( node.key ) === true ) return false;
			node = node.parent ? layout.controls[node.parent] : undefined;
		}
		return true;
	};
	const node = ( key: string ): ResLayoutControl => {
		const control = layout.controls[key];
		if ( !control ) throw Error( "Unknown res layout control " + key );
		return control;
	};
	/*
================
	placedRect

	The control's screen rect: authored absolute in layout space,
	offset to the draw origin, then the owner's shift.
	================
	*/
	const placedRect = ( control: ResLayoutControl, ox: number, oy: number ): UiRect => {
		const rect = rects.get( control.key ) ?? control.rect;
		const shift = shifts.get( control.key );
		return [
			ox + rect[0] - layout.root.rect[0] + (shift?.[0] ?? 0),
			oy + rect[1] - layout.root.rect[1] + (shift?.[1] ?? 0),
			rect[2],
			rect[3]
		];
	};
	/*
================
	drawControl

	Draws one control (and nothing else; the window draws children by
	key order). Buttons and checkboxes emit their click control when
	the owner gave them an interaction id.
	================
	*/
	const drawControl = ( frame: UiFrame, control: ResLayoutControl, ox: number, oy: number ) => {
		const paths = frame.paths, controls = frame.controls, resources = frame.resources, full = frame.full;
		const r = placedRect( control, ox, oy ), tint = tints.get( control.key );
		const id = ids.get( control.key );
		if (
			control.parent === null || control.type === RES_LAYOUT_TYPE_WINDOW || control.type === RES_LAYOUT_TYPE_FRAME
		) {
			// resLayoutFramePrefix drops the closing ".png"; with it kept, the
			// ring's quads named "mframe_wnd_.pngleft_up.png" (#4417).
			const prefix = resLayoutFramePrefix( control.frame );
			if ( prefix ) {
				paths.push( ...resLayoutFrameParts( control.frame ) );
				// The ring never fills its interior: a framed root window
				// lays the same flat body the authored windows do
				// (frame.ts windowChrome) under it.
				if ( control.parent === null && r[2] > 0 && r[3] > 0 ) {
					frame.quads.push( {
						rect: r,
						color: RES_LAYOUT_WINDOW_BODY,
						texture: "",
						uv: [ 0, 0, 1, 1 ],
						clip: full
					} );
				}
				frame.quads.push(
					...frameRing(
						r,
						prefix,
						frameParts().map( part => resources.size( prefix + part + ".png" ) ),
						full
					)
				);
			}
			// The authored caption is the Korean archive text; the owner
			// replaces it with its copy (setText on the window's key).
			const caption = texts.get( control.key ) ?? control.text;
			if ( control.type === RES_LAYOUT_TYPE_WINDOW && caption ) {
				frame.quads.push(
					...frame.text.quads(
						caption.trim(),
						[ r[0] + 10, r[1] + 7, r[2] - 20, 12 ],
						full,
						frame.white,
						{ hAlign: 1, vAlign: 0 }
					)
				);
			}
		} else if ( control.type === RES_LAYOUT_TYPE_TEXT ) {
			const value = texts.get( control.key ) ?? control.text;
			if ( value ) {
				const color = tint ?? controlColor( control.color );
				frame.quads.push(
					...frame.text.quads( value, r, full, color, {
						vAlign: 1,
						...(ellipsis.has( control.key ) ? { overflow: "ellipsis" as const } : {})
					} )
				);
			}
		} else if ( control.type === RES_LAYOUT_TYPE_TILE ) {
			// A tile's art is authored in the record's frame slot (the
			// importer reads the two slots apart, scripts/res_ui_layout.py);
			// the texture slot is the fallback. A tile with no art in
			// either draws nothing. Naming "" for loading would hold
			// the whole window unadmitted: an empty path never becomes
			// resident (frameDraw.unloadedImages).
			const path = resLayoutImagePath( control.frame || control.texture );
			if ( path ) {
				paths.push( path );
				frame.quads.push( ...normalTile( r, path, resources.size( path ), full ) );
			}
		} else if ( control.type === RES_LAYOUT_TYPE_BUTTON ) {
			const overlay = stateOverlay.get( control.key );
			const pressed = overlay?.press ?? (frame.pressed === id && id !== undefined);
			const focused = overlay?.focus ?? (!!id && frame.hover === id);
			const override = images.get( control.key );
			const stem = override ? override : control.texture;
			const path = buttonFacePath( stem, pressed ? "_press" : focused ? "_focus" : "" );
			layoutImage( frame, r, path, tint );
			if ( id ) controls.push( hitControl( control, id, r ) );
		} else if ( control.type === RES_LAYOUT_TYPE_CHECKBOX ) {
			const checked = checkedState.get( control.key ) === true;
			const swappedStem = images.get( control.key );
			const stem = swappedStem ? swappedStem : control.texture;
			if ( checked ) {
				const onPath = resLayoutImagePath( stem.replace( "_off", "_on" ) );
				layoutImage( frame, r, onPath, tint );
			} else if ( stem ) {
				layoutImage( frame, r, resLayoutImagePath( stem ), tint );
			}
			if ( id ) {
				controls.push( hitControl( control, id, r, checked ) );
			}
		} else {
			// Slot, scrollbar and unrecognised controls: their own art
			// (a slot's children draw immediately after their parent
			// in the window's control order).
			const stem = images.get( control.key ) ?? control.texture;
			if ( stem ) layoutImage( frame, r, resLayoutImagePath( stem ), tint );
			if ( id ) controls.push( hitControl( control, id, r ) );
		}
	};

	// The click control for one armed control; the owner's label rides
	// along only when it set one.
	const hitControl = ( control: ResLayoutControl, id: string, r: UiRect, selected?: boolean ): UiControl => {
		const binding = labels.get( control.key ), label = typeof binding === "string" && binding !== "" ? binding : id;
		return {
			id,
			label,
			rect: r,
			kind: "button" as const,
			...(selected === undefined ? {} : { selected })
		};
	};
	return {
		layout,
		show( key, visibleFlag = true ) {
			hidden.set( key, !visibleFlag );
		},
		hide( key ) {
			hidden.set( key, true );
		},
		setText( key, text ) {
			texts.set( key, text );
		},
		setTint( key, color ) {
			tints.set( key, color );
		},
		setImage( key, texture ) {
			images.set( key, texture );
		},
		setStates( key, states ) {
			stateOverlay.set( key, states );
		},
		setChecked( key, checked ) {
			checkedState.set( key, checked );
		},
		setId( key, id ) {
			ids.set( key, id );
		},
		setLabel( key, label ) {
			labels.set( key, label );
		},
		setShift( key, dx, dy ) {
			shifts.set( key, [ dx, dy ] );
		},
		setEllipsis( key, flag ) {
			if ( flag ) ellipsis.add( key );
			else ellipsis.delete( key );
		},
		setRect( key, rect ) {
			rects.set( key, [ rect[0], rect[1], rect[2], rect[3] ] );
		},
		control( key ) {
			return layout.controls[key];
		},
		draw( frame, ox, oy ) {
			for ( const key of layout.order ) {
				const control = layout.controls[key]!;
				if ( !visible( control ) ) continue;
				drawControl( frame, control, ox, oy );
			}
		}
	};
}

/*
================
controlColor

The authored ARGB u32 to the frame's own channel order.
================
*/
function controlColor( color: number ): UiQuad["color"] {
	return [ (color >>> 16 & 255) / 255, (color >>> 8 & 255) / 255, (color & 255) / 255, (color >>> 24 & 255) / 255 ];
}

/*
================
buttonFacePath

A button's art with the requested face, or its authored base when no
face exists (one face is the base art itself).
================
*/
function buttonFacePath( stem: string, face: "" | "_focus" | "_press" ): string {
	if ( !stem ) return "";
	if ( !face ) return resLayoutImagePath( stem );
	const cleaned = stem.toLowerCase().replace( /\.ddj$/, "" ).replace( /\.png$/, "" );
	return resLayoutImagePath( cleaned + face + ".png" );
}
