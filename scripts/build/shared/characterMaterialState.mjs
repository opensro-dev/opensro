/*
===========================================================================

characterMaterialState.mjs - a character material's native coverage and culling

v1.150 AAE3E0 plus CRTModEnvMap::Apply (AEE6D0). Coverage setup
precedes the modifier's active/reflection-quality/actor-opacity gates.

===========================================================================
*/
import { characterEnvironment, environmentCoverage } from "./characterEnvironment.mjs";

/*
================
characterMaterialState

materialIndex is the material's index in its BMT set (characterEnvironment);
the call also validates the environment descriptor, so every caller of this
rule refuses what the exporter would refuse.

Culling stays the material's own bit. An ADD-table draw sets 0x10000000
and A5E0E0 then skips RenderState_ApplyMaterialFlags, so its cull-none bit
never reaches the device, but only while AEE6D0 passes its quality and
actor-opacity gates; the client's shader drops back faces under exactly
those gates (pipelines.ts, ENVIRONMENT_ADD).
================
*/
export function characterMaterialState( flags, environmentModifiers = [], materialIndex ) {
	let alphaCutoff = (flags & 0x200) && !(flags & 0x30000000) ? 128 / 255 : 0;
	let fadeAlphaOnly = !!(flags & 0x40000000);
	for ( const modifier of environmentCoverage( flags, environmentModifiers ) ) {
		alphaCutoff = modifier.baseWords[5] & 2 ? 1 / 255 : 0;
		fadeAlphaOnly = true;
	}
	characterEnvironment( flags, environmentModifiers, materialIndex );
	return { alphaCutoff, fadeAlphaOnly: fadeAlphaOnly || alphaCutoff === 0, doubleSided: !!(flags & 1) };
}

/*
================
applyCharacterMaterialState

Write the native coverage, culling and environment descriptor onto one
exported glTF material.
================
*/
export function applyCharacterMaterialState( material, flags, environmentModifiers = [], materialIndex ) {
	const state = characterMaterialState( flags, environmentModifiers, materialIndex );
	delete material.alphaMode;
	delete material.alphaCutoff;
	if ( state.alphaCutoff ) {
		material.alphaMode = "MASK";
		material.alphaCutoff = state.alphaCutoff;
	}
	material.doubleSided = state.doubleSided;
	material.extras = { ...material.extras, sroFadeAlphaOnly: state.fadeAlphaOnly };
	const environment = characterEnvironment( flags, environmentModifiers, materialIndex );
	delete material.extras.sroEnvironment;
	delete material.extras.sroEnvironmentTexture;
	if ( environment ) material.extras.sroEnvironment = environment;
	material.extras.sroUnlit = !!(flags & 8);
	return material;
}
