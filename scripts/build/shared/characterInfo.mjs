/*
===========================================================================

characterInfo.mjs - #section characterInfo parsing and RefObj inheritance

Native CSkillEffectManager_LoadSkillEffectTextData #section characterInfo. This is one record, not separate
sound and mount catalogs: CICharactor_AttachVisualModel binds it at CICharactor+0x710;
ResourceTypeName feeds the sound callbacks, Ride Type lands at +0x18,
the optional second BSR lands at +0x24 for CICMonster_DeserializeSpawnPacket's CICRide branch,
and column 5 lands at +0x28 for CICharactor_Action_KnockdownDie's death model swap.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { normalizeAssetPath } from "./assetPaths.mjs";
import { splitTextDataRow } from "./textDataIo.mjs";

/*
================
expandCharacterInfoCodenames

Mirror SkillEffectCharacterInfo_ParseRow (CharacterInfo_ParseAndRegisterRow): a characterInfo key
whose final seven characters are NNN~NNN denotes an inclusive family. The
native loader registers one action-effect context for every expanded RefObj
name. Keep this expansion at the data boundary; consumers must never need to
know whether a record was authored directly or through the compact form.
================
*/
export function expandCharacterInfoCodenames( codename ) {
	const match = /^(.*?)(\d{3})~(\d{3})$/.exec( String( codename ?? "" ) );
	if ( !match ) return codename ? [ String( codename ) ] : [];
	const start = Number( match[2] );
	const end = Number( match[3] );
	if ( start > end ) return [];
	return Array.from(
		{ length: end - start + 1 },
		( _unused, index ) => `${match[1]}${String( start + index ).padStart( 3, "0" )}`
	);
}

/*
================
resolveCharacterInfoRows

91B7E0 registers only existing references; the first success seeds F08F58.
9171B0 looks up self, then common+5C (OrgObjCodeName128), then that seed.
The original-object lookup is one hop, not recursive inheritance.
================
*/
export function resolveCharacterInfoRows( authored, characterRows ) {
	const characters = new Map( [ ...characterRows ].filter( ( [, row] ) => Number( row[0] ) === 1 ) );
	const registered = new Map(
		authored.filter( row => characters.has( row.codename ) ).map( row => [ row.codename, row ] )
	);
	const first = registered.values().next().value;
	if ( !first ) throw Error( "No registered native characterInfo default" );
	return [ ...characters ].map( ( [codename, row] ) => {
		const source = registered.get( codename ) ?? registered.get( row[4] ) ?? first;
		return { ...source, codename, contextCodename: source.codename };
	} );
}

/*
================
normalizeCharacterInfoBsrPath

Canonicalize a characterInfo ride or death BSR cell into a res/... path, or
null when authored as "none" / empty.
================
*/
export function normalizeCharacterInfoBsrPath( value ) {
	const normalized = normalizeAssetPath( value );
	if ( !normalized || normalized === "none" ) return null;
	return normalized.startsWith( "res/" ) ? normalized : `res/${normalized}`;
}

/*
================
characterInfoResolver

CharacterInfo_FindByRefThenOriginalThenDefault (9171B0): a character's own
characterInfo row, else its original reference's, else the default, which is
the first row whose codename names a known reference
(CharacterInfo_RegisterExistingRefAndSeedDefault, 91B7E0). Every character
resolves, so none is left without a sound and effect profile. Native reads the
original reference from the character data (+0x5C); the clone base link in
characterdata column 4, the chain resolveNpcModel follows for the model, is
taken to be that reference. `records` keeps file order (a Map).
================
*/
export function characterInfoResolver( records, rows ) {
	const BASE_COLUMN = 4;
	const defaultCodename = [ ...records.keys() ].find( codename => rows.has( codename ) );
	const fallback = defaultCodename === undefined ? undefined : records.get( defaultCodename );
	return codename => records.get( codename ) ?? records.get( rows.get( codename )?.[BASE_COLUMN] ?? "" ) ?? fallback;
}

/*
================
loadCharacterInfo

Parse #section characterInfo from skilleffect.txt into a Map<codename, record>,
expanding NNN~NNN families and rejecting duplicate codenames.
================
*/
export function loadCharacterInfo( textdataDir ) {
	const records = new Map();
	const sourceLines = new Map();
	const text = fs
		.readFileSync( path.join( textdataDir, "skilleffect.txt" ), "utf16le" )
		.replace( /^\uFEFF/, "" );
	let inCharacterInfo = false;
	for ( const [lineIndex, line] of text.split( /\r?\n/ ).entries() ) {
		if ( line.startsWith( "#section" ) ) {
			inCharacterInfo = /^#section\s+characterInfo\b/i.test( line );
			continue;
		}
		if ( !inCharacterInfo || !line || line.startsWith( "//" ) ) continue;
		const cols = splitTextDataRow( line );
		const codename = cols[0];
		const soundProfileName = cols[1];
		if ( !codename ) continue;
		const rideTypeName = String( cols[3] ?? "none" ).trim().toUpperCase();
		const rideModelPath = normalizeCharacterInfoBsrPath( cols[4] );
		// 91B830 stores column 5 at record +0x28: the mesh 8E64F0 loads on death.
		const deathModelPath = normalizeCharacterInfoBsrPath( cols[5] );
		const riderTransformMode = rideTypeName === "RT_FIXED" ? 1 : rideTypeName === "RT_DUMMY" ? 2 : 0;
		const record = {
			soundProfileName: soundProfileName && soundProfileName.toLowerCase() !== "none" ? soundProfileName : null,
			rideModelPath,
			deathModelPath,
			riderTransformMode
		};
		for ( const expandedCodename of expandCharacterInfoCodenames( codename ) ) {
			const priorLine = sourceLines.get( expandedCodename );
			if ( priorLine !== undefined ) {
				throw new Error(
					`Duplicate skilleffect characterInfo codename ${expandedCodename}: ` +
						`lines ${priorLine + 1} and ${lineIndex + 1}`
				);
			}
			records.set( expandedCodename, record );
			sourceLines.set( expandedCodename, lineIndex );
		}
	}
	return records;
}
