/*
===========================================================================

buildCharacterSelectLizard.mjs - the character-select table lizard (interface_lizard) GLB

Unlike the China lion / Europe knight idols (static meshes), interface_lizard.bsr is a
fully skinned, animated character resource: skeleton prim/skel/nature/interface_lizard.bsk
+ three clips prim/ani/nature/interface_lizard_{stand,move,run}.ban. The native
CPSCharacterSelect_OnCreate (0x73BF00) loads it as the 3rd CInterfaceModel idol and arms
a CAnimationCallback on it (this+0x10c) so the engine can sequence the clips; the visible
"walking on the table" is baked root-motion (Bip01 translation) inside the move/run clips.

We reuse the skinned-avatar pipeline (assembleAvatar + avatarToGlb) but emit ALL three
clips (roles stand/move/run) instead of the crowd's walk/ride, so the runtime can drive the
idle/dash-out/walk-back wander. Output:
.generated/client-public/assets/character-select/interface_lizard.glb

===========================================================================
*/

import { claimPublicFile } from "../shared/publicationLedger.mjs";
import fs from "node:fs";
import path from "node:path";
import { assembleAvatar } from "../char/buildAvatar.mjs";
import { avatarToGlb } from "../char/exportGlb.mjs";
import { parseBan } from "../char/formats.mjs";
import { runConvertImages } from "../shared/convertImagesRunner.mjs";
import { loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { gameRoot, publicAssetsRoot } from "./paths.mjs";

const isCli = isMainScript( import.meta.url );

const LIZARD_BSR = "res/interface/interface_lizard.bsr";

// Role name <- animation filename suffix. Order matters only for logging.
const CLIP_ROLES = [
	{ role: "stand", suffix: "_stand" },
	{ role: "move", suffix: "_move" },
	{ role: "run", suffix: "_run" }
];

const outPath = path.join(
	publicAssetsRoot,
	"character-select",
	"interface_lizard.glb"
);

/*
================
convertTextures
================
*/
async function convertTextures() {
	console.log( "[lizard] converting textures (prim/mtrl/interface, prim/mtrl/nature) ..." );
	for ( const tree of [ "prim/mtrl/interface", "prim/mtrl/nature" ] ) {
		const res = await runConvertImages( [ tree ] );
		if ( res.status !== 0 ) {
			console.warn( `[lizard] texture conversion (${tree}) exited ${res.status}; model may be untextured` );
		}
	}
}

/*
================
buildCharacterSelectLizard
================
*/
export async function buildCharacterSelectLizard( { skipTextures = false } = {} ) {
	if ( !skipTextures ) await convertTextures();

	const avatar = await assembleAvatar( LIZARD_BSR );

	// Replace the auto-picked (likely empty) clip set with the lizard's stand/move/run.
	const clipName = ( p ) => p.toLowerCase().split( /[\\/]/ ).pop() ?? "";
	const clips = [];
	for ( const { role, suffix } of CLIP_ROLES ) {
		const animPath = avatar.animationPaths?.find( ( p ) => clipName( p ).includes( suffix ) );
		if ( !animPath ) {
			console.warn( `[lizard] no animation for role "${role}" (suffix ${suffix})` );
			continue;
		}
		const clip = parseBan( await loadDataAsset( animPath ), animPath );
		clips.push( { role, path: animPath, clip } );
	}
	// Clips are exported verbatim (authored Bip01 root motion). The move clip walks IN from
	// [7.02,0.29,-9.96] to stand home [3.47,0.29,-7.79] relative to the fixed
	// CInterfaceModel transform -> that off-table start is the bottom-edge walk-on the user
	// sees after the 2s entry dolly. Do NOT rebase; see verified doc section 3/5.
	avatar.clips = clips;
	avatar.clip = clips.find( ( c ) => c.role === "move" )?.clip ?? clips[0]?.clip ?? null;

	const glb = avatarToGlb( avatar );
	fs.mkdirSync( path.dirname( outPath ), { recursive: true } );
	fs.writeFileSync( outPath, glb );
	claimPublicFile( outPath );

	if ( isCli ) {
		console.log(
			`[lizard] OK ${avatar.name} -> ${path.relative( gameRoot, outPath )} ` +
				`(${glb.length} B, bones=${avatar.skeleton.boneCount}, parts=${avatar.parts.length}, ` +
				`clips=[${
					clips.map( ( c ) => `${c.role}:${c.clip.frameCount}f/${c.clip.durationMs}ms` ).join( ", " )
				}], ` +
				`materials=${avatar.materials.size})`
		);
	}

	return {
		bytes: glb.length,
		bones: avatar.skeleton.boneCount,
		parts: avatar.parts.length,
		clips: clips.map( ( c ) => c.role )
	};
}

if ( isCli ) {
	await buildCharacterSelectLizard( { skipTextures: process.argv.slice( 2 ).includes( "--skip-textures" ) } );
}
