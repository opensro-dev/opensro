/*
===========================================================================

npc-talk.ts - the NPC talk window (CIFNpcTalk) text box and menu

Lays out the prompt, the dialogue choices or the service menu (talk, shop
branches, warehouse, recall, teleport, end) in the authored talk box, with
the native 18-pixel line pitch, 19 visible lines and its scrollbar. Pure
geometry: the conversation itself belongs to the gameplay worker.

===========================================================================
*/
import { textLines, textBoxParagraphs } from "./text-lines";
import { chatScrollbar } from "./chat-scrollbar";
import type { NpcConversation } from "@/engine/foundation/gameplay/npc-dialogue";
import type { AuthoredLayout } from "./authored-layout";
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { MerchantBranch } from "@/engine/foundation/gameplay/merchant-branches";

/*
================
npcChoiceColor

5D31D0: signed difference of two level bytes. Only SN_ choices consult
the quest by-code table; missing records keep the ordinary menu color.
================
*/
export function npcChoiceColor(
	symbol: string,
	level: number,
	requiredLevel: ( code: string ) => number | undefined
): UiQuad["color"] {
	const required = symbol.startsWith( "SN_" ) ? requiredLevel( symbol.slice( 3 ) ) : undefined;
	if ( required !== undefined ) {
		const delta = (level & 255) - (required & 255);
		if ( delta >= 10 ) return [ 101 / 255, 175 / 255, 162 / 255, 1 ];
		if ( delta < 0 ) return [ 1, 74 / 255, 74 / 255, 1 ];
	}
	return [ 239 / 255, 218 / 255, 164 / 255, 1 ];
}

/*
================
npcDialogueCaption

Hash-bound executable literals use STT; shipped textuisystem only contains
CTL captions for these three identical actions. This is a named same-caption
compatibility mapping, not evidence that native TextManager aliases them.
================
*/
export function npcDialogueCaption( symbol: string, copy: ( symbol: string ) => string ): string {
	const value = copy( symbol );
	if ( value ) return value;
	switch ( symbol ) {
		case "UIIT_STT_YES":
			return copy( "UIIT_CTL_YES" );
		case "UIIT_STT_NO":
			return copy( "UIIT_CTL_NO" );
		case "UIIT_STT_CONFIRM":
			return copy( "UIIT_CTL_CONFIRM" );
		default:
			return "";
	}
}

/*
================
npcBranchLabel

Disambiguates identical branch labels (e.g. unpatched official English
"Purchase/sell/repair weapon") using regional group symbol conventions.
================
*/
export function npcBranchLabel(
	branch: MerchantBranch,
	branches: readonly MerchantBranch[],
	copy: ( symbol: string ) => string
): string {
	const label = copy( branch.labelSymbol );
	if ( branches.filter( b => copy( b.labelSymbol ) === label ).length > 1 ) {
		if ( branch.labelSymbol.includes( "_EU_" ) || branch.labelSymbol.includes( "EU_" ) ) {
			return `${label} (European)`;
		}
		if (
			branch.labelSymbol.includes( "_CH_" ) || branch.labelSymbol.includes( "CH_" ) ||
			branch.labelSymbol.includes( "_GROUP" )
		) return `${label} (Chinese)`;
	}
	return label;
}

/*
================
NpcTalkInput

One frame of the talk window: the conversation, the authored layout and
its origin, the text services, the pointer state and the NPC's service
menu. Service flags left out are off, except talk; portalRows replaces
the service menu with the teleport destinations. canReverseReturn adds the
reverse return's two destinations (5D4410, capability 0x20000000): the
beginner guides' free return, or a gate's while a scroll is held.
================
*/
export interface NpcTalkInput {
	readonly state: Exclude<NpcConversation, { phase: "closed"; }>;
	readonly layout: AuthoredLayout;
	readonly origin: readonly [number, number];
	readonly copy: ( symbol: string ) => string;
	readonly measure: ( text: string ) => number;
	readonly draw: ( text: string, rect: UiRect, clip: UiRect, color: UiQuad["color"] ) => UiQuad[];
	readonly size: ( path: string ) => readonly [number, number] | undefined;
	readonly hover: string | null;
	readonly pressed: string | null;
	readonly top: number;
	readonly canShop?: boolean;
	readonly branches?: readonly MerchantBranch[];
	readonly choiceColor?: ( symbol: string ) => UiQuad["color"];
	readonly canPortal?: boolean;
	readonly portalRows?: readonly { id: string; label: string; }[] | null;
	readonly canTalk?: boolean;
	readonly prompt?: string;
	readonly canRecall?: boolean;
	readonly canStorage?: boolean;
	// canMagicOption is the smith's row 0x2F (capability 0x80000000).
	readonly canMagicOption?: boolean;
	readonly canReverseReturn?: boolean;
	// 5D8FF0 0x800000: the fortress official's application row.
	readonly canFortressOfficial?: boolean;
	// jobRows are the job guild rows (job-guild.ts jobMenuRows).
	readonly jobRows?: readonly { readonly id: string; readonly label: string; }[];
}

/*
================
npcTalkLayout

5D4A10: font 0, 18px pitch, 19 visible lines. 5D5AE0 inserts the white
prompt and one spacer; 5D60C0 adds numbered interactive rows.
================
*/
export function npcTalkLayout( input: NpcTalkInput ) {
	const state = input.state,
		layout = input.layout,
		origin = input.origin,
		hover = input.hover,
		pressed = input.pressed;
	const copy = input.copy, measure = input.measure, draw = input.draw, size = input.size, top = input.top;
	const branches = input.branches ?? [], portalRows = input.portalRows ?? null, prompt = input.prompt ?? "";
	const choiceColor = input.choiceColor ?? (() => [ 239 / 255, 218 / 255, 164 / 255, 1 ] as UiQuad["color"]);
	const box = layout.GDR_NT_TALKBOX!, scroll = layout.GDR_NPCTALK_SCROLL!;
	const bounds: UiRect = [ origin[0] + box.rect[0], origin[1] + box.rect[1], box.rect[2], box.rect[3] ];
	const rows: { text: string; id?: string; color: UiQuad["color"]; }[] = [],
		normal: UiQuad["color"] = [ 239 / 255, 218 / 255, 164 / 255, 1 ];
	const dialogue = state.phase === "menu" ? null : state.dialogue;
	const wrap = ( value: string ) =>
		textBoxParagraphs( value ).flatMap( paragraph => textLines( paragraph, bounds[2], measure ) );
	if ( !dialogue && prompt ) {
		for ( const text of wrap( prompt ) ) rows.push( { text, color: [ 1, 1, 1, 1 ] } );
		rows.push( { text: "", color: normal } );
	}
	if ( dialogue ) {
		for ( const text of wrap( copy( dialogue.prompt ) ) ) rows.push( { text, color: [ 1, 1, 1, 1 ] } );
		rows.push( { text: "", color: normal } );
	}
	const options = dialogue ?
		dialogue.options.map( r => ({ id: "npc-choice:" + r.choice, label: npcDialogueCaption( r.symbol, copy ) }) ) :
		[
			...(portalRows ??
				[
					...((input.canTalk ?? true) ?
						[ { id: "npc-talk", label: copy( "UIIT_STT_NPC_CHATTING_WND_TALKSTART" ) } ] :
						[]),
					...(input.canShop ?
						(branches.length ?
							branches.map( branch => ({
								id: branches.length === 1 ? "shop-open" : "shop-group:" + branch.id,
								label: npcBranchLabel( branch, branches, copy )
							}) ) :
							[ { id: "shop-open", label: copy( "UIIT_STT_NPC_CHATTING_WND_SHOP" ) } ]) :
						[]),
					...(input.canStorage ?
						[ { id: "storage-open", label: copy( "UIIT_STT_UNITY_SERVER_USE_STORAGEROOM" ) } ] :
						[]),
					// 5D9100 lists row 0x2F after the storage rows.
					...(input.canMagicOption ?
						[ {
							id: "magic-option-open",
							label: copy( "UIIT_STT_AVATAR_MAGICOPTION_ENCHANT_MAGIC_PARAM" )
						} ] :
						[]),
					...(input.canRecall ?
						[ { id: "npc-recall-designate", label: copy( "UIIT_CTL_RECALL_POSITION" ) } ] :
						[]),
					...(input.canPortal ?
						[ { id: "npc-portal-open", label: copy( "UIIT_CTL_TELEPORT_TARGET" ) } ] :
						[]),
					// 5D4410: action 0x2B rows 1 and 2 send 0x7495 type 5 with 2 (the
					// last recall point) or 3 (where the player died).
					...(input.canReverseReturn ?
						[ {
							id: "npc-reverse-return:2",
							label: copy( "UIIT_MSG_ITEM_USE_REVERSE_PORTAL_RETRUN_TO_LAST_RETURN" )
						}, {
							id: "npc-reverse-return:3",
							label: copy( "UIIT_MSG_ITEM_USE_REVERSE_PORTAL_RETRUN_TO_LAST_DEATH" )
						} ] :
						[]),
					...(input.jobRows ?? []),
					// 5D7AD0 action 0x34: the official's one row.
					...(input.canFortressOfficial ?
						[ { id: "npc-fortress-war", label: copy( "SN_FORTRESS_OFFICIAL_WARAPPLY" ) } ] :
						[])
				]),
			{ id: "npc-talkend", label: copy( "UIIT_STT_NPC_CHATTING_WND_TALKEND" ) }
		];
	options.forEach( ( option, i ) => {
		const color = dialogue ? choiceColor( dialogue.options[i]!.symbol ) : normal;
		for ( const text of wrap( `${i + 1}. ${option.label}` ) ) rows.push( { text, id: option.id, color } );
	} );
	if ( rows.length > 1024 ) throw Error( "NPC text row capacity" );
	const range = Math.max( 0, rows.length - 19 ),
		offset = Math.max( 0, Math.min( range, Math.round( top ) ) ),
		quads: UiQuad[] = [],
		controls: UiControl[] = [];
	const busy = state.phase === "waiting" || state.phase === "uncertain";
	rows.slice( offset, offset + 19 ).forEach( ( row, i ) => {
		const rect: UiRect = [ bounds[0], bounds[1] + i * 18, bounds[2], 18 ],
			disabled = busy && row.id !== "npc-talkend";
		const color = row.id && (hover === row.id || pressed === row.id) && !disabled ?
			[ 1, 138 / 255, 0, 1 ] as const :
			row.color;
		quads.push( ...draw( row.text, rect, bounds, color ) );
		if ( row.id ) {
			const index = controls.findIndex( c => c.id === row.id ), previous = controls[index];
			if ( previous ) {
				controls[index] = {
					...previous,
					label: previous.label + " " + row.text,
					rect: [ previous.rect[0], previous.rect[1], previous.rect[2], rect[1] + 18 - previous.rect[1] ]
				};
			} else controls.push( { id: row.id, label: row.text, rect, kind: "button", disabled } );
		}
	} );
	// 5D4A10/5D4BA0 -> 544C10: 320 is thumb travel, not outer height.
	// Native child offsets are up=-16, thumb=0, down=travel+16.
	const r: UiRect = [ origin[0] + scroll.rect[0], origin[1] + scroll.rect[1], 16, scroll.rect[3] ];
	const bar = chatScrollbar( "npc-scroll", r, rows.length, 19, range - offset, size, bounds, hover, pressed );
	// Scroll chrome lies outside the text clip, inside the authored child window.
	const clip: UiRect = [ origin[0], origin[1], 364, 391 ];
	quads.push( ...bar.quads.map( q => ({ ...q, clip }) ) );
	controls.push( ...bar.controls );
	return { quads, controls, paths: bar.paths, bounds, range, travel: bar.travel };
}
