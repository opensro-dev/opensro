/*
===========================================================================

party-matching.ts - party listings and correlated approval requests.

Wire decoding, admission and deadlines share this owner so timer and button
responses cannot disagree about which request is still answerable.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { readPartyMember, type PartyMember } from "./social";
const PARTY_JOIN_TIMEOUT_MS = 10000;
const PARTY_JOIN_NO_REPLY = 2;

/*
================
PartyRegistration
================
*/
export interface PartyRegistration {
	readonly id: number;
	readonly party: number;
	readonly type: number;
	readonly purpose: number;
	readonly min: number;
	readonly max: number;
	readonly title: string;
}
/*
================
PartyListing
================
*/
export interface PartyListing extends PartyRegistration {
	readonly name: string;
	readonly race: number;
	readonly members: number;
}
/*
================
PartyJoinRequest
================
*/
export interface PartyJoinRequest {
	readonly expires: number;
	readonly a: number;
	readonly b: number;
	readonly native7c0: number;
	readonly primary: number;
	readonly secondary: number;
	readonly flags: number;
	readonly member: PartyMember;
}
/*
================
PartyMatching
================
*/
export interface PartyMatching {
	readonly joining?: { readonly name: string; readonly since: number; } | null;
	readonly auto: readonly number[];
	readonly page: number;
	readonly pages: number;
	readonly rows: readonly PartyListing[];
	// The local player's own listing (PartyMatchManager +0x08). It lives until
	// a delete reply clears it: a list page that does not show it keeps it.
	readonly own: PartyRegistration | null;
	readonly request: PartyJoinRequest | null;
	readonly pending: "page" | "join" | "register" | "modify" | "delete" | null;
	readonly result: number | null;
}
/*
================
PartyMatchCommand
================
*/
export type PartyMatchCommand =
	| { readonly kind: "party-match-auto"; readonly ids: readonly number[]; }
	| { readonly kind: "party-match-auto-stop"; }
	| { readonly kind: "party-match-page"; readonly page: number; }
	| { readonly kind: "party-match-join"; readonly id: number; }
	| {
		readonly kind: "party-match-register" | "party-match-modify";
		readonly registration: Omit<PartyRegistration, "id">;
	}
	| { readonly kind: "party-match-delete"; }
	| { readonly kind: "party-match-answer"; readonly a: number; readonly b: number; readonly answer: 0 | 1 | 2; };
/*
================
emptyPartyMatching
================
*/
export function emptyPartyMatching(): PartyMatching {
	return { auto: [], page: 1, pages: 0, rows: [], own: null, request: null, pending: null, result: null };
}
/*
================
partyMatchRequest
================
*/
export function partyMatchRequest(
	state: PartyMatching,
	command: PartyMatchCommand,
	now = 0
): { state: PartyMatching; frame: WireFrame | null; } {
	const bytes: number[] = [];
	const u = ( x: number, n: number ) => {
		if ( !Number.isInteger( x ) || x < 0 || x > (n === 4 ? 0xffffffff : 255) ) {
			throw Error( "Invalid party matching request" );
		}
		for ( let i = 0; i < n; i++ ) bytes.push( (x >>> (i * 8)) & 255 );
	};
	if ( command.kind === "party-match-auto-stop" ) return { state: { ...state, auto: [] }, frame: null };
	if ( command.kind === "party-match-auto" ) {
		if ( state.pending ) throw Error( "Party matching request pending" );
		if (
			command.ids.length > 10 || new Set( command.ids ).size !== command.ids.length ||
			command.ids.some( id => !state.rows.some( r => r.id === id ) )
		) throw Error( "Invalid auto-match candidates" );
		const [id, ...auto] = command.ids;
		if ( id === undefined ) return { state: { ...state, auto: [], result: 2 }, frame: null };
		const out = partyMatchRequest( { ...state, auto }, { kind: "party-match-join", id }, now );
		return out;
	}
	if ( command.kind === "party-match-answer" ) {
		if (
			!state.request || state.request.a !== command.a || state.request.b !== command.b ||
			![ 0, 1, 2 ].includes( command.answer )
		) throw Error( "Stale party join answer" );
		// 63CEAE arms 10000ms; 63C340 sends no-reply (2) when it elapses.
		const answer = now >= state.request.expires ? PARTY_JOIN_NO_REPLY : command.answer;
		u( command.a, 4 );
		u( command.b, 4 );
		u( answer, 1 );
		return { state: { ...state, request: null }, frame: { opcode: 0x30fa, payload: Uint8Array.from( bytes ) } };
	}
	if ( state.pending ) throw Error( "Party matching request pending" );
	let opcode: number, pending: PartyMatching["pending"];
	if ( command.kind === "party-match-page" ) {
		u( command.page, 1 );
		opcode = 0x7588;
		pending = "page";
	} else if ( command.kind === "party-match-join" ) {
		if ( !state.rows.some( r => r.id === command.id ) ) throw Error( "Missing party listing" );
		u( command.id, 4 );
		opcode = 0x75bf;
		pending = "join";
	} else if ( command.kind === "party-match-delete" ) {
		if ( !state.own ) throw Error( "No owned party listing" );
		u( state.own.id, 4 );
		opcode = 0x7535;
		pending = "delete";
	} else {
		const r = command.registration, modify = command.kind === "party-match-modify";
		if ( modify ? !state.own : !!state.own ) throw Error( "Party registration ownership mismatch" );
		if (
			r.type & ~7 || r.purpose > 3 || r.min < 1 || r.max > 90 || r.min > r.max || !r.title.length ||
			r.title.length > 50
		) throw Error( "Invalid party registration" );
		u( modify ? state.own!.id : 0, 4 );
		u( r.party, 4 );
		u( r.type, 1 );
		u( r.purpose, 1 );
		u( r.min, 1 );
		u( r.max, 1 );
		bytes.push( r.title.length, 0 );
		for ( let i = 0; i < r.title.length; i++ ) {
			const c = r.title.charCodeAt( i );
			bytes.push( c & 255, c >>> 8 );
		}
		opcode = modify ? 0x73dc : 0x76ff;
		pending = modify ? "modify" : "register";
	}
	return {
		state: {
			...state,
			pending,
			result: null,
			...(command.kind === "party-match-join" ?
				{ joining: { name: state.rows.find( r => r.id === command.id )!.name, since: now } } :
				{})
		},
		frame: { opcode, payload: Uint8Array.from( bytes ) }
	};
}
// 75E5E0 / 75DC40 / 75EA70: decode completely before committing any state.
/*
================
partyMatchPacket
================
*/
export function partyMatchPacket(
	state: PartyMatching,
	frame: WireFrame,
	localName = "",
	now = 0,
	local: PartyMatchLocal = { race: 0, members: 1 }
): PartyMatching | null {
	const op = frame.opcode;
	if ( ![ 0xb588, 0xb5bf, 0xb6ff, 0xb3dc, 0xb535, 0x75bf ].includes( op ) ) return null;
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let at = 0;
	const u = ( n: number ) => {
		if ( at + n > p.length ) throw Error( "Truncated party matching packet" );
		const x = n === 1 ? p[at]! : n === 2 ? v.getUint16( at, true ) : v.getUint32( at, true );
		at += n;
		return x;
	};
	const str = ( wide: boolean ) => {
		const n = u( 2 ) * (wide ? 2 : 1);
		if ( n > 8192 || at + n > p.length ) throw Error( "Invalid matching string" );
		const s = new TextDecoder( wide ? "utf-16le" : "windows-1252", { fatal: true } ).decode(
			p.subarray( at, at + n )
		);
		at += n;
		return s;
	};
	const registration = (): PartyRegistration => {
		const id = u( 4 ),
			party = u( 4 ),
			type = u( 1 ),
			purpose = u( 1 ),
			min = u( 1 ),
			max = u( 1 ),
			title = str( true );
		if ( !id ) throw Error( "Missing party registration identity" );
		return { id, party, type, purpose, min, max, title };
	};
	let next: PartyMatching;
	if ( op === 0x75bf ) {
		const a = u( 4 ),
			b = u( 4 ),
			native7c0 = u( 4 ),
			primary = u( 4 ),
			secondary = u( 4 ),
			flags = u( 1 ),
			decoded = readPartyMember( p, at );
		at = decoded.end;
		if ( !a || !b ) throw Error( "Invalid join correlation" );
		next = {
			...state,
			request: {
				expires: now + PARTY_JOIN_TIMEOUT_MS,
				a,
				b,
				native7c0,
				primary,
				secondary,
				flags,
				member: decoded.member
			}
		};
	} else {
		const flag = u( 1 );
		if ( flag !== 1 && flag !== 2 ) throw Error( "Invalid matching status" );
		const expected = op === 0xb588 ?
			"page" :
			op === 0xb5bf ?
			"join" :
			op === 0xb6ff ?
			"register" :
			op === 0xb3dc ?
			"modify" :
			"delete";
		const pending = state.pending === expected ? null : state.pending;
		if ( flag === 2 || op === 0xb5bf ) {
			const result = u( 1 );
			if ( flag === 1 && result > 2 ) throw Error( "Invalid join result" );
			next = {
				...state,
				pending,
				result,
				joining: op === 0xb5bf ? null : state.joining,
				auto: op === 0xb5bf && flag === 1 && result === 1 ? [] : state.auto
			};
		} else if ( op === 0xb535 ) {
			const id = u( 4 );
			next = {
				...state,
				pending,
				own: state.own?.id === id ? null : state.own,
				rows: state.rows.filter( r => r.id !== id ),
				result: null
			};
		} else if ( op === 0xb6ff || op === 0xb3dc ) {
			// PartyMatchManager_SetOwnRecord (80E6B0) builds the listing from the
			// reply plus the local name, race and party size, and
			// CIFPartyMatch_RebuildRows (636CE0) shows it at once: a new listing
			// never waits for the next page request.
			const own = registration(),
				row: PartyListing = { ...own, name: localName, race: local.race, members: local.members };
			next = {
				...state,
				pending,
				own,
				rows: state.rows.some( r => r.id === own.id ) ?
					state.rows.map( r => r.id === own.id ? { ...r, ...own } : r ) :
					[ row, ...state.rows ],
				result: null
			};
		} else {
			const page = u( 1 ), pages = u( 1 ), count = u( 1 ), rows: PartyListing[] = [], ids = new Set<number>();
			for ( let i = 0; i < count; i++ ) {
				const id = u( 4 ),
					party = u( 4 ),
					name = str( false ),
					race = u( 1 ),
					members = u( 1 ),
					type = u( 1 ),
					purpose = u( 1 ),
					min = u( 1 ),
					max = u( 1 ),
					title = str( true );
				if ( !id || ids.has( id ) ) throw Error( "Duplicate matching entry" );
				ids.add( id );
				rows.push( { id, party, name, race, members, type, purpose, min, max, title } );
			}
			next = {
				...state,
				page,
				pages,
				rows,
				pending,
				// A page shows one slice of the board: it can reveal the own
				// listing (after a relog) but never retire it; only 0xB535 does.
				own: (localName ? rows.find( r => r.name === localName ) : undefined) ?? state.own,
				result: null
			};
		}
	}
	if ( at !== p.length ) throw Error( "Trailing matching bytes" );
	return next;
}
// 637CF0 uses exact name equality and inclusive overlapping level intervals.
/*
================
partyMatchRows
================
*/
export function partyMatchRows(
	rows: readonly PartyListing[],
	filter: { name: string; purpose: number; min: number; max: number; },
	sort: keyof PartyListing = "id",
	descending = false
): PartyListing[] {
	return rows.filter( r =>
		(!filter.name || r.name === filter.name) && (filter.purpose === 4 || r.purpose === filter.purpose) &&
		r.min <= filter.max && r.max >= filter.min
	).sort( ( a, b ) => {
		const x = a[sort], y = b[sort];
		return (x < y ? -1 : x > y ? 1 : 0) * (descending ? -1 : 1);
	} );
}

// 63B130: native walks the current filtered/sorted candidates, limits ten,
// compares all three option bits, race (2=all), purpose and own level.
/*
================
partyAutoCandidates
================
*/
export function partyAutoCandidates(
	rows: readonly PartyListing[],
	purpose: number,
	race: number,
	type: number,
	level: number
): number[] {
	return rows.filter( r =>
		r.purpose === purpose && (race === 2 || r.race === race) && r.type === type && r.min <= level && r.max >= level
	).slice( 0, 10 ).map( r => r.id );
}

/*
================
partyCharacterCountries
================
*/
export function partyCharacterCountries( value: unknown ): Readonly<Record<number, number>> {
	const v = value as { format?: unknown; rows?: unknown; };
	if ( v?.format !== "sro-chardata-country" || !Array.isArray( v.rows ) ) {
		throw Error( "Invalid character country catalog" );
	}
	const out: Record<number, number> = {};
	for ( const row of v.rows ) {
		if ( typeof row !== "string" || !/^\d+\t\d+$/.test( row ) ) throw Error( "Invalid character country row" );
		const [id, country] = row.split( "\t" ).map( Number );
		if ( !id || country === undefined || country > 255 || id in out ) {
			throw Error( "Invalid character country identity" );
		}
		out[id] = country;
	}
	return out;
}

// 856920 reads active class +4F5; 868D00 derives it from the job suit.
// CICUser+782 job membership is a different field and must not gate this form.
/*
================
partyActiveJob
================
*/
export function partyActiveJob( inventory: readonly { readonly slot: number; readonly typeFlags: number; }[] ): number {
	const flags = inventory.find( row => row.slot === 8 )?.typeFlags ?? 0, job = flags >>> 11;
	return (flags & 0x7fe) === 0x3ac && job >= 1 && job <= 3 ? job : 4;
}
// 63C010 / server 5BF240: only registration and modification use this matrix.
/*
================
partyPurposeAllowed
================
*/
export function partyPurposeAllowed( job: number, purpose: number ): boolean {
	if ( !Number.isInteger( purpose ) || purpose < 0 || purpose > 3 ) return false;
	return job === 1 || job === 3 ? purpose === 2 : job === 2 ? purpose === 3 : job === 4 ? purpose < 2 : false;
}
/*
================
partyDefaultPurpose
================
*/
export function partyDefaultPurpose( job: number ): number {
	return job === 2 ? 3 : job === 1 || job === 3 ? 2 : 0;
}

/*
================
PartyMatchLocal

The local facts PartyMatchManager_SetOwnRecord (80E6B0) adds to a listing:
the player's race (country byte) and the party size, 1 outside a party.
================
*/
export interface PartyMatchLocal {
	readonly race: number;
	readonly members: number;
}

/*
================
partyMatchButtons

CIFPartyMatch_RefreshButtons (634A10), control by control:
  0x0F join, 0x11 auto: no own listing, outside a party, level >= 1, rows shown
  0x10 whisper: rows shown
  0x12 form: no own listing, and either outside a party at level >= 5 or the
       leader (SPartyData_IsLocalLeader) of a party that is not full
       (CCharacterDependentData_IsPartyFull: 4 members, 8 with EXP share)
  0x13 modify, 0x14 delete: an own listing registered under the local name
================
*/
export function partyMatchButtons( input: {
	readonly own: PartyRegistration | null;
	readonly ownName: string;
	readonly localName: string;
	readonly inParty: boolean;
	readonly leader: boolean;
	readonly members: number;
	readonly options: number;
	readonly level: number;
	readonly rows: number;
} ): Readonly<Record<15 | 16 | 17 | 18 | 19 | 20, boolean>> {
	const full = input.members >= ((input.options & 1) ? 8 : 4),
		open = !input.own && !input.inParty && input.level >= 1 && input.rows > 0,
		form = !input.own &&
			(!input.inParty && input.level >= 5 || input.inParty && input.leader && !full),
		owned = !!input.own && input.ownName === input.localName;
	return { 15: open, 16: input.rows > 0, 17: open, 18: form, 19: owned, 20: owned };
}
