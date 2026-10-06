/*
===========================================================================

fortress.ts - the fortress war as the client sees it

The fortress state built from the entry bootstrap and the 0x3887 stream
(CNetProcessSecond_OnFortressWarState 76C870): the fortress list, the war
periods, the registered war guilds, the local war role and record. Also
the fortress official's protocol: 0x71E1 requests
(CGInterface_SendFortressInteraction71E1 703130) and their 0xB1E1 answers
(CPSMission_OnFortressManagerResponse0xB1E1 754A40).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { SystemNotice } from "./system-notices";
import { fortressServiceReply, FORTRESS_SERVICE_REPLY, type FortressServiceReply } from "./fortress-services";

export const OP_FORTRESS_INTERACTION = 0x71e1;
export const OP_FORTRESS_INTERACTION_RESULT = 0xb1e1;
export const OP_FORTRESS_WAR_STATE = 0x3887;

// 0x71E1 subtypes the official sends (CIFNpcTalk action 0x34 and the
// application window, 6649C0).
export const FORTRESS_WAR_STATUS = 6;
export const FORTRESS_WAR_APPLY = 7;
export const FORTRESS_WAR_WITHDRAW = 8;

// The application kind byte: an attacker, or the owner's ally.
export const FORTRESS_REQUEST_ATTACK = 0;
export const FORTRESS_REQUEST_ALLY = 1;

// 754A40 shows an official refusal as native notice category 0x1E.
export const FORTRESS_NOTICE_CATEGORY = 0x1e;

/*
================
FortressRow

One siegefortress.txt row: its id, codename, icon, name symbol and the
official who takes its applications.
================
*/
export interface FortressRow {
	readonly id: number;
	readonly code: string;
	readonly icon?: string;
	readonly nameStrId?: string;
	readonly official?: string;
	readonly officialRefObjId?: number;
	// The gold an attacking guild pays to apply (row +0x80).
	readonly requestFee?: number;
}

/*
================
FortressState
================
*/
export interface FortressState {
	readonly localKills?: number;
	readonly localDeaths?: number;
	readonly role?: number;
	readonly worldId: number;
	readonly worlds: readonly { id: number; code: string; }[];
	readonly fortresses: readonly FortressRow[];
	readonly wars: readonly {
		id: number;
		name: string;
		flags: number;
		captureWait?: number;
		stoneWait?: number;
	}[];
	readonly countdownAtMs?: number;
	readonly registered: readonly number[];
	readonly listId: number;
	// The last fortress staff answer (fortress-services.ts); the official's
	// application answers also reach fortressManagerReply.
	readonly service?: FortressServiceReply;
}

/*
================
fortressBootstrap
================
*/
export function fortressBootstrap( value: unknown ): FortressState {
	const b = value as {
		localPlayerEntry?: { fortressWorld?: number; };
		gameWorldData?: { gameWorldId: number; warName: string; }[];
		siegeFortressData?: {
			fortressId: number;
			codeName: string;
			icon?: string;
			nameStrId?: string;
			officialNpcCode?: string;
			requestFee?: number;
			officialRefObjId?: number;
		}[];
	};
	const worlds = (b.gameWorldData ?? []).map( r => ({ id: r.gameWorldId, code: r.warName }) ),
		fortresses = (b.siegeFortressData ?? []).map( r => ({
			id: r.fortressId,
			code: r.codeName,
			icon: r.icon,
			nameStrId: r.nameStrId,
			official: r.officialNpcCode,
			requestFee: r.requestFee,
			officialRefObjId: r.officialRefObjId
		}) );
	for ( const rows of [ worlds, fortresses ] ) {
		if (
			rows.length > 65536 || new Set( rows.map( r => r.id ) ).size !== rows.length ||
			rows.some( r => !Number.isInteger( r.id ) || r.id < 0 || r.id > 0xffffffff || typeof r.code !== "string" )
		) throw Error( "Invalid fortress reference table" );
	}
	// 77B220 -> 862B80: preserve the complete packed word, then resolve its low u16.
	const worldId = b.localPlayerEntry?.fortressWorld ?? 0x10001;
	if ( !Number.isInteger( worldId ) || worldId < 0 || worldId > 0xffffffff ) {
		throw Error( "Invalid packed fortress world" );
	}
	return { worldId, worlds, fortresses, wars: [], registered: [], listId: 0 };
}

const FORTRESS_TIMER_PERIOD_MS = 1000;
const FORTRESS_STONE_WAIT_SECONDS = 180;

/*
================
fortressPacket

Folds one 0x3887 frame, or a fortress staff 0xB1E1 answer, into the
state; null for frames it does not own.
================
*/
export function fortressPacket( state: FortressState, frame: WireFrame, now = 0 ): FortressState | null {
	if ( frame.opcode === FORTRESS_SERVICE_REPLY ) {
		const service = fortressServiceReply( frame );
		return service ? { ...state, service } : null;
	}
	if ( frame.opcode !== OP_FORTRESS_WAR_STATE ) return null;
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	function take( n: number ) {
		if ( o + n > p.length ) throw Error( "Truncated fortress frame" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	function str() {
		const n = u16(), at = take( n );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
	}
	const subtype = u8();
	let next = state;
	if ( subtype === 0 ) {
		const count = u8(), wars: FortressState["wars"][number][] = [];
		for ( let i = 0; i < count; i++ ) {
			const id = u32(), name = str();
			for ( let j = 0; j < 4; j++ ) u32();
			const captureWait = u8() === 1 ? u32() : 0, stoneWait = u8() === 1 ? u32() : 0;
			const at = wars.findIndex( r => r.id === id ), row = { id, name, flags: 0, captureWait, stoneWait };
			if ( at < 0 ) wars.push( row );
			else wars[at] = row;
		}
		const flags = u8(), listId = u32();
		next = {
			...state,
			wars: wars.map( r => ({ ...r, flags }) ),
			listId,
			countdownAtMs: state.countdownAtMs ?? now
		};
	} else if ( subtype === 8 ) {
		const id = u32(), name = str();
		for ( let j = 0; j < 4; j++ ) u32();
		next = { ...state, wars: state.wars.map( r => r.id === id ? { ...r, name } : r ) };
	} else if ( subtype === 0x0a ) {
		const id = u32();
		next = {
			...state,
			countdownAtMs: state.countdownAtMs ?? now,
			wars: state.wars.map( row => row.id === id ? { ...row, stoneWait: FORTRESS_STONE_WAIT_SECONDS } : row )
		};
	} else if ( subtype === 0x0c || subtype === 0x0d ) {
		const id = u32();
		u8();
		next = { ...state, listId: subtype === 0x0c ? id : 0 };
	} else if ( subtype === 0x11 ) {
		u32();
		const localKills = u32(), localDeaths = u32();
		next = { ...state, localKills, localDeaths };
	} else if ( subtype === 0x12 ) {
		u32();
		next = { ...state, role: u8() };
	} else if ( subtype === 0x10 ) {
		u32();
		const count = u8(), registered: number[] = [];
		for ( let i = 0; i < count; i++ ) {
			const id = u32();
			if ( id && !registered.includes( id ) ) registered.push( id );
		}
		next = { ...state, registered };
	} else if ( [ 2, 6, 0x31, 0x32, 0x33, 0x34 ].includes( subtype ) ) {
		const bit = subtype === 2 || subtype === 6 ? 1 : subtype < 0x33 ? 4 : 2,
			set = subtype === 2 || subtype === 0x31 || subtype === 0x33;
		// 7E2100 uses XOR on the off arm, not AND-NOT.
		next = {
			...state,
			wars: state.wars.map( r => ({
				...r,
				flags: set ? r.flags | bit : r.flags ^ bit,
				...(subtype === 6 ? { captureWait: 0, stoneWait: 0 } : {})
			}) )
		};
	} else if ( ![ 1, 3, 4, 5, 9 ].includes( subtype ) ) return null;
	if ( o !== p.length ) throw Error( "Trailing fortress frame bytes" );
	return next;
}

/*
================
fortressRegistrationNotice

76C870 cases 0x0C and 0x0D: the applying guild's banner, naming the
fortress (7D4E70). An application of either kind reads
WARAPPLY_COMPLETE; a withdrawal reads the war or union cancel text.
================
*/
export function fortressRegistrationNotice( state: FortressState, frame: WireFrame ): SystemNotice | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_FORTRESS_WAR_STATE || (p[0] !== 0x0c && p[0] !== 0x0d) || p.length !== 6 ) return null;
	const id = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true ), kind = p[5]!;
	const key = p[0] === 0x0c ?
		"UIIT_MSG_FORT_OFFICIAL_WARAPPLY_COMPLETE" :
		kind === FORTRESS_REQUEST_ATTACK ?
		"UIIT_MSG_FORT_OFFICIAL_WARAPPLY_CANCEL" :
		"UIIT_MSG_FORT_OFFICIAL_UNIONAPPLY_CANCEL";
	const name = state.fortresses.find( r => r.id === id )?.nameStrId ?? null;
	return { key, value: 0, localizedArguments: [ name ], banner: true, bannerOnly: true };
}

export const FORTRESS_CONQUEST = 0x08;
export const FORTRESS_TOWERS_FALLEN = 0x0a;
export const FORTRESS_STRUCTURE_STATE = 0x0b;
// 76C870 case 0xB: state bit 0 shows the destroyed stage and its notice.
const STRUCTURE_STATE_DESTROYED = 1;

/*
================
FortressStructureState

76C870 case 0xB: u32 fortress, u32 object, u32 event zone, u16 state, and
for a headquarters (4F3A80 on the zone's structure) its guild's name.
================
*/
export interface FortressStructureState {
	readonly fortressId: number;
	readonly gid: number;
	readonly eventStructId: number;
	readonly state: number;
	readonly guildName?: string;
}

/*
================
fortressStructureState

Reads a subtype-0x0B frame; null for any other. The guild name follows
only a headquarters' row, so whatever follows the state word is it.
================
*/
export function fortressStructureState( frame: WireFrame ): FortressStructureState | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_FORTRESS_WAR_STATE || p[0] !== FORTRESS_STRUCTURE_STATE ) return null;
	if ( p.length < 15 ) throw Error( "Truncated fortress structure state" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	const row = {
		fortressId: v.getUint32( 1, true ),
		gid: v.getUint32( 5, true ),
		eventStructId: v.getUint32( 9, true ),
		state: v.getUint16( 13, true )
	};
	if ( p.length === 15 ) return row;
	const n = v.getUint16( 15, true );
	if ( p.length !== 17 + n ) throw Error( "Invalid fortress structure state length" );
	return { ...row, guildName: new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 17 ) ) };
}

/*
================
fortressCaptureNotice

76C870's capture arms: case 8 names the guild and the fortress it took
(UIIT_MSG_FORT_WAR_CONQUER), case 0xA tells that the stone's guard is
falling (UIIT_MSG_FORT_STRUCTURE_STATUS_CANCEL), and case 0xB names a
destroyed structure, or a removed headquarters with its guild.
structureName is the 0xB row's structure as the player sees it; a
structure the player cannot see has no name to print and no notice.
================
*/
export function fortressCaptureNotice(
	state: FortressState,
	frame: WireFrame,
	structureName: string | undefined
): SystemNotice | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_FORTRESS_WAR_STATE ) return null;
	if ( p[0] === FORTRESS_CONQUEST ) {
		const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
		if ( p.length < 7 ) throw Error( "Truncated fortress conquest" );
		const id = v.getUint32( 1, true ), n = v.getUint16( 5, true );
		if ( p.length !== 23 + n ) throw Error( "Invalid fortress conquest length" );
		const guild = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 7, 7 + n ) );
		const fortress = state.fortresses.find( r => r.id === id )?.nameStrId ?? null;
		return {
			key: "UIIT_MSG_FORT_WAR_CONQUER",
			value: 0,
			arguments: [ guild, "" ],
			localizedArguments: [ null, fortress ],
			banner: true
		};
	}
	if ( p[0] === FORTRESS_TOWERS_FALLEN ) {
		if ( p.length !== 5 ) throw Error( "Invalid fortress tower fall" );
		return { key: "UIIT_MSG_FORT_STRUCTURE_STATUS_CANCEL", value: 0, banner: true, bannerOnly: true };
	}
	const row = fortressStructureState( frame );
	if ( !row ) return null;
	const name = structureName;
	if ( name === undefined ) return null;
	if ( row.guildName !== undefined && row.state === 0 ) {
		return { key: "UIIT_MSG_FORT_CAMP_STATUS_DESTROY", value: 0, arguments: [ row.guildName, name ], banner: true };
	}
	if ( !(row.state & STRUCTURE_STATE_DESTROYED) ) return null;
	return { key: "UIIT_MSG_FORT_STRUCTURE_STATUS_DESTROY", value: 0, arguments: [ name ], banner: true };
}

/*
================
fortressActive
================
*/
export function fortressActive( state: FortressState ): boolean {
	const code = state.worlds.find( r => r.id === (state.worldId & 65535) )?.code,
		id = state.fortresses.find( r => r.code === code )?.id;
	return id !== undefined && ((state.wars.find( r => r.id === id )?.flags ?? 0) & 1) !== 0;
}

/*
================
fortressMusicActive
================
*/
export function fortressMusicActive( state: FortressState ): boolean {
	const code = state.worlds.find( r => r.id === (state.worldId & 65535) )?.code;
	const id = state.fortresses.find( r => r.code === code )?.id;
	return !!id && ((state.wars.find( r => r.id === id )?.flags ?? 0) & 1) !== 0;
}

/*
================
fortressMusicMode

76CDD3 chooses 3 AFTER start flags; 76D156 chooses 0 BEFORE end flags.
Other fortress messages do not implicitly recompute the latched music mode.
================
*/
export function fortressMusicMode(
	mode: number,
	before: FortressState,
	after: FortressState,
	subtype: number
): number {
	if ( subtype === 2 && fortressMusicActive( after ) ) return 3;
	if ( subtype === 6 && fortressMusicActive( before ) ) return 0;
	return mode;
}

/*
================
fortressStatus

828150 includes equality with the local guild even when absent from +94.
================
*/
export function fortressStatus(
	state: FortressState,
	local: number,
	target: number,
	allies: readonly number[]
): number {
	if ( !fortressActive( state ) ) return 0xcd;
	if ( !local || !target ) return 0xc8;
	if ( state.registered.includes( local ) ) return state.registered.includes( target ) ? 0xc9 : 0xcc;
	if ( state.registered.includes( target ) ) return 0xca;
	return local === target || allies.includes( target ) ? 0xcb : 0xcc;
}

// ============================================================================

/*
================
fortressInteraction

0x71E1 [u32 npc][u8 subtype], and for an application or withdrawal
[u32 fortress][u8 kind] (703130's generic branch, cases 7 and 8).
================
*/
export function fortressInteraction( npc: number, subtype: number, fortress = 0, kind = 0 ): WireFrame {
	const status = subtype === FORTRESS_WAR_STATUS;
	const payload = new Uint8Array( status ? 5 : 10 ), v = new DataView( payload.buffer );
	v.setUint32( 0, npc >>> 0, true );
	v.setUint8( 4, subtype );
	if ( !status ) {
		v.setUint32( 5, fortress >>> 0, true );
		v.setUint8( 9, kind );
	}
	return { opcode: OP_FORTRESS_INTERACTION, payload };
}

/*
================
FortressWarTime

The SYSTEMTIME the official sends: the next war's start.
================
*/
export interface FortressWarTime {
	readonly year: number;
	readonly month: number;
	readonly weekday: number;
	readonly day: number;
	readonly hour: number;
	readonly minute: number;
}

/*
================
FortressApplication

The guild's standing at the official: the next war, and the fortress and
kind it applied with, or null.
================
*/
export interface FortressApplication {
	readonly warStart: FortressWarTime | null;
	readonly applied: { readonly fortress: number; readonly kind: number; } | null;
}

/*
================
FortressManagerReply
================
*/
export type FortressManagerReply =
	| { readonly ok: true; readonly subtype: number; readonly application?: FortressApplication; }
	| { readonly ok: false; readonly subtype: number; readonly code: number; };

/*
================
fortressManagerReply

754A40 for the official's subtypes: [u8 subtype][u8 1|2], then on success
subtype 6's SYSTEMTIME, applied flag and application, or subtype 7/8's
fortress and kind; on refusal the code byte. Other subtypes belong to
the fortress manager and answer null here.
================
*/
export function fortressManagerReply(
	frame: WireFrame,
	previous: FortressApplication | null
): FortressManagerReply | null {
	if ( frame.opcode !== OP_FORTRESS_INTERACTION_RESULT ) return null;
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength ), subtype = p[0];
	if ( subtype !== FORTRESS_WAR_STATUS && subtype !== FORTRESS_WAR_APPLY && subtype !== FORTRESS_WAR_WITHDRAW ) {
		return null;
	}
	if ( p[1] === 2 ) {
		if ( p.length !== 3 ) throw Error( "Invalid fortress official refusal" );
		return { ok: false, subtype, code: p[2]! };
	}
	if ( p[1] !== 1 ) throw Error( "Invalid fortress official result" );
	if ( subtype === FORTRESS_WAR_STATUS ) {
		if ( p.length !== 19 && p.length !== 24 ) throw Error( "Invalid fortress war status" );
		const word = ( at: number ) => v.getUint16( 2 + at * 2, true );
		const warStart = {
			year: word( 0 ),
			month: word( 1 ),
			weekday: word( 2 ),
			day: word( 3 ),
			hour: word( 4 ),
			minute: word( 5 )
		};
		const applied = p[18] === 1 ? { fortress: v.getUint32( 19, true ), kind: p[23]! } : null;
		if ( (applied !== null) !== (p.length === 24) ) throw Error( "Invalid fortress war status" );
		return { ok: true, subtype, application: { warStart, applied } };
	}
	if ( p.length !== 7 ) throw Error( "Invalid fortress application result" );
	const applied = subtype === FORTRESS_WAR_APPLY ? { fortress: v.getUint32( 2, true ), kind: p[6]! } : null;
	return { ok: true, subtype, application: { warStart: previous?.warStart ?? null, applied } };
}

/*
================
advanceFortressCountdowns

7E22F0 decrements every nonzero counter once. A00BE0 fires an overdue
state timer once and resets its baseline to now, without catch-up ticks.
The timer stops on the following tick after all counters reach zero.
================
*/
export function advanceFortressCountdowns( state: FortressState, now: number ): FortressState {
	if ( state.countdownAtMs === undefined || now - state.countdownAtMs < FORTRESS_TIMER_PERIOD_MS ) return state;
	if ( !state.wars.some( row => (row.captureWait ?? 0) > 0 || (row.stoneWait ?? 0) > 0 ) ) {
		return { ...state, countdownAtMs: undefined };
	}
	return {
		...state,
		countdownAtMs: now,
		wars: state.wars.map( row => ({
			...row,
			captureWait: Math.max( 0, (row.captureWait ?? 0) - 1 ),
			stoneWait: Math.max( 0, (row.stoneWait ?? 0) - 1 )
		}) )
	};
}

/*
================
fortressCountdownNotices

6B5A50 announces whole minutes, 90 seconds, and each of the last 30
seconds. These are banner-only notices for the guild's active fortress.
================
*/
export function fortressCountdownNotices( state: FortressState ): SystemNotice[] {
	const war = state.wars.find( row => row.id === state.listId && (row.flags & 1) !== 0 );
	if ( !war ) return [];
	const out: SystemNotice[] = [];
	for (
		const [seconds, key] of [
			[ war.captureWait ?? 0, "UIIT_MSG_FORT_ETC_ENTER_COUNTDOWN" ],
			[ war.stoneWait ?? 0, "UIIT_MSG_FORT_ATTACK_FORT_STONE_COUNTDOWN" ]
		] as const
	) {
		if ( seconds > 0 && (seconds % 60 === 0 || seconds === 90 || seconds <= 30) ) {
			out.push( { key, value: seconds, banner: true, bannerOnly: true } );
		}
	}
	return out;
}
