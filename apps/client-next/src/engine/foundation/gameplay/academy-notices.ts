/*
===========================================================================

academy-notices.ts - Academy response acknowledgments and 0x3AC5 notices

Decodes the camp action acknowledgments (0xB4D4, 0xB785, 0xB10A, 0xB36D,
0xB220, 0xB7C5) and maps authoritative 0x3AC5 transitions onto their
native child-23 notice banners and system chat messages (773C60).

===========================================================================
*/

import type { WireFrame } from "@/engine/contracts/network";
import type { AcademyState } from "./academy";
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

// 76FC90 reads one extra u32 when 0xB7C5 fails with error 0x1D.
const ACADEMY_DISBAND_WAIT_CODE = 0x1d;

/*
================
academyAcknowledgment

76FC90, 76FD40/90/E0, 76FE30/80: these acknowledgments never change academy
membership or notice text. Authoritative changes arrive separately in 3AC5.
================
*/
export function academyAcknowledgment( frame: WireFrame ): { readonly notice: SystemNotice | null; } | null {
	if ( ![ 0xb4d4, 0xb785, 0xb10a, 0xb36d, 0xb220, 0xb7c5 ].includes( frame.opcode ) ) return null;
	const p = frame.payload;
	const expected = p[0] === 2 ?
		(frame.opcode === 0xb7c5 && p[1] === ACADEMY_DISBAND_WAIT_CODE ? 6 : 2) :
		1;
	if ( !p.length || p.length !== expected ) throw Error( "Invalid academy acknowledgment" );
	if ( p[0] === 2 ) return { notice: constantNativeNotice( 0x1d, p[1]! ) };
	// 76FE9B -> 67D030 -> child 23: seven-second notice window, no guide.
	if ( frame.opcode === 0xb220 && p[0] === 1 ) {
		return {
			notice: {
				key: "UIIT_MSG_TC_COMMON_KNOW_REMIND_UPDATE",
				value: 0,
				notificationBanner: true,
				bannerOnly: true
			}
		};
	}
	return { notice: null };
}

/*
================
academyStatusNotice

773C60: maps authoritative 0x3AC5 state transitions onto child 23 or chat.
================
*/
export function academyStatusNotice(
	state: AcademyState,
	next: AcademyState,
	frame: WireFrame
): SystemNotice | null {
	if ( frame.opcode !== 0x3ac5 ) return null;
	const p = frame.payload;
	// 773FA5: dissolution shows the child-23 notice banner.
	if ( p[0] === 1 ) {
		return {
			key: "UIIT_STT_TC_DISSOLUTION_SUCCESS",
			value: 0,
			notificationBanner: true,
			bannerOnly: true
		};
	}
	// 773F25: a joining member prints to system chat.
	if ( p[0] === 2 ) {
		const joined = next.members?.[next.members.length - 1];
		return joined ? { key: "UIIT_MSG_TC_MACHING_JOINING_MEMBER", value: 0, arguments: [ joined.name ] } : null;
	}
	// 7740F9: subtype 3 is voluntary leave (1), expulsion (2) or silent (3).
	if ( p[0] === 3 ) {
		const id = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
		const flag = p[5]!;
		const member = state.members?.find( m => m.id === id );
		if ( flag === 1 ) {
			if ( id === state.localMemberId ) {
				return {
					key: "UIIT_STT_TC_OUT_TRAININGCAMP_ANNOUNCE",
					value: 0,
					notificationBanner: true,
					bannerOnly: true
				};
			}
			return member ?
				{ key: "UIIT_STT_TC_OUT_TRAININGCAMP_OTHER_USER", value: 0, arguments: [ member.name ] } :
				null;
		}
		if ( flag === 2 ) {
			if ( id === state.localMemberId ) {
				return {
					key: "UIIT_MSG_TC_EXPULSION_RESULT_ANNOUNCE",
					value: 0,
					notificationBanner: true,
					bannerOnly: true
				};
			}
			return member ? { key: "UIIT_MSG_TC_EXPULSION_RESULT", value: 0, arguments: [ member.name ] } : null;
		}
		return null;
	}
	// 774622: subtype 4 is voluntary (0) or compulsory (1) graduation.
	if ( p[0] === 4 ) {
		const id = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
		const flag = p[5]!;
		if ( flag !== 0 && flag !== 1 ) return null;
		const member = state.members?.find( m => m.id === id );
		if ( id === state.localMemberId ) {
			return {
				key: "UIIT_STT_TC_GRADUATION_CONGRATULATIONS",
				value: 0,
				notificationBanner: true,
				bannerOnly: true
			};
		}
		return member ? { key: "UIIT_STT_TC_GRADUATION_ANNOUNCE", value: 0, arguments: [ member.name ] } : null;
	}
	// 774C64/69: an authoritative notice update also shows child 23.
	if ( p[0] === 7 ) {
		return {
			key: "UIIT_MSG_TC_COMMON_KNOW_REMIND_UPDATE",
			value: 0,
			notificationBanner: true,
			bannerOnly: true
		};
	}
	// 774D6E: membership-seed refusal uses the category-0x1D dispatcher.
	if ( p[0] === 10 && p[1] === 2 ) {
		return constantNativeNotice( 0x1d, p[2]! );
	}
	return null;
}
