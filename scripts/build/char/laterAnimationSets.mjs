/*
===========================================================================

laterAnimationSets.mjs - the Devil's Spirit transformation sets on the bodies

Port-only, not native to v1.150 (SRO_NASRUN, #4407). The later clients'
character BSRs carry three more animation sets, avatar_nasrun1..3: the
transformation's contraction (state 40, ANI_READY01) and its explosive
jump (state 191, ANI_SHOT), then its stand, run and wait. The v1.150 BSRs
have none. The Nasrun skill import stages each later BSR that authors
them beside the tree (its marker's animationSources) and copies their
clips into it; the locomotion build appends those sets to the body's own,
so they publish as any authored set (native:<set>:<state> clips).

Only the named sets are taken: the v1.150 body keeps every set it has, and
a set it already authors is never replaced. Without the marker (Nasrun
off) nothing changes.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { parseCharacterBsr } from "./formats.mjs";
import { NASRUN_SKILL_MARKER } from "./visualChanges.mjs";

/*
================
loadLaterAnimationSources

The import's { bsrPath: later BSR file } and the set names to take, or
null when the import staged none. root is the extracted tree.
================
*/
export function loadLaterAnimationSources( root ) {
	const marker = path.join( root, NASRUN_SKILL_MARKER );
	if ( !fs.existsSync( marker ) ) return null;
	const { animationSources, animationSets } = JSON.parse( fs.readFileSync( marker, "utf8" ) );
	if ( !animationSources || !Array.isArray( animationSets ) || !animationSets.length ) return null;
	const sources = new Map();
	for ( const [bsrPath, staged] of Object.entries( animationSources ) ) {
		const file = path.resolve( root, staged );
		if ( !file.startsWith( path.resolve( root ) + path.sep ) ) {
			throw Error( "Later BSR outside the tree: " + staged );
		}
		sources.set( normalizedBsrPath( bsrPath ), file );
	}
	return { sources, setNames: new Set( animationSets.map( name => String( name ).toLowerCase() ) ) };
}

/*
================
withLaterAnimationSets

bsr with the later body's named sets appended, for the body at bsrPath.
================
*/
export function withLaterAnimationSets( bsr, bsrPath, later ) {
	const file = later?.sources.get( normalizedBsrPath( bsrPath ) );
	if ( !file ) return bsr;
	const own = new Set( (bsr.animationSets ?? []).map( set => set.name.toLowerCase() ) );
	const added = parseCharacterBsr( fs.readFileSync( file ), file ).animationSets.filter( set =>
		later.setNames.has( set.name.toLowerCase() ) && !own.has( set.name.toLowerCase() )
	);
	return added.length ? { ...bsr, animationSets: [ ...(bsr.animationSets ?? []), ...added ] } : bsr;
}

/*
================
normalizedBsrPath
================
*/
function normalizedBsrPath( value ) {
	return String( value ).replaceAll( "\\", "/" ).toLowerCase();
}
