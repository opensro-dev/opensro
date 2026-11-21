/*
===========================================================================

effect-programs.mjs - compile effects from a catalogue-shaped value in tests

The worker reads one published effect file and its meshes (#3951) and
compiles them with program.ts. Tests hold effects as one catalogue-shaped
value instead: a small fixture written by hand, or the whole published
closure (scripts/lib/effectCatalog.mjs readEffectCatalogSync). This helper
turns one effect of that value into the published per-effect bytes and runs
the worker's own parse and compile on them, so every test exercises the
shipped decoder and none spells the file layout.

===========================================================================
*/
import "./native-source-loader.mjs";
import { effectProgramFromCatalog } from "../../../../scripts/lib/effectCatalog.mjs";

const { compileEffectProgram, parseEffectProgram } = await import(
	"../../src/engine/runtime/assets/worker/effects/program/program.ts"
);

/*
================
createCatalogEffects

A decoder over one catalogue: decode( catalogue, path ) compiles the effect
at path. The catalogue is a value or its JSON bytes; bytes are parsed once
per decoder, as tests decode many effects of one closure.
================
*/
export function createCatalogEffects() {
	let source = null, catalog = null;
	return {
		/*
		================
		decode
		================
		*/
		decode( input, path ) {
			if ( input !== source ) {
				catalog = input instanceof Uint8Array ?
					JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( input ) ) :
					input;
				source = input;
			}
			const { bytes, meshes } = effectProgramFromCatalog( catalog, path );
			return compileEffectProgram( parseEffectProgram( bytes ), meshes, path );
		}
	};
}
