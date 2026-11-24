/*
===========================================================================

reverse-user-point.ts - the reverse return scroll's party and user points

Later-native (v1.696 client, #3903): the scroll's choice box adds "Move
to party members." and "Move to the user's set location." (textuisystem
ids 4201/4202) beside the last recall and last death points. The party
row opens a member list (CNIFReversePortalParty, interface 0x2ca); the
user row opens the settings window (CNIFReversePortalUserSave, 0x2cb),
whose enroll button records where the player stands and whose move
button returns there. The server owns the party rule, the stored
location (schema 52) and every refusal; this file owns the wire shapes
the two share.

The later client's request bytes for these modes were not isolated, so
the wire is port-shaped, the same class as the map choice's gating
(reverse-return-map.ts): the use tail is choice 4 with a u32 member gid,
5 (move to the saved location) or 6 (enroll here, spends nothing) as one
byte; refusals ride the 0xB5BD channel on port bytes past the family's
native codes, mapped here to the later client's own lines (ids 4204,
4205, 4220, 4221); and the saved location arrives on the port-only
0x3FCE frame at world entry and after an enroll. No release-protocol
bump: a client that does not know the frame drops it unhandled (the
receive chain matches opcodes one by one) and merely shows no set
location until it reloads.

===========================================================================
*/

// The port-shaped 0x75BD choices of the two later destinations (the
// server's reverseReturnPartyChoice / reverseReturnUserChoice /
// reverseReturnEnrollChoice).
export const REVERSE_RETURN_PARTY = 4;
export const REVERSE_RETURN_USER = 5;
export const REVERSE_RETURN_ENROLL = 6;

// OP_REVERSE_USER_POINT is the port-only S->C saved location: u8 present,
// then u16 region id and f32 x/y/z when present.
export const OP_REVERSE_USER_POINT = 0x3fce;

const POINT_BODY_BYTES = 15;

/*
================
ReverseUserPoint

The saved location the owner was told (OpReverseUserPoint with its
present byte set).
================
*/
export interface ReverseUserPoint {
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
}

/*
================
decodeReverseUserPoint

One 0x3FCE body, whole or not at all: null when no location is enrolled.
================
*/
export function decodeReverseUserPoint( payload: Uint8Array ): ReverseUserPoint | null {
	if ( payload.length < 1 ) throw Error( "Invalid reverse user point length" );
	if ( payload[0] === 0 ) {
		if ( payload.length !== 1 ) throw Error( "Invalid reverse user point length" );
		return null;
	}
	if ( payload[0] !== 1 || payload.length !== POINT_BODY_BYTES ) {
		throw Error( "Invalid reverse user point" );
	}
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	const point = {
		regionId: view.getUint16( 1, true ),
		x: view.getFloat32( 3, true ),
		y: view.getFloat32( 7, true ),
		z: view.getFloat32( 11, true )
	};
	if ( point.regionId === 0 || ![ point.x, point.y, point.z ].every( Number.isFinite ) ) {
		throw Error( "Invalid reverse user point" );
	}
	return point;
}

/*
================
reversePartyTail

The item-use tail for a party member: choice 4 and the member's gid,
little-endian (a position is never sent; the server resolves it).
================
*/
export function reversePartyTail( gid: number ): Uint8Array {
	if ( !Number.isInteger( gid ) || gid < 1 || gid > 0xffffffff ) {
		throw Error( "Choose a party member for the reverse return scroll" );
	}
	const tail = new Uint8Array( 5 );
	tail[0] = REVERSE_RETURN_PARTY;
	new DataView( tail.buffer ).setUint32( 1, gid, true );
	return tail;
}

/*
================
reverseReturnFailureKey

The line for one of the two destinations' refusal bytes (0xB5BD result
2), as its textuisystem key, or undefined for any other byte (the native
notice route owns those). A switch, not a table: module-level state in
a foundation file must stay a scalar or an `as const` literal
(verify-ownership).
================
*/
export function reverseReturnFailureKey( code: number ): string | undefined {
	switch ( code ) {
		case 0xf6:
			return "UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE";
		case 0xf8:
			return "UIIT_MSG_REVERSE_RETRUN_SCROLL_PARTY_MOVE_1";
		case 0xfa:
			return "UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_2";
		case 0xfb:
			return "UIIT_MSG_REVERSE_RETRUN_SCROLL_USER_MOVE_FAIL_3";
		default:
			return undefined;
	}
}
