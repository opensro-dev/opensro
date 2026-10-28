import type {WireFrame} from '@/engine/contracts/network';
// 775F20 (opcode 0x3691) is the COS buff/state refresh. Its subtype byte
// selects three wire shapes; subtype 3 and 9 both carry an item-keyed window
// and reach 67A470 -> 6E6E00 kind 3, the only producer of a two-bar board
// slot. The item is one whose Param1 is authored in SECONDS; in v1.150 the
// COS-scoped family that fits is ITEM_MALL_PET_SKILL_* / growth potion (1800).
// A COS summoner's Param1 is minutes and cannot drive it, and nothing ties
// the GOLD_TIME_SERVICE premium tickets to this opcode.
// 6E5D40 stores a literal kind 1 for every other timed entry, so no attached
// effect can ever reach this shape.
export interface CosItemWindow {
    readonly itemRefObjId: number;
    // 6E6E00 seeds the elapsed accumulator to limit - arg5, and 6E6AA0 draws
    // (limit - elapsed) / limit. arg5 is therefore what is LEFT, not what has
    // run: kinds 6, 7 and 8 subtract it from a fixed cap the same way.
    readonly remainingSec: number;
    readonly packedExtra: number;
    readonly receivedAtMs: number;
}
// 6E6150 removes the matching row when its submit flag is 0, which 775F20
// selects by sending a zero remaining and extra. Absence and expiry are
// different states here: an exhausted window keeps its row.
export type CosTimerUpdate =
    | {readonly kind: 'set'; readonly timer: CosItemWindow}
    | {readonly kind: 'remove'; readonly itemRefObjId: number};
// One admitted item reference: 6E6E00 kind 3 resolves the ITEM record through
// 7EFE70 and reads its +0x29C Param1 as seconds and its +0x2A4 Param3 as the
// second bar's millisecond limit. 562600 prints the same +0x29C for a recall
// scroll and 6B1B30 casts its gauge from it, which is what pins the column.
export interface CosItemWindowReference {
    readonly durationSec: number;
    readonly aux: number;
    readonly icon?: string;
    readonly name?: string;
}
// Param1/Param3 are generic signed RefItem columns. Interpret them as time
// only for the same 3/3/13/15 family admitted by server PetSkillItemType.
// Other items use these columns for unrelated values, including negatives.
export function cosTimerReference(row: {
    readonly typeFlags: number;
    readonly nativeFields?: {readonly itemParam1_29c?: number; readonly itemParam3_2a4?: number};
    readonly icon?: string;
    readonly name?: string;
}): CosItemWindowReference | null {
    if ((row.typeFlags & 0xfffc) !== ((3 << 2) | (3 << 5) | (13 << 7) | (15 << 11))) return null;
    const duration = row.nativeFields?.itemParam1_29c;
    if (duration === undefined) return null;
    if (!Number.isInteger(duration) || duration < -1 || duration > 0xffffffff) throw Error('Invalid COS window duration');
    const aux = row.nativeFields?.itemParam3_2a4 ?? -1;
    if (!Number.isInteger(aux) || aux < -1 || aux > 0xffffffff) throw Error('Invalid COS window aux');
    return {durationSec: duration >>> 0, aux: aux >>> 0, ...(row.icon ? {icon: row.icon} : {}), ...(row.name ? {name: row.name} : {})};
}
export function cosTimerPacket(frame: WireFrame, nowMs: number): CosTimerUpdate | null {
    if (frame.opcode !== 0x3691) return null;
    const p = frame.payload;
    if (!p.length) throw Error('Empty COS state refresh');
    // Subtype 8 and the remain-time arms drive native windows this projection
    // does not own. They are consumed natively without a board window.
    if (p[0] !== 3 && p[0] !== 9) return null;
    if (p.length !== 13) throw Error('Invalid COS summon timer');
    const v = new DataView(p.buffer, p.byteOffset, p.byteLength);
    const itemRefObjId = v.getUint32(1, true), remainingSec = v.getUint32(5, true), packedExtra = v.getUint32(9, true);
    if (!itemRefObjId) throw Error('Invalid COS window reference');
    if (!remainingSec && !packedExtra) return {kind: 'remove', itemRefObjId};
    return {kind: 'set', timer: {itemRefObjId, remainingSec, packedExtra, receivedAtMs: nowMs}};
}
// 6E6AA0 treats a limit whose halves are both -1 as "no limit" and holds the
// bar full; a zero limit fails its ordering guard and holds it full too.
const NO_LIMIT = 0xffffffff;
function clamp(value: number) { return Math.min(1, Math.max(0, value)); }
// The primary bar counts the record duration down from what is left; 6E6AA0
// increments its elapsed accumulator unconditionally on every frame delta
// without gating on pet presence. The second bar runs the +0x2A4 aux against
// the low u16 of the packed extra, which 6E6AA0 decrements only while
// CICUser_IsInCombat(localPlayer) == 1 (localPlayer + 0x781, set by 0x3122 channel 8
// StateChannelBattle, i.e. in-combat state). For pet skill items with aux -1,
// the second bar remains full (1.0).
export function cosTimerBars(timer: CosItemWindow, reference: CosItemWindowReference, nowMs: number) {
    const since = Math.max(0, nowMs - timer.receivedAtMs), limit = reference.durationSec * 1000;
    if (reference.durationSec === NO_LIMIT || !(limit > 0)) return null;
    const aux = reference.aux;
    return {
        primary: clamp((timer.remainingSec * 1000 - since) / limit),
        secondary: aux === NO_LIMIT || !(aux > 0) ? 1 : clamp(((timer.packedExtra & 0xffff) * 1000 - since) / aux),
    };
}
