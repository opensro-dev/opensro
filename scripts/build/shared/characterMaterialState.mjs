// v1.150 AAE3E0 plus CRTModEnvMap::Apply (AEE6D0). Coverage setup
// precedes the modifier's active/reflection-quality/actor-opacity gates.
import { characterEnvironment } from "./characterEnvironment.mjs";
export function characterMaterialState( flags, environmentModifiers = [] ) {
	let alphaCutoff = (flags & 0x200) && !(flags & 0x30000000) ? 128 / 255 : 0;
	let fadeAlphaOnly = !!(flags & 0x40000000);
	if ( !(flags & 0x10000) ) {
		for ( const modifier of environmentModifiers ) {
			if (
				modifier.kind !== 2 || modifier.stateId !== -1 || modifier.baseWords[1] !== 1 ||
				modifier.baseWords[3] !== 0xffffffff
			) {
				throw Error( "Animated or material-scoped environment coverage needs runtime ownership" );
			}
			alphaCutoff = modifier.baseWords[5] & 2 ? 1 / 255 : 0;
			fadeAlphaOnly = true;
		}
	}
	return { alphaCutoff, fadeAlphaOnly: fadeAlphaOnly || alphaCutoff === 0, doubleSided: !!(flags & 1) };
}

export function applyCharacterMaterialState( material, flags, environmentModifiers = [] ) {
	const state = characterMaterialState( flags, environmentModifiers );
	delete material.alphaMode;
	delete material.alphaCutoff;
	if ( state.alphaCutoff ) {
		material.alphaMode = "MASK";
		material.alphaCutoff = state.alphaCutoff;
	}
	material.doubleSided = state.doubleSided;
	material.extras = { ...material.extras, sroFadeAlphaOnly: state.fadeAlphaOnly };
	const environment = characterEnvironment( flags, environmentModifiers );
	delete material.extras.sroEnvironment;
	delete material.extras.sroEnvironmentTexture;
	if ( environment ) material.extras.sroEnvironment = environment;
	material.extras.sroUnlit = !!(flags & 8);
	return material;
}
