/*
===========================================================================

gm-command.ts - the GM console's 0x75B6 requests and 0xB5B6 replies

The client's ChatInput_HandleGmCommand (509CF0) composes one 0x75B6 frame
per slash line; 751EC0 consumes the 0xB5B6 reply. Only arms whose wire
layout was verified in the client are composed here. Context-dependent
object and waypoint commands need their own reference authority; this
module never sends fabricated IDs. The same published item and monster
rows also feed the console helper's codename lists (runtime/ui/hud/
gm-helper.ts); the decoder there never invents a row the composer cannot
resolve.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

const OP_GM_COMMAND = 0x75b6;
const MAX_REFERENCE_ROWS = 65536;
const MAX_GM_NAME_BYTES = 128;
// The port's /SILK name amount (operator tooling, not native): subcommand
// 0xF0, past the native table's last command (0x30).
const GM_GRANT_SILK = 0xf0;
const MAX_SILK_GRANT = 1_000_000;
// The port's /SETSPEED [percent] (GM tooling, not native; the server's
// SRO_GM_SETSPEED): subcommand 0xF1 and a u16 percent of the effective
// speed. A bare /SETSPEED asks for normal speed. The server enforces the
// same range (action/gmspeed.go).
const GM_SET_SPEED = 0xf1;
const GM_SPEED_NORMAL_PERCENT = 100;
const GM_SPEED_MIN_PERCENT = 10;
const GM_SPEED_MAX_PERCENT = 1000;
const GM_ZOE = 0x0c;
const GM_MOB_KILL = 0x14;
const GM_INIT_Q = 0x1b;
const GM_RESET_Q = 0x1c;
const GM_COMP_Q = 0x1d;
const GM_REMOVE_Q = 0x1e;
const GM_MOVE_TO_NPC = 0x1f;
const GM_SIEGE_STRUCTURE = 0x20;
const GM_SIEGE_PERIOD = 0x21;
const MAX_OBJECT_ID = 0xffffffff;
const GM_COUNT_MASK = 0xff;

/*
================
siegePeriodAction

Maps the six /SIEGE period tokens (50C1B6..50C38A, matched with CRT_wcsicmp
at 0x009C5F6F) to their 0x21 action byte.
================
*/
function siegePeriodAction( token: string ): number {
	switch ( token.toUpperCase() ) {
		case "STARTWAR":
			return 1;
		case "ENDWAR":
			return 2;
		case "STARTTAX":
			return 3;
		case "ENDTAX":
			return 4;
		case "STARTREQ":
			return 5;
		case "ENDREQ":
			return 6;
		default:
			return 0;
	}
}

/*
================
codenameOrder

The helper lists codenames in their plain ASCII order. localeCompare would
change the order with the browser's language, and the native catalogues are
not localised.
================
*/
function codenameOrder( a: { readonly codename: string; }, b: { readonly codename: string; } ): number {
	return a.codename < b.codename ? -1 : a.codename > b.codename ? 1 : 0;
}

/*
================
GmReply
================
*/
export interface GmReply {
	readonly text?: string;
	readonly key?: string;
	readonly console: boolean;
}

/*
================
GmMonsterReference

A monster row the GM typed by codename, with its static type byte (client
record +0xA0).
================
*/
export interface GmMonsterReference {
	readonly refObjId: number;
	readonly monsterType: number;
}

/*
================
GmRequestContext

The worker owns the reference catalog and current selection. The composer
uses those authorities rather than inventing a target when none is selected.
================
*/
export interface GmRequestContext {
	readonly monster?: ( codename: string ) => GmMonsterReference | undefined;
	readonly targetGid?: number;
}

/*
================
GmMonsterRow

One /LOADMONSTER catalogue row: the codename the GM types, the title the
helper lists, and the level it sorts beside.
================
*/
export interface GmMonsterRow extends GmMonsterReference {
	readonly codename: string;
	readonly name: string;
	readonly level: number;
}

/*
================
GmItemReference
================
*/
export interface GmItemReference {
	readonly refObjId: number;
	readonly codename: string;
	// The English title from textdataname.txt. The server publishes it beside
	// the codename so the GM console can list what each row is.
	readonly name: string;
	readonly typeFlags: number;
	readonly maxStack: number;
}

/*
================
GmCatalogItem
================
*/
export interface GmCatalogItem {
	readonly codename: string;
	readonly name: string;
	// The composer's stackable mask decides the helper's default amount.
	readonly typeFlags: number;
}

/*
================
GmCatalogMonster
================
*/
export interface GmCatalogMonster {
	readonly codename: string;
	readonly name: string;
	// characterdata column 15: 0 normal, 3 unique. The Uniques tab filters it.
	readonly monsterType: number;
}

/*
================
GmCatalog

The compact codename catalogues the GM console helper lists. They are
derived from the public reference file the login already carries, and the
world journal forwards them once so presentation does not duplicate the
full tables.
================
*/
export interface GmCatalog {
	readonly items: readonly GmCatalogItem[];
	readonly monsters: readonly GmCatalogMonster[];
}

/*
================
gmItemReferenceRows

The /MAKEITEM catalog, validated at the worker and presentation boundaries.
Rows are sorted by codename so the helper's list order is stable.
================
*/
export function gmItemReferenceRows( value: unknown ): readonly GmItemReference[] {
	const rows = (value as { itemCommandReferences?: unknown; })?.itemCommandReferences ?? [];
	if ( !Array.isArray( rows ) || rows.length > MAX_REFERENCE_ROWS ) throw Error( "Invalid GM item catalog" );
	const result: GmItemReference[] = [], seen = new Set<string>();
	for ( const row of rows ) {
		const r = row as Partial<GmItemReference>;
		if (
			!r || typeof r.codename !== "string" || !r.codename.startsWith( "ITEM_" ) || r.codename.length > 256 ||
			!Number.isInteger( r.refObjId ) || r.refObjId! <= 0 || r.refObjId! > 0xffffffff ||
			!Number.isInteger( r.typeFlags ) || r.typeFlags! < 0 || r.typeFlags! > 65535 ||
			!Number.isInteger( r.maxStack ) || r.maxStack! < 1 || r.maxStack! > 65535 ||
			(r.name !== undefined && typeof r.name !== "string") ||
			seen.has( r.codename )
		) throw Error( "Invalid GM item reference" );
		const name = typeof r.name === "string" && r.name.length <= 256 ? r.name : r.codename;
		seen.add( r.codename );
		result.push( {
			refObjId: r.refObjId!,
			codename: r.codename,
			name: name || r.codename,
			typeFlags: r.typeFlags!,
			maxStack: r.maxStack!
		} );
	}
	return result.sort( codenameOrder );
}

/*
================
gmItemReferences

The /MAKEITEM catalog keyed by codename, as the composer reads it.
================
*/
export function gmItemReferences( value: unknown ): Map<string, GmItemReference> {
	return new Map( gmItemReferenceRows( value ).map( row => [ row.codename, row ] ) );
}

/*
================
gmMonsterReferences

The /LOADMONSTER catalogue: every published monster row, validated and
sorted by codename. A row without a codename cannot be typed, so it is
not a helper row.
================
*/
export function gmMonsterReferences( value: unknown ): readonly GmMonsterRow[] {
	const rows = (value as { refObjSnapshot?: unknown; })?.refObjSnapshot ?? [];
	if ( !Array.isArray( rows ) || rows.length > MAX_REFERENCE_ROWS ) throw Error( "Invalid GM monster catalog" );
	const result: GmMonsterRow[] = [], seen = new Set<string>();
	for ( const row of rows ) {
		const r = row as {
			refObjId?: unknown;
			codename?: unknown;
			kind?: unknown;
			name?: unknown;
			level?: unknown;
			monsterType?: unknown;
		};
		if ( !r || r.kind !== "monster" || typeof r.codename !== "string" || !r.codename ) continue;
		if (
			!Number.isInteger( r.refObjId ) || (r.refObjId as number) <= 0 ||
			(r.refObjId as number) > 0xffffffff || r.codename.length > 256 ||
			(r.name !== undefined && typeof r.name !== "string") ||
			(r.level !== undefined &&
				(!Number.isInteger( r.level ) || (r.level as number) < 0 || (r.level as number) > 255)) ||
			(r.monsterType !== undefined &&
				(!Number.isInteger( r.monsterType ) || (r.monsterType as number) < 0 ||
					(r.monsterType as number) > 255)) ||
			seen.has( r.codename )
		) throw Error( "Invalid GM monster reference" );
		const name = typeof r.name === "string" && r.name.length <= 256 ? r.name : r.codename;
		seen.add( r.codename );
		result.push( {
			refObjId: r.refObjId as number,
			codename: r.codename,
			name: name || r.codename,
			level: typeof r.level === "number" ? r.level : 0,
			monsterType: typeof r.monsterType === "number" ? r.monsterType : 0
		} );
	}
	return result.sort( codenameOrder );
}

/*
================
gmCatalog

Both helper catalogues from one bootstrap blob. The item rows live in
itemCommandReferences; the monsters are the published refObjSnapshot rows
the /LOADMONSTER composer already resolves.
================
*/
export function gmCatalog( value: unknown ): GmCatalog {
	return {
		items: gmItemReferenceRows( value ).map( row => ({
			codename: row.codename,
			name: row.name,
			typeFlags: row.typeFlags
		}) ),
		monsters: gmMonsterReferences( value ).map( row => ({
			codename: row.codename,
			name: row.name,
			monsterType: row.monsterType
		}) )
	};
}

/*
================
gmReply

B5B6 / 751EC0. The failure byte is passed to category 0 of 689420, not to
a GM-specific error table. Most failure codes are deliberately silent.
================
*/
export function gmReply( p: Uint8Array ): GmReply | null {
	if ( !p.length ) throw Error( "Truncated GM reply" );
	const result = p[0];
	if ( result !== 1 && result !== 2 ) return null;
	if ( p.length < 2 ) throw Error( "Truncated GM reply command" );
	const code = p[1]!;
	const string = ( failure = false ) => {
		if ( p.length < 4 ) throw Error( "Truncated GM reply string" );
		const size = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 2, true );
		if ( p.length !== 4 + size + Number( failure ) ) throw Error( "Invalid GM reply string length" );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 4, 4 + size ) );
	};
	if ( code === 0x19 || code === 0x1a ) {
		const value = string( result === 2 );
		const marked = (result === 1 && code === 0x1a) || (result === 2 && code === 0x19);
		return { console: true, text: (result === 1 ? "-> " : "Failed. -> ") + (marked ? "*" : "") + value };
	}
	if ( result === 1 && code === 1 ) return { console: false, text: string() };
	if ( code === GM_GRANT_SILK ) {
		// The port's /SILK answer: the recipient's new balance, or a refusal.
		if ( result === 2 ) {
			if ( p.length !== 2 ) throw Error( "Trailing GM silk refusal bytes" );
			return { console: true, text: "Failed. -> silk grant refused" };
		}
		if ( p.length !== 6 ) throw Error( "Invalid GM silk reply" );
		const balance = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true );
		return { console: true, text: "-> silk granted, balance " + balance };
	}
	if ( code === GM_SET_SPEED ) {
		// The port's /SETSPEED answer: the percent now in force, or a refusal.
		if ( result === 2 ) {
			if ( p.length !== 2 ) throw Error( "Trailing GM speed refusal bytes" );
			return { console: true, text: "Failed. -> speed change refused" };
		}
		if ( p.length !== 4 ) throw Error( "Invalid GM speed reply" );
		const percent = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 2, true );
		return { console: true, text: "-> speed set to " + percent + "%" };
	}
	if ( result === 1 && code === 4 ) {
		if ( p.length !== 14 ) throw Error( "Invalid GM world status" );
		const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
		return {
			console: false,
			text: `Player:${v.getInt32( 2, true )}, NPC_Mob:${v.getInt32( 6, true )}, DroppedItem:${
				v.getInt32( 10, true )
			}`
		};
	}
	if ( p.length !== 2 ) throw Error( "Trailing GM reply bytes" );
	if ( code === 0x20 || (result === 1 && code === 0x21) ) {
		return { console: false, text: "SiegeManager MSG Result - " + (result === 1 ? "Ok." : "Fail.") };
	}
	if ( result === 2 ) {
		const key = ({
			3: "UIIT_STT_ERR_COMMON_INVALID_TARGET",
			4: "UIIT_STT_ERR_COMMON_TOO_FAR",
			5: "UIIT_STT_ERR_COMMON_INVALID_OPERATION"
		} as Record<number, string>)[code];
		if ( key ) return { console: false, key };
	}
	return null;
}

/*
================
sizedNameFrame

One subcommand byte, then the u16 length and bytes of the typed name
(4FD5B0). An empty, oversized or NUL-bearing name is a malformed line, not a
frame.
================
*/
function sizedNameFrame( subcommand: number, text: string ): WireFrame {
	const bytes = new TextEncoder().encode( text );
	if ( !bytes.length || bytes.length >= MAX_GM_NAME_BYTES || text.includes( "\0" ) ) {
		throw Error( "Invalid GM name" );
	}
	const payload = new Uint8Array( 3 + bytes.length );
	payload[0] = subcommand;
	new DataView( payload.buffer ).setUint16( 1, bytes.length, true );
	payload.set( bytes, 3 );
	return { opcode: OP_GM_COMMAND, payload };
}

/*
================
gmRequest

The verified name and scalar arms of 509CF0. monster resolves a typed
codename the way the native client reads its own character data.
================
*/
export function gmRequest(
	line: string,
	items: ReadonlyMap<string, GmItemReference> = new Map(),
	heading = 0,
	context: GmRequestContext = {}
): WireFrame | null {
	const tokens = line.trim().split( /\s+/ ), name = tokens[0], arg = tokens[1] ?? "";
	const targetGid = context.targetGid ?? 0;
	// Retail 50B303: WP sends u8 16, u16 region, float32 XYZ, u16 heading.
	// Coordinate convenience uses that authority instead of 50BEE0's local warp.
	if ( name === "/warp" ) {
		if ( tokens.length !== 5 ) return null;
		const [region, x, y, z] = tokens.slice( 1 ).map( Number );
		if (
			!Number.isInteger( region ) || region! <= 0 || region! > 65535 ||
			![ x, y, z ].every( v => Number.isFinite( v ) && Number.isFinite( Math.fround( v! ) ) ) ||
			!Number.isInteger( heading ) || heading < 0 || heading > 65535
		) return null;
		const payload = new Uint8Array( 17 ), v = new DataView( payload.buffer );
		payload[0] = 16;
		v.setUint16( 1, region!, true );
		v.setFloat32( 3, x!, true );
		v.setFloat32( 7, y!, true );
		v.setFloat32( 11, z!, true );
		v.setUint16( 15, heading, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// Port-only: dedicated dummy references exist only with SRO_GM_TRAINING_DUMMY on.
	if ( name?.toUpperCase() === "/TRAININGDUMMY" && tokens.length <= 2 ) {
		const level = tokens.length === 1 ? 1 : /^\d+$/.test( arg ?? "" ) ? Number( arg ) : 0;
		if ( !Number.isInteger( level ) || level < 1 || level > 140 ) return null;
		const ref = context.monster?.( `MOB_GM_TRAINING_DUMMY_${String( level ).padStart( 3, "0" )}` );
		if ( !ref ) return null;
		const payload = Uint8Array.of( 6, 0, 0, 0, 0, 1, 0 );
		new DataView( payload.buffer ).setUint32( 1, ref.refObjId, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// ChatInput_HandleGmCommand matches with CRT_wcsicmp (0x50A77A).
	if ( name?.toUpperCase() === "/MAKEITEM" && tokens.length === 3 ) {
		const ref = items.get( arg );
		if ( !ref ) return null;
		// 50A838 reads the parsed integer's low byte BEFORE clamping.
		const amount = (Number.parseInt( tokens[2]!, 10 ) || 0) & 255, stackable = (ref.typeFlags & 0x60) === 0x60;
		const parameter = stackable ? Math.min( ref.maxStack, Math.max( 1, amount ) ) & 255 : Math.min( 12, amount );
		const payload = Uint8Array.of( 7, 0, 0, 0, 0, parameter );
		new DataView( payload.buffer ).setUint32( 1, ref.refObjId, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// 50A3D3 /LOADMONSTER codename count [CHAMP|GIANT|NORMAL]: subcmd 6, u32 ref,
	// u8 count (low byte, 1..255), u8 type. Without a type token 50A4A6 sends the
	// record's +0xA0 byte; an unrecognized token leaves 0 (50A49F).
	// ChatInput_HandleGmCommand matches with CRT_wcsicmp (0x50A3D5).
	if ( name?.toUpperCase() === "/LOADMONSTER" && (tokens.length === 3 || tokens.length === 4) ) {
		const ref = context.monster?.( arg );
		if ( !ref ) return null;
		const parsed = (Number.parseInt( tokens[2]!, 10 ) || 0) & 255, count = parsed <= 1 ? 1 : parsed;
		const word = tokens[3]?.toUpperCase();
		const type = tokens.length === 3 ?
			ref.monsterType & 255 :
			word === "NORMAL" ?
			0 :
			word === "GIANT" ?
			4 :
			word === "CHAMP" ?
			1 :
			0;
		const payload = Uint8Array.of( 6, 0, 0, 0, 0, count, type );
		new DataView( payload.buffer ).setUint32( 1, ref.refObjId, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// 50A5A8..50A6DF /ZOE codename count: subcmd 12, u32 ref, u8 count (the
	// parsed low byte; 0 and 1 become 1 and 255 stays 255, 50A6AB..50A6CD).
	// The server loads the monsters and kills them for rewards.
	if ( name?.toUpperCase() === "/ZOE" && tokens.length === 3 ) {
		const ref = context.monster?.( arg );
		if ( !ref ) return null;
		const parsed = (Number.parseInt( tokens[2]!, 10 ) || 0) & GM_COUNT_MASK, count = parsed <= 1 ? 1 : parsed;
		const payload = Uint8Array.of( GM_ZOE, 0, 0, 0, 0, count );
		new DataView( payload.buffer ).setUint32( 1, ref.refObjId, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// 50B645..50B77E /MOBKILL 0|1: subcmd 20, u32 selected object, u16 mode.
	// The native arm sends nothing without a selected target; the worker
	// supplies the target the player clicked.
	if ( name?.toUpperCase() === "/MOBKILL" && tokens.length === 2 && /^[01]$/.test( arg ) ) {
		if ( !Number.isInteger( targetGid ) || targetGid <= 0 || targetGid > MAX_OBJECT_ID ) return null;
		const payload = new Uint8Array( 7 ), view = new DataView( payload.buffer );
		payload[0] = GM_MOB_KILL;
		view.setUint32( 1, targetGid, true );
		view.setUint16( 5, Number( arg ), true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// Any case: the command is port-only, and players type it as /setspeed.
	if ( name?.toUpperCase() === "/SETSPEED" ) {
		if ( tokens.length > 2 || (tokens.length === 2 && !/^[0-9]+$/.test( tokens[1]! )) ) return null;
		const percent = tokens.length === 2 ? Number( tokens[1] ) : GM_SPEED_NORMAL_PERCENT;
		if ( percent < GM_SPEED_MIN_PERCENT || percent > GM_SPEED_MAX_PERCENT ) return null;
		const payload = Uint8Array.of( GM_SET_SPEED, 0, 0 );
		new DataView( payload.buffer ).setUint16( 1, percent, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	if ( name === "/SILK" ) {
		if ( tokens.length !== 3 || !/^[0-9]+$/.test( tokens[2]! ) ) return null;
		const amount = Number( tokens[2] ), bytes = new TextEncoder().encode( arg );
		if ( amount < 1 || amount > MAX_SILK_GRANT ) return null;
		if ( !bytes.length || bytes.length >= MAX_GM_NAME_BYTES || arg.includes( "\0" ) ) {
			throw Error( "Invalid GM name" );
		}
		const payload = new Uint8Array( 3 + bytes.length + 4 ), view = new DataView( payload.buffer );
		payload[0] = GM_GRANT_SILK;
		view.setUint16( 1, bytes.length, true );
		payload.set( bytes, 3 );
		view.setUint32( 3 + bytes.length, amount, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// 50C982..50CBE6 /movetonpc, /INITQ, /COMPQ, /REMOVEQ name: the
	// discriminator byte, then 4FD5B0's u16 length and ANSI codename.
	// /RESETQ is the lone token.
	const questTool = ({
		"/INITQ": GM_INIT_Q,
		"/COMPQ": GM_COMP_Q,
		"/REMOVEQ": GM_REMOVE_Q,
		"/MOVETONPC": GM_MOVE_TO_NPC
	} as Record<string, number>)[
		name?.toUpperCase() ?? ""
	];
	if ( questTool !== undefined ) return tokens.length === 2 ? sizedNameFrame( questTool, arg ) : null;
	if ( name?.toUpperCase() === "/RESETQ" ) {
		return tokens.length === 1 ? { opcode: OP_GM_COMMAND, payload: Uint8Array.of( GM_RESET_Q ) } : null;
	}
	const named = ({
		"/FINDUSER": 1,
		"/TOTOWN": 3,
		"/MOVETOUSER": 8,
		"/BAN": 13,
		"/RECALLUSER": 17,
		"/RECALLGUILD": 18,
		"/LIENAME": 25,
		"/REALNAME": 26
	} as Record<string, number>)[name ?? ""];
	if ( named !== undefined ) return tokens.length === 2 ? sizedNameFrame( named, arg ) : null;
	const simple = ({ "/GOTOWN": 2, "/WORLDSTATUS": 4, "/INVISIBLE": 14, "/INVINCIBLE": 15 } as Record<
		string,
		number
	>)[name ?? ""];
	if ( simple !== undefined ) {
		if ( simple >= 14 && tokens.length !== 1 ) return null;
		return { opcode: OP_GM_COMMAND, payload: Uint8Array.of( simple ) };
	}
	if ( name === "/SETTIME" && tokens.length === 2 ) {
		const hour = Number.parseInt( arg, 10 ) || 0;
		return { opcode: OP_GM_COMMAND, payload: Uint8Array.of( 10, hour < 0 || hour >= 24 ? 0 : hour ) };
	}
	if ( name?.toUpperCase() === "/FLAGWORLD" && tokens.length === 2 ) {
		return { opcode: OP_GM_COMMAND, payload: Uint8Array.of( 0x30, Number.parseInt( arg, 10 ) || 0 ) };
	}
	if ( name?.toUpperCase() === "/INSTANCE" && tokens.length === 3 ) {
		const payload = new Uint8Array( 5 ), view = new DataView( payload.buffer );
		payload[0] = 19;
		view.setUint16( 1, Number.parseInt( arg, 10 ) || 0, true );
		view.setUint16( 3, Number.parseInt( tokens[2]!, 10 ) || 0, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// 50C1B6..50C66B: /SIEGE period transitions (0x21) and structure actions (0x20),
	// matched case-insensitively with CRT_wcsicmp (0x009C5F6F).
	if ( name?.toUpperCase() === "/SIEGE" && tokens.length >= 2 ) {
		const periodAction = siegePeriodAction( arg );
		if ( periodAction !== 0 ) {
			return tokens.length === 2 ?
				{ opcode: OP_GM_COMMAND, payload: Uint8Array.of( GM_SIEGE_PERIOD, periodAction ) } :
				null;
		}
		const siegeSub = arg.toUpperCase();
		if ( (siegeSub === "STRUCT_BUILD" || siegeSub === "STRUCT_REPAIR") && tokens.length === 3 ) {
			const payload = new Uint8Array( 10 ), view = new DataView( payload.buffer );
			payload[0] = GM_SIEGE_STRUCTURE;
			payload[1] = siegeSub === "STRUCT_BUILD" ? 0x0a : 0x0c;
			view.setUint32( 2, 1, true );
			view.setUint32( 6, Number.parseInt( tokens[2]!, 10 ) || 0, true );
			return { opcode: OP_GM_COMMAND, payload };
		}
		if ( (siegeSub === "STRUCT_UPGRADE" || siegeSub === "GATE") && tokens.length === 4 ) {
			const action = siegeSub === "STRUCT_UPGRADE" ? 0x0b : 0x15;
			let param = (Number.parseInt( tokens[3]!, 10 ) || 0) & GM_COUNT_MASK;
			if ( action === 0x15 && param !== 0 ) param = 2;
			const payload = new Uint8Array( 11 ), view = new DataView( payload.buffer );
			payload[0] = GM_SIEGE_STRUCTURE;
			payload[1] = action;
			view.setUint32( 2, 1, true );
			view.setUint32( 6, Number.parseInt( tokens[2]!, 10 ) || 0, true );
			payload[10] = param;
			return { opcode: OP_GM_COMMAND, payload };
		}
		return null;
	}
	return null;
}
