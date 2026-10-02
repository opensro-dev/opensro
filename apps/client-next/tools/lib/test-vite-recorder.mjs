/*
===========================================================================

test-vite-recorder.mjs - Vite as test processes see it under the recorder

test-input-recorder.mjs resolves "vite" here for test processes. Rolldown,
Vite's bundler, reads modules natively, out of sight of the fs hooks, so
build() adds a plugin that records every module in the build graph (tree-
shaken ones included) plus the files its native resolver consults: the
lockfile that pins dependency versions and the client's package and
tsconfig files. Everything else is Vite itself, re-exported unchanged.

===========================================================================
*/
import * as vite from "vite";
import path from "node:path";
import { fileURLToPath } from "node:url";

export * from "vite";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../.." );
const RESOLVER_INPUTS = [ "../../pnpm-lock.yaml", "package.json", "tsconfig.json", "tsconfig.node.json" ];

/*
================
build
================
*/
export function build( config = {} ) {
	const recorder = globalThis[Symbol.for( "sro.testInputs" )];
	if ( !recorder ) return vite.build( config );
	for ( const file of RESOLVER_INPUTS ) recorder.note( "E", path.join( clientRoot, file ) );
	const plugin = {
		name: "sro-test-inputs",
		buildEnd() {
			for ( const id of this.getModuleIds() ) {
				// Virtual modules ("\0...") and query suffixes are not files.
				if ( id.startsWith( "\0" ) ) continue;
				const file = id.replace( /[?#].*$/, "" );
				if ( path.isAbsolute( file ) ) recorder.note( "R", file );
			}
		}
	};
	return vite.build( { ...config, plugins: [ ...(config.plugins ?? []), plugin ] } );
}
