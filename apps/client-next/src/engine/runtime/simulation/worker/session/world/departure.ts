/*
===========================================================================

departure.ts - the logout/restart countdown of one world session

0x70B7 asks for it; CPSMission_OnLogoutResponse0xB0B7 (0x74B2E0) arms it
through CGInterface_StartLogoutCountdown (0x6814D0) and 0x315A completes
it. A ground click during the countdown sends 0x731F, and
CPSMission_OnLogoutCancelResponse0xB31F (0x74B360) ends it through
CGInterface_CancelLogoutCountdown (0x67FD80). Zero ends the notice timer,
not the world (6875F0).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";

const OP_DEPARTURE_REQUEST = 0x70b7;
const OP_DEPARTURE_RESPONSE = 0xb0b7;
const OP_DEPARTURE_CANCEL_RESPONSE = 0xb31f;
const OP_DEPARTURE_COMPLETE = 0x315a;
const RESULT_OK = 1;
const RESULT_ERROR = 2;
const NOTICE_PERIOD_MS = 1000;

/*
================
refusalNotice

The category-0xF notices the countdown replies can carry.
================
*/
function refusalNotice( code: number ): string | null {
	if ( code === 1 ) return "UIIT_MSG_LOGOUT_ERR_CANT_LOGOUT_IN_BATTLE_STATE";
	if ( code === 2 ) return "UIIT_MSG_LOGOUT_ERR_CANT_LOGOUT_WHILE_TELEPORT_WORKING";
	return null;
}

/*
================
createDeparture
================
*/
export function createDeparture( send: ( frame: WireFrame ) => void, notice: ( value: SystemNotice ) => void ) {
	let requested: 1 | 2 | 0 = 0, accepted: 1 | 2 | 0 = 0, seconds = 0, next = 0;
	/*
================
show
================
	*/
	function show() {
		notice( { key: "UIIT_MSG_LOGOUT_REMAIN_TIME", value: seconds, banner: true } );
	}
	/*
================
reset
================
	*/
	function reset() {
		requested = 0;
		accepted = 0;
		seconds = 0;
		next = 0;
	}
	return {
		/*
================
request
================
		*/
		request( type: 1 | 2 ) {
			if ( requested ) return;
			send( { opcode: OP_DEPARTURE_REQUEST, payload: Uint8Array.of( type ) } );
			requested = type;
		},
		/*
================
pending

Whether a countdown runs (CGInterface +0x39C set): a ground click cancels it.
================
		*/
		pending() {
			return accepted !== 0;
		},
		/*
================
receive

Returns the completed departure type on 0x315A, 0 for another countdown
packet, null for a packet this owner does not read.
================
		*/
		receive( frame: WireFrame, now: number ): 1 | 2 | 0 | null {
			const p = frame.payload;
			if ( frame.opcode === OP_DEPARTURE_RESPONSE ) {
				if ( p[0] === RESULT_OK ) {
					if ( p.length !== 3 || (p[2] !== 1 && p[2] !== 2) ) throw Error( "Invalid restart countdown" );
					accepted = p[2];
					requested = accepted;
					seconds = p[1]!;
					next = now + NOTICE_PERIOD_MS;
					show();
				} else if ( p[0] === RESULT_ERROR ) {
					if ( p.length !== 2 ) throw Error( "Invalid restart refusal" );
					requested = 0;
					accepted = 0;
					next = 0;
					const key = refusalNotice( p[1]! );
					if ( key ) notice( { key, value: 0, banner: true } );
				} else throw Error( "Invalid restart result" );
				return 0;
			}
			if ( frame.opcode === OP_DEPARTURE_CANCEL_RESPONSE ) {
				if ( p[0] === RESULT_OK ) {
					if ( p.length !== 1 ) throw Error( "Invalid restart cancellation" );
					// 67FD80: the countdown and its notice timer end.
					reset();
					notice( { key: "UIIT_MSG_LOGOUT_REMAIN_TIME_CANCLE", value: 0, banner: true } );
				} else if ( p[0] === RESULT_ERROR ) {
					if ( p.length !== 2 ) throw Error( "Invalid restart cancellation refusal" );
					const key = refusalNotice( p[1]! );
					if ( key ) notice( { key, value: 0, banner: true } );
				} else throw Error( "Invalid restart cancellation result" );
				return 0;
			}
			if ( frame.opcode !== OP_DEPARTURE_COMPLETE ) return null;
			if ( p.length || !accepted ) throw Error( "Unexpected restart completion" );
			const type = accepted;
			reset();
			return type;
		},
		/*
================
step
================
		*/
		step( now: number ) {
			if ( !next || now < next ) return;
			const elapsed = Math.floor( (now - next) / NOTICE_PERIOD_MS ) + 1;
			seconds = Math.max( 0, seconds - elapsed );
			next = seconds ? next + elapsed * NOTICE_PERIOD_MS : 0;
			if ( seconds ) show();
		},
		reset
	};
}
