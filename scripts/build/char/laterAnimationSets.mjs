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

/*
================
laterTransformAuras

The transformation sets' auras, as worn-avatar effect rows by set name:
{ avatar_nasrun3: [{ effectPath, bone, root, scale, offset }] }. The later
bodies author them as animation-state particles (ModDataSet kind 1):
only avatar_nasrun3's stand (0), alternate stand (61) and combat stance
(6) carry one, keep03 at the character's origin. v1.150's modifier queue
(AnimationBindings_ApplyMotionResource 0xAB3DB0) turns a state's set off
before it looks up the next, so a run with no set of its own would drop
the aura; the later client keeps it while the wearer moves (the owner's
account, 2026-11-27; walking not yet confirmed). The aura is therefore
published as a persistent effect of the costume (buildEquipmentVisuals
costumeEffects), worn and removed with it, never as a state modifier of
the body: no other model's modifier queue changes. Each entry is kept
once across the bodies and states that repeat it. Empty without a later
source. readBsr parses one staged body (injected by tests).
================
*/
export function laterTransformAuras( later, readBsr = file => parseCharacterBsr( fs.readFileSync( file ), file ) ) {
	const auras = {}, seen = new Set();
	for ( const file of later?.sources.values() ?? [] ) {
		for ( const modifier of readBsr( file ).particleModifiers ) {
			const set = modifier.animationSetName.toLowerCase();
			if ( modifier.kind !== 1 || !later.setNames.has( set ) ) continue;
			for ( const entry of modifier.entries ) {
				const effectPath = normalizedBsrPath( entry.effectPath ), bone = entry.boneName ?? "";
				const offset = entry.vector3c ?? [ 0, 0, 0 ];
				const key = [ set, effectPath, bone, ...offset ].join( "|" );
				if ( seen.has( key ) ) continue;
				seen.add( key );
				(auras[set] ??= []).push( { effectPath, bone, root: bone === "", scale: 1, offset } );
			}
		}
	}
	return auras;
}
