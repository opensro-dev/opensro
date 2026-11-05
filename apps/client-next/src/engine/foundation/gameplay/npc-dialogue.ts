export type NpcGid = number & { readonly __npcGid: unique symbol; };
// B338 -> 75AE50 -> 67ABE0. The selected NPC contributes only these two
// capability bits; the acknowledged service owns the remaining lock mask.
export function npcInteractionMask( payload: Uint8Array, capabilities: number ): number | null {
	if ( payload.length === 2 && payload[0] === 2 ) return null;
	if ( payload[0] !== 1 || payload.length < 5 ) throw Error( "Invalid NPC interaction acknowledgement" );
	const mask = new DataView( payload.buffer, payload.byteOffset, payload.byteLength ).getUint32( 1, true );
	if ( payload.length !== (mask === 0x800 ? 6 : 5) ) throw Error( "Invalid NPC interaction service payload" );
	return (mask | (capabilities & 0x1400)) >>> 0;
}
export function npcGid( value: number ): NpcGid {
	if ( !Number.isInteger( value ) || value <= 0 || value > 0xffffffff ) throw Error( "Invalid NPC identity" );
	return value as NpcGid;
}
// Kind 5 (763C10 / 5D60C0): the rows of kind 4, then a wide item codename and a
// u32 count that format into the prompt as %s and %d.
export interface NpcDialogue {
	readonly kind: 1 | 2 | 3 | 4 | 5;
	readonly prompt: string;
	readonly options: readonly { readonly choice: number; readonly symbol: string; }[];
	readonly item?: { readonly codename: string; readonly count: number; };
}
export type NpcConversation =
	| { readonly phase: "closed"; }
	| (
		& { readonly gid: NpcGid; readonly dialogueRevision: number; }
		& (
			| { readonly phase: "menu"; }
			| { readonly phase: "waiting"; readonly dialogue: NpcDialogue | null; readonly deadline: number; }
			| { readonly phase: "ready"; readonly dialogue: NpcDialogue; }
			| { readonly phase: "uncertain"; readonly dialogue: NpcDialogue | null; }
		)
	);
export type NpcConversationEvent =
	| { readonly type: "select"; readonly gid: NpcGid; }
	| { readonly type: "request"; readonly now: number; }
	| { readonly type: "reply"; readonly dialogue: NpcDialogue; }
	| { readonly type: "tick"; readonly now: number; }
	| { readonly type: "close"; };
// Low-level engine forbids external dependencies (verify-ownership). This
// closed union/exhaustive transition is the documented statechart exemption.
export function npcConversationTransition( state: NpcConversation, event: NpcConversationEvent ): NpcConversation {
	switch ( event.type ) {
		case "select":
			return { phase: "menu", gid: event.gid, dialogueRevision: 0 };
		case "close":
			return { phase: "closed" };
		case "request":
			if ( state.phase !== "menu" && state.phase !== "ready" ) throw Error( "NPC conversation is not ready" );
			return {
				phase: "waiting",
				gid: state.gid,
				dialogueRevision: state.dialogueRevision,
				dialogue: state.phase === "ready" ? state.dialogue : null,
				deadline: event.now + 10000
			};
		// 5D672C resets the pane for every accepted reply, even identical text.
		// Publish the event identity: the HUD may never observe the waiting frame.
		case "reply":
			return state.phase === "waiting" || state.phase === "uncertain" ?
				{
					phase: "ready",
					gid: state.gid,
					dialogueRevision: state.dialogueRevision + 1,
					dialogue: event.dialogue
				} :
				state;
		case "tick":
			return state.phase === "waiting" && event.now >= state.deadline ?
				{
					phase: "uncertain",
					gid: state.gid,
					dialogueRevision: state.dialogueRevision,
					dialogue: state.dialogue
				} :
				state;
		default: {
			const unreachable: never = event;
			return unreachable;
		}
	}
}
// CPSMission 763C10 / CIFNPCTalk 5D60C0.
export function decodeNpcDialogue( p: Uint8Array ): NpcDialogue {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let offset = 0;
	function take( n: number ) {
		if ( offset + n > p.length ) throw Error( "Truncated NPC dialogue" );
		const start = offset;
		offset += n;
		return start;
	}
	const u8 = () => v.getUint8( take( 1 ) );
	function string() {
		const length = v.getUint16( take( 2 ), true );
		if ( length > 4096 ) throw Error( "NPC dialogue string budget" );
		const start = take( length );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( start, start + length ) );
	}
	const kind = u8();
	if ( kind < 1 || kind > 5 ) throw Error( "Unsupported NPC dialogue kind" );
	const prompt = string();
	const options: { choice: number; symbol: string; }[] = [];
	if ( kind === 1 ) options.push( { choice: 1, symbol: "UIIT_STT_CONFIRM" } );
	if ( kind === 2 ) options.push( { choice: 4, symbol: "" } );
	if ( kind === 3 ) options.push( { choice: 2, symbol: "UIIT_STT_YES" }, { choice: 3, symbol: "UIIT_STT_NO" } );
	if ( kind === 4 || kind === 5 ) {
		const count = u8();
		if ( count > 251 ) throw Error( "NPC option budget" );
		let maxQuestSeen = false;
		for ( let i = 0; i < count; i++ ) {
			const symbol = string();
			maxQuestSeen ||= symbol === "SN_TALK_COMMON_MAXQUEST";
			const choice = i + (maxQuestSeen ? 6 : 5);
			if ( choice > 255 ) throw Error( "NPC choice overflow" );
			options.push( { choice, symbol } );
		}
	}
	let item: { codename: string; count: number; } | undefined;
	if ( kind === 5 ) {
		const units = v.getUint16( take( 2 ), true );
		if ( units > 4096 ) throw Error( "NPC dialogue string budget" );
		const start = take( units * 2 );
		const codename = new TextDecoder( "utf-16le", { fatal: true } ).decode(
			p.subarray( start, start + units * 2 )
		);
		item = { codename, count: v.getUint32( take( 4 ), true ) };
	}
	if ( offset !== p.length ) throw Error( "NPC dialogue trailing bytes" );
	return { kind: kind as 1 | 2 | 3 | 4 | 5, prompt, options, ...(item ? { item } : {}) };
}
