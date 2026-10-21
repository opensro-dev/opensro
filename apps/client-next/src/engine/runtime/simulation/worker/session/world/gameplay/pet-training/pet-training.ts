/*
===========================================================================

pet-training.ts - the attack-pet tree's state (port-only 0x3FB1, not native)

With the server's SRO_ATTACK_PET_EXPANSION on, the pet window asks for an
attack pet's skills, row cooldowns and worn gear, and the server answers
after every change. This owner decodes the latest state; every rule
(points, pet-level requirements, row cooldowns, slotting, gear) is the
server's, so the window only draws what arrives here.

Body, little-endian: u32 petGid, u8 freePoints, u16 attackPercent,
u16 defensePercent, 2 x { u32 timerMs, u16 left, u8 kindLength, kind },
3 x u8 slotted, u8 skillCount, skillCount x { u8 id, u8 level,
u8 effectiveLevel, u8 nextPetLevel, u8 flags (1 action, 2 queued,
4 learnable, 8 live, 16 slotted), u16 value x10, u16 nextValue x10 },
u8 gearCount, gearCount x { u32 refObjId, u8 count, count x u64 }.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { InventoryItem, PetTraining, PetTrainingRow, PetTrainingSkill } from "@/engine/contracts/gameplay";

export const OP_PET_TRAINING_STATE = 0x3fb1;
export const OP_PET_SKILL_COUNTERS = 0x3fb6;
const MAX_SKILLS = 32;
const MAX_GEAR = 8;
const MAX_OPTIONS = 12;

/*
================
createPetTraining

item resolves a worn reference (sent ahead of the state) and its blue
options to a bag-style item for the slot; null when the reference is
unknown.
================
*/
export function createPetTraining(
	item: ( refObjId: number, slot: number, magic: readonly string[] ) => InventoryItem | null
) {
	let state: PetTraining | undefined;
	const text = new TextDecoder( "utf-8", { fatal: true } );
	return {
		/*
================
receive

Consumes a state frame; false for any other opcode. A malformed state is
a protocol error, never a partial one.
================
		*/
		receive( frame: WireFrame, now: number ): boolean {
			if ( frame.opcode === OP_PET_SKILL_COUNTERS ) {
				const p = frame.payload;
				if (
					(p.length !== 8 && p.length !== 10 && p.length !== 12 && p.length !== 17) || p[4]! >= 40 ||
					p[5]! >= 10 ||
					p[6]! >= 10 || p[7]! > 10 ||
					(p.length >= 10 && (p[8]! >= 7 || p[9]! >= 7)) ||
					(p.length >= 12 && (p[10]! > 10 || p[11]! > 10)) ||
					(p.length === 17 &&
						(p[12]! > 4 ||
							(p[12] === 0) !==
								(new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 13, true ) === 0)))
				) {
					throw Error( "Invalid pet skill counters" );
				}
				const gid = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 0, true );
				if ( state?.gid === gid ) {
					state = {
						...state,
						counters: {
							attacks: p[4]!,
							bearHits: p[5]!,
							ownerHits: p[6]!,
							resolve: p[7]!,
							...(p.length >= 10 ? { wolfExecute: p[8]!, ownerExecute: p[9]! } : {}),
							...(p.length >= 12 ? { wolfBlood: p[10]!, ownerBlood: p[11]! } : {}),
							...(p.length === 17 ?
								{
									hunt: p[12]!,
									huntTarget: new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32(
										13,
										true
									)
								} :
								{})
						}
					};
				}
				return true;
			}
			if ( frame.opcode !== OP_PET_TRAINING_STATE ) return false;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			let at = 0;
			const take = ( n: number ) => {
				if ( at + n > p.length ) throw new Error( "Truncated pet training state" );
				const start = at;
				at += n;
				return start;
			};
			const u8 = () => p[take( 1 )]!,
				u16 = () => v.getUint16( take( 2 ), true ),
				u32 = () => v.getUint32( take( 4 ), true );
			const gid = u32(), points = u8(), attackPercent = u16(), defensePercent = u16();
			const rows: PetTrainingRow[] = [];
			for ( let i = 0; i < 2; i++ ) {
				const timerMs = u32(), left = u16(), n = u8(), start = take( n );
				rows.push( { timerMs, left, kind: text.decode( p.subarray( start, start + n ) ) } );
			}
			const slotted = [ u8(), u8(), u8() ];
			const count = u8();
			if ( count > MAX_SKILLS ) throw new Error( "Invalid pet training skills" );
			const skills: PetTrainingSkill[] = [];
			for ( let i = 0; i < count; i++ ) {
				const id = u8(), level = u8(), effectiveLevel = u8(), nextPetLevel = u8(), flags = u8();
				skills.push( {
					id,
					level,
					effectiveLevel,
					nextPetLevel,
					action: (flags & 1) !== 0,
					queued: (flags & 2) !== 0,
					learnable: (flags & 4) !== 0,
					live: (flags & 8) !== 0,
					slotted: (flags & 16) !== 0,
					value: u16() / 10,
					nextValue: u16() / 10
				} );
			}
			const gears = u8();
			if ( gears > MAX_GEAR ) throw new Error( "Invalid pet training gear" );
			const gear: (InventoryItem | null)[] = [];
			for ( let i = 0; i < gears; i++ ) {
				const ref = u32(), n = u8();
				if ( n > MAX_OPTIONS ) throw new Error( "Invalid pet gear options" );
				const magic: string[] = [];
				for ( let k = 0; k < n; k++ ) magic.push( v.getBigUint64( take( 8 ), true ).toString() );
				gear.push( ref ? item( ref, i, magic ) : null );
			}
			let stats: PetTraining["stats"];
			if ( at < p.length ) {
				const version = u8();
				if ( version !== 1 && version !== 2 ) throw new Error( "Unknown pet combat profile" );
				const maxHp = u32(), magicPercent = u16(), magicDefensePercent = u16(), primary = u8();
				if ( maxHp === 0 || primary > 1 ) throw new Error( "Invalid pet combat profile" );
				stats = { maxHp, magicPercent, magicDefensePercent, magicPrimary: primary === 1 };
				if ( version === 2 ) {
					const gearStats = Array.from( { length: 6 }, () => u32() ), cooldownPercent = u16();
					if ( cooldownPercent > 80 ) throw new Error( "Invalid pet cooldown reduction" );
					stats = { ...stats, gearStats, cooldownPercent };
				}
			}
			if ( at !== p.length ) throw new Error( "Trailing pet training bytes" );
			state = {
				...(state?.gid === gid && state.counters ? { counters: state.counters } : {}),
				gid,
				points,
				attackPercent,
				defensePercent,
				rows,
				slotted,
				skills,
				gear,
				...(stats ? { stats } : {}),
				receivedAtMs: now
			};
			return true;
		},
		state: () => state,
		/*
================
clear
================
		*/
		clear() {
			state = undefined;
		}
	};
}
