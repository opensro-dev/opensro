/*
===========================================================================

fortress-map.ts - the battle aide's fortress map: catalog, markers and rules

CIFFortressMap (65E330) shows the fortress page of the world map with one
marker per event zone, a status panel for the selected zone and three
buttons: construction (0x3D), repair (0x3E) and upgrade (0x3F). Everything
here is a pure function of the published catalog
(/assets/data/fortressMap.json, buildFortressMapAsset.mjs) and the zone
records the 0x3887 roster fills (fortress.ts). The window state lives in
runtime/ui/hud/fortress-map-hud.ts.

===========================================================================
*/

// The battle aide's NPC function bit (5D8FF0 adds talk action 0x32 for it).
export const FORTRESS_AIDE_CAPABILITY = 0x1000000;

// 0x71E1 actions: the structure list that opens the window (5D8930 action
// 0x32 row 2), the upgrade a construction also stages, and the repair.
export const FORTRESS_STRUCTURE_LIST = 0x18;
export const FORTRESS_STRUCTURE_UPGRADE = 0x0b;
export const FORTRESS_STRUCTURE_REPAIR = 0x0c;

const ICON_ROOT = "/assets/images/Media_extracted/icon/";
const GATE_MARKER = "/assets/images/Media_extracted/interface/worldmap/map/gate.png";

// 57E250 sizes a structure marker 0x18; 57BE10 draws a barricade's gate at
// +-8.0.
export const FORTRESS_MARKER_SIZE = 24;
export const FORTRESS_GATE_MARKER_SIZE = 16;

// Zone types the window tests by name (65CF40, 65D560, 57E250).
const ZONE_DEFENSIVE_POSITION = "DEFENSIVE_POSITION";
const ZONE_GUARD_TOWER = "GUARD_TOWER";
const ZONE_BARRICADE_PREFIX = "BARRICADE";

// 65CF40: the buttons answer the fortress commander (GuildMember_IsFortressRole1
// 827DB0) and the battle commander (GuildMember_IsFortressRole4 827E10).
const ROLE_COMMANDER = 1;
const ROLE_BATTLE_COMMANDER = 4;

// 65D560: a guard tower at rank 1 asks which element tower it becomes.
const GUARD_TOWER_TYPE_RANK = 1;

// 65D340 clamps the repair price per point into [1, 1e38].
const REPAIR_PRICE_CEILING = 1e38;

/*
================
FortressMapPage

A worldmap_mapinfo row of a fortress world, in the world map's town-page
shape.
================
*/
export interface FortressMapPage {
	readonly id: number;
	readonly world: string;
	readonly image: string;
	readonly texture: readonly [number, number];
	readonly picture: readonly [number, number];
	readonly size: readonly [number, number];
	readonly bounds: readonly [number, number, number, number, number, number, number, number];
}

/*
================
FortressMapZone
================
*/
export interface FortressMapZone {
	readonly id: number;
	readonly codeName: string;
	readonly type: string;
	readonly world: string;
	readonly defaultStructure: string;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
}

/*
================
FortressStructureReference

The RefObjChar fields the map reads (buildFortressMapAsset.mjs names the
columns). upgrades is the siegestructupgradedata row, null without one.
================
*/
export interface FortressStructureReference {
	readonly codeName: string;
	readonly name: string;
	readonly icon: string;
	readonly level: number;
	readonly maxHp: number;
	readonly defence: number;
	readonly skill: number;
	readonly costRepair: number;
	readonly costRevive: number;
	readonly upgradeGold: number;
	readonly upgradeGuildPoints: number;
	readonly upgrades: readonly number[] | null;
}

/*
================
FortressMapCatalog
================
*/
export interface FortressMapCatalog {
	readonly pages: readonly FortressMapPage[];
	readonly zones: readonly FortressMapZone[];
	readonly structures: ReadonlyMap<number, FortressStructureReference>;
}

/*
================
FortressZoneRecord

One event zone as the roster left it (76C870 case 0xF): the structure
standing there, its hit points and state word, a headquarters' guild, and
the construction minutes the structure list (0x18) reported.
================
*/
export interface FortressZoneRecord {
	readonly zone: number;
	readonly ref: number;
	readonly hp: number;
	readonly state: number;
	readonly guild: string;
	readonly minutes: number;
}

/*
================
isCount
================
*/
function isCount( value: unknown ): value is number {
	return Number.isSafeInteger( value ) && (value as number) >= 0;
}

/*
================
decodeFortressMap

The published catalog, or a thrown error naming the first malformed part.
================
*/
export function decodeFortressMap( raw: unknown ): FortressMapCatalog {
	const value = raw as { format?: unknown; pages?: unknown; zones?: unknown; structures?: unknown; };
	if ( value?.format !== "sro-fortress-map" || !Array.isArray( value.pages ) || !Array.isArray( value.zones ) ) {
		throw Error( "Invalid fortress map catalog" );
	}
	const pages: FortressMapPage[] = [];
	for ( const page of value.pages as FortressMapPage[] ) {
		const numbers = [ ...page?.texture ?? [], ...page?.picture ?? [], ...page?.size ?? [], ...page?.bounds ?? [] ];
		if (
			!isCount( page?.id ) || typeof page.world !== "string" || typeof page.image !== "string" ||
			numbers.length !== 14 || !numbers.every( Number.isSafeInteger )
		) throw Error( "Invalid fortress map page" );
		pages.push( page );
	}
	const zones: FortressMapZone[] = [];
	for ( const zone of value.zones as FortressMapZone[] ) {
		if (
			!isCount( zone?.id ) || typeof zone.codeName !== "string" || typeof zone.type !== "string" ||
			!isCount( zone.regionId ) || zone.regionId > 0xffff || ![ zone.x, zone.y, zone.z ].every( Number.isFinite )
		) throw Error( "Invalid fortress map zone" );
		zones.push( zone );
	}
	const structures = new Map<number, FortressStructureReference>();
	for (
		const [key, row] of Object.entries( (value.structures ?? {}) as Record<string, FortressStructureReference> )
	) {
		const id = Number( key );
		if (
			!isCount( id ) || id === 0 || typeof row?.name !== "string" || typeof row.icon !== "string" ||
			![
				row.level,
				row.maxHp,
				row.defence,
				row.skill,
				row.costRepair,
				row.costRevive,
				row.upgradeGold,
				row.upgradeGuildPoints
			].every( isCount ) ||
			(row.upgrades !== null && !(Array.isArray( row.upgrades ) && row.upgrades.every( isCount )))
		) throw Error( "Invalid fortress structure " + key );
		structures.set( id, row );
	}
	return { pages, zones, structures };
}

/*
================
fortressMapPageFor

The page that shows a fortress's zones: the page of the first zone the
records name, else the first page.
================
*/
export function fortressMapPageFor(
	catalog: FortressMapCatalog,
	records: readonly FortressZoneRecord[]
): FortressMapPage | undefined {
	const zone = catalog.zones.find( row => records.some( record => record.zone === row.id ) );
	return catalog.pages.find( page => page.world === zone?.world ) ?? catalog.pages[0];
}

/*
================
fortressUpgradeTarget

GlobalDataManager_ResolveRefObjIDVariant (7E14E0) with type -1, as the
status panel and the upgrade button ask it. The loader (kind 0x3C) keys a
row's first target by the structure and each later one 0x40000000 further
on; type -1 looks up the second target, falls back to the first, and
answers the structure itself when it has no row. 0 is a target codename
with no reference: the top rank.
================
*/
export function fortressUpgradeTarget( catalog: FortressMapCatalog, ref: number ): number {
	const targets = catalog.structures.get( ref )?.upgrades;
	if ( !targets?.length ) return ref;
	return targets.length > 1 ? targets[1]! : targets[0]!;
}

/*
================
fortressMarker

The marker 57E250 builds for a zone: a barricade zone always shows the
gate; any other zone shows its standing structure's icon, "_r" below full
hit points and "_d" at none, and nothing while no structure stands.
================
*/
export function fortressMarker(
	catalog: FortressMapCatalog,
	zone: FortressMapZone,
	record: FortressZoneRecord | undefined
): { readonly image: string; readonly size: number; } | null {
	if ( zone.type.startsWith( ZONE_BARRICADE_PREFIX ) ) return { image: GATE_MARKER, size: FORTRESS_GATE_MARKER_SIZE };
	const structure = record && record.ref ? catalog.structures.get( record.ref ) : undefined;
	if ( !record || !structure?.icon ) return null;
	const stem = structure.icon.slice( 0, -".ddj".length );
	const variant = record.hp === structure.maxHp ? "" : record.hp > 0 ? "_r" : "_d";
	return { image: ICON_ROOT + stem + variant + ".png", size: FORTRESS_MARKER_SIZE };
}

/*
================
FortressMapStatus

The status panel's lines (65D970, control ids 50..60). feature is the
default skill's id, 0 to hide the feature row.
================
*/
export interface FortressMapStatus {
	readonly name: string;
	readonly topRank: boolean;
	readonly state:
		| ""
		| "UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_01"
		| "UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_02"
		| "UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_03";
	readonly rank: number;
	readonly defence: number;
	readonly hp: number;
	readonly maxHp: number;
	readonly feature: number;
}

/*
================
fortressMapStatus

The panel for a selected zone's record, or null to clear it. The rank,
defence and maximum come from the standing structure's reference, the
hit points from the roster (76C870 case 0xF copies them that way).
================
*/
export function fortressMapStatus(
	catalog: FortressMapCatalog,
	record: FortressZoneRecord | undefined
): FortressMapStatus | null {
	const structure = record ? catalog.structures.get( record.ref ) : undefined;
	if ( !record || !structure ) return null;
	const { maxHp, level, defence } = structure, hp = record.hp;
	const state = maxHp > 0 && hp === maxHp ?
		"UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_01" :
		level === 0 || defence === 0 ?
		"" :
		hp > 0 ?
		"UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_02" :
		"UIIT_STT_FORT_BATTLEAIDE_STRUCTSTATUS_03";
	return {
		name: structure.name,
		topRank: fortressUpgradeTarget( catalog, record.ref ) === 0,
		state,
		rank: level,
		defence,
		hp,
		maxHp,
		feature: structure.skill
	};
}

/*
================
FortressMapButtons
================
*/
export interface FortressMapButtons {
	readonly construct: boolean;
	readonly repair: boolean;
	readonly upgrade: boolean;
}

/*
================
fortressMapButtons

CIFFortressMap_RefreshDefensivePositionButtons (65CF40): all off without a
selected zone or for anyone but the commander and the battle commander. A
defensive position offers construction while it is empty (rank 0) and
repair and upgrade once built; repair needs missing hit points, upgrade
needs full hit points and a target.
================
*/
export function fortressMapButtons(
	catalog: FortressMapCatalog,
	zone: FortressMapZone | undefined,
	record: FortressZoneRecord | undefined,
	role: number
): FortressMapButtons {
	const off = { construct: false, repair: false, upgrade: false };
	if ( !zone || !record || (role !== ROLE_COMMANDER && role !== ROLE_BATTLE_COMMANDER) ) return off;
	const structure = catalog.structures.get( record.ref );
	const rank = structure?.level ?? 0, maxHp = structure?.maxHp ?? 0;
	let construct = false, repair = true, upgrade = true;
	if ( zone.type === ZONE_DEFENSIVE_POSITION ) {
		construct = rank === 0;
		repair = upgrade = rank !== 0;
	}
	if ( maxHp === record.hp ) {
		repair = false;
		if ( fortressUpgradeTarget( catalog, record.ref ) === 0 ) upgrade = false;
	} else {
		upgrade = false;
	}
	return { construct, repair, upgrade };
}

/*
================
fortressNeedsTowerType

65D560: upgrading a rank-1 guard tower first asks for the element
(UIIT_PAG_FORT_BATTLEAIDE_UPGRADETYPE); everything else confirms type 0.
================
*/
export function fortressNeedsTowerType(
	catalog: FortressMapCatalog,
	zone: FortressMapZone,
	record: FortressZoneRecord
): boolean {
	return zone.type.startsWith( ZONE_GUARD_TOWER ) &&
		catalog.structures.get( record.ref )?.level === GUARD_TOWER_TYPE_RANK;
}

/*
================
fortressTowerTypes

65E330's popup rows, in type order 0..3 (the four symbols it pushes into
the window's list at +0x874).
================
*/
export function fortressTowerTypes() {
	return [
		"UIIT_STT_FORT_BATTLEAIDE_UPGRADE_LIGTHNING",
		"UIIT_STT_FORT_BATTLEAIDE_UPGRADE_BLAZE",
		"UIIT_STT_FORT_BATTLEAIDE_UPGRADE_FREEZING",
		"UIIT_STT_FORT_BATTLEAIDE_UPGRADE_DARK"
	] as const;
}

/*
================
fortressRepairPrice

65D340: the price per hit point is the repair cost over the maximum,
clamped into [1, 1e38]; a destroyed structure counts one hit point and
adds its revive cost; nothing is missing at full hit points.
================
*/
export function fortressRepairPrice( structure: FortressStructureReference, hp: number ): number {
	let base = 0, current = hp;
	if ( current === 0 ) {
		base = structure.costRevive;
		current = 1;
	}
	const missing = structure.maxHp - current;
	if ( missing <= 0 ) base = 0;
	const ratio = Math.fround( structure.costRepair / structure.maxHp );
	const perPoint = !(ratio >= 1) ? 1 : ratio > REPAIR_PRICE_CEILING ? REPAIR_PRICE_CEILING : ratio;
	return base + Math.trunc( missing * perPoint );
}
