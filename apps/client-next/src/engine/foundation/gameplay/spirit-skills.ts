/*
===========================================================================

spirit-skills.ts - the skills a worn Devil's Spirit lends its wearer

Port-only, not native to v1.150 (SRO_NASRUN, #4384). The later clients
show a worn Spirit's skills in the equipment window's skill strip
(resinfo/ifequipskillslotwnd.txt, CIFEquipSkillSlotWnd) and let its
wearer cast them unlearned. The server owns which skills those are
(nasrun.SpiritAbility): it names them in the bootstrap (spiritSkills) and
in the port-only 0x3FC9 frame whenever the worn Spirit changes, lapses or
comes off. Body: u8 count, then count x u32 skill id.

===========================================================================
*/

export const SPIRIT_SKILLS_OPCODE = 0x3fc9;
// The strip's four CIFSkillSlot nodes (GDR_AVATAR_SKILL_SLOT_01..04).
export const MAX_SPIRIT_SKILLS = 4;

// The later clients' equipment window (resinfo/ifequipment.txt, the same
// in the v1.188 VSRO and v1.696 Media.pk2; its four avatar sockets sit
// where v1.150's do): GDR_AVATAR_SLOT_05 (node 117) holds the Spirit
// under the dress socket, and GDR_AVATAR_SKILL_WND (node 500, a
// CIFEquipSkillSlotWnd) holds its skill strip under that, its slots
// placed by resinfo/ifequipskillslotwnd.txt. Rects relative to the
// equipment window, as the authored nodes'.
export const SPIRIT_SOCKET_RECT = [ 5, 172, 32, 32 ] as const;
export const SPIRIT_SKILL_STRIP_RECT = [ 0, 240, 177, 50 ] as const;
export const SPIRIT_SKILL_SLOT_X = [ 11, 52, 93, 134 ] as const;
export const SPIRIT_SKILL_SLOT_Y = 4;
export const SPIRIT_SKILL_SLOT_SIZE = 32;
// GDR_AVATAR_SKILL_SLOT_BG_01..04: a 40x40 pt_block02.ddj under each slot,
// drawn whether or not the slot holds a skill.
export const SPIRIT_SKILL_SLOT_BG_X = [ 9, 50, 91, 132 ] as const;
export const SPIRIT_SKILL_SLOT_BG_Y = 2;
export const SPIRIT_SKILL_SLOT_BG_SIZE = 40;

/*
================
spiritSkillList

A validated list: at most MAX_SPIRIT_SKILLS positive u32 ids, no repeat.
================
*/
export function spiritSkillList( value: unknown ): number[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) || value.length > MAX_SPIRIT_SKILLS ) throw Error( "Invalid Spirit skills" );
	const out: number[] = [];
	for ( const id of value ) {
		if ( !Number.isInteger( id ) || id <= 0 || id > 0xffffffff || out.includes( id ) ) {
			throw Error( "Invalid Spirit skills" );
		}
		out.push( id );
	}
	return out;
}

/*
================
decodeSpiritSkills

The 0x3FC9 body's skill ids (empty: the Spirit lends nothing now).
================
*/
export function decodeSpiritSkills( payload: Uint8Array ): number[] {
	const count = payload[0];
	if ( count === undefined || payload.length !== 1 + 4 * count ) throw Error( "Invalid Spirit skills frame" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	return spiritSkillList( Array.from( { length: count }, ( _, i ) => view.getUint32( 1 + 4 * i, true ) ) );
}

// The HP and MP percent a Spirit grows its wearer by: the server's rule
// (game/nasrun/ability.go SpiritAbility, the iSRO-R shard's
// _RefAbilityByItemOptLevel): 15 % + plus, the Unique's 20 % + plus, for a
// plus the table authors (0..10). Mirrored for the tooltip only; the
// server applies its own.
const SPIRIT_BASE_GAUGE_PERCENT = 15;
const SPIRIT_UNIQUE_GAUGE_PERCENT = 20;
const SPIRIT_MAX_ABILITY_PLUS = 10;

/*
================
spiritGaugePercent

The Spirit's HP/MP percent at plus, or undefined for a plus no row names.
================
*/
export function spiritGaugePercent( codename: string | undefined, plus: number ): number | undefined {
	if ( !Number.isInteger( plus ) || plus < 0 || plus > SPIRIT_MAX_ABILITY_PLUS ) return undefined;
	const unique = codename?.endsWith( "_NASRUN_UNIQUE" ) ?? false;
	return (unique ? SPIRIT_UNIQUE_GAUGE_PERCENT : SPIRIT_BASE_GAUGE_PERCENT) + plus;
}
