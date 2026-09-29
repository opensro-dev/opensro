/*
===========================================================================

equipment-visuals.ts - native admission of worn items and default wear

Which worn items a character actually shows (priority masks, Hwan hair,
mounting, fortress clothing) and which default garments cover empty
sockets. Pure rules over item references; the catalog lookup and the
attachment assembly live in equipment-appearance.ts.

===========================================================================
*/

export interface EquipmentVisualRule {
	readonly slot: number | null;
	readonly avatarSlot?: number;
	readonly visualMask: number;
	readonly visualPriority: number;
}

export interface WornVisual {
	readonly refObjId: number;
	readonly slot: number;
	readonly avatar?: boolean;
}

// One worn item: its native socket, RefItemID and enhancement level.
export interface WornItem {
	readonly slot: number;
	readonly refObjId: number;
	readonly plus: number;
}

export interface DefaultWearRule {
	readonly armorClass: number;
	readonly thiefSuit: boolean;
	readonly visualMask: number;
}

const JOB_SLOT = 8;
const WEAPON_SLOT = 6;
const SHIELD_SLOT = 7;
const WORN_SLOT_COUNT = 9;
const JOB_PRIORITY = 50;

/*
================
selectDefaultWear

8EA2F0 / 8E91C0 / 8E8900. Occupancy and armor family use RAW references;
only avatar suppression uses admitted visuals. Output is a complete
replacement selection, so removing or changing equipment cannot retain
stale default pieces.
================
*/
export function selectDefaultWear(
	items: readonly WornVisual[],
	accepted: readonly WornVisual[],
	rules: Readonly<Record<string, DefaultWearRule>>,
	chinesePlayer: boolean
): readonly string[] {
	if ( !chinesePlayer || items.some( i => !i.avatar && i.slot === JOB_SLOT && rules[i.refObjId]?.thiefSuit ) ) {
		return [];
	}
	const first = [ ...items ].filter( i => !i.avatar && i.slot >= 0 && i.slot < WORN_SLOT_COUNT ).sort( ( a, b ) =>
		a.slot - b.slot
	).find( i => (rules[i.refObjId]?.armorClass ?? 0) !== 0 );
	const family = first && (rules[first.refObjId]?.armorClass ?? 0) >= 2 ? "light" : "clothes";
	// Default garments fill the chest (socket 1) and legs (socket 4).
	const pieces = [ { slot: 1, mask: 2, part: "BA" }, { slot: 4, mask: 16, part: "LA" } ];
	return pieces.filter( p =>
		!items.some( i => !i.avatar && i.slot === p.slot ) &&
		!accepted.some( i => i.avatar && ((rules[i.refObjId]?.visualMask ?? 0) & p.mask) !== 0 )
	).map( p => family + "_" + p.part );
}

/*
================
selectEquipmentVisuals

8E8710/8E8010/8E9A10/8E9B30/8E9C00: job50, avatar head/dress70,
attachment90, flag110, armor150. Only strictly higher priority masks
suppress. The job reference mask applies even when its resource cannot be
attached.
================
*/
export function selectEquipmentVisuals<T extends WornVisual>(
	items: readonly T[],
	rules: Readonly<Record<string, EquipmentVisualRule>>,
	resolvable: ReadonlySet<number>,
	hwanHair: boolean,
	mounted: boolean,
	fortress = false
) {
	const job = items.find( i => !i.avatar && i.slot === JOB_SLOT ),
		jobMask = job ? rules[job.refObjId]?.visualMask ?? 0 : 0;
	const accepted: T[] = [];
	const ordered = items.map( ( item, index ) => ({ item, index, rule: rules[item.refObjId] }) ).filter( (
		row
	): row is typeof row & { rule: EquipmentVisualRule; } => !!row.rule ).sort( ( a, b ) =>
		a.rule.visualPriority - b.rule.visualPriority || a.index - b.index
	);
	for ( const { item, rule } of ordered ) {
		if ( !resolvable.has( item.refObjId ) ) continue;
		// 8E8830 admits all weapon kinds and the two shield kinds. These are
		// precisely the native visual sockets 6/7; avatars have no such socket.
		if ( fortress && (item.avatar || (rule.slot !== WEAPON_SLOT && rule.slot !== SHIELD_SLOT)) ) continue;
		if ( !item.avatar && mounted && (item.slot === WEAPON_SLOT || item.slot === SHIELD_SLOT) ) continue;
		if (
			hwanHair &&
			(!item.avatar && item.slot === 0 || item.avatar && (rule.visualMask & 1) !== 0 ||
				!item.avatar && item.slot === JOB_SLOT && (rule.visualMask & 1) !== 0 && (rule.visualMask & 2) === 0)
		) continue;
		if ( rule.visualPriority > JOB_PRIORITY && (rule.visualMask & jobMask) !== 0 ) continue;
		if (
			accepted.some( other => {
				const prior = rules[other.refObjId]!;
				return other.avatar && prior.visualPriority < rule.visualPriority &&
					(prior.visualMask & rule.visualMask) !== 0;
			} )
		) continue;
		accepted.push( item );
	}
	// Preserve authored equipment/avatar traversal order after eligibility selection.
	const selected = new Set( accepted );
	return items.filter( item => selected.has( item ) );
}
