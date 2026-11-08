/*
===========================================================================

equipment-sets.ts - the equipment set tiers a shard publishes

Port-only, not native: the v1.150 client has no set rows. A shard that
applies degree-10/11 set bonuses (the server's SRO_CAP110_SET_BONUSES)
lists its set groups in the world-entry blob; an item names its group in
its own setId native field. A shard without set bonuses publishes nothing
and no set is ever listed.

===========================================================================
*/

// The magicoption param ids a tier carries (the server's SetOptionReinforceItem
// and SetOptionResistAll).
export const SET_OPTION_REINFORCE_ITEM = 389;
export const SET_OPTION_RESIST_ALL = 390;

const MAX_SETS = 256;
const MAX_TIERS = 16;
const MAX_PIECES = 32;
const MAX_OPTIONS = 16;
const MAX_VALUE = 100000;

export interface EquipmentSetOption {
	readonly option: number;
	readonly value: number;
}

export interface EquipmentSetTier {
	readonly pieces: number;
	readonly options: readonly EquipmentSetOption[];
}

// One piece of a set: the worn position its item sits in (TypeID4) and its name symbol.
export interface EquipmentSetPiece {
	readonly slot: number;
	readonly name: string;
}

export interface EquipmentSet {
	readonly id: number;
	readonly name: string;
	readonly pieces: readonly EquipmentSetPiece[];
	readonly tiers: readonly EquipmentSetTier[];
}

/*
================
isCount
================
*/
function isCount( value: unknown, max: number ): value is number {
	return typeof value === "number" && Number.isInteger( value ) && value >= 0 && value <= max;
}

/*
================
publishedEquipmentSets

The set groups a world-entry blob lists; absent means none. A malformed
list is refused whole: a half-read tier table would show a wrong bonus.
================
*/
export function publishedEquipmentSets( value: unknown ): readonly EquipmentSet[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) || value.length > MAX_SETS ) throw Error( "Invalid equipment sets" );
	return value.map( ( row: unknown ) => {
		const set = row as { id?: unknown; name?: unknown; pieces?: unknown; tiers?: unknown; } | null;
		if (
			!set || !isCount( set.id, 0xffffffff ) || (set.name !== undefined && typeof set.name !== "string") ||
			!Array.isArray( set.tiers ) || set.tiers.length > MAX_TIERS ||
			(set.pieces !== undefined && (!Array.isArray( set.pieces ) || set.pieces.length > MAX_PIECES))
		) throw Error( "Invalid equipment set" );
		return {
			id: set.id,
			name: (set.name as string | undefined) ?? "",
			pieces: ((set.pieces as unknown[] | undefined) ?? []).map( ( pieceRow: unknown ) => {
				const piece = pieceRow as { slot?: unknown; name?: unknown; } | null;
				if (
					!piece || !isCount( piece.slot, 31 ) || typeof piece.name !== "string" || piece.name.length > 256
				) {
					throw Error( "Invalid equipment set piece" );
				}
				return { slot: piece.slot, name: piece.name };
			} ),
			tiers: set.tiers.map( ( tierRow: unknown ) => {
				const tier = tierRow as { pieces?: unknown; options?: unknown; } | null;
				if (
					!tier || !isCount( tier.pieces, 13 ) || !Array.isArray( tier.options ) ||
					tier.options.length > MAX_OPTIONS
				) throw Error( "Invalid equipment set tier" );
				return {
					pieces: tier.pieces,
					options: tier.options.map( ( optionRow: unknown ) => {
						const option = optionRow as { option?: unknown; value?: unknown; } | null;
						if ( !option || !isCount( option.option, 65535 ) || !isCount( option.value, MAX_VALUE ) ) {
							throw Error( "Invalid equipment set option" );
						}
						return { option: option.option, value: option.value };
					} )
				};
			} )
		};
	} );
}
