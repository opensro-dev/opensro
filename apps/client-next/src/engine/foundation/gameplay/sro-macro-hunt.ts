/*
===========================================================================

sro-macro-hunt.ts - the SRO Macro's auto-hunt decisions (port-only)

Port-only, not native (#3907): the v1.696 Auto-Hunt, read from the v1.696
client (research note sro-macro.md, "Auto-hunt engine"). The engine walks
ordered step tables on object 017b4690; this module is the pure decision
surface the worker asks each tick: resurrect, return, repair, Energy of
Life, berserk, pickup, and which monster to take inside the configured
range.

The worker owns the world facts (vitals, bag counts, nearest monsters /
drops) and performs the resulting commands through the same paths a
player click would (item use, rebirth, berserk, select, pickup).

===========================================================================
*/

// Hunt block byte indices (live 017b3806.., writer 00da8bb0).
export const HUNT_BYTE_RANGE = 0;
export const HUNT_BYTE_FLAG = 1;
export const HUNT_BYTE_BERSERK = 2;
export const HUNT_BYTE_ENERGY = 3;
export const HUNT_BYTE_RESURRECT = 4;
export const HUNT_BYTE_RETURN = 5;
export const HUNT_BYTE_POTION_EMPTY = 6;
export const HUNT_BYTE_REPAIR = 7;
export const HUNT_BYTE_PICKUP = 8;
export const HUNT_BYTE_PILLAR = 9;
export const HUNT_BYTE_SPIRIT = 10;
export const HUNT_BYTE_TIME_TOWN = 11;

// 0068e700 jump table: 0 = In place, 1..4 = 30/50/70/100 m (native units
// are metres * 10: 300/500/700/1000).
export const HUNT_RANGE_IN_PLACE = 0;
const HUNT_RANGE_METRES: Readonly<Record<number, number>> = {
	1: 30,
	2: 50,
	3: 70,
	4: 100
};
// In-place still selects a mob in melee reach; native 68db80 keeps a
// short-range finder when the float is 0. Port inference: 7 units.
export const HUNT_IN_PLACE_REACH = 7;

// 006862d0 while dead.
export const HUNT_RESURRECT_SCROLL = 0;
export const HUNT_RESURRECT_TOWN = 1;
export const HUNT_RESURRECT_WAIT = 2;

// 00686b50 after 68ca40 says something is broken.
export const HUNT_REPAIR_HAMMER = 0;
export const HUNT_REPAIR_TOWN = 1;

// 00686a30: 0 ("All") always fires; any other stored value is "Not use".
export const HUNT_BERSERK_ALL = 0;

// 00687130: hours * 0x36ee80 ms from hunt start.
export const HUNT_HOUR_MS = 0x36ee80;

// 00686450: return when a registered HP/MP potion stack is under this.
export const HUNT_POTION_EMPTY_COUNT = 10;

// Item type words (bits 0xFFFC), same packing as count-job.ts.
const TYPE_MASK = 0xfffc;
const RETURN_SCROLL_TYPE = 0x09ec; // 3/3/3/1
const RESURRECTION_TYPE = 0x36ec; // 3/3/13/6
const HWAN_CHARGE_TYPE = 0x40ec; // 3/3/1/8 Energy of Life
const REPAIR_HAMMER_TYPE = 0x3eec; // 3/3/13/7 ITEM_MALL_REPAIR_HAMMER

/*
================
huntItemKind

Which hunt-used consumable a bag row is, by typeFlags.
================
*/
export type HuntItemKind = "return" | "resurrect" | "energy" | "repair-hammer";

export function huntItemKind( typeFlags: number ): HuntItemKind | undefined {
	const word = typeFlags & TYPE_MASK;
	if ( word === RETURN_SCROLL_TYPE ) return "return";
	if ( word === RESURRECTION_TYPE ) return "resurrect";
	if ( word === HWAN_CHARGE_TYPE ) return "energy";
	if ( word === REPAIR_HAMMER_TYPE ) return "repair-hammer";
	return undefined;
}

/*
================
MacroHuntAction

One action the hunt tick wants this frame. "none" means keep whatever
target/state the player already has.
================
*/
export type MacroHuntReturnReason = "timer" | "potion-hp" | "potion-mp" | "repair" | "vip-time";

export type MacroHuntAction =
	| { readonly kind: "none"; }
	| { readonly kind: "resurrect-scroll"; }
	| { readonly kind: "resurrect-town"; readonly noScroll?: boolean; }
	| { readonly kind: "wait-resurrect"; }
	| {
		readonly kind: "return-scroll";
		readonly reason: MacroHuntReturnReason;
		// Set when repair fell through because the hammer was missing
		// (UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_NOT_HAMMER).
		readonly noHammer?: boolean;
		// After a return the native queues hunt OFF (006865e0 / +0x18c).
		readonly endHunt?: boolean;
	}
	| { readonly kind: "repair-hammer"; }
	| { readonly kind: "energy-of-life"; }
	| { readonly kind: "berserk"; }
	| { readonly kind: "pickup"; readonly gid: number; }
	| { readonly kind: "select"; readonly gid: number; };

/*
================
MacroHuntFacts

World facts the worker gathers for one tick. Counts are stack totals of
the matching bag items; missing items are 0 / false.
================
*/
export interface MacroHuntFacts {
	readonly alive: boolean;
	readonly startedAtMs: number;
	readonly nowMs: number;
	readonly berserkGauge: number;
	readonly potionOn: boolean;
	readonly hpPotionCount: number;
	readonly mpPotionCount: number;
	readonly hasReturnScroll: boolean;
	readonly hasResurrectScroll: boolean;
	readonly hasRepairHammer: boolean;
	readonly hasEnergyOfLife: boolean;
	readonly equipmentBroken: boolean;
	// 006872d0: VIP / usage-limit clock already exhausted (byte 11).
	readonly usageTimeEnded: boolean;
	readonly targetGid: number | undefined;
	readonly targetLivingMonster: boolean;
	readonly nearestMonsterGid: number | undefined;
	readonly nearestPickupGid: number | undefined;
}

/*
================
huntRangeReach

The planar reach in world units for nearest-monster selection, or 0 when
the range byte is unknown (refuse to roam).
================
*/
export function huntRangeReach( rangeByte: number ): number {
	if ( rangeByte === HUNT_RANGE_IN_PLACE ) return HUNT_IN_PLACE_REACH;
	return HUNT_RANGE_METRES[rangeByte] ?? 0;
}

/*
================
huntReturnDeadlineMs

When the auto-return timer fires, or 0 when the option is off / unset.
================
*/
export function huntReturnDeadlineMs( returnByte: number, startedAtMs: number ): number {
	if ( !returnByte || startedAtMs <= 0 ) return 0;
	return startedAtMs + returnByte * HUNT_HOUR_MS;
}

/*
================
isDimensionPillarName

Byte 9 ("Do not attack Dimension pillar") has no engine imm xref beyond
the loader (sro-macro.md). Port inference: skip a monster whose display
name names the pillar (UIIT_STT_MACRO_NOT_ATTACK_HOLL / media English).
================
*/
export function isDimensionPillarName( name: string ): boolean {
	const lower = name.toLowerCase();
	return lower.includes( "dimension pillar" ) || lower.includes( "dimensional pillar" );
}

/*
================
pickMacroHuntAction

One tick of the survival / support steps, then target acquisition. Order
follows the native support table's priorities where they are read
(resurrect → return timer → potion empty → repair → Energy of Life →
berserk → pickup → select). A step that needs an item the bag lacks is
skipped so a later step can still run; the native often returns to town
instead — the port keeps hunting until a return scroll is present.
================
*/
export function pickMacroHuntAction(
	hunt: readonly number[],
	facts: MacroHuntFacts
): MacroHuntAction {
	if ( !facts.alive ) {
		const mode = hunt[HUNT_BYTE_RESURRECT] ?? HUNT_RESURRECT_SCROLL;
		if ( mode === HUNT_RESURRECT_WAIT ) return { kind: "wait-resurrect" };
		if ( mode === HUNT_RESURRECT_TOWN ) return { kind: "resurrect-town" };
		if ( facts.hasResurrectScroll ) return { kind: "resurrect-scroll" };
		// No scroll: fall through to town, as UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_NOT_REVIVAL_SCROLL.
		return { kind: "resurrect-town", noScroll: true };
	}

	const deadline = huntReturnDeadlineMs( hunt[HUNT_BYTE_RETURN] ?? 0, facts.startedAtMs );
	if ( deadline > 0 && facts.nowMs >= deadline && facts.hasReturnScroll ) {
		return { kind: "return-scroll", reason: "timer", endHunt: true };
	}

	// 006872d0: skill-on is gated in the worker; byte 11 alone here.
	if ( (hunt[HUNT_BYTE_TIME_TOWN] ?? 0) !== 0 && facts.usageTimeEnded && facts.hasReturnScroll ) {
		return { kind: "return-scroll", reason: "vip-time", endHunt: true };
	}

	if ( (hunt[HUNT_BYTE_POTION_EMPTY] ?? 0) !== 0 && facts.potionOn ) {
		if ( facts.hpPotionCount > 0 && facts.hpPotionCount < HUNT_POTION_EMPTY_COUNT && facts.hasReturnScroll ) {
			return { kind: "return-scroll", reason: "potion-hp", endHunt: true };
		}
		if ( facts.mpPotionCount > 0 && facts.mpPotionCount < HUNT_POTION_EMPTY_COUNT && facts.hasReturnScroll ) {
			return { kind: "return-scroll", reason: "potion-mp", endHunt: true };
		}
	}

	if ( facts.equipmentBroken ) {
		const repair = hunt[HUNT_BYTE_REPAIR] ?? HUNT_REPAIR_HAMMER;
		if ( repair === HUNT_REPAIR_HAMMER && facts.hasRepairHammer ) return { kind: "repair-hammer" };
		if ( facts.hasReturnScroll ) {
			return {
				kind: "return-scroll",
				reason: "repair",
				noHammer: repair === HUNT_REPAIR_HAMMER && !facts.hasRepairHammer,
				endHunt: true
			};
		}
	}

	if ( (hunt[HUNT_BYTE_ENERGY] ?? 0) !== 0 && facts.hasEnergyOfLife && facts.berserkGauge < 5 ) {
		return { kind: "energy-of-life" };
	}

	if ( (hunt[HUNT_BYTE_BERSERK] ?? 0) === HUNT_BERSERK_ALL && facts.berserkGauge === 5 ) {
		return { kind: "berserk" };
	}

	if ( (hunt[HUNT_BYTE_PICKUP] ?? 0) !== 0 && facts.nearestPickupGid !== undefined && !facts.targetLivingMonster ) {
		return { kind: "pickup", gid: facts.nearestPickupGid };
	}

	if ( !facts.targetLivingMonster && facts.nearestMonsterGid !== undefined ) {
		return { kind: "select", gid: facts.nearestMonsterGid };
	}

	return { kind: "none" };
}
