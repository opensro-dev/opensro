/*
===========================================================================

chat-link-hud.ts - the item links waiting in the chat editors

Port-only, not native (SRO_CHAT_ITEM_LINKS, owner decision 2026-11-07).
Alt+click on an item while the chat input or the Global Chatting box has
focus puts a link in the line being typed. The text holds one
CHAT_LINK_MARKER per link (chat-item-links.ts); this owner keeps, per
editor and in marker order, where each item is and the label the editor
draws in the marker's place. The hidden browser field holds the raw text,
so a backspace over the marker removes the whole token, and the edit that
removed it drops its link here.

===========================================================================
*/
import {
	CHAT_LINK_MARKER,
	CHAT_LINK_SOURCE,
	type ChatLinkRef,
	chatLinkLabel,
	markerCount,
	replaceChatMarkers,
	stripChatMarkers
} from "@/engine/foundation/gameplay/chat-item-links";

// The chat compose cap (CIFChatViewer_HandleChatInputKey 0x64 units); the
// marker is one unit of it.
const CHAT_LINE_LIMIT = 100;
// An avatar slot id names the costume's type (typeFlags >>> 11).
const AVATAR_TYPE_SHIFT = 11;

/*
================
ChatLink

One pending link: where the item is and what the editor shows for it.
================
*/
export interface ChatLink {
	readonly ref: ChatLinkRef;
	readonly label: string;
}

/*
================
ChatLinkItem

The fields of a held item a link reads.
================
*/
interface ChatLinkItem {
	readonly slot: number;
	readonly name?: string;
	readonly plus?: number;
	readonly typeFlags?: number;
}

/*
================
ChatLinkSources

The items a link may name, as the windows show them: the bag and worn
gear, the costume slots, the open warehouse, the open pet bag.
================
*/
export interface ChatLinkSources {
	readonly inventory?: readonly ChatLinkItem[];
	readonly avatarInventory?: readonly ChatLinkItem[];
	readonly storage?: readonly ChatLinkItem[];
	readonly pet?: readonly ChatLinkItem[];
	// The GID of the pet whose bag `pet` shows: a pet link names it, so
	// the server reads that pet's bag and not another companion's.
	readonly petGid?: number;
}

/*
================
chatLinkFromActivate

The link an Alt+click on control id would put in the line, or null when the
id names no held item. An avatar id names the costume type; the server
reads the costume by its slot.
================
*/
export function chatLinkFromActivate( id: string, sources: ChatLinkSources ): ChatLink | null {
	const colon = id.indexOf( ":" ), prefix = id.slice( 0, colon + 1 ), value = Number( id.slice( colon + 1 ) );
	if ( colon < 0 || !Number.isInteger( value ) ) return null;
	let source: number, item: ChatLinkItem | undefined;
	if ( prefix === "slot:" ) {
		source = CHAT_LINK_SOURCE.inventory;
		item = sources.inventory?.find( row => row.slot === value );
	} else if ( prefix === "storage-slot:" ) {
		source = CHAT_LINK_SOURCE.storage;
		item = sources.storage?.find( row => row.slot === value );
	} else if ( prefix === "cos-slot:" ) {
		source = CHAT_LINK_SOURCE.pet;
		item = sources.pet?.find( row => row.slot === value );
	} else if ( prefix === "avatar:" ) {
		source = CHAT_LINK_SOURCE.avatar;
		item = sources.avatarInventory?.find( row => (row.typeFlags ?? 0) >>> AVATAR_TYPE_SHIFT === value );
	} else return null;
	if ( !item ) return null;
	// A pet link names the pet whose bag is open (sources.petGid); a pet
	// ref without one names no pet and the server reads nothing.
	const ref: ChatLinkRef = source === CHAT_LINK_SOURCE.pet && sources.petGid !== undefined ?
		{ source, slot: item.slot, pet: sources.petGid } :
		{ source, slot: item.slot };
	return { ref, label: chatLinkLabel( item ) };
}

/*
================
createChatLinkHud
================
*/
// An editor without links: one shared empty list, read every frame.
const NO_LINKS: readonly ChatLink[] = [];
const NO_LABELS: readonly string[] = [];

export function createChatLinkHud() {
	const editors = new Map<string, readonly ChatLink[]>();
	// The labels each editor draws, kept beside its links so the per-frame
	// read allocates nothing.
	const labelsOf = new Map<string, readonly string[]>();
	/*
	================
	linksOf
	================
	*/
	function linksOf( editor: string ) {
		return editors.get( editor ) ?? NO_LINKS;
	}
	/*
	================
	setLinks
	================
	*/
	function setLinks( editor: string, links: readonly ChatLink[] ) {
		if ( !links.length ) {
			editors.delete( editor );
			labelsOf.delete( editor );
			return;
		}
		editors.set( editor, links );
		labelsOf.set( editor, links.map( link => link.label ) );
	}
	return {
		/*
		================
		insert

		Puts link's marker at caret. At the limit the oldest link and its
		marker go first; a line with no room for the marker takes nothing.
		================
		*/
		insert( editor: string, text: string, caret: number, link: ChatLink, limit: number ) {
			let links = [ ...linksOf( editor ) ];
			if ( links.length >= limit && links.length ) {
				// The oldest link is the first marker in the text.
				const first = text.indexOf( CHAT_LINK_MARKER );
				if ( first >= 0 ) {
					text = text.slice( 0, first ) + text.slice( first + 1 );
					if ( first < caret ) caret--;
				}
				links = links.slice( 1 );
			}
			if ( text.length + 1 > CHAT_LINE_LIMIT ) return { text, caret };
			const index = markerCount( text.slice( 0, caret ) );
			links.splice( index, 0, link );
			setLinks( editor, links );
			return { text: text.slice( 0, caret ) + CHAT_LINK_MARKER + text.slice( caret ), caret: caret + 1 };
		},
		/*
		================
		edited

		The editor's text changed from before to after: links whose markers
		the edit removed drop, and a marker the edit typed or pasted is no
		link, so it is taken out. Returns the text the editor keeps.
		================
		*/
		edited( editor: string, before: string, after: string ) {
			let head = 0;
			while ( head < before.length && head < after.length && before[head] === after[head] ) head++;
			let tail = 0;
			while (
				tail < before.length - head && tail < after.length - head &&
				before[before.length - 1 - tail] === after[after.length - 1 - tail]
			) tail++;
			const removed = before.slice( head, before.length - tail ),
				inserted = after.slice( head, after.length - tail );
			const links = [ ...linksOf( editor ) ];
			const at = markerCount( before.slice( 0, head ) ), gone = markerCount( removed );
			if ( gone ) {
				links.splice( at, gone );
				setLinks( editor, links );
			}
			if ( !inserted.includes( CHAT_LINK_MARKER ) ) return after;
			return after.slice( 0, head ) + stripChatMarkers( inserted ) + after.slice( after.length - tail );
		},
		/*
		================
		links
		================
		*/
		links( editor: string ): readonly ChatLinkRef[] {
			return linksOf( editor ).map( link => link.ref );
		},
		/*
		================
		labels
		================
		*/
		labels( editor: string ): readonly string[] {
			return labelsOf.get( editor ) ?? NO_LABELS;
		},
		/*
		================
		display

		What the editor draws for text: each marker as its label.
		================
		*/
		display( editor: string, text: string ) {
			return text.includes( CHAT_LINK_MARKER ) ?
				replaceChatMarkers( text, labelsOf.get( editor ) ?? NO_LABELS ) :
				text;
		},
		/*
		================
		clear

		The line was sent or given up.
		================
		*/
		clear( editor: string ) {
			setLinks( editor, NO_LINKS );
		}
	};
}
