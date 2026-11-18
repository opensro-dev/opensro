/*
===========================================================================

laterClientRoot.mjs - which later client the asset build imports from

The later-client steps (laterClientImports.mjs) read the pinned VSRO
archive. Before this module each step read SRO_VSRO_ROOT from the shell
alone, so a wrapper that dropped the variable (an agent's env filter, a
fresh terminal) built a release-variant tree with the plain world map,
no dungeon pages and an empty tomb minimap, and said so in four quiet
skip lines (11-17, the owner's Baghdad test).

The variant already knows: build_alexandria_variant.py records the VSRO
root it read in the variant stamp (variant_stamp.py, arguments.vsro).
So the build takes SRO_VSRO_ROOT when the shell names it, else the root
the game tree's variant was built from. Only a tree with neither (a
public clone, a plain v1.150 tree) skips. A named root that holds no
archive fails the build in its step: that tree was meant to carry the
later art.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";

import { gameRoot } from "../world/paths.mjs";

export const VSRO_ROOT_ENV = "SRO_VSRO_ROOT";

// variant_stamp.py: tree_state_root(variant) / STAMP_NAME, format tag.
const VARIANT_STAMP_PATH = [ ".state", "variant-stamp.json" ];
const VARIANT_STAMP_FORMAT = "sro-variant-stamp";

/*
================
stampedVsroRoot

The VSRO root the variant at variantRoot was built from, or "" when the
tree carries no variant stamp or the stamp names none. A stamp that is
there but unreadable throws: guessing would bring the quiet skip back.
================
*/
export function stampedVsroRoot( variantRoot = gameRoot ) {
	let text;
	try {
		text = readFileSync( path.join( variantRoot, ...VARIANT_STAMP_PATH ), "utf8" );
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return "";
		throw error;
	}
	const stamp = JSON.parse( text );
	if ( stamp?.format !== VARIANT_STAMP_FORMAT ) {
		throw Error( `${path.join( variantRoot, ...VARIANT_STAMP_PATH )} is not a variant stamp` );
	}
	const vsro = stamp.inputs?.arguments?.vsro;
	return typeof vsro === "string" ? vsro.trim() : "";
}

/*
================
resolveVsroRoot

The later client the build imports from and who named it: the shell's
SRO_VSRO_ROOT first, then the variant stamp, else none.
================
*/
export function resolveVsroRoot( env = process.env, stamped = stampedVsroRoot() ) {
	const named = (env[VSRO_ROOT_ENV] ?? "").trim();
	if ( named ) return { root: named, source: VSRO_ROOT_ENV };
	if ( stamped ) return { root: stamped, source: "the variant stamp" };
	return { root: "", source: "" };
}
