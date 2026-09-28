/*
===========================================================================

monster-nameplate.ts - party classification beside a monster's world name

The spawn record's auxiliary rarity byte owns this mark. Base rarity controls
the target-panel grade separately; champion and giant party monsters share
the same overhead mark as ordinary party monsters.

===========================================================================
*/

import type { EntityState } from "@/engine/contracts/world";
import type { UiRect } from "@/engine/contracts/ui";

const PARTY_MONSTER_ICON = "/assets/images/Media_extracted/icon/item/etc/europe_partymob.png";
const PARTY_AUXILIARY_FLAG = 1;
const ICON_SIZE = 16;
const ICON_NAME_OFFSET = 20;
const NATIVE_BOARD_OFFSET = 1.5;

/*
================
monsterPartyNameplate

862060 kind 4 tests CICMonster+771 == 1 and draws a 16-pixel mark twenty
pixels before the 781940 name-board origin. Selection changes name width,
so placement consumes the measured width rather than a guessed name length.
================
*/
export function monsterPartyNameplate( entity: EntityState, nameSize: readonly [number, number] ) {
	if ( entity.kind !== "monster" || entity.rarityAuxIcon !== PARTY_AUXILIARY_FLAG || nameSize[1] <= 0 ) return null;
	const rect: UiRect = [
		-(nameSize[0] >> 1) - NATIVE_BOARD_OFFSET - ICON_NAME_OFFSET,
		-(nameSize[1] >> 1) - NATIVE_BOARD_OFFSET,
		ICON_SIZE,
		ICON_SIZE
	];
	return { path: PARTY_MONSTER_ICON, rect };
}
