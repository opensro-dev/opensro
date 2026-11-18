/*
===========================================================================

caravan-hire.ts - the AI caravan hire window (port-only 0x3FC3 / 0x7FC3, not native)

The server opens and refreshes the window with a JSON body (the agents
hired where the player stands, available ones first, and a Trader's goods
choices by star); the window's request is u8 operation (0 refresh,
1 hire), u8 stars, then the agent's name. A client that predates the
frame drops the unknown opcode.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const CARAVAN_HIRE_OPCODE = 0x3fc3;
export const CARAVAN_HIRE_REQUEST_OPCODE = 0x7fc3;

export interface CaravanHireRow {
	readonly name: string;
	readonly display: string;
	readonly role: string;
	readonly town: string;
	readonly available: boolean;
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
			if ( frame.opcode !== CARAVAN_HIRE_OPCODE ) return false;
			const raw = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( frame.payload ) ) as Record<string, unknown>;
			if ( raw.version !== 1 || !Array.isArray( raw.rows ) || !Array.isArray( raw.stars ) || raw.rows.length > 256 || raw.stars.length > 5 ) {
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
					status: text( r.status, 64 )
				}) ),
				stars: raw.stars.map( ( s: Record<string, unknown> ) => {
					if ( typeof s.stars !== "number" || s.stars < 1 || s.stars > 5 ) throw new Error( "Invalid caravan hire" );
					return { stars: s.stars, goods: text( s.goods, 32 ), total: text( s.total, 32 ) };
				} )
			};
			return true;
		},
		state: () => hire,
		/*
================
request

The window's request frame: refresh (0) or hire (1).
================
		*/
		request( operation: 0 | 1, stars: number, name: string ): WireFrame {
			const bytes = new TextEncoder().encode( name );
			if ( bytes.length > 64 || !Number.isInteger( stars ) || stars < 0 || stars > 5 ) throw new Error( "Invalid caravan hire request" );
			return { opcode: CARAVAN_HIRE_REQUEST_OPCODE, payload: Uint8Array.of( operation, stars, ...bytes ) };
		},
		clear() {
			hire = null;
		}
	};
}
