/*
===========================================================================

academy.ts - the Academy (training camp) state: matching, roster, notice
and opening

Decodes the academy frames into AcademyState and composes its requests:
the matching board (0x7701 / 0x7592), the notice (0x7220) and the opening
at the storage NPC (0x72EC, answered by 0xB2EC). The 0x3AC5 status
machine and the 0xB2EC opening carry the same camp record (82AA10).

===========================================================================
*/
import { encodeWindows1252 } from "./windows1252";
import type { WireFrame } from "@/engine/contracts/network";

// ACADEMY_OPEN_FEE is the gold an opening costs: UIIT_MSG_TC_TRAININGCAMP_OPEN
// names it and 5D26F0 case 6 compares the balance with 0x186A0 before 702BD0
// sends 0x72EC.
export const ACADEMY_OPEN_FEE = 100000;
// ACADEMY_OPEN_LEVEL is 5D9100's floor for the "Academy Establishment" row
// (g_pLocalPlayer+0x820 >= 0x3C).
export const ACADEMY_OPEN_LEVEL = 60;
export interface AcademyListing {
	readonly id: number;
	readonly kind: number;
	readonly detail: string;
	readonly level: number;
	readonly model: number;
	readonly name: string;
	readonly students: number;
	readonly grade: number;
	readonly graduates: number;
}
export interface AcademyMember {
	readonly id: number;
	readonly name: string;
	readonly kind: number;
	readonly level: number;
	readonly entryLevel: number;
	readonly model: number;
	readonly offline: boolean;
	readonly honor: number;
	readonly location: string;
	readonly war?: number;
	readonly regionId?: number;
	readonly x?: number;
	readonly y?: number;
	readonly z?: number;
}
export interface AcademyHonorRow {
	readonly ranking: number;
	readonly rank: number;
	readonly name: string;
	readonly level: number;
	readonly maxLevel: number;
	readonly graduates: number;
	readonly guild: string;
}
export interface AcademyState {
	readonly members?: readonly AcademyMember[];
	readonly localMemberId?: number;
	readonly evaluation?: number;
	readonly subject?: string;
	readonly contents?: string;
	readonly member: boolean;
	readonly page: number;
	readonly pages: number;
	readonly rows: readonly AcademyListing[];
	readonly request: { readonly kind: "page" | "join"; readonly id: number; } | null;
	readonly result: number | null;
	// Count of accepted openings (0xB2EC [1]); 76FBA0 then opens the
	// Academy tab (CGInterface_OpenAcademyTab), which the UI does on a change.
	readonly opened?: number;
	// 771750 -> 69EEE0: each accepted 0xB2D0 replaces the Honor Ranking list
	// and opens CIFApprenticeShipHonorRanking at page 0.
	readonly honorRanks?: {
		readonly rows: readonly AcademyHonorRow[];
		readonly sequence: number;
	};
}
export type AcademyCommand =
	| { readonly kind: "academy-page"; readonly page: number; }
	| {
		readonly kind: "academy-join";
		readonly id: number;
	}
	| { readonly kind: "academy-notice"; readonly subject: string; readonly contents: string; }
	| {
		readonly kind: "academy-open";
	}
	| { readonly kind: "academy-honor-rank"; readonly gid: number; }
	| { readonly kind: "academy-disband"; }
	| { readonly kind: "academy-leave"; }
	| { readonly kind: "academy-graduate"; readonly satisfaction: number; }
	| { readonly kind: "academy-kick"; readonly name: string; }
	| { readonly kind: "academy-compulsory-graduate"; readonly name: string; };
/*
================
academyBootstrap
================
*/
export function academyBootstrap( value: unknown ): AcademyState | undefined {
	const member = (value as { academyMember?: unknown; }).academyMember;
	if ( member === undefined ) return undefined;
	if ( typeof member !== "boolean" ) throw Error( "Invalid academy membership" );
	return { member, page: 0, pages: 0, rows: [], request: null, result: null };
}
/*
================
academyCanJoin

Native 671a50 checks current level, independently of the under-40 entry icon.
================
*/
export function academyCanJoin( state: AcademyState, id: number, level: number | undefined ): boolean {
	return !state.member && level !== undefined && Number.isInteger( level ) && level > 0 && level < 60 &&
		state.rows.some( r => r.id === id );
}
/*
================
academyRequest
================
*/
export function academyRequest(
	state: AcademyState,
	command: Extract<AcademyCommand, { kind: "academy-page" | "academy-join"; }>,
	level?: number
): { state: AcademyState; frame: WireFrame; } {
	if ( state.request ) throw Error( "Academy request in progress" );
	let payload: Uint8Array, opcode: number;
	if ( command.kind === "academy-page" ) {
		if ( !Number.isInteger( command.page ) || command.page < 0 || command.page > 255 ) {
			throw Error( "Invalid academy page" );
		}
		payload = Uint8Array.of( command.page );
		opcode = 0x7701;
	} else {
		if ( !academyCanJoin( state, command.id, level ) ) throw Error( "No eligible academy selection" );
		payload = new Uint8Array( 4 );
		new DataView( payload.buffer ).setUint32( 0, command.id, true );
		opcode = 0x7592;
	}
	return {
		state: {
			...state,
			request: {
				kind: command.kind === "academy-page" ? "page" : "join",
				id: command.kind === "academy-page" ? command.page : command.id
			},
			result: null
		},
		frame: { opcode, payload }
	};
}
// v1.150 769720 / 769ca0. A join acknowledgement is not membership authority.
// ACADEMY_OPEN_WAIT_CODE is UIIT_MSG_TC_ERROR_NOT_OPEN_7D, the one 0xB2EC
// refusal 76FBA0 reads four more bytes after.
const ACADEMY_OPEN_WAIT_CODE = 0x1c;

/*
================
academyPacket
================
*/
export function academyPacket( state: AcademyState, frame: WireFrame ): AcademyState | null {
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let at = 0;
	const u = ( size: number ) => {
		if ( at + size > p.length ) throw Error( "Truncated academy packet" );
		const n = size === 1 ? p[at]! : size === 2 ? v.getUint16( at, true ) : v.getUint32( at, true );
		at += size;
		return n;
	};
	const str = ( wide: boolean ) => {
		const size = u( 2 ) * (wide ? 2 : 1);
		if ( size > 8192 || at + size > p.length ) throw Error( "Invalid academy string" );
		const s = new TextDecoder( wide ? "utf-16le" : "windows-1252", { fatal: true } ).decode(
			p.subarray( at, at + size )
		);
		at += size;
		return s;
	};
	// 8290E0 / 827890: retain the roster; membership alone cannot present the Academy pane.
	const signed = () => u( 2 ) << 16 >> 16;
	const memberRow = (): AcademyMember => {
		u( 4 );
		const id = u( 4 ), model = u( 4 );
		const name = str( false ), kind = u( 1 );
		u( 1 );
		for ( let j = 0; j < 16; j++ ) u( 1 );
		const entryLevel = u( 1 ),
			level = u( 1 ),
			honor = u( 4 ),
			offline = u( 1 ) === 1,
			war = u( 4 ),
			regionId = u( 2 ),
			x = signed(),
			y = signed(),
			z = signed(),
			location = str( false );
		if ( !id || kind > 2 ) throw Error( "Invalid academy member" );
		return { id, name, kind, entryLevel, level, model, offline, honor, location, war, regionId, x, y, z };
	};
	// 774B5F (case 6 => wire subtype 7) updates only the two notice fields.
	if ( frame.opcode === 0x3ac5 && p[0] === 7 ) {
		u( 1 );
		const subject = str( false ), contents = str( false );
		if ( at !== p.length ) throw Error( "Trailing academy notice bytes" );
		return { ...state, subject, contents };
	}
	if ( frame.opcode === 0x3ac5 && p[0] === 1 ) {
		if ( p.length !== 1 ) throw Error( "Trailing academy dissolution bytes" );
		return {
			...state,
			member: false,
			members: [],
			localMemberId: undefined,
			...(state.evaluation !== undefined ? { evaluation: undefined } : {}),
			subject: undefined,
			contents: undefined
		};
	}
	if ( frame.opcode === 0x3ac5 && (p[0] === 3 || p[0] === 4) ) {
		const status = u( 1 ), id = u( 4 ), flag = u( 1 );
		if ( at !== p.length ) throw Error( "Trailing academy removal bytes" );
		if ( status === 3 ? ![ 1, 2, 3 ].includes( flag ) : ![ 0, 1 ].includes( flag ) ) return state;
		if ( !(state.members ?? []).some( m => m.id === id ) ) throw Error( "Unknown academy member removal" );
		if ( id === state.localMemberId && !(status === 3 && flag === 3) ) {
			return {
				...state,
				member: false,
				members: [],
				localMemberId: undefined,
				...(state.evaluation !== undefined ? { evaluation: undefined } : {})
			};
		}
		return { ...state, members: state.members?.filter( m => m.id !== id ) };
	}
	// 774D78 (case 0xB => wire subtype 12) updates the camp evaluation tier (+0x34).
	if ( frame.opcode === 0x3ac5 && p[0] === 12 ) {
		u( 1 );
		const evaluation = u( 1 );
		if ( at !== p.length ) throw Error( "Trailing academy evaluation bytes" );
		return { ...state, evaluation };
	}
	if ( frame.opcode === 0x3ac5 && p[0] === 13 ) {
		u( 1 );
		const id = u( 4 ),
			location = str( false ),
			war = u( 4 ),
			regionId = u( 2 ),
			x = signed(),
			y = signed(),
			z = signed();
		if ( at !== p.length ) throw Error( "Trailing academy location bytes" );
		return {
			...state,
			members: state.members?.map( m => m.id === id ? { ...m, location, war, regionId, x, y, z } : m )
		};
	}
	if ( frame.opcode === 0x3ac5 && p[0] === 5 ) {
		u( 1 );
		const id = u( 4 ), flag = u( 1 );
		let patch: Partial<AcademyMember> = {};
		if ( flag === 1 ) patch = { offline: u( 1 ) === 1 };
		else if ( flag === 2 ) patch = { level: u( 1 ) };
		else if ( flag === 3 ) patch = { honor: u( 4 ) };
		else if ( flag === 6 ) patch = { entryLevel: u( 1 ) };
		if ( at !== p.length ) throw Error( "Trailing academy status bytes" );
		if ( !(state.members ?? []).some( m => m.id === id ) ) throw Error( "Unknown academy member status" );
		return { ...state, members: state.members?.map( m => m.id === id ? { ...m, ...patch } : m ) };
	}
	// 773C60 switches on subtype minus one; case 9 is wire subtype 10.
	// 774D4B..774D6E: result 2 reads one error byte, without replacing the roster.
	if ( frame.opcode === 0x3ac5 && p[0] === 10 && p[1] === 2 ) {
		u( 1 );
		u( 1 );
		u( 1 );
		if ( at !== p.length ) throw Error( "Trailing academy membership failure bytes" );
		return state;
	}
	// 82AA10 / 8290E0: the camp record the status-10 seed and the 0xB2EC
	// opening share - the local member id, a 16-byte block plus the +0x34
	// evaluation byte, the notice and the counted roster.
	const campRecord = (): AcademyState => {
		const localMemberId = u( 4 );
		for ( let i = 0; i < 16; i++ ) u( 1 );
		const evaluation = u( 1 );
		const subject = str( false ), contents = str( false ), count = u( 1 );
		if ( count > 8 ) throw Error( "Academy roster exceeds native capacity" );
		const members: AcademyMember[] = [];
		for ( let i = 0; i < count; i++ ) {
			const row = memberRow();
			if ( members.some( m => m.id === row.id ) ) throw Error( "Duplicate academy member" );
			members.push( row );
		}
		if ( at !== p.length ) throw Error( "Trailing academy seed bytes" );
		return {
			...state,
			member: members.some( m => m.id === localMemberId ),
			localMemberId,
			...(evaluation || state.evaluation !== undefined ? { evaluation: evaluation || undefined } : {}),
			subject,
			contents,
			members
		};
	};
	if ( frame.opcode === 0x3ac5 && p[0] === 10 && p[1] === 1 ) {
		u( 1 );
		u( 1 );
		return campRecord();
	}
	// 76FBA0: [1] and the new camp; [2] and a category-0x1D code, followed by
	// a u32 only for 0x1C (the reopen wait). A refusal changes no state.
	if ( frame.opcode === 0xb2ec ) {
		const result = u( 1 );
		if ( result === 1 ) return { ...campRecord(), opened: (state.opened ?? 0) + 1 };
		if ( result !== 2 ) throw Error( "Invalid academy opening answer" );
		if ( u( 1 ) === ACADEMY_OPEN_WAIT_CODE ) u( 4 );
		if ( at !== p.length ) throw Error( "Trailing academy opening bytes" );
		return state;
	}
	if ( frame.opcode === 0x3ac5 && p[0] === 2 ) {
		u( 1 );
		const row = memberRow();
		if ( at !== p.length ) throw Error( "Trailing academy member bytes" );
		const members = [ ...(state.members ?? []).filter( m => m.id !== row.id ), row ];
		if ( members.length > 8 ) throw Error( "Academy roster exceeds native capacity" );
		return { ...state, members };
	}
	if ( frame.opcode === 0xb701 ) {
		const flag = u( 1 );
		if ( flag !== 1 ) {
			const result = u( 1 );
			if ( flag !== 2 || at !== p.length ) throw Error( "Invalid academy failure" );
			return { ...state, request: state.request?.kind === "page" ? null : state.request, result };
		}
		const page = u( 1 ), pages = u( 1 ), count = u( 1 );
		if ( count > 12 ) throw Error( "Academy page exceeds native slots" );
		const rows: AcademyListing[] = [], ids = new Set<number>();
		for ( let i = 0; i < count; i++ ) {
			const id = u( 4 );
			u( 4 );
			const kind = u( 1 ), detail = str( true );
			u( 4 );
			u( 1 );
			const level = u( 1 ),
				model = u( 4 ),
				name = str( false ),
				students = u( 4 ),
				grade = u( 1 ),
				graduates = u( 4 );
			u( 4 );
			if ( !id || ids.has( id ) ) throw Error( "Duplicate academy listing" );
			ids.add( id );
			rows.push( { id, kind, detail, level, model, name, students, grade, graduates } );
		}
		if ( at !== p.length ) throw Error( "Trailing academy page bytes" );
		return {
			...state,
			page,
			pages,
			rows,
			request: state.request?.kind === "page" ? null : state.request,
			result: null
		};
	}
	if ( frame.opcode === 0xb592 ) {
		const flag = u( 1 ), result = u( 1 );
		if ( (flag !== 1 && flag !== 2) || at !== p.length ) throw Error( "Invalid academy join result" );
		return { ...state, request: state.request?.kind === "join" ? null : state.request, result };
	}
	// 771750: [1] and up to 50 honor-rank rows; [2] and one category-0x1D error byte.
	if ( frame.opcode === 0xb2d0 ) {
		const result = u( 1 );
		if ( result === 2 ) {
			u( 1 );
			if ( at !== p.length ) throw Error( "Trailing academy honor ranking bytes" );
			return state;
		}
		if ( result !== 1 ) throw Error( "Invalid academy honor ranking answer" );
		const count = u( 1 );
		if ( count > 50 ) throw Error( "Academy honor ranking exceeds native capacity" );
		const rows: AcademyHonorRow[] = [];
		for ( let i = 0; i < count; i++ ) {
			const ranking = u( 4 ),
				rank = u( 1 ),
				name = str( false ),
				level = u( 1 ),
				maxLevel = u( 1 ),
				graduates = u( 4 ),
				guild = str( false );
			rows.push( { ranking, rank, name, level, maxLevel, graduates, guild } );
		}
		if ( at !== p.length ) throw Error( "Trailing academy honor ranking bytes" );
		return {
			...state,
			honorRanks: { rows, sequence: (state.honorRanks?.sequence ?? 0) + 1 }
		};
	}
	return null;
}

/*
================
academyRank

81E9F0: rank is derived from role and current level, not the wire entry-level byte.
================
*/
export function academyRank( member: AcademyMember ): number {
	const { kind, level } = member;
	return kind === 0 ?
		(level >= 60 ? 7 : 0) :
		kind === 1 ?
		(level >= 50 ? 6 : level >= 40 ? 5 : 0) :
		level > 30 ?
		4 :
		level > 20 ?
		3 :
		level > 10 ?
		2 :
		level >= 1 ?
		1 :
		0;
}

/*
================
academyNoticeRequest

5F46F0 mode 2 validates before 704190 sanitizes and converts through 78E490/4B6960.
Native edit controls expose NUL-terminated UTF-16 text. Preserve spaces; no trim.
================
*/
export function academyNoticeRequest( subject: string, contents: string ): WireFrame | null {
	const fields = [ subject, contents ].map( s => s.split( "\0", 1 )[0]! );
	if ( fields.some( s => s.length === 0 ) ) return null;
	const encoded = fields.map( s => encodeWindows1252( s.replace( /["';]/g, " " ) ) );
	if ( encoded.some( s => s.length > 65535 ) ) throw Error( "Academy notice exceeds wire string length" );
	const payload = new Uint8Array( 4 + encoded[0]!.length + encoded[1]!.length ),
		view = new DataView( payload.buffer );
	let at = 0;
	for ( const bytes of encoded ) {
		view.setUint16( at, bytes.length, true );
		at += 2;
		payload.set( bytes, at );
		at += bytes.length;
	}
	return { opcode: 0x7220, payload };
}

/*
================
academyOpenRequest

702BD0's 0x72EC: one u32, which natively is the confirm box's own UI id
(5D26F0 pushes its wParam). The server reads and ignores it, so the port
sends zero.
================
*/
export function academyOpenRequest(): WireFrame {
	return { opcode: 0x72ec, payload: new Uint8Array( 4 ) };
}

/*
================
academyHonorRankRequest

703070's 0x72D0: [u32 gid] from 5D9CF0 row 0x2E on the storage NPC.
================
*/
export function academyHonorRankRequest( gid: number ): WireFrame {
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, gid >>> 0, true );
	return { opcode: 0x72d0, payload };
}

/*
================
academyDisbandRequest

702E70's 0x77C5: empty payload from 5C53C0 confirm case 1 (dissolve).
================
*/
export function academyDisbandRequest(): WireFrame {
	return { opcode: 0x77c5, payload: new Uint8Array( 0 ) };
}

/*
================
academyLeaveRequest

702FD0's 0x7785: empty payload from 5C76C0 confirm case 4 (leave).
================
*/
export function academyLeaveRequest(): WireFrame {
	return { opcode: 0x7785, payload: new Uint8Array( 0 ) };
}

/*
================
academyGraduateRequest

702F10's 0x710A: [u8 satisfaction] (4..0 from 6E9AC0, or 0 from 5C76C0 case 5).
================
*/
export function academyGraduateRequest( satisfaction: number ): WireFrame {
	if ( !Number.isInteger( satisfaction ) || satisfaction < 0 || satisfaction > 4 ) {
		throw Error( "Invalid academy graduate satisfaction" );
	}
	return { opcode: 0x710a, payload: Uint8Array.of( satisfaction ) };
}

/*
================
encodeAcademyMemberName

704BB0 / 704D30 write [u16 len][Windows-1252 bytes] for the target member's name.
================
*/
function encodeAcademyMemberName( name: string ): Uint8Array {
	const clean = name.split( "\0", 1 )[0]!;
	if ( !clean.length ) throw Error( "Empty academy member name" );
	const bytes = encodeWindows1252( clean );
	if ( bytes.length > 65535 ) throw Error( "Academy member name exceeds wire string length" );
	const payload = new Uint8Array( 2 + bytes.length );
	new DataView( payload.buffer ).setUint16( 0, bytes.length, true );
	payload.set( bytes, 2 );
	return payload;
}

/*
================
academyKickRequest

704BB0's 0x74D4: [u16 len][Windows-1252 name] from 5C76C0 confirm case 2 (ban).
================
*/
export function academyKickRequest( name: string ): WireFrame {
	return { opcode: 0x74d4, payload: encodeAcademyMemberName( name ) };
}

/*
================
academyCompulsoryGraduateRequest

704D30's 0x736D: [u16 len][Windows-1252 name] from 5C76C0 confirm case 1.
================
*/
export function academyCompulsoryGraduateRequest( name: string ): WireFrame {
	return { opcode: 0x736d, payload: encodeAcademyMemberName( name ) };
}
