/*
===========================================================================

expandedBuild.mjs - whether the build in use adds content to v1.150

A game root made by the overlay tool build_alexandria_variant.py carries the manifest
named below. Builds from it add skills, items, portals, effects and sounds to
the native data, so a gate that pins the native catalog byte for byte cannot
hold there; it checks that the native rows survive instead. A native build
has no such manifest and keeps the exact check.

The game root is asked of its one resolver (scripts/build/world/paths.mjs).

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { gameRoot } from "../build/world/paths.mjs";

export const VARIANT_MANIFEST = ".opensro-alexandria-variant.json";
export const EXPANDED_BUILD = fs.existsSync( path.join( gameRoot, VARIANT_MANIFEST ) );
