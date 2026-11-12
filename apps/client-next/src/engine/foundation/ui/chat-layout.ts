/*
===========================================================================

chat-layout.ts - native chat geometry and semantic message rows

The layout owns wrapping and maps every displayed continuation to its sender.
Draft selection remains with the HUD, and never sends a message by itself.
===========================================================================
*/
import { chatLineText, chatLineColor } from "./chat-presentation";
import { chatLineTime } from "./chat-time";
import { sealTier } from "../gameplay/item-seal";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { ChatLine } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect, UiControl } from "@/engine/contracts/ui";
import type { AuthoredLayout } from "./authored-layout";
import { chatScrollbar } from "./chat-scrollbar";
import { textLines } from "./text-lines";
import { STALL_CHAT_CHANNEL } from "@/engine/foundation/gameplay/stall";
import { type NoticeRowLink, noticeWrappedLinks, projectSpans } from "@/engine/foundation/gameplay/notice-links";
import { CHAT_LINK_MARKER, chatLinkSpans, replaceChatMarkers } from "@/engine/foundation/gameplay/chat-item-links";
import {
	isSunDropNoticeText,
	SEAL_NOTICE_COLOR_MOON,
	SEAL_NOTICE_COLOR_STAR,
	SEAL_NOTICE_COLOR_SUN,
	sealNoticeColorArgb
} from "@/engine/foundation/gameplay/system-notices";
// 6aeaa0 seeds two 56px rows; 6acd30 cycles 0..6. The bottom anchor is 52px.

/*
================
sunDropChatColor

Port-only, not native: Seal of Sun drop lines in the chat log draw in bold
gold instead of the standard channel-7 pink (0xffaec3).
================
*/
export function sunDropChatColor(): UiQuad["color"] {
	return [ 1, 215 / 255, 80 / 255, 1 ];
}

/*
================
CONTRIBUTOR_TAG / CONTRIBUTOR_TAG_COLOR

Port-only, not native (#3358): a line whose sender is in the shown
Contributor roster (contributor-titles.ts) carries this tag before the
sender. The tag is gold-orange (0xffb000) on every channel: the global
channel (6) already paints every line yellow (0xffff00), so a yellow
tag would vanish exactly where contributors are most visible. Drawn
in the outline glyph style (CONTRIBUTOR_TAG_STYLE) it also stands
apart from the line's own text.
================
*/
export const CONTRIBUTOR_TAG = "[Contributor] ";
export const CONTRIBUTOR_TAG_COLOR = 0xffb000;
const CONTRIBUTOR_TAG_STYLE = { fontStyle: 2 } as const;

/*
================
isSunDropChatLine

Whether a channel-7 notice line announces a Seal of Sun drop: prefers the
decoded item link's seal when present and falls back to the notice text.
================
*/
export function isSunDropChatLine( line: Pick<ChatLine, "channel" | "text" | "notice"> ): boolean {
	if ( line.channel !== 7 || !line.text.includes( " has dropped " ) ) return false;
	const fields = line.notice?.item?.tooltip?.fields;
	if ( fields ) {
		const itemClass = fields.itemClass ?? 0;
		return sealTier( fields.rarity, itemClass ) === 2;
	}
	return isSunDropNoticeText( line.text );
}

/*
================
noticeColorQuad

An ARGB notice colour as a glyph colour.
================
*/
function noticeColorQuad( argb: number ): UiQuad["color"] {
	return [ ((argb >>> 16) & 255) / 255, ((argb >>> 8) & 255) / 255, (argb & 255) / 255, (argb >>> 24) / 255 ];
}

/*
================
sealNoticeChatColor

Port-only, not native (#1640): a drop or plus notice line's colour by
its item's seal tier (system-notices' seal colours). The decoded item
link's fields decide when present - plus lines colour the same - and
a drop line without them reads its tier off the text. Undefined keeps
the channel's own colour.
================
*/
export function sealNoticeChatColor(
	line: Pick<ChatLine, "channel" | "text" | "notice">
): UiQuad["color"] | undefined {
	if ( line.channel !== 7 ) return undefined;
	const fields = line.notice?.item?.tooltip?.fields;
	if ( fields ) {
		const argb = sealNoticeColorArgb( fields.rarity, fields.itemClass );
		if ( argb !== undefined ) return noticeColorQuad( argb );
	}
	if ( !line.text.includes( " has dropped " ) ) return undefined;
	if ( isSunDropNoticeText( line.text ) ) return noticeColorQuad( SEAL_NOTICE_COLOR_SUN );
	if ( line.text.includes( "(Seal Of Moon)" ) ) return noticeColorQuad( SEAL_NOTICE_COLOR_MOON );
	if ( line.text.includes( "(Seal Of Star)" ) ) return noticeColorQuad( SEAL_NOTICE_COLOR_STAR );
	return undefined;
}
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
	// Port-only adaptive layout; omitted retains the native desktop geometry.
	// bottom reserves pixels below chat; maxRows caps the native 56px row groups.
	compact?: { bottom: number; maxRows: number; };
	rows: number;
	tab: number;
	input: string;
	lines: readonly ChatLine[];
	// Port-only, not native (#3358): the shown Contributor roster; a
	// line whose sender is named here carries the contributor tag.
	// Omitted or empty keeps every line untagged.
	contributorTitles?: readonly string[];
	chatTimestamps?: boolean;
	// Port-only: link a drop/plus notice's player and item (Experimental
	// "Links in notices"); omitted or false keeps every line plain.
	noticeLinks?: boolean;
	// Port-only (SRO_CHAT_ITEM_LINKS, Experimental "Item links in chat"),
	// true only while the server switch and the row are both on: a player
	// line's item labels are hover regions showing the item's tooltip and
	// the input keeps its focus on Alt presses; omitted or false is native.
	chatItemLinks?: boolean;
	// Port-only (SRO_CHAT_ITEM_LINKS): the labels of the links in the input,
	// drawn where their markers sit (chat-link-hud.ts).
	inputLabels?: readonly string[];
	// Port-only, not native (#1976, Experimental "Event chat tab"): a fifth
	// tab holding the channel-7 event notices, which All then skips.
	// Omitted or false keeps the native four tabs, notices in All included.
	eventTab?: boolean;
	welcome: string;
	copy: ( key: string ) => string;
	size: ( path: string ) => readonly [number, number] | undefined;
	text: ( value: string, r: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ) => readonly UiQuad[];
	hover: string | null;
	pressed: string | null;
	offset?: number;
	hidden?: boolean;
	measure: ( value: string, style?: GlyphStyle ) => number;
	// Port-only (#3176): the caller's kept wraps, one (text, width,
	// fontStyle) per entry, so a rebuild wraps only the lines that are
	// new. The HUD owns one beside its retained layouts; omitted, every
	// line wraps on every rebuild. Entries are pure wraps of the input
	// measure, so a cache must never be shared across measures.
	wrapCache?: Map<string, readonly string[]>;
	editState?: {
		focused: boolean;
		start: number;
		end: number;
		composing: boolean;
		caretHeight?: number;
		caretVisible?: boolean;
	};
}
// The kind prefix of an item-link span (chat-item-links.ts chatLinkSpans).
const CHAT_ITEM_KIND = "chat-item:";
// The wraps one cache holds at most (#3176): the log keeps 128 lines, so
// this holds several widths' worth of the whole log before the oldest
// wraps age out.
const CHAT_WRAP_CACHE_MAX = 512;

/*
================
chatWrappedLines

One line's wrapped rows through the caller's wrap cache: a hit comes
back as wrapped before (and is refreshed as most-recent), a miss wraps
once and is kept. The key carries the style's fontStyle because the
measure depends on the style only through it.
================
*/
function chatWrappedLines(
	cache: Map<string, readonly string[]> | undefined,
	full: string,
	width: number,
	fontStyle: number,
	wrap: () => readonly string[]
): readonly string[] {
	if ( !cache ) return wrap();
	const key = fontStyle + ":" + width + ":" + full;
	const kept = cache.get( key );
	if ( kept ) {
		cache.delete( key );
		cache.set( key, kept );
		return kept;
	}
	const wrapped = wrap();
	if ( cache.size >= CHAT_WRAP_CACHE_MAX ) cache.delete( cache.keys().next().value! );
	cache.set( key, wrapped );
	return wrapped;
}

/*
================
ChatRowItemLink

One piece of an item link's label on a displayed row.
================
*/
type ChatRowItemLink = ReturnType<typeof projectSpans<`chat-item:${number}`>>[number][number];

/*
================
chatLayout

6ACD90 selects a chat row; 6AC0F0 builds the whisper draft. Preserve the
author across filtering, wrapping and scrolling instead of parsing captions.
================
*/
export function chatLayout( state: ChatLayoutInput ) {
	// The rows of a line without item links share one empty list (port-only
	// item links; one list per pass, not per row).
	const noItemLinks: readonly ChatRowItemLink[] = [];
	const {
		layout,
		width,
		height,
		rows: requestedRows,
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
	// The Event tab (eventTab) is index 4; a saved selection past the tabs
	// the row currently offers reads as the last offered tab.
	const tabCount = state.eventTab ? 5 : 4,
		activeTab = Math.min( Math.max( 0, tab ), tabCount - 1 );
	// Only the displayed rows change: resizing must not overwrite the saved size.
	const chatWidth = state.compact ? Math.max( 0, Math.min( 399, width ) ) : 399,
		bottom = state.compact ? Math.max( 0, Math.min( height - 20, state.compact.bottom ) ) : 52,
		rows = state.compact ?
			Math.max(
				0,
				Math.min(
					Math.floor( requestedRows ),
					Math.floor( state.compact.maxRows ),
					Math.floor( (height - bottom - 62) / 56 )
				)
			) :
			requestedRows,
		backgroundWidth = Math.max( 0, chatWidth - 18 ),
		textWidth = Math.max( 1, chatWidth - 34 ),
		tabPitch = state.compact ? Math.max( 0, Math.min( 51, (chatWidth - 46) / tabCount ) ) : 51,
		total = rows ? 62 + rows * 56 : 20,
		y = height - bottom - total;
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
		image( bg, [ 18, y + 21, backgroundWidth, 4 ], layout.GDR_CHAT_BG_UP!.uv );
		image( bg, [ 18, y + 25, backgroundWidth, rows * 56 + 12 ], layout.GDR_CHAT_BG_MID!.uv );
		image( bg, [ 18, y + 37 + rows * 56, backgroundWidth, 4 ], layout.GDR_CHAT_BG_DOWN!.uv );
		button( "chat-whispers", "Whisper list", layout.GDR_BUTTON_WHISPERLIST!.texture, [ 15, y, 16, 20 ] );
		button( "chat-hide", "See/hide chatting", layout.GDR_BUTTON_CHATTABHIDE!.texture, [ 30, y, 16, 20 ] );
		// The native four captions come from the string table; the port-only
		// Event tab has no native string and captions itself.
		const keys: readonly (string | null)[] = [
			"UIIT_CTL_CHAT_ALL",
			"UIIT_CTL_PARTY",
			"UIIT_CTL_GUILD",
			"UIIT_STT_GUILD_RESPECT_ALLY",
			...(state.eventTab ? [ null ] : [])
		];
		if ( !hidden ) {
			keys.forEach( ( key, i ) => {
				const label = key === null ? "Event" : copy( key );
				const r: UiRect = [ 45 + i * tabPitch, y, tabPitch + 1, 20 ];
				image( "/assets/images/Media_extracted/interface/chattingwnd/chat_tab.png", r );
				const caption: UiRect = [ r[0] + 5, r[1] + 4, Math.max( 0, r[2] - 5 ), 14 ];
				quads.push(
					...text(
						label,
						caption,
						caption,
						i === activeTab ? white : [ 180 / 255, 180 / 255, 180 / 255, 1 ],
						{ hAlign: 1, fontStyle: i === activeTab ? 2 : 0 }
					)
				);
				controls.push( {
					id: "chat-tab:" + i,
					label,
					kind: "button",
					rect: r,
					selected: activeTab === i
				} );
				blocks.push( r );
				// The authored lamps cover the native four tabs only; the
				// Event tab selects by its bold white caption alone.
				if ( i === activeTab && i < 4 ) {
					const lamp = layout[
						[
							"GDR_STATIC_ALLLAMP",
							"GDR_STATIC_PARTYLAMP",
							"GDR_STATIC_GUILDLAMP",
							"GDR_STATIC_ALLYLAMP"
						][i]!
					]!;
					image( lamp.texture, [
						lamp.rect[0] + i * (tabPitch - 51),
						y + lamp.rect[1],
						lamp.rect[2],
						lamp.rect[3]
					] );
				}
			} );
		}
		const clip: UiRect = [ 27, y + 28, textWidth, rows * 56 + 7 ], channel = [ 1, 4, 5, 11 ][activeTab];
		const all = [
			...(activeTab === 0 && lines.length < 128 ?
				chatWrappedLines(
					state.wrapCache,
					welcome,
					textWidth,
					0,
					() => textLines( welcome, textWidth, measure, true )
				).map( value => ({
					value,
					tag: "",
					color: gold,
					style: undefined as GlyphStyle | undefined,
					recipient: undefined as string | undefined,
					sentAt: undefined as number | undefined,
					notice: undefined as ChatLine["notice"],
					links: [] as readonly NoticeRowLink[],
					itemLinks: noItemLinks,
					sequence: undefined as number | undefined
				}) ) :
				[]),
			// Stall lines belong to the stall window's own chat box. With the
			// Event tab offered, channel-7 event notices live only there:
			// All skips them and the native tabs never carried them.
			...lines.filter( l => {
				if ( l.channel === STALL_CHAT_CHANNEL ) return false;
				if ( !state.eventTab ) return activeTab === 0 || l.channel === channel;
				if ( activeTab === 4 ) return l.channel === 7;
				if ( activeTab === 0 ) return l.channel !== 7;
				return l.channel === channel;
			} ).flatMap(
				l => {
					const sun = isSunDropChatLine( l ),
						color = sealNoticeChatColor( l ) ?? chatLineColor( l.channel ),
						style: GlyphStyle | undefined = sun ? { fontStyle: 2 } : undefined,
						// Port-only, not native (#3358): the contributor tag
						// before a rostered sender, notices (channel 7)
						// carry no sender and are never tagged.
						tag = l.channel !== 7 && l.name !== "" && state.contributorTitles?.includes( l.name ) ?
							CONTRIBUTOR_TAG :
							"",
						// Port-only item links: each marker shows its item's label. A line
						// without a marker takes none of this path (the per-frame budget).
						raw = tag + chatLineText( l, copy ),
						labelled = raw.includes( CHAT_LINK_MARKER ) ?
							chatLinkSpans( raw, l.links, state.chatItemLinks === true ) :
							null,
						full = labelled?.text ?? raw,
						wrapped = chatWrappedLines(
							state.wrapCache,
							full,
							textWidth,
							style?.fontStyle ?? 0,
							() => textLines( full, textWidth, value => measure( value, style ), true )
						),
						links = noticeWrappedLinks( full, wrapped, state.noticeLinks ? l.notice : undefined ),
						itemLinks = labelled?.spans.length ? projectSpans( full, wrapped, labelled.spans ) : null;
					return wrapped.map( ( value, index ) => ({
						value,
						tag: index === 0 ? tag : "",
						color,
						style,
						recipient: l.channel !== 7 ? l.name : undefined,
						sentAt: l.sentAt,
						notice: state.noticeLinks ? l.notice : undefined,
						links: links[index] ?? ([] as readonly NoticeRowLink[]),
						itemLinks: itemLinks?.[index] ?? noItemLinks,
						sequence: l.sequence
					}) );
				}
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
		scrolling = { range: scroll.range, travel: scroll.travel, bounds: [ 0, y, chatWidth, total - 20 ] };
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
				helpText: state.chatTimestamps ? chatLineTime( line.sentAt ) || undefined : undefined
			} );
			if ( hover === id ) {
				quads.push( {
					texture: "",
					rect: [ r[0], r[1], measure( line.value, line.style ), 15 ],
					clip,
					uv: [ 0, 0, 1, 1 ],
					color: [ 1, 1, 1, 102 / 255 ]
				} );
			}
			// Port-only, not native (#3358): a tagged row draws its tag
			// in the tag colour and style, the rest in the line's own.
			if ( line.tag && line.value.startsWith( line.tag ) ) {
				const tagWidth = measure( line.tag, CONTRIBUTOR_TAG_STYLE );
				quads.push(
					...text(
						line.tag,
						r,
						clip,
						noticeColorQuad( 0xff000000 | CONTRIBUTOR_TAG_COLOR ),
						CONTRIBUTOR_TAG_STYLE
					)
				);
				quads.push(
					...text(
						line.value.slice( line.tag.length ),
						[ r[0] + tagWidth, r[1], r[2] - tagWidth, r[3] ],
						clip,
						line.color,
						line.style
					)
				);
			} else {
				quads.push( ...text( line.value, r, clip, line.color, line.style ) );
			}
			// Port-only notice links (notice-links.ts): the player's name whispers,
			// the item's name shows its tooltip; each underlines while hovered.
			if ( line.links.length > 0 && line.sequence !== undefined ) {
				for ( const link of line.links ) {
					const baseId = "notice-" + link.kind + ":" + line.sequence,
						linkId = link.part ? baseId + ":" + link.part : baseId,
						rect: UiRect = [
							r[0] + measure( line.value.slice( 0, link.at ), line.style ),
							r[1],
							measure( link.value, line.style ),
							14
						];
					controls.push( {
						id: linkId,
						label: link.value,
						kind: link.kind === "name" ? "button" : "region",
						rect,
						whisperTarget: link.kind === "name" ? (line.notice?.name ?? link.value) : undefined
					} );
					if ( hover === baseId || (hover !== null && hover.startsWith( baseId + ":" )) ) {
						quads.push( {
							texture: "",
							rect: [ rect[0], rect[1] + 13, rect[2], 1 ],
							clip,
							uv: [ 0, 0, 1, 1 ],
							color: line.color
						} );
					}
				}
			}
			// Port-only item links (chat-item-links.ts): a label shows the
			// linked item's tooltip and underlines while hovered.
			if ( line.sequence !== undefined ) {
				for ( const link of line.itemLinks ) {
					const baseId = "chat-item:" + line.sequence + ":" + link.kind.slice( CHAT_ITEM_KIND.length ),
						rect: UiRect = [
							r[0] + measure( line.value.slice( 0, link.at ), line.style ),
							r[1],
							measure( link.value, line.style ),
							14
						];
					controls.push( {
						id: link.part ? baseId + ":" + link.part : baseId,
						label: link.value,
						kind: "region",
						rect
					} );
					if ( hover === baseId || (hover !== null && hover.startsWith( baseId + ":" )) ) {
						quads.push( {
							texture: "",
							rect: [ rect[0], rect[1] + 13, rect[2], 1 ],
							clip,
							uv: [ 0, 0, 1, 1 ],
							color: line.color
						} );
					}
				}
			}
		}
		blocks.push( [ 18, y + 21, backgroundWidth, rows * 56 + 20 ] );
	}
	const inputY = height - bottom - 20;
	button( "chat-size", "Change chat size", layout.GDR_BTN_CHAT_SIZE!.texture, [ 0, inputY, 16, 20 ] );
	image( layout.GDR_CHAT_INPUTBOX!.texture, [ 18, inputY, backgroundWidth, 20 ], layout.GDR_CHAT_INPUTBOX!.uv );
	const edit: UiRect = [ 22, inputY + 3, Math.max( 0, chatWidth - 26 ), 14 ],
		start = Math.max( 0, Math.min( input.length, editState?.start ?? 0 ) ),
		end = Math.max( start, Math.min( input.length, editState?.end ?? start ) );
	// Port-only item links: a marker draws as its label (chat-link-hud.ts).
	const shown = ( value: string ) =>
		state.inputLabels?.length ? replaceChatMarkers( value, state.inputLabels ) : value;
	const before = measure( shown( input.slice( 0, start ) ) ),
		through = measure( shown( input.slice( 0, end ) ) ),
		scroll = editState?.focused ? Math.max( 0, through - edit[2] + 3 ) : 0;
	const ink = ( r: UiRect, color: UiQuad["color"] ) =>
		quads.push( { rect: r, clip: edit, texture: "", uv: [ 0, 0, 1, 1 ], color } );
	if ( editState?.focused && end > start ) {
		ink( [ edit[0] + before - scroll, edit[1], through - before, edit[3] ], [ .2, .4, .7, .6 ] );
	}
	quads.push(
		...text(
			shown( input ),
			[ edit[0] - scroll, edit[1], Math.max( edit[2], measure( shown( input ) ) ), edit[3] ],
			edit,
			white
		)
	);
	if ( editState?.focused ) {
		if ( editState.caretVisible !== false ) {
			ink( [ edit[0] + through - scroll, edit[1], 2, editState.caretHeight ?? 11 ], white );
		}
		if ( editState.composing ) {
			ink( [ edit[0] + before - scroll, edit[1] + edit[3] - 1, Math.max( 1, through - before ), 1 ], white );
		}
	}
	controls.push( {
		id: "chat-text",
		label: "Chat",
		kind: "text",
		rect: edit,
		value: input,
		maxLength: 100,
		...(state.chatItemLinks ? { keepFocusOnAltPress: true } : {})
	} );
	blocks.push( [ 18, inputY, backgroundWidth, 20 ] );
	return { quads, controls, paths, blocks, scrolling };
}
