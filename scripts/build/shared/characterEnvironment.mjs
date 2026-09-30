/*
===========================================================================

characterEnvironment.mjs - authored secondary reflection texture binding

===========================================================================
*/
import { readCharacterTexture } from "./nativeCharacterTextures.mjs";

// AADDC0 actually loads two entries, despite constructing four path strings.
const textures = [ "prim/mtrl/etc/spheremap_gray.ddj", "prim/mtrl/etc/spheremap_highlight.ddj" ];

/*
================
characterEnvironment

Validate the native ambient environment modifier before publishing its descriptor.
================
*/
export function characterEnvironment( flags, modifiers = [] ) {
	if ( flags & 0x10000 || !modifiers.length ) return undefined;
	const m = modifiers.at( -1 );
	if ( m.kind !== 2 || m.stateId !== -1 || m.baseWords[1] !== 1 || m.baseWords[3] !== 0xffffffff ) {
		throw Error( "Environment reflection requires ambient general ownership" );
	}
	if ( !(m.baseWords[5] & 1) ) throw Error( "Unpublished environment blend variant requires its own stage contract" );
	const textureId = m.words24[0];
	if ( textureId === 0xffffffff ) {
		throw Error( "Unpublished null environment texture requires its own stage contract" );
	}
	if ( textureId !== 0xffffffff && !textures[textureId] ) {
		throw Error( "Native environment texture ID outside loaded table" );
	}
	return { textureId, mode: 1 };
}

// The model image owner carries this secondary texture through assembly,
// residency and device restoration exactly like its base textures.

/*
================
embedCharacterEnvironment

Share secondary textures by authored identity through the model image owner.
================
*/
export function embedCharacterEnvironment( json, material, appendView, cache ) {
	const descriptor = material.extras?.sroEnvironment;
	delete material.extras?.sroEnvironmentTexture;
	if ( !descriptor || descriptor.textureId === 0xffffffff ) return;
	const id = descriptor.textureId;
	let index = cache.get( id );
	if ( index === undefined ) {
		json.images ??= [];
		json.textures ??= [];
		const name = "sro-environment-" + id;
		let source = json.images.findIndex( image => image.name === name );
		if ( source < 0 ) {
			const texture = readCharacterTexture( textures[id] );
			source = json.images.length;
			json.images.push( { name, bufferView: appendView( texture.bytes ), mimeType: texture.mime } );
		}
		index = json.textures.findIndex( texture => texture.source === source );
		if ( index < 0 ) {
			index = json.textures.length;
			json.textures.push( { source, sampler: 0 } );
		}
		cache.set( id, index );
	}
	material.extras.sroEnvironmentTexture = index;
}
