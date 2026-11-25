/*
===========================================================================

caravan-hire.ts - the AI caravan hire window (port-only 0x3FCF / 0x7FCF, not native)

The server opens and refreshes the window with a JSON body (the agents
hired where the player stands, available ones first, and a Trader's goods
choices by star); the window's request is u8 operation (0 refresh,
1 hire, 2 lead, 3 extend), u8 stars, then the agent's name. 0x3FD0 tells a hirer
who leads its hired Traders and who may (itself and them); the party
board's lead badges send operation 2 with the chosen name. 0x3FD1 follows
an NPC select when that NPC hires agents (u32 gid, the row's label); the
NPC's menu lists that row first and its click sends operation 0. A client
that predates the frames drops the unknown opcodes.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const CARAVAN_HIRE_OPCODE = 0x3fcf;
export const CARAVAN_HIRE_REQUEST_OPCODE = 0x7fcf;
export const CARAVAN_LEAD_OPCODE = 0x3fd0;
export const CARAVAN_HIRE_NPC_OPCODE = 0x3fd1;

export interface CaravanHireRow {
	readonly name: string;
	readonly display: string;
	readonly role: string;
	readonly town: string;
	readonly available: boolean;
	// The player's own Hunter or Thief, renewable here (operation 3).
	readonly extend: boolean;
	readonly status: string;
}

export interface CaravanHireStars {
	readonly stars: number;
	readonly goods: string;
	readonly total: string;
}

export interface CaravanHire {
	readonly sequence: number;
	readonly title: string;
	readonly fee: string;
	readonly message: string;
	readonly rows: readonly CaravanHireRow[];
	readonly stars: readonly CaravanHireStars[];
}

export interface CaravanHireNpc {
	readonly gid: number;
	readonly label: string;
}

export interface CaravanLead {
	readonly leader: string;
	readonly candidates: readonly { readonly name: string; readonly display: string; }[];
}

const text = ( v: unknown, max: number ): string => {
	if ( typeof v !== "string" || v.length > max ) throw new Error( "Invalid caravan hire" );
	return v;
};

/*
================
createCaravanHire
================
*/
export function createCaravanHire() {
	let hire: CaravanHire | null = null;
	let lead: CaravanLead | null = null;
	let hireNpc: CaravanHireNpc | null = null;
	let sequence = 0;
	return {
		/*
================
receive

Consumes a hire window frame; false for any other opcode. A malformed
body is a protocol error, never a partial window.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode === CARAVAN_HIRE_NPC_OPCODE ) {
				if ( frame.payload.length < 5 || frame.payload.length > 4 + 64 ) {
					throw new Error( "Invalid caravan hire NPC" );
				}
				const gid = new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true );
				hireNpc = {
					gid,
					label: new TextDecoder( "utf-8", { fatal: true } ).decode( frame.payload.subarray( 4 ) )
				};
				return true;
			}
			if ( frame.opcode === CARAVAN_LEAD_OPCODE ) {
				const raw = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( frame.payload ) ) as Record<
					string,
					unknown
				>;
				if ( raw.version !== 1 || !Array.isArray( raw.candidates ) || raw.candidates.length > 16 ) {
					throw new Error( "Invalid caravan lead" );
				}
				const candidates = raw.candidates.map( ( c: Record<string, unknown> ) => ({
					name: text( c.name, 64 ),
					display: text( c.display, 64 )
				}) );
				lead = candidates.length ? { leader: text( raw.leader, 64 ), candidates } : null;
				return true;
			}
			if ( frame.opcode !== CARAVAN_HIRE_OPCODE ) return false;
			const raw = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( frame.payload ) ) as Record<
				string,
				unknown
			>;
			if (
				raw.version !== 1 || !Array.isArray( raw.rows ) || !Array.isArray( raw.stars ) ||
				raw.rows.length > 256 || raw.stars.length > 5
			) {
				throw new Error( "Invalid caravan hire" );
			}
			hire = {
				sequence: ++sequence,
				title: text( raw.title, 96 ),
				fee: text( raw.fee, 32 ),
				message: text( raw.message, 256 ),
				rows: raw.rows.map( ( r: Record<string, unknown> ) => ({
					name: text( r.name, 64 ),
					display: text( r.display, 64 ),
					role: text( r.role, 16 ),
					town: text( r.town, 32 ),
					available: r.available === true,
					extend: r.extend === true,
					status: text( r.status, 64 )
				}) ),
				stars: raw.stars.map( ( s: Record<string, unknown> ) => {
					if ( typeof s.stars !== "number" || s.stars < 1 || s.stars > 5 ) {
						throw new Error( "Invalid caravan hire" );
					}
					return { stars: s.stars, goods: text( s.goods, 32 ), total: text( s.total, 32 ) };
				} )
			};
			return true;
		},
		state: () => hire,
		lead: () => lead,
		// The NPC that offers a hire row, as its select told (null for none).
		hireNpc: () => hireNpc,
		/*
================
request

The window's request frame: refresh (0), hire (1) or lead (2).
================
		*/
		request( operation: 0 | 1 | 2 | 3, stars: number, name: string ): WireFrame {
			const bytes = new TextEncoder().encode( name );
			if ( bytes.length > 64 || !Number.isInteger( stars ) || stars < 0 || stars > 5 ) {
				throw new Error( "Invalid caravan hire request" );
			}
			return { opcode: CARAVAN_HIRE_REQUEST_OPCODE, payload: Uint8Array.of( operation, stars, ...bytes ) };
		},
		clear() {
			hire = null;
		},
		// A new session: the window and the lead state both go.
		reset() {
			hire = null;
			lead = null;
			hireNpc = null;
		}
	};
}

/*
================
caravanLeadToggle

Whom a click on a member's lead mark (the party board's toggle, ui.ts)
asks to lead: a member who does not lead takes it; the leader's own mark
toggles it back - a Trader's to the hirer (candidates[0]), the hirer's to
its first hired Trader. "" when nothing changes.
================
*/
export function caravanLeadToggle( lead: CaravanLead, name: string ): string {
	const key = name.toLowerCase(), hirer = lead.candidates[0]?.name ?? "";
	if ( !lead.candidates.some( c => c.name.toLowerCase() === key ) ) return "";
	if ( lead.leader.toLowerCase() !== key ) return name;
	if ( key !== hirer.toLowerCase() ) return hirer;
	return lead.candidates[1]?.name ?? "";
}
