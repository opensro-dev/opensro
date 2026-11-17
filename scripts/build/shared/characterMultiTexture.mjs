/*
===========================================================================

characterMultiTexture.mjs - the extra texture a ModDataMultiTex draws

ModDataMultiTex and ModDataMultiTexRev (CRTModMultiTex 0xAEDD20,
CRTModMultiTexRev 0xAEE1A0) bind one more texture beside a material's own:
MultiTex at the next texture stage, Rev at stage 0 with the material's own
texture moved to stage 1 (CAnimInstance_RenderTrack 0xA5E419). The GLB
carries that texture as an embedded image, so the client draws the stage
without another fetch. One owner for the embedding; exportGlb asks here.

===========================================================================
*/
import { readCharacterTexture } from "./nativeCharacterTextures.mjs";

/*
================
multiTexturePath

The game path a payload names ("prim\mtrl\etc\robe_line.ddj"), in the
forward-slash lower-case form the texture reader resolves.
================
*/
export function multiTexturePath( texturePath ) {
	return texturePath.replaceAll( "\\", "/" ).toLowerCase();
}

/*
================
embedCharacterMultiTextures

Embed every multi-texture the material's sroBsrModifiers names, once per
GLB, and record their texture indices in extras.sroMultiTextures, in the
order of multiTextureModifiers (the client pairs them by position). read
loads one game texture as { bytes, mime }.
================
*/
export function embedCharacterMultiTextures( json, material, appendView, read = readCharacterTexture ) {
	const modifiers = material.extras?.sroBsrModifiers?.multiTextureModifiers ?? [];
	delete material.extras?.sroMultiTextures;
	if ( !modifiers.length ) return;
	json.images ??= [];
	json.textures ??= [];
	json.samplers ??= [ { magFilter: 9729, minFilter: 9987, wrapS: 10497, wrapT: 10497 } ];
	material.extras.sroMultiTextures = modifiers.map( modifier => {
		const gamePath = multiTexturePath( modifier.texturePath ), name = "sro-multitexture:" + gamePath;
		let source = json.images.findIndex( image => image.name === name );
		if ( source < 0 ) {
			const texture = read( gamePath );
			source = json.images.length;
			json.images.push( { name, bufferView: appendView( texture.bytes ), mimeType: texture.mime } );
		}
		let index = json.textures.findIndex( texture => texture.source === source );
		if ( index < 0 ) {
			index = json.textures.length;
			json.textures.push( { source, sampler: 0 } );
		}
		return index;
	} );
}
