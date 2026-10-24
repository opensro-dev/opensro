/*
===========================================================================

flagwar.ts - the Capture the Flag event wire (0x30A9) and its notices

Decodes the server event frames the native client folds in
CFlagWarClientState_HandleEventPacket (0x4b2530) into typed events and
the UIIT_MSG_FLAGWAR_* notices. Timer, carrier and sign-up state stay
with the gameplay worker; this module owns the byte shapes only. The
worker keeps the timer value honest (mission start arms 0x124f80 ms,
the elapsed sync re-arms it) and publishes the carrier gid. The drawn
plate is the native CIFTimerWnd (resinfo/iftimerwnd.txt, armed through
CGInterface_GetFlagWarTimerControl by the CIFEventTimer calls in the
0x4B2530 handler): the runtime draws it from this snapshot - the
creation anchor (ginterface GDR_TIMER_WND) and the digit-texture
mapping are pinned in foundation/ui/flagwar-timer-plate.ts. The
carrier's interaction-state-5 model waits on the entity lane.

===========================================================================
*/
import type { SystemNotice } from "./system-notices";

// The reference server's register handler refuses below level 20; the talk row
// hides the same way (0x5d3630 reads player+0x820 >= 0x14).
export const FLAG_WAR_LEVEL_MIN = 20;

// The mission timer is authored at 0x124f80 ms (20 minutes).
export const FLAG_WAR_MATCH_MS = 0x124f80;

/*
================
FlagWarEvent

One decoded 0x30A9 frame. Name payloads ride as a u16 count plus that
many single-byte characters, the shape the match owner writes.
================
*/
export type FlagWarEvent =
	| { readonly type: "standby10"; }
	| { readonly type: "standby5"; }
	| { readonly type: "standby1"; }
	| { readonly type: "gameStart"; }
	| { readonly type: "missionStart"; readonly team: number; }
	| { readonly type: "timerStop"; }
	| { readonly type: "carrier"; readonly gid: number; }
	| { readonly type: "joinResult"; }
	| { readonly type: "startNotEnter"; }
	| { readonly type: "levelError"; }
	| { readonly type: "gameCancel"; }
	| { readonly type: "pickupOurs"; readonly name: string; }
	| { readonly type: "pickupTheirs"; }
	| { readonly type: "masterKey"; readonly gid: number; readonly team: number; readonly name: string; }
	| { readonly type: "eventJoin"; }
	| { readonly type: "elapsed"; readonly ms: number; }
	| { readonly type: "flagDrop"; }
	| { readonly type: "missionScored"; readonly team: number; readonly name: string; }
	| { readonly type: "gameWin"; }
	| { readonly type: "channelBusy"; }
	| { readonly type: "enteredError"; };

/*
================
flagWarName

A trailing name field: u16 count, then that many single-byte characters.
================
*/
function flagWarName( p: Uint8Array, offset: number ): string {
	if ( p.length < offset + 2 ) throw Error( "Truncated flag-war name" );
	const n = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( offset, true );
	if ( p.length !== offset + 2 + n ) throw Error( "Invalid flag-war name length" );
	let name = "";
	for ( let i = 0; i < n; i++ ) name += String.fromCharCode( p[offset + 2 + i]! );
	return name;
}

/*
================
decodeFlagWarEvent

4b2530's arm table. Plain types carry no payload; the 0xFF family keys
off the second byte. Unknown subtypes (0x16/0x17 included) read as null:
the native handler ignores them too.
================
*/
export function decodeFlagWarEvent( p: Uint8Array ): FlagWarEvent | null {
	if ( !p.length ) throw Error( "Empty flag-war event" );
	const tag = p[0]!;
	if ( tag !== 0xff ) {
		const view = new DataView( p.buffer, p.byteOffset, p.byteLength );
		switch ( tag ) {
			case 0x02:
				if ( p.length !== 1 ) throw Error( "Invalid flag-war standby length" );
				return { type: "standby10" };
			case 0x04:
				if ( p.length !== 1 ) throw Error( "Invalid flag-war game start length" );
				return { type: "gameStart" };
			case 0x05:
			case 0x09:
				if ( p.length !== 1 ) throw Error( "Invalid flag-war timer stop length" );
				return { type: "timerStop" };
			// The mission start names the receiving player's team (u16);
			// only that team's clients show the notice and the timer.
			case 0x08:
				if ( p.length !== 3 ) throw Error( "Invalid flag-war mission start length" );
				return { type: "missionStart", team: view.getUint16( 1, true ) };
			case 0x0a:
				if ( p.length !== 5 ) throw Error( "Invalid flag-war carrier length" );
				return { type: "carrier", gid: view.getUint32( 1, true ) };
			case 0x0d:
				if ( p.length !== 1 ) throw Error( "Invalid flag-war standby length" );
				return { type: "standby5" };
			case 0x0e:
				if ( p.length !== 1 ) throw Error( "Invalid flag-war standby length" );
				return { type: "standby1" };
			default:
				return null;
		}
	}
	if ( p.length < 2 ) throw Error( "Truncated flag-war subtype" );
	const sub = p[1]!;
	switch ( sub ) {
		case 0x00:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war join result length" );
			return { type: "joinResult" };
		case 0x04:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war start refusal length" );
			return { type: "startNotEnter" };
		case 0x05:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war level error length" );
			return { type: "levelError" };
		case 0x06:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war cancel length" );
			return { type: "gameCancel" };
		case 0x09:
			return { type: "pickupOurs", name: flagWarName( p, 2 ) };
		case 0x0a:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war enemy pickup length" );
			return { type: "pickupTheirs" };
		case 0x0b: {
			if ( p.length < 8 ) throw Error( "Truncated flag-war master key" );
			const view = new DataView( p.buffer, p.byteOffset, p.byteLength );
			return {
				type: "masterKey",
				gid: view.getUint32( 2, true ),
				team: p[6]!,
				name: flagWarName( p, 7 )
			};
		}
		case 0x0c:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war event join length" );
			return { type: "eventJoin" };
		case 0x0e:
			if ( p.length !== 6 ) throw Error( "Invalid flag-war elapsed length" );
			return {
				type: "elapsed",
				ms: new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true )
			};
		case 0x0f:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war flag drop length" );
			return { type: "flagDrop" };
		case 0x10:
			if ( p.length < 3 ) throw Error( "Truncated flag-war mission score" );
			return { type: "missionScored", team: p[2]!, name: flagWarName( p, 3 ) };
		case 0x11:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war game win length" );
			return { type: "gameWin" };
		case 0x12:
		case 0x13:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war channel refusal length" );
			return { type: "channelBusy" };
		case 0x14:
		case 0x15:
			if ( p.length !== 2 ) throw Error( "Invalid flag-war entered error length" );
			return { type: "enteredError" };
		default:
			return null;
	}
}

/*
================
flagWarNotice

The notice one event raises, if any. The join answer reads as success or
cancel from the row the player clicked (joinExpected); a scored mission
reads clear or fail against the player's own team (unknown team: no
notice). Timer and carrier events move worker state and show nothing.
================
*/
export function flagWarNotice(
	event: FlagWarEvent,
	joinExpected: boolean,
	ownTeam: number | null
): SystemNotice | null {
	switch ( event.type ) {
		case "standby10":
			return { key: "UIIT_MSG_FLAGWAR_STANDBY_10", value: 0 };
		case "standby5":
			return { key: "UIIT_MSG_FLAGWAR_STANDBY_5", value: 0 };
		case "standby1":
			return { key: "UIIT_MSG_FLAGWAR_STANDBY_1", value: 0 };
		case "gameStart":
			return { key: "UIIT_MSG_FLAGWAR_GAME_START", value: 0 };
		case "missionStart":
			if ( ownTeam !== null && event.team !== ownTeam ) return null;
			return { key: "UIIT_MSG_FLAGWAR_GAME_START_MISSION", value: 0 };
		case "joinResult":
			return {
				key: joinExpected ? "UIIT_MSG_FLAGWAR_GAME_JOIN_SUCCES" : "UIIT_MSG_FLAGWAR_GAME_JOIN_CANCEL",
				value: 0
			};
		case "startNotEnter":
			return { key: "UIIT_MSG_FLAGWAR_GAME_START_NOT_ENTER", value: 0 };
		case "levelError":
			return { key: "UIIT_MSG_FLAGWAR_GAME_JOIN_LEVEL_ERORR", value: 0 };
		case "gameCancel":
			return { key: "UIIT_MSG_FLAGWAR_GAME_CANCEL", value: 0 };
		case "pickupOurs":
			return { key: "UIIT_MSG_FLAGWAR_PICKUP_FLAG_OURTEAM", value: 0, arguments: [ event.name ] };
		case "pickupTheirs":
			return { key: "UIIT_MSG_FLAGWAR_PICKUP_FLAG_OTHERTEAM", value: 0 };
		case "masterKey":
			// The native handler shows the text only on the key's own team.
			if ( ownTeam !== null && event.team !== ownTeam ) return null;
			return { key: "UIIT_MSG_FLAGWAR_PICKUP_MASTERKEY", value: 0, arguments: [ event.name ] };
		case "eventJoin":
			return { key: "UIIT_MSG_FLAGWAR_EVENT_JOIN", value: 0 };
		case "flagDrop":
			return { key: "UIIT_MSG_FLAGWAR_FLAG_DROP", value: 0 };
		case "missionScored":
			if ( ownTeam === null ) return null;
			return event.team === ownTeam ?
				{ key: "UIIT_MSG_FLAGWAR_FLAG_MISSION_CLEAR", value: 0, arguments: [ event.name ] } :
				{ key: "UIIT_MSG_FLAGWAR_FLAG_MISSION_FAIL", value: 0 };
		case "gameWin":
			return { key: "UIIT_MSG_FLAGWAR_GAME_WIN", value: 0 };
		case "channelBusy":
			return { key: "UIIT_MSG_FLAGWAR_PLAYER_FLAG_PICKUP", value: 0 };
		case "enteredError":
			return { key: "UIIT_MSG_FLAGWAR_GAME_ENTERD_ERORR", value: 0 };
		default:
			return null;
	}
}
