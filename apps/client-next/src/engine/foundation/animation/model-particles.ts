/*
===========================================================================

model-particles.ts - the effect programs a model's BSR attaches to it

Reads a BSR's particle modifiers (ModDataParticle) from the model manifest
into attachment rows: the program, its bone or root, its offset in BSR
space and its native trigger and override fields.

===========================================================================
*/
import { bsrParticleRotation } from "./bsr-particle-transform";

/*
================
ModelParticle
================
*/
export interface ModelParticle {
	readonly source?: "equipment";
	readonly scale?: number;
	readonly effectPath: string;
	readonly triggerMs?: number;
	readonly nightOnly?: boolean;
	readonly deferred?: number;
	readonly rotation?: Float32Array;
	readonly bone: string;
	readonly root: boolean;
	readonly offset: readonly [number, number, number];
	// A one-shot program (atstructeffect loop 0); model particles loop by default.
	readonly loop?: false;
}
/*
================
ambientModelParticles

ACD4D0 -> AE3840 -> AEC870/AEC910. This projection admits static ambient
emitters, including their native rotation overrides. Animation and named
activation have separate lifecycle producers.
================
*/
export function ambientModelParticles( value: unknown ): readonly ModelParticle[] {
	return readModelParticles( value, false );
}
/*
================
animationModelParticles
================
*/
export function animationModelParticles( value: unknown ): readonly ModelParticle[] {
	return readModelParticles( value, true );
}
/*
================
readModelParticles
================
*/
function readModelParticles( value: unknown, animation: boolean ): readonly ModelParticle[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) || value.length > 128 ) throw Error( "Invalid model particle modifiers" );
	const result: ModelParticle[] = [];
	for ( const candidate of value ) {
		const raw = candidate as { kind?: unknown; stateId?: unknown; animationSetName?: unknown; entries?: unknown; };
		if (
			!raw || typeof raw !== "object" || (animation ?
				raw.kind !== 1 || !Number.isInteger( raw.stateId ) :
				raw.kind !== 2 || raw.stateId !== -1 || raw.animationSetName !== "ambient") ||
			!Array.isArray( raw.entries )
		) throw Error( "Unsupported non-ambient model particle modifier" );
		for ( const candidateEntry of raw.entries ) {
			const entry = candidateEntry as {
				effectPath?: unknown;
				boneName?: unknown;
				vector3c?: unknown;
				field00?: unknown;
				field4c?: unknown;
				flag53?: unknown;
				flags50?: unknown;
				vector54?: unknown;
			};
			if (
				result.length >= 128 || !entry || typeof entry.effectPath !== "string" ||
				typeof entry.boneName !== "string" || !Array.isArray( entry.vector3c ) || entry.vector3c.length !== 3 ||
				entry.vector3c.some( ( n: unknown ) => typeof n !== "number" || !Number.isFinite( n ) )
			) throw Error( "Invalid model particle attachment" );
			if (
				typeof entry.field00 !== "number" || !Number.isInteger( entry.field00 ) || entry.field00 < 0 ||
				entry.field00 > 0xffffffff || (animation ?
					typeof entry.field4c !== "number" || !Number.isInteger( entry.field4c ) || entry.field4c < 0 ||
					entry.field4c > 0xffffffff :
					entry.field4c !== 0) ||
				(typeof entry.flag53 !== "number" || !Number.isInteger( entry.flag53 ) || entry.flag53 < 0 ||
					entry.flag53 > 255) ||
				!Array.isArray( entry.flags50 ) || entry.flags50.length !== 3 ||
				entry.flags50.some( ( n: unknown, i: number ) =>
					typeof n !== "number" || !Number.isInteger( n ) || n < 0 || n > 255 ||
					(i === 1 && (animation ? (n & ~1) !== 0 : n !== 0))
				)
			) throw Error( "Unsupported model particle trigger or override" );
			if (
				entry.flag53 !== 0 &&
				(!Array.isArray( entry.vector54 ) || entry.vector54.length !== 3 ||
					!entry.vector54.every( n => typeof n === "number" && Number.isFinite( n ) ))
			) throw Error( "Invalid model particle rotation override" );
			const rotation = entry.flag53 !== 0 ?
				bsrParticleRotation( entry.vector54 as [number, number, number] ) :
				undefined;
			const effectPath = entry.effectPath.replaceAll( "\\", "/" ).toLowerCase();
			if (
				!effectPath.endsWith( ".efp" ) || effectPath.startsWith( "/" ) || effectPath.includes( ".." ) ||
				effectPath.includes( ":" )
			) throw Error( "Invalid model particle resource path" );
			const root = entry.field00 !== 0;
			if ( !root && !entry.boneName ) throw Error( "Missing particle attachment bone" );
			result.push( {
				effectPath,
				...(animation ? { triggerMs: entry.field4c as number } : {}),
				nightOnly: !!(entry.flags50[1] & 1),
				deferred: entry.flags50[0] || undefined,
				rotation,
				bone: entry.boneName,
				root,
				offset: [ entry.vector3c[0], entry.vector3c[1], -entry.vector3c[2] ]
			} );
		}
	}
	return result;
}
