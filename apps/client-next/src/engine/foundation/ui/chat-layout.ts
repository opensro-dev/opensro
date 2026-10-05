/*
===========================================================================

chat-layout.ts - native chat geometry and semantic message rows

The layout owns wrapping and maps every displayed continuation to its sender.
Draft selection remains with the HUD, and never sends a message by itself.
===========================================================================
*/
import { chatLineText, chatLineColor } from "./chat-presentation";
import { chatLineTime } from "./chat-time";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { ChatLine } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect, UiControl } from "@/engine/contracts/ui";
import type { AuthoredLayout } from "./authored-layout";
import { chatScrollbar } from "./chat-scrollbar";
import { textLines } from "./text-lines";
// 6aeaa0 seeds two 56px rows; 6acd30 cycles 0..6. The bottom anchor is 52px.
/*
================
ChatLayoutInput

Keep view state and rendering services together so wrapped rows retain the
sender supplied by the chat protocol, independently of displayed text.
================
*/
export interface ChatLayoutInput {
	layout: AuthoredLayout;
	width: number;
	height: number;
	rows: number;
	tab: number;
	input: string;
	lines: readonly ChatLine[];
	welcome: string;
	copy: ( key: string ) => string;
	size: ( path: string ) => readonly [number, number] | undefined;
	text: ( value: string, r: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ) => readonly UiQuad[];
	hover: string | null;
	pressed: string | null;
	offset?: number;
	hidden?: boolean;
	measure: ( value: string ) => number;
	editState?: {
		focused: boolean;
		start: number;
		end: number;
		composing: boolean;
		caretHeight?: number;
		caretVisible?: boolean;
	};
}
/*
================
chatLayout

6ACD90 selects a chat row; 6AC0F0 builds the whisper draft. Preserve the
author across filtering, wrapping and scrolling instead of parsing captions.
================
*/
export function chatLayout( state: ChatLayoutInput ) {
	const {
		layout,
		width,
		height,
		rows,
		tab,
		input,
		lines,
		welcome,
		hover,
		pressed,
		offset = 0,
		hidden = false,
		editState
	} = state;
	const copy = state.copy, size = state.size, text = state.text, measure = state.measure;
	const quads: UiQuad[] = [],
		controls: UiControl[] = [],
		paths: string[] = [],
		blocks: UiRect[] = [],
		white = [ 1, 1, 1, 1 ] as const,
		gold = [ 219 / 255, 201 / 255, 155 / 255, 1 ] as const,
		full: UiRect = [ 0, 0, width, height ];
	const total = rows ? 62 + rows * 56 : 20, y = height - 52 - total;
	let scrolling = { range: 0, travel: 0, bounds: [ 0, 0, 0, 0 ] as UiRect };
	/*
================
image
================
	*/
	function image( path: string, r: UiRect, uv: UiRect = [ 0, 0, 1, 1 ] ) {
		paths.push( path );
		if ( size( path ) ) quads.push( { texture: path, rect: r, uv, color: white, clip: full } );
	}
	/*
================
button
================
	*/
	function button( id: string, label: string, path: string, r: UiRect ) {
		const variants = [ "", "_focus", "_press" ].map( s => path.replace( ".png", s + ".png" ) );
		paths.push( ...variants );
		image( variants[pressed === id ? 2 : hover === id ? 1 : 0]!, r );
		controls.push( { id, label, rect: r, kind: "button" } );
		blocks.push( r );
	}
	if ( rows ) {
		const bg = layout.GDR_CHAT_BG_MID!.texture;
		image( bg, [ 18, y + 21, 381, 4 ], layout.GDR_CHAT_BG_UP!.uv );
		image( bg, [ 18, y + 25, 381, rows * 56 + 12 ], layout.GDR_CHAT_BG_MID!.uv );
		image( bg, [ 18, y + 37 + rows * 56, 381, 4 ], layout.GDR_CHAT_BG_DOWN!.uv );
		button( "chat-whispers", "Whisper list", layout.GDR_BUTTON_WHISPERLIST!.texture, [ 15, y, 16, 20 ] );
		button( "chat-hide", "See/hide chatting", layout.GDR_BUTTON_CHATTABHIDE!.texture, [ 30, y, 16, 20 ] );
		const keys = [ "UIIT_CTL_CHAT_ALL", "UIIT_CTL_PARTY", "UIIT_CTL_GUILD", "UIIT_STT_GUILD_RESPECT_ALLY" ];
		if ( !hidden ) {
			keys.forEach( ( key, i ) => {
				const r: UiRect = [ 45 + i * 51, y, 52, 20 ];
				image( "/assets/images/Media_extracted/interface/chattingwnd/chat_tab.png", r );
				const caption: UiRect = [ r[0] + 5, r[1] + 4, 47, 14 ];
				quads.push(
					...text(
						copy( key ),
						caption,
						caption,
						i === tab ? white : [ 180 / 255, 180 / 255, 180 / 255, 1 ],
						{ hAlign: 1, fontStyle: i === tab ? 2 : 0 }
					)
				);
				controls.push( {
					id: "chat-tab:" + i,
					label: copy( key ),
					kind: "button",
					rect: r,
					selected: tab === i
				} );
				blocks.push( r );
				if ( i === tab ) {
					const lamp = layout[
						[
							"GDR_STATIC_ALLLAMP",
							"GDR_STATIC_PARTYLAMP",
							"GDR_STATIC_GUILDLAMP",
							"GDR_STATIC_ALLYLAMP"
						][i]!
					]!;
					image( lamp.texture, [ lamp.rect[0], y + lamp.rect[1], lamp.rect[2], lamp.rect[3] ] );
				}
			} );
		}
		const clip: UiRect = [ 27, y + 28, 365, rows * 56 + 7 ], channel = [ 1, 4, 5, 11 ][tab];
		const all = [
			...(tab === 0 && lines.length < 128 ?
				textLines( welcome, 365, measure, true ).map( value => ({
					value,
					color: gold,
					recipient: undefined as string | undefined,
					sentAt: undefined as number | undefined
				}) ) :
				[]),
			...lines.filter( l => tab === 0 || l.channel === channel ).flatMap( l =>
				textLines( chatLineText( l, copy ), 365, measure, true ).map( value => ({
					value,
					color: chatLineColor( l.channel ),
					recipient: l.channel !== 7 ? l.name : undefined,
					sentAt: l.sentAt
				}) )
			)
		];
		const end = all.length - Math.min( Math.max( 0, offset ), Math.max( 0, all.length - rows * 4 ) ),
			display = hidden ? [] : all.slice( Math.max( 0, end - rows * 4 ), end );
		const scroll = chatScrollbar(
			"chat-scroll",
			[ 0, y + 36, 16, rows * 56 - 28 ],
			all.length,
			rows * 4,
			offset,
			size,
			full,
			hover,
			pressed
		);
		scrolling = { range: scroll.range, travel: scroll.travel, bounds: [ 0, y, 399, total - 20 ] };
		quads.push( ...scroll.quads );
		paths.push( ...scroll.paths );
		controls.push( ...scroll.controls );
		blocks.push( [ 0, y + 20, 16, rows * 56 + 4 ] );
		for ( let i = 0; i < display.length; i++ ) {
			const line = display[i]!,
				id = "chat-line:" + i,
				r: UiRect = [ clip[0], clip[1] + clip[3] - 7 - display.length * 14 + i * 14, clip[2], 14 ];
			// CIFListCtrl hot row: 14px pitch, 15px quad, ARGB 66FFFFFF. System messages do not enable this branch.
			controls.push( {
				id,
				label: line.value,
				kind: line.recipient ? "button" : "region",
				rect: r,
				whisperTarget: line.recipient,
				helpText: chatLineTime( line.sentAt ) || undefined
			} );
			if ( hover === id ) {
				quads.push( {
					texture: "",
					rect: [ r[0], r[1], measure( line.value ), 15 ],
					clip,
					uv: [ 0, 0, 1, 1 ],
					color: [ 1, 1, 1, 102 / 255 ]
				} );
			}
			quads.push( ...text( line.value, r, clip, line.color ) );
		}
		blocks.push( [ 18, y + 21, 381, rows * 56 + 20 ] );
	}
	const inputY = height - 72;
	button( "chat-size", "Change chat size", layout.GDR_BTN_CHAT_SIZE!.texture, [ 0, inputY, 16, 20 ] );
	image( layout.GDR_CHAT_INPUTBOX!.texture, [ 18, inputY, 381, 20 ], layout.GDR_CHAT_INPUTBOX!.uv );
	const edit: UiRect = [ 22, inputY + 3, 373, 14 ],
		start = Math.max( 0, Math.min( input.length, editState?.start ?? 0 ) ),
		end = Math.max( start, Math.min( input.length, editState?.end ?? start ) );
	const before = measure( input.slice( 0, start ) ),
		through = measure( input.slice( 0, end ) ),
		scroll = editState?.focused ? Math.max( 0, through - edit[2] + 3 ) : 0;
	const ink = ( r: UiRect, color: UiQuad["color"] ) =>
		quads.push( { rect: r, clip: edit, texture: "", uv: [ 0, 0, 1, 1 ], color } );
	if ( editState?.focused && end > start ) {
		ink( [ edit[0] + before - scroll, edit[1], through - before, edit[3] ], [ .2, .4, .7, .6 ] );
	}
	quads.push(
		...text( input, [ edit[0] - scroll, edit[1], Math.max( edit[2], measure( input ) ), edit[3] ], edit, white )
	);
	if ( editState?.focused ) {
		if ( editState.caretVisible !== false ) {
			ink( [ edit[0] + through - scroll, edit[1], 2, editState.caretHeight ?? 11 ], white );
		}
		if ( editState.composing ) {
			ink( [ edit[0] + before - scroll, edit[1] + edit[3] - 1, Math.max( 1, through - before ), 1 ], white );
		}
	}
	controls.push( { id: "chat-text", label: "Chat", kind: "text", rect: edit, value: input, maxLength: 100 } );
	blocks.push( [ 18, inputY, 381, 20 ] );
	return { quads, controls, paths, blocks, scrolling };
}
