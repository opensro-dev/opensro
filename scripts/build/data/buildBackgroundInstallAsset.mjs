/*
===========================================================================

buildBackgroundInstallAsset.mjs - what the client installs after world entry

The retail client plays every effect sound and effect on demand: the sound
cache (SoundResourceCache_Acquire, 8F7F90) loads a sound synchronously on
its first play and keeps released sounds on an LRU list. That is instant
because the archive is on the local disk. In the browser the same first play
is a network round trip, so the first hit, swing or skill of a session lags.

The port keeps the retail play path and makes the archive local instead:
this step publishes /assets/delivery/background-install.json, the ordered
list of presentation files combat can trigger, and the client installs them
into its persistent asset store once the world is ready. Later plays read
local bytes, as retail does.

Tiers are installed in order:
- combat: the player's own sounds (swings, hits, footsteps, damage),
  skill and common sounds, pet sounds, particle textures and skill-stage
  models;
- world-sounds: every other effect sound (monster families, ambience,
  fireworks).
Music is not listed: it streams per region, as retail does.

===========================================================================
*/

import { mkdir } from "node:fs/promises";
import path from "node:path";

import { publishBytesAtomically } from "../shared/atomicPublish.mjs";
import { listFiles } from "../shared/fsUtils.mjs";
import { publicRoot as defaultPublicRoot } from "../world/paths.mjs";

export const BACKGROUND_INSTALL_PUBLIC_PATH = "/assets/delivery/background-install.json";
export const BACKGROUND_INSTALL_FORMAT = "sro-background-install";
export const BACKGROUND_INSTALL_VERSION = 1;

const EFFECT_SOUND_ROOT = "/assets/audio/sfx/prim/snd/";
const COMBAT_SOUND_FOLDERS = [ "player", "skill", "skill2", "common", "cos", "ui" ];
const PARTICLE_TEXTURE_ROOT = "/assets/images/particles_extracted/";
const SKILL_STAGE_MODEL_ROOT = "/assets/skillfx/";

/*
================
backgroundInstallTier

The tier a published file belongs to, or null when it is not installed in
the background. `publicPath` is lower-case.
================
*/
export function backgroundInstallTier( publicPath ) {
	if ( publicPath.startsWith( EFFECT_SOUND_ROOT ) ) {
		const folder = publicPath.slice( EFFECT_SOUND_ROOT.length ).split( "/" )[0];
		return COMBAT_SOUND_FOLDERS.includes( folder ) ? "combat" : "world-sounds";
	}
	if ( publicPath.startsWith( PARTICLE_TEXTURE_ROOT ) ) return "combat";
	if ( publicPath.startsWith( SKILL_STAGE_MODEL_ROOT ) && publicPath.endsWith( ".glb" ) ) return "combat";
	return null;
}

/*
================
buildBackgroundInstallAsset

Lists the published files by tier, sorted for a stable output, and
publishes the install list. Returns the per-tier file counts.
================
*/
export async function buildBackgroundInstallAsset( { publicRoot = defaultPublicRoot } = {} ) {
	const tiers = { "combat": [], "world-sounds": [] };
	for ( const file of await listFiles( path.join( publicRoot, "assets" ), { missing: "empty" } ) ) {
		const publicPath = "/" + path.relative( publicRoot, file ).split( path.sep ).join( "/" );
		// Precompressed sidecars are served in place of their base file.
		if ( /\.(?:br|gz|zst)$/i.test( publicPath ) ) continue;
		const tier = backgroundInstallTier( publicPath.toLowerCase() );
		if ( tier ) tiers[tier].push( publicPath );
	}
	const document = {
		format: BACKGROUND_INSTALL_FORMAT,
		version: BACKGROUND_INSTALL_VERSION,
		tiers: Object.entries( tiers ).map( ( [name, paths] ) => ({ name, paths: paths.sort() }) )
	};
	const target = path.join( publicRoot, ...BACKGROUND_INSTALL_PUBLIC_PATH.slice( 1 ).split( "/" ) );
	await mkdir( path.dirname( target ), { recursive: true } );
	await publishBytesAtomically( target, Buffer.from( JSON.stringify( document ) ), {
		logLabel: "background-install"
	} );
	return Object.fromEntries( document.tiers.map( ( tier ) => [ tier.name, tier.paths.length ] ) );
}
