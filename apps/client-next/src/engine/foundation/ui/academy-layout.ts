/*
===========================================================================

academy-layout.ts - the academy matching window (ifmentormatch)

Projects the matching window over the academy listing state: its chrome,
the search row (name edit, honor grade and recruitment combo boxes), the
sortable column heads, the listing slots and the page arrows. The window
owner in ui.ts feeds it the state and draws what it returns.

===========================================================================
*/
import { barChrome } from "./bar";
import { matchingSlots } from "./matching-slots";
import type { AuthoredControl, AuthoredLayout } from "./authored-layout";
import { authoredRect } from "./authored-layout";
import { frameParts, frameRing } from "./frame-ring";
import { normalTile } from "./normal-tile";
import { comboBoxChrome } from "./combo-box";
import { stretchRing } from "./stretch-ring";
import { fitLines, narrowFace } from "./text-lines";
import type { UiRect, UiQuad, UiControl } from "@/engine/contracts/ui";
import { academyCanJoin, type AcademyState } from "@/engine/foundation/gameplay/academy";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";

const ROOT = "/assets/images/Media_extracted/interface/ifcommon/";
// A fitted search label keeps this far from the control on its left.
const SEARCH_LABEL_GAP = 2;
// The pitch of a fitted search label's two lines (face 0 and 1 ink is
// 9 and 8 pixels tall).
const SEARCH_LABEL_PITCH = 10;

/*
================
AcademyFilter

The search row's state: the name typed, the picked honor grade (5 is
All) and recruitment kind (2 is All), and which combo box is open.
================
*/
export interface AcademyFilter {
	readonly name: string;
	readonly grade: number;
	readonly kind: number;
	readonly open: number;
}

/*
================
AcademyLayoutArgs

fitLabels is the Experimental "Fit window labels" row (port-only; off
draws the original): column heads wider than their plate take the narrow
face and the search labels fit the room left of them.
================
*/
export interface AcademyLayoutArgs {
	readonly data: {
		mentor: AuthoredLayout;
		mentorSlot: AuthoredLayout;
		strings: Readonly<Record<string, string>>;
	};
	readonly state: AcademyState;
	readonly selected: number;
	readonly x: number;
	readonly y: number;
	readonly clip: UiRect;
	readonly size: ( path: string ) => readonly [number, number] | undefined;
	readonly text: (
		s: string,
		r: UiRect,
		c: UiRect,
		color: UiQuad["color"],
		style?: GlyphStyle
	) => readonly UiQuad[];
	readonly measure: ( value: string, fontIndex: number ) => number;
	readonly hover: string | null;
	readonly pressed: string | null;
	readonly filter?: AcademyFilter;
	readonly level?: number;
	readonly fitLabels?: boolean;
}

/*
================
searchLabelRoom

The width a right-aligned search label may take: from its box's right
edge back to the search row control on its left (the honor grade label
reaches the name edit, the recruitment label the grade combo box).
================
*/
function searchLabelRoom( mentor: AuthoredLayout, node: AuthoredControl ): number {
	const right = node.rect[0] + node.rect[2];
	let left = 0;
	for ( const other of Object.values( mentor ) ) {
		if ( other === node || !other.name.startsWith( "GDR_MENTORMATCH_SEARCH_" ) ) continue;
		if ( other.type === "CIFFrame" || other.type === "CIFStatic" ) continue;
		const end = other.rect[0] + (other.rect[2] || other.size[0]);
		if ( end <= node.rect[0] && end > left ) left = end;
	}
	return right - left - SEARCH_LABEL_GAP;
}

/*
================
academyLayout
================
*/
export function academyLayout( args: AcademyLayoutArgs ) {
	const { data, state, selected, x, y, clip, hover, pressed, level } = args;
	// Property reads, not destructuring: the execution map follows a
	// callback through a record's property, not out of a binding pattern.
	const size = args.size, text = args.text;
	const filter = args.filter ?? { name: "", grade: 5, kind: 2, open: 0 };
	const quads: UiQuad[] = [], controls: UiControl[] = [], paths: string[] = [], white = [ 1, 1, 1, 1 ] as const;
	const image = ( path: string, r: UiRect ) => {
		paths.push( path );
		if ( size( path ) ) quads.push( { rect: r, clip, texture: path, uv: [ 0, 0, 1, 1 ], color: white } );
	};
	const bindings: Record<string, string> = {
		GDR_MENTORMATCH_JOIN_BTN: "academy-join",
		GDR_MENTORMATCH_UPDATE_BTN: "academy-refresh",
		GDR_MENTORMATCH_SEARCH_BTN: "academy-search",
		GDR_MENTORMATCH_WHISPER_BTN: "academy-whisper"
	};
	const background = ( type: string ) => [ "CIFFrame", "CIFStretchWnd", "CIFNormalTile" ].includes( type ) ? 0 : 1;
	for ( const node of Object.values( data.mentor ).sort( ( a, b ) => background( a.type ) - background( b.type ) ) ) {
		const r = authoredRect( node, x, y );
		if (
			node.name.includes( "REGISTER" ) && node.type !== "CIFButton" || node.name.includes( "JOIN_PROGRESS" ) ||
			node.type === "CIFMentorMatchSlot"
		) {
			continue;
		}
		if ( node.type === "CIFStretchWnd" ) {
			const ring = stretchRing( r, node.texture, size, clip );
			paths.push( ...ring.paths );
			quads.push( ...ring.quads );
		} else if ( node.type === "CIFFrame" ) {
			const parts = frameParts().map( p => node.texture + p + ".png" );
			paths.push( ...parts );
			quads.push( ...frameRing( r, node.texture, parts.map( size ), clip ) );
		} else if ( node.type === "CIFNormalTile" ) {
			paths.push( node.texture );
			quads.push( ...normalTile( r, node.texture, size( node.texture ), clip ) );
		} else if ( node.type === "CIFButton" ) {
			const header = node.name.startsWith( "GDR_MENTORMATCH_SLOTLIST_" ),
				id = bindings[node.name] ??
					(header ? "academy-sort:" + node.name.replace( "GDR_MENTORMATCH_SLOTLIST_", "" ) : undefined),
				disabled = !id || !!state.request ||
					id === "academy-join" && !academyCanJoin( state, selected, level ) ||
					id === "academy-whisper" && !state.rows.some( row => row.id === selected );
			const variants = (header ? [ "", "_focus", "_press", "" ] : [ "", "_focus", "_press", "_disable" ]).map(
				s => node.texture.replace( ".png", s + ".png" )
			);
			paths.push( ...variants );
			image( variants[disabled ? 3 : pressed === id ? 2 : hover === id ? 1 : 0]!, r );
			const caption = data.strings[node.text] ?? node.text;
			// Port-only (fitLabels): a head wider than its plate ("Graduates"
			// on 48 pixels runs into "Member") takes the narrow face.
			const fontIndex = args.fitLabels ?
				narrowFace( caption, r[2], node.fontIndex, args.measure ) :
				node.fontIndex;
			quads.push(
				...text( caption, r, r, disabled ? [ .5, .5, .5, 1 ] : node.color, { fontIndex, hAlign: 1, vAlign: 1 } )
			);
			if ( id ) controls.push( { id, label: caption, kind: "button", rect: r, disabled } );
		} else if ( node.type === "CIFStatic" || node.type === "CIFBarWnd" ) {
			if ( node.type === "CIFBarWnd" && node.texture ) {
				const bar = barChrome( r, node.texture, size, clip );
				paths.push( ...bar.paths );
				quads.push( ...bar.quads );
			}
			if ( node.texture && node.texture.endsWith( ".png" ) ) image( node.texture, r );
			if ( node.text && !node.name.endsWith( "DUMY" ) ) quads.push( ...staticText( args, node, r ) );
		}
	}
	for ( const { row, rect: r } of matchingSlots( data.mentor, "CIFMentorMatchSlot", state.rows, x, y ) ) {
		const [sx, sy] = r;
		// 675690 switches the bound row between normal and selected bar skins;
		// an unselected row is not an absent background.
		const prefix = ROOT + "com_bar01" + (row && row.id === selected ? "select" : "") + "_";
		const bar = barChrome( r, prefix, size, clip );
		paths.push( ...bar.paths );
		quads.push( ...bar.quads );
		if ( !row ) continue;
		const id = "academy-row:" + row.id;
		const values: Record<string, string> = {
			ID: String( row.id ),
			LEVEL: String( row.level ),
			NAME: row.name,
			SUBJECT: row.detail,
			STUDENT: String( row.graduates ),
			NUMBER: String( row.students ),
			GRADE_NUM: String( row.grade )
		};
		for ( const [suffix, value] of Object.entries( values ) ) {
			const n = data.mentorSlot["GDR_MENTORMATCH_SLOT_" + suffix];
			if ( n ) quads.push( ...text( value, authoredRect( n, sx, sy ), r, n.color ) );
		}
		controls.push( {
			id,
			label: row.name + ": " + row.detail,
			kind: "button",
			rect: r,
			selected: selected === row.id
		} );
	}
	const edit = authoredRect( data.mentor.GDR_MENTORMATCH_SEARCH_NAME_EDIT!, x, y );
	controls.push( {
		id: "academy-name",
		label: data.strings.UIIT_CTL_PARTYMATCH_PSEARCH_NAME ?? "Name",
		kind: "text",
		rect: edit,
		value: filter.name,
		maxLength: 64
	} );
	quads.push( ...text( filter.name, edit, edit, white, { overflow: "clip" } ) );
	for (
		const [id, dx, skin, disabled] of [
			[ "academy-prev", 360, "left", !!state.request || state.page === 0 ],
			[ "academy-next", 394, "right", !!state.request || state.page + 1 >= state.pages ]
		] as const
	) {
		const arrows = [ "", "_focus", "_press", "_disable" ].map( s => ROOT + "com_" + skin + "_arrow" + s + ".png" );
		paths.push( ...arrows );
		const r: UiRect = [ x + dx, y + 437, 16, 16 ];
		image( arrows[disabled ? 3 : pressed === id ? 2 : hover === id ? 1 : 0]!, r );
		controls.push( {
			id,
			label: skin === "left" ? "Previous page" : "Next page",
			kind: "button",
			rect: r,
			disabled
		} );
	}
	quads.push(
		...text( String( state.page + 1 ), [ x + 376, y + 441, 18, 12 ], clip, white, { fontIndex: 2, hAlign: 1 } )
	);
	for ( const which of [ 1, 2 ] ) {
		const node = data.mentor["GDR_MENTORMATCH_SEARCH_COMBOBOX" + which]!,
			r = authoredRect( node, x, y ),
			options = which === 1 ?
				[ "1", "2", "3", "4", "5", data.strings.UIIT_STT_OBJECTALL ?? "All" ] :
				[
					data.strings.UIIT_STT_TC_STUDENT ?? "Student",
					data.strings.UIIT_STT_TC_ASSISTANT_GUARDIAN ?? "Assistant guardian",
					data.strings.UIIT_STT_OBJECTALL ?? "All"
				];
		const combo = comboBoxChrome(
			r,
			size,
			clip,
			pressed === "academy-combo:" + which ? 2 : hover === "academy-combo:" + which ? 1 : 0
		);
		paths.push( ...combo.paths );
		quads.push( ...combo.quads );
		quads.push(
			...text( options[which === 1 ? filter.grade : filter.kind] ?? "", combo.textRect, r, white, {
				hAlign: 1,
				vAlign: 1
			} )
		);
		controls.push( {
			id: "academy-combo:" + which,
			label: which === 1 ? "Honor class" : "Recruitment type",
			kind: "button",
			rect: r
		} );
		if ( filter.open === which ) {
			const box: UiRect = [ r[0], r[1] + r[3], r[2], options.length * 20 ];
			quads.push( { rect: box, clip, texture: "", uv: [ 0, 0, 1, 1 ], color: [ 0, 0, 0, 1 ] } );
			options.forEach( ( value, i ) => {
				const row: UiRect = [ box[0], box[1] + i * 20, box[2], 20 ];
				quads.push( ...text( value, [ row[0] + 5, row[1], row[2] - 10, row[3] ], row, white, { vAlign: 1 } ) );
				controls.push( {
					id: "academy-option:" + which + ":" + i,
					label: value,
					kind: "button",
					rect: row,
					selected: i === (which === 1 ? filter.grade : filter.kind)
				} );
			} );
		}
	}
	return { quads, controls, paths };
}

/*
================
staticText

A static's label. Port-only (fitLabels): a right-aligned search label
wider than its 35-pixel box ("Honor Grade" over the name edit,
"Recruitment Condition" cut to "Recr...") takes the room back to the
control on its left, on one or two lines, in its face or the narrow one.
================
*/
function staticText( args: AcademyLayoutArgs, node: AuthoredControl, r: UiRect ): readonly UiQuad[] {
	const data = args.data, clip = args.clip, text = args.text, measure = args.measure;
	const value = data.strings[node.text] ?? node.text,
		style = { fontIndex: node.fontIndex, hAlign: node.hAlign, vAlign: node.vAlign };
	const search = !!args.fitLabels && node.hAlign === 2 && node.name.startsWith( "GDR_MENTORMATCH_SEARCH_" );
	if ( !search || measure( value, node.fontIndex ) <= r[2] ) return text( value, r, clip, node.color, style );
	const room = searchLabelRoom( data.mentor, node ), fitted = fitLines( value, room, node.fontIndex, measure );
	if ( !fitted ) return text( value, r, clip, node.color, style );
	const left = r[0] + r[2] - room, top = r[1] + r[3] / 2 - fitted.lines.length * SEARCH_LABEL_PITCH / 2;
	return fitted.lines.flatMap( ( line, index ) =>
		text( line, [ left, top + index * SEARCH_LABEL_PITCH, room, SEARCH_LABEL_PITCH ], clip, node.color, {
			...style,
			fontIndex: fitted.fontIndex,
			vAlign: 1
		} )
	);
}
