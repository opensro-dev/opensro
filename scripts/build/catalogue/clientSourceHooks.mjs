/*
===========================================================================

clientSourceHooks.mjs - import client-next's TypeScript from a build script

The catalogue runs the client's own tooltip code so the website says what
the game says. Client modules import each other without extensions and
through the "@/" alias; this resolve hook maps both onto the source tree,
and Node's type stripping loads the .ts files.

===========================================================================
*/
import { registerHooks } from "node:module";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

const sourceRoot = new URL( "../../../apps/client-next/src/", import.meta.url );

registerHooks( {
	/*
	================
	resolve
	================
	*/
	resolve( specifier, context, nextResolve ) {
		let target;
		if ( specifier.startsWith( "@/" ) ) target = new URL( specifier.slice( 2 ), sourceRoot );
		else if ( specifier.startsWith( "." ) && context.parentURL?.endsWith( ".ts" ) ) {
			target = new URL( specifier, context.parentURL );
		}
		if ( target && !/\.[a-z]+$/i.test( target.pathname ) ) {
			const direct = new URL( target.href + ".ts" ), index = new URL( target.href + "/index.ts" );
			if ( existsSync( fileURLToPath( direct ) ) ) target = direct;
			else if ( existsSync( fileURLToPath( index ) ) ) target = index;
		}
		return nextResolve( target?.href ?? specifier, context );
	}
} );
