/*
===========================================================================

verify-dependencies.mjs - the no-library decision as a check

The #2534 decision record settles the ui.ts split on no new package:
no runtime library, and no new dev-time library either. A decision
people must remember decays; this gate reads it from a list instead.
dependency-allowlist.json names every package the client may depend
on - today exactly the ones package.json already carries. Two escapes
are refused:

- a dependency or devDependency in apps/client-next/package.json that
  is not on the list, used or not: adding the package is the decision
  being reversed, so it fails here before any import exists;
- an import of a package not on the list by any file under
  src/engine/runtime/ui/, the split's window modules. Node builtins
  stay allowed, as do the tree's own relative and @/ imports.

Changing the list is the deliberate act: one reviewed line in
dependency-allowlist.json, with the #2534 decision cited.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { imports, main, project, root } from "./project.mjs";

export const ALLOWLIST_PATH = "tools/dependency-allowlist.json";
export const WINDOW_MODULE_PREFIX = "src/engine/runtime/ui/";

// A bare specifier is a package name: not relative, not the @/ tree
// alias, not a node: builtin. Scoped names keep both segments.
function packageName( specifier ) {
	if ( specifier.startsWith( "./" ) || specifier.startsWith( "../" ) || specifier.startsWith( "@/" ) ) return null;
	if ( specifier.startsWith( "node:" ) ) return null;
	const parts = specifier.split( "/" );
	return specifier.startsWith( "@" ) ? parts.slice( 0, 2 ).join( "/" ) : parts[0];
}

/*
================
readAllowlist

The allowed package names, sorted, from the committed list.
================
*/
export function readAllowlist( base = root ) {
	const parsed = JSON.parse( fs.readFileSync( path.join( base, ALLOWLIST_PATH ), "utf8" ) );
	if ( !Array.isArray( parsed?.packages ) || parsed.packages.some( name => typeof name !== "string" ) ) {
		throw new Error( `${ALLOWLIST_PATH}: "packages" must be an array of package names` );
	}
	return new Set( parsed.packages );
}

/*
================
verifyDependencies

The manifest escape first (a new package fails even with no import),
then every window module's package imports against the same list.
Returns the issue list; empty is a pass.
================
*/
export function verifyDependencies( base = root ) {
	const issues = [];
	const allowed = readAllowlist( base );
	const manifest = JSON.parse( fs.readFileSync( path.join( base, "package.json" ), "utf8" ) );
	for ( const section of [ "dependencies", "devDependencies" ] ) {
		for ( const name of Object.keys( manifest[section] ?? {} ) ) {
			if ( !allowed.has( name ) ) {
				issues.push(
					`package.json: ${section} adds "${name}", which is not on the no-library list ` +
						`(${ALLOWLIST_PATH}); the #2534 decision allows no new package`
				);
			}
		}
	}
	const model = project( base );
	for ( const [file, source] of model.files ) {
		if ( !file.startsWith( WINDOW_MODULE_PREFIX ) ) continue;
		for ( const edge of imports( file, source ).edges ) {
			const name = packageName( edge.specifier );
			if ( name && !allowed.has( name ) ) {
				issues.push(
					`${file}: imports package "${name}", which is not on the no-library list (${ALLOWLIST_PATH})`
				);
			}
		}
	}
	return issues;
}

if ( process.argv[1] === import.meta.filename ) {
	main( () => verifyDependencies() );
}
