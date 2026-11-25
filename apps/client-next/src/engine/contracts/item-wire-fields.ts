/*
===========================================================================

item-wire-fields.ts - the item fields a browser build reads (#4448)

One owner for the read set behind the server's wire diet (#4421,
#4447): a name belongs here exactly when client code reads it from an
item row's nativeFields or its tooltip.fields copy. The server
publishes exactly these on a wire row (wireItemFieldNames in
apps/server/internal/game/enterworld, checked against the generated
itemwirefields.json copy of this tuple), and the field bags are typed
ItemFieldValues so tsc is the census: a read of an unlisted name,
through any alias or helper, fails the type check instead of silently
thinning a player's tooltip.

Grouped by reader, mirroring nativefields.go.
===========================================================================
*/

export const ITEM_WIRE_FIELDS = [
	// Equipment stat families (item-tooltip-stats.ts).
	"varianceIntMin1c0",
	"varianceIntMax1c4",
	"varianceFloatMin1c8",
	"varianceFloatMax1cc",
	"varianceFloatPerPlus1d0",
	"varianceIntMin1d4",
	"varianceIntMax1d8",
	"varianceFloatPerPlus1dc",
	"varianceFloatMin1e0",
	"varianceFloatMax1e4",
	"varianceFloatPerPlus1e8",
	"varianceIntMin1ec",
	"varianceIntMax1f0",
	"varianceFloatMin1f4",
	"varianceFloatMax1f8",
	"varianceFloatPerPlus1fc",
	"varianceFloatMin200",
	"varianceFloatMax204",
	"varianceFloatPerPlus208",
	"varianceFloatMin20c",
	"varianceFloatMax210",
	"varianceFloatMin214",
	"varianceFloatMax218",
	"actionRange23c",
	"varianceIntMin240",
	"varianceIntMax244",
	"varianceIntMin248",
	"varianceIntMax24c",
	"varianceFloatPerPlus250",
	"varianceIntMin254",
	"varianceIntMax258",
	"varianceIntMin25c",
	"varianceIntMax260",
	"varianceFloatPerPlus264",
	"varianceFloatMin268",
	"varianceFloatMax26c",
	"varianceFloatMin270",
	"varianceFloatMax274",
	"varianceFloatMin278",
	"varianceFloatMax27c",
	"varianceFloatMin280",
	"varianceFloatMax284",
	"varianceIntMin288",
	"varianceIntMax28c",
	"varianceFloatPerPlus290",
	"varianceIntMin294",
	"varianceIntMax298",
	// Tooltip rows, prices and gates.
	"cashItem",
	"rarity",
	"canRepair",
	"price",
	"repairCostB4",
	"reviveCostB8",
	"country",
	"reqStr",
	"reqInt",
	"reqLevelType1",
	"requiredLevel",
	"reqLevelType2",
	"requiredLevel2",
	"reqLevelType3",
	"requiredLevel3",
	"reqLevelType4",
	"requiredLevel4",
	"maxStack",
	"reqGender",
	"itemClass",
	"setId",
	"maxDurability",
	"itemParam2_2a0",
	"itemParam6_2b0",
	"maxMagicOptions51c",
	"avatarAttachment51d",
	// Use, cooldown, macro and stone readers.
	"useCooldownGroup524",
	"useCooldownDuration528",
	"associatedSkillId",
	"useSkillId",
	"useSkillDurationMs",
	"canUse",
	"itemParam1_29c",
	"itemParam3_2a4",
	"itemParam4_2a8",
	"itemParam5_2ac",
	"stoneFamilyMask",
	"stoneTooltipMask"
] as const;

/*
================
ITEM_REQUIREMENT_SLOTS

The four native requirement slots as (type, level) field pairs: the
tooltip's requirement rows and the equip/quickslot gates walk all
four, not only the first.
================
*/
export const ITEM_REQUIREMENT_SLOTS = [
	[ "reqLevelType1", "requiredLevel" ],
	[ "reqLevelType2", "requiredLevel2" ],
	[ "reqLevelType3", "requiredLevel3" ],
	[ "reqLevelType4", "requiredLevel4" ]
] as const;

/*
================
ItemWireField
================
*/
export type ItemWireField = typeof ITEM_WIRE_FIELDS[number];

/*
================
ItemFieldValues

The field bag itself: any subset of the read set. Producers build it
from the validated wire row (item-tooltip-reference.ts); every read of
a name outside the tuple is a type error.
================
*/
export type ItemFieldValues = Partial<Readonly<Record<ItemWireField, number>>>;
