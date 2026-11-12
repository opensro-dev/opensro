/*
===========================================================================

avatar_model_screen.mjs - would the item-model builder refuse this model?

Two phases over stdin/stdout JSON, binary payloads as base64, never code.
"modifiers" reads each worn model's .bsr and returns the material set
paths and environment modifiers the builder would see. "verdicts" parses
each material set (.bmt) and runs the builder's own material-state rule
(characterMaterialState) on every material: the builder throws on an
animated or material-scoped environment map when the material carries no
environment coverage of its own, and one such model aborts the whole
asset build, so the avatar packer drops those rows before packing.

===========================================================================
*/
import { readFileSync } from "node:fs";
import { parseJmxBmtMaterialSet, parseJmxResourceBsr } from "../build/world/objects/formats.mjs";
import { characterMaterialState } from "../build/shared/characterMaterialState.mjs";

/*
================
screenModifiers

One worn model (.bsr) -> its material set paths and environment
modifiers, or the parse error that keeps the row out.
================
*/
function screenModifiers( request ) {
	try {
		const bytes = Buffer.from( request.bsr, "base64" );
		const object = parseJmxResourceBsr( bytes, request.code );
		return {
			code: request.code,
			materialPaths: object.materialPaths,
			environmentModifiers: object.modifiers?.environmentModifiers ?? []
		};
	} catch ( error ) {
		return { code: request.code, error: String( error?.message ?? error ) };
	}
}

/*
================
screenVerdict

One worn model's material sets -> ok, or the first refusal reason. A
material set that does not parse refuses the row: an unparseable model
cannot be proven safe for the builder.
================
*/
function screenVerdict( request ) {
	for ( const set of request.materials ) {
		let parsed;
		try {
			parsed = parseJmxBmtMaterialSet( Buffer.from( set.bytes, "base64" ), set.path );
		} catch ( error ) {
			return { code: request.code, ok: false, reason: `material set ${set.path}: ${error?.message ?? error}` };
		}
		for ( const material of parsed.materials ) {
			try {
				characterMaterialState( material.flags, request.environmentModifiers );
			} catch ( error ) {
				return { code: request.code, ok: false, reason: String( error?.message ?? error ) };
			}
		}
	}
	return { code: request.code, ok: true };
}

const mode = process.argv[2];
const requests = JSON.parse( readFileSync( 0, "utf8" ) );
if ( mode === "modifiers" ) {
	process.stdout.write( JSON.stringify( requests.map( screenModifiers ) ) );
} else if ( mode === "verdicts" ) {
	process.stdout.write( JSON.stringify( requests.map( screenVerdict ) ) );
} else {
	throw Error( `avatar_model_screen: unknown mode ${mode}` );
}
