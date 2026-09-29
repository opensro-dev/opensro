/*
===========================================================================

buildDefaultWear.mjs - native default clothing and fortress clothing

A body with no protector in a slot wears the native default garment, and a
fortress war participant wears the fortress resource in the team's material
set. Both are item attachments converted on a donor of the wearer's race and
sex (itemAttachments.mjs).

===========================================================================
*/
import { retailTextdataRoot } from "../world/paths.mjs";
import { resolveRoster } from "./resolveCharRoster.mjs";
import { buildItemSetGlb, createDonorPool } from "./itemAttachments.mjs";

const RACES = [ "CH", "EU" ];
const SEXES = [ "M", "W" ];
const DEFAULT_WEAR_FAMILIES = [ "clothes", "light" ];
const DEFAULT_WEAR_PARTS = [ "BA", "LA" ];
// 8E8EF0 selects the fortress clothing material set by team.
const FORTRESS_MATERIAL_SETS = [ 0, 3, 4 ];

/*
================
defaultWearBsrPath
================
*/
function defaultWearBsrPath( race, sex, family, part ) {
	const region = race === "CH" ? "china" : "europe";
	const body = sex === "M" ? "man" : "woman";
	const variant = race === "CH" ? "00" : "01";
	return `res/item/${region}/${body}_item/${family}_${variant}_${part.toLowerCase()}.bsr`;
}

/*
================
buildDefaultWear

Fills dress.defaultWear and dress.fortressWear. 8E91C0's table includes EU
defaults for ownerless and nonplayer models; live EU players are rejected by
the caller. EU players never attach the fortress resource either, but EU
ownerless character previews can.
================
*/
export async function buildDefaultWear( dress ) {
	const donorFor = createDonorPool( resolveRoster( retailTextdataRoot ).resolved );
	dress.defaultWear = {};
	dress.fortressWear = {};
	for ( const race of RACES ) {
		for ( const sex of SEXES ) {
			const donor = await donorFor( race, sex );
			if ( !donor ) throw Error( `Missing clothing donor ${race}/${sex}` );
			for ( const family of DEFAULT_WEAR_FAMILIES ) {
				for ( const part of DEFAULT_WEAR_PARTS ) {
					const key = `${race}_${sex}_${family}_${part}`;
					const entry = await buildItemSetGlb( {
						tag: "default-wear",
						key,
						...donor,
						pieces: [ { part, itemBsrPath: defaultWearBsrPath( race, sex, family, part ) } ],
						outSubdir: "equipment"
					} );
					if ( !entry ) throw Error( `Missing default wear ${key}` );
					dress.defaultWear[key] = entry;
				}
			}
			for ( const materialSetId of FORTRESS_MATERIAL_SETS ) {
				const key = `${race}_${sex}_${materialSetId}`;
				const entry = await buildItemSetGlb( {
					tag: "fortress-wear",
					key: "fortress_" + key,
					...donor,
					pieces: [ {
						part: "FORT",
						itemBsrPath: `res/item/etc/fort_${sex === "M" ? "man" : "woman"}.bsr`,
						materialSetId
					} ],
					outSubdir: "equipment"
				} );
				if ( !entry ) throw Error( `Missing fortress wear ${key}` );
				dress.fortressWear[key] = entry;
			}
		}
	}
}
